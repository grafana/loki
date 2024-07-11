package ingesterrf1

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

type ingesterMetrics struct {
	limiterEnabled                    prometheus.Gauge
	autoForgetUnhealthyIngestersTotal prometheus.Counter
	chunksFlushFailures               prometheus.Counter
	chunksFlushedPerReason            *prometheus.CounterVec
	flushedChunksStats                *analytics.Counter
	flushQueueLength                  prometheus.Gauge

	// Shutdown marker for ingester scale down
	shutdownMarker prometheus.Gauge
}

func newIngesterMetrics(r prometheus.Registerer, metricsNamespace string) *ingesterMetrics {
	return &ingesterMetrics{
		limiterEnabled: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_rf1_limiter_enabled",
			Help: "Whether the ingester's limiter is enabled",
		}),
		autoForgetUnhealthyIngestersTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_rf1_autoforget_unhealthy_ingesters_total",
			Help: "Total number of ingesters automatically forgotten",
		}),
		chunksFlushFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingester_rf1_chunks_flush_failures_total",
			Help:      "Total number of flush failures.",
		}),
		chunksFlushedPerReason: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingester_rf1_chunks_flushed_total",
			Help:      "Total flushed chunks per reason.",
		}, []string{"reason"}),
		flushedChunksStats: analytics.NewCounter("ingester_rf1_flushed_chunks"),
		flushQueueLength: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: "ingester_rf1",
			Name:      "flush_queue_length",
			Help:      "The total number of series pending in the flush queue.",
		}),
		shutdownMarker: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Subsystem: "ingester_rf1",
			Name:      "shutdown_marker",
			Help:      "1 if prepare shutdown has been called, 0 otherwise",
		}),
	}
}
