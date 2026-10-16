// bff: ponto de entrada. Aqui só "ligamos os fios" (config, adapters, cadeia de
// decorators, servidor): nenhuma regra de negócio mora no main. "bff healthcheck"
// é o subcomando do healthcheck do Docker (a imagem distroless não tem curl).
//
// Para quem está começando: em Go, um programa executável é sempre o pacote
// "main", e a execução começa pela função main(). Este arquivo é a "raiz de
// composição" da arquitetura limpa: o único lugar que conhece todas as peças
// concretas (Redis, gRPC, HTTP) e as encaixa. As outras camadas só conhecem
// interfaces, por isso dá para testá-las com peças falsas.
package main

// Os imports ficam em três grupos (convenção do Go): biblioteca padrão,
// bibliotecas de terceiros e pacotes do próprio projeto. O apelido
// "httpadapter" evita a colisão com o pacote padrão "net/http".
import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"

	"github.com/velosobr/passarim-bff/internal/adapter/cache"
	"github.com/velosobr/passarim-bff/internal/adapter/grpcclient"
	httpadapter "github.com/velosobr/passarim-bff/internal/adapter/http"
	"github.com/velosobr/passarim-bff/internal/adapter/http/middleware"
	"github.com/velosobr/passarim-bff/internal/adapter/media"
	"github.com/velosobr/passarim-bff/internal/adapter/metrics"
	"github.com/velosobr/passarim-bff/internal/adapter/resilience"
	"github.com/velosobr/passarim-bff/internal/adapter/server"
	"github.com/velosobr/passarim-bff/internal/adapter/telemetry"
	"github.com/velosobr/passarim-bff/internal/config"
	"github.com/velosobr/passarim-bff/internal/usecase"
)

// main decide entre dois modos:
//   - "bff healthcheck": faz um GET em /healthz e sai com 0 (saudável) ou 1.
//     O Docker chama isso de tempos em tempos (HEALTHCHECK do Dockerfile).
//   - sem argumentos: sobe o BFF de verdade, chamando run.
//
// Toda a lógica fica em run, que devolve um erro em vez de encerrar o
// processo. Assim o teste consegue chamar run e olhar o erro; main só
// transforma esse erro num log e no código de saída 1.
func main() {
	// os.Args[0] é o nome do programa; os.Args[1] é o primeiro argumento.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(server.Healthcheck(os.Getenv("HTTP_ADDR")))
	}
	if err := run(os.Getenv); err != nil {
		// O logger de run pode nem ter sido criado (a config falhou antes),
		// então criamos um logger JSON só para registrar o motivo da saída.
		slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("bff encerrou com erro", "error", err)
		os.Exit(1)
	}
}

// watchConnection loga as transições do estado da conexão gRPC com o catalog (spec §6).
// O estado NÃO afeta o /readyz: serve só de métrica e de pista nos logs.
//
// Uma conexão gRPC passa por estados (Idle, Connecting, Ready,
// TransientFailure, Shutdown). WaitForStateChange bloqueia até o estado sair
// do valor informado e devolve false quando o ctx é cancelado: é isso que
// encerra o laço no desligamento. Esta função roda numa goroutine própria
// (veja "go watchConnection" em run), porque ficaria bloqueada para sempre.
// Uma goroutine é uma função rodando em paralelo, leve e gerenciada pelo Go;
// "go f()" a inicia e segue em frente sem esperar.
func watchConnection(ctx context.Context, conn *grpc.ClientConn, log *slog.Logger) {
	state := conn.GetState()
	conn.Connect() // sai de Idle e começa a conectar já na subida
	for conn.WaitForStateChange(ctx, state) {
		state = conn.GetState()
		log.Info("conexão com o catalog mudou de estado", "state", state.String())
	}
}

