// Package postings defines types for the data object postings section. The
// postings section holds posting lists (Bloom-filter and label-based) for
// data objects referenced in an index object.
package postings

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj"
	sectionscolumnar "github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// sectionType identifies postings sections in a data object.
var sectionType = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "postings",
	Version:   1,
}

// ColumnType identifies the type of a column in the postings section.
type ColumnType int

const (
	ColumnTypeInvalid          ColumnType = iota // ColumnTypeInvalid is an invalid column type.
	ColumnTypeKind                               // "kind"
	ColumnTypeObjectPath                         // "object_path"
	ColumnTypeSectionIndex                       // "section_index"
	ColumnTypeColumnName                         // "column_name"
	ColumnTypeLabelValue                         // "label_value"
	ColumnTypeBloomFilter                        // "bloom_filter"
	ColumnTypeStreamIDBitmap                     // "stream_id_bitmap"
	ColumnTypeUncompressedSize                   // "uncompressed_size"
	ColumnTypeMinTimestamp                       // "min_timestamp"
	ColumnTypeMaxTimestamp                       // "max_timestamp"
)

var columnTypeNames = map[ColumnType]string{
	ColumnTypeInvalid:          "invalid",
	ColumnTypeKind:             "kind",
	ColumnTypeObjectPath:       "object_path",
	ColumnTypeSectionIndex:     "section_index",
	ColumnTypeColumnName:       "column_name",
	ColumnTypeLabelValue:       "label_value",
	ColumnTypeBloomFilter:      "bloom_filter",
	ColumnTypeStreamIDBitmap:   "stream_id_bitmap",
	ColumnTypeUncompressedSize: "uncompressed_size",
	ColumnTypeMinTimestamp:     "min_timestamp",
	ColumnTypeMaxTimestamp:     "max_timestamp",
}

// ParseColumnType parses a [ColumnType] from a string. The expected string
// format is the same as what's returned by [ColumnType.String].
func ParseColumnType(text string) (ColumnType, error) {
	switch text {
	case "invalid":
		return ColumnTypeInvalid, nil
	case "kind":
		return ColumnTypeKind, nil
	case "object_path":
		return ColumnTypeObjectPath, nil
	case "section_index":
		return ColumnTypeSectionIndex, nil
	case "column_name":
		return ColumnTypeColumnName, nil
	case "label_value":
		return ColumnTypeLabelValue, nil
	case "bloom_filter":
		return ColumnTypeBloomFilter, nil
	case "stream_id_bitmap":
		return ColumnTypeStreamIDBitmap, nil
	case "uncompressed_size":
		return ColumnTypeUncompressedSize, nil
	case "min_timestamp":
		return ColumnTypeMinTimestamp, nil
	case "max_timestamp":
		return ColumnTypeMaxTimestamp, nil
	}
	return ColumnTypeInvalid, fmt.Errorf("invalid column type %q", text)
}

// String returns the human-readable name of [ct].
func (ct ColumnType) String() string {
	text, ok := columnTypeNames[ct]
	if !ok {
		return fmt.Sprintf("ColumnType(%d)", ct)
	}
	return text
}

// CheckSection returns true if the section is a postings section.
func CheckSection(section *dataobj.Section) bool {
	return sectionType.Equals(section.Type)
}

// PostingKind identifies the kind of posting entry.
type PostingKind int64

const (
	// KindBloom identifies a Bloom-filter posting entry.
	KindBloom PostingKind = 0

	// KindLabel identifies a label-based posting entry.
	KindLabel PostingKind = 1
)

// Posting represents a single row in the postings section.
type Posting struct {
	Kind             PostingKind
	ObjectPath       string
	SectionIndex     int64
	ColumnName       string
	LabelValue       string // empty for Bloom postings
	BloomFilter      []byte // nil for Label postings
	StreamIDBitmap   []byte // always present
	UncompressedSize int64
	MinTimestamp     int64
	MaxTimestamp     int64
}

// Size returns an estimate of the encoded size of this posting in bytes.
func (p Posting) Size() int {
	// 5 int64 columns (kind, section_index, uncompressed_size, min_timestamp, max_timestamp) × 8 bytes
	return 5*8 + len(p.ObjectPath) + len(p.ColumnName) + len(p.LabelValue) + len(p.BloomFilter) + len(p.StreamIDBitmap)
}

// ColumnReader reads batches of columnar values from a single column.
type ColumnReader interface {
	// Read reads up to count values. Returns columnar.Array and any error.
	// Returns io.EOF when no more data is available.
	Read(ctx context.Context, count int) (columnar.Array, error)
	Close() error
}

// Section holds encoded column data for one flushed postings section.
type Section struct {
	ColumnNames []string
	RowCount    int
	// OpenColumn returns a ColumnReader for the named column.
	// Returns an error if the column is not found.
	OpenColumn func(name string) (ColumnReader, error)
}

// SectionEncoder encodes a batch of sorted Posting rows into a columnar encoder.
type SectionEncoder func(rows []Posting, enc *sectionscolumnar.Encoder) error
