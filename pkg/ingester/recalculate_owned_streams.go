package ingester

import (
	"context"
	"errors"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
)

var ownedStreamRingOp = ring.NewOp([]ring.InstanceState{ring.PENDING, ring.JOINING, ring.ACTIVE, ring.LEAVING}, nil)

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
	ringChanged, err := s.checkRingForChanges()
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to check ring for changes", "err", err)
		return
	}
	if !ringChanged {
		return
	}
	ownedTokenRange, err := s.getTokenRangesForIngester()
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to get token ranges for ingester", "err", err)
		return
	}

	for _, instance := range s.instancesSupplier() {
		if !instance.limiter.limits.UseOwnedStreamCount(instance.instanceID) {
			continue
		}
		err = instance.updateOwnedStreams(ownedTokenRange)
		if err != nil {
			level.Error(s.logger).Log("msg", "failed to update owned streams", "err", err)
		}
	}
}

func (s *recalculateOwnedStreams) checkRingForChanges() (bool, error) {
	rs, err := s.ingestersRing.GetAllHealthy(ownedStreamRingOp)
	if err != nil {
		return false, err
	}

	ringChanged := ring.HasReplicationSetChangedWithoutStateOrAddr(s.previousRing, rs)
	s.previousRing = rs
	return ringChanged, nil
}

func (s *recalculateOwnedStreams) getTokenRangesForIngester() (ring.TokenRanges, error) {
	ranges, err := s.ingestersRing.GetTokenRangesForInstance(s.ingesterID)
	if err != nil {
		if errors.Is(err, ring.ErrInstanceNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return ranges, nil
}
