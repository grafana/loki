package logline

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExtractorForVersion_AllVersions verifies that the shim returns a
// working extractor for every supported version.
func TestExtractorForVersion_AllVersions(t *testing.T) {
	for _, v := range []string{"v3"} {
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
