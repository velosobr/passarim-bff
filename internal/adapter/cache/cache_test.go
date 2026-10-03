package cache_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/velosobr/passarim-bff/internal/adapter/cache"
	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
	"github.com/velosobr/passarim-bff/internal/domain"
)

type env struct {
	cat   *fakeCatalog
	store *memStore
	clk   *clock
	m     *recMetrics
	c     interface {
		ListSpecies(context.Context, domain.ListQuery) (domain.SpeciesPage, error)
		GetSpecies(context.Context, string) (domain.Species, error)
		ListFilters(context.Context) (domain.Filters, error)
	}
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{cat: &fakeCatalog{}, store: newMemStore(), clk: &clock{now: time.Now()}, m: &recMetrics{}}
	e.c = cache.New(e.cat, e.store, cache.Config{FreshTTL: 10 * time.Minute, StaleTTL: 24 * time.Hour, Budget: 3 * time.Second, Now: e.clk.Now}, discardLog(), e.m)
	return e
}

func ctxWithMeta() (context.Context, *reqmeta.Meta) {
	m := reqmeta.New("req")
	return reqmeta.With(context.Background(), m), m
}

func TestCache_MissThenHitWhileFresh(t *testing.T) {
	e := newEnv(t)
	ctx, meta := ctxWithMeta()
	if _, err := e.c.GetSpecies(ctx, "sabia"); err != nil || meta.Cache() != "miss" {
		t.Fatalf("1ª: err=%v cache=%q", err, meta.Cache())
	}
	ctx2, meta2 := ctxWithMeta()
	s, err := e.c.GetSpecies(ctx2, "sabia")
	if err != nil || s.ID != "sabia" || meta2.Cache() != "hit" || e.cat.calls.Load() != 1 {
		t.Fatalf("2ª deveria ser hit sem chamar o catalog: err=%v cache=%q calls=%d", err, meta2.Cache(), e.cat.calls.Load())
	}
	if e.store.ttls["bff:v1:species:sabia"] != 24*time.Hour {
		t.Fatalf("o TTL no Redis é a janela de stale (24h): %v", e.store.ttls)
	}
	if e.m.count("miss") != 1 || e.m.count("hit") != 1 {
		t.Fatalf("métricas: %v", e.m.cache)
	}
}

func TestCache_RefreshesWhenFreshnessExpires(t *testing.T) {
	e := newEnv(t)
	_, _ = e.c.GetSpecies(context.Background(), "sabia")
	e.clk.Advance(11 * time.Minute) // passou dos 10 min: não é mais "fresco", mas ainda está no Redis
	ctx, meta := ctxWithMeta()
	if _, err := e.c.GetSpecies(ctx, "sabia"); err != nil || meta.Cache() != "miss" || e.cat.calls.Load() != 2 {
		t.Fatalf("deveria buscar de novo: err=%v cache=%q calls=%d", err, meta.Cache(), e.cat.calls.Load())
	}
}

// Review Focus #2: com o catalog doente, o app recebe o dado velho, não um 503.
func TestCache_ServesStaleOnUpstreamAndCircuitOpen(t *testing.T) {
	for _, k := range []domain.Kind{domain.KindUnavailable, domain.KindTimeout, domain.KindUpstream, domain.KindCircuitOpen} {
		e := newEnv(t)
		_, _ = e.c.GetSpecies(context.Background(), "sabia")
		e.clk.Advance(30 * time.Minute)
		e.cat.err = domain.NewError(k, errBoom)
		ctx, meta := ctxWithMeta()
		s, err := e.c.GetSpecies(ctx, "sabia")
		if err != nil || s.ID != "sabia" || meta.Cache() != "stale" || e.m.count("stale") != 1 {
			t.Errorf("%s: deveria servir stale: err=%v cache=%q", k, err, meta.Cache())
		}
		// Sem dado guardado, o erro sai (o handler vira 503).
		if _, err := e.c.GetSpecies(context.Background(), "nunca-vista"); domain.KindOf(err) != k {
			t.Errorf("%s: sem candidato deveria devolver o erro: %v", k, err)
		}
	}
}

func TestCache_NoStaleForNotFoundInvalidCanceledInternal(t *testing.T) {
	for _, k := range []domain.Kind{domain.KindNotFound, domain.KindInvalidArgument, domain.KindCanceled, domain.KindInternal} {
		e := newEnv(t)
		_, _ = e.c.GetSpecies(context.Background(), "sabia")
		e.clk.Advance(30 * time.Minute)
		e.cat.err = domain.NewError(k, errBoom)
		if _, err := e.c.GetSpecies(context.Background(), "sabia"); domain.KindOf(err) != k {
			t.Errorf("%s: não pode virar stale, deveria devolver o erro: %v", k, err)
		}
	}
}

