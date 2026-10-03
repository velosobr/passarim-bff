// Package cache é o decorator MAIS EXTERNO da cadeia: guarda as respostas do
// catalog no Redis e, quando o catalog falha, serve o dado velho
// (stale-while-error) em vez de um erro. Algoritmo e classificação de erros: spec §5.2 e §5.3.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/velosobr/passarim-bff/internal/adapter/metrics"
	"github.com/velosobr/passarim-bff/internal/adapter/reqmeta"
	"github.com/velosobr/passarim-bff/internal/domain"
	"github.com/velosobr/passarim-bff/internal/usecase"
)

type Config struct {
	FreshTTL time.Duration // até quando o dado é "fresco" (decidido em código, não pelo Redis)
	StaleTTL time.Duration // TTL no Redis = janela em que o dado velho ainda pode ser servido
	Budget   time.Duration // orçamento da chamada compartilhada ao catalog (singleflight)
	Now      func() time.Time
}

type cache struct {
	next  usecase.CatalogReader
	store Store
	cfg   Config
	log   *slog.Logger
	m     metrics.Metrics
	sf    singleflight.Group // por processo: cada réplica tem o seu
}

func New(next usecase.CatalogReader, store Store, cfg Config, log *slog.Logger, m metrics.Metrics) usecase.CatalogReader {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &cache{next: next, store: store, cfg: cfg, log: log, m: m}
}

// envelope é o que fica no Redis: o dado + quando foi gravado.
type envelope[D any] struct {
	StoredAt time.Time `json:"storedAt"`
	Data     D         `json:"data"`
}

type flightResult[T any] struct {
	value T
	err   error
}

// cached implementa o algoritmo da spec §5.2 para qualquer tipo T (domínio) / D (formato gravado).
func cached[T, D any](ctx context.Context, c *cache, key string, call func(context.Context) (T, error), enc func(T) D, dec func(D) T) (T, error) {
	meta := reqmeta.From(ctx)
	var zero T

	// 1. Lê o envelope ANTES de chamar o catalog; guarda em memória como candidato a stale.
	var candidate T
	var storedAt time.Time
	haveCandidate := false
	raw, found, err := c.store.Get(ctx, key)
	if err != nil {
		c.m.CacheResult("error")
		c.log.WarnContext(ctx, "Redis indisponível (leitura): seguindo como miss", "request_id", meta.RequestID(), "error", err)
	} else if found {
		var env envelope[D]
		if jerr := json.Unmarshal(raw, &env); jerr != nil {
			c.log.WarnContext(ctx, "envelope do cache ilegível: tratando como miss", "request_id", meta.RequestID(), "error", jerr)
		} else {
			candidate, storedAt, haveCandidate = dec(env.Data), env.StoredAt, true
		}
	}

	// 2. Fresco → devolve direto.
	if haveCandidate && c.cfg.Now().Sub(storedAt) < c.cfg.FreshTTL {
		c.m.CacheResult("hit")
		meta.SetCache("hit")
		return candidate, nil
	}

	// 3. Um único voo por chave. A função compartilhada NÃO usa o contexto do 1º chamador
	// (se ele desistir, os outros não podem sofrer): usa WithoutCancel (mantém request_id
	// e trace) + um orçamento próprio.
	ch := c.sf.DoChan(key, func() (any, error) {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.Budget)
		defer cancel()
		v, err := call(fctx)
		if err != nil {
			return flightResult[T]{err: err}, nil
		}
		if body, merr := json.Marshal(envelope[D]{StoredAt: c.cfg.Now(), Data: enc(v)}); merr == nil {
			if serr := c.store.Set(fctx, key, body, c.cfg.StaleTTL); serr != nil {
				c.m.CacheResult("error")
				c.log.WarnContext(fctx, "Redis indisponível (gravação)", "error", serr)
			}
		}
		return flightResult[T]{value: v}, nil
	})

	// 4. Cada chamador espera pelo resultado OU pela sua própria desistência.
	select {
	case res := <-ch:
		fr := res.Val.(flightResult[T])
		if fr.err == nil { // 5. sucesso
			c.m.CacheResult("miss")
			meta.SetCache("miss")
			return fr.value, nil
		}
		// 6/8. Erro: só os que "liberam stale" e só com candidato em memória.
		if domain.KindOf(fr.err).AllowsStale() && haveCandidate {
			c.log.WarnContext(ctx, "catalog falhou: servindo dado velho", "request_id", meta.RequestID(), "kind", domain.KindOf(fr.err).String(), "error", fr.err)
			c.m.CacheResult("stale")
			meta.SetCache("stale")
			return candidate, nil
		}
		return zero, fr.err
	case <-ctx.Done():
		// 7. Orçamento estourou e há candidato → stale. Cliente desistiu (ou sem candidato) → erro do contexto.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && haveCandidate {
			c.m.CacheResult("stale")
			meta.SetCache("stale")
			return candidate, nil
		}
		return zero, domain.NewError(domain.KindOf(ctx.Err()), ctx.Err())
	}
}

func (c *cache) GetSpecies(ctx context.Context, id string) (domain.Species, error) {
	return cached(ctx, c, speciesKey(id),
		func(cx context.Context) (domain.Species, error) { return c.next.GetSpecies(cx, id) }, speciesTo, speciesFrom)
}

func (c *cache) ListFilters(ctx context.Context) (domain.Filters, error) {
	return cached(ctx, c, filtersKey,
		func(cx context.Context) (domain.Filters, error) { return c.next.ListFilters(cx) }, filtersTo, filtersFrom)
}

func (c *cache) ListSpecies(ctx context.Context, q domain.ListQuery) (domain.SpeciesPage, error) {
	if q.Q != "" {
		// Busca com texto livre: cardinalidade ilimitada → fora do cache (sem Redis, sem
		// singleflight, sem stale). O X-Cache sai "miss".
		c.m.CacheResult("bypass")
		reqmeta.From(ctx).SetCache("miss")
		return c.next.ListSpecies(ctx, q)
	}
	return cached(ctx, c, listKey(q),
		func(cx context.Context) (domain.SpeciesPage, error) { return c.next.ListSpecies(cx, q) }, pageTo, pageFrom)
}
