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

// IterateFunc is a closure called for each stream.
type IterateFunc func(tenant string, partition int32, stream Stream)

// CondFunc is a function that is called for each stream passed to StoreCond,
// and is often used to check if a stream can be stored (for example, with
// the max series limit). It should return true if the stream can be stored.
type CondFunc func(acc float64, stream *proto.StreamMetadata) bool

// UsageStore stores per-tenant stream usage data.
type UsageStore struct {
	cfg     Config
	stripes []map[string]tenantUsage
	locks   []stripeLock

	// Used for tests.
	clock quartz.Clock
}

// tenantUsage contains the per-partition stream usage for a tenant.
type tenantUsage map[int32]map[uint64]Stream

// Stream represents the metadata for a stream loaded from the kafka topic.
// It contains the minimal information to count per tenant active streams and
// rate limits.
type Stream struct {
	Hash        uint64
	LastSeenAt  int64
	TotalSize   uint64
	RateBuckets []RateBucket
}

// RateBucket represents the bytes received during a specific time interval
// It is used to calculate the rate limit for a stream.
type RateBucket struct {
	Timestamp int64  // start of the interval
	Size      uint64 // bytes received during this interval
}

type stripeLock struct {
	sync.RWMutex
	// Padding to avoid multiple locks being on the same cache line.
	_ [40]byte
}

// NewUsageStore returns a new UsageStore.
func NewUsageStore(cfg Config) *UsageStore {
	s := &UsageStore{
		cfg:     cfg,
		stripes: make([]map[string]tenantUsage, numStripes),
		locks:   make([]stripeLock, numStripes),
		clock:   quartz.NewReal(),
	}
	for i := range s.stripes {
		s.stripes[i] = make(map[string]tenantUsage)
	}
	return s
}

