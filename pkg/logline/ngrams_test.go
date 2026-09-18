package logline

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExtractorForVersion_V4NumericRule verifies that the v4 extractor is wired
// and differs from v3 in the intended way: an all-digit 6-gram is not indexed, a
// 9-digit run yields a numeric term, and ordinary text is untouched.
func TestExtractorForVersion_V4NumericRule(t *testing.T) {
	v3fn, err := ExtractorForVersion("v3")
	require.NoError(t, err)
	v4fn, err := ExtractorForVersion("v4")
	require.NoError(t, err)

	// A short numeric needle is indexed by v3 and produces nothing under v4,
	// which is what makes the query fall through to Loki instead of narrowing on
	// a saturated term.
	require.NotEmpty(t, v3fn(6, "471853", nil, nil, nil))
	require.Empty(t, v4fn(6, "471853", nil, nil, nil))

	// A 9-digit needle is served by v4.
	require.NotEmpty(t, v4fn(6, "385634291", nil, nil, nil))

	// Ordinary text is identical.
	require.Equal(t, v3fn(6, "abcdefgh", nil, nil, nil), v4fn(6, "abcdefgh", nil, nil, nil))
}

// TestExtractorForVersion_AllVersions verifies that the shim returns a
// working extractor for every supported version.
func TestExtractorForVersion_AllVersions(t *testing.T) {
	for _, v := range AllVersions() {
		t.Run(v, func(t *testing.T) {
			fn, err := ExtractorForVersion(v)
			require.NoError(t, err)
			require.NotNil(t, fn)

			out := fn(6, "abcdefg", nil, nil, nil)
			require.NotEmpty(t, out, "extractor must produce ngrams for non-empty input")
		})
	}
}

func TestExtractorForVersion_UnknownVersionReturnsError(t *testing.T) {
	fn, err := ExtractorForVersion("v99")
	require.Error(t, err)
	require.Nil(t, fn)
	require.Contains(t, err.Error(), "v99")
}
