package ingester

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

var notOwnedStreamsMetric = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: constants.Loki,
	Name:      "ingester_not_owned_streams",
	Help:      "The total number of not owned streams in memory per tenant.",
}, []string{"tenant"})

type ownedStreamService struct {
	tenantID         string
	limiter          *Limiter
	fixedLimit       *atomic.Int32
	ownedStreamCount int
	lock             sync.RWMutex
	notOwnedStreams  map[model.Fingerprint]any
}

func newOwnedStreamService(tenantID string, limiter *Limiter) *ownedStreamService {
	svc := &ownedStreamService{
		tenantID:        tenantID,
		limiter:         limiter,
		fixedLimit:      atomic.NewInt32(0),
		notOwnedStreams: make(map[model.Fingerprint]any),
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

func (s *ownedStreamService) trackStreamOwnership(fp model.Fingerprint, owned bool) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if owned {
		s.ownedStreamCount++
		return
	}
	notOwnedStreamsMetric.WithLabelValues(s.tenantID).Inc()
	s.notOwnedStreams[fp] = nil
}

func (s *ownedStreamService) trackRemovedStream(fp model.Fingerprint) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, notOwned := s.notOwnedStreams[fp]; notOwned {
		notOwnedStreamsMetric.WithLabelValues(s.tenantID).Dec()
		delete(s.notOwnedStreams, fp)
		return
	}
	s.ownedStreamCount--
}

func (s *ownedStreamService) resetStreamCounts() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.ownedStreamCount = 0
	notOwnedStreamsMetric.WithLabelValues(s.tenantID).Set(0)
	s.notOwnedStreams = make(map[model.Fingerprint]any)
}

func (s *ownedStreamService) isStreamNotOwned(fp model.Fingerprint) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	_, notOwned := s.notOwnedStreams[fp]
	return notOwned
}
