package mempool

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type metrics struct {
	availableBuffersPerSlab *prometheus.GaugeVec
	errorsCounter           *prometheus.CounterVec
	accesses                *prometheus.CounterVec
	waitDuration            *prometheus.HistogramVec
}

const (
	opTypeGet = "get"
	opTypePut = "put"
)

func newMetrics(r prometheus.Registerer, name string) *metrics {
	return &metrics{
		availableBuffersPerSlab: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
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
		accesses: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace:   constants.Loki,
			Subsystem:   "mempool",
			Name:        "accesses_total",
			Help:        "The total amount of accesses to the pool.",
			ConstLabels: prometheus.Labels{"pool": name},
		}, []string{"slab", "op"}),
		waitDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   constants.Loki,
			Subsystem:   "mempool",
			Name:        "wait_duration_seconds",
			Help:        "Time spent waiting for obtaining buffer from slab.",
			ConstLabels: prometheus.Labels{"pool": name},
		}, []string{"slab"}),
	}
}
