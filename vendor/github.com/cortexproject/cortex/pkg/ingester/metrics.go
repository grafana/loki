package ingester

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/cortexproject/cortex/pkg/util"
)

const (
	memSeriesCreatedTotalName = "cortex_ingester_memory_series_created_total"
	memSeriesCreatedTotalHelp = "The total number of series that were created per user."

	memSeriesRemovedTotalName = "cortex_ingester_memory_series_removed_total"
	memSeriesRemovedTotalHelp = "The total number of series that were removed per user."
)

type ingesterMetrics struct {
	flushQueueLength      prometheus.Gauge
	ingestedSamples       prometheus.Counter
	ingestedSamplesFail   prometheus.Counter
	queries               prometheus.Counter
	queriedSamples        prometheus.Histogram
	queriedSeries         prometheus.Histogram
	queriedChunks         prometheus.Histogram
	memSeries             prometheus.Gauge
	memUsers              prometheus.Gauge
	memSeriesCreatedTotal *prometheus.CounterVec
	memSeriesRemovedTotal *prometheus.CounterVec
	createdChunks         prometheus.Counter
	walReplayDuration     prometheus.Gauge
	walCorruptionsTotal   prometheus.Counter

	// Chunks / blocks transfer.
	sentChunks     prometheus.Counter
	receivedChunks prometheus.Counter
	sentFiles      prometheus.Counter
	receivedFiles  prometheus.Counter
	receivedBytes  prometheus.Counter
	sentBytes      prometheus.Counter

	// Chunks flushing.
	chunkUtilization              prometheus.Histogram
	chunkLength                   prometheus.Histogram
	chunkSize                     prometheus.Histogram
	chunksPerUser                 *prometheus.CounterVec
	chunkSizePerUser              *prometheus.CounterVec
	chunkAge                      prometheus.Histogram
	memoryChunks                  prometheus.Gauge
	flushReasons                  *prometheus.CounterVec
	droppedChunks                 prometheus.Counter
	oldestUnflushedChunkTimestamp prometheus.Gauge
}

func newIngesterMetrics(r prometheus.Registerer, createMetricsConflictingWithTSDB bool) *ingesterMetrics {
	m := &ingesterMetrics{
		flushQueueLength: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_ingester_flush_queue_length",
			Help: "The total number of series pending in the flush queue.",
		}),
		ingestedSamples: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_ingested_samples_total",
			Help: "The total number of samples ingested.",
		}),
		ingestedSamplesFail: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_ingested_samples_failures_total",
			Help: "The total number of samples that errored on ingestion.",
		}),
		queries: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_queries_total",
			Help: "The total number of queries the ingester has handled.",
		}),
		queriedSamples: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name: "cortex_ingester_queried_samples",
			Help: "The total number of samples returned from queries.",
			// Could easily return 10m samples per query - 10*(8^(8-1)) = 20.9m.
			Buckets: prometheus.ExponentialBuckets(10, 8, 8),
		}),
		queriedSeries: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name: "cortex_ingester_queried_series",
			Help: "The total number of series returned from queries.",
			// A reasonable upper bound is around 100k - 10*(8^(6-1)) = 327k.
			Buckets: prometheus.ExponentialBuckets(10, 8, 6),
		}),
		queriedChunks: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name: "cortex_ingester_queried_chunks",
			Help: "The total number of chunks returned from queries.",
			// A small number of chunks per series - 10*(8^(7-1)) = 2.6m.
			Buckets: prometheus.ExponentialBuckets(10, 8, 7),
		}),
		memSeries: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_ingester_memory_series",
			Help: "The current number of series in memory.",
		}),
		memUsers: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_ingester_memory_users",
			Help: "The current number of users in memory.",
		}),
		createdChunks: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_chunks_created_total",
			Help: "The total number of chunks the ingester has created.",
		}),
		walReplayDuration: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_ingester_wal_replay_duration_seconds",
			Help: "Time taken to replay the checkpoint and the WAL.",
		}),
		walCorruptionsTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_wal_corruptions_total",
			Help: "Total number of WAL corruptions encountered.",
		}),

		// Chunks / blocks transfer.
		sentChunks: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_sent_chunks",
			Help: "The total number of chunks sent by this ingester whilst leaving.",
		}),
		receivedChunks: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_received_chunks",
			Help: "The total number of chunks received by this ingester whilst joining",
		}),
		sentFiles: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_sent_files",
			Help: "The total number of files sent by this ingester whilst leaving.",
		}),
		receivedFiles: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_received_files",
			Help: "The total number of files received by this ingester whilst joining",
		}),
		receivedBytes: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_received_bytes_total",
			Help: "The total number of bytes received by this ingester whilst joining",
		}),
		sentBytes: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_sent_bytes_total",
			Help: "The total number of bytes sent by this ingester whilst leaving",
		}),

		// Chunks flushing.
		chunkUtilization: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "cortex_ingester_chunk_utilization",
			Help:    "Distribution of stored chunk utilization (when stored).",
			Buckets: prometheus.LinearBuckets(0, 0.2, 6),
		}),
		chunkLength: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "cortex_ingester_chunk_length",
			Help:    "Distribution of stored chunk lengths (when stored).",
			Buckets: prometheus.ExponentialBuckets(5, 2, 11), // biggest bucket is 5*2^(11-1) = 5120
		}),
		chunkSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "cortex_ingester_chunk_size_bytes",
			Help:    "Distribution of stored chunk sizes (when stored).",
			Buckets: prometheus.ExponentialBuckets(500, 2, 5), // biggest bucket is 500*2^(5-1) = 8000
		}),
		chunksPerUser: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "cortex_ingester_chunks_stored_total",
			Help: "Total stored chunks per user.",
		}, []string{"user"}),
		chunkSizePerUser: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "cortex_ingester_chunk_stored_bytes_total",
			Help: "Total bytes stored in chunks per user.",
		}, []string{"user"}),
		chunkAge: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name: "cortex_ingester_chunk_age_seconds",
			Help: "Distribution of chunk ages (when stored).",
			// with default settings chunks should flush between 5 min and 12 hours
			// so buckets at 1min, 5min, 10min, 30min, 1hr, 2hr, 4hr, 10hr, 12hr, 16hr
			Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400, 36000, 43200, 57600},
		}),
		memoryChunks: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_ingester_memory_chunks",
			Help: "The total number of chunks in memory.",
		}),
		flushReasons: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "cortex_ingester_flush_reasons",
			Help: "Total number of series scheduled for flushing, with reasons.",
		}, []string{"reason"}),
		droppedChunks: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_dropped_chunks_total",
			Help: "Total number of chunks dropped from flushing because they have too few samples.",
		}),
		oldestUnflushedChunkTimestamp: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_oldest_unflushed_chunk_timestamp_seconds",
			Help: "Unix timestamp of the oldest unflushed chunk in the memory",
		}),
	}

	if createMetricsConflictingWithTSDB {
		m.memSeriesCreatedTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: memSeriesCreatedTotalName,
			Help: memSeriesCreatedTotalHelp,
		}, []string{"user"})

		m.memSeriesRemovedTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: memSeriesRemovedTotalName,
			Help: memSeriesRemovedTotalHelp,
		}, []string{"user"})
	}

	return m
}