// run monta e sobe o BFF, e só retorna quando ele termina: com nil num
// desligamento limpo (Ctrl+C ou docker stop), ou com o erro que impediu a
// subida ou derrubou um dos servidores.
//
// Recebe getenv (a função que lê variáveis de ambiente) como parâmetro em vez
// de chamar os.Getenv direto: no teste, passamos um mapa falso e simulamos
// qualquer configuração sem mexer no ambiente real.
//
// A ordem de montagem é: configuração → logger → sinais de desligamento →
// traces → métricas → Redis → cliente gRPC → cadeia de decorators → casos de
// uso e rotas HTTP → middlewares → portas de rede → servidor.
func run(getenv func(string) string) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	// slog é o logger estruturado da biblioteca padrão. Em JSON, cada linha de
	// log vira um objeto com campos (nível, mensagem, chaves extras), fácil de
	// filtrar em ferramentas de log. SetDefault faz o slog.Info() "solto" usar
	// esta mesma configuração.
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	// Contexto cancelado ao receber SIGINT (Ctrl+C) ou SIGTERM (docker stop).
	//
	// Um context.Context carrega um sinal de "pare" que atravessa o programa:
	// quem recebe o ctx observa ctx.Done() e encerra o que estiver fazendo.
	// "defer stop()" agenda a chamada de stop para o fim de run, aconteça o
	// que acontecer (é o jeito do Go de garantir uma limpeza).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Traces do OpenTelemetry: cada requisição vira um "span" com tempo e
	// detalhes, enviado ao coletor. shutdownTrace esvazia o buffer no fim.
	shutdownTrace, err := telemetry.Setup(ctx, cfg.OTelEndpoint, cfg.OTelServiceName)
	if err != nil {
		return err
	}
	// Métricas do Prometheus, servidas em /metrics na porta separada.
	prom := metrics.NewProm()

	// Redis: os prazos de discagem, leitura e escrita seguem o REDIS_TIMEOUT
	// (ver cache.NewRedisClient).
	redisClient, err := cache.NewRedisClient(cfg.RedisURL, cfg.RedisTimeout)
	if err != nil {
		return err
	}

	// Cliente gRPC do catalog. O TLS é opcional (só quando a CA é informada).
	// O stats handler do otelgrpc cria um span para cada chamada gRPC, ligado
	// ao span da requisição HTTP que a originou.
	grpcClient, err := grpcclient.New(grpcclient.Config{
		Addr: cfg.CatalogAddr, Logger: log,
		TLS: grpcclient.TLS{CAFile: cfg.CatalogTLSCAFile, CertFile: cfg.CatalogTLSCertFile, KeyFile: cfg.CatalogTLSKeyFile, ServerName: cfg.CatalogTLSServerName},
	}, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		return err
	}
	// A métrica de estado da conexão é calculada na hora da coleta, por isso
	// passamos uma função, e não um número.
	prom.SetConnState(func() float64 { return float64(grpcClient.Conn().GetState()) })
	go watchConnection(ctx, grpcClient.Conn(), log) // loga cada transição de estado (a métrica mostra o valor atual)

	// A cadeia da spec §5.1: cache → breaker → retry/timeout → gRPC. Todos são CatalogReader.
	//
	// Isto é o padrão "decorator": cada camada implementa a mesma interface
	// (usecase.CatalogReader) e embrulha a anterior, acrescentando um
	// comportamento. Montamos de dentro para fora, então a requisição passa
	// na ordem inversa: primeiro o cache, que só chama o breaker quando não
	// tem a resposta; o breaker chama o retry; o retry chama o gRPC.
	var reader usecase.CatalogReader = grpcClient
	reader = resilience.NewRetry(reader, resilience.RetryConfig{MaxRetries: cfg.CatalogMaxRetries, AttemptTimeout: cfg.CatalogAttemptTimeout}, prom)
	reader = resilience.NewBreaker(reader, resilience.DefaultBreakerConfig(), log, prom)
	reader = cache.New(reader, cache.NewRedisStore(redisClient, cfg.RedisTimeout),
		cache.Config{FreshTTL: cfg.CacheFreshTTL, StaleTTL: cfg.CacheStaleTTL, Budget: cfg.RequestBudget}, log, prom)

	// O catalog devolve só chaves de mídia; o media.Builder (mediaURLs) monta a
	// URL pública a partir de MEDIA_BASE_URL (ADR-0006).
	mediaURLs, err := media.New(cfg.MediaBaseURL, log)
	if err != nil {
		return err
	}
	// Readiness diz ao /readyz se o BFF aceita tráfego. O servidor o desliga
	// no começo do desligamento, para o balanceador parar de mandar pedidos.
	ready := &server.Readiness{}
	// Os três casos de uso recebem o reader já decorado e o orçamento de
	// tempo de cada requisição (REQUEST_BUDGET).
	h := &httpadapter.Handler{
		List:    usecase.ListSpecies{Catalog: reader, Budget: cfg.RequestBudget},
		Get:     usecase.GetSpecies{Catalog: reader, Budget: cfg.RequestBudget},
		Filters: usecase.ListFilters{Catalog: reader, Budget: cfg.RequestBudget},
		Media:   mediaURLs, Log: log, Ready: ready.Ready,
	}

	// Descobre o IP real do cliente: atrás do Traefik, o IP da conexão é o do
	// proxy, e o do cliente vem no cabeçalho (X-Forwarded-For), mas só quando
	// a conexão vem de um proxy confiável (TRUSTED_PROXIES).
	clientIP := middleware.NewIPResolver(cfg.TrustedProxies, cfg.ClientIPHeader)
	// Rate limit por IP, em memória de cada réplica (ADR-0015). IPs parados
	// há mais de 3 minutos são esquecidos na faxina seguinte, e no máximo
	// RATE_LIMIT_MAX_IPS ficam na memória.
	limiter := middleware.NewRateLimiter(middleware.RateLimitConfig{
		RPS: cfg.RateLimitRPS, Burst: cfg.RateLimitBurst, MaxIPs: cfg.RateLimitMaxIPs, IdleAfter: 3 * time.Minute, OnLimited: prom.RateLimited,
	}, clientIP, time.Now)
	prom.SetTrackedIPs(func() float64 { return float64(limiter.Tracked()) })
	// A cada minuto, uma goroutine faz a faxina dos IPs parados, até o ctx
	// ser cancelado.
	go limiter.Run(ctx, time.Minute)

	// Ordem de fora para dentro (spec §7.1). O otelhttp fica mais externo para o span cobrir tudo.
	//
	// O primeiro middleware da lista é o mais externo: Recovery pega um pânico
	// de qualquer camada de dentro; RequestID dá um id à requisição antes do
	// log de acesso; o rate limit e o Limits (só GET e HEAD, corpo com
	// tamanho máximo) barram o pedido antes de ele chegar às rotas.
	handler := middleware.Chain(h.Routes(),
		middleware.Recovery(log), middleware.RequestID(), middleware.AccessLog(log, clientIP, prom),
		middleware.SecurityHeaders(), limiter.Middleware(), middleware.Limits())
	handler = otelhttp.NewHandler(handler, "bff", otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return "HTTP " + middleware.MethodLabel(r.Method) }))

	// As métricas ficam num servidor separado, numa porta que não é exposta
	// ao público: só o Prometheus, na rede interna, a acessa.
	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", prom.Handler())

	// Abrimos as portas aqui (e não dentro do servidor) para que um erro como
	// "porta em uso" apareça já na subida. Se a segunda falhar, fechamos a
	// primeira para não deixá-la aberta à toa.
	apiLn, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return err
	}
	metricsLn, err := net.Listen("tcp", cfg.MetricsAddr)
	if err != nil {
		_ = apiLn.Close()
		return err
	}
	log.Info("bff no ar", "http", apiLn.Addr().String(), "metrics", metricsLn.Addr().String())

	// server.Run atende até o ctx ser cancelado (ou um dos servidores
	// falhar) e então desliga com calma: marca "não pronto", espera
	// DrainDelay, termina as requisições em andamento (até ShutdownTimeout) e
	// chama, em ordem, as funções de OnShutdown para fechar traces, gRPC e
	// Redis.
	return server.Run(ctx, server.Options{
		Listener: apiLn, MetricsListener: metricsLn, Handler: handler, MetricsHandler: metricsMux,
		DrainDelay: cfg.ShutdownDrainDelay, ShutdownTimeout: cfg.ShutdownTimeout, Ready: ready, Log: log,
		OnShutdown: []func(context.Context){
			func(c context.Context) { _ = shutdownTrace(c) }, // flush dos traces
			func(context.Context) { _ = grpcClient.Close() },
			func(context.Context) { _ = redisClient.Close() },
		},
	})
}
