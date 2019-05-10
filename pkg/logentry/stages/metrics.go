package stages

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/logentry/metric"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/prometheus/common/model"
)

const (
	MetricTypeCounter   = "counter"
	MetricTypeGauge     = "gauge"
	MetricTypeHistogram = "histogram"
)

type MetricConfig struct {
	MetricType  string    `mapstructure:"type"`
	Description string    `mapstructure:"description"`
	Source      *string   `mapstructure:"source"`
	Buckets     []float64 `mapstructure:"buckets"`
}

type MetricsConfig map[string]MetricConfig

type Valuer interface {
	Value(source *string) (interface{}, error)
}

type StageValuer interface {
	Process(labels model.LabelSet, time *time.Time, entry *string) Valuer
}

func withMetric(s StageValuer, cfg MetricsConfig, registry prometheus.Registerer) Stage {
	if registry == nil {
		return StageFunc(func(labels model.LabelSet, time *time.Time, entry *string) {
			_ = s.Process(labels, time, entry)
		})
	}
	metricStage := newMetric(cfg, registry)
	return StageFunc(func(labels model.LabelSet, time *time.Time, entry *string) {
		valuer := s.Process(labels, time, entry)
		if valuer != nil {
			metricStage.process(valuer, labels)
		}
	})
}

func newMetric(cfgs MetricsConfig, registry prometheus.Registerer) *metricStage {
	metrics := map[string]prometheus.Collector{}
	for name, config := range cfgs {
		var collector prometheus.Collector

		switch strings.ToLower(config.MetricType) {
		case MetricTypeCounter:
			collector = metric.NewCounters(name, config.Description)
		case MetricTypeGauge:
			collector = metric.NewGauges(name, config.Description)
		case MetricTypeHistogram:
			collector = metric.NewHistograms(name, config.Description, config.Buckets)
		}
		if collector != nil {
			registry.MustRegister(collector)
			metrics[name] = collector
		}
	}
	return &metricStage{
		cfg:     cfgs,
		metrics: metrics,
	}
}

type metricStage struct {
	cfg     MetricsConfig
	metrics map[string]prometheus.Collector
}

func (m *metricStage) process(v Valuer, labels model.LabelSet) {
	for name, collector := range m.metrics {
		switch vec := collector.(type) {
		case *metric.Counters:
			recordCounter(vec.With(labels), v, m.cfg[name])
		case *metric.Gauges:
			recordGauge(vec.With(labels), v, m.cfg[name])
		case *metric.Histograms:
			recordHistogram(vec.With(labels), v, m.cfg[name])
		}
	}
}

func recordCounter(counter prometheus.Counter, v Valuer, cfg MetricConfig) {
	unk, err := v.Value(cfg.Source)
	if err != nil {
		return
	}
	f, err := getFloat(unk)
	if err != nil || f < 0 {
		return
	}
	counter.Add(f)
}

func recordGauge(gauge prometheus.Gauge, v Valuer, cfg MetricConfig) {
	unk, err := v.Value(cfg.Source)
	if err != nil {
		return
	}
	f, err := getFloat(unk)
	if err != nil {
		return
	}
	gauge.Add(f)
}

func recordHistogram(histogram prometheus.Histogram, v Valuer, cfg MetricConfig) {
	unk, err := v.Value(cfg.Source)
	if err != nil {
		return
	}
	f, err := getFloat(unk)
	if err != nil {
		return
	}
	histogram.Observe(f)
}

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
		return math.NaN(), fmt.Errorf("Can't convert %v to float64", unk)
	}
}
