package logql

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func Test_SplitRangeInterval(t *testing.T) {
	for _, tc := range []struct {
		expr                 string
		expected             string
		expectedSplitQueries int
	}{
		{
			`bytes_over_time({app="foo"}[3s])`,
			`sum without () (
				downstream<bytes_over_time({app="foo"}[1s] offset 2s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[2s]), shard=<nil>>
			)`,
			2,
		},
		{
			`count_over_time({app="foo"}[5s])`,
			`sum without () (
				downstream<count_over_time({app="foo"}[1s] offset 4s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[2s] offset 2s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[2s]), shard=<nil>>
			)`,
			3,
		},
		// Should support expressions with offset operator
		{
			`count_over_time({app="foo"}[4s] offset 1s)`,
			`sum without () (
				downstream<count_over_time({app="foo"}[2s] offset 3s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[2s] offset 1s), shard=<nil>>
			)`,
			2,
		},
		{
			`sum_over_time({app="foo"} | unwrap bar [3s] offset 1s)`,
			`sum without () (
				downstream<sum_over_time({app="foo"} | unwrap bar [1s] offset 3s), shard=<nil>>
				++ downstream<sum_over_time({app="foo"} | unwrap bar [2s] offset 1s), shard=<nil>>
			)`,
			2,
		},
		{
			`sum_over_time({app="foo"} | unwrap bar [5s] offset 0s)`,
			`sum without () (
				downstream<sum_over_time({app="foo"} | unwrap bar [1s] offset 4s), shard=<nil>>
				++ downstream<sum_over_time({app="foo"} | unwrap bar [2s] offset 2s), shard=<nil>>
				++ downstream<sum_over_time({app="foo"} | unwrap bar [2s]), shard=<nil>>
			)`,
			3,
		},
		{
			`count_over_time({app="foo"}[3s] offset -1s)`,
			`sum without () (
				downstream<count_over_time({app="foo"}[1s] offset 1s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[2s] offset -1s), shard=<nil>>
			)`,
			2,
		},
		{
			`rate({app="foo"}[4s] offset 1m)`,
			`(sum without () (
				downstream<count_over_time({app="foo"}[2s] offset 1m2s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[2s] offset 1m0s), shard=<nil>>
			) / 4)`,
			2,
		},
	} {
		tc := tc
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()

			mapperStats := NewMapperStats()
			rvm, err := NewRangeMapper(2*time.Second, nilShardMetrics, mapperStats)
			require.NoError(t, err)

			noop, mappedExpr, err := rvm.Parse(syntax.MustParseExpr(tc.expr))
			require.NoError(t, err)

			require.Equal(t, removeWhiteSpace(tc.expected), removeWhiteSpace(mappedExpr.String()))
			require.Equal(t, tc.expectedSplitQueries, mapperStats.GetSplitQueries())
			require.Equal(t, false, noop)
		})
	}
}

func Test_RangeMapperSplitAlign(t *testing.T) {
	cases := []struct {
		name             string
		expr             string
		queryTime        time.Time
		splityByInterval time.Duration
		expected         string
		expectedSplits   int
	}{
		{
			name: "query_time_aligned_with_split_by",
			expr: `bytes_over_time({app="foo"}[3m])`,
			expected: `sum without() (
                               downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
                               ++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
                               ++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
                        )`,
			queryTime:        time.Unix(60, 0), // 1970 00:01:00
			splityByInterval: 1 * time.Minute,
			expectedSplits:   3,
		},
		{
			name: "query_time_aligned_with_split_by_with_original_offset",
			expr: `bytes_over_time({app="foo"}[3m] offset 20m10s)`, // NOTE: original query has offset, which should be considered in all the splits subquery
			expected: `sum without() (
                               downstream<bytes_over_time({app="foo"}[1m] offset 22m10s), shard=<nil>>
                               ++ downstream<bytes_over_time({app="foo"}[1m] offset 21m10s), shard=<nil>>
                               ++ downstream<bytes_over_time({app="foo"}[1m] offset 20m10s), shard=<nil>>
                        )`,
			queryTime:        time.Unix(60, 0), // 1970 00:01:00
			splityByInterval: 1 * time.Minute,
			expectedSplits:   3,
		},
		{
			name: "query_time_not_aligned_with_split_by",
			expr: `bytes_over_time({app="foo"}[3h])`,
			expected: `sum without() (
                               downstream<bytes_over_time({app="foo"}[6m] offset 2h54m0s), shard=<nil>>
                               ++ downstream<bytes_over_time({app="foo"}[1h] offset 1h54m0s), shard=<nil>>
                               ++ downstream<bytes_over_time({app="foo"}[1h] offset 54m0s), shard=<nil>>
                               ++ downstream<bytes_over_time({app="foo"}[54m]), shard=<nil>>
                        )`,
			queryTime:        time.Date(0, 0, 0, 12, 54, 0, 0, time.UTC), // 1970 12:54:00
			splityByInterval: 1 * time.Hour,
			expectedSplits:   4,
		},
		{
			name: "query_time_not_aligned_with_split_by_with_original_offset",
			expr: `bytes_over_time({app="foo"}[3h] offset 1h2m20s)`, // NOTE: original query has offset, which should be considered in all the splits subquery
			expected: `sum without() (
                               downstream<bytes_over_time({app="foo"}[6m] offset 3h56m20s), shard=<nil>>
                               ++ downstream<bytes_over_time({app="foo"}[1h] offset 2h56m20s), shard=<nil>>
                               ++ downstream<bytes_over_time({app="foo"}[1h] offset 1h56m20s), shard=<nil>>
                               ++ downstream<bytes_over_time({app="foo"}[54m] offset 1h2m20s), shard=<nil>>
                        )`,
			queryTime:        time.Date(0, 0, 0, 12, 54, 0, 0, time.UTC), // 1970 12:54:00
			splityByInterval: 1 * time.Hour,
			expectedSplits:   4,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mapperStats := NewMapperStats()
			rvm, err := NewRangeMapperWithSplitAlign(tc.splityByInterval, tc.queryTime, nilShardMetrics, mapperStats)
			require.NoError(t, err)

			noop, mappedExpr, err := rvm.Parse(syntax.MustParseExpr(tc.expr))
			require.NoError(t, err)

			require.Equal(t, removeWhiteSpace(tc.expected), removeWhiteSpace(mappedExpr.String()))
			require.Equal(t, tc.expectedSplits, mapperStats.GetSplitQueries())
			require.False(t, noop)

		})
	}
}

