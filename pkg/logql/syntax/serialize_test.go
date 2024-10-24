package syntax

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestJSONSerializationRoundTrip(t *testing.T) {
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
		"regexp": {
			query: `{env="prod", app=~"loki.*"} |~ ".*foo.*"`,
		},
		"line filter": {
			query: `{env="prod", app=~"loki.*"} |= "foo" |= "bar" or "baz" | line_format "blip{{ .foo }}blop" |= "blip"`,
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
		"multiple post filters where one is a noop": {
			query: `rate({app="foo"} | json | unwrap foo | latency >= 250ms or bytes=~".*" [1m])`,
		},
		"empty label filter string": {
			query: `rate({app="foo"} |= "bar" | json | unwrap latency | path!="" [5m])`,
		},
		"single variant": {
			query: `variants(count_over_time({app="loki"} [1m])) of ({app="loki"}[1m])`,
		},
		"multiple variants": {
			query: `variants(count_over_time({app="loki"} [1m]), bytes_over_time({app="loki"} [1m])) of ({app="loki"}[1m])`,
		},
		"single variant without logs": {
			query: `variants(count_over_time({app="loki"} [1m])) of ({app="loki"}[1m]) without logs`,
		},
		"multiple variants without logs": {
			query: `variants(count_over_time({app="loki"} [1m]), bytes_over_time({app="loki"} [1m])) of ({app="loki"}[1m]) without logs`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {

			expr, err := ParseExpr(test.query)
			require.NoError(t, err)

			var buf bytes.Buffer
			err = EncodeJSON(expr, &buf)
			require.NoError(t, err)

			t.Log(buf.String())

			actual, err := DecodeJSON(buf.String())
			require.NoError(t, err)

			require.Equal(t, expr.Pretty(0), actual.Pretty(0))
		})
	}
}

func TestJSONSerializationParseTestCases(t *testing.T) {
	for _, tc := range ParseTestCases {
		if tc.err == nil {
			t.Run(tc.in, func(t *testing.T) {
				ast, err := ParseExpr(tc.in)
				require.NoError(t, err)
				if strings.Contains(tc.in, "KiB") {
					t.Skipf("Byte roundtrip conversion is broken. '%s' vs '%s'", tc.in, ast.String())
				}

				var buf bytes.Buffer
				err = EncodeJSON(ast, &buf)
				require.NoError(t, err)
				actual, err := DecodeJSON(buf.String())
				require.NoError(t, err)

				t.Log(buf.String())

				AssertExpressions(t, tc.exp, actual)
			})
		}
	}
}

func TestJSONSerializationRoundTrip_Variants(t *testing.T) {
	tests := map[string]struct {
		query    string
		expected MultiVariantExpr
	}{
		"single variant": {
			query: `variants(count_over_time({app="loki"} [5m])) of ({app="loki"} [5m])`,
			expected: MultiVariantExpr{
				logRange: newLogRange(
					newMatcherExpr(
						[]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "app", "loki")},
					), 5*time.Minute, nil, nil),
				variants: []SampleExpr{
					newRangeAggregationExpr(&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "app", "loki"),
							},
						},
						Interval: 5 * time.Minute,
					}, "count_over_time", nil, nil),
				},
				includeLogs: true,
			},
		},
		"single variant without logs": {
			query: `variants(count_over_time({app="loki"} [5m])) of ({app="loki"} [5m]) without logs`,
			expected: MultiVariantExpr{
				logRange: newLogRange(
					newMatcherExpr(
						[]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "app", "loki")},
					), 5*time.Minute, nil, nil),
				variants: []SampleExpr{
					newRangeAggregationExpr(&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "app", "loki"),
							},
						},
						Interval: 5 * time.Minute,
					}, "count_over_time", nil, nil),
				},
				includeLogs: false,
			},
		},
		"multiple variants": {
			query: `variants(count_over_time({app="loki"} [5m]), bytes_over_time({app="loki"} [5m])) of ({app="loki"}[5m])`,
			expected: MultiVariantExpr{
				logRange: newLogRange(
					newMatcherExpr(
						[]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "app", "loki")},
					), 5*time.Minute, nil, nil),
				variants: []SampleExpr{
					newRangeAggregationExpr(&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "app", "loki"),
							},
						},
						Interval: 5 * time.Minute,
					}, "count_over_time", nil, nil),
					newRangeAggregationExpr(&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "app", "loki"),
							},
						},
						Interval: 5 * time.Minute,
					}, "bytes_over_time", nil, nil),
				},
				includeLogs: true,
			},
		},
		"multiple variants without logs": {
			query: `variants(count_over_time({app="loki"} [5m]), bytes_over_time({app="loki"} [5m])) of ({app="loki"}[5m]) without logs`,
			expected: MultiVariantExpr{
				logRange: newLogRange(
					newMatcherExpr(
						[]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "app", "loki")},
					), 5*time.Minute, nil, nil),
				variants: []SampleExpr{
					newRangeAggregationExpr(&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "app", "loki"),
							},
						},
						Interval: 5 * time.Minute,
					}, "count_over_time", nil, nil),
					newRangeAggregationExpr(&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "app", "loki"),
							},
						},
						Interval: 5 * time.Minute,
					}, "bytes_over_time", nil, nil),
				},
				includeLogs: false,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			expr, err := ParseExpr(test.query)
			require.NoError(t, err)

			var buf bytes.Buffer
			err = EncodeJSON(expr, &buf)
			require.NoError(t, err)

			actual, err := DecodeJSON(buf.String())
			require.NoError(t, err)

			require.Equal(t, expr.Pretty(0), actual.Pretty(0))

			require.ElementsMatch(
				t,
				test.expected.Variants(),
				actual.(*MultiVariantExpr).Variants(),
			)
			require.Equal(t, test.expected.LogRange(), actual.(*MultiVariantExpr).LogRange())
		})
	}
}
