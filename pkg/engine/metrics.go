package engine

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	status               = "status"
	statusSuccess        = "success"
	statusFailure        = "failure"
	statusNotImplemented = "notimplemented"

	queryTypeLabel = "query_type"
)

// metrics groups the v2 engine's query-grain metrics by query lifecycle phase.
// The core struct holds one sub-struct per phase; each sub-struct owns the
// metrics observed during that phase.
//
// NOTE: Metrics are subject to rapid change!
type metrics struct {
	query     queryMetrics
	planning  planningMetrics
	execution executionMetrics
}

// queryMetrics covers whole-query outcome and duration.
type queryMetrics struct {
	subqueries *prometheus.CounterVec // {status, query_type}
}

// planningMetrics covers logical, physical, and prepare planning.
type planningMetrics struct {
	logical  *prometheus.HistogramVec // {query_type}
	physical *prometheus.HistogramVec // {query_type}
	prepare  *prometheus.HistogramVec // {query_type}
}

// executionMetrics covers query execution and result collection.
type executionMetrics struct {
	duration *prometheus.HistogramVec // {query_type}
}

func newMetrics(r prometheus.Registerer) *metrics {
	return &metrics{
		query: queryMetrics{
			subqueries: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Name: "loki_engine_v2_subqueries_total",
				Help: "Total number of subqueries executed with the new engine",
			}, []string{status, queryTypeLabel}),
		},

		planning: planningMetrics{
			logical: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_logical_planning_duration_seconds",
				Help: "Duration of logical query planning in seconds",
			}, []string{queryTypeLabel}),
			physical: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_physical_planning_duration_seconds",
				Help: "Duration of physical query planning in seconds",
				Buckets: append(
					prometheus.DefBuckets,                    // 0.005s -> 10s
					prometheus.LinearBuckets(15, 5.0, 10)..., // 15s -> 60s
				),
			}, []string{queryTypeLabel}),
			prepare: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_prepare_duration_seconds",
				Help: "Duration of query preparation in seconds",
				Buckets: append(
					prometheus.DefBuckets,                    // 0.005s -> 10s
					prometheus.LinearBuckets(15, 5.0, 10)..., // 15s -> 60s
				),
			}, []string{queryTypeLabel}),
		},

		execution: executionMetrics{
			duration: newNativeHistogramVec(r, prometheus.HistogramOpts{
				Name: "loki_engine_v2_execution_duration_seconds",
				Help: "Duration of query execution in seconds",
				Buckets: append(
					prometheus.DefBuckets,                    // 0.005s -> 10s
					prometheus.LinearBuckets(15, 5.0, 10)..., // 15s -> 60s
				),
			}, []string{queryTypeLabel}),
		},
	}
}

func newNativeHistogramVec(r prometheus.Registerer, opts prometheus.HistogramOpts, labels []string) *prometheus.HistogramVec {
	opts.NativeHistogramBucketFactor = 1.1
	opts.NativeHistogramMaxBucketNumber = 100
	opts.NativeHistogramMinResetDuration = time.Hour

	return promauto.With(r).NewHistogramVec(opts, labels)
}
