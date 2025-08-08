package dataobj

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

type builderMetrics struct {
	scratchStoreSections        prometheus.Gauge
	scratchStoreBytes           *prometheus.GaugeVec
	scratchStoreRequestsSeconds *prometheus.HistogramVec
}

func newBuilderMetrics() *builderMetrics {
	return &builderMetrics{
		scratchStoreSections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_scratch_store_sections",
			Help: "Current number of sections in the scratch store",
		}),
		scratchStoreBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_dataobj_scratch_store_bytes",
			Help: "Current size of sections in the scratch store in bytes by region type",
		}, []string{"region_type"}),

		scratchStoreRequestsSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "loki_dataobj_scratch_store_requests_seconds",
			Help: "Time taken to perform operations on the scratch store",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{"operation", "result"}),
	}
}

// Register registers metrics to report to reg.
func (m *builderMetrics) Register(reg prometheus.Registerer) error {
	var errs []error

	errs = append(errs, reg.Register(m.scratchStoreSections))
	errs = append(errs, reg.Register(m.scratchStoreBytes))
	errs = append(errs, reg.Register(m.scratchStoreRequestsSeconds))

	return errors.Join(errs...)
}

// Unregister unregisters metrics from the provided Registerer.
func (m *builderMetrics) Unregister(reg prometheus.Registerer) {
	reg.Unregister(m.scratchStoreSections)
	reg.Unregister(m.scratchStoreBytes)
	reg.Unregister(m.scratchStoreRequestsSeconds)
}
