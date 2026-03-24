package sectionref

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/prometheus/prometheus/tsdb/fileutil"
)

var (
	ErrSectionRefPathTooLong     = fmt.Errorf("section reference path exceeds %d bytes", math.MaxUint16)
	ErrSectionRefSectionOutRange = fmt.Errorf("section reference section ID out of uint32 range")
)

const entrySize = 12 // 3 x uint32

// SectionRefLookup is a read-only interface for looking up section references
// by index. Both SectionRefTable (heap) and MmapSectionRefTable (mmap) satisfy it.
type SectionRefLookup interface {
	Lookup(idx uint32) (SectionRef, bool)
	Len() int
	Close() error
}

// SectionRef identifies a singleseries ID from a section location in object storage.
type SectionRef struct {
	Path      string
	SectionID int
	SeriesID  int
}

// SectionRefTable stores section references by index.
// It supports insert-or-dedup via Add and lookup by index via Lookup.
type SectionRefTable struct {
	refs  []SectionRef
	index map[SectionRef]uint32
}

func NewSectionRefTable(refs []SectionRef) *SectionRefTable {
	t := &SectionRefTable{
		refs:  refs,
		index: make(map[SectionRef]uint32, len(refs)),
	}
	for i, ref := range refs {
		t.index[ref] = uint32(i)
	}
	return t
}

func (t *SectionRefTable) Len() int {
	if t == nil {
		return 0
	}
	return len(t.refs)
}

func (t *SectionRefTable) Add(ref SectionRef) uint32 {
	if idx, ok := t.index[ref]; ok {
		return idx
	}

	idx := uint32(len(t.refs))
	t.refs = append(t.refs, ref)
	t.index[ref] = idx
	return idx
}

func (t *SectionRefTable) Lookup(idx uint32) (SectionRef, bool) {
	if t == nil || idx >= uint32(len(t.refs)) {
		return SectionRef{}, false
	}
	return t.refs[idx], true
}

func (t *SectionRefTable) Close() error { return nil }

