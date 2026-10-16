// Package config lê a configuração das variáveis de ambiente (12-factor).
// Os erros citam o NOME da variável, nunca o valor: REDIS_URL pode ter senha.
//
// "12-factor" é um conjunto de boas práticas para serviços: a configuração
// que muda entre ambientes (endereços, senhas, limites) vem de variáveis de
// ambiente, e não de arquivos dentro da imagem. A mesma imagem Docker roda no
// notebook e em produção; só o ambiente muda. Toda variável opcional tem um
// valor padrão pensado para o docker compose do passarim-docs.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Config é a configuração já lida e validada. Depois de Load, o resto do
// programa usa estes campos e nunca mais lê o ambiente.
//
// Campos, com a variável de ambiente de cada um:
//   - HTTPAddr (HTTP_ADDR, padrão ":8080"): onde a API REST escuta.
//   - MetricsAddr (METRICS_ADDR, ":9091"): onde fica o /metrics do Prometheus.
//   - CatalogAddr (CATALOG_ADDR, obrigatória): host:porta do gRPC do catalog.
//   - CatalogTLS* (CATALOG_TLS_*): TLS opcional com o catalog. Só a CA liga o
//     TLS simples; cert + chave ligam o mTLS (o BFF também se identifica).
//   - RedisURL (REDIS_URL, obrigatória) e RedisTimeout (REDIS_TIMEOUT, 100ms):
//     o Redis do cache e o prazo de cada operação nele.
//   - MediaBaseURL (MEDIA_BASE_URL, obrigatória): prefixo das URLs públicas de
//     fotos e cantos (ADR-0006).
//   - RequestBudget (REQUEST_BUDGET, 3s): tempo máximo de uma requisição.
//   - CatalogAttemptTimeout (CATALOG_ATTEMPT_TIMEOUT, 2s) e CatalogMaxRetries
//     (CATALOG_MAX_RETRIES, 2): prazo de cada tentativa ao catalog e quantas
//     vezes repetir.
//   - CacheFreshTTL (CACHE_FRESH_TTL, 10m) e CacheStaleTTL (CACHE_STALE_TTL,
//     24h): por quanto tempo uma resposta do cache é "fresca" e por quanto
//     tempo ainda pode ser servida "velha" quando o catalog cai (ADR-0016).
//   - RateLimit* (RATE_LIMIT_RPS 10, RATE_LIMIT_BURST 30, RATE_LIMIT_MAX_IPS
//     10000): pedidos por segundo por IP, rajada tolerada e quantos IPs
//     acompanhar na memória (ADR-0015).
//   - TrustedProxies (TRUSTED_PROXIES) e ClientIPHeader (CLIENT_IP_HEADER,
//     "X-Forwarded-For"): de quais proxies aceitar o cabeçalho com o IP real.
//     Vazio (padrão) = nenhum proxy confiável, e o cabeçalho é ignorado.
//   - ShutdownDrainDelay (SHUTDOWN_DRAIN_DELAY, 3s) e ShutdownTimeout
//     (SHUTDOWN_TIMEOUT, 10s): as esperas do desligamento gracioso.
//   - LogLevel (LOG_LEVEL, info): nível mínimo dos logs.
//   - OTel* (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME "passarim-bff"):
//     para onde mandar os traces; sem endpoint, os traces ficam desligados.
//
// Vários campos na mesma linha ("A, B string") é só uma forma curta do Go de
// declarar campos do mesmo tipo.
type Config struct {
	HTTPAddr, MetricsAddr string

	CatalogAddr                             string
	CatalogTLSCAFile, CatalogTLSCertFile    string
	CatalogTLSKeyFile, CatalogTLSServerName string
	RedisURL                                string
	RedisTimeout                            time.Duration
	MediaBaseURL                            string
	RequestBudget, CatalogAttemptTimeout    time.Duration
	CatalogMaxRetries                       int
	CacheFreshTTL, CacheStaleTTL            time.Duration
	RateLimitRPS                            float64
	RateLimitBurst, RateLimitMaxIPs         int
	TrustedProxies                          []netip.Prefix
	ClientIPHeader                          string
	ShutdownDrainDelay, ShutdownTimeout     time.Duration
	LogLevel                                slog.Level
	OTelEndpoint, OTelServiceName           string
}

