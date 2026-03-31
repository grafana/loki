package limits

import (
	"fmt"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestUsageStore_ActiveStreams(t *testing.T) {
	t.Run("iterates all streams", func(t *testing.T) {
		s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 10, &mockLimits{}, prometheus.NewRegistry())
		require.NoError(t, err)
		clock := quartz.NewMock(t)
		s.clock = clock
		for i := 0; i < 10; i++ {
			// Create 10 streams, one stream for each of the 10 partitions.
			require.NoError(t, s.Update(
				fmt.Sprintf("tenant%d", i),
				&proto.StreamMetadata{
					StreamHash: uint64(i),
					TotalSize:  100,
				},
				clock.Now()),
			)
		}
		// Assert that we can iterate all stored streams.
		expected := []uint64{0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9}
		actual := make([]uint64, 0, len(expected))
		for _, stream := range s.ActiveStreams() {
			actual = append(actual, stream.hash)
		}
		require.ElementsMatch(t, expected, actual)
	})

	t.Run("does not iterate expired streams", func(t *testing.T) {
		s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, &mockLimits{}, prometheus.NewRegistry())
		require.NoError(t, err)
		clock := quartz.NewMock(t)
		s.clock = clock
		require.NoError(t, s.Update(
			"tenant1",
			&proto.StreamMetadata{
				StreamHash: 0x1,
				TotalSize:  100,
			},
			clock.Now(),
		))
		// Advance the clock past the active time window.
		clock.Advance(15*time.Minute + 1)
		actual := 0
		for range s.ActiveStreams() {
			actual++
		}
		require.Equal(t, 0, actual)
	})
}

func TestUsageStore_TenantActiveStreams(t *testing.T) {
	t.Run("iterates all streams for tenant", func(t *testing.T) {
		s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 10, &mockLimits{}, prometheus.NewRegistry())
		require.NoError(t, err)
		clock := quartz.NewMock(t)
		s.clock = clock
		for i := 0; i < 10; i++ {
			tenant := "tenant1"
			if i >= 5 {
				tenant = "tenant2"
			}
			// Create 10 streams, one stream for each of the 10 partitions.
			require.NoError(t, s.Update(
				tenant,
				&proto.StreamMetadata{
					StreamHash: uint64(i),
					TotalSize:  100,
				},
				clock.Now(),
			))
		}
		// Check we can iterate the streams for each tenant.
		expected1 := []uint64{0x0, 0x1, 0x2, 0x3, 0x4}
		actual1 := make([]uint64, 0, 5)
		for _, stream := range s.TenantActiveStreams("tenant1") {
			actual1 = append(actual1, stream.hash)
		}
		require.ElementsMatch(t, expected1, actual1)
		expected2 := []uint64{0x5, 0x6, 0x7, 0x8, 0x9}
		actual2 := make([]uint64, 0, 5)
		for _, stream := range s.TenantActiveStreams("tenant2") {
			actual2 = append(actual2, stream.hash)
		}
		require.ElementsMatch(t, expected2, actual2)
	})

	t.Run("does not iterate expired streams", func(t *testing.T) {
		s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, &mockLimits{}, prometheus.NewRegistry())
		require.NoError(t, err)
		clock := quartz.NewMock(t)
		s.clock = clock
		require.NoError(t, s.Update(
			"tenant1",
			&proto.StreamMetadata{
				StreamHash: 0x1,
				TotalSize:  100,
			},
			clock.Now(),
		))
		// Advance the clock past the active time window.
		clock.Advance(15*time.Minute + 1)
		actual := 0
		for range s.TenantActiveStreams("tenant1") {
			actual++
		}
		require.Equal(t, 0, actual)
	})
}

func TestUsageStore_Update(t *testing.T) {
	s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, &mockLimits{}, prometheus.NewRegistry())
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	s.clock = clock
	metadata := &proto.StreamMetadata{
		StreamHash: 0x1,
		TotalSize:  100,
	}
	// Metadata outside the active time window returns an error.
	time1 := clock.Now().Add(-DefaultActiveWindow - 1)
	require.EqualError(t, s.Update("tenant", metadata, time1), "outside active time window")
	// Metadata within the active time window is accepted.
	time2 := clock.Now()
	require.NoError(t, s.Update("tenant", metadata, time2))
}

