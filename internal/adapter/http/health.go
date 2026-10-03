package httpadapter

import "net/http"

// /healthz = "o processo está vivo" (liveness).
// /readyz  = "pode receber tráfego" (readiness, usado pelo Traefik). NÃO consulta o
// catalog nem o Redis: com o catalog fora do ar, as duas réplicas ficariam
// não-prontas e o stale-while-error nunca seria servido (spec §6). Só fica 503 em shutdown.
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	plain(w, http.StatusOK, "ok")
}

func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	if h.Ready != nil && !h.Ready() {
		plain(w, http.StatusServiceUnavailable, "shutting down")
		return
	}
	plain(w, http.StatusOK, "ready")
}

func plain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
