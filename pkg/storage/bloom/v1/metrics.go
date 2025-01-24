package v1

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type Metrics struct {
	// writes
	bloomsTotal         prometheus.Counter   // number of blooms created
	bloomSize           prometheus.Histogram // size of the bloom filter in bytes
	hammingWeightRatio  prometheus.Histogram // ratio of the hamming weight of the bloom filter to the number of bits in the bloom filter
	estimatedCount      prometheus.Histogram // estimated number of elements in the bloom filter
	chunksIndexed       *prometheus.CounterVec
	chunksPerSeries     prometheus.Histogram
	blockSeriesIterated prometheus.Counter
	tokensTotal         prometheus.Counter
	insertsTotal        *prometheus.CounterVec
	sourceBytesAdded    prometheus.Counter
	blockSize           prometheus.Histogram
	seriesPerBlock      prometheus.Histogram
	chunksPerBlock      prometheus.Histogram
	blockFlushReason    *prometheus.CounterVec

	// reads
	pagesRead    *prometheus.CounterVec
	pagesSkipped *prometheus.CounterVec
	bytesRead    *prometheus.CounterVec
	bytesSkipped *prometheus.CounterVec

	recorderSeries *prometheus.CounterVec
	recorderChunks *prometheus.CounterVec
}

const (
	chunkIndexedTypeIterated = "iterated"
	chunkIndexedTypeCopied   = "copied"

	collisionTypeFalse = "false"
	collisionTypeTrue  = "true"
	collisionTypeCache = "cache"

	blockFlushReasonFull     = "full"
	blockFlushReasonFinished = "finished"

	pageTypeBloom  = "bloom"
	pageTypeSeries = "series"

	skipReasonTooLarge = "too_large"
	skipReasonErr      = "err"
	skipReasonOOB      = "out_of_bounds"

	recorderRequested = "requested"
	recorderFound     = "found"
	recorderSkipped   = "skipped"
	recorderMissed    = "missed"
	recorderEmpty     = "empty"
	recorderFiltered  = "filtered"
)

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		bloomsTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "blooms_created_total",
			Help:      "Number of blooms created",
		}),
		bloomSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "bloom_size",
			Help:      "Size of the bloom filter in bytes",
			Buckets:   prometheus.ExponentialBucketsRange(1<<10, 512<<20, 10),
		}),
		hammingWeightRatio: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "bloom_hamming_weight_ratio",
			Help:      "Ratio of the hamming weight of the bloom filter to the number of bits in the bloom filter",
			Buckets:   prometheus.ExponentialBucketsRange(0.001, 1, 12),
		}),
		estimatedCount: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "bloom_estimated_count",
			Help:      "Estimated number of elements in the bloom filter",
			Buckets:   prometheus.ExponentialBucketsRange(1, 33554432, 10),
		}),
		chunksIndexed: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "bloom_chunks_indexed_total",
			Help:      "Number of chunks indexed in bloom filters, partitioned by type. Type can be iterated or copied, where iterated indicates the chunk data was fetched and ngrams for it's contents generated whereas copied indicates the chunk already existed in another source block and was copied to the new block",
		}, []string{"type"}),
		chunksPerSeries: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "bloom_chunks_per_series",
			Help:      "Number of chunks per series. Can be copied from an existing bloom or iterated",
			Buckets:   prometheus.ExponentialBucketsRange(1, 4096, 10),
		}),
		blockSeriesIterated: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "bloom_block_series_iterated_total",
			Help:      "Number of series iterated in existing blocks while generating new blocks",
		}),
		tokensTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "bloom_tokens_total",
			Help:      "Number of tokens processed",
		}),
		insertsTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "bloom_inserts_total",
			Help:      "Number of inserts into the bloom filter. collision type may be `false` (no collision), `cache` (found in token cache) or true (found in bloom filter). token_type may be either `raw` (the original ngram) or `chunk_prefixed` (the ngram with the chunk prefix)",
		}, []string{"collision"}),
		sourceBytesAdded: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "bloom_source_bytes_added_total",
			Help:      "Number of bytes from chunks added to the bloom filter",
		}),

		blockSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "bloom_block_size",
			Help:      "Size of the bloom block in bytes",
			Buckets:   prometheus.ExponentialBucketsRange(1<<20, 1<<30, 8),
		}),
		seriesPerBlock: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "bloom_series_per_block",
			Help:      "Number of series per block",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 9), // 2 --> 256
		}),
		chunksPerBlock: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "bloom_chunks_per_block",
			Help:      "Number of chunks per block",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 15), // 2 --> 16384
		}),
		blockFlushReason: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "bloom_block_flush_reason_total",
			Help:      "Reason the block was finished. Can be either `full` (the block hit its maximum size) or `finished` (the block was finished due to the end of the series).",
		}, []string{"reason"}),

		pagesRead: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "bloom_pages_read_total",
			Help:      "Number of bloom pages read",
		}, []string{"type"}),
		pagesSkipped: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "bloom_pages_skipped_total",
			Help:      "Number of bloom pages skipped during query iteration",
		}, []string{"type", "reason"}),
		bytesRead: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "bloom_bytes_read_total",
			Help:      "Number of bytes read from bloom pages",
		}, []string{"type"}),
		bytesSkipped: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "bloom_bytes_skipped_total",
			Help:      "Number of bytes skipped during query iteration",
		}, []string{"type", "reason"}),

		recorderSeries: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "bloom_recorder_series_total",
			Help:      "Number of series reported by the bloom query recorder. Type can be requested (total), found (existed in blooms), skipped (due to page too large configurations, etc), missed (not found in blooms)",
		}, []string{"type"}),
		recorderChunks: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "bloom_recorder_chunks_total",
			Help:      "Number of chunks reported by the bloom query recorder. Type can be requested (total), found (existed in blooms), skipped (due to page too large configurations, etc), missed (not found in blooms), filtered (filtered out)",
		}, []string{"type"}),
	}
}
