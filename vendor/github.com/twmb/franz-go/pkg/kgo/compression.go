package kgo

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"runtime"
	"slices"
	"sync"

	"github.com/klauspost/compress/s2"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
)

var byteBuffers = sync.Pool{New: func() any { return bytes.NewBuffer(make([]byte, 8<<10)) }}

// CompressionCodecType is a bitfield specifying a Kafka-defined compression
// codec. Per spec, only four compression codecs are supported. However, if
// you control both the producer and consumer, you can technically override the
// codec to anything.
type CompressionCodecType int8

const (
	// CodecNone is a compression codec signifying no compression is used.
	CodecNone CompressionCodecType = iota
	// CodecGzip is a compression codec signifying gzip compression.
	CodecGzip
	// CodecSnappy is a compression codec signifying snappy compression.
	CodecSnappy
	// CodecLz4 is a compression codec signifying lz4 compression.
	CodecLz4
	// CodecZstd is a compression codec signifying zstd compression.
	CodecZstd

	// CodecError is returned from compressing or decompressing if an error
	// occurred.
	CodecError = -1
)

// CompressionCodec configures how records are compressed before being sent.
//
// Records are compressed within individual topics and partitions, inside of a
// RecordBatch. All records in a RecordBatch are compressed into one record
// for that batch.
type CompressionCodec struct {
	codec CompressionCodecType
	level int
}

// NoCompression is a compression option that avoids compression. This can
// always be used as a fallback compression.
func NoCompression() CompressionCodec { return CompressionCodec{CodecNone, 0} }

// GzipCompression enables gzip compression with the default compression level.
func GzipCompression() CompressionCodec { return CompressionCodec{CodecGzip, gzip.DefaultCompression} }

// SnappyCompression enables snappy compression.
func SnappyCompression() CompressionCodec { return CompressionCodec{CodecSnappy, 0} }

// Lz4Compression enables lz4 compression with the fastest compression level.
func Lz4Compression() CompressionCodec { return CompressionCodec{CodecLz4, 0} }

// ZstdCompression enables zstd compression with the default compression level.
func ZstdCompression() CompressionCodec { return CompressionCodec{CodecZstd, 0} }

// CompressFlag is a flag to instruct the compressor.
type CompressFlag uint16

const (
	// CompressDisableZstd instructs the compressor that zstd should not be
	// used, even if the compressor supports it. This is used when
	// producing to an old broker (pre Kafka v2.1) that does not yet
	// support zstd compression. If you are confident you will only produce
	// to new brokers, you can ignore this flag.
	CompressDisableZstd CompressFlag = 1 + iota
)

func mkCompressFlags(produceRequestVersion int16) []CompressFlag {
	if produceRequestVersion < 7 {
		return []CompressFlag{CompressDisableZstd}
	}
	return nil
}

// Compressor is an interface that defines how produce batches are compressed.
// You can override the default client internal compressor for more control
// over what compressors to use, level, and memory reuse.
type Compressor interface {
	// Compress compresses src and returns the compressed data as well as
	// the codec type that was used. The 'dst' [bytes.Buffer] argument is
	// pooled within the client and reused across calls to Compress. You
	// can use 'dst' to save memory and return 'dst.Bytes()'. The returned
	// slice is fully used *before* 'dst' is put back into the internal
	// pool. As an example, you can look at the franz-go internal
	// implementation of the default compressor in compression.go.
	//
	// Flags may optionally be provided to direct the compressor to enable
	// or disable features. New backwards compatible flags may be
	// introduced. If you add features to your compressor, be sure to
	// evaluate if new flags exist to opt into or out of features.
	Compress(dst *bytes.Buffer, src []byte, flags ...CompressFlag) ([]byte, CompressionCodecType)
}

// Decompressor is an interface that defines how fetch batches are
// decompressed. You can override the default client internal decompressor for
// more control over what decompressors to use and memory reuse.
type Decompressor interface {
	// Decompress decompresses src, which is compressed with codecType,
	// and returns the decompressed data or an error.
	//
	// If the decompression codec type is CodecNone, this should return
	// the input slice.
	Decompress(src []byte, codecType CompressionCodecType) ([]byte, error)
}

// WithLevel changes the compression codec's "level", effectively allowing for
// higher or lower compression ratios at the expense of CPU speed.
//
// For the zstd package, the level is a typed int; simply convert the type back
// to an int for this function.
//
// If the level is invalid, compressors just use a default level.
func (c CompressionCodec) WithLevel(level int) CompressionCodec {
	c.level = level
	return c
}

type compressor struct {
	options  []CompressionCodecType
	gzPool   sync.Pool
	lz4Pool  sync.Pool
	zstdPool sync.Pool
}

