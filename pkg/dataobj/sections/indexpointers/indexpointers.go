package indexpointers

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

var sectionType = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "indexpointers",
	Version:   columnar.FormatVersion,
}

// CheckSection returns true if section is a indexpointers section.
func CheckSection(section *dataobj.Section) bool { return sectionType.Equals(section.Type) }

// Section represents an opened indexpointers section.
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
// Unrecognized columns (e.g., when running older code against newer indexpointers
// sections) are skipped.
func (s *Section) Columns() []*Column { return s.columns }

// ColumnType represents the kind of information stored in a [Column].
type ColumnType int

const (
	ColumnTypeInvalid      ColumnType = iota // ColumnTypeInvalid is an invalid column.
	ColumnTypePath                           // ColumnTypePath is a column containing the path to the index object.
	ColumnTypeMinTimestamp                   // ColumnTypeMinTimestamp is a column containing the minimum timestamp of the index object.
	ColumnTypeMaxTimestamp                   // ColumnTypeMaxTimestamp is a column containing the maximum timestamp of the index object.
)

var columnTypeNames = map[ColumnType]string{
	ColumnTypeInvalid:      "invalid",
	ColumnTypePath:         "path",
	ColumnTypeMinTimestamp: "min_timestamp",
	ColumnTypeMaxTimestamp: "max_timestamp",
}

// ParseColumnType parses a [ColumnType] from a string. The expected string
// format is same same as the return value of [ColumnType.String].
func ParseColumnType(text string) (ColumnType, error) {
	switch text {
	case "invalid":
		return ColumnTypeInvalid, nil
	case "path":
		return ColumnTypePath, nil
	case "min_timestamp":
		return ColumnTypeMinTimestamp, nil
	case "max_timestamp":
		return ColumnTypeMaxTimestamp, nil
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

// A Column represents one of the columns in the indexpointers section. Valid columns
// can only be retrieved by calling [Section.Columns].
//
// Data in columns can be read by using a [Reader].
type Column struct {
	Section *Section
	Name    string
	Type    ColumnType

	inner *columnar.Column
}
