package sarama

import (
	"sync"

	"github.com/klauspost/compress/zstd"
)

type ZstdEncoderParams struct {
	Level int
}
type ZstdDecoderParams struct {
}

var zstdEncMap, zstdDecMap sync.Map

func getEncoder(params ZstdEncoderParams) *zstd.Encoder {
	if ret, ok := zstdEncMap.Load(params); ok {
		return ret.(*zstd.Encoder)
	}
	// It's possible to race and create multiple new writers.
	// Only one will survive GC after use.
	encoderLevel := zstd.SpeedDefault
	if params.Level != CompressionLevelDefault {
		encoderLevel = zstd.EncoderLevelFromZstd(params.Level)
	}
	zstdEnc, _ := zstd.NewWriter(nil, zstd.WithZeroFrames(true),
		zstd.WithEncoderLevel(encoderLevel))
	zstdEncMap.Store(params, zstdEnc)
	return zstdEnc
}

func getDecoder(params ZstdDecoderParams) *zstd.Decoder {
	if ret, ok := zstdDecMap.Load(params); ok {
		return ret.(*zstd.Decoder)
	}
	// It's possible to race and create multiple new readers.
	// Only one will survive GC after use.
	zstdDec, _ := zstd.NewReader(nil)
	zstdDecMap.Store(params, zstdDec)
	return zstdDec
}

func zstdDecompress(params ZstdDecoderParams, dst, src []byte) ([]byte, error) {
	return getDecoder(params).DecodeAll(src, dst)
}

func zstdCompress(params ZstdEncoderParams, dst, src []byte) ([]byte, error) {
	return getEncoder(params).EncodeAll(src, dst), nil
}
