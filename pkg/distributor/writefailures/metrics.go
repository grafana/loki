package writefailures

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type metrics struct {
	loggedCount    *prometheus.CounterVec
	discardedCount *prometheus.CounterVec
}

func newMetrics(reg prometheus.Registerer, subsystem string) *metrics {
	// prometheus.NewCounterVec(opts prometheus.CounterOpts, labelNames []string)
	return &metrics{
		loggedCount: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace:   "loki",
			Name:        "write_failures_logged_total",
			Help:        "The total number of write failures logs successfully emitted for a tenant.",
			ConstLabels: prometheus.Labels{"subsystem": subsystem},
		}, []string{"tenant_id"}),
		discardedCount: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace:   "loki",
			Name:        "write_failures_discarded_total",
			Help:        "The total number of write failures logs discarded for a tenant.",
			ConstLabels: prometheus.Labels{"subsystem": subsystem},
		}, []string{"tenant_id"}),
	}
}
