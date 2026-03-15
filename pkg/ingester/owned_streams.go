package ingester

import (
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	// noPolicy represents the absence of a policy
	noPolicy = ""
)

var notOwnedStreamsMetric = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: constants.Loki,
	Name:      "ingester_not_owned_streams",
	Help:      "The total number of not owned streams in memory.",
})

type ownedStreamService struct {
	tenantID         string
	limiter          *Limiter
	fixedLimit       *atomic.Int32
	ownedStreamCount *atomic.Int64
	lock             sync.RWMutex
	notOwnedStreams  map[model.Fingerprint]any

	// Track streams by policy for policy-specific limit enforcement
	policyStreamCounts map[string]*atomic.Int64
	policyLock         sync.RWMutex
}

func newOwnedStreamService(tenantID string, limiter *Limiter) *ownedStreamService {
	svc := &ownedStreamService{
		tenantID:           tenantID,
		limiter:            limiter,
		fixedLimit:         (&atomic.Int32{}),
		ownedStreamCount:   (&atomic.Int64{}),
		notOwnedStreams:    make(map[model.Fingerprint]any),
		policyStreamCounts: make(map[string]*atomic.Int64),
	}

	svc.updateFixedLimit()
	return svc
}

func (s *ownedStreamService) getOwnedStreamCount() int {
	return int(s.ownedStreamCount.Load())
}

func (s *ownedStreamService) getPolicyStreamCount(policy string) int {
	if policy == noPolicy {
		return 0
	}

	s.policyLock.RLock()
	defer s.policyLock.RUnlock()

	if policyCount, exists := s.policyStreamCounts[policy]; exists {
		return int(policyCount.Load())
	}
	return 0
}

// getActivePolicyCount returns the number of policies that currently have active streams
func (s *ownedStreamService) getActivePolicyCount() int {
	s.policyLock.RLock()
	defer s.policyLock.RUnlock()
	return len(s.policyStreamCounts)
}

func (s *ownedStreamService) updateFixedLimit() (old, newVal int32) {
	newLimit, _, _, _ := s.limiter.GetStreamCountLimit(s.tenantID, noPolicy)
	return s.fixedLimit.Swap(int32(newLimit)), int32(newLimit)
}

func (s *ownedStreamService) getFixedLimit() int {
	return int(s.fixedLimit.Load())
}

func (s *ownedStreamService) trackStreamOwnership(fp model.Fingerprint, owned bool, policy string) {
	// only need to inc the owned count; can use sync atomics.
	if owned {
		s.ownedStreamCount.Add(1)

		// Track policy-specific stream count if policy is specified
		if policy != noPolicy {
			s.policyLock.Lock()
			if s.policyStreamCounts[policy] == nil {
				s.policyStreamCounts[policy] = (&atomic.Int64{})
			}
			s.policyStreamCounts[policy].Add(1)
			s.policyLock.Unlock()
		}
		return
	}

	// need to update map; lock required
	s.lock.Lock()
	defer s.lock.Unlock()
	notOwnedStreamsMetric.Inc()
	s.notOwnedStreams[fp] = nil
}

func (s *ownedStreamService) trackRemovedStream(fp model.Fingerprint, policy string) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, notOwned := s.notOwnedStreams[fp]; notOwned {
		notOwnedStreamsMetric.Dec()
		delete(s.notOwnedStreams, fp)
		return
	}
	s.ownedStreamCount.Add(-1)

	// Decrement policy-specific stream count if policy is specified
	if policy != noPolicy {
		s.policyLock.Lock()
		if policyCount, exists := s.policyStreamCounts[policy]; exists {
			policyCount.Add(-1)
			// Clean up policy if count reaches zero to prevent unbounded map growth
			if policyCount.Load() == 0 {
				delete(s.policyStreamCounts, policy)
			}
		}
		s.policyLock.Unlock()
	}
}

func (s *ownedStreamService) resetStreamCounts() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.ownedStreamCount.Store(0)
	notOwnedStreamsMetric.Sub(float64(len(s.notOwnedStreams)))
	s.notOwnedStreams = make(map[model.Fingerprint]any)

	// Reset policy-specific stream counts and clean up the map
	s.policyLock.Lock()
	s.policyStreamCounts = make(map[string]*atomic.Int64)
	s.policyLock.Unlock()
}

func (s *ownedStreamService) isStreamNotOwned(fp model.Fingerprint) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	_, notOwned := s.notOwnedStreams[fp]
	return notOwned
}
