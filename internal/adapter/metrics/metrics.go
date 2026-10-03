// Package metrics define o CONTRATO de métricas que os decorators usam e uma
// implementação que não faz nada (para testes). A implementação Prometheus
// chega na Task 8: assim os decorators (Task 6) não dependem do Prometheus.
package metrics

import "time"

type Metrics interface {
	CacheResult(result string) // hit | miss | stale | bypass | error
	BreakerState(state int)    // 0 fechado, 1 half-open, 2 aberto
	CatalogRetry(method string)
	CatalogAttempt(method, code string, d time.Duration)
}

type noop struct{}

func (noop) CacheResult(string)                           {}
func (noop) BreakerState(int)                             {}
func (noop) CatalogRetry(string)                          {}
func (noop) CatalogAttempt(string, string, time.Duration) {}

func Noop() Metrics { return noop{} }
