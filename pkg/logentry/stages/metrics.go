package stages

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logentry/metric"
)

const (
	MetricTypeCounter   = "counter"
	MetricTypeGauge     = "gauge"
	MetricTypeHistogram = "histogram"

	ErrEmptyMetricsStageConfig = "empty metric stage configuration"
	ErrMetricsStageInvalidType = "invalid metric type '%s', metric type must be one of 'counter', 'gauge', or 'histogram'"
	ErrInvalidIdleDur          = "max_idle_duration could not be parsed as a time.Duration: '%s'"
	ErrSubSecIdleDur           = "max_idle_duration less than 1s not allowed"
)

// MetricConfig is a single metrics configuration.
type MetricConfig struct {
	MetricType   string  `mapstructure:"type"`
	Description  string  `mapstructure:"description"`
	Source       *string `mapstructure:"source"`
	Prefix       string  `mapstructure:"prefix"`
	IdleDuration *string `mapstructure:"max_idle_duration"`
	maxIdleSec   int64
	Config       interface{} `mapstructure:"config"`
}

// MetricsConfig is a set of configured metrics.
type MetricsConfig map[string]MetricConfig

func validateMetricsConfig(cfg MetricsConfig) error {
	if cfg == nil {
		return errors.New(ErrEmptyMetricsStageConfig)
	}
	for name, config := range cfg {
		//If the source is not defined, default to the metric name
		if config.Source == nil {
			cp := cfg[name]
			nm := name
			cp.Source = &nm
			cfg[name] = cp
		}

		config.MetricType = strings.ToLower(config.MetricType)
		if config.MetricType != MetricTypeCounter &&
			config.MetricType != MetricTypeGauge &&
			config.MetricType != MetricTypeHistogram {
			return errors.Errorf(ErrMetricsStageInvalidType, config.MetricType)
		}

		// Set the idle duration for metrics
		if config.IdleDuration != nil {
			d, err := time.ParseDuration(*config.IdleDuration)
			if err != nil {
				return errors.Errorf(ErrInvalidIdleDur, err)
			}
			if d < 1*time.Second {
				return errors.New(ErrSubSecIdleDur)
			}
			cp := cfg[name]
			cp.maxIdleSec = int64(d.Seconds())
			cfg[name] = cp
		} else {
			cp := cfg[name]
			cp.maxIdleSec = int64(5 * time.Minute.Seconds())
			cfg[name] = cp
		}
	}
	return nil
}

// newMetricStage creates a new set of metrics to process for each log entry
func newMetricStage(logger log.Logger, config interface{}, registry prometheus.Registerer) (*metricStage, error) {
	cfgs := &MetricsConfig{}
	err := mapstructure.Decode(config, cfgs)
	if err != nil {
		return nil, err
	}
	err = validateMetricsConfig(*cfgs)
	if err != nil {
		return nil, err
	}
	metrics := map[string]prometheus.Collector{}
	for name, cfg := range *cfgs {
		var collector prometheus.Collector

		customPrefix := ""
		if cfg.Prefix != "" {
			customPrefix = cfg.Prefix
		} else {
			customPrefix = "promtail_custom_"
		}

		switch strings.ToLower(cfg.MetricType) {
		case MetricTypeCounter:
			collector, err = metric.NewCounters(customPrefix+name, cfg.Description, cfg.Config, cfg.maxIdleSec)
			if err != nil {
				return nil, err
			}
		case MetricTypeGauge:
			collector, err = metric.NewGauges(customPrefix+name, cfg.Description, cfg.Config, cfg.maxIdleSec)
			if err != nil {
				return nil, err
			}
		case MetricTypeHistogram:
			collector, err = metric.NewHistograms(customPrefix+name, cfg.Description, cfg.Config, cfg.maxIdleSec)
			if err != nil {
				return nil, err
			}
		}
		if collector != nil {
			registry.MustRegister(collector)
			metrics[name] = collector
		}
	}
	return &metricStage{
		logger:  logger,
		cfg:     *cfgs,
		metrics: metrics,
	}, nil
}

// metricStage creates and updates prometheus metrics based on extracted pipeline data
type metricStage struct {
	logger  log.Logger
	cfg     MetricsConfig
	metrics map[string]prometheus.Collector
}

// Process implements Stage
func (m *metricStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	if _, ok := labels[dropLabel]; ok {
		return
	}
	for name, collector := range m.metrics {
		// There is a special case for counters where we count even if there is no match in the extracted map.
		if c, ok := collector.(*metric.Counters); ok {
			if c != nil && c.Cfg.MatchAll != nil && *c.Cfg.MatchAll {
				if c.Cfg.CountBytes != nil && *c.Cfg.CountBytes {
					if entry != nil {
						m.recordCounter(name, c, labels, len(*entry))
					}
				} else {
					m.recordCounter(name, c, labels, nil)
				}
				continue
			}
		}
		if v, ok := extracted[*m.cfg[name].Source]; ok {
			switch vec := collector.(type) {
			case *metric.Counters:
				m.recordCounter(name, vec, labels, v)
			case *metric.Gauges:
				m.recordGauge(name, vec, labels, v)
			case *metric.Histograms:
				m.recordHistogram(name, vec, labels, v)
			}
		} else {
			level.Debug(m.logger).Log("msg", "source does not exist", "err", fmt.Sprintf("source: %s, does not exist", *m.cfg[name].Source))
		}
	}
}

