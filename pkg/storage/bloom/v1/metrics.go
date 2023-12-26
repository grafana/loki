package v1

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	sbfCreationTime    prometheus.Counter   // time spent creating sbfs
	chunkSize          prometheus.Histogram // uncompressed size of all chunks summed per series
	bloomSize          prometheus.Histogram // size of the bloom filter in bytes
	hammingWeightRatio prometheus.Histogram // ratio of the hamming weight of the bloom filter to the number of bits in the bloom filter
	estimatedCount     prometheus.Histogram // estimated number of elements in the bloom filter
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		sbfCreationTime: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_creation_time",
			Help: "Time spent creating scalable bloom filters",
		}),
		chunkSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_chunk_series_size",
			Help:    "Uncompressed size of chunks in a series",
			Buckets: prometheus.ExponentialBucketsRange(1024, 1073741824, 10),
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
	}
}
