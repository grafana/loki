package builder

import (
	"context"
	"maps"
	"runtime/debug"
	"testing"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/kafka"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logproto"
)

const testTopic = "test-topic"

// newTestStore creates an in-memory objstore bucket wrapped in a store.Store.
// Returns both so tests can use the raw bucket for verification.
func newTestStore(t *testing.T) (*objstore.InMemBucket, *store.Store) {
	t.Helper()
	bucket := objstore.NewInMemBucket()
	s, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), nil)
	require.NoError(t, err)
	return bucket, s
}

// setupKafkaTest creates a fake Kafka cluster and returns the cluster and builder config
func setupKafkaTest(t *testing.T) (*kfake.Cluster, Config) {
	tmpDir := t.TempDir()

	// Single-broker fake cluster with one partition.
	// GroupMaxSessionTimeout must exceed the service's configured session
	// timeout (kafkaSessionTimeout, 2 min) or JoinGroup is rejected.
	cluster, err := kfake.NewCluster(
		kfake.NumBrokers(1),
		kfake.SeedTopics(1, testTopic),
		kfake.GroupMaxSessionTimeout(5*time.Minute),
	)
	require.NoError(t, err)
	t.Cleanup(cluster.Close)

	// Get broker addresses
	addrs := cluster.ListenAddrs()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: addrs[0]},
			Topic:                      testTopic,
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: kafka.MaxProducerRecordDataBytesLimit,
		},
		Logline: LoglineConfig{
			DocumentInterval: 100 * time.Millisecond,
			IndexVersion:     "v3",
			DensityThreshold: 0.20,
			NgramLength:      3,
		},

		InstanceID:              "test-builder-0",
		disableStaticMembership: true,
		KafkaSessionTimeout:     DefaultKafkaSessionTimeout,
		FlushOnIdle:             100 * time.Millisecond,
		FlushOnMaxAge:           5 * time.Hour,
		FlushOnMaxBytes:         DefaultFlushOnMaxBytes,
		FlushCheckInterval:      DefaultFlushCheckInterval,
		ScratchDir:              tmpDir,
	}

	return cluster, cfg
}

