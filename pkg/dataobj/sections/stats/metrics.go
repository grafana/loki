package stats

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// Metrics instruments the stats section.
type Metrics struct {
	columnar *columnar.Metrics

	encodeSeconds prometheus.Histogram
}

// NewMetrics creates a new set of metrics for the stats section.
func NewMetrics() *Metrics {
	return &Metrics{
		columnar: columnar.NewMetrics(sectionType),

		encodeSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "stats_encode_seconds",
			Help:      "The number of seconds it takes to encode the stats section.",

			// Native histogram only; classic buckets are disabled to avoid the
			// fixed cardinality cost.
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
	}
}

// Register registers metrics to report to reg.
func (m *Metrics) Register(reg prometheus.Registerer) error {
	var errs []error
	errs = append(errs, m.columnar.Register(reg))
	errs = append(errs, reg.Register(m.encodeSeconds))
	return errors.Join(errs...)
}

// Unregister unregisters metrics from the provided Registerer.
func (m *Metrics) Unregister(reg prometheus.Registerer) {
	m.columnar.Unregister(reg)
	reg.Unregister(m.encodeSeconds)
}

// Observe observes section statistics for a given section.
func (m *Metrics) Observe(ctx context.Context, section *Section) error {
	return m.columnar.Observe(ctx, section.inner)
}
