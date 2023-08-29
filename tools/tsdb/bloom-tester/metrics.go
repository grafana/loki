package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	tenants    prometheus.Counter
	series     prometheus.Counter // number of series
	seriesKept prometheus.Counter // number of series kept

	chunks     prometheus.Counter   // number of chunks
	chunksKept prometheus.Counter   // number of chunks kept
	chunkSize  prometheus.Histogram // uncompressed size of all chunks summed per series

	inserts    prometheus.Counter // number of inserts attempted into bloom filters
	collisions prometheus.Counter // number of inserts that collided with existing keys

	keysInserted  prometheus.Counter // number of keys inserted into bloom filters
	keyCollisions prometheus.Counter // number of keys that collided with existing keys

	valuesInserted  prometheus.Counter // number of values inserted into bloom filters
	valueCollisions prometheus.Counter // number of values that collided with existing values

	hammingWeightRatio prometheus.Histogram // ratio of the hamming weight of the bloom filter to the number of bits in the bloom filter
	estimatedCount     prometheus.Histogram // estimated number of elements in the bloom filter
	estimatedErrorRate prometheus.Histogram // estimated error rate of the bloom filter
	bloomSize          prometheus.Histogram // size of the bloom filter in bytes
	chunksPerSeries    prometheus.Histogram // number of chunks per series
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		tenants: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_tenants",
			Help: "Number of tenants",
		}),
		series: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_series",
			Help: "Number of series",
		}),
		seriesKept: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_series_kept",
			Help: "Number of series kept",
		}),
		chunks: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_chunks",
			Help: "Number of chunks",
		}),
		chunksKept: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_chunks_kept",
			Help: "Number of chunks kept",
		}),
		chunkSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_chunk_series_size",
			Help:    "Uncompressed size of chunks in a series",
			Buckets: prometheus.ExponentialBucketsRange(1<<10, 1<<30, 10),
		}),
		inserts: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_inserts",
			Help: "Number of inserts attempted into bloom filters",
		}),
		collisions: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_collisions",
			Help: "Number of inserts that collided with existing keys",
		}),
		keysInserted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_keys_inserted",
			Help: "Number of keys inserted into bloom filters",
		}),
		keyCollisions: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_key_collisions",
			Help: "Number of keys that collided with existing keys",
		}),
		valuesInserted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_values_inserted",
			Help: "Number of values inserted into bloom filters",
		}),
		valueCollisions: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_value_collisions",
			Help: "Number of values that collided with existing values",
		}),
		hammingWeightRatio: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_hamming_weight_ratio",
			Help:    "Ratio of the hamming weight of the bloom filter to the number of bits in the bloom filter",
			Buckets: prometheus.ExponentialBucketsRange(0.0001, 1, 10),
		}),
		estimatedCount: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_estimated_count",
			Help:    "Estimated number of elements in the bloom filter",
			Buckets: prometheus.ExponentialBucketsRange(1, 32<<20, 10),
		}),
		estimatedErrorRate: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_estimated_error_rate",
			Help:    "Estimated error rate of the bloom filter",
			Buckets: prometheus.ExponentialBucketsRange(0.0001, 0.5, 10),
		}),
		bloomSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_size",
			Help:    "Size of the bloom filter in bytes",
			Buckets: prometheus.ExponentialBucketsRange(128, 16<<20, 8),
		}),
		chunksPerSeries: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_chunks_per_series",
			Help:    "Number of chunks per series",
			Buckets: prometheus.ExponentialBucketsRange(1, 100e3, 10),
		}),
	}
}
