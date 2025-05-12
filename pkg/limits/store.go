package limits

import (
	"hash/fnv"
	"sync"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// AllFunc is a closure called for each stream.
type AllFunc func(tenant string, partitionID int32, stream Stream)

// UsageFunc is a closure called for
type UsageFunc func(partitionID int32, stream Stream)

// CondFunc is a function that is called for each stream in the metadata.
// It is used to check if the stream should be stored.
// It returns true if the stream should be stored, false otherwise.
type CondFunc func(acc float64, stream *proto.StreamMetadata) bool

// UsageStore is an interface for stores which track stream usage.
type UsageStore interface {
	// All iterates over all streams and calls the closure for each stream.
	// Iteration is unordered.
	All(fn AllFunc)

	// Usage iterates over all streams for the tenant, and calls the closure
	// for each stream. It is used to get current usage (i.e. the total number
	// of active streams and the total size of all active streams).
	Usage(tenant string, fn UsageFunc)

	// StoreCond tries to store the streams for the tenant.
	// until the partition limit is reached. It returns the total ingested bytes.
	StoreCond(tenant string, streams []*proto.StreamMetadata, lastSeenAt, cutoff, bucketStart, bucketCutOff int64, cond CondFunc) ([]*proto.StreamMetadata, []*proto.StreamMetadata)

	// Store updates the stream metadata for the tenant.
	Store(tenant string, partitionID int32, streamHash, recTotalSize uint64, recordTime, bucketStart, bucketCutOff int64)

	// Evict removes all streams that have not been seen in cutoff seconds.
	Evict(cutoff int64) map[string]int

	// EvictPartitions removes the partitions for all tenants.
	EvictPartitions(partitions []int32)
}

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

// MemUsageStore implements StreaMetadataStore.
type MemUsageStore struct {
	numPartitions int
	stripes       []map[string]map[int32]map[uint64]Stream // stripe -> tenant -> partitionID -> streamMetadata
	locks         []stripeLock
}

func NewMemUsageStore(numPartitions int) *MemUsageStore {
	s := &MemUsageStore{
		numPartitions: numPartitions,
		stripes:       make([]map[string]map[int32]map[uint64]Stream, numPartitions),
		locks:         make([]stripeLock, numPartitions),
	}
	for i := range s.stripes {
		s.stripes[i] = make(map[string]map[int32]map[uint64]Stream)
	}
	return s
}

func (s *MemUsageStore) All(fn AllFunc) {
	s.forEachRLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for partitionID, partition := range partitions {
				for _, stream := range partition {
					fn(tenant, partitionID, stream)
				}
			}
		}
	})
}

func (s *MemUsageStore) Usage(tenant string, fn UsageFunc) {
	s.withRLock(tenant, func(i int) {
		for partitionID, partition := range s.stripes[i][tenant] {
			for _, stream := range partition {
				fn(partitionID, stream)
			}
		}
	})
}

func (s *MemUsageStore) StoreCond(tenant string, streams []*proto.StreamMetadata, lastSeenAt, cutoff, bucketStart, bucketCutOff int64, cond CondFunc) ([]*proto.StreamMetadata, []*proto.StreamMetadata) {
	stored := make([]*proto.StreamMetadata, 0, len(streams))
	rejected := make([]*proto.StreamMetadata, 0, len(streams))

	s.withLock(tenant, func(i int) {
		if _, ok := s.stripes[i][tenant]; !ok {
			s.stripes[i][tenant] = make(map[int32]map[uint64]Stream)
		}

		activeStreams := make(map[int32]int)

		for _, stream := range streams {
			partitionID := int32(stream.StreamHash % uint64(s.numPartitions))

			if _, ok := s.stripes[i][tenant][partitionID]; !ok {
				s.stripes[i][tenant][partitionID] = make(map[uint64]Stream)
			}

			// Count as active streams all streams that are not expired.
			if _, ok := activeStreams[partitionID]; !ok {
				for _, stored := range s.stripes[i][tenant][partitionID] {
					if stored.LastSeenAt >= cutoff {
						activeStreams[partitionID]++
					}
				}
			}

			recorded, found := s.stripes[i][tenant][partitionID][stream.StreamHash]

			// If the stream is new or expired, check if it exceeds the limit.
			// If limit is not exceeded and the stream is expired, reset the stream.
			if !found || (recorded.LastSeenAt < cutoff) {
				activeStreams[partitionID]++

				if !cond(float64(activeStreams[partitionID]), stream) {
					rejected = append(rejected, stream)
					continue
				}

				// If the stream is stored and expired, reset the stream
				if found && recorded.LastSeenAt < cutoff {
					s.stripes[i][tenant][partitionID][stream.StreamHash] = Stream{Hash: stream.StreamHash, LastSeenAt: lastSeenAt}
				}
			}

			s.storeStream(i, tenant, partitionID, stream.StreamHash, stream.TotalSize, lastSeenAt, bucketStart, bucketCutOff)

			stored = append(stored, stream)
		}
	})

	return stored, rejected
}

