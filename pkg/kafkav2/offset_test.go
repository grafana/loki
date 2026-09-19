package kafkav2

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

// This test asserts that the correct offset is committed for the intended
// topic, partition and consumer group, and that no offsets are incorrectly
// committed for any other topics, partitions or consumer groups.
func TestCommitter_Commit(t *testing.T) {
	const (
		testTopic         = "test-topic"
		testConsumerGroup = "test-consumer-group"
	)
	ctx := t.Context()
	// Create a fake cluster for the test.
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, testTopic))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)
	client := mustKafkaClient(t, cluster.ListenAddrs()[0])
	adm := kadm.NewClient(client)

	// There should be no committed offsets.
	offsets, err := adm.FetchOffsets(ctx, testConsumerGroup)
	require.Equal(t, kerr.GroupIDNotFound, err)
	require.Nil(t, offsets)

	// Commit an offset, it should succeed.
	m := NewCommitter(adm)
	require.NoError(t, m.Commit(ctx, testTopic, 0, testConsumerGroup, 100))

	// Check that the offset was committed.
	offsets, err = adm.FetchOffsets(ctx, testConsumerGroup)
	require.NoError(t, err)
	topicOffsets, ok := offsets[testTopic]
	require.True(t, ok)
	require.Len(t, topicOffsets, 1)
	offset := topicOffsets[0]
	require.Equal(t, testTopic, offset.Topic)
	require.Equal(t, int32(0), offset.Partition)
	require.Equal(t, int64(100), offset.At)

	// No other consumer groups should exist. If they do, we have somehow
	// committed offsets for the wrong consumer group.
	groups, err := adm.ListGroups(ctx)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Contains(t, groups, testConsumerGroup)
}

func TestGroupCommitter_Commit(t *testing.T) {
	const (
		testTopic         = "test-topic"
		testConsumerGroup = "test-consumer-group"
	)
	ctx := t.Context()
	// Create a fake cluster for the test.
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, testTopic))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)
	client := mustKafkaClient(t, cluster.ListenAddrs()[0])
	adm := kadm.NewClient(client)
	m := NewGroupCommitter(adm, testTopic, testConsumerGroup)

	// Commit an offset, it should succeed.
	require.NoError(t, m.Commit(ctx, 0, 100))

	// Check that the offset was committed.
	offsets, err := adm.FetchOffsets(ctx, testConsumerGroup)
	require.NoError(t, err)
	topicOffsets, ok := offsets[testTopic]
	require.True(t, ok)
	require.Len(t, topicOffsets, 1)
	offset := topicOffsets[0]
	require.Equal(t, testTopic, offset.Topic)
	require.Equal(t, int32(0), offset.Partition)
	require.Equal(t, int64(100), offset.At)

	// No other consumer groups should exist. If they do, we have somehow
	// committed offsets for the wrong consumer group.
	groups, err := adm.ListGroups(ctx)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Contains(t, groups, testConsumerGroup)
}

func TestOffsetReader_LastCommittedOffset(t *testing.T) {
	const (
		testTopic         = "test-topic"
		testConsumerGroup = "test-consumer-group"
	)
	ctx := t.Context()
	// Create a fake cluster for the test.
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, testTopic))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)
	client := mustKafkaClient(t, cluster.ListenAddrs()[0])
	adm := kadm.NewClient(client)
	m := NewOffsetReader(client, testTopic, testConsumerGroup, log.NewNopLogger())

	// There should be no committed offsets.
	offset, err := m.LastCommittedOffset(ctx, 0)
	require.NoError(t, err)
	// -2 is a special offset which means start offset.
	require.Equal(t, int64(-2), offset)

	// Commit an offset, it should be returned in the next call.
	toCommit := kadm.Offsets{}
	toCommit.AddOffset(testTopic, 0, 100, -1)
	_, err = adm.CommitOffsets(ctx, testConsumerGroup, toCommit)
	require.NoError(t, err)
	offset, err = m.LastCommittedOffset(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, int64(100), offset)
}

func TestOffsetReader_EndOffset(t *testing.T) {
	const (
		testTopic         = "test-topic"
		testConsumerGroup = "test-consumer-group"
	)
	ctx := t.Context()
	// Create a fake cluster for the test.
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, testTopic))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)
	client := mustKafkaClient(t, cluster.ListenAddrs()[0])
	m := NewOffsetReader(client, testTopic, testConsumerGroup, log.NewNopLogger())

	// The offset should be 0 as no records have been produced.
	offset, err := m.EndOffset(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, int64(0), offset)

	// Produce a record, the end offset should be 1.
	res := client.ProduceSync(ctx, &kgo.Record{
		Topic:     testTopic,
		Key:       []byte("foo"),
		Value:     []byte("bar"),
		Timestamp: time.Now(),
	})
	require.NoError(t, res.FirstErr())
	offset, err = m.EndOffset(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), offset)

	// Produce another record, the end offset should be 2.
	res = client.ProduceSync(ctx, &kgo.Record{
		Topic:     testTopic,
		Key:       []byte("baz"),
		Value:     []byte("qux"),
		Timestamp: time.Now(),
	})
	require.NoError(t, res.FirstErr())
	offset, err = m.EndOffset(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), offset)
}
