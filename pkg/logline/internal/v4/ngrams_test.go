package v4

import (
	"testing"

	"github.com/stretchr/testify/require"

	v3 "github.com/grafana/loki/v3/pkg/logline/internal/v3"
)

// TestExtractFeatures_GoldenOutputs locks the exact byte-level output of the v4
// extraction algorithm for a representative set of inputs at n=6 (production
// value).
//
// The expected values are identical to v3's golden test, because v4 currently
// clones v3. DO NOT update them to match a changed algorithm: v4 is frozen once
// v4 indexes exist on disk. Change the emitted set only by adding a v5.
func TestExtractFeatures_GoldenOutputs(t *testing.T) {
	tests := []struct {
		name     string
		n        int
		text     string
		expected []string
	}{
		{
			name:     "simple word n=6",
			n:        6,
			text:     "abcdefgh",
			expected: []string{"ABCDEF", "BCDEFG", "CDEFGH"},
		},
		{
			name:     "punctuation normalization n=6",
			n:        6,
			text:     "a_b-c/d:e@f%g?h.i",
			expected: []string{"A.B.C.", ".B.C.D", "B.C.D.", ".C.D.E", "C.D.E.", ".D.E.F", "D.E.F.", ".E.F.G", "E.F.G.", ".F.G.H", "F.G.H.", ".G.H.I"},
		},
		{
			name:     "separator splitting n=6",
			n:        6,
			text:     "abcdefg,hijklmn",
			expected: []string{"ABCDEF", "BCDEFG", "HIJKLM", "IJKLMN"},
		},
		{
			name:     "space is not a separator n=6",
			n:        6,
			text:     "foo bar",
			expected: []string{"FOO BA", "OO BAR"},
		},
		{
			name:     "path with slashes n=6",
			n:        6,
			text:     "/api/v1/users",
			expected: []string{".API.V", "API.V1", "PI.V1.", "I.V1.U", ".V1.US", "V1.USE", "1.USER", ".USERS"},
		},
		{
			name:     "short tokens skipped n=6",
			n:        6,
			text:     "abcde,fghijk,xy",
			expected: []string{"FGHIJK"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractFeatures(tt.n, tt.text, nil, nil, nil)
			out := make([]string, len(got))
			for i, key := range got {
				out[i] = string(key[:tt.n])
			}
			require.Equal(t, tt.expected, out,
				"v4 golden output changed — do NOT update expected values; v4 is frozen")
		})
	}
}

func TestExtractFeatures_IncludesLabelValues(t *testing.T) {
	got := ExtractFeatures(6, "", nil, []string{"labelvalue"}, nil)
	require.NotEmpty(t, got)

	var found bool
	for _, key := range got {
		if string(key[:6]) == "LABELV" {
			found = true
			break
		}
	}
	require.True(t, found, "expected ngrams from label values")
}

// TestExtractFeatures_IdenticalToV3 asserts that v4 is, for now, a byte-for-byte
// clone of v3. The fork exists so extraction can diverge later without
// invalidating v3 indexes already on disk; until it does, any difference here is
// an accident.
//
// When v4 intentionally diverges, replace this with a test that pins the
// intended difference.
func TestExtractFeatures_IdenticalToV3(t *testing.T) {
	inputs := []string{
		"abcdefgh",
		"a_b-c/d:e@f%g?h.i",
		"abcdefg,hijklmn",
		"foo bar",
		"/api/v1/users",
		"abcde,fghijk,xy",
		"level=info ts=2026-08-12T12:39:34.933471853Z caller=foo.go:12 msg=\"hello world\"",
		"tenant=836243 id=1779629282473030489 dur=1.234567ms",
	}
	for _, in := range inputs {
		for n := 1; n <= 6; n++ {
			require.Equal(t,
				v3.ExtractFeatures(n, in, nil, nil, nil),
				ExtractFeatures(n, in, nil, nil, nil),
				"v4 must clone v3 exactly (n=%d, input=%q)", n, in)
		}
	}
}
