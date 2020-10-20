package log

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_SimplifiedRegex(t *testing.T) {
	fixtures := []string{
		"foo", "foobar", "bar", "foobuzz", "buzz", "f", "  ", "fba", "foofoofoo", "b", "foob", "bfoo", "FoO",
	}
	for _, test := range []struct {
		re         string
		simplified bool
		expected   Filterer
		match      bool
	}{
		// regex we intend to support.
		{"foo", true, newContainsFilter([]byte("foo"), false), true},
		{"not", true, newNotFilter(newContainsFilter([]byte("not"), false)), false},
		{"(foo)", true, newContainsFilter([]byte("foo"), false), true},
		{"(foo|ba)", true, newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("ba"), false)), true},
		{"(foo|ba|ar)", true, newOrFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("ba"), false)), newContainsFilter([]byte("ar"), false)), true},
		{"(foo|(ba|ar))", true, newOrFilter(newContainsFilter([]byte("foo"), false), newOrFilter(newContainsFilter([]byte("ba"), false), newContainsFilter([]byte("ar"), false))), true},
		{"foo.*", true, newContainsFilter([]byte("foo"), false), true},
		{".*foo", true, newNotFilter(newContainsFilter([]byte("foo"), false)), false},
		{".*foo.*", true, newContainsFilter([]byte("foo"), false), true},
		{"(.*)(foo).*", true, newContainsFilter([]byte("foo"), false), true},
		{"(foo.*|.*ba)", true, newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("ba"), false)), true},
		{"(foo.*|.*bar.*)", true, newNotFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("bar"), false))), false},
		{".*foo.*|bar", true, newNotFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("bar"), false))), false},
		{".*foo|bar", true, newNotFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("bar"), false))), false},
		// This construct is similar to (...), but won't create a capture group.
		{"(?:.*foo.*|bar)", true, newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("bar"), false)), true},
		// named capture group
		{"(?P<foo>.*foo.*|bar)", true, newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("bar"), false)), true},
		// parsed as (?-s:.)*foo(?-s:.)*|b(?:ar|uzz)
		{".*foo.*|bar|buzz", true, newOrFilter(newContainsFilter([]byte("foo"), false), newOrFilter(newContainsFilter([]byte("bar"), false), newContainsFilter([]byte("buzz"), false))), true},
		// parsed as (?-s:.)*foo(?-s:.)*|bar|uzz
		{".*foo.*|bar|uzz", true, newOrFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("bar"), false)), newContainsFilter([]byte("uzz"), false)), true},
		// parsed as foo|b(?:ar|(?:)|uzz)|zz
		{"foo|bar|b|buzz|zz", true, newOrFilter(newOrFilter(newContainsFilter([]byte("foo"), false), newOrFilter(newOrFilter(newContainsFilter([]byte("bar"), false), newContainsFilter([]byte("b"), false)), newContainsFilter([]byte("buzz"), false))), newContainsFilter([]byte("zz"), false)), true},
		// parsed as f(?:(?:)|oo(?:(?:)|bar))
		{"f|foo|foobar", true, newOrFilter(newContainsFilter([]byte("f"), false), newOrFilter(newContainsFilter([]byte("foo"), false), newContainsFilter([]byte("foobar"), false))), true},
		// parsed as f(?:(?-s:.)*|oobar(?-s:.)*)|(?-s:.)*buzz
		{"f.*|foobar.*|.*buzz", true, newOrFilter(newOrFilter(newContainsFilter([]byte("f"), false), newContainsFilter([]byte("foobar"), false)), newContainsFilter([]byte("buzz"), false)), true},
		// parsed as ((f(?-s:.)*)|foobar(?-s:.)*)|(?-s:.)*buzz
		{"((f.*)|foobar.*)|.*buzz", true, newOrFilter(newOrFilter(newContainsFilter([]byte("f"), false), newContainsFilter([]byte("foobar"), false)), newContainsFilter([]byte("buzz"), false)), true},
		{".*", true, TrueFilter, true},
		{".*|.*", true, TrueFilter, true},
		{".*||||", true, TrueFilter, true},
		{"", true, TrueFilter, true},
		{"(?i)foo", true, newContainsFilter([]byte("foo"), true), true},

		// regex we are not supporting.
		{"[a-z]+foo", false, nil, false},
		{".+foo", false, nil, false},
		{".*fo.*o", false, nil, false},
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

func Test_TrueFilter(t *testing.T) {
	empty := []byte("")
	for _, test := range []struct {
		name       string
		f          Filterer
		expectTrue bool
	}{
		{"empty match", newContainsFilter(empty, false), true},
		{"not empty match", newNotFilter(newContainsFilter(empty, true)), false},
		{"match", newContainsFilter([]byte("foo"), false), false},
		{"empty match and", NewAndFilter(newContainsFilter(empty, false), newContainsFilter(empty, false)), true},
		{"empty match or", newOrFilter(newContainsFilter(empty, false), newContainsFilter(empty, false)), true},
		{"nil right and", NewAndFilter(newContainsFilter(empty, false), nil), true},
		{"nil left or", newOrFilter(nil, newContainsFilter(empty, false)), true},
		{"nil right and not empty", NewAndFilter(newContainsFilter([]byte("foo"), false), nil), false},
		{"nil left or not empty", newOrFilter(nil, newContainsFilter([]byte("foo"), false)), false},
		{"nil both and", NewAndFilter(nil, nil), false}, // returns nil
		{"nil both or", newOrFilter(nil, nil), false},   // returns nil
		{"empty match and chained", NewAndFilter(newContainsFilter(empty, false), NewAndFilter(newContainsFilter(empty, false), NewAndFilter(newContainsFilter(empty, false), newContainsFilter(empty, false)))), true},
		{"empty match or chained", newOrFilter(newContainsFilter(empty, false), newOrFilter(newContainsFilter(empty, true), newOrFilter(newContainsFilter(empty, false), newContainsFilter(empty, false)))), true},
		{"empty match and", newNotFilter(NewAndFilter(newContainsFilter(empty, false), newContainsFilter(empty, false))), false},
		{"empty match or", newNotFilter(newOrFilter(newContainsFilter(empty, false), newContainsFilter(empty, false))), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.expectTrue {
				require.Equal(t, TrueFilter, test.f)
			} else {
				require.NotEqual(t, TrueFilter, test.f)
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
		{"(?i)foo"},
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
// A note on compiler optimizations
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
