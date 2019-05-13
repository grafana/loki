package metric

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

// Counters is a vector of counters for a each log stream.
type Counters struct {
	*metricVec
}

// NewCounters creates a new counter vec.
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

// With returns the counter associated with a stream labelset.
func (c *Counters) With(labels model.LabelSet) prometheus.Counter {
	return c.metricVec.With(labels).(prometheus.Counter)
}
