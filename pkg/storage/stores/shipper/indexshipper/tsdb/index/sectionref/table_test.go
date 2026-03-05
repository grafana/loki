package sectionref

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSectionRefTableAddAndLookup(t *testing.T) {
	tbl := NewSectionRefTable(nil)

	a := SectionRef{Path: "path-a", SectionID: 7, SeriesID: 1}
	b := SectionRef{Path: "path-b", SectionID: 9, SeriesID: 2}

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
	tbl.Add(SectionRef{Path: "s3://bucket/a", SectionID: 1, SeriesID: 4})
	tbl.Add(SectionRef{Path: "s3://bucket/b", SectionID: 2, SeriesID: 5})
	tbl.Add(SectionRef{Path: "s3://bucket/a", SectionID: 1, SeriesID: 4}) // dedupe

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
	tbl.Add(SectionRef{Path: strings.Repeat("a", 1<<16), SectionID: 1, SeriesID: 2})

	_, err := tbl.Encode()
	require.ErrorIs(t, err, ErrSectionRefPathTooLong)
}

func TestMmapSectionRefTable_FromBytes(t *testing.T) {
	tbl := NewSectionRefTable(nil)
	tbl.Add(SectionRef{Path: "objects/ab/obj001", SectionID: 0, SeriesID: 1})
	tbl.Add(SectionRef{Path: "objects/ab/obj001", SectionID: 1, SeriesID: 2})
	tbl.Add(SectionRef{Path: "objects/cd/obj002", SectionID: 0, SeriesID: 5})
	tbl.Add(SectionRef{Path: "objects/ab/obj001", SectionID: 0, SeriesID: 1}) // dedupe

	data, err := tbl.Encode()
	require.NoError(t, err)

	mmaped, err := NewMmapSectionRefTableFromBytes(data)
	require.NoError(t, err)
	defer mmaped.Close()

	require.Equal(t, tbl.Len(), mmaped.Len())

	for i := 0; i < tbl.Len(); i++ {
		want, ok := tbl.Lookup(uint32(i))
		require.True(t, ok)
		got, ok := mmaped.Lookup(uint32(i))
		require.True(t, ok)
		require.Equal(t, want, got, "mismatch at index %d", i)
	}

	_, ok := mmaped.Lookup(uint32(tbl.Len()))
	require.False(t, ok, "out-of-bounds lookup should return false")
}

func TestMmapSectionRefTable_OpenMmap(t *testing.T) {
	tbl := NewSectionRefTable(nil)
	tbl.Add(SectionRef{Path: "s3://bucket/a", SectionID: 1, SeriesID: 4})
	tbl.Add(SectionRef{Path: "s3://bucket/b", SectionID: 2, SeriesID: 5})

	data, err := tbl.Encode()
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "test.sections")
	require.NoError(t, os.WriteFile(path, data, 0o644))

	mmaped, err := OpenMmap(path)
	require.NoError(t, err)
	defer mmaped.Close()

	require.Equal(t, tbl.Len(), mmaped.Len())

	for i := 0; i < tbl.Len(); i++ {
		want, ok := tbl.Lookup(uint32(i))
		require.True(t, ok)
		got, ok := mmaped.Lookup(uint32(i))
		require.True(t, ok)
		require.Equal(t, want, got)
	}
}

func BenchmarkLookup_Decode(b *testing.B) {
	tbl := buildBenchTable(b)
	data, err := tbl.Encode()
	require.NoError(b, err)

	decoded, err := Decode(data)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decoded.Lookup(uint32(i % decoded.Len()))
	}
}

func BenchmarkLookup_Mmap(b *testing.B) {
	tbl := buildBenchTable(b)
	data, err := tbl.Encode()
	require.NoError(b, err)

	mmaped, err := NewMmapSectionRefTableFromBytes(data)
	require.NoError(b, err)
	defer mmaped.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mmaped.Lookup(uint32(i % mmaped.Len()))
	}
}

func buildBenchTable(tb testing.TB) *SectionRefTable {
	tb.Helper()
	tbl := NewSectionRefTable(nil)
	for i := 0; i < 10000; i++ {
		tbl.Add(SectionRef{
			Path:      "objects/ab/obj001",
			SectionID: i % 50,
			SeriesID:  i,
		})
	}
	return tbl
}
