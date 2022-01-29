package objectstore

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds a set of cloudflare metrics.
type Metrics struct {
	reg prometheus.Registerer

	objectEntries            prometheus.Counter
	objectReadBytes          *prometheus.GaugeVec
	objectReadLines          *prometheus.CounterVec
	objectActive             *prometheus.GaugeVec
	objectLogLengthHistogram *prometheus.HistogramVec
}

// NewMetrics creates a new set of cloudflare metrics. If reg is non-nil, the
// metrics will be registered.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	var m Metrics
	m.reg = reg

	m.objectEntries = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "object_target_entries_total",
		Help:      "Total number of successful entries sent via the cloudflare target",
	})

	m.objectReadBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "object_read_bytes_total",
		Help:      "Number of bytes read.",
	}, []string{"key", "store"})
	m.objectReadLines = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "object_read_lines_total",
		Help:      "Number of lines read.",
	}, []string{"key", "store"})
	m.objectActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "object_files_active_total",
		Help:      "Number of active files.",
	}, []string{"store"})
	m.objectLogLengthHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "promtail",
		Name:      "object_log_entries_bytes",
		Help:      "the total count of bytes",
		Buckets:   prometheus.ExponentialBuckets(16, 2, 8),
	}, []string{"key", "store"})
	if reg != nil {
		reg.MustRegister(
			m.objectEntries,
			m.objectReadBytes,
			m.objectReadLines,
			m.objectActive,
			m.objectLogLengthHistogram,
		)
	}

	return &m
}
