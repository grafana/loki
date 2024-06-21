package ingester

import (
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
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
	logger           log.Logger
}

func newOwnedStreamService(tenantID string, limiter *Limiter, logger log.Logger) *ownedStreamService {
	svc := &ownedStreamService{
		tenantID:        tenantID,
		limiter:         limiter,
		fixedLimit:      atomic.NewInt32(0),
		notOwnedStreams: make(map[model.Fingerprint]any),
		logger:          logger,
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
	newLimit, _, _, _ := s.limiter.GetStreamCountLimit(s.tenantID)
	oldLimit := s.fixedLimit.Swap(int32(newLimit))
	level.Debug(s.logger).Log("msg", "updating fixed limit", "tenant", s.tenantID, "old_limit", oldLimit, "new_limit", newLimit)
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
	level.Debug(s.logger).Log("msg", "stream is marked as not owned", "fingerprint", fp)
	notOwnedStreamsMetric.WithLabelValues(s.tenantID).Inc()
	s.notOwnedStreams[fp] = nil
}

func (s *ownedStreamService) trackRemovedStream(fp model.Fingerprint) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, notOwned := s.notOwnedStreams[fp]; notOwned {
		level.Debug(s.logger).Log("msg", "removing not owned stream", "fingerprint", fp)
		notOwnedStreamsMetric.WithLabelValues(s.tenantID).Dec()
		delete(s.notOwnedStreams, fp)
		return
	}
	level.Debug(s.logger).Log("msg", "removing owned stream", "fingerprint", fp)
	s.ownedStreamCount--
}

func (s *ownedStreamService) resetStreamCounts() {
	s.lock.Lock()
	defer s.lock.Unlock()
	//todo remove or pass a logger from the ingester
	level.Debug(s.logger).Log("msg", "resetting stream counts")
	s.ownedStreamCount = 0
	notOwnedStreamsMetric.WithLabelValues(s.tenantID).Set(0)
	s.notOwnedStreams = make(map[model.Fingerprint]any)
}

func (s *ownedStreamService) isStreamNotOwned(fp model.Fingerprint) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	_, notOwned := s.notOwnedStreams[fp]
	// todo remove
	level.Debug(s.logger).Log("msg", "checking stream ownership", "fingerprint", fp, "notOwned", notOwned)
	return notOwned
}
