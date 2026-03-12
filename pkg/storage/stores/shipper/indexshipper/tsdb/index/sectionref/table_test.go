package sectionref

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
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

func TestSectionRefTableDecodeThenAddUsesLazyMaps(t *testing.T) {
	src := NewSectionRefTable(nil)
	src.Add(SectionRef{Path: "s3://bucket/a", SectionID: 1})
	src.Add(SectionRef{Path: "s3://bucket/b", SectionID: 2})

	data, err := src.Encode()
	require.NoError(t, err)

	decoded, err := Decode(data)
	require.NoError(t, err)
	require.NotNil(t, decoded)

	existing := SectionRef{Path: "s3://bucket/a", SectionID: 1}
	newRef := SectionRef{Path: "s3://bucket/a", SectionID: 3}

	require.Equal(t, uint32(0), decoded.Add(existing))
	require.Equal(t, uint32(2), decoded.Add(newRef))
	require.Equal(t, 3, decoded.Len())
}

func TestMmapSectionRefTableFromBytes(t *testing.T) {
	tbl := NewSectionRefTable(nil)
	tbl.Add(SectionRef{Path: "s3://bucket/a", SectionID: 1})
	tbl.Add(SectionRef{Path: "s3://bucket/b", SectionID: 2})
	tbl.Add(SectionRef{Path: "s3://bucket/a", SectionID: 3})

	data, err := tbl.Encode()
	require.NoError(t, err)

	mmapTbl, err := NewMmapSectionRefTableFromBytes(data)
	require.NoError(t, err)
	defer mmapTbl.Close()

	require.Equal(t, tbl.Len(), mmapTbl.Len())
	for i := 0; i < tbl.Len(); i++ {
		want, ok := tbl.Lookup(uint32(i))
		require.True(t, ok)
		got, ok := mmapTbl.Lookup(uint32(i))
		require.True(t, ok)
		require.Equal(t, want, got)
	}

	_, ok := mmapTbl.Lookup(uint32(tbl.Len()))
	require.False(t, ok)
}

func TestMmapSectionRefTableOpenFile(t *testing.T) {
	tbl := NewSectionRefTable(nil)
	tbl.Add(SectionRef{Path: "s3://bucket/obj1", SectionID: 10})
	tbl.Add(SectionRef{Path: "s3://bucket/obj2", SectionID: 20})

	data, err := tbl.Encode()
	require.NoError(t, err)

	path := t.TempDir() + "/test.sections"
	require.NoError(t, os.WriteFile(path, data, 0o644))

	mmapTbl, err := OpenMmap(path)
	require.NoError(t, err)

	require.Equal(t, tbl.Len(), mmapTbl.Len())
	for i := 0; i < tbl.Len(); i++ {
		want, ok := tbl.Lookup(uint32(i))
		require.True(t, ok)
		got, ok := mmapTbl.Lookup(uint32(i))
		require.True(t, ok)
		require.Equal(t, want, got)
	}

	require.NoError(t, mmapTbl.Close())
}

func TestMmapSectionRefTableEmptyTable(t *testing.T) {
	tbl := NewSectionRefTable(nil)
	data, err := tbl.Encode()
	require.NoError(t, err)

	mmapTbl, err := NewMmapSectionRefTableFromBytes(data)
	require.NoError(t, err)
	defer mmapTbl.Close()

	require.Equal(t, 0, mmapTbl.Len())
	_, ok := mmapTbl.Lookup(0)
	require.False(t, ok)
}

func BenchmarkSectionRefTableAddRepeatedPaths(b *testing.B) {
	refs := buildBenchmarkRefs(100_000, 64)

	b.Run("current_prod", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			tbl := NewSectionRefTable(nil)
			for _, ref := range refs {
				tbl.Add(ref)
			}
		}
	})

	b.Run("no_symbolization_original", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			tbl := newNoSymbolTable()
			for _, ref := range refs {
				tbl.Add(ref)
			}
		}
	})

	b.Run("map_string_string", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			tbl := newCanonicalMapTable()
			for _, ref := range refs {
				tbl.Add(ref)
			}
		}
	})
}

func BenchmarkSectionRefTableDecodeRepeatedPaths(b *testing.B) {
	refs := buildBenchmarkRefs(100_000, 20)
	tbl := NewSectionRefTable(refs)
	data, err := tbl.Encode()
	require.NoError(b, err)

	b.Run("current_prod", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			got, err := Decode(data)
			require.NoError(b, err)
			if got.Len() != len(refs) {
				b.Fatalf("decoded table length mismatch: got=%d want=%d", got.Len(), len(refs))
			}
		}
	})

	b.Run("no_symbolization_original", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			got, err := decodeNoSymbolization(data)
			require.NoError(b, err)
			if got.Len() != len(refs) {
				b.Fatalf("decoded table length mismatch: got=%d want=%d", got.Len(), len(refs))
			}
		}
	})

	b.Run("map_string_string", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			got, err := decodeWithCanonicalMap(data)
			require.NoError(b, err)
			if got.Len() != len(refs) {
				b.Fatalf("decoded table length mismatch: got=%d want=%d", got.Len(), len(refs))
			}
		}
	})
}

type noSymbolTable struct {
	refs  []SectionRef
	index map[SectionRef]uint32
}

func newNoSymbolTable() *noSymbolTable {
	return &noSymbolTable{
		refs:  make([]SectionRef, 0),
		index: make(map[SectionRef]uint32),
	}
}

