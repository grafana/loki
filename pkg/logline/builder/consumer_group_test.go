package builder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// These tests cover the consumer-group machinery.
//
//   - onPartitionsAssigned / onPartitionsRevoked must keep ownedPartitions in
//     sync with the coordinator and clean up per-partition Prometheus labels
//     on revoke (otherwise dashboards keep reporting stale lag/bytes series
//     for partitions we no longer consume).
//
//   - onPartitionsRevoked must strip revoked partitions from lastConsumedOffsets
//     and pendingFlushOffsets so a background flush cannot zombie-commit them.
//
//   - onPartitionsAssigned must clear lastConsumedOffsets for newly-assigned
//     partitions so a post-revoke stale re-add cannot rewind after reassign.
//
//   - commitOffsets must filter to currently-owned partitions and must NOT
//     soft-skip membership errors as success.
//
//   - revoke during an in-flight flush must not race the aliased snapshot, and
//     shutting down while that flush is still uploading must not deadlock.
//
//   - isMembershipErr must classify exactly the broker error codes that mean
//     "this commit was rejected because of a membership change" (used for
//     the commit_offsets_membership metric label).
//
// We bootstrap a real Service via setupKafkaTest so the embedded kfake
// cluster and metrics registration match production wiring. The callbacks
// are pure in-process functions, so we drive them directly instead of
// orchestrating a real rebalance.

// newServiceForPartitionCallbackTests builds a minimal Service for driving
// assign/revoke callbacks directly. A full New() wires a live consumer-group
// client that can race real assignments into ownedPartitions (especially with
// the 16-partition default fake ring), which flakes these unit tests in CI.
func newServiceForPartitionCallbackTests(t *testing.T) *Service {
	t.Helper()
	svc := &Service{
		cfg: Config{
			Kafka: kafka.Config{Topic: testTopic},
		},
		logger:              log.NewNopLogger(),
		metrics:             NewMetrics(prometheus.NewRegistry()),
		lastConsumedOffsets: make(map[kafka.PartitionID]kafka.Offset),
	}
	empty := []kafka.PartitionID{}
	svc.ownedPartitions.Store(&empty)
	return svc
}

func TestOnPartitionsAssigned_MergesIntoOwnedSet(t *testing.T) {
	svc := newServiceForPartitionCallbackTests(t)

	// First assignment: empty → {0, 2, 4}.
	svc.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		testTopic: {0, 2, 4},
	})
	require.Equal(t, []kafka.PartitionID{0, 2, 4}, svc.snapshotOwnedPartitions())

	// Cooperative-sticky only delivers the delta on subsequent assigns —
	// the callback must merge, not replace.
	svc.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		testTopic: {1, 3},
	})
	require.Equal(t, []kafka.PartitionID{0, 1, 2, 3, 4}, svc.snapshotOwnedPartitions())

	// Assignments under a different topic key are ignored. We only ever
	// subscribe to one topic, but kgo's callback signature is a map, so
	// we defend against accidental fan-out.
	svc.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		"other-topic": {99},
	})
	require.Equal(t, []kafka.PartitionID{0, 1, 2, 3, 4}, svc.snapshotOwnedPartitions())

	// Empty / missing-topic deltas are no-ops.
	svc.onPartitionsAssigned(context.Background(), nil, nil)
	svc.onPartitionsAssigned(context.Background(), nil, map[string][]int32{testTopic: {}})
	require.Equal(t, []kafka.PartitionID{0, 1, 2, 3, 4}, svc.snapshotOwnedPartitions())
}

