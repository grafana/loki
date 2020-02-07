package logql

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prometheus/prometheus/pkg/labels"
)

func Test_logSelectorExpr_String(t *testing.T) {
	t.Parallel()
	tests := []string{
		`{foo!~"bar"}`,
		`{foo="bar", bar!="baz"}`,
		`{foo="bar", bar!="baz"} != "bip" !~ ".+bop"`,
		`{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap"`,
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt, func(t *testing.T) {
			t.Parallel()
			expr, err := ParseLogSelector(tt)
			if err != nil {
				t.Fatalf("failed to parse log selector: %s", err)
			}
			if expr.String() != strings.Replace(tt, " ", "", -1) {
				t.Fatalf("error expected: %s got: %s", tt, expr.String())
			}
		})
	}
}

func Test_SampleExpr_String(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{
		`rate( ( {job="mysql"} |="error" !="timeout" ) [10s] )`,
		`sum without(a) ( rate ( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )`,
		`sum by(a) (rate( ( {job="mysql"} |="error" !="timeout" ) [10s] ) )`,
		`sum(count_over_time({job="mysql"}[5m]))`,
		`topk(10,sum(rate({region="us-east1"}[5m])) by (name))`,
		`avg( rate( ( {job="nginx"} |= "GET" ) [10s] ) ) by (region)`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m]))`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m])) / sum by (cluster) (count_over_time({job="postgres"}[5m])) `,
		`
		sum by (cluster) (count_over_time({job="postgres"}[5m])) /
		sum by (cluster) (count_over_time({job="postgres"}[5m])) /
		sum by (cluster) (count_over_time({job="postgres"}[5m]))
		`,
		`sum by (cluster) (count_over_time({job="mysql"}[5m])) / min(count_over_time({job="mysql"}[5m])) `,
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
	} {
		tt := tt
		t.Run(tt.q, func(t *testing.T) {
			t.Parallel()
			expr, err := ParseLogSelector(tt.q)
			assert.Nil(t, err)
			assert.Equal(t, tt.expectedMatchers, expr.Matchers())
			f, err := expr.Filter()
			assert.Nil(t, err)
			if tt.lines == nil {
				assert.Nil(t, f)
			} else {
				for _, lc := range tt.lines {
					assert.Equal(t, lc.e, f([]byte(lc.l)))
				}
			}
		})
	}
}

func BenchmarkContainsFilter(b *testing.B) {
	expr, err := ParseLogSelector(`{app="foo"} |= "foo"`)
	if err != nil {
		b.Fatal(err)
	}

	f, err := expr.Filter()
	if err != nil {
		b.Fatal(err)
	}

	line := []byte("hello world foo bar")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if !f(line) {
			b.Fatal("doesn't match")
		}
	}
}