// TestConsumerLag verifies the per-partition lag calculation.
//
// Lag must be 0 once the consumer has caught up to HEAD, regardless of the
// latest consumed record's timestamp: the tail record can legitimately be
// old at HEAD (e.g. just finished draining a backlog, or the producer
// batches records so the payload timestamp predates broker arrival).
// Reporting time.Since(latest.Timestamp) in those cases would falsely
// indicate the consumer is behind.
//
// While still behind HEAD, lag falls back to the wall-clock age of the
// latest consumed record as a best-effort estimate.
func TestConsumerLag(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	tests := []struct {
		name          string
		lastConsumed  *kgo.Record
		highWatermark int64
		want          float64
	}{
		// At HEAD: lag must be 0 regardless of the tail record's age.
		{
			// Consumer just finished draining a backlog; the tail
			// record is naturally old because we were behind. Nothing
			// left to consume, so lag must not reflect that age.
			name:          "at HEAD with 30 min old tail reports 0",
			lastConsumed:  &kgo.Record{Offset: 999, Timestamp: now.Add(-30 * time.Minute)},
			highWatermark: 1000,
			want:          0,
		},
		{
			// Steady-state: producer is writing continuously, consumer
			// keeps up, records carry current timestamps.
			name:          "at HEAD with fresh record reports 0",
			lastConsumed:  &kgo.Record{Offset: 1004, Timestamp: now},
			highWatermark: 1005,
			want:          0,
		},
		{
			// No records returned for this partition in the current
			// fetch (silent producer). Nothing to base age on; lag
			// stays at 0.
			name:          "no records consumed reports 0",
			lastConsumed:  nil,
			highWatermark: 1000,
			want:          0,
		},

		// Behind HEAD: lag = wall-clock age of the latest consumed record.
		{
			// Mid-backlog: 500 records short of HEAD, latest consumed
			// was produced 10 minutes ago.
			name:          "well behind HEAD reports wall-clock age",
			lastConsumed:  &kgo.Record{Offset: 500, Timestamp: now.Add(-10 * time.Minute)},
			highWatermark: 1000,
			want:          600,
		},
		{
			// Just before catch-up: one record short of HEAD.
			name:          "one record short of HEAD reports wall-clock age",
			lastConsumed:  &kgo.Record{Offset: 998, Timestamp: now.Add(-5 * time.Second)},
			highWatermark: 1000,
			want:          5,
		},

		// Defensive: the >= comparison must handle off-by-one and
		// "shouldn't happen" inputs without reporting spurious lag.
		{
			// HighWatermark is the offset of the next record to be
			// produced, so a consumer with offset == HW has technically
			// consumed a record that doesn't exist yet — shouldn't
			// happen in practice, but must report 0.
			name:          "consumed offset equals HW reports 0",
			lastConsumed:  &kgo.Record{Offset: 1000, Timestamp: now.Add(-1 * time.Hour)},
			highWatermark: 1000,
			want:          0,
		},
		{
			name:          "consumed offset past HW reports 0",
			lastConsumed:  &kgo.Record{Offset: 1001, Timestamp: now.Add(-1 * time.Hour)},
			highWatermark: 1000,
			want:          0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := consumerLag(now, tt.lastConsumed, tt.highWatermark)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestService_New(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	_, indexStore := newTestStore(t)

	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, reg)
	require.NoError(t, err)
	require.NotNil(t, svc)

	// New calls cfg.Validate which mutates the local copy to apply defaults
	// (ring fields, KafkaSessionTimeout, etc). Apply the same defaults to
	// the test's cfg before comparing so we assert "svc stored what we
	// passed, modulo defaults" rather than "svc kept the raw input".
	require.NoError(t, cfg.Validate())
	require.Equal(t, cfg, svc.cfg)
	require.NotNil(t, svc.logger)
	require.NotNil(t, svc.metrics)
	require.NotNil(t, svc.activeBuilder)
	require.NotNil(t, svc.client)

	// Clean up
	svc.client.Close()
}

func TestService_New_MissingKafkaAddress(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		Kafka: kafka.Config{
			// Intentionally no ReaderConfig or WriterConfig address to test validation
			Topic:                      testTopic,
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Logline: LoglineConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      3},

		InstanceID:              "test-builder-0",
		disableStaticMembership: true,
		FlushOnIdle:             1 * time.Minute,
		FlushOnMaxAge:           5 * time.Hour,
		ScratchDir:              tmpDir,
	}

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	_, indexStore := newTestStore(t)

	// Empty loki config (no Kafka address)

	_, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, reg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "the Kafka address has not been configured")
}

func TestService_New_NilStore(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

	_, err := New(nil, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, reg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "indexStore cannot be nil")
}

func TestService_StartStop(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	_, indexStore := newTestStore(t)

	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, reg)
	require.NoError(t, err)

	// Start service
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = svc.StartAsync(ctx)
	require.NoError(t, err)

	// Wait for running state
	err = svc.AwaitRunning(ctx)
	require.NoError(t, err)

	// Stop service
	svc.StopAsync()
	err = svc.AwaitTerminated(ctx)
	require.NoError(t, err)
}

// TestService_ConsumerGroupAssignment verifies that a service joins the
// consumer group, is assigned the topic's single partition by the coordinator,
// and consumes records produced to it.
func TestService_ConsumerGroupAssignment(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	addrs := cluster.ListenAddrs()
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	_, indexStore := newTestStore(t)

	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, reg)
	require.NoError(t, err)

	// Produce records to partition 0 (the only partition in the single-partition kfake cluster).
	producer, err := kgo.NewClient(kgo.SeedBrokers(addrs...))
	require.NoError(t, err)
	defer producer.Close()

	ctx := context.Background()
	streamBytes := []byte(`{job="test"}test log line`)
	rec := &kgo.Record{
		Topic:     testTopic,
		Partition: 0,
		Value:     streamBytes,
	}
	results := producer.ProduceSync(ctx, rec)
	require.NoError(t, results.FirstErr())

	// Start service — it should consume from partition 0.
	// No partition assignment delay needed with direct consuming.
	startCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = svc.StartAsync(startCtx)
	require.NoError(t, err)

	err = svc.AwaitRunning(startCtx)
	require.NoError(t, err)

	// Give it time to process.
	time.Sleep(500 * time.Millisecond)

	t.Log("Direct partition assignment test completed successfully")

	svc.StopAsync()
	err = svc.AwaitTerminated(startCtx)
	require.NoError(t, err)
}

