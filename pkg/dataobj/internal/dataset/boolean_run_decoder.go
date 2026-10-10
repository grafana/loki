package dataset

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/parquet-go/bitpack"
)

// booleanRunMaxSupportedValueCount is the largest run length this decoder
// will accept, mirroring the limit parquet-go's own decoder enforces to
// avoid unbounded allocations from a corrupt run header.
const booleanRunMaxSupportedValueCount = math.MaxInt32

// booleanRunDecoder incrementally decodes boolean values encoded with
// Parquet's hybrid RLE/bit-packed encoding (see [bitmapRLEEncoding]), a run
// at a time, rather than requiring the entire input to be decoded in one
// call.
//
// github.com/parquet-go/parquet-go/encoding/rle only exposes a
// whole-buffer batch decode ([rle.Encoding.DecodeBoolean]), with no
// incremental API, so this reimplements its run-decoding loop directly:
// parsing each run's uvarint header (a small, mechanical, spec-following
// format -- see https://github.com/apache/parquet-format/blob/master/Encodings.md#run-length-encoding--bit-packing-hybrid-rle--3)
// and either copying a bit-packed run's bytes through directly (they're
// already in the packed-byte layout [memory.Bitmap] expects) or filling an
// RLE run's bytes with its repeated value. There's no bit-unpacking work to
// delegate to a shared library here, unlike [binaryPackedDecoder] -- a
// boolean RLE/bit-packed run already operates at byte granularity.
//
// Decoded output is a packed-bit byte array (LSB first per byte, one bit
// per boolean) -- the same representation [rle.Encoding.EncodeBoolean]'s
// src and [rle.Encoding.DecodeBoolean]'s dst use.
//
// The zero value is not ready for use; call reset before decoding.
type booleanRunDecoder struct {
	src []byte

	buf    []byte // Bytes decoded ahead of what's been returned by decodeBoolean.
	bufOff int
	bufLen int
}

// reset prepares dec to decode packed boolean bytes from src, which must
// hold a complete RLE/bit-packed boolean payload -- including its 4-byte
// length prefix -- in the format produced by [bitmapRLEEncoding.EncodeBoolean].
func (dec *booleanRunDecoder) reset(src []byte) error {
	dec.bufOff, dec.bufLen = 0, 0

	if len(src) == 4 {
		dec.src = nil
		return nil
	}
	if len(src) < 4 {
		return fmt.Errorf("bitmap: input shorter than 4 bytes: %w", io.ErrUnexpectedEOF)
	}
	n := int(binary.LittleEndian.Uint32(src))
	src = src[4:]
	if n > len(src) {
		return fmt.Errorf("bitmap: input shorter than length prefix: %d < %d: %w", len(src), n, io.ErrUnexpectedEOF)
	}

	dec.src = src[:n]
	return nil
}

// decodeBoolean decodes up to len(dst) packed bytes (8 boolean values each)
// into dst, returning the number of bytes written. decodeBoolean returns
// io.EOF once every byte described by the most recent call to reset has
// been returned, which may be on the same call that returns the final
// bytes.
func (dec *booleanRunDecoder) decodeBoolean(dst []byte) (int, error) {
	var n int

	if dec.bufOff < dec.bufLen {
		n += copy(dst, dec.buf[dec.bufOff:dec.bufLen])
		dec.bufOff += n
	}

	for n < len(dst) && len(dec.src) > 0 {
		if err := dec.decodeRun(); err != nil {
			return n, err
		}
		c := copy(dst[n:], dec.buf[dec.bufOff:dec.bufLen])
		dec.bufOff += c
		n += c
	}

	if len(dec.src) == 0 && dec.bufOff >= dec.bufLen {
		return n, io.EOF
	}
	return n, nil
}

// decodeRun decodes exactly the next run from dec.src into dec.buf,
// resetting dec.bufOff to 0 and dec.bufLen to the number of bytes produced.
func (dec *booleanRunDecoder) decodeRun() error {
	u, sz := binary.Uvarint(dec.src)
	if sz <= 0 {
		return fmt.Errorf("bitmap: decoding run-length block header: %w", io.ErrUnexpectedEOF)
	}
	dec.src = dec.src[sz:]

	count, bitpacked := uint(u>>1), (u&1) != 0
	if count == 0 {
		dec.bufOff, dec.bufLen = 0, 0
		return nil
	}
	if count > booleanRunMaxSupportedValueCount {
		return fmt.Errorf("bitmap: decoded run-length block cannot have more than %d values", booleanRunMaxSupportedValueCount)
	}

	if bitpacked {
		n := int(count)
		if n > len(dec.src) {
			return fmt.Errorf("bitmap: decoding bit-packed block of %d values: %w", n, io.ErrUnexpectedEOF)
		}
		dec.buf = append(dec.buf[:0], dec.src[:n]...)
		dec.src = dec.src[n:]
	} else {
		word := byte(0)
		if len(dec.src) > 0 {
			word = dec.src[0]
			dec.src = dec.src[1:]
		}
		length := bitpack.ByteCount(count)
		if cap(dec.buf) < length {
			dec.buf = make([]byte, length)
		} else {
			dec.buf = dec.buf[:length]
		}
		for i := range dec.buf {
			dec.buf[i] = word
		}
	}

	dec.bufOff, dec.bufLen = 0, len(dec.buf)
	return nil
}
