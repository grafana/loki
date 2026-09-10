package hintprovider

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractQueryNgrams_KnownVersion(t *testing.T) {
	result, err := ExtractQueryNgrams("hello world", 3, "v3")
	require.NoError(t, err)
	require.NotEmpty(t, result)
	// Result must be sorted.
	for i := 1; i < len(result); i++ {
		require.LessOrEqual(t, result[i-1], result[i], "result must be sorted at index %d", i)
	}
	// Result must be unique.
	seen := make(map[string]struct{}, len(result))
	for _, term := range result {
		_, dup := seen[term]
		require.False(t, dup, "duplicate term %q in result", term)
		seen[term] = struct{}{}
	}
}

func TestExtractQueryNgrams_UnknownVersionReturnsError(t *testing.T) {
	result, err := ExtractQueryNgrams("hello world", 3, "v99")
	require.Error(t, err)
	require.Nil(t, result)
}

func TestExtractQueryNgrams_TooShortReturnsNil(t *testing.T) {
	// "ab" is shorter than ngramLength=6, so no ngrams can be produced.
	result, err := ExtractQueryNgrams("ab", 6, "v3")
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestOrderUncorrelated_EmptyAndSingle(t *testing.T) {
	require.Nil(t, orderUncorrelated(nil))
	require.Equal(t, []string{"ABCDEF"}, orderUncorrelated([]string{"ABCDEF"}))
}

func TestOrderUncorrelated_EvenLength(t *testing.T) {
	in := []string{"a", "b", "c", "d", "e", "f"}
	got := orderUncorrelated(in)
	require.Equal(t, []string{"a", "d", "b", "e", "c", "f"}, got)
}

func TestOrderUncorrelated_OddLength(t *testing.T) {
	in := []string{"a", "b", "c", "d", "e", "f", "g"}
	got := orderUncorrelated(in)
	require.Equal(t, []string{"a", "d", "b", "e", "c", "f", "g"}, got)
}

func TestOrderUncorrelated_PreservesInputAndElements(t *testing.T) {
	in := []string{"t0", "t1", "t2", "t3", "t4"}
	inCopy := slices.Clone(in)

	got := orderUncorrelated(in)
	require.Equal(t, inCopy, in, "input slice should not be modified")

	sortedIn := slices.Clone(in)
	sortedGot := slices.Clone(got)
	slices.Sort(sortedIn)
	slices.Sort(sortedGot)
	require.Equal(t, sortedIn, sortedGot, "ordered output should be a permutation of input")
}
