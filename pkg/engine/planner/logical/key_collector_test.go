package logical

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestCollectRequestedKeys(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		expectedKeys  []string
		expectedHints map[string]string
	}{
		// Metric queries (with aggregations)
		{
			name:          "simple filter with count_over_time",
			query:         `count_over_time({app="test"} | logfmt | level="error" [5m])`,
			expectedKeys:  []string{"level"},
			expectedHints: map[string]string{},
		},
		{
			name:          "two different keys with count_over_time",
			query:         `count_over_time({app="test"} | logfmt | level="error" | status=200 [5m])`,
			expectedKeys:  []string{"level", "status"},
			expectedHints: map[string]string{},
		},
		{
			name:          "duplicate keys should appear only once",
			query:         `count_over_time({app="test"} | logfmt | level="error" | status=200 | level="warn" [5m])`,
			expectedKeys:  []string{"level", "status"},
			expectedHints: map[string]string{},
		},
		{
			name:          "keys should be sorted alphabetically",
			query:         `count_over_time({app="test"} | logfmt | status=200 | level="error" | app="prod" [5m])`,
			expectedKeys:  []string{"app", "level", "status"},
			expectedHints: map[string]string{},
		},
		{
			name:          "unwrap bytes should hint int64",
			query:         `sum_over_time({app="test"} | logfmt | unwrap bytes [5m])`,
			expectedKeys:  []string{"bytes"},
			expectedHints: map[string]string{"bytes": "int64"},
		},
		{
			name:          "unwrap duration with filters",
			query:         `sum_over_time({app="test"} | logfmt | level="error" | unwrap duration [5m])`,
			expectedKeys:  []string{"duration", "level"},
			expectedHints: map[string]string{"duration": "duration"},
		},
		{
			name:          "unwrap with duration conversion",
			query:         `sum_over_time({app="test"} | logfmt | unwrap duration(response_time) [5m])`,
			expectedKeys:  []string{"response_time"},
			expectedHints: map[string]string{"response_time": "duration"},
		},
		{
			name:          "unwrap with bytes conversion",
			query:         `sum_over_time({app="test"} | logfmt | unwrap bytes(response_size) [5m])`,
			expectedKeys:  []string{"response_size"},
			expectedHints: map[string]string{"response_size": "int64"},
		},
		{
			name:          "rate query with unwrap",
			query:         `rate({app="test"} | logfmt | unwrap bytes [5m])`,
			expectedKeys:  []string{"bytes"},
			expectedHints: map[string]string{"bytes": "int64"},
		},
		{
			name:          "quantile_over_time with unwrap",
			query:         `quantile_over_time(0.99, {app="test"} | logfmt | unwrap response_time [5m])`,
			expectedKeys:  []string{"response_time"},
			expectedHints: map[string]string{"response_time": "float64"},
		},
		{
			name:          "vector aggregation with groupby",
			query:         `sum by (region) (count_over_time({app="test"} | logfmt [5m]))`,
			expectedKeys:  []string{"region"},
			expectedHints: map[string]string{},
		},
		{
			name:          "vector aggregation with multiple groupby labels",
			query:         `sum by (region, zone, cluster) (count_over_time({app="test"} | logfmt [5m]))`,
			expectedKeys:  []string{"cluster", "region", "zone"},
			expectedHints: map[string]string{},
		},
		{
			name:          "complex query with unwrap and groupby",
			query:         `sum by (region) (sum_over_time({app="test"} | logfmt | unwrap bytes [5m]))`,
			expectedKeys:  []string{"bytes", "region"},
			expectedHints: map[string]string{"bytes": "int64"},
		},
		// Additional test cases for various scenarios
		{
			name:          "unwrap with filters before and after",
			query:         `sum_over_time({app="test"} | logfmt | status=200 | unwrap latency [5m])`,
			expectedKeys:  []string{"latency", "status"},
			expectedHints: map[string]string{"latency": "float64"},
		},
		// TODO(twhitney): For now we explicitly only support logfmt'd metric queries.
		// Obviously this will need to change once we implement log query support
		// Non-metric queries, and non-logfmt queries, should return nil
		{
			name:          "non-metric query should return nil",
			query:         `{app="test"} | logfmt | level="error"`,
			expectedKeys:  nil,
			expectedHints: nil,
		},
		{
			name:          "non-logfmt query should return nil",
			query:         `{app="test"} | level="error"`,
			expectedKeys:  nil,
			expectedHints: nil,
		},
		{
			name:          "non-logfmt metric query should also return nil",
			query:         `count_over_time({app="test"} | json | level="error" [5m])`,
			expectedKeys:  nil,
			expectedHints: nil,
		},
		{
			name:          "non-metric query with multiple filters should return nil",
			query:         `{app="test"} | logfmt | level="error" | status=200`,
			expectedKeys:  nil,
			expectedHints: nil,
		},
		{
			name:          "non-metric query with line format should return nil",
			query:         `{app="test"} | logfmt | line_format "{{.level}}"`,
			expectedKeys:  nil,
			expectedHints: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tt.query)
			require.NoError(t, err)

			keys, hints := CollectRequestedKeys(expr)

			require.Equal(t, tt.expectedKeys, keys, fmt.Sprintf("Should collect %+v key(s) from %s", tt.expectedKeys, tt.query))
			require.Equal(t, tt.expectedHints, hints, fmt.Sprintf("Should have hints %+v from %s", tt.expectedHints, tt.query))
		})
	}
}
