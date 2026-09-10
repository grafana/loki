package builder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

const (
	reasonShutdown    = "shutdown"
	reasonRunDisk     = "run_disk"
	reasonMemory      = "memory"
	reasonMaxAge      = "max_age"
	reasonIdleTimeout = "idle_timeout"
	reasonNone        = "none"

	shutdownTimeout = 5 * time.Minute

	// activeSetReconcileInterval is how often a builder checks for active-set
	// changes, capping forced rebalances to one per interval.
	activeSetReconcileInterval = 15 * time.Second
)

// rawRecord holds a Kafka record's raw bytes and metadata, pending decode.
type rawRecord struct {
	value     []byte
	timestamp time.Time
	partition kafka.PartitionID
	offset    kafka.Offset
	tenantID  string
}

// consumerLag returns the per-partition consumption lag in seconds, given the
// last record consumed in the current fetch (or nil if none) and the
// partition's HighWatermark.
//
// Lag is 0 when the consumer has caught up to HEAD (consumed offset + 1 >=
// HighWatermark), regardless of how long ago the last record was produced.
//
// When still behind HEAD, returns the wall-clock age of the latest consumed
// record.
func consumerLag(now time.Time, lastConsumed *kgo.Record, highWatermark int64) float64 {
	if lastConsumed == nil {
		return 0
	}
	if lastConsumed.Offset+1 >= highWatermark {
		return 0
	}
	return now.Sub(lastConsumed.Timestamp).Seconds()
}

// Service implements the dskit Service interface for the logline index builder.
type Service struct {
	services.Service

	cfg     Config
	logger  log.Logger
	metrics *Metrics

	// builderMtx guards activeBuilder, lastConsumedOffsets, pendingFlush,
	// and pendingFlushOffsets. It serialises record processing
	// (processRecordBatch) and builder swaps (swapBuilder) on the poll
	// goroutine with shutdown (stopping) on the dskit service goroutine,
	// and with revoke-time stripping of pending commit sets
	// (onPartitionsLostOrRevoked) against the flush goroutine's commit path.
	builderMtx          sync.Mutex
	activeBuilder       *indexBuilder
	lastConsumedOffsets map[kafka.PartitionID]kafka.Offset // highest offset consumed per partition in the current builder
	pendingFlush        chan struct{}                      // closed by flush goroutine on completion; nil when idle
	// pendingFlushOffsets is the offset snapshot held by an in-flight
	// executeFlush. Nil when idle. Revoke strips partitions from this map
	// under builderMtx so a commit issued after revoke cannot contain them.
	pendingFlushOffsets map[kafka.PartitionID]kafka.Offset

	// queue
	client        *kgo.Client
	decoder       *kafka.Decoder
	partitionRing ring.PartitionRingReader

	// store
	indexStore *store.Store
	minDate    string // earliest date partition to index; from store config

	// ownedPartitions is the current consumer-group partition assignment for
	// this member.
	ownedPartitions atomic.Pointer[[]kafka.PartitionID]
}

// New creates a new builder service with Kafka client and index store.
func New(
	indexStore *store.Store,
	cfg Config,
	minDate string,
	partitionRing ring.PartitionRingReader,
	logger log.Logger,
	reg prometheus.Registerer,
) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid builder settings: %w", err)
	}

	if indexStore == nil {
		return nil, fmt.Errorf("indexStore cannot be nil")
	}

	brokers := cfg.Kafka.ReaderConfig.Address
	if brokers == "" {
		brokers = cfg.Kafka.Address
	}

	_ = level.Info(logger).Log(
		"msg", "initializing builder service",
		"brokers", brokers,
		"instance_id", cfg.InstanceID,
		"consumer_group", cfg.Kafka.ConsumerGroup,
		"topic", cfg.Kafka.Topic,
		"scratch_dir", cfg.ScratchDir,
	)

	metrics := NewMetrics(reg)

	initialBuilder, err := newIndexBuilder(cfg, minDate, logger, metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create initial builder: %w", err)
	}

	decoder, err := kafka.NewDecoder()
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka decoder: %w", err)
	}

	svc := &Service{
		cfg:                 cfg,
		logger:              logger,
		metrics:             metrics,
		activeBuilder:       initialBuilder,
		decoder:             decoder,
		indexStore:          indexStore,
		minDate:             minDate,
		lastConsumedOffsets: make(map[kafka.PartitionID]kafka.Offset),
	}

	if partitionRing == nil {
		return nil, fmt.Errorf("partitionRing cannot be nil")
	}
	svc.partitionRing = partitionRing

	// Initialise to an empty slice so snapshotOwnedPartitions never returns nil
	empty := []kafka.PartitionID{}
	svc.ownedPartitions.Store(&empty)

	kafkaClient, err := svc.createKafkaClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka client: %w", err)
	}
	svc.client = kafkaClient

	svc.Service = services.NewBasicService(svc.starting, svc.running, svc.stopping)

	return svc, nil
}

