package middleware

import "net/http"

// Middleware é uma função que embrulha um handler com um comportamento comum.
type Middleware = func(http.Handler) http.Handler

// Chain aplica os middlewares: o PRIMEIRO da lista fica mais EXTERNO (roda primeiro).
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
