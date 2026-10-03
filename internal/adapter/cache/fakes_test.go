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

type memStore struct {
	mu             sync.Mutex
	data           map[string][]byte
	ttls           map[string]time.Duration
	gets, sets     int
	getErr, setErr error
}

func newMemStore() *memStore {
	return &memStore{data: map[string][]byte{}, ttls: map[string]time.Duration{}}
}

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
type fakeCatalog struct {
	calls   atomic.Int32
	err     error
	species domain.Species
	gate    chan struct{} // se não for nil, a chamada espera aqui
	entered chan struct{} // avisa que a chamada começou (se não for nil)
}

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

type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) Advance(d time.Duration) { c.now = c.now.Add(d) }

type recMetrics struct {
	mu    sync.Mutex
	cache []string
}

func (m *recMetrics) CacheResult(r string)                       { m.mu.Lock(); m.cache = append(m.cache, r); m.mu.Unlock() }
func (*recMetrics) BreakerState(int)                             {}
func (*recMetrics) CatalogRetry(string)                          {}
func (*recMetrics) CatalogAttempt(string, string, time.Duration) {}

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

func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

var errBoom = errors.New("boom")