// Review Focus #6: busca com texto livre não cria chaves no Redis.
func TestCache_SearchBypassesCache(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{"sabia", "bem-te-vi", "xyz"} {
		ctx, meta := ctxWithMeta()
		if _, err := e.c.ListSpecies(ctx, domain.ListQuery{Q: q, Limit: 20}); err != nil || meta.Cache() != "miss" {
			t.Fatalf("q=%q: err=%v cache=%q", q, err, meta.Cache())
		}
	}
	if e.store.gets != 0 || e.store.sets != 0 || e.cat.calls.Load() != 3 || e.m.count("bypass") != 3 {
		t.Fatalf("q não vazio deve ficar fora do cache: gets=%d sets=%d calls=%d bypass=%d", e.store.gets, e.store.sets, e.cat.calls.Load(), e.m.count("bypass"))
	}
}

func TestCache_ListWithoutSearchIsCachedAndKeyIsNormalized(t *testing.T) {
	e := newEnv(t)
	q := domain.ListQuery{State: "SP", Biome: domain.BiomeCerrado, Limit: 20}
	_, _ = e.c.ListSpecies(context.Background(), q)
	_, _ = e.c.ListSpecies(context.Background(), q) // mesma consulta normalizada → mesma chave
	if len(e.store.keys()) != 1 || e.cat.calls.Load() != 1 {
		t.Fatalf("mesma consulta = uma chave e uma chamada: keys=%v calls=%d", e.store.keys(), e.cat.calls.Load())
	}
	_, _ = e.c.ListSpecies(context.Background(), domain.ListQuery{State: "SP", Biome: domain.BiomeCerrado, Limit: 10}) // limit diferente
	_, _ = e.c.ListSpecies(context.Background(), domain.ListQuery{State: "RJ", Biome: domain.BiomeCerrado, Limit: 20})
	_, _ = e.c.ListSpecies(context.Background(), domain.ListQuery{State: "SP", Biome: domain.BiomeCerrado, Limit: 20, Cursor: "abc"})
	if len(e.store.keys()) != 4 {
		t.Fatalf("limit, estado e cursor diferentes geram chaves diferentes: %v", e.store.keys())
	}
	for _, k := range e.store.keys() {
		if len(k) < len("bff:v1:list:")+64 || k[:12] != "bff:v1:list:" {
			t.Errorf("chave fora do padrão bff:v1:list:{sha256}: %s", k)
		}
	}
}

func TestCache_FiltersAreCached(t *testing.T) {
	e := newEnv(t)
	_, _ = e.c.ListFilters(context.Background())
	f, err := e.c.ListFilters(context.Background())
	if err != nil || len(f.Biomes) != 1 || e.cat.calls.Load() != 1 || e.store.keys()[0] != "bff:v1:filters" {
		t.Fatalf("filtros: %+v err=%v calls=%d keys=%v", f, err, e.cat.calls.Load(), e.store.keys())
	}
}

func TestCache_InvalidEnvelopeIsMiss(t *testing.T) {
	e := newEnv(t)
	e.store.data["bff:v1:species:sabia"] = []byte("{isto não é um envelope")
	ctx, meta := ctxWithMeta()
	if _, err := e.c.GetSpecies(ctx, "sabia"); err != nil || meta.Cache() != "miss" || e.cat.calls.Load() != 1 {
		t.Fatalf("envelope inválido = miss: err=%v cache=%q calls=%d", err, meta.Cache(), e.cat.calls.Load())
	}
	if string(e.store.data["bff:v1:species:sabia"]) == "{isto não é um envelope" {
		t.Fatal("o envelope ruim deveria ser sobrescrito pelo bom")
	}
}

func TestCache_RedisDownIsMissAndNeverBreaksTheRequest(t *testing.T) {
	e := newEnv(t)
	e.store.getErr, e.store.setErr = errBoom, errBoom
	ctx, meta := ctxWithMeta()
	s, err := e.c.GetSpecies(ctx, "sabia")
	if err != nil || s.ID != "sabia" || meta.Cache() != "miss" {
		t.Fatalf("Redis fora do ar não pode derrubar: err=%v cache=%q", err, meta.Cache())
	}
	if e.m.count("error") == 0 {
		t.Fatalf("a falha do Redis deveria ser contada: %v", e.m.cache)
	}
}

func TestCache_SingleflightCollapsesConcurrentCalls(t *testing.T) {
	e := newEnv(t)
	e.cat.gate = make(chan struct{})
	e.cat.entered = make(chan struct{}, 1)
	var wg sync.WaitGroup
	results := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.c.GetSpecies(context.Background(), "sabia")
			results <- err
		}()
	}
	<-e.cat.entered
	time.Sleep(50 * time.Millisecond) // dá tempo de todos entrarem no mesmo voo
	close(e.cat.gate)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if e.cat.calls.Load() != 1 {
		t.Fatalf("10 chamadores, 1 chamada ao catalog; houve %d", e.cat.calls.Load())
	}
}

