package tail

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type TailMetrics struct {
	tailsActive         prometheus.Gauge
	tailedStreamsActive prometheus.Gauge
	tailedBytesTotal    prometheus.Counter
}

func NewTailMetrics(r prometheus.Registerer) *TailMetrics {
	return &TailMetrics{
		tailsActive: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_querier_tail_active",
			Help: "Number of active tailers",
		}),
		tailedStreamsActive: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_querier_tail_active_streams",
			Help: "Number of active streams being tailed",
		}),
		tailedBytesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_querier_tail_bytes_total",
			Help: "total bytes tailed",
		}),
	}
}
