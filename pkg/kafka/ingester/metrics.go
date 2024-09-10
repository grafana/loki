package ingester

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type ingesterMetrics struct {
	// Shutdown marker for ingester scale down
	shutdownMarker prometheus.Gauge
}

func newIngesterMetrics(r prometheus.Registerer) *ingesterMetrics {
	return &ingesterMetrics{
		shutdownMarker: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_prepare_shutdown_requested",
			Help: "1 if the ingester has been requested to prepare for shutdown via endpoint or marker file.",
		}),
	}
}
