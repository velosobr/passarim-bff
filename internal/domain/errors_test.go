// Testes da classificação de erros: a tabela de decisões de cada Kind e a
// descoberta do Kind a partir de qualquer erro.

package domain_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// A tabela é a única fonte de verdade sobre retry, breaker e stale: este
// teste a escreve por extenso, Kind por Kind. Mudar uma decisão (por
// exemplo, passar a repetir KindUpstream) exige mudar o teste junto, de
// propósito.
//
// O formato "lista de casos + laço" é o "table-driven test", o estilo mais
// comum de teste em Go: cada linha da tabela é um cenário.
func TestKindTable(t *testing.T) {
	cases := []struct {
		kind                  domain.Kind
		retry, breaker, stale bool
	}{
		{domain.KindUnavailable, true, true, true},
		{domain.KindTimeout, true, true, true},
		{domain.KindUpstream, false, true, true},
		{domain.KindCircuitOpen, false, false, true},
		{domain.KindNotFound, false, false, false},
		{domain.KindInvalidArgument, false, false, false},
		{domain.KindCanceled, false, false, false},
		{domain.KindInternal, false, false, false},
	}
	for _, c := range cases {
		if c.kind.Retryable() != c.retry || c.kind.CountsForBreaker() != c.breaker || c.kind.AllowsStale() != c.stale {
			t.Errorf("%s: retry=%v breaker=%v stale=%v; esperado %v/%v/%v", c.kind,
				c.kind.Retryable(), c.kind.CountsForBreaker(), c.kind.AllowsStale(), c.retry, c.breaker, c.stale)
		}
	}
}

// KindOf acha o Kind em erros diretos, em erros embrulhados com %w (o
// fmt.Errorf com %w guarda o erro original para o errors.As achar), nos erros
// de contexto e nos de validação. Um erro qualquer vira KindInternal.
func TestKindOf(t *testing.T) {
	cases := map[string]struct {
		err  error
		want domain.Kind
	}{
		"erro tipado":  {domain.NewError(domain.KindNotFound, errors.New("x")), domain.KindNotFound},
		"embrulhado":   {fmt.Errorf("camada: %w", domain.NewError(domain.KindUnavailable, errors.New("x"))), domain.KindUnavailable},
		"deadline":     {context.DeadlineExceeded, domain.KindTimeout},
		"cancelado":    {context.Canceled, domain.KindCanceled},
		"validação":    {&domain.ValidationError{Fields: []domain.FieldError{{Field: "id", Reason: "x"}}}, domain.KindInvalidArgument},
		"desconhecido": {errors.New("boom"), domain.KindInternal},
	}
	for name, c := range cases {
		if got := domain.KindOf(c.err); got != c.want {
			t.Errorf("%s: KindOf = %s, quer %s", name, got, c.want)
		}
	}
}

// O *Error guarda a causa original, e errors.Is consegue achá-la através do
// embrulho (graças ao método Unwrap).
func TestErrorKeepsCauseForLogsButIsUnwrappable(t *testing.T) {
	cause := errors.New("causa interna")
	err := domain.NewError(domain.KindUpstream, cause)
	if !errors.Is(err, cause) {
		t.Fatal("Unwrap deveria expor a causa (para logs e testes)")
	}
}
