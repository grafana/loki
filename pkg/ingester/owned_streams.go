package ingester

import "go.uber.org/atomic"

type ownedStreamService struct {
	tenantID   string
	limiter    *Limiter
	fixedLimit *atomic.Int32

	//todo: implement job to recalculate it
	ownedStreamCount *atomic.Int64
}

func newOwnedStreamService(tenantID string, limiter *Limiter) *ownedStreamService {
	svc := &ownedStreamService{
		tenantID:         tenantID,
		limiter:          limiter,
		ownedStreamCount: atomic.NewInt64(0),
		fixedLimit:       atomic.NewInt32(0),
	}
	svc.updateFixedLimit()
	return svc
}

func (s *ownedStreamService) getOwnedStreamCount() int {
	return int(s.ownedStreamCount.Load())
}

func (s *ownedStreamService) updateFixedLimit() {
	limit, _, _, _ := s.limiter.GetStreamCountLimit(s.tenantID)
	s.fixedLimit.Store(int32(limit))
}

func (s *ownedStreamService) getFixedLimit() int {
	return int(s.fixedLimit.Load())
}

func (s *ownedStreamService) incOwnedStreamCount() {
	s.ownedStreamCount.Inc()
}

func (s *ownedStreamService) decOwnedStreamCount() {
	s.ownedStreamCount.Dec()
}
