// Classificação de erros do BFF. Cada falha (do catalog ou nossa) ganha um
// Kind, e é o Kind que decide o que fazer com ela: tentar de novo, contar no
// circuit breaker, servir o dado velho do cache ou escolher o status HTTP.

package domain

import (
	"context"
	"errors"
)

// Kind classifica um erro vindo do catalog (ou do próprio BFF). A tabela
// abaixo é a ÚNICA fonte de verdade: o grpcclient traduz o código gRPC para
// um Kind, e retry, breaker, cache e HTTP só consultam estas funções.
type Kind int

// iota é um contador do Go que começa em 0 e soma 1 a cada linha do bloco
// const: KindInternal = 0, KindUnavailable = 1, e assim por diante. O valor
// zero é KindInternal de propósito: um erro sem classificação é tratado como
// bug, o caso mais seguro.
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

// String devolve o nome do Kind, usado em logs e mensagens de erro. Ter um
// método String() faz o tipo aparecer pelo nome (e não pelo número) quando é
// impresso com fmt ou slog. "[...]string{...}" é um array cujo tamanho o
// compilador conta sozinho; o índice é o próprio Kind.
func (k Kind) String() string {
	return [...]string{"Internal", "Unavailable", "Timeout", "Upstream", "CircuitOpen", "NotFound", "InvalidArgument", "Canceled"}[k]
}

// Retryable: vale a pena tentar de novo? Só falhas passageiras.
//
// Um catalog fora do ar por um instante ou uma resposta lenta podem dar certo
// na segunda tentativa. Um "não encontrado" ou um parâmetro inválido vão
// falhar igual, então repetir só gastaria tempo.
func (k Kind) Retryable() bool { return k == KindUnavailable || k == KindTimeout }

// CountsForBreaker: conta como falha do catalog? Cliente que cancela ou pede
// uma espécie inexistente NÃO é falha do catalog (senão abriria o circuito à toa).
func (k Kind) CountsForBreaker() bool {
	return k == KindUnavailable || k == KindTimeout || k == KindUpstream
}

// AllowsStale: se o catalog falhou assim, podemos servir o dado velho do cache?
//
// Sim quando a falha é do catalog (fora do ar, lento, quebrado ou com o
// circuito aberto): um dado de algumas horas atrás é melhor que um erro. Não
// quando a resposta é legítima, como um 404: servir uma espécie "velha" que
// não existe mais seria mentir.
func (k Kind) AllowsStale() bool {
	return k == KindUnavailable || k == KindTimeout || k == KindUpstream || k == KindCircuitOpen
}

// Error carrega o Kind e a causa original (a causa vai SÓ para logs: nunca para a resposta HTTP).
//
// Em Go, qualquer tipo com o método Error() string é um erro (a interface
// error é implícita: não existe "implements"). Este tipo embrulha o erro
// original sem perdê-lo.
type Error struct {
	Kind Kind
	Err  error
}

// NewError cria um erro classificado. Devolve o tipo error (e não *Error)
// para quem chama tratá-lo como qualquer outro erro.
func NewError(kind Kind, err error) error { return &Error{Kind: kind, Err: err} }

// Error monta a mensagem "Kind: causa", ou só o Kind quando não há causa.
func (e *Error) Error() string {
	if e.Err == nil {
		return e.Kind.String()
	}
	return e.Kind.String() + ": " + e.Err.Error()
}

// Unwrap devolve a causa. É o que permite a errors.Is e a errors.As
// "enxergarem" o erro de dentro do embrulho (por exemplo, achar um
// context.DeadlineExceeded embrulhado num *Error).
func (e *Error) Unwrap() error { return e.Err }

// KindOf descobre o Kind de qualquer erro da cadeia.
//
// A ordem importa: primeiro um *Error explícito (quem o criou já sabia o
// Kind); depois um erro de validação nosso; depois os erros de contexto
// (prazo estourado ou requisição cancelada). Qualquer outra coisa é
// KindInternal.
func KindOf(err error) Kind {
	// errors.As percorre a cadeia de Unwrap procurando um erro do tipo
	// pedido e, se acha, o coloca na variável apontada.
	var de *Error
	if errors.As(err, &de) {
		return de.Kind
	}
	var ve *ValidationError
	if errors.As(err, &ve) {
		return KindInvalidArgument
	}
	// errors.Is compara com um valor específico (e não com um tipo), também
	// atravessando os embrulhos.
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return KindTimeout
	case errors.Is(err, context.Canceled):
		return KindCanceled
	}
	return KindInternal
}
