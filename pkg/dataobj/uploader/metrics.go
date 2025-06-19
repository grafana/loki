package uploader

import (
	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	uploadTime     prometheus.Histogram
	uploadFailures prometheus.Counter
	shaPrefixSize  prometheus.Gauge
}

func newMetrics(shaPrefixSize int) *metrics {
	metrics := &metrics{
		uploadFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_upload_failures_total",
			Help: "Total number of upload failures",
		}),
		uploadTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "upload_time_seconds",
			Help:      "Time taken writing data objects to object storage.",
			Buckets:   prometheus.DefBuckets,
		}),
		shaPrefixSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "sha_prefix_size",
			Help:      "The size of the SHA prefix used for object storage keys.",
		}),
	}

	metrics.shaPrefixSize.Set(float64(shaPrefixSize))
	return metrics
}

func (m *metrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		m.uploadFailures,
		m.uploadTime,
	}

	for _, collector := range collectors {
		if err := reg.Register(collector); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				return err
			}
		}
	}
	return nil
}

func (m *metrics) unregister(reg prometheus.Registerer) {
	collectors := []prometheus.Collector{
		m.uploadFailures,
		m.uploadTime,
	}

	for _, collector := range collectors {
		reg.Unregister(collector)
	}
}
