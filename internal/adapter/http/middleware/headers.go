// Cabeçalhos de segurança aplicados em toda resposta.

package middleware

import "net/http"

// SecurityHeaders: cabeçalhos defensivos em TODA resposta (OWASP API8).
//
// OWASP API8 é o risco de "configuração de segurança incorreta". Os três
// cabeçalhos: nosniff proíbe o navegador de "adivinhar" o tipo do conteúdo
// (um JSON nunca é executado como script); a Content-Security-Policy
// "default-src 'none'" não deixa a resposta carregar nada; no-referrer não
// envia a URL de origem a outros sites.
func SecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Content-Security-Policy", "default-src 'none'")
			h.Set("Referrer-Policy", "no-referrer")
			next.ServeHTTP(w, r)
		})
	}
}
