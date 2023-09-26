package worker

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	concurrentWorkers             prometheus.Gauge
	inflightRequests              prometheus.Gauge
	frontendClientRequestDuration *prometheus.HistogramVec
	frontendClientsGauge          prometheus.Gauge
}

func NewMetrics(_ Config, r prometheus.Registerer) *Metrics {
	return &Metrics{
		concurrentWorkers: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_querier_worker_concurrency",
			Help: "Number of concurrent querier workers",
		}),
		inflightRequests: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_querier_worker_inflight_queries",
			Help: "Number of queries being processed by the querier workers",
		}),
		frontendClientRequestDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_querier_query_frontend_request_duration_seconds",
			Help:    "Time spend doing requests to frontend.",
			Buckets: prometheus.ExponentialBuckets(0.001, 4, 6),
		}, []string{"operation", "status_code"}),
		frontendClientsGauge: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_querier_query_frontend_clients",
			Help: "The current number of clients connected to query-frontend.",
		}),
	}
}
