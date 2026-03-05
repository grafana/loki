package sectionref

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
	"unsafe"

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
// memory-mapped file. Path strings are never copied to the Go heap; Lookup
// returns strings that point directly into the mmap'd region via unsafe.String.
// These strings are only valid while the table is open — callers must not
// retain them past Close(). This follows the same pattern as TSDB's yoloString
// for symbol table lookups.
type MmapSectionRefTable struct {
	file        *fileutil.MmapFile
	pathOffsets []uint32 // byte offset of each path's [u16 len] header in data
	pathCount   uint32
	data        []byte // raw mmap'd bytes
	entryStart  int    // byte offset where the fixed-size entries begin
	entryCount  uint32
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

	pathOffsets := make([]uint32, pathCount)
	for i := range pathOffsets {
		if off+2 > len(data) {
			return nil, fmt.Errorf("truncated path length at index %d", i)
		}
		pathOffsets[i] = uint32(off)
		slen := int(binary.LittleEndian.Uint16(data[off:]))
		off += 2
		if off+slen > len(data) {
			return nil, fmt.Errorf("truncated path string at index %d", i)
		}
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
		file:        file,
		pathOffsets: pathOffsets,
		pathCount:   pathCount,
		data:        data,
		entryStart:  off,
		entryCount:  entryCount,
	}, nil
}

// pathAt returns the path string at index idx by pointing directly into the
// mmap'd data. The returned string shares its backing memory with the mmap
// region and is only valid while the table is open.
func (t *MmapSectionRefTable) pathAt(idx uint32) string {
	off := int(t.pathOffsets[idx])
	slen := int(binary.LittleEndian.Uint16(t.data[off:]))
	return unsafe.String(&t.data[off+2], slen) // #nosec G103 -- valid for lifetime of mmap
}

func (t *MmapSectionRefTable) Lookup(idx uint32) (SectionRef, bool) {
	if idx >= t.entryCount {
		return SectionRef{}, false
	}
	off := t.entryStart + int(idx)*entrySize
	pIdx := binary.LittleEndian.Uint32(t.data[off:])
	secID := binary.LittleEndian.Uint32(t.data[off+4:])
	seriesID := binary.LittleEndian.Uint32(t.data[off+8:])
	if pIdx >= t.pathCount {
		return SectionRef{}, false
	}
	return SectionRef{
		Path:      t.pathAt(pIdx),
		SectionID: int(secID),
		SeriesID:  int(seriesID),
	}, true
}

func (t *MmapSectionRefTable) Len() int {
	return int(t.entryCount)
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
