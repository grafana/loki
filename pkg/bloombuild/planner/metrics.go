package planner

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	metricsNamespace = "loki"
	metricsSubsystem = "bloomplanner"

	statusSuccess = "success"
	statusFailure = "failure"
)

type Metrics struct {
	running prometheus.Gauge

	buildStarted   prometheus.Counter
	buildCompleted *prometheus.CounterVec
	buildTime      *prometheus.HistogramVec

	tenantsDiscovered prometheus.Counter
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		running: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "running",
			Help:      "Value will be 1 if bloom planner is currently running on this instance",
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