func (s *Service) snapshotOwnedPartitions() []kafka.PartitionID {
	p := s.ownedPartitions.Load()
	if p == nil {
		return nil
	}
	return *p
}

// storeOwnedPartitions atomically replaces the assignment snapshot.
func (s *Service) storeOwnedPartitions(parts []kafka.PartitionID) {
	s.ownedPartitions.Store(&parts)
}

// onPartitionsAssigned merges the coordinator's assignment delta
// into the owned-partition set.
//
// Signature is dictated by kgo.OnPartitionsAssigned, which passes raw int32
// partition ids; we convert to kafka.PartitionID at this boundary.
func (s *Service) onPartitionsAssigned(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
	rawParts, ok := assigned[s.cfg.Kafka.Topic]
	if !ok || len(rawParts) == 0 {
		return
	}
	parts := toPartitionIDs(rawParts)
	current := s.snapshotOwnedPartitions()
	set := make(map[kafka.PartitionID]struct{}, len(current)+len(parts))
	for _, p := range current {
		set[p] = struct{}{}
	}
	for _, p := range parts {
		set[p] = struct{}{}
	}
	next := setToSortedSlice(set)

	// Drop any stale offset for newly-assigned partitions *before*
	// publishing ownership. kgo restarts consumption from the committed
	// offset on assignment, so a leftover entry is stale by definition
	// (typical source: a PollRecords batch already in hand when revoke
	// fired, which processRecordBatch re-adds). Delete-before-publish
	// closes a tiny window where swapBuilder could move the stale entry
	// into pendingFlushOffsets after ownership is visible to the
	// owned-filter. Cooperative assign callbacks only deliver the delta,
	// so continuously-owned partitions are untouched.
	s.builderMtx.Lock()
	for _, p := range parts {
		delete(s.lastConsumedOffsets, p)
		if s.pendingFlushOffsets != nil {
			delete(s.pendingFlushOffsets, p)
		}
	}
	s.builderMtx.Unlock()
	s.storeOwnedPartitions(next)

	level.Info(s.logger).Log(
		"msg", "partitions assigned",
		"added", fmt.Sprintf("%v", parts),
		"owned", fmt.Sprintf("%v", next),
		"owned_count", len(next),
	)
}

// onPartitionsLostOrRevoked removes the lost/revoked partitions from the
// owned set and strips them from every pending commit set.
//
// This is the client-side fence against zombie offset commits: the broker
// (classic protocol) accepts OffsetCommit for any partition as long as
// member ID + generation are valid, so a background flush that still holds
// a revoked partition in its snapshot can rewind the new owner's offset.
// Stripping here closes that race — the revoke callback blocks the
// rebalance, and franz-go serialises CommitOffsetsSync against join/sync,
// so a commit issued before this returns still legitimately owns the
// partition (old generation, ordered before the new owner's offset fetch),
// and a commit issued after can no longer contain it.
//
// Signature is dictated by kgo.OnPartitionsRevoked / kgo.OnPartitionsLost.
func (s *Service) onPartitionsLostOrRevoked(_ context.Context, _ *kgo.Client, lost map[string][]int32) {
	rawParts, ok := lost[s.cfg.Kafka.Topic]
	if !ok || len(rawParts) == 0 {
		return
	}
	parts := toPartitionIDs(rawParts)
	current := s.snapshotOwnedPartitions()
	set := make(map[kafka.PartitionID]struct{}, len(current))
	for _, p := range current {
		set[p] = struct{}{}
	}
	for _, p := range parts {
		delete(set, p)
		// DeleteLabelValues is a no-op when the label series doesn't
		// exist, so revoking a partition we never owned is harmless.
		label := p.String()
		s.metrics.consumptionLagSeconds.DeleteLabelValues(label)
		s.metrics.bytesReceivedTotal.DeleteLabelValues(label)
	}
	next := setToSortedSlice(set)
	s.storeOwnedPartitions(next)

	// Abandon commit eligibility for revoked partitions. Shared mutex with
	// commitOffsets so the flush goroutine cannot observe a half-stripped map.
	s.builderMtx.Lock()
	for _, p := range parts {
		delete(s.lastConsumedOffsets, p)
		if s.pendingFlushOffsets != nil {
			delete(s.pendingFlushOffsets, p)
		}
	}
	s.builderMtx.Unlock()

	level.Info(s.logger).Log(
		"msg", "partitions revoked",
		"removed", fmt.Sprintf("%v", parts),
		"owned", fmt.Sprintf("%v", next),
		"owned_count", len(next),
	)
}

