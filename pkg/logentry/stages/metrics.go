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

func withMetric(s Mutator, cfg MetricsConfig, registry prometheus.Registerer) Stage {
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
	for name, cfg := range cfgs {
		var collector prometheus.Collector

		switch strings.ToLower(cfg.MetricType) {
		case MetricTypeCounter:
			collector = metric.NewCounters(name, cfg.Description)
		case MetricTypeGauge:
			collector = metric.NewGauges(name, cfg.Description)
		case MetricTypeHistogram:
			collector = metric.NewHistograms(name, cfg.Description, cfg.Buckets)
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

func (m *metricStage) process(v Extractor, labels model.LabelSet) {
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

func recordCounter(counter prometheus.Counter, e Extractor, cfg MetricConfig) {
	unk, err := e.Value(cfg.Source)
	if err != nil {
		return
	}
	f, err := getFloat(unk)
	if err != nil || f < 0 {
		return
	}
	counter.Add(f)
}

func recordGauge(gauge prometheus.Gauge, e Extractor, cfg MetricConfig) {
	unk, err := e.Value(cfg.Source)
	if err != nil {
		return
	}
	f, err := getFloat(unk)
	if err != nil {
		return
	}
	//todo Gauge we be able to add,inc,dec,set
	gauge.Add(f)
}

func recordHistogram(histogram prometheus.Histogram, e Extractor, cfg MetricConfig) {
	unk, err := e.Value(cfg.Source)
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
