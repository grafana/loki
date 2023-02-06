package index

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type metrics struct {
	indexQueryLatency *prometheus.HistogramVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		indexQueryLatency: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki",
			Name:      "index_request_duration_seconds",
			Help:      "Time (in seconds) spent in serving index query requests",
			Buckets:   prometheus.ExponentialBucketsRange(0.005, 100, 12),
		}, []string{"operation", "status_code"}),
	}
}
