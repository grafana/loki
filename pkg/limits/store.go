package limits

import (
	"hash/fnv"
	"sync"
	"time"

	"github.com/coder/quartz"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// The number of stripe locks.
const numStripes = 64

// iterateFunc is a closure called for each stream.
type iterateFunc func(tenant string, partition int32, stream streamUsage)

// condFunc is a function that is called for each stream passed to update,
// and is often used to check if a stream can be stored (for example, with
// the max series limit). It should return true if the stream can be stored.
type condFunc func(acc float64, stream *proto.StreamMetadata) bool

// usageStore stores per-tenant stream usage data.
type usageStore struct {
	activeWindow  time.Duration
	rateWindow    time.Duration
	bucketSize    time.Duration
	numPartitions int
	stripes       []map[string]tenantUsage
	locks         []stripeLock

	// Used for tests.
	clock quartz.Clock
}

// tenantUsage contains the per-partition stream usage for a tenant.
type tenantUsage map[int32]map[uint64]streamUsage

// streamUsage represents the metadata for a stream loaded from the kafka topic.
// It contains the minimal information to count per tenant active streams and
// rate limits.
type streamUsage struct {
	hash        uint64
	lastSeenAt  int64
	totalSize   uint64
	rateBuckets []rateBucket
}

// RateBucket represents the bytes received during a specific time interval
// It is used to calculate the rate limit for a stream.
type rateBucket struct {
	timestamp int64  // start of the interval
	size      uint64 // bytes received during this interval
}

type stripeLock struct {
	sync.RWMutex
	// Padding to avoid multiple locks being on the same cache line.
	_ [40]byte
}

// newUsageStore returns a new UsageStore.
func newUsageStore(activeWindow, rateWindow, bucketSize time.Duration, numPartitions int) *usageStore {
	s := &usageStore{
		activeWindow:  activeWindow,
		rateWindow:    rateWindow,
		bucketSize:    bucketSize,
		numPartitions: numPartitions,
		stripes:       make([]map[string]tenantUsage, numStripes),
		locks:         make([]stripeLock, numStripes),
		clock:         quartz.NewReal(),
	}
	for i := range s.stripes {
		s.stripes[i] = make(map[string]tenantUsage)
	}
	return s
}

// Iter iterates all active streams and calls f for each iterated stream.
// As this method acquires a read lock, f must not block.
func (s *usageStore) Iter(fn iterateFunc) {
	s.forEachRLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for partition, streams := range partitions {
				for _, stream := range streams {
					fn(tenant, partition, stream)
				}
			}
		}
	})
}

// IterTenant iterates all active streams for the tenant and calls f for
// each iterated stream. As this method acquires a read lock, f must not
// block.
func (s *usageStore) IterTenant(tenant string, fn iterateFunc) {
	s.withRLock(tenant, func(i int) {
		for partition, streams := range s.stripes[i][tenant] {
			for _, stream := range streams {
				fn(tenant, partition, stream)
			}
		}
	})
}

func (s *usageStore) Update(tenant string, streams []*proto.StreamMetadata, lastSeenAt time.Time, cond condFunc) ([]*proto.StreamMetadata, []*proto.StreamMetadata) {
	var (
		// Calculate the cutoff for the window size
		cutoff = lastSeenAt.Add(-s.activeWindow).UnixNano()
		// Get the bucket for this timestamp using the configured interval duration
		bucketStart = lastSeenAt.Truncate(s.bucketSize).UnixNano()
		// Calculate the rate window cutoff for cleaning up old buckets
		bucketCutoff = lastSeenAt.Add(-s.rateWindow).UnixNano()
		stored       = make([]*proto.StreamMetadata, 0, len(streams))
		rejected     = make([]*proto.StreamMetadata, 0, len(streams))
	)
	s.withLock(tenant, func(i int) {
		if _, ok := s.stripes[i][tenant]; !ok {
			s.stripes[i][tenant] = make(tenantUsage)
		}

		activeStreams := make(map[int32]int)

		for _, stream := range streams {
			partition := s.getPartitionForHash(stream.StreamHash)

			if _, ok := s.stripes[i][tenant][partition]; !ok {
				s.stripes[i][tenant][partition] = make(map[uint64]streamUsage)
			}

			// Count as active streams all streams that are not expired.
			if _, ok := activeStreams[partition]; !ok {
				for _, stored := range s.stripes[i][tenant][partition] {
					if stored.lastSeenAt >= cutoff {
						activeStreams[partition]++
					}
				}
			}

			recorded, found := s.stripes[i][tenant][partition][stream.StreamHash]

			// If the stream is new or expired, check if it exceeds the limit.
			// If limit is not exceeded and the stream is expired, reset the stream.
			if !found || (recorded.lastSeenAt < cutoff) {
				activeStreams[partition]++

				if cond != nil && !cond(float64(activeStreams[partition]), stream) {
					rejected = append(rejected, stream)
					continue
				}

				// If the stream is stored and expired, reset the stream
				if found && recorded.lastSeenAt < cutoff {
					s.stripes[i][tenant][partition][stream.StreamHash] = streamUsage{hash: stream.StreamHash, lastSeenAt: lastSeenAt.UnixNano()}
				}
			}

			s.storeStream(i, tenant, partition, stream.StreamHash, stream.TotalSize, lastSeenAt, bucketStart, bucketCutoff)

			stored = append(stored, stream)
		}
	})

	return stored, rejected
}

