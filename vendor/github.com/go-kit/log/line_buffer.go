package log

import (
	"bytes"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// LineBufferedLogger buffers log lines to be flushed periodically. Without a line buffer, Log() will call the write
// syscall for every log line which is expensive if logging thousands of lines per second.
type LineBufferedLogger struct {
	buf     *threadsafeBuffer
	entries uint32
	cap     uint32
	w       io.Writer
}

// Size returns the number of entries in the buffer.
func (l *LineBufferedLogger) Size() uint32 {
	return atomic.LoadUint32(&l.entries)
}

// Write writes the given bytes to the line buffer, and increments the entries counter.
// If the buffer is full (entries == cap), it will be flushed, and the entries counter reset.
func (l *LineBufferedLogger) Write(p []byte) (n int, err error) {
	// when we've filled the buffer, flush it
	if l.Size() == l.cap {
		// Flush resets the size to 0
		if err := l.Flush(); err != nil {
			return 0, err
		}
	}

	atomic.AddUint32(&l.entries, 1)

	return l.buf.Write(p)
}

// Flush forces the buffer to be written to the underlying writer.
func (l *LineBufferedLogger) Flush() error {
	if l.Size() <= 0 {
		return nil
	}

	// reset the counter
	atomic.StoreUint32(&l.entries, 0)

	_, err := l.buf.WriteTo(l.w)
	return err
}

// NewLineBufferedLogger creates a new LineBufferedLogger with a configured capacity and flush period.
// Lines are flushed when the context is done, the buffer is full, or the flush period is reached.
func NewLineBufferedLogger(w io.Writer, cap uint32, flushPeriod time.Duration) *LineBufferedLogger {
	l := &LineBufferedLogger{
		w:   w,
		buf: newThreadsafeBuffer(bytes.NewBuffer([]byte{})),
		cap: cap,
	}

	// flush periodically
	go func() {
		tick := time.NewTicker(flushPeriod)
		defer tick.Stop()

		for {
			select {
			case <-tick.C:
				l.Flush()
			}
		}

	}()

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

// newThreadsafeBuffer returns a new threadsafeBuffer wrapping the given bytes.Buffer.
func newThreadsafeBuffer(buf *bytes.Buffer) *threadsafeBuffer {
	return &threadsafeBuffer{
		buf: buf,
	}
}
