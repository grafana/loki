package generic

import (
	"context"
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

type Metrics struct {
	columnar      *columnar.Metrics
	encodeSeconds prometheus.Histogram
	entitiesTotal prometheus.Counter
	appendsTotal  prometheus.Counter
}

func NewMetrics(kind string) *Metrics {
	return &Metrics{
		columnar: columnar.NewMetrics(SectionTypeGeneric),
		encodeSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "loki_dataobj_generic_encode_seconds",
			ConstLabels: prometheus.Labels{"kind": kind},
			Help:        "The number of seconds it takes to encode the generic section.",
		}),
		entitiesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "loki_dataobj_generic_entities_total",
			ConstLabels: prometheus.Labels{"kind": kind},
			Help:        "Total number of records in the generic section.",
		}),
		appendsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "loki_dataob_generic_appends_total",
			ConstLabels: prometheus.Labels{"kind": kind},
			Help:        "Total number of appends operations in the generic section.",
		}),
	}
}

func (m *Metrics) Register(reg prometheus.Registerer) error {
	var errs []error
	errs = append(errs, m.columnar.Register(reg))
	errs = append(errs, reg.Register(m.encodeSeconds))
	return errors.Join(errs...)
}

func (m *Metrics) Unregister(reg prometheus.Registerer) {
	m.columnar.Unregister(reg)

	reg.Unregister(m.encodeSeconds)
}

// Observe observes section statistics for a given section.
func (m *Metrics) Observe(ctx context.Context, section *Section) error {
	return m.columnar.Observe(ctx, section.inner)
}
