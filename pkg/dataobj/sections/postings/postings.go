// Package postings defines types for the data object postings section. The
// postings section holds posting lists (Bloom-filter and label-based) for
// data objects referenced in an index object.
package postings

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// sectionType identifies postings sections in a data object.
var sectionType = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "postings",
	Version:   columnar.FormatVersion,
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


// Section represents an opened postings section.
type Section struct {
	inner   *columnar.Section
	columns []*Column
}

// Open opens a [Section] from an underlying [dataobj.Section]. Open returns an
// error if the section metadata could not be read or if the provided ctx is
// canceled.
func Open(ctx context.Context, section *dataobj.Section) (*Section, error) {
	if !CheckSection(section) {
		return nil, fmt.Errorf("section type mismatch: got=%s want=%s", section.Type, sectionType)
	} else if section.Type.Version != columnar.FormatVersion {
		return nil, fmt.Errorf("unsupported section version: got=%d want=%d", section.Type.Version, columnar.FormatVersion)
	}

	dec, err := columnar.NewDecoder(section.Reader, section.Type.Version)
	if err != nil {
		return nil, fmt.Errorf("creating decoder: %w", err)
	}

	columnarSection, err := columnar.Open(ctx, section.Tenant, dec)
	if err != nil {
		return nil, fmt.Errorf("opening columnar section: %w", err)
	}

	sec := &Section{inner: columnarSection}
	if err := sec.init(); err != nil {
		return nil, fmt.Errorf("initializing section: %w", err)
	}
	return sec, nil
}

func (s *Section) init() error {
	for _, col := range s.inner.Columns() {
		colType, err := ParseColumnType(col.Type.Logical)
		if err != nil {
			// Skip over unrecognized columns; probably come from a newer
			// version of the code.
			continue
		}

		s.columns = append(s.columns, &Column{
			Section: s,
			Name:    col.Tag,
			Type:    colType,

			inner: col,
		})
	}

	return nil
}

// Columns returns the set of Columns in the section. The slice of returned
// columns must not be mutated.
//
// Unrecognized columns (e.g., when running older code against newer postings
// sections) are skipped.
func (s *Section) Columns() []*Column { return s.columns }

// A Column represents one of the columns in the postings section. Valid columns
// can only be retrieved by calling [Section.Columns].
//
// Data in columns can be read by using a [Reader].
type Column struct {
	Section *Section   // Section that contains the column.
	Name    string     // Optional name of the column.
	Type    ColumnType // Type of data in the column.

	inner *columnar.Column
}
