package logql

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_SplitRangeInterval(t *testing.T) {
	rvm, err := NewRangeVectorMapper(2 * time.Second)
	require.NoError(t, err)

	for _, tc := range []struct {
		expr     string
		expected string
	}{
		{
			`bytes_over_time({app="foo"}[3s])`,
			`sum without(
				downstream<bytes_over_time({app="foo"}[1s] offset 2s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[2s]), shard=<nil>>
			)`,
		},
		{
			`count_over_time({app="foo"}[5s])`,
			`sum without(
				downstream<count_over_time({app="foo"}[1s] offset 4s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[2s] offset 2s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[2s]), shard=<nil>>
			)`,
		},
	} {
		tc := tc
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			noop, mappedExpr, err := rvm.Parse(tc.expr)
			require.NoError(t, err)
			require.Equal(t, removeWhiteSpace(tc.expected), removeWhiteSpace(mappedExpr.String()))
			require.Equal(t, false, noop)
		})
	}
}

func Test_SplitRangeVectorMapping(t *testing.T) {
	rvm, err := NewRangeVectorMapper(time.Minute)
	require.NoError(t, err)

	for _, tc := range []struct {
		expr       string
		expected   string
		expectNoop bool
	}{
		// Range vector aggregators
		{
			`bytes_over_time({app="foo"}[3m])`,
			`sum without(
				downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
			)`,
			false,
		},
		{
			`count_over_time({app="foo"}[3m])`,
			`sum without(
				downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
			)`,
			false,
		},
		{
			`sum_over_time({app="foo"} | unwrap bar [3m])`,
			`sum without(
				downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
				++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
				++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
			)`,
			false,
		},
		{
			`max_over_time({app="foo"} | unwrap bar [3m])`,
			`max without(
				downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
			)`,
			false,
		},
		{
			`max_over_time({app="foo"} | json | unwrap bar [3m]) by (bar)`,
			`max by (bar) (
				downstream<max_over_time({app="foo"} | json | unwrap bar [1m] offset 2m0s) by (bar), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | json | unwrap bar [1m] offset 1m0s) by (bar), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | json | unwrap bar [1m]) by (bar), shard=<nil>>
			)`,
			false,
		},
		{
			`max_over_time({app="foo"} | unwrap bar [3m]) by (baz)`,
			`max by (baz) (
				downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
			)`,
			false,
		},
		{
			`min_over_time({app="foo"} | unwrap bar [3m])`,
			`min without(
				downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
				++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
				++ downstream<min_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
			)`,
			false,
		},
		{
			`min_over_time({app="foo"} | unwrap bar [3m]) by (baz)`,
			`min by (baz) (
				downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
				++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
				++ downstream<min_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
			)`,
			false,
		},

		// Vector aggregator - sum
		{
			`sum(bytes_over_time({app="foo"}[3m]))`,
			`sum(
				sum without (
					downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`sum(count_over_time({app="foo"}[3m]))`,
			`sum(
				sum without (
					downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`sum(sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`sum(
				sum without (
					downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`sum(max_over_time({app="foo"} | unwrap bar [3m]))`,
			`sum(
				max without (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`sum(max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`sum(
				max by (baz) (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`sum(min_over_time({app="foo"}  | unwrap bar [3m]))`,
			`sum(
				min without (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`sum(min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`sum(
				min by (baz) (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			false,
		},

		// Vector aggregator - sum by
		{
			`sum by (baz) (bytes_over_time({app="foo"}[3m]))`,
			`sum by (baz) (
				sum without (
					downstream<bytes_over_time({app="foo"} [1m] offset 2m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"} [1m] offset 1m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"} [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`sum by (baz) (count_over_time({app="foo"}[3m]))`,
			`sum by (baz) (
				sum without (
					downstream<count_over_time({app="foo"} [1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"} [1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"} [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`sum by (baz) (sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`sum by (baz) (
				sum without (
					downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`sum by (baz) (max_over_time({app="foo"} | unwrap bar [3m]))`,
			`sum by (baz) (
				max without (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`sum by (baz) (max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`sum by (baz) (
				max by (baz) (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`sum by (baz) (min_over_time({app="foo"} | unwrap bar [3m]))`,
			`sum by (baz) (
				min without (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`sum by (baz) (min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`sum by (baz) (
				min by (baz) (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			false,
		},

		// Vector aggregator - count
		{
			`count(bytes_over_time({app="foo"}[3m]))`,
			`count(
				sum without (
					downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`count(count_over_time({app="foo"}[3m]))`,
			`count(
				sum without (
					downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`count(sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`count(
				sum without (
					downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`count(max_over_time({app="foo"} | unwrap bar [3m]))`,
			`count(
				max without (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
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
			false,
		},
		{
			`count(min_over_time({app="foo"} | unwrap bar [3m]))`,
			`count(
				min without (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
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
			false,
		},

		// Vector aggregator - count by
		{
			`count by (baz) (bytes_over_time({app="foo"}[3m]))`,
			`count by (baz) (
				sum without (
					downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`count by (baz) (count_over_time({app="foo"}[3m]))`,
			`count by (baz) (
				sum without (
					downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`count by (baz) (sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`count by (baz) (
				sum without (
					downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`count by (baz) (max_over_time({app="foo"} | unwrap bar [3m]))`,
			`count by (baz) (
				max without (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
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
			false,
		},
		{
			`count by (baz) (min_over_time({app="foo"} | unwrap bar [3m]))`,
			`count by (baz) (
				min without (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
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
			false,
		},

		// Vector aggregator - max
		{
			`max(bytes_over_time({app="foo"}[3m]))`,
			`max(
				sum without (
					downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`max(count_over_time({app="foo"}[3m]))`,
			`max(
				sum without (
					downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`max(sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`max(
				sum without (
					downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`max(max_over_time({app="foo"} | unwrap bar [3m]))`,
			`max(
				max without (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`max(max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`max(
				max by (baz) (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`max(min_over_time({app="foo"} | unwrap bar [3m]))`,
			`max(
				min without (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`max(min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`max(
				min by (baz) (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			false,
		},

		// Vector aggregator - max by
		{
			`max by (baz) (bytes_over_time({app="foo"}[3m]))`,
			`max by (baz) (
				sum without (
					downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`max by (baz) (count_over_time({app="foo"}[3m]))`,
			`max by (baz) (
				sum without (
					downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`max by (baz) (sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`max by (baz) (
				sum without (
					downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`max by (baz) (max_over_time({app="foo"} | unwrap bar [3m]))`,
			`max by (baz) (
				max without (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`max by (baz) (max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`max by (baz) (
				max by (baz) (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`max by (baz) (min_over_time({app="foo"} | unwrap bar [3m]))`,
			`max by (baz) (
				min without (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`max by (baz) (min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`max by (baz) (
				min by (baz) (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			false,
		},

		// Vector aggregator - min
		{
			`min(bytes_over_time({app="foo"}[3m]))`,
			`min(
				sum without (
					downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`min(count_over_time({app="foo"}[3m]))`,
			`min(
				sum without (
					downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`min(sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`min(
				sum without (
					downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`min(max_over_time({app="foo"} | unwrap bar [3m]))`,
			`min(
				max without (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`min(max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`min(
				max by (baz) (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`min(min_over_time({app="foo"} | unwrap bar [3m]))`,
			`min(
				min without (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`min(min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`min(
				min by (baz) (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			false,
		},

		// Vector aggregator - min by
		{
			`min by (baz) (bytes_over_time({app="foo"}[3m]))`,
			`min by (baz) (
				sum without (
					downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`min by (baz) (count_over_time({app="foo"}[3m]))`,
			`min by (baz) (
				sum without (
					downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`min by (baz) (sum_over_time({app="foo"} | unwrap bar [3m]))`,
			`min by (baz) (
				sum without (
					downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`min by (baz) (max_over_time({app="foo"} | unwrap bar [3m]))`,
			`min by (baz) (
				max without (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`min by (baz) (max_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`min by (baz) (
				max by (baz) (
					downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<max_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`min by (baz) (min_over_time({app="foo"} | unwrap bar [3m]))`,
			`min by (baz) (
				min without (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			`min by (baz) (min_over_time({app="foo"} | unwrap bar [3m]) by (baz))`,
			`min by (baz) (
				min by (baz) (
					downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s) by (baz), shard=<nil>>
					++ downstream<min_over_time({app="foo"} | unwrap bar [1m]) by (baz), shard=<nil>>
				)
			)`,
			false,
		},

		// Binary operations
		{
			`bytes_over_time({app="foo"}[3m]) + count_over_time({app="foo"}[5m])`,
			`(sum without (
				downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
			) +
			sum without (
				downstream<count_over_time({app="foo"}[1m] offset 4m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m] offset 3m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
			))
			`,
			false,
		},
		{
			`sum(count_over_time({app="foo"}[3m]) * count(sum_over_time({app="foo"} | unwrap bar [5m])))`,
			`sum(
				(sum without(
					downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				) *
				count (
					sum without(
						downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 4m0s), shard=<nil>>
						++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 3m0s), shard=<nil>>
						++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
						++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
						++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>
					)
				))
			)
			`,
			false,
		},

		// Multi vector aggregator layer queries
		{
			`sum(max(bytes_over_time({app="foo"}[3m])))`,
			`sum(
				max(
					sum without(
						downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
						++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
						++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>
					)
				)
			)
			`,
			false,
		},

		// Non-splittable vector aggregators - should go deeper in the AST
		{
			`topk(2, count_over_time({app="foo"}[3m]))`,
			`topk(2, 
				sum without(
					downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},

		// Non-splittable range vector aggregators
		{
			`quantile_over_time(0.95, {app="foo"} | unwrap bar[3m])`,
			`quantile_over_time(0.95, {app="foo"} | unwrap bar[3m])`,
			true,
		},
		{
			`sum(avg_over_time({app="foo"} | unwrap bar[3m]))`,
			`sum(avg_over_time({app="foo"} | unwrap bar[3m]))`,
			true,
		},

		// should be noop if range interval is slower or equal to split interval (1m)
		{
			`bytes_over_time({app="foo"}[1m])`,
			`bytes_over_time({app="foo"}[1m])`,
			true,
		},

		// should be noop if inner range aggregation includes a stage for label extraction such as `| json` or `| logfmt`
		// because otherwise the downstream queries would result in too many series
		{
			`sum(min_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`sum(min_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			true,
		},
		{
			`sum(max_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`sum(max_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			true,
		},
		{
			`sum(sum_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`sum(sum_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			true,
		},
		{
			`min(min_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`min(min_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			true,
		},
		{
			`min(max_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`min(max_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			true,
		},
		{
			`min(sum_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`min(sum_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			true,
		},
		{
			`max(min_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`max(min_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			true,
		},
		{
			`max(max_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`max(max_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			true,
		},
		{
			`max(sum_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			`max(sum_over_time({app="foo"} | logfmt | unwrap bar [3m]))`,
			true,
		},
		{
			`max_over_time({app="foo"} | json | unwrap bar [3m])`,
			`max_over_time({app="foo"} | json | unwrap bar [3m])`,
			true,
		},
	} {
		tc := tc
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			noop, mappedExpr, err := rvm.Parse(tc.expr)
			require.NoError(t, err)
			require.Equal(t, removeWhiteSpace(tc.expected), removeWhiteSpace(mappedExpr.String()))
			require.Equal(t, tc.expectNoop, noop)
		})
	}
}

func Test_FailQuery(t *testing.T) {
	rvm, err := NewRangeVectorMapper(2 * time.Minute)
	require.NoError(t, err)
	_, _, err = rvm.Parse(`{app="foo"} |= "err"`)
	require.Error(t, err)
}