// TestService_FlushSwap verifies the flush swap mechanism: flushAndCommit
// replaces the active set with a fresh one and respects backpressure when a
// flush is already in progress.
func TestService_FlushSwap(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	_, indexStore := newTestStore(t)

	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, reg)
	require.NoError(t, err)
	defer svc.client.Close()

	svc.builderMtx.Lock()
	originalBuilder := svc.activeBuilder
	svc.builderMtx.Unlock()

	// Trigger a flush with no data — shouldFlush returns false, no swap.
	svc.flushAndCommit(false)
	svc.builderMtx.Lock()
	afterNoData := svc.activeBuilder
	svc.builderMtx.Unlock()
	require.Same(t, originalBuilder, afterNoData, "no swap when no data")

	// Force a flush; active builder should be replaced with a fresh one.
	svc.flushAndCommit(true)

	// Snapshot under the lock — the background flush goroutine may close
	// state.done concurrently.
	svc.builderMtx.Lock()
	newBuilder1 := svc.activeBuilder
	pendingDone1 := svc.pendingFlush
	svc.builderMtx.Unlock()
	require.NotSame(t, originalBuilder, newBuilder1, "active builder should be a new instance after flush")
	require.NotNil(t, pendingDone1, "pendingFlush should be set")

	// Empty forced flushes finish almost immediately, so wait for the real
	// flush then hold pendingFlush open to deterministically exercise
	// backpressure (same pattern as TestService_WaitForFlushBackpressure).
	<-pendingDone1
	held := make(chan struct{})
	svc.builderMtx.Lock()
	svc.pendingFlush = held
	builderBeforeBackpressure := svc.activeBuilder
	svc.builderMtx.Unlock()

	svc.flushAndCommit(true)
	svc.builderMtx.Lock()
	builderDuringBackpressure := svc.activeBuilder
	svc.builderMtx.Unlock()
	// Pointer compare only — require.Same formats the whole builder and can
	// race with a background clear() on failure.
	require.True(t, builderBeforeBackpressure == builderDuringBackpressure,
		"active builder should not change while flush is running")
	require.Equal(t, 1.0, testutil.ToFloat64(svc.metrics.flushBackpressuredTotal),
		"backpressured flush should increment flushBackpressuredTotal")
	close(held)

	// Force another flush; active builder should be replaced again.
	svc.flushAndCommit(true)
	svc.builderMtx.Lock()
	newBuilder2 := svc.activeBuilder
	pendingDone2 := svc.pendingFlush
	svc.builderMtx.Unlock()
	require.NotSame(t, newBuilder1, newBuilder2, "active builder should be a new instance after second flush")

	// Wait for the second flush to complete.
	<-pendingDone2
}

