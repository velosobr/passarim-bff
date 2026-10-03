package middleware

import (
	"net/http"

	httpadapter "github.com/velosobr/passarim-bff/internal/adapter/http"
)

// maxBodyBytes: a API só tem GET, então o corpo nunca é lido; o limite é defesa extra.
const maxBodyBytes = 1 << 10

// Limits recusa qualquer método que não seja GET/HEAD (405 em problem+json, o
// mux da stdlib responderia texto puro) e limita o corpo.
func Limits() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				httpadapter.WriteProblem(w, r, http.StatusMethodNotAllowed, httpadapter.CodeMethodNotAllowed, nil)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes) // mesmo request: o Pattern do mux continua visível ao AccessLog
			next.ServeHTTP(w, r)
		})
	}
}
