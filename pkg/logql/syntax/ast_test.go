package syntax

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/log"
)

var labelBar, _ = ParseLabels("{app=\"bar\"}")

func Test_logSelectorExpr_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		selector     string
		expectFilter bool
	}{
		{`{foo="bar"}`, false},
		{`{foo="bar", bar!="baz"}`, false},
		{`{foo="bar", bar!="baz"} != "bip" !~ ".+bop"`, true},
		{`{foo="bar"} |= "baz" |~ "blip" |> "qux" !> "waldo" != "flip" !~ "flap"`, true},
		{`{foo="bar", bar!="baz"} |= ""`, false},
		{`{foo="bar", bar!="baz"} |= "" |= ip("::1")`, true},
		{`{foo="bar", bar!="baz"} |= "" != ip("127.0.0.1")`, true},
		{`{foo="bar", bar!="baz"} |~ ""`, false},
		{`{foo="bar", bar!="baz"} |~ ".*"`, false},
		{`{foo="bar", bar!="baz"} |= "" |= ""`, false},
		{`{foo="bar", bar!="baz"} |~ "" |= "" |~ ".*"`, false},
		{`{foo="bar", bar!="baz"} |> ""`, true},
		{`{foo="bar", bar!="baz"} |> "<_>"`, true},
		{`{foo="bar", bar!="baz"} |> "<_>" !> "<_> <_>"`, true},
		{`{foo="bar", bar!="baz"} != "bip" !~ ".+bop" |> "<_> bop <_>" | json`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | logfmt`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | logfmt --strict`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | logfmt --strict --keep-empty`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | unpack | foo>5`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | pattern "<foo> bar <buzz>" | foo>5`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | logfmt | b>=10GB`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | logfmt | b=ip("127.0.0.1")`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | logfmt | b=ip("127.0.0.1") | level="error"`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | logfmt --strict b="foo" | b=ip("127.0.0.1") | level="error"`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | logfmt --strict --keep-empty b="foo" | b=ip("127.0.0.1") | level="error"`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | logfmt | b=ip("127.0.0.1") | level="error" | c=ip("::1")`, true}, // chain inside label filters.
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | regexp "(?P<foo>foo|bar)"`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | regexp "(?P<foo>foo|bar)" | ( ( foo<5.01 , bar>20ms ) or foo="bar" ) | line_format "blip{{.boop}}bap" | label_format foo=bar,bar="blip{{.blop}}"`, true},
		{`{foo="bar"} | logfmt | counter>-1 | counter>=-1 | counter<-1 | counter<=-1 | counter!=-1 | counter==-1`, true},
	}

	for _, tt := range tests {
		t.Run(tt.selector, func(t *testing.T) {
			t.Parallel()
			expr, err := ParseLogSelector(tt.selector, true)
			if err != nil {
				t.Fatalf("failed to parse log selector: %s", err)
			}
			p, err := expr.Pipeline()
			if err != nil {
				t.Fatalf("failed to get filter: %s", err)
			}
			if !tt.expectFilter {
				require.Equal(t, log.NewNoopPipeline(), p)
			}
			if expr.String() != tt.selector {
				t.Fatalf("error expected: %s got: %s", tt.selector, expr.String())
			}
		})
	}
}

func Test_SampleExpr_String(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{
		`rate( ( {job="mysql"} |="error" !="timeout" ) [10s] )>-1`,
		`rate( ( {job="mysql"} |="error" !="timeout" ) [10s] )`,
		`absent_over_time( ( {job="mysql"} |="error" !="timeout" ) [10s] )`,
		`absent_over_time( ( {job="mysql"} |="error" !="timeout" ) [10s] offset 10d )`,
		`vector(123)`,
		`sort(sum by(a) (rate( ( {job="mysql"} |="error" !="timeout" ) [10s] ) ))`,
		`sort_desc(sum by(a) (rate( ( {job="mysql"} |="error" !="timeout" ) [10s] ) ))`,
		`sum without(a) ( rate ( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )`,
		`sum by(a) (rate( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )`,
		`sum(count_over_time({job="mysql"}[5m]))`,
		`sum(count_over_time({job="mysql"}[5m] offset 10m))`,
		`sum(count_over_time({job="mysql"} | json [5m]))`,
		`sum(count_over_time({job="mysql"} | json [5m] offset 10m))`,
		`sum(count_over_time({job="mysql"} | logfmt [5m]))`,
		`sum(count_over_time({job="mysql"} | logfmt --strict [5m] offset 10m))`,
		`sum(count_over_time({job="mysql"} | pattern "<foo> bar <buzz>" | json [5m]))`,
		`sum(count_over_time({job="mysql"} | unpack | json [5m]))`,
		`sum(count_over_time({job="mysql"} | regexp "(?P<foo>foo|bar)" [5m]))`,
		`sum(count_over_time({job="mysql"} | regexp "(?P<foo>foo|bar)" [5m] offset 10y))`,
		`topk(10,sum(rate({region="us-east1"}[5m])) by (name))`,
		`topk by (name)(10,sum(rate({region="us-east1"}[5m])))`,
		`avg( rate( ( {job="nginx"} |= "GET" ) [10s] ) ) by (region)`,
		`avg(min_over_time({job="nginx"} |= "GET" | unwrap foo[10s])) by (region)`,
		`avg(min_over_time({job="nginx"} |= "GET" | unwrap foo[10s] offset 10m)) by (region)`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m]))`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m] offset 10m))`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m])) / sum by (cluster) (count_over_time({job="postgres"}[5m])) `,
		`sum by (cluster) (count_over_time({job="mysql"}[5m] offset 10m)) / sum by (cluster) (count_over_time({job="postgres"}[5m] offset 10m)) `,
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
		`stdvar_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
		| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m] offset 10m)`,
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
		`last_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms | unwrap latency | __error__!~".*" | foo >5[5m])`,
		`first_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms | unwrap latency | __error__!~".*" | foo >5[5m])`,
		`absent_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms | unwrap latency | __error__!~".*" | foo >5[5m])`,
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
		`10 / (5/2)`,
		`(count_over_time({job="postgres"}[5m])/2) or vector(2)`,
		`10 / (count_over_time({job="postgres"}[5m])/2)`,
		`{app="foo"} | json response_status="response.status.code", first_param="request.params[0]"`,
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
		`(((
			sum by(typename,pool,commandname,colo)(sum_over_time({_namespace_="appspace", _schema_="appspace-1min", pool=~"r1testlvs", colo=~"slc|lvs|rno", env!~"(pre-production|sandbox)"} | logfmt | status!="0" | ( ( type=~"(?i)^(Error|Exception|Fatal|ERRPAGE|ValidationError)$" or typename=~"(?i)^(Error|Exception|Fatal|ERRPAGE|ValidationError)$" ) or status=~"(?i)^(Error|Exception|Fatal|ERRPAGE|ValidationError)$" ) | commandname=~"(?i).*|UNSET" | unwrap sumcount[5m])) / 60)
				or on ()
				((sum by(typename,pool,commandname,colo)(sum_over_time({_namespace_="appspace", _schema_="appspace-15min", pool=~"r1testlvs", colo=~"slc|lvs|rno", env!~"(pre-production|sandbox)"} | logfmt | status!="0" | ( ( type=~"(?i)^(Error|Exception|Fatal|ERRPAGE|ValidationError)$" or typename=~"(?i)^(Error|Exception|Fatal|ERRPAGE|ValidationError)$" ) or status=~"(?i)^(Error|Exception|Fatal|ERRPAGE|ValidationError)$" ) | commandname=~"(?i).*|UNSET" | unwrap sumcount[5m])) / 15) / 60))
				or on ()
				((sum by(typename,pool,commandname,colo) (sum_over_time({_namespace_="appspace", _schema_="appspace-1h", pool=~"r1testlvs", colo=~"slc|lvs|rno", env!~"(pre-production|sandbox)"} | logfmt | status!="0" | ( ( type=~"(?i)^(Error|Exception|Fatal|ERRPAGE|ValidationError)$" or typename=~"(?i)^(Error|Exception|Fatal|ERRPAGE|ValidationError)$" ) or status=~"(?i)^(Error|Exception|Fatal|ERRPAGE|ValidationError)$" ) | commandname=~"(?i).*|UNSET" | unwrap sumcount[5m])) / 60) / 60))`,
		`{app="foo"} | logfmt code="response.code", IPAddress="host"`,
	} {
		t.Run(tc, func(t *testing.T) {
			expr, err := ParseExpr(tc)
			require.Nil(t, err)

			expr2, err := ParseExpr(expr.String())
			require.Nil(t, err)

			AssertExpressions(t, expr, expr2)
		})
	}
}

