package thortsdbexample

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/prometheus/prometheus/model/labels"
)

var parallelism = 32

func main() {
}

// scanStreamLabels fetches each referenced object in parallel, iterates its
// streams sections, and counts unique key=value label pairs across all objects.
// streamRef holds the data needed to build a TSDB entry for one stream in one object.
type streamRef struct {
	labels  labels.Labels
	path    string
	section int
	minTime int64
	maxTime int64
	rows    int
	KB      int
}

// sectionRef identifies the object and section that a ChunkMeta points to.
// Stored in the lookup table alongside the TSDB index.
type sectionRef struct {
	Path      string
	SectionID int
}

// encodeSectionRefTable serializes a sectionRef slice into a compact binary format
// with an interned string table for paths.
//
// Wire format:
//
//	[uint32 path_count]             -- interned string table
//	for each path:
//	  [uint16 len][path bytes]
//	[uint32 entry_count]            -- entries indexed by checksum
//	for each entry:
//	  [uint32 path_string_index][uint32 section_id]
func encodeSectionRefTable(refs []sectionRef) []byte {
	pathIdx := make(map[string]uint32)
	var pathStrings []string
	for _, ref := range refs {
		if _, ok := pathIdx[ref.Path]; !ok {
			pathIdx[ref.Path] = uint32(len(pathStrings))
			pathStrings = append(pathStrings, ref.Path)
		}
	}

	var buf bytes.Buffer

	// String table.
	binary.Write(&buf, binary.LittleEndian, uint32(len(pathStrings)))
	for _, s := range pathStrings {
		binary.Write(&buf, binary.LittleEndian, uint16(len(s)))
		buf.WriteString(s)
	}

	// Entries.
	binary.Write(&buf, binary.LittleEndian, uint32(len(refs)))
	for _, ref := range refs {
		binary.Write(&buf, binary.LittleEndian, pathIdx[ref.Path])
		binary.Write(&buf, binary.LittleEndian, uint32(ref.SectionID))
	}

	return buf.Bytes()
}

// decodeSectionRefTable deserializes the binary table produced by encodeSectionRefTable.
// The returned slice is indexed by checksum: table[chunkMeta.Checksum] == sectionRef.
func decodeSectionRefTable(data []byte) ([]sectionRef, error) {
	r := bytes.NewReader(data)

	// String table.
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

	// Entries.
	var entryCount uint32
	if err := binary.Read(r, binary.LittleEndian, &entryCount); err != nil {
		return nil, fmt.Errorf("reading entry count: %w", err)
	}
	entries := make([]sectionRef, entryCount)
	for i := range entries {
		var pIdx, secID uint32
		if err := binary.Read(r, binary.LittleEndian, &pIdx); err != nil {
			return nil, fmt.Errorf("reading path index: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &secID); err != nil {
			return nil, fmt.Errorf("reading section ID: %w", err)
		}
		entries[i] = sectionRef{Path: pathStrings[pIdx], SectionID: int(secID)}
	}
	return entries, nil
}
