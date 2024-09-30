package ingester

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

type ingesterMetrics struct {
	checkpointDeleteFail       prometheus.Counter
	checkpointDeleteTotal      prometheus.Counter
	checkpointCreationFail     prometheus.Counter
	checkpointCreationTotal    prometheus.Counter
	checkpointDuration         prometheus.Summary
	checkpointLoggedBytesTotal prometheus.Counter

	walDiskFullFailures     prometheus.Counter
	walReplayActive         prometheus.Gauge
	walReplayDuration       prometheus.Gauge
	walReplaySamplesDropped *prometheus.CounterVec
	walReplayBytesDropped   *prometheus.CounterVec
	walCorruptionsTotal     *prometheus.CounterVec
	walLoggedBytesTotal     prometheus.Counter
	walRecordsLogged        prometheus.Counter

	recoveredStreamsTotal prometheus.Counter
	recoveredChunksTotal  prometheus.Counter
	recoveredEntriesTotal prometheus.Counter
	duplicateEntriesTotal prometheus.Counter
	recoveredBytesTotal   prometheus.Counter
	recoveryBytesInUse    prometheus.Gauge
	recoveryIsFlushing    prometheus.Gauge

	limiterEnabled prometheus.Gauge

	autoForgetUnhealthyIngestersTotal prometheus.Counter

	chunkUtilization              prometheus.Histogram
	memoryChunks                  prometheus.Gauge
	chunkEntries                  prometheus.Histogram
	chunkSize                     prometheus.Histogram
	chunkCompressionRatio         prometheus.Histogram
	chunksPerTenant               *prometheus.CounterVec
	chunkSizePerTenant            *prometheus.CounterVec
	chunkAge                      prometheus.Histogram
	chunkEncodeTime               prometheus.Histogram
	chunksFlushFailures           prometheus.Counter
	chunksFlushedPerReason        *prometheus.CounterVec
	chunkLifespan                 prometheus.Histogram
	chunksEncoded                 *prometheus.CounterVec
	chunkDecodeFailures           *prometheus.CounterVec
	flushedChunksStats            *analytics.Counter
	flushedChunksBytesStats       *analytics.Statistics
	flushedChunksLinesStats       *analytics.Statistics
	flushedChunksAgeStats         *analytics.Statistics
	flushedChunksLifespanStats    *analytics.Statistics
	flushedChunksUtilizationStats *analytics.Statistics

	chunksCreatedTotal prometheus.Counter
	samplesPerChunk    prometheus.Histogram
	blocksPerChunk     prometheus.Histogram
	chunkCreatedStats  *analytics.Counter

	// Shutdown marker for ingester scale down
	shutdownMarker prometheus.Gauge

	flushQueueLength       prometheus.Gauge
	duplicateLogBytesTotal *prometheus.CounterVec
	streamsOwnershipCheck  prometheus.Histogram
}

// setRecoveryBytesInUse bounds the bytes reports to >= 0.
// TODO(owen-d): we can gain some efficiency by having the flusher never update this after recovery ends.
func (m *ingesterMetrics) setRecoveryBytesInUse(v int64) {
	if v < 0 {
		v = 0
	}
	m.recoveryBytesInUse.Set(float64(v))
}

const (
	walTypeCheckpoint = "checkpoint"
	walTypeSegment    = "segment"

	duplicateReason = "duplicate"
)

