package limits

import (
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestUsageStore_All(t *testing.T) {
	now := time.Now()
	cutoff := now.Add(-5 * time.Minute).UnixNano()
	// Create a store with 10 partitions.
	s := NewUsageStore(Config{NumPartitions: 10})
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
	s := NewUsageStore(Config{NumPartitions: 10})
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
			s := NewUsageStore(Config{NumPartitions: test.numPartitions})
			s.Update("tenant", test.seed, now.UnixNano(), cutoff, bucketStart, bucketCutoff, nil)
			streamLimitCond := streamLimitExceeded(test.maxGlobalStreams)
			accepted, rejected := s.Update("tenant", test.streams, now.UnixNano(), cutoff, bucketStart, bucketCutoff, streamLimitCond)
			require.ElementsMatch(t, test.expectedAccepted, accepted)
			require.ElementsMatch(t, test.expectedRejected, rejected)
		})
	}
}

func TestUsageStore_Evict(t *testing.T) {
	s := NewUsageStore(Config{
		NumPartitions: 1,
		WindowSize:    time.Hour,
	})
	clock := quartz.NewMock(t)
	s.clock = clock
	s1 := Stream{Hash: 0x1, LastSeenAt: clock.Now().UnixNano()}
	s.set("tenant1", s1)
	s2 := Stream{Hash: 0x2, LastSeenAt: clock.Now().Add(-61 * time.Minute).UnixNano()}
	s.set("tenant1", s2)
	s3 := Stream{Hash: 0x3, LastSeenAt: clock.Now().UnixNano()}
	s.set("tenant2", s3)
	s4 := Stream{Hash: 0x4, LastSeenAt: clock.Now().Add(-59 * time.Minute).UnixNano()}
	s.set("tenant2", s4)
	// Evict all streams older than the window size.
	s.Evict()
	actual := make(map[string][]Stream)
	s.All(func(tenant string, _ int32, stream Stream) {
		actual[tenant] = append(actual[tenant], stream)
	})
	expected := map[string][]Stream{
		"tenant1": {s1},
		"tenant2": {s3, s4},
	}
	require.Equal(t, expected, actual)
}

func TestUsageStore_EvictPartitions(t *testing.T) {
	// Create a store with 10 partitions.
	s := NewUsageStore(Config{NumPartitions: 10})
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
