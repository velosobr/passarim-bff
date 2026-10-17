// Respostas de erro no formato problem+json (RFC 9457), com códigos estáveis
// que o app usa para escolher a tela de erro.

package httpadapter

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
	"github.com/velosobr/passarim-bff/internal/domain"
)

// Códigos estáveis do contrato (o app mapeia cada um para uma tela de erro).
// "Estável" quer dizer que estes textos não mudam: o app compara com eles.
const (
	CodeInvalidParameter   = "INVALID_PARAMETER"
	CodeSpeciesNotFound    = "SPECIES_NOT_FOUND"
	CodeNotFound           = "NOT_FOUND"
	CodeMethodNotAllowed   = "METHOD_NOT_ALLOWED"
	CodeRateLimited        = "RATE_LIMITED"
	CodeServiceUnavailable = "SERVICE_UNAVAILABLE"
	CodeInternal           = "INTERNAL"
)

// problemTitles são os títulos legíveis de cada código, em português.
var problemTitles = map[string]string{
	CodeInvalidParameter:   "Parâmetro inválido",
	CodeSpeciesNotFound:    "Espécie não encontrada",
	CodeNotFound:           "Recurso não encontrado",
	CodeMethodNotAllowed:   "Método não permitido",
	CodeRateLimited:        "Muitas requisições",
	CodeServiceUnavailable: "Serviço temporariamente indisponível",
	CodeInternal:           "Erro interno",
}

// fieldErrorDTO é um parâmetro inválido dentro da lista "errors".
type fieldErrorDTO struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// problemDTO segue o RFC 9457 (application/problem+json).
//
// type, title e status são campos do padrão; code e requestId são extensões
// nossas. O requestId permite achar a requisição nos logs a partir de um
// print de erro. omitempty tira "errors" do JSON quando a lista está vazia.
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
//
// O type vira uma URN como "urn:passarim:problem:species-not-found", um
// identificador que não precisa ser uma página na web. no-store proíbe
// guardar respostas de erro em cache.
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
