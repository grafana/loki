package dataobj

import (
	"context"
	"io"
	"iter"
)

// A Sections is a slice of [Section].
type Sections []*Section

// Filter returns an iterator over sections that pass some predicate. The index
// field is the number of the section that passed the predicate.
func (s Sections) Filter(predicate func(*Section) bool) iter.Seq2[int, *Section] {
	return func(yield func(int, *Section) bool) {
		var matches int

		for _, sec := range s {
			if !predicate(sec) {
				continue
			} else if !yield(matches, sec) {
				return
			}
			matches++
		}
	}
}

// Count returns the number of sections that pass some predicate.
func (s Sections) Count(predicate func(*Section) bool) int {
	var count int
	for range s.Filter(predicate) {
		count++
	}
	return count
}

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

	// Version is an optional section-specified value denoting an encoding
	// version of the section.
	Version uint32
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
	// ExtensionData returns optional encoded information about the section
	// stored at the file level, provided through the [SectionWriter]. Sections
	// can use this for retrieving critical information that must be known
	// without needing to read the metadata first.
	//
	// ExtensionData will be nil if no extension data is available.
	ExtensionData() []byte

	// DataRange opens a reader of length bytes from the data region of a
	// section. The offset argument determines where in the data region reading
	// should start.
	//
	// DataRange returns an error if the read fails or if offset+length goes
	// beyond the readable data region. The returned reader is only valid as long
	// as the provided ctx is not canceled.
	DataRange(ctx context.Context, offset, length int64) (io.ReadCloser, error)

	// MetadataRange opens a reader of length bytes from the metadata region of
	// a section. The offset argument determines where in the metadata region
	// reading should start.
	//
	// MetadataRange returns an error if the read fails or if offset+length goes
	// beyond the readable metadata region. The returned reader is only valid as long
	// as the provided ctx is not canceled.
	MetadataRange(ctx context.Context, offset, length int64) (io.ReadCloser, error)

	// DataSize returns the total size of the data region of a section. DataSize
	// returns 0 for sections with no data region.
	DataSize() int64

	// MetadataSize returns the total size of the metadata region of a section.
	// MetadataSize returns 0 for sections with no metadata region.
	MetadataSize() int64
}

// A SectionBuilder accumulates data for a single in-progress section.
//
// Each section package provides an implementation of SectionBuilder that
// includes utilities to buffer data into that section. Callers should use
// Bytes or EstimatedSize to determine when enough data has been accumulated
// into a section.
type SectionBuilder interface {
	// Type returns the SectionType representing the section being built.
	// Implementations are responsible for guaranteeing that two no
	// SectionBuilders return the same SectionType for different encodings.
	//
	// The returned Type is encoded directly into data objects. Implementations
	// that change SectionType values should be careful to continue supporting
	// old values for backwards compatibility.
	Type() SectionType

	// Flush encodes and flushes the section to w. Encodings that rely on byte
	// offsets should be relative to the first byte of the section's data.
	//
	// Flush returns the number of bytes written to w, and any error encountered
	// while encoding or flushing.
	//
	// After Flush is called, the SectionBuilder is reset to a fresh state and
	// can be reused.
	Flush(w SectionWriter) (n int64, err error)

	// Reset resets the SectionBuilder to a fresh state.
	Reset()
}

// SectionWriter writes data object sections to an underlying stream, such as a
// data object.
type SectionWriter interface {
	// WriteSection writes a section to the underlying data stream, partitioned
	// by section data and section metadata. It returns the sum of bytes written
	// from both input slices (0 <= n <= len(data)+len(metadata)) and any error
	// encountered that caused the write to stop early.
	//
	// The extensionData slice is an optional field for section information to
	// store at the file level. To minimize the cost of opening data objects,
	// sections should only use this field for information that's required to
	// start reading section metadata and to keep the payload as small as
	// possible. Since the extensionData field is written to the file-wide
	// metadata, its length does not impact the return value of n.
	//
	// Implementations of WriteSection:
	//
	//   - Must return an error if the write stops early.
	//   - Must not modify the slices passed to it, even temporarily.
	//   - Must not retain references to slices after WriteSection returns.
	//
	// The physical layout of data and metadata is not defined: they may be
	// written non-contiguously, interleaved, or in any order.
	WriteSection(data, metadata []byte, extensionData []byte) (n int64, err error)
}
