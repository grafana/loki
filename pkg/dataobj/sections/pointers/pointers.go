// Package pointers defines types used for the data object pointers section. The
// pointers section holds a list of pointers to sections present in the data object.
package pointers

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

var sectionType = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "pointers",
	Version:   columnar.FormatVersion,
}

// CheckSection returns true if section is a streams section.
func CheckSection(section *dataobj.Section) bool { return sectionType.Equals(section.Type) }

// Section represents an opened streams section.
type Section struct {
	inner   *columnar.Section
	columns []*Column
}

// Open opens a Section from an underlying [dataobj.Section]. Open returns an
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
		return nil, fmt.Errorf("intializing section: %w", err)
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
// sections must not be mutated.
//
// Unrecognized columns (e.g., when running older code against newer streams
// sections) are skipped.
func (s *Section) Columns() []*Column { return s.columns }

// A Column represents one of the columns in the streams section. Valid columns
// can only be retrieved by calling [Section.Columns].
//
// Data in columns can be read by using a [Reader].
type Column struct {
	Section *Section   // Section that contains the column.
	Name    string     // Optional name of the column.
	Type    ColumnType // Type of data in the column.

	inner *columnar.Column
}

// ColumnType represents the kind of information stored in a [Column].
type ColumnType int

const (
	ColumnTypeInvalid ColumnType = iota // ColumnTypeInvalid is an invalid column.
	ColumnTypePath
	ColumnTypeSection
	ColumnTypePointerKind // ColumnTypePointerKind is a column containing the kind of pointer: stream or column.

	ColumnTypeStreamID         // ColumnTypeStreamID is a column containing a set of stream IDs.
	ColumnTypeStreamIDRef      // ColumnTypeStreamIDRef is a column containing a set of stream IDs from the referenced object.
	ColumnTypeMinTimestamp     // ColumnTypeMinTimestamp is a column containing minimum timestamps per stream.
	ColumnTypeMaxTimestamp     // ColumnTypeMaxTimestamp is a column containing maximum timestamps per stream.
	ColumnTypeRowCount         // ColumnTypeRowCount is a column containing row count per stream.
	ColumnTypeUncompressedSize // ColumnTypeUncompressedSize is a column containing uncompressed size per stream.

	ColumnTypeColumnName        // ColumnTypeColumnName is a column containing the name of the column in the referenced object.
	ColumnTypeColumnIndex       // ColumnTypeColumnIndex is a column containing the index of the column in the referenced object.
	ColumnTypeValuesBloomFilter // ColumnTypeValuesBloomFilter is a column containing a bloom filter of the values in the column in the referenced object.
)

// ParseColumnType parses a [ColumnType] from a string. The expected string
// format is the same as what's returned by [ColumnType.String].
func ParseColumnType(text string) (ColumnType, error) {
	switch text {
	case "invalid":
		return ColumnTypeInvalid, nil
	case "path":
		return ColumnTypePath, nil
	case "section":
		return ColumnTypeSection, nil
	case "pointer_kind":
		return ColumnTypePointerKind, nil

	case "stream_id":
		return ColumnTypeStreamID, nil
	case "stream_id_ref":
		return ColumnTypeStreamIDRef, nil
	case "min_timestamp":
		return ColumnTypeMinTimestamp, nil
	case "max_timestamp":
		return ColumnTypeMaxTimestamp, nil
	case "row_count":
		return ColumnTypeRowCount, nil
	case "uncompressed_size":
		return ColumnTypeUncompressedSize, nil

	case "column_name":
		return ColumnTypeColumnName, nil
	case "column_index":
		return ColumnTypeColumnIndex, nil
	case "values_bloom_filter":
		return ColumnTypeValuesBloomFilter, nil
	}

	return ColumnTypeInvalid, fmt.Errorf("invalid column type %q", text)
}

var columnTypeNames = map[ColumnType]string{
	ColumnTypeInvalid:     "invalid",
	ColumnTypePath:        "path",
	ColumnTypeSection:     "section",
	ColumnTypePointerKind: "pointer_kind",

	ColumnTypeStreamID:         "stream_id",
	ColumnTypeStreamIDRef:      "stream_id_ref",
	ColumnTypeMinTimestamp:     "min_timestamp",
	ColumnTypeMaxTimestamp:     "max_timestamp",
	ColumnTypeRowCount:         "row_count",
	ColumnTypeUncompressedSize: "uncompressed_size",

	ColumnTypeColumnName:        "column_name",
	ColumnTypeColumnIndex:       "column_index",
	ColumnTypeValuesBloomFilter: "values_bloom_filter",
}

// String returns the human-readable name of ct.
func (ct ColumnType) String() string {
	text, ok := columnTypeNames[ct]
	if !ok {
		return fmt.Sprintf("ColumnType(%d)", ct)
	}
	return text
}

func ColumnTypeFromField(field arrow.Field) ColumnType {
	switch field.Name {
	case "path.path.utf8":
		return ColumnTypePath
	case "section.int64":
		return ColumnTypeSection
	case "stream_id.int64":
		return ColumnTypeStreamID
	case "stream_id_ref.int64":
		return ColumnTypeStreamIDRef
	case "min_timestamp.timestamp":
		return ColumnTypeMinTimestamp
	case "max_timestamp.timestamp":
		return ColumnTypeMaxTimestamp
	case "row_count.int64":
		return ColumnTypeRowCount
	case "uncompressed_size.int64":
		return ColumnTypeUncompressedSize
	case "column_name.column_name.utf8":
		return ColumnTypeColumnName
	case "column_index.int64":
		return ColumnTypeColumnIndex
	case "values_bloom_filter.int64":
		return ColumnTypeValuesBloomFilter
	default:
		return ColumnTypeInvalid
	}
}
