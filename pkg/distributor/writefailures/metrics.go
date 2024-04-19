package writefailures

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type metrics struct {
	loggedCount    *prometheus.CounterVec
	discardedCount *prometheus.CounterVec
}

func newMetrics(reg prometheus.Registerer, subsystem string) *metrics {
	return &metrics{
		loggedCount: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace:   constants.Loki,
			Name:        "write_failures_logged_total",
			Help:        "The total number of write failures logs successfully emitted for a tenant.",
			ConstLabels: prometheus.Labels{"subsystem": subsystem},
		}, []string{"org_id"}),
		discardedCount: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace:   constants.Loki,
			Name:        "write_failures_discarded_total",
			Help:        "The total number of write failures logs discarded for a tenant.",
			ConstLabels: prometheus.Labels{"subsystem": subsystem},
		}, []string{"org_id"}),
	}
}
