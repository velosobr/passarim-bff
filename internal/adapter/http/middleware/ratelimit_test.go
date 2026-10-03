package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/velosobr/passarim-bff/internal/adapter/http/middleware"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newLimiter(t *testing.T, cfg middleware.RateLimitConfig, clk *clock) (*middleware.RateLimiter, http.Handler) {
	t.Helper()
	ip := func(r *http.Request) string { return r.Header.Get("X-Test-IP") }
	rl := middleware.NewRateLimiter(cfg, ip, clk.Now)
	h := middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }), middleware.RequestID(), rl.Middleware())
	return rl, h
}

func hit(h http.Handler, ip, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("X-Test-IP", ip)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestRateLimit_BurstThen429WithRetryAfter(t *testing.T) {
	clk := &clock{now: time.Unix(1_000_000, 0)}
	limited := 0
	_, h := newLimiter(t, middleware.RateLimitConfig{RPS: 10, Burst: 3, MaxIPs: 100, IdleAfter: 3 * time.Minute, OnLimited: func() { limited++ }}, clk)
	for i := 0; i < 3; i++ {
		if rec := hit(h, "1.1.1.1", "/v1/species"); rec.Code != 200 {
			t.Fatalf("a rajada de 3 deveria passar (req %d): %d", i, rec.Code)
		}
	}
	rec := hit(h, "1.1.1.1", "/v1/species")
	if rec.Code != 429 || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("4ª deveria ser 429 problem+json: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if ra, _ := strconv.Atoi(rec.Header().Get("Retry-After")); ra < 1 {
		t.Fatalf("Retry-After mínimo 1s: %q", rec.Header().Get("Retry-After"))
	}
	if limited != 1 {
		t.Fatalf("OnLimited deveria ser chamado 1 vez, veio %d", limited)
	}
	clk.Advance(200 * time.Millisecond) // 10 req/s → 2 fichas
	if rec := hit(h, "1.1.1.1", "/v1/species"); rec.Code != 200 {
		t.Fatalf("depois de 200ms o balde enche de novo: %d", rec.Code)
	}
}

func TestRateLimit_PerIPAndExemptHealth(t *testing.T) {
	clk := &clock{now: time.Unix(1_000_000, 0)}
	_, h := newLimiter(t, middleware.RateLimitConfig{RPS: 1, Burst: 1, MaxIPs: 100, IdleAfter: time.Minute}, clk)
	_ = hit(h, "1.1.1.1", "/v1/species")
	if hit(h, "1.1.1.1", "/v1/species").Code != 429 {
		t.Fatal("o mesmo IP deveria estar limitado")
	}
	if hit(h, "2.2.2.2", "/v1/species").Code != 200 {
		t.Fatal("outro IP tem o seu próprio balde")
	}
	for i := 0; i < 5; i++ {
		if hit(h, "1.1.1.1", "/healthz").Code != 200 || hit(h, "1.1.1.1", "/readyz").Code != 200 {
			t.Fatal("/healthz e /readyz ficam fora do limite")
		}
	}
}

func TestRateLimit_RetryAfterFollowsRate(t *testing.T) {
	clk := &clock{now: time.Unix(1_000_000, 0)}
	_, h := newLimiter(t, middleware.RateLimitConfig{RPS: 0.25, Burst: 1, MaxIPs: 10, IdleAfter: time.Minute}, clk)
	_ = hit(h, "1.1.1.1", "/v1/x")
	if ra := hit(h, "1.1.1.1", "/v1/x").Header().Get("Retry-After"); ra != "4" {
		t.Fatalf("1/0.25 = 4s, veio %q", ra)
	}
}

// O teto de IPs despeja o MAIS OCIOSO; o limiter nunca passa a recusar todo mundo.
func TestRateLimit_EvictsLeastRecentlyUsedAtCap(t *testing.T) {
	clk := &clock{now: time.Unix(1_000_000, 0)}
	rl, h := newLimiter(t, middleware.RateLimitConfig{RPS: 1, Burst: 1, MaxIPs: 2, IdleAfter: time.Hour}, clk)
	_ = hit(h, "a", "/v1/x") // a esgota o balde
	_ = hit(h, "b", "/v1/x")
	_ = hit(h, "a", "/v1/x") // a fica "recente"; b é o mais ocioso
	_ = hit(h, "c", "/v1/x") // estoura o teto: despeja b
	if rl.Tracked() != 2 {
		t.Fatalf("o teto é 2, rastreando %d", rl.Tracked())
	}
	if hit(h, "a", "/v1/x").Code != 429 {
		t.Fatal("a continua rastreado e limitado")
	}
	if hit(h, "b", "/v1/x").Code != 200 {
		t.Fatal("b foi despejado: volta com balde cheio (nunca recusamos um IP novo)")
	}
	if hit(h, "d", "/v1/x").Code != 200 {
		t.Fatal("um IP novo sempre é atendido, mesmo no teto")
	}
}

func TestRateLimit_CleanupRemovesIdleIPs(t *testing.T) {
	clk := &clock{now: time.Unix(1_000_000, 0)}
	rl, h := newLimiter(t, middleware.RateLimitConfig{RPS: 1, Burst: 1, MaxIPs: 100, IdleAfter: 3 * time.Minute}, clk)
	_ = hit(h, "a", "/v1/x")
	clk.Advance(2 * time.Minute)
	_ = hit(h, "b", "/v1/x")
	clk.Advance(2 * time.Minute) // a está ocioso há 4 min; b há 2 min
	rl.Cleanup()
	if rl.Tracked() != 1 {
		t.Fatalf("só b deveria restar: %d", rl.Tracked())
	}
}
