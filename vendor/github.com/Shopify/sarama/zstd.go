package sarama

import (
	"sync"

	"github.com/klauspost/compress/zstd"
)

// zstdMaxBufferedEncoders maximum number of not-in-use zstd encoders
// If the pool of encoders is exhausted then new encoders will be created on the fly
const zstdMaxBufferedEncoders = 1

type ZstdEncoderParams struct {
	Level int
}
type ZstdDecoderParams struct {
}

var zstdDecMap sync.Map

var zstdAvailableEncoders sync.Map

func getZstdEncoderChannel(params ZstdEncoderParams) chan *zstd.Encoder {
	if c, ok := zstdAvailableEncoders.Load(params); ok {
		return c.(chan *zstd.Encoder)
	}
	c, _ := zstdAvailableEncoders.LoadOrStore(params, make(chan *zstd.Encoder, zstdMaxBufferedEncoders))
	return c.(chan *zstd.Encoder)
}

func getZstdEncoder(params ZstdEncoderParams) *zstd.Encoder {
	select {
	case enc := <-getZstdEncoderChannel(params):
		return enc
	default:
		encoderLevel := zstd.SpeedDefault
		if params.Level != CompressionLevelDefault {
			encoderLevel = zstd.EncoderLevelFromZstd(params.Level)
		}
		zstdEnc, _ := zstd.NewWriter(nil, zstd.WithZeroFrames(true),
			zstd.WithEncoderLevel(encoderLevel),
			zstd.WithEncoderConcurrency(1))
		return zstdEnc
	}
}

func releaseEncoder(params ZstdEncoderParams, enc *zstd.Encoder) {
	select {
	case getZstdEncoderChannel(params) <- enc:
	default:
	}
}

func getDecoder(params ZstdDecoderParams) *zstd.Decoder {
	if ret, ok := zstdDecMap.Load(params); ok {
		return ret.(*zstd.Decoder)
	}
	// It's possible to race and create multiple new readers.
	// Only one will survive GC after use.
	zstdDec, _ := zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
	zstdDecMap.Store(params, zstdDec)
	return zstdDec
}

func zstdDecompress(params ZstdDecoderParams, dst, src []byte) ([]byte, error) {
	return getDecoder(params).DecodeAll(src, dst)
}

func zstdCompress(params ZstdEncoderParams, dst, src []byte) ([]byte, error) {
	enc := getZstdEncoder(params)
	out := enc.EncodeAll(src, dst)
	releaseEncoder(params, enc)
	return out, nil
}
