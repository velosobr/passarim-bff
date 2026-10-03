package httpadapter

import (
	"errors"
	"net/http"

	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
	"github.com/velosobr/passarim-bff/internal/domain"
)

// statusClientClosed: convenção do nginx para "o cliente desistiu". Só aparece
// no log de acesso (ninguém lê a resposta), e não polui as métricas de 5xx.
const statusClientClosed = 499

// fail é o ÚNICO lugar que traduz erro de domínio em resposta HTTP.
// fieldHint diz qual parâmetro apontar quando o CATALOG reclama de InvalidArgument
// ("cursor" na lista, "id" no detalhe); nunca repassamos a mensagem dele.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error, fieldHint string) {
	kind := domain.KindOf(err)
	rid := reqmeta.RequestID(r.Context())
	switch kind {
	case domain.KindInvalidArgument:
		var ve *domain.ValidationError
		if errors.As(err, &ve) {
			WriteProblem(w, r, http.StatusBadRequest, CodeInvalidParameter, ve.Fields)
			return
		}
		h.Log.WarnContext(r.Context(), "catalog recusou o parâmetro", "request_id", rid, "error", err)
		WriteProblem(w, r, http.StatusBadRequest, CodeInvalidParameter, []domain.FieldError{{Field: fieldHint, Reason: "valor inválido"}})
	case domain.KindNotFound:
		WriteProblem(w, r, http.StatusNotFound, CodeSpeciesNotFound, nil)
	case domain.KindCanceled:
		w.WriteHeader(statusClientClosed) // sem corpo: ninguém está ouvindo
	case domain.KindUnavailable, domain.KindTimeout, domain.KindUpstream, domain.KindCircuitOpen:
		h.Log.WarnContext(r.Context(), "catalog indisponível", "request_id", rid, "kind", kind.String(), "error", err)
		WriteProblem(w, r, http.StatusServiceUnavailable, CodeServiceUnavailable, nil)
	default:
		h.Log.ErrorContext(r.Context(), "erro interno", "request_id", rid, "error", err)
		WriteProblem(w, r, http.StatusInternalServerError, CodeInternal, nil)
	}
}