// toPartitionIDs converts a slice of raw kgo partition ids into typed
// PartitionIDs. The underlying integer types match so this is a copy with
// no per-element cost beyond the cast.
func toPartitionIDs(raw []int32) []kafka.PartitionID {
	out := make([]kafka.PartitionID, len(raw))
	for i, p := range raw {
		out[i] = kafka.PartitionID(p)
	}
	return out
}

// setToSortedSlice returns the keys of set as an ascending-sorted slice.
// Used to keep the owned-partition snapshot canonical.
func setToSortedSlice(set map[kafka.PartitionID]struct{}) []kafka.PartitionID {
	out := make([]kafka.PartitionID, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// isMembershipErr reports whether err is one of the broker error codes that
// mean "this offset commit was rejected because consumer-group membership
// changed". Used to label flushErrorsTotal{commit_offsets_membership};
// the error still propagates to the caller.
func isMembershipErr(err error) bool {
	if err == nil {
		return false
	}
	var ke *kerr.Error
	if !errors.As(err, &ke) {
		return false
	}
	switch ke {
	case kerr.RebalanceInProgress,
		kerr.IllegalGeneration,
		kerr.UnknownMemberID,
		kerr.FencedInstanceID,
		kerr.UnknownTopicOrPartition:
		return true
	}
	return false
}

func (s *Service) starting(ctx context.Context) error {
	owned := s.snapshotOwnedPartitions()
	_ = level.Info(s.logger).Log(
		"msg", "service starting",
		"owned_partitions", fmt.Sprintf("%v", owned),
		"owned_count", len(owned),
		"consumer_group", s.cfg.Kafka.ConsumerGroup,
		"topic", s.cfg.Kafka.Topic,
		"scratch_dir", s.cfg.ScratchDir,
	)
	if err := s.wipeScratchDir(); err != nil {
		return err
	}
	// Block until the partition ring has at least one partition before
	// we transition to Running. Without this gate, the first JoinGroup
	// could land while the ring is still empty and the active-sticky balancer
	// would refuse to assign anything silently stranding the consumer group.
	if err := waitForPartitions(ctx, s.partitionRing, s.cfg.WaitRingPopulatedTimeout, s.logger); err != nil {
		return fmt.Errorf("partition ring not ready: %w", err)
	}
	return nil
}

// waitForPartitions polls partitionRing until it reports at least one
// partition or the deadline expires.
func waitForPartitions(ctx context.Context, r ring.PartitionRingReader, timeout time.Duration, logger log.Logger) error {
	if r.PartitionRing().PartitionsCount() > 0 {
		return nil
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()

	_ = level.Info(logger).Log("msg", "waiting for partition ring to be populated", "timeout", timeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("partition ring did not become populated within %s "+
				"(check ring.key, ring.memberlist.cluster_label, and ring.memberlist.join_members)", timeout)
		case <-tick.C:
			if c := r.PartitionRing().PartitionsCount(); c > 0 {
				_ = level.Info(logger).Log("msg", "partition ring populated", "partitions", c)
				return nil
			}
		}
	}
}

// wipeScratchDir creates the scratch directory (if absent) and removes any
// orphaned .lidx files left by a previous process run.
func (s *Service) wipeScratchDir() error {
	if err := os.MkdirAll(s.cfg.ScratchDir, 0755); err != nil {
		return fmt.Errorf("failed to create scratch directory: %w", err)
	}
	entries, err := os.ReadDir(s.cfg.ScratchDir)
	if err != nil {
		return fmt.Errorf("failed to read scratch directory: %w", err)
	}
	for _, entry := range entries {
		path := filepath.Join(s.cfg.ScratchDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("failed to remove %s: %w", path, err)
		}
	}
	return nil
}

func (s *Service) running(ctx context.Context) error {
	owned := s.snapshotOwnedPartitions()
	level.Info(s.logger).Log("msg", "service running",
		"owned_partitions", fmt.Sprintf("%v", owned),
		"owned_count", len(owned))

	// Periodic flush check runs independently of the poll loop so that
	// idle/age thresholds fire even when PollFetches blocks or errors.
	flushTicker := time.NewTicker(s.cfg.FlushCheckInterval)
	defer flushTicker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-flushTicker.C:
				s.flushAndCommit(false)
			}
		}
	}()

	// The balancer runs only on a Kafka rebalance; force one when the ring's
	// active set changes so it re-runs.
	go s.reconcileActiveSet(ctx)

	var records []rawRecord

	for ctx.Err() == nil {
		s.waitForFlushBackpressure(ctx)
		if ctx.Err() != nil {
			break
		}

		fetches := s.client.PollFetches(ctx)

		if fetches.IsClientClosed() {
			level.Error(s.logger).Log("msg", "kafka client closed unexpectedly, exiting running loop")
			return fmt.Errorf("kafka client closed unexpectedly")
		}

		if ctx.Err() != nil {
			level.Info(s.logger).Log("msg", "service context canceled, exiting running loop", "err", ctx.Err())
			break
		}

		fetches.EachError(func(topic string, partition int32, err error) {
			level.Warn(s.logger).Log("msg", "kafka poll error", "topic", topic, "partition", partition, "err", err)
		})

		consumptionStart := time.Now()
		records = records[:0]

		// Reset lag for all owned partitions before processing. The
		// EachPartition callback below overwrites with the real value for
		// partitions that have records in this fetch; partitions that received
		// nothing stay at 0 instead of freezing at a stale value from a
		// previous iteration.
		for _, pid := range s.snapshotOwnedPartitions() {
			s.metrics.consumptionLagSeconds.WithLabelValues(pid.String()).Set(0)
		}

		fetches.EachPartition(func(ftp kgo.FetchTopicPartition) {
			partition := kafka.PartitionID(ftp.Partition).String()
			var lastConsumed *kgo.Record
			for i := range ftp.Records {
				r := ftp.Records[i]
				if len(r.Value) == 0 {
					continue
				}
				s.metrics.bytesReceivedTotal.WithLabelValues(partition).Add(float64(len(r.Value)))
				records = append(records, rawRecord{
					value:     r.Value,
					timestamp: r.Timestamp,
					partition: kafka.PartitionID(r.Partition),
					offset:    kafka.Offset(r.Offset),
					tenantID:  string(r.Key),
				})
				lastConsumed = r
			}
			s.metrics.consumptionLagSeconds.WithLabelValues(partition).Set(
				consumerLag(time.Now(), lastConsumed, ftp.HighWatermark),
			)
		})

		if len(records) > 0 {
			if err := s.processRecordBatch(records); err != nil {
				// Decode errors should never happen against trusted upstream
				// producers; surface them by failing the service so the
				// orchestrator restarts the pod and the regression is visible.
				return err
			}
		}

		s.metrics.consumptionDuration.Observe(time.Since(consumptionStart).Seconds())
		s.flushAndCommit(false)
	}

	level.Info(s.logger).Log("msg", "running loop exiting", "err", ctx.Err())
	return nil
}

