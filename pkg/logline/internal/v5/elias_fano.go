package v5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"slices"

	"github.com/parquet-go/bitpack"
)

// ---------------------------------------------------------------------------
// Elias-Fano encoding (format.PostingsEncodingFastEliasFanoBlocked).
//
// Payload layout for a non-empty, strictly increasing docID list:
//
//	uvarint(count) | uvarint(maxID) | low bits | high bits
//
// L = floor(log2((maxID+1) / count)), or 0 when that ratio is <= 1.
//
//   - Low bits: the low L bits of every docID, packed LSB-first into
//     ceil(count*L / 8) bytes.
//   - High bits: a unary bit vector of (maxID>>L)+count bits, LSB-first and
//     byte-padded. The i-th docID sets bit (docID>>L)+i.
//
// Neither section is self-delimiting; both sizes follow from count and maxID.
// Indexes written with this layout are read back as-is, so it is frozen.
// ---------------------------------------------------------------------------

// eliasFanoSIMDMinValues is the shortest list packed with bitpack. Below it
// the dispatch costs more than it saves. The output bytes are identical
// either way.
const eliasFanoSIMDMinValues = 8

var errEliasFanoEmpty = errors.New("elias-fano: empty docID list")

// appendEliasFano appends the Elias-Fano payload for docIDs to dst. docIDs
// must be non-empty and strictly increasing. On error dst is returned
// unchanged.
func appendEliasFano(dst []byte, docIDs []uint32) ([]byte, error) {
	if len(docIDs) == 0 {
		return dst, errEliasFanoEmpty
	}
	base := len(dst)
	count := uint64(len(docIDs))
	maxID := uint64(docIDs[len(docIDs)-1])
	lowBits := eliasFanoLowBits(maxID+1, count)
	lowBytes := eliasFanoLowBytes(count, lowBits)
	highBytes := int((maxID>>lowBits + count + 7) / 8)

	dst = binary.AppendUvarint(dst, count)
	dst = binary.AppendUvarint(dst, maxID)
	start := len(dst)
	dst = slices.Grow(dst, lowBytes+highBytes)[:start+lowBytes+highBytes]
	lows := dst[start : start+lowBytes]
	highs := dst[start+lowBytes:]

	if lowBits > 0 {
		if len(docIDs) >= eliasFanoSIMDMinValues {
			// bitpack masks each value to lowBits and overwrites every
			// output byte, so docIDs are packed as-is into the reused
			// buffer (see TestBitpackContract).
			bitpack.Pack(lows, docIDs, lowBits)
		} else {
			packLowBits(lows, docIDs, lowBits)
		}
	}

	// The unary high bits are irregular scatter writes and stay scalar.
	clear(highs)
	for i, id := range docIDs {
		// maxID is the last element, so an earlier element above it is out of
		// order and would also index past highs.
		if (i > 0 && id <= docIDs[i-1]) || uint64(id) > maxID {
			return dst[:base], fmt.Errorf("elias-fano: docIDs not strictly increasing at index %d", i)
		}
		pos := uint64(id)>>lowBits + uint64(i)
		highs[pos/8] |= 1 << (pos % 8)
	}
	return dst, nil
}

