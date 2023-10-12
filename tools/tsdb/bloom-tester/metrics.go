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

type QueryExperiment struct {
	name         string
	searchString string
}

func NewQueryExperiment(name string, searchString string) QueryExperiment {
	return QueryExperiment{name: name,
		searchString: searchString}
}

const ExperimentLabel = "experiment"
const QueryExperimentLabel = "query_experiment"
const LookupResultType = "lookup_result_type"
const FalsePositive = "false_postive"
const FalseNegative = "false_negative"
const TruePositive = "true_positive"
const TrueNegative = "true_negative"

type Metrics struct {
	tenants        prometheus.Counter
	readTenants    prometheus.Counter
	series         prometheus.Counter // number of series
	readSeries     prometheus.Counter // number of series
	seriesKept     prometheus.Counter // number of series kept
	readSeriesKept prometheus.Counter // number of series kept

	chunks          prometheus.Counter   // number of chunks
	readChunks      prometheus.Counter   // number of chunks
	chunksKept      prometheus.Counter   // number of chunks kept
	readChunksKept  prometheus.Counter   // number of chunks kept
	chunksPerSeries prometheus.Histogram // number of chunks per series
	chunkSize       prometheus.Histogram // uncompressed size of all chunks summed per series
	readChunkSize   prometheus.Histogram // uncompressed size of all chunks summed per series

	lines      *prometheus.CounterVec // number of lines processed per experiment (should be the same)
	inserts    *prometheus.CounterVec // number of inserts attempted into bloom filters
	collisions *prometheus.CounterVec // number of inserts that collided with existing keys

	hammingWeightRatio *prometheus.HistogramVec // ratio of the hamming weight of the bloom filter to the number of bits in the bloom filter
	estimatedCount     *prometheus.HistogramVec // estimated number of elements in the bloom filter
	estimatedErrorRate *prometheus.HistogramVec // estimated error rate of the bloom filter
	bloomSize          *prometheus.HistogramVec // size of the bloom filter in bytes
	readBloomSize      *prometheus.HistogramVec // size of the bloom filter in bytes

	totalChunkMatchesPerSeries *prometheus.CounterVec // total number of matches for a given string, iterating over all lines in a chunk
	chunkMatchesPerSeries      *prometheus.CounterVec // number of matches for a given string in a chunk
	sbfMatchesPerSeries        *prometheus.CounterVec // number of matches for a given string, using the bloom filter
	missesPerSeries            *prometheus.CounterVec // number of cases where the bloom filter did not have a match, but the chunks contained the string (should be zero)
	//counterPerSeries           *prometheus.CounterVec // number of matches for a given string
	sbfCount        prometheus.Counter // number of chunks
	experimentCount prometheus.Counter // number of experiments performed

	sbfLookups      *prometheus.CounterVec
	sbfCreationTime *prometheus.CounterVec // time spent creating sbfs
	sbfsCreated     *prometheus.CounterVec // number of sbfs created
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		tenants: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_tenants",
			Help: "Number of tenants",
		}),
		readTenants: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_tenants_read",
			Help: "Number of tenants",
		}),
		series: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_series",
			Help: "Number of series",
		}),
		readSeries: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_series_read",
			Help: "Number of series",
		}),
		seriesKept: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_series_kept",
			Help: "Number of series kept",
		}),
		readSeriesKept: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_series_kept_read",
			Help: "Number of series kept",
		}),
		chunks: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_chunks",
			Help: "Number of chunks",
		}),
		readChunks: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_chunks_read",
			Help: "Number of chunks",
		}),
		chunksKept: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_chunks_kept",
			Help: "Number of chunks kept",
		}),
		readChunksKept: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_chunks_kept_read",
			Help: "Number of chunks kept",
		}),
		sbfCount: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "bloom_files_found",
			Help: "Number of bloom files processed",
		}),
		experimentCount: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "num_experiments",
			Help: "Number of experiments performed",
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
		readChunkSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "bloom_chunk_series_size_read",
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
		readBloomSize: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bloom_size_read",
			Help:    "Size of the bloom filter in bytes",
			Buckets: prometheus.ExponentialBucketsRange(128, 16<<20, 8),
		}, []string{ExperimentLabel}),
		chunkMatchesPerSeries: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "chunk_matches_per_series",
			Help: "Number of chunk matches per series",
		}, []string{ExperimentLabel, QueryExperimentLabel}),
		totalChunkMatchesPerSeries: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "total_chunk_matches_per_series",
			Help: "Number of total chunk matches per series",
		}, []string{ExperimentLabel, QueryExperimentLabel}),
		sbfMatchesPerSeries: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "sbf_matches_per_series",
			Help: "Number of sbf matches per series",
		}, []string{ExperimentLabel, QueryExperimentLabel}),
		missesPerSeries: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "sbf_misses_per_series",
			Help: "Number of sbf misses per series",
		}, []string{ExperimentLabel, QueryExperimentLabel}),
		sbfLookups: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "sbf_lookups",
			Help: "sbf lookup results",
		}, []string{ExperimentLabel, QueryExperimentLabel, LookupResultType}),
		sbfCreationTime: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "bloom_creation_time",
			Help: "Time spent creating sbfs",
		}, []string{ExperimentLabel}),
		sbfsCreated: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "blooms_created",
			Help: "number of sbfs created",
		}, []string{ExperimentLabel}),
	}
}