func (s *Service) reconcileActiveSet(ctx context.Context) {
	forceRebalance := func() {
		if s.client != nil {
			s.client.ForceRebalance()
		}
	}
	s.reconcileActiveSetLoop(ctx, activeSetReconcileInterval, forceRebalance)
}

// reconcileActiveSetLoop forces a rebalance when the active partition set
// changes so the balancer re-runs. ForceRebalance only acts on the leader, but
// every member runs this against the same ring, so the leader fires.
func (s *Service) reconcileActiveSetLoop(ctx context.Context, interval time.Duration, forceRebalance func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	last := s.partitionRing.PartitionRing().ActivePartitionIDs()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current := s.partitionRing.PartitionRing().ActivePartitionIDs()
			if slices.Equal(current, last) {
				continue
			}
			level.Info(s.logger).Log("msg", "active partition set changed, forcing consumer-group rebalance")
			last = current
			forceRebalance()
		}
	}
}

func (s *Service) stopping(stoppingErr error) error {
	level.Info(s.logger).Log("msg", "service stopping", "err", stoppingErr)
	defer s.client.Close()
	// The partition ring service is owned by the caller — see Service.starting.

	// Wait for any in-flight flush without holding builderMtx: commitOffsets
	// now takes that lock, so awaiting under it would deadlock until
	// shutdownTimeout (and stall revoke callbacks for the same reason).
	if !s.awaitPendingFlush(shutdownTimeout) {
		level.Error(s.logger).Log("msg", "timed out waiting for in-progress flush, skipping final flush")
		return fmt.Errorf("timed out waiting for in-progress flush to complete")
	}

	// Grab the current builder for a final flush before shutting down.
	var lastBuilder *indexBuilder
	var lastOffsets map[kafka.PartitionID]kafka.Offset
	err := func() error {
		s.builderMtx.Lock()
		defer s.builderMtx.Unlock()

		var err error
		lastBuilder, lastOffsets, err = s.swapBuilder()
		if err != nil {
			level.Error(s.logger).Log("msg", "failed to swap builder during shutdown", "err", err)
			return fmt.Errorf("failed to swap builder during shutdown: %w", err)
		}

		return nil
	}()
	if err != nil {
		return err
	}

	// Final synchronous flush gets its own full timeout budget.
	level.Info(s.logger).Log("msg", "running final flush before shutdown")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	s.executeFlush(ctx, lastBuilder, lastOffsets, reasonShutdown, make(chan struct{}))

	level.Info(s.logger).Log("msg", "service stopped")
	return err
}

