// Package encoding provides utilities for encoding and decoding data objects.
package encoding

import "errors"

var (
	magic = []byte("THOR")
)

const (
	fileFormatVersion = 0x1
)

var (
	ErrElementExist = errors.New("open element already exists")
	ErrClosed       = errors.New("element is closed")

	// errElementNoExist is used when a child element tries to notify its parent
	// of it closing but the parent doesn't have a child open. This would
	// indicate a bug in the encoder so it's not exposed to callers.
	errElementNoExist = errors.New("open element does not exist")
)

// SectionWriter writes data object sections to an underlying stream, such as a
// data object.
type SectionWriter interface {
	// WriteSection writes a section to the underlying data stream, partitioned
	// by section data and section metadata. It returns the sum of bytes written
	// from both input slices (0 <= n <= len(data)+len(metadata)) and any error
	// encountered that caused the write to stop early.
	//
	// Implementations of WriteSection:
	//
	//   - Must return an error if the write stops early.
	//   - Must not modify the slices passed to it, even temporarily.
	//   - Must not retain references to slices after WriteSection returns.
	//
	// The physical layout of data and metadata is not defined: they may be
	// written non-contiguously, interleaved, or in any order.
	WriteSection(data, metadata []byte) (n int64, err error)
}
