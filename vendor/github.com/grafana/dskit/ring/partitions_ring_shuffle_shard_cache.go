package ring

import (
	"math"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// shuffleShardCacheStorage is a generic interface for cache storage.
// This abstracts the difference between map-based and LRU-based storage.
type shuffleShardCacheStorage[V any] interface {
	get(key subringCacheKey) (V, bool)
	set(key subringCacheKey, value V)
	len() int
}

// partitionRingShuffleShardCache delegates storage operations to two generic cache storage implementations.
// All cache operations are protected by a mutex to ensure thread-safety for compound operations
// like check-then-act patterns (e.g., in setSubringWithLookback).
type partitionRingShuffleShardCache struct {
	mtx                  sync.RWMutex
	cacheWithoutLookback shuffleShardCacheStorage[*PartitionRing]
	cacheWithLookback    shuffleShardCacheStorage[cachedSubringWithLookback[*PartitionRing]]
}

// newPartitionRingShuffleShardCache creates a new partition ring shuffle shard cache.
// If size <= 0 an unbounded map-based cache is used.
// If size > 0, a zero-alloc direct-mapped cache with the specified capacity is used.
func newPartitionRingShuffleShardCache(size int) (*partitionRingShuffleShardCache, error) {
	var cacheWithoutLookback shuffleShardCacheStorage[*PartitionRing]
	var cacheWithLookback shuffleShardCacheStorage[cachedSubringWithLookback[*PartitionRing]]

	if size > 0 {
		cacheWithoutLookback = newDirectMappedCacheStorage[*PartitionRing](size)
		cacheWithLookback = newDirectMappedCacheStorage[cachedSubringWithLookback[*PartitionRing]](size)
	} else {
		cacheWithoutLookback = newMapCacheStorage[*PartitionRing]()
		cacheWithLookback = newMapCacheStorage[cachedSubringWithLookback[*PartitionRing]]()
	}

	return &partitionRingShuffleShardCache{
		cacheWithoutLookback: cacheWithoutLookback,
		cacheWithLookback:    cacheWithLookback,
	}, nil
}

func (r *partitionRingShuffleShardCache) setSubring(identifier string, size int, subring *PartitionRing) {
	if subring == nil {
		return
	}

	r.mtx.Lock()
	defer r.mtx.Unlock()

	r.cacheWithoutLookback.set(subringCacheKey{identifier: identifier, shardSize: size}, subring)
}

func (r *partitionRingShuffleShardCache) getSubring(identifier string, size int) *PartitionRing {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	cached, ok := r.cacheWithoutLookback.get(subringCacheKey{identifier: identifier, shardSize: size})
	if !ok {
		return nil
	}

	return cached
}

func (r *partitionRingShuffleShardCache) setSubringWithLookback(identifier string, size int, lookbackPeriod time.Duration, now time.Time, subring *PartitionRing) {
	if subring == nil {
		return
	}

	var (
		lookbackWindowStart                   = now.Add(-lookbackPeriod).Unix()
		validForLookbackWindowsStartingBefore = int64(math.MaxInt64)
	)

	for id, partition := range subring.desc.Partitions {
		if !subring.isInSubring(id) {
			continue
		}
		stateChangedDuringLookbackWindow := partition.StateTimestamp >= lookbackWindowStart

		if stateChangedDuringLookbackWindow && partition.StateTimestamp < validForLookbackWindowsStartingBefore {
			validForLookbackWindowsStartingBefore = partition.StateTimestamp
		}
	}

	// Only update cache if subring's lookback window starts later than the previously cached subring for this identifier,
	// if there is one. This prevents cache thrashing due to different calls competing if their lookback windows start
	// before and after the time a partition state has changed.
	key := subringCacheKey{identifier: identifier, shardSize: size, lookbackPeriod: lookbackPeriod}

	r.mtx.Lock()
	defer r.mtx.Unlock()

	if existingEntry, haveCached := r.cacheWithLookback.get(key); !haveCached || existingEntry.validForLookbackWindowsStartingAfter < lookbackWindowStart {
		r.cacheWithLookback.set(key, cachedSubringWithLookback[*PartitionRing]{
			subring:                               subring,
			validForLookbackWindowsStartingAfter:  lookbackWindowStart,
			validForLookbackWindowsStartingBefore: validForLookbackWindowsStartingBefore,
		})
	}
}

func (r *partitionRingShuffleShardCache) getSubringWithLookback(identifier string, size int, lookbackPeriod time.Duration, now time.Time) *PartitionRing {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	cached, ok := r.cacheWithLookback.get(subringCacheKey{identifier: identifier, shardSize: size, lookbackPeriod: lookbackPeriod})
	if !ok {
		return nil
	}

	lookbackWindowStart := now.Add(-lookbackPeriod).Unix()
	if lookbackWindowStart < cached.validForLookbackWindowsStartingAfter || lookbackWindowStart > cached.validForLookbackWindowsStartingBefore {
		// The cached subring is not valid for the lookback window that has been requested.
		return nil
	}

	return cached.subring
}

// mapCacheStorage is a generic map-based implementation of shuffleShardCacheStorage.
// Note: This implementation does not have its own mutex because thread-safety is guaranteed
// by the mutex in partitionRingShuffleShardCache.
type mapCacheStorage[V any] struct {
	cache map[subringCacheKey]V
}

var _ shuffleShardCacheStorage[*PartitionRing] = (*mapCacheStorage[*PartitionRing])(nil)

func newMapCacheStorage[V any]() *mapCacheStorage[V] {
	return &mapCacheStorage[V]{
		cache: make(map[subringCacheKey]V),
	}
}

func (s *mapCacheStorage[V]) get(key subringCacheKey) (V, bool) { //nolint:unused
	cached, ok := s.cache[key]
	return cached, ok
}

func (s *mapCacheStorage[V]) set(key subringCacheKey, value V) { //nolint:unused
	s.cache[key] = value
}

func (s *mapCacheStorage[V]) len() int { //nolint:unused
	return len(s.cache)
}

// lruCacheStorage is a generic LRU-based implementation of shuffleShardCacheStorage.
type lruCacheStorage[V any] struct {
	cache *lru.Cache[subringCacheKey, V]
}

var _ shuffleShardCacheStorage[*PartitionRing] = (*lruCacheStorage[*PartitionRing])(nil)

func newLRUCacheStorage[V any](size int) (*lruCacheStorage[V], error) {
	cache, err := lru.New[subringCacheKey, V](size)
	if err != nil {
		return nil, err
	}
	return &lruCacheStorage[V]{cache: cache}, nil
}

func (s *lruCacheStorage[V]) get(key subringCacheKey) (V, bool) { //nolint:unused
	return s.cache.Get(key)
}

func (s *lruCacheStorage[V]) set(key subringCacheKey, value V) { //nolint:unused
	s.cache.Add(key, value)
}

func (s *lruCacheStorage[V]) len() int { //nolint:unused
	return s.cache.Len()
}

// directMappedCacheStorage is a fixed-capacity, zero-alloc direct-mapped cache.
// On every set() the entry is placed at slots[hash(key)%N] (no allocation).
// This eliminates the per-insertion list-node alloc of lruCacheStorage.
// Eviction is implicit: a new entry silently overwrites a colliding slot.
// The capacity must be a power of two.
type directMappedCacheStorage[V any] struct {
	mask  uint64
	slots []directMappedSlot[V]
}

type directMappedSlot[V any] struct {
	key   subringCacheKey
	value V
	valid bool
}

var _ shuffleShardCacheStorage[*PartitionRing] = (*directMappedCacheStorage[*PartitionRing])(nil)

func newDirectMappedCacheStorage[V any](size int) *directMappedCacheStorage[V] {
	// Round size up to the next power of two.
	n := 1
	for n < size {
		n <<= 1
	}
	return &directMappedCacheStorage[V]{
		mask:  uint64(n - 1),
		slots: make([]directMappedSlot[V], n),
	}
}

func hashSubringCacheKey(key subringCacheKey) uint64 {
	// FNV-1a over the identifier bytes, then mix in shardSize and lookbackPeriod.
	const (
		fnvOffset = uint64(14695981039346656037)
		fnvPrime  = uint64(1099511628211)
	)
	h := fnvOffset
	for i := 0; i < len(key.identifier); i++ {
		h ^= uint64(key.identifier[i])
		h *= fnvPrime
	}
	h ^= uint64(key.shardSize)
	h *= fnvPrime
	h ^= uint64(key.lookbackPeriod)
	h *= fnvPrime
	return h
}

func (s *directMappedCacheStorage[V]) get(key subringCacheKey) (V, bool) { //nolint:unused
	idx := hashSubringCacheKey(key) & s.mask
	slot := &s.slots[idx]
	if slot.valid && slot.key == key {
		return slot.value, true
	}
	var zero V
	return zero, false
}

func (s *directMappedCacheStorage[V]) set(key subringCacheKey, value V) { //nolint:unused
	idx := hashSubringCacheKey(key) & s.mask
	s.slots[idx] = directMappedSlot[V]{key: key, value: value, valid: true}
}

func (s *directMappedCacheStorage[V]) len() int { //nolint:unused
	count := 0
	for i := range s.slots {
		if s.slots[i].valid {
			count++
		}
	}
	return count
}
