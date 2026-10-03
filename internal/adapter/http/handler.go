// Package httpadapter é a borda REST do BFF: rotas /v1, DTOs, erros
// problem+json. Os middlewares (Task 5) ficam em ./middleware.
package httpadapter

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/velosobr/passarim-bff/internal/adapter/media"
	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
	"github.com/velosobr/passarim-bff/internal/domain"
)

type SpeciesLister interface {
	Run(ctx context.Context, raw domain.RawListQuery) (domain.SpeciesPage, error)
}

type SpeciesGetter interface {
	Run(ctx context.Context, id string) (domain.Species, error)
}

type FiltersLister interface {
	Run(ctx context.Context) (domain.Filters, error)
}

type Handler struct {
	List    SpeciesLister
	Get     SpeciesGetter
	Filters FiltersLister
	Media   *media.Builder
	Log     *slog.Logger
}

// Routes monta o roteador da stdlib (Go 1.22+: padrões com método e {id}).
// O "/" no fim captura rota inexistente para responder 404 em problem+json
// (o mux padrão responderia texto puro).
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/species", h.listSpecies)
	mux.HandleFunc("GET /v1/species/{id}", h.getSpecies)
	mux.HandleFunc("GET /v1/filters", h.listFilters)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(w, r, http.StatusNotFound, CodeNotFound, nil)
	})
	return mux
}

var listParams = []string{"q", "biome", "state", "cursor", "limit"}

func (h *Handler) listSpecies(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	raw := domain.RawListQuery{Q: query.Get("q"), Biome: query.Get("biome"), State: query.Get("state"), Cursor: query.Get("cursor"), Limit: query.Get("limit")}
	for _, p := range listParams {
		if len(query[p]) > 1 {
			raw.Repeated = append(raw.Repeated, p)
		}
	}
	page, err := h.List.Run(r.Context(), raw)
	if err != nil {
		h.fail(w, r, err, "cursor")
		return
	}
	h.ok(w, r, toPageDTO(r.Context(), page, h.Media))
}

func (h *Handler) getSpecies(w http.ResponseWriter, r *http.Request) {
	s, err := h.Get.Run(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err, "id")
		return
	}
	h.ok(w, r, toSpeciesDTO(r.Context(), s, h.Media))
}

func (h *Handler) listFilters(w http.ResponseWriter, r *http.Request) {
	f, err := h.Filters.Run(r.Context())
	if err != nil {
		h.fail(w, r, err, "")
		return
	}
	h.ok(w, r, toFiltersDTO(f))
}

// ok escreve um 200 com os cabeçalhos de cache. O corpo é montado ANTES de
// escrever, para um erro de serialização nunca deixar uma resposta pela metade.
func (h *Handler) ok(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		h.fail(w, r, err, "")
		return
	}
	cache := reqmeta.From(r.Context()).Cache()
	if cache == "" {
		cache = "miss"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Header().Set("X-Cache", cache)
	_, _ = w.Write(body)
}
