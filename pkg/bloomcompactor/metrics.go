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
)

type Metrics struct {
	bloomMetrics     *v1.Metrics
	compactorRunning prometheus.Gauge
	chunkSize        prometheus.Histogram // uncompressed size of all chunks summed per series

	compactionsStarted  prometheus.Counter
	compactionCompleted *prometheus.CounterVec
	compactionTime      *prometheus.HistogramVec

	tenantsDiscovered    prometheus.Counter
	tenantsOwned         prometheus.Counter
	tenantsSkipped       prometheus.Counter
	tenantsStarted       prometheus.Counter
	tenantsCompleted     *prometheus.CounterVec
	tenantsCompletedTime *prometheus.HistogramVec
	tenantsSeries        prometheus.Histogram

	blocksCreated prometheus.Counter
	blocksDeleted prometheus.Counter
	metasCreated  prometheus.Counter
	metasDeleted  prometheus.Counter
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
			Buckets:   prometheus.ExponentialBucketsRange(1024, 1073741824, 10),
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
		tenantsCompleted: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_completed_total",
			Help:      "Number of tenants successfully processed during the current compaction run",
		}, []string{"status"}),
		tenantsCompletedTime: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_time_seconds",
			Help:      "Time spent processing tenants.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"status"}),
		tenantsSeries: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_series",
			Help:      "Number of series processed per tenant in the owned fingerprint-range.",
			// Up to 10M series per tenant, way more than what we expect given our max_global_streams_per_user limits
			Buckets: prometheus.ExponentialBucketsRange(1, 10000000, 10),
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
	}

	return &m
}
