// Package resilience tem os decorators de retry/timeout e de circuit breaker
// sobre usecase.CatalogReader. Cada um implementa a MESMA interface do que
// embrulha, então a ordem é montada no main (cache → breaker → retry → gRPC).
//
// Este arquivo é o retry: quando uma chamada ao catalog falha por um motivo
// passageiro (catalog reiniciando, rede lenta), tenta de novo algumas vezes,
// com uma pausa crescente entre as tentativas. A pausa é o "backoff
// exponencial" (o teto dobra a cada retry) com "jitter" (o valor real é
// sorteado entre zero e o teto). Sem o sorteio, todas as réplicas que
// falharam juntas tentariam de novo juntas, batendo no catalog ao mesmo tempo.
package resilience

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/velosobr/passarim-bff/internal/adapter/metrics"
	"github.com/velosobr/passarim-bff/internal/domain"
	"github.com/velosobr/passarim-bff/internal/usecase"
)

// Os dois tempos fixos do retry. O operador "<<" desloca bits para a
// esquerda: x << n é o mesmo que x × 2ⁿ.
const (
	BaseBackoff      = 50 * time.Millisecond  // antes do retry n espera [0, BaseBackoff × 2ⁿ]
	MinAttemptBudget = 200 * time.Millisecond // sem pelo menos isto de tempo, não tenta de novo
)

// RetryConfig são as regras do retry. Now, Sleep e Jitter são funções para
// os testes trocarem o relógio, a espera e o sorteio por versões
// previsíveis; com nil, NewRetry usa as reais.
type RetryConfig struct {
	MaxRetries     int           // retries além da 1ª tentativa
	AttemptTimeout time.Duration // limite de UMA tentativa
	Now            func() time.Time
	Sleep          func(ctx context.Context, d time.Duration) error // injetável nos testes
	Jitter         func(max time.Duration) time.Duration            // sorteia em [0, max]
}

// retry é o decorator: embrulha o próximo da cadeia (next) e registra as
// métricas de cada tentativa.
type retry struct {
	next usecase.CatalogReader
	cfg  RetryConfig
	m    metrics.Metrics
}

// NewRetry repete só falhas passageiras (Kind.Retryable) e só leituras (todas as nossas são).
//
// Repetir só leituras importa: repetir uma escrita poderia gravar a mesma
// coisa duas vezes, mas ler de novo não muda nada no catalog.
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

// sleepCtx espera d, mas acorda antes se o contexto acabar: um time.Sleep
// comum seguiria dormindo mesmo com o cliente já tendo desistido. O timer é
// parado no fim para não ficar ocupando memória.
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
// Mede quanto a tentativa levou e registra o resultado na métrica, por método
// e por Kind.
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

// do é o laço do retry. Para quando: a tentativa deu certo; o erro não é
// passageiro; acabaram os retries; ou não sobra tempo, depois da pausa, para
// uma tentativa com pelo menos MinAttemptBudget. Nesse último caso devolve o
// erro da última tentativa, que é mais útil que um "acabou o tempo".
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

// Os três métodos fazem do retry um CatalogReader: cada um só dá nome à
// operação (usado nas métricas) e chama do.
func (r *retry) ListSpecies(ctx context.Context, q domain.ListQuery) (domain.SpeciesPage, error) {
	return do(ctx, r, "ListSpecies", func(c context.Context) (domain.SpeciesPage, error) { return r.next.ListSpecies(c, q) })
}

func (r *retry) GetSpecies(ctx context.Context, id string) (domain.Species, error) {
	return do(ctx, r, "GetSpecies", func(c context.Context) (domain.Species, error) { return r.next.GetSpecies(c, id) })
}

func (r *retry) ListFilters(ctx context.Context) (domain.Filters, error) {
	return do(ctx, r, "ListFilters", func(c context.Context) (domain.Filters, error) { return r.next.ListFilters(c) })
}
