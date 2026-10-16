package dataset

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/parquet-go/parquet-go/encoding/rle"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/memory"
)

// bitmapEncoder encodes boolean values (0/1) using the Parquet hybrid
// RLE/bit-packed encoding (github.com/parquet-go/parquet-go/encoding/rle),
// the same encoding Parquet uses for boolean-typed data pages, definition
// levels, and repetition levels.
//
// bitmapEncoder is used both as the general encoder for
// [datasetmd.PHYSICAL_TYPE_UINT64]/[datasetmd.ENCODING_TYPE_BITMAP] columns,
// and directly (bypassing the encoding registry) by [pageBuilder] to encode
// every page's null-presence bitmap.
//
// # Format
//
// Values are buffered in memory as a packed bitset (matching the input
// format [rle.Encoding.EncodeBoolean] expects) until Flush is called, since
// the underlying RLE/bit-pack encoder operates on a full batch of values at
// once. Unlike Parquet, which relies on an externally-known page value count
// to know how many bits of the decoded (and possibly padded) output are
// valid, encoded pages here are self-describing: Flush writes a
// uvarint-encoded count of valid bits before the RLE-encoded payload.
type bitmapEncoder struct {
	w streamio.Writer

	bits    []byte // Packed bits, least-significant-bit first per byte.
	numBits int    // Number of valid bits in bits.

	encoded []byte // Reused output buffer for Flush; see Flush for why reuse is safe.
}

var _ valueEncoder = (*bitmapEncoder)(nil)

var bitmapRLEEncoding rle.Encoding

// newBitmapEncoder creates a new bitmap encoder that writes encoded numbers to w.
func newBitmapEncoder(w streamio.Writer) *bitmapEncoder {
	var enc bitmapEncoder
	enc.Reset(w)
	return &enc
}

// PhysicalType returns [datasetmd.PHYSICAL_TYPE_UINT64].
func (enc *bitmapEncoder) PhysicalType() datasetmd.PhysicalType {
	return datasetmd.PHYSICAL_TYPE_UINT64
}

// EncodingType returns [datasetmd.ENCODING_TYPE_BITMAP].
func (enc *bitmapEncoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_BITMAP
}

// Encode appends a new uint64 value to enc. v must be 0 or 1.
func (enc *bitmapEncoder) Encode(v Value) error {
	return enc.EncodeN(v, 1)
}

// EncodeN appends n copies of v to enc. v must be 0 or 1.
func (enc *bitmapEncoder) EncodeN(v Value, n uint64) error {
	if v.Type() != datasetmd.PHYSICAL_TYPE_UINT64 {
		return fmt.Errorf("invalid value type %s", v.Type())
	}
	uv := v.Uint64()
	if uv != 0 && uv != 1 {
		// Both decoder and encoder specialize on boolean values for now as this encoding is used only for presence
		// bitmap at the moment. We can add support for larger values later if required.
		return fmt.Errorf("invalid value %d of %s", uv, v.Type())
	}
	if n == 0 {
		return nil
	}

	enc.bits = appendBits(enc.bits, enc.numBits, uv == 1, int(n))
	enc.numBits += int(n)
	return nil
}

// EstimatedSize returns an estimate of the size of the RLE/bit-packed-encoded
// page if Flush were called right now: the packed-bit size of all buffered
// values plus the uvarint value-count prefix.
func (enc *bitmapEncoder) EstimatedSize() int {
	if enc.numBits == 0 {
		return 0
	}
	return streamio.UvarintSize(uint64(enc.numBits)) + (enc.numBits+7)/8
}

