package deletion

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type DeleteRequestClientMetrics struct {
	deleteRequestsLookupsTotal       prometheus.Counter
	deleteRequestsLookupsFailedTotal prometheus.Counter
}

func (m DeleteRequestClientMetrics) Unregister() {
	prometheus.Unregister(m.deleteRequestsLookupsTotal)
	prometheus.Unregister(m.deleteRequestsLookupsFailedTotal)
}

func NewDeleteRequestClientMetrics(r prometheus.Registerer) *DeleteRequestClientMetrics {
	m := DeleteRequestClientMetrics{}

	m.deleteRequestsLookupsTotal = promauto.With(r).NewCounter(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "delete_request_lookups_total",
		Help:      "Number times the client has looked up delete requests",
	})

	m.deleteRequestsLookupsFailedTotal = promauto.With(r).NewCounter(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "delete_request_lookups_failed_total",
		Help:      "Number times the client has failed to look up delete requests",
	})

	return &m
}

type deleteRequestHandlerMetrics struct {
	deleteRequestsReceivedTotal *prometheus.CounterVec
}

func newDeleteRequestHandlerMetrics(r prometheus.Registerer) *deleteRequestHandlerMetrics {
	m := deleteRequestHandlerMetrics{}

	m.deleteRequestsReceivedTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "compactor_delete_requests_received_total",
		Help:      "Number of delete requests received per user",
	}, []string{"user"})

	return &m
}

type deleteRequestsManagerMetrics struct {
	deleteRequestsProcessedTotal         *prometheus.CounterVec
	deleteRequestsChunksSelectedTotal    *prometheus.CounterVec
	loadPendingRequestsAttemptsTotal     *prometheus.CounterVec
	deletionFailures                     *prometheus.CounterVec
	oldestPendingDeleteRequestAgeSeconds prometheus.Gauge
	pendingDeleteRequestsCount           prometheus.Gauge
	deletedLinesTotal                    *prometheus.CounterVec
}

func newDeleteRequestsManagerMetrics(r prometheus.Registerer) *deleteRequestsManagerMetrics {
	m := deleteRequestsManagerMetrics{}

	m.deleteRequestsProcessedTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "compactor_delete_requests_processed_total",
		Help:      "Number of delete requests processed per user",
	}, []string{"user"})
	m.deleteRequestsChunksSelectedTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "compactor_delete_requests_chunks_selected_total",
		Help:      "Number of chunks selected while building delete plans per user",
	}, []string{"user"})
	m.deletionFailures = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "compactor_delete_processing_fails_total",
		Help:      "Number of times the delete phase of compaction has failed",
	}, []string{"cause"})
	m.loadPendingRequestsAttemptsTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "compactor_load_pending_requests_attempts_total",
		Help:      "Number of attempts that were made to load pending requests with status",
	}, []string{"status"})
	m.oldestPendingDeleteRequestAgeSeconds = promauto.With(r).NewGauge(prometheus.GaugeOpts{
		Namespace: constants.Loki,
		Name:      "compactor_oldest_pending_delete_request_age_seconds",
		Help:      "Age of oldest pending delete request in seconds since they are over their cancellation period",
	})
	m.pendingDeleteRequestsCount = promauto.With(r).NewGauge(prometheus.GaugeOpts{
		Namespace: constants.Loki,
		Name:      "compactor_pending_delete_requests_count",
		Help:      "Count of delete requests which are over their cancellation period and have not finished processing yet",
	})
	m.deletedLinesTotal = promauto.With(r).NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "compactor_deleted_lines",
		Help:      "Number of deleted lines per user",
	}, []string{"user"})

	return &m
}
