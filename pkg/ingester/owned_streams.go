package ingester

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

var notOwnedStreamsMetric = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: constants.Loki,
	Name:      "ingester_not_owned_streams",
	Help:      "The total number of not owned streams in memory per tenant.",
}, []string{"tenant"})

type ownedStreamService struct {
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
	notOwnedStreamsMetric.WithLabelValues(s.tenantID).Inc()
	s.notOwnedStreamCount++
}

func (s *ownedStreamService) decOwnedStreamCount() {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.notOwnedStreamCount > 0 {
		notOwnedStreamsMetric.WithLabelValues(s.tenantID).Dec()
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
	notOwnedStreamsMetric.WithLabelValues(s.tenantID).Set(0)
}
