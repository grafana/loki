package stages

import (
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logentry/metric"
)

var testMetricYaml = `
pipeline_stages:
- json:
    expressions:
      app: app
      payload: payload
- metrics:
    loki_count:
      type: Counter
      description: uhhhhhhh
      source: app
      config:
        value: loki
        action: inc
    bloki_count:
      type: Gauge
      description: blerrrgh
      source: app
      config:
        value: bloki
        action: dec
    payload_size_bytes:
      type: Histogram
      description: grrrragh
      source: payload
      config:
        buckets: [10, 20]
`

var testMetricLogLine1 = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"app":"loki",
    "payload": 10,
	"component": ["parser","type"],
	"level" : "WARN"
}
`
var testMetricLogLine2 = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"app":"bloki",
    "payload": 20,
	"component": ["parser","type"],
	"level" : "WARN"
}
`

const expectedMetrics = `# HELP promtail_custom_bloki_count blerrrgh
# TYPE promtail_custom_bloki_count gauge
promtail_custom_bloki_count -1.0
# HELP promtail_custom_loki_count uhhhhhhh
# TYPE promtail_custom_loki_count counter
promtail_custom_loki_count 1.0
# HELP promtail_custom_payload_size_bytes grrrragh
# TYPE promtail_custom_payload_size_bytes histogram
promtail_custom_payload_size_bytes_bucket{le="10.0"} 1.0
promtail_custom_payload_size_bytes_bucket{le="20.0"} 2.0
promtail_custom_payload_size_bytes_bucket{le="+Inf"} 2.0
promtail_custom_payload_size_bytes_sum 30.0
promtail_custom_payload_size_bytes_count 2.0
`

func TestMetricsPipeline(t *testing.T) {
	registry := prometheus.NewRegistry()
	pl, err := NewPipeline(util.Logger, loadConfig(testMetricYaml), nil, registry)
	if err != nil {
		t.Fatal(err)
	}
	lbls := model.LabelSet{}
	ts := time.Now()
	extracted := map[string]interface{}{}
	entry := testMetricLogLine1
	pl.Process(lbls, extracted, &ts, &entry)
	entry = testMetricLogLine2
	pl.Process(lbls, extracted, &ts, &entry)

	if err := testutil.GatherAndCompare(registry,
		strings.NewReader(expectedMetrics)); err != nil {
		t.Fatalf("missmatch metrics: %v", err)
	}
}

func Test(t *testing.T) {
	tests := map[string]struct {
		config MetricsConfig
		err    error
	}{
		"empty": {
			nil,
			errors.New(ErrEmptyMetricsStageConfig),
		},
		"invalid metric type": {
			MetricsConfig{
				"metric1": MetricConfig{
					MetricType: "Piplne",
				},
			},
			errors.Errorf(ErrMetricsStageInvalidType, "piplne"),
		},
		"valid": {
			MetricsConfig{
				"metric1": MetricConfig{
					MetricType:  "Counter",
					Description: "some description",
					Config: metric.CounterConfig{
						Action: "inc",
					},
				},
			},
			nil,
		},
	}

	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := validateMetricsConfig(test.config)
			if (err != nil) && (err.Error() != test.err.Error()) {
				t.Errorf("Metrics stage validation error, expected error = %v, actual error = %v", test.err, err)
				return
			}
		})
	}
}

var labelFoo = model.LabelSet(map[model.LabelName]model.LabelValue{"foo": "bar", "bar": "foo"})
var labelFu = model.LabelSet(map[model.LabelName]model.LabelValue{"fu": "baz", "baz": "fu"})

