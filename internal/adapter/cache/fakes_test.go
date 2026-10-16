// Peças falsas dos testes do cache: um Store em memória, um catalog que
// conta chamadas e pode ficar "travado", um relógio controlável e um
// registro de métricas.
package cache_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/velosobr/passarim-bff/internal/domain"
)

// memStore é um Store num map, protegido por trava. Conta leituras e
// gravações e pode fingir que o Redis caiu (getErr, setErr). Guarda o TTL
// de cada chave para o teste conferir.
type memStore struct {
	mu             sync.Mutex
	data           map[string][]byte
	ttls           map[string]time.Duration
	gets, sets     int
	getErr, setErr error
}

// newMemStore cria um memStore vazio. Um map precisa ser criado com make ou
// com um literal antes do uso: escrever num map nil causa pânico.
func newMemStore() *memStore {
	return &memStore{data: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

// Get e Set imitam o RedisStore: com o erro configurado, falham; senão,
// leem e gravam no map.
func (s *memStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	if s.getErr != nil {
		return nil, false, s.getErr
	}
	v, ok := s.data[key]
	return v, ok, nil
}

func (s *memStore) Set(_ context.Context, key string, v []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets++
	if s.setErr != nil {
		return s.setErr
	}
	s.data[key], s.ttls[key] = v, ttl
	return nil
}

// keys lista as chaves gravadas (em ordem qualquer).
func (s *memStore) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.data {
		out = append(out, k)
	}
	return out
}

// fakeCatalog conta chamadas e pode bloquear até o teste liberar (para provar o singleflight).
//
// Os dois canais são o "semáforo" do teste: entered avisa que uma chamada
// começou, e gate a segura até o teste fechar o canal. Fechar um canal
// libera, de uma vez, todo mundo que espera nele.
type fakeCatalog struct {
	calls   atomic.Int32
	err     error
	species domain.Species
	gate    chan struct{} // se não for nil, a chamada espera aqui
	entered chan struct{} // avisa que a chamada começou (se não for nil)
}

// wait é o miolo das três chamadas: conta, avisa (sem bloquear, graças ao
// default do select), espera o gate ou o fim do contexto e devolve o erro
// configurado. select espera o primeiro de vários canais que ficar pronto.
func (f *fakeCatalog) wait(ctx context.Context) error {
	f.calls.Add(1)
	if f.entered != nil {
		select {
		case f.entered <- struct{}{}:
		default:
		}
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return domain.NewError(domain.KindOf(ctx.Err()), ctx.Err())
		}
	}
	return f.err
}

// Os três métodos devolvem dados fixos. Na lista, o nome comum leva o texto
// buscado, para o teste saber qual consulta chegou ao catalog.
func (f *fakeCatalog) ListSpecies(ctx context.Context, q domain.ListQuery) (domain.SpeciesPage, error) {
	if err := f.wait(ctx); err != nil {
		return domain.SpeciesPage{}, err
	}
	return domain.SpeciesPage{Items: []domain.Summary{{ID: "a", CommonName: q.Q + "x"}}, NextCursor: "n"}, nil
}

func (f *fakeCatalog) GetSpecies(ctx context.Context, id string) (domain.Species, error) {
	if err := f.wait(ctx); err != nil {
		return domain.Species{}, err
	}
	s := f.species
	s.ID = id
	return s, nil
}

func (f *fakeCatalog) ListFilters(ctx context.Context) (domain.Filters, error) {
	if err := f.wait(ctx); err != nil {
		return domain.Filters{}, err
	}
	return domain.Filters{Biomes: []domain.BiomeCount{{Biome: domain.BiomePampa, SpeciesCount: 3}}}, nil
}

// clock é um relógio falso: Now devolve a hora guardada e Advance a empurra
// para a frente. Assim o teste "espera" 10 minutos num instante.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) Advance(d time.Duration) { c.now = c.now.Add(d) }

// recMetrics guarda cada resultado de cache registrado ("hit", "miss",
// "stale"...). Os outros métodos da interface de métricas não fazem nada.
type recMetrics struct {
	mu    sync.Mutex
	cache []string
}

func (m *recMetrics) CacheResult(r string)                       { m.mu.Lock(); m.cache = append(m.cache, r); m.mu.Unlock() }
func (*recMetrics) BreakerState(int)                             {}
func (*recMetrics) CatalogRetry(string)                          {}
func (*recMetrics) CatalogAttempt(string, string, time.Duration) {}

// count diz quantas vezes o resultado r foi registrado.
func (m *recMetrics) count(r string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.cache {
		if c == r {
			n++
		}
	}
	return n
}

// discardLog é um logger que joga tudo fora.
func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

// errBoom é um erro qualquer, para os testes que só precisam de "deu erro".
var errBoom = errors.New("boom")