// DefaultCompressor returns the default client compressor. The returned
// compressor will compress produce batches in preference-order of the
// specified codecs. Usually, you only need to specify one codec. If you are
// speaking to an old broker that may not support zstd, you may need to specify
// a second compressor as fallback (old Kafka did not support zstd).  If no
// codecs are specified, or the specified codec is CodecNone, this returns
// 'nil, nil'. A compressor is only used within the client if it is non-nil.
func DefaultCompressor(codecs ...CompressionCodec) (Compressor, error) {
	if len(codecs) == 0 {
		return nil, nil
	}

	used := make(map[CompressionCodecType]bool) // we keep one type of codec per CompressionCodec
	var keepIdx int
	for _, codec := range codecs {
		if _, exists := used[codec.codec]; exists {
			continue
		}
		used[codec.codec] = true
		codecs[keepIdx] = codec
		keepIdx++
	}
	codecs = codecs[:keepIdx]

	for _, codec := range codecs {
		if codec.codec < 0 || codec.codec > 4 {
			return nil, errors.New("unknown compression codec")
		}
	}

	c := new(compressor)

out:
	for _, codec := range codecs {
		c.options = append(c.options, codec.codec)
		switch codec.codec {
		case CodecNone:
			break out
		case CodecGzip:
			level := gzip.DefaultCompression
			if codec.level != 0 {
				if _, err := gzip.NewWriterLevel(nil, codec.level); err != nil {
					level = codec.level
				}
			}
			c.gzPool = sync.Pool{New: func() any { c, _ := gzip.NewWriterLevel(nil, level); return c }}
		case CodecSnappy: // (no pool needed for snappy)
		case CodecLz4:
			level := max(codec.level, 0)
			fn := func() any { return lz4.NewWriter(new(bytes.Buffer)) }
			w := lz4.NewWriter(new(bytes.Buffer))
			if err := w.Apply(lz4.CompressionLevelOption(lz4.CompressionLevel(level))); err == nil {
				fn = func() any {
					w := lz4.NewWriter(new(bytes.Buffer))
					w.Apply(lz4.CompressionLevelOption(lz4.CompressionLevel(level)))
					return w
				}
			}
			w.Close()
			c.lz4Pool = sync.Pool{New: fn}
		case CodecZstd:
			opts := []zstd.EOption{
				zstd.WithWindowSize(64 << 10),
				zstd.WithEncoderConcurrency(1),
				zstd.WithZeroFrames(true),
			}
			fn := func() any {
				zstdEnc, _ := zstd.NewWriter(nil, opts...)
				r := &zstdEncoder{zstdEnc}
				runtime.SetFinalizer(r, func(r *zstdEncoder) { r.inner.Close() })
				return r
			}
			zstdEnc, err := zstd.NewWriter(nil, append(opts, zstd.WithEncoderLevel(zstd.EncoderLevel(codec.level)))...)
			if err == nil {
				zstdEnc.Close()
				opts = append(opts, zstd.WithEncoderLevel(zstd.EncoderLevel(codec.level)))
			}
			c.zstdPool = sync.Pool{New: fn}
		}
	}

	if c.options[0] == CodecNone {
		return nil, nil // first codec was passthrough
	}

	return c, nil
}

type zstdEncoder struct {
	inner *zstd.Encoder
}

func (c *compressor) Compress(dst *bytes.Buffer, src []byte, flags ...CompressFlag) ([]byte, CompressionCodecType) {
	var disableZstd bool
	for _, flag := range flags {
		if flag == CompressDisableZstd {
			disableZstd = true
		}
	}

	var use CompressionCodecType
	for _, option := range c.options {
		if option == CodecZstd && disableZstd {
			continue
		}
		use = option
		break
	}

	var out []byte
	switch use {
	case CodecNone:
		return src, 0
	case CodecGzip:
		gz := c.gzPool.Get().(*gzip.Writer)
		defer c.gzPool.Put(gz)
		gz.Reset(dst)
		if _, err := gz.Write(src); err != nil {
			return nil, CodecError
		}
		if err := gz.Close(); err != nil {
			return nil, CodecError
		}
		out = dst.Bytes()
	case CodecLz4:
		lz := c.lz4Pool.Get().(*lz4.Writer)
		defer c.lz4Pool.Put(lz)
		lz.Reset(dst)
		if _, err := lz.Write(src); err != nil {
			return nil, CodecError
		}
		if err := lz.Close(); err != nil {
			return nil, CodecError
		}
		out = dst.Bytes()
	case CodecSnappy:
		// Because the Snappy and Zstd codecs do not accept an io.Writer interface
		// and directly take a []byte slice, here, the underlying []byte slice (`dst`)
		// obtained from the bytes.Buffer{} from the pool is passed.
		// As the `Write()` method on the buffer isn't used, its internal
		// book-keeping goes out of sync, making the buffer unusable for further
		// reading and writing via it's (eg: accessing via `Byte()`). For subsequent
		// reads, the underlying slice has to be used directly.
		//
		// In this particular context, it is acceptable as there are no subsequent
		// operations performed on the buffer and it is immediately returned to the
		// pool and `Reset()` the next time it is obtained and used where `compress()`
		// is called.
		if l := s2.MaxEncodedLen(len(src)); l > dst.Cap() {
			dst.Grow(l)
		}
		out = s2.EncodeSnappy(dst.Bytes(), src)
	case CodecZstd:
		zstdEnc := c.zstdPool.Get().(*zstdEncoder)
		defer c.zstdPool.Put(zstdEnc)
		if l := zstdEnc.inner.MaxEncodedSize(len(src)); l > dst.Cap() {
			dst.Grow(l)
		}
		out = zstdEnc.inner.EncodeAll(src, dst.Bytes())
	}

	return out, use
}

