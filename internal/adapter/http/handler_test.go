// Testes do handler HTTP com casos de uso falsos: formato das respostas,
// null contra [], cabeçalhos e a tradução de cada tipo de erro em status.

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
	"github.com/velosobr/passarim-bff/internal/usecase"
)

// Fakes dos casos de uso: o handler só conhece as interfaces.
type fakeList struct {
	page domain.SpeciesPage
	err  error
	got  domain.RawListQuery
}

// Cada fake guarda o que recebeu (quando importa) e devolve o que o teste
// configurou.
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
	got domain.RawFiltersQuery
}

func (f *fakeFilters) Run(_ context.Context, raw domain.RawFiltersQuery) (domain.Filters, error) {
	f.got = raw
	return f.f, f.err
}

type fakeFeatured struct {
	out usecase.FeaturedOutput
	err error
}

func (f *fakeFeatured) Run(context.Context) (usecase.FeaturedOutput, error) { return f.out, f.err }

// base é a MEDIA_BASE_URL dos testes.
const base = "http://localhost:8888/buckets/passarim-media"

// newHandler monta só as rotas (sem middlewares) com os fakes.
func newHandler(t *testing.T, l *fakeList, g *fakeGet, f *fakeFilters) http.Handler {
	t.Helper()
	return newHandlerWithFeatured(t, l, g, f, &fakeFeatured{})
}

// newHandlerWithFeatured é o newHandler com a "Ave do dia" escolhida pelo teste.
func newHandlerWithFeatured(t *testing.T, l *fakeList, g *fakeGet, f *fakeFilters, fe *fakeFeatured) http.Handler {
	t.Helper()
	m, err := media.New(base, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	h := &httpadapter.Handler{List: l, Get: g, Filters: f, Featured: fe, Media: m, Log: slog.New(slog.DiscardHandler)}
	return h.Routes()
}

// do faz uma requisição na memória e devolve a resposta gravada. O
// httptest dispensa servidor e porta: chama o handler direto.
func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req = req.WithContext(reqmeta.With(req.Context(), reqmeta.New("req-1")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decode lê o corpo JSON como um mapa genérico, para o teste olhar os campos
// pelo nome exato do contrato.
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, rec.Body.String())
	}
	return m
}

// A lista sai com o Content-Type certo, a URL completa da miniatura, o
// status de conservação com rótulo e null nos campos ausentes. O handler
// repassa os parâmetros crus (quem valida é o caso de uso).
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

// Lista vazia sai como [], e não como null.
func TestListSpecies_EmptyListIsArrayNotNull(t *testing.T) {
	rec := do(newHandler(t, &fakeList{}, &fakeGet{}, &fakeFilters{}), "GET", "/v1/species")
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("lista vazia deve sair como []: %s", rec.Body.String())
	}
}

// O cursor da próxima página chega ao JSON.
func TestListSpecies_NextCursor(t *testing.T) {
	l := &fakeList{page: domain.SpeciesPage{NextCursor: "abc"}}
	if body := decode(t, do(newHandler(t, l, &fakeGet{}, &fakeFilters{}), "GET", "/v1/species")); body["nextCursor"] != "abc" {
		t.Fatalf("nextCursor: %v", body["nextCursor"])
	}
}

// Um parâmetro repetido é informado ao caso de uso, que o recusa.
func TestListSpecies_RepeatedParamIsInvalid(t *testing.T) {
	l := &fakeList{}
	rec := do(newHandler(t, l, &fakeGet{}, &fakeFilters{}), "GET", "/v1/species?state=SP&state=RJ")
	if len(l.got.Repeated) != 1 || l.got.Repeated[0] != "state" {
		t.Fatalf("o handler deve informar o parâmetro repetido: %+v", l.got)
	}
	_ = rec
}

// Erro de validação vira 400 em problem+json, sem cache, com type, código,
// requestId e a lista de campos inválidos.
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

// A espécie completa sai com todas as seções do contrato.
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

// Numa espécie quase vazia, os campos opcionais estão presentes e null, e
// as listas estão presentes e vazias. O app pode confiar nesse formato.
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

// Espécie inexistente vira 404 SPECIES_NOT_FOUND; id inválido vira 400
// apontando o campo id.
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

// Os filtros saem com código, rótulo e contagem.
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