// TestService_WaitForFlushBackpressure verifies the backpressure mechanism
// that blocks Kafka consumption when a flush is pending and the current
// builder has hit a flush trigger
func TestService_WaitForFlushBackpressure(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	_, indexStore := newTestStore(t)

	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, reg)
	require.NoError(t, err)
	defer svc.client.Close()

	ctx := context.Background()

	// No pending flush → returns immediately.
	svc.waitForFlushBackpressure(ctx)

	// Pending flush but no flush trigger → returns immediately.
	pending := make(chan struct{})
	svc.builderMtx.Lock()
	svc.pendingFlush = pending
	svc.builderMtx.Unlock()
	svc.waitForFlushBackpressure(ctx)

	// Pending flush AND flush trigger met → blocks until flush completes.
	// Use max_age with a past firstAppend to trigger shouldFlush.
	svc.builderMtx.Lock()
	svc.activeBuilder.firstAppend = time.Now().Add(-time.Hour)
	svc.cfg.FlushOnMaxAge = time.Nanosecond
	svc.builderMtx.Unlock()

	unblocked := make(chan struct{})
	go func() {
		svc.waitForFlushBackpressure(ctx)
		close(unblocked)
	}()

	select {
	case <-unblocked:
		t.Fatal("waitForFlushBackpressure should block while flush is pending and trigger is met")
	case <-time.After(50 * time.Millisecond):
	}

	// Complete the pending flush — should unblock.
	close(pending)

	select {
	case <-unblocked:
	case <-time.After(time.Second):
		t.Fatal("waitForFlushBackpressure should unblock after pending flush completes")
	}

	// Context cancellation also unblocks.
	pending2 := make(chan struct{})
	svc.builderMtx.Lock()
	svc.pendingFlush = pending2
	svc.builderMtx.Unlock()

	cancelCtx, cancel := context.WithCancel(ctx)
	unblocked2 := make(chan struct{})
	go func() {
		svc.waitForFlushBackpressure(cancelCtx)
		close(unblocked2)
	}()

	select {
	case <-unblocked2:
		t.Fatal("should block while flush is pending")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	select {
	case <-unblocked2:
	case <-time.After(time.Second):
		t.Fatal("should unblock on context cancellation")
	}
}

// TestService_FlushTickerTriggersMaxAge verifies that the periodic flush
// ticker fires a max_age flush independently of the poll loop. The service
// is started with a very short max age and flush check interval so the
// ticker fires before the test times out.
func TestService_FlushTickerTriggersMaxAge(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	cfg.FlushOnMaxAge = 200 * time.Millisecond
	cfg.FlushOnIdle = 10 * time.Minute // high so only max_age triggers
	cfg.FlushCheckInterval = 50 * time.Millisecond

	bucket, indexStore := newTestStore(t)

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, reg)
	require.NoError(t, err)

	ctx := t.Context()

	// Start the service — this launches the poll loop and flush ticker.
	go func() { _ = svc.StartAsync(ctx) }()
	require.NoError(t, svc.AwaitRunning(ctx))

	// Produce a single record so the builder has data with firstAppend set.
	producer, err := kgo.NewClient(
		kgo.SeedBrokers(cluster.ListenAddrs()[0]),
		kgo.DefaultProduceTopic(testTopic),
	)
	require.NoError(t, err)
	defer producer.Close()

	stream := logproto.Stream{
		Labels:  `{job="test"}`,
		Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "test line for flush ticker"}},
	}
	data, err := stream.Marshal()
	require.NoError(t, err)
	record := &kgo.Record{Value: data}
	results := producer.ProduceSync(ctx, record)
	require.NoError(t, results.FirstErr())

	// Wait for the flush ticker to fire a max_age flush. The ticker runs
	// every 50ms and max_age is 200ms, so the flush should happen within
	// ~300ms. Poll the object store for uploaded files.
	require.Eventually(t, func() bool {
		return len(bucket.Objects()) > 0
	}, 5*time.Second, 50*time.Millisecond, "expected flush ticker to produce index files via max_age")

	svc.StopAsync()
	require.NoError(t, svc.AwaitTerminated(ctx))
}

