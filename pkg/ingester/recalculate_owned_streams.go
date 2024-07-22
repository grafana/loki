package ingester

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
)

type recalculateOwnedStreams struct {
	services.Service

	logger log.Logger

	instancesSupplier func() []*instance
	ingesterID        string
	previousRing      ring.ReplicationSet
	ingestersRing     ring.ReadRing
	ticker            *time.Ticker
}

func newRecalculateOwnedStreams(instancesSupplier func() []*instance, ingesterID string, ring ring.ReadRing, ringPollInterval time.Duration, logger log.Logger) *recalculateOwnedStreams {
	svc := &recalculateOwnedStreams{
		ingestersRing:     ring,
		instancesSupplier: instancesSupplier,
		ingesterID:        ingesterID,
		logger:            logger,
	}
	svc.Service = services.NewTimerService(ringPollInterval, nil, svc.iteration, nil)
	return svc
}

func (s *recalculateOwnedStreams) iteration(_ context.Context) error {
	s.recalculate()
	return nil
}

func (s *recalculateOwnedStreams) recalculate() {
	level.Info(s.logger).Log("msg", "starting recalculate owned streams job")
	defer func() {
		s.updateFixedLimitForAll()
		level.Info(s.logger).Log("msg", "completed recalculate owned streams job")
	}()
	ringChanged, err := s.checkRingForChanges()
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
		err := instance.updateOwnedStreams(s.ingestersRing, s.ingesterID)
		if err != nil {
			level.Error(s.logger).Log("msg", "failed to re-evaluate streams ownership", "tenant", instance.instanceID, "err", err)
		}
	}
}

func (s *recalculateOwnedStreams) updateFixedLimitForAll() {
	for _, instance := range s.instancesSupplier() {
		oldLimit, newLimit := instance.ownedStreamsSvc.updateFixedLimit()
		if oldLimit != newLimit {
			level.Info(s.logger).Log("msg", "fixed limit has been updated", "tenant", instance.instanceID, "old", oldLimit, "new", newLimit)
		}
	}
}

func (s *recalculateOwnedStreams) checkRingForChanges() (bool, error) {
	rs, err := s.ingestersRing.GetAllHealthy(ring.WriteNoExtend)
	if err != nil {
		return false, err
	}

	ringChanged := ring.HasReplicationSetChangedWithoutStateOrAddr(s.previousRing, rs)
	s.previousRing = rs
	return ringChanged, nil
}
