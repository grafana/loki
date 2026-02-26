package index

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChunkRefTable_AddAndLookup(t *testing.T) {
	table := NewChunkRefTable()

	ref0 := SectionRef{Path: "objects/ab/cdef0123", SectionID: 0}
	ref1 := SectionRef{Path: "objects/ab/cdef0123", SectionID: 1}
	ref2 := SectionRef{Path: "objects/cd/ef456789", SectionID: 0}

	idx0 := table.Add(ref0)
	idx1 := table.Add(ref1)
	idx2 := table.Add(ref2)

	require.Equal(t, uint32(0), idx0)
	require.Equal(t, uint32(1), idx1)
	require.Equal(t, uint32(2), idx2)

	require.Equal(t, ref0, table.Lookup(idx0))
	require.Equal(t, ref1, table.Lookup(idx1))
	require.Equal(t, ref2, table.Lookup(idx2))
	require.Equal(t, 3, table.Len())
}

func TestChunkRefTable_AddDedup(t *testing.T) {
	table := NewChunkRefTable()

	ref := SectionRef{Path: "objects/ab/cdef0123", SectionID: 0}

	idx1 := table.Add(ref)
	idx2 := table.Add(ref)

	require.Equal(t, idx1, idx2, "duplicate Add should return the same index")
	require.Equal(t, 1, table.Len())
}

func TestChunkRefTable_EncodeDecodeRoundTrip(t *testing.T) {
	table := NewChunkRefTable()

	refs := []SectionRef{
		{Path: "objects/ab/cdef0123456789abcdef0123456789abcdef0123456789abcdef", SectionID: 0},
		{Path: "objects/ab/cdef0123456789abcdef0123456789abcdef0123456789abcdef", SectionID: 1},
		{Path: "objects/cd/ef456789abcdef0123456789abcdef0123456789abcdef012345", SectionID: 0},
		{Path: "objects/ef/01234567890abcdef0123456789abcdef0123456789abcdef0123", SectionID: 5},
	}

	for _, ref := range refs {
		table.Add(ref)
	}

	encoded := table.Encode()
	decoded, err := DecodeChunkRefTable(encoded)
	require.NoError(t, err)

	require.Equal(t, table.Len(), decoded.Len())
	for i := 0; i < table.Len(); i++ {
		require.Equal(t, table.Lookup(uint32(i)), decoded.Lookup(uint32(i)))
	}
}

func TestChunkRefTable_DecodeEmpty(t *testing.T) {
	table := NewChunkRefTable()
	encoded := table.Encode()
	decoded, err := DecodeChunkRefTable(encoded)
	require.NoError(t, err)
	require.Equal(t, 0, decoded.Len())
}

func TestChunkRefTable_DecodeInvalidData(t *testing.T) {
	_, err := DecodeChunkRefTable([]byte{0x01})
	require.Error(t, err)
}

func TestChunkRefTable_DecodeInvalidPathIndex(t *testing.T) {
	table := NewChunkRefTable()
	table.Add(SectionRef{Path: "objects/ab/cdef", SectionID: 0})
	encoded := table.Encode()

	// Corrupt the path index in the first entry: set it to 99 (out of range).
	// The entry block starts after: 4 (path_count) + 2+14 (one path string) + 4 (entry_count) = 24 bytes.
	// First 4 bytes of entry are the path index.
	encoded[24] = 99

	_, err := DecodeChunkRefTable(encoded)
	require.Error(t, err)
	require.Contains(t, err.Error(), "path index")
}
