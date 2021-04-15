package syslog

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds a set of syslog metrics.
type Metrics struct {
	reg prometheus.Registerer

	syslogEntries       prometheus.Counter
	syslogParsingErrors prometheus.Counter
	syslogEmptyMessages prometheus.Counter
}

// NewMetrics creates a new set of syslog metrics. If reg is non-nil, the
// metrics will be registered.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	var m Metrics
	m.reg = reg

	m.syslogEntries = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "syslog_target_entries_total",
		Help:      "Total number of successful entries sent to the syslog target",
	})
	m.syslogParsingErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "syslog_target_parsing_errors_total",
		Help:      "Total number of parsing errors while receiving syslog messages",
	})
	m.syslogEmptyMessages = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "syslog_empty_messages_total",
		Help:      "Total number of empty messages receiving from syslog",
	})

	if reg != nil {
		reg.MustRegister(
			m.syslogEntries,
			m.syslogParsingErrors,
			m.syslogEmptyMessages,
		)
	}

	return &m
}
