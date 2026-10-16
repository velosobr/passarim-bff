// Testes do retry, com relógio, espera e sorteio falsos: o teste roda
// instantâneo e cada valor de tempo é previsível.
package resilience_test

import (
	"context"
	"testing"
	"time"

	"github.com/velosobr/passarim-bff/internal/adapter/resilience"
	"github.com/velosobr/passarim-bff/internal/domain"
)

// retryEnv guarda o catalog roteirizado, as esperas pedidas (waits), os
// tetos de jitter pedidos (maxes), as métricas e o "agora" fixo do relógio.
type retryEnv struct {
	r     *scripted
	waits []time.Duration
	maxes []time.Duration
	m     *recMetrics
	base  time.Time
}

// newRetry monta o retry com 2 retries, tentativas de até 2 s e um prazo de
// 3 s por chamada. O jitter devolve sempre o teto, o pior caso.
func newRetry(t *testing.T, errs ...error) (*retryEnv, func() error) {
	t.Helper()
	env := &retryEnv{r: &scripted{errs: errs}, m: &recMetrics{}, base: time.Now()}
	cfg := resilience.RetryConfig{
		MaxRetries: 2, AttemptTimeout: 2 * time.Second,
		Now:    func() time.Time { return env.base },
		Sleep:  func(_ context.Context, d time.Duration) error { env.waits = append(env.waits, d); return nil },
		Jitter: func(max time.Duration) time.Duration { env.maxes = append(env.maxes, max); return max }, // pior caso: espera o máximo
	}
	rd := resilience.NewRetry(env.r, cfg, env.m)
	return env, func() error {
		ctx, cancel := context.WithDeadline(context.Background(), env.base.Add(3*time.Second))
		defer cancel()
		_, err := rd.GetSpecies(ctx, "x")
		return err
	}
}

// Unavailable e Timeout são repetidos até dar certo na 3ª tentativa; todos
// os outros Kinds falham na primeira, sem repetir.
func TestRetry_RetriesOnlyUnavailableAndTimeout(t *testing.T) {
	for _, k := range []domain.Kind{domain.KindUnavailable, domain.KindTimeout} {
		env, call := newRetry(t, kindErr(k), kindErr(k), nil)
		if err := call(); err != nil || env.r.calls != 3 {
			t.Errorf("%s: esperava sucesso na 3ª tentativa: err=%v calls=%d", k, err, env.r.calls)
		}
	}
	for _, k := range []domain.Kind{domain.KindNotFound, domain.KindInvalidArgument, domain.KindUpstream, domain.KindInternal, domain.KindCanceled, domain.KindCircuitOpen} {
		env, call := newRetry(t, kindErr(k))
		if err := call(); domain.KindOf(err) != k || env.r.calls != 1 {
			t.Errorf("%s: não deveria repetir: err=%v calls=%d", k, err, env.r.calls)
		}
	}
}

// Com falha sempre, são 3 chamadas (1 + 2 retries) e 2 retries na métrica.
func TestRetry_GivesUpAfterMaxRetries(t *testing.T) {
	env, call := newRetry(t, kindErr(domain.KindUnavailable))
	err := call()
	if domain.KindOf(err) != domain.KindUnavailable || env.r.calls != 3 {
		t.Fatalf("1 tentativa + 2 retries = 3 chamadas: err=%v calls=%d", err, env.r.calls)
	}
	if env.m.retries != 2 {
		t.Fatalf("métrica de retries = %d", env.m.retries)
	}
}

// O teto da espera dobra a cada retry: 100 ms e depois 200 ms.
func TestRetry_BackoffIsExponentialWithJitterBounds(t *testing.T) {
	env, call := newRetry(t, kindErr(domain.KindUnavailable))
	_ = call()
	// Antes do retry n espera um valor em [0, 50ms × 2ⁿ]: n=1 → 100ms, n=2 → 200ms.
	if len(env.maxes) != 2 || env.maxes[0] != 100*time.Millisecond || env.maxes[1] != 200*time.Millisecond {
		t.Fatalf("limites do jitter: %v", env.maxes)
	}
}

