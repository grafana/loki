package watermarks

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// Metrics instruments the watermarks section.
type Metrics struct {
	columnar *columnar.Metrics

	encodeSeconds prometheus.Histogram
}

// NewMetrics creates [Metrics] and registers them with reg. If reg is nil, the
// metrics exist but are not registered.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		columnar: columnar.NewMetrics(sectionType),

		encodeSeconds: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "watermarks_encode_seconds",
			Help:      "Time to encode the watermarks section, in seconds.",
		}),
	}

	if reg != nil {
		if err := m.columnar.Register(reg); err != nil {
			panic(fmt.Errorf("registering watermarks section metrics: %w", err))
		}
	}

	return m
}

// Observe records the column statistics of section.
func (m *Metrics) Observe(ctx context.Context, section *Section) error {
	return m.columnar.Observe(ctx, section.inner)
}
