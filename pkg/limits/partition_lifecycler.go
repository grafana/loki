package limits

import (
	"context"
	"fmt"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/twmb/franz-go/pkg/kgo"

	kafka_partition "github.com/grafana/loki/v3/pkg/kafka/partition"
)

// partitionLifecycler manages assignment and revocation of partitions.
type partitionLifecycler struct {
	partitionManager *partitionManager
	offsetManager    kafka_partition.OffsetManager
	usage            *usageStore
	activeWindow     time.Duration
	logger           log.Logger

	// Used in tests.
	clock quartz.Clock
}

// newPartitionLifecycler returns a new partitionLifecycler.
func newPartitionLifecycler(
	partitionManager *partitionManager,
	offsetManager kafka_partition.OffsetManager,
	usage *usageStore,
	activeWindow time.Duration,
	logger log.Logger,
) *partitionLifecycler {
	return &partitionLifecycler{
		partitionManager: partitionManager,
		offsetManager:    offsetManager,
		usage:            usage,
		activeWindow:     activeWindow,
		logger:           logger,
		clock:            quartz.NewReal(),
	}
}

// Assign implements kgo.OnPartitionsAssigned.
func (l *partitionLifecycler) Assign(ctx context.Context, _ *kgo.Client, topics map[string][]int32) {
	// We expect the client to just consume one topic.
	// TODO(grobinson): Figure out what to do if this is not the case.
	for _, partitions := range topics {
		l.partitionManager.Assign(ctx, partitions)
		for _, partition := range partitions {
			if err := l.determineStateFromOffsets(ctx, partition); err != nil {
				level.Error(l.logger).Log(
					"msg", "failed to check offsets, partition is ready",
					"partition", partition,
					"err", err,
				)
				l.partitionManager.SetReady(partition)
			}
		}
		return
	}
}

// Revoke implements kgo.OnPartitionsRevoked.
func (l *partitionLifecycler) Revoke(ctx context.Context, _ *kgo.Client, topics map[string][]int32) {
	// We expect the client to just consume one topic.
	// TODO(grobinson): Figure out what to do if this is not the case.
	for _, partitions := range topics {
		l.partitionManager.Revoke(ctx, partitions)
		l.usage.EvictPartitions(partitions)
		return
	}
}

func (l *partitionLifecycler) determineStateFromOffsets(ctx context.Context, partition int32) error {
	logger := log.With(l.logger, "partition", partition)
	// Get the start offset for the partition. This can be greater than zero
	// if a retention period has deleted old records.
	startOffset, err := l.offsetManager.FetchPartitionOffset(
		ctx, partition, kafka_partition.KafkaStartOffset)
	if err != nil {
		return fmt.Errorf("failed to get last produced offset: %w", err)
	}
	// The last produced offset is the next offset after the last produced
	// record. For example, if a partition contains 1 record, then the last
	// produced offset is 1. However, the offset of the last produced record
	// is 0, as offsets start from 0.
	lastProducedOffset, err := l.offsetManager.FetchPartitionOffset(
		ctx, partition, kafka_partition.KafkaEndOffset)
	if err != nil {
		return fmt.Errorf("failed to get last produced offset: %w", err)
	}
	// Get the first offset produced within the window. This can be the same
	// offset as the last produced offset if no records have been produced
	// within that time.
	nextOffset, err := l.offsetManager.NextOffset(ctx, partition, l.clock.Now().Add(-l.activeWindow))
	if err != nil {
		return fmt.Errorf("failed to get next offset: %w", err)
	}
	level.Debug(logger).Log(
		"msg", "fetched offsets",
		"start_offset", startOffset,
		"last_produced_offset", lastProducedOffset,
		"next_offset", nextOffset,
	)
	if startOffset >= lastProducedOffset {
		// The partition has no records. This happens when either the
		// partition has never produced a record, or all records that have
		// been produced have been deleted due to the retention period.
		level.Debug(logger).Log("msg", "no records in partition, partition is ready")
		l.partitionManager.SetReady(partition)
		return nil
	}
	if nextOffset == lastProducedOffset {
		level.Debug(logger).Log("msg", "no records within window size, partition is ready")
		l.partitionManager.SetReady(partition)
		return nil
	}
	// Since we want to fetch all records up to and including the last
	// produced record, we must fetch all records up to and including the
	// last produced offset - 1.
	level.Debug(logger).Log("msg", "partition is replaying")
	l.partitionManager.SetReplaying(partition, lastProducedOffset-1)
	return nil
}
