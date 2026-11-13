package pattern

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatcherMatchesPrefix(t *testing.T) {
	type testCase struct {
		name string
		line string
		want [][]byte
	}
	for _, tt := range []testCase{
		// Differs from the line filter: the parser accepts an empty capture, but
		// Test rejects "prefix " for the filter "prefix <_>".
		{name: "empty capture", line: "prefix ", want: [][]byte{[]byte("")}},
		{name: "short line", line: "pre", want: nil},
		{name: "empty line", line: "", want: nil},
		{name: "later occurrence", line: "x prefix value", want: nil},
		{name: "long miss", line: strings.Repeat("x", 4096) + "prefix value", want: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, err := New("prefix <value>")
			require.NoError(t, err)
			require.Equal(t, tt.want, m.Matches([]byte(tt.line)))
		})
	}
}

func TestMatcherTestSemantics(t *testing.T) {
	type testCase struct {
		name    string
		pattern string
		line    string
		want    bool
	}
	for _, tt := range []testCase{
		{name: "empty", pattern: "", line: "", want: true},
		{name: "empty pattern", pattern: "", line: "log", want: false},
		{name: "empty line", pattern: "<_>", line: "", want: false},
		{name: "literal only", pattern: "foo", line: "foo", want: true},
		{name: "missing literal", pattern: "foo<_>", line: "bar", want: false},
		{name: "literal remainder", pattern: "foo", line: "foo suffix", want: false},
		// Odd: "foo" matches "xfoo" because the start is not anchored, but it does
		// not match "foofoo" because Test only tries the first "foo".
		{name: "first occurrence", pattern: "foo", line: "foofoo", want: false},
		// Odd: "<_>foo" matches "a xfoo" but not "foo xfoo". The first "foo" leaves
		// the capture empty, and Test does not try the later "foo". The parser
		// "<x>foo" accepts this line with an empty capture.
		{name: "no retry after empty capture", pattern: "<_>foo", line: "foo xfoo", want: false},
		// Differs from the parser: #12398 says that "<_>" masks non-empty text,
		// but the parser "foo<x>bar" accepts this line with an empty capture.
		{name: "empty interior capture", pattern: "foo<_>bar", line: "foobar", want: false},
		// Odd: "bar x" is a valid non-empty capture, but Test stops at the first
		// "bar". The parser "foo<x>bar" accepts this line with an empty capture.
		{name: "no retry interior", pattern: "foo<_>bar", line: "foobar xbar", want: false},
		{name: "repeated literal", pattern: "<_>foo<_>foo<_>", line: "xfooyfooz", want: true},
		// Differs from the parser: no split gives three non-empty captures, but
		// the parser "<a>foo<b>foo<c>" accepts this line with an empty middle capture.
		{name: "adjacent repeated literal", pattern: "<_>foo<_>foo<_>", line: "xfoofooz", want: false},
		{name: "binary input", pattern: "<_>foo<_>", line: "\x00foo\xff", want: true},
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
