package sectionref

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
)

var (
	ErrSectionRefPathTooLong     = fmt.Errorf("section reference path exceeds %d bytes", math.MaxUint16)
	ErrSectionRefSectionOutRange = fmt.Errorf("section reference section ID out of uint32 range")
)

// SectionRef identifies a singleseries ID from a section location in object storage.
type SectionRef struct {
	Path      string
	SectionID int
	SeriesID  int
}

type sectionRefEntry struct {
	pathID    uint32
	sectionID int
	seriesID  int
}

type sectionRefKey struct {
	pathID    uint32
	sectionID int
	seriesID  int
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
			seriesID:  ref.SeriesID,
		})
		t.index[sectionRefKey{
			pathID:    pathID,
			sectionID: ref.SectionID,
			seriesID:  ref.SeriesID,
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
		seriesID:  ref.SeriesID,
	}
	if idx, ok := t.index[key]; ok {
		return idx
	}

	idx := uint32(len(t.entries))
	t.entries = append(t.entries, sectionRefEntry{
		pathID:    pathID,
		sectionID: ref.SectionID,
		seriesID:  ref.SeriesID,
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
		SeriesID:  entry.seriesID,
	}, true
}

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
		if entry.seriesID < 0 || uint64(entry.seriesID) > math.MaxUint32 {
			return nil, ErrSectionRefSectionOutRange
		}
		if err := binary.Write(&buf, binary.LittleEndian, uint32(entry.seriesID)); err != nil {
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

		t.entries[i] = sectionRefEntry{
			pathID:    pIdx,
			sectionID: int(secID),
			seriesID:  int(seriesID),
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
			seriesID:  entry.seriesID,
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
