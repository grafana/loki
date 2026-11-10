package builder

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the logline index builder.
type Metrics struct {
	// Index building metrics
	bytesReceivedTotal *prometheus.CounterVec

	// Decode metrics
	decodeErrorsTotal      prometheus.Counter
	droppedLinesPreMinDate prometheus.Counter

	// Flush metrics
	flushesTotal             *prometheus.CounterVec
	flushErrorsTotal         *prometheus.CounterVec
	flushBackpressuredTotal  prometheus.Counter
	flushBackpressureSeconds prometheus.Counter
	consumptionLagSeconds    *prometheus.GaugeVec

	// Output metrics
	indexFilesWrittenTotal  prometheus.Counter
	indexFileSizeBytes      prometheus.Histogram
	indexFileTermsCount     prometheus.Histogram
	indexFileDocumentsCount prometheus.Histogram

	// Upload metrics
	uploadDuration prometheus.Histogram

	// Loop timing metrics
	consumptionDuration prometheus.Histogram
	flushDuration       prometheus.Histogram

	// Day bucket metrics
	linesPerBucket *prometheus.CounterVec

	// Flush trigger gauges
	runDiskBytes         prometheus.Gauge
	estimatedMemoryBytes prometheus.Gauge

	// Run spill / merge metrics (all off the hot path: spills happen once per
	// buffer fill, the merge once per flush)
	runsSpilledTotal   prometheus.Counter
	runSpillBytesTotal prometheus.Counter
	mergeDuration      prometheus.Histogram
	runsPerMerge       prometheus.Histogram
}

// NewMetrics creates a new Metrics instance.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		decodeErrorsTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_builder_decode_errors_total",
			Help: "Total number of protobuf decode errors (skipped records)",
		}),
		droppedLinesPreMinDate: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_builder_dropped_lines_pre_min_date_total",
			Help: "Total number of log lines dropped because their timestamp is before min_date",
		}),
		flushesTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "logline_index_builder_flushes_total",
			Help: "Total number of flushes",
		}, []string{"reason"}),
		flushErrorsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "logline_index_builder_flush_errors_total",
			Help: "Total number of flush errors",
		}, []string{"step"}),
		flushBackpressuredTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_builder_flush_backpressured_total",
			Help: "Number of flush triggers skipped because a background flush was already in progress.",
		}),
		flushBackpressureSeconds: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_builder_flush_backpressure_seconds_total",
			Help: "Total seconds the poll loop was blocked waiting for a flush to complete.",
		}),
		consumptionLagSeconds: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "logline_index_builder_consumption_lag_seconds",
			Help: "How far behind HEAD this partition is, in seconds. Reports 0 when consumed offset has reached the broker's HighWatermark (i.e. caught up), regardless of how long ago the last record was produced. When still behind HEAD, reports the wall-clock age of the latest consumed record.",
		}, []string{"partition"}),
		bytesReceivedTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "logline_index_builder_bytes_received_total",
			Help: "Total number of bytes received",
		}, []string{"partition"}),
		indexFilesWrittenTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_builder_index_files_written_total",
			Help: "Total number of .lidx files written",
		}),
		indexFileSizeBytes: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "logline_index_builder_index_file_size_bytes",
			Help: "Size of written .lidx files in bytes",
			Buckets: []float64{
				100 * 1024,         // 100 KiB — sparse old-day buckets
				256 * 1024,         // 256 KiB
				512 * 1024,         // 512 KiB
				1 * 1024 * 1024,    // 1 MiB
				5 * 1024 * 1024,    // 5 MiB
				10 * 1024 * 1024,   // 10 MiB
				20 * 1024 * 1024,   // 20 MiB
				40 * 1024 * 1024,   // 40 MiB
				60 * 1024 * 1024,   // 60 MiB
				80 * 1024 * 1024,   // 80 MiB
				100 * 1024 * 1024,  // 100 MiB
				150 * 1024 * 1024,  // 150 MiB
				256 * 1024 * 1024,  // 256 MiB
				512 * 1024 * 1024,  // 512 MiB
				1024 * 1024 * 1024, // 1 GiB
			},
		}),
		indexFileTermsCount: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_builder_index_file_terms_count",
			Help:    "Number of unique terms (n-grams) in written .lidx files",
			Buckets: append([]float64{100, 500}, prometheus.ExponentialBuckets(1000, 10, 7)...), // 100 to 10B
		}),
		indexFileDocumentsCount: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_builder_index_file_documents_count",
			Help:    "Number of document time-buckets in written .lidx files",
			Buckets: []float64{10, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 25000, 50000, 100000},
		}),
		uploadDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_builder_upload_duration_seconds",
			Help:    "Duration of upload operations to object storage in seconds",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2.5, 5, 10, 20, 30, 60},
		}),
		consumptionDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_builder_consumption_duration_seconds",
			Help:    "Duration of each Kafka consumption poll-and-process cycle in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30},
		}),
		flushDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_builder_flush_duration_seconds",
			Help:    "Duration of flush-and-commit operations in seconds",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 180, 300, 600},
		}),
		linesPerBucket: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "logline_index_builder_lines_per_bucket_total",
			Help: "Total number of log lines processed per day bucket",
		}, []string{"date"}),
		runDiskBytes: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_index_builder_run_disk_bytes",
			Help: "Bytes of spilled run files on scratch disk for the active builder. Compared against flush_on_max_bytes (and size scratch PVC capacity at 2-3x that).",
		}),
		estimatedMemoryBytes: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_index_builder_estimated_memory_bytes",
			Help: "Capacity-based resident working set of the active builder in bytes: the ingest sort buffers (~24 B per postings_buffer_pairs pair, ~460 MiB floor at the default), the shard-reorder scratch (1 B per pair, allocated lazily by the first sharded spill), plus referenced-tick bitsets. Feeds the GOMEMLIMIT-fraction full-flush trigger (70% of GOMEMLIMIT when set).",
		}),
		runsSpilledTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_builder_runs_spilled_total",
			Help: "Total number of sorted runs spilled to scratch disk.",
		}),
		runSpillBytesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "logline_index_builder_run_spill_bytes_total",
			Help: "Total bytes written to spilled run files on scratch disk.",
		}),
		mergeDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_builder_merge_duration_seconds",
			Help:    "Duration of the runs-to-.lidx k-way merge inside prepareIndexes in seconds. flush_duration_seconds covers the whole cycle including upload; this isolates the merge.",
			Buckets: []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
		}),
		runsPerMerge: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "logline_index_builder_runs_per_merge",
			Help:    "Number of sorted runs (k-way fan-in) feeding each flush's merge.",
			Buckets: []float64{1, 2, 4, 8, 16, 32, 64, 128},
		}),
	}

	return m
}
