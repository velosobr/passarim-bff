package httpadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpadapter "github.com/velosobr/passarim-bff/internal/adapter/http"
	"github.com/velosobr/passarim-bff/internal/adapter/media"
	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
	"github.com/velosobr/passarim-bff/internal/domain"
)

// Fakes dos casos de uso: o handler só conhece as interfaces.
type fakeList struct {
	page domain.SpeciesPage
	err  error
	got  domain.RawListQuery
}

func (f *fakeList) Run(_ context.Context, raw domain.RawListQuery) (domain.SpeciesPage, error) {
	f.got = raw
	return f.page, f.err
}

type fakeGet struct {
	sp  domain.Species
	err error
}

func (f *fakeGet) Run(context.Context, string) (domain.Species, error) { return f.sp, f.err }

type fakeFilters struct {
	f   domain.Filters
	err error
}

func (f *fakeFilters) Run(context.Context) (domain.Filters, error) { return f.f, f.err }

const base = "http://localhost:8888/buckets/passarim-media"

func newHandler(t *testing.T, l *fakeList, g *fakeGet, f *fakeFilters) http.Handler {
	t.Helper()
	m, err := media.New(base, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	h := &httpadapter.Handler{List: l, Get: g, Filters: f, Media: m, Log: slog.New(slog.DiscardHandler)}
	return h.Routes()
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req = req.WithContext(reqmeta.With(req.Context(), reqmeta.New("req-1")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, rec.Body.String())
	}
	return m
}

func TestListSpecies_OK(t *testing.T) {
	l := &fakeList{page: domain.SpeciesPage{Items: []domain.Summary{
		{ID: "turdus-rufiventris", CommonName: "Sabiá-laranjeira", ScientificName: "Turdus rufiventris", ThumbnailKey: "species/turdus-rufiventris/photo-1-thumb.webp", Conservation: "LC"},
		{ID: "cariama-cristata", CommonName: "Seriema", ScientificName: "Cariama cristata"},
	}}}
	rec := do(newHandler(t, l, &fakeGet{}, &fakeFilters{}), "GET", "/v1/species?state=sp&limit=10")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("status/content-type: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if l.got.State != "sp" || l.got.Limit != "10" {
		t.Fatalf("o handler deve repassar as strings cruas: %+v", l.got)
	}
	body := decode(t, rec)
	items := body["items"].([]any)
	first, second := items[0].(map[string]any), items[1].(map[string]any)
	if first["thumbnailUrl"] != base+"/species/turdus-rufiventris/photo-1-thumb.webp" || first["commonName"] != "Sabiá-laranjeira" {
		t.Fatalf("primeiro item: %v", first)
	}
	if c := first["conservationStatus"].(map[string]any); c["code"] != "LC" || c["label"] != "Pouco preocupante" {
		t.Fatalf("conservação: %v", c)
	}
	if second["thumbnailUrl"] != nil || second["conservationStatus"] != nil {
		t.Fatalf("sem foto e sem status devem ser null: %v", second)
	}
	if v, ok := body["nextCursor"]; !ok || v != nil {
		t.Fatalf("nextCursor deveria ser null (e presente): %v %v", v, ok)
	}
}

func TestListSpecies_EmptyListIsArrayNotNull(t *testing.T) {
	rec := do(newHandler(t, &fakeList{}, &fakeGet{}, &fakeFilters{}), "GET", "/v1/species")
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("lista vazia deve sair como []: %s", rec.Body.String())
	}
}

func TestListSpecies_NextCursor(t *testing.T) {
	l := &fakeList{page: domain.SpeciesPage{NextCursor: "abc"}}
	if body := decode(t, do(newHandler(t, l, &fakeGet{}, &fakeFilters{}), "GET", "/v1/species")); body["nextCursor"] != "abc" {
		t.Fatalf("nextCursor: %v", body["nextCursor"])
	}
}

func TestListSpecies_RepeatedParamIsInvalid(t *testing.T) {
	l := &fakeList{}
	rec := do(newHandler(t, l, &fakeGet{}, &fakeFilters{}), "GET", "/v1/species?state=SP&state=RJ")
	if len(l.got.Repeated) != 1 || l.got.Repeated[0] != "state" {
		t.Fatalf("o handler deve informar o parâmetro repetido: %+v", l.got)
	}
	_ = rec
}

