package limits

import (
	"context"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestConsumer_ProcessRecords(t *testing.T) {
	t.Run("records for own zone are stored when replaying partitions", func(t *testing.T) {
		// Create a record in the same zone.
		sameZoneRecord := proto.StreamMetadataRecord{
			Zone:   "zone1",
			Tenant: "tenant",
			Metadata: &proto.StreamMetadata{
				StreamHash: 0x1,
				TotalSize:  100,
			},
		}
		b, err := sameZoneRecord.Marshal()
		require.NoError(t, err)
		// Set up a mock kafka that will return the record during the first poll.
		clock := quartz.NewMock(t)
		kafka := mockKafka{
			fetches: []kgo.Fetches{{{
				Topics: []kgo.FetchTopic{{
					Topic: "test",
					Partitions: []kgo.FetchPartition{{
						Partition: 1,
						Records: []*kgo.Record{{
							Key:       []byte("tenant"),
							Value:     b,
							Timestamp: clock.Now(),
						}},
					}},
				}},
			}}},
		}
		reg := prometheus.NewRegistry()
		// Need to assign the partition and set it to ready.
		m, err := newPartitionManager(reg)
		require.NoError(t, err)
		m.Assign([]int32{1})
		m.SetReplaying(1, 1000)
		// Create a usage store, we will use this to check if the record
		// was stored.
		s, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, &mockLimits{}, reg)
		require.NoError(t, err)
		s.clock = clock
		c := newConsumer(&kafka, m, s, nil, newOffsetReadinessCheck(m), "zone1", DefaultRemovalRecordSlack,
			log.NewNopLogger(), prometheus.NewRegistry())
		ctx := context.Background()
		require.NoError(t, c.pollFetches(ctx))
		// Check that the record was stored.
		var n int
		for range s.ActiveStreams() {
			n++
		}
		require.Equal(t, 1, n)
	})

	t.Run("records for own zone are discarded for ready partitions", func(t *testing.T) {
		// Create a record in the same zone.
		sameZoneRecord := proto.StreamMetadataRecord{
			Zone:   "zone1",
			Tenant: "tenant",
			Metadata: &proto.StreamMetadata{
				StreamHash: 0x1,
				TotalSize:  100,
			},
		}
		b, err := sameZoneRecord.Marshal()
		require.NoError(t, err)
		clock := quartz.NewMock(t)
		// Set up a mock kafka that will return the record during the first poll.
		kafka := mockKafka{
			fetches: []kgo.Fetches{{{
				Topics: []kgo.FetchTopic{{
					Topic: "test",
					Partitions: []kgo.FetchPartition{{
						Partition: 1,
						Records: []*kgo.Record{{
							Key:       []byte("tenant"),
							Value:     b,
							Timestamp: clock.Now(),
						}},
					}},
				}},
			}}},
		}
		reg := prometheus.NewRegistry()
		// Need to assign the partition and set it to ready.
		m, err := newPartitionManager(reg)
		require.NoError(t, err)
		m.Assign([]int32{1})
		m.SetReady(1)
		// Create a usage store, we will use this to check if the record
		// was discarded.
		s, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, &mockLimits{}, reg)
		require.NoError(t, err)
		s.clock = clock
		c := newConsumer(&kafka, m, s, nil, newOffsetReadinessCheck(m), "zone1", DefaultRemovalRecordSlack,
			log.NewNopLogger(), prometheus.NewRegistry())
		ctx := context.Background()
		require.NoError(t, c.pollFetches(ctx))
		// Check that the record was discarded.
		var n int
		for range s.ActiveStreams() {
			n++
		}
		require.Equal(t, 0, n)
	})

	t.Run("rate buckets are merged into the stream shard store", func(t *testing.T) {
		clock := quartz.NewMock(t)
		otherZoneRecord := proto.StreamMetadataRecord{
			Zone:     "zone2",
			Tenant:   "tenant",
			Metadata: &proto.StreamMetadata{StreamHash: 0x1},
			ShardRateBucket: &proto.ShardRateBucket{
				BucketStart: clock.Now().Truncate(DefaultBucketSize).UnixNano(),
				Size_:       600,
				Pushes:      3,
			},
			ShardCount: 2,
		}
		b, err := otherZoneRecord.Marshal()
		require.NoError(t, err)
		kafka := mockKafka{
			fetches: []kgo.Fetches{{{
				Topics: []kgo.FetchTopic{{
					Topic: "test",
					Partitions: []kgo.FetchPartition{{
						Partition: 1,
						Records: []*kgo.Record{{
							Key:       []byte("tenant"),
							Value:     b,
							Timestamp: clock.Now(),
						}},
					}},
				}},
			}}},
		}
		reg := prometheus.NewRegistry()
		m, err := newPartitionManager(reg)
		require.NoError(t, err)
		m.Assign([]int32{1})
		m.SetReady(1)
		usage, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, &mockLimits{}, reg)
		require.NoError(t, err)
		usage.clock = clock
		streamShards, err := newStreamShardStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, "zone1", true, &mockLimits{}, reg)
		require.NoError(t, err)
		streamShards.clock = clock
		c := newConsumer(&kafka, m, usage, streamShards, newOffsetReadinessCheck(m), "zone1", DefaultRemovalRecordSlack,
			log.NewNopLogger(), prometheus.NewRegistry())
		require.NoError(t, c.pollFetches(context.Background()))
		require.Equal(t, 1, countTrackedStreams(streamShards))
	})
}