// Com pouco tempo sobrando, o retry desiste em vez de fazer uma tentativa
// que não teria tempo de terminar.
func TestRetry_NoNewAttemptWithoutRemainingBudget(t *testing.T) {
	env := &retryEnv{r: &scripted{errs: []error{kindErr(domain.KindUnavailable)}}, m: &recMetrics{}, base: time.Now()}
	rd := resilience.NewRetry(env.r, resilience.RetryConfig{
		MaxRetries: 2, AttemptTimeout: 2 * time.Second, Now: func() time.Time { return env.base },
		Sleep: func(context.Context, time.Duration) error { return nil }, Jitter: func(max time.Duration) time.Duration { return max },
	}, env.m)
	// Sobram só 250ms: depois do backoff de 100ms restariam 150ms (< 200ms mínimos) → não tenta de novo.
	ctx, cancel := context.WithDeadline(context.Background(), env.base.Add(250*time.Millisecond))
	defer cancel()
	_, err := rd.GetSpecies(ctx, "x")
	if domain.KindOf(err) != domain.KindUnavailable || env.r.calls != 1 {
		t.Fatalf("sem orçamento, não repete: err=%v calls=%d", err, env.r.calls)
	}
}

// Cada tentativa tem o menor entre o limite da config (2 s) e o que resta do
// prazo da requisição.
func TestRetry_AttemptTimeoutIsMinOfConfigAndRemaining(t *testing.T) {
	env, call := newRetry(t, nil)
	_ = call()
	if got := env.r.deadlines[0].Sub(env.base); got > 2*time.Second+100*time.Millisecond || got < 1900*time.Millisecond {
		t.Fatalf("tentativa deveria ter ~2s (limite da config), tem %v", got)
	}
	// Com só 500ms de orçamento total, a tentativa não pode passar disso.
	env2 := &retryEnv{r: &scripted{}, m: &recMetrics{}, base: time.Now()}
	rd := resilience.NewRetry(env2.r, resilience.RetryConfig{MaxRetries: 0, AttemptTimeout: 2 * time.Second, Now: func() time.Time { return env2.base }}, env2.m)
	ctx, cancel := context.WithDeadline(context.Background(), env2.base.Add(500*time.Millisecond))
	defer cancel()
	_, _ = rd.GetSpecies(ctx, "x")
	if got := env2.r.deadlines[0].Sub(env2.base); got > 500*time.Millisecond {
		t.Fatalf("a tentativa nunca estende o prazo da requisição: %v", got)
	}
}

// Cada tentativa vira uma métrica com o método e o resultado.
func TestRetry_RecordsAttemptMetricsPerMethodAndCode(t *testing.T) {
	env, call := newRetry(t, kindErr(domain.KindUnavailable), nil)
	_ = call()
	if len(env.m.attempts) != 2 || env.m.attempts[0] != "GetSpecies:Unavailable" || env.m.attempts[1] != "GetSpecies:ok" {
		t.Fatalf("tentativas: %v", env.m.attempts)
	}
}

// Se o cliente desiste durante a pausa, o retry para na hora e devolve
// Canceled.
func TestRetry_StopsWhenContextIsCanceledDuringBackoff(t *testing.T) {
	env := &retryEnv{r: &scripted{errs: []error{kindErr(domain.KindUnavailable)}}, m: &recMetrics{}, base: time.Now()}
	rd := resilience.NewRetry(env.r, resilience.RetryConfig{
		MaxRetries: 2, AttemptTimeout: 2 * time.Second, Now: func() time.Time { return env.base },
		Sleep: func(ctx context.Context, _ time.Duration) error { return context.Canceled }, Jitter: func(max time.Duration) time.Duration { return max },
	}, env.m)
	_, err := rd.GetSpecies(context.Background(), "x")
	if domain.KindOf(err) != domain.KindCanceled || env.r.calls != 1 {
		t.Fatalf("cancelado no backoff: err=%v calls=%d", err, env.r.calls)
	}
}
