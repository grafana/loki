package kgo

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"runtime"
	"sync"

	"github.com/klauspost/compress/s2"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
)

var byteBuffers = sync.Pool{New: func() any { return bytes.NewBuffer(make([]byte, 8<<10)) }}

type codecType int8

const (
	codecNone codecType = iota
	codecGzip
	codecSnappy
	codecLZ4
	codecZstd
)

// CompressionCodec configures how records are compressed before being sent.
//
// Records are compressed within individual topics and partitions, inside of a
// RecordBatch. All records in a RecordBatch are compressed into one record
// for that batch.
type CompressionCodec struct {
	codec codecType
	level int
}

// NoCompression is a compression option that avoids compression. This can
// always be used as a fallback compression.
func NoCompression() CompressionCodec { return CompressionCodec{codecNone, 0} }

// GzipCompression enables gzip compression with the default compression level.
func GzipCompression() CompressionCodec { return CompressionCodec{codecGzip, gzip.DefaultCompression} }

// SnappyCompression enables snappy compression.
func SnappyCompression() CompressionCodec { return CompressionCodec{codecSnappy, 0} }

// Lz4Compression enables lz4 compression with the fastest compression level.
func Lz4Compression() CompressionCodec { return CompressionCodec{codecLZ4, 0} }

// ZstdCompression enables zstd compression with the default compression level.
func ZstdCompression() CompressionCodec { return CompressionCodec{codecZstd, 0} }

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
	options  []codecType
	gzPool   sync.Pool
	lz4Pool  sync.Pool
	zstdPool sync.Pool
}

func newCompressor(codecs ...CompressionCodec) (*compressor, error) {
	if len(codecs) == 0 {
		return nil, nil
	}

	used := make(map[codecType]bool) // we keep one type of codec per CompressionCodec
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
		case codecNone:
			break out
		case codecGzip:
			level := gzip.DefaultCompression
			if codec.level != 0 {
				if _, err := gzip.NewWriterLevel(nil, codec.level); err != nil {
					level = codec.level
				}
			}
			c.gzPool = sync.Pool{New: func() any { c, _ := gzip.NewWriterLevel(nil, level); return c }}
		case codecSnappy: // (no pool needed for snappy)
		case codecLZ4:
			level := codec.level
			if level < 0 {
				level = 0 // 0 == lz4.Fast
			}
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
		case codecZstd:
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

	if c.options[0] == codecNone {
		return nil, nil // first codec was passthrough
	}

	return c, nil
}

type zstdEncoder struct {
	inner *zstd.Encoder
}

// Compress compresses src to buf, returning buf's inner slice once done or nil
// if an error is encountered.
//
// The writer should be put back to its pool after the returned slice is done
// being used.
func (c *compressor) compress(dst *bytes.Buffer, src []byte, produceRequestVersion int16) ([]byte, codecType) {
	var use codecType
	for _, option := range c.options {
		if option == codecZstd && produceRequestVersion < 7 {
			continue
		}
		use = option
		break
	}

	var out []byte
	switch use {
	case codecNone:
		return src, 0
	case codecGzip:
		gz := c.gzPool.Get().(*gzip.Writer)
		defer c.gzPool.Put(gz)
		gz.Reset(dst)
		if _, err := gz.Write(src); err != nil {
			return nil, -1
		}
		if err := gz.Close(); err != nil {
			return nil, -1
		}
		out = dst.Bytes()
	case codecLZ4:
		lz := c.lz4Pool.Get().(*lz4.Writer)
		defer c.lz4Pool.Put(lz)
		lz.Reset(dst)
		if _, err := lz.Write(src); err != nil {
			return nil, -1
		}
		if err := lz.Close(); err != nil {
			return nil, -1
		}
		out = dst.Bytes()
	case codecSnappy:
		// Because the Snappy and Zstd codecs do not accept an io.Writer interface
		// and directly take a []byte slice, here, the underlying []byte slice (`dst`)
		// obtained from the bytes.Buffer{} from the pool is passed.
		// As the `Write()` method on the buffer isn't used, its internal
		// book-keeping goes out of sync, making the buffer unusable for further
		// reading and writing via it's (eg: accessing via `Byte()`). For subsequent
		// reads, the underlying slice has to be used directly.
		//
		// In this particular context, it is acceptable as there there are no subsequent
		// operations performed on the buffer and it is immediately returned to the
		// pool and `Reset()` the next time it is obtained and used where `compress()`
		// is called.
		if l := s2.MaxEncodedLen(len(src)); l > dst.Cap() {
			dst.Grow(l)
		}
		out = s2.EncodeSnappy(dst.Bytes(), src)
	case codecZstd:
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
}

func newDecompressor() *decompressor {
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
	}
	return d
}

type zstdDecoder struct {
	inner *zstd.Decoder
}

func (d *decompressor) decompress(src []byte, codec byte) ([]byte, error) {
	// Early return in case there is no compression
	compCodec := codecType(codec)
	if compCodec == codecNone {
		return src, nil
	}
	out := byteBuffers.Get().(*bytes.Buffer)
	out.Reset()
	defer byteBuffers.Put(out)

	switch compCodec {
	case codecGzip:
		ungz := d.ungzPool.Get().(*gzip.Reader)
		defer d.ungzPool.Put(ungz)
		if err := ungz.Reset(bytes.NewReader(src)); err != nil {
			return nil, err
		}
		if _, err := io.Copy(out, ungz); err != nil {
			return nil, err
		}
		return append([]byte(nil), out.Bytes()...), nil
	case codecSnappy:
		if len(src) > 16 && bytes.HasPrefix(src, xerialPfx) {
			return xerialDecode(src)
		}
		decoded, err := s2.Decode(out.Bytes(), src)
		if err != nil {
			return nil, err
		}
		return append([]byte(nil), decoded...), nil
	case codecLZ4:
		unlz4 := d.unlz4Pool.Get().(*lz4.Reader)
		defer d.unlz4Pool.Put(unlz4)
		unlz4.Reset(bytes.NewReader(src))
		if _, err := io.Copy(out, unlz4); err != nil {
			return nil, err
		}
		return append([]byte(nil), out.Bytes()...), nil
	case codecZstd:
		unzstd := d.unzstdPool.Get().(*zstdDecoder)
		defer d.unzstdPool.Put(unzstd)
		decoded, err := unzstd.inner.DecodeAll(src, out.Bytes())
		if err != nil {
			return nil, err
		}
		return append([]byte(nil), decoded...), nil
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