func Test_SplitRangeVectorMapping(t *testing.T) {
	for _, tc := range []struct {
		expr                 string
		expected             string
		expectedSplitQueries int
	}{
		// Range vector aggregators
		{
			`bytes_over_time({app="foo"}[3m])`,
			`sum without () (
				downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
			)`,
			3,
		},
		{
			`count_over_time({app="foo"}[3m])`,
			`sum without () (
				downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
			)`,
			3,
		},
		{
			`sum_over_time({app="foo"} | unwrap bar [3m])`,
			`sum without () (
				downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
				++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
				++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
			)`,
			3,
		},
		{
			`max_over_time({app="foo"} | unwrap bar [3m])`,
			`max without () (
				downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
			)`,
			3,
		},
		{
			`max_over_time({app="foo"} | json | unwrap bar [3m]) by (bar)`,
			`max by (bar) (
				downstream<max_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s) by (bar), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s) by (bar), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | json | unwrap bar [1m]) by (bar), shard=<nil>>
			)`,
			3,
		},
		{
			`max_over_time({app="foo"} | unwrap bar [3m]) by (baz)`,
			`max by (baz) (
				downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
			)`,
			3,
		},
		{
			`min_over_time({app="foo"} | unwrap bar [3m])`,
			`min without () (
				downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
				++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
				++ downstream<min_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
			)`,
			3,
		},
		{
			`min_over_time({app="foo"} | unwrap bar [3m]) by (baz)`,
			`min by (baz) (
				downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
				++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
				++ downstream<min_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
			)`,
			3,
		},
		{
			`rate({app="foo"}[3m])`,
			`(sum without () (
				downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
			) / 180)`,
			3,
		},
		{
			`rate({app="foo"} | unwrap bar[3m])`,
			`(sum without () (
				   downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
				++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
				++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
			) / 180)`,
			3,
		},
		{
			`bytes_rate({app="foo"}[3m])`,
			`(sum without () (
				downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
			) / 180)`,
			3,
		},

		// Vector aggregator - sum
		{
			`sum(bytes_over_time({app="foo"}[3m]))`,
			`sum(
				sum without () (
					   downstream<sum(bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum(bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum(bytes_over_time({app="foo"}[1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum(count_over_time({app="foo"}[3m]))`,
			`sum(
				sum without () (
					   downstream<sum(count_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum(count_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum(count_over_time({app="foo"}[1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum(sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`sum(
				sum without () (
					   downstream<sum(sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum(sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum(sum_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum(max_over_time({app="foo"} | unwrap bar [3m]))`,
			`sum(
				max without () (
					   downstream<sum(max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum(max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum(max_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum(max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`sum(
				max by (baz) (
					   downstream<sum(max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<sum(max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<sum(max_over_time({app="foo"} | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum(min_over_time({app="foo"}  | unwrap bar [3m]))`,
			`sum(
				min without () (
					   downstream<sum(min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum(min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum(min_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum(min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`sum(
				min by (baz) (
					   downstream<sum(min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<sum(min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<sum(min_over_time({app="foo"} | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum(rate({app="foo"}[3m]))`,
			`sum(
				(sum without () (
					   downstream<sum(count_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum(count_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum(count_over_time({app="foo"}[1m])), shard=<nil>>
				) / 180)
			)`,
			3,
		},
		{
			`sum(bytes_rate({app="foo"}[3m]))`,
			`sum(
				(sum without () (
					   downstream<sum(bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum(bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum(bytes_over_time({app="foo"}[1m])), shard=<nil>>
				) / 180)
			)`,
			3,
		},

		// Vector aggregator - sum by
		{
			`sum by (baz) (bytes_over_time({app="foo"}[3m]))`,
			`sum by (baz) (
				sum without () (
					downstream<sum by (baz) (bytes_over_time({app="foo"} [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum by (baz) (bytes_over_time({app="foo"} [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum by (baz) (bytes_over_time({app="foo"} [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum by (baz) (count_over_time({app="foo"}[3m]))`,
			`sum by (baz) (
				sum without () (
					downstream<sum by (baz) (count_over_time({app="foo"} [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum by (baz) (count_over_time({app="foo"} [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum by (baz) (count_over_time({app="foo"} [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum by (baz) (sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`sum by (baz) (
				sum without () (
					downstream<sum by (baz) (sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum by (baz) (sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum by (baz) (sum_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum by (baz) (max_over_time({app="foo"} | unwrap bar [3m]))`,
			`sum by (baz) (
				max without () (
					downstream<sum by (baz) (max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum by (baz) (max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum by (baz) (max_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum by (baz) (max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`sum by (baz) (
				max by (baz) (
					downstream<sum by (baz) (max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<sum by (baz) (max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<sum by (baz) (max_over_time({app="foo"} | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum by (baz) (min_over_time({app="foo"} | unwrap bar [3m]))`,
			`sum by (baz) (
				min without () (
					downstream<sum by (baz) (min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum by (baz) (min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum by (baz) (min_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum by (baz) (min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`sum by (baz) (
				min by (baz) (
					downstream<sum by (baz) (min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<sum by (baz) (min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<sum by (baz) (min_over_time({app="foo"} | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum by (baz) (rate({app="foo"}[3m]))`,
			`sum by (baz) (
					(sum without () (
						downstream<sum by (baz) (count_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
						++ downstream<sum by (baz) (count_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
						++ downstream<sum by (baz) (count_over_time({app="foo"}[1m])), shard=<nil>>
					) / 180)
				)`,
			3,
		},
		{
			`sum by (baz) (bytes_rate({app="foo"}[3m]))`,
			`sum by (baz) (
					(sum without () (
						downstream<sum by (baz) (bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
						++ downstream<sum by (baz) (bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
						++ downstream<sum by (baz) (bytes_over_time({app="foo"}[1m])), shard=<nil>>
					) / 180)
				)`,
			3,
		},

		// Vector aggregator - count
		{
			`count(bytes_over_time({app="foo"}[3m]))`,
			`count(
				sum without () (
					downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count(count_over_time({app="foo"}[3m]))`,
			`count(
				sum without () (
					downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count(sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`count(
				sum without () (
					downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count(max_over_time({app="foo"} | unwrap bar [3m]))`,
			`count(
				max without () (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count(max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`count(
				max by (baz) (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count(min_over_time({app="foo"} | unwrap bar [3m]))`,
			`count(
				min without () (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count(min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`count(
				min by (baz) (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count(rate({app="foo"}[3m]))`,
			`count(
				(sum without () (
					   downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				) / 180)
			)`,
			3,
		},
		{
			`count(bytes_rate({app="foo"}[3m]))`,
			`count(
				(sum without () (
					   downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
				) / 180)
			)`,
			3,
		},

		// Vector aggregator - count by
		{
			`count by (baz) (bytes_over_time({app="foo"}[3m]))`,
			`count by (baz) (
				sum without () (
					   downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count by (baz) (count_over_time({app="foo"}[3m]))`,
			`count by (baz) (
				sum without () (
					   downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count by (baz) (sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`count by (baz) (
				sum without () (
					   downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count by (baz) (max_over_time({app="foo"} | unwrap bar [3m]))`,
			`count by (baz) (
				max without () (
					   downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count by (baz) (max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`count by (baz) (
				max by (baz) (
					   downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count by (baz) (min_over_time({app="foo"} | unwrap bar [3m]))`,
			`count by (baz) (
				min without () (
					   downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count by (baz) (min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`count by (baz) (
				min by (baz) (
					   downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count by (baz) (rate({app="foo"}[3m]))`,
			`count by (baz) (
					(sum without () (
						   downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
						++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
						++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
					) / 180)
				)`,
			3,
		},
		{
			`count by (baz) (bytes_rate({app="foo"}[3m]))`,
			`count by (baz) (
					(sum without () (
						   downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
						++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
						++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
					) / 180)
				)`,
			3,
		},

		// Vector aggregator - max
		{
			`max(bytes_over_time({app="foo"}[3m]))`,
			`max(
				sum without () (
					   downstream<max(bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<max(bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<max(bytes_over_time({app="foo"}[1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max(count_over_time({app="foo"}[3m]))`,
			`max(
				sum without () (
					   downstream<max(count_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<max(count_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<max(count_over_time({app="foo"}[1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max(sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`max(
				sum without () (
					   downstream<max(sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<max(sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<max(sum_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max(max_over_time({app="foo"} | unwrap bar [3m]))`,
			`max(
				max without () (
					   downstream<max(max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<max(max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<max(max_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max(max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`max(
				max by (baz) (
					   downstream<max(max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<max(max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<max(max_over_time({app="foo"} | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max(min_over_time({app="foo"} | unwrap bar [3m]))`,
			`max(
				min without () (
					   downstream<max(min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<max(min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<max(min_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max(min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`max(
				min by (baz) (
					   downstream<max(min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<max(min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<max(min_over_time({app="foo"} | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max(rate({app="foo"}[3m]))`,
			`max(
				(sum without () (
					   downstream<max(count_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<max(count_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<max(count_over_time({app="foo"}[1m])), shard=<nil>>
				) / 180)
			)`,
			3,
		},
		{
			`max(bytes_rate({app="foo"}[3m]))`,
			`max(
				(sum without () (
					   downstream<max(bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<max(bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<max(bytes_over_time({app="foo"}[1m])), shard=<nil>>
				) / 180)
			)`,
			3,
		},

		// Vector aggregator - max by
		{
			`max by (baz) (bytes_over_time({app="foo"}[3m]))`,
			`max by (baz) (
				sum without () (
					   downstream<max by (baz) (bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<max by (baz) (bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<max by (baz) (bytes_over_time({app="foo"}[1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max by (baz) (count_over_time({app="foo"}[3m]))`,
			`max by (baz) (
				sum without () (
                       downstream<max by (baz) (count_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<max by (baz) (count_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<max by (baz) (count_over_time({app="foo"}[1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max by (baz) (sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`max by (baz) (
				sum without () (
					   downstream<max by (baz) (sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<max by (baz) (sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<max by (baz) (sum_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max by (baz) (max_over_time({app="foo"} | unwrap bar [3m]))`,
			`max by (baz) (
				max without () (
					   downstream<max by (baz) (max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<max by (baz) (max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<max by (baz) (max_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max by (baz) (max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`max by (baz) (
				max by (baz) (
					   downstream<max by (baz) (max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<max by (baz) (max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<max by (baz) (max_over_time({app="foo"} | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max by (baz) (min_over_time({app="foo"} | unwrap bar [3m]))`,
			`max by (baz) (
				min without () (
					   downstream<max by (baz) (min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<max by (baz) (min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<max by (baz) (min_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max by (baz) (min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`max by (baz) (
				min by (baz) (
					   downstream<max by (baz) (min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<max by (baz) (min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<max by (baz) (min_over_time({app="foo"} | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max by (baz) (rate({app="foo"}[3m]))`,
			`max by (baz) (
					(sum without () (
						   downstream<max by (baz) (count_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
						++ downstream<max by (baz) (count_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
						++ downstream<max by (baz) (count_over_time({app="foo"}[1m])), shard=<nil>>
					) / 180)
				)`,
			3,
		},
		{
			`max by (baz) (bytes_rate({app="foo"}[3m]))`,
			`max by (baz) (
					(sum without () (
						   downstream<max by (baz) (bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
						++ downstream<max by (baz) (bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
						++ downstream<max by (baz) (bytes_over_time({app="foo"}[1m])), shard=<nil>>
					) / 180)
				)`,
			3,
		},

		// Vector aggregator - min
		{
			`min(bytes_over_time({app="foo"}[3m]))`,
			`min(
				sum without () (
					   downstream<min(bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<min(bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<min(bytes_over_time({app="foo"}[1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min(count_over_time({app="foo"}[3m]))`,
			`min(
				sum without () (
					   downstream<min(count_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<min(count_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<min(count_over_time({app="foo"}[1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min(sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`min(
				sum without () (
					   downstream<min(sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<min(sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<min(sum_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min(max_over_time({app="foo"} | unwrap bar [3m]))`,
			`min(
				max without () (
					   downstream<min(max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<min(max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<min(max_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min(max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`min(
				max by (baz) (
					   downstream<min(max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<min(max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<min(max_over_time({app="foo"} | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min(min_over_time({app="foo"} | unwrap bar [3m]))`,
			`min(
				min without () (
					   downstream<min(min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<min(min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<min(min_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min(min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`min(
				min by (baz) (
					   downstream<min(min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<min(min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<min(min_over_time({app="foo"} | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min(rate({app="foo"}[3m]))`,
			`min(
				(sum without () (
					   downstream<min(count_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<min(count_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<min(count_over_time({app="foo"}[1m])), shard=<nil>>
				) / 180)
			)`,
			3,
		},
		{
			`min(bytes_rate({app="foo"}[3m]))`,
			`min(
				(sum without () (
					   downstream<min(bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<min(bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<min(bytes_over_time({app="foo"}[1m])), shard=<nil>>
				) / 180)
			)`,
			3,
		},

		// Vector aggregator - min by
		{
			`min by (baz) (bytes_over_time({app="foo"}[3m]))`,
			`min by (baz) (
				sum without () (
					   downstream<min by (baz) (bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<min by (baz) (bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<min by (baz) (bytes_over_time({app="foo"}[1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min by (baz) (count_over_time({app="foo"}[3m]))`,
			`min by (baz) (
				sum without () (
					   downstream<min by (baz) (count_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
					++ downstream<min by (baz) (count_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
					++ downstream<min by (baz) (count_over_time({app="foo"}[1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min by (baz) (sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`min by (baz) (
				sum without () (
					   downstream<min by (baz) (sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<min by (baz) (sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<min by (baz) (sum_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min by (baz) (max_over_time({app="foo"} | unwrap bar [3m]))`,
			`min by (baz) (
				max without () (
					   downstream<min by (baz) (max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<min by (baz) (max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<min by (baz) (max_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min by (baz) (max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`min by (baz) (
				max by (baz) (
					   downstream<min by (baz) (max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<min by (baz) (max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<min by (baz) (max_over_time({app="foo"} | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min by (baz) (min_over_time({app="foo"} | unwrap bar [3m]))`,
			`min by (baz) (
				min without () (
					   downstream<min by (baz) (min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<min by (baz) (min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<min by (baz) (min_over_time({app="foo"} | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min by (baz) (min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`min by (baz) (
				min by (baz) (
					   downstream<min by (baz) (min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<min by (baz) (min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<min by (baz) (min_over_time({app="foo"} | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min by (baz) (rate({app="foo"}[3m]))`,
			`min by (baz) (
					(sum without () (
						   downstream<min by (baz) (count_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
						++ downstream<min by (baz) (count_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
						++ downstream<min by (baz) (count_over_time({app="foo"}[1m])), shard=<nil>>
					) / 180)
				)`,
			3,
		},
		{
			`min by (baz) (bytes_rate({app="foo"}[3m]))`,
			`min by (baz) (
					(sum without () (
						   downstream<min by (baz) (bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
						++ downstream<min by (baz) (bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
						++ downstream<min by (baz) (bytes_over_time({app="foo"}[1m])), shard=<nil>>
					) / 180)
				)`,
			3,
		},

		// Label extraction stage
		{
			`max_over_time({app="foo"} | logfmt | unwrap bar [3m]) by (baz)`,
			`max by (baz) (
				downstream<max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | logfmt | unwrap bar [1m]) by (baz), shard=<nil>>
			)`,
			3,
		},
		{
			`min_over_time({app="foo"} | json | unwrap bar [3m]) by (baz)`,
			`min by (baz) (
				downstream<min_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
				++ downstream<min_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
				++ downstream<min_over_time({app="foo"} | json | unwrap bar [1m]) by (baz), shard=<nil>>
			)`,
			3,
		},
		{
			`sum(bytes_over_time({app="foo"} | logfmt [3m]))`,
			`sum(
				downstream<sum(bytes_over_time({app="foo"} | logfmt [1m] offset 2m0s)), shard=<nil>>
				++ downstream<sum(bytes_over_time({app="foo"} | logfmt [1m] offset 1m0s)), shard=<nil>>
				++ downstream<sum(bytes_over_time({app="foo"} | logfmt [1m])), shard=<nil>>
			)`,
			3,
		},
		{
			`sum(count_over_time({app="foo"} | json [3m]))`,
			`sum(
				downstream<sum(count_over_time({app="foo"} | json [1m] offset 2m0s)), shard=<nil>>
				++ downstream<sum(count_over_time({app="foo"} | json [1m] offset 1m0s)), shard=<nil>>
				++ downstream<sum(count_over_time({app="foo"} | json [1m])), shard=<nil>>
			)`,
			3,
		},
		{
			`sum(sum_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`sum(
				sum without () (
					   downstream<sum(sum_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum(sum_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum(sum_over_time({app="foo"} | logfmt | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum(max_over_time({app="foo"} | json | unwrap bar [3m]))`,
			`sum(
				max without () (
					   downstream<sum(max_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum(max_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum(max_over_time({app="foo"} | json | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum(max_over_time({app="foo"} | logfmt | unwrap bar [3m]) by (baz))`,
			`sum(
				max by (baz) (
					   downstream<sum(max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<sum(max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<sum(max_over_time({app="foo"} | logfmt | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum(min_over_time({app="foo"} | json | unwrap bar [3m]))`,
			`sum(
				min without () (
					   downstream<sum(min_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum(min_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum(min_over_time({app="foo"} | json | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum(min_over_time({app="foo"} | logfmt | unwrap bar [3m]) by (baz))`,
			`sum(
				min by (baz) (
					   downstream<sum(min_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<sum(min_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<sum(min_over_time({app="foo"} | logfmt | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum(rate({app="foo"} | json [3m]))`,
			`sum(
					(sum without () (
						downstream<sum(count_over_time({app="foo"} | json [1m] offset 2m0s)), shard=<nil>>
						++ downstream<sum(count_over_time({app="foo"} | json [1m] offset 1m0s)), shard=<nil>>
						++ downstream<sum(count_over_time({app="foo"} | json [1m])), shard=<nil>>
					) / 180)
				)`,
			3,
		},
		{
			`sum(bytes_rate({app="foo"} | logfmt [3m]))`,
			`sum(
					(sum without () (
						downstream<sum(bytes_over_time({app="foo"} | logfmt [1m] offset 2m0s)), shard=<nil>>
						++ downstream<sum(bytes_over_time({app="foo"} | logfmt [1m] offset 1m0s)), shard=<nil>>
						++ downstream<sum(bytes_over_time({app="foo"} | logfmt [1m])), shard=<nil>>
					) / 180)
				)`,
			3,
		},
		{
			`sum by (baz) (bytes_over_time({app="foo"} | json [3m]))`,
			`sum by (baz) (
				downstream<sum by (baz) (bytes_over_time({app="foo"} | json [1m] offset 2m0s)), shard=<nil>>
				++ downstream<sum by (baz) (bytes_over_time({app="foo"} | json [1m] offset 1m0s)), shard=<nil>>
				++ downstream<sum by (baz) (bytes_over_time({app="foo"} | json [1m])), shard=<nil>>
			)`,
			3,
		},
		{
			`sum by (baz) (count_over_time({app="foo"} | logfmt [3m]))`,
			`sum by (baz) (
				downstream<sum by (baz) (count_over_time({app="foo"} | logfmt [1m] offset 2m0s)), shard=<nil>>
				++ downstream<sum by (baz) (count_over_time({app="foo"} | logfmt [1m] offset 1m0s)), shard=<nil>>
				++ downstream<sum by (baz) (count_over_time({app="foo"} | logfmt [1m])), shard=<nil>>
			)`,
			3,
		},
		{
			`sum by (baz) (sum_over_time({app="foo"} | json | unwrap bar [3m]))`,
			`sum by (baz) (
				sum without () (
					downstream<sum by (baz) (sum_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum by (baz) (sum_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum by (baz) (sum_over_time({app="foo"} | json | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`sum by (baz) (
				max without () (
					downstream<sum by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum by (baz) (max_over_time({app="foo"} | json | unwrap bar [3m]) by (baz))`,
			`sum by (baz) (
				max by (baz) (
					downstream<sum by (baz) (max_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<sum by (baz) (max_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<sum by (baz) (max_over_time({app="foo"} | json | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum by (baz) (min_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`sum by (baz) (
				min without () (
					downstream<sum by (baz) (min_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum by (baz) (min_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum by (baz) (min_over_time({app="foo"} | logfmt | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum by (baz) (min_over_time({app="foo"} | json | unwrap bar [3m]) by (baz))`,
			`sum by (baz) (
				min by (baz) (
					downstream<sum by (baz) (min_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<sum by (baz) (min_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<sum by (baz) (min_over_time({app="foo"} | json | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`sum by (baz) (rate({app="foo"} | logfmt [3m]))`,
			`sum by (baz) (
					(sum without () (
						downstream<sum by (baz) (count_over_time({app="foo"} | logfmt [1m] offset 2m0s)), shard=<nil>>
						++ downstream<sum by (baz) (count_over_time({app="foo"} | logfmt [1m] offset 1m0s)), shard=<nil>>
						++ downstream<sum by (baz) (count_over_time({app="foo"} | logfmt [1m])), shard=<nil>>
					) / 180)
				)`,
			3,
		},
		{
			`sum by (baz) (bytes_rate({app="foo"} | json [3m]))`,
			`sum by (baz) (
					(sum without () (
						downstream<sum by (baz) (bytes_over_time({app="foo"} | json [1m] offset 2m0s)), shard=<nil>>
						++ downstream<sum by (baz) (bytes_over_time({app="foo"} | json [1m] offset 1m0s)), shard=<nil>>
						++ downstream<sum by (baz) (bytes_over_time({app="foo"} | json [1m])), shard=<nil>>
					) / 180)
				)`,
			3,
		},
		{
			`count(max_over_time({app="foo"} | logfmt | unwrap bar [3m]) by (baz))`,
			`count(
				max by (baz) (
					downstream<max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | logfmt | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count(min_over_time({app="foo"} | json | unwrap bar [3m]) by (baz))`,
			`count(
				min by (baz) (
					downstream<min_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | json | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [3m]) by (baz))`,
			`count by (baz) (
				max by (baz) (
					downstream<max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | logfmt | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count by (baz) (min_over_time({app="foo"} | json | unwrap bar [3m]) by (baz))`,
			`count by (baz) (
				min by (baz) (
					downstream<min_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | json | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max(bytes_over_time({app="foo"} | logfmt [3m]))`,
			`max(
				downstream<max(bytes_over_time({app="foo"} | logfmt [1m] offset 2m0s)), shard=<nil>>
				++ downstream<max(bytes_over_time({app="foo"} | logfmt [1m] offset 1m0s)), shard=<nil>>
				++ downstream<max(bytes_over_time({app="foo"} | logfmt [1m])), shard=<nil>>
			)`,
			3,
		},
		{
			`max(count_over_time({app="foo"} | json [3m]))`,
			`max(
				downstream<max(count_over_time({app="foo"} | json [1m] offset 2m0s)), shard=<nil>>
				++ downstream<max(count_over_time({app="foo"} | json [1m] offset 1m0s)), shard=<nil>>
				++ downstream<max(count_over_time({app="foo"} | json [1m])), shard=<nil>>
			)`,
			3,
		},
		{
			`max(sum_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`max(
				sum without () (
					   downstream<max(sum_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<max(sum_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<max(sum_over_time({app="foo"} | logfmt | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max(max_over_time({app="foo"} | json | unwrap bar [3m]))`,
			`max(
				max without () (
					   downstream<max(max_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<max(max_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<max(max_over_time({app="foo"} | json | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max(max_over_time({app="foo"} | logfmt | unwrap bar [3m]) by (baz))`,
			`max(
				max by (baz) (
					   downstream<max(max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<max(max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<max(max_over_time({app="foo"} | logfmt | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max(min_over_time({app="foo"} | json | unwrap bar [3m]))`,
			`max(
				min without () (
					   downstream<max(min_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<max(min_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<max(min_over_time({app="foo"} | json | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max(min_over_time({app="foo"} | logfmt | unwrap bar [3m]) by (baz))`,
			`max(
				min by (baz) (
					   downstream<max(min_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<max(min_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<max(min_over_time({app="foo"} | logfmt | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max by (baz) (bytes_over_time({app="foo"} | json [3m]))`,
			`max by (baz) (
				downstream<max by (baz) (bytes_over_time({app="foo"} | json [1m] offset 2m0s)), shard=<nil>>
				++ downstream<max by (baz) (bytes_over_time({app="foo"} | json [1m] offset 1m0s)), shard=<nil>>
				++ downstream<max by (baz) (bytes_over_time({app="foo"} | json [1m])), shard=<nil>>
			)`,
			3,
		},
		{
			`max by (baz) (count_over_time({app="foo"} | logfmt [3m]))`,
			`max by (baz) (
				downstream<max by (baz) (count_over_time({app="foo"} | logfmt [1m] offset 2m0s)), shard=<nil>>
				++ downstream<max by (baz) (count_over_time({app="foo"} | logfmt [1m] offset 1m0s)), shard=<nil>>
				++ downstream<max by (baz) (count_over_time({app="foo"} | logfmt [1m])), shard=<nil>>
			)`,
			3,
		},
		{
			`max by (baz) (sum_over_time({app="foo"} | json | unwrap bar [3m]))`,
			`max by (baz) (
				sum without () (
					downstream<max by (baz) (sum_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<max by (baz) (sum_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<max by (baz) (sum_over_time({app="foo"} | json | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`max by (baz) (
				max without () (
					downstream<max by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<max by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<max by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max by (baz) (max_over_time({app="foo"} | json | unwrap bar [3m]) by (baz))`,
			`max by (baz) (
				max by (baz) (
					downstream<max by (baz) (max_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<max by (baz) (max_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<max by (baz) (max_over_time({app="foo"} | json | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max by (baz) (min_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`max by (baz) (
				min without () (
					downstream<max by (baz) (min_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<max by (baz) (min_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<max by (baz) (min_over_time({app="foo"} | logfmt | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`max by (baz) (min_over_time({app="foo"} | json | unwrap bar [3m]) by (baz))`,
			`max by (baz) (
				min by (baz) (
					downstream<max by (baz) (min_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<max by (baz) (min_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<max by (baz) (min_over_time({app="foo"} | json | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min(bytes_over_time({app="foo"} | logfmt [3m]))`,
			`min(
				downstream<min(bytes_over_time({app="foo"} | logfmt [1m] offset 2m0s)), shard=<nil>>
				++ downstream<min(bytes_over_time({app="foo"} | logfmt [1m] offset 1m0s)), shard=<nil>>
				++ downstream<min(bytes_over_time({app="foo"} | logfmt [1m])), shard=<nil>>
			)`,
			3,
		},
		{
			`min(count_over_time({app="foo"} | json [3m]))`,
			`min(
				downstream<min(count_over_time({app="foo"} | json [1m] offset 2m0s)), shard=<nil>>
				++ downstream<min(count_over_time({app="foo"} | json [1m] offset 1m0s)), shard=<nil>>
				++ downstream<min(count_over_time({app="foo"} | json [1m])), shard=<nil>>
			)`,
			3,
		},
		{
			`min(sum_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`min(
				sum without () (
					   downstream<min(sum_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<min(sum_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<min(sum_over_time({app="foo"} | logfmt | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min(max_over_time({app="foo"} | json | unwrap bar [3m]))`,
			`min(
				max without () (
					   downstream<min(max_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<min(max_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<min(max_over_time({app="foo"} | json | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min(max_over_time({app="foo"} | logfmt | unwrap bar [3m]) by (baz))`,
			`min(
				max by (baz) (
					   downstream<min(max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<min(max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<min(max_over_time({app="foo"} | logfmt | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min(min_over_time({app="foo"} | json | unwrap bar [3m]))`,
			`min(
				min without () (
					   downstream<min(min_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<min(min_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<min(min_over_time({app="foo"} | json | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min(min_over_time({app="foo"} | logfmt | unwrap bar [3m]) by (baz))`,
			`min(
				min by (baz) (
					   downstream<min(min_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<min(min_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<min(min_over_time({app="foo"} | logfmt | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min by (baz) (bytes_over_time({app="foo"} | json [3m]))`,
			`min by (baz) (
				downstream<min by (baz) (bytes_over_time({app="foo"} | json [1m] offset 2m0s)), shard=<nil>>
				++ downstream<min by (baz) (bytes_over_time({app="foo"} | json [1m] offset 1m0s)), shard=<nil>>
				++ downstream<min by (baz) (bytes_over_time({app="foo"} | json [1m])), shard=<nil>>
			)`,
			3,
		},
		{
			`min by (baz) (count_over_time({app="foo"} | logfmt [3m]))`,
			`min by (baz) (
				downstream<min by (baz) (count_over_time({app="foo"} | logfmt [1m] offset 2m0s)), shard=<nil>>
				++ downstream<min by (baz) (count_over_time({app="foo"} | logfmt [1m] offset 1m0s)), shard=<nil>>
				++ downstream<min by (baz) (count_over_time({app="foo"} | logfmt [1m])), shard=<nil>>
			)`,
			3,
		},
		{
			`min by (baz) (sum_over_time({app="foo"} | json | unwrap bar [3m]))`,
			`min by (baz) (
				sum without () (
					downstream<min by (baz) (sum_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<min by (baz) (sum_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<min by (baz) (sum_over_time({app="foo"} | json | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`min by (baz) (
				max without () (
					downstream<min by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<min by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<min by (baz) (max_over_time({app="foo"} | logfmt | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min by (baz) (max_over_time({app="foo"} | json | unwrap bar [3m]) by (baz))`,
			`min by (baz) (
				max by (baz) (
					downstream<min by (baz) (max_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<min by (baz) (max_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<min by (baz) (max_over_time({app="foo"} | json | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min by (baz) (min_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`min by (baz) (
				min without () (
					downstream<min by (baz) (min_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 2m0s)), shard=<nil>>
					++ downstream<min by (baz) (min_over_time({app="foo"} | logfmt | unwrap bar [1m] offset 1m0s)), shard=<nil>>
					++ downstream<min by (baz) (min_over_time({app="foo"} | logfmt | unwrap bar [1m])), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`min by (baz) (min_over_time({app="foo"} | json | unwrap bar [3m]) by (baz))`,
			`min by (baz) (
				min by (baz) (
					downstream<min by (baz) (min_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s) by (baz)), shard=<nil>>
					++ downstream<min by (baz) (min_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s) by (baz)), shard=<nil>>
					++ downstream<min by (baz) (min_over_time({app="foo"} | json | unwrap bar [1m]) by (baz)), shard=<nil>>
				)
			)`,
			3,
		},

		// Binary operations
		{
			`2 * bytes_over_time({app="foo"}[3m])`,
			`(
				2 *
				sum without () (
				   downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			3,
		},
		{
			`count_over_time({app="foo"}[3m]) * 2`,
			`(
				sum without () (
				   downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				)
				* 2
			)`,
			3,
		},
		{
			`bytes_over_time({app="foo"}[3m]) + count_over_time({app="foo"}[4m])`,
			`(sum without () (
				   downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
			) +
			sum without () (
				downstream<count_over_time({app="foo"}[1m] offset 3m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
			))
			`,
			7,
		},
		{
			`sum(count_over_time({app="foo"}[3m]) * count(sum_over_time({app="foo"} | unwrap bar [4m])))`,
			`sum(
				(sum without () (
					   downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				) *
				count (
					sum without () (
						downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 3m0s), shard=<nil>>
						++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
						++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
						++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
					)
				))
			)
			`,
			7,
		},
		{
			`sum by (app) (bytes_rate({app="foo"}[3m])) / sum by (app) (rate({app="foo"}[3m]))`,
			`(
				sum by (app) (
					(sum without () (
						   downstream<sum by (app) (bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
						++ downstream<sum by (app) (bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
						++ downstream<sum by (app) (bytes_over_time({app="foo"}[1m])), shard=<nil>>
					) / 180)
				)
				/
				sum by (app) (
					(sum without () (
						   downstream<sum by (app) (count_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
						++ downstream<sum by (app) (count_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
						++ downstream<sum by (app) (count_over_time({app="foo"}[1m])), shard=<nil>>
					) / 180)
				)
			)`,
			6,
		},
		{
			`sum by (app) (count_over_time({app="foo"} | logfmt | duration > 10s [3m])) / sum (count_over_time({app="foo"} [3m]))`,
			`(
				sum by (app) (
					downstream<sum by (app) (count_over_time({app="foo"} | logfmt | duration > 10s [1m] offset 2m0s)), shard=<nil>>
					++ downstream<sum by (app) (count_over_time({app="foo"} | logfmt | duration > 10s [1m] offset 1m0s)), shard=<nil>>
					++ downstream<sum by (app) (count_over_time({app="foo"} | logfmt | duration > 10s [1m])), shard=<nil>>
				)
				/
				sum (
					sum without () (
						   downstream<sum (count_over_time({app="foo"} [1m] offset 2m0s)), shard=<nil>>
						++ downstream<sum (count_over_time({app="foo"} [1m] offset 1m0s)), shard=<nil>>
						++ downstream<sum (count_over_time({app="foo"} [1m])), shard=<nil>>
					)
				)
			)`,
			6,
		},

		// Multi vector aggregator layer queries
		{
			`sum(max(bytes_over_time({app="foo"}[3m])))`,
			`sum(
				max(
					sum without () (
						   downstream<max(bytes_over_time({app="foo"}[1m] offset 2m0s)), shard=<nil>>
						++ downstream<max(bytes_over_time({app="foo"}[1m] offset 1m0s)), shard=<nil>>
						++ downstream<max(bytes_over_time({app="foo"}[1m])), shard=<nil>>
					)
				)
			)
			`,
			3,
		},

		// Non-splittable vector aggregators - should go deeper in the AST
		{
			`topk(2, count_over_time({app="foo"}[3m]))`,
			`topk(2,
				sum without () (
					   downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			3,
		},

		// regression test queries
		{
			`topk(10,sum by (org_id) (rate({container="query-frontend",namespace="loki"} |= "metrics.go" | logfmt | unwrap bytes(total_bytes) | __error__="" [3m])))`,
			`topk(10,
			  sum by (org_id) (
					(
						sum without () (
							   downstream<sum by(org_id)(sum_over_time({container="query-frontend",namespace="loki"} |= "metrics.go" | logfmt | unwrap bytes(total_bytes) | __error__="" [1m] offset 2m0s)),shard=<nil>>
              				++ downstream<sum by(org_id)(sum_over_time({container="query-frontend",namespace="loki"} |= "metrics.go" | logfmt | unwrap bytes(total_bytes) | __error__="" [1m] offset 1m0s)),shard=<nil>>
							++ downstream<sum by(org_id)(sum_over_time({container="query-frontend",namespace="loki"} |= "metrics.go" | logfmt | unwrap bytes(total_bytes) | __error__="" [1m])),shard=<nil>>
				    )
					/ 180
				  )
				)
			)`,
			3,
		},

		// label_replace
		{
			`label_replace(sum by (baz) (count_over_time({app="foo"}[3m])), "x", "$1", "a", "(.*)")`,
			`label_replace(
				sum by (baz) (
					sum without () (
						downstream<sum by (baz) (count_over_time({app="foo"} [1m] offset 2m0s)), shard=<nil>>
						++ downstream<sum by (baz) (count_over_time({app="foo"} [1m] offset 1m0s)), shard=<nil>>
						++ downstream<sum by (baz) (count_over_time({app="foo"} [1m])), shard=<nil>>
					)
				),
				"x", "$1", "a", "(.*)"
			)`,
			3,
		},
		{
			`label_replace(rate({job="api-server", service="a:c"} |= "err" [3m]), "foo", "$1", "service", "(.*):.*")`,
			`label_replace(
				(
					sum without () (
						downstream<count_over_time({job="api-server",service="a:c"} |= "err" [1m] offset 2m0s), shard=<nil>>
						++ downstream<count_over_time({job="api-server",service="a:c"} |= "err" [1m] offset 1m0s), shard=<nil>>
						++ downstream<count_over_time({job="api-server",service="a:c"} |= "err" [1m]), shard=<nil>>
					)
				/ 180),
				"foo", "$1", "service", "(.*):.*"
			)`,
			3,
		},
	} {
		tc := tc
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()

			mapperStats := NewMapperStats()
			rvm, err := NewRangeMapper(time.Minute, nilShardMetrics, mapperStats)
			require.NoError(t, err)

			noop, mappedExpr, err := rvm.Parse(syntax.MustParseExpr(tc.expr))
			require.NoError(t, err)

			require.Equal(t, removeWhiteSpace(tc.expected), removeWhiteSpace(mappedExpr.String()))
			require.Equal(t, tc.expectedSplitQueries, mapperStats.GetSplitQueries())
			require.False(t, noop)
		})
	}
}

func Test_SplitRangeVectorMapping_Noop(t *testing.T) {
	for _, tc := range []struct {
		expr     string
		expected string
	}{
		// Non-splittable range vector aggregators
		{
			`quantile_over_time(0.95, {app="foo"} | unwrap bar[3m])`,
			`quantile_over_time(0.95, {app="foo"} | unwrap bar[3m])`,
		},
		{
			`sum(avg_over_time({app="foo"} | unwrap bar[3m]))`,
			`sum(avg_over_time({app="foo"} | unwrap bar[3m]))`,
		},

		// should be noop if range interval is lower or equal to split interval (1m)
		{
			`bytes_over_time({app="foo"}[1m])`,
			`bytes_over_time({app="foo"}[1m])`,
		},

		// should be noop if inner range aggregation includes a stage for label extraction such as `| json` or `| logfmt`
		// because otherwise the downstream queries would result in too many series
		{
			`bytes_over_time({app="foo"} | logfmt [3m])`,
			`bytes_over_time({app="foo"} | logfmt [3m])`,
		},
		{
			`count_over_time({app="foo"} | json [3m])`,
			`count_over_time({app="foo"} | json [3m])`,
		},
		{
			`sum_over_time({app="foo"} | logfmt | unwrap bar [3m])`,
			`sum_over_time({app="foo"} | logfmt | unwrap bar [3m])`,
		},
		{
			`max_over_time({app="foo"} | json | unwrap bar [3m])`,
			`max_over_time({app="foo"} | json | unwrap bar [3m])`,
		},
		{
			`min_over_time({app="foo"} | logfmt | unwrap bar [3m])`,
			`min_over_time({app="foo"} | logfmt | unwrap bar [3m])`,
		},
		{
			`rate({app="foo"} | json [3m])`,
			`rate({app="foo"} | json [3m])`,
		},
		{
			`bytes_rate({app="foo"} | logfmt [3m])`,
			`bytes_rate({app="foo"} | logfmt [3m])`,
		},
		// should be noop if inner range aggregation includes a stage for label extraction
		// and the vector aggregator is count
		{
			`count(bytes_over_time({app="foo"} | logfmt [3m]))`,
			`count(bytes_over_time({app="foo"} | logfmt [3m]))`,
		},
		{
			`count by (foo) (bytes_over_time({app="foo"} | json [3m]))`,
			`count by (foo) (bytes_over_time({app="foo"} | json [3m]))`,
		},
		{
			`count(count_over_time({app="foo"} | logfmt [3m]))`,
			`count(count_over_time({app="foo"} | logfmt [3m]))`,
		},
		{
			`count by (foo) (count_over_time({app="foo"} | json [3m]))`,
			`count by (foo) (count_over_time({app="foo"} | json [3m]))`,
		},
		{
			`count(sum_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`count(sum_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
		},
		{
			`count by (foo) (sum_over_time({app="foo"} | json | unwrap bar [3m]))`,
			`count by (foo) (sum_over_time({app="foo"} | json | unwrap bar [3m]))`,
		},
		{
			`count(max_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`count(max_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
		},
		{
			`count by (foo) (max_over_time({app="foo"} | json | unwrap bar [3m]))`,
			`count by (foo) (max_over_time({app="foo"} | json | unwrap bar [3m]))`,
		},
		{
			`count(min_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`count(min_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
		},
		{
			`count by (foo) (min_over_time({app="foo"} | json | unwrap bar [3m]))`,
			`count by (foo) (min_over_time({app="foo"} | json | unwrap bar [3m]))`,
		},
		{
			`count(rate({app="foo"} | logfmt [3m]))`,
			`count(rate({app="foo"} | logfmt [3m]))`,
		},
		{
			`count by (foo) (rate({app="foo"} | json [3m]))`,
			`count by (foo) (rate({app="foo"} | json [3m]))`,
		},
		{
			`count(bytes_rate({app="foo"} | logfmt [3m]))`,
			`count(bytes_rate({app="foo"} | logfmt [3m]))`,
		},
		{
			`count by (foo) (bytes_rate({app="foo"} | json [3m]))`,
			`count by (foo) (bytes_rate({app="foo"} | json [3m]))`,
		},
		// should be noop if inner range aggregation includes a stage for label extraction
		// the vector aggregator is max or min and the inner range aggregator is rate or bytes_rate
		{
			`max(rate({app="foo"} | logfmt [3m]))`,
			`max(rate({app="foo"} | logfmt [3m]))`,
		},
		{
			`max by (foo) (rate({app="foo"} | json [3m]))`,
			`max by (foo) (rate({app="foo"} | json [3m]))`,
		},
		{
			`max(bytes_rate({app="foo"} | logfmt [3m]))`,
			`max(bytes_rate({app="foo"} | logfmt [3m]))`,
		},
		{
			`max by (foo) (bytes_rate({app="foo"} | json [3m]))`,
			`max by (foo) (bytes_rate({app="foo"} | json [3m]))`,
		},
		{
			`min(rate({app="foo"} | logfmt [3m]))`,
			`min(rate({app="foo"} | logfmt [3m]))`,
		},
		{
			`min by (foo) (rate({app="foo"} | json [3m]))`,
			`min by (foo) (rate({app="foo"} | json [3m]))`,
		},
		{
			`min(bytes_rate({app="foo"} | logfmt [3m]))`,
			`min(bytes_rate({app="foo"} | logfmt [3m]))`,
		},
		{
			`min by (foo) (bytes_rate({app="foo"} | json [3m]))`,
			`min by (foo) (bytes_rate({app="foo"} | json [3m]))`,
		},

		// if one side of a binary expression is a noop, the full query is a noop as well
		{
			`sum by (foo) (sum_over_time({app="foo"} | json | unwrap bar [3m])) / sum_over_time({app="foo"} | json | unwrap bar [6m])`,
			`(sum by (foo) (sum_over_time({app="foo"} | json | unwrap bar [3m])) / sum_over_time({app="foo"} | json | unwrap bar [6m]))`,
		},
		{
			`count_over_time({app="foo"}[3m]) or vector(0)`,
			`(count_over_time({app="foo"}[3m]) or vector(0.000000))`,
		},
		{
			`sum(last_over_time({app="foo"} | logfmt | unwrap total_count [1d]) by (foo)) or vector(0)`,
			`(sum(last_over_time({app="foo"} | logfmt | unwrap total_count [1d]) by (foo)) or vector(0.000000))`,
		},

		// should be noop if literal expression
		{
			`5`,
			`5`,
		},
		{
			`5 * 5`,
			`25`,
		},
		// should be noop if VectorExpr
		{
			`vector(0)`,
			`vector(0.000000)`,
		},
	} {
		tc := tc
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()

			mapperStats := NewMapperStats()
			rvm, err := NewRangeMapper(time.Minute, nilShardMetrics, mapperStats)
			require.NoError(t, err)

			noop, mappedExpr, err := rvm.Parse(syntax.MustParseExpr(tc.expr))
			require.NoError(t, err)

			require.Equal(t, removeWhiteSpace(tc.expected), removeWhiteSpace(mappedExpr.String()))
			require.Equal(t, 0, mapperStats.GetSplitQueries())
			require.True(t, noop)
		})
	}
}

func Test_FailQuery(t *testing.T) {
	rvm, err := NewRangeMapper(2*time.Minute, nilShardMetrics, NewMapperStats())
	require.NoError(t, err)
	_, _, err = rvm.Parse(syntax.MustParseExpr(`{app="foo"} |= "err"`))
	require.Error(t, err)
	// Empty parameter to json parser
	_, _, err = rvm.Parse(syntax.MustParseExpr(`topk(10,sum by(namespace)(count_over_time({application="nginx", site!="eu-west-1-dev"} |= "/artifactory/" != "api" != "binarystore" | json [1d])))`))
	require.NoError(t, err)
}

func Test_NoPanicOnClone(t *testing.T) {
	rvm, err := NewRangeMapper(2*time.Minute, nilShardMetrics, NewMapperStats())
	require.NoError(t, err)

	longQuery := `count_over_time({foo="bar"} | line_format ` + "`" + `{{"looooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooooong query"}}` + "`" + "[1h])"
	expr, err := syntax.ParseSampleExpr(longQuery)
	require.NoError(t, err)

	require.NotPanics(t, func() {
		_, _ = rvm.Map(expr, nil, rvm.metrics.downstreamRecorder())
	})
}