// Flush writes any remaining values to the underlying [streamio.Writer].
func (enc *bitmapEncoder) Flush() error {
	if enc.numBits == 0 {
		return nil
	}

	// Zero any unused trailing bits in the last byte; appendBits guarantees
	// bytes are zeroed before use, but Flush may be called on a buffer whose
	// last byte was only partially filled by the final EncodeN call, which
	// only sets bits it's asked to and never clears ones beyond enc.numBits
	// that a *previous* page might have left set before a Reset (Reset
	// doesn't reallocate enc.bits, only resizes it).
	if rem := enc.numBits % 8; rem != 0 {
		enc.bits[len(enc.bits)-1] &^= 0xFF << uint(rem)
	}

	if err := streamio.WriteUvarint(enc.w, uint64(enc.numBits)); err != nil {
		return err
	}

	// enc.encoded retains whatever capacity it grew to on a previous page.
	// Reusing it is safe because the result is written to enc.w and then
	// discarded immediately below -- nothing retains it past this call, so
	// unlike the decoder there's no cross-page aliasing concern.
	encoded, err := bitmapRLEEncoding.EncodeBoolean(enc.encoded[:0], enc.bits)
	if err != nil {
		return fmt.Errorf("bitmap: encoding values: %w", err)
	}
	enc.encoded = encoded

	n, err := enc.w.Write(encoded)
	if n != len(encoded) {
		return fmt.Errorf("short write; expected %d bytes, wrote %d", len(encoded), n)
	}

	// bitmapEncoder is used directly by [pageBuilder] as its presence
	// encoder, which -- unlike [pageBuilder.valuesEnc] -- is never explicitly
	// Reset between pages (only Flushed), so Flush itself must clear the
	// buffered bits to be ready for the next page.
	enc.bits = enc.bits[:0]
	enc.numBits = 0

	return err
}

// Reset resets enc to write to w.
func (enc *bitmapEncoder) Reset(w streamio.Writer) {
	enc.w = w
	enc.bits = enc.bits[:0]
	enc.numBits = 0
}

// appendBits appends n copies of the bit v onto bits, which holds numBits
// valid bits packed least-significant-bit first (per byte). It returns the
// (possibly reallocated) updated slice; callers are responsible for tracking
// the new bit count (numBits+n) themselves.
//
// Any newly-exposed bytes are explicitly zeroed before v is applied, since a
// reused buffer (via a slice re-sliced back to length 0) may still hold
// stale bits from a previous use beyond its current length.
func appendBits(bits []byte, numBits int, v bool, n int) []byte {
	if n <= 0 {
		return bits
	}

	var (
		oldLen      = len(bits)
		end         = numBits + n
		neededBytes = (end + 7) / 8
	)

	switch {
	case cap(bits) < neededBytes:
		grown := make([]byte, neededBytes, max(neededBytes, 2*cap(bits)))
		copy(grown, bits)
		bits = grown
	case len(bits) < neededBytes:
		bits = bits[:neededBytes]
	}
	if neededBytes > oldLen {
		clear(bits[oldLen:neededBytes])
	}

	if !v {
		return bits
	}

	var (
		startByte, startBit = numBits / 8, numBits % 8
		endByte, endBit     = end / 8, end % 8
	)

	if startByte == endByte {
		for b := startBit; b < endBit; b++ {
			bits[startByte] |= 1 << uint(b)
		}
		return bits
	}

	if startBit != 0 {
		for b := startBit; b < 8; b++ {
			bits[startByte] |= 1 << uint(b)
		}
		startByte++
	}
	for i := startByte; i < endByte; i++ {
		bits[i] = 0xFF
	}
	if endBit != 0 {
		for b := range endBit {
			bits[endByte] |= 1 << uint(b)
		}
	}
	return bits
}

// bitmapDecoder decodes boolean presence values encoded by [bitmapEncoder],
// using [booleanRunDecoder] to decode a few packed bytes at a time rather
// than the whole page up front.
//
// Decoded packed bytes are kept in packedBuf, viewed through cache, private
// fields that are never exposed to callers directly: DecodeTo always copies
// the requested range into a caller-provided, allocator-backed bitmap via
// [memory.Bitmap.AppendBitmap], so it's safe to freely reuse packedBuf's
// capacity across refills and across pages via Reset.
type bitmapDecoder struct {
	data    []byte
	started bool
	total   int // Total number of valid bits described by data.
	pos     int // Number of bits already served.

	stream booleanRunDecoder

	packedBuf []byte        // Scratch for pulling packed bytes from stream. Never exposed directly.
	cache     memory.Bitmap // View over packedBuf: decoded bits not yet served.
}

var _ valueDecoder = (*bitmapDecoder)(nil)

