package rendezvous

import (
	"context"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
)

// PartitionRingWatcher is a Service similar to
// https://github.com/grafana/dskit/blob/main/ring/partition_ring_watcher.go, which watches the KV store for changes in
// the partition ring.
// It maintains an in-memory copy of partition ring membership, and provides a ShuffleSharder with a rendezvous hashing
// implementation as an alternative to the existing shuffle sharding mechanism in dskit.
type PartitionRingWatcher struct {
	services.Service
	kvClient kv.Client
	logger   log.Logger
	config   Config
	sharder  atomic.Pointer[ShuffleSharder]
}

type Config struct {
	HeartbeatTimeout time.Duration `yaml:"heartbeat_timeout" category:"advanced"`
	Key              string        // The Key where membership is stored in the KV store
}

// New creates a new PartitionRingWatcher that watches the given KV Key for ring membership.
func New(config Config, kvClient kv.Client, logger log.Logger) *PartitionRingWatcher {
	s := &PartitionRingWatcher{
		kvClient: kvClient,
		logger:   logger,
		config:   config,
	}
	s.Service = services.NewBasicService(s.starting, s.loop, nil).WithName("rendezvous sharder")
	return s
}

func (s *PartitionRingWatcher) ShuffleSharder() *ShuffleSharder {
	return s.sharder.Load()
}

func (s *PartitionRingWatcher) starting(ctx context.Context) error {
	value, err := s.kvClient.Get(ctx, s.config.Key)
	if err != nil {
		return errors.Wrap(err, "unable to initialise rendezvous sharder state")
	}
	if value != nil {
		s.updateShuffleSharder(value.(*ring.PartitionRingDesc))
	} else {
		level.Info(s.logger).Log("msg", "ring doesn't exist in KV store yet")
	}
	return nil
}

func (s *PartitionRingWatcher) loop(ctx context.Context) error {
	s.kvClient.WatchKey(ctx, s.config.Key, func(value interface{}) bool {
		if value == nil {
			level.Info(s.logger).Log("msg", "ring doesn't exist in KV store yet")
			return true
		}
		s.updateShuffleSharder(value.(*ring.PartitionRingDesc))
		return true
	})
	return nil
}

func (s *PartitionRingWatcher) updateShuffleSharder(ringDesc *ring.PartitionRingDesc) {
	partitions := ringDesc.Partitions
	partitionIDs := make([]int32, 0, len(partitions))
	for _, partition := range partitions {
		if partition.State == ring.PartitionActive &&
			time.Since(time.Unix(partition.StateTimestamp, 0)) <= s.config.HeartbeatTimeout {
			partitionIDs = append(partitionIDs, partition.Id)
		}
	}
	sharder := NewShuffleSharder(partitionIDs)
	s.sharder.Store(&sharder)
}
