package index

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

type Metrics struct {
	IndexQueryLatency *prometheus.HistogramVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		IndexQueryLatency: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "index_request_duration_seconds",
			Help:      "Time (in seconds) spent in serving index query requests",
			Buckets:   prometheus.ExponentialBucketsRange(0.005, 100, 12),
		}, []string{"operation", "status_code"}),
	}
}
