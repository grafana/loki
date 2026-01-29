package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestJSONParser_Process(t *testing.T) {
	tests := []struct {
		name               string
		line               []byte
		requestedKeyLookup map[string]struct{}
		want               map[string]string
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
			map[string]struct{}{"app": {}, "level": {}},
			map[string]string{
				"app":   "foo",
				"level": "info",
			},
		},
		{
			"requested keys filtering - nested",
			[]byte(`{"app":"foo","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			map[string]struct{}{"app": {}, "pod_uuid": {}},
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
			result, err := j.process(tt.line, tt.requestedKeyLookup)
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

func TestBuildJSONColumns(t *testing.T) {
	t.Run("reuses parser and lookup map across multiple lines", func(t *testing.T) {
		// Create input with multiple lines to ensure reuse
		builder := array.NewStringBuilder(memory.DefaultAllocator)
		builder.Append(`{"app":"foo","level":"info","msg":"first"}`)
		builder.Append(`{"app":"bar","level":"debug","msg":"second","extra":"data"}`)
		builder.Append(`{"app":"baz","level":"warn","msg":"third","nested":{"key":"value"}}`)
		input := builder.NewStringArray()
		defer input.Release()

		requestedKeys := []string{"app", "level", "nested_key"}

		headers, columns := buildJSONColumns(input, requestedKeys)

		// Verify we got the expected columns
		require.Contains(t, headers, "app")
		require.Contains(t, headers, "level")
		require.Contains(t, headers, "nested_key")
		require.Len(t, headers, 3)
		require.Len(t, columns, 3)

		// Defer releasing columns until after all checks
		defer func() {
			for _, col := range columns {
				col.Release()
			}
		}()

		// Verify each column has the right number of rows
		for _, col := range columns {
			require.Equal(t, 3, col.Len())
		}

		// Verify specific values
		var appCol, levelCol, nestedCol *array.String
		for i, h := range headers {
			switch h {
			case "app":
				appCol = columns[i].(*array.String)
			case "level":
				levelCol = columns[i].(*array.String)
			case "nested_key":
				nestedCol = columns[i].(*array.String)
			}
		}

		require.NotNil(t, appCol)
		require.Equal(t, "foo", appCol.Value(0))
		require.Equal(t, "bar", appCol.Value(1))
		require.Equal(t, "baz", appCol.Value(2))

		require.NotNil(t, levelCol)
		require.Equal(t, "info", levelCol.Value(0))
		require.Equal(t, "debug", levelCol.Value(1))
		require.Equal(t, "warn", levelCol.Value(2))

		require.NotNil(t, nestedCol)
		require.True(t, nestedCol.IsNull(0))
		require.True(t, nestedCol.IsNull(1))
		require.Equal(t, "value", nestedCol.Value(2))
	})

	t.Run("handles all lines without requested keys", func(t *testing.T) {
		builder := array.NewStringBuilder(memory.DefaultAllocator)
		builder.Append(`{"a":"1","b":"2"}`)
		builder.Append(`{"c":"3","d":"4"}`)
		input := builder.NewStringArray()
		defer input.Release()

		headers, columns := buildJSONColumns(input, nil)
		defer func() {
			for _, col := range columns {
				col.Release()
			}
		}()

		// Should extract all keys across all lines
		require.ElementsMatch(t, []string{"a", "b", "c", "d"}, headers)
		require.Len(t, columns, 4)

		for _, col := range columns {
			require.Equal(t, 2, col.Len())
		}
	})
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
