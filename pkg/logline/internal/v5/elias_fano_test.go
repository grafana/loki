package v5

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/parquet-go/bitpack"
	"github.com/stretchr/testify/require"
)

// referenceEliasFano is a deliberately naive, bit-at-a-time encoder written
// straight from the layout documented in elias_fano.go. It shares no code with
// the production encoder so the two can be checked against each other.
func referenceEliasFano(docIDs []uint32) []byte {
	count := uint64(len(docIDs))
	maxID := uint64(docIDs[len(docIDs)-1])
	var lowBits uint
	for uint64(1)<<(lowBits+1) <= (maxID+1)/count {
		lowBits++
	}

	lows := make([]byte, (count*uint64(lowBits)+7)/8)
	highs := make([]byte, (maxID>>lowBits+count+7)/8)
	for i, id := range docIDs {
		for b := range lowBits {
			if id>>b&1 == 1 {
				bit := uint64(i)*uint64(lowBits) + uint64(b)
				lows[bit/8] |= 1 << (bit % 8)
			}
		}
		pos := uint64(id)>>lowBits + uint64(i)
		highs[pos/8] |= 1 << (pos % 8)
	}

	out := binary.AppendUvarint(nil, count)
	out = binary.AppendUvarint(out, maxID)
	out = append(out, lows...)
	return append(out, highs...)
}

// TestEliasFanoGolden pins the on-disk layout. The expected bytes are worked
// out by hand from the layout, not produced by the encoder. If this fails the
// format has changed and existing indexes would no longer decode: revert.
func TestEliasFanoGolden(t *testing.T) {
	cases := []struct {
		name   string
		docIDs []uint32
		want   []byte
	}{
		{
			// count=1 max=0: L=0, one high bit at position 0.
			name:   "single_zero",
			docIDs: []uint32{0},
			want:   []byte{0x01, 0x00, 0x01},
		},
		{
			// count=4 max=21: L=floor(log2(22/4))=2.
			// lows 01,11,00,01 -> 0x4D; highs at 0,1,4,8 -> 0x13 0x01.
			name:   "scalar_low_bits",
			docIDs: []uint32{1, 3, 8, 21},
			want:   []byte{0x04, 0x15, 0x4D, 0x13, 0x01},
		},
		{
			// count=8 max=7: L=0, highs at 0,2,...,14.
			name:   "dense_no_low_bits",
			docIDs: []uint32{0, 1, 2, 3, 4, 5, 6, 7},
			want:   []byte{0x08, 0x07, 0x55, 0x55},
		},
		{
			// count=8 max=29: L=1, packed through bitpack. Every low bit is
			// 1 -> 0xFF; highs at 0,3,...,21 -> 0x49 0x92 0x24.
			name:   "simd_low_bits",
			docIDs: []uint32{1, 5, 9, 13, 17, 21, 25, 29},
			want:   []byte{0x08, 0x1D, 0xFF, 0x49, 0x92, 0x24},
		},
		{
			// count=1 max=2^32-1: L=32, the widest possible low part.
			name:   "max_uint32",
			docIDs: []uint32{math.MaxUint32},
			want:   []byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F, 0xFF, 0xFF, 0xFF, 0xFF, 0x01},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, referenceEliasFano(tc.docIDs), "reference encoder disagrees with hand-computed bytes")

			got, err := appendEliasFano(nil, tc.docIDs)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)

			decoded, err := decodeEliasFanoInto(nil, tc.want)
			require.NoError(t, err)
			require.Equal(t, tc.docIDs, decoded)
		})
	}
}

// eliasFanoGrid returns n strictly increasing docIDs whose Elias-Fano low-bit
// width is exactly lowBits: element i has high part i and random low bits,
// and the last element is n<<lowBits - 1. Random low bits sit above the low
// part in every element, which exercises bitpack's masking.
func eliasFanoGrid(rng *rand.Rand, n int, lowBits uint) []uint32 {
	ids := make([]uint32, n)
	lowMask := uint64(1)<<lowBits - 1
	for i := range ids {
		ids[i] = uint32(uint64(i)<<lowBits | rng.Uint64()&lowMask)
	}
	ids[n-1] = uint32(uint64(n)<<lowBits - 1)
	return ids
}

// eliasFanoRandomGaps returns up to n strictly increasing docIDs with random
// gaps averaging about 2^meanGapBits, giving irregular high-bit patterns.
func eliasFanoRandomGaps(rng *rand.Rand, n int, meanGapBits uint) []uint32 {
	ids := make([]uint32, 0, n)
	next := rng.Uint64N(uint64(1) << meanGapBits)
	for len(ids) < n && next <= math.MaxUint32 {
		ids = append(ids, uint32(next))
		next += 1 + rng.Uint64N(uint64(1)<<(meanGapBits+1))
	}
	return ids
}

func TestEliasFanoMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	lengths := []int{1, 2, 3, 7, 8, 9, 15, 16, 17, 31, 63, 64, 65, 1000, 4097}

	var inputs [][]uint32
	for _, n := range lengths {
		for lowBits := uint(0); lowBits <= 32; lowBits++ {
			if uint64(n)<<lowBits > uint64(1)<<32 {
				continue
			}
			inputs = append(inputs, eliasFanoGrid(rng, n, lowBits))
		}
		for gapBits := uint(0); gapBits <= 24; gapBits += 3 {
			if ids := eliasFanoRandomGaps(rng, n, gapBits); len(ids) > 0 {
				inputs = append(inputs, ids)
			}
		}
	}

	// Every width bitpack can be asked for (1..29 for lists of >= 8 values)
	// must be covered on the SIMD path.
	simdWidths := map[uint]bool{}
	for _, ids := range inputs {
		if len(ids) >= eliasFanoSIMDMinValues {
			simdWidths[eliasFanoLowBits(uint64(ids[len(ids)-1])+1, uint64(len(ids)))] = true
		}
	}
	for w := uint(1); w <= 29; w++ {
		require.True(t, simdWidths[w], "no SIMD-path input with %d low bits", w)
	}

	for _, ids := range inputs {
		name := fmt.Sprintf("n=%d/max=%d", len(ids), ids[len(ids)-1])
		want := referenceEliasFano(ids)

		got, err := appendEliasFano(nil, ids)
		require.NoError(t, err, name)
		require.Equal(t, want, got, name)

		// Reused buffers hold stale bytes from earlier terms. The encoder
		// must overwrite them, and it must leave the existing prefix alone.
		dirty := bytes.Repeat([]byte{0xAB}, 3+len(want)+64)
		got, err = appendEliasFano(dirty[:3], ids)
		require.NoError(t, err, name)
		require.Equal(t, append([]byte{0xAB, 0xAB, 0xAB}, want...), got, name)

		decoded, err := decodeEliasFanoInto(nil, want)
		require.NoError(t, err, name)
		require.Equal(t, ids, decoded, name)

		stale := make([]uint32, 5, len(ids)+8)
		for i := range stale {
			stale[i] = 0xDEADBEEF
		}
		decoded, err = decodeEliasFanoInto(stale, want)
		require.NoError(t, err, name)
		require.Equal(t, ids, decoded, name)
	}
}

func TestEliasFanoEncodeRejectsInvalidInput(t *testing.T) {
	for _, ids := range [][]uint32{
		nil,
		{1, 1},
		{5, 3},
		// 100 is above the last element (the maximum used for sizing), so a
		// missing check would write past the high-bit vector.
		{1, 100, 5},
		{0, 1, 2, 3, 4, 5, 6, 7, 1 << 30, 9},
	} {
		prefix := []byte{0x42}
		got, err := appendEliasFano(prefix, ids)
		require.Error(t, err, "%v", ids)
		require.Equal(t, prefix, got, "dst must be unchanged on error")
	}
}

func TestEliasFanoDecodeRejectsCorruptPayloads(t *testing.T) {
	valid := referenceEliasFano([]uint32{1, 5, 9, 13, 17, 21, 25, 29, 40, 1000})
	cases := map[string][]byte{
		"empty":             {},
		"zero_count":        {0x00, 0x00},
		"count_over_uint32": binary.AppendUvarint(nil, 1<<32),
		"max_over_uint32":   binary.AppendUvarint([]byte{0x01}, 1<<32),
		"count_above_max+1": {0x05, 0x02, 0xFF},
		"truncated":         valid[:len(valid)-1],
		"trailing_byte":     append(slices.Clone(valid), 0x00),
		// count=2 max=1 (L=0, 3 high bits). Bits 0 and 1 decode to 0 and 0.
		"duplicate_id": {0x02, 0x01, 0x03},
		// Only one high bit for a count of two.
		"missing_high_bit": {0x02, 0x01, 0x01},
		// Three high bits for a count of two.
		"extra_high_bit": {0x02, 0x01, 0x07},
		// High bit set in the byte padding, past the vector's end.
		"high_bit_in_padding": {0x02, 0x01, 0x85},
		// count=1 max=3 (L=2): the single ID decodes to 0, not 3.
		"max_mismatch": {0x01, 0x03, 0x00, 0x01},
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := decodeEliasFanoInto(nil, payload)
			require.Error(t, err)
		})
	}
}

