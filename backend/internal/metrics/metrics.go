// Package metrics — метрики Prometheus (дополнение §18): HTTP api,
// состояние очереди и данных из БД, счётчики worker-а.
//
// Реестр свой у каждого процесса, а не глобальный prometheus.DefaultRegisterer:
// тесты создают по серверу на тест, и глобальный реестр отказал бы в
// повторной регистрации.
package metrics

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/gorm"
)

const namespace = "linguastack"

// scrapeTimeout — предел запросов к БД на один опрос.
const scrapeTimeout = 5 * time.Second

// NewRegistry — реестр со стандартными метриками Go и процесса.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return reg
}

// Handler отдаёт метрики только с заголовком «Authorization: Bearer
// <token>». Пустой токен — метрик нет (404): открытые метрики раскрывают
// размер базы и активность пользователей.
func Handler(g prometheus.Gatherer, token string) http.Handler {
	if token == "" {
		return http.NotFoundHandler()
	}
	want := []byte("Bearer " + token)
	inner := promhttp.HandlerFor(g, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "Требуется токен метрик", http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// HTTP — счётчик и длительность запросов api. Метка route — шаблон
// маршрута («/api/books/{id}»), не настоящий путь: иначе ряд на каждый UUID.
type HTTP struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewHTTP регистрирует метрики HTTP в reg.
func NewHTTP(reg prometheus.Registerer) *HTTP {
	h := &HTTP{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "http_requests_total", Help: "Запросы api по методу, шаблону маршрута и статусу.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace, Name: "http_request_duration_seconds", Help: "Длительность запросов api.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
		}, []string{"route"}),
	}
	reg.MustRegister(h.requests, h.duration)
	return h
}

// Observe учитывает запрос; nil — метрики выключены.
func (h *HTTP) Observe(method, route string, status int, d time.Duration) {
	if h == nil {
		return
	}
	h.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	h.duration.WithLabelValues(route).Observe(d.Seconds())
}

// API — метрики процесса api: HTTP и состояние из БД.
type API struct {
	Registry *prometheus.Registry
	HTTP     *HTTP
}

// NewAPI собирает реестр api. Счётчики из БД читаются при каждом опросе.
func NewAPI(db *gorm.DB) *API {
	reg := NewRegistry()
	reg.MustRegister(&stateCollector{db: db})
	return &API{Registry: reg, HTTP: NewHTTP(reg)}
}

// Handler — обработчик /metrics для этого реестра.
func (a *API) Handler(token string) http.Handler { return Handler(a.Registry, token) }

var (
	usersDesc = prometheus.NewDesc(namespace+"_users", "Пользователи.", nil, nil)
	booksDesc = prometheus.NewDesc(namespace+"_books", "Книги.", nil, nil)
	projDesc  = prometheus.NewDesc(namespace+"_projects", "Проекты перевода по статусу.", []string{"status"}, nil)
	tasksDesc = prometheus.NewDesc(namespace+"_tasks", "Задачи перевода по статусу.", []string{"status"}, nil)
	expDesc   = prometheus.NewDesc(namespace+"_exports", "Экспорты по статусу.", []string{"status"}, nil)
	notesDesc = prometheus.NewDesc(namespace+"_notifications", "Уведомления Telegram по статусу.", []string{"status"}, nil)
	costDesc  = prometheus.NewDesc(namespace+"_provider_cost_usd_total", "Стоимость вызовов провайдеров, USD.", nil, nil)
	tokDesc   = prometheus.NewDesc(namespace+"_provider_tokens_total", "Токены провайдеров.", []string{"direction"}, nil)
)

// stateCollector читает счётчики из БД при опросе: они верны для всех
// процессов сразу, а не для одного api.
type stateCollector struct {
	db *gorm.DB
}

func (c *stateCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{usersDesc, booksDesc, projDesc, tasksDesc, expDesc, notesDesc, costDesc, tokDesc} {
		ch <- d
	}
}

func (c *stateCollector) Collect(ch chan<- prometheus.Metric) {
	if c.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), scrapeTimeout)
	defer cancel()
	s, err := TakeSnapshot(ctx, c.db)
	if err != nil {
		slog.Warn("метрики: счётчики из БД недоступны", "error", err)
		ch <- prometheus.NewInvalidMetric(usersDesc, err)
		return
	}
	gauge := func(d *prometheus.Desc, v float64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, labels...)
	}
	gauge(usersDesc, float64(s.Users))
	gauge(booksDesc, float64(s.Books))
	for _, g := range []struct {
		d *prometheus.Desc
		m map[string]int64
	}{{projDesc, s.Projects}, {tasksDesc, s.Tasks}, {expDesc, s.Exports}, {notesDesc, s.Notifications}} {
		for status, n := range g.m {
			gauge(g.d, float64(n), status)
		}
	}
	// Сумма по проектам: удаление книги уменьшает её, и Prometheus видит
	// сброс счётчика — rate() это переносит.
	ch <- prometheus.MustNewConstMetric(costDesc, prometheus.CounterValue, s.CostUSD)
	ch <- prometheus.MustNewConstMetric(tokDesc, prometheus.CounterValue, float64(s.InputTokens), "input")
	ch <- prometheus.MustNewConstMetric(tokDesc, prometheus.CounterValue, float64(s.OutputTokens), "output")
}

// Worker — счётчики worker-а: батчи, сегменты и вызовы провайдера.
type Worker struct {
	batches  *prometheus.CounterVec
	segments *prometheus.CounterVec
	calls    prometheus.Counter
	duration prometheus.Histogram
}

// BatchResult — итог одного батча перевода.
type BatchResult struct {
	Translated, Failed, Calls int
	Err                       bool // батч завершился ошибкой провайдера или сети
	Duration                  time.Duration
}

// NewWorker регистрирует метрики worker-а в reg.
func NewWorker(reg prometheus.Registerer) *Worker {
	w := &Worker{
		batches: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "worker_batches_total", Help: "Батчи перевода по итогу.",
		}, []string{"result"}),
		segments: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace, Name: "worker_segments_total", Help: "Сегменты, обработанные worker-ом.",
		}, []string{"result"}),
		calls: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace, Name: "worker_provider_calls_total", Help: "Вызовы провайдеров.",
		}),
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace, Name: "worker_batch_duration_seconds", Help: "Длительность батча.",
			Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600, 1200},
		}),
	}
	reg.MustRegister(w.batches, w.segments, w.calls, w.duration)
	return w
}

// Batch учитывает батч; nil — метрики выключены.
func (w *Worker) Batch(b BatchResult) {
	if w == nil {
		return
	}
	result := "ok"
	if b.Err {
		result = "error"
	}
	w.batches.WithLabelValues(result).Inc()
	w.segments.WithLabelValues("translated").Add(float64(b.Translated))
	w.segments.WithLabelValues("failed").Add(float64(b.Failed))
	w.calls.Add(float64(b.Calls))
	w.duration.Observe(b.Duration.Seconds())
}