// writeTimeout é o WriteTimeout fixo do servidor HTTP (NewHTTPServer, em
// internal/adapter/server); o orçamento + 1 s de margem precisa caber nele.
// Se o orçamento fosse maior, o servidor cortaria a conexão antes de o BFF
// conseguir responder, e o cliente veria um erro de rede em vez do dado
// velho do cache ou de um 503 em problem+json.
const writeTimeout = 5 * time.Second

// Load recebe getenv (e não chama os.Getenv) para os testes passarem um ambiente falso.
//
// Load lê todas as variáveis, aplica os padrões e valida. No primeiro
// problema, devolve um erro que cita o nome da variável. O valor nunca entra
// na mensagem, para não vazar senha em log.
func Load(getenv func(string) string) (Config, error) {
	// Primeiro, os textos simples: "or" troca o vazio pelo padrão.
	c := Config{
		HTTPAddr:             or(getenv("HTTP_ADDR"), ":8080"),
		MetricsAddr:          or(getenv("METRICS_ADDR"), ":9091"),
		CatalogAddr:          getenv("CATALOG_ADDR"),
		CatalogTLSCAFile:     getenv("CATALOG_TLS_CA_FILE"),
		CatalogTLSCertFile:   getenv("CATALOG_TLS_CERT_FILE"),
		CatalogTLSKeyFile:    getenv("CATALOG_TLS_KEY_FILE"),
		CatalogTLSServerName: getenv("CATALOG_TLS_SERVER_NAME"),
		RedisURL:             getenv("REDIS_URL"),
		MediaBaseURL:         getenv("MEDIA_BASE_URL"),
		ClientIPHeader:       or(getenv("CLIENT_IP_HEADER"), "X-Forwarded-For"),
		OTelEndpoint:         getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		OTelServiceName:      or(getenv("OTEL_SERVICE_NAME"), "passarim-bff"),
	}
	// Slice (e não map): a ordem de conferência é fixa, então o erro é reproduzível.
	//
	// "struct{ name, value string }" é uma struct anônima, criada só para este
	// laço. Em Go, a ordem de iteração de um map é sorteada a cada execução;
	// com um map, o erro poderia citar ora uma variável, ora outra.
	for _, req := range []struct{ name, value string }{
		{"CATALOG_ADDR", c.CatalogAddr}, {"REDIS_URL", c.RedisURL}, {"MEDIA_BASE_URL", c.MediaBaseURL},
	} {
		if req.value == "" {
			return c, errors.New(req.name + " é obrigatória")
		}
	}
	// Depois, os valores que precisam ser convertidos. O padrão "if x, err =
	// f(); err != nil" atribui e confere o erro na mesma linha.
	var err error
	if c.RedisTimeout, err = dur(getenv, "REDIS_TIMEOUT", 100*time.Millisecond); err != nil {
		return c, err
	}
	if c.RequestBudget, err = dur(getenv, "REQUEST_BUDGET", 3*time.Second); err != nil {
		return c, err
	}
	if c.RequestBudget+time.Second > writeTimeout {
		return c, errors.New("REQUEST_BUDGET: precisa ser no máximo 4s (WriteTimeout é 5s)")
	}
	if c.CatalogAttemptTimeout, err = dur(getenv, "CATALOG_ATTEMPT_TIMEOUT", 2*time.Second); err != nil {
		return c, err
	}
	if c.CacheFreshTTL, err = dur(getenv, "CACHE_FRESH_TTL", 10*time.Minute); err != nil {
		return c, err
	}
	if c.CacheStaleTTL, err = dur(getenv, "CACHE_STALE_TTL", 24*time.Hour); err != nil {
		return c, err
	}
	if c.ShutdownDrainDelay, err = dur(getenv, "SHUTDOWN_DRAIN_DELAY", 3*time.Second); err != nil {
		return c, err
	}
	if c.ShutdownTimeout, err = dur(getenv, "SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return c, err
	}
	// Zero retries é válido (mínimo 0): quer dizer "uma tentativa só".
	if c.CatalogMaxRetries, err = integer(getenv, "CATALOG_MAX_RETRIES", 2, 0); err != nil {
		return c, err
	}
	if c.RateLimitBurst, err = integer(getenv, "RATE_LIMIT_BURST", 30, 1); err != nil {
		return c, err
	}
	if c.RateLimitMaxIPs, err = integer(getenv, "RATE_LIMIT_MAX_IPS", 10000, 1); err != nil {
		return c, err
	}
	// RATE_LIMIT_RPS aceita fração (por exemplo, 0.5 = um pedido a cada 2 s),
	// por isso é float, e não usa o helper integer.
	if v := getenv("RATE_LIMIT_RPS"); v == "" {
		c.RateLimitRPS = 10
	} else if c.RateLimitRPS, err = strconv.ParseFloat(v, 64); err != nil || c.RateLimitRPS <= 0 {
		return c, errors.New("RATE_LIMIT_RPS: esperava um número positivo")
	}
	if c.TrustedProxies, err = prefixes(getenv("TRUSTED_PROXIES")); err != nil {
		return c, err
	}
	// A base das URLs de mídia tem de ser absoluta (com esquema e host), senão
	// o app receberia caminhos que não consegue abrir.
	if u, perr := url.Parse(c.MediaBaseURL); perr != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return c, errors.New("MEDIA_BASE_URL: esperava uma URL absoluta http ou https")
	}
	if v := getenv("LOG_LEVEL"); v != "" {
		if err := c.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return c, errors.New("LOG_LEVEL: esperava debug, info, warn ou error")
		}
	}
	// TLS: cert e chave andam juntos, e só fazem sentido com a CA.
	//
	// "(a == "") != (b == "")" é verdadeiro quando só um dos dois foi
	// definido: é um "ou exclusivo" escrito com comparação de booleanos.
	if (c.CatalogTLSCertFile == "") != (c.CatalogTLSKeyFile == "") {
		return c, errors.New("CATALOG_TLS_CERT_FILE e CATALOG_TLS_KEY_FILE devem ser definidos juntos")
	}
	if c.CatalogTLSCertFile != "" && c.CatalogTLSCAFile == "" {
		return c, errors.New("CATALOG_TLS_CERT_FILE exige CATALOG_TLS_CA_FILE")
	}
	return c, nil
}

