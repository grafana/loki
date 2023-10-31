package bloomcompactor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	metricsNamespace = "loki_bloomcompactor"
)

type metrics struct {
	compactionRunsStarted          prometheus.Counter
	compactionRunsCompleted        prometheus.Counter
	compactionRunsErred            prometheus.Counter
	compactionRunDiscoveredTenants prometheus.Gauge
	compactionRunSkippedTenants    prometheus.Gauge
	compactionRunSucceededTenants  prometheus.Gauge
	compactionRunFailedTenants     prometheus.Gauge
	compactionRunSkippedJobs       prometheus.Gauge
	compactionRunSucceededJobs     prometheus.Gauge
	compactionRunFailedJobs        prometheus.Gauge
	compactionRunInterval          prometheus.Gauge
	compactorRunning               prometheus.Gauge
}

func newMetrics(r prometheus.Registerer) *metrics {
	m := metrics{
		compactionRunsStarted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "runs_started_total",
			Help:      "Total number of compactions started",
		}),
		compactionRunsCompleted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "runs_completed_total",
			Help:      "Total number of compactions completed successfully",
		}),
		compactionRunsErred: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "runs_failed_total",
			Help:      "Total number of compaction runs failed",
		}),
		compactionRunDiscoveredTenants: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "tenants_discovered",
			Help:      "Number of tenants discovered during the current compaction run",
		}),
		compactionRunSkippedTenants: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "tenants_skipped",
			Help:      "Number of tenants skipped during the current compaction run",
		}),
		compactionRunSucceededTenants: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "tenants_succeeded",
			Help:      "Number of tenants successfully processed during the current compaction run",
		}),
		compactionRunFailedTenants: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "tenants_failed",
			Help:      "Number of tenants failed processing during the current compaction run",
		}),
		compactionRunSkippedJobs: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "jobs_skipped",
			Help:      "Number of jobs skipped during the current compaction run",
		}),
		compactionRunSucceededJobs: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "jobs_succeeded",
			Help:      "Number of jobs successfully processed during the current compaction run",
		}),
		compactionRunFailedJobs: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "jobs_failed",
			Help:      "Number of jobs failed processing during the current compaction run",
		}),
		compactionRunInterval: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "compaction_interval_seconds",
			Help:      "The configured interval on which compaction is run in seconds",
		}),
		compactorRunning: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "running",
			Help:      "Value will be 1 if compactor is currently running on this instance",
		}),
	}

	return &m
}
