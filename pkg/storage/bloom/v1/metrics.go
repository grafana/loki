package v1

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	sbfCreationTime    prometheus.Counter   // time spent creating sbfs
	bloomSize          prometheus.Histogram // size of the bloom filter in bytes
	hammingWeightRatio prometheus.Histogram // ratio of the hamming weight of the bloom filter to the number of bits in the bloom filter
	estimatedCount     prometheus.Histogram // estimated number of elements in the bloom filter
	chunksIndexed      *prometheus.CounterVec
}

const chunkIndexedTypeIterated = "iterated"
const chunkIndexedTypeCopied = "copied"

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		sbfCreationTime: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_creation_time_total",
			Help: "Time spent creating scalable bloom filters",
		}),
		bloomSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_size",
			Help:    "Size of the bloom filter in bytes",
			Buckets: prometheus.ExponentialBucketsRange(128, 16777216, 8),
		}),
		hammingWeightRatio: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_hamming_weight_ratio",
			Help:    "Ratio of the hamming weight of the bloom filter to the number of bits in the bloom filter",
			Buckets: prometheus.ExponentialBucketsRange(0.001, 1, 12),
		}),
		estimatedCount: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_estimated_count",
			Help:    "Estimated number of elements in the bloom filter",
			Buckets: prometheus.ExponentialBucketsRange(1, 33554432, 10),
		}),
		chunksIndexed: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "bloom_chunks_indexed_total",
			Help: "Number of chunks indexed in bloom filters, partitioned by type. Type can be iterated or copied, where iterated indicates the chunk data was fetched and ngrams for it's contents generated whereas copied indicates the chunk already existed in another source block and was copied to the new block",
		}, []string{"type"}),
	}
}
