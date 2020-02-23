package logql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_ParseRegex(t *testing.T) {
	for _, test := range []struct {
		re   string
		line string
	}{
		{"foo", "foo"},
		{"(foo)", "foobar"},
		{"(foo|ba)", "foobar"},
		{"(foo.*|.*ba)", "foobar"},
		{"(foo.*|.*ba)", "fo"},
		{"(foo|ba|ar)", "bar"},
		{"(foo|(ba|ar))", "bar"},
		{"foo.*", "foobar"},
		{".*foo", "foobar"},
		{".*foo.*", "foobar"},
		{"(.*)(foo).*", "foobar"},
	} {
		t.Run(test.re, func(t *testing.T) {
			assertRegex(t, test.re, test.line, true)
			assertRegex(t, test.re, test.line, false)
		})
	}
}

func assertRegex(t *testing.T, re, line string, match bool) {
	t.Helper()
	f, err := ParseRegex(re, match)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := defaultRegex(re, match)
	if err != nil {
		t.Fatal(err)
	}
	l := []byte(line)
	// ensure we have different filter but same result
	require.NotEqual(t, f, f2)
	require.Equal(t, f2.Filter(l), f.Filter(l))
}

func Benchmark_Regex(b *testing.B) {
	b.ReportAllocs()

	for _, test := range []struct {
		re      string
		match   bool
		logline []byte
	}{
		{"foo|bar", true, []byte(`level=debug ts=2020-02-22T14:57:59.398312973Z caller=logging.go:44 traceID=2107b6b551458908 msg="GET /metrics (200) 4.599635ms`)},
	} {
		f, err := defaultRegex("foo|bar", true)
		if err != nil {
			b.Fatal(err)
		}
		b.Run("default_"+test.re, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = f.Filter(logLine)
			}
		})
		f, err = ParseRegex("foo|bar", true)
		if err != nil {
			b.Fatal(err)
		}
		b.Run("simplified_"+test.re, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = f.Filter(logLine)
			}
		})
	}

}

var logLine = []byte(`level=debug ts=2020-02-22T14:57:59.398312973Z caller=logging.go:44 traceID=2107b6b551458908 msg="GET /metrics (200) 4.599635ms`)
