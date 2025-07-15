package stages

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/logentry/metric"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
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
      prefix: my_promtail_custom_
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
    total_lines_count:
      type: Counter
      description: nothing to see here...
      config:
        match_all: true
        action: inc
    total_bytes_count:
      type: Counter
      description: nothing to see here...
      config:
        match_all: true
        count_entry_bytes: true
        action: add
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

var testMetricLogLineWithMissingKey = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"payload": 20,
	"component": ["parser","type"],
	"level" : "WARN"
}
`

const expectedMetrics = `# HELP my_promtail_custom_loki_count uhhhhhhh
# TYPE my_promtail_custom_loki_count counter
my_promtail_custom_loki_count{test="app"} 1
# HELP promtail_custom_bloki_count blerrrgh
# TYPE promtail_custom_bloki_count gauge
promtail_custom_bloki_count{test="app"} -1
# HELP promtail_custom_payload_size_bytes grrrragh
# TYPE promtail_custom_payload_size_bytes histogram
promtail_custom_payload_size_bytes_bucket{test="app",le="10"} 1
promtail_custom_payload_size_bytes_bucket{test="app",le="20"} 2
promtail_custom_payload_size_bytes_bucket{test="app",le="+Inf"} 2
promtail_custom_payload_size_bytes_sum{test="app"} 30
promtail_custom_payload_size_bytes_count{test="app"} 2
# HELP promtail_custom_total_bytes_count nothing to see here...
# TYPE promtail_custom_total_bytes_count counter
promtail_custom_total_bytes_count{test="app"} 255
# HELP promtail_custom_total_lines_count nothing to see here...
# TYPE promtail_custom_total_lines_count counter
promtail_custom_total_lines_count{test="app"} 2
`

func TestMetricsPipeline(t *testing.T) {
	registry := prometheus.NewRegistry()
	pl, err := NewPipeline(util_log.Logger, loadConfig(testMetricYaml), nil, registry)
	if err != nil {
		t.Fatal(err)
	}

	out := <-pl.Run(withInboundEntries(newEntry(nil, model.LabelSet{"test": "app"}, testMetricLogLine1, time.Now())))
	out.Line = testMetricLogLine2
	<-pl.Run(withInboundEntries(out))

	if err := testutil.GatherAndCompare(registry,
		strings.NewReader(expectedMetrics)); err != nil {
		t.Fatalf("mismatch metrics: %v", err)
	}

	pl.Cleanup()

	if err := testutil.GatherAndCompare(registry,
		strings.NewReader("")); err != nil {
		t.Fatalf("mismatch metrics: %v", err)
	}
}

func TestNegativeGauge(t *testing.T) {
	registry := prometheus.NewRegistry()
	testConfig := `
pipeline_stages:
- regex:
    expression: 'vehicle=(?P<vehicle>\d+) longitude=(?P<longitude>[-]?\d+\.\d+) latitude=(?P<latitude>\d+\.\d+)'
- labels:
    vehicle:
- metrics:
    longitude:
        type: Gauge
        description: "longitude GPS vehicle"
        source: longitude
        config:
          match_all: true
          action: set

`
	pl, err := NewPipeline(util_log.Logger, loadConfig(testConfig), nil, registry)
	if err != nil {
		t.Fatal(err)
	}

	<-pl.Run(withInboundEntries(newEntry(nil, model.LabelSet{"test": "app"}, `#<13>Jan 28 14:25:52 vehicle=1 longitude=-10.1234 latitude=15.1234`, time.Now())))
	if err := testutil.GatherAndCompare(registry,
		strings.NewReader(`
# HELP promtail_custom_longitude longitude GPS vehicle
# TYPE promtail_custom_longitude gauge
promtail_custom_longitude{test="app",vehicle="1"} -10.1234
`)); err != nil {
		t.Fatalf("mismatch metrics: %v", err)
	}
}

