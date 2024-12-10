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
	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
)

func TestPartitionCommitter(t *testing.T) {
	// Create a test Kafka cluster
	numPartitions := int32(3)
	topicName := "test-topic"
	_, kafkaCfg := testkafka.CreateCluster(t, numPartitions, topicName)
	kafkaCfg.ConsumerGroup = "test-group"

	client, err := client.NewReaderClient(kafkaCfg, kprom.NewMetrics("foo"), log.NewNopLogger())
	require.NoError(t, err)

	// Create a Kafka admin client
	admClient := kadm.NewClient(client)
	defer admClient.Close()

	// Create a partition committer
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	partitionID := int32(1)
	reader := newKafkaOffsetManager(
		client,
		kafkaCfg,
		"fake-instance-id",
		logger,
	)
	committer := newCommitter(reader, partitionID, kafkaCfg.ConsumerGroupOffsetCommitInterval, logger, reg)

	// Test committing an offset
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	testOffset := int64(100)
	err = committer.Commit(ctx, testOffset)
	require.NoError(t, err)

	// Verify metrics
	assert.Equal(t, float64(1), testutil.ToFloat64(committer.commitRequestsTotal))
	assert.Equal(t, float64(0), testutil.ToFloat64(committer.commitFailuresTotal))
	assert.Equal(t, float64(testOffset), testutil.ToFloat64(committer.lastCommittedOffset))

	// Verify committed offset
	offsets, err := admClient.FetchOffsets(context.Background(), reader.ConsumerGroup())
	require.NoError(t, err)
	committedOffset, ok := offsets.Lookup(topicName, partitionID)
	require.True(t, ok)
	assert.Equal(t, testOffset, committedOffset.At)

	// Test committing a new offset
	newTestOffset := int64(200)
	err = committer.Commit(ctx, newTestOffset)
	require.NoError(t, err)

	// Verify updated metrics
	assert.Equal(t, float64(2), testutil.ToFloat64(committer.commitRequestsTotal))
	assert.Equal(t, float64(0), testutil.ToFloat64(committer.commitFailuresTotal))
	assert.Equal(t, float64(newTestOffset), testutil.ToFloat64(committer.lastCommittedOffset))

	// Verify updated committed offset
	offsets, err = admClient.FetchOffsets(context.Background(), reader.ConsumerGroup())
	require.NoError(t, err)
	committedOffset, ok = offsets.Lookup(topicName, partitionID)
	require.True(t, ok)
	assert.Equal(t, newTestOffset, committedOffset.At)
}
