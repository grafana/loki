package kafkav2

import (
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
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

	t.Run("returns the start offset when the group never committed", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 1)
		offset, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).LastCommittedOffset(t.Context(), testConsumerGroup, 0)
		require.NoError(t, err)
		require.Equal(t, OffsetStart, offset)
	})

	t.Run("returns the committed offset", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 1)
		require.NoError(t, NewCommitter(kadm.NewClient(client)).Commit(t.Context(), testTopic, 0, testConsumerGroup, 100))
		offset, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).LastCommittedOffset(t.Context(), testConsumerGroup, 0)
		require.NoError(t, err)
		require.Equal(t, int64(100), offset)
	})

	t.Run("requests only the given partition of the group", func(t *testing.T) {
		cluster, client := newTestCluster(t, testTopic, 2)
		requested := recordOffsetFetches(cluster)
		_, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).LastCommittedOffset(t.Context(), testConsumerGroup, 1)
		require.NoError(t, err)
		require.Equal(t, map[string][]int32{testConsumerGroup: {1}}, requested())
	})

	t.Run("returns an error when the broker denies access to the group", func(t *testing.T) {
		cluster, client := newTestCluster(t, testTopic, 1)
		denyNextOffsetFetch(cluster)
		_, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).LastCommittedOffset(t.Context(), testConsumerGroup, 0)
		require.ErrorIs(t, err, kerr.GroupAuthorizationFailed)
	})
}

func TestOffsetReader_ResumeOffset(t *testing.T) {
	const (
		testTopic         = "test-topic"
		testConsumerGroup = "test-consumer-group"
	)

	t.Run("returns the start offset when the group never committed", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 1)
		offset, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).ResumeOffset(t.Context(), testConsumerGroup, 0)
		require.NoError(t, err)
		require.Equal(t, OffsetStart, offset)
	})

	t.Run("returns the offset after the committed offset", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 1)
		require.NoError(t, NewCommitter(kadm.NewClient(client)).Commit(t.Context(), testTopic, 0, testConsumerGroup, 100))
		offset, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).ResumeOffset(t.Context(), testConsumerGroup, 0)
		require.NoError(t, err)
		require.Equal(t, int64(101), offset)
	})

	t.Run("returns an error when the broker denies access to the group", func(t *testing.T) {
		cluster, client := newTestCluster(t, testTopic, 1)
		denyNextOffsetFetch(cluster)
		_, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).ResumeOffset(t.Context(), testConsumerGroup, 0)
		require.ErrorIs(t, err, kerr.GroupAuthorizationFailed)
	})
}

func TestOffsetReader_EndOffset(t *testing.T) {
	const testTopic = "test-topic"
	ctx := t.Context()
	// Create a fake cluster for the test.
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, testTopic))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)
	client := mustKafkaClient(t, cluster.ListenAddrs()[0])
	m := NewOffsetReader(client, testTopic, log.NewNopLogger())

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

func TestOffsetReader_StartOffset(t *testing.T) {
	const testTopic = "test-topic"

	newTestReader := func(t *testing.T) (*kgo.Client, *OffsetReader) {
		t.Helper()
		cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, testTopic))
		require.NoError(t, err)
		t.Cleanup(cluster.Close)
		client := mustKafkaClient(t, cluster.ListenAddrs()[0])
		return client, NewOffsetReader(client, testTopic, log.NewNopLogger())
	}

	produce := func(t *testing.T, client *kgo.Client, n int) {
		t.Helper()
		for range n {
			res := client.ProduceSync(t.Context(), &kgo.Record{
				Topic:     testTopic,
				Value:     []byte("foo"),
				Timestamp: time.Now(),
			})
			require.NoError(t, res.FirstErr())
		}
	}

	deleteRecordsBefore := func(t *testing.T, client *kgo.Client, offset int64) {
		t.Helper()
		offsets := kadm.Offsets{}
		offsets.AddOffset(testTopic, 0, offset, -1)
		res, err := kadm.NewClient(client).DeleteRecords(t.Context(), offsets)
		require.NoError(t, err)
		require.NoError(t, res.Error())
	}

	t.Run("returns zero when no records were produced", func(t *testing.T) {
		_, r := newTestReader(t)
		offset, err := r.StartOffset(t.Context(), 0)
		require.NoError(t, err)
		require.Equal(t, int64(0), offset)
	})

	t.Run("returns zero when no records were deleted", func(t *testing.T) {
		client, r := newTestReader(t)
		produce(t, client, 3)
		offset, err := r.StartOffset(t.Context(), 0)
		require.NoError(t, err)
		require.Equal(t, int64(0), offset)
	})

	t.Run("returns the offset after the deleted records", func(t *testing.T) {
		client, r := newTestReader(t)
		produce(t, client, 3)
		deleteRecordsBefore(t, client, 2)
		offset, err := r.StartOffset(t.Context(), 0)
		require.NoError(t, err)
		require.Equal(t, int64(2), offset)
	})

	t.Run("returns the end offset when every record was deleted", func(t *testing.T) {
		client, r := newTestReader(t)
		produce(t, client, 3)
		deleteRecordsBefore(t, client, 3)
		start, err := r.StartOffset(t.Context(), 0)
		require.NoError(t, err)
		end, err := r.EndOffset(t.Context(), 0)
		require.NoError(t, err)
		require.Equal(t, int64(3), start)
		require.Equal(t, end, start)
	})

	t.Run("returns an error when the partition does not exist", func(t *testing.T) {
		_, r := newTestReader(t)
		_, err := r.StartOffset(t.Context(), 1)
		require.ErrorIs(t, err, kerr.UnknownTopicOrPartition)
	})
}

