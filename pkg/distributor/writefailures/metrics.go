package writefailures

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type metrics struct {
	loggedCount    *prometheus.CounterVec
	discardedCount *prometheus.CounterVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		loggedCount: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "write_failures_logged_total",
			Help:      "The total number log failures were logged for a tenant.",
		}, []string{"tenant_id"}),
		discardedCount: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "write_failures_discarded_total",
			Help:      "The total number of write failures logs discarded for a tenant.",
		}, []string{"tenant_id"}),
	}
}