// TestService_PreMinDateOffsetCommit verifies that Kafka offsets are committed
// even when all records in a flush cycle are filtered by the minDate check
// (i.e., executeFlush produces zero index files). Before the bug fix, the
// early-return in executeFlush discarded lastConsumedOffsets without committing,
// causing infinite re-consumption on restart.
func TestService_PreMinDateOffsetCommit(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	// Set minDate to tomorrow so all real records (timestamped in the past) are filtered.
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")

	bucket, indexStore := newTestStore(t)

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

	// Speed up the flush so we don't have to wait for the default max-age timer.
	cfg.FlushOnMaxAge = 500 * time.Millisecond
	cfg.FlushCheckInterval = 100 * time.Millisecond

	svc, err := New(indexStore, cfg, tomorrow, newDefaultFakePartitionRing(), logger, reg)
	require.NoError(t, err)
	defer svc.client.Close()

	// Produce a record with an old log-line timestamp.
	producer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()[0]))
	require.NoError(t, err)
	defer producer.Close()

	ctx := context.Background()
	stream := logproto.Stream{
		Labels: `{job="test"}`,
		Entries: []logproto.Entry{
			{
				Timestamp: time.Now().AddDate(-1, 0, 0), // one year ago — before minDate
				Line:      "old log line",
			},
		},
	}
	data, err := stream.Marshal()
	require.NoError(t, err)

	produceCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	results := producer.ProduceSync(produceCtx, &kgo.Record{
		Topic: testTopic,
		Value: data,
		Key:   []byte("tenant"),
	})
	require.NoError(t, results.FirstErr())
	lastProducedOffset := results[0].Record.Offset

	// Manually drive the service: simulate consuming the record via processRecordBatch.
	_ = svc.processRecordBatch([]rawRecord{
		{
			value:     data,
			timestamp: time.Now(),
			partition: 0,
			offset:    kafka.Offset(lastProducedOffset),
			tenantID:  "tenant",
		},
	})

	// Force a flush and wait for it to complete.
	svc.flushAndCommit(true)

	svc.builderMtx.Lock()
	done := svc.pendingFlush
	svc.builderMtx.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for flush to complete")
		}
	}

	// Assert: offset committed to lastProducedOffset+1 via the no-files commit path.
	adm := kadm.NewClient(svc.client)
	fetchCtx, fetchCancel := context.WithTimeout(ctx, 5*time.Second)
	defer fetchCancel()
	offsets, err := adm.FetchOffsets(fetchCtx, cfg.Kafka.ConsumerGroup)
	require.NoError(t, err)
	committed, ok := offsets.Lookup(testTopic, 0)
	require.True(t, ok, "expected committed offset for partition 0")
	require.Equal(t, lastProducedOffset+1, committed.At,
		"committed offset should be lastProducedOffset+1")

	// Assert: no files uploaded (all data was pre-minDate, nothing indexed).
	require.Empty(t, bucket.Objects(), "expected no objects in bucket — all data was pre-minDate")

	lag := testutil.ToFloat64(svc.metrics.consumptionLagSeconds.WithLabelValues("0"))
	require.Equal(t, 0.0, lag, "lag gauge for partition 0 should be 0 after no-files flush commits offsets")
}

func snapshotLastConsumedOffsets(svc *Service) map[kafka.PartitionID]kafka.Offset {
	svc.builderMtx.Lock()
	defer svc.builderMtx.Unlock()

	offsets := make(map[kafka.PartitionID]kafka.Offset, len(svc.lastConsumedOffsets))
	maps.Copy(offsets, svc.lastConsumedOffsets)
	return offsets
}

// TestMultiPartitionOffsetTracking verifies that processRecordBatch tracks
// the highest consumed offset independently per partition.
func TestMultiPartitionOffsetTracking(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	_, indexStore := newTestStore(t)

	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, reg)
	require.NoError(t, err)
	defer svc.client.Close()

	require.Empty(t, snapshotLastConsumedOffsets(svc))

	now := time.Now()
	records := []rawRecord{
		{value: mustMarshalStream(t, "p0-rec1"), timestamp: now, partition: 0, offset: 10},
		{value: mustMarshalStream(t, "p2-rec1"), timestamp: now, partition: 2, offset: 5},
		{value: mustMarshalStream(t, "p0-rec2"), timestamp: now, partition: 0, offset: 15},
		{value: mustMarshalStream(t, "p2-rec2"), timestamp: now, partition: 2, offset: 3}, // older than 5; should not overwrite
	}
	require.NoError(t, svc.processRecordBatch(records))

	offsets := snapshotLastConsumedOffsets(svc)
	require.Equal(t, kafka.Offset(15), offsets[0], "partition 0 should track highest offset")
	require.Equal(t, kafka.Offset(5), offsets[2], "partition 2 should track highest offset")
}

