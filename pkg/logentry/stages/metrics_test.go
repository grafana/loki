package stages

import (
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
)

var labelFoo = model.LabelSet(map[model.LabelName]model.LabelValue{"foo": "bar", "bar": "foo"})
var labelFu = model.LabelSet(map[model.LabelName]model.LabelValue{"fu": "baz", "baz": "fu"})

func Test_withMetric(t *testing.T) {
	jsonConfig := JSONConfig{
		Expressions: map[string]string{
			"total_keys":      "length(keys(@))",
			"keys_per_line":   "length(keys(@))",
			"numeric_float":   "numeric.float",
			"numeric_integer": "numeric.integer",
			"numeric_string":  "numeric.string",
			"contains_warn":   "contains(values(@),'WARN')",
			"contains_false":  "contains(keys(@),'nope')",
			"unconvertable":   "values(@)",
		},
	}
	regexConfig := map[string]interface{}{
		"expression": "(?P<get>\"GET).*HTTP/1.1\" (?P<status>\\d*) (?P<time>\\d*) ",
	}
	timeSource := "time"
	metricsConfig := MetricsConfig{
		"total_keys": MetricConfig{
			MetricType:  "Counter",
			Description: "the total keys per doc",
		},
		"keys_per_line": MetricConfig{
			MetricType:  "Histogram",
			Description: "keys per doc",
			Buckets:     []float64{1, 3, 5, 10},
		},
		"numeric_float": MetricConfig{
			MetricType:  "Gauge",
			Description: "numeric_float",
		},
		"numeric_integer": MetricConfig{
			MetricType:  "Gauge",
			Description: "numeric.integer",
		},
		"numeric_string": MetricConfig{
			MetricType:  "Gauge",
			Description: "numeric.string",
		},
		"contains_warn": MetricConfig{
			MetricType:  "Counter",
			Description: "contains_warn",
		},
		"contains_false": MetricConfig{
			MetricType:  "Counter",
			Description: "contains_false",
		},
		// FIXME Not showing results currently if there are no counts, this doesn't make it into the output
		"unconvertible": MetricConfig{
			MetricType:  "Counter",
			Description: "unconvertible",
		},
		// FIXME Not entirely sure how we want to implement this one?
		"matches": MetricConfig{
			MetricType:  "Counter",
			Description: "all matches",
		},
		"response_time_ms": MetricConfig{
			MetricType:  "Histogram",
			Source:      &timeSource,
			Description: "response time in ms",
			Buckets:     []float64{1, 2, 3},
		},
	}

	registry := prometheus.NewRegistry()
	jsonStage, err := New(util.Logger, "test", StageTypeJSON, jsonConfig, registry)
	if err != nil {
		t.Fatalf("failed to create stage with metrics: %v", err)
	}
	regexStage, err := New(util.Logger, "test", StageTypeRegex, regexConfig, registry)
	if err != nil {
		t.Fatalf("failed to create stage with metrics: %v", err)
	}
	metricStage, err := New(util.Logger, "test", StageTypeMetric, metricsConfig, registry)
	if err != nil {
		t.Fatalf("failed to create stage with metrics: %v", err)
	}
	var ts = time.Now()
	var entry = logFixture
	extr := map[string]interface{}{}
	jsonStage.Process(labelFoo, extr, &ts, &entry)
	regexStage.Process(labelFoo, extr, &ts, &regexLogFixture)
	metricStage.Process(labelFoo, extr, &ts, &entry)

	//jsonStage.Process(labelFu, extr, &ts, &entry)
	//regexStage.Process(labelFu, extr, &ts, &regexLogFixture)
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

const goldenMetrics = `# HELP promtail_custom_contains_false contains_false
# TYPE promtail_custom_contains_false counter
promtail_custom_contains_false{bar="foo",foo="bar"} 0.0
promtail_custom_contains_false{baz="fu",fu="baz"} 0.0
# HELP promtail_custom_contains_warn contains_warn
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
promtail_custom_matches{bar="foo",foo="bar"} 3.0
promtail_custom_matches{baz="fu",fu="baz"} 3.0
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
# HELP promtail_custom_unconvertible unconvertible
# TYPE promtail_custom_unconvertible counter
promtail_custom_unconvertible{bar="foo",foo="bar"} 0.0
promtail_custom_unconvertible{baz="fu",fu="baz"} 0.0
`
