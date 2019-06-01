package metric

import (
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

const (
	CounterInc = "inc"
	CounterAdd = "add"

	ErrCounterActionRequired = "counter action must be defined as either `inc` or `add`"
	ErrCounterInvalidAction  = "action %s is not valid, action must be either `inc` or `add`"
)

type CounterConfig struct {
	Value  *string `mapstructure:"value"`
	Action string  `mapstructure:"action"`
}

func validateCounterConfig(config *CounterConfig) error {
	if config.Action == "" {
		return errors.New(ErrCounterActionRequired)
	}
	config.Action = strings.ToLower(config.Action)
	if config.Action != CounterInc && config.Action != CounterAdd {
		return errors.Errorf(ErrCounterInvalidAction, config.Action)
	}
	return nil
}

func parseCounterConfig(config interface{}) (*CounterConfig, error) {
	cfg := &CounterConfig{}
	err := mapstructure.Decode(config, cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Counters is a vec tor of counters for a each log stream.
type Counters struct {
	*metricVec
	Cfg *CounterConfig
}

// NewCounters creates a new counter vec.
func NewCounters(name, help string, config interface{}) (*Counters, error) {
	cfg, err := parseCounterConfig(config)
	if err != nil {
		return nil, err
	}
	err = validateCounterConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Counters{
		metricVec: newMetricVec(func(labels map[string]string) prometheus.Metric {
			return prometheus.NewCounter(prometheus.CounterOpts{
				Help:        help,
				Name:        name,
				ConstLabels: labels,
			})
		}),
		Cfg: cfg,
	}, nil
}

// With returns the counter associated with a stream labelset.
func (c *Counters) With(labels model.LabelSet) prometheus.Counter {
	return c.metricVec.With(labels).(prometheus.Counter)
}