// or devolve v, ou def quando v está vazio.
func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// dur lê uma duração no formato do Go ("100ms", "3s", "10m", "24h"). Vazio
// vira o padrão; zero, negativo ou texto inválido viram erro com o nome da
// variável.
func dur(getenv func(string) string, key string, def time.Duration) (time.Duration, error) {
	v := getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s: esperava uma duração positiva, ex.: 3s", key)
	}
	return d, nil
}

// integer lê um inteiro que precisa ser pelo menos min. Vazio vira o padrão.
func integer(getenv func(string) string, key string, def, min int) (int, error) {
	v := getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min {
		return 0, fmt.Errorf("%s: esperava um inteiro >= %d", key, min)
	}
	return n, nil
}

// prefixes lê uma lista de redes em notação CIDR separadas por vírgula
// ("172.30.0.10/32, 10.0.0.0/8"). CIDR é o jeito de escrever uma faixa de IPs:
// o número depois da barra diz quantos bits iniciais são fixos (/32 = um IP
// só). Masked zera os bits livres, para "10.1.2.3/8" virar "10.0.0.0/8".
func prefixes(v string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, errors.New("TRUSTED_PROXIES: CIDR inválido (ex.: 172.30.0.10/32)")
		}
		out = append(out, p.Masked())
	}
	return out, nil
}