// decodeEliasFanoInto decodes an Elias-Fano payload into dst, reusing dst
// capacity. The payload comes from object storage, so every malformed input
// returns an error rather than panicking.
func decodeEliasFanoInto(dst []uint32, payload []byte) ([]uint32, error) {
	count, n := binary.Uvarint(payload)
	if n <= 0 || count == 0 || count > math.MaxUint32 {
		return nil, fmt.Errorf("invalid elias-fano count")
	}
	payload = payload[n:]
	maxID, n := binary.Uvarint(payload)
	if n <= 0 || maxID > math.MaxUint32 || count > maxID+1 {
		return nil, fmt.Errorf("invalid elias-fano maximum")
	}
	payload = payload[n:]

	lowBits := eliasFanoLowBits(maxID+1, count)
	lowBytes := eliasFanoLowBytes(count, lowBits)
	highBitCount := maxID>>lowBits + count
	if uint64(len(payload)) != uint64(lowBytes)+(highBitCount+7)/8 {
		return nil, fmt.Errorf("invalid elias-fano payload size")
	}

	out := slices.Grow(dst[:0], int(count))[:count]
	switch {
	case lowBits == 0:
		clear(out)
	case len(payload) >= lowBytes+bitpack.PaddingInt32:
		// Unpack may read PaddingInt32 bytes past the low bits. The high-bit
		// vector that follows supplies them, avoiding a padded copy.
		bitpack.Unpack(out, payload, lowBits)
	default:
		unpackLowBits(out, payload[:lowBytes], lowBits)
	}

	if err := decodeEliasFanoHighBits(out, payload[lowBytes:], lowBits, highBitCount, maxID); err != nil {
		return nil, err
	}
	return out, nil
}

// decodeEliasFanoHighBits combines the unary high parts in highs with the low
// parts already in out. It scans 64 bits at a time, which removes most of the
// per-byte loop overhead from an otherwise irregular stream.
func decodeEliasFanoHighBits(out []uint32, highs []byte, lowBits uint, highBitCount, maxID uint64) error {
	decoded := 0
	var prev uint64
	for base := 0; base < len(highs); base += 8 {
		var word uint64
		if base+8 <= len(highs) {
			word = binary.LittleEndian.Uint64(highs[base:])
		} else {
			var tail [8]byte
			copy(tail[:], highs[base:])
			word = binary.LittleEndian.Uint64(tail[:])
		}
		for word != 0 {
			pos := uint64(base)*8 + uint64(bits.TrailingZeros64(word))
			word &= word - 1
			if pos >= highBitCount || decoded == len(out) {
				return fmt.Errorf("invalid elias-fano high bits")
			}
			// The i-th set bit is at position >= i, so this cannot underflow.
			id := (pos-uint64(decoded))<<lowBits | uint64(out[decoded])
			if id > maxID || (decoded > 0 && id <= prev) {
				return fmt.Errorf("invalid elias-fano doc ID")
			}
			out[decoded] = uint32(id)
			prev = id
			decoded++
		}
	}
	if decoded != len(out) || prev != maxID {
		return fmt.Errorf("elias-fano count or maximum mismatch")
	}
	return nil
}

func eliasFanoLowBits(universe, count uint64) uint {
	ratio := universe / count
	if ratio <= 1 {
		return 0
	}
	return uint(bits.Len64(ratio) - 1)
}

func eliasFanoLowBytes(count uint64, lowBits uint) int {
	return int((count*uint64(lowBits) + 7) / 8)
}

// packLowBits packs the low width bits of each value LSB-first into dst,
// overwriting every byte. dst must be eliasFanoLowBytes(len(values), width)
// long.
func packLowBits(dst []byte, values []uint32, width uint) {
	mask := uint64(1)<<width - 1
	var acc uint64
	var used uint
	j := 0
	for _, v := range values {
		acc |= (uint64(v) & mask) << used
		used += width
		for used >= 8 {
			dst[j] = byte(acc)
			j++
			acc >>= 8
			used -= 8
		}
	}
	if used > 0 {
		dst[j] = byte(acc)
	}
}

// unpackLowBits is the scalar inverse of packLowBits, used when the payload
// is too short to give bitpack.Unpack its read padding.
func unpackLowBits(dst []uint32, src []byte, width uint) {
	mask := uint64(1)<<width - 1
	var acc uint64
	var avail uint
	j := 0
	for i := range dst {
		for avail < width {
			acc |= uint64(src[j]) << avail
			avail += 8
			j++
		}
		dst[i] = uint32(acc & mask)
		acc >>= width
		avail -= width
	}
}
