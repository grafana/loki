package ingesterkafka

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type ingesterMetrics struct {
	// Shutdown marker for ingester scale down.
	shutdownMarker prometheus.Gauge
}

func newIngesterMetrics(r prometheus.Registerer) *ingesterMetrics {
	return &ingesterMetrics{
		shutdownMarker: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_kafka_shutdown_marker",
			Help: "1 if prepare shutdown has been called, 0 otherwise.",
		}),
	}
}
