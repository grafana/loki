package builder

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	"github.com/grafana/loki/v3/pkg/kafkav2"
)

// requiredReplicasReporter exports the number of builder replicas needed to
// consume every partition with unread records.
type requiredReplicasReporter struct {
	services.Service

	offsets                  *kafkav2.OffsetReader
	partitionToConsumerGroup func(partition int32) string
	interval                 time.Duration
	logger                   log.Logger
	metrics                  *requiredReplicasMetrics
}

// newRequiredReplicasReporter returns a reporter that refreshes the gauge once
// every interval. partitionToConsumerGroup returns the consumer group of the
// builder that consumes the given partition.
func newRequiredReplicasReporter(
	offsets *kafkav2.OffsetReader,
	partitionToConsumerGroup func(partition int32) string,
	interval time.Duration,
	logger log.Logger,
	reg prometheus.Registerer,
) *requiredReplicasReporter {
	r := &requiredReplicasReporter{
		offsets:                  offsets,
		partitionToConsumerGroup: partitionToConsumerGroup,
		interval:                 interval,
		logger:                   logger,
		metrics:                  newRequiredReplicasMetrics(reg),
	}
	r.Service = services.NewTimerService(interval, nil, r.iteration, nil).WithName("dataobj-builder-required-replicas-reporter")
	return r
}

// iteration refreshes the gauge. It never returns an error, because a failed
// refresh must not stop the builder.
func (r *requiredReplicasReporter) iteration(ctx context.Context) error {
	if err := r.refresh(ctx); err != nil && !errors.Is(err, context.Canceled) {
		level.Warn(r.logger).Log("msg", "failed to refresh required replicas", "err", err)
	}
	return nil
}

// refresh reads the offsets of every partition and updates the gauge. It
// leaves the gauge unchanged if it returns an error.
func (r *requiredReplicasReporter) refresh(ctx context.Context) error {
	const minRefreshTimeout = time.Minute
	ctx, cancel := context.WithTimeout(ctx, max(r.interval, minRefreshTimeout))
	defer cancel()
	start := time.Now()
	replicas, err := r.readRequiredReplicas(ctx)
	r.metrics.refreshDuration.WithLabelValues(resultOf(err)).Observe(time.Since(start).Seconds())
	if err != nil {
		return err
	}
	r.metrics.requiredReplicas.WithLabelValues().Set(float64(replicas))
	r.metrics.lastRefresh.SetToCurrentTime()
	return nil
}

func (r *requiredReplicasReporter) readRequiredReplicas(ctx context.Context) (int32, error) {
	// Read the start offsets before the end offsets. Retention only moves the
	// start offset forward, so each start offset is at most its end offset.
	startOffsets, err := r.offsets.StartOffsets(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list start offsets: %w", err)
	}
	endOffsets, err := r.offsets.EndOffsets(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list end offsets: %w", err)
	}
	// Fetch the committed offsets only for partitions that have records,
	// because the topic can have many pre-created partitions that get none.
	// The end offset is the offset after the last record, so a partition with
	// one record at offset 0 has a start offset of 0 and an end offset of 1.
	groups := make(map[int32]string)
	for partition, endOffset := range endOffsets {
		if endOffset > startOffsets[partition] {
			groups[partition] = r.partitionToConsumerGroup(partition)
		}
	}
	lastCommittedOffsets, err := r.offsets.LastCommittedOffsets(ctx, groups)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch committed offsets: %w", err)
	}
	// Builder-0 always counts, because it exports this gauge. A value of 0
	// would scale builder-0 away, and nothing would scale the builders back up.
	replicas := int32(1)
	for partition := range groups {
		// Builders commit the offset of the last record they consumed.
		resumeOffset := lastCommittedOffsets[partition] + 1
		if recordsLeftToConsume(startOffsets[partition], endOffsets[partition], resumeOffset) > 0 {
			replicas = max(replicas, partition+1)
		}
	}
	level.Debug(r.logger).Log("msg", "refreshed required replicas", "required_replicas", replicas, "partitions_with_records", len(groups))
	return replicas, nil
}

// newPartitionToConsumerGroupFunc returns a function that maps a partition to
// the consumer group of the builder that consumes it.
//
// It expects that instanceID ends with "-N", e.g., "dataobj-builder-0"
//
// f := newPartitionToConsumerGroupFunc("dataobj-builder-0")
// f(1), _ == "dataobj-builder-1"
// f(2), _ == "dataobj-builder-2"
func newPartitionToConsumerGroupFunc(instanceID string) (func(partition int32) string, error) {
	partition, err := partitionring.ExtractPartitionID(instanceID)
	if err != nil {
		return nil, err
	}
	prefix, ok := strings.CutSuffix(instanceID, "-"+strconv.Itoa(int(partition)))
	if !ok {
		return nil, fmt.Errorf("%s does not end with its partition ID %d", instanceID, partition)
	}
	return func(partition int32) string {
		return prefix + "-" + strconv.Itoa(int(partition))
	}, nil
}

// recordsLeftToConsume returns the number of records of a partition that its
// consumer group did not consume yet. resumeOffset is the next offset to
// consume.
func recordsLeftToConsume(startOffset, endOffset, resumeOffset int64) int64 {
	// startOffset could be > resumeOffset if the records are removed from the
	// partition due to retention before the consumer group processed and committed them.
	nextOffsetToConsume := max(resumeOffset, startOffset)
	return max(endOffset-nextOffsetToConsume, 0)
}
