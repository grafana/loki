package partition

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
)

func TestPartitionCommitter(t *testing.T) {
	// Set a maximum timeout on the test.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Set up a test cluster.
	topic := "test-topic"
	_, kafkaCfg := testkafka.CreateCluster(t, 3, topic)
	consumerGroup := "test-consumer-group"
	kafkaCfg.ConsumerGroup = consumerGroup

	// Create a test client and admin client.
	client, err := client.NewReaderClient("test-client", kafkaCfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	admClient := kadm.NewClient(client)
	defer admClient.Close()

	// Set up a committer for partition 1 and another for partition 2.
	offsetManager, err := NewKafkaOffsetManager(
		kafkaCfg,
		consumerGroup,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	partition1 := int32(1)
	committer1 := newCommitter(
		offsetManager,
		partition1,
		kafkaCfg.ConsumerGroupOffsetCommitInterval,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)
	partition2 := int32(2)
	committer2 := newCommitter(
		offsetManager,
		partition2,
		kafkaCfg.ConsumerGroupOffsetCommitInterval,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// Should be able to commit an offset for partition 1.
	offset1 := int64(100)
	require.NoError(t, committer1.Commit(ctx, offset1))
	require.Equal(t, float64(1), testutil.ToFloat64(committer1.commitRequestsTotal))
	require.Equal(t, float64(0), testutil.ToFloat64(committer1.commitFailuresTotal))
	require.Equal(t, float64(offset1), testutil.ToFloat64(committer1.lastCommittedOffset))

	// Check that the offset was committed for partition 1.
	offsets, err := admClient.FetchOffsets(ctx, consumerGroup)
	require.NoError(t, err)
	committedOffset, ok := offsets.Lookup(topic, partition1)
	require.True(t, ok)
	assert.Equal(t, offset1, committedOffset.At)

	// It should not have been committed for partition 2.
	committedOffset, ok = offsets.Lookup(topic, partition2)
	require.False(t, ok)

	// Neither should it have been committed for other consumer groups.
	offsets, err = admClient.FetchOffsets(ctx, "test-consumer-group-2")
	require.NoError(t, err)
	committedOffset, ok = offsets.Lookup(topic, partition1)
	require.False(t, ok)

	// Should be able to commit a new offset for partition 1.
	offset2 := int64(200)
	require.NoError(t, committer1.Commit(ctx, offset2))
	assert.Equal(t, float64(2), testutil.ToFloat64(committer1.commitRequestsTotal))
	assert.Equal(t, float64(0), testutil.ToFloat64(committer1.commitFailuresTotal))
	assert.Equal(t, float64(offset2), testutil.ToFloat64(committer1.lastCommittedOffset))

	// Check that the offset was committed.
	offsets, err = admClient.FetchOffsets(ctx, consumerGroup)
	require.NoError(t, err)
	committedOffset, ok = offsets.Lookup(topic, partition1)
	require.True(t, ok)
	assert.Equal(t, offset2, committedOffset.At)

	// Should be able to commit an offset for partition 2.
	offset3 := int64(300)
	require.NoError(t, committer2.Commit(ctx, offset3))
	assert.Equal(t, float64(1), testutil.ToFloat64(committer2.commitRequestsTotal))
	assert.Equal(t, float64(0), testutil.ToFloat64(committer2.commitFailuresTotal))
	assert.Equal(t, float64(offset3), testutil.ToFloat64(committer2.lastCommittedOffset))

	// Check that the offset was committed.
	offsets, err = admClient.FetchOffsets(ctx, consumerGroup)
	require.NoError(t, err)
	committedOffset, ok = offsets.Lookup(topic, partition2)
	require.True(t, ok)
	assert.Equal(t, offset3, committedOffset.At)
}
