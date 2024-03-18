package bloomcompactor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

const (
	metricsNamespace = "loki"
	metricsSubsystem = "bloomcompactor"

	statusSuccess = "success"
	statusFailure = "failure"

	tenantLabel = "tenant"
)

type Metrics struct {
	bloomMetrics     *v1.Metrics
	compactorRunning prometheus.Gauge
	chunkSize        prometheus.Histogram // uncompressed size of all chunks summed per series

	compactionsStarted  prometheus.Counter
	compactionCompleted *prometheus.CounterVec
	compactionTime      *prometheus.HistogramVec

	tenantsDiscovered   prometheus.Counter
	tenantsOwned        prometheus.Counter
	tenantsSkipped      prometheus.Counter
	tenantsStarted      prometheus.Counter
	tenantTableRanges   *prometheus.CounterVec
	seriesPerCompaction prometheus.Histogram
	bytesPerCompaction  prometheus.Histogram

	blocksReused prometheus.Counter

	blocksCreated prometheus.Counter
	blocksDeleted prometheus.Counter
	metasCreated  prometheus.Counter
	metasDeleted  prometheus.Counter

	progress      prometheus.Gauge
	timePerTenant *prometheus.CounterVec

	// Retention metrics
	retentionRunning          prometheus.Gauge
	retentionTime             *prometheus.HistogramVec
	retentionDaysPerIteration *prometheus.HistogramVec
}

func NewMetrics(r prometheus.Registerer, bloomMetrics *v1.Metrics) *Metrics {
	m := Metrics{
		bloomMetrics: bloomMetrics,
		compactorRunning: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "running",
			Help:      "Value will be 1 if compactor is currently running on this instance",
		}),
		chunkSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "chunk_series_size",
			Help:      "Uncompressed size of chunks in a series",
			// 256B -> 100GB, 10 buckets
			Buckets: prometheus.ExponentialBucketsRange(256, 100<<30, 10),
		}),

		compactionsStarted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "compactions_started_total",
			Help:      "Total number of compactions started",
		}),
		compactionCompleted: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "compactions_completed_total",
			Help:      "Total number of compactions completed",
		}, []string{"status"}),
		compactionTime: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "compactions_time_seconds",
			Help:      "Time spent during a compaction cycle.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"status"}),

		tenantsDiscovered: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_discovered_total",
			Help:      "Number of tenants discovered during the current compaction run",
		}),
		tenantsOwned: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_owned",
			Help:      "Number of tenants owned by this instance",
		}),
		tenantsSkipped: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_skipped_total",
			Help:      "Number of tenants skipped since they are not owned by this instance",
		}),
		tenantsStarted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_started_total",
			Help:      "Number of tenants started to process during the current compaction run",
		}),
		tenantTableRanges: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenant_table_ranges_completed_total",
			Help:      "Number of tenants table ranges (table, tenant, keyspace) processed during the current compaction run",
		}, []string{"status"}),
		seriesPerCompaction: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "series_per_compaction",
			Help:      "Number of series during compaction (tenant, table, fingerprint-range). Includes series which copied from other blocks and don't need to be indexed",
			// Up to 10M series per tenant, way more than what we expect given our max_global_streams_per_user limits
			Buckets: prometheus.ExponentialBucketsRange(1, 10e6, 10),
		}),
		bytesPerCompaction: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "bytes_per_compaction",
			Help:      "Number of source bytes from chunks added during a compaction cycle (the tenant, table, keyspace tuple).",
			// 1KB -> 100GB, 10 buckets
			Buckets: prometheus.ExponentialBucketsRange(1<<10, 100<<30, 10),
		}),
		blocksReused: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "blocks_reused_total",
			Help:      "Number of overlapping bloom blocks reused when creating new blocks",
		}),
		blocksCreated: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "blocks_created_total",
			Help:      "Number of blocks created",
		}),
		blocksDeleted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "blocks_deleted_total",
			Help:      "Number of blocks deleted",
		}),
		metasCreated: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "metas_created_total",
			Help:      "Number of metas created",
		}),
		metasDeleted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "metas_deleted_total",
			Help:      "Number of metas deleted",
		}),

		progress: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "progress",
			Help:      "Progress of the compaction process as a percentage. 1 means compaction is complete.",
		}),

		// TODO(owen-d): cleanup tenant metrics over time as ring changes
		// TODO(owen-d): histogram for distributions?
		timePerTenant: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenant_compaction_seconds_total",
			Help:      "Time spent processing a tenant.",
		}, []string{tenantLabel}),

		// Retention
		retentionRunning: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "retention_running",
			Help:      "1 if retention is running in this compactor.",
		}),

		retentionDaysPerIteration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "retention_days_processed",
			Help:      "Number of days iterated over during the retention process.",
			// 1day -> 5 years, 10 buckets
			Buckets: prometheus.ExponentialBucketsRange(1, 365*5, 10),
		}, []string{"status"}),

		retentionTime: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "retention_time_seconds",
			Help:      "Time this retention process took to complete.",
			// 1second -> 5 years, 10 buckets
			Buckets: prometheus.DefBuckets,
		}, []string{"status"}),
	}

	return &m
}
