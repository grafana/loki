package array

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataset/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Sink is the interface for storing BufferData.
type Sink interface {
	// WriteBuffers stores BufferData and returns handles to the corresponding
	// Buffers.
	//
	// Implementations must not store data beyond the call to WriteBuffers, or
	// modify it, even temporarily.
	WriteBuffers(ctx context.Context, data []BufferData) ([]Buffer, error)
}

// A Writer accumulates data into an [Array].
type Writer interface {
	// Append appends the data from arr to the Writer. Append returns an error
	// if the data is invalid for the Writer.
	//
	// Implementations must not retain references to arr beyond the call to
	// Append.
	Append(arr columnar.Array) error

	// Flush flushes buffered data and returns a new [Array] that represents the
	// encoded data.
	//
	// After Flush is called, the Writer is reset and ready to accept new data.
	Flush(ctx context.Context, sink Sink) (Array, error)
}

// NewWriter creates a [Writer] to build an Array.
//
// The provided alloc may used to allocate memory that is used across the entire
// Writer's lifetime. Callers must ensure that the allocator is valid for the
// entire lifetime of the Writer.
//
// NewWriter returns an error if spec is invalid, or if spec and typ are not
// compatible.
func NewWriter(alloc *memory.Allocator, spec Spec, typ types.Type) (Writer, error) {
	switch spec.Kind() {
	case EncodingKindBool:
		return newBoolWriter(alloc, spec, typ)
	case EncodingKindPlain:
		return newPlainWriter(alloc, spec, typ)

	default:
		return nil, fmt.Errorf("unsupported encoding kind %q", spec.Kind())
	}
}

// Source is the interface for retrieving BufferData.
type Source interface {
	// ReadBuffers retrieves the read-only BufferData for the given Buffers. The
	// output elements match the order of the input bufs slice.
	//
	// Implementations may use the allocator to allocate memory to store
	// BufferData, but are not required to (such as when the buffer data is
	// already cached in memory).
	//
	// Callers must assume that the returned BufferData shares the same lifetime
	// as alloc.
	ReadBuffers(ctx context.Context, alloc *memory.Allocator, bufs []Buffer) ([]BufferData, error)
}

// A Reader reads data for an [Array].
type Reader interface {
	// Read returns array data of up to the next count values. At the end of the
	// data, Read returns nil, io.EOF.
	//
	// If there was an error reading the page, Read returns the error with
	// no array.
	//
	// Implementations may use the provided allocator to allocate memory for the
	// returned data, but are not required to (such as when the data is already
	// cached in memory).
	//
	// Callers must assume that the returned [columnar.Array] lives for at least
	// as long as alloc and no longer than the lifetime of the Reader.
	Read(ctx context.Context, alloc *memory.Allocator, count int) (columnar.Array, error)

	// Close closes the Reader and releases any resources it holds.
	Close() error
}

// NewReader creates a [Reader] for an Array. The provided source is used to
// retrieve buffer data for the array.
//
// The provided alloc may used to allocate memory that is used across the entire
// Reader's lifetime. Callers must ensure that the allocator is valid for the
// entire lifetime of the Reader.
//
// NewReader returns an error if Array specifies an invalid encoding and type
// pair.
func NewReader(alloc *memory.Allocator, arr Array, source Source) (Reader, error) {
	switch arr.Encoding.Kind() {
	case EncodingKindBool:
		return newBoolReader(alloc, arr, source)
	case EncodingKindPlain:
		return newPlainReader(alloc, arr, source)

	default:
		return nil, fmt.Errorf("unsupported encoding kind %q", arr.Encoding.Kind())
	}
}
