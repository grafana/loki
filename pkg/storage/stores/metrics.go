package stores

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type metrics struct {
	storeRequestLatency *prometheus.HistogramVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		storeRequestLatency: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki",
			Name:      "store_request_duration_seconds",
			Help:      "Time (in seconds) spent in serving store requests",
		}, []string{"operation", "status_code"}),
	}
}
