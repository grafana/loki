package aggregation

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type ChunkMetrics struct {
	chunks  *prometheus.GaugeVec
	samples *prometheus.CounterVec
}

func NewChunkMetrics(r prometheus.Registerer, metricsNamespace string) *ChunkMetrics {
	return &ChunkMetrics{
		chunks: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "metric_chunks",
			Help:      "The total number of chunks in memory.",
		}, []string{"service_name"}),
		samples: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "pattern_ingester",
			Name:      "metric_samples",
			Help:      "The total number of samples in memory.",
		}, []string{"service_name"}),
	}
}
