package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
)

// Observer recebe cada requisição concluída (a Task 8 liga isto ao Prometheus).
type Observer interface {
	ObserveHTTP(route, method string, status int, d time.Duration)
}

// AccessLog escreve UMA linha por requisição e avisa o Observer. A rota logada
// é o PADRÃO casado ("GET /v1/species/{id}"), nunca a URL com parâmetros: não
// vaza buscas do usuário e mantém a cardinalidade das métricas baixa.
func AccessLog(log *slog.Logger, ip IPResolver, obs Observer) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r) // o mux grava r.Pattern neste mesmo request
			d := time.Since(start)
			route := r.Pattern
			if route == "" || route == "/" { // "/" é o 404 genérico
				route = "unmatched"
			}
			log.InfoContext(r.Context(), "requisição",
				"request_id", reqmeta.RequestID(r.Context()), "method", r.Method, "route", route,
				"status", rec.Status(), "duration_ms", float64(d.Microseconds())/1000,
				"cache", reqmeta.From(r.Context()).Cache(), "client_ip", ip(r))
			if obs != nil {
				obs.ObserveHTTP(route, r.Method, rec.Status(), d)
			}
		})
	}
}
