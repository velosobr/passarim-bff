package httpadapter

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
	"github.com/velosobr/passarim-bff/internal/domain"
)

// Códigos estáveis do contrato (o app mapeia cada um para uma tela de erro).
const (
	CodeInvalidParameter   = "INVALID_PARAMETER"
	CodeSpeciesNotFound    = "SPECIES_NOT_FOUND"
	CodeNotFound           = "NOT_FOUND"
	CodeMethodNotAllowed   = "METHOD_NOT_ALLOWED"
	CodeRateLimited        = "RATE_LIMITED"
	CodeServiceUnavailable = "SERVICE_UNAVAILABLE"
	CodeInternal           = "INTERNAL"
)

var problemTitles = map[string]string{
	CodeInvalidParameter:   "Parâmetro inválido",
	CodeSpeciesNotFound:    "Espécie não encontrada",
	CodeNotFound:           "Recurso não encontrado",
	CodeMethodNotAllowed:   "Método não permitido",
	CodeRateLimited:        "Muitas requisições",
	CodeServiceUnavailable: "Serviço temporariamente indisponível",
	CodeInternal:           "Erro interno",
}

type fieldErrorDTO struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// problemDTO segue o RFC 9457 (application/problem+json).
type problemDTO struct {
	Type      string          `json:"type"`
	Title     string          `json:"title"`
	Status    int             `json:"status"`
	Code      string          `json:"code"`
	RequestID string          `json:"requestId"`
	Errors    []fieldErrorDTO `json:"errors,omitempty"`
}

// WriteProblem escreve um erro padronizado. Os textos são FIXOS (escritos aqui):
// nunca repassamos mensagem de erro de outra camada.
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, code string, fields []domain.FieldError) {
	p := problemDTO{
		Type:      "urn:passarim:problem:" + strings.ToLower(strings.ReplaceAll(code, "_", "-")),
		Title:     problemTitles[code],
		Status:    status,
		Code:      code,
		RequestID: reqmeta.RequestID(r.Context()),
	}
	for _, f := range fields {
		p.Errors = append(p.Errors, fieldErrorDTO{Field: f.Field, Reason: f.Reason})
	}
	body, err := json.Marshal(p)
	if err != nil { // não deveria acontecer; resposta mínima segura
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
