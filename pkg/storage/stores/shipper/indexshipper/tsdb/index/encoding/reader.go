// SPDX-License-Identifier: AGPL-3.0-only
// Copied from: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/reader.go

package encoding

// BufReader provides the low-level byte access interface for Decbuf's read operations
type BufReader interface {
	// Reset moves the cursor to the beginning of the data segment owned by the reader,
	// at the base offset configured at reader initialization.
	Reset() error

	// ResetAt moves the cursor to the given offset in the data segment owned by the reader,
	// relative to the base offset configured at reader initialization.
	// CAUTION: This operation may be very expensive and result in the discard of buffered data.
	// Use Skip to move forward to avoid unnecessary buffer discard.
	// ResetAt should only be used to move backwards.
	//
	// Attempting to ResetAt to the end of the data segment is valid.
	// Attempting to ResetAt _beyond_ the end of the data segment will return an error.
	ResetAt(off int) error

	// Skip advances the cursor by the given number of bytes in the data segment.
	// Attempting to skip to the end of the data segment is valid.
	// Attempting to skip _beyond_ the end of the data segment will return an error.
	Skip(l int) error

	// Peek returns at most the given number of bytes from the data segment, without consuming them.
	// The byte slice returned becomes invalid at the next read.
	// It is valid to Peek beyond the end of the data segment;
	// in this case implementations MUST return the available bytes up to the end and a nil error.
	Peek(n int) ([]byte, error)

	// Read returns the given number of bytes from the data segment, consuming them.
	// It is NOT valid to read beyond the end of the data segment;
	// in this case implementations MUST return a nil byte slice and an ErrInvalidSize error,
	// and the remaining bytes MUST be consumed.
	Read(n int) ([]byte, error)

	// ReadInto reads len(b) bytes from the data segment into b, consuming them.
	// It is NOT valid to read beyond the end of the data segment;
	// in this case implementations MUST return a nil byte slice and an ErrInvalidSize error,
	// and the remaining bytes MUST be consumed.
	ReadInto(b []byte) error

	// Size returns the length of the underlying buffer in bytes.
	Size() int

	// Len returns the remaining number of bytes in the data segment owned by the reader,
	// from the current offset to the length configured at reader initialization.
	Len() int

	// Offset returns the cursor offset in the data segment owned by the reader,
	// relative to the base offset configured at reader initialization.
	Offset() int

	// Buffered returns the number of bytes that can be read from the reader which are already in memory.
	Buffered() int

	Close() error
}
