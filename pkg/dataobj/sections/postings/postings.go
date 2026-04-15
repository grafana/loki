// Package postings defines types for the data object postings section. The
// postings section holds posting lists (Bloom-filter and label-based) for
// data objects referenced in an index object.
package postings

import (
	"context"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj"
)

// sectionType identifies postings sections in a data object.
var sectionType = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "postings",
	Version:   1,
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

// SectionEncoder encodes a batch of sorted Posting rows into a Section.
type SectionEncoder func(ctx context.Context, rows []Posting) (Section, error)
