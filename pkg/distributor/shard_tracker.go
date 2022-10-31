package distributor

import (
	"sync"
)

const (
	// defaultStripeSize is the default number of entries to allocate in the
	// stripeSeries list.
	defaultStripeSize = 1 << 10
)

// stripeLock is taken from ruler/storage/wal/series.go
type stripeLock struct {
	sync.RWMutex
	// Padding to avoid multiple locks being on the same cache line.
	_ [40]byte
}

// ShardTracker is a data structure to keep track of the last pushed shard
// number for a given stream hash. This allows the distributor to evenly shard
// streams across pushes even when any given push has fewer entries than the
// calculated number of shards
type ShardTracker struct {
	size         int
	currentShard []int
	locks        []stripeLock
}

func NewShardTracker() *ShardTracker {
	tracker := &ShardTracker{
		size:         defaultStripeSize,
		currentShard: make([]int, defaultStripeSize),
		locks:        make([]stripeLock, defaultStripeSize),
	}

	return tracker
}

func (t *ShardTracker) LastShardNum(streamHash uint64) int {
	i := streamHash & uint64(t.size-1)

	t.locks[i].Lock()
	defer t.locks[i].Unlock()

	return t.currentShard[i]
}

func (t *ShardTracker) SetLastShardNum(streamHash uint64, shardNum int) {
	i := streamHash & uint64(t.size-1)

	t.locks[i].Lock()
	defer t.locks[i].Unlock()

	t.currentShard[i] = shardNum
}
