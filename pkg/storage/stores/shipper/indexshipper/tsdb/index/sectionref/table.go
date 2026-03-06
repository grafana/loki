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

// SectionRefTable stores section references by index.
// It supports insert-or-dedup via Add and lookup by index via Lookup.
type SectionRefTable struct {
	refs        []SectionRef
	index       map[SectionRef]uint32
	pathSymbols map[string]string
}

func NewSectionRefTable(refs []SectionRef) *SectionRefTable {
	t := &SectionRefTable{
		refs:        make([]SectionRef, 0, len(refs)),
		index:       make(map[SectionRef]uint32, len(refs)),
		pathSymbols: make(map[string]string, len(refs)),
	}
	for _, ref := range refs {
		ref.Path = t.symbolizePath(ref.Path)
		idx := uint32(len(t.refs))
		t.refs = append(t.refs, ref)
		t.index[ref] = idx
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
	ref.Path = t.symbolizePath(ref.Path)
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

func Decode(data []byte) (*SectionRefTable, error) {
	r := bytes.NewReader(data)

	var pathCount uint32
	if err := binary.Read(r, binary.LittleEndian, &pathCount); err != nil {
		return nil, fmt.Errorf("reading path count: %w", err)
	}

	pathSymbols := make(map[string]string, pathCount)
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
		path := string(buf)
		if canonical, ok := pathSymbols[path]; ok {
			pathStrings[i] = canonical
		} else {
			pathSymbols[path] = path
			pathStrings[i] = path
		}
	}

	var entryCount uint32
	if err := binary.Read(r, binary.LittleEndian, &entryCount); err != nil {
		return nil, fmt.Errorf("reading entry count: %w", err)
	}

	t := &SectionRefTable{
		refs:        make([]SectionRef, entryCount),
		index:       make(map[SectionRef]uint32, entryCount),
		pathSymbols: pathSymbols,
	}
	for i := range t.refs {
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

		ref := SectionRef{
			Path:      pathStrings[pIdx],
			SectionID: int(secID),
			SeriesID:  int(seriesID),
		}
		t.refs[i] = ref
		t.index[ref] = uint32(i)
	}

	if r.Len() != 0 {
		return nil, fmt.Errorf("unexpected %d trailing bytes", r.Len())
	}

	return t, nil
}

func (t *SectionRefTable) symbolizePath(path string) string {
	if canonical, ok := t.pathSymbols[path]; ok {
		return canonical
	}
	t.pathSymbols[path] = path
	return path
}
