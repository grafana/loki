package builder

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	"github.com/grafana/loki/v3/pkg/kafkav2"
)

// requiredReplicasReporter exports the number of builder replicas needed to
// consume every partition with unread records.
//
// The builder with ordinal p consumes partition p. So the number is the
// highest partition with unread records plus 1, not a count of partitions.
// It is 0 when no partition has unread records.
//
// The gauge keeps its last value when a refresh fails. It exports nothing
// until the first refresh succeeds, so a restart never exports a false 0.
type requiredReplicasReporter struct {
	services.Service

	offsets       *kafkav2.TopicOffsetReader
	consumerGroup func(partition int32) string
	interval      time.Duration
	logger        log.Logger

	// requiredReplicas has no labels. It is a vector because a vector
	// exports no series until the first Set.
	requiredReplicas *prometheus.GaugeVec
	refreshFailures  prometheus.Counter
}

// newRequiredReplicasReporter returns a reporter that refreshes the gauge once
// every interval. consumerGroup returns the consumer group of the builder
// that consumes the given partition.
func newRequiredReplicasReporter(
	offsets *kafkav2.TopicOffsetReader,
	consumerGroup func(partition int32) string,
	interval time.Duration,
	logger log.Logger,
	reg prometheus.Registerer,
) *requiredReplicasReporter {
	r := &requiredReplicasReporter{
		offsets:       offsets,
		consumerGroup: consumerGroup,
		interval:      interval,
		logger:        logger,
		requiredReplicas: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_dataobj_builder_required_replicas",
			Help: "The number of builder replicas needed to consume every partition with unread records: the highest such partition plus 1, or 0 if there is none. The value stays the same while refreshes fail.",
		}, nil),
		refreshFailures: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_builder_required_replicas_refresh_failures_total",
			Help: "Total number of failed refreshes of loki_dataobj_builder_required_replicas.",
		}),
	}
	r.Service = services.NewBasicService(nil, r.running, nil).WithName("dataobj-builder-required-replicas-reporter")
	return r
}

func (r *requiredReplicasReporter) running(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		if err := r.refresh(ctx); err != nil {
			level.Warn(r.logger).Log("msg", "failed to refresh required replicas", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// refresh reads the offsets of every partition and updates the gauge. It
// leaves the gauge unchanged if it returns an error.
func (r *requiredReplicasReporter) refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, r.interval)
	defer cancel()
	replicas, err := r.readRequiredReplicas(ctx)
	if err != nil {
		r.refreshFailures.Inc()
		return err
	}
	r.requiredReplicas.WithLabelValues().Set(float64(replicas))
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
	// Fetch the committed offsets only for partitions that have records. The
	// other partitions have no unread records, and most of the 1000 default
	// partitions are empty.
	groups := make(map[int32]string)
	for partition, endOffset := range endOffsets {
		if endOffset > startOffsets[partition] {
			groups[partition] = r.consumerGroup(partition)
		}
	}
	resumeOffsets, err := r.offsets.ResumeOffsets(ctx, groups)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch committed offsets: %w", err)
	}
	var replicas int32
	for partition := range groups {
		if recordsLeftToConsume(startOffsets[partition], endOffsets[partition], resumeOffsets[partition]) > 0 {
			replicas = max(replicas, partition+1)
		}
	}
	level.Debug(r.logger).Log("msg", "refreshed required replicas", "required_replicas", replicas, "partitions_with_records", len(groups))
	return replicas, nil
}

// newConsumerGroupFunc returns a function that maps a partition to the
// consumer group of the builder that consumes it.
//
// A builder uses its hostname as its consumer group. The hostname ends with
// the partition of the builder, for example dataobj-builder-3. So the group of
// partition p is instanceID with its partition replaced by p.
//
// The function returns the wrong group for a builder that sets
// Config.PartitionID. That builder keeps its hostname as its group, and the
// hostname does not end with the partition.
func newConsumerGroupFunc(instanceID string) (func(partition int32) string, error) {
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
