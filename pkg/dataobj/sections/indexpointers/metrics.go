package indexpointers

import (
	"context"
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

type Metrics struct {
	columnar *columnar.Metrics

	encodeSeconds prometheus.Histogram
	recordsTotal  prometheus.Counter
	minTimestamp  prometheus.Gauge
	maxTimestamp  prometheus.Gauge
}

func NewMetrics() *Metrics {
	return &Metrics{
		columnar: columnar.NewMetrics(sectionType),

		encodeSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "index_pointers_encode_seconds",
			Help:      "The number of seconds it takes to encode the index pointers section.",
		}),
		recordsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki_dataobj",
			Subsystem: "index_pointers",
			Name:      "records_total",

			Help: "Total number of records in the index pointers section.",
		}),

		minTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "index_pointers",
			Name:      "min_timestamp",

			Help: "The minimum timestamp (in unix seconds) across all index pointers; this resets after an encode.",
		}),

		maxTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "index_pointers",
			Name:      "max_timestamp",

			Help: "The maximum timestamp (in unix seconds) across all index pointers; this resets after an encode.",
		}),
	}
}

func (m *Metrics) Register(reg prometheus.Registerer) error {
	var errs []error
	errs = append(errs, m.columnar.Register(reg))
	errs = append(errs, reg.Register(m.encodeSeconds))
	errs = append(errs, reg.Register(m.recordsTotal))
	errs = append(errs, reg.Register(m.minTimestamp))
	errs = append(errs, reg.Register(m.maxTimestamp))
	return errors.Join(errs...)
}

func (m *Metrics) Unregister(reg prometheus.Registerer) {
	m.columnar.Unregister(reg)

	reg.Unregister(m.encodeSeconds)
	reg.Unregister(m.recordsTotal)
	reg.Unregister(m.minTimestamp)
	reg.Unregister(m.maxTimestamp)
}

// Observe observes section statistics for a given section.
func (m *Metrics) Observe(ctx context.Context, section *Section) error {
	return m.columnar.Observe(ctx, section.inner)
}
