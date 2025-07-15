package parquet

import (
	"fmt"

	"github.com/parquet-go/parquet-go/compress"
	"github.com/parquet-go/parquet-go/compress/brotli"
	"github.com/parquet-go/parquet-go/compress/gzip"
	"github.com/parquet-go/parquet-go/compress/lz4"
	"github.com/parquet-go/parquet-go/compress/snappy"
	"github.com/parquet-go/parquet-go/compress/uncompressed"
	"github.com/parquet-go/parquet-go/compress/zstd"
	"github.com/parquet-go/parquet-go/format"
)

var (
	// Uncompressed is a parquet compression codec representing uncompressed
	// pages.
	Uncompressed uncompressed.Codec

	// Snappy is the SNAPPY parquet compression codec.
	Snappy snappy.Codec

	// Gzip is the GZIP parquet compression codec.
	Gzip = gzip.Codec{
		Level: gzip.DefaultCompression,
	}

	// Brotli is the BROTLI parquet compression codec.
	Brotli = brotli.Codec{
		Quality: brotli.DefaultQuality,
		LGWin:   brotli.DefaultLGWin,
	}

	// Zstd is the ZSTD parquet compression codec.
	Zstd = zstd.Codec{
		Level: zstd.DefaultLevel,
	}

	// Lz4Raw is the LZ4_RAW parquet compression codec.
	Lz4Raw = lz4.Codec{
		Level: lz4.DefaultLevel,
	}

	// Table of compression codecs indexed by their code in the parquet format.
	compressionCodecs = [...]compress.Codec{
		format.Uncompressed: &Uncompressed,
		format.Snappy:       &Snappy,
		format.Gzip:         &Gzip,
		format.Brotli:       &Brotli,
		format.Zstd:         &Zstd,
		format.Lz4Raw:       &Lz4Raw,
	}
)

// LookupCompressionCodec returns the compression codec associated with the
// given code.
//
// The function never returns nil. If the encoding is not supported,
// an "unsupported" codec is returned.
func LookupCompressionCodec(codec format.CompressionCodec) compress.Codec {
	if codec >= 0 && int(codec) < len(compressionCodecs) {
		if c := compressionCodecs[codec]; c != nil {
			return c
		}
	}
	return &unsupported{codec}
}

type unsupported struct {
	codec format.CompressionCodec
}

func (u *unsupported) String() string {
	return "UNSUPPORTED"
}

func (u *unsupported) CompressionCodec() format.CompressionCodec {
	return u.codec
}

func (u *unsupported) Encode(dst, src []byte) ([]byte, error) {
	return dst[:0], u.error()
}

func (u *unsupported) Decode(dst, src []byte) ([]byte, error) {
	return dst[:0], u.error()
}

func (u *unsupported) error() error {
	return fmt.Errorf("unsupported compression codec: %s", u.codec)
}

func isCompressed(c compress.Codec) bool {
	return c != nil && c.CompressionCodec() != format.Uncompressed
}
