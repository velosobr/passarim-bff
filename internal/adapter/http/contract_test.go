package httpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	httpadapter "github.com/velosobr/passarim-bff/internal/adapter/http"
	"github.com/velosobr/passarim-bff/internal/adapter/http/middleware"
	"github.com/velosobr/passarim-bff/internal/adapter/media"
	"github.com/velosobr/passarim-bff/internal/domain"
	"github.com/velosobr/passarim-bff/internal/usecase"
)

// stubCatalog é o "catalog" por baixo dos casos de uso REAIS: cada cenário escolhe o que ele devolve.
type stubCatalog struct {
	page    domain.SpeciesPage
	species domain.Species
	filters domain.Filters
	err     error
}

func (s *stubCatalog) ListSpecies(context.Context, domain.ListQuery) (domain.SpeciesPage, error) {
	return s.page, s.err
}
func (s *stubCatalog) GetSpecies(context.Context, string) (domain.Species, error) {
	return s.species, s.err
}
func (s *stubCatalog) ListFilters(context.Context) (domain.Filters, error) { return s.filters, s.err }

var _ usecase.CatalogReader = (*stubCatalog)(nil)

// stack monta a MESMA pilha do main: middlewares + roteador + casos de uso reais.
func stack(t *testing.T, cat usecase.CatalogReader, burst int) (http.Handler, *httpadapter.Handler) {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	m, err := media.New("http://localhost:8888/buckets/passarim-media", log)
	if err != nil {
		t.Fatal(err)
	}
	h := &httpadapter.Handler{
		List:    usecase.ListSpecies{Catalog: cat, Budget: time.Second},
		Get:     usecase.GetSpecies{Catalog: cat, Budget: time.Second},
		Filters: usecase.ListFilters{Catalog: cat, Budget: time.Second},
		Media:   m, Log: log,
	}
	ip := func(*http.Request) string { return "203.0.113.9" }
	rl := middleware.NewRateLimiter(middleware.RateLimitConfig{RPS: 1, Burst: burst, MaxIPs: 10, IdleAfter: time.Minute}, ip, time.Now)
	return middleware.Chain(h.Routes(), middleware.Recovery(log), middleware.RequestID(), middleware.AccessLog(log, ip, nil),
		middleware.SecurityHeaders(), rl.Middleware(), middleware.Limits()), h
}

func loadSpec(t *testing.T) (*openapi3.T, routers.Router) {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile("../../../openapi.yaml")
	if err != nil {
		t.Fatalf("openapi.yaml não carrega: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml inválido: %v", err)
	}
	r, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	return doc, r
}

// validate confere UMA resposta real contra o contrato (status, cabeçalhos, tipo do corpo e schema).
func validate(t *testing.T, router routers.Router, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()
	route, params, err := router.FindRoute(req)
	if err != nil {
		t.Fatalf("%s %s não existe no openapi.yaml: %v", req.Method, req.URL.Path, err)
	}
	err = openapi3filter.ValidateResponse(context.Background(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, PathParams: params, Route: route},
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Body:                   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
	})
	if err != nil {
		t.Errorf("%s %s → %d viola o contrato: %v\ncorpo: %s", req.Method, req.URL.RequestURI(), rec.Code, err, rec.Body.String())
	}
}

func fullSpecies() domain.Species {
	size, diet := 23, "Frutos e insetos"
	return domain.Species{
		ID: "turdus-rufiventris", CommonName: "Sabiá-laranjeira", ScientificName: "Turdus rufiventris", Family: "Turdidae",
		Conservation: "LC", SizeCm: &size, Diet: &diet, Description: "d",
		DescriptionCredit: domain.Credit{Author: "Passarim", License: "CC-BY-SA", Source: "curated", SourceURL: "https://pt.wikipedia.org/x"},
		Facts:             []domain.Fact{{Text: "f", Source: "s"}},
		Biomes:            []domain.Biome{domain.BiomeMataAtlantica},
		States:            []string{"SP"},
		Photos: []domain.Photo{{ThumbKey: "species/t/photo-1-thumb.webp", MediumKey: "species/t/photo-1-medium.webp", LargeKey: "species/t/photo-1-large.webp",
			Width: 1600, Height: 1067, Credit: domain.Credit{Author: "Fulano", License: "CC-BY-NC", Source: "inaturalist", SourceURL: "https://www.inaturalist.org/photos/1"}}},
		Audio:    &domain.Audio{Key: "species/t/audio-9.aac", DurationMs: 21500, Credit: domain.Credit{Author: "B", License: "CC-BY-NC-SA", Source: "xeno-canto", SourceURL: "https://xeno-canto.org/9"}},
		Clusters: []domain.Cluster{{Lat: -23.5, Lng: -46.6, Count: 42, Precision: 0.5}},
	}
}

// validateProblemSchema: rota inexistente e método inválido não têm operação no contrato
// (o FindRoute do kin-openapi não os encontra), então o corpo é validado contra o schema Problem.
func validateProblemSchema(t *testing.T, doc *openapi3.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, esperado application/problem+json", ct)
	}
	var body any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo não é JSON: %v", err)
	}
	if err := doc.Components.Schemas["Problem"].Value.VisitJSON(body); err != nil {
		t.Errorf("corpo viola o schema Problem: %v\n%s", err, rec.Body.String())
	}
}

