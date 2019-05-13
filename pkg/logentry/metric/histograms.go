package metric

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

// Histograms is a vector of histograms for a each log stream.
type Histograms struct {
	*metricVec
}

// NewHistograms creates a new histogram vec.
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

// With returns the histogram associated with a stream labelset.
func (h *Histograms) With(labels model.LabelSet) prometheus.Histogram {
	return h.metricVec.With(labels).(prometheus.Histogram)
}