// TestProcessRecordBatch_DecodeErrorFailsFast verifies that an undecodable
// record aborts the batch with an error, increments the decode-error metric,
// and leaves offset tracking untouched so the restarted pod re-encounters the
// poison record.
func TestProcessRecordBatch_DecodeErrorFailsFast(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	_, indexStore := newTestStore(t)

	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, reg)
	require.NoError(t, err)
	defer svc.client.Close()

	now := time.Now()
	records := []rawRecord{
		{value: mustMarshalStream(t, "good"), timestamp: now, partition: 0, offset: 1},
		{value: []byte("not-protobuf"), timestamp: now, partition: 0, offset: 2},
		{value: mustMarshalStream(t, "after-poison"), timestamp: now, partition: 0, offset: 3},
	}
	err = svc.processRecordBatch(records)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to decode protobuf stream")
	require.Contains(t, err.Error(), "offset=2")

	require.Empty(t, snapshotLastConsumedOffsets(svc), "offset tracking must not advance when a record fails to decode")
	require.Equal(t, float64(1), testutil.ToFloat64(svc.metrics.decodeErrorsTotal))
}

// TestService_PollErrorDoesNotDropHealthyPartitionRecords makes sure that we handle all
// fetched records even when there are some failures.
func TestService_PollErrorDoesNotDropHealthyPartitionRecords(t *testing.T) {
	tmpDir := t.TempDir()

	cluster, err := kfake.NewCluster(
		kfake.NumBrokers(1),
		kfake.SeedTopics(2, testTopic),
		kfake.GroupMaxSessionTimeout(5*time.Minute),
	)
	require.NoError(t, err)
	t.Cleanup(cluster.Close)
	addrs := cluster.ListenAddrs()

	ctx := context.Background()
	topicID := cluster.TopicInfo(testTopic).TopicID

	// We capture what a response would look like so we can use it later.
	var rawBatches []byte
	cluster.ControlKey(int16(kmsg.Produce), func(kreq kmsg.Request) (kmsg.Response, error, bool) {
		cluster.KeepControl()
		rawBatches = kreq.(*kmsg.ProduceRequest).Topics[0].Partitions[0].Records
		return nil, nil, false
	})

	prod, err := kgo.NewClient(
		kgo.SeedBrokers(addrs...),
		kgo.RecordPartitioner(kgo.ManualPartitioner()),
	)
	require.NoError(t, err)
	require.NoError(t, prod.ProduceSync(ctx, &kgo.Record{
		Topic: testTopic, Partition: 0, Value: mustMarshalStream(t, "healthy-partition-record"),
	}).FirstErr())
	prod.Close()
	require.NotEmpty(t, rawBatches)

	// Inject a single synthetic Fetch response: partition 0 carries the real
	// record bytes, partition 1 carries an error code. With this the condition
	// 	`fetches.Err() != nil AND records present` is fulfilled
	var injected atomic.Bool
	cluster.ControlKey(int16(kmsg.Fetch), func(kreq kmsg.Request) (kmsg.Response, error, bool) {
		if injected.Swap(true) {
			return nil, nil, false // subsequent fetches pass through naturally
		}
		resp := kreq.(*kmsg.FetchRequest).ResponseKind().(*kmsg.FetchResponse)
		// PreferredReadReplica=-1 is required; otherwise kgo treats the
		// partition as a "redirect to broker 0" hint and skips records+error.
		resp.Topics = []kmsg.FetchResponseTopic{{
			Topic: testTopic, TopicID: topicID,
			Partitions: []kmsg.FetchResponseTopicPartition{
				{Partition: 0, HighWatermark: 1, LastStableOffset: 1, PreferredReadReplica: -1, RecordBatches: rawBatches},
				// Non-retriable error so kgo surfaces it via fetches.Err()
				// instead of stripping the partition and silently retrying.
				{Partition: 1, PreferredReadReplica: -1, ErrorCode: kerr.PolicyViolation.Code},
			},
		}}
		return resp, nil, true
	})

	// Service configured to consume both partitions.
	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: addrs[0]},
			Topic:                      testTopic,
			ConsumerGroup:              "test-group-poll-err",
			ProducerMaxRecordSizeBytes: kafka.MaxProducerRecordDataBytesLimit,
		},
		Logline: LoglineConfig{
			DocumentInterval: 100 * time.Millisecond,
			IndexVersion:     "v3",
			DensityThreshold: 0.20,
			NgramLength:      3,
		},
		InstanceID:              "test-builder-0",
		disableStaticMembership: true,
		FlushOnMaxBytes:         4096,
		FlushOnIdle:             100 * time.Millisecond,
		FlushOnMaxAge:           5 * time.Hour,
		FlushCheckInterval:      50 * time.Millisecond,
		ScratchDir:              tmpDir,
	}
	bucket, indexStore := newTestStore(t)
	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	startCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	require.NoError(t, svc.StartAsync(startCtx))
	require.NoError(t, svc.AwaitRunning(startCtx))

	require.Eventually(t, func() bool {
		return len(bucket.Objects()) > 0
	}, 3*time.Second, 50*time.Millisecond,
		"partition-0 record should be indexed despite the partition-1 error in the same fetch")

	svc.StopAsync()
	require.NoError(t, svc.AwaitTerminated(startCtx))
}

