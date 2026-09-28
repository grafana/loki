package kgo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"runtime"
	"slices"
	"sync"

	"github.com/klauspost/compress/gzip" // same format as compress/gzip, faster in both directions
	"github.com/klauspost/compress/s2"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
)

var byteBuffers = sync.Pool{New: func() any { return bytes.NewBuffer(make([]byte, 8<<10)) }}

// ErrMaxDecompress is returned when a batch we consumed would decompress
// larger than [MaxDecompressBatchBytes]. The client treats this error as
// fatal for the partition and it can only be recovered via SetOffsets or by
// you restarting your client with a higher limit. A custom decompressor that
// returns this error fatally stops the partition the same way.
var ErrMaxDecompress = errors.New("decompressed data would exceed MaxDecompressBatchBytes")

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

	use := c.pickCodec(disableZstd)

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

// pickCodec returns the first configured codec, skipping zstd if it is
// disabled (produce versions before 7 cannot use it).
func (c *compressor) pickCodec(disableZstd bool) CompressionCodecType {
	for _, option := range c.options {
		if option != CodecZstd || !disableZstd {
			return option
		}
	}
	return CodecNone
}

// streamWriter is a codec writer that can flush mid stream, which is what
// lets us measure a batch's compressed size before adding one more record.
type streamWriter interface {
	io.Writer
	Flush() error
	Close() error
	Reset(io.Writer)
}

// streamCompressor compresses records into dst. Records collect in buf and
// reach the codec in chunks; after flush, dst.Len() is the exact compressed
// size so far.
type streamCompressor struct {
	dst *bytes.Buffer
	buf []byte
	w   streamWriter
	put func()
}

var (
	streamPool = sync.Pool{New: func() any { return new(streamCompressor) }}
	xerialPool = sync.Pool{New: func() any { return new(xerialWriter) }}
)

// newStream returns a streaming compressor writing into dst, or nil if the
// codec cannot stream.
func (c *compressor) newStream(codec CompressionCodecType, dst *bytes.Buffer) *streamCompressor {
	sc := streamPool.Get().(*streamCompressor)
	sc.dst, sc.buf = dst, sc.buf[:0]
	switch codec {
	case CodecGzip:
		gz := c.gzPool.Get().(*gzip.Writer)
		sc.w, sc.put = gz, func() { c.gzPool.Put(gz) }
	case CodecLz4:
		lz := c.lz4Pool.Get().(*lz4.Writer)
		sc.w, sc.put = lz, func() { c.lz4Pool.Put(lz) }
	case CodecZstd:
		ze := c.zstdPool.Get().(*zstdEncoder)
		sc.w, sc.put = ze.inner, func() { c.zstdPool.Put(ze) }
	case CodecSnappy:
		xw := xerialPool.Get().(*xerialWriter)
		sc.w, sc.put = xw, func() { xerialPool.Put(xw) }
	default:
		streamPool.Put(sc)
		return nil
	}
	sc.w.Reset(dst)
	return sc
}

// worst bounds what n pending bytes can add to dst. gzip, lz4, and zstd
// store a raw block when compressing would grow it, at worst 5 bytes per
// 16KB, and their frames add under 30 bytes, so 1/1024 plus 64 covers all
// three; that is measured from the codecs, so mergeSpan checks the finished
// blob too. Snappy's format bounds a block at 32 + b + b/6, and we write
// blocks of at most 32KB, each behind a 4 byte length, after a 16 byte
// header.
func (sc *streamCompressor) worst(n int) int {
	if _, ok := sc.w.(*xerialWriter); ok {
		return n + n/6 + 36*(n/xerialBlockSize+1) + 16
	}
	return n + n>>10 + 64
}

// streamChunk is how many record bytes collect before a codec Write. For
// snappy and zstd, 4KB and 16KB measured slower (more codec calls), 32KB
// through 128KB the same, and 256KB slower again (the chunk no longer sits
// in cache next to the codec's own buffers); gzip does not care. 32KB is
// the smallest size on that plateau and the xerial block size, so for
// snappy one Write is one block.
const streamChunk = 32 << 10

// write hands whole chunks of buffered records to the codec, keeping the
// remainder for the next write; forced, it hands over everything.
func (sc *streamCompressor) write(force bool) error {
	n := len(sc.buf)
	if !force {
		n -= n % streamChunk
		if n == 0 {
			return nil
		}
	}
	_, err := sc.w.Write(sc.buf[:n])
	sc.buf = append(sc.buf[:0], sc.buf[n:]...)
	return err
}

// flush pushes everything through the codec, after which dst.Len() is the
// exact compressed size so far.
func (sc *streamCompressor) flush() error {
	if err := sc.write(true); err != nil {
		return err
	}
	return sc.w.Flush()
}

// finish closes the stream and returns the codec and the compressor to
// their pools. On success, dst holds the complete compressed frame.
func (sc *streamCompressor) finish() error {
	err := sc.write(true)
	if err == nil {
		err = sc.w.Close()
	}
	sc.w.Reset(nil) // drop the codec's reference to dst before pooling it
	sc.put()
	sc.dst, sc.w, sc.put = nil, nil, nil
	streamPool.Put(sc)
	return err
}

