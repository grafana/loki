package hintprovider

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// TestSupportedMetricQuery is the spec for which metric queries logline may
// accelerate. A qualifying query has exactly one logs stage, that stage passes
// the same SupportedQuery rules as a log query, and the only wrappers around
// it are vector aggregations. The returned literals are the needles for that
// logs stage.
//
// SupportedMetricQuery currently returns nil, so qualifying cases fail.
func TestSupportedMetricQuery(t *testing.T) {
	const ngramLength = 6

	tests := []struct {
		name     string
		query    string
		expected []string
	}{
		{
			name:     "bare count_over_time",
			query:    `count_over_time({app="api"} |= "timeout" [5m])`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "sum of count_over_time",
			query:    `sum(count_over_time({app="api"} |= "timeout" [5m]))`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "sum by of count_over_time",
			query:    `sum by (service_name) (count_over_time({environment="product-production"} |= "Failed to decode the input token" [1h]))`,
			expected: []string{"FAILED TO DECODE THE INPUT TOKEN"},
		},
		{
			name:     "logs volume sum by count_over_time",
			query:    `sum by (level) (count_over_time({app="api"} |= "timeout" [1m]))`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "sum without",
			query:    `sum without (host) (count_over_time({app="api"} |= "timeout" [5m]))`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "parentheses do not add a logs stage",
			query:    `sum((count_over_time({app="api"} |= "timeout" [5m])))`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "rate over the same logs stage",
			query:    `sum(rate({app="api"} |= "timeout" [5m]))`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "bytes_over_time over the same logs stage",
			query:    `bytes_over_time({app="api"} |= "timeout" [5m])`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "absent_over_time over the same logs stage",
			query:    `absent_over_time({app="api"} |= "timeout" [5m])`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "offset does not change eligibility",
			query:    `count_over_time({app="api"} |= "timeout" [5m] offset 10m)`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "unwrap does not add a logs stage",
			query:    `sum(avg_over_time({app="api"} |= "timeout" | unwrap latency [5m]))`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "range aggregation grouping",
			query:    `max_over_time({app="api"} |= "timeout" | unwrap latency [5m]) by (host)`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "nested vector aggregations around one range",
			query:    `max(sum by (host) (rate({app="api"} |= "timeout" [5m])))`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "topk around one range",
			query:    `topk(5, count_over_time({app="api"} |= "timeout" [5m]))`,
			expected: []string{"TIMEOUT"},
		},
		{
			name:     "inner pipeline with a line filter and a label filter",
			query:    `sum(count_over_time({app="api"} |= "database timeout" | trace_id="abc123def456" [5m]))`,
			expected: []string{"DATABASE TIMEOUT", "ABC123DEF456"},
		},
		{
			name:     "inner pipeline keeps a post-parser label filter",
			query:    `count_over_time({app="foo"} |= "database timeout" | json | level="errorrr" [5m])`,
			expected: []string{"DATABASE TIMEOUT", "ERRORRR"},
		},

		{
			name:     "arithmetic of two counts",
			query:    `sum(count_over_time({app="api"} |= "timeout" [5m])) / sum(count_over_time({app="api"} |= "database" [5m]))`,
			expected: nil,
		},
		{
			name:     "comparison of one count",
			query:    `sum(count_over_time({app="api"} |= "timeout" [5m])) > 1`,
			expected: nil,
		},
		{
			name:     "comparison under a vector aggregation",
			query:    `sum(count_over_time({app="api"} |= "timeout" [5m]) > 1)`,
			expected: nil,
		},
		{
			name:     "addition of two ranges under one sum",
			query:    `sum(count_over_time({app="api"} |= "timeout" [5m]) + count_over_time({app="api"} |= "database" [5m]))`,
			expected: nil,
		},
		{
			name:     "or of two ranges",
			query:    `count_over_time({app="api"} |= "timeout" [5m]) or count_over_time({app="api"} |= "database" [5m])`,
			expected: nil,
		},
		{
			name:     "label_replace around an otherwise eligible query",
			query:    `label_replace(sum(count_over_time({app="api"} |= "timeout" [5m])), "dst", "$1", "src", "(.*)")`,
			expected: nil,
		},
		{
			name:     "approx_count_distinct is not a range aggregation",
			query:    `approx_count_distinct(status, {app="api"} |= "timeout" [5m])`,
			expected: nil,
		},
		{
			name:     "no positive filter",
			query:    `count_over_time({app="api"}[5m])`,
			expected: nil,
		},
		{
			name:     "literal shorter than an n-gram",
			query:    `count_over_time({app="api"} |= "db" [5m])`,
			expected: nil,
		},
		{
			name:     "or line filter has no mandatory literal",
			query:    `count_over_time({app="api"} |= "timeout" or "database" [5m])`,
			expected: nil,
		},
		{
			name:     "literal has no logs stage",
			query:    `1`,
			expected: nil,
		},
		{
			name:     "vector has no logs stage",
			query:    `vector(1)`,
			expected: nil,
		},
		{
			name:     "a log query is not a metric query",
			query:    `{app="api"} |= "timeout"`,
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tc.query)
			require.NoError(t, err)

			got := SupportedMetricQuery(expr, ngramLength)
			if tc.expected == nil {
				require.Nil(t, got)
				return
			}
			require.ElementsMatch(t, tc.expected, got)
		})
	}
}