func TestOnPartitionsRevoked_RemovesFromOwnedSetAndDeletesMetricLabels(t *testing.T) {
	svc := newServiceForPartitionCallbackTests(t)

	// Seed ownership and per-partition metric labels for {0, 1, 2}.
	svc.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		testTopic: {0, 1, 2},
	})
	for _, p := range []string{"0", "1", "2"} {
		svc.metrics.consumptionLagSeconds.WithLabelValues(p).Set(42)
		svc.metrics.bytesReceivedTotal.WithLabelValues(p).Add(100)
	}
	require.Equal(t, 3, testutil.CollectAndCount(svc.metrics.consumptionLagSeconds))
	require.Equal(t, 3, testutil.CollectAndCount(svc.metrics.bytesReceivedTotal))

	// Revoke {1}: owned set drops to {0, 2}; the partition-1 label series
	// must be deleted so dashboards stop reporting stale lag/bytes.
	svc.onPartitionsLostOrRevoked(context.Background(), nil, map[string][]int32{
		testTopic: {1},
	})
	require.Equal(t, []kafka.PartitionID{0, 2}, svc.snapshotOwnedPartitions())
	require.Equal(t, 2, testutil.CollectAndCount(svc.metrics.consumptionLagSeconds),
		"consumptionLagSeconds label for revoked partition must be deleted")
	require.Equal(t, 2, testutil.CollectAndCount(svc.metrics.bytesReceivedTotal),
		"bytesReceivedTotal label for revoked partition must be deleted")

	// Revoking a partition we don't own is a harmless no-op (kgo can
	// replay revokes around Close).
	svc.onPartitionsLostOrRevoked(context.Background(), nil, map[string][]int32{
		testTopic: {99},
	})
	require.Equal(t, []kafka.PartitionID{0, 2}, svc.snapshotOwnedPartitions())

	// Revoking everything we still own leaves the set empty.
	svc.onPartitionsLostOrRevoked(context.Background(), nil, map[string][]int32{
		testTopic: {0, 2},
	})
	require.Empty(t, svc.snapshotOwnedPartitions())
	require.Equal(t, 0, testutil.CollectAndCount(svc.metrics.consumptionLagSeconds))
	require.Equal(t, 0, testutil.CollectAndCount(svc.metrics.bytesReceivedTotal))
}

// TestOnPartitionsAssigned_ClearsStaleOffsets verifies that a re-assignment
// drops leftover offsets from both the active and in-flight builders. Without
// this, the sequence revoke → processRecordBatch re-add → builder swap →
// interim owner commits ahead → reassign-back would let the owned-filter pass
// a stale rewind through.
func TestOnPartitionsAssigned_ClearsStaleOffsets(t *testing.T) {
	svc := newServiceForPartitionCallbackTests(t)

	svc.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		testTopic: {0, 1},
	})
	svc.lastConsumedOffsets = map[kafka.PartitionID]kafka.Offset{
		0: 10, 1: 20,
	}

	svc.onPartitionsLostOrRevoked(context.Background(), nil, map[string][]int32{
		testTopic: {1},
	})
	require.Equal(t, map[kafka.PartitionID]kafka.Offset{0: 10}, svc.lastConsumedOffsets)

	// Simulate the stale-batch re-add that processRecordBatch can perform
	// after revoke strips the partition, followed by a builder swap that
	// moves the stale offset into the in-flight commit set.
	svc.lastConsumedOffsets[1] = 100
	svc.pendingFlushOffsets = map[kafka.PartitionID]kafka.Offset{
		0: 5, 1: 100,
	}

	// Partition 1 comes back — any pre-existing offset is stale; kgo will
	// resume from the committed offset.
	svc.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		testTopic: {1},
	})
	require.Equal(t, map[kafka.PartitionID]kafka.Offset{0: 10}, svc.lastConsumedOffsets,
		"newly-assigned partition must drop stale lastConsumedOffsets entry")
	require.Equal(t, map[kafka.PartitionID]kafka.Offset{0: 5}, svc.pendingFlushOffsets,
		"newly-assigned partition must drop stale pendingFlushOffsets entry")
	require.Equal(t, []kafka.PartitionID{0, 1}, svc.snapshotOwnedPartitions())
}

// TestOnPartitionsRevoked_StripsPendingCommitSets verifies the zombie-commit
// fence: revoke must remove partitions from both the active offset map and
// any in-flight executeFlush snapshot so a later CommitOffsetsSync cannot
// rewind the new owner's committed offset.
func TestOnPartitionsRevoked_StripsPendingCommitSets(t *testing.T) {
	svc := newServiceForPartitionCallbackTests(t)

	svc.onPartitionsAssigned(context.Background(), nil, map[string][]int32{
		testTopic: {0, 1, 2},
	})
	svc.lastConsumedOffsets = map[kafka.PartitionID]kafka.Offset{
		0: 10, 1: 20, 2: 30,
	}
	// Simulate an in-flight flush that snapshotted offsets at swap time.
	svc.pendingFlushOffsets = map[kafka.PartitionID]kafka.Offset{
		0: 5, 1: 15, 2: 25,
	}

	svc.onPartitionsLostOrRevoked(context.Background(), nil, map[string][]int32{
		testTopic: {1},
	})

	require.Equal(t, []kafka.PartitionID{0, 2}, svc.snapshotOwnedPartitions())
	require.Equal(t, map[kafka.PartitionID]kafka.Offset{0: 10, 2: 30}, svc.lastConsumedOffsets,
		"revoked partition must be stripped from active lastConsumedOffsets")
	require.Equal(t, map[kafka.PartitionID]kafka.Offset{0: 5, 2: 25}, svc.pendingFlushOffsets,
		"revoked partition must be stripped from in-flight flush snapshot")

	// Revoking the rest clears both maps.
	svc.onPartitionsLostOrRevoked(context.Background(), nil, map[string][]int32{
		testTopic: {0, 2},
	})
	require.Empty(t, svc.lastConsumedOffsets)
	require.Empty(t, svc.pendingFlushOffsets)
}

