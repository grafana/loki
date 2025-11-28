package generic

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

func NewGenericSectionType(kind string) dataobj.SectionType {
	return dataobj.SectionType{
		Namespace: "github.com/grafana/loki",
		Kind:      kind,
		Version:   columnar.FormatVersion,
	}
}

var SectionTypeGeneric = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "generic",
	Version:   columnar.FormatVersion,
}

func CheckSection(kind string) func(*dataobj.Section) bool {
	return func(section *dataobj.Section) bool {
		return NewGenericSectionType(kind).Equals(section.Type)
	}
}

// Section represents an opened generic section.
type Section struct {
	inner   *columnar.Section
	columns []*Column
}

// Open opens a Section from an underlying [dataobj.Section]. Open returns an
// error if the section metadata could not be read or if the provided ctx is
// canceled.
func Open(ctx context.Context, section *dataobj.Section, kind string) (*Section, error) {
	if !CheckSection(kind)(section) {
		return nil, fmt.Errorf("section type mismatch: got=%s want=%s", section.Type, SectionTypeGeneric)
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
func (s *Section) Columns() []*Column { return s.columns }

// PrimarySortOrder returns the primary sort order information of the section
// as a tuple of [ColumnType] and [SortDirection].
func (s *Section) PrimarySortOrder() (ColumnType, SortDirection, error) {
	ty, protoDir, err := s.inner.PrimarySortOrder()
	if err != nil {
		return ColumnTypeInvalid, SortDirectionUnspecified, err
	}

	dir, ok := convertSortDirection(protoDir)
	if !ok {
		return ColumnTypeInvalid, SortDirectionUnspecified, fmt.Errorf("invalid sort direction %d in sort info", protoDir)
	}

	colType, err := ParseColumnType(ty.Logical)
	if err != nil {
		return ColumnTypeInvalid, SortDirectionUnspecified, err
	}
	return colType, dir, nil
}

// A Column represents one of the columns in the logs section. Valid columns
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
	ColumnTypeInvalid   ColumnType = iota // ColumnTypeInvalid is an invalid column.
	ColumnTypeString                      // ColumnTypeString is a column that contains string values.
	ColumnTypeTimestamp                   // ColumnTypeTimestamp is a column that contains timestamps.
	ColumnTypeInteger                     // ColumnTypeInteger is a column that contains integer values.
	// Not supported by dataobj yet
	// ColumnTypeFloat                    // ColumnTypeFloat is a column that contains float values.
	// ColumnTypeDuration                 // ColumnTypeDuration is a column that contains duration values.
	// ColumnTypeBytes                    // ColumnTypeBytes is a column that contains byte values.
)

var columnTypeNames = map[ColumnType]string{
	ColumnTypeInvalid:   "invalid",
	ColumnTypeString:    "string",
	ColumnTypeTimestamp: "timestamp",
	ColumnTypeInteger:   "integer",
}

// ParseColumnType parses a [ColumnType] from a string. The expected string
// format is the same as the return value of [ColumnType.String].
func ParseColumnType(text string) (ColumnType, error) {
	switch text {
	case "invalid":
		return ColumnTypeInvalid, nil
	case "string":
		return ColumnTypeString, nil
	case "timestamp":
		return ColumnTypeTimestamp, nil
	case "integer":
		return ColumnTypeInteger, nil
	}

	return ColumnTypeInvalid, fmt.Errorf("invalid column type %q", text)
}

// String returns the human-readable name of ct.
func (ct ColumnType) String() string {
	text, ok := columnTypeNames[ct]
	if !ok {
		return fmt.Sprintf("ColumnType(%d)", ct)
	}
	return text
}

// SortDirection represents sort direction of a column.
type SortDirection int

const (
	SortDirectionDescending  SortDirection = -1 // SortDirectionDescending represents descending sort order (largest values first).
	SortDirectionUnspecified SortDirection = 0  // Sort direction is unspecified.
	SortDirectionAscending   SortDirection = 1  // SortDirectionAscending represents ascending sort order (smallest values first).
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
