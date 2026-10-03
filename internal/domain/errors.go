package domain

import (
	"context"
	"errors"
)

// Kind classifica um erro vindo do catalog (ou do próprio BFF). A tabela
// abaixo é a ÚNICA fonte de verdade: o grpcclient traduz o código gRPC para
// um Kind, e retry, breaker, cache e HTTP só consultam estas funções.
type Kind int

const (
	KindInternal        Kind = iota // bug, resposta inesperada, Unimplemented...
	KindUnavailable                 // gRPC Unavailable
	KindTimeout                     // gRPC DeadlineExceeded (ou nosso timeout)
	KindUpstream                    // gRPC Internal/Unknown/ResourceExhausted (ex.: Postgres caiu)
	KindCircuitOpen                 // breaker aberto
	KindNotFound                    // gRPC NotFound
	KindInvalidArgument             // gRPC InvalidArgument (ou validação nossa)
	KindCanceled                    // o cliente desistiu
)

func (k Kind) String() string {
	return [...]string{"Internal", "Unavailable", "Timeout", "Upstream", "CircuitOpen", "NotFound", "InvalidArgument", "Canceled"}[k]
}

// Retryable: vale a pena tentar de novo? Só falhas passageiras.
func (k Kind) Retryable() bool { return k == KindUnavailable || k == KindTimeout }

// CountsForBreaker: conta como falha do catalog? Cliente que cancela ou pede
// uma espécie inexistente NÃO é falha do catalog (senão abriria o circuito à toa).
func (k Kind) CountsForBreaker() bool {
	return k == KindUnavailable || k == KindTimeout || k == KindUpstream
}

// AllowsStale: se o catalog falhou assim, podemos servir o dado velho do cache?
func (k Kind) AllowsStale() bool {
	return k == KindUnavailable || k == KindTimeout || k == KindUpstream || k == KindCircuitOpen
}

// Error carrega o Kind e a causa original (a causa vai SÓ para logs: nunca para a resposta HTTP).
type Error struct {
	Kind Kind
	Err  error
}

func NewError(kind Kind, err error) error { return &Error{Kind: kind, Err: err} }

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Kind.String()
	}
	return e.Kind.String() + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// KindOf descobre o Kind de qualquer erro da cadeia.
func KindOf(err error) Kind {
	var de *Error
	if errors.As(err, &de) {
		return de.Kind
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		return KindInvalidArgument
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return KindTimeout
	case errors.Is(err, context.Canceled):
		return KindCanceled
	}
	return KindInternal
}
