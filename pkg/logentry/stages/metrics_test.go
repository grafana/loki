package stages

import (
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
)

var labelFoo = model.LabelSet(map[model.LabelName]model.LabelValue{"foo": "bar", "bar": "foo"})
var labelFu = model.LabelSet(map[model.LabelName]model.LabelValue{"fu": "baz", "baz": "fu"})

func Test_withMetric(t *testing.T) {
	jsonConfig := map[string]interface{}{
		"metrics": map[string]interface{}{
			"total_keys": map[string]interface{}{
				"type":        "Counter",
				"source":      "length(keys(@))",
				"description": "the total keys per doc",
			},
			"keys_per_line": map[string]interface{}{
				"type":        "Histogram",
				"source":      "length(keys(@))",
				"description": "keys per doc",
				"buckets":     []float64{1, 3, 5, 10},
			},
			"numeric_float": map[string]interface{}{
				"type":        "Gauge",
				"source":      "numeric.float",
				"description": "numeric_float",
			},
			"numeric_integer": map[string]interface{}{
				"type":        "Gauge",
				"source":      "numeric.integer",
				"description": "numeric.integer",
			},
			"numeric_string": map[string]interface{}{
				"type":        "Gauge",
				"source":      "numeric.string",
				"description": "numeric.string",
			},
			"contains_warn": map[string]interface{}{
				"type":        "Counter",
				"source":      "contains(values(@),'WARN')",
				"description": "contains_warn",
			},
			"contains_false": map[string]interface{}{
				"type":        "Counter",
				"source":      "contains(keys(@),'nope')",
				"description": "contains_false",
			},
			"unconvertible": map[string]interface{}{
				"type":        "Counter",
				"source":      "values(@)",
				"description": "unconvertible",
			},
		},
	}
	regexConfig := map[string]interface{}{
		"expression": "(?P<get>\"GET).*HTTP/1.1\" (?P<status>\\d*) (?P<time>\\d*) ",
		"metrics": map[string]interface{}{
			"matches": map[string]interface{}{
				"type":        "Counter",
				"description": "all matches",
			},
			"response_time_ms": map[string]interface{}{
				"type":        "Histogram",
				"source":      "time",
				"description": "response time in ms",
				"buckets":     []float64{1, 2, 3},
			},
		},
	}
	registry := prometheus.NewRegistry()
	jsonStage, err := New(util.Logger, StageTypeJSON, jsonConfig, registry)
	if err != nil {
		t.Fatalf("failed to create stage with metrics: %v", err)
	}
	regexStage, err := New(util.Logger, StageTypeRegex, regexConfig, registry)
	if err != nil {
		t.Fatalf("failed to create stage with metrics: %v", err)
	}
	var ts = time.Now()
	var entry = logFixture

	jsonStage.Process(labelFoo, &ts, &entry)
	jsonStage.Process(labelFu, &ts, &entry)
	regexStage.Process(labelFoo, &ts, &regexLogFixture)
	regexStage.Process(labelFu, &ts, &regexLogFixture)

	var names []string
	names = append(names, metricNames(jsonConfig)...)
	names = append(names, metricNames(regexConfig)...)
	if err := testutil.GatherAndCompare(registry,
		strings.NewReader(goldenMetrics), names...); err != nil {
		t.Fatalf("missmatch metrics: %v", err)
	}
}

func metricNames(cfg map[string]interface{}) []string {
	metrics := cfg["metrics"].(map[string]interface{})
	result := make([]string, 0, len(cfg))
	for name := range metrics {
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
