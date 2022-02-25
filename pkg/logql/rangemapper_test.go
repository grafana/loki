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
			`sum(avg_over_time({app="foo"} | unwrap bar[3m]))`,
			`sum(avg_over_time({app="foo"} | unwrap bar[3m]))`,
			true,
		},
		{
			`rate({app="foo"}[3m])`,
			`sum(downstream<rate({app="foo"}[1m] offset 2m0s), shard=<nil>>
				++ downstream<rate({app="foo"}[1m] offset 1m0s), shard=<nil>>
				++ downstream<rate({app="foo"}[1m]), shard=<nil>>)`,
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
