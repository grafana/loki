package querytee

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	comparisonMatch    = "match"
	comparisonMismatch = "mismatch"
	comparisonFailed   = "fail"
	comparisonSkipped  = "skipped"

	unknownIssuer = "unknown"
	canaryIssuer  = "loki-canary"
)

type ProxyMetrics struct {
	requestsTotal             *prometheus.CounterVec
	requestDuration           *prometheus.HistogramVec
	responsesTotal            *prometheus.CounterVec
	responsesComparedTotal    *prometheus.CounterVec
	missingMetrics            *prometheus.HistogramVec
	secondaryBackendDecisions *prometheus.CounterVec

	// Sampling metrics
	queriesSampled    *prometheus.CounterVec
	samplingDecisions *prometheus.CounterVec
}

func NewProxyMetrics(registerer prometheus.Registerer) *ProxyMetrics {
	m := &ProxyMetrics{
		requestsTotal: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querytee_requests_total",
			Help: "Total number of HTTP requests received by query-tee.",
		}, []string{"method", "route"}),
		requestDuration: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_querytee_request_duration_seconds",
			Help:                            "Time (in seconds) spent serving HTTP requests.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"backend", "backend_alias", "method", "route", "status_code", "issuer"}),
		responsesTotal: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querytee_responses_total",
			Help: "Total number of responses sent back to the client by the selected backend.",
		}, []string{"backend", "backend_alias", "method", "route", "issuer"}),
		responsesComparedTotal: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querytee_responses_compared_total",
			Help: "Total number of responses compared per route and backend name by result.",
		}, []string{"backend", "backend_alias", "route", "result", "issuer", "tenant"}),
		missingMetrics: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_querytee_missing_metrics_series",
			Help:                            "Number of missing metrics (series) in a vector response.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"backend", "backend_alias", "route", "status_code", "issuer"}),
		secondaryBackendDecisions: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querytee_secondary_backend_decisions_total",
			Help: "Total number of requests by the decision to send them to the secondary backend, or the reason not to.",
		}, []string{"route", "decision"}),

		queriesSampled: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querytee_queries_sampled_total",
			Help: "Total number of queries that were sampled and sent to Kafka.",
		}, []string{"tenant", "route"}),

		samplingDecisions: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_querytee_sampling_decisions_total",
			Help: "Total number of sampling decisions made.",
		}, []string{"tenant", "route", "decision"}),
	}

	return m
}
