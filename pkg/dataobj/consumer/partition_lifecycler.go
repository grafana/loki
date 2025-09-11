package consumer

import (
	"context"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/twmb/franz-go/pkg/kgo"
)

// processorLifecycler allows mocking of partition processor lifecycler in tests.
type processorLifecycler interface {
	Register(ctx context.Context, client *kgo.Client, topic string, partition int32)
	Deregister(ctx context.Context, topic string, partition int32)
	Stop(ctx context.Context)
}

// partitionLifecycler manages assignment and revocation of partitions.
type partitionLifecycler struct {
	processors processorLifecycler
	logger     log.Logger
	done       bool
	mtx        sync.Mutex
}

// newPartitionLifecycler returns a new partitionLifecycler.
func newPartitionLifecycler(
	processors processorLifecycler,
	logger log.Logger,
) *partitionLifecycler {
	return &partitionLifecycler{
		processors: processors,
		logger:     logger,
	}
}

// Assign implements [kgo.OnPartitionsAssigned].
func (l *partitionLifecycler) Assign(
	ctx context.Context,
	client *kgo.Client,
	topics map[string][]int32,
) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if l.done {
		return
	}
	for topic, partitions := range topics {
		for _, partition := range partitions {
			l.processors.Register(ctx, client, topic, partition)
		}
	}
}

// Revoke implements [kgo.OnPartitionsRevoked].
func (l *partitionLifecycler) Revoke(
	ctx context.Context,
	_ *kgo.Client,
	topics map[string][]int32) {
	for topic, partitions := range topics {
		for _, partition := range partitions {
			l.processors.Deregister(ctx, topic, partition)
		}
	}
}

// Stop shutsdown the lifecycler.
func (l *partitionLifecycler) Stop(ctx context.Context) {
	level.Debug(l.logger).Log("msg", "stopping")
	defer func() { level.Debug(l.logger).Log("msg", "stopped") }()
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.processors.Stop(ctx)
	l.done = true
}
