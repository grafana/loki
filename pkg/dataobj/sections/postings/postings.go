// Package postings defines types for the data object postings section. The
// postings section holds posting lists (Bloom-filter and label-based) for
// data objects referenced in an index object.
package postings

import "github.com/grafana/loki/v3/pkg/dataobj"

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
	LabelValue       *string // nil for Bloom postings
	BloomFilter      []byte  // nil for Label postings
	StreamIDBitmap   []byte  // always present
	UncompressedSize int64
	MinTimestamp     int64
	MaxTimestamp     int64
}
