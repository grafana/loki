package ingester

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"

	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
)

type ownershipStrategy interface {
	checkRingForChanges() (bool, error)
	isOwnedStream(*stream) (bool, error)
}

type recalculateOwnedStreamsSvc struct {
	services.Service

	logger log.Logger

	ownershipStrategy ownershipStrategy
	instancesSupplier func() []*instance
	ticker            *time.Ticker
}

func newRecalculateOwnedStreamsSvc(instancesSupplier func() []*instance, ownershipStrategy ownershipStrategy, ringPollInterval time.Duration, logger log.Logger) *recalculateOwnedStreamsSvc {
	svc := &recalculateOwnedStreamsSvc{
		instancesSupplier: instancesSupplier,
		logger:            logger,
		ownershipStrategy: ownershipStrategy,
	}
	svc.Service = services.NewTimerService(ringPollInterval, nil, svc.iteration, nil)
	return svc
}

func (s *recalculateOwnedStreamsSvc) iteration(_ context.Context) error {
	s.recalculate()
	return nil
}

func (s *recalculateOwnedStreamsSvc) recalculate() {
	level.Info(s.logger).Log("msg", "starting recalculate owned streams job")
	defer func() {
		s.updateFixedLimitForAll()
		level.Info(s.logger).Log("msg", "completed recalculate owned streams job")
	}()
	ringChanged, err := s.ownershipStrategy.checkRingForChanges()
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to check ring for changes", "err", err)
		return
	}
	if !ringChanged {
		level.Debug(s.logger).Log("msg", "ring is not changed, skipping the job")
		return
	}
	level.Info(s.logger).Log("msg", "detected ring changes, re-evaluating streams ownership")

	for _, instance := range s.instancesSupplier() {
		if !instance.limiter.limits.UseOwnedStreamCount(instance.instanceID) {
			continue
		}

		level.Info(s.logger).Log("msg", "updating streams ownership", "tenant", instance.instanceID)
		err := instance.updateOwnedStreams(s.ownershipStrategy.isOwnedStream)
		if err != nil {
			level.Error(s.logger).Log("msg", "failed to re-evaluate streams ownership", "tenant", instance.instanceID, "err", err)
		}
	}
}

func (s *recalculateOwnedStreamsSvc) updateFixedLimitForAll() {
	for _, instance := range s.instancesSupplier() {
		oldLimit, newLimit := instance.ownedStreamsSvc.updateFixedLimit()
		if oldLimit != newLimit {
			level.Info(s.logger).Log("msg", "fixed limit has been updated", "tenant", instance.instanceID, "old", oldLimit, "new", newLimit)
		}
	}
}

type ownedStreamsIngesterStrategy struct {
	logger log.Logger

	ingesterID    string
	previousRing  ring.ReplicationSet
	ingestersRing ring.ReadRing

	descsBufPool sync.Pool
	hostsBufPool sync.Pool
	zoneBufPool  sync.Pool
}

func newOwnedStreamsIngesterStrategy(ingesterID string, ingestersRing ring.ReadRing, logger log.Logger) *ownedStreamsIngesterStrategy {
	return &ownedStreamsIngesterStrategy{
		ingesterID:    ingesterID,
		ingestersRing: ingestersRing,
		logger:        logger,
		descsBufPool: sync.Pool{New: func() interface{} {
			return make([]ring.InstanceDesc, ingestersRing.ReplicationFactor()+1)
		}},
		hostsBufPool: sync.Pool{New: func() interface{} {
			return make([]string, ingestersRing.ReplicationFactor()+1)
		}},
		zoneBufPool: sync.Pool{New: func() interface{} {
			return make([]string, ingestersRing.ZonesCount()+1)
		}},
	}
}

func (s *ownedStreamsIngesterStrategy) checkRingForChanges() (bool, error) {
	rs, err := s.ingestersRing.GetAllHealthy(ring.WriteNoExtend)
	if err != nil {
		return false, err
	}

	ringChanged := ring.HasReplicationSetChangedWithoutStateOrAddr(s.previousRing, rs)
	s.previousRing = rs
	return ringChanged, nil
}

//nolint:staticcheck
func (s *ownedStreamsIngesterStrategy) isOwnedStream(str *stream) (bool, error) {
	descsBuf := s.descsBufPool.Get().([]ring.InstanceDesc)
	hostsBuf := s.hostsBufPool.Get().([]string)
	zoneBuf := s.zoneBufPool.Get().([]string)
	defer func() {
		s.descsBufPool.Put(descsBuf[:0])
		s.hostsBufPool.Put(hostsBuf[:0])
		s.zoneBufPool.Put(zoneBuf[:0])
	}()

	replicationSet, err := s.ingestersRing.Get(lokiring.TokenFor(str.tenant, str.labelsString), ring.WriteNoExtend, descsBuf, hostsBuf, zoneBuf)
	if err != nil {
		return false, fmt.Errorf("error getting replication set for stream %s: %v", str.labelsString, err)
	}
	return s.isOwnedStreamInner(replicationSet, s.ingesterID), nil
}

func (s *ownedStreamsIngesterStrategy) isOwnedStreamInner(replicationSet ring.ReplicationSet, ingesterID string) bool {
	for _, instanceDesc := range replicationSet.Instances {
		if instanceDesc.Id == ingesterID {
			return true
		}
	}
	return false
}

type ownedStreamsPartitionStrategy struct {
	logger log.Logger

	partitionID              int32
	partitionRingWatcher     ring.PartitionRingReader
	previousActivePartitions []int32
	getPartitionShardSize    func(user string) int
}

func newOwnedStreamsPartitionStrategy(partitionID int32, ring ring.PartitionRingReader, getPartitionShardSize func(user string) int, logger log.Logger) *ownedStreamsPartitionStrategy {
	return &ownedStreamsPartitionStrategy{
		partitionID:           partitionID,
		partitionRingWatcher:  ring,
		logger:                logger,
		getPartitionShardSize: getPartitionShardSize,
	}
}

func (s *ownedStreamsPartitionStrategy) checkRingForChanges() (bool, error) {
	// When using partitions ring, we consider ring to be changed if active partitions have changed.
	r := s.partitionRingWatcher.PartitionRing()
	if r.PartitionsCount() == 0 {
		return false, ring.ErrEmptyRing
	}
	// todo(ctovena): We might need to consider partition shard size changes as well.
	activePartitions := r.ActivePartitionIDs()
	ringChanged := !slices.Equal(s.previousActivePartitions, activePartitions)
	s.previousActivePartitions = activePartitions
	return ringChanged, nil
}

func (s *ownedStreamsPartitionStrategy) isOwnedStream(str *stream) (bool, error) {
	subring, err := s.partitionRingWatcher.PartitionRing().ShuffleShard(str.tenant, s.getPartitionShardSize(str.tenant))
	if err != nil {
		return false, fmt.Errorf("failed to get shuffle shard for stream: %w", err)
	}
	partitionForStream, err := subring.ActivePartitionForKey(lokiring.TokenFor(str.tenant, str.labelsString))
	if err != nil {
		return false, fmt.Errorf("failed to find active partition for stream: %w", err)
	}

	return partitionForStream == s.partitionID, nil
}
