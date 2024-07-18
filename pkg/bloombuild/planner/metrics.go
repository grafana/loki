package planner

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/queue"
)

const (
	metricsNamespace = "loki"
	metricsSubsystem = "bloomplanner"

	statusSuccess = "success"
	statusFailure = "failure"

	phasePlanning = "planning"
	phaseBuilding = "building"
)

type Metrics struct {
	running prometheus.Gauge

	// Extra Queue metrics
	connectedBuilders prometheus.GaugeFunc
	queueDuration     prometheus.Histogram
	inflightRequests  prometheus.Summary
	tasksRequeued     prometheus.Counter
	taskLost          prometheus.Counter

	planningTime     prometheus.Histogram
	buildStarted     prometheus.Counter
	buildCompleted   *prometheus.CounterVec
	buildTime        *prometheus.HistogramVec
	buildLastSuccess prometheus.Gauge

	blocksDeleted *prometheus.CounterVec
	metasDeleted  *prometheus.CounterVec

	tenantsDiscovered    prometheus.Counter
	tenantTasksPlanned   *prometheus.GaugeVec
	tenantTasksCompleted *prometheus.GaugeVec

	// Retention metrics
	retentionRunning                  prometheus.Gauge
	retentionTime                     *prometheus.HistogramVec
	retentionDaysPerIteration         *prometheus.HistogramVec
	retentionTenantsPerIteration      *prometheus.HistogramVec
	retentionTenantsExceedingLookback prometheus.Gauge
}

func NewMetrics(
	r prometheus.Registerer,
	getConnectedBuilders func() float64,
) *Metrics {
	return &Metrics{
		running: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "running",
			Help:      "Value will be 1 if bloom planner is currently running on this instance",
		}),
		connectedBuilders: promauto.With(r).NewGaugeFunc(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "connected_builders",
			Help:      "Number of builders currently connected to the planner.",
		}, getConnectedBuilders),
		queueDuration: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "queue_duration_seconds",
			Help:      "Time spend by tasks in queue before getting picked up by a builder.",
			Buckets:   prometheus.DefBuckets,
		}),
		inflightRequests: promauto.With(r).NewSummary(prometheus.SummaryOpts{
			Namespace:  metricsNamespace,
			Subsystem:  metricsSubsystem,
			Name:       "inflight_tasks",
			Help:       "Number of inflight tasks (either queued or processing) sampled at a regular interval. Quantile buckets keep track of inflight tasks over the last 60s.",
			Objectives: map[float64]float64{0.5: 0.05, 0.75: 0.02, 0.8: 0.02, 0.9: 0.01, 0.95: 0.01, 0.99: 0.001},
			MaxAge:     time.Minute,
			AgeBuckets: 6,
		}),
		tasksRequeued: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tasks_requeued_total",
			Help:      "Total number of tasks requeued due to not being picked up by a builder.",
		}),
		taskLost: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tasks_lost_total",
			Help:      "Total number of tasks lost due to not being picked up by a builder and failed to be requeued.",
		}),

		planningTime: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "planning_time_seconds",
			Help:      "Time spent planning a build cycle.",
			// 1s --> 1h (steps of 1 minute)
			Buckets: prometheus.LinearBuckets(1, 60, 60),
		}),
		buildStarted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "build_started_total",
			Help:      "Total number of builds started",
		}),
		buildCompleted: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "build_completed_total",
			Help:      "Total number of builds completed",
		}, []string{"status"}),
		buildTime: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "build_time_seconds",
			Help:      "Time spent during a builds cycle.",
			// Buckets in seconds:
			Buckets: append(
				// 1s --> 1h (steps of 10 minutes)
				prometheus.LinearBuckets(1, 600, 6),
				// 1h --> 24h (steps of 1 hour)
				prometheus.LinearBuckets(3600, 3600, 24)...,
			),
		}, []string{"status"}),
		buildLastSuccess: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "build_last_successful_run_timestamp_seconds",
			Help:      "Unix timestamp of the last successful build cycle.",
		}),

		blocksDeleted: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "blocks_deleted_total",
			Help:      "Number of blocks deleted",
		}, []string{"phase"}),
		metasDeleted: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "metas_deleted_total",
			Help:      "Number of metas deleted",
		}, []string{"phase"}),

		tenantsDiscovered: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_discovered_total",
			Help:      "Number of tenants discovered during the current build iteration",
		}),
		tenantTasksPlanned: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenant_tasks_planned",
			Help:      "Number of tasks planned for a tenant during the current build iteration.",
		}, []string{"tenant"}),
		tenantTasksCompleted: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenant_tasks_completed",
			Help:      "Number of tasks completed for a tenant during the current build iteration.",
		}, []string{"tenant", "status"}),

		// Retention
		retentionRunning: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "retention_running",
			Help:      "1 if retention is running in this compactor.",
		}),

		retentionTime: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "retention_time_seconds",
			Help:      "Time this retention process took to complete.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"status"}),

		retentionDaysPerIteration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "retention_days_processed",
			Help:      "Number of days iterated over during the retention process.",
			// 1day -> 5 years, 10 buckets
			Buckets: prometheus.ExponentialBucketsRange(1, 365*5, 10),
		}, []string{"status"}),

		retentionTenantsPerIteration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "retention_tenants_processed",
			Help:      "Number of tenants on which retention was applied during the retention process.",
			// 1 tenant -> 10k tenants, 10 buckets
			Buckets: prometheus.ExponentialBucketsRange(1, 10000, 10),
		}, []string{"status"}),

		retentionTenantsExceedingLookback: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "retention_tenants_exceeding_lookback",
			Help:      "Number of tenants with a retention exceeding the configured retention lookback.",
		}),
	}
}

func NewQueueMetrics(r prometheus.Registerer) *queue.Metrics {
	return queue.NewMetrics(r, metricsNamespace, metricsSubsystem)
}