// This test asserts that we update the correct rate buckets, and as rate
// buckets are implemented as a circular list, when we reach the end of
// list the next bucket is the start of the list.
func TestUsageStore_UpdateRates(t *testing.T) {
	s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, &mockLimits{}, prometheus.NewRegistry())
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	s.clock = clock
	metadata := []*proto.StreamMetadata{{
		StreamHash: 0x1,
		TotalSize:  100,
	}}
	// Metadata at clock.Now() should update the first rate bucket because
	// the mocked clock starts at 2024-01-01T00:00:00Z.
	time1 := clock.Now()
	rates, err := s.UpdateRates("tenant", metadata, time1)
	require.NoError(t, err)
	expected := make([]rateBucket, 1, 5)
	expected[0].timestamp = time1.UnixNano()
	expected[0].size = 100
	require.Len(t, rates, 1)
	require.Equal(t, expected, rates[0].rateBuckets)
	// Update the first bucket with the same metadata but 1 second later.
	clock.Advance(time.Second)
	time2 := clock.Now()
	rates, err = s.UpdateRates("tenant", metadata, time2)
	require.NoError(t, err)
	expected[0].size = 200
	require.Equal(t, expected, rates[0].rateBuckets)
	// Advance the clock forward to the next bucket. Should update the second
	// bucket and leave the first bucket unmodified.
	clock.Advance(time.Minute)
	time3 := clock.Now()
	rates, err = s.UpdateRates("tenant", metadata, time3)
	require.NoError(t, err)
	// As the clock is now 1 second ahead of the bucket start time, we must
	// truncate the expected time to the start of the bucket.
	expected = append(expected, rateBucket{})
	expected[1].timestamp = time3.Truncate(time.Minute).UnixNano()
	expected[1].size = 100
	require.Equal(t, expected, rates[0].rateBuckets)
	// Advance the clock to the last bucket.
	clock.Advance(3 * time.Minute)
	time4 := clock.Now()
	rates, err = s.UpdateRates("tenant", metadata, time4)
	require.NoError(t, err)
	expected = append(expected, rateBucket{})
	expected[2].timestamp = time4.Truncate(time.Minute).UnixNano()
	expected[2].size = 100
	require.Equal(t, expected, rates[0].rateBuckets)
	// Advance the clock one last one. It should wrap around to the start of
	// the list and replace the original bucket with time1.
	clock.Advance(time.Minute)
	time5 := clock.Now()
	rates, err = s.UpdateRates("tenant", metadata, time5)
	require.NoError(t, err)
	expected[0].timestamp = time5.Truncate(time.Minute).UnixNano()
	expected[0].size = 100
	require.Equal(t, expected, rates[0].rateBuckets)
}

// This test asserts that rate buckets are not updated while the TODOs are
// in place.
func TestUsageStore_RateBucketsAreNotUsed(t *testing.T) {
	s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, &mockLimits{}, prometheus.NewRegistry())
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	s.clock = clock
	metadata := &proto.StreamMetadata{
		StreamHash: 0x1,
		TotalSize:  100,
	}
	require.NoError(t, s.Update("tenant", metadata, clock.Now()))

	partition := s.getPartitionForHash(0x1)
	i := s.getStripe("tenant")
	stream, ok := s.stripes[i]["tenant"][partition][noPolicy][0x1]
	require.True(t, ok)
	require.Equal(t, uint64(0), stream.totalSize)
	require.Nil(t, stream.rateBuckets)
}

// TestUsageStore_UpdateRates_AfterUpdate asserts that UpdateRates works correctly
// when called on a stream that was previously created via Update() (which doesn't
// initialize rateBuckets). This prevents panics when a stream is created by
// ExceedsLimits and then UpdateRates is called on it (e.g., from cross-zone replication).
func TestUsageStore_UpdateRates_AfterUpdate(t *testing.T) {
	s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, &mockLimits{}, prometheus.NewRegistry())
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	s.clock = clock

	// First, create a stream via Update() (simulates ExceedsLimits creating the stream)
	err = s.Update("tenant", &proto.StreamMetadata{
		StreamHash: 0x1,
		TotalSize:  50,
	}, clock.Now())
	require.NoError(t, err)

	// Verify the stream exists but has no rate buckets
	s.withRLock("tenant", func(i int) {
		partition := s.getPartitionForHash(0x1)
		stream, ok := s.stripes[i]["tenant"][partition][noPolicy][0x1]
		require.True(t, ok, "stream should exist")
		require.Nil(t, stream.rateBuckets, "rateBuckets should be nil after Update()")
	})

	// Now call UpdateRates on the same stream (simulates cross-zone replication)
	// This should NOT panic and should initialize rateBuckets
	rates, err := s.UpdateRates("tenant", []*proto.StreamMetadata{{
		StreamHash: 0x1,
		TotalSize:  100,
	}}, clock.Now())
	require.NoError(t, err)
	require.Len(t, rates, 1)
	require.NotNil(t, rates[0].rateBuckets, "rateBuckets should be initialized")
	require.Greater(t, len(rates[0].rateBuckets), 0, "rateBuckets should have entries")
}

