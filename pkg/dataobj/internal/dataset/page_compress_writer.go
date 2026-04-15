package dataset

import (
	"bufio"
	"bytes"
	"fmt"
	"io"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
)

// A compressWriter is a [streamio.Writer] that compresses data passed to it.
type compressWriter struct {
	// To be able to implmeent [io.ByteWriter], we always write directly to buf,
	// which then flushes to w once it's full.

	w   io.WriteCloser // Compressing writer.
	buf *bufio.Writer  // Buffered writer in front of w to be able to call WriteByte.

	rawBytes int // Number of uncompressed bytes written.

	compression datasetmd.CompressionType // Compression type being used.
	opts        *CompressionOptions       // Options to customize compression.
}

var (
	_ streamio.Writer = (*compressWriter)(nil)

	defaultCompressionOptions = new(CompressionOptions)
)

func newCompressWriter(w io.Writer, ty datasetmd.CompressionType, opts *CompressionOptions) *compressWriter {
	if opts == nil {
		// Use a default value in order to ensure that all compressWriters use the same shared zstd writer after init(), unless they explicitly pass different options.
		opts = defaultCompressionOptions
	}
	opts.init()

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
			if c.opts.zstdWriter == nil {
				panic("zstd compression requested but zstd writer is not initialized; Use CompressionOptions.init() to initialize it")
			}
			compressedWriter = &deferredZstdCompressor{
				inner:       w,
				zstdEncoder: c.opts.zstdWriter(),
				buf:         bytes.NewBuffer(nil),
			}

		default:
			panic(fmt.Sprintf("compressWriter.Reset: unknown compression type %v", c.compression))
		}

		c.w = compressedWriter
	}

	if c.buf != nil {
		c.buf.Reset(c.w)
	} else {
		c.buf = bufio.NewWriterSize(c.w, 256)
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

type deferredZstdCompressor struct {
	inner       io.Writer     // Writer to send compressed data to
	zstdEncoder *zstd.Encoder // Compressor
	buf         *bytes.Buffer // Buffered uncompressed data
}

func (c *deferredZstdCompressor) Write(p []byte) (int, error) {
	return c.buf.Write(p)
}

func (c *deferredZstdCompressor) Reset(w io.Writer) {
	c.inner = w
	c.buf.Reset()
}

func (c *deferredZstdCompressor) Flush() error {
	tmpBuf := bufpool.Get(c.buf.Len())
	defer bufpool.Put(tmpBuf)

	compressed := c.zstdEncoder.EncodeAll(c.buf.Bytes(), tmpBuf.Bytes())
	_, err := c.inner.Write(compressed)
	c.buf.Reset()
	return err
}

func (c *deferredZstdCompressor) Close() error {
	return nil
}