func (s *MemUsageStore) Store(tenant string, partitionID int32, streamHash, recTotalSize uint64, recordTime, bucketStart, bucketCutOff int64) {
	s.withLock(tenant, func(i int) {
		// Initialize tenant map if it doesn't exist
		if _, ok := s.stripes[i][tenant]; !ok {
			s.stripes[i][tenant] = make(map[int32]map[uint64]Stream)
		}

		// Initialize partition map if it doesn't exist
		if s.stripes[i][tenant][partitionID] == nil {
			s.stripes[i][tenant][partitionID] = make(map[uint64]Stream)
		}

		s.storeStream(i, tenant, partitionID, streamHash, recTotalSize, recordTime, bucketStart, bucketCutOff)
	})
}

func (s *MemUsageStore) storeStream(i int, tenant string, partitionID int32, streamHash, recTotalSize uint64, recordTime, bucketStart, bucketCutOff int64) {
	// Check if the stream already exists in the metadata
	recorded, ok := s.stripes[i][tenant][partitionID][streamHash]

	// Create new stream metadata with the initial interval
	if !ok {
		s.stripes[i][tenant][partitionID][streamHash] = Stream{
			Hash:        streamHash,
			LastSeenAt:  recordTime,
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
	s.stripes[i][tenant][partitionID][streamHash] = recorded
}

func (s *MemUsageStore) Evict(cutoff int64) map[string]int {
	evicted := make(map[string]int)
	s.forEachLock(func(i int) {
		for tenant, streams := range s.stripes[i] {
			for partitionID, partition := range streams {
				for streamHash, stream := range partition {
					if stream.LastSeenAt < cutoff {
						delete(s.stripes[i][tenant][partitionID], streamHash)
						evicted[tenant]++
					}
				}
			}
		}
	})
	return evicted
}

func (s *MemUsageStore) EvictPartitions(partitions []int32) {
	s.forEachLock(func(i int) {
		for tenant, tenantPartitions := range s.stripes[i] {
			for _, deleteID := range partitions {
				delete(tenantPartitions, deleteID)
			}
			if len(tenantPartitions) == 0 {
				delete(s.stripes[i], tenant)
			}
		}
	})
}

// forEachRLock executes fn with a shared lock for each stripe.
func (s *MemUsageStore) forEachRLock(fn func(i int)) {
	for i := range s.stripes {
		s.locks[i].RLock()
		fn(i)
		s.locks[i].RUnlock()
	}
}

// forEachLock executes fn with an exclusive lock for each stripe.
func (s *MemUsageStore) forEachLock(fn func(i int)) {
	for i := range s.stripes {
		s.locks[i].Lock()
		fn(i)
		s.locks[i].Unlock()
	}
}

// withRLock executes fn with a shared lock on the stripe.
func (s *MemUsageStore) withRLock(tenant string, fn func(i int)) {
	i := s.getStripe(tenant)
	s.locks[i].RLock()
	defer s.locks[i].RUnlock()
	fn(i)
}

// withLock executes fn with an exclusive lock on the stripe.
func (s *MemUsageStore) withLock(tenant string, fn func(i int)) {
	i := s.getStripe(tenant)
	s.locks[i].Lock()
	defer s.locks[i].Unlock()
	fn(i)
}

// getStripe returns the stripe index for the tenant.
func (s *MemUsageStore) getStripe(tenant string) int {
	h := fnv.New32()
	_, _ = h.Write([]byte(tenant))
	return int(h.Sum32() % uint32(len(s.locks)))
}

// streamLimitExceeded returns a CondFunc that checks if the number of active streams
// exceeds the given limit. If it does, the stream is added to the results map.
func streamLimitExceeded(limit uint64) CondFunc {
	return func(acc float64, _ *proto.StreamMetadata) bool {
		return acc <= float64(limit)
	}
}
