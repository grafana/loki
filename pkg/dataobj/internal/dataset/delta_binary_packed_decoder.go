package dataset

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/parquet-go/bitpack"
)

// deltaMaxSupportedBlockSize is the largest DELTA_BINARY_PACKED block size
// this decoder will accept, mirroring the limit parquet-go's own decoder
// enforces to avoid unbounded allocations from a corrupt block-size field.
const deltaMaxSupportedBlockSize = 65536

// binaryPackedDecoder incrementally decodes int64 values encoded with
// Parquet's DELTA_BINARY_PACKED encoding (see [deltaEncoding]), a block of
// up to blockSize values at a time, rather than requiring the entire page
// to be decoded in one call.
//
// github.com/parquet-go/parquet-go/encoding/delta only exposes a
// whole-buffer batch decode ([delta.BinaryPackedEncoding.DecodeInt64]), with
// no incremental API and no exported hooks into its block-level parsing, so
// this reimplements that bookkeeping directly: parsing the page/block/
// mini-block headers (a small, mechanical, spec-following format -- see
// https://github.com/apache/parquet-format/blob/master/Encodings.md#delta-encoding-delta_binary_packed--5)
// and reconstructing values from deltas via a running sum. The actual
// bit-unpacking -- the part that benefits from hand-optimized assembly --
// stays delegated to [bitpack.Unpack], a public API from a separate module
// that parquet-go's own decoder also calls into.
//
// The zero value is not ready for use; call reset before decoding.
type binaryPackedDecoder struct {
	src           []byte
	lastValue     int64
	numMiniBlocks int
	blockSize     int
	toDecode      int // Values not yet decoded from src (excludes buf).

	buf    []int64 // Values decoded ahead of what's been returned by decodeInt64.
	bufOff int
	bufLen int

	tmp []byte // Scratch space for unpacking a mini-block that needs copying.
}

// reset prepares dec to decode int64 values from src, which must hold a
// complete DELTA_BINARY_PACKED page in the format produced by
// [deltaEncoding.EncodeInt64]. It returns the total number of values
// encoded in the page.
func (dec *binaryPackedDecoder) reset(src []byte) (int, error) {
	blockSize, numMiniBlocks, totalValues, firstValue, rest, err := decodeDeltaHeader(src)
	if err != nil {
		return 0, err
	}

	dec.src = rest
	dec.numMiniBlocks = numMiniBlocks
	dec.blockSize = blockSize
	dec.bufOff, dec.bufLen = 0, 0
	dec.toDecode = 0

	if cap(dec.buf) < blockSize {
		dec.buf = make([]int64, blockSize)
	}

	if totalValues > 0 {
		dec.buf = dec.buf[:1]
		dec.buf[0] = firstValue
		dec.bufLen = 1
		dec.lastValue = firstValue
		dec.toDecode = totalValues - 1
	}

	return totalValues, nil
}

// decodeInt64 decodes up to len(dst) values into dst, returning the number
// of values written. decodeInt64 returns io.EOF once every value described
// by the most recent call to reset has been returned, which may be on the
// same call that returns the final values.
func (dec *binaryPackedDecoder) decodeInt64(dst []int64) (int, error) {
	var n int

	if dec.bufOff < dec.bufLen {
		n += copy(dst, dec.buf[dec.bufOff:dec.bufLen])
		dec.bufOff += n
	}

	for n < len(dst) && dec.toDecode > 0 {
		blockLength := min(dec.blockSize, dec.toDecode)

		out := dec.buf[:blockLength]
		direct := len(dst)-n >= blockLength
		if direct {
			// Common case: dst has room for the whole block, so decode
			// straight into it instead of buffering and copying.
			out = dst[n : n+blockLength]
		}

		if err := dec.decodeBlock(out); err != nil {
			return n, err
		}
		dec.toDecode -= blockLength

		if direct {
			n += blockLength
		} else {
			dec.bufLen = blockLength
			dec.bufOff = copy(dst[n:], dec.buf[:blockLength])
			n += dec.bufOff
		}
	}

	if dec.toDecode == 0 && dec.bufOff >= dec.bufLen {
		return n, io.EOF
	}
	return n, nil
}

