package uploader

import (
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	uploadTime    prometheus.Histogram
	uploadCount   *prometheus.CounterVec
	shaPrefixSize prometheus.Gauge
}

func newMetrics(shaPrefixSize int) *metrics {
	subsystem := "dataobj_uploader"
	metrics := &metrics{
		uploadCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "upload_count_total",
			Help:      "Total number of uploads grouped by status",
		}, []string{"status"}),
		uploadTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "upload_time_seconds",
			Help:      "Time taken writing data objects to object storage.",
			Buckets:   prometheus.DefBuckets,
		}),
		shaPrefixSize: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "sha_prefix_size",
			Help:      "The size of the SHA prefix used for object storage keys.",
		}),
	}

	metrics.shaPrefixSize.Set(float64(shaPrefixSize))
	return metrics
}

func (m *metrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		m.uploadCount,
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
		m.uploadCount,
		m.uploadTime,
	}

	for _, collector := range collectors {
		reg.Unregister(collector)
	}
}
