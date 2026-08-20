package syntax

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

func Test_Extractor(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{
		`rate( ( {job="mysql"} |="error" !="timeout" ) [10s] )`,
		`absent_over_time( ( {job="mysql"} |="error" !="timeout" ) [10s] )`,
		`absent_over_time( ( {job="mysql"} |="error" !="timeout" ) [10s] offset 30s )`,
		`sum without(a) ( rate ( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )`,
		`sum by(a) (rate( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )`,
		`sum(count_over_time({job="mysql"}[5m]))`,
		`sum(count_over_time({job="mysql"} | json [5m]))`,
		`sum(count_over_time({job="mysql"} | logfmt [5m]))`,
		`sum(count_over_time({job="mysql"} | pattern "<foo> bar <buzz>" [5m]))`,
		`sum(count_over_time({job="mysql"} | regexp "(?P<foo>foo|bar)" [5m]))`,
		`sum(count_over_time({job="mysql"} | regexp "(?P<foo>foo|bar)" [5m] offset 1h))`,
		`topk(10,sum(rate({region="us-east1"}[5m])) by (name))`,
		`topk by (name)(10,sum(rate({region="us-east1"}[5m])))`,
		`avg( rate( ( {job="nginx"} |= "GET" ) [10s] ) ) by (region)`,
		`avg(min_over_time({job="nginx"} |= "GET" | unwrap foo[10s])) by (region)`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m]))`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m])) / sum by (cluster) (count_over_time({job="postgres"}[5m])) `,
		`
			sum by (cluster) (count_over_time({job="postgres"}[5m])) /
			sum by (cluster) (count_over_time({job="postgres"}[5m])) /
			sum by (cluster) (count_over_time({job="postgres"}[5m]))
			`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m])) / min(count_over_time({job="mysql"}[5m])) `,
		`sum by (job) (
				count_over_time({namespace="tns"} |= "level=error"[5m])
			/
				count_over_time({namespace="tns"}[5m])
			)`,
		`stdvar_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m])`,
		`sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms|unwrap latency [5m])`,
		`sum by (job) (
				sum_over_time({namespace="tns"} |= "level=error" | json | foo=5 and bar<25ms | unwrap latency[5m])
			/
				count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
			)`,
		`sum by (job) (
				sum_over_time({namespace="tns"} |= "level=error" | json | foo=5 and bar<25ms | unwrap bytes(latency)[5m])
			/
				count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
			)`,
		`sum by (job) (
				sum_over_time(
					{namespace="tns"} |= "level=error" | json | avg=5 and bar<25ms | unwrap duration(latency) [5m]
				)
			/
				count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
			)`,
		`sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms | unwrap latency | __error__!~".*" | foo >5[5m])`,
		`absent_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms | unwrap latency | __error__!~".*" | foo >5[5m])`,
		`absent_over_time({namespace="tns"} |= "level=error" | json [5m])`,
		`sum by (job) (
				sum_over_time(
					{namespace="tns"} |= "level=error" | json | avg=5 and bar<25ms | unwrap duration(latency)  | __error__!~".*" [5m]
				)
			/
				count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
			)`,
		`label_replace(
				sum by (job) (
					sum_over_time(
						{namespace="tns"} |= "level=error" | json | avg=5 and bar<25ms | unwrap duration(latency)  | __error__!~".*" [5m]
					)
				/
					count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m])
				),
				"foo",
				"$1",
				"service",
				"(.*):.*"
			)
			`,
		`label_replace(
				sum by (job) (
					sum_over_time(
						{namespace="tns"} |= "level=error" | json | avg=5 and bar<25ms | unwrap duration(latency)  | __error__!~".*" [5m] offset 1h
					)
				/
					count_over_time({namespace="tns"} | logfmt | label_format foo=bar[5m] offset 1h)
				),
				"foo",
				"$1",
				"service",
				"(.*):.*"
			)
			`,
		`approx_count_distinct(mac, {job="mysql"}[10s]) by (version)`,
	} {
		t.Run(tc, func(t *testing.T) {
			expr, err := ParseSampleExpr(tc)
			require.Nil(t, err)
			extractor, err := expr.Extractor()
			require.Nil(t, err)
			require.NotNil(t, extractor)
		})
	}
}

// Test_Extractor_NilForExprsThatDoNotReadLogs pins the nil half of the Extractor
// contract. Callers skip reading chunks entirely when they get nil, so a change
// that returned a real extractor here would make these queries scan the store for
// samples they never derive from log lines.
func Test_Extractor_NilForExprsThatDoNotReadLogs(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{
		`vector(0)`,
		`1 + 1`,
	} {
		t.Run(tc, func(t *testing.T) {
			expr, err := ParseSampleExpr(tc)
			require.Nil(t, err)

			extractor, err := expr.Extractor()
			require.Nil(t, err)
			require.Nil(t, extractor)
		})
	}
}

// Test_Extractor_DoesNotMutateGroupingInPlace ensure the expression groups
// are not mutated in place. A VectorAggregationExpr's Grouping can be shared
// with another expression evaluated concurrently (e.g. the sum/count legs of
// a sharded avg_over_time), so extractor() must sort a private copy.
func Test_Extractor_DoesNotMutateGroupingInPlace(t *testing.T) {
	t.Parallel()

	expr, err := ParseSampleExpr(`sum by (c, a) (sum_over_time({job="mysql"} | unwrap bytes [5m]))`)
	require.NoError(t, err)

	vecAgg, ok := expr.(*VectorAggregationExpr)
	require.True(t, ok, "expected a VectorAggregationExpr, got %T", expr)
	require.Equal(t, []string{"c", "a"}, vecAgg.Grouping.Groups)

	_, err = expr.Extractor()
	require.NoError(t, err)

	require.Equal(t, []string{"c", "a"}, vecAgg.Grouping.Groups)
}

func TestLabelAggregationExtractorDoesNotMutateGrouping(t *testing.T) {
	expr, err := ParseExpr(`approx_count_distinct(mac, {foo="bar"}[1d]) by (version, region)`)
	require.NoError(t, err)
	agg, ok := expr.(*LabelAggregationExpr)
	require.True(t, ok)
	require.Equal(t, []string{"version", "region"}, agg.Grouping.Groups)

	_, err = agg.Extractor()
	require.NoError(t, err)
	require.Equal(t, []string{"version", "region"}, agg.Grouping.Groups)
	require.Contains(t, agg.String(), "by (version,region)")
}

func TestCountDistinctSketchExprValidatesMatchers(t *testing.T) {
	invalid := NewCountDistinctSketchExpr("mac", &LogRangeExpr{
		Left:     newMatcherExpr(nil),
		Interval: time.Hour,
	}, &Grouping{Groups: []string{"version"}})
	require.Equal(t, logqlmodel.NewParseError(errAtleastOneEqualityMatcherRequired, 0, 0), validateSampleExpr(invalid))

	valid := NewCountDistinctSketchExpr("mac", &LogRangeExpr{
		Left:     newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
		Interval: time.Hour,
	}, &Grouping{Groups: []string{"version"}})
	require.NoError(t, validateSampleExpr(valid))
}
