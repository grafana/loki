package pool

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"time"
)

// ContextDeadlineError preserves the socket timeout for connection lifecycle decisions.
type ContextDeadlineError struct {
	original error
	timeout  net.Error
	cause    error
}

func (e *ContextDeadlineError) Error() string {
	return e.cause.Error() + ": " + e.original.Error()
}

func (e *ContextDeadlineError) Unwrap() error        { return e.original }
func (e *ContextDeadlineError) Is(target error) bool { return target == e.cause }

func (e *ContextDeadlineError) Timeout() bool { return e.timeout.Timeout() }

// Temporary is declared so the error still satisfies net.Error.
func (e *ContextDeadlineError) Temporary() bool { return false }

type deadlineReader struct {
	reader   io.Reader
	ctx      context.Context
	deadline time.Time
	// readWithError is set when a read returned bytes together with an error: bufio
	// then caches that error while it still hands the bytes to the caller. Only such
	// a read needs the command boundary to drop the cached error (see WithReader).
	readWithError atomic.Bool
}

func (r *deadlineReader) Read(b []byte) (int, error) {
	n, err := r.reader.Read(b)
	if n > 0 && err != nil {
		r.readWithError.Store(true)
	}
	return n, contextDeadlineError(r.ctx, r.deadline, err)
}

// takeReadWithError reports whether a read returned bytes together with an error,
// clearing the flag. False on the common path keeps WithReader to one atomic load.
func (r *deadlineReader) takeReadWithError() bool {
	return r.readWithError.Swap(false)
}

// Only translate socket timeouts when the context supplied the applied deadline.
func contextDeadlineError(ctx context.Context, deadline time.Time, err error) error {
	if err == nil || ctx == nil || deadline.IsZero() {
		return err
	}
	ctxDeadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(ctxDeadline) {
		return err
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		return err
	}
	ctxErr := ctx.Err()
	if ctxErr == nil {
		// The socket timer can fire before the context timer publishes its error.
		// The wrap still only applies to a deadline this context supplied: an
		// independent transport timeout that fires this early fails the connection,
		// which the pool sees as a redial rather than a retry (#4305).
		ctxErr = context.DeadlineExceeded
	}
	return &ContextDeadlineError{original: err, timeout: netErr, cause: ctxErr}
}
