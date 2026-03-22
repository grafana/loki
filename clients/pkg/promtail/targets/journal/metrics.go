package journal

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds a set of journal target metrics.
type Metrics struct {
	reg prometheus.Registerer

	journalErrors *prometheus.CounterVec
	journalLines  prometheus.Counter
}

const (
	noMessageError   = "no_message"
	emptyLabelsError = "empty_labels"
)

// NewMetrics creates a new set of journal target metrics. If reg is non-nil, the
// metrics will be registered.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	var m Metrics
	m.reg = reg

	m.journalErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "journal_target_parsing_errors_total",
		Help:      "Total number of parsing errors while reading journal messages",
	}, []string{"error"})
	m.journalLines = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "journal_target_lines_total",
		Help:      "Total number of successful journal lines read",
	})

	if reg != nil {
		reg.MustRegister(
			m.journalErrors,
			m.journalLines,
		)
	}

	return &m
}
