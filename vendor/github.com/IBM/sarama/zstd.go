package sarama

import (
	"runtime"
	"sync"

	"github.com/klauspost/compress/zstd"
)

type ZstdEncoderParams struct {
	Level int
}
type ZstdDecoderParams struct {
}

var (
	zstdDecMap        sync.Map
	zstdDecoderInitMu sync.Mutex
)

var (
	zstdAvailableEncoders sync.Map
	zstdEncoderInitMu     sync.Mutex
)

// getZstdEncoderChannel returns the buffered channel that retains idle zstd
// encoders for the given params. The slow path holds a single global mutex
// and re-checks the sync.Map under the lock so that a stampede of goroutines
// arriving for the same not-yet-seen ZstdEncoderParams cannot create
// multiple competing channels. The channel is sized to GOMAXPROCS so that
// the previous size-1 cap can no longer force concurrent callers to
// allocate a fresh encoder per batch.
func getZstdEncoderChannel(params ZstdEncoderParams) chan *zstd.Encoder {
	if c, ok := zstdAvailableEncoders.Load(params); ok {
		return c.(chan *zstd.Encoder)
	}

	zstdEncoderInitMu.Lock()
	defer zstdEncoderInitMu.Unlock()

	if c, ok := zstdAvailableEncoders.Load(params); ok {
		return c.(chan *zstd.Encoder)
	}

	ch := make(chan *zstd.Encoder, max(runtime.GOMAXPROCS(0), 1))
	zstdAvailableEncoders.Store(params, ch)
	return ch
}

func newZstdEncoder(params ZstdEncoderParams) *zstd.Encoder {
	encoderLevel := zstd.SpeedDefault
	if params.Level != CompressionLevelDefault {
		encoderLevel = zstd.EncoderLevelFromZstd(params.Level)
	}
	enc, _ := zstd.NewWriter(nil,
		zstd.WithZeroFrames(true),
		zstd.WithEncoderLevel(encoderLevel),
		zstd.WithEncoderConcurrency(1))
	return enc
}

func getZstdEncoder(params ZstdEncoderParams) *zstd.Encoder {
	select {
	case enc := <-getZstdEncoderChannel(params):
		return enc
	default:
		return newZstdEncoder(params)
	}
}

func releaseEncoder(params ZstdEncoderParams, enc *zstd.Encoder) {
	select {
	case getZstdEncoderChannel(params) <- enc:
	default:
		// pool is at capacity; let the encoder be garbage collected.
	}
}

// zstdDecoderKey keys the decoder cache on maxDecodedSize as well as params,
// since the size bound is fixed when the decoder is built and can't be changed
// per call.
type zstdDecoderKey struct {
	ZstdDecoderParams
	maxDecodedSize int
}

func getDecoder(params ZstdDecoderParams, maxDecodedSize int) *zstd.Decoder {
	key := zstdDecoderKey{params, maxDecodedSize}
	if ret, ok := zstdDecMap.Load(key); ok {
		return ret.(*zstd.Decoder)
	}

	zstdDecoderInitMu.Lock()
	defer zstdDecoderInitMu.Unlock()

	if ret, ok := zstdDecMap.Load(key); ok {
		return ret.(*zstd.Decoder)
	}

	opts := []zstd.DOption{zstd.WithDecoderConcurrency(0)}
	if maxDecodedSize > 0 {
		opts = append(opts, zstd.WithDecoderMaxMemory(uint64(maxDecodedSize)))
	}
	zstdDec, _ := zstd.NewReader(nil, opts...)
	zstdDecMap.Store(key, zstdDec)
	return zstdDec
}

func zstdDecompress(params ZstdDecoderParams, dst, src []byte, maxDecodedSize int) ([]byte, error) {
	return getDecoder(params, maxDecodedSize).DecodeAll(src, dst)
}

func zstdCompress(params ZstdEncoderParams, dst, src []byte) ([]byte, error) {
	enc := getZstdEncoder(params)
	out := enc.EncodeAll(src, dst)
	releaseEncoder(params, enc)
	return out, nil
}