// processRecordBatch decodes and indexes a batch of raw Kafka records.
// The builderMtx is held for the duration to serialise record processing
// with the builder swap that happens in flushAndCommit.
//
// A decode failure aborts the batch and is returned to the caller. The bad
// record's offset is intentionally not advanced — running() will surface the
// error, the service will fail, and the restarted pod will re-encounter the
// same record so the failure stays visible until upstream is fixed.
func (s *Service) processRecordBatch(records []rawRecord) error {
	s.builderMtx.Lock()
	defer s.builderMtx.Unlock()

	for _, rec := range records {
		stream, parsedLabels, err := s.decoder.Decode(rec.value)
		if err != nil {
			s.metrics.decodeErrorsTotal.Inc()
			return fmt.Errorf("failed to decode protobuf stream (partition=%d offset=%d size=%d): %w",
				rec.partition, rec.offset, len(rec.value), err)
		}
		// processStream returns an error only when spilling a sorted run to
		// scratch disk fails (e.g. volume full) — non-recoverable I/O; the pod
		// must restart and re-consume from the last committed offset. Bail
		// without advancing offsets so the failure stays visible. The recordRef
		// is read only if the out-of-window panic fires, attributing the crash
		// to this record.
		ref := recordRef{valid: true, partition: rec.partition, offset: rec.offset, tenantID: rec.tenantID}
		if err := s.activeBuilder.processStream(&stream, &parsedLabels, rec.timestamp, ref); err != nil {
			return fmt.Errorf("run spill failed (partition=%d offset=%d): %w",
				rec.partition, rec.offset, err)
		}
	}

	// Partition offsets are independent sequences; track per-partition max so each
	// partition's committed offset advances independently. The !ok guard ensures
	// offset 0 is stored correctly on the first record (0 > 0 is false without it).
	for _, rec := range records {
		if current, ok := s.lastConsumedOffsets[rec.partition]; !ok || rec.offset > current {
			s.lastConsumedOffsets[rec.partition] = rec.offset
		}
	}
	return nil
}

// hasPendingFlush reports whether a background flush is still in progress.
// If the flush has completed, it clears pendingFlush / pendingFlushOffsets
// and returns false.
// Caller must hold builderMtx.
func (s *Service) hasPendingFlush() bool {
	if s.pendingFlush == nil {
		return false
	}
	select {
	case <-s.pendingFlush:
		s.pendingFlush = nil
		s.pendingFlushOffsets = nil
		return false
	default:
		return true
	}
}

// waitForFlushBackpressure blocks the poll loop when a background flush is
// in progress AND the current builder has already hit a flush trigger. Without
// this, the poll loop consumes unbounded data into the fresh builder while the
// old builder's flush is still uploading
func (s *Service) waitForFlushBackpressure(ctx context.Context) {
	s.builderMtx.Lock()
	if !s.hasPendingFlush() {
		s.builderMtx.Unlock()
		return
	}
	shouldFlush, _ := s.shouldFlush()
	if !shouldFlush {
		s.builderMtx.Unlock()
		return
	}

	// Grab the channel reference before releasing the lock so we can
	// block without holding builderMtx (which the flush goroutine needs).
	pending := s.pendingFlush
	s.builderMtx.Unlock()

	level.Warn(s.logger).Log("msg", "backpressure: blocking consumption until pending flush completes")

	start := time.Now()
	select {
	case <-pending:
	case <-ctx.Done():
	}
	s.metrics.flushBackpressureSeconds.Add(time.Since(start).Seconds())
}

// awaitPendingFlush blocks until the in-progress flush completes or the
// timeout elapses. Returns true if the flush completed (or there was none),
// false on timeout. Clears pendingFlush / pendingFlushOffsets on completion.
//
// Must NOT be called while holding builderMtx: the flush goroutine's
// commitOffsets acquires that lock, so waiting under it deadlocks. Mirrors
// waitForFlushBackpressure — take the channel ref under the lock, wait
// unlocked, then re-lock to clear state.
//
// Only called from stopping(), after running() has returned and the poll
// goroutine is gone.
func (s *Service) awaitPendingFlush(timeout time.Duration) bool {
	s.builderMtx.Lock()
	pending := s.pendingFlush
	s.builderMtx.Unlock()

	if pending == nil {
		return true
	}

	level.Debug(s.logger).Log("msg", "waiting for in-progress flush during shutdown")

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-pending:
		s.builderMtx.Lock()
		// Clear only if this is still the flush we waited on. (No concurrent
		// flushAndCommit after running() returns, but be precise.)
		if s.pendingFlush == pending {
			s.pendingFlush = nil
			s.pendingFlushOffsets = nil
		}
		s.builderMtx.Unlock()
		return true
	case <-timer.C:
		return false
	}
}

