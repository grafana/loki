package ingester

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"

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

	// Track owned streams by policy for policy-specific limit enforcement
	policyStreams *policyStreamCounts
}

func newOwnedStreamService(tenantID string, limiter *Limiter) *ownedStreamService {
	svc := &ownedStreamService{
		tenantID:         tenantID,
		limiter:          limiter,
		fixedLimit:       atomic.NewInt32(0),
		ownedStreamCount: atomic.NewInt64(0),
		notOwnedStreams:  make(map[model.Fingerprint]any),
		policyStreams:    newPolicyStreamCounts(),
	}

	svc.updateFixedLimit()
	return svc
}

func (s *ownedStreamService) getOwnedStreamCount() int {
	return int(s.ownedStreamCount.Load())
}

func (s *ownedStreamService) getPolicyStreamCount(policy string) int {
	return s.policyStreams.get(policy)
}

// getActivePolicyCount returns the number of policies that currently have active streams
func (s *ownedStreamService) getActivePolicyCount() int {
	return s.policyStreams.len()
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
		s.ownedStreamCount.Inc()

		s.policyStreams.inc(policy)
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
	s.ownedStreamCount.Dec()

	s.policyStreams.dec(policy)
}

func (s *ownedStreamService) resetStreamCounts() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.ownedStreamCount.Store(0)
	notOwnedStreamsMetric.Sub(float64(len(s.notOwnedStreams)))
	s.notOwnedStreams = make(map[model.Fingerprint]any)

	s.policyStreams.reset()
}

func (s *ownedStreamService) isStreamNotOwned(fp model.Fingerprint) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	_, notOwned := s.notOwnedStreams[fp]
	return notOwned
}

// policyStreamCounts tracks the number of streams per policy. Streams without a policy are not
// tracked.
type policyStreamCounts struct {
	mtx    sync.RWMutex
	counts map[string]int
}

func newPolicyStreamCounts() *policyStreamCounts {
	return &policyStreamCounts{counts: make(map[string]int)}
}

func (c *policyStreamCounts) inc(policy string) {
	if policy == noPolicy {
		return
	}
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.counts[policy]++
}

func (c *policyStreamCounts) dec(policy string) {
	if policy == noPolicy {
		return
	}
	c.mtx.Lock()
	defer c.mtx.Unlock()
	n, ok := c.counts[policy]
	if !ok {
		return
	}
	// Clean up policy if count reaches zero to prevent unbounded map growth
	if n <= 1 {
		delete(c.counts, policy)
		return
	}
	c.counts[policy] = n - 1
}

func (c *policyStreamCounts) get(policy string) int {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.counts[policy]
}

// len returns the number of policies that currently have streams.
func (c *policyStreamCounts) len() int {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return len(c.counts)
}

// sum returns the total number of streams across the policies for which include returns true.
func (c *policyStreamCounts) sum(include func(policy string) bool) int {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	total := 0
	for policy, n := range c.counts {
		if include(policy) {
			total += n
		}
	}
	return total
}

func (c *policyStreamCounts) reset() {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.counts = make(map[string]int)
}
