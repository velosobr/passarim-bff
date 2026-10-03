package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"

	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
)

// O catalog descarta ids com mais de 64 caracteres; por isso o limite aqui.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID aceita o X-Request-Id recebido se for seguro; senão gera um novo
// (32 caracteres hex). Grava no contexto (reqmeta) e na resposta. Faz r.WithContext
// (uma cópia do request): por isso fica FORA do AccessLog (ver o aviso da Task 5).
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-Id")
			if !validRequestID.MatchString(id) {
				b := make([]byte, 16)
				_, _ = rand.Read(b) // crypto/rand não falha em sistemas suportados
				id = hex.EncodeToString(b)
			}
			w.Header().Set("X-Request-Id", id)
			next.ServeHTTP(w, r.WithContext(reqmeta.With(r.Context(), reqmeta.New(id))))
		})
	}
}
