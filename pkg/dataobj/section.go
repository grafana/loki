package dataobj

import (
	"context"
	"io"
)

// A Section is a subset of an [Object] that holds a specific type of data. Use
// section packages for higher-level abstractions around sections.
type Section struct {
	Type   SectionType   // The type denoting the kind of data held in a section.
	Reader SectionReader // The low-level reader for a Section.
}

// SectionType uniquely identifies a [Section] type.
type SectionType struct {
	Namespace string // A namesapce for the section (e.g., "github.com/grafana/loki").
	Kind      string // The kind of section, scoped to the namespace (e.g., "logs").
}

func (ty SectionType) String() string {
	return ty.Namespace + "/" + ty.Kind
}

// SectionReader is a low-level interface to read data ranges and metadata from
// a section.
//
// Section packages provider higher-level abstractions around [Section] using
// this interface.
type SectionReader interface {
	// DataRange opens a reader of length bytes from the data region of a
	// section. The offset argument determines where in the data region reading
	// should start.
	//
	// DataRange returns an error if the read fails or if offset+length goes
	// beyond the readable data region. The returned reader is only valid as long
	// as the provided ctx is not canceled.
	//
	// Implementations of SectionReader should interpret offset as relative to
	// the start of the data object if the data object does not explicitly
	// specify a data region. This is only relevant to older versions of data
	// objects where sections used absolute offsets and the data region was
	// implicitly the entire file.
	DataRange(ctx context.Context, offset, length int64) (io.ReadCloser, error)

	// Metadata opens a reader to the entire metadata region of a section.
	// Metadata returns an error if the read fails. The returned reader is only
	// valid as long as the provided ctx is not canceled.
	Metadata(ctx context.Context) (io.ReadCloser, error)
}
