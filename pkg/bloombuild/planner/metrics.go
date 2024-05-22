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
)

type Metrics struct {
	running prometheus.Gauge

	// Extra Queue metrics
	connectedBuilders prometheus.GaugeFunc
	queueDuration     prometheus.Histogram
	inflightRequests  prometheus.Summary

	buildStarted   prometheus.Counter
	buildCompleted *prometheus.CounterVec
	buildTime      *prometheus.HistogramVec

	tenantsDiscovered prometheus.Counter
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
			Buckets:   prometheus.DefBuckets,
		}, []string{"status"}),

		tenantsDiscovered: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "tenants_discovered_total",
			Help:      "Number of tenants discovered during the current build iteration",
		}),
	}
}

func NewQueueMetrics(r prometheus.Registerer) *queue.Metrics {
	return queue.NewMetrics(r, metricsNamespace, metricsSubsystem)
}
