package metric

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

// Gauges is a vector of gauges for a each log stream.
type Gauges struct {
	*metricVec
}

// NewGauges creates a new gauge vec.
func NewGauges(name, help string) *Gauges {
	return &Gauges{
		metricVec: newMetricVec(func(labels map[string]string) prometheus.Metric {
			return prometheus.NewGauge(prometheus.GaugeOpts{
				Help:        help,
				Name:        name,
				ConstLabels: labels,
			})
		})}
}

// With returns the gauge associated with a stream labelset.
func (g *Gauges) With(labels model.LabelSet) prometheus.Gauge {
	return g.metricVec.With(labels).(prometheus.Gauge)
}
