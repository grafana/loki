package syntax_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/logql/syntax"
)

func TestRoundtrip(t *testing.T) {
	q := `{x="a"} |= "b"`
	expr, err := syntax.ParseExpr(q)
	assert.NoError(t, err)

	assert.Equal(t, q, expr.String())
}

func TestLabelInserter(t *testing.T) {
	t.Parallel()
	labelToInsert := labels.MustNewMatcher(labels.MatchEqual, "app", "external-debug")

	testCases := []struct {
		in       string
		expected string
	}{
		{
			in:       `{x="a"} |= "b"`,
			expected: `{x="a", ` + labelToInsert.String() + `} |= "b"`,
		},
		{
			in:       `sum(rate({x="a"} |= "b"[5m]))`,
			expected: `sum(rate({x="a", ` + labelToInsert.String() + `} |= "b"[5m]))`,
		},
		{
			in:       `sum(rate({x="a"} |= "b"[5m])) / sum(rate({x="a"} |= "b"[5m]))`,
			expected: `sum(rate({x="a", ` + labelToInsert.String() + `} |= "b"[5m])) / sum(rate({x="a", ` + labelToInsert.String() + `} |= "b"[5m]))`,
		},
		{
			in:       `quantile_over_time(0.99,{app="api-mock-router"} | json | __error__="" | ( RouterName!="default@file" , RouterName!="ping@internal" ) | label_format status="{{ regexReplaceAllLiteral \"..$\" .OriginStatus \"xx\" }}" | unwrap Duration[5m]) by(RequestMethod,RequestPath,status)`,
			expected: `quantile_over_time(0.99,{app="api-mock-router", ` + labelToInsert.String() + `} | json | __error__="" | ( RouterName!="default@file" , RouterName!="ping@internal" ) | label_format status="{{ regexReplaceAllLiteral \"..$\" .OriginStatus \"xx\" }}" | unwrap Duration[5m]) by(RequestMethod,RequestPath,status)`,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.in, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tC.in)
			assert.NoError(t, err)
			walkAndFindMatcherExpr(expr, func(e *syntax.MatcherExpr) {
				e.Matchers = append(e.Matchers, labelToInsert)
			})

			assert.Equal(t, tC.expected, expr.String())
		})
	}
}

func walkAndFindMatcherExpr(expr syntax.Expr, cb func(*syntax.MatcherExpr)) {
	fmt.Printf("%T\n", expr)
	if e, ok := expr.(*syntax.MatcherExpr); ok {
		cb(e)
	}
	for _, sub := range expr.Sub() {
		walkAndFindMatcherExpr(sub, cb)
	}
}

func TestBuildQuery(t *testing.T) {
	t.Parallel()

	floatPtr := func(f float64) *float64 {
		return &f
	}

	testCases := []struct {
		expr     syntax.Expr
		expected string
	}{
		{
			expr: &syntax.RangeAggregationExpr{
				Left: &syntax.LogRange{
					Left: &syntax.MatcherExpr{
						Matchers: []*labels.Matcher{
							labels.MustNewMatcher(labels.MatchEqual, "a", "a"),
							labels.MustNewMatcher(labels.MatchEqual, "b", "b"),
						},
					},
					Interval: 5 * time.Minute,
					Unwrap: &syntax.UnwrapExpr{
						Identifier: "duration",
					},
				},
				Operation: syntax.OpRangeTypeQuantile,
				Params:    floatPtr(0.99),
			},
			expected: `quantile_over_time(0.99,{a="a", b="b"} | unwrap duration[5m])`,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.expected, func(t *testing.T) {
			assert.Equal(t, tC.expected, tC.expr.String())
		})
	}
}