func TestIsMembershipErr(t *testing.T) {
	t.Parallel()

	// Membership: broker rejects the commit because membership/generation
	// changed. Used for the commit_offsets_membership metric label; the
	// error still propagates so flush does not log success.
	membership := []error{
		kerr.RebalanceInProgress,
		kerr.IllegalGeneration,
		kerr.UnknownMemberID,
		kerr.FencedInstanceID,
		kerr.UnknownTopicOrPartition,
	}
	for _, err := range membership {
		require.Truef(t, isMembershipErr(err),
			"%v must classify as a membership error", err)
		// Wrapping via %w must preserve the classification — callers may
		// add context (e.g. partition ID) before returning.
		wrapped := fmt.Errorf("commit failed: %w", err)
		require.Truef(t, isMembershipErr(wrapped),
			"wrapped %v must still classify as membership", err)
	}

	// Non-membership: anything else must NOT classify as membership.
	other := []error{
		kerr.RequestTimedOut,
		kerr.CoordinatorNotAvailable,
		kerr.NotCoordinator,
		errors.New("network error"),
		nil,
	}
	for _, err := range other {
		require.Falsef(t, isMembershipErr(err),
			"%v must NOT classify as a membership error", err)
	}
}

// TestCommitOffsets_MembershipErrorPropagates verifies that when the broker
// rejects an offset commit for a specific partition with a membership error
// code, commitOffsets:
//
//  1. returns an error naming the uncommitted partitions (does NOT soft-skip),
//  2. increments flushErrorsTotal{step="commit_offsets_membership"}.
//
// Soft-skipping these as flush success is what made zombie-commit incidents
// hard to trace — the log said "flush step succeeded" while offsets were
// not durable.
func TestCommitOffsets_MembershipErrorPropagates(t *testing.T) {
	cluster, lokiConfig, cfg := setupKafkaTest(t)
	defer cluster.Close()

	_, indexStore := newTestStore(t)
	svc, err := New(lokiConfig, indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	// Start the service so it joins the consumer group — CommitOffsetsSync
	// needs a live member ID / generation; otherwise the broker would
	// reject the commit with UNKNOWN_MEMBER_ID and we couldn't exercise
	// the per-partition error path.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, svc.Service.StartAsync(ctx))
	require.NoError(t, svc.Service.AwaitRunning(ctx))
	t.Cleanup(func() {
		svc.Service.StopAsync()
		_ = svc.Service.AwaitTerminated(context.Background())
	})

	// Own partition 0 so the owned-partition filter does not drop the commit
	// before it reaches the broker.
	svc.storeOwnedPartitions([]kafka.PartitionID{0})

	// Inject a synthetic OffsetCommit response that flags partition 0 with
	// a membership error code. KeepControl + Swap on the first invocation
	// only — subsequent commits (including the one fired during shutdown)
	// pass through naturally so the cleanup path doesn't deadlock.
	var injected atomic.Bool
	cluster.ControlKey(int16(kmsg.OffsetCommit), func(kreq kmsg.Request) (kmsg.Response, error, bool) {
		if injected.Swap(true) {
			return nil, nil, false
		}
		cluster.KeepControl()
		req := kreq.(*kmsg.OffsetCommitRequest)
		resp := req.ResponseKind().(*kmsg.OffsetCommitResponse)
		topics := make([]kmsg.OffsetCommitResponseTopic, 0, len(req.Topics))
		for _, rt := range req.Topics {
			parts := make([]kmsg.OffsetCommitResponseTopicPartition, 0, len(rt.Partitions))
			for _, rp := range rt.Partitions {
				parts = append(parts, kmsg.OffsetCommitResponseTopicPartition{
					Partition: rp.Partition,
					// Per-partition membership error — broker form of
					// "you no longer own this partition".
					ErrorCode: kerr.UnknownTopicOrPartition.Code,
				})
			}
			topics = append(topics, kmsg.OffsetCommitResponseTopic{
				Topic:      rt.Topic,
				Partitions: parts,
			})
		}
		resp.Topics = topics
		return resp, nil, true
	})

	// Drive the commit directly: a real flush cycle wouldn't be reliable
	// here because we don't actually have records to consume.
	err = svc.commitOffsets(ctx, map[kafka.PartitionID]kafka.Offset{0: 42})
	require.Error(t, err, "membership error must propagate so flush does not log success")
	require.Contains(t, err.Error(), "offsets NOT committed for partitions")
	require.Contains(t, err.Error(), "0")

	require.Equal(t, float64(1),
		testutil.ToFloat64(svc.metrics.flushErrorsTotal.WithLabelValues("commit_offsets_membership")),
		"flushErrorsTotal{commit_offsets_membership} must increment for the failed partition")
}

// TestCommitOffsets_FiltersToOwnedPartitions verifies defense-in-depth
// filtering: partitions absent from ownedPartitions are not included in
// the CommitOffsetsSync request, even if they remain in the caller's map.
func TestCommitOffsets_FiltersToOwnedPartitions(t *testing.T) {
	cluster, lokiConfig, cfg := setupKafkaTest(t)
	defer cluster.Close()

	_, indexStore := newTestStore(t)
	svc, err := New(lokiConfig, indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, svc.Service.StartAsync(ctx))
	require.NoError(t, svc.Service.AwaitRunning(ctx))
	t.Cleanup(func() {
		svc.Service.StopAsync()
		_ = svc.Service.AwaitTerminated(context.Background())
	})

	// Own only partition 0; the caller's map still has partition 1 (the
	// zombie we must not commit).
	svc.storeOwnedPartitions([]kafka.PartitionID{0})

	var committed []int32
	var injected atomic.Bool
	cluster.ControlKey(int16(kmsg.OffsetCommit), func(kreq kmsg.Request) (kmsg.Response, error, bool) {
		if injected.Swap(true) {
			return nil, nil, false
		}
		cluster.KeepControl()
		req := kreq.(*kmsg.OffsetCommitRequest)
		for _, rt := range req.Topics {
			for _, rp := range rt.Partitions {
				committed = append(committed, rp.Partition)
			}
		}
		resp := req.ResponseKind().(*kmsg.OffsetCommitResponse)
		topics := make([]kmsg.OffsetCommitResponseTopic, 0, len(req.Topics))
		for _, rt := range req.Topics {
			parts := make([]kmsg.OffsetCommitResponseTopicPartition, 0, len(rt.Partitions))
			for _, rp := range rt.Partitions {
				parts = append(parts, kmsg.OffsetCommitResponseTopicPartition{
					Partition: rp.Partition,
					ErrorCode: 0,
				})
			}
			topics = append(topics, kmsg.OffsetCommitResponseTopic{
				Topic:      rt.Topic,
				Partitions: parts,
			})
		}
		resp.Topics = topics
		return resp, nil, true
	})

	err = svc.commitOffsets(ctx, map[kafka.PartitionID]kafka.Offset{
		0: 42,
		1: 99, // revoked / unowned — must not appear in the request
	})
	require.NoError(t, err)
	require.Equal(t, []int32{0}, committed,
		"CommitOffsetsSync must only include currently-owned partitions")
}

// blockingUploadBucket wraps a bucket and blocks the first Upload until
// unblock is closed. Used to park an in-flight flush between prepare and
// commit so tests can revoke / shut down concurrently.
type blockingUploadBucket struct {
	objstore.Bucket
	uploadStarted chan struct{}
	unblock       chan struct{}
	startedOnce   sync.Once
}

type messageSignalLogger struct {
	log.Logger
	message  string
	signaled chan struct{}
	once     sync.Once
}

func (l *messageSignalLogger) Log(keyvals ...any) error {
	for i := 0; i+1 < len(keyvals); i += 2 {
		if keyvals[i] == "msg" && keyvals[i+1] == l.message {
			l.once.Do(func() { close(l.signaled) })
			break
		}
	}
	return l.Logger.Log(keyvals...)
}

func (b *blockingUploadBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	b.startedOnce.Do(func() { close(b.uploadStarted) })
	select {
	case <-b.unblock:
	case <-ctx.Done():
		return ctx.Err()
	}
	return b.Bucket.Upload(ctx, name, r)
}

// TestRevokeDuringFlushThenShutdown covers the two races the fence must
// survive together:
//
//  1. revoke concurrent with an in-flight flush (strips pendingFlushOffsets
//     while executeFlush is mid-upload; commit must not see a half-stripped
//     map or commit the revoked partition), and
//  2. shutdown while that flush is still uploading — stopping must not hold
//     builderMtx across awaitPendingFlush, or commitOffsets deadlocks until
//     the 5-minute shutdownTimeout.
func TestRevokeDuringFlushThenShutdown(t *testing.T) {
	cluster, lokiConfig, cfg := setupKafkaTest(t)
	defer cluster.Close()

	inner := objstore.NewInMemBucket()
	blocked := &blockingUploadBucket{
		Bucket:        inner,
		uploadStarted: make(chan struct{}),
		unblock:       make(chan struct{}),
	}
	indexStore, err := store.NewStore(blocked, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), nil)
	require.NoError(t, err)

	waitLogger := &messageSignalLogger{
		Logger:   log.NewNopLogger(),
		message:  "waiting for in-progress flush during shutdown",
		signaled: make(chan struct{}),
	}
	svc, err := New(lokiConfig, indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), waitLogger, prometheus.NewRegistry())
	require.NoError(t, err)

	// Join the group so CommitOffsetsSync has a live member/generation.
	// Do not rely on the poll loop for records — we drive processRecordBatch
	// and flushAndCommit directly so the blocked upload is deterministic.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	require.NoError(t, svc.Service.StartAsync(ctx))
	require.NoError(t, svc.Service.AwaitRunning(ctx))

	require.Eventually(t, func() bool {
		owned := svc.snapshotOwnedPartitions()
		return slices.Contains(owned, kafka.PartitionID(0)) &&
			slices.Contains(owned, kafka.PartitionID(1))
	}, 5*time.Second, 10*time.Millisecond, "timed out waiting for the initial group assignment")

	stream := logproto.Stream{
		Labels:  `{job="test"}`,
		Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "revoke-during-flush test line"}},
	}
	data, err := stream.Marshal()
	require.NoError(t, err)

	require.NoError(t, svc.processRecordBatch([]rawRecord{
		{value: data, timestamp: time.Now(), partition: 0, offset: 10, tenantID: "tenant"},
		{value: data, timestamp: time.Now(), partition: 1, offset: 20, tenantID: "tenant"},
	}))

	svc.flushAndCommit(true)

	select {
	case <-blocked.uploadStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for flush to reach upload")
	}

	svc.builderMtx.Lock()
	require.NotNil(t, svc.pendingFlushOffsets, "in-flight flush must publish its offset snapshot")
	require.Contains(t, svc.pendingFlushOffsets, kafka.PartitionID(1))
	svc.builderMtx.Unlock()

	// Revoke while upload is parked — must strip the in-flight snapshot
	// without blocking on builderMtx (flush is in upload, not commit yet).
	revokeDone := make(chan struct{})
	go func() {
		svc.onPartitionsLostOrRevoked(context.Background(), nil, map[string][]int32{
			testTopic: {1},
		})
		close(revokeDone)
	}()
	select {
	case <-revokeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("revoke blocked while flush was mid-upload")
	}

	svc.builderMtx.Lock()
	require.NotContains(t, svc.pendingFlushOffsets, kafka.PartitionID(1),
		"revoked partition must be stripped from in-flight flush snapshot")
	require.NotContains(t, svc.lastConsumedOffsets, kafka.PartitionID(1),
		"revoked partition must be stripped from active offsets")
	require.Contains(t, svc.pendingFlushOffsets, kafka.PartitionID(0))
	svc.builderMtx.Unlock()

	// Start shutdown while upload is still blocked. With the old code
	// stopping() acquired builderMtx and waited on pendingFlush — deadlocking
	// once we unblock and commitOffsets tries to take the same lock.
	stopErr := make(chan error, 1)
	go func() {
		svc.Service.StopAsync()
		stopErr <- svc.Service.AwaitTerminated(context.Background())
	}()

	select {
	case <-waitLogger.signaled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shutdown to await the pending flush")
	}
	close(blocked.unblock)

	select {
	case err := <-stopErr:
		require.NoError(t, err, "shutdown with in-flight flush must complete without deadlock")
	case <-time.After(10 * time.Second):
		t.Fatal("stopping deadlocked — held builderMtx across awaitPendingFlush while flush needed it for commitOffsets")
	}
}
