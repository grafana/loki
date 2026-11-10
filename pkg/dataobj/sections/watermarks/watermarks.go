// Package watermarks provides access to the watermarks section of a
// table-of-contents (ToC) object.
//
// A watermark records, for one Kafka topic and partition, the highest offset
// whose records the ToC already holds. A section holds a few rows, so the
// package has no Arrow reader and no predicates.
package watermarks

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

var sectionType = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "watermarks",
	Version:   columnar.FormatVersion,
}

// SectionType returns the type of watermarks sections.
func SectionType() dataobj.SectionType { return sectionType }

// CheckSection returns true if section is a watermarks section.
func CheckSection(section *dataobj.Section) bool { return sectionType.Equals(section.Type) }

// Section represents an opened watermarks section.
type Section struct {
	inner   *columnar.Section
	columns []*Column
}

// Open opens a Section from an underlying [dataobj.Section].
//
// Open returns an error if the section has another type or version, if the
// metadata could not be read, if ctx is canceled, or if the section has
// columns but not exactly one topic, partition and watermark column.
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
	counts := make(map[ColumnType]int)
	for _, col := range s.inner.Columns() {
		colType, err := ParseColumnType(col.Type.Logical)
		if err != nil || colType == ColumnTypeInvalid {
			// Skip unrecognized columns. A newer Loki version can add columns.
			continue
		}
		counts[colType]++

		s.columns = append(s.columns, &Column{
			Section: s,
			Name:    col.Tag,
			Type:    colType,

			inner: col,
		})
	}

	if s.isEmpty() {
		return nil
	}
	for _, ct := range sectionColumns {
		if counts[ct] != 1 {
			return fmt.Errorf("got %d %s columns, want 1", counts[ct], ct)
		}
	}
	return nil
}

// isEmpty reports whether the section has no columns at all. The encoder drops
// columns without rows, so a section with no rows has no columns, and the
// dataset reader cannot open it.
func (s *Section) isEmpty() bool { return len(s.inner.Columns()) == 0 }

// Columns returns the set of Columns in the section. The caller must not modify
// the returned slice.
//
// Unrecognized columns (e.g., when running older code against newer watermarks
// sections) are skipped.
func (s *Section) Columns() []*Column { return s.columns }

// sectionColumns lists the column types that a non-empty section holds, one
// column each.
var sectionColumns = []ColumnType{ColumnTypeTopic, ColumnTypePartition, ColumnTypeWatermark}

// ColumnType represents the kind of information stored in a [Column].
type ColumnType int

const (
	ColumnTypeInvalid   ColumnType = iota // ColumnTypeInvalid marks a column of unknown type.
	ColumnTypeTopic                       // ColumnTypeTopic is a column containing the Kafka topic.
	ColumnTypePartition                   // ColumnTypePartition is a column containing the Kafka partition.
	ColumnTypeWatermark                   // ColumnTypeWatermark is a column containing the highest offset the ToC holds.
)

var columnTypeNames = map[ColumnType]string{
	ColumnTypeInvalid:   "invalid",
	ColumnTypeTopic:     "topic",
	ColumnTypePartition: "partition",
	ColumnTypeWatermark: "watermark",
}

// ParseColumnType parses a [ColumnType] from a string. The expected string
// format is the same as the return value of [ColumnType.String].
func ParseColumnType(text string) (ColumnType, error) {
	switch text {
	case "invalid":
		return ColumnTypeInvalid, nil
	case "topic":
		return ColumnTypeTopic, nil
	case "partition":
		return ColumnTypePartition, nil
	case "watermark":
		return ColumnTypeWatermark, nil
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

// Column is one of the columns in the watermarks section. Get a valid Column
// from [Section.Columns].
type Column struct {
	Section *Section
	Name    string
	Type    ColumnType

	inner *columnar.Column
}
