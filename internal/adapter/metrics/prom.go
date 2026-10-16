// Implementação das métricas com o Prometheus.
//
// Os tipos de métrica usados aqui:
//   - Counter: um contador que só sobe (total de requisições, de retries).
//   - Gauge: um valor que sobe e desce (estado do breaker, IPs rastreados).
//   - Histogram: distribui medidas em faixas ("buckets") de duração, para
//     calcular, por exemplo, quanto tempo levam 95% das requisições.
//
// O sufixo "Vec" quer dizer "com rótulos" (labels): uma mesma métrica
// separada por valores, como route="/v1/species" e status="200". Cada
// combinação de rótulos é uma série guardada pelo Prometheus, por isso os
// rótulos nunca levam dado livre do usuário (o que criaria séries sem fim).

package metrics

import (
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prom implementa Metrics (cadeia de resiliência) e middleware.Observer (HTTP) com Prometheus.
// Usa um Registry PRÓPRIO (não o global): os testes não se atrapalham e só sai o que é nosso.
type Prom struct {
	reg      *prometheus.Registry
	cache    *prometheus.CounterVec
	breaker  prometheus.Gauge
	retries  *prometheus.CounterVec
	attempt  *prometheus.HistogramVec
	httpReqs *prometheus.CounterVec
	httpDur  *prometheus.HistogramVec
	limited  prometheus.Counter

	// Gauges "ao vivo": o valor é lido a cada coleta (as funções chegam depois, no main).
	tracked atomic.Pointer[func() float64]
	conn    atomic.Pointer[func() float64]
}

// latencyBuckets são as faixas de duração dos histogramas, em segundos: de
// 5 ms a 5 s.
var latencyBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5}

// NewProm cria e registra todas as métricas. Além das nossas, registra as do
// próprio Go (memória, goroutines) e as do processo (CPU, arquivos abertos).
// MustRegister encerra com pânico se um nome se repetir, um erro de
// programação que o teste pega na hora.
func NewProm() *Prom {
	p := &Prom{reg: prometheus.NewRegistry()}
	p.cache = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "bff_cache_requests_total", Help: "Resultado do cache por requisição (hit, miss, stale, bypass, error)."}, []string{"result"})
	p.breaker = prometheus.NewGauge(prometheus.GaugeOpts{Name: "bff_breaker_state", Help: "Estado do circuit breaker do catalog: 0 fechado, 1 half-open, 2 aberto."})
	p.retries = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "bff_catalog_retries_total", Help: "Retries ao catalog."}, []string{"method"})
	p.attempt = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "bff_catalog_request_duration_seconds", Help: "Duração de cada tentativa ao catalog.", Buckets: latencyBuckets}, []string{"method", "code"})
	p.httpReqs = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "bff_http_requests_total", Help: "Requisições HTTP."}, []string{"route", "method", "status"})
	p.httpDur = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "bff_http_request_duration_seconds", Help: "Duração das requisições HTTP.", Buckets: latencyBuckets}, []string{"route", "method"})
	p.limited = prometheus.NewCounter(prometheus.CounterOpts{Name: "bff_rate_limited_total", Help: "Requisições recusadas pelo rate limit (429)."})
	trackedGauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "bff_rate_limiter_tracked_ips", Help: "IPs rastreados pelo rate limiter."}, loadFn(&p.tracked))
	connGauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "bff_catalog_connection_state", Help: "Estado da conexão gRPC com o catalog (0 idle, 1 connecting, 2 ready, 3 transient failure, 4 shutdown)."}, loadFn(&p.conn))
	p.reg.MustRegister(p.cache, p.breaker, p.retries, p.attempt, p.httpReqs, p.httpDur, p.limited, trackedGauge, connGauge,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return p
}

// loadFn devolve a função que o Prometheus chama a cada coleta: lê a função
// guardada no ponteiro atômico e a executa (0 enquanto não houver nenhuma).
// O ponteiro atômico permite trocar a função sem trava, com segurança, mesmo
// com o Prometheus lendo ao mesmo tempo.
func loadFn(ptr *atomic.Pointer[func() float64]) func() float64 {
	return func() float64 {
		if f := ptr.Load(); f != nil {
			return (*f)()
		}
		return 0
	}
}

// SetTrackedIPs e SetConnState ligam os gauges "ao vivo" às funções que os
// calculam; Handler é o que responde em /metrics.
func (p *Prom) SetTrackedIPs(f func() float64) { p.tracked.Store(&f) }
func (p *Prom) SetConnState(f func() float64)  { p.conn.Store(&f) }
func (p *Prom) Handler() http.Handler          { return promhttp.HandlerFor(p.reg, promhttp.HandlerOpts{}) }

// Os métodos abaixo implementam Metrics e o RateLimited do rate limit: cada
// um incrementa um contador, muda um gauge ou registra uma duração.
func (p *Prom) CacheResult(result string)  { p.cache.WithLabelValues(result).Inc() }
func (p *Prom) BreakerState(state int)     { p.breaker.Set(float64(state)) }
func (p *Prom) CatalogRetry(method string) { p.retries.WithLabelValues(method).Inc() }
func (p *Prom) CatalogAttempt(method, code string, d time.Duration) {
	p.attempt.WithLabelValues(method, code).Observe(d.Seconds())
}
func (p *Prom) RateLimited() { p.limited.Inc() }

// ObserveHTTP: o rótulo "route" é SEMPRE o padrão do roteador (ou "unmatched"):
// cardinalidade baixa e nenhum dado do usuário nas métricas.
func (p *Prom) ObserveHTTP(route, method string, status int, d time.Duration) {
	p.httpReqs.WithLabelValues(route, method, strconv.Itoa(status)).Inc()
	p.httpDur.WithLabelValues(route, method).Observe(d.Seconds())
}
