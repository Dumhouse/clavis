// Package metrics wires Prometheus instrumentation without global state so
// tests can create independent servers.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics owns a private registry and HTTP instruments.
type Metrics struct {
	Registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	jobs     *prometheus.CounterVec
}

// New returns a fresh metrics set.
func New() *Metrics {
	m := &Metrics{
		Registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "clavis",
			Name:      "http_requests_total",
			Help:      "Total HTTP requests by method, route, and status.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "clavis",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency by method and route.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "route"}),
		jobs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "clavis",
			Name:      "jobs_processed_total",
			Help:      "Background jobs processed by kind and result.",
		}, []string{"kind", "result"}),
	}
	m.Registry.MustRegister(m.requests, m.duration, m.jobs)
	return m
}

// Handler serves the registry in Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

// Observe records one HTTP request. route must be the chi route pattern, not
// the raw path, to keep cardinality bounded.
func (m *Metrics) Observe(method, route string, status int, elapsed time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.duration.WithLabelValues(method, route).Observe(elapsed.Seconds())
}

// JobProcessed records a background job result.
func (m *Metrics) JobProcessed(kind, result string) {
	m.jobs.WithLabelValues(kind, result).Inc()
}
