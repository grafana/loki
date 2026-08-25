package buffer

import (
	"context"

	"github.com/grafana/loki/v3/pkg/memory"
)

// Sink stores buffer [Data] and returns handles to the stored buffers.
type Sink interface {
	// WriteBuffers stores each [Data] and returns handles to the corresponding
	// buffers. Returned IDs must be non-zero; ID 0 is reserved for the
	// [Invalid] buffer.
	//
	// Implementations must not store data beyond the call to WriteBuffers, or
	// modify it, even temporarily.
	WriteBuffers(ctx context.Context, data []Data) ([]ID, error)
}

// Source retrieves buffer [Data] from handles.
type Source interface {
	// ReadBuffers retrieves the read-only Data for the given buffer IDs. The
	// output elements match the order of the input bufs slice and have the same
	// length as bufs. Callers must not pass buffers with ID 0.
	//
	// ReadBuffers returns an error if any of the given buffers are not found.
	//
	// Implementations may use the provided allocator to allocate memory for
	// returned Data, but are not required to. Returned Data must remain valid
	// for at least the lifetime of alloc.
	ReadBuffers(ctx context.Context, alloc *memory.Allocator, bufs []ID) ([]Data, error)

	// BufferSizes returns the byte size of each requested buffer.
	//
	// BufferSizes returns an error if any of the given buffers are not found.
	BufferSizes(ctx context.Context, alloc *memory.Allocator, bufs []ID) ([]int64, error)
}

// Store combines Sink and Source into a single interface for read-write
// access to buffer data.
type Store interface {
	Sink
	Source

	// Delete removes the given buffers from the store.
	Delete(ctx context.Context, bufs []ID) error
}
