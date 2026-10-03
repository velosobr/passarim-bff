package metrics_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/velosobr/passarim-bff/internal/adapter/http/middleware"
	"github.com/velosobr/passarim-bff/internal/adapter/metrics"
)

var (
	_ metrics.Metrics     = (*metrics.Prom)(nil)
	_ middleware.Observer = (*metrics.Prom)(nil)
)

func scrape(t *testing.T, p *metrics.Prom) string {
	t.Helper()
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	b, _ := io.ReadAll(rec.Body)
	return string(b)
}

func TestProm_ChainMetrics(t *testing.T) {
	p := metrics.NewProm()
	p.CacheResult("hit")
	p.CacheResult("hit")
	p.CacheResult("stale")
	p.CacheResult("bypass")
	p.BreakerState(2)
	p.CatalogRetry("GetSpecies")
	p.CatalogAttempt("GetSpecies", "Unavailable", 120*time.Millisecond)
	out := scrape(t, p)
	for _, want := range []string{
		`bff_cache_requests_total{result="hit"} 2`,
		`bff_cache_requests_total{result="stale"} 1`,
		`bff_cache_requests_total{result="bypass"} 1`,
		`bff_breaker_state 2`,
		`bff_catalog_retries_total{method="GetSpecies"} 1`,
		`bff_catalog_request_duration_seconds_count{code="Unavailable",method="GetSpecies"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou a linha %q", want)
		}
	}
}

func TestProm_HTTPMetricsUseRoutePatternAndStringStatus(t *testing.T) {
	p := metrics.NewProm()
	p.ObserveHTTP("GET /v1/species/{id}", "GET", 404, 12*time.Millisecond)
	p.ObserveHTTP("unmatched", "GET", 404, time.Millisecond)
	p.RateLimited()
	out := scrape(t, p)
	for _, want := range []string{
		`bff_http_requests_total{method="GET",route="GET /v1/species/{id}",status="404"} 1`,
		`bff_http_requests_total{method="GET",route="unmatched",status="404"} 1`,
		`bff_http_request_duration_seconds_count{method="GET",route="GET /v1/species/{id}"} 1`,
		`bff_rate_limited_total 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("faltou a linha %q", want)
		}
	}
}

func TestProm_GaugeFuncsReadLiveValues(t *testing.T) {
	p := metrics.NewProm()
	if !strings.Contains(scrape(t, p), "bff_rate_limiter_tracked_ips 0") {
		t.Fatal("sem função registrada, o gauge vale 0")
	}
	tracked, state := 7.0, 2.0
	p.SetTrackedIPs(func() float64 { return tracked })
	p.SetConnState(func() float64 { return state })
	out := scrape(t, p)
	if !strings.Contains(out, "bff_rate_limiter_tracked_ips 7") || !strings.Contains(out, "bff_catalog_connection_state 2") {
		t.Fatalf("gauges: %s", out)
	}
	tracked = 9
	if !strings.Contains(scrape(t, p), "bff_rate_limiter_tracked_ips 9") {
		t.Fatal("o gauge deveria refletir o valor atual a cada coleta")
	}
}

func TestProm_ExposesGoRuntimeMetrics(t *testing.T) {
	if !strings.Contains(scrape(t, metrics.NewProm()), "go_goroutines") {
		t.Fatal("o coletor padrão de runtime Go deveria estar registrado")
	}
}
