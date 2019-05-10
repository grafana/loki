package metric

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

type Counters struct {
	*metricVec
}

func NewCounters(name, help string) *Counters {
	return &Counters{
		metricVec: newMetricVec(func(labels map[string]string) prometheus.Metric {
			return prometheus.NewCounter(prometheus.CounterOpts{
				Help:        help,
				Name:        name,
				ConstLabels: labels,
			})
		})}
}

func (c *Counters) With(labels model.LabelSet) prometheus.Counter {
	return c.metricVec.With(labels).(prometheus.Counter)
}
