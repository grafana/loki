package v3

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExtractFeatures_GoldenOutputs locks the exact byte-level output of the v3
// extraction algorithm for a representative set of inputs at n=6 (production
// value).
//
// DO NOT update the expected values in this test. If this test fails, the v3
// algorithm has been accidentally modified — v3 is frozen because v3 indexes
// already exist on disk and were built under this algorithm.
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
				"v3 golden output changed — do NOT update expected values; v3 is frozen")
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
