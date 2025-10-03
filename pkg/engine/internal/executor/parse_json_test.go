package executor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSONParser_Process(t *testing.T) {
	tests := []struct {
		name          string
		line          []byte
		requestedKeys []string
		want          map[string]string
	}{
		{
			"multi depth",
			[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			nil, // Extract all keys
			map[string]string{
				"app":                "foo",
				"namespace":          "prod",
				"pod_uuid":           "foo",
				"pod_deployment_ref": "foobar",
			},
		},
		{
			"empty object",
			[]byte(`{}`),
			nil,
			map[string]string{},
		},
		{
			"single key-value",
			[]byte(`{"key": "value"}`),
			nil,
			map[string]string{"key": "value"},
		},
		{
			"numeric",
			[]byte(`{"counter":1, "price": {"_net_":5.56909}}`),
			nil,
			map[string]string{
				"counter":     "1",
				"price__net_": "5.56909",
			},
		},
		{
			"whitespace key value",
			[]byte(`{" ": {"foo":"bar"}}`),
			nil,
			map[string]string{
				"foo": "bar",
			},
		},
		{
			"whitespace key and whitespace subkey values",
			[]byte(`{" ": {" ":"bar"}}`),
			nil,
			map[string]string{
				"": "bar",
			},
		},
		{
			"whitespace key and empty subkey values",
			[]byte(`{" ": {"":"bar"}}`),
			nil,
			map[string]string{
				"": "bar",
			},
		},
		{
			"empty key and empty subkey values",
			[]byte(`{"": {"":"bar"}}`),
			nil,
			map[string]string{
				"": "bar",
			},
		},
		{
			"escaped",
			[]byte(`{"counter":1,"foo":"foo\\\"bar", "price": {"_net_":5.56909}}`),
			nil,
			map[string]string{
				"counter":     "1",
				"price__net_": "5.56909",
				"foo":         `foo\"bar`,
			},
		},
		{
			"invalid utf8 rune",
			[]byte(`{"counter":1,"foo":"�", "price": {"_net_":5.56909}}`),
			nil,
			map[string]string{
				"counter":     "1",
				"price__net_": "5.56909",
				"foo":         " ",
			},
		},
		{
			"correctly handles utf8 values",
			[]byte(`{"name": "José", "mood": "😀"}`),
			nil,
			map[string]string{
				"name": "José",
				"mood": "😀",
			},
		},
		{
			"correctly handles utf8 keys",
			[]byte(`{"José": "😀"}`),
			nil,
			map[string]string{
				"Jos_": "😀",
			},
		},
		{
			"skip arrays",
			[]byte(`{"counter":1, "price": {"net_":["10","20"]}}`),
			nil,
			map[string]string{
				"counter": "1",
			},
		},
		{
			"bad key replaced",
			[]byte(`{"cou-nter":1}`),
			nil,
			map[string]string{
				"cou_nter": "1",
			},
		},
		{
			"number keys starting with digit get prefixed",
			[]byte(`{"123key": "value"}`),
			nil,
			map[string]string{"_123key": "value"},
		},
		{
			"nested number keys",
			[]byte(`{"parent": {"456child": "value"}}`),
			nil,
			map[string]string{"parent__456child": "value"},
		},
		{
			"nested bad key replaced",
			[]byte(`{"foo":{"cou-nter":1}}`),
			nil,
			map[string]string{
				"foo_cou_nter": "1",
			},
		},
		{
			"boolean fields",
			[]byte(`{"enabled": true, "debug": false, "nested": {"active": true}}`),
			nil,
			map[string]string{
				"enabled":       "true",
				"debug":         "false",
				"nested_active": "true",
			},
		},
		{
			"null fields are skipped",
			[]byte(`{"name": "test", "value": null, "nested": {"field": null, "other": "value"}}`),
			nil,
			map[string]string{
				"name":         "test",
				"nested_other": "value",
			},
		},
		{
			"requested keys filtering - top level",
			[]byte(`{"app":"foo","namespace":"prod","level":"info"}`),
			[]string{"app", "level"},
			map[string]string{
				"app":   "foo",
				"level": "info",
			},
		},
		{
			"requested keys filtering - nested",
			[]byte(`{"app":"foo","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			[]string{"app", "pod_uuid"},
			map[string]string{
				"app":      "foo",
				"pod_uuid": "foo",
			},
		},
		{
			"deep nested object",
			[]byte(`{"nested": {"deep": {"very": {"deep": "value"}}}}`),
			nil,
			map[string]string{
				"nested_deep_very_deep": "value",
			},
		},
		{
			"complex mixed types",
			[]byte(`{"user": {"name": "john", "details": {"age": 30, "city": "NYC", "active": true}}, "status": "active", "count": 42}`),
			nil,
			map[string]string{
				"user_name":           "john",
				"user_details_age":    "30",
				"user_details_city":   "NYC",
				"user_details_active": "true",
				"status":              "active",
				"count":               "42",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := newJSONParser()
			result, err := j.process(tt.line, tt.requestedKeys)
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestJSONParser_Process_Malformed(t *testing.T) {
	tests := []struct {
		name string
		line []byte
	}{
		{"missing closing brace", []byte(`{"key": "value"`)},
		{"invalid json", []byte(`not json`)},
		{"more invalid json", []byte(`{n}`)},
		{"unclosed string", []byte(`{"key": "unclosed`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := newJSONParser()
			result, err := j.process(tt.line, nil)
			require.Error(t, err)
			// Should return empty result on error
			require.Empty(t, result)
		})
	}
}

func BenchmarkJSONParser_Process(b *testing.B) {
	testCases := []struct {
		name string
		line []byte
	}{
		{
			"simple",
			[]byte(`{"level": "info", "msg": "test message", "time": "2023-01-01T10:00:00Z"}`),
		},
		{
			"nested",
			[]byte(`{"user": {"name": "john", "details": {"age": 30, "city": "NYC"}}, "status": "active"}`),
		},
		{
			"complex",
			[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar","config":{"replicas":3,"image":"nginx:latest"}}},"timestamp":"2023-01-01T10:00:00Z","level":"info"}`),
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			j := newJSONParser()
			b.ResetTimer()

			for b.Loop() {
				_, err := j.process(tc.line, nil)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
