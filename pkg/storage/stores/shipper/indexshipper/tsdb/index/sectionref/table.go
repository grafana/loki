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

const entrySize = 8 // pathID(4) + sectionID(4)

var (
	ErrSectionRefPathTooLong     = fmt.Errorf("section reference path exceeds %d bytes", math.MaxUint16)
	ErrSectionRefSectionOutRange = fmt.Errorf("section reference section ID out of uint32 range")
)

// SectionRefLookup is a read-only interface for looking up section references
// by index. Both SectionRefTable and MmapSectionRefTable implement it.
type SectionRefLookup interface {
	Lookup(idx uint32) (SectionRef, bool)
	Len() int
	Close() error
}

var (
	_ SectionRefLookup = (*SectionRefTable)(nil)
	_ SectionRefLookup = (*MmapSectionRefTable)(nil)
)

// SectionRef identifies a section location in object storage.
type SectionRef struct {
	Path      string
	SectionID int
}

type sectionRefEntry struct {
	pathID    uint32
	sectionID int
}

type sectionRefKey struct {
	pathID    uint32
	sectionID int
}

// SectionRefTable stores section references by index.
// It supports insert-or-dedup via Add and lookup by index via Lookup.
type SectionRefTable struct {
	paths   []string
	entries []sectionRefEntry

	// Built eagerly for mutable tables and lazily for decoded tables.
	index     map[sectionRefKey]uint32
	pathIndex map[string]uint32
}

func NewSectionRefTable(refs []SectionRef) *SectionRefTable {
	t := &SectionRefTable{
		paths:     make([]string, 0, len(refs)),
		entries:   make([]sectionRefEntry, 0, len(refs)),
		index:     make(map[sectionRefKey]uint32, len(refs)),
		pathIndex: make(map[string]uint32, len(refs)),
	}
	for i := range refs {
		ref := refs[i]
		pathID := t.internPath(ref.Path)
		t.entries = append(t.entries, sectionRefEntry{
			pathID:    pathID,
			sectionID: ref.SectionID,
		})
		t.index[sectionRefKey{
			pathID:    pathID,
			sectionID: ref.SectionID,
		}] = uint32(i)
	}
	return t
}

func (t *SectionRefTable) Len() int {
	if t == nil {
		return 0
	}
	return len(t.entries)
}

func (t *SectionRefTable) Add(ref SectionRef) uint32 {
	t.ensureMutableState()
	pathID := t.internPath(ref.Path)
	key := sectionRefKey{
		pathID:    pathID,
		sectionID: ref.SectionID,
	}
	if idx, ok := t.index[key]; ok {
		return idx
	}

	idx := uint32(len(t.entries))
	t.entries = append(t.entries, sectionRefEntry{
		pathID:    pathID,
		sectionID: ref.SectionID,
	})
	t.index[key] = idx
	return idx
}

func (t *SectionRefTable) Lookup(idx uint32) (SectionRef, bool) {
	if t == nil || idx >= uint32(len(t.entries)) {
		return SectionRef{}, false
	}
	entry := t.entries[idx]
	if entry.pathID >= uint32(len(t.paths)) {
		return SectionRef{}, false
	}

	return SectionRef{
		Path:      t.paths[entry.pathID],
		SectionID: entry.sectionID,
	}, true
}

// Close is a no-op for in-memory tables. It exists to satisfy SectionRefLookup.
func (t *SectionRefTable) Close() error { return nil }

// Encode serializes the table into a compact binary format with an interned
// string table for paths.
func (t *SectionRefTable) Encode() ([]byte, error) {
	var buf bytes.Buffer

	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(t.paths))); err != nil {
		return nil, err
	}
	for _, s := range t.paths {
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

	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(t.entries))); err != nil {
		return nil, err
	}
	for _, entry := range t.entries {
		if entry.sectionID < 0 || uint64(entry.sectionID) > math.MaxUint32 {
			return nil, ErrSectionRefSectionOutRange
		}
		if err := binary.Write(&buf, binary.LittleEndian, entry.pathID); err != nil {
			return nil, err
		}
		if err := binary.Write(&buf, binary.LittleEndian, uint32(entry.sectionID)); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

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

	t := &SectionRefTable{
		paths:   pathStrings,
		entries: make([]sectionRefEntry, entryCount),
	}
	for i := range t.entries {
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

		t.entries[i] = sectionRefEntry{
			pathID:    pIdx,
			sectionID: int(secID),
		}
	}

	if r.Len() != 0 {
		return nil, fmt.Errorf("unexpected %d trailing bytes", r.Len())
	}

	return t, nil
}

