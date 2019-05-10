package metric

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

type Histograms struct {
	*metricVec
}

func NewHistograms(name, help string, buckets []float64) *Histograms {
	return &Histograms{
		metricVec: newMetricVec(func(labels map[string]string) prometheus.Metric {
			return prometheus.NewHistogram(prometheus.HistogramOpts{
				Help:        help,
				Name:        name,
				ConstLabels: labels,
				Buckets:     buckets,
			})
		})}
}

func (h *Histograms) With(labels model.LabelSet) prometheus.Histogram {
	return h.metricVec.With(labels).(prometheus.Histogram)
}