func TestInvalidParameter_400WithFields(t *testing.T) {
	l := &fakeList{err: &domain.ValidationError{Fields: []domain.FieldError{{Field: "limit", Reason: "deve ser um inteiro entre 1 e 50"}, {Field: "state", Reason: "UF desconhecida"}}}}
	rec := do(newHandler(t, l, &fakeGet{}, &fakeFilters{}), "GET", "/v1/species?limit=999&state=XX")
	if rec.Code != 400 || rec.Header().Get("Content-Type") != "application/problem+json" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("400: %d %s %s", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"))
	}
	b := decode(t, rec)
	if b["code"] != "INVALID_PARAMETER" || b["type"] != "urn:passarim:problem:invalid-parameter" || b["requestId"] != "req-1" || b["status"].(float64) != 400 || b["title"] == "" {
		t.Fatalf("problem: %v", b)
	}
	if errs := b["errors"].([]any); len(errs) != 2 || errs[0].(map[string]any)["field"] != "limit" {
		t.Fatalf("errors[]: %v", b["errors"])
	}
}

func TestGetSpecies_FullDetail(t *testing.T) {
	size, diet := 23, "Frutos e insetos"
	g := &fakeGet{sp: domain.Species{
		ID: "turdus-rufiventris", CommonName: "Sabiá-laranjeira", ScientificName: "Turdus rufiventris", Family: "Turdidae",
		Conservation: "LC", SizeCm: &size, Diet: &diet, Description: "d",
		DescriptionCredit: domain.Credit{Author: "Passarim", License: "CC-BY-SA", Source: "curated", SourceURL: "https://pt.wikipedia.org/x"},
		Facts:             []domain.Fact{{Text: "f", Source: "s"}},
		Biomes:            []domain.Biome{domain.BiomeMataAtlantica, domain.BiomeCerrado},
		States:            []string{"SP", "RJ"},
		Photos: []domain.Photo{{ThumbKey: "species/t/photo-1-thumb.webp", MediumKey: "species/t/photo-1-medium.webp", LargeKey: "species/t/photo-1-large.webp",
			Width: 1600, Height: 1067, Credit: domain.Credit{Author: "Fulano", License: "CC-BY-NC", Source: "inaturalist", SourceURL: "https://www.inaturalist.org/photos/1"}}},
		Audio:    &domain.Audio{Key: "species/t/audio-9.aac", DurationMs: 21500, Credit: domain.Credit{Author: "Beltrano", License: "CC-BY-NC-SA", Source: "xeno-canto", SourceURL: "https://xeno-canto.org/9"}},
		Clusters: []domain.Cluster{{Lat: -23.5, Lng: -46.6, Count: 42, Precision: 0.5}},
	}}
	rec := do(newHandler(t, &fakeList{}, g, &fakeFilters{}), "GET", "/v1/species/turdus-rufiventris")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	b := decode(t, rec)
	if b["sizeCm"].(float64) != 23 || b["diet"] != "Frutos e insetos" || b["family"] != "Turdidae" {
		t.Fatalf("campos: %v", b)
	}
	chips := b["chips"].([]any)
	if len(chips) != 2 || chips[0].(map[string]any)["label"] != "Mata Atlântica" {
		t.Fatalf("chips (biomas rotulados): %v", chips)
	}
	photo := b["photos"].([]any)[0].(map[string]any)
	if photo["largeUrl"] != base+"/species/t/photo-1-large.webp" || photo["credit"].(map[string]any)["sourceUrl"] != "https://www.inaturalist.org/photos/1" || photo["width"].(float64) != 1600 {
		t.Fatalf("foto: %v", photo)
	}
	audio := b["audio"].(map[string]any)
	if audio["url"] != base+"/species/t/audio-9.aac" || audio["durationMs"].(float64) != 21500 {
		t.Fatalf("áudio: %v", audio)
	}
	w := b["whereToFind"].(map[string]any)
	if w["biomes"].([]any)[0] != "mata_atlantica" || w["states"].([]any)[1] != "RJ" || w["clusters"].([]any)[0].(map[string]any)["count"].(float64) != 42 {
		t.Fatalf("whereToFind: %v", w)
	}
}

