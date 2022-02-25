package logql

import (
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/stretchr/testify/require"
)

func Test_SplitRangeVectorMapping(t *testing.T) {
	rvm, err := NewRangeVectorMapper(time.Minute)
	require.NoError(t, err)

	for _, tc := range []struct {
		expr       string
		expected   string
		expectNoop bool
	}{
		{
			`sum(rate({app="foo"}[3m]))`,
			`sum(downstream<sum(rate({app="foo"}[1m] offset 2m0s)), shard=<nil>>
				++ downstream<sum(rate({app="foo"}[1m] offset 1m0s)), shard=<nil>>
				++ downstream<sum(rate({app="foo"}[1m])), shard=<nil>>)`,
			false,
		},
		{
			`sum(rate({app="foo"}[3m])) by (bar)`,
			`sum by (bar) (downstream<sum  by (bar) (rate({app="foo"}[1m] offset 2m0s)), shard=<nil>>
				++ downstream<sum by (bar) (rate({app="foo"}[1m] offset 1m0s)), shard=<nil>>
				++ downstream<sum by (bar)(rate({app="foo"}[1m])), shard=<nil>>)`,
			false,
		},
		{
			`count(rate({app="foo"}[3m]))`,
			`sum (downstream<count(rate({app="foo"}[1m] offset 2m0s)), shard=<nil>>
				++ downstream<count(rate({app="foo"}[1m] offset 1m0s)), shard=<nil>>
				++ downstream<count(rate({app="foo"}[1m])), shard=<nil>>)`,
			false,
		},
		{
			`count(rate({app="foo"}[3m])) by (bar)`,
			`sum by (bar) (downstream<count  by (bar) (rate({app="foo"}[1m] offset 2m0s)), shard=<nil>>
				++ downstream<count by (bar) (rate({app="foo"}[1m] offset 1m0s)), shard=<nil>>
				++ downstream<count by (bar)(rate({app="foo"}[1m])), shard=<nil>>)`,
			false,
		},
		{
			`max(rate({app="foo"}[3m]))`,
			`max (downstream<max(rate({app="foo"}[1m] offset 2m0s)), shard=<nil>>
				++ downstream<max (rate({app="foo"}[1m] offset 1m0s)), shard=<nil>>
				++ downstream<max (rate({app="foo"}[1m])), shard=<nil>>)`,
			false,
		},
		{
			`max(rate({app="foo"}[3m])) by (bar)`,
			`max by (bar) (downstream<max  by (bar) (rate({app="foo"}[1m] offset 2m0s)), shard=<nil>>
				++ downstream<max by (bar) (rate({app="foo"}[1m] offset 1m0s)), shard=<nil>>
				++ downstream<max by (bar)(rate({app="foo"}[1m])), shard=<nil>>)`,
			false,
		},
		{
			`min(rate({app="foo"}[3m]))`,
			`min (downstream<min(rate({app="foo"}[1m] offset 2m0s)), shard=<nil>>
				++ downstream<min (rate({app="foo"}[1m] offset 1m0s)), shard=<nil>>
				++ downstream<min (rate({app="foo"}[1m])), shard=<nil>>)`,
			false,
		},
		{
			`min(rate({app="foo"}[3m])) by (bar)`,
			`min by (bar) (downstream<min  by (bar) (rate({app="foo"}[1m] offset 2m0s)), shard=<nil>>
				++ downstream<min by (bar) (rate({app="foo"}[1m] offset 1m0s)), shard=<nil>>
				++ downstream<min by (bar)(rate({app="foo"}[1m])), shard=<nil>>)`,
			false,
		},
		{
			`bytes_over_time({app="foo"}[3m])`,
			`sum (downstream<bytes_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<bytes_over_time({app="foo"}[1m]), shard=<nil>>)`,
			false,
		},
		{
			`bytes_rate({app="foo"}[3m])`,
			`sum (downstream<bytes_rate({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<bytes_rate({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<bytes_rate({app="foo"}[1m]), shard=<nil>>)`,
			false,
		},
		{
			`count_over_time({app="foo"}[3m])`,
			`sum (downstream<count_over_time({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<count_over_time({app="foo"}[1m]), shard=<nil>>)`,
			false,
		},
		{
			`rate({app="foo"}[3m])`,
			`sum(downstream<rate({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<rate({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<rate({app="foo"}[1m]), shard=<nil>>)`,
			false,
		},
		{
			`sum_over_time({app="foo"} | unwrap bar [3m])`,
			`sum (downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
				++ downstream<sum_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
				++ downstream<sum_over_time({app="foo"} | unwrap bar [1m]), shard=<nil>>)`,
			false,
		},
		{
			`min_over_time({app="foo"} | unwrap bar [3m])`,
			`min(downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
				++ downstream<min_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
				++ downstream<min_over_time({app="foo"} | unwrap bar[1m]), shard=<nil>>)`,
			false,
		},
		{
			`max_over_time({app="foo"} | unwrap bar [3m])`,
			`max(downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 2m0s), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | unwrap bar [1m] offset 1m0s), shard=<nil>>
				++ downstream<max_over_time({app="foo"} | unwrap bar[1m]), shard=<nil>>)`,
			false,
		},
		{
			// TODO: Add more binary operations
			`rate({app="foo"}[3m]) / rate({app="foo"}[2m])`,
			`(
				sum(
					downstream<rate({app="foo"}[1m] offset 2m0s), shard=<nil>>
					++ downstream<rate({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<rate({app="foo"}[1m]), shard=<nil>>
				)
				/
				sum(
					downstream<rate({app="foo"}[1m] offset 1m0s), shard=<nil>>
					++ downstream<rate({app="foo"}[1m]), shard=<nil>>
				)
			)`,
			false,
		},
		{
			// should go deeper in the AST in case the top function is non-splittable
			`topk(2, rate({app="foo"}[3m]))`,
			`topk(2, 
				sum(downstream<rate({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<rate({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<rate({app="foo"}[1m]), shard=<nil>>)
			)`,
			false,
		},
		{
			`quantile_over_time(0.95, {app="foo"} | unwrap bar[3m])`,
			`quantile_over_time(0.95, {app="foo"} | unwrap bar[3m])`,
			true,
		},
		{
			// should be noop if function is non-splittable (avg_over_time)
			`sum(avg_over_time({app="foo"} | unwrap bar[3m]))`,
			`sum(avg_over_time({app="foo"} | unwrap bar[3m]))`,
			true,
		},
		{
			// should be noop if range interval is slower or equal to split interval (1m)
			`rate({app="foo"}[1m])`,
			`rate({app="foo"}[1m])`,
			true,
		},
	} {
		tc := tc
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			mapped, mappedExpr, err := rvm.Parse(tc.expr)
			require.NoError(t, err)
			require.Equal(t, removeWhiteSpace(tc.expected), removeWhiteSpace(mappedExpr.String()))
			require.Equal(t, tc.expectNoop, mapped)
		})
	}
}

func Test_FailQuery(t *testing.T) {
	rvm, err := NewRangeVectorMapper(time.Minute)
	require.NoError(t, err)
	_, _, err = rvm.Parse(`{app="foo"} |= "err"`)
	require.Error(t, err)
}

func removeWhiteSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
