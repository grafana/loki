package ingester

import (
	"context"
	"errors"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
)

var ownedStreamRingOp = ring.NewOp([]ring.InstanceState{ring.PENDING, ring.JOINING, ring.ACTIVE, ring.LEAVING}, nil)

type recalculateOwnedStreams struct {
	services.Service

	logger log.Logger

	instances        map[string]*instance
	ingesterID       string
	previousRing     ring.ReplicationSet
	ingestersRing    ring.ReadRing
	ringPollInterval time.Duration
	ticker           *time.Ticker
}

func newRecalculateOwnedStreams(instances map[string]*instance, ingesterID string, ring ring.ReadRing, ringPollInterval time.Duration, logger log.Logger) *recalculateOwnedStreams {
	svc := &recalculateOwnedStreams{
		ingestersRing:    ring,
		instances:        instances,
		ringPollInterval: ringPollInterval,
		ingesterID:       ingesterID,
		logger:           logger,
	}
	svc.Service = services.NewBasicService(svc.starting, svc.running, svc.stopping)
	return svc
}

func (s *recalculateOwnedStreams) starting(ctx context.Context) error {
	s.ticker = time.NewTicker(s.ringPollInterval)
	return nil
}

func (s *recalculateOwnedStreams) running(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.ticker.C:
			ringChanged, err := s.checkRingForChanges()
			if err != nil {
				level.Error(s.logger).Log("msg", "failed to check ring for changes", "error", err)
				continue
			}
			if ringChanged {
				ownedTokenRange, err := s.getTokenRangesForIngester()
				if err != nil {
					level.Error(s.logger).Log("msg", "failed to get token ranges for ingester", "error", err)
					continue
				}

				for _, instance := range s.instances {
					err = instance.updateOwnedStreams(ownedTokenRange)
					if err != nil {
						level.Error(s.logger).Log("msg", "failed to update owned streams", "error", err)
					}
				}
			}
		}
	}
}

func (s *recalculateOwnedStreams) stopping(_ error) error {
	if s.ticker != nil {
		s.ticker.Stop()
	}
	return nil
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
