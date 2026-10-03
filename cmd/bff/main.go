// bff: ponto de entrada. Aqui só "ligamos os fios" (config, adapters, cadeia de
// decorators, servidor): nenhuma regra de negócio mora no main. "bff healthcheck"
// é o subcomando do healthcheck do Docker (a imagem distroless não tem curl).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
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

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(server.Healthcheck(os.Getenv("HTTP_ADDR")))
	}
	if err := run(os.Getenv); err != nil {
		slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("bff encerrou com erro", "error", err)
		os.Exit(1)
	}
}

// watchConnection loga as transições do estado da conexão gRPC com o catalog (spec §6).
// O estado NÃO afeta o /readyz: serve só de métrica e de pista nos logs.
func watchConnection(ctx context.Context, conn *grpc.ClientConn, log *slog.Logger) {
	state := conn.GetState()
	conn.Connect() // sai de Idle e começa a conectar já na subida
	for conn.WaitForStateChange(ctx, state) {
		state = conn.GetState()
		log.Info("conexão com o catalog mudou de estado", "state", state.String())
	}
}

func run(getenv func(string) string) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	// Contexto cancelado ao receber SIGINT (Ctrl+C) ou SIGTERM (docker stop).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTrace, err := telemetry.Setup(ctx, cfg.OTelEndpoint, cfg.OTelServiceName)
	if err != nil {
		return err
	}
	prom := metrics.NewProm()

	// Redis: a URL pode ter senha, então o erro nunca a repete.
	redisOpt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return errors.New("REDIS_URL: URL inválida (ex.: redis://redis:6379)")
	}
	redisOpt.MaxRetries = -1 // sem retries internos: o cache tem timeout próprio e falha rápido
	redisClient := redis.NewClient(redisOpt)

	grpcClient, err := grpcclient.New(grpcclient.Config{
		Addr: cfg.CatalogAddr, Logger: log,
		TLS: grpcclient.TLS{CAFile: cfg.CatalogTLSCAFile, CertFile: cfg.CatalogTLSCertFile, KeyFile: cfg.CatalogTLSKeyFile, ServerName: cfg.CatalogTLSServerName},
	}, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		return err
	}
	prom.SetConnState(func() float64 { return float64(grpcClient.Conn().GetState()) })
	go watchConnection(ctx, grpcClient.Conn(), log) // loga cada transição de estado (a métrica mostra o valor atual)

	// A cadeia da spec §5.1: cache → breaker → retry/timeout → gRPC. Todos são CatalogReader.
	var reader usecase.CatalogReader = grpcClient
	reader = resilience.NewRetry(reader, resilience.RetryConfig{MaxRetries: cfg.CatalogMaxRetries, AttemptTimeout: cfg.CatalogAttemptTimeout}, prom)
	reader = resilience.NewBreaker(reader, resilience.DefaultBreakerConfig(), log, prom)
	reader = cache.New(reader, cache.NewRedisStore(redisClient, cfg.RedisTimeout),
		cache.Config{FreshTTL: cfg.CacheFreshTTL, StaleTTL: cfg.CacheStaleTTL, Budget: cfg.RequestBudget}, log, prom)

	mediaURLs, err := media.New(cfg.MediaBaseURL, log)
	if err != nil {
		return err
	}
	ready := &server.Readiness{}
	h := &httpadapter.Handler{
		List:    usecase.ListSpecies{Catalog: reader, Budget: cfg.RequestBudget},
		Get:     usecase.GetSpecies{Catalog: reader, Budget: cfg.RequestBudget},
		Filters: usecase.ListFilters{Catalog: reader, Budget: cfg.RequestBudget},
		Media:   mediaURLs, Log: log, Ready: ready.Ready,
	}

	clientIP := middleware.NewIPResolver(cfg.TrustedProxies, cfg.ClientIPHeader)
	limiter := middleware.NewRateLimiter(middleware.RateLimitConfig{
		RPS: cfg.RateLimitRPS, Burst: cfg.RateLimitBurst, MaxIPs: cfg.RateLimitMaxIPs, IdleAfter: 3 * time.Minute, OnLimited: prom.RateLimited,
	}, clientIP, time.Now)
	prom.SetTrackedIPs(func() float64 { return float64(limiter.Tracked()) })
	go limiter.Run(ctx, time.Minute)

	// Ordem de fora para dentro (spec §7.1). O otelhttp fica mais externo para o span cobrir tudo.
	handler := middleware.Chain(h.Routes(),
		middleware.Recovery(log), middleware.RequestID(), middleware.AccessLog(log, clientIP, prom),
		middleware.SecurityHeaders(), limiter.Middleware(), middleware.Limits())
	handler = otelhttp.NewHandler(handler, "bff", otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return "HTTP " + r.Method }))

	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", prom.Handler())

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
