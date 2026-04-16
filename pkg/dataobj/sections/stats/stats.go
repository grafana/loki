// Package stats defines types for the data object stats section. The stats
// section holds per-section statistics for the data objects referenced in an
// index object.
package stats

import (
	"context"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj"
)

// sectionType identifies stats sections in a data object (unexported, matching existing convention).
var sectionType = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "stats",
	Version:   1,
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

// ColumnReader reads batches of columnar values from a single column.
type ColumnReader interface {
	// Read reads up to count values. Returns columnar.Array and any error.
	// Returns io.EOF when no more data is available.
	Read(ctx context.Context, count int) (columnar.Array, error)
	Close() error
}

// Section holds encoded column data for one flushed stats section.
type Section struct {
	ColumnNames []string
	RowCount    int
	// OpenColumn returns a ColumnReader for the named column.
	// Returns an error if the column is not found.
	OpenColumn func(name string) (ColumnReader, error)
}

// SectionEncoder encodes a batch of sorted Stat rows into a Section.
type SectionEncoder func(ctx context.Context, rows []Stat) (Section, error)