func Test_SampleExpr_String_Fail(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{
		`topk(0, sum(rate({region="us-east1"}[5m])) by (name))`,
		`topk by (name)(0,sum(rate({region="us-east1"}[5m])))`,
		`bottomk(0, sum(rate({region="us-east1"}[5m])) by (name))`,
		`bottomk by (name)(0,sum(rate({region="us-east1"}[5m])))`,
	} {
		t.Run(tc, func(t *testing.T) {
			_, err := ParseExpr(tc)
			require.ErrorContains(t, err, "parse error : invalid parameter (must be greater than 0)")
		})
	}
}

func Test_SampleExpr_Sort_Fail(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{
		`sort(sum by(a) (rate( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )) by (app)`,
		`sort_desc(sum by(a) (rate( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )) by (app)`,
	} {
		t.Run(tc, func(t *testing.T) {
			_, err := ParseExpr(tc)
			require.ErrorContains(t, err, "sort and sort_desc doesn't allow grouping by")
		})
	}
}

func TestMatcherGroups(t *testing.T) {
	for i, tc := range []struct {
		query string
		exp   []MatcherRange
	}{
		{
			query: `{job="foo"}`,
			exp: []MatcherRange{
				{
					Matchers: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchEqual, "job", "foo"),
					},
				},
			},
		},
		{
			query: `count_over_time({job="foo"}[5m]) / count_over_time({job="bar"}[5m] offset 10m)`,
			exp: []MatcherRange{
				{
					Interval: 5 * time.Minute,
					Matchers: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchEqual, "job", "foo"),
					},
				},
				{
					Interval: 5 * time.Minute,
					Offset:   10 * time.Minute,
					Matchers: []*labels.Matcher{
						labels.MustNewMatcher(labels.MatchEqual, "job", "bar"),
					},
				},
			},
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			expr, err := ParseExpr(tc.query)
			require.Nil(t, err)
			out, err := MatcherGroups(expr)
			require.Nil(t, err)
			require.Equal(t, tc.exp, out)
		})
	}
}

