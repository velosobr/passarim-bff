// Rate limit por IP: cada cliente pode fazer até RPS requisições por
// segundo, com uma rajada de até Burst. Passou disso, recebe 429.
//
// O algoritmo é o "token bucket" (balde de fichas): cada IP tem um balde que
// enche RPS fichas por segundo, até o máximo de Burst. Cada requisição gasta
// uma ficha; sem ficha, a requisição é recusada. Isso permite uma rajada
// curta (abrir o app carrega várias coisas de uma vez) e segura um uso
// contínuo abusivo. Quem implementa o balde é o golang.org/x/time/rate.

package middleware

import (
	"container/list"
	"context"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	httpadapter "github.com/velosobr/passarim-bff/internal/adapter/http"
)

// RateLimitConfig são os limites, vindos da configuração (RATE_LIMIT_*).
type RateLimitConfig struct {
	RPS       float64       // fichas por segundo, por IP
	Burst     int           // tamanho do balde
	MaxIPs    int           // teto de IPs rastreados (memória limitada)
	IdleAfter time.Duration // IP sem requisições há mais que isto é esquecido pela limpeza
	OnLimited func()        // chamado a cada 429 (métrica); pode ser nil
}

// RateLimiter: token bucket por IP, em memória, por réplica (ADR-0015).
// O mapa é limitado por uma lista LRU: no teto, o IP MAIS OCIOSO é despejado;
// nunca recusamos todo mundo porque o mapa encheu.
type RateLimiter struct {
	cfg RateLimitConfig
	ip  IPResolver
	now func() time.Time

	// mu é uma trava (sync.Mutex): só uma goroutine por vez mexe no mapa e na
	// lista, porque várias requisições chegam ao mesmo tempo.
	mu   sync.Mutex
	byIP map[string]*list.Element
	lru  *list.List // frente = mais recente
}

// entry é o que guardamos por IP: o balde e a última vez que ele foi usado.
type entry struct {
	ip   string
	lim  *rate.Limiter
	last time.Time
}

// NewRateLimiter cria o limitador vazio. ip descobre o IP da requisição e
// now é o relógio (trocado nos testes).
func NewRateLimiter(cfg RateLimitConfig, ip IPResolver, now func() time.Time) *RateLimiter {
	return &RateLimiter{cfg: cfg, ip: ip, now: now, byIP: map[string]*list.Element{}, lru: list.New()}
}

// allow consome uma ficha do balde do IP (criando-o se preciso).
//
// LRU ("least recently used") é a lista em ordem de uso: cada IP usado vai
// para a frente, e o do fundo é o que está parado há mais tempo. O mapa
// byIP aponta direto para o elemento da lista, para achar um IP sem
// percorrer a lista inteira.
func (rl *RateLimiter) allow(ip string) bool {
	now := rl.now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	el, ok := rl.byIP[ip]
	if !ok {
		if rl.lru.Len() >= rl.cfg.MaxIPs {
			if oldest := rl.lru.Back(); oldest != nil {
				delete(rl.byIP, oldest.Value.(*entry).ip)
				rl.lru.Remove(oldest)
			}
		}
		el = rl.lru.PushFront(&entry{ip: ip, lim: rate.NewLimiter(rate.Limit(rl.cfg.RPS), rl.cfg.Burst)})
		rl.byIP[ip] = el
	} else {
		rl.lru.MoveToFront(el)
	}
	e := el.Value.(*entry)
	e.last = now
	return e.lim.AllowN(now, 1)
}

// Cleanup esquece IPs ociosos há mais de IdleAfter.
func (rl *RateLimiter) Cleanup() {
	cutoff := rl.now().Add(-rl.cfg.IdleAfter)
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for el := rl.lru.Back(); el != nil; {
		prev := el.Prev()
		if e := el.Value.(*entry); e.last.Before(cutoff) {
			delete(rl.byIP, e.ip)
			rl.lru.Remove(el)
		}
		el = prev
	}
}

// Run chama Cleanup periodicamente até o contexto acabar (rode numa goroutine).
// Um Ticker manda um sinal no canal t.C a cada intervalo.
func (rl *RateLimiter) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		// select espera o que vier primeiro: o fim do contexto ou o próximo tique.
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rl.Cleanup()
		}
	}
}

// Tracked é o número de IPs rastreados (alimenta a métrica bff_rate_limiter_tracked_ips).
func (rl *RateLimiter) Tracked() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.lru.Len()
}

// Middleware recusa com 429 quem passou do limite. O cabeçalho Retry-After
// diz em quantos segundos tentar de novo: o tempo de uma ficha nova,
// arredondado para cima e no mínimo 1. As rotas de saúde nunca são
// limitadas, para o Docker e o Traefik não marcarem o BFF como doente.
func (rl *RateLimiter) Middleware() Middleware {
	// max é uma função embutida do Go (desde a 1.21) que devolve o maior valor.
	retryAfter := strconv.Itoa(max(1, int(math.Ceil(1/rl.cfg.RPS))))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" { // probes nunca são limitadas
				next.ServeHTTP(w, r)
				return
			}
			if !rl.allow(rl.ip(r)) {
				if rl.cfg.OnLimited != nil {
					rl.cfg.OnLimited()
				}
				w.Header().Set("Retry-After", retryAfter)
				httpadapter.WriteProblem(w, r, http.StatusTooManyRequests, httpadapter.CodeRateLimited, nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