func TestGetSpecies_NullablesAndEmptyArrays(t *testing.T) {
	rec := do(newHandler(t, &fakeList{}, &fakeGet{sp: domain.Species{ID: "x"}}, &fakeFilters{}), "GET", "/v1/species/x")
	b := decode(t, rec)
	for _, k := range []string{"sizeCm", "diet", "audio", "conservationStatus"} {
		if v, ok := b[k]; !ok || v != nil {
			t.Errorf("%s deveria estar presente e null: %v %v", k, v, ok)
		}
	}
	for _, k := range []string{"facts", "chips", "photos"} {
		if arr, ok := b[k].([]any); !ok || len(arr) != 0 {
			t.Errorf("%s deveria ser []: %v", k, b[k])
		}
	}
	w := b["whereToFind"].(map[string]any)
	for _, k := range []string{"states", "biomes", "clusters"} {
		if arr, ok := w[k].([]any); !ok || len(arr) != 0 {
			t.Errorf("whereToFind.%s deveria ser []: %v", k, w[k])
		}
	}
}

func TestGetSpecies_NotFoundAndInvalidID(t *testing.T) {
	g := &fakeGet{err: domain.NewError(domain.KindNotFound, errors.New("x"))}
	rec := do(newHandler(t, &fakeList{}, g, &fakeFilters{}), "GET", "/v1/species/nao-existe")
	if b := decode(t, rec); rec.Code != 404 || b["code"] != "SPECIES_NOT_FOUND" || b["type"] != "urn:passarim:problem:species-not-found" {
		t.Fatalf("404: %d %v", rec.Code, b)
	}
	g = &fakeGet{err: &domain.ValidationError{Fields: []domain.FieldError{{Field: "id", Reason: "identificador inválido"}}}}
	rec = do(newHandler(t, &fakeList{}, g, &fakeFilters{}), "GET", "/v1/species/ABC")
	if b := decode(t, rec); rec.Code != 400 || b["errors"].([]any)[0].(map[string]any)["field"] != "id" {
		t.Fatalf("id inválido: %d %v", rec.Code, b)
	}
}

func TestListFilters_OK(t *testing.T) {
	f := &fakeFilters{f: domain.Filters{
		Biomes: []domain.BiomeCount{{Biome: domain.BiomePampa, SpeciesCount: 3}},
		States: []domain.StateCount{{State: "RS", SpeciesCount: 5}},
	}}
	b := decode(t, do(newHandler(t, &fakeList{}, &fakeGet{}, f), "GET", "/v1/filters"))
	biome := b["biomes"].([]any)[0].(map[string]any)
	state := b["states"].([]any)[0].(map[string]any)
	if biome["code"] != "pampa" || biome["label"] != "Pampa" || biome["speciesCount"].(float64) != 3 || state["code"] != "RS" || state["speciesCount"].(float64) != 5 {
		t.Fatalf("filtros: %v", b)
	}
}

func TestSuccessHeaders(t *testing.T) {
	rec := do(newHandler(t, &fakeList{}, &fakeGet{}, &fakeFilters{}), "GET", "/v1/species")
	if rec.Header().Get("Cache-Control") != "public, max-age=60" || rec.Header().Get("X-Cache") != "miss" {
		t.Fatalf("headers: %v", rec.Header())
	}
}

func TestXCacheComesFromRequestMeta(t *testing.T) {
	m, _ := media.New(base, slog.New(slog.DiscardHandler))
	list := stubList(func(ctx context.Context, _ domain.RawListQuery) (domain.SpeciesPage, error) {
		reqmeta.From(ctx).SetCache("stale") // é o que o decorator de cache faz
		return domain.SpeciesPage{}, nil
	})
	h := (&httpadapter.Handler{List: list, Get: &fakeGet{}, Filters: &fakeFilters{}, Media: m, Log: slog.New(slog.DiscardHandler)}).Routes()
	if rec := do(h, "GET", "/v1/species"); rec.Header().Get("X-Cache") != "stale" {
		t.Fatalf("X-Cache = %q", rec.Header().Get("X-Cache"))
	}
}