func TestContract_ResponsesMatchOpenAPI(t *testing.T) {
	doc, router := loadSpec(t)
	unavailable := domain.NewError(domain.KindUnavailable, errors.New("segredo-interno"))
	cases := []struct {
		name, method, target string
		cat                  *stubCatalog
		want                 int
		schemaOnly           bool // sem operação no contrato: valida só o schema Problem
	}{
		{"lista 200", "GET", "/v1/species?state=sp&limit=5", &stubCatalog{page: domain.SpeciesPage{Items: []domain.Summary{
			{ID: "a", CommonName: "A", ScientificName: "A a", ThumbnailKey: "species/a/photo-1-thumb.webp", Conservation: "VU"},
			{ID: "b", CommonName: "B", ScientificName: "B b"}}, NextCursor: "abc"}}, 200, false},
		{"lista vazia 200", "GET", "/v1/species", &stubCatalog{}, 200, false},
		{"detalhe completo 200", "GET", "/v1/species/turdus-rufiventris", &stubCatalog{species: fullSpecies()}, 200, false},
		{"detalhe só com o mínimo 200 (nulos e listas vazias)", "GET", "/v1/species/x", &stubCatalog{species: domain.Species{ID: "x"}}, 200, false},
		{"filtros 200", "GET", "/v1/filters", &stubCatalog{filters: domain.Filters{
			Biomes: []domain.BiomeCount{{Biome: domain.BiomePampa, SpeciesCount: 3}}, States: []domain.StateCount{{State: "RS", SpeciesCount: 5}}}}, 200, false},
		{"400 parâmetros inválidos", "GET", "/v1/species?limit=999&state=XX&biome=deserto", &stubCatalog{}, 400, false},
		{"400 id inválido", "GET", "/v1/species/ABC_def", &stubCatalog{}, 400, false},
		{"404 espécie", "GET", "/v1/species/nao-existe", &stubCatalog{err: domain.NewError(domain.KindNotFound, errors.New("x"))}, 404, false},
		{"404 rota", "GET", "/v1/species/a/b/c", &stubCatalog{}, 404, true},
		{"405 método", "POST", "/v1/species", &stubCatalog{}, 405, true},
		{"503 catalog fora", "GET", "/v1/species/x", &stubCatalog{err: unavailable}, 503, false},
		{"500 erro interno", "GET", "/v1/filters", &stubCatalog{err: domain.NewError(domain.KindInternal, errors.New("x"))}, 500, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, _ := stack(t, c.cat, 100)
			req := httptest.NewRequest(c.method, c.target, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status %d, esperado %d: %s", rec.Code, c.want, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "segredo-interno") {
				t.Fatal("texto interno vazou")
			}
			if c.schemaOnly {
				validateProblemSchema(t, doc, rec)
				return
			}
			validate(t, router, req, rec)
		})
	}
}

func TestContract_RateLimited429(t *testing.T) {
	_, router := loadSpec(t)
	h, _ := stack(t, &stubCatalog{}, 1) // balde de 1 ficha
	var last *httptest.ResponseRecorder
	var lastReq *http.Request
	for i := 0; i < 2; i++ {
		lastReq = httptest.NewRequest("GET", "/v1/filters", nil)
		last = httptest.NewRecorder()
		h.ServeHTTP(last, lastReq)
	}
	if last.Code != 429 {
		t.Fatalf("a 2ª requisição deveria ser 429, foi %d", last.Code)
	}
	validate(t, router, lastReq, last)
}

func TestContract_Panic500(t *testing.T) {
	_, router := loadSpec(t)
	h, _ := stack(t, panicCatalog{&stubCatalog{}}, 100)
	req := httptest.NewRequest("GET", "/v1/filters", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 500 {
		t.Fatalf("panic deveria virar 500: %d", rec.Code)
	}
	validate(t, router, req, rec)
}

type panicCatalog struct{ *stubCatalog }

func (panicCatalog) ListFilters(context.Context) (domain.Filters, error) { panic("boom") }

// Toda rota registrada precisa estar documentada: esquecer de atualizar o openapi.yaml quebra o CI.
func TestContract_EveryRegisteredRouteIsDocumented(t *testing.T) {
	doc, _ := loadSpec(t)
	_, h := stack(t, &stubCatalog{}, 1)
	for _, pattern := range h.Patterns() {
		method, path, ok := strings.Cut(pattern, " ")
		if !ok {
			t.Fatalf("padrão sem método: %q", pattern)
		}
		item := doc.Paths.Find(path)
		if item == nil || item.GetOperation(method) == nil {
			t.Errorf("a rota %q está registrada mas não existe no openapi.yaml", pattern)
		}
	}
}

// Contrato sem "rota fantasma": o inverso também vale para os endpoints públicos.
func TestContract_DocumentedPublicRoutesAreRegistered(t *testing.T) {
	doc, _ := loadSpec(t)
	_, h := stack(t, &stubCatalog{}, 1)
	registered := map[string]bool{}
	for _, p := range h.Patterns() {
		registered[p] = true
	}
	for path, item := range doc.Paths.Map() {
		if !strings.HasPrefix(path, "/v1/") {
			continue
		}
		for method := range item.Operations() {
			if !registered[method+" "+path] {
				t.Errorf("%s %s está no openapi.yaml mas não foi registrada", method, path)
			}
		}
	}
}
