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

func TestUsageStore_Iter(t *testing.T) {
	t.Run("iterates all streams", func(t *testing.T) {
		s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 10, prometheus.NewRegistry())
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
		s.Iter(func(_ string, _ int32, s streamUsage) {
			actual = append(actual, s.hash)
		})
		require.ElementsMatch(t, expected, actual)
	})

	t.Run("does not iterate expired streams", func(t *testing.T) {
		s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, prometheus.NewRegistry())
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
		s.Iter(func(_ string, _ int32, _ streamUsage) { actual++ })
		require.Equal(t, 0, actual)
	})
}

func TestUsageStore_IterTenant(t *testing.T) {
	t.Run("iterates all streams for tenant", func(t *testing.T) {
		s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 10, prometheus.NewRegistry())
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
		s.IterTenant("tenant1", func(_ string, _ int32, stream streamUsage) {
			actual1 = append(actual1, stream.hash)
		})
		require.ElementsMatch(t, expected1, actual1)
		expected2 := []uint64{0x5, 0x6, 0x7, 0x8, 0x9}
		actual2 := make([]uint64, 0, 5)
		s.IterTenant("tenant2", func(_ string, _ int32, stream streamUsage) {
			actual2 = append(actual2, stream.hash)
		})
		require.ElementsMatch(t, expected2, actual2)
	})

	t.Run("does not iterate expired streams", func(t *testing.T) {
		s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, prometheus.NewRegistry())
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
		s.IterTenant("tenant1", func(_ string, _ int32, _ streamUsage) { actual++ })
		require.Equal(t, 0, actual)
	})
}

func TestUsageStore_Update(t *testing.T) {
	s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, prometheus.NewRegistry())
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
func TestUsageStore_UpdateRateBuckets(t *testing.T) {
	s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	s.clock = clock
	metadata := &proto.StreamMetadata{
		StreamHash: 0x1,
		TotalSize:  100,
	}
	// Metadata at clock.Now() should update the first rate bucket because
	// the mocked clock starts at 2024-01-01T00:00:00Z.
	time1 := clock.Now()
	require.NoError(t, s.Update("tenant", metadata, time1))
	stream, ok := s.getForTests("tenant", 0x1)
	require.True(t, ok)
	expected := newRateBuckets(5*time.Minute, time.Minute)
	expected[0].timestamp = time1.UnixNano()
	expected[0].size = 100
	require.Equal(t, expected, stream.rateBuckets)
	// Update the first bucket with the same metadata but 1 second later.
	clock.Advance(time.Second)
	time2 := clock.Now()
	require.NoError(t, s.Update("tenant", metadata, time2))
	expected[0].size = 200
	require.Equal(t, expected, stream.rateBuckets)
	// Advance the clock forward to the next bucket. Should update the second
	// bucket and leave the first bucket unmodified.
	clock.Advance(time.Minute)
	time3 := clock.Now()
	require.NoError(t, s.Update("tenant", metadata, time3))
	stream, ok = s.getForTests("tenant", 0x1)
	require.True(t, ok)
	// As the clock is now 1 second ahead of the bucket start time, we must
	// truncate the expected time to the start of the bucket.
	expected[1].timestamp = time3.Truncate(time.Minute).UnixNano()
	expected[1].size = 100
	require.Equal(t, expected, stream.rateBuckets)
	// Advance the clock to the last bucket.
	clock.Advance(3 * time.Minute)
	time4 := clock.Now()
	require.NoError(t, s.Update("tenant", metadata, time4))
	stream, ok = s.getForTests("tenant", 0x1)
	require.True(t, ok)
	expected[4].timestamp = time4.Truncate(time.Minute).UnixNano()
	expected[4].size = 100
	require.Equal(t, expected, stream.rateBuckets)
	// Advance the clock one last one. It should wrap around to the start of
	// the list and replace the original bucket with time1.
	clock.Advance(time.Minute)
	time5 := clock.Now()
	require.NoError(t, s.Update("tenant", metadata, time5))
	stream, ok = s.getForTests("tenant", 0x1)
	require.True(t, ok)
	expected[0].timestamp = time5.Truncate(time.Minute).UnixNano()
	expected[0].size = 100
	require.Equal(t, expected, stream.rateBuckets)
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
			s, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, test.numPartitions, prometheus.NewRegistry())
			require.NoError(t, err)
			clock := quartz.NewMock(t)
			s.clock = clock
			for _, stream := range test.seed {
				require.NoError(t, s.Update("tenant", stream, clock.Now()))
			}
			limits := mockLimits{MaxGlobalStreams: test.maxGlobalStreams}
			toProduce, accepted, rejected, err := s.UpdateCond("tenant", test.streams, clock.Now(), &limits)
			require.NoError(t, err)
			require.ElementsMatch(t, test.expectedToProduce, toProduce)
			require.ElementsMatch(t, test.expectedAccepted, accepted)
			require.ElementsMatch(t, test.expectedRejected, rejected)
		})
	}
}

