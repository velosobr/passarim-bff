// Package resilience tem os decorators de retry/timeout e de circuit breaker
// sobre usecase.CatalogReader. Cada um implementa a MESMA interface do que
// embrulha, então a ordem é montada no main (cache → breaker → retry → gRPC).
package resilience

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/velosobr/passarim-bff/internal/adapter/metrics"
	"github.com/velosobr/passarim-bff/internal/domain"
	"github.com/velosobr/passarim-bff/internal/usecase"
)

const (
	BaseBackoff      = 50 * time.Millisecond  // antes do retry n espera [0, BaseBackoff × 2ⁿ]
	MinAttemptBudget = 200 * time.Millisecond // sem pelo menos isto de tempo, não tenta de novo
)

type RetryConfig struct {
	MaxRetries     int           // retries além da 1ª tentativa
	AttemptTimeout time.Duration // limite de UMA tentativa
	Now            func() time.Time
	Sleep          func(ctx context.Context, d time.Duration) error // injetável nos testes
	Jitter         func(max time.Duration) time.Duration            // sorteia em [0, max]
}

type retry struct {
	next usecase.CatalogReader
	cfg  RetryConfig
	m    metrics.Metrics
}

// NewRetry repete só falhas passageiras (Kind.Retryable) e só leituras (todas as nossas são).
func NewRetry(next usecase.CatalogReader, cfg RetryConfig, m metrics.Metrics) usecase.CatalogReader {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleepCtx
	}
	if cfg.Jitter == nil {
		cfg.Jitter = func(max time.Duration) time.Duration { return rand.N(max + 1) } //nolint:gosec // jitter não precisa ser criptográfico
	}
	return &retry{next: next, cfg: cfg, m: m}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// attempt roda UMA tentativa com timeout = min(AttemptTimeout, tempo restante da requisição).
func attempt[T any](ctx context.Context, r *retry, method string, fn func(context.Context) (T, error)) (T, error) {
	tctx, cancel := context.WithTimeout(ctx, r.cfg.AttemptTimeout) // WithTimeout já nunca passa do prazo do pai
	defer cancel()
	start := r.cfg.Now()
	v, err := fn(tctx)
	code := "ok"
	if err != nil {
		code = domain.KindOf(err).String()
	}
	r.m.CatalogAttempt(method, code, r.cfg.Now().Sub(start))
	return v, err
}

func do[T any](ctx context.Context, r *retry, method string, fn func(context.Context) (T, error)) (T, error) {
	for n := 0; ; n++ {
		v, err := attempt(ctx, r, method, fn)
		if err == nil || !domain.KindOf(err).Retryable() || n >= r.cfg.MaxRetries {
			return v, err
		}
		wait := r.cfg.Jitter(BaseBackoff << (n + 1)) // retry 1 → até 100ms; retry 2 → até 200ms
		if dl, ok := ctx.Deadline(); ok && dl.Sub(r.cfg.Now())-wait < MinAttemptBudget {
			return v, err // sem tempo para outra tentativa útil
		}
		if serr := r.cfg.Sleep(ctx, wait); serr != nil {
			var zero T
			return zero, domain.NewError(domain.KindOf(serr), serr)
		}
		r.m.CatalogRetry(method)
	}
}

func (r *retry) ListSpecies(ctx context.Context, q domain.ListQuery) (domain.SpeciesPage, error) {
	return do(ctx, r, "ListSpecies", func(c context.Context) (domain.SpeciesPage, error) { return r.next.ListSpecies(c, q) })
}

func (r *retry) GetSpecies(ctx context.Context, id string) (domain.Species, error) {
	return do(ctx, r, "GetSpecies", func(c context.Context) (domain.Species, error) { return r.next.GetSpecies(c, id) })
}

func (r *retry) ListFilters(ctx context.Context) (domain.Filters, error) {
	return do(ctx, r, "ListFilters", func(c context.Context) (domain.Filters, error) { return r.next.ListFilters(c) })
}
