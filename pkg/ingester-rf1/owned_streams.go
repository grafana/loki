package ingesterrf1

import (
	"sync"

	"github.com/grafana/dskit/services"
	"go.uber.org/atomic"
)

type ownedStreamService struct {
	services.Service

	tenantID            string
	limiter             *Limiter
	fixedLimit          *atomic.Int32
	ownedStreamCount    int
	notOwnedStreamCount int
	lock                sync.RWMutex
}

func newOwnedStreamService(tenantID string, limiter *Limiter) *ownedStreamService {
	svc := &ownedStreamService{
		tenantID:   tenantID,
		limiter:    limiter,
		fixedLimit: atomic.NewInt32(0),
	}

	svc.updateFixedLimit()
	return svc
}

func (s *ownedStreamService) getOwnedStreamCount() int {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.ownedStreamCount
}

func (s *ownedStreamService) updateFixedLimit() {
	limit, _, _, _ := s.limiter.GetStreamCountLimit(s.tenantID)
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

func (s *ownedStreamService) incNotOwnedStreamCount() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.notOwnedStreamCount++
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

func (s *ownedStreamService) resetStreamCounts() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.ownedStreamCount = 0
	s.notOwnedStreamCount = 0
}
