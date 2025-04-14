package generator

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type metrics struct {
	activeStreamsTotal   *prometheus.CounterVec
	kafkaWriteLatency    prometheus.Histogram
	kafkaWriteBytesTotal prometheus.Counter
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		activeStreamsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "active_streams_total",
			Help: "The total number of active streams",
		}, []string{"tenant"}),
		kafkaWriteLatency: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "kafka_write_latency_seconds",
			Help:                            "Latency to write stream metadata records to Kafka.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			NativeHistogramMaxBucketNumber:  100,
			Buckets:                         prometheus.DefBuckets,
		}),
		kafkaWriteBytesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "kafka_write_bytes_total",
			Help: "Total number of bytes sent to Kafka.",
		}),
	}
}
