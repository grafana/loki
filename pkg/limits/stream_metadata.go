package limits

import (
	"sync"
)

// AllFunc is a function that is called for each stream in the metadata.
// It is used to count per tenant active streams.
// Note: The collect function should not modify the stream metadata.
type AllFunc = func(tenant string, partitionID int32, stream Stream)

// UsageFunc is a function that is called for per tenant streams.
// It is used to read the stream metadata for a specific tenant.
// Note: The collect function should not modify the stream metadata.
type UsageFunc = func(partitionID int32, stream Stream)

// StreamMetadata represents the ingest limits interface for the stream metadata.
type StreamMetadata interface {
	// All iterates over all streams and applies the given function.
	All(fn AllFunc)

	// Usage iterates over all streams for a specific tenant and collects the overall usage,
	// e.g. the total active streams and the total size of the streams.
	Usage(tenant string, fn UsageFunc)

	// Store updates or creates the stream metadata for a specific tenant and partition.
	Store(tenant string, partitionID int32, streamHash, recTotalSize uint64, recordTime, bucketStart, bucketCutOff int64)

	// Evict removes all streams that have not been seen for a specific time.
	Evict(cutoff int64) map[string]int

	// EvictPartitions removes all unassigned partitions from the metadata for every tenant.
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

type streamMetadata struct {
	stripes []map[string]map[int32][]Stream // stripe -> tenant -> partitionID -> streamMetadata
	locks   []stripeLock
}

func NewStreamMetadata(size int) StreamMetadata {
	s := &streamMetadata{
		stripes: make([]map[string]map[int32][]Stream, size),
		locks:   make([]stripeLock, size),
	}

	for i := range s.stripes {
		s.stripes[i] = make(map[string]map[int32][]Stream)
	}

	return s
}

func (s *streamMetadata) All(fn AllFunc) {
	for i := range s.stripes {
		s.locks[i].RLock()

		for tenant, partitions := range s.stripes[i] {
			for partitionID, partition := range partitions {
				for _, stream := range partition {
					fn(tenant, partitionID, stream)
				}
			}
		}

		s.locks[i].RUnlock()
	}
}

func (s *streamMetadata) Usage(tenant string, fn UsageFunc) {
	for i := range s.stripes {
		s.locks[i].RLock()

		for partitionID, partition := range s.stripes[i][tenant] {
			for _, stream := range partition {
				fn(partitionID, stream)
			}
		}

		s.locks[i].RUnlock()
	}
}
func (s *streamMetadata) Store(tenant string, partitionID int32, streamHash, recTotalSize uint64, recordTime, bucketStart, bucketCutOff int64) {
	i := uint64(partitionID) & uint64(len(s.locks)-1)

	s.locks[i].Lock()
	defer s.locks[i].Unlock()

	// Initialize stripe map if it doesn't exist
	if s.stripes[i] == nil {
		s.stripes[i] = make(map[string]map[int32][]Stream)
	}

	// Initialize tenant map if it doesn't exist
	if _, ok := s.stripes[i][tenant]; !ok {
		s.stripes[i][tenant] = make(map[int32][]Stream)
	}

	// Initialize partition map if it doesn't exist
	if s.stripes[i][tenant][partitionID] == nil {
		s.stripes[i][tenant][partitionID] = make([]Stream, 0)
	}

	for j, stream := range s.stripes[i][tenant][partitionID] {
		if stream.Hash == streamHash {
			// Update total size
			totalSize := stream.TotalSize + recTotalSize

			// Update or add size for the current bucket
			updated := false
			sb := make([]RateBucket, 0, len(stream.RateBuckets)+1)

			// Only keep buckets within the rate window and update the current bucket
			for _, bucket := range stream.RateBuckets {
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

			s.stripes[i][tenant][partitionID][j] = Stream{
				Hash:        stream.Hash,
				LastSeenAt:  recordTime,
				TotalSize:   totalSize,
				RateBuckets: sb,
			}
			return
		}
	}

	// Create new stream metadata with the initial interval
	s.stripes[i][tenant][partitionID] = append(s.stripes[i][tenant][partitionID], Stream{
		Hash:        streamHash,
		LastSeenAt:  recordTime,
		TotalSize:   recTotalSize,
		RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: recTotalSize}},
	})
}

func (s *streamMetadata) Evict(cutoff int64) map[string]int {
	evicted := make(map[string]int)

	for i := range s.locks {
		s.locks[i].Lock()

		for tenant, streams := range s.stripes[i] {
			for partitionID, partition := range streams {
				activeStreams := make([]Stream, 0)

				for _, stream := range partition {
					if stream.LastSeenAt >= cutoff {
						activeStreams = append(activeStreams, stream)
					} else {
						evicted[tenant]++
					}
				}

				s.stripes[i][tenant][partitionID] = activeStreams
			}
		}
		s.locks[i].Unlock()
	}

	return evicted
}

func (s *streamMetadata) EvictPartitions(partitions []int32) {
	for i := range s.locks {
		s.locks[i].Lock()

		for _, id := range partitions {
			for tenant, partitions := range s.stripes[i] {
				delete(partitions, id)

				if len(partitions) == 0 {
					delete(s.stripes[i], tenant)
				}
			}
		}

		s.locks[i].Unlock()
	}
}
