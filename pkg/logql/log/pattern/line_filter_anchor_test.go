package pattern

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLineFilterMatchesWholeLine describes how a |> or !> pattern should match
// a log line, and shows where Matcher.Test differs from that today.
//
// Expected: the pattern covers the whole line. A leading literal must be at the
// start of the line, and a trailing literal must be at the end of the line. To
// allow other text at either end, put <_> at that end of the pattern.
//
// Today: Test anchors a trailing literal at the end of the line, but it accepts
// a leading literal at any position in the line. The cases where today differs
// from want fail until Test anchors the leading literal.
//
// The pattern parser documentation says that a leading literal is anchored at
// the start of the line, and #12398 says that line filters use the same
// semantics as the pattern parser.
func TestLineFilterMatchesWholeLine(t *testing.T) {
	type testCase struct {
		name   string
		filter string
		line   string
		// today is the result of Test before the leading literal is anchored.
		today bool
		// want is the expected result.
		want bool
	}
	for _, tt := range []testCase{
		// Start of the line. Test does not anchor a leading literal, so these
		// cases fail today.
		{
			name:   "leading literal later in line",
			filter: "foo <_>",
			line:   "prefix foo bar",
			today:  true,
			want:   false,
		},
		{
			name:   "leading and trailing literals",
			filter: "foo <_> baz",
			line:   "prefix foo bar baz",
			today:  true,
			want:   false,
		},
		{
			name:   "request line",
			filter: "GET <_>",
			line:   `level=info msg="retry after GET /x"`,
			today:  true,
			want:   false,
		},
		{
			name:   "literal only, text before",
			filter: "foo",
			line:   "prefix foo",
			today:  true,
			want:   false,
		},

		// Start of the line, already correct today.
		{
			name:   "leading literal at start",
			filter: "foo <_>",
			line:   "foo bar",
			today:  true,
			want:   true,
		},
		{
			name:   "leading <_> allows text before",
			filter: "<_> foo <_>",
			line:   "prefix foo bar",
			today:  true,
			want:   true,
		},

		// End of the line. Test already anchors a trailing literal.
		{
			name:   "trailing literal at end",
			filter: "<_> bar",
			line:   "foo bar",
			today:  true,
			want:   true,
		},
		{
			name:   "trailing literal earlier in line",
			filter: "<_> bar",
			line:   "foo bar baz",
			today:  false,
			want:   false,
		},
		{
			name:   "literal only, text after",
			filter: "foo",
			line:   "foo suffix",
			today:  false,
			want:   false,
		},
		{
			name:   "literal only, exact line",
			filter: "foo",
			line:   "foo",
			today:  true,
			want:   true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, err := ParseLineFilter([]byte(tt.filter))
			require.NoError(t, err)
			require.Equal(t, tt.want, f.Test([]byte(tt.line)), "line filter %q on line %q: want %v, today %v", tt.filter, tt.line, tt.want, tt.today)
		})
	}
}
