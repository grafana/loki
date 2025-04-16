package limits

import (
	"sync"
)

type StreamMetadata interface {
	All(fn func(stream streamMetadata, tenant string, partitionID int32))
	Collect(tenant string, fn func(stream streamMetadata, partitionID int32))
	Upsert(tenant string, partitionID int32, streamHash uint64, recordTime int64, recTotalSize uint64, bucketStart int64, bucketCutOff int64)
	Evict(cutoff int64) map[string]int
	EvictPartitions(partitions []int32)
}

type streamMetadata struct {
	hash       uint64
	lastSeenAt int64
	totalSize  uint64
	// Add a slice to track bytes per time interval for sliding window rate calculation
	rateBuckets []rateBucket
}

// rateBucket represents the bytes received during a specific time interval
type rateBucket struct {
	timestamp int64  // start of the interval
	size      uint64 // bytes received during this interval
}

type stripeLock struct {
	sync.RWMutex
	// Padding to avoid multiple locks being on the same cache line.
	_ [40]byte
}

type streamMetadataStripes struct {
	stripes []map[string]map[int32][]streamMetadata // stripe -> tenant -> partitionID -> streamMetadata
	locks   []stripeLock
}

func NewStreamMetadata(size int) StreamMetadata {
	s := &streamMetadataStripes{
		stripes: make([]map[string]map[int32][]streamMetadata, size),
		locks:   make([]stripeLock, size),
	}

	for i := range s.stripes {
		s.stripes[i] = make(map[string]map[int32][]streamMetadata)
	}

	return s
}

func (s *streamMetadataStripes) All(fn func(stream streamMetadata, tenant string, partitionID int32)) {
	for i := range s.stripes {
		s.locks[i].RLock()

		for tenant, partitions := range s.stripes[i] {
			for partitionID, partition := range partitions {
				for _, stream := range partition {
					fn(stream, tenant, partitionID)
				}
			}
		}

		s.locks[i].RUnlock()
	}
}
func (s *streamMetadataStripes) Collect(tenant string, fn func(stream streamMetadata, partitionID int32)) {
	for i := range s.stripes {
		s.locks[i].RLock()

		for partitionID, partition := range s.stripes[i][tenant] {
			for _, stream := range partition {
				fn(stream, partitionID)
			}
		}

		s.locks[i].RUnlock()
	}
}
func (s *streamMetadataStripes) Upsert(tenant string, partitionID int32, streamHash uint64, recordTime int64, recTotalSize uint64, bucketStart int64, bucketCutOff int64) {
	i := uint64(partitionID) & uint64(len(s.locks)-1)

	s.locks[i].Lock()
	defer s.locks[i].Unlock()

	// Initialize stripe map if it doesn't exist
	if s.stripes[i] == nil {
		s.stripes[i] = make(map[string]map[int32][]streamMetadata)
	}

	// Initialize tenant map if it doesn't exist
	if _, ok := s.stripes[i][tenant]; !ok {
		s.stripes[i][tenant] = make(map[int32][]streamMetadata)
	}

	// Initialize partition map if it doesn't exist
	if s.stripes[i][tenant][partitionID] == nil {
		s.stripes[i][tenant][partitionID] = make([]streamMetadata, 0)
	}

	for j, stream := range s.stripes[i][tenant][partitionID] {
		if stream.hash == streamHash {
			// Update total size
			totalSize := stream.totalSize + recTotalSize

			// Update or add size for the current bucket
			updated := false
			sb := make([]rateBucket, 0, len(stream.rateBuckets)+1)

			// Only keep buckets within the rate window and update the current bucket
			for _, bucket := range stream.rateBuckets {
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

			s.stripes[i][tenant][partitionID][j] = streamMetadata{
				hash:        stream.hash,
				lastSeenAt:  recordTime,
				totalSize:   totalSize,
				rateBuckets: sb,
			}
			return
		}
	}

	// Create new stream metadata with the initial interval
	s.stripes[i][tenant][partitionID] = append(s.stripes[i][tenant][partitionID], streamMetadata{
		hash:        streamHash,
		lastSeenAt:  recordTime,
		totalSize:   recTotalSize,
		rateBuckets: []rateBucket{{timestamp: bucketStart, size: recTotalSize}},
	})
}

func (s *streamMetadataStripes) Evict(cutoff int64) map[string]int {
	evicted := make(map[string]int)

	for i := range s.locks {
		s.locks[i].Lock()

		for tenant, streams := range s.stripes[i] {
			for partitionID, partition := range streams {
				activeStreams := make([]streamMetadata, 0)

				for _, stream := range partition {
					if stream.lastSeenAt >= cutoff {
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

func (s *streamMetadataStripes) EvictPartitions(partitions []int32) {
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
