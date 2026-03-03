package uv

import (
	"fmt"
	"time"
)

// pollReader reads data from an [io.Reader] using different native poll APIs
// depending on the operating system.
//
// On Linux, it uses the epoll API.
// On Windows, it uses The Windows I/O and Console APIs.
// On macOS and other BSD-based systems, it will try to use the kqueue API and
// fall back to Unix select if kqueue is not available (e.g., on TTY).
// On other Unix-like systems, it uses the select API.
// On all other systems, it falls back to a simple read loop with a timeout.
type pollReader interface {
	// Read reads data from the underlying [io.Reader]. It blocks until data is
	// available or an error occurs.
	//
	// Use [pollReader] to check for data availability before calling Read to
	// avoid blocking.
	Read(p []byte) (n int, err error)

	// Poll notifies when data is available to read with the given timeout. Use
	// a negative duration to wait indefinitely.
	Poll(timeout time.Duration) (ready bool, err error)

	// Cancel cancels any ongoing poll or read operations. It returns true if
	// an operation was canceled, false otherwise.
	Cancel() bool

	// Close closes the reader and releases any resources associated with it.
	Close() error
}

// ErrCanceled is returned when a poll or read operation is canceled.
var ErrCanceled = fmt.Errorf("poll canceled")
