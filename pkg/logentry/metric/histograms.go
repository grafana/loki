package metric

import (
	"github.com/mitchellh/mapstructure"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

type HistogramConfig struct {
	Value   *string   `mapstructure:"value"`
	Buckets []float64 `mapstructure:"buckets"`
}

func validateHistogramConfig(config *HistogramConfig) error {
	return nil
}

func parseHistogramConfig(config interface{}) (*HistogramConfig, error) {
	cfg := &HistogramConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Histograms is a vector of histograms for a each log stream.
type Histograms struct {
	*metricVec
	Cfg *HistogramConfig
}

// NewHistograms creates a new histogram vec.
func NewHistograms(name, help string, config interface{}) (*Histograms, error) {
	cfg, err := parseHistogramConfig(config)
	if err != nil {
		return nil, err
	}
	err = validateHistogramConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Histograms{
		metricVec: newMetricVec(func(labels map[string]string) prometheus.Metric {
			return prometheus.NewHistogram(prometheus.HistogramOpts{
				Help:        help,
				Name:        name,
				ConstLabels: labels,
				Buckets:     cfg.Buckets,
			})
		}),
		Cfg: cfg,
	}, nil
}

// With returns the histogram associated with a stream labelset.
func (h *Histograms) With(labels model.LabelSet) prometheus.Histogram {
	return h.metricVec.With(labels).(prometheus.Histogram)
}
