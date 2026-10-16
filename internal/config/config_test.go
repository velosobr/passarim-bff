// Testes do Load: padrões, obrigatórias, valores inválidos e a regra de nunca
// pôr o valor de uma variável numa mensagem de erro.
//
// O pacote config_test (com o sufixo _test) é um pacote separado: ele só
// enxerga o que o config exporta, como qualquer outro usuário do pacote. Isso
// garante que testamos a API pública, e não detalhes internos.
package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/velosobr/passarim-bff/internal/config"
)

// env transforma um mapa num getenv falso: a chave ausente devolve "", como
// uma variável de ambiente não definida.
func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// required devolve um ambiente mínimo válido, só com as três obrigatórias.
// Cada teste parte dele e muda o que quer exercitar.
func required() map[string]string {
	return map[string]string{"CATALOG_ADDR": "catalog-api:50051", "REDIS_URL": "redis://redis:6379", "MEDIA_BASE_URL": "http://localhost:8888/buckets/passarim-media"}
}

// Só com as obrigatórias, todos os outros campos ganham os padrões
// documentados. Mudar um padrão sem querer quebra este teste.
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

// Tirando cada obrigatória, uma por vez, o erro cita o nome dela.
func TestLoad_MissingRequiredNamesTheVariable(t *testing.T) {
	for _, name := range []string{"CATALOG_ADDR", "REDIS_URL", "MEDIA_BASE_URL"} {
		m := required()
		delete(m, name)
		if _, err := config.Load(env(m)); err == nil || !strings.Contains(err.Error(), name) {
			// t.Errorf marca a falha mas deixa o laço seguir, para vermos
			// todas as variáveis com problema de uma vez.
			t.Errorf("sem %s: o erro deveria citar o nome, veio %v", name, err)
		}
	}
}

// Um valor inválido por variável: cada um precisa falhar citando a variável.
// REQUEST_BUDGET=5s falha pela regra do WriteTimeout (orçamento + 1 s ≤ 5 s).
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

// Zero retries é um valor válido ("tente uma vez só"), e não um erro.
func TestLoad_RetriesAcceptsZero(t *testing.T) {
	m := required()
	m["CATALOG_MAX_RETRIES"] = "0"
	if c, err := config.Load(env(m)); err != nil || c.CatalogMaxRetries != 0 {
		t.Fatalf("0 retries é válido: %+v %v", c, err)
	}
}

// Confere três coisas: a lista de proxies aceita espaços depois da vírgula;
// certificado sem chave (e sem CA) é recusado; só a CA é aceita (TLS simples,
// sem o BFF apresentar certificado).
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
//
// Aqui a REDIS_URL é válida e quem falha é outra variável: o teste garante
// que nenhuma mensagem de erro arrasta junto o resto da configuração.
func TestLoad_ErrorNeverContainsValues(t *testing.T) {
	m := required()
	m["REDIS_URL"] = "redis://:super-secreto@redis:6379"
	m["REQUEST_BUDGET"] = "xx"
	_, err := config.Load(env(m))
	if err == nil || strings.Contains(err.Error(), "super-secreto") {
		t.Fatalf("o erro não pode conter o valor: %v", err)
	}
}

// A ordem em que as obrigatórias são conferidas é fixa (CATALOG_ADDR, REDIS_URL, MEDIA_BASE_URL):
// o erro precisa ser reproduzível, não depender da ordem de iteração de um map.
//
// Repetimos 50 vezes porque, se o código usasse um map, a ordem sorteada
// acabaria mostrando outra variável em alguma das rodadas.
func TestLoad_MissingRequiredReportsInFixedOrder(t *testing.T) {
	for i := 0; i < 50; i++ {
		_, err := config.Load(env(map[string]string{}))
		if err == nil || !strings.Contains(err.Error(), "CATALOG_ADDR") {
			t.Fatalf("o primeiro erro deveria citar CATALOG_ADDR, veio %v", err)
		}
	}
}