func newIngesterMetrics(r prometheus.Registerer, metricsNamespace string) *ingesterMetrics {
	return &ingesterMetrics{
		walDiskFullFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_disk_full_failures_total",
			Help: "Total number of wal write failures due to full disk.",
		}),
		walReplayActive: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_wal_replay_active",
			Help: "Whether the WAL is replaying",
		}),
		walReplayDuration: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_wal_replay_duration_seconds",
			Help: "Time taken to replay the checkpoint and the WAL.",
		}),
		walReplaySamplesDropped: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_ingester_wal_discarded_samples_total",
			Help: "WAL segment entries discarded during replay",
		}, []string{validation.ReasonLabel}),
		walReplayBytesDropped: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_ingester_wal_discarded_bytes_total",
			Help: "WAL segment bytes discarded during replay",
		}, []string{validation.ReasonLabel}),
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
		duplicateEntriesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_duplicate_entries_total",
			Help: "Entries discarded during WAL replay due to existing in checkpoints.",
		}),
		recoveredBytesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_wal_recovered_bytes_total",
			Help: "Total number of bytes recovered from the WAL.",
		}),
		recoveryBytesInUse: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_wal_bytes_in_use",
			Help: "Total number of bytes in use by the WAL recovery process.",
		}),
		recoveryIsFlushing: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_wal_replay_flushing",
			Help: "Whether the wal replay is in a flushing phase due to backpressure",
		}),
		limiterEnabled: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingester_limiter_enabled",
			Help: "Whether the ingester's limiter is enabled",
		}),
		autoForgetUnhealthyIngestersTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_autoforget_unhealthy_ingesters_total",
			Help: "Total number of ingesters automatically forgotten",
		}),
		chunkUtilization: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunk_utilization",
			Help:      "Distribution of stored chunk utilization (when stored).",
			Buckets:   prometheus.LinearBuckets(0, 0.2, 6),
		}),
		memoryChunks: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "ingester_memory_chunks",
			Help:      "The total number of chunks in memory.",
		}),
		chunkEntries: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunk_entries",
			Help:      "Distribution of stored lines per chunk (when stored).",
			Buckets:   prometheus.ExponentialBuckets(200, 2, 9), // biggest bucket is 200*2^(9-1) = 51200
		}),
		chunkSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunk_size_bytes",
			Help:      "Distribution of stored chunk sizes (when stored).",
			Buckets:   prometheus.ExponentialBuckets(20000, 2, 10), // biggest bucket is 20000*2^(10-1) = 10,240,000 (~10.2MB)
		}),
		chunkCompressionRatio: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunk_compression_ratio",
			Help:      "Compression ratio of chunks (when stored).",
			Buckets:   prometheus.LinearBuckets(.75, 2, 10),
		}),
		chunksPerTenant: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunks_stored_total",
			Help:      "Total stored chunks per tenant.",
		}, []string{"tenant"}),
		chunkSizePerTenant: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunk_stored_bytes_total",
			Help:      "Total bytes stored in chunks per tenant.",
		}, []string{"tenant"}),
		chunkAge: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunk_age_seconds",
			Help:      "Distribution of chunk ages (when stored).",
			// with default settings chunks should flush between 5 min and 12 hours
			// so buckets at 1min, 5min, 10min, 30min, 1hr, 2hr, 4hr, 10hr, 12hr, 16hr
			Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400, 36000, 43200, 57600},
		}),
		chunkEncodeTime: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunk_encode_time_seconds",
			Help:      "Distribution of chunk encode times.",
			// 10ms to 10s.
			Buckets: prometheus.ExponentialBuckets(0.01, 4, 6),
		}),
		chunksFlushFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunks_flush_failures_total",
			Help:      "Total number of flush failures.",
		}),
		chunksFlushedPerReason: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunks_flushed_total",
			Help:      "Total flushed chunks per reason.",
		}, []string{"reason"}),
		chunkLifespan: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunk_bounds_hours",
			Help:      "Distribution of chunk end-start durations.",
			// 1h -> 8hr
			Buckets: prometheus.LinearBuckets(1, 1, 8),
		}),
		chunksEncoded: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunks_encoded_total",
			Help:      "The total number of chunks encoded in the ingester.",
		}, []string{"user"}),
		chunkDecodeFailures: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunk_decode_failures_total",
			Help:      "The number of freshly encoded chunks that failed to decode.",
		}, []string{"user"}),
		flushedChunksStats:      analytics.NewCounter("ingester_flushed_chunks"),
		flushedChunksBytesStats: analytics.NewStatistics("ingester_flushed_chunks_bytes"),
		flushedChunksLinesStats: analytics.NewStatistics("ingester_flushed_chunks_lines"),
		flushedChunksAgeStats: analytics.NewStatistics(
			"ingester_flushed_chunks_age_seconds",
		),
		flushedChunksLifespanStats: analytics.NewStatistics(
			"ingester_flushed_chunks_lifespan_seconds",
		),
		flushedChunksUtilizationStats: analytics.NewStatistics(
			"ingester_flushed_chunks_utilization",
		),
		chunksCreatedTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingester_chunks_created_total",
			Help:      "The total number of chunks created in the ingester.",
		}),
		samplesPerChunk: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "ingester",
			Name:      "samples_per_chunk",
			Help:      "The number of samples in a chunk.",

			Buckets: prometheus.LinearBuckets(4096, 2048, 6),
		}),
		blocksPerChunk: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "ingester",
			Name:      "blocks_per_chunk",
			Help:      "The number of blocks in a chunk.",

			Buckets: prometheus.ExponentialBuckets(5, 2, 6),
		}),

		chunkCreatedStats: analytics.NewCounter("ingester_chunk_created"),

		shutdownMarker: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Subsystem: "ingester",
			Name:      "shutdown_marker",
			Help:      "1 if prepare shutdown has been called, 0 otherwise",
		}),

		flushQueueLength: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: "ingester",
			Name:      "flush_queue_length",
			Help:      "The total number of series pending in the flush queue.",
		}),

		streamsOwnershipCheck: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "ingester_streams_ownership_check_duration_ms",
			Help:      "Distribution of streams ownership check durations in milliseconds.",
			// 100ms to 5s.
			Buckets: []float64{100, 250, 350, 500, 750, 1000, 1500, 2000, 5000},
		}),

		duplicateLogBytesTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: "ingester",
			Name:      "duplicate_log_bytes_total",
			Help:      "The total number of bytes that were discarded for duplicate log lines.",
		}, []string{"tenant"}),
	}
}
