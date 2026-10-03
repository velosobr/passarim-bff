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

	mu   sync.Mutex
	byIP map[string]*list.Element
	lru  *list.List // frente = mais recente
}

type entry struct {
	ip   string
	lim  *rate.Limiter
	last time.Time
}

func NewRateLimiter(cfg RateLimitConfig, ip IPResolver, now func() time.Time) *RateLimiter {
	return &RateLimiter{cfg: cfg, ip: ip, now: now, byIP: map[string]*list.Element{}, lru: list.New()}
}

// allow consome uma ficha do balde do IP (criando-o se preciso).
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
func (rl *RateLimiter) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
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

func (rl *RateLimiter) Middleware() Middleware {
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