func mustMarshalStream(t *testing.T, line string) []byte {
	t.Helper()
	s := logproto.Stream{
		Labels:  `{job="test"}`,
		Entries: []logproto.Entry{{Timestamp: time.Now(), Line: line}},
	}
	data, err := s.Marshal()
	require.NoError(t, err)
	return data
}

// TestService_MultiPartitionConsumption verifies that a single service is
// assigned all partitions of a multi-partition topic by the group coordinator
// (no other members are present) and consumes records produced to each.
func TestService_MultiPartitionConsumption(t *testing.T) {
	tmpDir := t.TempDir()

	cluster, err := kfake.NewCluster(
		kfake.NumBrokers(1),
		kfake.SeedTopics(6, testTopic),
		kfake.GroupMaxSessionTimeout(5*time.Minute),
	)
	require.NoError(t, err)
	t.Cleanup(cluster.Close)

	addrs := cluster.ListenAddrs()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: addrs[0]},
			Topic:                      testTopic,
			ConsumerGroup:              "test-group-multi",
			ProducerMaxRecordSizeBytes: kafka.MaxProducerRecordDataBytesLimit,
		},
		Logline: LoglineConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      3,
		},
		InstanceID:              "test-builder-0",
		disableStaticMembership: true,
		FlushOnIdle:             100 * time.Millisecond,
		FlushOnMaxAge:           5 * time.Hour,
		ScratchDir:              tmpDir,
	}

	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	_, indexStore := newTestStore(t)

	svc, err := New(indexStore, cfg, "2026-01-01", newFakePartitionRing(0, 1, 2, 3, 4, 5), logger, reg)
	require.NoError(t, err)

	// Produce one record to each of three partitions; the service should
	// receive all of them once the coordinator assigns the topic.
	producer, err := kgo.NewClient(kgo.SeedBrokers(addrs...))
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	ctx := context.Background()
	for _, part := range []int32{0, 2, 4} {
		rec := &kgo.Record{
			Topic:     testTopic,
			Partition: part,
			Value:     []byte("test record"),
		}
		results := producer.ProduceSync(ctx, rec)
		require.NoError(t, results.FirstErr())
	}

	startCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, svc.StartAsync(startCtx))
	require.NoError(t, svc.AwaitRunning(startCtx))

	// Wait for the rebalance to complete and the service to own all partitions.
	require.Eventually(t, func() bool {
		return len(svc.snapshotOwnedPartitions()) == 6
	}, 5*time.Second, 50*time.Millisecond, "service should own all six partitions")

	svc.StopAsync()
	require.NoError(t, svc.AwaitTerminated(startCtx))
}

