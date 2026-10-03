package server_test

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/velosobr/passarim-bff/internal/adapter/server"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func get(url string) (int, error) {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode, nil
}

// A sequência da spec §6: readyz→503, espera o drenar, Shutdown (termina o que está em andamento), hooks.
func TestRun_GracefulShutdownSequence(t *testing.T) {
	ready := &server.Readiness{}
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !ready.Ready() {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	})
	started := make(chan struct{})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond) // uma requisição em andamento quando o shutdown começa
		w.WriteHeader(200)
	})
	api, metr := listen(t), listen(t)
	var hookRan atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx, server.Options{
			Listener: api, MetricsListener: metr, Handler: mux, MetricsHandler: http.NewServeMux(),
			DrainDelay: 400 * time.Millisecond, ShutdownTimeout: 3 * time.Second, Ready: ready, Log: slog.New(slog.DiscardHandler),
			OnShutdown: []func(context.Context){func(context.Context) { hookRan.Store(true) }},
		})
	}()
	base := "http://" + api.Addr().String()
	if code, err := get(base + "/readyz"); err != nil || code != 200 {
		t.Fatalf("pronto antes do shutdown: %d %v", code, err)
	}
	slowDone := make(chan int, 1)
	go func() { code, _ := get(base + "/slow"); slowDone <- code }()
	<-started

	cancel() // SIGTERM
	time.Sleep(100 * time.Millisecond)
	if code, err := get(base + "/readyz"); err != nil || code != 503 {
		t.Fatalf("durante a espera de drenagem o readyz é 503 e o servidor ainda atende: %d %v", code, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code := <-slowDone; code != 200 {
		t.Fatalf("a requisição em andamento deveria terminar normalmente: %d", code)
	}
	if !hookRan.Load() {
		t.Fatal("os hooks de OnShutdown (flush de traces, fechar conexões) deveriam rodar")
	}
	if _, err := get(base + "/readyz"); err == nil {
		t.Fatal("depois do shutdown o servidor não atende mais")
	}
}

func TestRun_ServesMetricsOnSeparateListener(t *testing.T) {
	api, metr := listen(t), listen(t)
	api2 := http.NewServeMux()
	m := http.NewServeMux()
	m.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "ok") })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx, server.Options{Listener: api, MetricsListener: metr, Handler: api2, MetricsHandler: m,
			DrainDelay: 10 * time.Millisecond, ShutdownTimeout: time.Second, Ready: &server.Readiness{}, Log: slog.New(slog.DiscardHandler)})
	}()
	if code, err := get("http://" + metr.Addr().String() + "/metrics"); err != nil || code != 200 {
		t.Fatalf("metrics: %d %v", code, err)
	}
	if code, _ := get("http://" + api.Addr().String() + "/metrics"); code != 404 {
		t.Fatalf("o /metrics NÃO pode estar na porta da API (é porta separada): %d", code)
	}
	cancel()
	<-done
}

func TestRun_ReturnsErrorWhenAServerFails(t *testing.T) {
	api := listen(t)
	metr := listen(t)
	_ = metr.Close() // o servidor de métricas não consegue servir
	err := server.Run(context.Background(), server.Options{Listener: api, MetricsListener: metr, Handler: http.NewServeMux(), MetricsHandler: http.NewServeMux(),
		DrainDelay: time.Millisecond, ShutdownTimeout: time.Second, Ready: &server.Readiness{}, Log: slog.New(slog.DiscardHandler)})
	if err == nil {
		t.Fatal("falha ao servir deveria encerrar o Run com erro")
	}
}

func TestServerTimeoutsAreSet(t *testing.T) {
	o := server.Options{}
	hs := server.NewHTTPServer(o.Handler)
	if hs.ReadHeaderTimeout != 2*time.Second || hs.ReadTimeout != 5*time.Second || hs.WriteTimeout != 5*time.Second || hs.IdleTimeout != 60*time.Second || hs.MaxHeaderBytes != 8<<10 {
		t.Fatalf("timeouts da spec §7.2: %+v", hs)
	}
}

func TestHealthcheck(t *testing.T) {
	ok := listen(t)
	go func() {
		_ = (&http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" {
				w.WriteHeader(404)
				return
			}
			w.WriteHeader(200)
		})}).Serve(ok)
	}()
	_, port, _ := net.SplitHostPort(ok.Addr().String())
	if code := server.Healthcheck(":" + port); code != 0 {
		t.Fatalf("saudável deveria sair 0: %d", code)
	}
	bad := listen(t)
	go func() {
		_ = (&http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })}).Serve(bad)
	}()
	_, badPort, _ := net.SplitHostPort(bad.Addr().String())
	if code := server.Healthcheck(":" + badPort); code != 1 {
		t.Fatalf("500 deveria sair 1: %d", code)
	}
	free := listen(t)
	_, freePort, _ := net.SplitHostPort(free.Addr().String())
	_ = free.Close()
	if code := server.Healthcheck(":" + freePort); code != 1 {
		t.Fatalf("sem servidor deveria sair 1: %d", code)
	}
	if code := server.Healthcheck(""); code != 1 && code != 0 { // vazio usa :8080; só não pode dar panic
		t.Fatalf("código inesperado %d", code)
	}
}