// Uma resposta de sucesso pode ficar 60 s em cache e diz de onde veio
// (X-Cache: miss quando ninguém registrou nada).
func TestSuccessHeaders(t *testing.T) {
	rec := do(newHandler(t, &fakeList{}, &fakeGet{}, &fakeFilters{}), "GET", "/v1/species")
	if rec.Header().Get("Cache-Control") != "public, max-age=60" || rec.Header().Get("X-Cache") != "miss" {
		t.Fatalf("headers: %v", rec.Header())
	}
}

// O X-Cache repete o que o decorator de cache gravou no registro da
// requisição.
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

// stubList é uma função que se passa pelo caso de uso: um tipo função com
// o método Run, que só chama a própria função.
type stubList func(context.Context, domain.RawListQuery) (domain.SpeciesPage, error)

func (s stubList) Run(ctx context.Context, r domain.RawListQuery) (domain.SpeciesPage, error) {
	return s(ctx, r)
}

// Uma rota inexistente responde 404 em problem+json, como as outras.
func TestRouteNotFound_IsProblemJSON(t *testing.T) {
	rec := do(newHandler(t, &fakeList{}, &fakeGet{}, &fakeFilters{}), "GET", "/v1/nada")
	if b := decode(t, rec); rec.Code != 404 || b["code"] != "NOT_FOUND" || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("404 de rota: %d %v", rec.Code, b)
	}
}

// As quatro falhas do catalog e o Unimplemented (catalog numa versão sem o
// método) viram 503 SERVICE_UNAVAILABLE.
func TestUpstreamErrors_503(t *testing.T) {
	for _, k := range []domain.Kind{domain.KindUnavailable, domain.KindTimeout, domain.KindUpstream, domain.KindCircuitOpen, domain.KindUnimplemented} {
		g := &fakeGet{err: domain.NewError(k, errors.New("x"))}
		rec := do(newHandler(t, &fakeList{}, g, &fakeFilters{}), "GET", "/v1/species/x")
		if b := decode(t, rec); rec.Code != 503 || b["code"] != "SERVICE_UNAVAILABLE" {
			t.Errorf("%s → %d %v", k, rec.Code, b)
		}
	}
}

// Um bug ou um erro sem classificação vira 500 INTERNAL.
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

// Nenhum texto interno chega ao cliente: com uma causa que contém uma URL
// de banco com senha, nenhuma resposta, de nenhum tipo de erro, repete
// qualquer pedaço dela.
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

// Cliente que desistiu: status 499 e nenhum corpo.
func TestCanceledRequest_NoBodyAndStatus499(t *testing.T) {
	g := &fakeGet{err: domain.NewError(domain.KindCanceled, context.Canceled)}
	rec := do(newHandler(t, &fakeList{}, g, &fakeFilters{}), "GET", "/v1/species/x")
	if rec.Code != 499 || rec.Body.Len() != 0 {
		t.Fatalf("cliente que desistiu: status %d, corpo %q", rec.Code, rec.Body.String())
	}
}

// audience e primaryBiome chegam ao caso de uso como vieram; um parâmetro
// repetido é marcado.
func TestListSpecies_AudienceAndPrimaryBiome(t *testing.T) {
	l := &fakeList{}
	do(newHandler(t, l, &fakeGet{}, &fakeFilters{}), "GET", "/v1/species?audience=kids&primaryBiome=pampa&audience=adult")
	if l.got.Audience != "kids" || l.got.PrimaryBiome != "pampa" || len(l.got.Repeated) != 1 || l.got.Repeated[0] != "audience" {
		t.Fatalf("consulta: %+v", l.got)
	}
}

// Os filtros recebem a audiência e devolvem primaryBiomes no formato de biomes.
func TestListFilters_AudienceAndPrimaryBiomes(t *testing.T) {
	f := &fakeFilters{f: domain.Filters{PrimaryBiomes: []domain.BiomeCount{{Biome: domain.BiomeCerrado, SpeciesCount: 4}}}}
	b := decode(t, do(newHandler(t, &fakeList{}, &fakeGet{}, f), "GET", "/v1/filters?audience=kids"))
	pb := b["primaryBiomes"].([]any)[0].(map[string]any)
	if f.got.Audience != "kids" || pb["code"] != "cerrado" || pb["label"] != "Cerrado" || pb["speciesCount"].(float64) != 4 {
		t.Fatalf("consulta=%+v filtros=%v", f.got, b)
	}
}