// Evict evicts all streams that have not been seen within the window.
func (s *usageStore) Evict() map[string]int {
	cutoff := s.clock.Now().Add(-s.activeWindow).UnixNano()
	evicted := make(map[string]int)
	s.forEachLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for partition, streams := range partitions {
				for streamHash, stream := range streams {
					if stream.lastSeenAt < cutoff {
						delete(s.stripes[i][tenant][partition], streamHash)
						evicted[tenant]++
					}
				}
			}
		}
	})
	return evicted
}

// EvictPartitions evicts all streams for the specified partitions.
func (s *usageStore) EvictPartitions(partitionsToEvict []int32) {
	s.forEachLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for _, partitionToEvict := range partitionsToEvict {
				delete(partitions, partitionToEvict)
			}
			if len(partitions) == 0 {
				delete(s.stripes[i], tenant)
			}
		}
	})
}

func (s *usageStore) storeStream(i int, tenant string, partition int32, streamHash, recTotalSize uint64, recordTime time.Time, bucketStart, bucketCutOff int64) {
	// Check if the stream already exists in the metadata
	recorded, ok := s.stripes[i][tenant][partition][streamHash]

	// Create new stream metadata with the initial interval
	if !ok {
		s.stripes[i][tenant][partition][streamHash] = streamUsage{
			hash:        streamHash,
			lastSeenAt:  recordTime.UnixNano(),
			totalSize:   recTotalSize,
			rateBuckets: []rateBucket{{timestamp: bucketStart, size: recTotalSize}},
		}
		return
	}

	// Update total size
	totalSize := recTotalSize + recorded.totalSize

	// Update or add size for the current bucket
	updated := false
	sb := make([]rateBucket, 0, len(recorded.rateBuckets)+1)

	// Only keep buckets within the rate window and update the current bucket
	for _, bucket := range recorded.rateBuckets {
		// Clean up buckets outside the rate window
		if bucket.timestamp < bucketCutOff {
			continue
		}

		if bucket.timestamp == bucketStart {
			// Update existing bucket
			sb = append(sb, rateBucket{
				timestamp: bucketStart,
				size:      bucket.size + recTotalSize,
			})
			updated = true
		} else {
			// Keep other buckets within the rate window as is
			sb = append(sb, bucket)
		}
	}

	// Add new bucket if it wasn't updated
	if !updated {
		sb = append(sb, rateBucket{
			timestamp: bucketStart,
			size:      recTotalSize,
		})
	}

	recorded.totalSize = totalSize
	recorded.rateBuckets = sb
	s.stripes[i][tenant][partition][streamHash] = recorded
}

// forEachRLock executes fn with a shared lock for each stripe.
func (s *usageStore) forEachRLock(fn func(i int)) {
	for i := range s.stripes {
		s.locks[i].RLock()
		fn(i)
		s.locks[i].RUnlock()
	}
}

// forEachLock executes fn with an exclusive lock for each stripe.
func (s *usageStore) forEachLock(fn func(i int)) {
	for i := range s.stripes {
		s.locks[i].Lock()
		fn(i)
		s.locks[i].Unlock()
	}
}

// withRLock executes fn with a shared lock on the stripe.
func (s *usageStore) withRLock(tenant string, fn func(i int)) {
	i := s.getStripe(tenant)
	s.locks[i].RLock()
	defer s.locks[i].RUnlock()
	fn(i)
}

// withLock executes fn with an exclusive lock on the stripe.
func (s *usageStore) withLock(tenant string, fn func(i int)) {
	i := s.getStripe(tenant)
	s.locks[i].Lock()
	defer s.locks[i].Unlock()
	fn(i)
}

// getStripe returns the stripe index for the tenant.
func (s *usageStore) getStripe(tenant string) int {
	h := fnv.New32()
	_, _ = h.Write([]byte(tenant))
	return int(h.Sum32() % uint32(len(s.locks)))
}

// getPartitionForHash returns the partition for the hash.
func (s *usageStore) getPartitionForHash(hash uint64) int32 {
	return int32(hash % uint64(s.numPartitions))
}

// Used in tests.
func (s *usageStore) set(tenant string, stream streamUsage) {
	partition := s.getPartitionForHash(stream.hash)
	s.withLock(tenant, func(i int) {
		if _, ok := s.stripes[i][tenant]; !ok {
			s.stripes[i][tenant] = make(tenantUsage)
		}
		if _, ok := s.stripes[i][tenant][partition]; !ok {
			s.stripes[i][tenant][partition] = make(map[uint64]streamUsage)
		}
		s.stripes[i][tenant][partition][stream.hash] = stream
	})

}

// streamLimitExceeded returns a condFunc that checks if the number of active
// streams exceeds the given limit. If it does, the stream is added to the
// results map.
func streamLimitExceeded(limit uint64) condFunc {
	return func(acc float64, _ *proto.StreamMetadata) bool {
		return acc <= float64(limit)
	}
}
