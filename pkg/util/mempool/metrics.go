package mempool

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type metrics struct {
	availableBuffersPerSlab *prometheus.CounterVec
	errorsCounter           *prometheus.CounterVec
}

func newMetrics(r prometheus.Registerer, name string) *metrics {
	return &metrics{
		availableBuffersPerSlab: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace:   constants.Loki,
			Subsystem:   "mempool",
			Name:        "available_buffers_per_slab",
			Help:        "The amount of available buffers per slab.",
			ConstLabels: prometheus.Labels{"pool": name},
		}, []string{"slab"}),
		errorsCounter: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace:   constants.Loki,
			Subsystem:   "mempool",
			Name:        "errors_total",
			Help:        "The total amount of errors returned from the pool.",
			ConstLabels: prometheus.Labels{"pool": name},
		}, []string{"slab", "reason"}),
	}
}
