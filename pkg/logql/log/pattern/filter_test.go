package pattern

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatcherMatchesPrefix(t *testing.T) {
	for _, tt := range []struct {
		name string
		line string
		want [][]byte
	}{
		{"empty capture", "prefix ", [][]byte{[]byte("")}},
		{"short line", "pre", nil},
		{"empty line", "", nil},
		{"later occurrence", "x prefix value", nil},
		{"long miss", strings.Repeat("x", 4096) + "prefix value", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, err := New("prefix <value>")
			require.NoError(t, err)
			require.Equal(t, tt.want, m.Matches([]byte(tt.line)))
		})
	}
}

func TestMatcherTestSemantics(t *testing.T) {
	for _, tt := range []struct {
		name    string
		pattern string
		line    string
		want    bool
	}{
		{"empty", "", "", true},
		{"empty pattern", "", "log", false},
		{"empty line", "<_>", "", false},
		{"literal only", "foo", "foo", true},
		{"missing literal", "foo<_>", "bar", false},
		{"unanchored literal", "foo", "prefix foo", true},
		{"literal remainder", "foo", "foo suffix", false},
		{"first occurrence", "foo", "foofoo", false},
		{"no retry after empty capture", "<_>foo", "foo xfoo", false},
		{"unanchored trailing capture", "foo<_>", "prefix foox", true},
		{"empty interior capture", "foo<_>bar", "foobar", false},
		{"no retry interior", "foo<_>bar", "foobar xbar", false},
		{"repeated literal", "<_>foo<_>foo<_>", "xfooyfooz", true},
		{"adjacent repeated literal", "<_>foo<_>foo<_>", "xfoofooz", false},
		{"binary input", "<_>foo<_>", "\x00foo\xff", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, err := ParseLineFilter([]byte(tt.pattern))
			require.NoError(t, err)
			require.Equal(t, tt.want, m.Test([]byte(tt.line)))
		})
	}
	var zero Matcher
	require.True(t, zero.Test(nil))
	require.False(t, zero.Test([]byte("log")))
}

func BenchmarkMatcherTest(b *testing.B) {
	for _, tt := range fixtures {
		b.Run(tt.expr, func(b *testing.B) {
			m, err := New(tt.expr)
			require.NoError(b, err)
			line := []byte(tt.in)
			require.Equal(b, tt.matches, m.Test(line))
			b.ReportAllocs()
			b.SetBytes(int64(len(line)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkMatched = m.Test(line)
			}
		})
	}
}

var benchmarkMatched bool

func BenchmarkMatcherFilter(b *testing.B) {
	logfmt := `level=info ts=2026-11-09T12:00:00Z caller=handler.go:123 tenant=example msg="request completed" status=200 duration=10ms`
	long := strings.Repeat("x", 4096)
	for _, tt := range []struct {
		name    string
		pattern string
		line    string
		want    bool
	}{
		{"literal/hit", "request completed", "request completed", true},
		{"literal/miss", "level=debug", logfmt, false},
		{"contains/hit", "<_>msg=<_>", logfmt, true},
		{"contains/miss", "<_>level=debug<_>", logfmt, false},
		{"prefix/hit", "level=info<_>", logfmt, true},
		{"prefix/miss", "level=debug<_>", logfmt, false},
		{"multi/hit", "level=info<_>msg=<_>status=200<_>", logfmt, true},
		{"multi/late_miss", "level=info<_>msg=<_>status=500<_>", logfmt, false},
		{"suffix/hit", "<_>duration=10ms", logfmt, true},
		{"suffix/repeated", "<_>x", "xx", false},
		{"capture_only", "<_>", logfmt, true},
		{"long/hit", "<_>needle<_>", long + "needle tail", true},
		{"long/miss", "<_>needle<_>", long, false},
		{"short/miss", "level=info<_>msg=<_>status=200<_>", "x", false},
	} {
		b.Run(tt.name, func(b *testing.B) {
			m, err := ParseLineFilter([]byte(tt.pattern))
			require.NoError(b, err)
			line := []byte(tt.line)
			require.Equal(b, tt.want, m.Test(line))
			b.ReportAllocs()
			b.SetBytes(int64(len(line)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkMatched = m.Test(line)
			}
		})
	}
}

func BenchmarkMatcherMatchesPrefixMiss(b *testing.B) {
	m, err := New("prefix <value>")
	require.NoError(b, err)
	line := []byte(strings.Repeat("x", 4096) + "prefix value")
	require.Nil(b, m.Matches(line))
	b.ReportAllocs()
	b.SetBytes(int64(len(line)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res = m.Matches(line)
	}
}
