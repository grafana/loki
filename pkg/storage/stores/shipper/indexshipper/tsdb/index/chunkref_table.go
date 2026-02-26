package index

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// SectionRef identifies a data object and section that a ChunkMeta points to.
// Stored in the lookup table alongside the TSDB index.
type SectionRef struct {
	Path      string
	SectionID int
}

// ChunkRefTable maps uint32 indices (stored in ChunkMeta.Checksum) to
// SectionRef values. It is serialized as a sidecar .lookup file alongside a
// .tsdb file.
type ChunkRefTable struct {
	refs  []SectionRef
	dedup map[SectionRef]uint32
}

// NewChunkRefTable returns an empty ChunkRefTable ready for Add calls.
func NewChunkRefTable() *ChunkRefTable {
	return &ChunkRefTable{
		dedup: make(map[SectionRef]uint32),
	}
}

// Add inserts a SectionRef into the table, deduplicating identical entries.
// It returns the uint32 index that should be stored in ChunkMeta.Checksum.
func (t *ChunkRefTable) Add(ref SectionRef) uint32 {
	if idx, ok := t.dedup[ref]; ok {
		return idx
	}
	idx := uint32(len(t.refs))
	t.refs = append(t.refs, ref)
	t.dedup[ref] = idx
	return idx
}

// Lookup resolves a ChunkMeta.Checksum value to its SectionRef.
func (t *ChunkRefTable) Lookup(idx uint32) SectionRef {
	return t.refs[idx]
}

// Len returns the number of entries in the table.
func (t *ChunkRefTable) Len() int {
	return len(t.refs)
}

// Encode serializes the table into a compact binary format with an interned
// string table for object paths.
//
// Wire format:
//
//	[uint32 path_count]
//	for each path:
//	  [uint16 len][path bytes]
//	[uint32 entry_count]
//	for each entry:
//	  [uint32 path_string_index][uint32 section_id]
func (t *ChunkRefTable) Encode() []byte {
	pathIdx := make(map[string]uint32, len(t.refs))
	var pathStrings []string
	for _, ref := range t.refs {
		if _, ok := pathIdx[ref.Path]; !ok {
			pathIdx[ref.Path] = uint32(len(pathStrings))
			pathStrings = append(pathStrings, ref.Path)
		}
	}

	var buf bytes.Buffer

	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(pathStrings)))
	for _, s := range pathStrings {
		_ = binary.Write(&buf, binary.LittleEndian, uint16(len(s)))
		buf.WriteString(s)
	}

	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(t.refs)))
	for _, ref := range t.refs {
		_ = binary.Write(&buf, binary.LittleEndian, pathIdx[ref.Path])
		_ = binary.Write(&buf, binary.LittleEndian, uint32(ref.SectionID))
	}

	return buf.Bytes()
}

// DecodeChunkRefTable deserializes a ChunkRefTable from bytes produced by
// Encode. The resulting table supports Lookup but not further Add calls (the
// dedup map is not populated).
func DecodeChunkRefTable(data []byte) (*ChunkRefTable, error) {
	r := bytes.NewReader(data)

	var pathCount uint32
	if err := binary.Read(r, binary.LittleEndian, &pathCount); err != nil {
		return nil, fmt.Errorf("reading path count: %w", err)
	}
	pathStrings := make([]string, pathCount)
	for i := range pathStrings {
		var slen uint16
		if err := binary.Read(r, binary.LittleEndian, &slen); err != nil {
			return nil, fmt.Errorf("reading path string length: %w", err)
		}
		buf := make([]byte, slen)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("reading path string: %w", err)
		}
		pathStrings[i] = string(buf)
	}

	var entryCount uint32
	if err := binary.Read(r, binary.LittleEndian, &entryCount); err != nil {
		return nil, fmt.Errorf("reading entry count: %w", err)
	}
	refs := make([]SectionRef, entryCount)
	for i := range refs {
		var pIdx, secID uint32
		if err := binary.Read(r, binary.LittleEndian, &pIdx); err != nil {
			return nil, fmt.Errorf("reading path index for entry %d: %w", i, err)
		}
		if int(pIdx) >= len(pathStrings) {
			return nil, fmt.Errorf("path index %d out of range (have %d paths)", pIdx, len(pathStrings))
		}
		if err := binary.Read(r, binary.LittleEndian, &secID); err != nil {
			return nil, fmt.Errorf("reading section ID for entry %d: %w", i, err)
		}
		refs[i] = SectionRef{Path: pathStrings[pIdx], SectionID: int(secID)}
	}

	return &ChunkRefTable{refs: refs}, nil
}
