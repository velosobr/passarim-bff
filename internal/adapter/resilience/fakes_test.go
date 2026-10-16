// Peças falsas dos testes de resiliência: um catalog "roteirizado", que
// devolve erros numa ordem combinada, e um registro de métricas.
package resilience_test

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// scripted devolve, em ordem, os erros da lista (nil = sucesso); guarda o contexto de cada chamada.
type scripted struct {
	mu        sync.Mutex
	errs      []error
	calls     int
	deadlines []time.Time
}

// next conta a chamada, guarda o prazo do contexto e devolve o próximo erro
// do roteiro (depois do último, repete o último).
func (s *scripted) next(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dl, _ := ctx.Deadline()
	s.deadlines = append(s.deadlines, dl)
	i := s.calls
	s.calls++
	if i < len(s.errs) {
		return s.errs[i]
	}
	if len(s.errs) > 0 {
		return s.errs[len(s.errs)-1]
	}
	return nil
}

// Os três métodos só consomem o roteiro.
func (s *scripted) ListSpecies(ctx context.Context, _ domain.ListQuery) (domain.SpeciesPage, error) {
	return domain.SpeciesPage{}, s.next(ctx)
}
func (s *scripted) GetSpecies(ctx context.Context, id string) (domain.Species, error) {
	return domain.Species{ID: id}, s.next(ctx)
}
func (s *scripted) ListFilters(ctx context.Context) (domain.Filters, error) {
	return domain.Filters{}, s.next(ctx)
}

// kindErr cria um erro do domínio com o Kind pedido e sem causa.
func kindErr(k domain.Kind) error { return domain.NewError(k, nil) }

// recMetrics anota o que foi contado.
type recMetrics struct {
	mu       sync.Mutex
	retries  int
	attempts []string
	states   []int
	cache    []string
}

// Cada método anota a métrica recebida, com trava, porque o breaker pode
// avisar a mudança de estado de outra goroutine.
func (m *recMetrics) CacheResult(r string) { m.mu.Lock(); m.cache = append(m.cache, r); m.mu.Unlock() }
func (m *recMetrics) BreakerState(s int)   { m.mu.Lock(); m.states = append(m.states, s); m.mu.Unlock() }
func (m *recMetrics) CatalogRetry(string)  { m.mu.Lock(); m.retries++; m.mu.Unlock() }
func (m *recMetrics) CatalogAttempt(method, code string, _ time.Duration) {
	m.mu.Lock()
	m.attempts = append(m.attempts, method+":"+code)
	m.mu.Unlock()
}

// testLogger é um logger que descarta tudo.
func testLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