// swapBuilder replaces the active builder and offsets with fresh instances,
// returning the old ones for flushing. Caller must hold builderMtx.
func (s *Service) swapBuilder() (*indexBuilder, map[kafka.PartitionID]kafka.Offset, error) {
	oldBuilder := s.activeBuilder
	oldOffsets := s.lastConsumedOffsets

	freshBuilder, err := newIndexBuilder(s.cfg, s.minDate, s.logger, s.metrics)
	if err != nil {
		return nil, nil, err
	}
	s.activeBuilder = freshBuilder
	s.lastConsumedOffsets = make(map[kafka.PartitionID]kafka.Offset, len(s.snapshotOwnedPartitions()))

	return oldBuilder, oldOffsets, nil
}

// flushAndCommit checks flush triggers and, if a flush is needed, swaps the active
// builder and launches a background flush goroutine. It returns immediately
// after the swap -- all I/O happens off the hot path.
//
// When force is true the trigger check is skipped and a flush is always attempted.
func (s *Service) flushAndCommit(force bool) {
	s.builderMtx.Lock()
	defer s.builderMtx.Unlock()

	reason := reasonNone
	if !force {
		var shouldFlush bool
		if shouldFlush, reason = s.shouldFlush(); !shouldFlush {
			return
		}
	}

	if s.hasPendingFlush() {
		s.metrics.flushBackpressuredTotal.Inc()
		level.Info(s.logger).Log("msg", "flush in progress, deferring", "reason", reason)
		return
	}

	if reason == reasonNone {
		reason = "forced"
	}
	level.Info(s.logger).Log("msg", "starting flush", "reason", reason, "buckets", s.activeBuilder.bucketCount())

	oldBuilder, offsetsToCommit, err := s.swapBuilder()
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to create fresh builder, skipping flush", "err", err)
		return
	}

	// Launch background flush of the old builder. pendingFlushOffsets aliases
	// the same map executeFlush will commit from, so revoke can strip keys
	// out from under an in-flight flush.
	done := make(chan struct{})
	s.pendingFlush = done
	s.pendingFlushOffsets = offsetsToCommit
	go s.executeFlush(context.Background(), oldBuilder, offsetsToCommit, reason, done)
}

// executeFlush runs the full flush pipeline for the given builder.
func (s *Service) executeFlush(ctx context.Context, builder *indexBuilder, lastConsumedOffsets map[kafka.PartitionID]kafka.Offset, reason string, done chan struct{}) {
	defer close(done)

	retryTilSuccess := func(step string, retry func(ctx context.Context) error) bool {
		stepStart := time.Now()
		b := backoff.New(ctx, backoff.Config{
			MinBackoff: 1 * time.Second,
			MaxBackoff: 10 * time.Second,
		})
		for b.Ongoing() {
			err := retry(ctx)
			if err == nil {
				level.Info(s.logger).Log("msg", "flush step succeeded", "step", step, "duration", time.Since(stepStart))
				return true
			}
			s.metrics.flushErrorsTotal.WithLabelValues(step).Inc()
			level.Error(s.logger).Log("msg", "flush failed", "step", step, "err", err)
			b.Wait()
		}
		level.Warn(s.logger).Log("msg", "flush abandoned, clearing builder to avoid stale state", "step", step)
		return false
	}

	start := time.Now()
	var files []fileInfo
	if !retryTilSuccess("prepare_flush_files", func(_ context.Context) error {
		var err error
		files, err = builder.prepareIndexes()
		return err
	}) {
		builder.clear()
		return
	}

	if len(files) == 0 {
		// there is no data in this flush cycle. Still attempt an offset commit:
		// we may have been drawing down the queue without producing index
		// data (pre-minDate records, empty protos, zero ngrams; records outside
		// the docID window panic at ingest rather than being dropped).
		// commitOffsets is a no-op when the (possibly revoke-stripped) map is
		// empty — do not len()-guard here; that would race with revoke's
		// concurrent delete on the aliased pendingFlushOffsets map.
		level.Info(s.logger).Log("msg", "no index files produced, committing offsets")
		retryTilSuccess("commit_offsets", func(ctx context.Context) error {
			return s.commitOffsets(ctx, lastConsumedOffsets)
		})
		builder.clear()
		return
	}

	if !retryTilSuccess("upload_flush_files", func(ctx context.Context) error {
		return s.uploadPartialIndexes(ctx, files)
	}) {
		builder.clear()
		return
	}

	// Commit exactly the offset snapshotted at swap time. Records consumed
	// into the new active builder since the swap are NOT committed here; they will
	// be committed by the next flush cycle.
	//
	// On-disk cleanup happens after upload succeeds (in builder.clear below, or
	// in the abandonment paths above). Deleting inside the upload goroutines
	// would race with retryTilSuccess: a partial success deletes some files,
	// then the retry fails reading them.
	if !retryTilSuccess("commit_offsets", func(ctx context.Context) error {
		return s.commitOffsets(ctx, lastConsumedOffsets)
	}) {
		// Data was uploaded but not committed -- at-least-once invariant
		// preserved: a restarted instance re-consumes from the last commit and
		// re-uploads the same data under FRESH storageIDs (they are random per
		// cycle), so cross-restart duplicates are expected and are collapsed by
		// downstream compaction. The Exists() check in uploadPartialIndexes only
		// dedupes upload retries within this process, not across restarts.
		builder.clear()
		return
	}

	bucketCount := builder.bucketCount()
	builder.clear()

	flushDuration := time.Since(start)
	s.metrics.flushDuration.Observe(flushDuration.Seconds())
	s.metrics.flushesTotal.WithLabelValues(reason).Inc()

	level.Info(s.logger).Log(
		"msg", "flush complete",
		"reason", reason,
		"buckets", bucketCount,
		"files", len(files),
		"duration", flushDuration,
	)
}

