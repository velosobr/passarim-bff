package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/velosobr/passarim-bff/internal/config"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func required() map[string]string {
	return map[string]string{"CATALOG_ADDR": "catalog-api:50051", "REDIS_URL": "redis://redis:6379", "MEDIA_BASE_URL": "http://localhost:8888/buckets/passarim-media"}
}

func TestLoad_Defaults(t *testing.T) {
	c, err := config.Load(env(required()))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" || c.MetricsAddr != ":9091" || c.RequestBudget != 3*time.Second || c.CatalogAttemptTimeout != 2*time.Second ||
		c.CatalogMaxRetries != 2 || c.CacheFreshTTL != 10*time.Minute || c.CacheStaleTTL != 24*time.Hour || c.RateLimitRPS != 10 ||
		c.RateLimitBurst != 30 || c.RateLimitMaxIPs != 10000 || c.ClientIPHeader != "X-Forwarded-For" || c.RedisTimeout != 100*time.Millisecond ||
		c.ShutdownDrainDelay != 3*time.Second || c.ShutdownTimeout != 10*time.Second || c.OTelServiceName != "passarim-bff" || c.OTelEndpoint != "" {
		t.Fatalf("padrões errados: %+v", c)
	}
}

func TestLoad_MissingRequiredNamesTheVariable(t *testing.T) {
	for _, name := range []string{"CATALOG_ADDR", "REDIS_URL", "MEDIA_BASE_URL"} {
		m := required()
		delete(m, name)
		if _, err := config.Load(env(m)); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("sem %s: o erro deveria citar o nome, veio %v", name, err)
		}
	}
}

func TestLoad_InvalidValues(t *testing.T) {
	cases := map[string]string{
		"REQUEST_BUDGET": "5s", "CATALOG_ATTEMPT_TIMEOUT": "abc", "CATALOG_MAX_RETRIES": "-1", "CACHE_FRESH_TTL": "0s",
		"RATE_LIMIT_RPS": "0", "RATE_LIMIT_BURST": "x", "TRUSTED_PROXIES": "nao-e-cidr", "MEDIA_BASE_URL": "ftp://x/y", "LOG_LEVEL": "gritando",
	}
	for name, bad := range cases {
		m := required()
		m[name] = bad
		if _, err := config.Load(env(m)); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s=%q deveria falhar citando a variável: %v", name, bad, err)
		}
	}
}

func TestLoad_RetriesAcceptsZero(t *testing.T) {
	m := required()
	m["CATALOG_MAX_RETRIES"] = "0"
	if c, err := config.Load(env(m)); err != nil || c.CatalogMaxRetries != 0 {
		t.Fatalf("0 retries é válido: %+v %v", c, err)
	}
}

func TestLoad_TrustedProxiesAndTLSRules(t *testing.T) {
	m := required()
	m["TRUSTED_PROXIES"] = "172.30.0.10/32, 10.0.0.0/8"
	c, err := config.Load(env(m))
	if err != nil || len(c.TrustedProxies) != 2 {
		t.Fatalf("CIDRs: %+v %v", c.TrustedProxies, err)
	}
	m = required()
	m["CATALOG_TLS_CERT_FILE"] = "/c.pem"
	if _, err := config.Load(env(m)); err == nil || !strings.Contains(err.Error(), "CATALOG_TLS") {
		t.Errorf("certificado sem chave e sem CA deveria falhar: %v", err)
	}
	m = required()
	m["CATALOG_TLS_CA_FILE"] = "/ca.pem"
	if _, err := config.Load(env(m)); err != nil {
		t.Errorf("só a CA é válido (TLS simples): %v", err)
	}
}

// Segredos: REDIS_URL pode ter senha; o valor nunca aparece em erros.
func TestLoad_ErrorNeverContainsValues(t *testing.T) {
	m := required()
	m["REDIS_URL"] = "redis://:super-secreto@redis:6379"
	m["REQUEST_BUDGET"] = "xx"
	_, err := config.Load(env(m))
	if err == nil || strings.Contains(err.Error(), "super-secreto") {
		t.Fatalf("o erro não pode conter o valor: %v", err)
	}
}
