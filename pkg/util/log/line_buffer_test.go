package log

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

const (
	flushPeriod = 10 * time.Millisecond
	bufferSize  = 10e6
)

// BenchmarkLineBuffered creates line-buffered loggers of various capacities to see which perform best.
func BenchmarkLineBuffered(b *testing.B) {

	for i := 1; i <= 2048; i *= 2 {
		f := outFile(b)
		defer os.RemoveAll(f.Name())

		bufLog := NewLineBufferedLogger(f, uint32(i),
			WithFlushPeriod(flushPeriod),
			WithPrellocatedBuffer(bufferSize),
		)
		l := log.NewLogfmtLogger(bufLog)

		b.Run(fmt.Sprintf("capacity:%d", i), func(b *testing.B) {
			b.ReportAllocs()
			b.StartTimer()

			require.NoError(b, f.Truncate(0))

			logger := log.With(l, "common_key", "common_value")
			for j := 0; j < b.N; j++ {
				logger.Log("foo_key", "foo_value")
			}

			// force a final flush for outstanding lines in buffer
			bufLog.Flush()
			b.StopTimer()

			contents, err := os.ReadFile(f.Name())
			require.NoErrorf(b, err, "could not read test file: %s", f.Name())

			lines := strings.Split(string(contents), "\n")
			require.Equal(b, b.N, len(lines)-1)
		})
	}
}

// BenchmarkLineUnbuffered should perform roughly equivalently to a line-buffered logger with a capacity of 1.
func BenchmarkLineUnbuffered(b *testing.B) {
	b.ReportAllocs()

	f := outFile(b)
	defer os.RemoveAll(f.Name())

	l := log.NewLogfmtLogger(f)
	benchmarkRunner(b, l, baseMessage)

	b.StopTimer()

	contents, err := os.ReadFile(f.Name())
	require.NoErrorf(b, err, "could not read test file: %s", f.Name())

	lines := strings.Split(string(contents), "\n")
	require.Equal(b, b.N, len(lines)-1)
}

func BenchmarkLineDiscard(b *testing.B) {
	b.ReportAllocs()

	l := log.NewLogfmtLogger(io.Discard)
	benchmarkRunner(b, l, baseMessage)
}

func TestLineBufferedConcurrency(t *testing.T) {
	t.Parallel()
	bufLog := NewLineBufferedLogger(io.Discard, 32,
		WithFlushPeriod(flushPeriod),
		WithPrellocatedBuffer(bufferSize),
	)
	testConcurrency(t, log.NewLogfmtLogger(bufLog), 10000)
}

func TestOnFlushCallback(t *testing.T) {
	var (
		flushCount     uint32
		flushedEntries int
		buf            bytes.Buffer
	)

	callback := func(entries uint32) {
		flushCount++
		flushedEntries += int(entries)
	}

	bufLog := NewLineBufferedLogger(&buf, 2,
		WithFlushPeriod(flushPeriod),
		WithPrellocatedBuffer(bufferSize),
		WithFlushCallback(callback),
	)

	l := log.NewLogfmtLogger(bufLog)
	require.NoError(t, l.Log("line"))
	require.NoError(t, l.Log("line"))
	// first flush
	require.NoError(t, l.Log("line"))

	// force a second
	require.NoError(t, bufLog.Flush())

	require.Equal(t, uint32(2), flushCount)
	require.Equal(t, len(strings.Split(buf.String(), "\n"))-1, flushedEntries)
}

// outFile creates a real OS file for testing.
// We cannot use stdout/stderr since we need to read the contents afterwards to validate, and we have to write to a file
// to benchmark the impact of write() syscalls.
func outFile(b *testing.B) *os.File {
	f, err := os.CreateTemp(b.TempDir(), "linebuffer*")
	require.NoErrorf(b, err, "cannot create test file")

	return f
}

// Copied from go-kit/log
// These test are designed to be run with the race detector.

func testConcurrency(t *testing.T, logger log.Logger, total int) {
	n := int(math.Sqrt(float64(total)))
	share := total / n

	errC := make(chan error, n)

	for i := 0; i < n; i++ {
		go func() {
			errC <- spam(logger, share)
		}()
	}

	for i := 0; i < n; i++ {
		err := <-errC
		if err != nil {
			t.Fatalf("concurrent logging error: %v", err)
		}
	}
}

func spam(logger log.Logger, count int) error {
	for i := 0; i < count; i++ {
		err := logger.Log("key", i)
		if err != nil {
			return err
		}
	}
	return nil
}

func benchmarkRunner(b *testing.B, logger log.Logger, f func(log.Logger)) {
	lc := log.With(logger, "common_key", "common_value")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f(lc)
	}
}

var (
	baseMessage = func(logger log.Logger) { logger.Log("foo_key", "foo_value") }
)