// commitOffsets commits the next-to-consume offset for every currently-owned
// partition that advanced in this cycle via the kgo consumer-group client.
// This persists our position so a restarted pod resumes from the right place.
// Partitions that received no records in this cycle are skipped — their
// committed offset remains unchanged.
//
// Defense in depth against zombie commits: even after revoke strips the
// shared maps, we filter to snapshotOwnedPartitions immediately before
// building the CommitOffsetsSync request so a revoked partition can never
// leave this process in an OffsetCommit.
//
// All owned partitions are committed in a single CommitOffsets call. On
// failure the retry loop recommits; Kafka offset commits are idempotent.
// Membership errors are NOT soft-skipped — they propagate so the flush
// step is not logged as success while offsets remain uncommitted.
func (s *Service) commitOffsets(ctx context.Context, lastConsumedOffsets map[kafka.PartitionID]kafka.Offset) error {
	s.builderMtx.Lock()
	owned := s.snapshotOwnedPartitions()
	ownedSet := make(map[kafka.PartitionID]struct{}, len(owned))
	for _, p := range owned {
		ownedSet[p] = struct{}{}
	}

	parts := make(map[int32]kgo.EpochOffset, len(lastConsumedOffsets))
	var skipped []kafka.PartitionID
	for partID, lastOffset := range lastConsumedOffsets {
		if _, ok := ownedSet[partID]; !ok {
			skipped = append(skipped, partID)
			continue
		}
		// Kafka convention: commit the next offset to consume, not the last consumed.
		parts[int32(partID)] = kgo.EpochOffset{Offset: int64(lastOffset) + 1, Epoch: -1}
		level.Debug(s.logger).Log("msg", "committing offset",
			"partition", partID,
			"offset", int64(lastOffset)+1,
		)
	}
	s.builderMtx.Unlock()

	if len(skipped) > 0 {
		slices.Sort(skipped)
		level.Info(s.logger).Log(
			"msg", "skipping offset commit for partitions no longer owned",
			"partitions", fmt.Sprintf("%v", skipped),
		)
	}
	if len(parts) == 0 {
		return nil
	}

	offsets := map[string]map[int32]kgo.EpochOffset{
		s.cfg.Kafka.Topic: parts,
	}

	var commitErr error
	s.client.CommitOffsetsSync(ctx, offsets,
		func(_ *kgo.Client, _ *kmsg.OffsetCommitRequest, resp *kmsg.OffsetCommitResponse, err error) {
			if err != nil {
				if isMembershipErr(err) {
					s.metrics.flushErrorsTotal.WithLabelValues("commit_offsets_membership").Inc()
				}
				commitErr = fmt.Errorf("offsets NOT committed for partitions %v: %w",
					partitionIDsFromEpochOffsets(parts), err)
				return
			}
			for _, topic := range resp.Topics {
				for _, p := range topic.Partitions {
					if p.ErrorCode == 0 {
						continue
					}
					perr := kerr.ErrorForCode(p.ErrorCode)
					if isMembershipErr(perr) {
						s.metrics.flushErrorsTotal.WithLabelValues("commit_offsets_membership").Inc()
					}
					commitErr = fmt.Errorf("offsets NOT committed for partitions %v: %w",
						[]kafka.PartitionID{kafka.PartitionID(p.Partition)}, perr)
					return
				}
			}
		},
	)
	return commitErr
}

