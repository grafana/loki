package metric

import (
	"time"

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
func NewHistograms(name, help string, config interface{}, maxIdleSec int64) (*Histograms, error) {
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
			return &expiringHistogram{prometheus.NewHistogram(prometheus.HistogramOpts{
				Help:        help,
				Name:        name,
				ConstLabels: labels,
				Buckets:     cfg.Buckets,
			}),
				0,
			}
		}, maxIdleSec),
		Cfg: cfg,
	}, nil
}

// With returns the histogram associated with a stream labelset.
func (h *Histograms) With(labels model.LabelSet) prometheus.Histogram {
	return h.metricVec.With(labels).(prometheus.Histogram)
}

type expiringHistogram struct {
	prometheus.Histogram
	lastModSec int64
}

// Observe adds a single observation to the histogram.
func (h *expiringHistogram) Observe(val float64) {
	h.Histogram.Observe(val)
	h.lastModSec = time.Now().Unix()
}

// HasExpired implements Expirable
func (h *expiringHistogram) HasExpired(currentTimeSec int64, maxAgeSec int64) bool {
	return currentTimeSec-h.lastModSec >= maxAgeSec
}
