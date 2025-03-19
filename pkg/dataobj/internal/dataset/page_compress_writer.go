package dataset

import (
	"bufio"
	"fmt"
	"io"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

// A compressWriter is a [streamio.Writer] that compresses data passed to it.
type compressWriter struct {
	// To be able to implmeent [io.ByteWriter], we always write directly to buf,
	// which then flushes to w once it's full.

	w   io.WriteCloser // Compressing writer.
	buf *bufio.Writer  // Buffered writer in front of w to be able to call WriteByte.

	rawBytes int // Number of uncompressed bytes written.

	compression datasetmd.CompressionType // Compression type being used.
	opts        CompressionOptions        // Options to customize compression.
}

var _ streamio.Writer = (*compressWriter)(nil)

func newCompressWriter(w io.Writer, ty datasetmd.CompressionType, opts CompressionOptions) *compressWriter {
	c := compressWriter{compression: ty, opts: opts}
	c.Reset(w)
	return &c
}

// Write writes p to c.
func (c *compressWriter) Write(p []byte) (n int, err error) {
	n, err = c.buf.Write(p)
	c.rawBytes += n
	return
}

// WriteByte writes a single byte to c.
func (c *compressWriter) WriteByte(b byte) error {
	if err := c.buf.WriteByte(b); err != nil {
		return err
	}
	c.rawBytes++
	return nil
}

// Flush compresses any pending uncompressed data in the buffer.
func (c *compressWriter) Flush() error {
	// Flush our buffer first so c.w is up to date.
	if err := c.buf.Flush(); err != nil {
		return fmt.Errorf("flushing buffer: %w", err)
	}

	// c.w may not support Flush (such as when using no compression), so we check
	// first.
	if f, ok := c.w.(interface{ Flush() error }); ok {
		if err := f.Flush(); err != nil {
			return fmt.Errorf("flushing compressing writer: %w", err)
		}
	}

	return nil
}

// Reset discards the writer's state and switches the compressor to write to w.
// This permits reusing a compressWriter rather than allocating a new one.
func (c *compressWriter) Reset(w io.Writer) {
	resetter, ok := c.w.(interface{ Reset(io.Writer) })
	switch ok {
	case true:
		resetter.Reset(w)
	default:
		// c.w is unset or doesn't support Reset; build a new writer.
		var compressedWriter io.WriteCloser

		switch c.compression {
		case datasetmd.COMPRESSION_TYPE_UNSPECIFIED, datasetmd.COMPRESSION_TYPE_NONE:
			compressedWriter = nopCloseWriter{w}

		case datasetmd.COMPRESSION_TYPE_SNAPPY:
			compressedWriter = snappy.NewBufferedWriter(w)

		case datasetmd.COMPRESSION_TYPE_ZSTD:
			zw, err := zstd.NewWriter(w, c.opts.Zstd...)
			if err != nil {
				panic(fmt.Sprintf("compressWriter.Reset: creating zstd writer: %v", err))
			}
			compressedWriter = zw

		default:
			panic(fmt.Sprintf("compressWriter.Reset: unknown compression type %v", c.compression))
		}

		c.w = compressedWriter
	}

	if c.buf != nil {
		c.buf.Reset(c.w)
	} else {
		c.buf = bufio.NewWriter(c.w)
	}
	c.rawBytes = 0
}

// BytesWritten returns the number of uncompressed bytes written to c.
func (c *compressWriter) BytesWritten() int { return c.rawBytes }

// Close flushes and then closes c.
func (c *compressWriter) Close() error {
	if err := c.Flush(); err != nil {
		return err
	}
	return c.w.Close()
}

type nopCloseWriter struct{ w io.Writer }

func (w nopCloseWriter) Write(p []byte) (n int, err error) { return w.w.Write(p) }
func (w nopCloseWriter) Close() error                      { return nil }