func TestUsageStore_UpdateCond(t *testing.T) {
	tests := []struct {
		name             string
		numPartitions    int
		maxGlobalStreams int
		// seed contains the (optional) streams that should be seeded before
		// the test.
		seed              []*proto.StreamMetadata
		streams           []*proto.StreamMetadata
		expectedToProduce []*proto.StreamMetadata
		expectedAccepted  []*proto.StreamMetadata
		expectedRejected  []*proto.StreamMetadata
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
		expectedToProduce: []*proto.StreamMetadata{
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
		expectedToProduce: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000},
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
		maxGlobalStreams: 2,
		streams: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000}, // partition 0
			{StreamHash: 0x1, TotalSize: 1000}, // partition 1
			{StreamHash: 0x3, TotalSize: 1000}, // partition 1
			{StreamHash: 0x5, TotalSize: 1000}, // partition 1
		},
		expectedToProduce: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000},
			{StreamHash: 0x1, TotalSize: 1000},
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
		maxGlobalStreams: 2,
		streams: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000}, // partition 0
			{StreamHash: 0x1, TotalSize: 1000}, // partition 1
			{StreamHash: 0x2, TotalSize: 1000}, // partition 0
			{StreamHash: 0x3, TotalSize: 1000}, // partition 1
		},
		expectedToProduce: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000},
			{StreamHash: 0x1, TotalSize: 1000},
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
		expectedToProduce: []*proto.StreamMetadata{
			{StreamHash: 0x0, TotalSize: 1000},
			{StreamHash: 0x1, TotalSize: 1000},
			{StreamHash: 0x2, TotalSize: 1000},
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
			s, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, test.numPartitions, &mockLimits{MaxGlobalStreams: test.maxGlobalStreams}, prometheus.NewRegistry())
			require.NoError(t, err)
			clock := quartz.NewMock(t)
			s.clock = clock
			for _, stream := range test.seed {
				require.NoError(t, s.Update("tenant", stream, clock.Now()))
			}
			toProduce, accepted, rejected, err := s.UpdateCond("tenant", test.streams, clock.Now())
			require.NoError(t, err)
			require.ElementsMatch(t, test.expectedToProduce, toProduce)
			require.ElementsMatch(t, test.expectedAccepted, accepted)
			require.ElementsMatch(t, test.expectedRejected, rejected)
		})
	}
}

func TestUsageStore_UpdateCond_ToProduce(t *testing.T) {
	s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, &mockLimits{}, prometheus.NewRegistry())
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	s.clock = clock
	metadata1 := []*proto.StreamMetadata{{
		StreamHash: 0x1,
		TotalSize:  100,
	}}
	toProduce, accepted, rejected, err := s.UpdateCond("tenant", metadata1, clock.Now())
	require.NoError(t, err)
	require.Empty(t, rejected)
	require.Len(t, accepted, 1)
	require.Equal(t, metadata1, toProduce)
	// Another update for the same stream in the same minute should not produce
	// a new record.
	clock.Advance(time.Second)
	toProduce, accepted, rejected, err = s.UpdateCond("tenant", metadata1, clock.Now())
	require.NoError(t, err)
	require.Empty(t, rejected)
	require.Empty(t, toProduce)
	require.Len(t, accepted, 1)
	// A different record in the same minute should be produced.
	metadata2 := []*proto.StreamMetadata{{
		StreamHash: 0x2,
		TotalSize:  100,
	}}
	toProduce, accepted, rejected, err = s.UpdateCond("tenant", metadata2, clock.Now())
	require.NoError(t, err)
	require.Empty(t, rejected)
	require.Len(t, accepted, 1)
	require.Equal(t, metadata2, toProduce)
	// Move the clock forward and metadata1 should be produced again.
	clock.Advance(time.Minute)
	toProduce, accepted, rejected, err = s.UpdateCond("tenant", metadata1, clock.Now())
	require.NoError(t, err)
	require.Empty(t, rejected)
	require.Len(t, accepted, 1)
	require.Equal(t, metadata1, toProduce)
}

