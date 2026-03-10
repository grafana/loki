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
)

// NOTE: Metrics are subject to rapid change!
type metrics struct {
	subqueries       *prometheus.CounterVec
	logicalPlanning  prometheus.Histogram
	physicalPlanning prometheus.Histogram
	workflowPlanning prometheus.Histogram
	execution        prometheus.Histogram
}

func newMetrics(r prometheus.Registerer) *metrics {
	return &metrics{
		subqueries: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_v2_subqueries_total",
			Help: "Total number of subqueries executed with the new engine",
		}, []string{status}),
		logicalPlanning: newNativeHistogram(r, prometheus.HistogramOpts{
			Name: "loki_engine_v2_logical_planning_duration_seconds",
			Help: "Duration of logical query planning in seconds",
		}),
		physicalPlanning: newNativeHistogram(r, prometheus.HistogramOpts{
			Name: "loki_engine_v2_physical_planning_duration_seconds",
			Help: "Duration of physical query planning in seconds",
			Buckets: append(
				prometheus.DefBuckets,                    // 0.005s -> 10s
				prometheus.LinearBuckets(15, 5.0, 10)..., // 15s -> 60s
			),
		}),
		workflowPlanning: newNativeHistogram(r, prometheus.HistogramOpts{
			Name: "loki_engine_v2_workflow_planning_duration_seconds",
			Help: "Duration of workflow query planning in seconds",
			Buckets: append(
				prometheus.DefBuckets,                    // 0.005s -> 10s
				prometheus.LinearBuckets(15, 5.0, 10)..., // 15s -> 60s
			),
		}),
		execution: newNativeHistogram(r, prometheus.HistogramOpts{
			Name: "loki_engine_v2_execution_duration_seconds",
			Help: "Duration of query execution in seconds",
			Buckets: append(
				prometheus.DefBuckets,                    // 0.005s -> 10s
				prometheus.LinearBuckets(15, 5.0, 10)..., // 15s -> 60s
			),
		}),
	}
}

func newNativeHistogram(r prometheus.Registerer, opts prometheus.HistogramOpts) prometheus.Histogram {
	opts.NativeHistogramBucketFactor = 1.1
	opts.NativeHistogramMaxBucketNumber = 100
	opts.NativeHistogramMinResetDuration = time.Hour

	return promauto.With(r).NewHistogram(opts)
}
