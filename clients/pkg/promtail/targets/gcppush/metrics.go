package gcppush

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	gcpPushEntries *prometheus.CounterVec
	gcpPushErrors  *prometheus.CounterVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	var m Metrics

	m.gcpPushEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "gcp_push_target_entries_total",
		Help:      "Number of successful entries received by the GCP Push target",
	}, []string{})

	m.gcpPushErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "gcp_push_target_parsing_errors_total",
		Help:      "Number of parsing errors while receiving GCP Push messages",
	}, []string{})

	reg.MustRegister(m.gcpPushEntries, m.gcpPushErrors)
	return &m
}
