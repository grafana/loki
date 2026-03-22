package metric

import (
	"strings"
	"time"

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
func NewGauges(name, help string, config interface{}, maxIdleSec int64) (*Gauges, error) {
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
			return &expiringGauge{prometheus.NewGauge(prometheus.GaugeOpts{
				Help:        help,
				Name:        name,
				ConstLabels: labels,
			}),
				0,
			}
		}, maxIdleSec),
		Cfg: cfg,
	}, nil
}

// With returns the gauge associated with a stream labelset.
func (g *Gauges) With(labels model.LabelSet) prometheus.Gauge {
	return g.metricVec.With(labels).(prometheus.Gauge)
}

type expiringGauge struct {
	prometheus.Gauge
	lastModSec int64
}

// Set sets the Gauge to an arbitrary value.
func (g *expiringGauge) Set(val float64) {
	g.Gauge.Set(val)
	g.lastModSec = time.Now().Unix()
}

// Inc increments the Gauge by 1. Use Add to increment it by arbitrary
// values.
func (g *expiringGauge) Inc() {
	g.Gauge.Inc()
	g.lastModSec = time.Now().Unix()
}

// Dec decrements the Gauge by 1. Use Sub to decrement it by arbitrary
// values.
func (g *expiringGauge) Dec() {
	g.Gauge.Dec()
	g.lastModSec = time.Now().Unix()
}

// Add adds the given value to the Gauge. (The value can be negative,
// resulting in a decrease of the Gauge.)
func (g *expiringGauge) Add(val float64) {
	g.Gauge.Add(val)
	g.lastModSec = time.Now().Unix()
}

// Sub subtracts the given value from the Gauge. (The value can be
// negative, resulting in an increase of the Gauge.)
func (g *expiringGauge) Sub(val float64) {
	g.Gauge.Sub(val)
	g.lastModSec = time.Now().Unix()
}

// SetToCurrentTime sets the Gauge to the current Unix time in seconds.
func (g *expiringGauge) SetToCurrentTime() {
	g.Gauge.SetToCurrentTime()
	g.lastModSec = time.Now().Unix()
}

// HasExpired implements Expirable
func (g *expiringGauge) HasExpired(currentTimeSec int64, maxAgeSec int64) bool {
	return currentTimeSec-g.lastModSec >= maxAgeSec
}
