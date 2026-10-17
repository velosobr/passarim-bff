// Id único por requisição, para seguir a mesma requisição nos logs do BFF e
// do catalog.

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
// (uma cópia do request): por isso fica FORA do AccessLog. O AccessLog lê o
// r.Pattern que o roteador grava no request; se o RequestID ficasse por
// dentro, o roteador gravaria o Pattern na cópia, e o AccessLog, segurando o
// original, não o veria.
//
// O id gerado usa crypto/rand (sorteio seguro): 16 bytes aleatórios viram
// 32 caracteres hexadecimais, sem chance prática de repetição.
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