func TestMetricStage_Process(t *testing.T) {
	jsonConfig := JSONConfig{
		Expressions: map[string]string{
			"total_keys":      "length(keys(@))",
			"keys_per_line":   "length(keys(@))",
			"numeric_float":   "numeric.float",
			"numeric_integer": "numeric.integer",
			"numeric_string":  "numeric.string",
			"contains_warn":   "contains(values(@),'WARN')",
			"contains_false":  "contains(keys(@),'nope')",
		},
	}
	regexConfig := map[string]interface{}{
		"expression": "(?P<get>\"GET).*HTTP/1.1\" (?P<status>\\d*) (?P<time>\\d*) ",
	}
	timeSource := "time"
	true := "true"
	metricsConfig := MetricsConfig{
		"total_keys": MetricConfig{
			MetricType:  "Counter",
			Description: "the total keys per doc",
			Config: metric.CounterConfig{
				Action: metric.CounterAdd,
			},
		},
		"keys_per_line": MetricConfig{
			MetricType:  "Histogram",
			Description: "keys per doc",
			Config: metric.HistogramConfig{
				Buckets: []float64{1, 3, 5, 10},
			},
		},
		"numeric_float": MetricConfig{
			MetricType:  "Gauge",
			Description: "numeric_float",
			Config: metric.GaugeConfig{
				Action: metric.GaugeAdd,
			},
		},
		"numeric_integer": MetricConfig{
			MetricType:  "Gauge",
			Description: "numeric.integer",
			Config: metric.GaugeConfig{
				Action: metric.GaugeAdd,
			},
		},
		"numeric_string": MetricConfig{
			MetricType:  "Gauge",
			Description: "numeric.string",
			Config: metric.GaugeConfig{
				Action: metric.GaugeAdd,
			},
		},
		"contains_warn": MetricConfig{
			MetricType:  "Counter",
			Description: "contains_warn",
			Config: metric.CounterConfig{
				Value:  &true,
				Action: metric.CounterInc,
			},
		},
		"contains_false": MetricConfig{
			MetricType:  "Counter",
			Description: "contains_false",
			Config: metric.CounterConfig{
				Value:  &true,
				Action: metric.CounterAdd,
			},
		},
		"matches": MetricConfig{
			MetricType:  "Counter",
			Source:      &timeSource,
			Description: "all matches",
			Config: metric.CounterConfig{
				Action: metric.CounterInc,
			},
		},
		"response_time_ms": MetricConfig{
			MetricType:  "Histogram",
			Source:      &timeSource,
			Description: "response time in ms",
			Config: metric.HistogramConfig{
				Buckets: []float64{1, 2, 3},
			},
		},
	}

	registry := prometheus.NewRegistry()
	jsonStage, err := New(util.Logger, nil, StageTypeJSON, jsonConfig, registry)
	if err != nil {
		t.Fatalf("failed to create stage with metrics: %v", err)
	}
	regexStage, err := New(util.Logger, nil, StageTypeRegex, regexConfig, registry)
	if err != nil {
		t.Fatalf("failed to create stage with metrics: %v", err)
	}
	metricStage, err := New(util.Logger, nil, StageTypeMetric, metricsConfig, registry)
	if err != nil {
		t.Fatalf("failed to create stage with metrics: %v", err)
	}
	var ts = time.Now()
	var entry = logFixture
	extr := map[string]interface{}{}
	jsonStage.Process(labelFoo, extr, &ts, &entry)
	regexStage.Process(labelFoo, extr, &ts, &regexLogFixture)
	metricStage.Process(labelFoo, extr, &ts, &entry)
	// Process the same extracted values again with different labels so we can verify proper metric/label assignments
	metricStage.Process(labelFu, extr, &ts, &entry)

	names := metricNames(metricsConfig)
	if err := testutil.GatherAndCompare(registry,
		strings.NewReader(goldenMetrics), names...); err != nil {
		t.Fatalf("missmatch metrics: %v", err)
	}
}

func metricNames(cfg MetricsConfig) []string {
	result := make([]string, 0, len(cfg))
	for name := range cfg {
		result = append(result, customPrefix+name)
	}
	return result
}

