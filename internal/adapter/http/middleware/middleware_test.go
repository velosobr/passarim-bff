package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/velosobr/passarim-bff/internal/adapter/http/middleware"
	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
)

var discard = slog.New(slog.DiscardHandler)

func TestRecovery_PanicBecomes500WithoutStack(t *testing.T) {
	h := middleware.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("segredo-do-panic") }),
		middleware.Recovery(discard), middleware.RequestID())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != 500 || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status/content-type: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	var p map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if p["code"] != "INTERNAL" || p["requestId"] == "" || strings.Contains(rec.Body.String(), "segredo-do-panic") || strings.Contains(rec.Body.String(), "goroutine") {
		t.Fatalf("corpo: %s", rec.Body.String())
	}
}

func TestRecovery_RepropagatesAbortHandler(t *testing.T) {
	h := middleware.Recovery(discard)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	defer func() {
		if r, _ := recover().(error); !errors.Is(r, http.ErrAbortHandler) {
			t.Fatalf("http.ErrAbortHandler deveria ser repropagado, veio %v", r)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
}

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestRequestID_AcceptsValidAndReplacesInvalid(t *testing.T) {
	var seen string
	h := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = reqmeta.RequestID(r.Context()) }))
	for _, valid := range []string{"abc-123_X.y", strings.Repeat("a", 64)} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Request-Id", valid)
		h.ServeHTTP(rec, req)
		if seen != valid || rec.Header().Get("X-Request-Id") != valid {
			t.Errorf("%q deveria ser aceito: ctx=%q header=%q", valid, seen, rec.Header().Get("X-Request-Id"))
		}
	}
	for _, bad := range []string{strings.Repeat("a", 65), "tem espaço", "ação", "a\nb", ""} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		if bad != "" {
			req.Header.Set("X-Request-Id", bad)
		}
		h.ServeHTTP(rec, req)
		if !hex32.MatchString(seen) || rec.Header().Get("X-Request-Id") != seen {
			t.Errorf("%q deveria ser trocado por 32 hex, veio ctx=%q", bad, seen)
		}
	}
}

type recObserver struct {
	route, method string
	status        int
	calls         int
}

func (o *recObserver) ObserveHTTP(route, method string, status int, _ time.Duration) {
	o.route, o.method, o.status = route, method, status
	o.calls++
}

func TestAccessLog_FieldsRouteAndObserver(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/species/{id}", func(w http.ResponseWriter, r *http.Request) {
		reqmeta.From(r.Context()).SetCache("hit")
		w.WriteHeader(200)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	obs := &recObserver{}
	ip := func(*http.Request) string { return "203.0.113.9" }
	h := middleware.Chain(mux, middleware.RequestID(), middleware.AccessLog(log, ip, obs))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/species/turdus-rufiventris?q=segredo", nil))
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("log não é JSON: %v\n%s", err, buf.String())
	}
	if line["route"] != "GET /v1/species/{id}" || line["status"].(float64) != 200 || line["cache"] != "hit" || line["client_ip"] != "203.0.113.9" ||
		line["method"] != "GET" || line["request_id"] == "" || line["duration_ms"] == nil {
		t.Fatalf("campos do log: %v", line)
	}
	if strings.Contains(buf.String(), "turdus-rufiventris") || strings.Contains(buf.String(), "segredo") {
		t.Fatalf("o log guarda o padrão da rota, nunca a URL com parâmetros: %s", buf.String())
	}
	if obs.route != "GET /v1/species/{id}" || obs.status != 200 || obs.method != "GET" || obs.calls != 1 {
		t.Fatalf("observer: %+v", obs)
	}

	buf.Reset()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/nada/aqui", nil))
	if obs.route != "unmatched" {
		t.Fatalf("rota sem padrão deveria ser 'unmatched', veio %q", obs.route)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	middleware.SecurityHeaders()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	for k, v := range map[string]string{"X-Content-Type-Options": "nosniff", "Content-Security-Policy": "default-src 'none'", "Referrer-Policy": "no-referrer"} {
		if rec.Header().Get(k) != v {
			t.Errorf("%s = %q, esperado %q", k, rec.Header().Get(k), v)
		}
	}
}

func TestLimits_OnlyGetAndHead(t *testing.T) {
	called := false
	h := middleware.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), middleware.RequestID(), middleware.Limits())
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/v1/species", strings.NewReader("corpo")))
		var p map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		if rec.Code != 405 || rec.Header().Get("Allow") != "GET, HEAD" || p["code"] != "METHOD_NOT_ALLOWED" || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: %d allow=%q corpo=%s", m, rec.Code, rec.Header().Get("Allow"), rec.Body.String())
		}
	}
	if called {
		t.Fatal("métodos recusados não podem chegar ao handler")
	}
	for _, m := range []string{"GET", "HEAD"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/v1/species", nil))
		if rec.Code == 405 {
			t.Errorf("%s deveria passar", m)
		}
	}
}

func TestChain_FirstIsOutermost(t *testing.T) {
	var order []string
	mk := func(name string) middleware.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { order = append(order, name); next.ServeHTTP(w, r) })
		}
	}
	middleware.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "handler") }), mk("a"), mk("b")).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if strings.Join(order, ",") != "a,b,handler" {
		t.Fatalf("ordem: %v", order)
	}
}