func TestPipelineWithMissingKey_Metrics(t *testing.T) {
	var buf bytes.Buffer
	w := log.NewSyncWriter(&buf)
	logger := log.NewLogfmtLogger(w)
	pl, err := NewPipeline(logger, loadConfig(testMetricYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}
	Debug = true
	processEntries(pl, newEntry(nil, nil, testMetricLogLineWithMissingKey, time.Now()))
	expectedLog := "level=debug msg=\"failed to convert extracted value to string, can't perform value comparison\" metric=bloki_count err=\"can't convert <nil> to string\""
	if !(strings.Contains(buf.String(), expectedLog)) {
		t.Errorf("\nexpected: %s\n+actual: %s", expectedLog, buf.String())
	}
}

var testMetricWithDropYaml = `
pipeline_stages:
- json:
    expressions:
      app: app
      drop: drop
- match:
    selector: '{drop="true"}'
    action: drop
- metrics:
    loki_count:
      type: Counter
      source: app
      description: "should only inc on non dropped labels"
      config:
        action: inc
`

const expectedDropMetrics = `# HELP logentry_dropped_lines_total A count of all log lines dropped as a result of a pipeline stage
# TYPE logentry_dropped_lines_total counter
logentry_dropped_lines_total{reason="match_stage"} 1
# HELP promtail_custom_loki_count should only inc on non dropped labels
# TYPE promtail_custom_loki_count counter
promtail_custom_loki_count 1
`

func TestMetricsWithDropInPipeline(t *testing.T) {
	registry := prometheus.NewRegistry()
	pl, err := NewPipeline(util_log.Logger, loadConfig(testMetricWithDropYaml), nil, registry)
	if err != nil {
		t.Fatal(err)
	}
	lbls := model.LabelSet{}
	droppingLabels := model.LabelSet{
		"drop": "true",
	}
	in := make(chan Entry)
	out := pl.Run(in)

	in <- newEntry(nil, lbls, testMetricLogLine1, time.Now())
	e := <-out
	e.Labels = droppingLabels
	e.Line = testMetricLogLine2
	in <- e
	close(in)
	<-out

	if err := testutil.GatherAndCompare(registry,
		strings.NewReader(expectedDropMetrics)); err != nil {
		t.Fatalf("mismatch metrics: %v", err)
	}
}

var testMetricWithNonPromLabel = `
pipeline_stages:
- static_labels:
    good_label: "1"
- metrics:
    loki_count:
      type: Counter
      source: app
      description: "should count all entries"
      config:
        match_all: true
        action: inc
`

func TestNonPrometheusLabelsShouldBeDropped(t *testing.T) {
	const counterConfig = `
pipeline_stages:
- static_labels:
    good_label: "1"
- tenant:
    value: "2"
- metrics:
    loki_count:
      type: Counter
      source: app
      description: "should count all entries"
      config:
        match_all: true
        action: inc
`

	const expectedCounterMetrics = `# HELP promtail_custom_loki_count should count all entries
# TYPE promtail_custom_loki_count counter
promtail_custom_loki_count{good_label="1"} 1
`

	const gaugeConfig = `
pipeline_stages:
- regex:
    expression: 'vehicle=(?P<vehicle>\d+) longitude=(?P<longitude>[-]?\d+\.\d+) latitude=(?P<latitude>\d+\.\d+)'
- labels:
    vehicle:
- metrics:
    longitude:
        type: Gauge
        description: "longitude GPS vehicle"
        source: longitude
        config:
          match_all: true
          action: set
`

	const expectedGaugeMetrics = `# HELP promtail_custom_longitude longitude GPS vehicle
# TYPE promtail_custom_longitude gauge
promtail_custom_longitude{vehicle="1"} -10.1234
`

	const histogramConfig = `
pipeline_stages:
- json:
    expressions:
      payload: payload
- metrics:
    payload_size_bytes:
      type: Histogram
      description: payload size in bytes
      source: payload
      config:
        buckets: [10, 20]
`

	const expectedHistogramMetrics = `# HELP promtail_custom_payload_size_bytes payload size in bytes
# TYPE promtail_custom_payload_size_bytes histogram
promtail_custom_payload_size_bytes_bucket{test="app",le="10"} 1
promtail_custom_payload_size_bytes_bucket{test="app",le="20"} 1
promtail_custom_payload_size_bytes_bucket{test="app",le="+Inf"} 1
promtail_custom_payload_size_bytes_sum{test="app"} 10
promtail_custom_payload_size_bytes_count{test="app"} 1
`
	for name, tc := range map[string]struct {
		promtailConfig  string
		labels          model.LabelSet
		line            string
		expectedCollect string
	}{
		"counter metric with non-prometheus incoming label": {
			promtailConfig: testMetricWithNonPromLabel,
			labels: model.LabelSet{
				"__bad_label__": "2",
			},
			line:            testMetricLogLine1,
			expectedCollect: expectedCounterMetrics,
		},
		"counter metric with tenant step injected label": {
			promtailConfig:  counterConfig,
			line:            testMetricLogLine1,
			expectedCollect: expectedCounterMetrics,
		},
		"gauge metric with non-prometheus incoming label": {
			promtailConfig: gaugeConfig,
			labels: model.LabelSet{
				"__bad_label__": "2",
			},
			line:            `#<13>Jan 28 14:25:52 vehicle=1 longitude=-10.1234 latitude=15.1234`,
			expectedCollect: expectedGaugeMetrics,
		},
		"histogram metric with non-prometheus incoming label": {
			promtailConfig: histogramConfig,
			labels: model.LabelSet{
				"test":          "app",
				"__bad_label__": "2",
			},
			line:            testMetricLogLine1,
			expectedCollect: expectedHistogramMetrics,
		},
	} {
		t.Run(name, func(t *testing.T) {
			registry := prometheus.NewRegistry()
			pl, err := NewPipeline(util_log.Logger, loadConfig(tc.promtailConfig), nil, registry)
			require.NoError(t, err)
			in := make(chan Entry)
			out := pl.Run(in)

			in <- newEntry(nil, tc.labels, tc.line, time.Now())
			close(in)
			<-out

			err = testutil.GatherAndCompare(registry, strings.NewReader(tc.expectedCollect))
			require.NoError(t, err, "gathered metrics are different than expected")
		})
	}
}

var metricTestInvalidIdle = "10f"

func TestValidateMetricsConfig(t *testing.T) {
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
					MetricType: "Pipe_line",
				},
			},
			errors.Errorf(ErrMetricsStageInvalidType, "pipe_line"),
		},
		"invalid idle duration": {
			MetricsConfig{
				"metric1": MetricConfig{
					MetricType:   "Counter",
					IdleDuration: &metricTestInvalidIdle,
				},
			},
			errors.Errorf(ErrInvalidIdleDur, `time: unknown unit "f" in duration "10f"`),
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
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := validateMetricsConfig(test.config)
			if ((err != nil) && (err.Error() != test.err.Error())) || (err == nil && test.err != nil) {
				t.Errorf("Metrics stage validation error, expected error = %v, actual error = %v", test.err, err)
				return
			}
		})
	}
}

