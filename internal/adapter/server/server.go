// Package server sobe os dois servidores HTTP (API e métricas) e cuida do
// shutdown em ordem: readyz→503, espera a drenagem, Shutdown, hooks (spec §6).
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Readiness diz se o BFF pode receber tráfego. Vira "não pronto" ao receber SIGTERM.
type Readiness struct{ shuttingDown atomic.Bool }

func (r *Readiness) SetShuttingDown() { r.shuttingDown.Store(true) }
func (r *Readiness) Ready() bool      { return !r.shuttingDown.Load() }

type Options struct {
	Listener, MetricsListener   net.Listener
	Handler, MetricsHandler     http.Handler
	DrainDelay, ShutdownTimeout time.Duration
	Ready                       *Readiness
	Log                         *slog.Logger
	OnShutdown                  []func(context.Context) // flush de traces, fechar gRPC e Redis...
}

// NewHTTPServer aplica os timeouts da spec §7.2 (OWASP API4). WriteTimeout 5 s = orçamento da requisição (3 s) + margem.
func NewHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler: h, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8 << 10,
	}
}

// Run serve até o contexto ser cancelado (SIGTERM/SIGINT) ou um servidor falhar.
func Run(ctx context.Context, o Options) error {
	api, metrics := NewHTTPServer(o.Handler), NewHTTPServer(o.MetricsHandler)
	errCh := make(chan error, 2)
	serve := func(s *http.Server, l net.Listener, name string) {
		if err := s.Serve(l); !errors.Is(err, http.ErrServerClosed) {
			errCh <- errors.Join(errors.New("servidor "+name), err)
		}
	}
	go serve(api, o.Listener, "api")
	go serve(metrics, o.MetricsListener, "metrics")

	var runErr error
	select {
	case <-ctx.Done():
		o.Log.Info("sinal recebido, encerrando com calma")
	case runErr = <-errCh:
		o.Log.Error("servidor falhou, encerrando", "error", runErr)
	}

	// 1. /readyz passa a 503 (o Traefik tira esta réplica do balanceamento).
	o.Ready.SetShuttingDown()
	// 2. Espera mais que um intervalo de health check do Traefik. O servidor continua atendendo.
	time.Sleep(o.DrainDelay)
	// 3. Para de aceitar conexões e termina as requisições em andamento.
	sctx, cancel := context.WithTimeout(context.Background(), o.ShutdownTimeout)
	defer cancel()
	if err := api.Shutdown(sctx); err != nil {
		runErr = errors.Join(runErr, err)
	}
	if err := metrics.Shutdown(sctx); err != nil {
		runErr = errors.Join(runErr, err)
	}
	// 4. Hooks: flush de traces (até 2 s), fecha a conexão gRPC e o cliente Redis.
	hctx, hcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer hcancel()
	for _, f := range o.OnShutdown {
		f(hctx)
	}
	return runErr
}