// Encode serializes the table into a compact binary format with an interned
// string table for paths.
func (t *SectionRefTable) Encode() ([]byte, error) {
	pathIdx := make(map[string]uint32)
	pathStrings := make([]string, 0, len(t.refs))
	for _, ref := range t.refs {
		if _, ok := pathIdx[ref.Path]; !ok {
			pathIdx[ref.Path] = uint32(len(pathStrings))
			pathStrings = append(pathStrings, ref.Path)
		}
	}

	var buf bytes.Buffer

	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(pathStrings))); err != nil {
		return nil, err
	}
	for _, s := range pathStrings {
		if len(s) > math.MaxUint16 {
			return nil, ErrSectionRefPathTooLong
		}
		if err := binary.Write(&buf, binary.LittleEndian, uint16(len(s))); err != nil {
			return nil, err
		}
		if _, err := buf.WriteString(s); err != nil {
			return nil, err
		}
	}

	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(t.refs))); err != nil {
		return nil, err
	}
	for _, ref := range t.refs {
		if ref.SectionID < 0 || uint64(ref.SectionID) > math.MaxUint32 {
			return nil, ErrSectionRefSectionOutRange
		}
		if err := binary.Write(&buf, binary.LittleEndian, pathIdx[ref.Path]); err != nil {
			return nil, err
		}
		if err := binary.Write(&buf, binary.LittleEndian, uint32(ref.SectionID)); err != nil {
			return nil, err
		}
		if err := binary.Write(&buf, binary.LittleEndian, uint32(ref.SeriesID)); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

// MmapSectionRefTable is a read-only section ref table backed by a
// memory-mapped file. Path strings are eagerly copied to the Go heap during
// init so that Lookup only touches the fixed-size entries region of the mmap,
// avoiding page faults on the string table at the start of the file.
type MmapSectionRefTable struct {
	file       *fileutil.MmapFile
	paths      []string // heap-cached path strings, indexed by path table ordinal
	data       []byte   // raw mmap'd bytes
	entryStart int      // byte offset where the fixed-size entries begin
	entryCount uint32
}

// OpenMmap opens a .sections file and memory-maps it. The path string table
// is parsed eagerly (it is small); entries are read lazily on Lookup.
func OpenMmap(path string) (*MmapSectionRefTable, error) {
	f, err := fileutil.OpenMmapFile(path)
	if err != nil {
		return nil, fmt.Errorf("mmap section ref table %s: %w", path, err)
	}

	t, err := newMmapTable(f.Bytes(), f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return t, nil
}

// NewMmapSectionRefTableFromBytes creates a read-only table from a raw byte
// slice with no underlying file. Useful for testing.
func NewMmapSectionRefTableFromBytes(data []byte) (*MmapSectionRefTable, error) {
	return newMmapTable(data, nil)
}

func newMmapTable(data []byte, file *fileutil.MmapFile) (*MmapSectionRefTable, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("section ref table too short: %d bytes", len(data))
	}

	off := 0

	pathCount := binary.LittleEndian.Uint32(data[off:])
	off += 4

	// Eagerly copy path strings to heap so Lookup never touches the
	// string table region of the mmap.
	paths := make([]string, pathCount)
	for i := range paths {
		if off+2 > len(data) {
			return nil, fmt.Errorf("truncated path length at index %d", i)
		}
		slen := int(binary.LittleEndian.Uint16(data[off:]))
		off += 2
		if off+slen > len(data) {
			return nil, fmt.Errorf("truncated path string at index %d", i)
		}
		paths[i] = string(data[off : off+slen])
		off += slen
	}

	if off+4 > len(data) {
		return nil, fmt.Errorf("truncated entry count")
	}
	entryCount := binary.LittleEndian.Uint32(data[off:])
	off += 4

	expectedEnd := off + int(entryCount)*entrySize
	if expectedEnd > len(data) {
		return nil, fmt.Errorf("truncated entries: need %d bytes, have %d", expectedEnd, len(data))
	}
	if expectedEnd < len(data) {
		return nil, fmt.Errorf("unexpected %d trailing bytes", len(data)-expectedEnd)
	}

	return &MmapSectionRefTable{
		file:       file,
		paths:      paths,
		data:       data,
		entryStart: off,
		entryCount: entryCount,
	}, nil
}

func (t *MmapSectionRefTable) Lookup(idx uint32) (SectionRef, bool) {
	if idx >= t.entryCount {
		return SectionRef{}, false
	}
	off := t.entryStart + int(idx)*entrySize
	pIdx := binary.LittleEndian.Uint32(t.data[off:])
	secID := binary.LittleEndian.Uint32(t.data[off+4:])
	seriesID := binary.LittleEndian.Uint32(t.data[off+8:])
	if pIdx >= uint32(len(t.paths)) {
		return SectionRef{}, false
	}
	return SectionRef{
		Path:      t.paths[pIdx],
		SectionID: int(secID),
		SeriesID:  int(seriesID),
	}, true
}

func (t *MmapSectionRefTable) Len() int {
	return int(t.entryCount)
}

// MmapBytes returns the raw mmap'd byte slice backing the table. Callers can
// use this to issue madvise hints (e.g. MADV_WILLNEED) for prefetching.
func (t *MmapSectionRefTable) MmapBytes() []byte {
	return t.data
}

func (t *MmapSectionRefTable) Close() error {
	if t.file != nil {
		return t.file.Close()
	}
	return nil
}

var (
	_ SectionRefLookup = (*SectionRefTable)(nil)
	_ SectionRefLookup = (*MmapSectionRefTable)(nil)
)

func Decode(data []byte) (*SectionRefTable, error) {
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

	refs := make([]SectionRef, entryCount)
	for i := range refs {
		var pIdx, secID, seriesID uint32
		if err := binary.Read(r, binary.LittleEndian, &pIdx); err != nil {
			return nil, fmt.Errorf("reading path index: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &secID); err != nil {
			return nil, fmt.Errorf("reading section ID: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &seriesID); err != nil {
			return nil, fmt.Errorf("reading series ID: %w", err)
		}
		if pIdx >= uint32(len(pathStrings)) {
			return nil, fmt.Errorf("path index %d out of range %d", pIdx, len(pathStrings))
		}
		if strconv.IntSize == 32 && secID > math.MaxInt32 {
			return nil, fmt.Errorf("section ID %d overflows int", secID)
		}
		if strconv.IntSize == 32 && seriesID > math.MaxInt32 {
			return nil, fmt.Errorf("series ID %d overflows int", seriesID)
		}

		refs[i] = SectionRef{
			Path:      pathStrings[pIdx],
			SectionID: int(secID),
			SeriesID:  int(seriesID),
		}
	}

	if r.Len() != 0 {
		return nil, fmt.Errorf("unexpected %d trailing bytes", r.Len())
	}

	return NewSectionRefTable(refs), nil
}
