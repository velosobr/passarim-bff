package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	httpadapter "github.com/velosobr/passarim-bff/internal/adapter/http"
	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
)

// Recovery impede que um panic derrube o processo: responde 500 limpo (sem
// stack na resposta) e loga a stack com o request_id. É o middleware mais
// externo; o request_id já foi gravado na RESPOSTA pelo middleware RequestID.
func Recovery(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w}
			defer func() {
				p := recover()
				if p == nil {
					return
				}
				if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) { // sinal legítimo do net/http: deixa passar
					panic(p)
				}
				rid := w.Header().Get("X-Request-Id")
				log.ErrorContext(r.Context(), "panic na requisição", "request_id", rid, "panic", p, "stack", string(debug.Stack()))
				if rec.wrote { // a resposta já começou: não dá mais para trocar o status
					return
				}
				r2 := r.WithContext(reqmeta.With(r.Context(), reqmeta.New(rid)))
				httpadapter.WriteProblem(w, r2, http.StatusInternalServerError, httpadapter.CodeInternal, nil)
			}()
			next.ServeHTTP(rec, r)
		})
	}
}