// decodeBlock decodes exactly one block (up to dec.blockSize values; fewer
// for a page's final block) from dec.src into out, which must have length
// equal to that count.
func (dec *binaryPackedDecoder) decodeBlock(out []int64) error {
	minDelta, bitWidths, rest, err := decodeDeltaBlockHeader(dec.src, dec.numMiniBlocks)
	if err != nil {
		return err
	}
	dec.src = rest

	numValuesInMiniBlock := dec.blockSize / dec.numMiniBlocks
	writeOffset := 0

	for _, bitWidth := range bitWidths {
		n := min(numValuesInMiniBlock, len(out)-writeOffset)
		if n <= 0 {
			break
		}
		if bitWidth != 0 {
			miniBlockSize := (numValuesInMiniBlock * int(bitWidth)) / 8
			miniBlockData := dec.src
			if miniBlockSize <= len(dec.src) {
				miniBlockData = dec.src[:miniBlockSize]
			}
			dec.src = dec.src[len(miniBlockData):]
			if len(miniBlockData) < miniBlockSize+bitpack.PaddingInt64 {
				// bitpack.Unpack reads a little past the bytes it strictly
				// needs (for unchecked-width vectorized unpacking), so a
				// mini-block at the very end of src needs copying into a
				// buffer with room for that overread.
				dec.tmp = growBytes(dec.tmp[:0], miniBlockSize+bitpack.PaddingInt64)
				miniBlockData = dec.tmp[:copy(dec.tmp, miniBlockData)]
			}
			miniBlockData = miniBlockData[:miniBlockSize]
			bitpack.Unpack(out[writeOffset:writeOffset+n], miniBlockData, uint(bitWidth))
		} else {
			// A bit width of zero means every delta in this mini-block
			// equals the block's minDelta (so the "delta minus minDelta"
			// stored value is 0 for all of them); nothing was written to
			// src for it. Unlike decodeInt64, out here isn't guaranteed to
			// already be zeroed (it may be a block of dec.buf left over
			// from a previous, larger block, or caller-owned memory), so
			// it must be cleared explicitly.
			clear(out[writeOffset : writeOffset+n])
		}
		writeOffset += n
	}

	if writeOffset < len(out) {
		return fmt.Errorf("%d missing values: %w", len(out)-writeOffset, io.ErrUnexpectedEOF)
	}

	dec.lastValue = decodeDeltaBlock(out, minDelta, dec.lastValue)
	return nil
}

// decodeDeltaHeader parses a DELTA_BINARY_PACKED page header: block size,
// number of mini-blocks per block, total value count, and the first value.
func decodeDeltaHeader(src []byte) (blockSize, numMiniBlocks, totalValues int, firstValue int64, rest []byte, err error) {
	u, n := binary.Uvarint(src)
	if n <= 0 {
		return 0, 0, 0, 0, src, fmt.Errorf("delta: decoding block size: %w", io.ErrUnexpectedEOF)
	}
	blockSize = int(u)
	src = src[n:]

	u, n = binary.Uvarint(src)
	if n <= 0 {
		return 0, 0, 0, 0, src, fmt.Errorf("delta: decoding number of mini-blocks: %w", io.ErrUnexpectedEOF)
	}
	numMiniBlocks = int(u)
	src = src[n:]

	u, n = binary.Uvarint(src)
	if n <= 0 {
		return 0, 0, 0, 0, src, fmt.Errorf("delta: decoding total values: %w", io.ErrUnexpectedEOF)
	}
	totalValues = int(u)
	src = src[n:]

	firstValue, n = binary.Varint(src)
	if n <= 0 {
		return 0, 0, 0, 0, src, fmt.Errorf("delta: decoding first value: %w", io.ErrUnexpectedEOF)
	}
	src = src[n:]

	switch {
	case numMiniBlocks == 0:
		err = fmt.Errorf("delta: invalid number of mini-blocks (%d)", numMiniBlocks)
	case blockSize <= 0 || blockSize%128 != 0:
		err = fmt.Errorf("delta: block size is not a multiple of 128 (%d)", blockSize)
	case blockSize > deltaMaxSupportedBlockSize:
		err = fmt.Errorf("delta: block size is too large (%d)", blockSize)
	case numMiniBlocks <= 0 || (blockSize/numMiniBlocks)%32 != 0:
		err = fmt.Errorf("delta: mini-block size is not a multiple of 32 (%d)", blockSize/numMiniBlocks)
	case totalValues < 0:
		err = fmt.Errorf("delta: total value count is negative (%d)", totalValues)
	case totalValues > math.MaxInt32:
		err = fmt.Errorf("delta: too many values (%d)", totalValues)
	}

	return blockSize, numMiniBlocks, totalValues, firstValue, src, err
}

// decodeDeltaBlockHeader parses one block header: the block's minimum delta
// (frame of reference) and the bit width of each of its mini-blocks.
//
// If src is shorter than numMiniBlocks, bitWidths is returned short rather
// than erroring here -- the caller (decodeBlock) will still notice the
// resulting shortfall in decoded values and report it then.
func decodeDeltaBlockHeader(src []byte, numMiniBlocks int) (minDelta int64, bitWidths, rest []byte, err error) {
	minDelta, n := binary.Varint(src)
	if n <= 0 {
		return 0, nil, src, fmt.Errorf("delta: decoding min delta: %w", io.ErrUnexpectedEOF)
	}
	src = src[n:]
	if len(src) < numMiniBlocks {
		return minDelta, src, nil, nil
	}
	return minDelta, src[:numMiniBlocks], src[numMiniBlocks:], nil
}

// decodeDeltaBlock reconstructs a block's actual values in place: out holds
// "delta - minDelta" for each value (decodeBlock leaves 0 in place for
// mini-blocks with a bit width of 0, meaning every delta in that mini-block
// equals minDelta). Returns the new running last value, to seed the next
// block.
func decodeDeltaBlock(out []int64, minDelta, lastValue int64) int64 {
	for i := range out {
		out[i] += minDelta
		out[i] += lastValue
		lastValue = out[i]
	}
	return lastValue
}

// growBytes returns buf resized to length n, growing (and zero-filling the
// newly exposed region) if necessary.
func growBytes(buf []byte, n int) []byte {
	if cap(buf) < n {
		next := make([]byte, n)
		copy(next, buf)
		return next
	}
	if n > len(buf) {
		clear(buf[len(buf):n])
	}
	return buf[:n]
}