// TSDB metrics collector. Each tenant has its own registry, that TSDB code uses.
type tsdbMetrics struct {
	// We aggregate metrics from individual TSDB registries into
	// a single set of counters, which are exposed as Cortex metrics.
	dirSyncs        *prometheus.Desc // sum(thanos_shipper_dir_syncs_total)
	dirSyncFailures *prometheus.Desc // sum(thanos_shipper_dir_sync_failures_total)
	uploads         *prometheus.Desc // sum(thanos_shipper_uploads_total)
	uploadFailures  *prometheus.Desc // sum(thanos_shipper_upload_failures_total)

	// These two metrics replace metrics in ingesterMetrics, as we count them differently
	memSeriesCreatedTotal *prometheus.Desc
	memSeriesRemovedTotal *prometheus.Desc

	regsMu sync.RWMutex                    // custom mutex for shipper registry, to avoid blocking main user state mutex on collection
	regs   map[string]*prometheus.Registry // One prometheus registry per tenant
}

func newTSDBMetrics(r prometheus.Registerer) *tsdbMetrics {
	m := &tsdbMetrics{
		regs: make(map[string]*prometheus.Registry),

		dirSyncs: prometheus.NewDesc(
			"cortex_ingester_shipper_dir_syncs_total",
			"TSDB: Total number of dir syncs",
			nil, nil),
		dirSyncFailures: prometheus.NewDesc(
			"cortex_ingester_shipper_dir_sync_failures_total",
			"TSDB: Total number of failed dir syncs",
			nil, nil),
		uploads: prometheus.NewDesc(
			"cortex_ingester_shipper_uploads_total",
			"TSDB: Total number of uploaded blocks",
			nil, nil),
		uploadFailures: prometheus.NewDesc(
			"cortex_ingester_shipper_upload_failures_total",
			"TSDB: Total number of block upload failures",
			nil, nil),

		memSeriesCreatedTotal: prometheus.NewDesc(memSeriesCreatedTotalName, memSeriesCreatedTotalHelp, []string{"user"}, nil),
		memSeriesRemovedTotal: prometheus.NewDesc(memSeriesRemovedTotalName, memSeriesRemovedTotalHelp, []string{"user"}, nil),
	}

	if r != nil {
		r.MustRegister(m)
	}
	return m
}

func (sm *tsdbMetrics) Describe(out chan<- *prometheus.Desc) {
	out <- sm.dirSyncs
	out <- sm.dirSyncFailures
	out <- sm.uploads
	out <- sm.uploadFailures
	out <- sm.memSeriesCreatedTotal
	out <- sm.memSeriesRemovedTotal
}

func (sm *tsdbMetrics) Collect(out chan<- prometheus.Metric) {
	data := util.BuildMetricFamiliesPerUserFromUserRegistries(sm.registries())

	// OK, we have it all. Let's build results.
	data.SendSumOfCounters(out, sm.dirSyncs, "thanos_shipper_dir_syncs_total")
	data.SendSumOfCounters(out, sm.dirSyncFailures, "thanos_shipper_dir_sync_failures_total")
	data.SendSumOfCounters(out, sm.uploads, "thanos_shipper_uploads_total")
	data.SendSumOfCounters(out, sm.uploadFailures, "thanos_shipper_upload_failures_total")

	data.SendSumOfCountersPerUser(out, sm.memSeriesCreatedTotal, "prometheus_tsdb_head_series_created_total")
	data.SendSumOfCountersPerUser(out, sm.memSeriesRemovedTotal, "prometheus_tsdb_head_series_removed_total")
}

// make a copy of the map, so that metrics can be gathered while the new registry is being added.
func (sm *tsdbMetrics) registries() map[string]*prometheus.Registry {
	sm.regsMu.RLock()
	defer sm.regsMu.RUnlock()

	regs := make(map[string]*prometheus.Registry, len(sm.regs))
	for u, r := range sm.regs {
		regs[u] = r
	}
	return regs
}

func (sm *tsdbMetrics) setRegistryForUser(userID string, registry *prometheus.Registry) {
	sm.regsMu.Lock()
	sm.regs[userID] = registry
	sm.regsMu.Unlock()
}
