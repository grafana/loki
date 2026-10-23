package builder

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryFlushThresholdBytes(t *testing.T) {
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })

	require.Zero(t, memoryFlushThresholdBytes())

	debug.SetMemoryLimit(1000)
	require.Equal(t, uint64(700), memoryFlushThresholdBytes())
}

// TestPostingsBuffer_ResidentBytes pins the capacity-based accounting: the
// four sort buffers count in full from allocation (12 B/pair live + 12 B/pair
// scratch), the lazily-allocated shard-reorder scratch counts at 1 B/pair once
// the first sharded spill creates it, each first-touched (shard, day) adds its
// bitset, releasing the sort buffers drops exactly the buffer+scratch terms,
// and clear zeroes the rest.
func TestPostingsBuffer_ResidentBytes(t *testing.T) {
	const batch = 64
	ticksPerDay := uint64((24 * 60 * 60 * 1000) / 100) // 100ms interval
	b := newPostingsBuffer(postingsBufferConfig{
		bufferPairs:    batch,
		spillWatermark: 0.75,
		ticksPerDay:    ticksPerDay,
	})

	bufferBytes := uint64(2*batch*8 + 2*batch*4) // keys+keyBuf, docs+docBuf
	require.Equal(t, bufferBytes, b.residentBytes(), "empty buffer must report full capacity")

	// Appending pairs must not change the estimate — capacity is what's resident.
	b.appendPair([8]byte{'a'}, 1)
	require.Equal(t, bufferBytes, b.residentBytes())

	// First touch of a (shard, day) adds one dense bitset; a second touch of
	// the same key adds nothing.
	bitsetBytes := ((ticksPerDay + 63) / 64) * 8
	b.recordDocumentTick(0, 1)
	require.Equal(t, bufferBytes+bitsetBytes, b.residentBytes())
	b.recordDocumentTick(0, 2)
	require.Equal(t, bufferBytes+bitsetBytes, b.residentBytes())
	b.recordDocumentTick(1, 1)
	require.Equal(t, bufferBytes+2*bitsetBytes, b.residentBytes())

	// The merge releases the sort buffers; only refTicks remains resident.
	b.releaseSortBuffers()
	require.Equal(t, 2*bitsetBytes, b.residentBytes())

	b.clear()
	require.Zero(t, b.residentBytes())

	// Sharded buffers additionally count the shard-reorder scratch (1 B/pair),
	// which exists only after the first sharded spill allocates it lazily.
	bs := newPostingsBuffer(postingsBufferConfig{
		bufferPairs:    batch,
		spillWatermark: 0.75,
		ticksPerDay:    ticksPerDay,
		shardCount:     2,
		shardFn:        func(ngram [8]byte, count int) int { return int(ngram[0]) % count },
	})
	require.Equal(t, bufferBytes, bs.residentBytes(), "scratch must not count before it is allocated")
	bs.appendPair([8]byte{'a'}, 1) // shard 1 (0x61 % 2)
	bs.appendPair([8]byte{'b'}, 2) // shard 0 (0x62 % 2)
	bs.sortedHead = 2
	bs.shardSortAndTrack(bs.sortedHead)
	scratchBytes := uint64(cap(bs.shardScratch))
	require.Equal(t, uint64(2), scratchBytes, "shard scratch is 1 B per reordered pair")
	// Two pairs in two distinct shards on the same day: two bitsets.
	require.Equal(t, bufferBytes+scratchBytes+2*bitsetBytes, bs.residentBytes())

	// releaseSortBuffers drops the scratch along with the sort buffers.
	bs.releaseSortBuffers()
	require.Equal(t, 2*bitsetBytes, bs.residentBytes())
}
