package queue

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	QueueLength       *prometheus.GaugeVec   // Per tenant and reason.
	DiscardedRequests *prometheus.CounterVec // Per tenant.
	// unexported fields must only used by the RequestQueue which resides in the same package
	enqueueCount *prometheus.CounterVec // Per tenant and level
	dequeueCount *prometheus.CounterVec // Per tenant and querier
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
		enqueueCount: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "cortex",
			Subsystem: subsystem,
			Name:      "enqueue_count",
			Help:      "Total number of enqueued (sub-)queries.",
		}, []string{"user", "level"}),
		dequeueCount: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "cortex",
			Subsystem: subsystem,
			Name:      "dequeue_count",
			Help:      "Total number of dequeued (sub-)queries.",
		}, []string{"user", "querier"}),
	}
}

func (m *Metrics) Cleanup(user string) {
	m.QueueLength.DeleteLabelValues(user)
	m.DiscardedRequests.DeleteLabelValues(user)
	m.dequeueCount.DeletePartialMatch(prometheus.Labels{"user": user})
}
