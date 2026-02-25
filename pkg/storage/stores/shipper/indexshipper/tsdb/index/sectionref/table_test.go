package sectionref

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSectionRefTableAddAndLookup(t *testing.T) {
	tbl := NewSectionRefTable(nil)

	a := SectionRef{Path: "path-a", SectionID: 7}
	b := SectionRef{Path: "path-b", SectionID: 9}

	idxA1 := tbl.Add(a)
	idxB := tbl.Add(b)
	idxA2 := tbl.Add(a)

	require.Equal(t, uint32(0), idxA1)
	require.Equal(t, uint32(1), idxB)
	require.Equal(t, idxA1, idxA2)
	require.Equal(t, 2, tbl.Len())

	gotA, ok := tbl.Lookup(idxA1)
	require.True(t, ok)
	require.Equal(t, a, gotA)

	_, ok = tbl.Lookup(10)
	require.False(t, ok)
}

func TestSectionRefTableEncodeDecodeRoundTrip(t *testing.T) {
	tbl := NewSectionRefTable(nil)
	tbl.Add(SectionRef{Path: "s3://bucket/a", SectionID: 1})
	tbl.Add(SectionRef{Path: "s3://bucket/b", SectionID: 2})
	tbl.Add(SectionRef{Path: "s3://bucket/a", SectionID: 1}) // dedupe

	data, err := tbl.Encode()
	require.NoError(t, err)

	decoded, err := Decode(data)
	require.NoError(t, err)
	require.Equal(t, tbl.Len(), decoded.Len())

	for i := 0; i < tbl.Len(); i++ {
		want, ok := tbl.Lookup(uint32(i))
		require.True(t, ok)
		got, ok := decoded.Lookup(uint32(i))
		require.True(t, ok)
		require.Equal(t, want, got)
	}
}

func TestSectionRefTableEncodePathTooLong(t *testing.T) {
	tbl := NewSectionRefTable(nil)
	tbl.Add(SectionRef{Path: strings.Repeat("a", 1<<16), SectionID: 1})

	_, err := tbl.Encode()
	require.ErrorIs(t, err, ErrSectionRefPathTooLong)
}

func TestDecodeSectionRefTableBadPathIndex(t *testing.T) {
	var b bytes.Buffer

	// path_count = 1
	require.NoError(t, binary.Write(&b, binary.LittleEndian, uint32(1)))
	// path[0] = "x"
	require.NoError(t, binary.Write(&b, binary.LittleEndian, uint16(1)))
	_, err := b.WriteString("x")
	require.NoError(t, err)
	// entry_count = 1
	require.NoError(t, binary.Write(&b, binary.LittleEndian, uint32(1)))
	// entry path index = 2 (invalid), section = 0
	require.NoError(t, binary.Write(&b, binary.LittleEndian, uint32(2)))
	require.NoError(t, binary.Write(&b, binary.LittleEndian, uint32(0)))

	_, err = Decode(b.Bytes())
	require.Error(t, err)
	require.Contains(t, err.Error(), "path index")
}
