package heroku

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	herokuEntries *prometheus.CounterVec
	herokuErrors  *prometheus.CounterVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	var m Metrics

	m.herokuEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "heroku_drain_target_entries_total",
		Help:      "Number of successful entries received by the Heroku target",
	}, []string{})

	m.herokuErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "heroku_drain_target_parsing_errors_total",
		Help:      "Number of parsing errors while receiving Heroku messages",
	}, []string{})

	reg.MustRegister(m.herokuEntries, m.herokuErrors)
	return &m
}
