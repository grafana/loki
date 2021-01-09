package logql

import (
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/log"
)

func Test_logSelectorExpr_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		selector     string
		expectFilter bool
	}{
		{`{foo!~"bar"}`, false},
		{`{foo="bar", bar!="baz"}`, false},
		{`{foo="bar", bar!="baz"} != "bip" !~ ".+bop"`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap"`, true},
		{`{foo="bar", bar!="baz"} |= ""`, false},
		{`{foo="bar", bar!="baz"} |~ ""`, false},
		{`{foo="bar", bar!="baz"} |~ ".*"`, false},
		{`{foo="bar", bar!="baz"} |= "" |= ""`, false},
		{`{foo="bar", bar!="baz"} |~ "" |= "" |~ ".*"`, false},
		{`{foo="bar", bar!="baz"} != "bip" !~ ".+bop" | json`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | logfmt`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | logfmt | b>=10GB`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | regexp "(?P<foo>foo|bar)"`, true},
		{`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | regexp "(?P<foo>foo|bar)" | ( ( foo<5.01 , bar>20ms ) or foo="bar" ) | line_format "blip{{.boop}}bap" | label_format foo=bar,bar="blip{{.blop}}"`, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.selector, func(t *testing.T) {
			t.Parallel()
			expr, err := ParseLogSelector(tt.selector)
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
		`rate( ( {job="mysql"} |="error" !="timeout" ) [10s] )`,
		`absent_over_time( ( {job="mysql"} |="error" !="timeout" ) [10s] )`,
		`sum without(a) ( rate ( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )`,
		`sum by(a) (rate( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )`,
		`sum(count_over_time({job="mysql"}[5m]))`,
		`sum(count_over_time({job="mysql"} | json [5m]))`,
		`sum(count_over_time({job="mysql"} | logfmt [5m]))`,
		`sum(count_over_time({job="mysql"} | regexp "(?P<foo>foo|bar)" [5m]))`,
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
	} {
		t.Run(tc, func(t *testing.T) {
			expr, err := ParseExpr(tc)
			require.Nil(t, err)

			expr2, err := ParseExpr(expr.String())
			require.Nil(t, err)
			require.Equal(t, expr, expr2)
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
	} {
		t.Run(tc, func(t *testing.T) {
			expr, err := ParseLogSelector(tc)
			require.Nil(t, err)

			p, err := expr.Pipeline()
			require.Nil(t, err)
			_, _, ok := p.ForStream(labelBar).Process([]byte("bleepbloop"))

			require.True(t, ok)
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
			`{app="foo"} | logfmt | duration > 1s and total_bytes < 1GB`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
			},
			[]linecheck{{"duration=5m total_bytes=5kB", true}, {"duration=1s total_bytes=256B", false}, {"duration=0s", false}},
		},
	} {
		tt := tt
		t.Run(tt.q, func(t *testing.T) {
			t.Parallel()
			expr, err := ParseLogSelector(tt.q)
			assert.Nil(t, err)
			assert.Equal(t, tt.expectedMatchers, expr.Matchers())
			p, err := expr.Pipeline()
			assert.Nil(t, err)
			if tt.lines == nil {
				assert.Equal(t, p, log.NewNoopPipeline())
			} else {
				sp := p.ForStream(labelBar)
				for _, lc := range tt.lines {
					_, _, ok := sp.Process([]byte(lc.l))
					assert.Equalf(t, lc.e, ok, "query for line '%s' was %v and not %v", lc.l, ok, lc.e)
				}
			}
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
			out: `0 > count_over_time({foo="bar"}[1m])`,
		},
		{
			in:  `1 > bool 1 > bool count_over_time({foo="bar"}[1m])`,
			out: `0 > bool count_over_time({foo="bar"}[1m])`,
		},
		{

			in:  `0 > count_over_time({foo="bar"}[1m])`,
			out: `0 > count_over_time({foo="bar"}[1m])`,
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
	expr, err := ParseLogSelector(`{app="foo"} |= "foo"`)
	if err != nil {
		b.Fatal(err)
	}

	p, err := expr.Pipeline()
	if err != nil {
		b.Fatal(err)
	}

	line := []byte("hello world foo bar")

	b.ResetTimer()

	sp := p.ForStream(labelBar)
	for i := 0; i < b.N; i++ {
		if _, _, ok := sp.Process(line); !ok {
			b.Fatal("doesn't match")
		}
	}
}

func Test_parserExpr_Parser(t *testing.T) {
	tests := []struct {
		name    string
		op      string
		param   string
		want    log.Stage
		wantErr bool
	}{
		{"json", OpParserTypeJSON, "", log.NewJSONParser(), false},
		{"logfmt", OpParserTypeLogfmt, "", log.NewLogfmtParser(), false},
		{"regexp", OpParserTypeRegexp, "(?P<foo>foo)", mustNewRegexParser("(?P<foo>foo)"), false},
		{"regexp err ", OpParserTypeRegexp, "foo", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &labelParserExpr{
				op:    tt.op,
				param: tt.param,
			}
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

func mustNewRegexParser(re string) log.Stage {
	r, err := log.NewRegexpParser(re)
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