func (t *noSymbolTable) Add(ref SectionRef) uint32 {
	if idx, ok := t.index[ref]; ok {
		return idx
	}
	idx := uint32(len(t.refs))
	t.refs = append(t.refs, ref)
	t.index[ref] = idx
	return idx
}

func (t *noSymbolTable) Len() int {
	return len(t.refs)
}

type canonicalMapTable struct {
	refs      []SectionRef
	index     map[SectionRef]uint32
	canonical map[string]string
}

func newCanonicalMapTable() *canonicalMapTable {
	return &canonicalMapTable{
		refs:      make([]SectionRef, 0),
		index:     make(map[SectionRef]uint32),
		canonical: make(map[string]string),
	}
}

func (t *canonicalMapTable) Add(ref SectionRef) uint32 {
	if path, ok := t.canonical[ref.Path]; ok {
		ref.Path = path
	} else {
		t.canonical[ref.Path] = ref.Path
	}

	if idx, ok := t.index[ref]; ok {
		return idx
	}

	idx := uint32(len(t.refs))
	t.refs = append(t.refs, ref)
	t.index[ref] = idx
	return idx
}

func (t *canonicalMapTable) Len() int {
	return len(t.refs)
}

func decodeNoSymbolization(data []byte) (*noSymbolTable, error) {
	r := bytes.NewReader(data)

	var pathCount uint32
	if err := binary.Read(r, binary.LittleEndian, &pathCount); err != nil {
		return nil, fmt.Errorf("reading path count: %w", err)
	}

	pathStrings := make([]string, pathCount)
	for i := range pathStrings {
		var slen uint16
		if err := binary.Read(r, binary.LittleEndian, &slen); err != nil {
			return nil, fmt.Errorf("reading path length: %w", err)
		}
		buf := make([]byte, slen)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("reading path: %w", err)
		}
		pathStrings[i] = string(buf)
	}

	var entryCount uint32
	if err := binary.Read(r, binary.LittleEndian, &entryCount); err != nil {
		return nil, fmt.Errorf("reading entry count: %w", err)
	}

	tbl := newNoSymbolTable()
	tbl.refs = make([]SectionRef, 0, entryCount)
	tbl.index = make(map[SectionRef]uint32, entryCount)

	for i := uint32(0); i < entryCount; i++ {
		var pIdx, secID uint32
		if err := binary.Read(r, binary.LittleEndian, &pIdx); err != nil {
			return nil, fmt.Errorf("reading path index: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &secID); err != nil {
			return nil, fmt.Errorf("reading section ID: %w", err)
		}
		if pIdx >= uint32(len(pathStrings)) {
			return nil, fmt.Errorf("path index %d out of range %d", pIdx, len(pathStrings))
		}
		if strconv.IntSize == 32 && secID > math.MaxInt32 {
			return nil, fmt.Errorf("section ID %d overflows int", secID)
		}

		tbl.Add(SectionRef{
			Path:      pathStrings[pIdx],
			SectionID: int(secID),
		})
	}

	if r.Len() != 0 {
		return nil, fmt.Errorf("unexpected %d trailing bytes", r.Len())
	}

	return tbl, nil
}

func decodeWithCanonicalMap(data []byte) (*canonicalMapTable, error) {
	r := bytes.NewReader(data)

	var pathCount uint32
	if err := binary.Read(r, binary.LittleEndian, &pathCount); err != nil {
		return nil, fmt.Errorf("reading path count: %w", err)
	}

	pathStrings := make([]string, pathCount)
	for i := range pathStrings {
		var slen uint16
		if err := binary.Read(r, binary.LittleEndian, &slen); err != nil {
			return nil, fmt.Errorf("reading path length: %w", err)
		}
		buf := make([]byte, slen)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("reading path: %w", err)
		}
		pathStrings[i] = string(buf)
	}

	var entryCount uint32
	if err := binary.Read(r, binary.LittleEndian, &entryCount); err != nil {
		return nil, fmt.Errorf("reading entry count: %w", err)
	}

	tbl := newCanonicalMapTable()
	tbl.refs = make([]SectionRef, 0, entryCount)
	tbl.index = make(map[SectionRef]uint32, entryCount)
	tbl.canonical = make(map[string]string, len(pathStrings))

	for i := uint32(0); i < entryCount; i++ {
		var pIdx, secID uint32
		if err := binary.Read(r, binary.LittleEndian, &pIdx); err != nil {
			return nil, fmt.Errorf("reading path index: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &secID); err != nil {
			return nil, fmt.Errorf("reading section ID: %w", err)
		}
		if pIdx >= uint32(len(pathStrings)) {
			return nil, fmt.Errorf("path index %d out of range %d", pIdx, len(pathStrings))
		}
		if strconv.IntSize == 32 && secID > math.MaxInt32 {
			return nil, fmt.Errorf("section ID %d overflows int", secID)
		}

		tbl.Add(SectionRef{
			Path:      pathStrings[pIdx],
			SectionID: int(secID),
		})
	}

	if r.Len() != 0 {
		return nil, fmt.Errorf("unexpected %d trailing bytes", r.Len())
	}

	return tbl, nil
}

func buildBenchmarkRefs(entryCount int, uniquePaths int) []SectionRef {
	refs := make([]SectionRef, 0, entryCount)
	paths := make([]string, uniquePaths)
	for i := range uniquePaths {
		paths[i] = fmt.Sprintf("s3://tenant-bucket/shared/path-%02d", i)
	}

	for i := range entryCount {
		refs = append(refs, SectionRef{
			Path:      paths[i%uniquePaths],
			SectionID: i % 1024,
		})
	}
	return refs
}
