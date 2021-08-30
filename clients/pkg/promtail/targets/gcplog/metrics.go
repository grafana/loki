package gcplog

import "github.com/prometheus/client_golang/prometheus"

// Metrics stores gcplog entry mertrics.
type Metrics struct {
	// reg is the Registerer used to create this set of metrics.
	reg prometheus.Registerer

	gcplogEntries                 *prometheus.CounterVec
	gcplogErrors                  *prometheus.CounterVec
	gcplogTargetLastSuccessScrape *prometheus.GaugeVec
}

// NewMetrics creates a new set of metrics. Metrics will be registered to reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	var m Metrics
	m.reg = reg

	m.gcplogEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "gcplog_target_entries_total",
		Help:      "Help number of successful entries sent to the gcplog target",
	}, []string{"project"})

	m.gcplogErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "gcplog_target_parsing_errors_total",
		Help:      "Total number of parsing errors while receiving gcplog messages",
	}, []string{"project"})

	m.gcplogTargetLastSuccessScrape = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "gcplog_target_last_success_scrape",
		Help:      "Timestamp of the specific target's last successful poll",
	}, []string{"project", "target"})

	reg.MustRegister(m.gcplogEntries, m.gcplogErrors)
	return &m
}
