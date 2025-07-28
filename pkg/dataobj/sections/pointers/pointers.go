// package pointers defines types used for the data object pointers section. The
// pointers section holds a list of pointers to sections present in the data object.
package pointers

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/pointersmd"
)

var sectionType = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "pointers",
}

// CheckSection returns true if section is a streams section.
func CheckSection(section *dataobj.Section) bool { return section.Type == sectionType }

// Section represents an opened streams section.
type Section struct {
	reader  dataobj.SectionReader
	columns []*Column
}

// Open opens a Section from an underlying [dataobj.Section]. Open returns an
// error if the section metadata could not be read or if the provided ctx is
// canceled.
func Open(ctx context.Context, section *dataobj.Section) (*Section, error) {
	if !CheckSection(section) {
		return nil, fmt.Errorf("section type mismatch: got=%s want=%s", section.Type, sectionType)
	}

	sec := &Section{reader: section.Reader}
	if err := sec.init(ctx); err != nil {
		return nil, fmt.Errorf("intializing section: %w", err)
	}
	return sec, nil
}

func (s *Section) init(ctx context.Context) error {
	dec := newDecoder(s.reader)
	metadata, err := dec.Metadata(ctx)
	if err != nil {
		return fmt.Errorf("failed to decode metadata: %w", err)
	}
	columnDescs := metadata.GetColumns()

	for _, col := range columnDescs {
		colType, ok := convertColumnType(col.Type)
		if !ok {
			// Skip over unrecognized columns.
			continue
		}

		s.columns = append(s.columns, &Column{
			Section: s,
			Name:    col.Info.Name,
			Type:    colType,

			desc: col,
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

	desc *pointersmd.ColumnDesc // Column description used for further decoding and reading.
}

// ColumnType represents the kind of information stored in a [Column].
type ColumnType int

func convertColumnType(protoType pointersmd.ColumnType) (ColumnType, bool) {
	switch protoType {
	case pointersmd.COLUMN_TYPE_UNSPECIFIED:
		return ColumnTypeInvalid, true
	case pointersmd.COLUMN_TYPE_PATH:
		return ColumnTypePath, true
	case pointersmd.COLUMN_TYPE_SECTION:
		return ColumnTypeSection, true
	case pointersmd.COLUMN_TYPE_POINTER_KIND:
		return ColumnTypePointerKind, true

	case pointersmd.COLUMN_TYPE_STREAM_ID:
		return ColumnTypeStreamID, true
	case pointersmd.COLUMN_TYPE_STREAM_ID_REF:
		return ColumnTypeStreamIDRef, true
	case pointersmd.COLUMN_TYPE_MIN_TIMESTAMP:
		return ColumnTypeMinTimestamp, true
	case pointersmd.COLUMN_TYPE_MAX_TIMESTAMP:
		return ColumnTypeMaxTimestamp, true
	case pointersmd.COLUMN_TYPE_ROW_COUNT:
		return ColumnTypeRowCount, true
	case pointersmd.COLUMN_TYPE_UNCOMPRESSED_SIZE:
		return ColumnTypeUncompressedSize, true

	case pointersmd.COLUMN_TYPE_COLUMN_NAME:
		return ColumnTypeColumnName, true
	case pointersmd.COLUMN_TYPE_COLUMN_INDEX:
		return ColumnTypeColumnIndex, true
	case pointersmd.COLUMN_TYPE_VALUES_BLOOM_FILTER:
		return ColumnTypeValuesBloomFilter, true
	}

	return ColumnTypeInvalid, false
}

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