// newBitmapDecoder creates a new bitmap decoder that reads encoded bools from data.
func newBitmapDecoder(data []byte) *bitmapDecoder {
	var dec bitmapDecoder
	dec.Reset(data)
	return &dec
}

// PhysicalType returns [datasetmd.PHYSICAL_TYPE_UINT64].
func (dec *bitmapDecoder) PhysicalType() datasetmd.PhysicalType {
	return datasetmd.PHYSICAL_TYPE_UINT64
}

// EncodingType returns [datasetmd.ENCODING_TYPE_BITMAP].
func (dec *bitmapDecoder) EncodingType() datasetmd.EncodingType {
	return datasetmd.ENCODING_TYPE_BITMAP
}

// Decode decodes up to count values and returns them as a bitmap. The number
// of decoded values is bm.Len(). At the end of the stream, Decode returns
// any decoded values along with [io.EOF].
func (dec *bitmapDecoder) Decode(alloc *memory.Allocator, count int) (columnar.Array, error) {
	bm := memory.NewBitmap(alloc, count)
	err := dec.DecodeTo(&bm, count)
	return columnar.NewBool(bm, memory.Bitmap{}), err
}

func (dec *bitmapDecoder) DecodeTo(bm *memory.Bitmap, count int) error {
	if err := dec.ensureStarted(); err != nil {
		return err
	}

	bm.Grow(count)
	bm.Resize(0)

	if dec.pos >= dec.total {
		return io.EOF
	}

	n := min(count, dec.total-dec.pos)
	for n > 0 {
		if dec.cache.Len() == 0 {
			if err := dec.refill(n); err != nil {
				return err
			}
			if dec.cache.Len() == 0 {
				// The RLE stream ran out before dec.total bits were produced:
				// truncated or corrupt input.
				return fmt.Errorf("bitmap: decoding values: %w", io.ErrUnexpectedEOF)
			}
		}

		take := min(n, dec.cache.Len())
		src := dec.cache.Slice(0, take)
		bm.AppendBitmap(*src)
		dec.cache = *dec.cache.Slice(take, dec.cache.Len())
		dec.pos += take
		n -= take
	}

	if dec.pos >= dec.total {
		return io.EOF
	}
	return nil
}

// ensureStarted lazily parses the uvarint value-count prefix and resets the
// RLE stream decoder on the first call after Reset.
func (dec *bitmapDecoder) ensureStarted() error {
	if dec.started {
		return nil
	}
	dec.started = true

	if len(dec.data) == 0 {
		dec.total = 0
		return nil
	}

	total, n := binary.Uvarint(dec.data)
	if n <= 0 {
		return fmt.Errorf("bitmap: reading value count: %w", io.ErrUnexpectedEOF)
	}
	dec.total = int(total)

	if err := dec.stream.reset(dec.data[n:]); err != nil {
		return fmt.Errorf("bitmap: decoding values: %w", err)
	}
	return nil
}

// refill decodes more packed bytes from dec.stream into dec.packedBuf and
// points dec.cache at the result, pulling at least enough for need bits (but
// never more than what's actually available).
func (dec *bitmapDecoder) refill(need int) error {
	const minPull = 64 // bytes; avoid pulling absurdly small chunks from dec.stream.

	wantBytes := max(minPull, (need+7)/8)
	if cap(dec.packedBuf) < wantBytes {
		dec.packedBuf = make([]byte, wantBytes)
	} else {
		dec.packedBuf = dec.packedBuf[:cap(dec.packedBuf)]
	}

	n, err := dec.stream.decodeBoolean(dec.packedBuf)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("bitmap: decoding values: %w", err)
	}

	// The final refill of a page may decode a few trailing pad bits in its
	// last byte beyond dec.total; cap the cache to the true remaining count
	// so they're never exposed to a caller.
	bits := min(8*n, dec.total-dec.pos)
	dec.cache = memory.BitmapFrom(dec.packedBuf[:n], bits, 0)
	return nil
}

// Reset resets dec to read from data.
func (dec *bitmapDecoder) Reset(data []byte) {
	dec.data = data
	dec.started = false
	dec.total = 0
	dec.pos = 0
	dec.cache = memory.Bitmap{}
}