func TestOffsetReader_StartOffsets(t *testing.T) {
	const testTopic = "test-topic"

	t.Run("returns zero for each partition when no records were deleted", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 3)
		produceTo(t, client, testTopic, 1, 3)
		offsets, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).StartOffsets(t.Context())
		require.NoError(t, err)
		require.Equal(t, map[int32]int64{0: 0, 1: 0, 2: 0}, offsets)
	})

	t.Run("returns the offset after the deleted records of each partition", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 3)
		produceTo(t, client, testTopic, 1, 3)
		produceTo(t, client, testTopic, 2, 5)
		deleteRecords(t, client, testTopic, 1, 2)
		deleteRecords(t, client, testTopic, 2, 5)
		offsets, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).StartOffsets(t.Context())
		require.NoError(t, err)
		require.Equal(t, map[int32]int64{0: 0, 1: 2, 2: 5}, offsets)
	})

	t.Run("returns an error when the topic does not exist", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 1)
		_, err := NewOffsetReader(client, "other-topic", log.NewNopLogger()).StartOffsets(t.Context())
		require.ErrorIs(t, err, kerr.UnknownTopicOrPartition)
	})
}

func TestOffsetReader_EndOffsets(t *testing.T) {
	const testTopic = "test-topic"

	t.Run("returns zero for each partition when no records were produced", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 3)
		offsets, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).EndOffsets(t.Context())
		require.NoError(t, err)
		require.Equal(t, map[int32]int64{0: 0, 1: 0, 2: 0}, offsets)
	})

	t.Run("returns the offset after the last record of each partition", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 3)
		produceTo(t, client, testTopic, 0, 1)
		produceTo(t, client, testTopic, 2, 4)
		offsets, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).EndOffsets(t.Context())
		require.NoError(t, err)
		require.Equal(t, map[int32]int64{0: 1, 1: 0, 2: 4}, offsets)
	})

	t.Run("returns an error when the topic does not exist", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 1)
		_, err := NewOffsetReader(client, "other-topic", log.NewNopLogger()).EndOffsets(t.Context())
		require.ErrorIs(t, err, kerr.UnknownTopicOrPartition)
	})
}

