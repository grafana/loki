// Package lz4 implements the LZ4_RAW parquet compression codec.
package lz4

import (
	"github.com/parquet-go/parquet-go/format"
	"github.com/pierrec/lz4/v4"
)

type Level = lz4.CompressionLevel

const (
	Fastest = lz4.CompressionLevel(99)
	Fast    = lz4.Fast
	Level1  = lz4.Level1
	Level2  = lz4.Level2
	Level3  = lz4.Level3
	Level4  = lz4.Level4
	Level5  = lz4.Level5
	Level6  = lz4.Level6
	Level7  = lz4.Level7
	Level8  = lz4.Level8
	Level9  = lz4.Level9
)

const (
	DefaultLevel = Fast
)

type Codec struct {
	Level Level
}

func (c *Codec) String() string {
	return "LZ4_RAW"
}

func (c *Codec) CompressionCodec() format.CompressionCodec {
	return format.Lz4Raw
}

func (c *Codec) Encode(dst, src []byte) ([]byte, error) {
	dst = reserveAtLeast(dst, lz4.CompressBlockBound(len(src)))

	var (
		n   int
		err error
	)
	if c.Level == Fastest {
		compressor := lz4.Compressor{}
		n, err = compressor.CompressBlock(src, dst)
	} else {
		compressor := lz4.CompressorHC{Level: c.Level}
		n, err = compressor.CompressBlock(src, dst)
	}
	return dst[:n], err
}

func (c *Codec) Decode(dst, src []byte) ([]byte, error) {
	// 3x seems like a common compression ratio, so we optimistically size the
	// output buffer to that size. Feel free to change the value if you observe
	// different behaviors.
	dst = reserveAtLeast(dst, 3*len(src))

	for {
		n, err := lz4.UncompressBlock(src, dst)
		// The lz4 package does not expose the error values, they are declared
		// in internal/lz4errors. Based on what I read of the implementation,
		// the only condition where this function errors is if the output buffer
		// was too short.
		//
		// https://github.com/pierrec/lz4/blob/a5532e5996ee86d17f8ce2694c08fb5bf3c6b471/internal/lz4block/block.go#L45-L53
		if err != nil {
			dst = make([]byte, 2*len(dst))
		} else {
			return dst[:n], nil
		}
	}
}

func reserveAtLeast(b []byte, n int) []byte {
	if cap(b) < n {
		b = make([]byte, n)
	} else {
		b = b[:cap(b)]
	}
	return b
}
