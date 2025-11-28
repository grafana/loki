package dataset

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

// A compressWriter is a [streamio.Writer] that compresses data passed to it.
// The compression encoder is created lazily on the first Flush() call.
type compressWriter struct {
	// To be able to implement [io.ByteWriter], we always write directly to buf,
	// which then writes to inner once the writer is flushed.

	inner io.Writer     // The underlying writer (before compression).
	buf   *bytes.Buffer // buffer for uncompressed data before it is written to the compressed writer

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
// On the first call, this creates the compression encoder.
func (c *compressWriter) Flush() error {
	if err := c.writeToInner(); err != nil {
		return fmt.Errorf("flush to inner writer: %w", err)
	}

	return nil
}

// writeToInner creates the compression encoder and transitions from buffering
// to tempBuf to writing directly to the encoder.
func (c *compressWriter) writeToInner() error {
	// Create the compression encoder
	var cw io.WriteCloser

	switch c.compression {
	case datasetmd.COMPRESSION_TYPE_UNSPECIFIED, datasetmd.COMPRESSION_TYPE_NONE:
		cw = nopCloseWriter{c.inner}
		defer cw.Close()

	case datasetmd.COMPRESSION_TYPE_SNAPPY:
		cw = snappy.NewBufferedWriter(c.inner)
		defer cw.Close()

	case datasetmd.COMPRESSION_TYPE_ZSTD:
		zw, err := zstdPool.Get(c.inner, c.opts.Zstd)
		if err != nil {
			return err
		}
		defer zstdPool.Put(zw, c.opts.Zstd)
		cw = zw

	default:
		return fmt.Errorf("unknown compression type %v", c.compression)
	}

	// Write the buffered data from buf via cw to inner
	if c.buf.Len() > 0 {
		if _, err := c.buf.WriteTo(cw); err != nil {
			return fmt.Errorf("writing buffered data to encoder: %w", err)
		}
	}

	// cw may not support Flush (such as when using no compression), so we check first.
	if flusher, ok := cw.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			return fmt.Errorf("flushing compressing writer: %w", err)
		}
	}

	return nil
}

// Reset discards the writer's state and switches the compressor to write to w.
// This permits reusing a compressWriter rather than allocating a new one.
func (c *compressWriter) Reset(w io.Writer) {
	// Store the underlying writer
	c.inner = w

	// Initialize or reset temporary buffer
	if c.buf != nil {
		c.buf.Reset()
	} else {
		c.buf = bytes.NewBuffer(make([]byte, 0, 4096))
	}

	c.rawBytes = 0
}

// BytesWritten returns the number of uncompressed bytes written to c.
func (c *compressWriter) BytesWritten() int { return c.rawBytes }

// Close flushes and then closes c.
func (c *compressWriter) Close() error {
	return c.Flush()
}

type nopCloseWriter struct{ w io.Writer }

func (w nopCloseWriter) Write(p []byte) (n int, err error) { return w.w.Write(p) }
func (w nopCloseWriter) Close() error                      { return nil }

var (
	zstdPool = zstdWriterPool{pools: make(map[zstd.EncoderLevel]*sync.Pool)}
)

type zstdWriterPool struct {
	pools map[zstd.EncoderLevel]*sync.Pool
}

func (p *zstdWriterPool) new(w io.Writer, l zstd.EncoderLevel) (*zstd.Encoder, error) {
	writer, err := zstd.NewWriter(w, zstd.WithEncoderLevel(l))
	if err != nil {
		return nil, fmt.Errorf("creating zstd writer: %w", err)
	}
	return writer, nil
}

// Get gets or creates a new [zstd.Encoder] for encoding level l and reset it to write to w
func (p *zstdWriterPool) Get(w io.Writer, l zstd.EncoderLevel) (*zstd.Encoder, error) {
	l = max(l, zstd.SpeedDefault)
	pool, ok := p.pools[l]
	if !ok {
		p.pools[l] = new(sync.Pool)
		return p.new(w, l)
	}

	if zw := pool.Get(); zw != nil {
		writer := zw.(*zstd.Encoder)
		writer.Reset(w)
		return writer, nil
	}

	return p.new(w, l)
}

// Put places the zstd encoder w back to the pool for level l
// This call implicitly closes and resets the [zstd.Encoder].
func (p *zstdWriterPool) Put(w *zstd.Encoder, l zstd.EncoderLevel) {
	l = max(l, zstd.SpeedDefault)
	w.Close()
	w.Reset(nil)
	p.pools[l].Put(w)
}
