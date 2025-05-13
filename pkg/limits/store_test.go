package limits

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestUsageStore_All(t *testing.T) {
	now := time.Now()
	cutoff := now.Add(-5 * time.Minute).UnixNano()
	// Create a store with 10 partitions.
	s := NewUsageStore(10)
	// Create 10 streams. Since we use i as the hash, we can expect the
	// streams to be sharded over all 10 partitions.
	streams := make([]*proto.StreamMetadata, 10)
	for i := 0; i < 10; i++ {
		streams[i] = &proto.StreamMetadata{
			StreamHash: uint64(i),
		}
	}
	// Add the streams to the store, all streams should be accepted.
	accepted, rejected := s.Update("tenant", streams, now.UnixNano(), cutoff, 0, 0, nil)
	require.Len(t, accepted, 10)
	require.Empty(t, rejected)
	// Check that we can iterate all stored streams.
	expected := []uint64{0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9}
	actual := make([]uint64, 0, len(expected))
	s.All(func(_ string, _ int32, s Stream) {
		actual = append(actual, s.Hash)
	})
	require.ElementsMatch(t, expected, actual)
}

func TestUsageStore_ForTenant(t *testing.T) {
	now := time.Now()
	cutoff := now.Add(-5 * time.Minute).UnixNano()
	// Create a store with 10 partitions.
	s := NewUsageStore(10)
	// Create 10 streams. Since we use i as the hash, we can expect the
	// streams to be sharded over all 10 partitions.
	streams := make([]*proto.StreamMetadata, 10)
	for i := 0; i < 10; i++ {
		streams[i] = &proto.StreamMetadata{
			StreamHash: uint64(i),
		}
	}
	// Add the streams to the store, but with the streams shared between
	// two tenants.
	accepted, rejected := s.Update("tenant1", streams[0:5], now.UnixNano(), cutoff, 0, 0, nil)
	require.Len(t, accepted, 5)
	require.Empty(t, rejected)
	accepted, rejected = s.Update("tenant2", streams[5:], now.UnixNano(), cutoff, 0, 0, nil)
	require.Len(t, accepted, 5)
	require.Empty(t, rejected)
	// Check we can iterate just the streams for each tenant.
	expected1 := []uint64{0x0, 0x1, 0x2, 0x3, 0x4}
	actual1 := make([]uint64, 0, 5)
	s.ForTenant("tenant1", func(_ string, _ int32, stream Stream) {
		actual1 = append(actual1, stream.Hash)
	})
	require.ElementsMatch(t, expected1, actual1)
	expected2 := []uint64{0x5, 0x6, 0x7, 0x8, 0x9}
	actual2 := make([]uint64, 0, 5)
	s.ForTenant("tenant2", func(_ string, _ int32, stream Stream) {
		actual2 = append(actual2, stream.Hash)
	})
	require.ElementsMatch(t, expected2, actual2)
}

