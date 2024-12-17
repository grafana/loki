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
			want: 16,
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
					// Do nothing
				}
			})
			require.Equal(t, test.want, expr.String())
		})
	}
}
