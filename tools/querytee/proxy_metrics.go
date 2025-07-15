package querytee

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	comparisonSuccess = "success"
	comparisonFailed  = "fail"
	comparisonSkipped = "skipped"

	unknownIssuer = "unknown"
	canaryIssuer  = "loki-canary"
)

type ProxyMetrics struct {
	requestDuration        *prometheus.HistogramVec
	responsesTotal         *prometheus.CounterVec
	responsesComparedTotal *prometheus.CounterVec
	missingMetrics         *prometheus.HistogramVec
}

func NewProxyMetrics(registerer prometheus.Registerer) *ProxyMetrics {
	m := &ProxyMetrics{
		requestDuration: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "cortex_querytee",
			Name:      "request_duration_seconds",
			Help:      "Time (in seconds) spent serving HTTP requests.",
			Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 0.75, 1, 1.5, 2, 3, 4, 5, 10, 25, 50, 100},
		}, []string{"backend", "method", "route", "status_code", "issuer"}),
		responsesTotal: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "cortex_querytee",
			Name:      "responses_total",
			Help:      "Total number of responses sent back to the client by the selected backend.",
		}, []string{"backend", "method", "route", "issuer"}),
		responsesComparedTotal: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "cortex_querytee",
			Name:      "responses_compared_total",
			Help:      "Total number of responses compared per route and backend name by result.",
		}, []string{"backend", "route", "result", "issuer"}),
		missingMetrics: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "cortex_querytee",
			Name:      "missing_metrics_series",
			Help:      "Number of missing metrics (series) in a vector response.",
			Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 0.75, 1, 1.5, 2, 3, 4, 5, 10, 25, 50, 100},
		}, []string{"backend", "route", "status_code", "issuer"}),
	}

	return m
}
