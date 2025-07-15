// SPDX-License-Identifier: AGPL-3.0-only

package partition

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
)

const (
	testTopic = "test"
	testGroup = "testgroup"
)

func TestKafkaGetGroupLag(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	t.Cleanup(func() { cancel(errors.New("test done")) })

	_, addr := testkafka.CreateClusterWithoutCustomConsumerGroupsSupport(t, 3, testTopic)
	kafkaClient := mustKafkaClient(t, addr)
	admClient := kadm.NewClient(kafkaClient)

	const numRecords = 5

	var producedRecords []kgo.Record
	kafkaTime := time.Now().Add(-12 * time.Hour)
	for i := int64(0); i < numRecords; i++ {
		kafkaTime = kafkaTime.Add(time.Minute)

		// Produce and keep records to partition 0.
		res := produceRecords(ctx, t, kafkaClient, kafkaTime, "1", testTopic, 0, []byte(`test value`))
		rec, err := res.First()
		require.NoError(t, err)
		require.NotNil(t, rec)

		producedRecords = append(producedRecords, *rec)

		// Produce same records to partition 1 (this partition won't have any commits).
		produceRecords(ctx, t, kafkaClient, kafkaTime, "1", testTopic, 1, []byte(`test value`))
	}
	require.Len(t, producedRecords, numRecords)

	// Commit last produced record from partition 0.
	rec := producedRecords[len(producedRecords)-1]
	offsets := make(kadm.Offsets)
	offsets.Add(kadm.Offset{
		Topic:       rec.Topic,
		Partition:   rec.Partition,
		At:          rec.Offset + 1,
		LeaderEpoch: rec.LeaderEpoch,
	})
	err := admClient.CommitAllOffsets(ctx, testGroup, offsets)
	require.NoError(t, err)

	// Truncate partition 1 after second to last record to emulate the retention
	// Note Kafka sets partition's start offset to the requested offset. Any records within the segment before the requested offset can no longer be read.
	// Note the difference between DeleteRecords and DeleteOffsets in kadm docs.
	deleteRecOffsets := make(kadm.Offsets)
	deleteRecOffsets.Add(kadm.Offset{
		Topic:     testTopic,
		Partition: 1,
		At:        numRecords - 2,
	})
	_, err = admClient.DeleteRecords(ctx, deleteRecOffsets)
	require.NoError(t, err)

	getTopicPartitionLag := func(t *testing.T, lag kadm.GroupLag, topic string, part int32) int64 {
		l, ok := lag.Lookup(topic, part)
		require.True(t, ok)
		return l.Lag
	}

	t.Run("fallbackOffset=milliseconds", func(t *testing.T) {
		// get the timestamp of the last produced record
		rec := producedRecords[len(producedRecords)-1]
		fallbackOffset := rec.Timestamp.Add(-time.Millisecond).UnixMilli()
		groupLag, err := GetGroupLag(ctx, admClient, testTopic, testGroup, fallbackOffset)
		require.NoError(t, err)

		require.EqualValues(t, 0, getTopicPartitionLag(t, groupLag, testTopic, 0), "partition 0 must have no lag")
		require.EqualValues(t, 1, getTopicPartitionLag(t, groupLag, testTopic, 1), "partition 1 must fall back to known record and get its lag from there")
		require.EqualValues(t, 0, getTopicPartitionLag(t, groupLag, testTopic, 2), "partition 2 has no data and must have no lag")
	})

	t.Run("fallbackOffset=before-earliest", func(t *testing.T) {
		// get the timestamp of third to last produced record (record before earliest in partition 1)
		rec := producedRecords[len(producedRecords)-3]
		fallbackOffset := rec.Timestamp.Add(-time.Millisecond).UnixMilli()
		groupLag, err := GetGroupLag(ctx, admClient, testTopic, testGroup, fallbackOffset)
		require.NoError(t, err)

		require.EqualValues(t, 0, getTopicPartitionLag(t, groupLag, testTopic, 0), "partition 0 must have no lag")
		require.EqualValues(t, 2, getTopicPartitionLag(t, groupLag, testTopic, 1), "partition 1 must fall back to earliest and get its lag from there")
		require.EqualValues(t, 0, getTopicPartitionLag(t, groupLag, testTopic, 2), "partition 2 has no data and must have no lag")
	})

	t.Run("fallbackOffset=0", func(t *testing.T) {
		groupLag, err := GetGroupLag(ctx, admClient, testTopic, testGroup, 0)
		require.NoError(t, err)

		require.EqualValues(t, 0, getTopicPartitionLag(t, groupLag, testTopic, 0), "partition 0 must have no lag")
		require.EqualValues(t, 2, getTopicPartitionLag(t, groupLag, testTopic, 1), "partition 1 must fall back to the earliest and get its lag from there")
		require.EqualValues(t, 0, getTopicPartitionLag(t, groupLag, testTopic, 2), "partition 2 has no data and must have no lag")
	})

	t.Run("group=unknown", func(t *testing.T) {
		groupLag, err := GetGroupLag(ctx, admClient, testTopic, "unknown", 0)
		require.NoError(t, err)

		// This group doesn't have any commits, so it must calc its lag from the fallback.
		require.EqualValues(t, numRecords, getTopicPartitionLag(t, groupLag, testTopic, 0))
		require.EqualValues(t, 2, getTopicPartitionLag(t, groupLag, testTopic, 1), "partition 1 must fall back to the earliest and get its lag from there")
		require.EqualValues(t, 0, getTopicPartitionLag(t, groupLag, testTopic, 2), "partition 2 has no data and must have no lag")
	})
}

func mustKafkaClient(t *testing.T, addrs ...string) *kgo.Client {
	writeClient, err := kgo.NewClient(
		kgo.SeedBrokers(addrs...),
		kgo.AllowAutoTopicCreation(),
		// We will choose the partition of each record.
		kgo.RecordPartitioner(kgo.ManualPartitioner()),
	)
	require.NoError(t, err)
	t.Cleanup(writeClient.Close)
	return writeClient
}

func produceRecords(
	ctx context.Context,
	t *testing.T,
	kafkaClient *kgo.Client,
	ts time.Time,
	userID string,
	topic string,
	part int32,
	val []byte,
) kgo.ProduceResults {
	rec := &kgo.Record{
		Timestamp: ts,
		Key:       []byte(userID),
		Value:     val,
		Topic:     topic,
		Partition: part, // samples in this batch are split between N partitions
	}
	produceResult := kafkaClient.ProduceSync(ctx, rec)
	require.NoError(t, produceResult.FirstErr())
	return produceResult
}

func commitOffset(ctx context.Context, t *testing.T, kafkaClient *kgo.Client, group string, offset kadm.Offset) {
	offsets := make(kadm.Offsets)
	offsets.Add(offset)
	err := kadm.NewClient(kafkaClient).CommitAllOffsets(ctx, group, offsets)
	require.NoError(t, err)
}
