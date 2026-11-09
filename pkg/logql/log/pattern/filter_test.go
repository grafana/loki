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
