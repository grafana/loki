package metric

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

type Gauges struct {
	*metricVec
}

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

func (g *Gauges) With(labels model.LabelSet) prometheus.Gauge {
	return g.metricVec.With(labels).(prometheus.Gauge)
}
