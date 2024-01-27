package bloomcompactor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	metricsNamespace = "loki"
	metricsSubsystem = "bloomcompactor"

	statusSuccess = "success"
	statusFailure = "failure"
)

type metrics struct {
	compactionRunsStarted          prometheus.Counter
	compactionRunsCompleted        *prometheus.CounterVec
	compactionRunTime              *prometheus.HistogramVec
	compactionRunDiscoveredTenants prometheus.Counter
	compactionRunSkippedTenants    prometheus.Counter
	compactionRunTenantsCompleted  *prometheus.CounterVec
	compactionRunTenantsTime       *prometheus.HistogramVec
	compactionRunJobStarted        prometheus.Counter
	compactionRunJobCompleted      *prometheus.CounterVec
	compactionRunJobTime           *prometheus.HistogramVec
	compactionRunInterval          prometheus.Gauge
	compactorRunning               prometheus.Gauge
}

func newMetrics(r prometheus.Registerer) *metrics {
	m := metrics{
		compactionRunsStarted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "runs_started_total",
			Help:      "Total number of compactions started",
		}),
		compactionRunsCompleted: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "runs_completed_total",
			Help:      "Total number of compactions completed successfully",
		}, []string{"status"}),
		compactionRunTime: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "runs_time_seconds",
			Help:      "Time spent during a compaction cycle.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"status"}),
		compactionRunDiscoveredTenants: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_discovered",
			Help:      "Number of tenants discovered during the current compaction run",
		}),
		compactionRunSkippedTenants: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_skipped",
			Help:      "Number of tenants skipped during the current compaction run",
		}),
		compactionRunTenantsCompleted: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_completed",
			Help:      "Number of tenants successfully processed during the current compaction run",
		}, []string{"status"}),
		compactionRunTenantsTime: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_time_seconds",
			Help:      "Time spent processing tenants.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"status"}),
		compactionRunJobStarted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "job_started",
			Help:      "Number of jobs started processing during the current compaction run",
		}),
		compactionRunJobCompleted: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "job_completed",
			Help:      "Number of jobs successfully processed during the current compaction run",
		}, []string{"status"}),
		compactionRunJobTime: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "job_time_seconds",
			Help:      "Time spent processing jobs.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"status"}),
		compactionRunInterval: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "compaction_interval_seconds",
			Help:      "The configured interval on which compaction is run in seconds",
		}),
		compactorRunning: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "running",
			Help:      "Value will be 1 if compactor is currently running on this instance",
		}),
	}

	return &m
}
