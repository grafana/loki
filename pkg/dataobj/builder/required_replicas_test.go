package builder

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/grafana/loki/v3/pkg/kafkav2"
)

func TestRequiredReplicasReporter(t *testing.T) {
	const (
		testTopic      = "test-topic"
		testPartitions = 10
	)

	newTestReporter := func(t *testing.T) (*kfake.Cluster, *kgo.Client, *requiredReplicasReporter) {
		t.Helper()
		cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(testPartitions, testTopic))
		require.NoError(t, err)
		t.Cleanup(cluster.Close)
		client, err := kgo.NewClient(
			kgo.SeedBrokers(cluster.ListenAddrs()[0]),
			kgo.RecordPartitioner(kgo.ManualPartitioner()),
		)
		require.NoError(t, err)
		t.Cleanup(client.Close)
		consumerGroup, err := newConsumerGroupFunc("dataobj-builder-0")
		require.NoError(t, err)
		r := newRequiredReplicasReporter(
			kafkav2.NewTopicOffsetReader(client, testTopic),
			consumerGroup,
			10*time.Second,
			log.NewNopLogger(),
			prometheus.NewRegistry(),
		)
		return cluster, client, r
	}

	produce := func(t *testing.T, client *kgo.Client, partition int32, n int) {
		t.Helper()
		for range n {
			res := client.ProduceSync(t.Context(), &kgo.Record{
				Topic:     testTopic,
				Partition: partition,
				Value:     []byte("foo"),
				Timestamp: time.Now(),
			})
			require.NoError(t, res.FirstErr())
		}
	}

	// commit commits offset for the builder of partition. A builder commits the
	// offset of the last record it consumed.
	commit := func(t *testing.T, client *kgo.Client, r *requiredReplicasReporter, partition int32, offset int64) {
		t.Helper()
		committer := kafkav2.NewCommitter(kadm.NewClient(client))
		require.NoError(t, committer.Commit(t.Context(), testTopic, partition, r.consumerGroup(partition), offset))
	}

	// deleteRecordsBefore moves the start offset of partition to offset, as
	// retention does.
	deleteRecordsBefore := func(t *testing.T, client *kgo.Client, partition int32, offset int64) {
		t.Helper()
		offsets := kadm.Offsets{}
		offsets.AddOffset(testTopic, partition, offset, -1)
		res, err := kadm.NewClient(client).DeleteRecords(t.Context(), offsets)
		require.NoError(t, err)
		require.NoError(t, res.Error())
	}

	failNextOffsetFetch := func(cluster *kfake.Cluster) {
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

	requireReplicas := func(t *testing.T, r *requiredReplicasReporter, expected float64) {
		t.Helper()
		require.Equal(t, 1, testutil.CollectAndCount(r.requiredReplicas))
		require.Equal(t, expected, testutil.ToFloat64(r.requiredReplicas))
	}

	t.Run("reports zero when no partition has records", func(t *testing.T) {
		_, _, r := newTestReporter(t)
		require.NoError(t, r.refresh(t.Context()))
		requireReplicas(t, r, 0)
	})

	t.Run("reports zero when every builder committed its last record", func(t *testing.T) {
		_, client, r := newTestReporter(t)
		produce(t, client, 0, 3)
		produce(t, client, 4, 5)
		commit(t, client, r, 0, 2)
		commit(t, client, r, 4, 4)
		require.NoError(t, r.refresh(t.Context()))
		requireReplicas(t, r, 0)
	})

	t.Run("reports the partition plus one when its builder has unread records", func(t *testing.T) {
		_, client, r := newTestReporter(t)
		produce(t, client, 3, 5)
		commit(t, client, r, 3, 3)
		require.NoError(t, r.refresh(t.Context()))
		requireReplicas(t, r, 4)
	})

	t.Run("reports the partition plus one when its builder never committed", func(t *testing.T) {
		_, client, r := newTestReporter(t)
		produce(t, client, 5, 1)
		require.NoError(t, r.refresh(t.Context()))
		requireReplicas(t, r, 6)
	})

	t.Run("reports zero when retention deleted every record of a partition its builder never consumed", func(t *testing.T) {
		_, client, r := newTestReporter(t)
		produce(t, client, 5, 3)
		deleteRecordsBefore(t, client, 5, 3)
		require.NoError(t, r.refresh(t.Context()))
		requireReplicas(t, r, 0)
	})

	t.Run("reports the partition plus one when records remain after a start offset above the committed offset", func(t *testing.T) {
		_, client, r := newTestReporter(t)
		produce(t, client, 2, 5)
		commit(t, client, r, 2, 0)
		deleteRecordsBefore(t, client, 2, 3)
		require.NoError(t, r.refresh(t.Context()))
		requireReplicas(t, r, 3)
	})

	t.Run("reports zero when retention deleted every record after the committed offset", func(t *testing.T) {
		_, client, r := newTestReporter(t)
		produce(t, client, 2, 5)
		commit(t, client, r, 2, 0)
		deleteRecordsBefore(t, client, 2, 5)
		require.NoError(t, r.refresh(t.Context()))
		requireReplicas(t, r, 0)
	})

	t.Run("reports the highest partition with unread records plus one", func(t *testing.T) {
		_, client, r := newTestReporter(t)
		produce(t, client, 2, 1)
		produce(t, client, 7, 1)
		produce(t, client, 9, 1)
		commit(t, client, r, 9, 0)
		require.NoError(t, r.refresh(t.Context()))
		requireReplicas(t, r, 8)
	})

	t.Run("keeps the last value and counts the failure when a refresh fails", func(t *testing.T) {
		cluster, client, r := newTestReporter(t)
		produce(t, client, 3, 1)
		require.NoError(t, r.refresh(t.Context()))
		requireReplicas(t, r, 4)

		produce(t, client, 7, 1)
		failNextOffsetFetch(cluster)
		require.ErrorIs(t, r.refresh(t.Context()), kerr.GroupAuthorizationFailed)
		requireReplicas(t, r, 4)
		require.Equal(t, float64(1), testutil.ToFloat64(r.refreshFailures))

		require.NoError(t, r.refresh(t.Context()))
		requireReplicas(t, r, 8)
	})

	t.Run("exports no value until the first refresh succeeds", func(t *testing.T) {
		cluster, client, r := newTestReporter(t)
		produce(t, client, 3, 1)
		failNextOffsetFetch(cluster)
		require.Error(t, r.refresh(t.Context()))
		require.Equal(t, 0, testutil.CollectAndCount(r.requiredReplicas))
		require.Equal(t, float64(1), testutil.ToFloat64(r.refreshFailures))
	})

	t.Run("refreshes when the service starts", func(t *testing.T) {
		_, client, r := newTestReporter(t)
		produce(t, client, 1, 1)
		ctx := testContext(t)
		require.NoError(t, services.StartAndAwaitRunning(ctx, r))
		t.Cleanup(func() { require.NoError(t, services.StopAndAwaitTerminated(context.Background(), r)) })
		require.Eventually(t, func() bool {
			return testutil.CollectAndCount(r.requiredReplicas) == 1
		}, 5*time.Second, 10*time.Millisecond)
		requireReplicas(t, r, 2)
	})
}

