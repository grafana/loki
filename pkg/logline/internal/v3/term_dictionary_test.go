package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTermDictionary_BuildAndQuery(t *testing.T) {
	ngrams := [][NgramLength]byte{
		{'A', 'A', 'A', 'A', 'A', 'A'},
		{'B', 'B', 'B', 'B', 'B', 'B'},
		{'C', 'C', 'C', 'C', 'C', 'C'},
	}

	dict := BuildTermDictionary(ngrams)

	assert.Equal(t, 3, dict.Count)
	assert.Greater(t, dict.CompressedSize(), 0)

	// Query existing terms
	pos0, err := dict.Query(ngrams[0])
	require.NoError(t, err)
	assert.Equal(t, 0, pos0)
	pos1, err := dict.Query(ngrams[1])
	require.NoError(t, err)
	assert.Equal(t, 1, pos1)
	pos2, err := dict.Query(ngrams[2])
	require.NoError(t, err)
	assert.Equal(t, 2, pos2)

	// Query non-existing term
	notFound := [NgramLength]byte{'Z', 'Z', 'Z', 'Z', 'Z', 'Z'}
	posNF, err := dict.Query(notFound)
	require.NoError(t, err)
	assert.Equal(t, -1, posNF)
}

func TestTermDictionary_BuildFromStrings(t *testing.T) {
	ngrams := []string{"AAAAAA", "BBBBBB", "CCCCCC"}

	dict := BuildTermDictionaryFromStrings(ngrams)

	assert.Equal(t, 3, dict.Count)
	pos0, err := dict.QueryString("AAAAAA")
	require.NoError(t, err)
	assert.Equal(t, 0, pos0)
	pos1, err := dict.QueryString("BBBBBB")
	require.NoError(t, err)
	assert.Equal(t, 1, pos1)
	pos2, err := dict.QueryString("CCCCCC")
	require.NoError(t, err)
	assert.Equal(t, 2, pos2)
}

func TestTermDictionary_Empty(t *testing.T) {
	dict := BuildTermDictionary(nil)

	assert.Equal(t, 0, dict.Count)
	assert.Equal(t, 0, dict.NumBlocks)

	notFound := [NgramLength]byte{'A', 'A', 'A', 'A', 'A', 'A'}
	posNF, err := dict.Query(notFound)
	require.NoError(t, err)
	assert.Equal(t, -1, posNF)
}

func TestTermDictionary_SingleTerm(t *testing.T) {
	ngrams := [][NgramLength]byte{
		{'T', 'E', 'S', 'T', '1', '2'},
	}

	dict := BuildTermDictionary(ngrams)

	assert.Equal(t, 1, dict.Count)
	assert.Equal(t, 1, dict.NumBlocks)
	pos, err := dict.Query(ngrams[0])
	require.NoError(t, err)
	assert.Equal(t, 0, pos)
}

func TestTermDictionary_LargeDataset(t *testing.T) {
	// Create a dataset to test query functionality
	// Note: Block size is now 131072 (128K), so 200 terms fit in 1 block
	ngrams := make([][NgramLength]byte, 200)
	for i := range 200 {
		ngrams[i] = [NgramLength]byte{
			byte('A' + (i / 26 / 26 % 26)),
			byte('A' + (i / 26 % 26)),
			byte('A' + (i % 26)),
			'0', '0', '0',
		}
	}

	dict := BuildTermDictionary(ngrams)

	assert.Equal(t, 200, dict.Count)
	assert.GreaterOrEqual(t, dict.NumBlocks, 1)

	// Query all terms
	for i, ngram := range ngrams {
		pos, err := dict.Query(ngram)
		require.NoError(t, err)
		assert.Equal(t, i, pos, "term %d not found at expected position", i)
	}
}

func TestTermDictionary_GetAllTerms(t *testing.T) {
	ngrams := [][NgramLength]byte{
		{'A', 'A', 'A', 'A', 'A', 'A'},
		{'B', 'B', 'B', 'B', 'B', 'B'},
		{'C', 'C', 'C', 'C', 'C', 'C'},
	}

	dict := BuildTermDictionary(ngrams)
	allTerms, err := dict.GetAllTerms()
	require.NoError(t, err)

	require.Len(t, allTerms, 3)
	assert.Equal(t, ngrams[0], allTerms[0])
	assert.Equal(t, ngrams[1], allTerms[1])
	assert.Equal(t, ngrams[2], allTerms[2])
}

func TestTermDictionary_Serialization(t *testing.T) {
	ngrams := [][NgramLength]byte{
		{'A', 'A', 'A', 'A', 'A', 'A'},
		{'B', 'B', 'B', 'B', 'B', 'B'},
		{'C', 'C', 'C', 'C', 'C', 'C'},
	}

	dict := BuildTermDictionary(ngrams)

	// Serialize
	data := dict.ToBytes()
	require.NotEmpty(t, data)

	// Deserialize
	restored, err := TermDictionaryFromBytes(data)
	require.NoError(t, err)

	assert.Equal(t, dict.Count, restored.Count)
	assert.Equal(t, dict.NumBlocks, restored.NumBlocks)

	// Query restored dictionary
	pos0, err := restored.Query(ngrams[0])
	require.NoError(t, err)
	assert.Equal(t, 0, pos0)
	pos1, err := restored.Query(ngrams[1])
	require.NoError(t, err)
	assert.Equal(t, 1, pos1)
	pos2, err := restored.Query(ngrams[2])
	require.NoError(t, err)
	assert.Equal(t, 2, pos2)
}

func TestTermDictionary_PrefixCompression(t *testing.T) {
	// Terms with common prefixes should compress well
	ngrams := [][NgramLength]byte{
		{'P', 'R', 'E', 'F', 'I', 'X'},
		{'P', 'R', 'E', 'F', 'I', 'Y'},
		{'P', 'R', 'E', 'F', 'I', 'Z'},
		{'P', 'R', 'E', 'F', 'J', 'A'},
		{'P', 'R', 'E', 'F', 'J', 'B'},
	}

	dict := BuildTermDictionary(ngrams)

	// All terms should be queryable
	for i, ngram := range ngrams {
		pos, err := dict.Query(ngram)
		require.NoError(t, err)
		assert.Equal(t, i, pos)
	}
}

func BenchmarkTermDictionary_Build(b *testing.B) {
	ngrams := make([][NgramLength]byte, 10000)
	for i := range 10000 {
		ngrams[i] = [NgramLength]byte{
			byte('A' + (i / 26 / 26 / 26 % 26)),
			byte('A' + (i / 26 / 26 % 26)),
			byte('A' + (i / 26 % 26)),
			byte('A' + (i % 26)),
			'0', '0',
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildTermDictionary(ngrams)
	}
}

func BenchmarkTermDictionary_Query(b *testing.B) {
	ngrams := make([][NgramLength]byte, 10000)
	for i := range 10000 {
		ngrams[i] = [NgramLength]byte{
			byte('A' + (i / 26 / 26 / 26 % 26)),
			byte('A' + (i / 26 / 26 % 26)),
			byte('A' + (i / 26 % 26)),
			byte('A' + (i % 26)),
			'0', '0',
		}
	}

	dict := BuildTermDictionary(ngrams)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dict.Query(ngrams[i%10000]) //nolint:errcheck
	}
}
