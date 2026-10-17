// Testes do circuit breaker: quando ele abre, quais erros contam, como ele
// volta a fechar e o Kind que ele devolve quando está aberto.

package resilience_test

import (
	"context"
	"testing"
	"time"

	"github.com/velosobr/passarim-bff/internal/adapter/resilience"
	"github.com/velosobr/passarim-bff/internal/domain"
)

// fastBreaker usa os valores de produção, menos o tempo aberto, encurtado
// para o teste não demorar.
func fastBreaker() resilience.BreakerConfig {
	c := resilience.DefaultBreakerConfig()
	c.Timeout = 30 * time.Millisecond // em produção são 30 s; aqui 30 ms para o teste não demorar
	return c
}

// newBreaker monta o breaker sobre um catalog roteirizado com os erros
// dados e devolve uma função que faz uma chamada.
func newBreaker(t *testing.T, cfg resilience.BreakerConfig, errs ...error) (*scripted, *recMetrics, func() error) {
	t.Helper()
	s, m := &scripted{errs: errs}, &recMetrics{}
	b := resilience.NewBreaker(s, cfg, testLogger(), m)
	return s, m, func() error { _, err := b.GetSpecies(context.Background(), "x"); return err }
}

// Com 5 falhas seguidas, o circuito abre: a 6ª chamada falha na hora com
// CircuitOpen, sem chegar ao catalog, e a métrica vai para 2 (aberto).
func TestBreaker_OpensAfter5ConsecutiveFailures(t *testing.T) {
	s, m, call := newBreaker(t, fastBreaker(), kindErr(domain.KindUnavailable))
	for i := 0; i < 5; i++ {
		_ = call()
	}
	err := call()
	if domain.KindOf(err) != domain.KindCircuitOpen || s.calls != 5 {
		t.Fatalf("a 6ª chamada deveria falhar na hora, sem chegar ao catalog: err=%v calls=%d", err, s.calls)
	}
	if len(m.states) == 0 || m.states[len(m.states)-1] != 2 {
		t.Fatalf("a métrica do estado deveria ser 2 (aberto): %v", m.states)
	}
}

// O circuito também abre pela proporção: 60% de falhas em pelo menos 10
// chamadas, mesmo sem 5 seguidas.
func TestBreaker_OpensOnFailureRatio(t *testing.T) {
	// 6 falhas e 4 sucessos intercalados (nunca 5 seguidas): 60% em 10 chamadas. A ÚLTIMA precisa
	// ser uma falha: o ReadyToTrip só é avaliado quando uma chamada falha.
	f := func() error { return kindErr(domain.KindUnavailable) }
	pattern := []error{f(), nil, f(), f(), nil, f(), nil, f(), nil, f()}
	s, _, call := newBreaker(t, fastBreaker(), pattern...)
	for range pattern {
		_ = call()
	}
	if err := call(); domain.KindOf(err) != domain.KindCircuitOpen {
		t.Fatalf("60%% de falhas em 10 chamadas deveria abrir: %v (calls=%d)", err, s.calls)
	}
}

// NotFound, InvalidArgument, Canceled e Internal NÃO são falha do catalog. Mesmo com 30
// erros desses, o circuito continua fechado e todas as chamadas chegam ao
// catalog.
func TestBreaker_IgnoresNonInfrastructureErrors(t *testing.T) {
	for _, k := range []domain.Kind{domain.KindNotFound, domain.KindInvalidArgument, domain.KindCanceled, domain.KindInternal} {
		s, _, call := newBreaker(t, fastBreaker(), kindErr(k))
		for i := 0; i < 30; i++ {
			if err := call(); domain.KindOf(err) != k {
				t.Fatalf("%s: o circuito abriu indevidamente na chamada %d: %v", k, i, err)
			}
		}
		if s.calls != 30 {
			t.Errorf("%s: todas as chamadas deveriam chegar ao catalog (%d)", k, s.calls)
		}
	}
}

// O ciclo completo: abre com 5 falhas, espera o tempo aberto, deixa passar
// as 3 chamadas de teste do meio-aberto e, como elas dão certo, fecha. O
// time.Sleep aqui espera de verdade, porque o gobreaker usa o relógio real.
func TestBreaker_HalfOpenThenClosesOnSuccess(t *testing.T) {
	errs := []error{kindErr(domain.KindUnavailable), kindErr(domain.KindUnavailable), kindErr(domain.KindUnavailable), kindErr(domain.KindUnavailable), kindErr(domain.KindUnavailable), nil}
	s, m, call := newBreaker(t, fastBreaker(), errs...)
	for i := 0; i < 5; i++ {
		_ = call()
	}
	if domain.KindOf(call()) != domain.KindCircuitOpen {
		t.Fatal("deveria estar aberto")
	}
	time.Sleep(60 * time.Millisecond) // passou o Timeout: vira half-open
	// No half-open o breaker deixa passar MaxRequests (3) chamadas de teste; com 3 sucessos seguidos, fecha.
	for i := 0; i < 3; i++ {
		if err := call(); err != nil {
			t.Fatalf("chamada de teste %d do half-open deveria passar: %v", i+1, err)
		}
	}
	if err := call(); err != nil || s.calls != 9 {
		t.Fatalf("circuito fechado de novo: err=%v calls=%d (5 falhas + 3 de teste + 1)", err, s.calls)
	}
	if m.states[len(m.states)-1] != 0 {
		t.Fatalf("estado final deveria ser 0 (fechado): %v", m.states)
	}
}

// O erro de circuito aberto libera o dado velho do cache e não conta como
// falha do catalog.
func TestBreaker_WrapsErrorsAsCircuitOpenKind(t *testing.T) {
	_, _, call := newBreaker(t, fastBreaker(), kindErr(domain.KindUnavailable))
	for i := 0; i < 5; i++ {
		_ = call()
	}
	if k := domain.KindOf(call()); !k.AllowsStale() || k.CountsForBreaker() {
		t.Fatalf("CircuitOpen libera stale e não conta no breaker: %s", k)
	}
}
