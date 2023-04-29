package kubernetes

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds a set of Kubernetes target metrics.
type Metrics struct {
	reg prometheus.Registerer

	kubernetesEntries prometheus.Counter
	kubernetesErrors  prometheus.Counter
}

// NewMetrics creates a new set of Kubernetes target metrics. If reg is non-nil, the
// metrics will be registered.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	var m Metrics
	m.reg = reg

	m.kubernetesEntries = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "kubernetes_target_entries_total",
		Help:      "Total number of successful entries sent to the Kubernetes target",
	})
	m.kubernetesErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "kubernetes_target_parsing_errors_total",
		Help:      "Total number of parsing errors while receiving Kubernetes messages",
	})

	if reg != nil {
		reg.MustRegister(
			m.kubernetesEntries,
			m.kubernetesErrors,
		)
	}

	return &m
}
