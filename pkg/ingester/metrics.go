package ingester

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type ingesterMetrics struct {
	checkpointDeleteFail       prometheus.Counter
	checkpointDeleteTotal      prometheus.Counter
	checkpointCreationFail     prometheus.Counter
	checkpointCreationTotal    prometheus.Counter
	checkpointDuration         prometheus.Summary
	checkpointLoggedBytesTotal prometheus.Counter

	walDiskFullFailures prometheus.Counter
	walReplayDuration   prometheus.Gauge
	walCorruptionsTotal *prometheus.CounterVec
	walLoggedBytesTotal prometheus.Counter
	walRecordsLogged    prometheus.Counter

	recoveredStreamsTotal prometheus.Counter
	recoveredChunksTotal  prometheus.Counter
	recoveredEntriesTotal prometheus.Counter
}

const (
	walTypeCheckpoint = "checkpoint"
	walTypeSegment    = "segment"
)

func newIngesterMetrics(r prometheus.Registerer) *ingesterMetrics {
	return &ingesterMetrics{
		walDiskFullFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_disk_full_failures_total",
			Help: "Total number of wal write failures due to full disk.",
		}),
		walReplayDuration: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_wal_replay_duration_seconds",
			Help: "Time taken to replay the checkpoint and the WAL.",
		}),
		walCorruptionsTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_ingester_wal_corruptions_total",
			Help: "Total number of WAL corruptions encountered.",
		}, []string{"type"}),
		checkpointDeleteFail: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_checkpoint_deletions_failed_total",
			Help: "Total number of checkpoint deletions that failed.",
		}),
		checkpointDeleteTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_checkpoint_deletions_total",
			Help: "Total number of checkpoint deletions attempted.",
		}),
		checkpointCreationFail: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_checkpoint_creations_failed_total",
			Help: "Total number of checkpoint creations that failed.",
		}),
		checkpointCreationTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_checkpoint_creations_total",
			Help: "Total number of checkpoint creations attempted.",
		}),
		checkpointDuration: promauto.With(r).NewSummary(prometheus.SummaryOpts{
			Name:       "loki_ingester_checkpoint_duration_seconds",
			Help:       "Time taken to create a checkpoint.",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		}),
		walRecordsLogged: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_records_logged_total",
			Help: "Total number of WAL records logged.",
		}),
		checkpointLoggedBytesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_checkpoint_logged_bytes_total",
			Help: "Total number of bytes written to disk for checkpointing.",
		}),
		walLoggedBytesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_logged_bytes_total",
			Help: "Total number of bytes written to disk for WAL records.",
		}),
		recoveredStreamsTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_recovered_streams_total",
			Help: "Total number of streams recovered from the WAL.",
		}),
		recoveredChunksTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_recovered_chunks_total",
			Help: "Total number of chunks recovered from the WAL checkpoints.",
		}),
		recoveredEntriesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_recovered_entries_total",
			Help: "Total number of entries recovered from the WAL.",
		}),
	}
}
