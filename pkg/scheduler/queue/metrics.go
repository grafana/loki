package queue

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	queueLength       *prometheus.GaugeVec   // Per tenant
	discardedRequests *prometheus.CounterVec // Per tenant
	enqueueCount      *prometheus.CounterVec // Per tenant and level
}

func NewMetrics(subsystem string, registerer prometheus.Registerer) *Metrics {
	return &Metrics{
		queueLength: promauto.With(registerer).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "cortex",
			Subsystem: subsystem,
			Name:      "queue_length",
			Help:      "Number of queries in the queue.",
		}, []string{"user"}),
		discardedRequests: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "cortex",
			Subsystem: subsystem,
			Name:      "discarded_requests_total",
			Help:      "Total number of query requests discarded.",
		}, []string{"user"}),
		enqueueCount: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Subsystem: subsystem,
			Name:      "enqueue_count",
			Help:      "Total number of enqueued (sub-)queries.",
		}, []string{"user", "level"}),
	}
}

func (m *Metrics) Cleanup(user string) {
	m.queueLength.DeleteLabelValues(user)
	m.discardedRequests.DeleteLabelValues(user)
	m.enqueueCount.DeletePartialMatch(prometheus.Labels{"user": user})
}