const goldenMetrics = `# HELP promtail_custom_contains_warn contains_warn
# TYPE promtail_custom_contains_warn counter
promtail_custom_contains_warn{bar="foo",foo="bar"} 1.0
promtail_custom_contains_warn{baz="fu",fu="baz"} 1.0
# HELP promtail_custom_keys_per_line keys per doc
# TYPE promtail_custom_keys_per_line histogram
promtail_custom_keys_per_line_bucket{bar="foo",foo="bar",le="1.0"} 0.0
promtail_custom_keys_per_line_bucket{bar="foo",foo="bar",le="3.0"} 0.0
promtail_custom_keys_per_line_bucket{bar="foo",foo="bar",le="5.0"} 0.0
promtail_custom_keys_per_line_bucket{bar="foo",foo="bar",le="10.0"} 1.0
promtail_custom_keys_per_line_bucket{bar="foo",foo="bar",le="+Inf"} 1.0
promtail_custom_keys_per_line_sum{bar="foo",foo="bar"} 8.0
promtail_custom_keys_per_line_count{bar="foo",foo="bar"} 1.0
promtail_custom_keys_per_line_bucket{baz="fu",fu="baz",le="1.0"} 0.0
promtail_custom_keys_per_line_bucket{baz="fu",fu="baz",le="3.0"} 0.0
promtail_custom_keys_per_line_bucket{baz="fu",fu="baz",le="5.0"} 0.0
promtail_custom_keys_per_line_bucket{baz="fu",fu="baz",le="10.0"} 1.0
promtail_custom_keys_per_line_bucket{baz="fu",fu="baz",le="+Inf"} 1.0
promtail_custom_keys_per_line_sum{baz="fu",fu="baz"} 8.0
promtail_custom_keys_per_line_count{baz="fu",fu="baz"} 1.0
# HELP promtail_custom_matches all matches
# TYPE promtail_custom_matches counter
promtail_custom_matches{bar="foo",foo="bar"} 1.0
promtail_custom_matches{baz="fu",fu="baz"} 1.0
# HELP promtail_custom_numeric_float numeric_float
# TYPE promtail_custom_numeric_float gauge
promtail_custom_numeric_float{bar="foo",foo="bar"} 12.34
promtail_custom_numeric_float{baz="fu",fu="baz"} 12.34
# HELP promtail_custom_numeric_integer numeric.integer
# TYPE promtail_custom_numeric_integer gauge
promtail_custom_numeric_integer{bar="foo",foo="bar"} 123.0
promtail_custom_numeric_integer{baz="fu",fu="baz"} 123.0
# HELP promtail_custom_numeric_string numeric.string
# TYPE promtail_custom_numeric_string gauge
promtail_custom_numeric_string{bar="foo",foo="bar"} 123.0
promtail_custom_numeric_string{baz="fu",fu="baz"} 123.0
# HELP promtail_custom_response_time_ms response time in ms
# TYPE promtail_custom_response_time_ms histogram
promtail_custom_response_time_ms_bucket{bar="foo",foo="bar",le="1.0"} 0.0
promtail_custom_response_time_ms_bucket{bar="foo",foo="bar",le="2.0"} 0.0
promtail_custom_response_time_ms_bucket{bar="foo",foo="bar",le="3.0"} 0.0
promtail_custom_response_time_ms_bucket{bar="foo",foo="bar",le="+Inf"} 1.0
promtail_custom_response_time_ms_sum{bar="foo",foo="bar"} 932.0
promtail_custom_response_time_ms_count{bar="foo",foo="bar"} 1.0
promtail_custom_response_time_ms_bucket{baz="fu",fu="baz",le="1.0"} 0.0
promtail_custom_response_time_ms_bucket{baz="fu",fu="baz",le="2.0"} 0.0
promtail_custom_response_time_ms_bucket{baz="fu",fu="baz",le="3.0"} 0.0
promtail_custom_response_time_ms_bucket{baz="fu",fu="baz",le="+Inf"} 1.0
promtail_custom_response_time_ms_sum{baz="fu",fu="baz"} 932.0
promtail_custom_response_time_ms_count{baz="fu",fu="baz"} 1.0
# HELP promtail_custom_total_keys the total keys per doc
# TYPE promtail_custom_total_keys counter
promtail_custom_total_keys{bar="foo",foo="bar"} 8.0
promtail_custom_total_keys{baz="fu",fu="baz"} 8.0
`
