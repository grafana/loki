package builder

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

type builderMetrics struct {
	chunkUtilization              prometheus.Histogram
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

	inflightJobs prometheus.Gauge
}

func newBuilderMetrics(r prometheus.Registerer) *builderMetrics {
	return &builderMetrics{
		chunkUtilization: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunk_utilization",
			Help:      "Distribution of stored chunk utilization (when stored).",
			Buckets:   prometheus.LinearBuckets(0, 0.2, 6),
		}),
		chunkEntries: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunk_entries",
			Help:      "Distribution of stored lines per chunk (when stored).",
			Buckets:   prometheus.ExponentialBuckets(200, 2, 9), // biggest bucket is 200*2^(9-1) = 51200
		}),
		chunkSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunk_size_bytes",
			Help:      "Distribution of stored chunk sizes (when stored).",
			Buckets:   prometheus.ExponentialBuckets(20000, 2, 10), // biggest bucket is 20000*2^(10-1) = 10,240,000 (~10.2MB)
		}),
		chunkCompressionRatio: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunk_compression_ratio",
			Help:      "Compression ratio of chunks (when stored).",
			Buckets:   prometheus.LinearBuckets(.75, 2, 10),
		}),
		chunksPerTenant: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunks_stored_total",
			Help:      "Total stored chunks per tenant.",
		}, []string{"tenant"}),
		chunkSizePerTenant: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunk_stored_bytes_total",
			Help:      "Total bytes stored in chunks per tenant.",
		}, []string{"tenant"}),
		chunkAge: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunk_age_seconds",
			Help:      "Distribution of chunk ages (when stored).",
			// with default settings chunks should flush between 5 min and 12 hours
			// so buckets at 1min, 5min, 10min, 30min, 1hr, 2hr, 4hr, 10hr, 12hr, 16hr
			Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400, 36000, 43200, 57600},
		}),
		chunkEncodeTime: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunk_encode_time_seconds",
			Help:      "Distribution of chunk encode times.",
			// 10ms to 10s.
			Buckets: prometheus.ExponentialBuckets(0.01, 4, 6),
		}),
		chunksFlushFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunks_flush_failures_total",
			Help:      "Total number of flush failures.",
		}),
		chunksFlushedPerReason: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunks_flushed_total",
			Help:      "Total flushed chunks per reason.",
		}, []string{"reason"}),
		chunkLifespan: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunk_bounds_hours",
			Help:      "Distribution of chunk end-start durations.",
			// 1h -> 8hr
			Buckets: prometheus.LinearBuckets(1, 1, 8),
		}),
		chunksEncoded: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunks_encoded_total",
			Help:      "The total number of chunks encoded in the ingester.",
		}, []string{"user"}),
		chunkDecodeFailures: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunk_decode_failures_total",
			Help:      "The number of freshly encoded chunks that failed to decode.",
		}, []string{"user"}),
		flushedChunksStats:      analytics.NewCounter("block_builder_flushed_chunks"),
		flushedChunksBytesStats: analytics.NewStatistics("block_builder_flushed_chunks_bytes"),
		flushedChunksLinesStats: analytics.NewStatistics("block_builder_flushed_chunks_lines"),
		flushedChunksAgeStats: analytics.NewStatistics(
			"block_builder_flushed_chunks_age_seconds",
		),
		flushedChunksLifespanStats: analytics.NewStatistics(
			"block_builder_flushed_chunks_lifespan_seconds",
		),
		flushedChunksUtilizationStats: analytics.NewStatistics(
			"block_builder_flushed_chunks_utilization",
		),
		chunksCreatedTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_chunks_created_total",
			Help:      "The total number of chunks created in the ingester.",
		}),
		samplesPerChunk: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "block_builder",
			Name:      "samples_per_chunk",
			Help:      "The number of samples in a chunk.",

			Buckets: prometheus.LinearBuckets(4096, 2048, 6),
		}),
		blocksPerChunk: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "block_builder",
			Name:      "blocks_per_chunk",
			Help:      "The number of blocks in a chunk.",

			Buckets: prometheus.ExponentialBuckets(5, 2, 6),
		}),

		chunkCreatedStats: analytics.NewCounter("block_builder_chunk_created"),
		inflightJobs: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Name:      "block_builder_inflight_jobs",
			Help:      "The number of jobs currently being processed by the block builder.",
		}),
	}
}