func TestDefaultIdleDuration(t *testing.T) {
	registry := prometheus.NewRegistry()
	metricsConfig := MetricsConfig{
		"total_keys": MetricConfig{
			MetricType:  "Counter",
			Description: "the total keys per doc",
			Config: metric.CounterConfig{
				Action: metric.CounterAdd,
			},
		},
	}
	ms, err := New(util_log.Logger, nil, StageTypeMetric, metricsConfig, registry)
	if err != nil {
		t.Fatalf("failed to create stage with metrics: %v", err)
	}
	assert.Equal(t, int64(5*time.Minute.Seconds()), ms.(*metricStage).cfg["total_keys"].maxIdleSec)
}

var (
	labelFoo = model.LabelSet(map[model.LabelName]model.LabelValue{"foo": "bar", "bar": "foo"})
	labelFu  = model.LabelSet(map[model.LabelName]model.LabelValue{"fu": "baz", "baz": "fu"})
)

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
	regexHTTPFixture := `11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932ms"`
	regexConfig := map[string]interface{}{
		"expression": "(?P<get>\"GET).*HTTP/1.1\" (?P<status>\\d*) (?P<time>\\d*ms)",
	}
	timeSource := "time"
	tru := "true"
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
				Value:  &tru,
				Action: metric.CounterInc,
			},
		},
		"contains_false": MetricConfig{
			MetricType:  "Counter",
			Description: "contains_false",
			Config: metric.CounterConfig{
				Value:  &tru,
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
		"response_time_seconds": MetricConfig{
			MetricType:  "Histogram",
			Source:      &timeSource,
			Description: "response time in ms",
			Config: metric.HistogramConfig{
				Buckets: []float64{0.5, 1, 2},
			},
		},
	}

	registry := prometheus.NewRegistry()
	jsonStage, err := New(util_log.Logger, nil, StageTypeJSON, jsonConfig, registry)
	if err != nil {
		t.Fatalf("failed to create stage with metrics: %v", err)
	}
	regexStage, err := New(util_log.Logger, nil, StageTypeRegex, regexConfig, registry)
	if err != nil {
		t.Fatalf("failed to create stage with metrics: %v", err)
	}
	metricStage, err := New(util_log.Logger, nil, StageTypeMetric, metricsConfig, registry)
	if err != nil {
		t.Fatalf("failed to create stage with metrics: %v", err)
	}
	out := processEntries(jsonStage, newEntry(nil, labelFoo, logFixture, time.Now()))
	out[0].Line = regexHTTPFixture
	out = processEntries(regexStage, out...)
	out = processEntries(metricStage, out...)
	out[0].Labels = labelFu
	// Process the same extracted values again with different labels so we can verify proper metric/label assignments
	_ = processEntries(metricStage, out...)
	names := metricNames(metricsConfig)
	if err := testutil.GatherAndCompare(registry,
		strings.NewReader(goldenMetrics), names...); err != nil {
		t.Fatalf("mismatch metrics: %v", err)
	}
}

