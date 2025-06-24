// Package streams defines types used for the data object streams section. The
// streams section holds a list of streams present in the data object.
package streams

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
)

var sectionType = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "streams",
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
	cols, err := dec.Columns(ctx)
	if err != nil {
		return fmt.Errorf("failed to decode columns: %w", err)
	}

	for _, col := range cols {
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
// Unrecognized columns (e.g., when running older code against newer sterams
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

	desc *streamsmd.ColumnDesc // Column description used for further decoding and reading.
}

// ColumnType represents the kind of information stored in a [Column].
type ColumnType int

func convertColumnType(protoType streamsmd.ColumnType) (ColumnType, bool) {
	switch protoType {
	case streamsmd.COLUMN_TYPE_UNSPECIFIED:
		return ColumnTypeInvalid, true
	case streamsmd.COLUMN_TYPE_STREAM_ID:
		return ColumnTypeStreamID, true
	case streamsmd.COLUMN_TYPE_MIN_TIMESTAMP:
		return ColumnTypeMinTimestamp, true
	case streamsmd.COLUMN_TYPE_MAX_TIMESTAMP:
		return ColumnTypeMaxTimestamp, true
	case streamsmd.COLUMN_TYPE_LABEL:
		return ColumnTypeLabel, true
	case streamsmd.COLUMN_TYPE_ROWS:
		return ColumnTypeRows, true
	case streamsmd.COLUMN_TYPE_UNCOMPRESSED_SIZE:
		return ColumnTypeUncompressedSize, true
	}

	return ColumnTypeInvalid, false
}

const (
	ColumnTypeInvalid      ColumnType = iota // ColumnTypeInvalid is an invalid column.
	ColumnTypeStreamID                       // ColumnTypeStreamID is a column containing a set of stream IDs.
	ColumnTypeMinTimestamp                   // ColumnTypeMinTimestamp is a column containing minimum timestamps per stream.
	ColumnTypeMaxTimestamp                   // ColumnTypeMaxTimestamp is a column containing maximum timestamps per stream.

	// ColumnTypeLabel is a column containing a sequence of label values per
	// stream. There will be one ColumnTypeLabels per label name; the name of the
	// label is stored as the column name.
	ColumnTypeLabel

	ColumnTypeRows             // ColumnTypeRows is a column containing row counts per stream.
	ColumnTypeUncompressedSize // ColumnTypeUncompressedSize is a column containing uncompressed size per stream.
)

var columnTypeNames = map[ColumnType]string{
	ColumnTypeInvalid:          "invalid",
	ColumnTypeStreamID:         "stream_id",
	ColumnTypeMinTimestamp:     "min_timestamp",
	ColumnTypeMaxTimestamp:     "max_timestamp",
	ColumnTypeLabel:            "label",
	ColumnTypeRows:             "rows",
	ColumnTypeUncompressedSize: "uncompressed_size",
}

// String returns the human-readable name of ct.
func (ct ColumnType) String() string {
	text, ok := columnTypeNames[ct]
	if !ok {
		return fmt.Sprintf("ColumnType(%d)", ct)
	}
	return text
}
