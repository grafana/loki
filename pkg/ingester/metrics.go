package ingester

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type ingesterMetrics struct {
	walReplayDuration   prometheus.Gauge
	walCorruptionsTotal prometheus.Counter
}

func newIngesterMetrics(r prometheus.Registerer) *ingesterMetrics {
	return &ingesterMetrics{
		walReplayDuration: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_wal_replay_duration_seconds",
			Help: "Time taken to replay the checkpoint and the WAL.",
		}),
		walCorruptionsTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_corruptions_total",
			Help: "Total number of WAL corruptions encountered.",
		}),
	}
}