// O detalhe traz kids (fatos só com o texto), primaryBiome e os picos.
// Sem Kids e sem bioma principal, os dois saem null; um canto sem picos sai
// com peaks [] (nunca null).
func TestGetSpecies_KidsPrimaryBiomeAndPeaks(t *testing.T) {
	g := &fakeGet{sp: domain.Species{ID: "t", PrimaryBiome: domain.BiomeCerrado,
		Kids:  &domain.KidsContent{Description: "D", Facts: []string{"F"}, DidYouKnow: "V"},
		Audio: &domain.Audio{Key: "species/t/a.aac", Peaks: []float32{0, 1}}}}
	b := decode(t, do(newHandler(t, &fakeList{}, g, &fakeFilters{}), "GET", "/v1/species/t"))
	kids := b["kids"].(map[string]any)
	if b["primaryBiome"] != "cerrado" || kids["description"] != "D" || kids["didYouKnow"] != "V" ||
		kids["facts"].([]any)[0].(map[string]any)["text"] != "F" || len(b["audio"].(map[string]any)["peaks"].([]any)) != 2 {
		t.Fatalf("detalhe: %v", b)
	}
	g = &fakeGet{sp: domain.Species{ID: "t", Audio: &domain.Audio{Key: "species/t/a.aac"}}}
	b = decode(t, do(newHandler(t, &fakeList{}, g, &fakeFilters{}), "GET", "/v1/species/t"))
	if b["kids"] != nil || b["primaryBiome"] != nil || b["audio"].(map[string]any)["peaks"] == nil {
		t.Fatalf("sem Kids: %v", b)
	}
}

// A "Ave do dia" sai com a data, o card, o canto com os picos e o max-age
// até a meia-noite de São Paulo.
func TestFeatured_OK(t *testing.T) {
	fe := &fakeFeatured{out: usecase.FeaturedOutput{Date: "2026-11-05", MaxAge: 7200, Featured: domain.Featured{
		Species: domain.Summary{ID: "t", CommonName: "Sabiá", PrimaryBiome: domain.BiomeMataAtlantica, ThumbnailKey: "species/t/thumb.webp"},
		Audio:   &domain.Audio{Key: "species/t/a.aac", DurationMs: 3000, Peaks: []float32{1}}}}}
	rec := do(newHandlerWithFeatured(t, &fakeList{}, &fakeGet{}, &fakeFilters{}, fe), "GET", "/v1/featured")
	b := decode(t, rec)
	sp := b["species"].(map[string]any)
	if rec.Code != 200 || b["date"] != "2026-11-05" || sp["id"] != "t" || sp["primaryBiome"] != "mata_atlantica" ||
		b["audio"].(map[string]any)["peaks"].([]any)[0].(float64) != 1 || rec.Header().Get("Cache-Control") != "public, max-age=7200" {
		t.Fatalf("%d %v %v", rec.Code, b, rec.Header())
	}
	fe.out.Featured.Audio = nil
	if b := decode(t, do(newHandlerWithFeatured(t, &fakeList{}, &fakeGet{}, &fakeFilters{}, fe), "GET", "/v1/featured")); b["audio"] != nil {
		t.Fatalf("sem canto, audio é null: %v", b)
	}
}

// Os erros da "Ave do dia" (spec da frente B, seção 7): sem ave elegível,
// 404; data recusada pelo catalog, 500; catalog numa versão antiga ou fora
// do ar, 503.
func TestFeatured_Errors(t *testing.T) {
	cases := map[domain.Kind]struct {
		status int
		code   string
	}{
		domain.KindNotFound:        {404, "FEATURED_NOT_FOUND"},
		domain.KindInvalidArgument: {500, "INTERNAL"},
		domain.KindUnimplemented:   {503, "SERVICE_UNAVAILABLE"},
		domain.KindUnavailable:     {503, "SERVICE_UNAVAILABLE"},
	}
	for kind, want := range cases {
		fe := &fakeFeatured{err: domain.NewError(kind, errors.New("x"))}
		rec := do(newHandlerWithFeatured(t, &fakeList{}, &fakeGet{}, &fakeFilters{}, fe), "GET", "/v1/featured")
		if b := decode(t, rec); rec.Code != want.status || b["code"] != want.code {
			t.Errorf("%s → %d %v, esperava %d %s", kind, rec.Code, b, want.status, want.code)
		}
	}
}