func FuzzDecodeEliasFano(f *testing.F) {
	rng := rand.New(rand.NewPCG(3, 4))
	f.Add([]byte{0x04, 0x15, 0x4D, 0x13, 0x01})
	f.Add(referenceEliasFano(eliasFanoGrid(rng, 64, 7)))
	f.Add(referenceEliasFano(eliasFanoRandomGaps(rng, 300, 5)))
	f.Fuzz(func(t *testing.T, payload []byte) {
		ids, err := decodeEliasFanoInto(nil, payload)
		if err != nil {
			return
		}
		count, _ := binary.Uvarint(payload)
		require.Len(t, ids, int(count))
		for i := 1; i < len(ids); i++ {
			require.Less(t, ids[i-1], ids[i])
		}
		// Padding bits in the low section are ignored, so re-encoding need
		// not reproduce payload byte for byte, but it must decode the same.
		reencoded, err := appendEliasFano(nil, ids)
		require.NoError(t, err)
		again, err := decodeEliasFanoInto(nil, reencoded)
		require.NoError(t, err)
		require.Equal(t, ids, again)
	})
}

// TestBitpackContract pins the parquet-go/bitpack behaviour the Elias-Fano
// codec relies on, for whichever kernels this platform dispatches to:
//   - Pack masks each value to the bit width, so docIDs are packed unmasked.
//   - Pack overwrites every output byte, so reused buffers need no clearing.
//   - Pack writes nothing past ByteCount, and Unpack nothing past len(dst).
//   - Unpack ignores whatever sits in its read padding (the high bits).
func TestBitpackContract(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	for width := uint(1); width <= 32; width++ {
		for _, n := range []int{8, 9, 15, 16, 17, 63, 64, 65, 1000, 4097} {
			values := make([]uint32, n)
			masked := make([]uint32, n)
			for i := range values {
				values[i] = rng.Uint32()
				masked[i] = uint32(uint64(values[i]) & (uint64(1)<<width - 1))
			}
			size := bitpack.ByteCount(width * uint(n))
			name := fmt.Sprintf("width=%d/n=%d", width, n)

			want := make([]byte, size)
			packLowBits(want, masked, width)

			backing := bytes.Repeat([]byte{0xFF}, size+64)
			bitpack.Pack(backing[:size], values, width)
			require.Equal(t, want, backing[:size], name)
			require.Equal(t, bytes.Repeat([]byte{0xFF}, 64), backing[size:], "%s: Pack wrote past ByteCount", name)

			src := make([]byte, size+bitpack.PaddingInt32)
			copy(src, want)
			for i := size; i < len(src); i++ {
				src[i] = byte(rng.Uint32())
			}
			out := make([]uint32, n+16)
			for i := range out {
				out[i] = 0xDEADBEEF
			}
			bitpack.Unpack(out[:n], src, width)
			require.Equal(t, masked, out[:n], name)
			for _, v := range out[n:] {
				require.Equal(t, uint32(0xDEADBEEF), v, "%s: Unpack wrote past len(dst)", name)
			}
		}
	}
}

func BenchmarkEliasFano(b *testing.B) {
	rng := rand.New(rand.NewPCG(7, 8))
	for _, tc := range []struct {
		name string
		ids  []uint32
	}{
		{"n=5", eliasFanoRandomGaps(rng, 5, 12)},
		{"n=1k/sparse", eliasFanoRandomGaps(rng, 1000, 9)},
		{"n=100k/dense", eliasFanoRandomGaps(rng, 100_000, 2)},
	} {
		payload, err := appendEliasFano(nil, tc.ids)
		require.NoError(b, err)
		varint := appendDeltaVarInt(nil, tc.ids)
		b.Run("encode/"+tc.name, func(b *testing.B) {
			b.SetBytes(int64(4 * len(tc.ids)))
			buf := make([]byte, 0, len(payload))
			for b.Loop() {
				buf, _ = appendEliasFano(buf[:0], tc.ids)
			}
		})
		b.Run("decode/"+tc.name, func(b *testing.B) {
			b.SetBytes(int64(4 * len(tc.ids)))
			out := make([]uint32, 0, len(tc.ids))
			for b.Loop() {
				out, _ = decodeEliasFanoInto(out, payload)
			}
			b.ReportMetric(float64(len(payload))/float64(len(varint)), "size/varint")
		})
	}
}