// Review Focus #1: o primeiro chamador desiste, o segundo (no mesmo voo) ainda recebe o dado.
func TestCache_FirstCallerCancelsSecondStillGetsData(t *testing.T) {
	e := newEnv(t)
	e.cat.gate = make(chan struct{})
	e.cat.entered = make(chan struct{}, 1)
	ctx1, cancel1 := context.WithCancel(context.Background())
	res1, res2 := make(chan error, 1), make(chan error, 1)
	var got domain.Species
	go func() { _, err := e.c.GetSpecies(ctx1, "sabia"); res1 <- err }()
	<-e.cat.entered // o voo começou com o contexto do 1º chamador
	go func() {
		s, err := e.c.GetSpecies(context.Background(), "sabia")
		got = s
		res2 <- err
	}()
	time.Sleep(50 * time.Millisecond) // o 2º entra no mesmo voo
	cancel1()
	if err := <-res1; domain.KindOf(err) != domain.KindCanceled {
		t.Fatalf("o 1º chamador deveria receber Canceled: %v", err)
	}
	close(e.cat.gate) // o catalog responde
	if err := <-res2; err != nil || got.ID != "sabia" {
		t.Fatalf("o 2º chamador não pode herdar o cancelamento do 1º: err=%v got=%+v", err, got)
	}
	if e.cat.calls.Load() != 1 {
		t.Fatalf("uma só chamada ao catalog: %d", e.cat.calls.Load())
	}
}

func TestCache_ExpiredBudgetWithCandidateServesStale(t *testing.T) {
	e := newEnv(t)
	_, _ = e.c.GetSpecies(context.Background(), "sabia") // guarda um candidato
	e.clk.Advance(30 * time.Minute)
	e.cat.gate = make(chan struct{}) // o catalog "trava"
	defer close(e.cat.gate)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond) // o orçamento da requisição estoura
	defer cancel()
	rctx, meta := reqmeta.With(ctx, reqmeta.New("r")), (*reqmeta.Meta)(nil)
	meta = reqmeta.From(rctx)
	s, err := e.c.GetSpecies(rctx, "sabia")
	if err != nil || s.ID != "sabia" || meta.Cache() != "stale" {
		t.Fatalf("estourou o orçamento com candidato em memória: serve stale. err=%v cache=%q", err, meta.Cache())
	}
}

func TestCache_ClientCancelWithCandidateIsStillAnError(t *testing.T) {
	e := newEnv(t)
	_, _ = e.c.GetSpecies(context.Background(), "sabia")
	e.clk.Advance(30 * time.Minute)
	e.cat.gate = make(chan struct{})
	defer close(e.cat.gate)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	if _, err := e.c.GetSpecies(ctx, "sabia"); domain.KindOf(err) != domain.KindCanceled {
		t.Fatalf("cliente que desistiu recebe o erro do contexto, não stale: %v", err)
	}
}

func TestCache_DetailRoundTripKeepsEveryField(t *testing.T) {
	e := newEnv(t)
	size, diet := 23, "frutos"
	e.cat.species = domain.Species{
		CommonName: "Sabiá", ScientificName: "Turdus rufiventris", Family: "Turdidae", SizeCm: &size, Diet: &diet, Conservation: "LC",
		Description: "d", DescriptionCredit: domain.Credit{Author: "a", License: "l", Source: "s", SourceURL: "u"},
		Facts: []domain.Fact{{Text: "t", Source: "s"}}, Biomes: []domain.Biome{domain.BiomeCerrado}, States: []string{"SP"},
		Photos:   []domain.Photo{{ThumbKey: "t", MediumKey: "m", LargeKey: "l", Width: 1, Height: 2, Credit: domain.Credit{Author: "p"}}},
		Audio:    &domain.Audio{Key: "k", DurationMs: 5, Credit: domain.Credit{Author: "z"}},
		Clusters: []domain.Cluster{{Lat: 1, Lng: 2, Count: 3, Precision: 0.5}},
	}
	first, _ := e.c.GetSpecies(context.Background(), "sabia")
	second, err := e.c.GetSpecies(context.Background(), "sabia") // veio do Redis
	if err != nil {
		t.Fatal(err)
	}
	if e.cat.calls.Load() != 1 || second.CommonName != first.CommonName || *second.SizeCm != 23 || *second.Diet != "frutos" ||
		second.Audio == nil || second.Audio.Key != "k" || len(second.Photos) != 1 || second.Photos[0].LargeKey != "l" ||
		len(second.Clusters) != 1 || second.Clusters[0].Precision != 0.5 || len(second.Biomes) != 1 || second.States[0] != "SP" || second.Facts[0].Text != "t" {
		t.Fatalf("o que sai do cache precisa ser igual ao que entrou: %+v", second)
	}
	_ = errors.New
}
