package queue

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	QueueLength       *prometheus.GaugeVec   // Per tenant and reason.
	DiscardedRequests *prometheus.CounterVec // Per tenant.
}

func NewMetrics(subsystem string, registerer prometheus.Registerer) *Metrics {
	return &Metrics{
		QueueLength: promauto.With(registerer).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "cortex",
			Subsystem: subsystem,
			Name:      "queue_length",
			Help:      "Number of queries in the queue.",
		}, []string{"user"}),
		DiscardedRequests: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "cortex",
			Subsystem: subsystem,
			Name:      "discarded_requests_total",
			Help:      "Total number of query requests discarded.",
		}, []string{"user"}),
	}
}
