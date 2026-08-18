//go:build integration

package integration

import (
	"errors"
	"fmt"
	"strings"

	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/util"
)

var (
	ErrNoMetricFound     = fmt.Errorf("metric not found")
	ErrInvalidMetricType = fmt.Errorf("invalid metric type")
)

func extractMetricFamily(name, metrics string) (*io_prometheus_client.MetricFamily, error) {
	parser := expfmt.NewTextParser(model.UTF8Validation)
	mfs, err := parser.TextToMetricFamilies(strings.NewReader(metrics))
	if err != nil {
		return nil, err
	}

	mf, ok := mfs[name]
	if !ok {
		return nil, ErrNoMetricFound
	}
	return mf, nil
}

// sumCounter returns the sum of every label series of the named counter metric family, using
// util.MetricFamilyMap.SumCounters. It returns 0 for a metric that is not present.
func sumCounter(metricName, metrics string) (float64, error) {
	parser := expfmt.NewTextParser(model.UTF8Validation)
	mfs, err := parser.TextToMetricFamilies(strings.NewReader(metrics))
	if err != nil {
		return 0, err
	}
	return util.MetricFamilyMap(mfs).SumCounters(metricName), nil
}

// histogramSampleCountForOutcome returns the observation count of the named histogram metric family
// for the series carrying outcome="<outcome>". It returns 0 when the family or that series is absent
// (so a never-observed outcome reads as 0, not an error).
func histogramSampleCountForOutcome(metrics, name, outcome string) (uint64, error) {
	mf, err := extractMetricFamily(name, metrics)
	if errors.Is(err, ErrNoMetricFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if mf.GetType() != io_prometheus_client.MetricType_HISTOGRAM {
		return 0, ErrInvalidMetricType
	}
	for _, m := range mf.GetMetric() {
		for _, l := range m.GetLabel() {
			if l.GetName() == "outcome" && l.GetValue() == outcome {
				return m.GetHistogram().GetSampleCount(), nil
			}
		}
	}
	return 0, nil
}

func extractMetric(metricName, metrics string) (float64, map[string]string, error) {
	mf, err := extractMetricFamily(metricName, metrics)
	if err != nil {
		return 0, nil, err
	}

	var val float64
	switch mf.GetType() {
	case io_prometheus_client.MetricType_COUNTER:
		val = *mf.Metric[0].Counter.Value
	case io_prometheus_client.MetricType_GAUGE:
		val = *mf.Metric[0].Gauge.Value
	default:
		return 0, nil, ErrInvalidMetricType
	}

	labels := make(map[string]string)
	for _, l := range mf.Metric[0].Label {
		labels[*l.Name] = *l.Value
	}

	return val, labels, nil
}