func TestUsageStore_Evict(t *testing.T) {
	s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, &mockLimits{}, prometheus.NewRegistry())
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	s.clock = clock
	// The stream 0x1 should be evicted when the clock is advanced 15 mins.
	require.NoError(t, s.Update("tenant1", &proto.StreamMetadata{
		StreamHash: 0x1,
	}, clock.Now()))
	// The streams 0x2 and 0x3 should not be evicted as will still be within
	// the active time window after advancing the clock.
	require.NoError(t, s.Update("tenant2", &proto.StreamMetadata{
		StreamHash: 0x2,
	}, clock.Now().Add(5*time.Minute)))
	require.NoError(t, s.Update("tenant2", &proto.StreamMetadata{
		StreamHash: 0x3,
	}, clock.Now().Add(15*time.Minute)))
	// Advance the clock and run an eviction.
	clock.Advance(15*time.Minute + 1)
	s.Evict()
	actual1 := 0
	for range s.TenantActiveStreams("tenant1") {
		actual1++
	}
	require.Equal(t, 0, actual1)
	actual2 := 0
	for range s.TenantActiveStreams("tenant2") {
		actual2++
	}
	require.Equal(t, 2, actual2)
}

func TestUsageStore_EvictPartitions(t *testing.T) {
	// Create a store with 10 partitions.
	s, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 10, &mockLimits{}, prometheus.NewRegistry())
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	s.clock = clock
	// Create 10 streams. Since we use i as the hash, we can expect the
	// streams to be sharded over all 10 partitions.
	for i := 0; i < 10; i++ {
		s.setForTests("tenant", streamUsage{hash: uint64(i), lastSeenAt: clock.Now().UnixNano()})
	}
	// Evict the first 5 partitions.
	s.EvictPartitions([]int32{0, 1, 2, 3, 4})
	// The streams for the last 5 partitions should not have been evicted.
	expected := []uint64{5, 6, 7, 8, 9}
	actual := make([]uint64, 0, len(expected))
	for _, stream := range s.ActiveStreams() {
		actual = append(actual, stream.hash)
	}
	require.ElementsMatch(t, expected, actual)
}

