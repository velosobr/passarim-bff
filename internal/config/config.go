// Package config lê a configuração das variáveis de ambiente (12-factor).
// Os erros citam o NOME da variável, nunca o valor: REDIS_URL pode ter senha.
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

// writeTimeout é o WriteTimeout fixo do servidor (Task 8); o orçamento + 1 s de margem precisa caber nele.
const writeTimeout = 5 * time.Second

// Load recebe getenv (e não chama os.Getenv) para os testes passarem um ambiente falso.
func Load(getenv func(string) string) (Config, error) {
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
	for name, v := range map[string]string{"CATALOG_ADDR": c.CatalogAddr, "REDIS_URL": c.RedisURL, "MEDIA_BASE_URL": c.MediaBaseURL} {
		if v == "" {
			return c, errors.New(name + " é obrigatória")
		}
	}
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
	if c.CatalogMaxRetries, err = integer(getenv, "CATALOG_MAX_RETRIES", 2, 0); err != nil {
		return c, err
	}
	if c.RateLimitBurst, err = integer(getenv, "RATE_LIMIT_BURST", 30, 1); err != nil {
		return c, err
	}
	if c.RateLimitMaxIPs, err = integer(getenv, "RATE_LIMIT_MAX_IPS", 10000, 1); err != nil {
		return c, err
	}
	if v := getenv("RATE_LIMIT_RPS"); v == "" {
		c.RateLimitRPS = 10
	} else if c.RateLimitRPS, err = strconv.ParseFloat(v, 64); err != nil || c.RateLimitRPS <= 0 {
		return c, errors.New("RATE_LIMIT_RPS: esperava um número positivo")
	}
	if c.TrustedProxies, err = prefixes(getenv("TRUSTED_PROXIES")); err != nil {
		return c, err
	}
	if u, perr := url.Parse(c.MediaBaseURL); perr != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return c, errors.New("MEDIA_BASE_URL: esperava uma URL absoluta http ou https")
	}
	if v := getenv("LOG_LEVEL"); v != "" {
		if err := c.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return c, errors.New("LOG_LEVEL: esperava debug, info, warn ou error")
		}
	}
	// TLS: cert e chave andam juntos, e só fazem sentido com a CA.
	if (c.CatalogTLSCertFile == "") != (c.CatalogTLSKeyFile == "") {
		return c, errors.New("CATALOG_TLS_CERT_FILE e CATALOG_TLS_KEY_FILE devem ser definidos juntos")
	}
	if c.CatalogTLSCertFile != "" && c.CatalogTLSCAFile == "" {
		return c, errors.New("CATALOG_TLS_CERT_FILE exige CATALOG_TLS_CA_FILE")
	}
	return c, nil
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

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
