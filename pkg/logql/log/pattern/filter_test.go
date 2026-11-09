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
		{"literal remainder", "foo", "foo suffix", false},
		{"first occurrence", "foo", "foofoo", false},
		{"no retry after empty capture", "<_>foo", "foo xfoo", false},
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

// The pattern parser anchors a leading literal at the start of the line, as the
// pattern parser documentation says. #12398 says that the |> and !> line filters
// use the same semantics, except that named captures are not allowed. Test
// accepts the leading literal at any position in the line, so a line filter
// matches lines that the parser does not match.
func TestMatcherTestAnchorsLeadingLiteral(t *testing.T) {
	for _, tt := range []struct {
		name   string
		filter string
		parser string
		line   string
		want   bool
	}{
		{"leading literal at start", "foo <_>", "foo <x>", "foo bar", true},
		{"leading literal later in line", "foo <_>", "foo <x>", "prefix foo bar", false},
		{"leading and trailing literals", "foo <_> baz", "foo <x> baz", "prefix foo bar baz", false},
		{"request line", "GET <_>", "GET <path>", `level=info msg="retry after GET /x"`, false},
		{"literal only", "foo", "", "prefix foo", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			line := []byte(tt.line)
			if tt.parser != "" {
				p, err := New(tt.parser)
				require.NoError(t, err)
				require.Equal(t, tt.want, p.Matches(line) != nil, "pattern parser %q on %q", tt.parser, tt.line)
			}
			f, err := ParseLineFilter([]byte(tt.filter))
			require.NoError(t, err)
			require.Equal(t, tt.want, f.Test(line), "line filter %q on %q", tt.filter, tt.line)
		})
	}
}
