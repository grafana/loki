package engine

import (
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
	execution        prometheus.Histogram
}

func newMetrics(r prometheus.Registerer) *metrics {
	return &metrics{
		subqueries: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_engine_v2_subqueries",
			Help: "Total number of subqueries executed with the new engine",
		}, []string{status}),
		logicalPlanning: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_v2_logical_planning",
			Help: "Duration of logical planning in seconds",
		}),
		physicalPlanning: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_v2_physical_planning",
			Help: "Duration of physical planning in seconds",
			Buckets: append(
				prometheus.DefBuckets,                    // 0.005s -> 10s
				prometheus.LinearBuckets(15, 5.0, 10)..., // 15s -> 60s
			),
		}),
		execution: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name: "loki_engine_v2_execution",
			Help: "Duration of execution in seconds",
			Buckets: append(
				prometheus.DefBuckets,                    // 0.005s -> 10s
				prometheus.LinearBuckets(15, 5.0, 10)..., // 15s -> 60s
			),
		}),
	}
}
