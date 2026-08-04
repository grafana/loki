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

	// defaultStreamCountBucket is the stream-count bucket for streams whose policy has no
	// stream-count override, including streams with no policy at all (see
	// Limiter.streamCountBucket). Named buckets exist only for policies with an override;
	// every stream is counted in exactly one bucket.
	defaultStreamCountBucket = noPolicy
)

var notOwnedStreamsMetric = promauto.NewGauge(prometheus.GaugeOpts{
	Namespace: constants.Loki,
	Name:      "ingester_not_owned_streams",
	Help:      "The total number of not owned streams in memory.",
})

type ownedStreamService struct {
	tenantID   string
	limiter    *Limiter
	fixedLimit *atomic.Int32

	lock            sync.RWMutex
	notOwnedStreams map[model.Fingerprint]any
	// streamCounts tracks owned streams per stream-count bucket. Buckets are created lazily
	// and removed when their count reaches zero. The map is guarded by lock; the counters are
	// atomics so existing buckets can be incremented under the read lock.
	streamCounts map[string]*atomic.Int64
}

func newOwnedStreamService(tenantID string, limiter *Limiter) *ownedStreamService {
	svc := &ownedStreamService{
		tenantID:        tenantID,
		limiter:         limiter,
		fixedLimit:      atomic.NewInt32(0),
		notOwnedStreams: make(map[model.Fingerprint]any),
		streamCounts:    make(map[string]*atomic.Int64),
	}

	svc.updateFixedLimit()
	return svc
}

// getStreamCount returns the number of owned streams tracked in the given bucket.
func (s *ownedStreamService) getStreamCount(bucket string) int {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if count, exists := s.streamCounts[bucket]; exists {
		return int(count.Load())
	}
	return 0
}

// getActivePolicyCount returns the number of policy buckets that currently have active streams
func (s *ownedStreamService) getActivePolicyCount() int {
	s.lock.RLock()
	defer s.lock.RUnlock()

	n := len(s.streamCounts)
	if _, exists := s.streamCounts[defaultStreamCountBucket]; exists {
		n--
	}
	return n
}

func (s *ownedStreamService) updateFixedLimit() (old, newVal int32) {
	newLimit, _, _, _ := s.limiter.GetStreamCountLimit(s.tenantID, defaultStreamCountBucket)
	return s.fixedLimit.Swap(int32(newLimit)), int32(newLimit)
}

func (s *ownedStreamService) getFixedLimit() int {
	return int(s.fixedLimit.Load())
}

// trackStreamOwnership counts an owned stream in exactly one bucket. The bucket must be the
// stream's streamCountBucket so that the later trackRemovedStream call decrements the same
// bucket.
func (s *ownedStreamService) trackStreamOwnership(fp model.Fingerprint, owned bool, bucket string) {
	if owned {
		// Fast path: the bucket usually exists already and its atomic counter can be
		// incremented under the read lock.
		s.lock.RLock()
		count, exists := s.streamCounts[bucket]
		if exists {
			count.Inc()
		}
		s.lock.RUnlock()
		if exists {
			return
		}

		s.lock.Lock()
		if s.streamCounts[bucket] == nil {
			s.streamCounts[bucket] = atomic.NewInt64(0)
		}
		s.streamCounts[bucket].Inc()
		s.lock.Unlock()
		return
	}

	// need to update map; lock required
	s.lock.Lock()
	defer s.lock.Unlock()
	notOwnedStreamsMetric.Inc()
	s.notOwnedStreams[fp] = nil
}

func (s *ownedStreamService) trackRemovedStream(fp model.Fingerprint, bucket string) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, notOwned := s.notOwnedStreams[fp]; notOwned {
		notOwnedStreamsMetric.Dec()
		delete(s.notOwnedStreams, fp)
		return
	}

	if count, exists := s.streamCounts[bucket]; exists {
		count.Dec()
		// Clean up the bucket if count reaches zero to prevent unbounded map growth
		if count.Load() == 0 {
			delete(s.streamCounts, bucket)
		}
	}
}

func (s *ownedStreamService) resetStreamCounts() {
	s.lock.Lock()
	defer s.lock.Unlock()
	notOwnedStreamsMetric.Sub(float64(len(s.notOwnedStreams)))
	s.notOwnedStreams = make(map[model.Fingerprint]any)
	s.streamCounts = make(map[string]*atomic.Int64)
}

func (s *ownedStreamService) isStreamNotOwned(fp model.Fingerprint) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	_, notOwned := s.notOwnedStreams[fp]
	return notOwned
}
