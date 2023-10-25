package queue

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	queueLength       *prometheus.GaugeVec     // Per tenant
	discardedRequests *prometheus.CounterVec   // Per tenant
	enqueueCount      *prometheus.CounterVec   // Per tenant and level
	querierWaitTime   *prometheus.HistogramVec // Per querier wait time
}

func NewMetrics(subsystem string, registerer prometheus.Registerer, metricsNamespace string) *Metrics {
	return &Metrics{
		queueLength: promauto.With(registerer).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: subsystem,
			Name:      "queue_length",
			Help:      "Number of queries in the queue.",
		}, []string{"user"}),
		discardedRequests: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: subsystem,
			Name:      "discarded_requests_total",
			Help:      "Total number of query requests discarded.",
		}, []string{"user"}),
		enqueueCount: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: subsystem,
			Name:      "enqueue_count",
			Help:      "Total number of enqueued (sub-)queries.",
		}, []string{"user", "level"}),
		querierWaitTime: promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: subsystem,
			Name:      "querier_wait_seconds",
			Help:      "Time spend waiting for new requests.",
			Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 240},
		}, []string{"querier"}),
	}
}

func (m *Metrics) Cleanup(user string) {
	m.queueLength.DeleteLabelValues(user)
	m.discardedRequests.DeleteLabelValues(user)
	m.enqueueCount.DeletePartialMatch(prometheus.Labels{"user": user})
}
