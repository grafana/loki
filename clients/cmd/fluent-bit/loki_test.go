//go:build cgo

package main

import (
	"errors"
	"reflect"
	"testing"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"

	"github.com/grafana/loki/v3/pkg/logproto"
)

var now = time.Now()

func Test_loki_sendRecord(t *testing.T) {
	simpleRecordFixture := map[interface{}]interface{}{
		"foo":   "bar",
		"bar":   500,
		"error": make(chan struct{}),
	}
	mapRecordFixture := map[interface{}]interface{}{
		// lots of key/value pairs in map to increase chances of test hitting in case of unsorted map marshalling
		"A": "A",
		"B": "B",
		"C": "C",
		"D": "D",
		"E": "E",
		"F": "F",
		"G": "G",
		"H": "H",
	}
	byteArrayRecordFixture := map[interface{}]interface{}{
		"label": "label",
		"outer": []byte("foo"),
		"map": map[interface{}]interface{}{
			"inner": []byte("bar"),
		},
	}
	mixedTypesRecordFixture := map[interface{}]interface{}{
		"label": "label",
		"int":   42,
		"float": 42.42,
		"array": []interface{}{42, 42.42, "foo"},
		"map": map[interface{}]interface{}{
			"nested": map[interface{}]interface{}{
				"foo":     "bar",
				"invalid": []byte("a\xc5z"),
			},
		},
	}
	nestedJSONFixture := map[interface{}]interface{}{
		"kubernetes": map[interface{}]interface{}{
			"annotations": map[interface{}]interface{}{
				"kubernetes.io/psp":  "test",
				"prometheus.io/port": "8085",
			},
		},
		"log": "\tstatus code: 403, request id: b41c1ffa-c586-4359-a7da-457dd8da4bad\n",
	}

	tests := []struct {
		name    string
		cfg     *config
		record  map[interface{}]interface{}
		want    []api.Entry
		wantErr bool
	}{
		{"map to JSON", &config{labelKeys: []string{"A"}, lineFormat: jsonFormat}, mapRecordFixture, []api.Entry{{Labels: model.LabelSet{"A": "A"}, Entry: logproto.Entry{Line: `{"B":"B","C":"C","D":"D","E":"E","F":"F","G":"G","H":"H"}`, Timestamp: now}}}, false},
		{"map to kvPairFormat", &config{labelKeys: []string{"A"}, lineFormat: kvPairFormat}, mapRecordFixture, []api.Entry{{Labels: model.LabelSet{"A": "A"}, Entry: logproto.Entry{Line: `B=B C=C D=D E=E F=F G=G H=H`, Timestamp: now}}}, false},
		{"not enough records", &config{labelKeys: []string{"foo"}, lineFormat: jsonFormat, removeKeys: []string{"bar", "error"}}, simpleRecordFixture, []api.Entry{}, false},
		{"labels", &config{labelKeys: []string{"bar", "fake"}, lineFormat: jsonFormat, removeKeys: []string{"fuzz", "error"}}, simpleRecordFixture, []api.Entry{{Labels: model.LabelSet{"bar": "500"}, Entry: logproto.Entry{Line: `{"foo":"bar"}`, Timestamp: now}}}, false},
		{"remove key", &config{labelKeys: []string{"fake"}, lineFormat: jsonFormat, removeKeys: []string{"foo", "error", "fake"}}, simpleRecordFixture, []api.Entry{{Labels: model.LabelSet{}, Entry: logproto.Entry{Line: `{"bar":500}`, Timestamp: now}}}, false},
		{"error", &config{labelKeys: []string{"fake"}, lineFormat: jsonFormat, removeKeys: []string{"foo"}}, simpleRecordFixture, []api.Entry{}, true},
		{"key value", &config{labelKeys: []string{"fake"}, lineFormat: kvPairFormat, removeKeys: []string{"foo", "error", "fake"}}, simpleRecordFixture, []api.Entry{{Labels: model.LabelSet{}, Entry: logproto.Entry{Line: `bar=500`, Timestamp: now}}}, false},
		{"single", &config{labelKeys: []string{"fake"}, dropSingleKey: true, lineFormat: kvPairFormat, removeKeys: []string{"foo", "error", "fake"}}, simpleRecordFixture, []api.Entry{{Labels: model.LabelSet{}, Entry: logproto.Entry{Line: `500`, Timestamp: now}}}, false},
		{"labelmap", &config{labelMap: map[string]interface{}{"bar": "other"}, lineFormat: jsonFormat, removeKeys: []string{"bar", "error"}}, simpleRecordFixture, []api.Entry{{Labels: model.LabelSet{"other": "500"}, Entry: logproto.Entry{Line: `{"foo":"bar"}`, Timestamp: now}}}, false},
		{"byte array", &config{labelKeys: []string{"label"}, lineFormat: jsonFormat}, byteArrayRecordFixture, []api.Entry{{Labels: model.LabelSet{"label": "label"}, Entry: logproto.Entry{Line: `{"map":{"inner":"bar"},"outer":"foo"}`, Timestamp: now}}}, false},
		{"mixed types", &config{labelKeys: []string{"label"}, lineFormat: jsonFormat}, mixedTypesRecordFixture, []api.Entry{{Labels: model.LabelSet{"label": "label"}, Entry: logproto.Entry{Line: `{"array":[42,42.42,"foo"],"float":42.42,"int":42,"map":{"nested":{"foo":"bar","invalid":"a\ufffdz"}}}`, Timestamp: now}}}, false},
		{"JSON inner string escaping", &config{removeKeys: []string{"kubernetes"}, labelMap: map[string]interface{}{"kubernetes": map[string]interface{}{"annotations": map[string]interface{}{"kubernetes.io/psp": "label"}}}, lineFormat: jsonFormat}, nestedJSONFixture, []api.Entry{{Labels: model.LabelSet{"label": "test"}, Entry: logproto.Entry{Line: `{"log":"\tstatus code: 403, request id: b41c1ffa-c586-4359-a7da-457dd8da4bad\n"}`, Timestamp: now}}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := fake.New(func() {})
			l := &loki{
				cfg:    tt.cfg,
				client: rec,
				logger: logger,
			}
			err := l.sendRecord(tt.record, now)
			if (err != nil) != tt.wantErr {
				t.Errorf("sendRecord() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			rec.Stop()
			got := rec.Received()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sendRecord() want:%v got:%v", tt.want, got)
			}
		})
	}
}

func Test_createLine(t *testing.T) {
	tests := []struct {
		name    string
		records map[string]interface{}
		f       format
		want    string
		wantErr bool
	}{

		{"json", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": "bazz"}}, jsonFormat, `{"foo":"bar","bar":{"bizz":"bazz"}}`, false},
		{"json with number", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": 20}}, jsonFormat, `{"foo":"bar","bar":{"bizz":20}}`, false},
		{"bad json", map[string]interface{}{"foo": make(chan interface{})}, jsonFormat, "", true},
		{"kv with space", map[string]interface{}{"foo": "bar", "bar": "foo foo"}, kvPairFormat, `bar="foo foo" foo=bar`, false},
		{"kv with number", map[string]interface{}{"foo": "bar foo", "decimal": 12.2}, kvPairFormat, `decimal=12.2 foo="bar foo"`, false},
		{"kv with nil", map[string]interface{}{"foo": "bar", "null": nil}, kvPairFormat, `foo=bar null=null`, false},
		{"kv with array", map[string]interface{}{"foo": "bar", "array": []string{"foo", "bar"}}, kvPairFormat, `array="[foo bar]" foo=bar`, false},
		{"kv with map", map[string]interface{}{"foo": "bar", "map": map[string]interface{}{"foo": "bar", "bar ": "foo "}}, kvPairFormat, `foo=bar map="map[bar :foo  foo:bar]"`, false},
		{"kv empty", map[string]interface{}{}, kvPairFormat, ``, false},
		{"bad format", nil, format(3), "", true},
		{"nested json", map[string]interface{}{"log": `{"level":"error"}`}, jsonFormat, `{"log":{"level":"error"}}`, false},
		{"nested json", map[string]interface{}{"log": `["level","error"]`}, jsonFormat, `{"log":["level","error"]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &loki{
				logger: logger,
			}
			got, err := l.createLine(tt.records, tt.f)
			if (err != nil) != tt.wantErr {
				t.Errorf("createLine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if tt.f == jsonFormat {
				compareJSON(t, got, tt.want)
			} else {
				if got != tt.want {
					t.Errorf("createLine() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// compareJson unmarshal both string to map[string]interface compare json result.
// we can't compare string to string as jsoniter doesn't ensure field ordering.
func compareJSON(t *testing.T, got, want string) {
	var w map[string]interface{}
	err := jsoniter.Unmarshal([]byte(want), &w)
	if err != nil {
		t.Errorf("failed to unmarshal string: %s", err)
	}
	var g map[string]interface{}
	err = jsoniter.Unmarshal([]byte(got), &g)
	if err != nil {
		t.Errorf("failed to unmarshal string: %s", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("compareJson() = %v, want %v", g, w)
	}
}

func Test_removeKeys(t *testing.T) {
	tests := []struct {
		name     string
		records  map[string]interface{}
		expected map[string]interface{}
		keys     []string
	}{
		{"remove all keys", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": "bazz"}}, map[string]interface{}{}, []string{"foo", "bar"}},
		{"remove none", map[string]interface{}{"foo": "bar"}, map[string]interface{}{"foo": "bar"}, []string{}},
		{"remove not existing", map[string]interface{}{"foo": "bar"}, map[string]interface{}{"foo": "bar"}, []string{"bar"}},
		{"remove one", map[string]interface{}{"foo": "bar", "bazz": "buzz"}, map[string]interface{}{"foo": "bar"}, []string{"bazz"}},

		// Dot notation tests for removeKeys
		{"remove nested key", map[string]interface{}{"log": map[string]interface{}{"level": "INFO", "message": "test"}}, map[string]interface{}{"log": map[string]interface{}{"message": "test"}}, []string{"log.level"}},
		{"remove multiple nested keys", map[string]interface{}{"log": map[string]interface{}{"level": "INFO", "message": "test", "logger": "app"}}, map[string]interface{}{"log": map[string]interface{}{"message": "test"}}, []string{"log.level", "log.logger"}},
		{"remove deep nested key", map[string]interface{}{"kubernetes": map[string]interface{}{"pod": map[string]interface{}{"name": "test", "namespace": "default"}}}, map[string]interface{}{"kubernetes": map[string]interface{}{"pod": map[string]interface{}{"namespace": "default"}}}, []string{"kubernetes.pod.name"}},
		{"remove non-existing nested", map[string]interface{}{"log": map[string]interface{}{"message": "test"}}, map[string]interface{}{"log": map[string]interface{}{"message": "test"}}, []string{"log.level"}},
		{"remove mixed regular and nested", map[string]interface{}{"service": "api", "log": map[string]interface{}{"level": "INFO", "message": "test"}}, map[string]interface{}{"log": map[string]interface{}{"message": "test"}}, []string{"service", "log.level"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			removeKeys(tt.records, tt.keys)
			if !reflect.DeepEqual(tt.expected, tt.records) {
				t.Errorf("removeKeys() = %v, want %v", tt.records, tt.expected)
			}
		})
	}
}

func Test_extractLabels(t *testing.T) {
	tests := []struct {
		name    string
		records map[string]interface{}
		keys    []string
		want    model.LabelSet
	}{
		{"single string", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": "bazz"}}, []string{"foo"}, model.LabelSet{"foo": "bar"}},
		{"multiple", map[string]interface{}{"foo": "bar", "bar": map[string]interface{}{"bizz": "bazz"}}, []string{"foo", "bar"}, model.LabelSet{"foo": "bar", "bar": "map[bizz:bazz]"}},
		{"nil", map[string]interface{}{"foo": nil}, []string{"foo"}, model.LabelSet{"foo": "<nil>"}},
		{"none", map[string]interface{}{"foo": nil}, []string{}, model.LabelSet{}},
		{"missing", map[string]interface{}{"foo": "bar"}, []string{"foo", "buzz"}, model.LabelSet{"foo": "bar"}},
		{"skip invalid", map[string]interface{}{"foo.blah": "bar", "bar": "a\xc5z"}, []string{"foo.blah", "bar"}, model.LabelSet{}},

		// Dot notation tests
		{"dot notation simple", map[string]interface{}{"log": map[string]interface{}{"level": "INFO"}}, []string{"log.level"}, model.LabelSet{"log_level": "INFO"}},
		{"dot notation multiple levels", map[string]interface{}{"kubernetes": map[string]interface{}{"pod": map[string]interface{}{"name": "test-pod"}}}, []string{"kubernetes.pod.name"}, model.LabelSet{"kubernetes_pod_name": "test-pod"}},
		{"dot notation mixed with regular", map[string]interface{}{"service": "api", "log": map[string]interface{}{"level": "ERROR", "logger": "app"}}, []string{"service", "log.level", "log.logger"}, model.LabelSet{"service": "api", "log_level": "ERROR", "log_logger": "app"}},
		{"dot notation missing nested", map[string]interface{}{"log": map[string]interface{}{"message": "test"}}, []string{"log.level"}, model.LabelSet{}},
		{"dot notation non-map intermediate", map[string]interface{}{"log": "not a map"}, []string{"log.level"}, model.LabelSet{}},
		{"dot notation with numbers", map[string]interface{}{"metrics": map[string]interface{}{"cpu": 85.5, "memory": 1024}}, []string{"metrics.cpu", "metrics.memory"}, model.LabelSet{"metrics_cpu": "85.5", "metrics_memory": "1024"}},
		{"dot notation with boolean", map[string]interface{}{"flags": map[string]interface{}{"enabled": true, "debug": false}}, []string{"flags.enabled", "flags.debug"}, model.LabelSet{"flags_enabled": "true", "flags_debug": "false"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractLabels(tt.records, tt.keys); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractLabels() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_toStringMap(t *testing.T) {
	tests := []struct {
		name   string
		record map[interface{}]interface{}
		want   map[string]interface{}
	}{
		{"already string", map[interface{}]interface{}{"string": "foo", "bar": []byte("buzz")}, map[string]interface{}{"string": "foo", "bar": "buzz"}},
		{"skip non string", map[interface{}]interface{}{"string": "foo", 1.0: []byte("buzz")}, map[string]interface{}{"string": "foo"}},
		{
			"byteslice in array",
			map[interface{}]interface{}{"string": "foo", "bar": []interface{}{map[interface{}]interface{}{"baz": []byte("quux")}}},
			map[string]interface{}{"string": "foo", "bar": []interface{}{map[string]interface{}{"baz": "quux"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toStringMap(tt.record); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("toStringMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_labelMapping(t *testing.T) {
	tests := []struct {
		name    string
		records map[string]interface{}
		mapping map[string]interface{}
		want    model.LabelSet
	}{
		{
			"empty record",
			map[string]interface{}{},
			map[string]interface{}{},
			model.LabelSet{},
		},
		{
			"empty subrecord",
			map[string]interface{}{
				"kubernetes": map[interface{}]interface{}{
					"foo": []byte("buzz"),
				},
			},
			map[string]interface{}{},
			model.LabelSet{},
		},
		{
			"deep string",
			map[string]interface{}{
				"int":   "42",
				"float": "42.42",
				"array": `[42,42.42,"foo"]`,
				"kubernetes": map[string]interface{}{
					"label": map[string]interface{}{
						"component": map[string]interface{}{
							"buzz": "value",
						},
					},
				},
			},
			map[string]interface{}{
				"int":   "int",
				"float": "float",
				"array": "array",
				"kubernetes": map[string]interface{}{
					"label": map[string]interface{}{
						"component": map[string]interface{}{
							"buzz": "label",
						},
					},
				},
				"stream": "output",
				"nope":   "nope",
			},
			model.LabelSet{
				"int":   "42",
				"float": "42.42",
				"array": `[42,42.42,"foo"]`,
				"label": "value",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := model.LabelSet{}
			if mapLabels(tt.records, tt.mapping, got); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mapLabels() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_AutoKubernetesLabels(t *testing.T) {
	tests := []struct {
		name    string
		records map[interface{}]interface{}
		want    model.LabelSet
		err     error
	}{
		{
			"records without labels",
			map[interface{}]interface{}{
				"kubernetes": map[interface{}]interface{}{
					"foo": []byte("buzz"),
				},
			},
			model.LabelSet{
				"foo": "buzz",
			},
			nil,
		},
		{
			"records with labels",
			map[interface{}]interface{}{
				"kubernetes": map[string]interface{}{
					"labels": map[string]interface{}{
						"foo":  "bar",
						"buzz": "value",
					},
				},
			},
			model.LabelSet{
				"foo":  "bar",
				"buzz": "value",
			},
			nil,
		},
		{
			"records without kubernetes labels",
			map[interface{}]interface{}{
				"foo":   "bar",
				"label": "value",
			},
			model.LabelSet{},
			errors.New("kubernetes labels not found, no labels will be added"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := toStringMap(tt.records)
			lbs := model.LabelSet{}
			err := autoLabels(m, lbs)
			if err != nil && err.Error() != tt.err.Error() {
				t.Errorf("error in autolabels, error = %v", err)
				return
			}
			if !reflect.DeepEqual(lbs, tt.want) {
				t.Errorf("mapLabels() = %v, want %v", lbs, tt.want)
			}
		})
	}
}

func Test_DotNotation_EndToEnd(t *testing.T) {
	// Example log from a Spring Boot application on AWS ECS
	record := map[string]interface{}{
		"@timestamp": "2024-01-15T10:30:45.123Z",
		"service":    "payment-api",
		"log": map[string]interface{}{
			"level":     "ERROR",
			"logger":    "com.example.payment.PaymentService",
			"message":   "Payment processing failed",
			"thread":    "http-nio-8080-exec-1",
			"exception": "java.lang.RuntimeException: Connection timeout",
		},
		"kubernetes": map[string]interface{}{
			"namespace": "production",
			"pod": map[string]interface{}{
				"name": "payment-api-7b6d4f9c-x2j9k",
				"ip":   "10.0.5.42",
			},
			"labels": map[string]interface{}{
				"app":     "payment-api",
				"version": "v1.2.3",
			},
		},
		"aws": map[string]interface{}{
			"ecs": map[string]interface{}{
				"cluster": "prod-cluster",
				"service": "payment-service",
				"task": map[string]interface{}{
					"arn":    "arn:aws:ecs:us-west-2:123456789:task/abc123",
					"family": "payment-api-task",
				},
			},
		},
		"metrics": map[string]interface{}{
			"cpu":      85.5,
			"memory":   2048,
			"requests": 1500,
		},
	}

	// Define keys to extract as labels using dot notation
	labelKeys := []string{
		"service",
		"log.level",
		"log.logger",
		"kubernetes.namespace",
		"kubernetes.pod.name",
		"aws.ecs.cluster",
		"aws.ecs.task.family",
		"metrics.cpu",
	}

	// Extract labels
	labels := extractLabels(record, labelKeys)

	// Verify extracted labels
	expectedLabels := model.LabelSet{
		"service":              "payment-api",
		"log_level":            "ERROR",
		"log_logger":           "com.example.payment.PaymentService",
		"kubernetes_namespace": "production",
		"kubernetes_pod_name":  "payment-api-7b6d4f9c-x2j9k",
		"aws_ecs_cluster":      "prod-cluster",
		"aws_ecs_task_family":  "payment-api-task",
		"metrics_cpu":          "85.5",
	}

	if !reflect.DeepEqual(labels, expectedLabels) {
		t.Errorf("extractLabels() = %v, want %v", labels, expectedLabels)
	}

	// Remove the extracted keys from the record
	removeKeys(record, labelKeys)

	// Verify that nested fields are removed properly
	if logMap, ok := record["log"].(map[string]interface{}); ok {
		if _, exists := logMap["level"]; exists {
			t.Error("log.level should have been removed")
		}
		if _, exists := logMap["logger"]; exists {
			t.Error("log.logger should have been removed")
		}
		// Message should still be there (not removed)
		if _, exists := logMap["message"]; !exists {
			t.Error("log.message should NOT have been removed")
		}
	}

	// Service field should be completely removed
	if _, exists := record["service"]; exists {
		t.Error("service field should have been removed")
	}
}

func Test_DotNotation_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		records  map[string]interface{}
		keys     []string
		expected model.LabelSet
	}{
		{
			"empty key list",
			map[string]interface{}{"foo": "bar"},
			[]string{},
			model.LabelSet{},
		},
		{
			"nil records",
			nil,
			[]string{"foo"},
			model.LabelSet{},
		},
		{
			"malformed dot notation - double dots",
			map[string]interface{}{"log": map[string]interface{}{"level": "INFO"}},
			[]string{"log..level"},
			model.LabelSet{"log__level": "INFO"}, // Double underscore reflects double dots
		},
		{
			"malformed dot notation - leading dot",
			map[string]interface{}{"log": map[string]interface{}{"level": "INFO"}},
			[]string{".log.level"},
			model.LabelSet{"_log_level": "INFO"}, // Leading underscore reflects leading dot
		},
		{
			"malformed dot notation - trailing dot",
			map[string]interface{}{"log": map[string]interface{}{"level": "INFO"}},
			[]string{"log.level."},
			model.LabelSet{"log_level_": "INFO"}, // Trailing underscore reflects trailing dot
		},
		{
			"empty string key",
			map[string]interface{}{"foo": "bar"},
			[]string{""},
			model.LabelSet{},
		},
		{
			"only dots key",
			map[string]interface{}{"foo": "bar"},
			[]string{"..."},
			model.LabelSet{},
		},
		{
			"mixed valid and invalid keys",
			map[string]interface{}{
				"valid": "test",
				"log":   map[string]interface{}{"level": "INFO"},
			},
			[]string{"valid", "", "log.level", "missing.key"},
			model.LabelSet{"valid": "test", "log_level": "INFO"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractLabels(tt.records, tt.keys)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("extractLabels() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func Test_RemoveKeys_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		records  map[string]interface{}
		keys     []string
		expected map[string]interface{}
	}{
		{
			"empty key list",
			map[string]interface{}{"foo": "bar"},
			[]string{},
			map[string]interface{}{"foo": "bar"},
		},
		{
			"malformed dot notation - double dots",
			map[string]interface{}{"log": map[string]interface{}{"level": "INFO", "message": "test"}},
			[]string{"log..level"},
			map[string]interface{}{"log": map[string]interface{}{"message": "test"}},
		},
		{
			"empty string key",
			map[string]interface{}{"foo": "bar"},
			[]string{""},
			map[string]interface{}{"foo": "bar"},
		},
		{
			"only dots key",
			map[string]interface{}{"foo": "bar"},
			[]string{"..."},
			map[string]interface{}{"foo": "bar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy of the input to avoid side effects
			recordsCopy := make(map[string]interface{})
			for k, v := range tt.records {
				recordsCopy[k] = v
			}

			removeKeys(recordsCopy, tt.keys)
			if !reflect.DeepEqual(recordsCopy, tt.expected) {
				t.Errorf("removeKeys() = %v, want %v", recordsCopy, tt.expected)
			}
		})
	}
}

// Benchmarks for performance testing
func BenchmarkExtractLabels_SimpleKeys(b *testing.B) {
	records := map[string]interface{}{
		"service": "api",
		"level":   "ERROR",
		"region":  "us-west-2",
		"cluster": "prod",
	}
	keys := []string{"service", "level", "region", "cluster"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractLabels(records, keys)
	}
}

func BenchmarkExtractLabels_DotNotation(b *testing.B) {
	records := map[string]interface{}{
		"service": "api",
		"log": map[string]interface{}{
			"level":  "ERROR",
			"logger": "com.example.Service",
		},
		"kubernetes": map[string]interface{}{
			"namespace": "production",
			"pod": map[string]interface{}{
				"name": "api-7b6d4f9c-x2j9k",
			},
		},
	}
	keys := []string{"service", "log.level", "log.logger", "kubernetes.namespace", "kubernetes.pod.name"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractLabels(records, keys)
	}
}

func BenchmarkExtractLabels_DeepNesting(b *testing.B) {
	records := map[string]interface{}{
		"a": map[string]interface{}{
			"b": map[string]interface{}{
				"c": map[string]interface{}{
					"d": map[string]interface{}{
						"e": "deep_value",
					},
				},
			},
		},
	}
	keys := []string{"a.b.c.d.e"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractLabels(records, keys)
	}
}

func BenchmarkRemoveKeys_DotNotation(b *testing.B) {
	baseRecord := map[string]interface{}{
		"service": "api",
		"log": map[string]interface{}{
			"level":   "ERROR",
			"logger":  "com.example.Service",
			"message": "error occurred",
		},
		"kubernetes": map[string]interface{}{
			"namespace": "production",
			"pod": map[string]interface{}{
				"name": "api-7b6d4f9c-x2j9k",
			},
		},
	}
	keys := []string{"service", "log.level", "log.logger"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Create a fresh copy for each iteration
		records := make(map[string]interface{})
		for k, v := range baseRecord {
			records[k] = v
		}
		removeKeys(records, keys)
	}
}
