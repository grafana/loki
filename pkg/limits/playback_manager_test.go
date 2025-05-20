package limits

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestPlaybackManager_ProcessRecords(t *testing.T) {
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
		k := mockKafka{
			fetches: []kgo.Fetches{{{
				Topics: []kgo.FetchTopic{{
					Topic: "test",
					Partitions: []kgo.FetchPartition{{
						Partition: 1,
						Records: []*kgo.Record{{
							Key:   []byte("tenant"),
							Value: b,
						}},
					}},
				}},
			}}},
		}
		ctx := context.Background()
		// Need to assign the partition and set it to ready.
		m := NewPartitionManager()
		m.Assign(ctx, []int32{1})
		m.SetReplaying(1, 1000)
		// Create a usage store, we will use this to check if the record
		// was stored.
		u := NewUsageStore(Config{
			ActiveWindow:  time.Hour,
			NumPartitions: 1,
		})
		p := NewPlaybackManager(&k, m, u, NewOffsetReadinessCheck(m), "zone1",
			log.NewNopLogger(), prometheus.NewRegistry())
		require.NoError(t, p.pollFetches(ctx))
		// Check that the record was stored.
		var n int
		u.All(func(_ string, _ int32, _ Stream) { n++ })
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
		// Set up a mock kafka that will return the record during the first poll.
		k := mockKafka{
			fetches: []kgo.Fetches{{{
				Topics: []kgo.FetchTopic{{
					Topic: "test",
					Partitions: []kgo.FetchPartition{{
						Partition: 1,
						Records: []*kgo.Record{{
							Key:   []byte("tenant"),
							Value: b,
						}},
					}},
				}},
			}}},
		}
		ctx := context.Background()
		// Need to assign the partition and set it to ready.
		m := NewPartitionManager()
		m.Assign(ctx, []int32{1})
		m.SetReady(1)
		// Create a usage store, we will use this to check if the record
		// was discarded.
		u := NewUsageStore(Config{
			ActiveWindow:  time.Hour,
			NumPartitions: 1,
		})
		p := NewPlaybackManager(&k, m, u, NewOffsetReadinessCheck(m), "zone1",
			log.NewNopLogger(), prometheus.NewRegistry())
		require.NoError(t, p.pollFetches(ctx))
		// Check that the record was discarded.
		var n int
		u.All(func(_ string, _ int32, _ Stream) { n++ })
		require.Equal(t, 0, n)
	})
}

func TestPlaybackmanager_ReadinessCheck(t *testing.T) {
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
	// Set up a mock kafka that will return the records over two consecutive
	// polls.
	k := mockKafka{
		fetches: []kgo.Fetches{{{
			// First poll.
			Topics: []kgo.FetchTopic{{
				Topic: "test",
				Partitions: []kgo.FetchPartition{{
					Partition: 1,
					Records: []*kgo.Record{{
						Key:    []byte("tenant"),
						Value:  b1,
						Offset: 1,
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
						Key:    []byte("tenant"),
						Value:  b2,
						Offset: 2,
					}},
				}},
			}},
		}}},
	}
	ctx := context.Background()
	// Need to assign the partition and set it to replaying.
	m := NewPartitionManager()
	m.Assign(ctx, []int32{1})
	// The partition should be marked ready when the second record
	// has been consumed.
	m.SetReplaying(1, 2)
	// We don't need the usage store for this test.
	u := NewUsageStore(Config{
		ActiveWindow:  time.Hour,
		NumPartitions: 1,
	})
	p := NewPlaybackManager(&k, m, u, NewOffsetReadinessCheck(m), "zone1",
		log.NewNopLogger(), prometheus.NewRegistry())
	// The first poll should fetch the first record.
	require.NoError(t, p.pollFetches(ctx))
	// The partition should still be replaying as we have not read up to
	// the target offset.
	state, ok := m.GetState(1)
	require.True(t, ok)
	require.Equal(t, PartitionReplaying, state)
	// Check that the record was stored.
	var n int
	u.All(func(_ string, _ int32, _ Stream) { n++ })
	require.Equal(t, 1, n)
	// The second poll should fetch the second (and last) record.
	require.NoError(t, p.pollFetches(ctx))
	// The partition should still be ready as we have read up to the target
	// offset.
	state, ok = m.GetState(1)
	require.True(t, ok)
	require.Equal(t, PartitionReady, state)
	// Check that the record was stored.
	n = 0
	u.All(func(_ string, _ int32, _ Stream) { n++ })
	require.Equal(t, 2, n)
}
