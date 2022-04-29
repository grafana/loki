package file

import "github.com/prometheus/client_golang/prometheus"

// Metrics hold the set of file-based metrics.
type Metrics struct {
	// Registerer used. May be nil.
	reg prometheus.Registerer

	// File-specific metrics
	readBytes       *prometheus.GaugeVec
	totalBytes      *prometheus.GaugeVec
	readLines       *prometheus.CounterVec
	filesActive     prometheus.Gauge
	streamTimestamp *prometheus.GaugeVec

	// Manager metrics
	failedTargets *prometheus.CounterVec
	targetsActive prometheus.Gauge
}

// NewMetrics creates a new set of file metrics. If reg is non-nil, the metrics
// will be registered.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	var m Metrics
	m.reg = reg

	m.readBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "read_bytes_total",
		Help:      "Number of bytes read.",
	}, []string{"path"})
	m.totalBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "file_bytes_total",
		Help:      "Number of bytes total.",
	}, []string{"path"})
	m.readLines = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "read_lines_total",
		Help:      "Number of lines read.",
	}, []string{"path"})
	m.filesActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "files_active_total",
		Help:      "Number of active files.",
	})
	m.streamTimestamp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "stream_last_timestamp_seconds",
		Help:      "The last timestamp read for streams (post pipeline stage processing) per path.",
	}, []string{"path"})

	m.failedTargets = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "targets_failed_total",
		Help:      "Number of failed targets.",
	}, []string{"reason"})
	m.targetsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "targets_active_total",
		Help:      "Number of active total.",
	})

	if reg != nil {
		reg.MustRegister(
			m.readBytes,
			m.totalBytes,
			m.readLines,
			m.filesActive,
			m.streamTimestamp,
			m.failedTargets,
			m.targetsActive,
		)
	}

	return &m
}