// partitionIDsFromEpochOffsets returns the partition keys of parts as a
// sorted slice, for error messages that must name every uncommitted partition.
func partitionIDsFromEpochOffsets(parts map[int32]kgo.EpochOffset) []kafka.PartitionID {
	out := make([]kafka.PartitionID, 0, len(parts))
	for p := range parts {
		out = append(out, kafka.PartitionID(p))
	}
	slices.Sort(out)
	return out
}

// shouldFlush returns true and a reason if any flush trigger is met.
// Must be called under builderMtx.
func (s *Service) shouldFlush() (bool, string) {
	b := s.activeBuilder

	if b.firstAppend.IsZero() {
		return false, ""
	}

	now := time.Now()

	// Disk-usage trigger. Scoped to the active builder only — a swapped-out
	// builder under flush owns its own, now-retired run files, whose disk
	// usage is not counted here. That's what makes this safe to evaluate
	// during a pending flush: the active builder's counter starts at zero on
	// swap and climbs only as the fresh cycle spills new runs. No "counter
	// stays over threshold for the whole cleanup window, stalls the consumer"
	// failure mode.
	totalDisk := b.runDiskBytes()
	s.metrics.runDiskBytes.Set(float64(totalDisk))
	if totalDisk >= s.cfg.FlushOnMaxBytes {
		return true, reasonRunDisk
	}

	totalEst := b.estimatedMemoryBytes()
	s.metrics.estimatedMemoryBytes.Set(float64(totalEst))
	if threshold := memoryFlushThresholdBytes(); threshold > 0 && totalEst >= threshold {
		return true, reasonMemory
	}

	if now.Sub(b.firstAppend) >= s.cfg.FlushOnMaxAge {
		return true, reasonMaxAge
	}

	if now.Sub(b.lastAppend) >= s.cfg.FlushOnIdle {
		return true, reasonIdleTimeout
	}

	return false, ""
}

// uploadPartialIndexes uploads the cycle's .lidx files to the index store.
// Each file is written as a store index with metadata derived from the
// builder's accumulation cycle. StorageIDs are pre-generated by the caller
// (stored in fileInfo.storageID) so retries reuse the same object paths.
//
// The read handles in fileInfo.file are owned by the producing indexBuilder;
// this function only reads from them (seeking to 0 at each attempt so retries
// re-read from the start). builder.clear closes the handles and removes the
// files.
func (s *Service) uploadPartialIndexes(ctx context.Context, files []fileInfo) error {
	g, ctx := errgroup.WithContext(ctx)

	for _, f := range files {

		g.Go(func() error {
			uploadStart := time.Now()

			if _, err := f.file.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("failed to seek index file: %w", err)
			}

			meta := store.Meta{
				StorageID:        f.storageID,
				Date:             f.date,
				Version:          s.cfg.Logline.IndexVersion,
				MinLogTs:         f.minLogTs,
				MaxLogTs:         f.maxLogTs,
				MinRecordTs:      f.minEnqueuedTime,
				MaxRecordTs:      f.maxEnqueuedTime,
				ShardCount:       s.cfg.Logline.ShardCount,
				ShardAlgorithm:   s.cfg.Logline.ShardAlgorithm,
				ShardValue:       f.shardValue,
				DocumentInterval: s.cfg.Logline.DocumentInterval,
			}
			if err := meta.SetFileInfo(f.file); err != nil {
				return fmt.Errorf("failed to populate file info: %w", err)
			}
			header, err := logline.ReadHeaderAt(f.file, meta.SizeBytes)
			if err != nil {
				return fmt.Errorf("failed to read index header: %w", err)
			}
			meta.IndexHeader = &header

			exists, err := s.indexStore.IndexExists(ctx, meta)
			if err != nil {
				return fmt.Errorf("failed to check index existence: %w", err)
			}
			if exists {
				level.Info(s.logger).Log("msg", "index already exists, skipping upload", "path", meta.IndexPath())
				s.metrics.uploadDuration.Observe(time.Since(uploadStart).Seconds())
				return nil
			}

			if err := s.indexStore.PutIndex(ctx, f.file, meta); err != nil {
				return fmt.Errorf("failed to upload index: %w", err)
			}

			level.Info(s.logger).Log(
				"msg", "uploaded index",
				"path", meta.IndexPath(),
				"date", f.date,
				"min_log_ts", f.minLogTs.Format(time.RFC3339),
				"max_log_ts", f.maxLogTs.Format(time.RFC3339),
				"min_rec_ts", f.minEnqueuedTime.Format(time.RFC3339),
				"max_rec_ts", f.maxEnqueuedTime.Format(time.RFC3339),
				"duration", time.Since(uploadStart),
			)
			s.metrics.uploadDuration.Observe(time.Since(uploadStart).Seconds())
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("partial upload failure: %w", err)
	}
	return nil
}