type decompressor struct {
	ungzPool   sync.Pool
	unlz4Pool  sync.Pool
	unzstdPool sync.Pool
	pools      pools
	max        int // how large a batch may decompress to
}

// DefaultDecompressor returns the default decompressor used by clients.
// The first pool provided that implements PoolDecompressBytes will be
// used where possible.
//
// The default decompressor bounds batches at math.MaxInt32; internally,
// clients initialize decompressors with [MaxDecompressBatchBytes].
func DefaultDecompressor(pools ...Pool) Decompressor {
	return newDecompressor(math.MaxInt32, pools...)
}

func newDecompressor(max int, pools ...Pool) *decompressor {
	d := &decompressor{
		max: max,
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
					zstd.WithDecoderMaxMemory(uint64(max)),
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
	max := d.max
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
		return decompressSnappy(dst, src, max)
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
		if err := readBounded(out, lim, max); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
	out := byteBuffers.Get().(*bytes.Buffer)
	out.Reset()
	defer byteBuffers.Put(out)
	if err := readBounded(out, lim, max); err != nil {
		return nil, err
	}
	return slices.Clone(out.Bytes()), nil
}

// readBounded streams lim into out, rejecting more than max bytes. We call
// ReadFrom directly rather than io.Copy so that a stack allocated out does
// not escape through the io.Writer interface.
func readBounded(out *bytes.Buffer, lim *io.LimitedReader, max int) error {
	lim.N = int64(max) + 1
	if n, err := out.ReadFrom(lim); err != nil {
		return err
	} else if n > int64(max) {
		return ErrMaxDecompress
	}
	return nil
}

func decompressSnappy(dst, src []byte, max int) ([]byte, error) {
	if len(src) > 16 && bytes.HasPrefix(src, xerialPfx) {
		return xerialDecode(dst, src, max)
	}
	// The decoded length is read from the header and allocated up
	// front; check the claim before decoding.
	if l, err := s2.DecodedLen(src); err != nil {
		return nil, err
	} else if l > max {
		return nil, ErrMaxDecompress
	}
	return s2.Decode(dst, src)
}

// decompressZstd relies on the decoder's WithDecoderMaxMemory bound: the
// decoder rejects a frame declaring a content size over the bound before
// allocating, errors when a frame outgrows its declared size or the bound
// while decoding, and counts every frame of src against the bound.
func (d *decompressor) decompressZstd(dst, src []byte) ([]byte, error) {
	unzstd := d.unzstdPool.Get().(*zstdDecoder)
	defer d.unzstdPool.Put(unzstd)
	out, err := unzstd.inner.DecodeAll(src, dst)
	if errors.Is(err, zstd.ErrDecoderSizeExceeded) {
		return nil, fmt.Errorf("%w: %w", ErrMaxDecompress, err)
	}
	return out, err
}

var xerialPfx = []byte{130, 83, 78, 65, 80, 80, 89, 0}

// xerialHeader is the prefix followed by the version and the minimum
// compatible version, both 1: the Java reader rejects a lower version.
var xerialHeader = append(append([]byte{}, xerialPfx...), 0, 0, 0, 1, 0, 0, 0, 1)

// xerialBlockSize is the block size the Java stream writes.
const xerialBlockSize = 32 << 10

// xerialWriter frames snappy the way the Java producer does: the header,
// then per block a big endian length and a raw snappy block. Blocks are
// independent, so there is nothing to flush or close.
type xerialWriter struct {
	dst     io.Writer
	buf     []byte
	started bool
}

func (w *xerialWriter) Reset(dst io.Writer) { w.dst, w.started = dst, false }
func (*xerialWriter) Flush() error          { return nil }
func (*xerialWriter) Close() error          { return nil }

func (w *xerialWriter) Write(p []byte) (int, error) {
	w.buf = w.buf[:0]
	if !w.started {
		w.buf = append(w.buf, xerialHeader...)
		w.started = true
	}
	w.buf = appendXerialBlocks(w.buf, p, xerialBlockSize)
	_, err := w.dst.Write(w.buf)
	return len(p), err
}

// appendXerialBlocks appends in as xerial framed blocks of at most
// chunkSize bytes: a big endian length, then a raw snappy block.
func appendXerialBlocks(dst, in []byte, chunkSize int) []byte {
	for len(in) > 0 {
		n := min(chunkSize, len(in))
		dst = slices.Grow(dst, 4+s2.MaxEncodedLen(n))
		at := len(dst)
		block := s2.EncodeSnappy(dst[at+4:cap(dst)], in[:n]) // encodes in place, given the room
		dst = binary.BigEndian.AppendUint32(dst, uint32(len(block)))
		dst = dst[:at+4+len(block)]
		in = in[n:]
	}
	return dst
}

var errMalformedXerial = errors.New("malformed xerial framing")

// xerialDecode appends the decoded chunks to dst (commonly a len-0 pooled
// slice, or nil) and returns the result.
func xerialDecode(dst, src []byte, max int) ([]byte, error) {
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
		if total > int64(max-len(dst)) {
			return nil, ErrMaxDecompress
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