// All iterates all streams, and calls the [IterateFunc] closure for each
// iterated stream. As [All] acquires a read lock, the closure must not
// make blocking calls while iterating streams.
func (s *UsageStore) All(fn IterateFunc) {
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

// ForTenant iterates all streams for the tenant, and calls the [IterateFunc]
// closure for each iterated stream. As [ForTenant] aquires a read lock, the
// closure must not make blocking calls while iterating streams.
func (s *UsageStore) ForTenant(tenant string, fn IterateFunc) {
	s.withRLock(tenant, func(i int) {
		for partition, streams := range s.stripes[i][tenant] {
			for _, stream := range streams {
				fn(tenant, partition, stream)
			}
		}
	})
}

func (s *UsageStore) Update(tenant string, streams []*proto.StreamMetadata, lastSeenAt time.Time, cond CondFunc) ([]*proto.StreamMetadata, []*proto.StreamMetadata) {
	var (
		// Calculate the cutoff for the window size
		cutoff = lastSeenAt.Add(-s.cfg.WindowSize).UnixNano()
		// Get the bucket for this timestamp using the configured interval duration
		bucketStart = lastSeenAt.Truncate(s.cfg.BucketDuration).UnixNano()
		// Calculate the rate window cutoff for cleaning up old buckets
		bucketCutoff = lastSeenAt.Add(-s.cfg.RateWindow).UnixNano()
		stored       = make([]*proto.StreamMetadata, 0, len(streams))
		rejected     = make([]*proto.StreamMetadata, 0, len(streams))
	)
	s.withLock(tenant, func(i int) {
		if _, ok := s.stripes[i][tenant]; !ok {
			s.stripes[i][tenant] = make(tenantUsage)
		}

		activeStreams := make(map[int32]int)

		for _, stream := range streams {
			partition := int32(stream.StreamHash % uint64(s.cfg.NumPartitions))

			if _, ok := s.stripes[i][tenant][partition]; !ok {
				s.stripes[i][tenant][partition] = make(map[uint64]Stream)
			}

			// Count as active streams all streams that are not expired.
			if _, ok := activeStreams[partition]; !ok {
				for _, stored := range s.stripes[i][tenant][partition] {
					if stored.LastSeenAt >= cutoff {
						activeStreams[partition]++
					}
				}
			}

			recorded, found := s.stripes[i][tenant][partition][stream.StreamHash]

			// If the stream is new or expired, check if it exceeds the limit.
			// If limit is not exceeded and the stream is expired, reset the stream.
			if !found || (recorded.LastSeenAt < cutoff) {
				activeStreams[partition]++

				if cond != nil && !cond(float64(activeStreams[partition]), stream) {
					rejected = append(rejected, stream)
					continue
				}

				// If the stream is stored and expired, reset the stream
				if found && recorded.LastSeenAt < cutoff {
					s.stripes[i][tenant][partition][stream.StreamHash] = Stream{Hash: stream.StreamHash, LastSeenAt: lastSeenAt.UnixNano()}
				}
			}

			s.storeStream(i, tenant, partition, stream.StreamHash, stream.TotalSize, lastSeenAt, bucketStart, bucketCutoff)

			stored = append(stored, stream)
		}
	})

	return stored, rejected
}

// Evict evicts all streams that have not been seen within the window.
func (s *UsageStore) Evict() map[string]int {
	cutoff := s.clock.Now().Add(-s.cfg.WindowSize).UnixNano()
	evicted := make(map[string]int)
	s.forEachLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for partition, streams := range partitions {
				for streamHash, stream := range streams {
					if stream.LastSeenAt < cutoff {
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
func (s *UsageStore) EvictPartitions(partitionsToEvict []int32) {
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

func (s *UsageStore) storeStream(i int, tenant string, partition int32, streamHash, recTotalSize uint64, recordTime time.Time, bucketStart, bucketCutOff int64) {
	// Check if the stream already exists in the metadata
	recorded, ok := s.stripes[i][tenant][partition][streamHash]

	// Create new stream metadata with the initial interval
	if !ok {
		s.stripes[i][tenant][partition][streamHash] = Stream{
			Hash:        streamHash,
			LastSeenAt:  recordTime.UnixNano(),
			TotalSize:   recTotalSize,
			RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: recTotalSize}},
		}
		return
	}

	// Update total size
	totalSize := recTotalSize + recorded.TotalSize

	// Update or add size for the current bucket
	updated := false
	sb := make([]RateBucket, 0, len(recorded.RateBuckets)+1)

	// Only keep buckets within the rate window and update the current bucket
	for _, bucket := range recorded.RateBuckets {
		// Clean up buckets outside the rate window
		if bucket.Timestamp < bucketCutOff {
			continue
		}

		if bucket.Timestamp == bucketStart {
			// Update existing bucket
			sb = append(sb, RateBucket{
				Timestamp: bucketStart,
				Size:      bucket.Size + recTotalSize,
			})
			updated = true
		} else {
			// Keep other buckets within the rate window as is
			sb = append(sb, bucket)
		}
	}

	// Add new bucket if it wasn't updated
	if !updated {
		sb = append(sb, RateBucket{
			Timestamp: bucketStart,
			Size:      recTotalSize,
		})
	}

	recorded.TotalSize = totalSize
	recorded.RateBuckets = sb
	s.stripes[i][tenant][partition][streamHash] = recorded
}

// forEachRLock executes fn with a shared lock for each stripe.
func (s *UsageStore) forEachRLock(fn func(i int)) {
	for i := range s.stripes {
		s.locks[i].RLock()
		fn(i)
		s.locks[i].RUnlock()
	}
}

// forEachLock executes fn with an exclusive lock for each stripe.
func (s *UsageStore) forEachLock(fn func(i int)) {
	for i := range s.stripes {
		s.locks[i].Lock()
		fn(i)
		s.locks[i].Unlock()
	}
}

// withRLock executes fn with a shared lock on the stripe.
func (s *UsageStore) withRLock(tenant string, fn func(i int)) {
	i := s.getStripe(tenant)
	s.locks[i].RLock()
	defer s.locks[i].RUnlock()
	fn(i)
}

// withLock executes fn with an exclusive lock on the stripe.
func (s *UsageStore) withLock(tenant string, fn func(i int)) {
	i := s.getStripe(tenant)
	s.locks[i].Lock()
	defer s.locks[i].Unlock()
	fn(i)
}

// getStripe returns the stripe index for the tenant.
func (s *UsageStore) getStripe(tenant string) int {
	h := fnv.New32()
	_, _ = h.Write([]byte(tenant))
	return int(h.Sum32() % uint32(len(s.locks)))
}

// Used in tests.
func (s *UsageStore) set(tenant string, stream Stream) {
	partition := int32(stream.Hash % uint64(s.cfg.NumPartitions))
	s.withLock(tenant, func(i int) {
		if _, ok := s.stripes[i][tenant]; !ok {
			s.stripes[i][tenant] = make(tenantUsage)
		}
		if _, ok := s.stripes[i][tenant][partition]; !ok {
			s.stripes[i][tenant][partition] = make(map[uint64]Stream)
		}
		s.stripes[i][tenant][partition][stream.Hash] = stream
	})

}

// streamLimitExceeded returns a CondFunc that checks if the number of active streams
// exceeds the given limit. If it does, the stream is added to the results map.
func streamLimitExceeded(limit uint64) CondFunc {
	return func(acc float64, _ *proto.StreamMetadata) bool {
		return acc <= float64(limit)
	}
}
