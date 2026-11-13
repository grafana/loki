package chunkenc

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecbufUvarintRejectsOverflow(t *testing.T) {
	tmp := make([]byte, binary.MaxVarintLen64)

	// A varint that does not fit in an int must be rejected rather than
	// wrapping to a negative value.
	n := binary.PutUvarint(tmp, uint64(math.MaxUint64))
	d := decbuf{b: tmp[:n]}
	require.Equal(t, 0, d.uvarint())
	require.ErrorIs(t, d.err(), ErrInvalidSize)

	// A value that fits is still decoded unchanged.
	n = binary.PutUvarint(tmp, 12345)
	d = decbuf{b: tmp[:n]}
	require.Equal(t, 12345, d.uvarint())
	require.NoError(t, d.err())
}

// A head block whose entry count varint exceeds math.MaxInt used to wrap to a
// negative int and panic in make([]entry, ln). It must now fail cleanly.
func TestHeadBlockLoadBytesRejectsOverflowingLength(t *testing.T) {
	tmp := make([]byte, binary.MaxVarintLen64)
	var b []byte
	b = append(b, ChunkFormatV3) // version

	n := binary.PutUvarint(tmp, uint64(math.MaxUint64)) // entry count
	b = append(b, tmp[:n]...)
	n = binary.PutUvarint(tmp, 0) // size
	b = append(b, tmp[:n]...)
	n = binary.PutVarint(tmp, 0) // mint
	b = append(b, tmp[:n]...)
	n = binary.PutVarint(tmp, 0) // maxt
	b = append(b, tmp[:n]...)

	hb := &headBlock{}
	require.NotPanics(t, func() {
		require.Error(t, hb.LoadBytes(b))
	})
}