func TestOffsetReader_LastCommittedOffsets(t *testing.T) {
	const testTopic = "test-topic"

	commit := func(t *testing.T, client *kgo.Client, group string, partition int32, offset int64) {
		t.Helper()
		require.NoError(t, NewCommitter(kadm.NewClient(client)).Commit(t.Context(), testTopic, partition, group, offset))
	}

	t.Run("returns an empty result when no groups are given", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 1)
		offsets, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).LastCommittedOffsets(t.Context(), nil)
		require.NoError(t, err)
		require.Empty(t, offsets)
	})

	t.Run("returns the committed offset of each group", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 3)
		commit(t, client, "group-0", 0, 4)
		commit(t, client, "group-2", 2, 0)
		offsets, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).LastCommittedOffsets(t.Context(), map[int32]string{
			0: "group-0",
			2: "group-2",
		})
		require.NoError(t, err)
		require.Equal(t, map[int32]int64{0: 4, 2: 0}, offsets)
	})

	t.Run("returns a negative offset when the group never committed", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 1)
		offsets, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).LastCommittedOffsets(t.Context(), map[int32]string{0: "group-0"})
		require.NoError(t, err)
		require.Len(t, offsets, 1)
		require.Negative(t, offsets[0])
	})

	t.Run("returns a negative offset when the group committed only another partition", func(t *testing.T) {
		_, client := newTestCluster(t, testTopic, 2)
		commit(t, client, "group-0", 1, 4)
		offsets, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).LastCommittedOffsets(t.Context(), map[int32]string{0: "group-0"})
		require.NoError(t, err)
		require.Len(t, offsets, 1)
		require.Negative(t, offsets[0])
	})

	t.Run("requests only the partition of each group", func(t *testing.T) {
		cluster, client := newTestCluster(t, testTopic, 3)
		requested := recordOffsetFetches(cluster)
		_, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).LastCommittedOffsets(t.Context(), map[int32]string{
			0: "group-0",
			2: "group-2",
		})
		require.NoError(t, err)
		require.Equal(t, map[string][]int32{"group-0": {0}, "group-2": {2}}, requested())
	})

	t.Run("returns an error when the broker denies access to a group", func(t *testing.T) {
		cluster, client := newTestCluster(t, testTopic, 1)
		denyNextOffsetFetch(cluster)
		_, err := NewOffsetReader(client, testTopic, log.NewNopLogger()).LastCommittedOffsets(t.Context(), map[int32]string{0: "group-0"})
		require.ErrorIs(t, err, kerr.GroupAuthorizationFailed)
	})
}

// newTestCluster returns a fake cluster with one topic of the given number of
// partitions, and a client for it.
func newTestCluster(t *testing.T, topic string, partitions int32) (*kfake.Cluster, *kgo.Client) {
	t.Helper()
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(partitions, topic))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)
	return cluster, mustKafkaClient(t, cluster.ListenAddrs()[0])
}

func produceTo(t *testing.T, client *kgo.Client, topic string, partition int32, n int) {
	t.Helper()
	for range n {
		res := client.ProduceSync(t.Context(), &kgo.Record{
			Topic:     topic,
			Partition: partition,
			Value:     []byte("foo"),
			Timestamp: time.Now(),
		})
		require.NoError(t, res.FirstErr())
	}
}

// deleteRecords moves the start offset of the partition to offset, as
// retention does.
func deleteRecords(t *testing.T, client *kgo.Client, topic string, partition int32, offset int64) {
	t.Helper()
	offsets := kadm.Offsets{}
	offsets.AddOffset(topic, partition, offset, -1)
	res, err := kadm.NewClient(client).DeleteRecords(t.Context(), offsets)
	require.NoError(t, err)
	require.NoError(t, res.Error())
}

// denyNextOffsetFetch makes the cluster answer the next OffsetFetch request
// with GROUP_AUTHORIZATION_FAILED for every group in it.
func denyNextOffsetFetch(cluster *kfake.Cluster) {
	cluster.ControlKey(kmsg.OffsetFetch.Int16(), func(kreq kmsg.Request) (kmsg.Response, error, bool) {
		req := kreq.(*kmsg.OffsetFetchRequest)
		resp := req.ResponseKind().(*kmsg.OffsetFetchResponse)
		for _, g := range req.Groups {
			rg := kmsg.NewOffsetFetchResponseGroup()
			rg.Group = g.Group
			rg.ErrorCode = kerr.GroupAuthorizationFailed.Code
			resp.Groups = append(resp.Groups, rg)
		}
		return resp, nil, true
	})
}

// recordOffsetFetches records the partitions that OffsetFetch requests name,
// keyed by group. A group that asks for all of its committed offsets, with
// null topics, maps to nil. The cluster still handles every request.
func recordOffsetFetches(cluster *kfake.Cluster) func() map[string][]int32 {
	var (
		mu        sync.Mutex
		requested = map[string][]int32{}
	)
	cluster.ControlKey(kmsg.OffsetFetch.Int16(), func(kreq kmsg.Request) (kmsg.Response, error, bool) {
		cluster.KeepControl()
		mu.Lock()
		defer mu.Unlock()
		for _, g := range kreq.(*kmsg.OffsetFetchRequest).Groups {
			if g.Topics == nil {
				requested[g.Group] = nil
			}
			for _, topic := range g.Topics {
				requested[g.Group] = append(requested[g.Group], topic.Partitions...)
			}
		}
		return nil, nil, false
	})
	return func() map[string][]int32 {
		mu.Lock()
		defer mu.Unlock()
		return requested
	}
}
