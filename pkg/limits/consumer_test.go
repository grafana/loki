package limits

import (
	"context"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
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
		u, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, &mockLimits{}, reg)
		require.NoError(t, err)
		u.clock = clock
		c := newConsumer(&kafka, m, u, newOffsetReadinessCheck(m), "zone1",
			log.NewNopLogger(), prometheus.NewRegistry())
		ctx := context.Background()
		require.NoError(t, c.pollFetches(ctx))
		// Check that the record was stored.
		var n int
		u.Iter(func(_ string, _ int32, _ streamUsage) { n++ })
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
		u, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, &mockLimits{}, reg)
		require.NoError(t, err)
		u.clock = clock
		c := newConsumer(&kafka, m, u, newOffsetReadinessCheck(m), "zone1",
			log.NewNopLogger(), prometheus.NewRegistry())
		ctx := context.Background()
		require.NoError(t, c.pollFetches(ctx))
		// Check that the record was discarded.
		var n int
		u.Iter(func(_ string, _ int32, _ streamUsage) { n++ })
		require.Equal(t, 0, n)
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
	u, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, &mockLimits{}, reg)
	require.NoError(t, err)
	u.clock = clock
	c := newConsumer(&kafka, m, u, newOffsetReadinessCheck(m), "zone1",
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
	u.Iter(func(_ string, _ int32, _ streamUsage) { n++ })
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
	u.Iter(func(_ string, _ int32, _ streamUsage) { n++ })
	require.Equal(t, 2, n)
}

func TestConsumer_ProcessRecord_RateData(t *testing.T) {
	clock := quartz.NewMock(t)
	store, err := newUsageStore(15*time.Minute, 5*time.Minute, time.Minute, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	store.clock = clock

	consumer := &consumer{
		usage:            store,
		zone:             "zone1",
		recordsDiscarded: prometheus.NewCounter(prometheus.CounterOpts{Name: "test_discarded"}),
		recordsInvalid:   prometheus.NewCounter(prometheus.CounterOpts{Name: "test_invalid"}),
	}

	t.Run("processes rate data correctly", func(t *testing.T) {
		// Create a StreamMetadataRecord for rate data from a different zone
		// (so it won't be discarded)
		rateRecord := proto.StreamMetadataRecord{
			Zone:   "zone2", // Different zone
			Tenant: "tenant1",
			Metadata: &proto.StreamMetadata{
				StreamHash: 0x123,
				TotalSize:  500,
			},
		}
		b, err := rateRecord.Marshal()
		require.NoError(t, err)

		kafkaRecord := &kgo.Record{
			Key:       []byte("tenant1"),
			Value:     b,
			Timestamp: clock.Now(),
		}

		// Process the record
		err = consumer.processRecord(context.Background(), partitionReady, kafkaRecord)
		require.NoError(t, err)

		// Verify the data was stored and rate buckets were updated
		// Calculate the partition for this stream hash
		partition := int32(0x123 % 1) // 1 partition
		stream, exists := store.getStreamUsage("tenant1", partition, 0x123)
		require.True(t, exists)
		require.Equal(t, uint64(0x123), stream.hash)
		require.Equal(t, uint64(500), stream.totalSize)
		require.NotNil(t, stream.rateBuckets)
		require.Len(t, stream.rateBuckets, 5) // 5 minutes / 1 minute = 5 buckets
	})

	t.Run("handles invalid record data", func(t *testing.T) {
		// Create an invalid record
		kafkaRecord := &kgo.Record{
			Key:       []byte("tenant1"),
			Value:     []byte("invalid data"),
			Timestamp: clock.Now(),
		}

		// Process the invalid record
		err := consumer.processRecord(context.Background(), partitionReady, kafkaRecord)
		require.Error(t, err)
		require.Contains(t, err.Error(), "corrupted record")
	})
}