func (t *SectionRefTable) ensureMutableState() {
	if t.index != nil && t.pathIndex != nil {
		return
	}

	t.index = make(map[sectionRefKey]uint32, len(t.entries))
	t.pathIndex = make(map[string]uint32, len(t.paths))

	for i, p := range t.paths {
		if _, ok := t.pathIndex[p]; !ok {
			t.pathIndex[p] = uint32(i)
		}
	}
	for i, entry := range t.entries {
		t.index[sectionRefKey{
			pathID:    entry.pathID,
			sectionID: entry.sectionID,
		}] = uint32(i)
	}
}

func (t *SectionRefTable) internPath(path string) uint32 {
	if idx, ok := t.pathIndex[path]; ok {
		return idx
	}
	idx := uint32(len(t.paths))
	t.paths = append(t.paths, path)
	t.pathIndex[path] = idx
	return idx
}

// MmapSectionRefTable is a read-only, memory-mapped section-ref table.
// The path strings are copied onto the Go heap during open, but the entry
// data (the bulk of the file) stays in the OS page cache and is accessed
// directly from the mmap'd region.
type MmapSectionRefTable struct {
	paths      []string
	data       []byte // slice into the mmap'd region covering all entries
	entryCount uint32
	mmap       *fileutil.MmapFile // non-nil for file-backed; nil for from-bytes
}

// OpenMmap opens a section-ref sidecar file and returns a memory-mapped,
// read-only table. The caller must call Close when done.
func OpenMmap(path string) (*MmapSectionRefTable, error) {
	f, err := fileutil.OpenMmapFile(path)
	if err != nil {
		return nil, fmt.Errorf("mmap section-ref table %q: %w", path, err)
	}

	t, err := parseMmapData(f.Bytes())
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("parsing mmap'd section-ref table %q: %w", path, err)
	}
	t.mmap = f
	return t, nil
}

// NewMmapSectionRefTableFromBytes creates a read-only table backed by an
// existing byte slice (e.g. for testing). The caller must keep data alive
// for the lifetime of the returned table.
func NewMmapSectionRefTableFromBytes(data []byte) (*MmapSectionRefTable, error) {
	return parseMmapData(data)
}

func (t *MmapSectionRefTable) Lookup(idx uint32) (SectionRef, bool) {
	if idx >= t.entryCount {
		return SectionRef{}, false
	}
	off := int(idx) * entrySize
	pathID := binary.LittleEndian.Uint32(t.data[off:])
	sectionID := binary.LittleEndian.Uint32(t.data[off+4:])

	if pathID >= uint32(len(t.paths)) {
		return SectionRef{}, false
	}
	return SectionRef{
		Path:      t.paths[pathID],
		SectionID: int(sectionID),
	}, true
}

func (t *MmapSectionRefTable) Len() int {
	return int(t.entryCount)
}

// Close unmaps the underlying file if the table is file-backed.
func (t *MmapSectionRefTable) Close() error {
	if t.mmap != nil {
		return t.mmap.Close()
	}
	return nil
}

// parseMmapData parses the path table from the raw bytes and returns a table
// whose entry data slice points directly into the provided buffer.
func parseMmapData(data []byte) (*MmapSectionRefTable, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("data too short for path count")
	}
	pathCount := binary.LittleEndian.Uint32(data[:4])
	offset := 4

	paths := make([]string, pathCount)
	for i := uint32(0); i < pathCount; i++ {
		if offset+2 > len(data) {
			return nil, fmt.Errorf("data too short for path length at index %d", i)
		}
		slen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2
		end := offset + slen
		if end > len(data) {
			return nil, fmt.Errorf("data too short for path at index %d", i)
		}
		paths[i] = string(data[offset:end])
		offset = end
	}

	if offset+4 > len(data) {
		return nil, fmt.Errorf("data too short for entry count")
	}
	entryCount := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	expected := int(entryCount) * entrySize
	remaining := len(data) - offset
	if remaining != expected {
		return nil, fmt.Errorf("expected %d bytes for %d entries, got %d remaining", expected, entryCount, remaining)
	}

	return &MmapSectionRefTable{
		paths:      paths,
		data:       data[offset:],
		entryCount: entryCount,
	}, nil
}
