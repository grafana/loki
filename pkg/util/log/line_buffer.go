package log

import (
	"bytes"
	"io"
	"sync"
	"time"

	"go.uber.org/atomic"
)

// LineBufferedLogger buffers log lines to be flushed periodically. Without a line buffer, Log() will call the write
// syscall for every log line which is expensive if logging thousands of lines per second.
type LineBufferedLogger struct {
	buf     *threadsafeBuffer
	entries atomic.Uint32
	cap     uint32
	w       io.Writer

	onFlush func(entries uint32)
}

// Size returns the number of entries in the buffer.
func (l *LineBufferedLogger) Size() uint32 {
	return l.entries.Load()
}

// Write writes the given bytes to the line buffer, and increments the entries counter.
// If the buffer is full (entries == cap), it will be flushed, and the entries counter reset.
func (l *LineBufferedLogger) Write(p []byte) (n int, err error) {
	// when we've filled the buffer, flush it
	if l.Size() >= l.cap {
		// Flush resets the size to 0
		if err := l.Flush(); err != nil {
			l.buf.Reset()
			return 0, err
		}
	}

	l.entries.Inc()

	return l.buf.Write(p)
}

// Flush forces the buffer to be written to the underlying writer.
func (l *LineBufferedLogger) Flush() error {
	sz := l.Size()
	if sz <= 0 {
		return nil
	}

	// reset the counter
	l.entries.Store(0)

	// WriteTo() calls Reset() on the underlying buffer, so it's not needed here
	_, err := l.buf.WriteTo(l.w)

	// only call OnFlush callback if write was successful
	if err == nil && l.onFlush != nil {
		l.onFlush(sz)
	}

	return err
}

type LineBufferedLoggerOption func(*LineBufferedLogger)

// WithFlushPeriod creates a new LineBufferedLoggerOption that sets the flush period for the LineBufferedLogger.
func WithFlushPeriod(d time.Duration) LineBufferedLoggerOption {
	return func(l *LineBufferedLogger) {
		go func() {
			tick := time.NewTicker(d)
			defer tick.Stop()

			for range tick.C {
				l.Flush()
			}
		}()
	}
}

// WithFlushCallback allows for a callback function to be executed when Flush() is called.
// The length of the buffer at the time of the Flush() will be passed to the function.
func WithFlushCallback(fn func(entries uint32)) LineBufferedLoggerOption {
	return func(l *LineBufferedLogger) {
		l.onFlush = fn
	}
}

// WithPrellocatedBuffer preallocates a buffer to reduce GC cycles and slice resizing.
func WithPrellocatedBuffer(size uint32) LineBufferedLoggerOption {
	return func(l *LineBufferedLogger) {
		l.buf = newThreadsafeBuffer(bytes.NewBuffer(make([]byte, 0, size)))
	}
}

// NewLineBufferedLogger creates a new LineBufferedLogger with a configured capacity.
// Lines are flushed when the context is done, the buffer is full, or the flush period is reached.
func NewLineBufferedLogger(w io.Writer, cap uint32, opts ...LineBufferedLoggerOption) *LineBufferedLogger {
	l := &LineBufferedLogger{
		w:   w,
		buf: newThreadsafeBuffer(bytes.NewBuffer([]byte{})),
		cap: cap,
	}

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// threadsafeBuffer wraps the non-threadsafe bytes.Buffer.
type threadsafeBuffer struct {
	sync.RWMutex

	buf *bytes.Buffer
}

// Read returns the contents of the buffer.
func (t *threadsafeBuffer) Read(p []byte) (n int, err error) {
	t.RLock()
	defer t.RUnlock()

	return t.buf.Read(p)
}

// Write writes the given bytes to the underlying writer.
func (t *threadsafeBuffer) Write(p []byte) (n int, err error) {
	t.Lock()
	defer t.Unlock()

	return t.buf.Write(p)
}

// WriteTo writes the buffered lines to the given writer.
func (t *threadsafeBuffer) WriteTo(w io.Writer) (n int64, err error) {
	t.Lock()
	defer t.Unlock()

	return t.buf.WriteTo(w)
}

// Reset resets the buffer to be empty,
// but it retains the underlying storage for use by future writes.
// Reset is the same as Truncate(0).
func (t *threadsafeBuffer) Reset() {
	t.Lock()
	defer t.Unlock()

	t.buf.Reset()
}

// newThreadsafeBuffer returns a new threadsafeBuffer wrapping the given bytes.Buffer.
func newThreadsafeBuffer(buf *bytes.Buffer) *threadsafeBuffer {
	return &threadsafeBuffer{
		buf: buf,
	}
}
