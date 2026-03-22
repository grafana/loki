// Package gzip implements the GZIP parquet compression codec.
package gzip

import (
	"io"
	"strings"

	"github.com/klauspost/compress/gzip"
	"github.com/parquet-go/parquet-go/compress"
	"github.com/parquet-go/parquet-go/format"
)

const (
	emptyGzip = "\x1f\x8b\b\x00\x00\x00\x00\x00\x02\xff\x01\x00\x00\xff\xff\x00\x00\x00\x00\x00\x00\x00\x00"
)

const (
	NoCompression      = gzip.NoCompression
	BestSpeed          = gzip.BestSpeed
	BestCompression    = gzip.BestCompression
	DefaultCompression = gzip.DefaultCompression
	HuffmanOnly        = gzip.HuffmanOnly
)

type Codec struct {
	Level int

	r compress.Decompressor
	w compress.Compressor
}

func (c *Codec) String() string {
	return "GZIP"
}

func (c *Codec) CompressionCodec() format.CompressionCodec {
	return format.Gzip
}

func (c *Codec) Encode(dst, src []byte) ([]byte, error) {
	return c.w.Encode(dst, src, func(w io.Writer) (compress.Writer, error) {
		return gzip.NewWriterLevel(w, c.Level)
	})
}

func (c *Codec) Decode(dst, src []byte) ([]byte, error) {
	return c.r.Decode(dst, src, func(r io.Reader) (compress.Reader, error) {
		z, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		return &reader{Reader: z}, nil
	})
}

type reader struct {
	*gzip.Reader
	emptyGzip strings.Reader
}

func (r *reader) Reset(rr io.Reader) error {
	if rr == nil {
		r.emptyGzip.Reset(emptyGzip)
		rr = &r.emptyGzip
	}
	return r.Reader.Reset(rr)
}