func metricNames(cfg MetricsConfig) []string {
	result := make([]string, 0, len(cfg))
	for name, config := range cfg {
		customPrefix := ""
		if config.Prefix != "" {
			customPrefix = config.Prefix
		} else {
			customPrefix = "promtail_custom_"
		}
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
# HELP promtail_custom_response_time_seconds response time in ms
# TYPE promtail_custom_response_time_seconds histogram
promtail_custom_response_time_seconds_bucket{bar="foo",foo="bar",le="0.5"} 0
promtail_custom_response_time_seconds_bucket{bar="foo",foo="bar",le="1"} 1
promtail_custom_response_time_seconds_bucket{bar="foo",foo="bar",le="2"} 1
promtail_custom_response_time_seconds_bucket{bar="foo",foo="bar",le="+Inf"} 1
promtail_custom_response_time_seconds_sum{bar="foo",foo="bar"} 0.932
promtail_custom_response_time_seconds_count{bar="foo",foo="bar"} 1
promtail_custom_response_time_seconds_bucket{baz="fu",fu="baz",le="0.5"} 0
promtail_custom_response_time_seconds_bucket{baz="fu",fu="baz",le="1"} 1
promtail_custom_response_time_seconds_bucket{baz="fu",fu="baz",le="2"} 1
promtail_custom_response_time_seconds_bucket{baz="fu",fu="baz",le="+Inf"} 1
promtail_custom_response_time_seconds_sum{baz="fu",fu="baz"} 0.932
promtail_custom_response_time_seconds_count{baz="fu",fu="baz"} 1.0
# HELP promtail_custom_total_keys the total keys per doc
# TYPE promtail_custom_total_keys counter
promtail_custom_total_keys{bar="foo",foo="bar"} 8.0
promtail_custom_total_keys{baz="fu",fu="baz"} 8.0
`