func TestUsageStore_UpdateCond_ToProduce(t *testing.T) {
	s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	s.clock = clock
	limits := mockLimits{MaxGlobalStreams: 10}
	metadata1 := []*proto.StreamMetadata{{
		StreamHash: 0x1,
		TotalSize:  100,
	}}
	toProduce, accepted, rejected, err := s.UpdateCond("tenant", metadata1, clock.Now(), &limits)
	require.NoError(t, err)
	require.Empty(t, rejected)
	require.Len(t, accepted, 1)
	require.Equal(t, metadata1, toProduce)
	// Another update for the same stream in the same minute should not produce
	// a new record.
	clock.Advance(time.Second)
	toProduce, accepted, rejected, err = s.UpdateCond("tenant", metadata1, clock.Now(), &limits)
	require.NoError(t, err)
	require.Empty(t, rejected)
	require.Empty(t, toProduce)
	require.Len(t, accepted, 1)
	// A different record in the same minute should be produced.
	metadata2 := []*proto.StreamMetadata{{
		StreamHash: 0x2,
		TotalSize:  100,
	}}
	toProduce, accepted, rejected, err = s.UpdateCond("tenant", metadata2, clock.Now(), &limits)
	require.NoError(t, err)
	require.Empty(t, rejected)
	require.Len(t, accepted, 1)
	require.Equal(t, metadata2, toProduce)
	// Move the clock forward and metadata1 should be produced again.
	clock.Advance(time.Minute)
	toProduce, accepted, rejected, err = s.UpdateCond("tenant", metadata1, clock.Now(), &limits)
	require.NoError(t, err)
	require.Empty(t, rejected)
	require.Len(t, accepted, 1)
	require.Equal(t, metadata1, toProduce)
}

func TestUsageStore_Evict(t *testing.T) {
	s, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, prometheus.NewRegistry())
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
	s.IterTenant("tenant1", func(_ string, _ int32, _ streamUsage) {
		actual1++
	})
	require.Equal(t, 0, actual1)
	actual2 := 0
	s.IterTenant("tenant2", func(_ string, _ int32, _ streamUsage) {
		actual2++
	})
	require.Equal(t, 2, actual2)
}

func TestUsageStore_EvictPartitions(t *testing.T) {
	// Create a store with 10 partitions.
	s, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 10, prometheus.NewRegistry())
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
	// The last 5 partitions should still have data.
	expected := []int32{5, 6, 7, 8, 9}
	actual := make([]int32, 0, len(expected))
	s.Iter(func(_ string, partition int32, _ streamUsage) {
		actual = append(actual, partition)
	})
	require.ElementsMatch(t, expected, actual)
}

func newRateBuckets(rateWindow, bucketSize time.Duration) []rateBucket {
	return make([]rateBucket, int(rateWindow/bucketSize))
}
