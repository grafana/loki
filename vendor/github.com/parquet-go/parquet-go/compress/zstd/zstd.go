// Package zstd implements the ZSTD parquet compression codec.
package zstd

import (
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/parquet-go/parquet-go/format"
)

type Level = zstd.EncoderLevel

const (
	// SpeedFastest will choose the fastest reasonable compression.
	// This is roughly equivalent to the fastest Zstandard mode.
	SpeedFastest = zstd.SpeedFastest

	// SpeedDefault is the default "pretty fast" compression option.
	// This is roughly equivalent to the default Zstandard mode (level 3).
	SpeedDefault = zstd.SpeedDefault

	// SpeedBetterCompression will yield better compression than the default.
	// Currently it is about zstd level 7-8 with ~ 2x-3x the default CPU usage.
	// By using this, notice that CPU usage may go up in the future.
	SpeedBetterCompression = zstd.SpeedBetterCompression

	// SpeedBestCompression will choose the best available compression option.
	// This will offer the best compression no matter the CPU cost.
	SpeedBestCompression = zstd.SpeedBestCompression
)

const (
	DefaultLevel = SpeedDefault

	DefaultConcurrency = 1
)

type Codec struct {
	Level Level

	// Concurrency is the number of CPU cores to use for encoding and decoding.
	// If Concurrency is 0, it will use DefaultConcurrency.
	Concurrency uint

	encoders sync.Pool // *zstd.Encoder
	decoders sync.Pool // *zstd.Decoder
}

func (c *Codec) String() string {
	return "ZSTD"
}

func (c *Codec) CompressionCodec() format.CompressionCodec {
	return format.Zstd
}

func (c *Codec) Encode(dst, src []byte) ([]byte, error) {
	e, _ := c.encoders.Get().(*zstd.Encoder)
	if e == nil {
		var err error
		e, err = zstd.NewWriter(nil,
			zstd.WithEncoderConcurrency(c.concurrency()),
			zstd.WithEncoderLevel(c.level()),
			zstd.WithZeroFrames(true),
			zstd.WithEncoderCRC(false),
		)
		if err != nil {
			return dst[:0], err
		}
	}
	defer c.encoders.Put(e)
	return e.EncodeAll(src, dst[:0]), nil
}

func (c *Codec) Decode(dst, src []byte) ([]byte, error) {
	d, _ := c.decoders.Get().(*zstd.Decoder)
	if d == nil {
		var err error
		d, err = zstd.NewReader(nil,
			zstd.WithDecoderConcurrency(c.concurrency()),
		)
		if err != nil {
			return dst[:0], err
		}
	}
	defer c.decoders.Put(d)
	return d.DecodeAll(src, dst[:0])
}

func (c *Codec) level() Level {
	if c.Level != 0 {
		return c.Level
	}
	return DefaultLevel
}

func (c *Codec) concurrency() int {
	if c.Concurrency != 0 {
		return int(c.Concurrency)
	}
	return DefaultConcurrency
}
