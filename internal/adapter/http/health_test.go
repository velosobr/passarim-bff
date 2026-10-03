package httpadapter_test

import (
	"errors"
	"log/slog"
	"net/http"
	"testing"

	httpadapter "github.com/velosobr/passarim-bff/internal/adapter/http"
	"github.com/velosobr/passarim-bff/internal/adapter/media"
	"github.com/velosobr/passarim-bff/internal/domain"
)

func healthHandler(t *testing.T, get *fakeGet, ready func() bool) http.Handler {
	t.Helper()
	m, _ := media.New(base, slog.New(slog.DiscardHandler))
	return (&httpadapter.Handler{List: &fakeList{}, Get: get, Filters: &fakeFilters{}, Media: m, Log: slog.New(slog.DiscardHandler), Ready: ready}).Routes()
}

func TestHealthz_AlwaysOK(t *testing.T) {
	rec := do(healthHandler(t, &fakeGet{}, func() bool { return false }), "GET", "/healthz")
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("healthz: %d %v", rec.Code, rec.Header())
	}
}

// Review Focus #2: o readyz NÃO consulta o catalog nem o Redis; se consultasse, com o catalog
// fora do ar as duas réplicas sairiam do balanceador e o stale nunca seria servido.
func TestReadyz_IgnoresCatalogAndRedis(t *testing.T) {
	broken := &fakeGet{err: domain.NewError(domain.KindUnavailable, errors.New("catalog fora do ar"))}
	rec := do(healthHandler(t, broken, nil), "GET", "/readyz")
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("readyz com catalog quebrado deveria ser 200: %d", rec.Code)
	}
}

func TestReadyz_503WhileShuttingDown(t *testing.T) {
	shuttingDown := false
	h := healthHandler(t, &fakeGet{}, func() bool { return !shuttingDown })
	if do(h, "GET", "/readyz").Code != 200 {
		t.Fatal("pronto no início")
	}
	shuttingDown = true
	if rec := do(h, "GET", "/readyz"); rec.Code != 503 {
		t.Fatalf("em shutdown deveria ser 503: %d", rec.Code)
	}
	if do(h, "GET", "/healthz").Code != 200 {
		t.Fatal("o processo continua vivo durante o shutdown")
	}
}

func TestHealthRoutesAreRegisteredPatterns(t *testing.T) {
	m, _ := media.New(base, slog.New(slog.DiscardHandler))
	h := &httpadapter.Handler{List: &fakeList{}, Get: &fakeGet{}, Filters: &fakeFilters{}, Media: m, Log: slog.New(slog.DiscardHandler)}
	h.Routes()
	got := map[string]bool{}
	for _, p := range h.Patterns() {
		got[p] = true
	}
	if !got["GET /healthz"] || !got["GET /readyz"] {
		t.Fatalf("padrões: %v", h.Patterns())
	}
}