func TestUsageStore_Store(t *testing.T) {
	now := time.Now()
	cutoff := now.Add(-5 * time.Minute).UnixNano()
	bucketStart := now.Truncate(time.Minute).UnixNano()
	bucketCutoff := now.Add(-5 * time.Minute).UnixNano()

	tests := []struct {
		name             string
		numPartitions    int
		maxGlobalStreams uint64
		// seed contains the (optional) streams that should be seeded before
		// the test.
		seed             []*proto.StreamMetadata
		streams          []*proto.StreamMetadata
		expectedAccepted []*proto.StreamMetadata
		expectedRejected []*proto.StreamMetadata
	}{{
		name:             "no streams",
		numPartitions:    1,
		maxGlobalStreams: 1,
	}, {
		name:             "all streams within stream limit",
		numPartitions:    1,
		maxGlobalStreams: 2,
		streams: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000},
			{StreamHash: 0x1, TotalSize: 1000},
		},
		expectedAccepted: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000},
			{StreamHash: 0x1, TotalSize: 1000},
		},
	}, {
		name:             "some streams rejected",
		numPartitions:    1,
		maxGlobalStreams: 1,
		streams: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000},
			{StreamHash: 0x1, TotalSize: 1000},
		},
		expectedAccepted: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000},
		},
		expectedRejected: []*proto.StreamMetadata{
			{StreamHash: 0x1, TotalSize: 1000},
		},
	}, {
		name:             "one stream rejected in first partition",
		numPartitions:    2,
		maxGlobalStreams: 1,
		streams: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000}, // partition 0
			{StreamHash: 0x1, TotalSize: 1000}, // partition 1
			{StreamHash: 0x3, TotalSize: 1000}, // partition 1
			{StreamHash: 0x5, TotalSize: 1000}, // partition 1
		},
		expectedAccepted: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000},
			{StreamHash: 0x1, TotalSize: 1000},
		},
		expectedRejected: []*proto.StreamMetadata{
			{StreamHash: 0x3, TotalSize: 1000},
			{StreamHash: 0x5, TotalSize: 1000},
		},
	}, {
		name:             "one stream rejected in all partitions",
		numPartitions:    2,
		maxGlobalStreams: 1,
		streams: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000}, // partition 0
			{StreamHash: 0x1, TotalSize: 1000}, // partition 1
			{StreamHash: 0x2, TotalSize: 1000}, // partition 0
			{StreamHash: 0x3, TotalSize: 1000}, // partition 1
		},
		expectedAccepted: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000},
			{StreamHash: 0x1, TotalSize: 1000},
		},
		expectedRejected: []*proto.StreamMetadata{
			{StreamHash: 0x2, TotalSize: 1000},
			{StreamHash: 0x3, TotalSize: 1000},
		},
	}, {
		name:             "drops new streams but updates existing streams",
		numPartitions:    2,
		maxGlobalStreams: 2,
		seed: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000},
			{StreamHash: 0x2, TotalSize: 1000},
		},
		streams: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000}, // existing, partition 0
			{StreamHash: 0x1, TotalSize: 1000}, // new, partition 1
			{StreamHash: 0x2, TotalSize: 1000}, // existing, partition 0
			{StreamHash: 0x4, TotalSize: 1000}, // new, partition 0
		},
		expectedAccepted: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000},
			{StreamHash: 0x1, TotalSize: 1000},
			{StreamHash: 0x2, TotalSize: 1000},
		},
		expectedRejected: []*proto.StreamMetadata{
			{StreamHash: 0x4, TotalSize: 1000},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := NewUsageStore(test.numPartitions)
			s.Update("tenant", test.seed, now.UnixNano(), cutoff, bucketStart, bucketCutoff, nil)
			streamLimitCond := streamLimitExceeded(test.maxGlobalStreams)
			accepted, rejected := s.Update("tenant", test.streams, now.UnixNano(), cutoff, bucketStart, bucketCutoff, streamLimitCond)
			require.ElementsMatch(t, test.expectedAccepted, accepted)
			require.ElementsMatch(t, test.expectedRejected, rejected)
		})
	}
}

