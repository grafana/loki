// Package logs defines types used for the data object logs section. The logs
// section holds a list of log records across multiple streams.
package logs

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

var sectionType = dataobj.SectionType{
	Namespace: "github.com/grafana/loki",
	Kind:      "logs",
}

// CheckSection returns true if section is a logs section.
func CheckSection(section *dataobj.Section) bool { return section.Type == sectionType }

// Section represents an opened logs section.
type Section struct{ reader dataobj.SectionReader }

// Open opens a Section from an underlying [dataobj.Section]. Open returns an
// error if the section metadata could not be read or if the provided ctx is
// canceled.
func Open(_ context.Context, section *dataobj.Section) (*Section, error) {
	if !CheckSection(section) {
		return nil, fmt.Errorf("section type mismatch: got=%s want=%s", section.Type, sectionType)
	}

	// TODO(rfratto): pre-load metadata to expose column information to callers.
	return &Section{reader: section.Reader}, nil
}
