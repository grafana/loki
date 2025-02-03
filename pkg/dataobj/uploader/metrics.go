package uploader

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	UploadTime prometheus.Histogram
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		UploadTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "upload_time_seconds",
			Help:      "Time taken writing data objects to object storage.",
			Buckets:   prometheus.DefBuckets,
		}),
	}
}

func (m *Metrics) Register(reg prometheus.Registerer) error {
	if err := reg.Register(m.UploadTime); err != nil {
		if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
			return err
		}
	}
	return nil
}

func (m *Metrics) Unregister(reg prometheus.Registerer) {
	reg.Unregister(m.UploadTime)
}
