// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package cleaner

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	r prometheus.Registerer

	DiscoveryError     *prometheus.CounterVec
	SegmentError       *prometheus.CounterVec
	ManagedStorage     prometheus.Gauge
	AbandonedStorage   prometheus.Gauge
	CleanupRunsSuccess prometheus.Counter
	CleanupRunsErrors  prometheus.Counter
	CleanupTimes       prometheus.Histogram
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	m := Metrics{r: r}

	m.DiscoveryError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cleaner_storage_error_total",
			Help: "Errors encountered discovering local storage paths",
		},
		[]string{"storage"},
	)

	m.SegmentError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cleaner_segment_error_total",
			Help: "Errors encountered finding most recent WAL segments",
		},
		[]string{"storage"},
	)

	m.ManagedStorage = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "cleaner_managed_storage",
			Help: "Number of storage directories associated with managed instances",
		},
	)

	m.AbandonedStorage = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "cleaner_abandoned_storage",
			Help: "Number of storage directories not associated with any managed instance",
		},
	)

	m.CleanupRunsSuccess = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "cleaner_success_total",
			Help: "Number of successfully removed abandoned WALs",
		},
	)

	m.CleanupRunsErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "cleaner_errors_total",
			Help: "Number of errors removing abandoned WALs",
		},
	)

	m.CleanupTimes = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "cleaner_cleanup_seconds",
			Help: "Time spent performing each periodic WAL cleanup",
		},
	)

	if r != nil {
		r.MustRegister(
			m.DiscoveryError,
			m.SegmentError,
			m.ManagedStorage,
			m.AbandonedStorage,
			m.CleanupRunsSuccess,
			m.CleanupRunsErrors,
			m.CleanupTimes,
		)
	}

	return &m
}

func (m *Metrics) Unregister() {
	if m.r == nil {
		return
	}
	cs := []prometheus.Collector{
		m.DiscoveryError,
		m.SegmentError,
		m.ManagedStorage,
		m.AbandonedStorage,
		m.CleanupRunsSuccess,
		m.CleanupRunsErrors,
		m.CleanupTimes,
	}
	for _, c := range cs {
		m.r.Unregister(c)
	}
}
