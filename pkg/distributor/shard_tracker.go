package distributor

import (
	"encoding/binary"
	"math/rand/v2"
	"sync"

	"github.com/cespare/xxhash/v2"
)

const (
	// defaultStripeSize is the number of hash stripes in ShardTracker.
	defaultStripeSize = 1 << 10
)

// stripeLock is taken from ruler/storage/wal/series.go
type stripeLock struct {
	sync.RWMutex
	// Padding to avoid multiple locks being on the same cache line.
	_ [40]byte
}

// ShardTracker rotates pushes across shards using a counter per tenant and hash stripe.
type ShardTracker struct {
	size         int
	currentShard []map[string]uint64
	locks        []stripeLock
	seed         uint64
}

// NewShardTracker creates a tracker with an independent rotation seed.
func NewShardTracker() *ShardTracker {
	tracker := &ShardTracker{
		size:         defaultStripeSize,
		currentShard: make([]map[string]uint64, defaultStripeSize),
		locks:        make([]stripeLock, defaultStripeSize),
		seed:         rand.Uint64(),
	}

	for i := range defaultStripeSize {
		tracker.currentShard[i] = make(map[string]uint64)
	}

	return tracker
}

// Reserve atomically advances by count shards and returns the first shard, with
// numbering wrapping at totalShards. Invalid reservations leave the counter unchanged.
// With randomize enabled, each cycle has a stream-specific phase to avoid persistent
// alignment between distributors or streams sharing a stripe.
func (t *ShardTracker) Reserve(tenant string, streamHash uint64, totalShards, count int, randomize bool) int {
	if totalShards <= 0 || count <= 0 || count > totalShards {
		return 0
	}
	i := streamHash & uint64(t.size-1)
	t.locks[i].Lock()
	start := t.currentShard[i][tenant]
	t.currentShard[i][tenant] = start + uint64(count)
	t.locks[i].Unlock()

	total := uint64(totalShards)
	if !randomize {
		return int(start % total)
	}

	// A fixed phase can strand a stream on the same shards when other streams
	// in its stripe consistently advance the counter by the rest of the cycle.
	var key [24]byte
	binary.LittleEndian.PutUint64(key[:8], t.seed)
	binary.LittleEndian.PutUint64(key[8:16], streamHash)
	binary.LittleEndian.PutUint64(key[16:], start/total)
	phase := xxhash.Sum64(key[:]) % total
	return int((start%total + phase) % total)
}

// LastShardNum reads the shared rotation for the legacy assignment path.
func (t *ShardTracker) LastShardNum(tenant string, streamHash uint64) int {
	i := streamHash & uint64(t.size-1)
	t.locks[i].Lock()
	defer t.locks[i].Unlock()
	return int(t.currentShard[i][tenant])
}

// SetLastShardNum updates the shared rotation for the legacy assignment path.
func (t *ShardTracker) SetLastShardNum(tenant string, streamHash uint64, shardNum int) {
	i := streamHash & uint64(t.size-1)
	t.locks[i].Lock()
	defer t.locks[i].Unlock()
	t.currentShard[i][tenant] = uint64(shardNum)
}
