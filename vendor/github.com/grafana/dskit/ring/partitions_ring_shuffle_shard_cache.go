package ring

import (
	"math"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

type partitionRingShuffleShardCache struct {
	mtx                  sync.RWMutex
	cacheWithoutLookback map[subringCacheKey]*PartitionRing
	cacheWithLookback    map[subringCacheKey]cachedSubringWithLookback[*PartitionRing]
	logger               log.Logger
}

func newPartitionRingShuffleShardCache(logger log.Logger) *partitionRingShuffleShardCache {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &partitionRingShuffleShardCache{
		cacheWithoutLookback: map[subringCacheKey]*PartitionRing{},
		cacheWithLookback:    map[subringCacheKey]cachedSubringWithLookback[*PartitionRing]{},
		logger:               logger,
	}
}

func (r *partitionRingShuffleShardCache) setSubring(identifier string, size int, subring *PartitionRing) {
	if subring == nil {
		return
	}

	r.mtx.Lock()
	defer r.mtx.Unlock()

	r.cacheWithoutLookback[subringCacheKey{identifier: identifier, shardSize: size}] = subring

	// Log cache size after adding
	cacheSize := len(r.cacheWithoutLookback)
	level.Info(r.logger).Log(
		"msg", "shuffle shard cache without lookback size",
		"cache_type", "without_lookback",
		"size", cacheSize,
		"identifier", identifier,
		"shard_size", size,
	)
}

func (r *partitionRingShuffleShardCache) getSubring(identifier string, size int) *PartitionRing {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	cached := r.cacheWithoutLookback[subringCacheKey{identifier: identifier, shardSize: size}]
	if cached == nil {
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

	for _, partition := range subring.desc.Partitions {
		stateChangedDuringLookbackWindow := partition.StateTimestamp >= lookbackWindowStart

		if stateChangedDuringLookbackWindow && partition.StateTimestamp < validForLookbackWindowsStartingBefore {
			validForLookbackWindowsStartingBefore = partition.StateTimestamp
		}
	}

	r.mtx.Lock()
	defer r.mtx.Unlock()

	// Only update cache if subring's lookback window starts later than the previously cached subring for this identifier,
	// if there is one. This prevents cache thrashing due to different calls competing if their lookback windows start
	// before and after the time a partition state has changed.
	key := subringCacheKey{identifier: identifier, shardSize: size, lookbackPeriod: lookbackPeriod}

	if existingEntry, haveCached := r.cacheWithLookback[key]; !haveCached || existingEntry.validForLookbackWindowsStartingAfter < lookbackWindowStart {
		r.cacheWithLookback[key] = cachedSubringWithLookback[*PartitionRing]{
			subring:                               subring,
			validForLookbackWindowsStartingAfter:  lookbackWindowStart,
			validForLookbackWindowsStartingBefore: validForLookbackWindowsStartingBefore,
		}

		// Log cache size after adding
		cacheSize := len(r.cacheWithLookback)
		level.Info(r.logger).Log(
			"msg", "shuffle shard cache with lookback size",
			"cache_type", "with_lookback",
			"size", cacheSize,
			"identifier", identifier,
			"shard_size", size,
			"lookback_period", lookbackPeriod,
		)
	}
}

func (r *partitionRingShuffleShardCache) getSubringWithLookback(identifier string, size int, lookbackPeriod time.Duration, now time.Time) *PartitionRing {
	r.mtx.RLock()
	defer r.mtx.RUnlock()

	cached, ok := r.cacheWithLookback[subringCacheKey{identifier: identifier, shardSize: size, lookbackPeriod: lookbackPeriod}]
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
