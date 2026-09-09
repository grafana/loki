package syntax

import (
	"strings"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/log"
)

func TestClone(t *testing.T) {
	tests := map[string]struct {
		query string
	}{
		"simple matchers": {
			query: `{env="prod", app=~"loki.*"}`,
		},
		"simple aggregation": {
			query: `count_over_time({env="prod", app=~"loki.*"}[5m])`,
		},
		"simple aggregation with unwrap": {
			query: `sum_over_time({env="prod", app=~"loki.*"} | unwrap bytes[5m])`,
		},
		"bin op": {
			query: `(count_over_time({env="prod", app=~"loki.*"}[5m]) >= 0)`,
		},
		"label filter": {
			query: `{app="foo"} |= "bar" | json | ( latency>=250ms or ( status_code<500 , status_code>200 ) )`,
		},
		"line filter": {
			query: `{app="foo"} |= "bar" | json |= "500" or "200"`,
		},
		"drop label": {
			query: `{app="foo"} |= "bar" | json | drop latency, status_code="200"`,
		},
		"keep label": {
			query: `{app="foo"} |= "bar" | json | keep latency, status_code="200"`,
		},
		"regexp": {
			query: `{env="prod", app=~"loki.*"} |~ ".*foo.*"`,
		},
		"vector matching": {
			query: `(sum by (cluster)(rate({foo="bar"}[5m])) / ignoring (cluster)  count(rate({foo="bar"}[5m])))`,
		},
		"sum over or vector": {
			query: `(sum(count_over_time({foo="bar"}[5m])) or vector(1.000000))`,
		},
		"label replace": {
			query: `label_replace(vector(0.000000),"foo","bar","","")`,
		},
		"filters with bytes": {
			query: `{app="foo"} |= "bar" | json | ( status_code <500 or ( status_code>200 , size>=2.5KiB ) )`,
		},
		"post filter": {
			query: `quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
				| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo
				| __error__ !~".+"[5m]) by (namespace,instance)`,
		},
		"multiple post filters": {
			query: `rate({app="foo"} | json | unwrap foo | latency >= 250ms or bytes > 42B or ( status_code < 500 and status_code > 200) or source = ip("") and user = "me" [1m])`,
		},
		"true filter": {
			query: `{ foo = "bar" } | foo =~".*"`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {

			expr, err := ParseExpr(test.query)
			require.NoError(t, err)

			actual, err := Clone[Expr](expr)
			require.NoError(t, err)

			require.Equal(t, expr.Pretty(0), actual.Pretty(0))
		})
	}
}

func TestCloneStringLabelFilter(t *testing.T) {
	for name, tc := range map[string]struct {
		expr Expr
	}{
		"pipeline": {
			expr: newPipelineExpr(
				newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
				MultiStageExpr{
					newLogfmtParserExpr(nil),
					newLabelFilterExpr(&log.StringLabelFilter{Matcher: labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")}),
				},
			),
		},
		"filterer": {
			expr: &LabelFilterExpr{
				LabelFilterer: &log.LineFilterLabelFilter{
					Matcher: mustNewMatcher(labels.MatchEqual, "foo", "bar"),
					Filter:  log.ExistsFilter,
				},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			actual, err := Clone[Expr](tc.expr)
			require.NoError(t, err)

			require.Equal(t, tc.expr.Pretty(0), actual.Pretty(0))
			require.Equal(t, tc.expr, actual)
		})
	}
}

func TestCloneParseTestCases(t *testing.T) {
	for _, tc := range ParseTestCases {
		if tc.err == nil {
			t.Run(tc.in, func(t *testing.T) {
				ast, err := ParseExpr(tc.in)
				require.NoError(t, err)
				if strings.Contains(tc.in, "KiB") {
					t.Skipf("Byte roundtrip conversion is broken. '%s' vs '%s'", tc.in, ast.String())
				}

				actual, err := Clone[Expr](ast)
				require.NoError(t, err)

				require.Equal(t, ast.Pretty(0), actual.Pretty(0))
			})
		}
	}
}
