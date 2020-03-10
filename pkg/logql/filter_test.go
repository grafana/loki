package logql

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_SimplifiedRegex(t *testing.T) {
	fixtures := []string{
		"foo", "foobar", "bar", "foobuzz", "buzz", "f", "  ", "fba", "foofoofoo", "b", "foob", "bfoo",
	}
	for _, test := range []struct {
		re         string
		simplified bool
		expected   LineFilter
		match      bool
	}{
		// regex we intend to support.
		{"foo", true, literalFilter([]byte("foo")), true},
		{"not", true, newNotFilter(literalFilter([]byte("not"))), false},
		{"(foo)", true, literalFilter([]byte("foo")), true},
		{"(foo|ba)", true, newOrFilter(literalFilter([]byte("foo")), literalFilter([]byte("ba"))), true},
		{"(foo|ba|ar)", true, newOrFilter(newOrFilter(literalFilter([]byte("foo")), literalFilter([]byte("ba"))), literalFilter([]byte("ar"))), true},
		{"(foo|(ba|ar))", true, newOrFilter(literalFilter([]byte("foo")), newOrFilter(literalFilter([]byte("ba")), literalFilter([]byte("ar")))), true},
		{"foo.*", true, literalFilter([]byte("foo")), true},
		{".*foo", true, newNotFilter(literalFilter([]byte("foo"))), false},
		{".*foo.*", true, literalFilter([]byte("foo")), true},
		{"(.*)(foo).*", true, literalFilter([]byte("foo")), true},
		{"(foo.*|.*ba)", true, newOrFilter(literalFilter([]byte("foo")), literalFilter([]byte("ba"))), true},
		{"(foo.*|.*bar.*)", true, newNotFilter(newOrFilter(literalFilter([]byte("foo")), literalFilter([]byte("bar")))), false},
		{".*foo.*|bar", true, newNotFilter(newOrFilter(literalFilter([]byte("foo")), literalFilter([]byte("bar")))), false},
		{".*foo|bar", true, newNotFilter(newOrFilter(literalFilter([]byte("foo")), literalFilter([]byte("bar")))), false},
		// This construct is similar to (...), but won't create a capture group.
		{"(?:.*foo.*|bar)", true, newOrFilter(literalFilter([]byte("foo")), literalFilter([]byte("bar"))), true},
		// named capture group
		{"(?P<foo>.*foo.*|bar)", true, newOrFilter(literalFilter([]byte("foo")), literalFilter([]byte("bar"))), true},
		// parsed as (?-s:.)*foo(?-s:.)*|b(?:ar|uzz)
		{".*foo.*|bar|buzz", true, newOrFilter(literalFilter([]byte("foo")), newOrFilter(literalFilter([]byte("bar")), literalFilter([]byte("buzz")))), true},
		// parsed as (?-s:.)*foo(?-s:.)*|bar|uzz
		{".*foo.*|bar|uzz", true, newOrFilter(newOrFilter(literalFilter([]byte("foo")), literalFilter([]byte("bar"))), literalFilter([]byte("uzz"))), true},
		// parsed as foo|b(?:ar|(?:)|uzz)|zz
		{"foo|bar|b|buzz|zz", true, newOrFilter(newOrFilter(literalFilter([]byte("foo")), newOrFilter(newOrFilter(literalFilter([]byte("bar")), literalFilter([]byte("b"))), literalFilter([]byte("buzz")))), literalFilter([]byte("zz"))), true},
		// parsed as f(?:(?:)|oo(?:(?:)|bar))
		{"f|foo|foobar", true, newOrFilter(literalFilter([]byte("f")), newOrFilter(literalFilter([]byte("foo")), literalFilter([]byte("foobar")))), true},
		// parsed as f(?:(?-s:.)*|oobar(?-s:.)*)|(?-s:.)*buzz
		{"f.*|foobar.*|.*buzz", true, newOrFilter(newOrFilter(literalFilter([]byte("f")), literalFilter([]byte("foobar"))), literalFilter([]byte("buzz"))), true},
		// parsed as ((f(?-s:.)*)|foobar(?-s:.)*)|(?-s:.)*buzz
		{"((f.*)|foobar.*)|.*buzz", true, newOrFilter(newOrFilter(literalFilter([]byte("f")), literalFilter([]byte("foobar"))), literalFilter([]byte("buzz"))), true},

		// regex we are not supporting.
		{"[a-z]+foo", false, nil, false},
		{".+foo", false, nil, false},
		{".*fo.*o", false, nil, false},
		{".*", false, nil, false},
		{`\d`, false, nil, false},
		{`\sfoo`, false, nil, false},
		{`foo?`, false, nil, false},
		{`foo{1,2}bar{2,3}`, false, nil, false},
		{`foo|\d*bar`, false, nil, false},
		{`foo|fo{1,2}`, false, nil, false},
		{`foo|fo\d*`, false, nil, false},
		{`foo|fo\d+`, false, nil, false},
		{`(\w\d+)`, false, nil, false},
		{`.*f.*oo|fo{1,2}`, false, nil, false},
	} {
		t.Run(test.re, func(t *testing.T) {
			d, err := newRegexpFilter(test.re, test.match)
			require.NoError(t, err, "invalid regex")

			f, err := parseRegexpFilter(test.re, test.match)
			require.NoError(t, err)

			// if we don't expect simplification then the filter should be the same as the default one.
			if !test.simplified {
				require.Equal(t, d, f)
				return
			}
			// otherwise ensure we have different filter
			require.NotEqual(t, f, d)
			require.Equal(t, test.expected, f)
			// tests all lines with both filter, they should have the same result.
			for _, line := range fixtures {
				l := []byte(line)
				require.Equal(t, d.Filter(l), f.Filter(l), "regexp %s failed line: %s", test.re, line)
			}
		})
	}
}

func Benchmark_LineFilter(b *testing.B) {
	b.ReportAllocs()
	logline := `level=bar ts=2020-02-22T14:57:59.398312973Z caller=logging.go:44 traceID=2107b6b551458908 msg="GET /buzz (200) 4.599635ms`
	for _, test := range []struct {
		re string
	}{
		{"foo.*"},
		{".*foo.*"},
		{".*foo"},
		{"foo|bar"},
		{"foo|bar|buzz"},
		{"foo|(bar|buzz)"},
		{"foo|bar.*|buzz"},
		{".*foo.*|bar|uzz"},
		{"((f.*)|foobar.*)|.*buzz"},
		{"(?P<foo>.*foo.*|bar)"},
	} {
		benchmarkRegex(b, test.re, logline, true)
		benchmarkRegex(b, test.re, logline, false)
	}
}

// see https://dave.cheney.net/2013/06/30/how-to-write-benchmarks-in-go
// A note on compiler optimisations
var res bool

func benchmarkRegex(b *testing.B, re, line string, match bool) {
	var m bool
	l := []byte(line)
	d, err := newRegexpFilter(re, match)
	if err != nil {
		b.Fatal(err)
	}
	s, err := parseRegexpFilter(re, match)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.Run(fmt.Sprintf("default_%v_%s", match, re), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m = d.Filter(l)
		}
	})
	b.Run(fmt.Sprintf("simplified_%v_%s", match, re), func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m = s.Filter(l)
		}
	})
	res = m
}