// Name implements Stage
func (m *metricStage) Name() string {
	return StageTypeMetric
}

// recordCounter will update a counter metric
func (m *metricStage) recordCounter(name string, counter *metric.Counters, labels model.LabelSet, v interface{}) {
	// If value matching is defined, make sure value matches.
	if counter.Cfg.Value != nil {
		stringVal, err := getString(v)
		if err != nil {
			if Debug {
				level.Debug(m.logger).Log("msg", "failed to convert extracted value to string, "+
					"can't perform value comparison", "metric", name, "err",
					fmt.Sprintf("can't convert %v to string", reflect.TypeOf(v)))
			}
			return
		}
		if *counter.Cfg.Value != stringVal {
			return
		}
	}

	switch counter.Cfg.Action {
	case metric.CounterInc:
		counter.With(labels).Inc()
	case metric.CounterAdd:
		f, err := getFloat(v)
		if err != nil || f < 0 {
			if Debug {
				level.Debug(m.logger).Log("msg", "failed to convert extracted value to positive float", "metric", name, "err", err)
			}
			return
		}
		counter.With(labels).Add(f)
	}
}

// recordGauge will update a gauge metric
func (m *metricStage) recordGauge(name string, gauge *metric.Gauges, labels model.LabelSet, v interface{}) {
	// If value matching is defined, make sure value matches.
	if gauge.Cfg.Value != nil {
		stringVal, err := getString(v)
		if err != nil {
			if Debug {
				level.Debug(m.logger).Log("msg", "failed to convert extracted value to string, "+
					"can't perform value comparison", "metric", name, "err",
					fmt.Sprintf("can't convert %v to string", reflect.TypeOf(v)))
			}
			return
		}
		if *gauge.Cfg.Value != stringVal {
			return
		}
	}

	switch gauge.Cfg.Action {
	case metric.GaugeSet:
		f, err := getFloat(v)
		if err != nil || f < 0 {
			if Debug {
				level.Debug(m.logger).Log("msg", "failed to convert extracted value to positive float", "metric", name, "err", err)
			}
			return
		}
		gauge.With(labels).Set(f)
	case metric.GaugeInc:
		gauge.With(labels).Inc()
	case metric.GaugeDec:
		gauge.With(labels).Dec()
	case metric.GaugeAdd:
		f, err := getFloat(v)
		if err != nil || f < 0 {
			if Debug {
				level.Debug(m.logger).Log("msg", "failed to convert extracted value to positive float", "metric", name, "err", err)
			}
			return
		}
		gauge.With(labels).Add(f)
	case metric.GaugeSub:
		f, err := getFloat(v)
		if err != nil || f < 0 {
			if Debug {
				level.Debug(m.logger).Log("msg", "failed to convert extracted value to positive float", "metric", name, "err", err)
			}
			return
		}
		gauge.With(labels).Sub(f)
	}
}

// recordHistogram will update a Histogram metric
func (m *metricStage) recordHistogram(name string, histogram *metric.Histograms, labels model.LabelSet, v interface{}) {
	// If value matching is defined, make sure value matches.
	if histogram.Cfg.Value != nil {
		stringVal, err := getString(v)
		if err != nil {
			if Debug {
				level.Debug(m.logger).Log("msg", "failed to convert extracted value to string, "+
					"can't perform value comparison", "metric", name, "err",
					fmt.Sprintf("can't convert %v to string", reflect.TypeOf(v)))
			}
			return
		}
		if *histogram.Cfg.Value != stringVal {
			return
		}
	}
	f, err := getFloat(v)
	if err != nil {
		if Debug {
			level.Debug(m.logger).Log("msg", "failed to convert extracted value to float", "metric", name, "err", err)
		}
		return
	}
	histogram.With(labels).Observe(f)
}

// getFloat will take the provided value and return a float64 if possible
func getFloat(unk interface{}) (float64, error) {

	switch i := unk.(type) {
	case float64:
		return i, nil
	case float32:
		return float64(i), nil
	case int64:
		return float64(i), nil
	case int32:
		return float64(i), nil
	case int:
		return float64(i), nil
	case uint64:
		return float64(i), nil
	case uint32:
		return float64(i), nil
	case uint:
		return float64(i), nil
	case string:
		return strconv.ParseFloat(i, 64)
	case bool:
		if i {
			return float64(1), nil
		}
		return float64(0), nil
	default:
		return math.NaN(), fmt.Errorf("can't convert %v to float64", unk)
	}
}