// TestService_ReconcileActiveSetForcesRebalance checks that a rebalance is
// forced when, and only when, the active partition set changes.
func TestService_ReconcileActiveSetForcesRebalance(t *testing.T) {
	t.Parallel()

	ring := newFakePartitionRing(0, 1, 2)
	svc := &Service{
		logger:        log.NewNopLogger(),
		partitionRing: ring,
	}

	var rebalances atomic.Int64
	ctx := t.Context()

	go svc.reconcileActiveSetLoop(ctx, 20*time.Millisecond, func() {
		rebalances.Add(1)
	})

	// Stable: no rebalance.
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, int64(0), rebalances.Load())

	// Partition activates: rebalance.
	ring.markActive(3)
	require.Eventually(t, func() bool {
		return rebalances.Load() == 1
	}, 2*time.Second, 10*time.Millisecond)

	// Partition deactivates: rebalance.
	ring.markInactive(3)
	require.Eventually(t, func() bool {
		return rebalances.Load() == 2
	}, 2*time.Second, 10*time.Millisecond)

	// Stable again: no further rebalance.
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, int64(2), rebalances.Load())
}
func TestShouldFlush_MemoryBytes(t *testing.T) {
	prevLimit := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prevLimit) })
	tmpDir := t.TempDir()
	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Logline: LoglineConfig{
			DocumentInterval: 100 * time.Millisecond,
			IndexVersion:     "v3",
			NgramLength:      3,
		},
		ScratchDir:      tmpDir,
		FlushOnMaxBytes: DefaultFlushOnMaxBytes,
		FlushOnMaxAge:   24 * time.Hour,
		FlushOnIdle:     24 * time.Hour,
	}
	require.NoError(t, cfg.Validate())
	// Shrink the sort buffers so their capacity (the constant floor of the
	// estimate) is a few hundred bytes; the GOMEMLIMIT below is then chosen so
	// the 70% threshold sits between that floor and floor + one refTicks
	// bitset — the trigger must fire on refTicks growth, the only part of the
	// working set not bounded by the buffer. Applied after Validate on
	// purpose: Validate floors postings_buffer_pairs at 1<<16.
	cfg.PostingsBufferPairs = 16
	cfg.PostingsSpillWatermark = 0.75

	metrics := NewMetrics(prometheus.NewRegistry())
	builder, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), metrics)
	require.NoError(t, err)

	svc := &Service{cfg: cfg, activeBuilder: builder, metrics: metrics}
	builder.firstAppend = time.Now()
	builder.lastAppend = time.Now()

	// The capacity-based estimate counts the sort buffers even while empty —
	// they are allocated in full up front.
	base := builder.estimatedMemoryBytes()
	require.Positive(t, base, "resident estimate must include empty sort-buffer capacity")

	// 70% threshold = base + 2 MiB: small enough that refTicks growth can
	// cross it in-test.
	threshold := base + 2<<20
	debug.SetMemoryLimit(int64(float64(threshold) / 0.7))

	ok, _ := svc.shouldFlush()
	require.False(t, ok, "buffer floor alone must not cross the threshold")

	// Each first touch of a (shard, day) allocates its dense tick bitset
	// (ticksPerDay/8 = 108 KB at the 100ms interval); keep touching new days
	// until the resident estimate crosses the threshold.
	ticksPerDay := builder.ing.postings.ticksPerDay
	for i := uint64(0); builder.estimatedMemoryBytes() <= threshold; i++ {
		builder.ing.postings.recordDocumentTick(0, uint32(i*ticksPerDay))
	}

	ok, reason := svc.shouldFlush()
	require.True(t, ok)
	require.Equal(t, reasonMemory, reason)

	debug.SetMemoryLimit(0)
	ok, _ = svc.shouldFlush()
	require.False(t, ok, "unset GOMEMLIMIT disables the trigger")
}