func TestStreamMetadata_Evict(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name                 string
		usage                *UsageStore
		cutOff               int64
		assignedPartitionIDs []int32
		expectedEvictions    map[string]int
		expectedUsage        map[string]map[int32]map[uint64]Stream
	}{{
		name: "all streams active",
		usage: &UsageStore{
			stripes: []map[string]tenantUsage{{
				"tenant1": {
					0: {
						1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
						2: {Hash: 2, LastSeenAt: now.UnixNano(), TotalSize: 2000},
					},
				},
			}},
			locks: make([]stripeLock, 1),
		},
		cutOff:               now.Add(-time.Hour).UnixNano(),
		assignedPartitionIDs: []int32{0},
		expectedEvictions:    map[string]int{},
		expectedUsage: map[string]map[int32]map[uint64]Stream{
			"tenant1": {
				0: {
					1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
					2: {Hash: 2, LastSeenAt: now.UnixNano(), TotalSize: 2000},
				},
			},
		},
	}, {
		name: "all streams expired",
		usage: &UsageStore{
			stripes: []map[string]tenantUsage{{
				"tenant1": {
					0: {
						1: {Hash: 1, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 1000},
						2: {Hash: 2, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 2000},
					},
				},
			}},
			locks: make([]stripeLock, 1),
		},
		cutOff:               now.Add(-time.Hour).UnixNano(),
		assignedPartitionIDs: []int32{0},
		expectedEvictions: map[string]int{
			"tenant1": 2,
		},
		expectedUsage: map[string]map[int32]map[uint64]Stream{},
	}, {
		name: "mixed active and expired streams",
		usage: &UsageStore{
			stripes: []map[string]tenantUsage{{
				"tenant1": {
					0: {
						1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
						2: {Hash: 2, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 2000},
						3: {Hash: 3, LastSeenAt: now.UnixNano(), TotalSize: 3000},
					},
				},
			}},
			locks: make([]stripeLock, 1),
		},
		cutOff:               now.Add(-time.Hour).UnixNano(),
		assignedPartitionIDs: []int32{0},
		expectedEvictions: map[string]int{
			"tenant1": 1,
		},
		expectedUsage: map[string]map[int32]map[uint64]Stream{
			"tenant1": {
				0: {
					1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
					3: {Hash: 3, LastSeenAt: now.UnixNano(), TotalSize: 3000},
				},
			},
		},
	}, {
		name: "multiple tenants with mixed streams",
		usage: &UsageStore{
			stripes: []map[string]tenantUsage{{
				"tenant1": {
					0: {
						1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
						2: {Hash: 2, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 2000},
					},
				},
				"tenant2": {
					0: {
						3: {Hash: 3, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 3000},
						4: {Hash: 4, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 4000},
					},
				},
				"tenant3": {0: {5: {Hash: 5, LastSeenAt: now.UnixNano(), TotalSize: 5000}}},
			}},
			locks: make([]stripeLock, 1),
		},
		cutOff:               now.Add(-time.Hour).UnixNano(),
		assignedPartitionIDs: []int32{0},
		expectedEvictions: map[string]int{
			"tenant1": 1,
			"tenant2": 2,
		},
		expectedUsage: map[string]map[int32]map[uint64]Stream{
			"tenant1": {0: {1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000}}},
			"tenant3": {0: {5: {Hash: 5, LastSeenAt: now.UnixNano(), TotalSize: 5000}}},
		},
	}, {
		name: "multiple partitions with some empty after eviction",
		usage: &UsageStore{
			stripes: []map[string]tenantUsage{{
				"tenant1": {
					0: {
						1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
						2: {Hash: 2, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 2000},
					},
				},
			}, {
				"tenant1": {1: {3: {Hash: 3, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 3000}}},
			}, {
				"tenant1": {2: {4: {Hash: 4, LastSeenAt: now.UnixNano(), TotalSize: 4000}}},
			}},
			locks: make([]stripeLock, 3),
		},
		cutOff:               now.Add(-time.Hour).UnixNano(),
		assignedPartitionIDs: []int32{0, 1, 2},
		expectedEvictions: map[string]int{
			"tenant1": 2,
		},
		expectedUsage: map[string]map[int32]map[uint64]Stream{
			"tenant1": {
				0: {1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000}},
				2: {4: {Hash: 4, LastSeenAt: now.UnixNano(), TotalSize: 4000}},
			},
		},
	}, {
		name: "unassigned partitions should still be evicted",
		usage: &UsageStore{
			stripes: []map[string]tenantUsage{{
				"tenant1": {0: {1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000}}},
			}, {
				"tenant1": {1: {2: {Hash: 2, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 2000}}},
			}},
			locks: make([]stripeLock, 2),
		},
		cutOff:               now.Add(-time.Hour).UnixNano(),
		assignedPartitionIDs: []int32{0},
		expectedEvictions: map[string]int{
			"tenant1": 1,
		},
		expectedUsage: map[string]map[int32]map[uint64]Stream{
			"tenant1": {0: {1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000}}},
		},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualEvictions := tt.usage.Evict(tt.cutOff)
			actualUsage := make(map[string]map[int32]map[uint64]Stream)
			tt.usage.All(func(tenant string, partitionID int32, stream Stream) {
				if actualUsage[tenant] == nil {
					actualUsage[tenant] = make(map[int32]map[uint64]Stream)
				}
				if actualUsage[tenant][partitionID] == nil {
					actualUsage[tenant][partitionID] = make(map[uint64]Stream)
				}
				actualUsage[tenant][partitionID][stream.Hash] = stream
			})
			require.Equal(t, tt.expectedEvictions, actualEvictions)
			require.Equal(t, tt.expectedUsage, actualUsage)
		})
	}
}

func TestUsageStore_EvictPartitions(t *testing.T) {
	// Create a store with 10 partitions.
	s := NewUsageStore(10)
	// Create 10 streams. Since we use i as the hash, we can expect the
	// streams to be sharded over all 10 partitions.
	streams := make([]*proto.StreamMetadata, 10)
	for i := 0; i < 10; i++ {
		streams[i] = &proto.StreamMetadata{
			StreamHash: uint64(i),
		}
	}
	now := time.Now()
	s.Update("tenant", streams, now.UnixNano(), now.Add(-time.Minute).UnixNano(), 0, 0, nil)
	// Evict the first 5 partitions.
	s.EvictPartitions([]int32{0, 1, 2, 3, 4})
	// The last 5 partitions should still have data.
	expected := []int32{5, 6, 7, 8, 9}
	actual := make([]int32, 0, len(expected))
	s.All(func(_ string, partition int32, _ Stream) {
		actual = append(actual, partition)
	})
	require.ElementsMatch(t, expected, actual)
}
