package limits

import (
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestUsageStore_All(t *testing.T) {
	// Create a store with 10 partitions.
	s := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 10)
	clock := quartz.NewMock(t)
	s.clock = clock
	// Create 10 streams. Since we use i as the hash, we can expect the
	// streams to be sharded over all 10 partitions.
	for i := 0; i < 10; i++ {
		s.set("tenant", streamUsage{hash: uint64(i)})
	}
	// Check that we can iterate all stored streams.
	expected := []uint64{0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9}
	actual := make([]uint64, 0, len(expected))
	s.all(func(_ string, _ int32, s streamUsage) {
		actual = append(actual, s.hash)
	})
	require.ElementsMatch(t, expected, actual)
}

func TestUsageStore_ForTenant(t *testing.T) {
	// Create a store with 10 partitions.
	s := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 10)
	clock := quartz.NewMock(t)
	s.clock = clock
	// Create 10 streams. Since we use i as the hash, we can expect the
	// streams to be sharded over all 10 partitions.
	for i := 0; i < 10; i++ {
		tenant := "tenant1"
		if i >= 5 {
			tenant = "tenant2"
		}
		s.set(tenant, streamUsage{hash: uint64(i)})
	}
	// Check we can iterate just the streams for each tenant.
	expected1 := []uint64{0x0, 0x1, 0x2, 0x3, 0x4}
	actual1 := make([]uint64, 0, 5)
	s.forTenant("tenant1", func(_ string, _ int32, stream streamUsage) {
		actual1 = append(actual1, stream.hash)
	})
	require.ElementsMatch(t, expected1, actual1)
	expected2 := []uint64{0x5, 0x6, 0x7, 0x8, 0x9}
	actual2 := make([]uint64, 0, 5)
	s.forTenant("tenant2", func(_ string, _ int32, stream streamUsage) {
		actual2 = append(actual2, stream.hash)
	})
	require.ElementsMatch(t, expected2, actual2)
}

func TestUsageStore_Store(t *testing.T) {
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
			s := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, test.numPartitions)
			clock := quartz.NewMock(t)
			s.clock = clock
			s.update("tenant", test.seed, clock.Now(), nil)
			streamLimitCond := streamLimitExceeded(test.maxGlobalStreams)
			accepted, rejected := s.update("tenant", test.streams, clock.Now(), streamLimitCond)
			require.ElementsMatch(t, test.expectedAccepted, accepted)
			require.ElementsMatch(t, test.expectedRejected, rejected)
		})
	}
}

func TestUsageStore_Evict(t *testing.T) {
	s := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1)
	clock := quartz.NewMock(t)
	s.clock = clock
	s1 := streamUsage{hash: 0x1, lastSeenAt: clock.Now().UnixNano()}
	s.set("tenant1", s1)
	s2 := streamUsage{hash: 0x2, lastSeenAt: clock.Now().Add(-121 * time.Minute).UnixNano()}
	s.set("tenant1", s2)
	s3 := streamUsage{hash: 0x3, lastSeenAt: clock.Now().UnixNano()}
	s.set("tenant2", s3)
	s4 := streamUsage{hash: 0x4, lastSeenAt: clock.Now().Add(-59 * time.Minute).UnixNano()}
	s.set("tenant2", s4)
	// Evict all streams older than the window size.
	s.evict()
	actual := make(map[string][]streamUsage)
	s.all(func(tenant string, _ int32, stream streamUsage) {
		actual[tenant] = append(actual[tenant], stream)
	})
	// We can't use require.Equal as [All] iterates streams in a non-deterministic
	// order. Instead use ElementsMatch for each expected tenant.
	expected := map[string][]streamUsage{
		"tenant1": {s1},
		"tenant2": {s3, s4},
	}
	require.Len(t, actual, len(expected))
	for tenant := range expected {
		require.ElementsMatch(t, expected[tenant], actual[tenant])
	}
}

func TestUsageStore_EvictPartitions(t *testing.T) {
	// Create a store with 10 partitions.
	s := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 10)
	clock := quartz.NewMock(t)
	s.clock = clock
	// Create 10 streams. Since we use i as the hash, we can expect the
	// streams to be sharded over all 10 partitions.
	for i := 0; i < 10; i++ {
		s.set("tenant", streamUsage{hash: uint64(i)})
	}
	// Evict the first 5 partitions.
	s.evictPartitions([]int32{0, 1, 2, 3, 4})
	// The last 5 partitions should still have data.
	expected := []int32{5, 6, 7, 8, 9}
	actual := make([]int32, 0, len(expected))
	s.all(func(_ string, partition int32, _ streamUsage) {
		actual = append(actual, partition)
	})
	require.ElementsMatch(t, expected, actual)
}
