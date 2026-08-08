package limits

import (
	"context"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestConsumer_ProcessRecords(t *testing.T) {
	t.Run("records for own zone are stored when replaying partitions", func(t *testing.T) {
		reg := prometheus.NewRegistry()
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
		fetchesCh := make(chan kgo.Fetches, 1)
		fetchesCh <- kgo.Fetches{{
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
		}}
		kafkaClient := mockKafka{fetches: fetchesCh}
		// Assign the partition to the PartitionManager and set it as ready.
		partitionManager, err := newPartitionManager(reg)
		require.NoError(t, err)
		partitionManager.Assign([]int32{1})
		partitionManager.SetReplaying(1, 1000)
		// Create a store, we will use this to assert the consumer added the stream
		// to the store as expected.
		store, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, &mockLimits{}, reg)
		require.NoError(t, err)
		store.clock = clock
		consumer := newConsumer(&kafkaClient, partitionManager, store, newOffsetReadinessCheck(partitionManager), "zone1", log.NewNopLogger(), reg)
		postFetchCh := make(chan struct{})
		consumer.postFetchCh = postFetchCh
		cancelCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		t.Cleanup(cancel)
		require.NoError(t, services.StartAndAwaitRunning(cancelCtx, consumer))
		defer services.StopAndAwaitTerminated(t.Context(), consumer) //nolint:errcheck
		<-postFetchCh
		// Check that the record was stored.
		var n int
		for range store.ActiveStreams() {
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
		fetchesCh := make(chan kgo.Fetches, 1)
		fetchesCh <- kgo.Fetches{{
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
		}}
		kafkaClient := mockKafka{fetches: fetchesCh}
		// Assign the partition to the PartitionManager and set it as ready.
		reg := prometheus.NewRegistry()
		partitionManager, err := newPartitionManager(reg)
		require.NoError(t, err)
		partitionManager.Assign([]int32{1})
		partitionManager.SetReady(1)
		// Create a usage store, we will use this to check if the record was discarded.
		store, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, &mockLimits{}, reg)
		require.NoError(t, err)
		store.clock = clock
		c := newConsumer(&kafkaClient, partitionManager, store, newOffsetReadinessCheck(partitionManager), "zone1", log.NewNopLogger(), prometheus.NewRegistry())
		postFetchCh := make(chan struct{})
		c.postFetchCh = postFetchCh
		cancelCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		t.Cleanup(cancel)
		require.NoError(t, services.StartAndAwaitRunning(cancelCtx, c))
		defer services.StopAndAwaitTerminated(t.Context(), c) //nolint:errcheck
		<-postFetchCh
		// Check that the record was discarded.
		var n int
		for range store.ActiveStreams() {
			n++
		}
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
	fetchesCh := make(chan kgo.Fetches, 2)
	fetchesCh <- kgo.Fetches{{
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
	}}
	kafkaClient := mockKafka{fetches: fetchesCh}
	reg := prometheus.NewRegistry()
	// Need to assign the partition and set it to replaying.
	partitionManager, err := newPartitionManager(reg)
	require.NoError(t, err)
	partitionManager.Assign([]int32{1})
	// The partition should be marked ready when the second record
	// has been consumed.
	partitionManager.SetReplaying(1, 2)
	// We don't need the usage store for this test.
	store, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, &mockLimits{}, reg)
	require.NoError(t, err)
	store.clock = clock
	c := newConsumer(&kafkaClient, partitionManager, store, newOffsetReadinessCheck(partitionManager), "zone1", log.NewNopLogger(), prometheus.NewRegistry())
	postFetchCh := make(chan struct{})
	c.postFetchCh = postFetchCh
	cancelCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, services.StartAndAwaitRunning(cancelCtx, c))
	defer services.StopAndAwaitTerminated(t.Context(), c) //nolint:errcheck
	// The first poll should fetch the first record.
	<-postFetchCh
	// The partition should still be replaying as we have not read up to
	// the target offset.
	state, ok := partitionManager.GetState(1)
	require.True(t, ok)
	require.Equal(t, partitionReplaying, state)
	// Check that the record was stored.
	var n int
	for range store.ActiveStreams() {
		n++
	}
	require.Equal(t, 1, n)
	// The second poll should fetch the second (and last) record.
	fetchesCh <- kgo.Fetches{{
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
	}}
	<-postFetchCh
	// The partition should still be ready as we have read up to the target
	// offset.
	state, ok = partitionManager.GetState(1)
	require.True(t, ok)
	require.Equal(t, partitionReady, state)
	// Check that the record was stored.
	n = 0
	for range store.ActiveStreams() {
		n++
	}
	require.Equal(t, 2, n)
}
