//go:build integration

package integration

import (
	"fmt"
	"strings"

	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

var (
	ErrNoMetricFound     = fmt.Errorf("metric not found")
	ErrInvalidMetricType = fmt.Errorf("invalid metric type")
)

func extractMetricFamily(name, metrics string) (*io_prometheus_client.MetricFamily, error) {
	var parser expfmt.TextParser
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
