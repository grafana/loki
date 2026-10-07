package builder

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafkav2"
)

func TestOffsetCommittedDownscaleFunc(t *testing.T) {
	const (
		testTopic         = "test-topic"
		testConsumerGroup = "test-consumer-group"
		testPartition     = int32(0)
	)

	// newTestPartition returns a client for a fake cluster with one partition
	// in testTopic, and the downscale func for the given partition.
	newTestPartition := func(t *testing.T, partition int32) (*kgo.Client, downscalePermittedFunc) {
		t.Helper()
		cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, testTopic))
		require.NoError(t, err)
		t.Cleanup(cluster.Close)
		client, err := kgo.NewClient(
			kgo.SeedBrokers(cluster.ListenAddrs()[0]),
			kgo.RecordPartitioner(kgo.ManualPartitioner()),
		)
		require.NoError(t, err)
		t.Cleanup(client.Close)
		offsetReader := kafkav2.NewOffsetReader(client, testTopic, testConsumerGroup, log.NewNopLogger())
		return client, newOffsetCommittedDownscaleFunc(offsetReader, partition, log.NewNopLogger())
	}

	produce := func(t *testing.T, client *kgo.Client, n int) {
		t.Helper()
		for range n {
			res := client.ProduceSync(t.Context(), &kgo.Record{
				Topic:     testTopic,
				Partition: testPartition,
				Value:     []byte("foo"),
				Timestamp: time.Now(),
			})
			require.NoError(t, res.FirstErr())
		}
	}

	// deleteRecordsBefore moves the log start offset to offset, as retention
	// does.
	deleteRecordsBefore := func(t *testing.T, client *kgo.Client, offset int64) {
		t.Helper()
		offsets := kadm.Offsets{}
		offsets.AddOffset(testTopic, testPartition, offset, -1)
		res, err := kadm.NewClient(client).DeleteRecords(t.Context(), offsets)
		require.NoError(t, err)
		require.NoError(t, res.Error())
	}

	commit := func(t *testing.T, client *kgo.Client, offset int64) {
		t.Helper()
		committer := kafkav2.NewGroupCommitter(kadm.NewClient(client), testTopic, testConsumerGroup)
		require.NoError(t, committer.Commit(t.Context(), testPartition, offset))
	}

	t.Run("allows downscale when no records were produced", func(t *testing.T) {
		_, downscalePermitted := newTestPartition(t, testPartition)
		permitted, err := downscalePermitted(t.Context())
		require.NoError(t, err)
		require.True(t, permitted)
	})

	t.Run("allows downscale when the last record is committed", func(t *testing.T) {
		client, downscalePermitted := newTestPartition(t, testPartition)
		produce(t, client, 5)
		commit(t, client, 4)
		permitted, err := downscalePermitted(t.Context())
		require.NoError(t, err)
		require.True(t, permitted)
	})

	t.Run("blocks downscale when the group never committed and records remain", func(t *testing.T) {
		client, downscalePermitted := newTestPartition(t, testPartition)
		produce(t, client, 5)
		permitted, err := downscalePermitted(t.Context())
		require.NoError(t, err)
		require.False(t, permitted)
	})

	t.Run("blocks downscale when records after the committed offset remain", func(t *testing.T) {
		client, downscalePermitted := newTestPartition(t, testPartition)
		produce(t, client, 5)
		commit(t, client, 2)
		permitted, err := downscalePermitted(t.Context())
		require.NoError(t, err)
		require.False(t, permitted)
	})

	t.Run("allows downscale when retention deleted every record and the group never committed", func(t *testing.T) {
		client, downscalePermitted := newTestPartition(t, testPartition)
		produce(t, client, 5)
		deleteRecordsBefore(t, client, 5)
		permitted, err := downscalePermitted(t.Context())
		require.NoError(t, err)
		require.True(t, permitted)
	})

	t.Run("allows downscale when retention deleted every record after the committed offset", func(t *testing.T) {
		client, downscalePermitted := newTestPartition(t, testPartition)
		produce(t, client, 5)
		commit(t, client, 1)
		deleteRecordsBefore(t, client, 5)
		permitted, err := downscalePermitted(t.Context())
		require.NoError(t, err)
		require.True(t, permitted)
	})

	t.Run("blocks downscale when retention deleted some uncommitted records and others remain", func(t *testing.T) {
		client, downscalePermitted := newTestPartition(t, testPartition)
		produce(t, client, 5)
		commit(t, client, 0)
		deleteRecordsBefore(t, client, 3)
		permitted, err := downscalePermitted(t.Context())
		require.NoError(t, err)
		require.False(t, permitted)
	})

	t.Run("allows downscale when the commit catches up after retention deleted some records", func(t *testing.T) {
		client, downscalePermitted := newTestPartition(t, testPartition)
		produce(t, client, 5)
		commit(t, client, 0)
		deleteRecordsBefore(t, client, 3)
		commit(t, client, 4)
		permitted, err := downscalePermitted(t.Context())
		require.NoError(t, err)
		require.True(t, permitted)
	})

	t.Run("allows downscale when the committed offset is past the end offset", func(t *testing.T) {
		client, downscalePermitted := newTestPartition(t, testPartition)
		produce(t, client, 5)
		commit(t, client, 100)
		permitted, err := downscalePermitted(t.Context())
		require.NoError(t, err)
		require.True(t, permitted)
	})

	t.Run("allows downscale when the partition does not exist in the topic", func(t *testing.T) {
		client, downscalePermitted := newTestPartition(t, testPartition+1)
		produce(t, client, 5)
		permitted, err := downscalePermitted(t.Context())
		require.NoError(t, err)
		require.True(t, permitted)
	})

	t.Run("returns an error when the offsets cannot be read", func(t *testing.T) {
		_, downscalePermitted := newTestPartition(t, testPartition)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		permitted, err := downscalePermitted(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.False(t, permitted)
	})
}
