package v3

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

// TestTermIterator_MultiBlock verifies that the lazy block-walking iterator correctly
// crosses a block boundary when a term dictionary spans more than TermDictBlockSize terms.
func TestTermIterator_MultiBlock(t *testing.T) {
	const totalTerms = TermDictBlockSize + 1 // forces exactly 2 blocks

	dir := t.TempDir()
	lastBlock0Term := term(fmt.Sprintf("%06d", TermDictBlockSize-1))
	firstBlock1Term := term(fmt.Sprintf("%06d", TermDictBlockSize))

	// Build sorted distinct 6-byte terms using fmt.Sprintf to produce
	// lexicographically ordered 6-byte ASCII terms. Range 000000–131072,
	// all distinct, lexicographic order matches numeric order.
	postings := make(map[[8]byte][]uint32, totalTerms)
	for i := range totalTerms {
		var t6 [8]byte
		s := fmt.Sprintf("%06d", i)
		copy(t6[:6], s)
		switch t6 {
		case lastBlock0Term:
			postings[t6] = []uint32{1}
		case firstBlock1Term:
			postings[t6] = []uint32{2}
		default:
			postings[t6] = []uint32{0}
		}
	}

	docs := []format.DocumentMetadata{
		{ID: 0, MinTimeUnix: 1, MaxTimeUnix: 2},
		{ID: 1, MinTimeUnix: 3, MaxTimeUnix: 4},
		{ID: 2, MinTimeUnix: 5, MaxTimeUnix: 6},
	}
	path := writeTestIndex(t, dir, "multiblock.idx", docs, postings)

	r, err := OpenIndexFile(path)
	require.NoError(t, err)
	defer r.Close()

	it, err := r.NewTermIterator()
	require.NoError(t, err)

	count := 0
	var prev [8]byte
	var lastBlock0Docs []uint32
	var firstBlock1Docs []uint32
	for it.Next() {
		cur := it.Term()
		if count > 0 {
			require.True(t, compareTerm8(cur, prev) > 0, "terms must be in ascending order at position %d", count)
		}
		require.False(t, it.Bitmap().MatchesAll)
		switch cur {
		case lastBlock0Term:
			lastBlock0Docs = append([]uint32(nil), it.Bitmap().Roaring.ToArray()...)
		case firstBlock1Term:
			firstBlock1Docs = append([]uint32(nil), it.Bitmap().Roaring.ToArray()...)
		}
		prev = cur
		count++
	}
	require.NoError(t, it.Err())
	require.Equal(t, totalTerms, count, "must visit all terms across block boundary")
	require.Equal(t, []uint32{1}, lastBlock0Docs)
	require.Equal(t, []uint32{2}, firstBlock1Docs)
}
