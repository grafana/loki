package main

import (
	"github.com/owen-d/BoomFilters/boom"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Experiment struct {
	name          string
	tokenizer     Tokenizer
	bloom         func() *boom.ScalableBloomFilter
	encodeChunkID bool
}

func NewExperiment(name string, tokenizer Tokenizer, encodeChunkID bool, bloom func() *boom.ScalableBloomFilter) Experiment {
	return Experiment{
		name:          name,
		tokenizer:     tokenizer,
		bloom:         bloom,
		encodeChunkID: encodeChunkID,
	}
}

const ExperimentLabel = "experiment"

type Metrics struct {
	tenants    prometheus.Counter
	series     prometheus.Counter // number of series
	seriesKept prometheus.Counter // number of series kept

	chunks          prometheus.Counter   // number of chunks
	chunksKept      prometheus.Counter   // number of chunks kept
	chunksPerSeries prometheus.Histogram // number of chunks per series
	chunkSize       prometheus.Histogram // uncompressed size of all chunks summed per series

	lines      *prometheus.CounterVec // number of lines processed per experiment (should be the same)
	inserts    *prometheus.CounterVec // number of inserts attempted into bloom filters
	collisions *prometheus.CounterVec // number of inserts that collided with existing keys

	hammingWeightRatio *prometheus.HistogramVec // ratio of the hamming weight of the bloom filter to the number of bits in the bloom filter
	estimatedCount     *prometheus.HistogramVec // estimated number of elements in the bloom filter
	estimatedErrorRate *prometheus.HistogramVec // estimated error rate of the bloom filter
	bloomSize          *prometheus.HistogramVec // size of the bloom filter in bytes
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
		chunksPerSeries: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_chunks_per_series",
			Help:    "Number of chunks per series",
			Buckets: prometheus.ExponentialBucketsRange(1, 10000, 12),
		}),
		chunkSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_chunk_series_size",
			Help:    "Uncompressed size of chunks in a series",
			Buckets: prometheus.ExponentialBucketsRange(1<<10, 1<<30, 10),
		}),
		lines: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "bloom_lines",
			Help: "Number of lines processed",
		}, []string{ExperimentLabel}),
		inserts: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "bloom_inserts",
			Help: "Number of inserts attempted into bloom filters",
		}, []string{ExperimentLabel}),
		collisions: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "bloom_collisions",
			Help: "Number of inserts that collided with existing keys",
		}, []string{ExperimentLabel}),
		hammingWeightRatio: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bloom_hamming_weight_ratio",
			Help:    "Ratio of the hamming weight of the bloom filter to the number of bits in the bloom filter",
			Buckets: prometheus.ExponentialBucketsRange(0.001, 1, 12),
		}, []string{ExperimentLabel}),
		estimatedCount: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bloom_estimated_count",
			Help:    "Estimated number of elements in the bloom filter",
			Buckets: prometheus.ExponentialBucketsRange(1, 32<<20, 10),
		}, []string{ExperimentLabel}),
		estimatedErrorRate: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bloom_estimated_error_rate",
			Help:    "Estimated error rate of the bloom filter",
			Buckets: prometheus.ExponentialBucketsRange(0.0001, 0.5, 10),
		}, []string{ExperimentLabel}),
		bloomSize: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bloom_size",
			Help:    "Size of the bloom filter in bytes",
			Buckets: prometheus.ExponentialBucketsRange(128, 16<<20, 8),
		}, []string{ExperimentLabel}),
	}
}
