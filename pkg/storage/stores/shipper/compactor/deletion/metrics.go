package deletion

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type deleteRequestHandlerMetrics struct {
	deleteRequestsReceivedTotal *prometheus.CounterVec
}

func newDeleteRequestHandlerMetrics(r prometheus.Registerer) *deleteRequestHandlerMetrics {
	m := deleteRequestHandlerMetrics{}

	m.deleteRequestsReceivedTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "compactor_delete_requests_received_total",
		Help:      "Number of delete requests received per user",
	}, []string{"user"})

	return &m
}

type deleteRequestsManagerMetrics struct {
	deleteRequestsProcessedTotal         *prometheus.CounterVec
	deleteRequestsChunksSelectedTotal    *prometheus.CounterVec
	loadPendingRequestsAttemptsTotal     *prometheus.CounterVec
	oldestPendingDeleteRequestAgeSeconds prometheus.Gauge
	pendingDeleteRequestsCount           prometheus.Gauge
}

func newDeleteRequestsManagerMetrics(r prometheus.Registerer) *deleteRequestsManagerMetrics {
	m := deleteRequestsManagerMetrics{}

	m.deleteRequestsProcessedTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "compactor_delete_requests_processed_total",
		Help:      "Number of delete requests processed per user",
	}, []string{"user"})
	m.deleteRequestsChunksSelectedTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "compactor_delete_requests_chunks_selected_total",
		Help:      "Number of chunks selected while building delete plans per user",
	}, []string{"user"})
	m.loadPendingRequestsAttemptsTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "compactor_load_pending_requests_attempts_total",
		Help:      "Number of attempts that were made to load pending requests with status",
	}, []string{"status"})
	m.oldestPendingDeleteRequestAgeSeconds = promauto.With(r).NewGauge(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "compactor_oldest_pending_delete_request_age_seconds",
		Help:      "Age of oldest pending delete request in seconds, since they are over their cancellation period",
	})
	m.pendingDeleteRequestsCount = promauto.With(r).NewGauge(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "compactor_pending_delete_requests_count",
		Help:      "Count of delete requests which are over their cancellation period and have not finished processing yet",
	})

	return &m
}
