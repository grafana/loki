package kgo

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"runtime"
	"slices"
	"sync"

	"github.com/klauspost/compress/s2"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
)

var byteBuffers = sync.Pool{New: func() any { return bytes.NewBuffer(make([]byte, 8<<10)) }}

// maxDecompressedSize caps how much one batch may decompress to. Fetch
// limits bound only the compressed bytes on the wire; nothing in the
// protocol bounds the decompressed size, and the whole batch is
// materialized contiguously while decompressing. Without a cap, a few-KB
// malicious or corrupt batch can demand tens of GiB: zstd frames declare a
// content size that is honored up to the decoder's configured limit (the
// library default is 64 GiB), gzip expands up to ~1032x, lz4 up to ~255x,
// and snappy headers claim up to 4 GiB. No legitimate batch can exceed
// math.MaxInt32 decompressed: every known producer serializes a batch's
// records into an int32-indexed buffer before compressing (this client's
// own appendTo, the Java client, librdkafka),
// so a batch claiming more is corrupt or hostile and is rejected like any
// other corrupt batch: a loud, repeated fetch error with no offset advance.
// A var only so tests can shrink it.
var maxDecompressedSize = int64(math.MaxInt32)

var errDecompressedTooLarge = errors.New("decompressed data exceeds the maximum allowed decompressed batch size (corrupt or malicious batch)")

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

	// CodecError is returned as the used-codec from Compress if an error
	// occurred while compressing (Decompress reports errors via its error
	// return instead).
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
				if _, err := gzip.NewWriterLevel(nil, codec.level); err == nil {
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
			New: func() any {
				r := new(gzipDecoder)
				r.lim.R = &r.inner
				return r
			},
		},
		unlz4Pool: sync.Pool{
			New: func() any {
				r := &lz4Decoder{inner: lz4.NewReader(nil)}
				r.lim.R = r.inner
				return r
			},
		},
		unzstdPool: sync.Pool{
			New: func() any {
				zstdDec, _ := zstd.NewReader(nil,
					zstd.WithDecoderLowmem(true),
					zstd.WithDecoderConcurrency(1),
					zstd.WithDecoderMaxMemory(uint64(maxDecompressedSize)),
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

// The gzip and lz4 decoders are pooled together with the bytes.Reader they
// read from and the LimitedReader that bounds their output, so that a
// decompress allocates none of them.
type gzipDecoder struct {
	inner gzip.Reader
	src   bytes.Reader
	lim   io.LimitedReader
}

type lz4Decoder struct {
	inner *lz4.Reader
	src   bytes.Reader
	lim   io.LimitedReader
}

func (d *decompressor) Decompress(src []byte, codecType CompressionCodecType) (_ []byte, err error) {
	if codecType == CodecNone {
		return src, nil
	}

	var (
		dst      []byte
		userPool PoolDecompressBytes
		pooled   []byte
	)
	d.pools.each(func(p Pool) bool {
		if pdecompressBytes, ok := p.(PoolDecompressBytes); ok {
			userPool = pdecompressBytes
			pooled = pdecompressBytes.GetDecompressBytes(src, codecType)
			// Only the slice's capacity is used: decompressed data
			// must start at index 0, while a buffer initialized with
			// len > 0 (a pool returning make([]byte, sizeGuess))
			// would have the copy/append based codecs write after the
			// existing length, prefixing the output with stale bytes.
			dst = pooled[:0]
			return true
		}
		return false
	})
	userPooled := userPool != nil
	if userPooled {
		// A batch that fails to decode yields no records, so nothing
		// will Recycle the slice: put it back now. We do not know how
		// far the codec wrote before failing, so we clear the whole
		// capacity.
		defer func() {
			if err != nil {
				clear(pooled[:cap(pooled)])
				userPool.PutDecompressBytes(pooled)
			}
		}()
	}

	// For user provided slices, we put back into the pool only after the
	// user calls Recycle on every record that has a reference to the
	// slice, so we can return the pool's slice directly.
	//
	// Snappy and zstd decode into dst when it has the capacity and
	// otherwise allocate their own output. With no user pool, dst is nil
	// and the fresh allocation is what we return: it is not shared with
	// anything, so there is nothing to clone.
	var lim *io.LimitedReader
	switch codecType {
	case CodecSnappy:
		return decompressSnappy(dst, src)
	case CodecZstd:
		return d.decompressZstd(dst, src)
	case CodecGzip:
		ungz := d.ungzPool.Get().(*gzipDecoder)
		defer d.ungzPool.Put(ungz)
		ungz.src.Reset(src)
		if err := ungz.inner.Reset(&ungz.src); err != nil {
			return nil, err
		}
		lim = &ungz.lim
	case CodecLz4:
		unlz4 := d.unlz4Pool.Get().(*lz4Decoder)
		defer d.unlz4Pool.Put(unlz4)
		unlz4.src.Reset(src)
		unlz4.inner.Reset(&unlz4.src)
		lim = &unlz4.lim
	default:
		return nil, errors.New("unknown compression codec")
	}

	// Gzip and lz4 stream into a bytes.Buffer. With no user pool we use
	// our own pooled buffer so it grows once and is reused; we must clone
	// before the deferred Put.
	if userPooled {
		out := bytes.NewBuffer(dst)
		if err := readBounded(out, lim); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
	out := byteBuffers.Get().(*bytes.Buffer)
	out.Reset()
	defer byteBuffers.Put(out)
	if err := readBounded(out, lim); err != nil {
		return nil, err
	}
	return slices.Clone(out.Bytes()), nil
}

// readBounded streams lim into out, rejecting more than
// maxDecompressedSize. We call ReadFrom directly rather than io.Copy so
// that a stack allocated out does not escape through the io.Writer
// interface.
func readBounded(out *bytes.Buffer, lim *io.LimitedReader) error {
	lim.N = maxDecompressedSize + 1
	if n, err := out.ReadFrom(lim); err != nil {
		return err
	} else if n > maxDecompressedSize {
		return errDecompressedTooLarge
	}
	return nil
}

func decompressSnappy(dst, src []byte) ([]byte, error) {
	if len(src) > 16 && bytes.HasPrefix(src, xerialPfx) {
		return xerialDecode(dst, src)
	}
	// The decoded length is read from the header and allocated up
	// front; check the claim before decoding.
	if l, err := s2.DecodedLen(src); err != nil {
		return nil, err
	} else if int64(l) > maxDecompressedSize {
		return nil, errDecompressedTooLarge
	}
	return s2.Decode(dst, src)
}

func (d *decompressor) decompressZstd(dst, src []byte) ([]byte, error) {
	unzstd := d.unzstdPool.Get().(*zstdDecoder)
	defer d.unzstdPool.Put(unzstd)
	return unzstd.inner.DecodeAll(src, dst)
}

var xerialPfx = []byte{130, 83, 78, 65, 80, 80, 89, 0}

var errMalformedXerial = errors.New("malformed xerial framing")

// xerialDecode appends the decoded chunks to dst (commonly a len-0 pooled
// slice, or nil) and returns the result.
func xerialDecode(dst, src []byte) ([]byte, error) {
	// bytes 0-8: xerial header
	// bytes 8-16: xerial version
	// everything after: uint32 chunk size, snappy chunk
	// we come into this function knowing src is at least 16
	src = src[16:]
	// Walk the chunk headers first, summing the claimed decoded lengths
	// and bounding the total, so that dst grows once. This touches a few
	// bytes per chunk and skips the rest.
	var total int64
	for rem := src; len(rem) > 0; {
		if len(rem) < 4 {
			return nil, errMalformedXerial
		}
		size := int32(binary.BigEndian.Uint32(rem))
		rem = rem[4:]
		if size < 0 || len(rem) < int(size) {
			return nil, errMalformedXerial
		}
		l, err := s2.DecodedLen(rem[:size])
		if err != nil {
			return nil, err
		}
		total += int64(l)
		if total > maxDecompressedSize-int64(len(dst)) {
			return nil, errDecompressedTooLarge
		}
		rem = rem[size:]
	}
	dst = slices.Grow(dst, int(total))
	// s2 decodes in place when the destination has room for the decoded
	// length, so each chunk decodes straight into dst's spare capacity.
	for len(src) > 0 {
		size := int(binary.BigEndian.Uint32(src))
		src = src[4:]
		l, err := s2.DecodedLen(src[:size])
		if err != nil {
			return nil, err
		}
		if _, err := s2.Decode(dst[len(dst):len(dst)+l], src[:size]); err != nil {
			return nil, err
		}
		dst = dst[:len(dst)+l]
		src = src[size:]
	}
	return dst, nil
}
