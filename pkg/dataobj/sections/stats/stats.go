// Package stats defines types for the data object stats section. The stats
// section holds per-section statistics for the data objects referenced in an
// index object.
package stats

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// sectionType identifies stats sections in a data object (unexported, matching existing convention).
var sectionType = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "stats",
	Version:   1,
}

// ColumnType identifies the type of a column in the stats section.
type ColumnType int

const (
	ColumnTypeInvalid          ColumnType = iota // ColumnTypeInvalid is an invalid column type.
	ColumnTypeObjectPath                         // "object_path"
	ColumnTypeSectionIndex                       // "section_index"
	ColumnTypeSortSchema                         // "sort_schema"
	ColumnTypeMinTimestamp                       // "min_timestamp"
	ColumnTypeMaxTimestamp                       // "max_timestamp"
	ColumnTypeRowCount                           // "row_count"
	ColumnTypeUncompressedSize                   // "uncompressed_size"
	ColumnTypeLabel                              // "label" — dynamic; tag carries label name
)

var columnTypeNames = map[ColumnType]string{
	ColumnTypeInvalid:          "invalid",
	ColumnTypeObjectPath:       "object_path",
	ColumnTypeSectionIndex:     "section_index",
	ColumnTypeSortSchema:       "sort_schema",
	ColumnTypeMinTimestamp:     "min_timestamp",
	ColumnTypeMaxTimestamp:     "max_timestamp",
	ColumnTypeRowCount:         "row_count",
	ColumnTypeUncompressedSize: "uncompressed_size",
	ColumnTypeLabel:            "label",
}

// ParseColumnType parses a [ColumnType] from a string. The expected string
// format is the same as what's returned by [ColumnType.String].
func ParseColumnType(text string) (ColumnType, error) {
	switch text {
	case "invalid":
		return ColumnTypeInvalid, nil
	case "object_path":
		return ColumnTypeObjectPath, nil
	case "section_index":
		return ColumnTypeSectionIndex, nil
	case "sort_schema":
		return ColumnTypeSortSchema, nil
	case "min_timestamp":
		return ColumnTypeMinTimestamp, nil
	case "max_timestamp":
		return ColumnTypeMaxTimestamp, nil
	case "row_count":
		return ColumnTypeRowCount, nil
	case "uncompressed_size":
		return ColumnTypeUncompressedSize, nil
	case "label":
		return ColumnTypeLabel, nil
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

// CheckSection returns true if the section is a stats section.
func CheckSection(section *dataobj.Section) bool {
	return sectionType.Equals(section.Type)
}

// Stat represents a single row in the stats section.
type Stat struct {
	ObjectPath       string
	SectionIndex     int64
	SortSchema       string
	Labels           map[string]string // Label values keyed by sort schema key name
	MinTimestamp     int64             // UnixNano
	MaxTimestamp     int64             // UnixNano
	RowCount         int64
	UncompressedSize int64
}

// SectionEncoder encodes a batch of sorted Stat rows into a columnar encoder.
type SectionEncoder func(rows []Stat, enc *columnar.Encoder) error