func TestNewConsumerGroupFunc(t *testing.T) {
	t.Run("returns the instance name with the partition ID replaced", func(t *testing.T) {
		consumerGroup, err := newConsumerGroupFunc("dataobj-builder-0")
		require.NoError(t, err)
		require.Equal(t, "dataobj-builder-0", consumerGroup(0))
		require.Equal(t, "dataobj-builder-7", consumerGroup(7))
		require.Equal(t, "dataobj-builder-123", consumerGroup(123))
	})

	t.Run("keeps every part of the instance name before the partition ID", func(t *testing.T) {
		consumerGroup, err := newConsumerGroupFunc("dataobj-builder-zone-a-12")
		require.NoError(t, err)
		require.Equal(t, "dataobj-builder-zone-a-3", consumerGroup(3))
	})

	t.Run("returns an error when the instance name has no partition ID", func(t *testing.T) {
		_, err := newConsumerGroupFunc("dataobj-builder")
		require.Error(t, err)
	})

	t.Run("returns an error when the partition ID has leading zeros", func(t *testing.T) {
		_, err := newConsumerGroupFunc("dataobj-builder-007")
		require.Error(t, err)
	})

	t.Run("returns an error for a local hostname", func(t *testing.T) {
		_, err := newConsumerGroupFunc("laptop.local")
		require.Error(t, err)
	})
}
