// Package stats defines types for the data object stats section. The stats
// section holds per-section statistics for the data objects referenced in an
// index object.
package stats

import "github.com/grafana/loki/v3/pkg/dataobj"

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
	RunID            int64
	SortSchema       string
	ServiceName      string // Label value for the service_name sort key
	MinTimestamp     int64  // UnixNano
	MaxTimestamp     int64  // UnixNano
	RowCount         int64
	UncompressedSize int64
}
