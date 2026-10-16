// Package middleware tem as camadas que toda requisição HTTP atravessa
// antes de chegar às rotas: recuperação de pânico, id da requisição, log de
// acesso, cabeçalhos de segurança, rate limit e limites de método e corpo.
//
// Um middleware recebe o próximo handler e devolve um novo handler que faz
// alguma coisa antes e/ou depois de chamá-lo. É o mesmo padrão decorator da
// cadeia do catalog, aplicado ao HTTP. Em Go, um http.Handler é qualquer
// tipo com o método ServeHTTP(w, r); http.HandlerFunc transforma uma função
// comum num handler.
package middleware

import "net/http"

// Middleware é uma função que embrulha um handler com um comportamento comum.
// O "=" faz deste nome um apelido (alias) do tipo função, e não um tipo
// novo: qualquer func(http.Handler) http.Handler serve, sem conversão.
type Middleware = func(http.Handler) http.Handler

// Chain aplica os middlewares: o PRIMEIRO da lista fica mais EXTERNO (roda primeiro).
// Por isso o laço anda de trás para a frente: embrulhamos primeiro com o
// último, e o primeiro fica por fora de todos.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
