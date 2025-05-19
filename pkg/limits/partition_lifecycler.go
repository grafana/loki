package limits

import (
	"context"

	"github.com/go-kit/log"
	"github.com/twmb/franz-go/pkg/kgo"
)

// PartitionLifecycler manages assignment and revocation of partitions.
type PartitionLifecycler struct {
	partitionManager *PartitionManager
	usage            *UsageStore
	logger           log.Logger
}

// NewPartitionLifecycler returns a new PartitionLifecycler.
func NewPartitionLifecycler(partitionManager *PartitionManager, usage *UsageStore, logger log.Logger) *PartitionLifecycler {
	return &PartitionLifecycler{
		partitionManager: partitionManager,
		usage:            usage,
		logger:           log.With(logger, "component", "limits.PartitionLifecycler"),
	}
}

// Assign implements kgo.OnPartitionsAssigned.
func (l *PartitionLifecycler) Assign(ctx context.Context, _ *kgo.Client, topics map[string][]int32) {
	// We expect the client to just consume one topic.
	// TODO(grobinson): Figure out what to do if this is not the case.
	for _, partitions := range topics {
		l.partitionManager.Assign(ctx, partitions)
		return
	}
}

// Revoke implements kgo.OnPartitionsRevoked.
func (l *PartitionLifecycler) Revoke(ctx context.Context, _ *kgo.Client, topics map[string][]int32) {
	// We expect the client to just consume one topic.
	// TODO(grobinson): Figure out what to do if this is not the case.
	for _, partitions := range topics {
		l.partitionManager.Revoke(ctx, partitions)
		l.usage.EvictPartitions(partitions)
		return
	}
}
