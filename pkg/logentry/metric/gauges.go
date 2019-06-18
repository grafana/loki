package metric

import (
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

const (
	GaugeSet = "set"
	GaugeInc = "inc"
	GaugeDec = "dec"
	GaugeAdd = "add"
	GaugeSub = "sub"

	ErrGaugeActionRequired = "gauge action must be defined as `set`, `inc`, `dec`, `add`, or `sub`"
	ErrGaugeInvalidAction  = "action %s is not valid, action must be `set`, `inc`, `dec`, `add`, or `sub`"
)

type GaugeConfig struct {
	Value  *string `mapstructure:"value"`
	Action string  `mapstructure:"action"`
}

func validateGaugeConfig(config *GaugeConfig) error {
	if config.Action == "" {
		return errors.New(ErrGaugeActionRequired)
	}
	config.Action = strings.ToLower(config.Action)
	if config.Action != GaugeSet &&
		config.Action != GaugeInc &&
		config.Action != GaugeDec &&
		config.Action != GaugeAdd &&
		config.Action != GaugeSub {
		return errors.Errorf(ErrGaugeInvalidAction, config.Action)
	}
	return nil
}

func parseGaugeConfig(config interface{}) (*GaugeConfig, error) {
	cfg := &GaugeConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Gauges is a vector of gauges for a each log stream.
type Gauges struct {
	*metricVec
	Cfg *GaugeConfig
}

// NewGauges creates a new gauge vec.
func NewGauges(name, help string, config interface{}) (*Gauges, error) {
	cfg, err := parseGaugeConfig(config)
	if err != nil {
		return nil, err
	}
	err = validateGaugeConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Gauges{
		metricVec: newMetricVec(func(labels map[string]string) prometheus.Metric {
			return prometheus.NewGauge(prometheus.GaugeOpts{
				Help:        help,
				Name:        name,
				ConstLabels: labels,
			})
		}),
		Cfg: cfg,
	}, nil
}

// With returns the gauge associated with a stream labelset.
func (g *Gauges) With(labels model.LabelSet) prometheus.Gauge {
	return g.metricVec.With(labels).(prometheus.Gauge)
}
