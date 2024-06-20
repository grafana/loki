package ingester

import (
	"context"
	"errors"
	"fmt"
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
	ownedTokenRange, err := s.getTokenRangesForIngester()
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to get token ranges for ingester", "err", err)
		s.resetRingState()
		return
	}

	for _, instance := range s.instancesSupplier() {
		if !instance.limiter.limits.UseOwnedStreamCount(instance.instanceID) {
			continue
		}

		instance.updateOwnedStreams(ownedTokenRange)
	}
}

func (s *recalculateOwnedStreams) updateFixedLimitForAll() {
	for _, instance := range s.instancesSupplier() {
		instance.ownedStreamsSvc.updateFixedLimit()
	}
}

func (s *recalculateOwnedStreams) resetRingState() {
	s.previousRing = ring.ReplicationSet{}
}

func (s *recalculateOwnedStreams) checkRingForChanges() (bool, error) {
	rs, err := s.ingestersRing.GetAllHealthy(ring.WriteNoExtend)
	if err != nil {
		return false, err
	}

	//todo remove
	level.Debug(s.logger).Log("previous ring", fmt.Sprintf("%+v", s.previousRing))
	level.Debug(s.logger).Log("new ring", fmt.Sprintf("%+v", rs))
	ringChanged := ring.HasReplicationSetChangedWithoutStateOrAddr(s.previousRing, rs)
	s.previousRing = rs
	return ringChanged, nil
}

func (s *recalculateOwnedStreams) getTokenRangesForIngester() (ring.TokenRanges, error) {
	ranges, err := s.ingestersRing.GetTokenRangesForInstance(s.ingesterID)
	if err != nil {
		if errors.Is(err, ring.ErrInstanceNotFound) {
			level.Debug(s.logger).Log("msg", "failed to get token ranges for ingester", "ingesterID", s.ingesterID, "err", err)
			return nil, nil
		}
		return nil, err
	}

	return ranges, nil
}