func TestUsageStore_PolicyBasedStreamLimits(t *testing.T) {
	t.Run("policy-specific stream limits override default limits", func(t *testing.T) {
		// Create a mockLimits with policy-specific overrides
		limits := &mockLimits{
			MaxGlobalStreams: 10, // Default limit: 10 streams
		}

		// Add policy-specific limits
		policyLimits := &mockLimitsWithPolicy{
			mockLimits: *limits,
			policyLimits: map[string]int{
				"high-priority": 5, // Policy-specific limit: 5 streams
				"low-priority":  3, // Policy-specific limit: 3 streams
			},
		}

		s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, policyLimits, prometheus.NewRegistry())
		require.NoError(t, err)
		clock := quartz.NewMock(t)
		s.clock = clock

		// Test 1: Default streams (no policy) should use default limit (10)
		defaultStreams := []*proto.StreamMetadata{
			{StreamHash: 0x1, TotalSize: 100}, // Should be accepted
			{StreamHash: 0x2, TotalSize: 100}, // Should be accepted
			{StreamHash: 0x3, TotalSize: 100}, // Should be accepted
			{StreamHash: 0x4, TotalSize: 100}, // Should be accepted
			{StreamHash: 0x5, TotalSize: 100}, // Should be accepted
			{StreamHash: 0x6, TotalSize: 100}, // Should be accepted
			{StreamHash: 0x7, TotalSize: 100}, // Should be accepted
			{StreamHash: 0x8, TotalSize: 100}, // Should be accepted
			{StreamHash: 0x9, TotalSize: 100}, // Should be accepted
			{StreamHash: 0xA, TotalSize: 100}, // Should be accepted (10th stream)
			{StreamHash: 0xB, TotalSize: 100}, // Should be rejected (11th stream)
		}

		toProduce, accepted, rejected, err := s.UpdateCond("tenant", defaultStreams, clock.Now())
		require.NoError(t, err)
		require.Len(t, accepted, 10)  // First 10 streams accepted
		require.Len(t, rejected, 1)   // 11th stream rejected
		require.Len(t, toProduce, 10) // First 10 streams should be produced
	})

	t.Run("streams with different policies tracked separately", func(t *testing.T) {
		policyLimits := &mockLimitsWithPolicy{
			mockLimits: mockLimits{MaxGlobalStreams: 10},
			policyLimits: map[string]int{
				"high-priority": 3, // 3 streams allowed
				"low-priority":  2, // 2 streams allowed
			},
		}

		s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, policyLimits, prometheus.NewRegistry())
		require.NoError(t, err)
		clock := quartz.NewMock(t)
		s.clock = clock

		// Test high-priority policy streams
		highPriorityStreams := []*proto.StreamMetadata{
			{StreamHash: 0x1, TotalSize: 100, IngestionPolicy: "high-priority"}, // Accepted
			{StreamHash: 0x2, TotalSize: 100, IngestionPolicy: "high-priority"}, // Accepted
			{StreamHash: 0x3, TotalSize: 100, IngestionPolicy: "high-priority"}, // Accepted
			{StreamHash: 0x4, TotalSize: 100, IngestionPolicy: "high-priority"}, // Rejected (4th stream)
		}

		toProduce, accepted, rejected, err := s.UpdateCond("tenant", highPriorityStreams, clock.Now())
		require.NoError(t, err)
		require.Len(t, accepted, 3) // First 3 streams accepted
		require.Len(t, rejected, 1) // 4th stream rejected
		require.Len(t, toProduce, 3)

		// Test low-priority policy streams (should be tracked separately)
		lowPriorityStreams := []*proto.StreamMetadata{
			{StreamHash: 0x5, TotalSize: 100, IngestionPolicy: "low-priority"}, // Accepted
			{StreamHash: 0x6, TotalSize: 100, IngestionPolicy: "low-priority"}, // Accepted
			{StreamHash: 0x7, TotalSize: 100, IngestionPolicy: "low-priority"}, // Rejected (3rd stream)
		}

		toProduce, accepted, rejected, err = s.UpdateCond("tenant", lowPriorityStreams, clock.Now())
		require.NoError(t, err)
		require.Len(t, accepted, 2) // First 2 streams accepted
		require.Len(t, rejected, 1) // 3rd stream rejected
		require.Len(t, toProduce, 2)
	})

	t.Run("default streams tracked separately from policy streams", func(t *testing.T) {
		policyLimits := &mockLimitsWithPolicy{
			mockLimits: mockLimits{MaxGlobalStreams: 5}, // Default limit: 5 streams
			policyLimits: map[string]int{
				"special-policy": 3, // Policy limit: 3 streams
			},
		}

		s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, policyLimits, prometheus.NewRegistry())
		require.NoError(t, err)
		clock := quartz.NewMock(t)
		s.clock = clock

		// First, add some default streams (no policy)
		defaultStreams := []*proto.StreamMetadata{
			{StreamHash: 0x1, TotalSize: 100}, // Default stream - accepted
			{StreamHash: 0x2, TotalSize: 100}, // Default stream - accepted
			{StreamHash: 0x3, TotalSize: 100}, // Default stream - accepted
			{StreamHash: 0x4, TotalSize: 100}, // Default stream - accepted
			{StreamHash: 0x5, TotalSize: 100}, // Default stream - accepted
			{StreamHash: 0x6, TotalSize: 100}, // Default stream - rejected (6th stream)
		}

		toProduce, accepted, rejected, err := s.UpdateCond("tenant", defaultStreams, clock.Now())
		require.NoError(t, err)
		require.Len(t, accepted, 5) // First 5 default streams accepted
		require.Len(t, rejected, 1) // 6th default stream rejected
		require.Len(t, toProduce, 5)

		// Now add policy streams (should be tracked separately and not affect default stream count)
		policyStreams := []*proto.StreamMetadata{
			{StreamHash: 0x7, TotalSize: 100, IngestionPolicy: "special-policy"}, // Policy stream - accepted
			{StreamHash: 0x8, TotalSize: 100, IngestionPolicy: "special-policy"}, // Policy stream - accepted
			{StreamHash: 0x9, TotalSize: 100, IngestionPolicy: "special-policy"}, // Policy stream - accepted
			{StreamHash: 0xA, TotalSize: 100, IngestionPolicy: "special-policy"}, // Policy stream - rejected (4th policy stream)
		}

		toProduce, accepted, rejected, err = s.UpdateCond("tenant", policyStreams, clock.Now())
		require.NoError(t, err)
		require.Len(t, accepted, 3) // First 3 policy streams accepted
		require.Len(t, rejected, 1) // 4th policy stream rejected
		require.Len(t, toProduce, 3)

		// Verify that we can still add more default streams (they're tracked separately)
		moreDefaultStreams := []*proto.StreamMetadata{
			{StreamHash: 0xB, TotalSize: 100}, // This should still be rejected because we already have 5 default streams
		}

		toProduce, accepted, rejected, err = s.UpdateCond("tenant", moreDefaultStreams, clock.Now())
		require.NoError(t, err)
		require.Len(t, accepted, 0) // No more default streams can be accepted
		require.Len(t, rejected, 1) // This default stream is rejected
		require.Len(t, toProduce, 0)
	})

	t.Run("policies without custom limits use default bucket", func(t *testing.T) {
		policyLimits := &mockLimitsWithPolicy{
			mockLimits: mockLimits{MaxGlobalStreams: 4}, // Default limit: 4 streams
			policyLimits: map[string]int{
				"custom-policy": 2, // Only this policy has a custom limit
			},
		}

		s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, policyLimits, prometheus.NewRegistry())
		require.NoError(t, err)
		clock := quartz.NewMock(t)
		s.clock = clock

		// Streams with "custom-policy" should use the custom limit (2 streams)
		customPolicyStreams := []*proto.StreamMetadata{
			{StreamHash: 0x1, TotalSize: 100, IngestionPolicy: "custom-policy"}, // Accepted
			{StreamHash: 0x2, TotalSize: 100, IngestionPolicy: "custom-policy"}, // Accepted
			{StreamHash: 0x3, TotalSize: 100, IngestionPolicy: "custom-policy"}, // Rejected (3rd stream)
		}

		toProduce, accepted, rejected, err := s.UpdateCond("tenant", customPolicyStreams, clock.Now())
		require.NoError(t, err)
		require.Len(t, accepted, 2) // First 2 custom policy streams accepted
		require.Len(t, rejected, 1) // 3rd custom policy stream rejected
		require.Len(t, toProduce, 2)

		// Streams with "other-policy" (no custom limit) should use default bucket and count against default limit
		otherPolicyStreams := []*proto.StreamMetadata{
			{StreamHash: 0x4, TotalSize: 100, IngestionPolicy: "other-policy"}, // Should go to default bucket
			{StreamHash: 0x5, TotalSize: 100, IngestionPolicy: "other-policy"}, // Should go to default bucket
			{StreamHash: 0x6, TotalSize: 100, IngestionPolicy: "other-policy"}, // Should go to default bucket
			{StreamHash: 0x7, TotalSize: 100, IngestionPolicy: "other-policy"}, // Should go to default bucket
			{StreamHash: 0x8, TotalSize: 100, IngestionPolicy: "other-policy"}, // Should be rejected (5th stream in default bucket)
		}

		toProduce, accepted, rejected, err = s.UpdateCond("tenant", otherPolicyStreams, clock.Now())
		require.NoError(t, err)
		require.Len(t, accepted, 4) // First 4 other policy streams accepted (using default bucket)
		require.Len(t, rejected, 1) // 5th other policy stream rejected
		require.Len(t, toProduce, 4)
	})
}

// mockLimitsWithPolicy extends mockLimits to support policy-specific limits
type mockLimitsWithPolicy struct {
	mockLimits
	policyLimits map[string]int
}

func (m *mockLimitsWithPolicy) PolicyMaxGlobalStreamsPerUser(_, policy string) (int, bool) {
	if limit, exists := m.policyLimits[policy]; exists {
		return limit, true
	}
	return 0, false // No custom limit for this policy
}

func newRateBuckets(rateWindow, bucketSize time.Duration) []rateBucket {
	return make([]rateBucket, int(rateWindow/bucketSize))
}
