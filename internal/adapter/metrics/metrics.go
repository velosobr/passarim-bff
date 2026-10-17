// Package metrics define o CONTRATO de métricas que os decorators usam e uma
// implementação que não faz nada (para testes). A implementação Prometheus
// fica em prom.go: assim os decorators dependem só desta interface, e não do
// Prometheus.
//
// Métricas são números que o serviço expõe para acompanhar a saúde dele ao
// longo do tempo: quantos acertos de cache, quantos retries, quanto tempo
// demora cada chamada. O Prometheus busca esses números de tempos em tempos
// em /metrics, e o Grafana os mostra em gráficos.
package metrics

import "time"

// Metrics são os eventos que a cadeia de resiliência registra.
type Metrics interface {
	CacheResult(result string) // hit | miss | stale | bypass | error
	BreakerState(state int)    // 0 fechado, 1 half-open, 2 aberto
	CatalogRetry(method string)
	CatalogAttempt(method, code string, d time.Duration)
}

// noop implementa Metrics sem fazer nada: serve para testes que não querem
// conferir métricas.
type noop struct{}

func (noop) CacheResult(string)                           {}
func (noop) BreakerState(int)                             {}
func (noop) CatalogRetry(string)                          {}
func (noop) CatalogAttempt(string, string, time.Duration) {}

// Noop devolve a implementação vazia.
func Noop() Metrics { return noop{} }