func Test_NilFilterDoesntPanic(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{
		`{namespace="dev", container_name="cart"} |= "" |= "bloop"`,
		`{namespace="dev", container_name="cart"} |= "bleep" |= ""`,
		`{namespace="dev", container_name="cart"} |= "bleep" |= "" |= "bloop"`,
		`{namespace="dev", container_name="cart"} |= "bleep" |= "" |= "bloop"`,
		`{namespace="dev", container_name="cart"} |= "bleep" |= "bloop" |= ""`,
		`{namespace="dev", container_name="cart"} !> ""`,
	} {
		t.Run(tc, func(t *testing.T) {
			expr, err := ParseLogSelector(tc, true)
			require.Nil(t, err)

			p, err := expr.Pipeline()
			require.Nil(t, err)
			_, _, matches := p.ForStream(labelBar).Process(0, []byte("bleepbloop"))

			require.True(t, matches)
		})
	}
}

type linecheck struct {
	l string
	e bool
}

func Test_FilterMatcher(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		q string

		expectedMatchers []*labels.Matcher
		// test line against the resulting filter, if empty filter should also be nil
		lines []linecheck
	}{
		{
			`{app="foo",cluster=~".+bar"}`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
				mustNewMatcher(labels.MatchRegexp, "cluster", ".+bar"),
			},
			nil,
		},
		{
			`{app!="foo",cluster=~".+bar",bar!~".?boo"}`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchNotEqual, "app", "foo"),
				mustNewMatcher(labels.MatchRegexp, "cluster", ".+bar"),
				mustNewMatcher(labels.MatchNotRegexp, "bar", ".?boo"),
			},
			nil,
		},
		{
			`{app="foo"} |= "foo"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foobar", true}, {"bar", false}},
		},
		{
			`{app="foo"} |= "foo" != "bar"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foobuzz", true}, {"bar", false}},
		},
		{
			`{app="foo"} |= "foo" !~ "f.*b"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo", true}, {"bar", false}, {"foobar", false}},
		},
		{
			`{app="foo"} |= "foo" |~ "f.*b"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo", false}, {"bar", false}, {"foobar", true}},
		},
		{
			`{app="foo"} |~ "foo"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo", true}, {"bar", false}, {"foobar", true}},
		},
		{
			`{app="foo"} |> "foo <_>"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo bar", true}, {"foo", false}},
		},
		{
			`{app="foo"} !> "foo <_>"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo bar", false}, {"foo", true}},
		},
		{
			`{app="foo"} |~ "foo\\.bar\\.baz"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo", false}, {"bar", false}, {"foo.bar.baz", true}},
		},
		{
			"{app=\"foo\"} | logfmt | field =~ `foo\\.bar`",
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"field=foo", false}, {"field=bar", false}, {"field=foo.bar", true}},
		},
		{
			`{app="foo"} | logfmt | duration > 1s and total_bytes < 1GB`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"duration=5m total_bytes=5kB", true}, {"duration=1s total_bytes=256B", false}, {"duration=0s", false}},
		},
		{
			`{app="foo"} |= "foo" or "bar"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo", true}, {"bar", true}, {"none", false}},
		},
		{
			`{app="foo"} |= "test" |= "foo" or "bar"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test foo", true}, {"test bar", true}, {"none", false}},
		},
		{
			`{app="foo"} |= "test" |= "foo" or "bar" or "baz"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test foo", true}, {"test bar", true}, {"test baz", true}, {"baz", false}, {"bar", false}, {"foo", false}, {"none", false}},
		},
		{
			`{app="foo"} |= "test" |= "foo" or "bar" or "baz" |= "car"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"car test foo", true}, {"car test bar", true}, {"car test baz", true}, {"baz", false}, {"bar", false}, {"test", false}, {"foo", false}, {"car", false}, {"none", false}},
		},
		{
			`{app="foo"} |= "test" |= "foo" or "bar" or "baz"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test foo", true}, {"test bar", true}, {"test baz", true}, {"none", false}},
		},
		{
			`{app="foo"} |= "foo" or "bar" |= "buzz" or "fizz"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo buzz", true}, {"bar fizz", true}, {"foo", false}, {"bar", false}, {"none", false}},
		},
		{
			`{app="foo"} |~ "foo" or "bar"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo", true}, {"bar", true}, {"none", false}},
		},
		{
			`{app="foo"} |~ "test" |~ "foo" or "bar"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test foo", true}, {"test bar", true}, {"none", false}},
		},
		{
			`{app="foo"} |~ "test" |~ "foo" or "bar" or "baz"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test foo", true}, {"test bar", true}, {"test baz", true}, {"baz", false}, {"bar", false}, {"foo", false}, {"none", false}},
		},
		{
			`{app="foo"} |~ "test" |~ "foo" or "bar" or "baz" |~ "car"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"car test foo", true}, {"car test bar", true}, {"car test baz", true}, {"baz", false}, {"bar", false}, {"test", false}, {"foo", false}, {"car", false}, {"none", false}},
		},
		{
			`{app="foo"} != "foo" or "bar"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo", false}, {"bar", false}, {"none", true}},
		},
		{
			`{app="foo"} != "test" != "foo" or "bar"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test", false}, {"foo", false}, {"bar", false}, {"none", true}},
		},
		{
			`{app="foo"} != "test" != "foo" or "bar" or "baz"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test", false}, {"foo", false}, {"bar", false}, {"baz", false}, {"none", true}},
		},
		{
			`{app="foo"} != "test" != "foo" or "bar" or "baz" != "car"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test", false}, {"foo", false}, {"bar", false}, {"baz", false}, {"car", false}, {"none", true}},
		},

		{
			`{app="foo"} |~ "foo" or "bar"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo", true}, {"bar", true}, {"none", false}},
		},
		{
			`{app="foo"} !~ "foo" or "bar"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo", false}, {"bar", false}, {"none", true}},
		},
		{
			`{app="foo"} !~ "test" !~ "foo" or "bar"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test", false}, {"foo", false}, {"bar", false}, {"none", true}},
		},
		{
			`{app="foo"} !~ "test" !~ "foo" or "bar" or "baz"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test", false}, {"foo", false}, {"bar", false}, {"baz", false}, {"none", true}},
		},
		{
			`{app="foo"} !~ "test" !~ "foo" or "bar" or "baz" !~ "car"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test", false}, {"foo", false}, {"bar", false}, {"baz", false}, {"car", false}, {"none", true}},
		},
		{
			`{app="foo"} |= ip("127.0.0.1") or "foo"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo", true}, {"bar", false}, {"127.0.0.2", false}, {"127.0.0.1", true}},
		},
		{
			`{app="foo"} != ip("127.0.0.1") or "foo"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo", false}, {"bar", true}, {"127.0.0.2", true}, {"127.0.0.1", false}},
		},
		{
			`{app="foo"} |> "<_>foo<_>" or "<_>bar<_>"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test foo test", true}, {"test bar test", true}, {"none", false}},
		},
		{
			`{app="foo"} |> "<_>foo<_>" or "<_>bar<_>" or "<_>baz<_>"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"test foo test", true}, {"test bar test", true}, {"test baz test", true}, {"none", false}},
		},
		{
			`{app="foo"} !> "foo" or "bar"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"foo", false}, {"bar", false}, {"none", true}},
		},
		{
			`{app="foo"} | logfmt | duration > -1s`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"duration=5m", true}, {"duration=1s", true}, {"duration=0s", true}, {"duration=-5m", false}},
		},
		{
			`{app="foo"} | logfmt | count > -1`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"count=5", true}, {"count=1", true}, {"count=0", true}, {"count=-5", false}},
		},
		{
			`{app="foo"} | logfmt | counter <= -1`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"counter=1", false}, {"counter=0", false}, {"counter=-1", true}, {"counter=-2", true}},
		},
		{
			`{app="foo"} |~ "\\|"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"\\", false}, {"|", true}},
		},
		{
			`{app="foo"} |~ "(?i)\\|"`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"\\", false}, {"|", true}},
		},
	} {
		t.Run(tt.q, func(t *testing.T) {
			t.Parallel()
			expr, err := ParseLogSelector(tt.q, true)
			assert.Nil(t, err)
			AssertMatchers(t, tt.expectedMatchers, expr.Matchers())
			p, err := expr.Pipeline()
			assert.Nil(t, err)
			if tt.lines == nil {
				assert.Equal(t, p, log.NewNoopPipeline())
			} else {
				sp := p.ForStream(labelBar)
				for _, lc := range tt.lines {
					_, _, matches := sp.Process(0, []byte(lc.l))
					assert.Equalf(t, lc.e, matches, "query for line '%s' was %v and not %v", lc.l, matches, lc.e)
				}
			}
		})
	}
}

func TestOrLineFilterTypes(t *testing.T) {
	for _, tt := range []struct {
		ty log.LineMatchType
	}{
		{log.LineMatchEqual},
		{log.LineMatchNotEqual},
		{log.LineMatchRegexp},
		{log.LineMatchNotRegexp},
		{log.LineMatchPattern},
		{log.LineMatchNotPattern},
	} {
		t.Run("right inherits left's type", func(t *testing.T) {
			left := &LineFilterExpr{LineFilter: LineFilter{Ty: tt.ty, Match: "something"}}
			right := &LineFilterExpr{LineFilter: LineFilter{Ty: log.LineMatchEqual, Match: "something"}}

			_ = newOrLineFilter(left, right)
			require.Equal(t, tt.ty, right.Ty)
			require.Equal(t, tt.ty, left.Ty)
		})

		t.Run("right inherits left's type with multiple or filters", func(t *testing.T) {
			f1 := &LineFilterExpr{LineFilter: LineFilter{Ty: tt.ty, Match: "something"}}
			f2 := &LineFilterExpr{LineFilter: LineFilter{Ty: log.LineMatchEqual, Match: "something"}}
			f3 := &LineFilterExpr{LineFilter: LineFilter{Ty: log.LineMatchEqual, Match: "something"}}

			_ = newOrLineFilter(f1, newOrLineFilter(f2, f3))
			require.Equal(t, tt.ty, f1.Ty)
			require.Equal(t, tt.ty, f2.Ty)
			require.Equal(t, tt.ty, f3.Ty)
		})
	}
}

func TestStringer(t *testing.T) {
	for _, tc := range []struct {
		in  string
		out string
	}{
		{
			in:  `1 > 1 > 1`,
			out: `0`,
		},
		{
			in:  `1.6`,
			out: `1.6`,
		},
		{
			in:  `1 > 1 > bool 1`,
			out: `0`,
		},
		{
			in:  `1 > bool 1 > count_over_time({foo="bar"}[1m])`,
			out: `(0 > count_over_time({foo="bar"}[1m]))`,
		},
		{
			in:  `1 > bool 1 > bool count_over_time({foo="bar"}[1m])`,
			out: `(0 > bool count_over_time({foo="bar"}[1m]))`,
		},
		{
			in:  `0 > count_over_time({foo="bar"}[1m])`,
			out: `(0 > count_over_time({foo="bar"}[1m]))`,
		},
		{
			in:  `{app="foo"} |= "foo" or "bar"`,
			out: `{app="foo"} |= "foo" or "bar"`,
		},
		{
			in:  `{app="foo"} |= "foo" or "bar" or "baz"`,
			out: `{app="foo"} |= "foo" or "bar" or "baz"`,
		},
		{
			in:  `{app="foo"} |= "foo" or "bar" or "baz" |= "car"`,
			out: `{app="foo"} |= "foo" or "bar" or "baz" |= "car"`,
		},
		{
			in:  `{app="foo"} |= "foo" or "bar" or "baz" |= "car" |= "a" or "b" or "c"`,
			out: `{app="foo"} |= "foo" or "bar" or "baz" |= "car" |= "a" or "b" or "c"`,
		},
		{
			in:  `{app="foo"} |~ "foo" or "bar" or "baz"`,
			out: `{app="foo"} |~ "foo" or "bar" or "baz"`,
		},
		{
			in:  `{app="foo"} |= "foo" or "bar" |= "buzz" or "fizz"`,
			out: `{app="foo"} |= "foo" or "bar" |= "buzz" or "fizz"`,
		},
		{
			out: `{app="foo"} |= "foo" or "bar" |~ "buzz|fizz"`,
			in:  `{app="foo"} |= "foo" or "bar" |~ "buzz|fizz"`,
		},
		{
			in:  `{app="foo"} |= ip("127.0.0.1") or "foo"`,
			out: `{app="foo"} |= ip("127.0.0.1") or "foo"`,
		},
		{
			in:  `{app="foo"} |= "foo" or ip("127.0.0.1")`,
			out: `{app="foo"} |= "foo" or ip("127.0.0.1")`,
		},
		{
			in:  `{app="foo"} |~ ip("127.0.0.1") or "foo"`,
			out: `{app="foo"} |~ ip("127.0.0.1") or "foo"`,
		},
		{
			in:  `{app="foo"} |> "foo <_> baz" or "foo <_>"`,
			out: `{app="foo"} |> "foo <_> baz" or "foo <_>"`,
		},
		{
			in:  `{app="foo"} |> "foo <_> baz" or "foo <_>" |> "foo <_> baz"`,
			out: `{app="foo"} |> "foo <_> baz" or "foo <_>" |> "foo <_> baz"`,
		},
		{ // !(A || B) == !A && !B
			in:  `{app="foo"} != "foo" or "bar"`,
			out: `{app="foo"} != "foo" != "bar"`,
		},
		{
			in:  `{app="foo"} != "test" != "foo" or "bar"`,
			out: `{app="foo"} != "test" != "foo" != "bar"`,
		},
		{
			in:  `{app="foo"} != "test" != "foo" or "bar" or "baz"`,
			out: `{app="foo"} != "test" != "foo" != "bar" != "baz"`,
		},
		{
			in:  `{app="foo"} != "foo" or "bar" or "baz" != "car"`,
			out: `{app="foo"} != "foo" != "bar" != "baz" != "car"`,
		},
		{
			in:  `{app="foo"} != "foo" or "bar" or "baz" != "car" != "a" or "b" or "c"`,
			out: `{app="foo"} != "foo" != "bar" != "baz" != "car" != "a" != "b" != "c"`,
		},
		{
			// Mix of != and |=
			in:  `{app="foo"} |= "foo" or "bar" or "baz" != "car" != "a" or "b" or "c"`,
			out: `{app="foo"} |= "foo" or "bar" or "baz" != "car" != "a" != "b" != "c"`,
		},
		{
			in:  `{app="foo"} !~ "foo" or "bar"`,
			out: `{app="foo"} !~ "foo" !~ "bar"`,
		},
		{
			in:  `{app="foo"} !~ "test" !~ "foo" or "bar"`,
			out: `{app="foo"} !~ "test" !~ "foo" !~ "bar"`,
		},
		{
			in:  `{app="foo"} !~ "test" !~ "foo" or "bar" or "baz"`,
			out: `{app="foo"} !~ "test" !~ "foo" !~ "bar" !~ "baz"`,
		},
		{
			in:  `{app="foo"} !~ "foo" or "bar" or "baz" !~ "car"`,
			out: `{app="foo"} !~ "foo" !~ "bar" !~ "baz" !~ "car"`,
		},
		{
			in:  `{app="foo"} !~ "foo" or "bar" or "baz" !~ "car" !~ "a" or "b" or "c"`,
			out: `{app="foo"} !~ "foo" !~ "bar" !~ "baz" !~ "car" !~ "a" !~ "b" !~ "c"`,
		},
		{
			// Mix of !~ and |~
			in:  `{app="foo"} |~ "foo" or "bar" or "baz" !~ "car" !~ "a" or "b" or "c"`,
			out: `{app="foo"} |~ "foo" or "bar" or "baz" !~ "car" !~ "a" !~ "b" !~ "c"`,
		},
		{
			in:  `{app="foo"} != ip("127.0.0.1") or "foo"`,
			out: `{app="foo"} != ip("127.0.0.1") != "foo"`,
		},
		{
			in:  `{app="foo"} !~ ip("127.0.0.1") or "foo"`,
			out: `{app="foo"} !~ ip("127.0.0.1") !~ "foo"`,
		},
		{
			in:  `{app="foo"} !> "<_> foo <_>" or "foo <_>" !> "foo <_> baz"`,
			out: `{app="foo"} !> "<_> foo <_>" !> "foo <_>" !> "foo <_> baz"`,
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			expr, err := ParseExpr(tc.in)
			require.Nil(t, err)
			require.Equal(t, tc.out, expr.String())
		})
	}
}

func BenchmarkContainsFilter(b *testing.B) {
	lines := [][]byte{
		[]byte("hello world foo bar"),
		[]byte("bar hello world for"),
		[]byte("hello world foobar and the bar and more bar until the end"),
		[]byte("hello world foobar and the bar and more bar and more than one hundred characters for sure until the end"),
		[]byte("hello world foobar and the bar and more bar and more than one hundred characters for sure until the end and yes bar"),
	}

	benchmarks := []struct {
		name string
		expr string
	}{
		{
			"AllMatches",
			`{app="foo"} |= "foo" |= "hello" |= "world" |= "bar" |> "<_> world <_>"`,
		},
		{
			"OneMatches",
			`{app="foo"} |= "foo" |= "not" |= "in" |= "there" |> "yet"`,
		},
		{
			"MixedFiltersTrue",
			`{app="foo"} |= "foo" != "not" |~ "hello.*bar" != "there" |= "world" |> "<_> more than one <_>"`,
		},
		{
			"MixedFiltersFalse",
			`{app="foo"} |= "baz" != "not" |~ "hello.*bar" != "there" |= "world" !> "<_> more than one"`,
		},
		{
			"GreedyRegex",
			`{app="foo"} |~ "hello.*bar.*"`,
		},
		{
			"NonGreedyRegex",
			`{app="foo"} |~ "hello.*?bar.*?"`,
		},
		{
			"ReorderedRegex",
			`{app="foo"} |~ "hello.*?bar.*?" |= "not"`,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			expr, err := ParseLogSelector(bm.expr, false)
			if err != nil {
				b.Fatal(err)
			}

			p, err := expr.Pipeline()
			if err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			sp := p.ForStream(labelBar)
			for i := 0; i < b.N; i++ {
				for _, line := range lines {
					sp.Process(0, line)
				}
			}
		})
	}
}

func Test_parserExpr_Parser(t *testing.T) {
	tests := []struct {
		name      string
		op        string
		param     string
		want      log.Stage
		wantErr   bool
		wantPanic bool
	}{
		{"json", OpParserTypeJSON, "", log.NewJSONParser(), false, false},
		{"unpack", OpParserTypeUnpack, "", log.NewUnpackParser(), false, false},
		{"pattern", OpParserTypePattern, "<foo> bar <buzz>", mustNewPatternParser("<foo> bar <buzz>"), false, false},
		{"pattern err", OpParserTypePattern, "bar", nil, true, true},
		{"regexp", OpParserTypeRegexp, "(?P<foo>foo)", mustNewRegexParser("(?P<foo>foo)"), false, false},
		{"regexp err ", OpParserTypeRegexp, "foo", nil, true, true},
		{"unknown op", "DummyOp", "", nil, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var e *LabelParserExpr
			if tt.wantPanic {
				require.Panics(t, func() { e = newLabelParserExpr(tt.op, tt.param) })
				return
			}
			e = newLabelParserExpr(tt.op, tt.param)
			got, err := e.Stage()
			if (err != nil) != tt.wantErr {
				t.Errorf("parserExpr.Parser() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				require.Nil(t, got)
			} else {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func Test_parserExpr_String(t *testing.T) {
	tests := []struct {
		name  string
		op    string
		param string
		want  string
	}{
		{"valid regexp", OpParserTypeRegexp, "foo", `| regexp "foo"`},
		{"empty regexp", OpParserTypeRegexp, "", `| regexp ""`},
		{"valid pattern", OpParserTypePattern, "buzz", `| pattern "buzz"`},
		{"empty pattern", OpParserTypePattern, "", `| pattern ""`},
		{"valid json", OpParserTypeJSON, "", `| json`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := LabelParserExpr{
				Op:    tt.op,
				Param: tt.param,
			}
			require.Equal(t, tt.want, l.String())
		})
	}
}

func mustNewRegexParser(re string) log.Stage {
	r, err := log.NewRegexpParser(re)
	if err != nil {
		panic(err)
	}
	return r
}

func mustNewPatternParser(p string) log.Stage {
	r, err := log.NewPatternParser(p)
	if err != nil {
		panic(err)
	}
	return r
}

func Test_canInjectVectorGrouping(t *testing.T) {
	tests := []struct {
		vecOp   string
		rangeOp string
		want    bool
	}{
		{OpTypeSum, OpRangeTypeBytes, true},
		{OpTypeSum, OpRangeTypeBytesRate, true},
		{OpTypeSum, OpRangeTypeSum, true},
		{OpTypeSum, OpRangeTypeRate, true},
		{OpTypeSum, OpRangeTypeCount, true},

		{OpTypeSum, OpRangeTypeAvg, false},
		{OpTypeSum, OpRangeTypeMax, false},
		{OpTypeSum, OpRangeTypeQuantile, false},
		{OpTypeSum, OpRangeTypeStddev, false},
		{OpTypeSum, OpRangeTypeStdvar, false},
		{OpTypeSum, OpRangeTypeMin, false},
		{OpTypeSum, OpRangeTypeMax, false},

		{OpTypeAvg, OpRangeTypeBytes, false},
		{OpTypeCount, OpRangeTypeBytesRate, false},
		{OpTypeBottomK, OpRangeTypeSum, false},
		{OpTypeMax, OpRangeTypeRate, false},
		{OpTypeMin, OpRangeTypeCount, false},
		{OpTypeTopK, OpRangeTypeCount, false},
	}
	for _, tt := range tests {
		t.Run(tt.vecOp+"_"+tt.rangeOp, func(t *testing.T) {
			if got := canInjectVectorGrouping(tt.vecOp, tt.rangeOp); got != tt.want {
				t.Errorf("canInjectVectorGrouping() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_MergeBinOpVectors_Filter(t *testing.T) {
	res, err := MergeBinOp(
		OpTypeGT,
		&promql.Sample{F: 2},
		&promql.Sample{F: 0},
		false,
		true,
		true,
	)
	require.NoError(t, err)

	// ensure we return the left hand side's value (2) instead of the
	// comparison operator's result (1: the truthy answer)
	require.Equal(t, &promql.Sample{F: 2}, res)
}

func TestFilterReordering(t *testing.T) {
	t.Run("it makes sure line filters are as early in the pipeline stages as possible", func(t *testing.T) {
		logExpr := `{container_name="app"} |= "foo" |= "next" | logfmt |="bar" |="baz" | line_format "{{.foo}}" |="1" |="2" | logfmt |="3"`
		l, err := ParseExpr(logExpr)
		require.NoError(t, err)

		stages := l.(*PipelineExpr).MultiStages.reorderStages()
		require.Len(t, stages, 5)
		require.Equal(t, `|= "foo" |= "next" |= "bar" |= "baz" | logfmt | line_format "{{.foo}}" |= "1" |= "2" |= "3" | logfmt`, MultiStageExpr(stages).String())
	})

	t.Run("unpack test", func(t *testing.T) {
		logExpr := `{container_name="app"} |= "06497595" | unpack != "message" | json | line_format "new log: {{.foo}}"`
		l, err := ParseExpr(logExpr)
		require.NoError(t, err)

		stages := l.(*PipelineExpr).MultiStages.reorderStages()
		require.Len(t, stages, 5)
		require.Equal(t, `|= "06497595" | unpack != "message" | json | line_format "new log: {{.foo}}"`, MultiStageExpr(stages).String())
	})

	t.Run("it makes sure label filter order is kept", func(t *testing.T) {
		logExpr := `{container_name="app"} | bar="next" |= "foo" | logfmt |="bar" |="baz" | line_format "{{.foo}}" |="1" |="2" | logfmt |="3"`
		l, err := ParseExpr(logExpr)
		require.NoError(t, err)

		stages := l.(*PipelineExpr).MultiStages.reorderStages()
		require.Len(t, stages, 6)
		require.Equal(t, `| bar="next" |= "foo" |= "bar" |= "baz" | logfmt | line_format "{{.foo}}" |= "1" |= "2" |= "3" | logfmt`, MultiStageExpr(stages).String())
	})

	t.Run("it makes sure line filters before labels filters keeps correct ordering", func(t *testing.T) {
		logExpr := `{container_name="app"} |= "foo" |bar="next"`
		l, err := ParseExpr(logExpr)
		require.NoError(t, err)

		stages := l.(*PipelineExpr).MultiStages.reorderStages()
		require.Len(t, stages, 2)
		require.Equal(t, `|= "foo" | bar="next"`, MultiStageExpr(stages).String())
	})

	t.Run("it makes sure json before label filter keeps correct ordering", func(t *testing.T) {
		logExpr := `{container_name="app"} | json | bar="next"`
		l, err := ParseExpr(logExpr)
		require.NoError(t, err)

		stages := l.(*PipelineExpr).MultiStages.reorderStages()
		require.Len(t, stages, 2)
		require.Equal(t, `| json | bar="next"`, MultiStageExpr(stages).String())
	})
}

var result bool

func BenchmarkReorderedPipeline(b *testing.B) {
	logfmtLine := []byte(`level=info ts=2020-12-14T21:25:20.947307459Z caller=metrics.go:83 org_id=29 traceID=c80e691e8db08e2 latency=fast query="sum by (object_name) (rate(({container=\"metrictank\", cluster=\"hm-us-east2\"} |= \"PANIC\")[5m]))" query_type=metric range_type=range length=5m0s step=15s duration=322.623724ms status=200 throughput=1.2GB total_bytes=375MB`)

	logExpr := `{container_name="app"} | logfmt |= "slow"`
	l, err := ParseLogSelector(logExpr, true)
	require.NoError(b, err)

	p, err := l.Pipeline()
	require.NoError(b, err)

	sp := p.ForStream(labels.EmptyLabels())

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, _, result = sp.Process(0, logfmtLine)
	}
}

func TestParseLargeQuery(t *testing.T) {
	// This query originally failed to parse because there was a function
	// definition at 1024 bytes whichcaused the lexer scanner to become
	// out of sync with it's underlying reader due to shared state
	line := `((sum(count_over_time({foo="bar"} |~ "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" [6h])) / sum(count_over_time({foo="bar"} |~ "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" [6h]))) * 100)`

	_, err := ParseExpr(line)
	require.NoError(t, err)
}

func TestLogSelectorExprHasFilter(t *testing.T) {
	for query, hasFilter := range map[string]bool{
		`{foo="bar"} |= ""`:                  false,
		`{foo="bar"} |= "" |= ""`:            false,
		`{foo="bar"} |~ ""`:                  false,
		`{foo="bar"} |= "notempty"`:          true,
		`{foo="bar"} |= "" |= "notempty"`:    true,
		`{foo="bar"} != ""`:                  true,
		`{foo="bar"} | lbl="notempty"`:       true,
		`{foo="bar"} |= "" | lbl="notempty"`: true,
	} {
		expr, err := ParseExpr(query)
		require.NoError(t, err)
		require.Equal(t, hasFilter, expr.(LogSelectorExpr).HasFilter())
	}
}

func TestGroupingString(t *testing.T) {
	g := Grouping{
		Groups:  []string{"a", "b"},
		Without: false,
	}
	require.Equal(t, " by (a,b)", g.String())

	g = Grouping{
		Groups:  []string{},
		Without: false,
	}
	require.Equal(t, " by ()", g.String())

	g = Grouping{
		Groups:  nil,
		Without: false,
	}
	require.Equal(t, " by ()", g.String())

	g = Grouping{
		Groups:  []string{"a", "b"},
		Without: true,
	}
	require.Equal(t, " without (a,b)", g.String())

	g = Grouping{
		Groups:  []string{},
		Without: true,
	}
	require.Equal(t, " without ()", g.String())

	g = Grouping{
		Groups:  nil,
		Without: true,
	}
	require.Equal(t, " without ()", g.String())
}

func TestCombineFilters(t *testing.T) {
	in := []*LineFilterExpr{
		{LineFilter: LineFilter{Ty: log.LineMatchEqual, Match: "test1"}},
		{LineFilter: LineFilter{Ty: log.LineMatchEqual, Match: "test2"}},
	}

	var combineFilter StageExpr
	for i := 0; i < 2; i++ {
		combineFilter = combineFilters(in)
	}

	current := combineFilter.(*LineFilterExpr)
	i := 0
	for ; current.Left != nil; current = current.Left {
		i++
		if i > 2 {
			t.Fatalf("left num isn't a correct number")
		}
	}
}
