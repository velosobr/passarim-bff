// Circuit breaker do catalog. A ideia vem dos disjuntores elétricos: quando
// o catalog começa a falhar muito, o breaker "abre" e passa a recusar as
// chamadas na hora, sem esperar o catalog. Isso protege o catalog (que ganha
// fôlego para se recuperar) e o BFF (que não fica preso esperando respostas
// que não vêm; o cache serve o dado velho no lugar).
//
// Os três estados:
//   - fechado: tudo normal, as chamadas passam e as falhas são contadas;
//   - aberto: as chamadas são recusadas na hora, por Timeout (30 s);
//   - meio-aberto: depois do Timeout, deixa passar algumas chamadas de teste
//     (MaxRequests); se derem certo, fecha; se falharem, abre de novo.
//
// Usamos a biblioteca gobreaker, da Sony.
package resilience

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/velosobr/passarim-bff/internal/adapter/metrics"
	"github.com/velosobr/passarim-bff/internal/domain"
	"github.com/velosobr/passarim-bff/internal/usecase"
)

// BreakerConfig são os limites do breaker. Interval é de quanto em quanto
// tempo os contadores do estado fechado são zerados, para falhas antigas não
// se somarem às de agora.
type BreakerConfig struct {
	MaxRequests         uint32        // chamadas de teste no half-open
	Interval            time.Duration // zera os contadores do estado fechado
	Timeout             time.Duration // quanto tempo fica aberto
	ConsecutiveFailures uint32        // abre com N falhas seguidas...
	MinRequests         uint32        // ...ou com FailureRatio de falhas em pelo menos N chamadas
	FailureRatio        float64
}

// DefaultBreakerConfig são os valores da spec (§5.4). Ficam como constantes
// comentadas, não como variáveis de ambiente.
func DefaultBreakerConfig() BreakerConfig {
	return BreakerConfig{MaxRequests: 3, Interval: 60 * time.Second, Timeout: 30 * time.Second, ConsecutiveFailures: 5, MinRequests: 10, FailureRatio: 0.6}
}

// breaker é o decorator: embrulha o próximo da cadeia e o disjuntor da
// biblioteca. "[any]" diz que o disjuntor guarda resultados de qualquer
// tipo; o guard converte de volta para o tipo certo.
type breaker struct {
	next usecase.CatalogReader
	cb   *gobreaker.CircuitBreaker[any]
}

// NewBreaker: um breaker único para o catalog (os 3 métodos dependem do mesmo
// serviço e do mesmo banco). Envolve o retry: uma operação lógica conta UMA vez.
func NewBreaker(next usecase.CatalogReader, cfg BreakerConfig, log *slog.Logger, m metrics.Metrics) usecase.CatalogReader {
	cb := gobreaker.NewCircuitBreaker[any](gobreaker.Settings{
		Name: "catalog", MaxRequests: cfg.MaxRequests, Interval: cfg.Interval, Timeout: cfg.Timeout,
		ReadyToTrip: func(c gobreaker.Counts) bool {
			return c.ConsecutiveFailures >= cfg.ConsecutiveFailures ||
				(c.Requests >= cfg.MinRequests && float64(c.TotalFailures)/float64(c.Requests) >= cfg.FailureRatio)
		},
		// Só falha de INFRAESTRUTURA conta. NotFound, InvalidArgument, Canceled (cliente que
		// desconectou) e Internal não são sinal de que o catalog esteja doente.
		IsSuccessful: func(err error) bool { return err == nil || !domain.KindOf(err).CountsForBreaker() },
		OnStateChange: func(name string, from, to gobreaker.State) {
			log.Info("circuit breaker mudou de estado", "breaker", name, "from", from.String(), "to", to.String())
			m.BreakerState(stateNumber(to))
		},
	})
	return &breaker{next: next, cb: cb}
}

// stateNumber converte o estado num número para a métrica: 0 fechado, 1
// meio-aberto, 2 aberto.
func stateNumber(s gobreaker.State) int {
	switch s {
	case gobreaker.StateHalfOpen:
		return 1
	case gobreaker.StateOpen:
		return 2
	}
	return 0
}

// guard roda fn dentro do breaker e traduz "aberto" para o Kind CircuitOpen.
//
// ErrTooManyRequests é a recusa do meio-aberto, quando as chamadas de teste
// já estão ocupadas; para quem chama, é o mesmo que "aberto". v.(T) é uma
// "type assertion": diz ao Go que o valor guardado como any é, na verdade,
// um T.
func guard[T any](b *breaker, fn func() (T, error)) (T, error) {
	v, err := b.cb.Execute(func() (any, error) { return fn() })
	if err != nil {
		var zero T
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return zero, domain.NewError(domain.KindCircuitOpen, err)
		}
		if v != nil {
			return v.(T), err // erros "normais" devolvem o valor parcial como veio
		}
		return zero, err
	}
	return v.(T), nil
}

// Os três métodos passam pela mesma proteção: um só breaker para o catalog.
func (b *breaker) ListSpecies(ctx context.Context, q domain.ListQuery) (domain.SpeciesPage, error) {
	return guard(b, func() (domain.SpeciesPage, error) { return b.next.ListSpecies(ctx, q) })
}

func (b *breaker) GetSpecies(ctx context.Context, id string) (domain.Species, error) {
	return guard(b, func() (domain.Species, error) { return b.next.GetSpecies(ctx, id) })
}

func (b *breaker) ListFilters(ctx context.Context) (domain.Filters, error) {
	return guard(b, func() (domain.Filters, error) { return b.next.ListFilters(ctx) })
}
