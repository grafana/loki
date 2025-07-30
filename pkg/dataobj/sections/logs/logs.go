// Package logs defines types used for the data object logs section. The logs
// section holds a list of log records across multiple streams.
package logs

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
)

var sectionType = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "logs",
}

// CheckSection returns true if section is a logs section.
func CheckSection(section *dataobj.Section) bool { return section.Type == sectionType }

// Section represents an opened logs section.
type Section struct {
	reader   dataobj.SectionReader
	columns  []*Column
	sortInfo *datasetmd.SectionSortInfo
	tenantID string
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
	s.tenantID = metadata.GetTenantID()

	for _, col := range metadata.GetColumns() {
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

	s.sortInfo = metadata.SortInfo
	return nil
}

// TenantID returns the tenant that owns the section.
func (s *Section) TenantID() string { return s.tenantID }

// Columns returns the set of Columns in the section. The slice of returned
// sections must not be mutated.
//
// Unrecognized columns (e.g., when running older code against newer sterams
// sections) are skipped.
func (s *Section) Columns() []*Column { return s.columns }

// PrimarySortOrder returns the primary sort order information of the section
// as a tuple of [ColumnType] and [SortDirection].
func (s *Section) PrimarySortOrder() (ColumnType, SortDirection, error) {
	if s.sortInfo == nil || len(s.sortInfo.ColumnSorts) == 0 {
		return ColumnTypeInvalid, SortDirectionUnspecified, fmt.Errorf("missing sort order information")
	}

	si := s.sortInfo.ColumnSorts[0] // primary sort order
	idx := int(si.ColumnIndex)
	if idx < 0 || idx >= len(s.columns) {
		return ColumnTypeInvalid, SortDirectionUnspecified, fmt.Errorf("invalid column reference in sort info")
	}

	dir, ok := convertSortDirection(si.Direction)
	if !ok {
		return ColumnTypeInvalid, SortDirectionUnspecified, fmt.Errorf("invalid sort direction %d in sort info", si.Direction)
	}

	return s.columns[idx].Type, dir, nil
}

// A Column represents one of the columns in the logs section. Valid columns
// can only be retrieved by calling [Section.Columns].
//
// Data in columns can be read by using a [Reader].
type Column struct {
	Section *Section   // Section that contains the column.
	Name    string     // Optional name of the column.
	Type    ColumnType // Type of data in the column.

	desc *logsmd.ColumnDesc // Column description used for further decoding and reading.
}

// ColumnType represents the kind of information stored in a [Column].
type ColumnType int

const (
	ColumnTypeInvalid   ColumnType = iota // ColumnTypeInvalid is an invalid column.
	ColumnTypeStreamID                    // ColumnTypeStreamID is a column that contains stream IDs.
	ColumnTypeTimestamp                   // ColumnTypeTimestamp is a column that contains timestamps per log record.

	// ColumnTypeMetadata is a column containing a sequence of structured
	// metadata values per log record. There will be one ColumnTypeMetadata per
	// structured metadata key; the name of the structured metadata key is stored
	// as the column name.
	ColumnTypeMetadata

	ColumnTypeMessage // ColumnTypeMessage is a column that contains log messages.
)

func convertColumnType(protoType logsmd.ColumnType) (ColumnType, bool) {
	switch protoType {
	case logsmd.COLUMN_TYPE_UNSPECIFIED:
		return ColumnTypeInvalid, true
	case logsmd.COLUMN_TYPE_STREAM_ID:
		return ColumnTypeStreamID, true
	case logsmd.COLUMN_TYPE_TIMESTAMP:
		return ColumnTypeTimestamp, true
	case logsmd.COLUMN_TYPE_METADATA:
		return ColumnTypeMetadata, true
	case logsmd.COLUMN_TYPE_MESSAGE:
		return ColumnTypeMessage, true
	}

	return ColumnTypeInvalid, false
}

// SortDirection represents sort direction of a column.
type SortDirection int

const (
	SortDirectionUnspecified SortDirection = 0 // Sort direction is unspecified.
	SortDirectionAscending   SortDirection = 1 // SortDirectionAscending represents ascending sort order (smallest values first).
	SortDirectionDescending  SortDirection = 2 // SortDirectionDescending represents descending sort order (largest values first).
)

func convertSortDirection(protoDirection datasetmd.SortDirection) (SortDirection, bool) {
	switch protoDirection {
	case datasetmd.SORT_DIRECTION_UNSPECIFIED:
		return SortDirectionUnspecified, true
	case datasetmd.SORT_DIRECTION_ASCENDING:
		return SortDirectionAscending, true
	case datasetmd.SORT_DIRECTION_DESCENDING:
		return SortDirectionDescending, true
	}

	return SortDirectionUnspecified, false
}

func IsMetadataColumn(colType string) bool {
	return colType == logsmd.COLUMN_TYPE_METADATA.String()
}

var columnTypeNames = map[ColumnType]string{
	ColumnTypeInvalid:   "invalid",
	ColumnTypeStreamID:  "stream_id",
	ColumnTypeTimestamp: "timestamp",
	ColumnTypeMetadata:  "metadata",
	ColumnTypeMessage:   "message",
}

// String returns the human-readable name of ct.
func (ct ColumnType) String() string {
	text, ok := columnTypeNames[ct]
	if !ok {
		return fmt.Sprintf("ColumnType(%d)", ct)
	}
	return text
}
