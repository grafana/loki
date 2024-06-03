package ingester

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"go.uber.org/atomic"
)

var ownedStreamRingOp = ring.NewOp([]ring.InstanceState{ring.PENDING, ring.JOINING, ring.ACTIVE, ring.LEAVING}, nil)

type ownedStreamService struct {
	services.Service

	logger log.Logger

	instanceID          string
	limiter             *Limiter
	fixedLimit          *atomic.Int32
	ownedStreamCount    int
	notOwnedStreamCount int
	lock                sync.RWMutex

	ingestersRing    ring.ReadRing
	previousRing     ring.ReplicationSet
	ringPollInterval time.Duration
}

func newOwnedStreamService(instanceID string, limiter *Limiter, readRing ring.ReadRing, ringPollInterval time.Duration) *ownedStreamService {
	svc := &ownedStreamService{
		instanceID:       instanceID,
		limiter:          limiter,
		fixedLimit:       atomic.NewInt32(0),
		ingestersRing:    readRing,
		ringPollInterval: ringPollInterval,
	}

	svc.Service = services.NewBasicService(svc.starting, svc.running, svc.stopping)
	svc.updateFixedLimit()
	return svc
}

func (s *ownedStreamService) getOwnedStreamCount() int {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.ownedStreamCount
}

func (s *ownedStreamService) updateFixedLimit() {
	limit, _, _, _ := s.limiter.GetStreamCountLimit(s.instanceID)
	s.fixedLimit.Store(int32(limit))
}

func (s *ownedStreamService) getFixedLimit() int {
	return int(s.fixedLimit.Load())
}

func (s *ownedStreamService) incOwnedStreamCount() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.ownedStreamCount++
}

func (s *ownedStreamService) decOwnedStreamCount() {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.notOwnedStreamCount > 0 {
		s.notOwnedStreamCount--
		return
	}
	s.ownedStreamCount--
}

func (s *ownedStreamService) starting(ctx context.Context) error {
	return nil
}

func (s *ownedStreamService) running(ctx context.Context) error {
	ticker := time.NewTicker(s.ringPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			ringChanged, err := s.checkRingForChanges()
			if err != nil {
				level.Error(s.logger).Log("msg", "failed to check ring for changes", "error", err)
				continue
			}
			if ringChanged {
				s.updateOwnedStreams(ctx)
			}
		}
	}
}

func (s *ownedStreamService) stopping(_ error) error {
	return nil
}

func (s *ownedStreamService) checkRingForChanges() (bool, error) {
	rs, err := s.ingestersRing.GetAllHealthy(ownedStreamRingOp)
	if err != nil {
		return false, err
	}

	ringChanged := ring.HasReplicationSetChangedWithoutStateOrAddr(s.previousRing, rs)
	s.previousRing = rs
	return ringChanged, nil
}

func (s *ownedStreamService) getTokenRangesForTenant() (ring.TokenRanges, error) {
	ranges, err := s.ingestersRing.GetTokenRangesForInstance(s.instanceID)
	if err != nil {
		if errors.Is(err, ring.ErrInstanceNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return ranges, nil
}

func (s *ownedStreamService) ownerKeyAndValue() (string, string) {
	return "ingester", s.instanceID
}

func (s *ownedStreamService) updateOwnedStreams(_ context.Context) {
	ranges, err := s.getTokenRangesForTenant()
	if err != nil {
		k, v := s.ownerKeyAndValue()
		level.Error(s.logger).Log("msg", "failed to get token ranges for tenant", "error", err, k, v)
		return
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	newOwnedStreamCount := len(ranges)
	// todo: fix
	s.notOwnedStreamCount = s.ownedStreamCount - newOwnedStreamCount
	s.ownedStreamCount = newOwnedStreamCount

	s.updateFixedLimit()
	// todo: logging
}