func TestConsumer_ReadinessCheck(t *testing.T) {
	// Create two records. It doesn't matter which zone.
	sameZoneRecord := proto.StreamMetadataRecord{
		Zone:   "zone1",
		Tenant: "tenant",
		Metadata: &proto.StreamMetadata{
			StreamHash: 0x1,
			TotalSize:  100,
		},
	}
	b1, err := sameZoneRecord.Marshal()
	require.NoError(t, err)
	otherZoneRecord := proto.StreamMetadataRecord{
		Zone:   "zone2",
		Tenant: "tenant",
		Metadata: &proto.StreamMetadata{
			StreamHash: 0x2,
			TotalSize:  100,
		},
	}
	b2, err := otherZoneRecord.Marshal()
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	// Set up a mock kafka that will return the records over two consecutive
	// polls.
	kafka := mockKafka{
		fetches: []kgo.Fetches{{{
			// First poll.
			Topics: []kgo.FetchTopic{{
				Topic: "test",
				Partitions: []kgo.FetchPartition{{
					Partition: 1,
					Records: []*kgo.Record{{
						Key:       []byte("tenant"),
						Value:     b1,
						Timestamp: clock.Now(),
						Offset:    1,
					}},
				}},
			}},
		}}, {{
			// Second poll.
			Topics: []kgo.FetchTopic{{
				Topic: "test",
				Partitions: []kgo.FetchPartition{{
					Partition: 1,
					Records: []*kgo.Record{{
						Key:       []byte("tenant"),
						Value:     b2,
						Timestamp: clock.Now(),
						Offset:    2,
					}},
				}},
			}},
		}}},
	}
	reg := prometheus.NewRegistry()
	// Need to assign the partition and set it to replaying.
	m, err := newPartitionManager(reg)
	require.NoError(t, err)
	m.Assign([]int32{1})
	// The partition should be marked ready when the second record
	// has been consumed.
	m.SetReplaying(1, 2)
	// We don't need the usage store for this test.
	s, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, &mockLimits{}, reg)
	require.NoError(t, err)
	s.clock = clock
	c := newConsumer(&kafka, m, s, nil, newOffsetReadinessCheck(m), "zone1", DefaultRemovalRecordSlack,
		log.NewNopLogger(), prometheus.NewRegistry())
	// The first poll should fetch the first record.
	ctx := context.Background()
	require.NoError(t, c.pollFetches(ctx))
	// The partition should still be replaying as we have not read up to
	// the target offset.
	state, ok := m.GetState(1)
	require.True(t, ok)
	require.Equal(t, partitionReplaying, state)
	// Check that the record was stored.
	var n int
	for range s.ActiveStreams() {
		n++
	}
	require.Equal(t, 1, n)
	// The second poll should fetch the second (and last) record.
	require.NoError(t, c.pollFetches(ctx))
	// The partition should still be ready as we have read up to the target
	// offset.
	state, ok = m.GetState(1)
	require.True(t, ok)
	require.Equal(t, partitionReady, state)
	// Check that the record was stored.
	n = 0
	for range s.ActiveStreams() {
		n++
	}
	require.Equal(t, 2, n)
}