type stubList func(context.Context, domain.RawListQuery) (domain.SpeciesPage, error)

func (s stubList) Run(ctx context.Context, r domain.RawListQuery) (domain.SpeciesPage, error) {
	return s(ctx, r)
}

func TestRouteNotFound_IsProblemJSON(t *testing.T) {
	rec := do(newHandler(t, &fakeList{}, &fakeGet{}, &fakeFilters{}), "GET", "/v1/nada")
	if b := decode(t, rec); rec.Code != 404 || b["code"] != "NOT_FOUND" || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("404 de rota: %d %v", rec.Code, b)
	}
}

func TestUpstreamErrors_503(t *testing.T) {
	for _, k := range []domain.Kind{domain.KindUnavailable, domain.KindTimeout, domain.KindUpstream, domain.KindCircuitOpen} {
		g := &fakeGet{err: domain.NewError(k, errors.New("x"))}
		rec := do(newHandler(t, &fakeList{}, g, &fakeFilters{}), "GET", "/v1/species/x")
		if b := decode(t, rec); rec.Code != 503 || b["code"] != "SERVICE_UNAVAILABLE" {
			t.Errorf("%s → %d %v", k, rec.Code, b)
		}
	}
}

func TestInternalAndUnknownErrors_500(t *testing.T) {
	for _, err := range []error{domain.NewError(domain.KindInternal, nil), errors.New("qualquer coisa")} {
		rec := do(newHandler(t, &fakeList{}, &fakeGet{err: err}, &fakeFilters{}), "GET", "/v1/species/x")
		if b := decode(t, rec); rec.Code != 500 || b["code"] != "INTERNAL" {
			t.Errorf("%v → %d %v", err, rec.Code, b)
		}
	}
}

// InvalidArgument vindo do CATALOG (ex.: cursor adulterado) vira 400 apontando o campo
// pelo método chamado, sem repassar a mensagem dele.
func TestCatalogInvalidArgument_MapsToFieldByMethod(t *testing.T) {
	cause := domain.NewError(domain.KindInvalidArgument, errors.New("segredo-interno: cursor corrompido"))
	rec := do(newHandler(t, &fakeList{err: cause}, &fakeGet{}, &fakeFilters{}), "GET", "/v1/species?cursor=abc")
	b := decode(t, rec)
	if rec.Code != 400 || b["errors"].([]any)[0].(map[string]any)["field"] != "cursor" {
		t.Fatalf("lista: %d %v", rec.Code, b)
	}
	rec = do(newHandler(t, &fakeList{}, &fakeGet{err: cause}, &fakeFilters{}), "GET", "/v1/species/x")
	b = decode(t, rec)
	if rec.Code != 400 || b["errors"].([]any)[0].(map[string]any)["field"] != "id" {
		t.Fatalf("detalhe: %d %v", rec.Code, b)
	}
}

// Review Focus #5: nenhum texto interno chega ao cliente.
func TestErrors_NeverLeakInternalText(t *testing.T) {
	secret := errors.New("segredo-interno: postgres://user:senha@10.0.0.5/db")
	for _, k := range []domain.Kind{domain.KindUnavailable, domain.KindUpstream, domain.KindInternal, domain.KindNotFound, domain.KindInvalidArgument, domain.KindCircuitOpen} {
		g := &fakeGet{err: domain.NewError(k, secret)}
		rec := do(newHandler(t, &fakeList{}, g, &fakeFilters{}), "GET", "/v1/species/x")
		if strings.Contains(rec.Body.String(), "segredo-interno") || strings.Contains(rec.Body.String(), "postgres") || strings.Contains(rec.Body.String(), "10.0.0.5") {
			t.Errorf("%s vazou texto interno: %s", k, rec.Body.String())
		}
	}
}

func TestCanceledRequest_NoBodyAndStatus499(t *testing.T) {
	g := &fakeGet{err: domain.NewError(domain.KindCanceled, context.Canceled)}
	rec := do(newHandler(t, &fakeList{}, g, &fakeFilters{}), "GET", "/v1/species/x")
	if rec.Code != 499 || rec.Body.Len() != 0 {
		t.Fatalf("cliente que desistiu: status %d, corpo %q", rec.Code, rec.Body.String())
	}
}
