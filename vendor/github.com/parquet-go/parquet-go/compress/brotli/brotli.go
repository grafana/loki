// Package brotli implements the BROTLI parquet compression codec.
package brotli

import (
	"io"

	"github.com/andybalholm/brotli"
	"github.com/parquet-go/parquet-go/compress"
	"github.com/parquet-go/parquet-go/format"
)

const (
	DefaultQuality = 0
	DefaultLGWin   = 0
)

type Codec struct {
	// Quality controls the compression-speed vs compression-density trade-offs.
	// The higher the quality, the slower the compression. Range is 0 to 11.
	Quality int
	// LGWin is the base 2 logarithm of the sliding window size.
	// Range is 10 to 24. 0 indicates automatic configuration based on Quality.
	LGWin int

	r compress.Decompressor
	w compress.Compressor
}

func (c *Codec) String() string {
	return "BROTLI"
}

func (c *Codec) CompressionCodec() format.CompressionCodec {
	return format.Brotli
}

func (c *Codec) Encode(dst, src []byte) ([]byte, error) {
	return c.w.Encode(dst, src, func(w io.Writer) (compress.Writer, error) {
		return brotli.NewWriterOptions(w, brotli.WriterOptions{
			Quality: c.Quality,
			LGWin:   c.LGWin,
		}), nil
	})
}

func (c *Codec) Decode(dst, src []byte) ([]byte, error) {
	return c.r.Decode(dst, src, func(r io.Reader) (compress.Reader, error) {
		return reader{brotli.NewReader(r)}, nil
	})
}

type reader struct{ *brotli.Reader }

func (reader) Close() error { return nil }
