package bloomcompactor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	metricsNamespace = "loki"
	metricsSubsystem = "bloomcompactor"
)

type metrics struct {
	compactionRunsStarted          prometheus.Counter
	compactionRunsCompleted        prometheus.Counter
	compactionRunsFailed           prometheus.Counter
	compactionRunDiscoveredTenants prometheus.Counter
	compactionRunSkippedTenants    prometheus.Counter
	compactionRunSucceededTenants  prometheus.Counter
	compactionRunFailedTenants     prometheus.Counter
	compactionRunJobStarted        prometheus.Counter
	compactionRunJobSuceeded       prometheus.Counter
	compactionRunJobFailed         prometheus.Counter
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
		compactionRunsCompleted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "runs_completed_total",
			Help:      "Total number of compactions completed successfully",
		}),
		compactionRunsFailed: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "runs_failed_total",
			Help:      "Total number of compaction runs failed",
		}),
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
		compactionRunSucceededTenants: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_succeeded",
			Help:      "Number of tenants successfully processed during the current compaction run",
		}),
		compactionRunFailedTenants: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_failed",
			Help:      "Number of tenants failed processing during the current compaction run",
		}),
		compactionRunJobStarted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "job_started",
			Help:      "Number of jobs started processing during the current compaction run",
		}),
		compactionRunJobSuceeded: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "job_succeeded",
			Help:      "Number of jobs successfully processed during the current compaction run",
		}),
		compactionRunJobFailed: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "job_failed",
			Help:      "Number of jobs failed processing during the current compaction run",
		}),
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
