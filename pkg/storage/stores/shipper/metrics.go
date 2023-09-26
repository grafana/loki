package shipper

import (
	"github.com/grafana/dskit/instrument"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type metrics struct {
	// duration in seconds spent in serving request on index managed by BoltDB Shipper
	requestDurationSeconds *prometheus.HistogramVec
}

func newMetrics(r prometheus.Registerer) *metrics {
	return &metrics{
		requestDurationSeconds: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "request_duration_seconds",
			Help:      "Time (in seconds) spent serving requests when using boltdb shipper",
			Buckets:   instrument.DefBuckets,
		}, []string{"operation", "status_code"}),
	}
}