type decompressor struct {
	ungzPool   sync.Pool
	unlz4Pool  sync.Pool
	unzstdPool sync.Pool
	pools      pools
}

// DefaultDecompressor returns the default decompressor used by clients.
// The first pool provided that implements PoolDecompressBytes will be
// used where possible.
func DefaultDecompressor(pools ...Pool) Decompressor {
	d := &decompressor{
		ungzPool: sync.Pool{
			New: func() any { return new(gzip.Reader) },
		},
		unlz4Pool: sync.Pool{
			New: func() any { return lz4.NewReader(nil) },
		},
		unzstdPool: sync.Pool{
			New: func() any {
				zstdDec, _ := zstd.NewReader(nil,
					zstd.WithDecoderLowmem(true),
					zstd.WithDecoderConcurrency(1),
				)
				r := &zstdDecoder{zstdDec}
				runtime.SetFinalizer(r, func(r *zstdDecoder) {
					r.inner.Close()
				})
				return r
			},
		},
		pools: pools,
	}
	return d
}

type zstdDecoder struct {
	inner *zstd.Decoder
}

func (d *decompressor) Decompress(src []byte, codecType CompressionCodecType) ([]byte, error) {
	if codecType == CodecNone {
		return src, nil
	}

	var (
		out        *bytes.Buffer
		rfn        func() []byte
		userPooled bool
	)
	d.pools.each(func(p Pool) bool {
		if pdecompressBytes, ok := p.(PoolDecompressBytes); ok {
			s := pdecompressBytes.GetDecompressBytes(src, codecType)
			out = bytes.NewBuffer(s)
			rfn = out.Bytes
			userPooled = true
			return true
		}
		return false
	})
	if out == nil {
		out = byteBuffers.Get().(*bytes.Buffer)
		out.Reset()
		defer byteBuffers.Put(out)
		// We clone out.Bytes since we are pooling out ourselves; we
		// need to clone before return since we immediately put into
		// the pool.
		//
		// For user provided slices, we put back into the pool only
		// after the user calls Recycle on every record that has a
		// reference to the slice. Thus, we can return the original
		// slice from the user-provided pool: it is only recycled
		// at the end when the user says they are done.
		rfn = func() []byte { return slices.Clone(out.Bytes()) }
	}

	switch codecType {
	case CodecGzip:
		ungz := d.ungzPool.Get().(*gzip.Reader)
		defer d.ungzPool.Put(ungz)
		if err := ungz.Reset(bytes.NewReader(src)); err != nil {
			return nil, err
		}
		if _, err := io.Copy(out, ungz); err != nil {
			return nil, err
		}
		return rfn(), nil
	case CodecSnappy:
		if len(src) > 16 && bytes.HasPrefix(src, xerialPfx) {
			return xerialDecode(src)
		}
		decoded, err := s2.Decode(out.Bytes(), src)
		if err != nil {
			return nil, err
		}
		if userPooled {
			return decoded, nil
		}
		return slices.Clone(decoded), nil
	case CodecLz4:
		unlz4 := d.unlz4Pool.Get().(*lz4.Reader)
		defer d.unlz4Pool.Put(unlz4)
		unlz4.Reset(bytes.NewReader(src))
		if _, err := io.Copy(out, unlz4); err != nil {
			return nil, err
		}
		return rfn(), nil
	case CodecZstd:
		unzstd := d.unzstdPool.Get().(*zstdDecoder)
		defer d.unzstdPool.Put(unzstd)
		decoded, err := unzstd.inner.DecodeAll(src, out.Bytes())
		if err != nil {
			return nil, err
		}
		if userPooled {
			return decoded, nil
		}
		return slices.Clone(decoded), nil
	default:
		return nil, errors.New("unknown compression codec")
	}
}

var xerialPfx = []byte{130, 83, 78, 65, 80, 80, 89, 0}

var errMalformedXerial = errors.New("malformed xerial framing")

func xerialDecode(src []byte) ([]byte, error) {
	// bytes 0-8: xerial header
	// bytes 8-16: xerial version
	// everything after: uint32 chunk size, snappy chunk
	// we come into this function knowing src is at least 16
	src = src[16:]
	var dst, chunk []byte
	var err error
	for len(src) > 0 {
		if len(src) < 4 {
			return nil, errMalformedXerial
		}
		size := int32(binary.BigEndian.Uint32(src))
		src = src[4:]
		if size < 0 || len(src) < int(size) {
			return nil, errMalformedXerial
		}
		if chunk, err = s2.Decode(chunk[:cap(chunk)], src[:size]); err != nil {
			return nil, err
		}
		src = src[size:]
		dst = append(dst, chunk...)
	}
	return dst, nil
}
