package logical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestCollectLogfmtMetricKeys(t *testing.T) {
	t.Run("returns empty keys but collects unwrap type hints", func(t *testing.T) {
		expr := syntax.MustParseExpr(`sum by (app) (sum_over_time({app="test"} | logfmt | unwrap bytes [5m]))`)
		keys, hints := collectLogfmtMetricKeys(expr)

		require.Empty(t, keys, "should return empty keys array")
		require.Equal(t, "int64", hints["bytes"], "should have correct type hint for bytes")
	})

	t.Run("collects duration type hint for duration unwrap", func(t *testing.T) {
		expr := syntax.MustParseExpr(`sum by (app) (sum_over_time({app="test"} | logfmt | unwrap duration [5m]))`)
		keys, hints := collectLogfmtMetricKeys(expr)

		require.Empty(t, keys, "should return empty keys array")
		require.Equal(t, "duration", hints["duration"], "should have correct type hint for duration")
	})

	t.Run("defaults to float64 for unspecified types", func(t *testing.T) {
		expr := syntax.MustParseExpr(`sum by (app) (sum_over_time({app="test"} | logfmt | unwrap latency [5m]))`)
		keys, hints := collectLogfmtMetricKeys(expr)

		require.Empty(t, keys, "should return empty keys array")
		require.Equal(t, "float64", hints["latency"], "should default to float64 type hint")
	})

	t.Run("returns empty for queries without logfmt", func(t *testing.T) {
		expr := syntax.MustParseExpr(`sum by (app) (count_over_time({app="test"} [5m]))`)
		keys, hints := collectLogfmtMetricKeys(expr)

		require.Empty(t, keys, "should return empty keys without logfmt")
		require.Empty(t, hints, "should return empty hints without logfmt")
	})

	t.Run("returns empty for count_over_time without unwrap", func(t *testing.T) {
		expr := syntax.MustParseExpr(`sum by (region) (count_over_time({job="api"} | logfmt | status="200" [5m]))`)
		keys, hints := collectLogfmtMetricKeys(expr)

		require.Empty(t, keys, "should return empty keys array")
		require.Empty(t, hints, "should have no hints for count_over_time")
	})
}

func TestGetUnwrapTypeHint(t *testing.T) {
	tests := []struct {
		name     string
		unwrap   *syntax.UnwrapExpr
		expected string
	}{
		{
			name:     "nil unwrap returns float64",
			unwrap:   nil,
			expected: "float64",
		},
		{
			name: "bytes operation returns int64",
			unwrap: &syntax.UnwrapExpr{
				Operation:  "bytes",
				Identifier: "foo",
			},
			expected: "int64",
		},
		{
			name: "duration operation returns duration",
			unwrap: &syntax.UnwrapExpr{
				Operation:  "duration",
				Identifier: "foo",
			},
			expected: "duration",
		},
		{
			name: "duration_seconds operation returns duration",
			unwrap: &syntax.UnwrapExpr{
				Operation:  "duration_seconds",
				Identifier: "foo",
			},
			expected: "duration",
		},
		{
			name: "bytes identifier without operation returns int64",
			unwrap: &syntax.UnwrapExpr{
				Operation:  "",
				Identifier: "bytes",
			},
			expected: "int64",
		},
		{
			name: "duration identifier without operation returns duration",
			unwrap: &syntax.UnwrapExpr{
				Operation:  "",
				Identifier: "duration",
			},
			expected: "duration",
		},
		{
			name: "unknown identifier defaults to float64",
			unwrap: &syntax.UnwrapExpr{
				Operation:  "",
				Identifier: "latency",
			},
			expected: "float64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getUnwrapTypeHint(tt.unwrap)
			require.Equal(t, tt.expected, result)
		})
	}
}