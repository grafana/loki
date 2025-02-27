package syntax

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func Test_Walkable(t *testing.T) {
	type table struct {
		desc string
		expr string
		want int
	}
	tests := []table{
		{
			desc: "vector aggregate query",
			expr: `sum by (cluster) (rate({job="foo"} |= "bar" | logfmt | bazz="buzz" [5m]))`,
			want: 8,
		},
		{
			desc: "bin op query",
			expr: `(sum by(cluster)(rate({job="foo"} |= "bar" | logfmt | bazz="buzz"[5m])) / sum by(cluster)(rate({job="foo"} |= "bar" | logfmt | bazz="buzz"[5m])))`,
			want: 17,
		},
		{
			desc: "single variant query",
			expr: `variants(count_over_time({job="foo"}[5m])) of ({job="foo"}[5m])`,
			want: 6,
		},
		{
			desc: "single range aggregation variant query",
			expr: `variants(sum by (job) (count_over_time({job="foo"}[5m]))) of ({job="foo"}[5m])`,
			want: 7,
		},
		{
			desc: "multiple variants query",
			expr: `variants(count_over_time({job="foo"}[5m]), bytes_over_time({job="foo"}[5m])) of ({job="foo"}[5m])`,
			want: 9,
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			expr, err := ParseExpr(test.expr)
			require.Nil(t, err)

			var cnt int
			expr.Walk(func(_ Expr) { cnt++ })
			require.Equal(t, test.want, cnt)
		})
	}
}

func Test_AppendMatchers(t *testing.T) {
	type table struct {
		desc     string
		expr     string
		want     string
		matchers []*labels.Matcher
	}
	tests := []table{
		{
			desc: "vector range query",
			expr: `sum by(cluster)(rate({job="foo"} |= "bar" | logfmt | bazz="buzz"[5m]))`,
			want: `sum by (cluster)(rate({job="foo", namespace="a"} |= "bar" | logfmt | bazz="buzz"[5m]))`,
			matchers: []*labels.Matcher{
				{
					Name:  "namespace",
					Type:  labels.MatchEqual,
					Value: "a",
				},
			},
		},
		{
			desc: "bin op query",
			expr: `sum by(cluster)(rate({job="foo"} |= "bar" | logfmt | bazz="buzz"[5m])) / sum by(cluster)(rate({job="foo"} |= "bar" | logfmt | bazz="buzz"[5m]))`,
			want: `(sum by (cluster)(rate({job="foo", namespace="a"} |= "bar" | logfmt | bazz="buzz"[5m])) / sum by (cluster)(rate({job="foo", namespace="a"} |= "bar" | logfmt | bazz="buzz"[5m])))`,
			matchers: []*labels.Matcher{
				{
					Name:  "namespace",
					Type:  labels.MatchEqual,
					Value: "a",
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			expr, err := ParseExpr(test.expr)
			require.NoError(t, err)

			expr.Walk(func(e Expr) {
				switch me := e.(type) {
				case *MatchersExpr:
					me.AppendMatchers(test.matchers)
				default:
					// do nothing
				}
			})
			require.Equal(t, test.want, expr.String())
		})
	}
}
