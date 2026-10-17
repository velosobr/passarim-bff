// Package cache é o decorator MAIS EXTERNO da cadeia: guarda as respostas do
// catalog no Redis e, quando o catalog falha, serve o dado velho
// (stale-while-error) em vez de um erro. Algoritmo e classificação de erros: spec §5.2 e §5.3.
//
// Os conceitos, em resumo (ADR-0016):
//   - Fresco: uma resposta gravada há menos de FreshTTL (10 min por padrão) é
//     devolvida direto, sem chamar o catalog.
//   - Velho (stale): depois disso a resposta continua no Redis até StaleTTL
//     (24 h). Ela não é usada quando o catalog responde, mas salva o app quando
//     o catalog falha: um dado de horas atrás é melhor que uma tela de erro.
//   - Envelope: o Redis guarda o dado junto com o horário em que foi gravado.
//     É assim que o código decide se ele está fresco, sem depender do TTL do
//     próprio Redis.
//   - Singleflight: se cem pedidos iguais chegam juntos com o cache vencido,
//     só um vai ao catalog e os outros esperam a mesma resposta. Isso evita a
//     "manada" (thundering herd) que derrubaria o catalog bem quando ele volta.
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

// Config são os tempos do cache. Now é a função que diz "que horas são";
// os testes trocam por um relógio falso para simular a passagem do tempo sem
// esperar de verdade.
type Config struct {
	FreshTTL time.Duration // até quando o dado é "fresco" (decidido em código, não pelo Redis)
	StaleTTL time.Duration // TTL no Redis = janela em que o dado velho ainda pode ser servido
	Budget   time.Duration // orçamento da chamada compartilhada ao catalog (singleflight)
	Now      func() time.Time
}

// cache é o decorator: embrulha o próximo CatalogReader (next) e guarda as
// respostas no store. Fica não exportado (minúsculo) porque quem está de
// fora só precisa da interface que New devolve.
type cache struct {
	next  usecase.CatalogReader
	store Store
	cfg   Config
	log   *slog.Logger
	m     metrics.Metrics
	sf    singleflight.Group // por processo: cada réplica tem o seu
}

// New monta o decorator. Devolve usecase.CatalogReader, e não *cache, para
// o resto do programa continuar falando só com a interface.
func New(next usecase.CatalogReader, store Store, cfg Config, log *slog.Logger, m metrics.Metrics) usecase.CatalogReader {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &cache{next: next, store: store, cfg: cfg, log: log, m: m}
}

// envelope é o que fica no Redis: o dado + quando foi gravado. "[D any]" é
// um parâmetro de tipo (generics): D é trocado pelo DTO real no uso. As tags
// `json:"..."` dão o nome de cada campo no JSON gravado.
type envelope[D any] struct {
	StoredAt time.Time `json:"storedAt"`
	Data     D         `json:"data"`
}

// flightResult leva o resultado do voo compartilhado (valor ou erro) a todos
// os que estão esperando. O erro vai dentro do resultado, e não como erro do
// singleflight, para cada chamador decidir sozinho se serve o dado velho.
//
// T é o tipo do domínio: uma espécie, uma página ou os filtros.
type flightResult[T any] struct {
	value T
	err   error
}

// cached implementa o algoritmo da spec §5.2 para qualquer tipo T (domínio) / D (formato gravado).
//
// T é o tipo do domínio (o que o BFF usa) e D é o DTO gravado em JSON no
// Redis; enc converte T em D e dec faz o caminho de volta. call é a chamada
// real ao próximo da cadeia. Os passos numerados abaixo seguem a spec.
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
	// select espera o primeiro de vários canais que ficar pronto: aqui, o
	// resultado do voo (ch) ou o fim do contexto deste chamador (ctx.Done()).
	select {
	case res := <-ch:
		// res.Val é any; ".(flightResult[T])" é uma type assertion, que diz ao Go
		// qual é o tipo real guardado ali.
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

// Os três métodos abaixo fazem do cache um CatalogReader: cada um só escolhe
// a chave e as funções de conversão e delega a cached.
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