func TestConsumer_RemovalRecords(t *testing.T) {
	tests := []struct {
		name string
		// The zone of the removal record.
		zone string
		// The state of the partition when the removal record is consumed.
		state partitionState
		// How long after the removal the stream was last seen.
		lastSeenAfterRemoval time.Duration
		expectedStreams      int
		expectedRemoved      float64
	}{{
		name:            "stream is deleted in a ready partition",
		zone:            removalRecordZone,
		state:           partitionReady,
		expectedStreams: 0,
		expectedRemoved: 1,
	}, {
		name:            "stream is deleted in a replaying partition",
		zone:            removalRecordZone,
		state:           partitionReplaying,
		expectedStreams: 0,
		expectedRemoved: 1,
	}, {
		// Removal records are applied whatever their zone, so an ingester
		// that shares a zone name with this instance is not discarded as
		// one of our own records.
		name:            "stream is deleted when the record has our zone",
		zone:            "zone1",
		state:           partitionReady,
		expectedStreams: 0,
		expectedRemoved: 1,
	}, {
		name:                 "stream seen after the removal is kept",
		zone:                 removalRecordZone,
		state:                partitionReady,
		lastSeenAfterRemoval: DefaultRemovalRecordSlack + time.Second,
		expectedStreams:      1,
		expectedRemoved:      0,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := quartz.NewMock(t)
			removedLastSeenAt := clock.Now()
			rec := proto.StreamMetadataRecord{
				Zone:              test.zone,
				Tenant:            "tenant",
				Metadata:          &proto.StreamMetadata{StreamHash: 0x1},
				RemovedLastSeenAt: removedLastSeenAt.UnixNano(),
			}
			b, err := rec.Marshal()
			require.NoError(t, err)
			kafka := mockKafka{
				fetches: []kgo.Fetches{{{
					Topics: []kgo.FetchTopic{{
						Topic: "test",
						Partitions: []kgo.FetchPartition{{
							Partition: 1,
							Records: []*kgo.Record{{
								Key:       []byte("tenant"),
								Value:     b,
								Timestamp: clock.Now(),
							}},
						}},
					}},
				}}},
			}
			reg := prometheus.NewRegistry()
			m, err := newPartitionManager(reg)
			require.NoError(t, err)
			m.Assign([]int32{1})
			if test.state == partitionReady {
				m.SetReady(1)
			} else {
				m.SetReplaying(1, 1000)
			}
			s, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, &mockLimits{}, reg)
			require.NoError(t, err)
			s.clock = clock
			s.setForTests("tenant", streamUsage{
				hash:       0x1,
				lastSeenAt: removedLastSeenAt.Add(test.lastSeenAfterRemoval).UnixNano(),
			})
			c := newConsumer(&kafka, m, s, nil, newOffsetReadinessCheck(m), "zone1", DefaultRemovalRecordSlack,
				log.NewNopLogger(), prometheus.NewRegistry())
			require.NoError(t, c.pollFetches(context.Background()))
			var n int
			for range s.ActiveStreams() {
				n++
			}
			require.Equal(t, test.expectedStreams, n)
			require.Equal(t, test.expectedRemoved, testutil.ToFloat64(c.streamsRemoved))
			// A removal record is valid even though it has no other fields.
			require.Equal(t, float64(0), testutil.ToFloat64(c.recordsInvalid))
		})
	}
}
