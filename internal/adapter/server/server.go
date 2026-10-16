// Package server sobe os dois servidores HTTP (API e métricas) e cuida do
// shutdown em ordem: readyz→503, espera a drenagem, Shutdown, hooks (spec §6).
//
// "Desligamento gracioso" quer dizer não cortar ninguém no meio: quando o
// Docker manda parar, o BFF avisa o Traefik que não está mais pronto, espera
// o Traefik parar de mandar pedidos novos, termina os pedidos em andamento e
// só então fecha as conexões com o catalog e o Redis.
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

// Readiness diz se o BFF pode receber tráfego. Vira "não pronto" no início do
// desligamento (SIGTERM ou falha de um servidor).
// atomic.Bool é um booleano que várias goroutines podem ler e escrever ao
// mesmo tempo sem trava (o readyz lê enquanto o Run escreve).
type Readiness struct{ shuttingDown atomic.Bool }

// SetShuttingDown marca o início do desligamento; Ready diz se ainda está
// pronto.
func (r *Readiness) SetShuttingDown() { r.shuttingDown.Store(true) }
func (r *Readiness) Ready() bool      { return !r.shuttingDown.Load() }

// Options é tudo o que Run precisa: as duas portas já abertas, os dois
// handlers, as esperas do desligamento, a prontidão, o logger e as funções a
// chamar no fim.
type Options struct {
	Listener, MetricsListener   net.Listener
	Handler, MetricsHandler     http.Handler
	DrainDelay, ShutdownTimeout time.Duration
	Ready                       *Readiness
	Log                         *slog.Logger
	OnShutdown                  []func(context.Context) // flush de traces, fechar gRPC e Redis...
}

// NewHTTPServer aplica os timeouts da spec §7.2 (OWASP API4). WriteTimeout 5 s = orçamento da requisição (3 s) + margem.
//
// OWASP API4 é o risco de "consumo sem limite de recursos". Sem timeouts, um
// cliente lento de propósito (que manda o cabeçalho um byte por vez)
// prenderia uma conexão para sempre. ReadHeaderTimeout limita a leitura dos
// cabeçalhos, ReadTimeout a da requisição inteira, WriteTimeout a escrita da
// resposta, IdleTimeout o tempo de uma conexão parada entre requisições, e
// MaxHeaderBytes (8 << 10 = 8 KiB) o tamanho dos cabeçalhos.
func NewHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler: h, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 8 << 10,
	}
}

// Run serve até o contexto ser cancelado (SIGTERM/SIGINT) ou um servidor falhar.
//
// Cada servidor roda numa goroutine. Serve só retorna quando o servidor
// para: com ErrServerClosed num Shutdown normal, ou com outro erro numa
// falha, que vai para o canal errCh. O select espera o que vier primeiro: o
// sinal de parar ou a falha de um servidor. errors.Join junta vários erros
// num só, sem perder nenhum.
func Run(ctx context.Context, o Options) error {
	api, metrics := NewHTTPServer(o.Handler), NewHTTPServer(o.MetricsHandler)
	// Um canal (chan) é um cano por onde goroutines trocam valores. Este cabe
	// dois erros sem bloquear, um por servidor.
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
