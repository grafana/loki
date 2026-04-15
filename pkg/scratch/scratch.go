// Package scratch provides an abstraction for scratch space.
package scratch

import (
	"fmt"
	"io"

	"github.com/go-kit/log"
)

// HandleNotFoundError is returned when performing an operation on a handle that
// doesn't exist.
type HandleNotFoundError Handle

// Error returns a string representation of e.
func (e HandleNotFoundError) Error() string {
	return fmt.Sprintf("handle %d not found", uint64(e))
}

// Handle is a identifier for data placed into scratch space. Handles are unique
// per store. Handles are immutable once created and can only be read or
// deleted.
//
// The zero value for handle is invalid.
type Handle uint64

// InvalidHandle is the zero value for Handle.
const InvalidHandle = Handle(0)

// Store is an abstraction for temporarily storing data. Store is ephemeral and
// starts fresh every time it is created.
//
// Store implementations must be safe for concurrent access.
type Store interface {
	// Ideally Store would support some kind of streaming
	//
	//   PutReader(r io.Reader) (Handle, error)
	//
	// method, but it's tricky to implement when considering [Filesystem]s
	// fallback mechanism, which naively would require buffering the entire data
	// into memory anyway (for passing to the fallback if the write to disk
	// fails).
	//
	// It may be possible to implement efficiently if we buffered the most
	// recent written chunk; then falling back could be implemented by replaying
	// what _did_ get written to disk, followed by the chunk that failed, and
	// then all other data. However, this is more complicated than it's worth
	// right now.
	//
	// This can be revisited if there's a growing need for streaming data to the
	// Store without needing to buffer it all into memory first.

	// Put stores the contents of p into scratch space.
	Put(p []byte) Handle

	// Read returns a reader for the file identifier by h. Read returns
	// [InvalidHandleError] if h doesn't exist in the Store.
	//
	// Callers must close the reader when done to release resources. Reading may
	// fail if the handle is removed while reading data.
	Read(h Handle) (io.ReadSeekCloser, error)

	// Remove removes the handle identified by h. Remove returns
	// [InvalidHandleError] if h doesn't exist in the Store.
	Remove(h Handle) error
}

// Open returns the default [Store] implementation based on whether path is set.
//
// If path is empty, Open returns a [Memory] store. If path is non-empty, Open
// returns a [Filesystem] store.
//
// Open returns an error if the path is non-empty but refers to a non-existent
// directory.
//
// To observe metrics on the returned store, wrap the result with
// [ObserveStore].
func Open(logger log.Logger, path string) (Store, error) {
	if path == "" {
		return NewMemory(), nil
	}

	return NewFilesystem(logger, path)
}
