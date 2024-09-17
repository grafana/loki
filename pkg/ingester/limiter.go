package ingester

import (
	"fmt"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	errMaxStreamsPerUserLimitExceeded = "tenant '%v' per-user streams limit exceeded, streams: %d exceeds calculated limit: %d (local limit: %d, global limit: %d, global/ingesters: %d)"
)

// RingCount is the interface exposed by a ring implementation which allows
// to count members
type RingCount interface {
	HealthyInstancesCount() int
	HealthyInstancesInZoneCount() int
	ZonesCount() int
}

type Limits interface {
	UnorderedWrites(userID string) bool
	UseOwnedStreamCount(userID string) bool
	MaxLocalStreamsPerUser(userID string) int
	MaxGlobalStreamsPerUser(userID string) int
	PerStreamRateLimit(userID string) validation.RateLimit
	ShardStreams(userID string) shardstreams.Config
}

// Limiter implements primitives to get the maximum number of streams
// an ingester can handle for a specific tenant
type Limiter struct {
	limits            Limits
	ring              RingCount
	replicationFactor int
	metrics           *ingesterMetrics

	mtx      sync.RWMutex
	disabled bool
}

func (l *Limiter) DisableForWALReplay() {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.disabled = true
	l.metrics.limiterEnabled.Set(0)
}

func (l *Limiter) Enable() {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.disabled = false
	l.metrics.limiterEnabled.Set(1)
}

// NewLimiter makes a new limiter
func NewLimiter(limits Limits, metrics *ingesterMetrics, ring RingCount, replicationFactor int) *Limiter {
	return &Limiter{
		limits:            limits,
		ring:              ring,
		replicationFactor: replicationFactor,
		metrics:           metrics,
	}
}

func (l *Limiter) UnorderedWrites(userID string) bool {
	// WAL replay should not discard previously ack'd writes,
	// so allow out of order writes while the limiter is disabled.
	// This allows replaying unordered WALs into ordered configurations.
	if l.disabled {
		return true
	}
	return l.limits.UnorderedWrites(userID)
}

func (l *Limiter) GetStreamCountLimit(tenantID string) (calculatedLimit, localLimit, globalLimit, adjustedGlobalLimit int) {
	// Start by setting the local limit either from override or default
	localLimit = l.limits.MaxLocalStreamsPerUser(tenantID)

	// We can assume that streams are evenly distributed across ingesters
	// so we do convert the global limit into a local limit
	globalLimit = l.limits.MaxGlobalStreamsPerUser(tenantID)
	adjustedGlobalLimit = l.convertGlobalToLocalLimit(globalLimit)

	// Set the calculated limit to the lesser of the local limit or the new calculated global limit
	calculatedLimit = l.minNonZero(localLimit, adjustedGlobalLimit)

	// If both the local and global limits are disabled, we just
	// use the largest int value
	if calculatedLimit == 0 {
		calculatedLimit = math.MaxInt32
	}
	return
}

func (l *Limiter) minNonZero(first, second int) int {
	if first == 0 || (second != 0 && first > second) {
		return second
	}

	return first
}

func (l *Limiter) convertGlobalToLocalLimit(globalLimit int) int {
	if globalLimit == 0 || l.replicationFactor == 0 {
		return 0
	}

	zonesCount := l.ring.ZonesCount()
	if zonesCount <= 1 {
		return calculateLimitForSingleZone(globalLimit, l)
	}

	return calculateLimitForMultipleZones(globalLimit, zonesCount, l)
}

func calculateLimitForSingleZone(globalLimit int, l *Limiter) int {
	numIngesters := l.ring.HealthyInstancesCount()
	if numIngesters > 0 {
		return int((float64(globalLimit) / float64(numIngesters)) * float64(l.replicationFactor))
	}
	return 0
}

func calculateLimitForMultipleZones(globalLimit, zonesCount int, l *Limiter) int {
	ingestersInZone := l.ring.HealthyInstancesInZoneCount()
	if ingestersInZone > 0 {
		return int((float64(globalLimit) * float64(l.replicationFactor)) / float64(zonesCount) / float64(ingestersInZone))
	}
	return 0
}

type supplier[T any] func() T

type streamCountLimiter struct {
	tenantID                   string
	limiter                    *Limiter
	defaultStreamCountSupplier supplier[int]
	ownedStreamSvc             *ownedStreamService
}

var noopFixedLimitSupplier = func() int {
	return 0
}

func newStreamCountLimiter(tenantID string, defaultStreamCountSupplier supplier[int], limiter *Limiter, service *ownedStreamService) *streamCountLimiter {
	return &streamCountLimiter{
		tenantID:                   tenantID,
		limiter:                    limiter,
		defaultStreamCountSupplier: defaultStreamCountSupplier,
		ownedStreamSvc:             service,
	}
}

func (l *streamCountLimiter) AssertNewStreamAllowed(tenantID string) error {
	streamCountSupplier, fixedLimitSupplier := l.getSuppliers(tenantID)
	calculatedLimit, localLimit, globalLimit, adjustedGlobalLimit := l.getCurrentLimit(tenantID, fixedLimitSupplier)
	actualStreamsCount := streamCountSupplier()
	if actualStreamsCount < calculatedLimit {
		return nil
	}

	return fmt.Errorf(errMaxStreamsPerUserLimitExceeded, tenantID, actualStreamsCount, calculatedLimit, localLimit, globalLimit, adjustedGlobalLimit)
}

func (l *streamCountLimiter) getCurrentLimit(tenantID string, fixedLimitSupplier supplier[int]) (calculatedLimit, localLimit, globalLimit, adjustedGlobalLimit int) {
	calculatedLimit, localLimit, globalLimit, adjustedGlobalLimit = l.limiter.GetStreamCountLimit(tenantID)
	fixedLimit := fixedLimitSupplier()
	if fixedLimit > calculatedLimit {
		calculatedLimit = fixedLimit
	}
	return
}

func (l *streamCountLimiter) getSuppliers(tenant string) (streamCountSupplier, fixedLimitSupplier supplier[int]) {
	if l.limiter.limits.UseOwnedStreamCount(tenant) {
		return l.ownedStreamSvc.getOwnedStreamCount, l.ownedStreamSvc.getFixedLimit
	}
	return l.defaultStreamCountSupplier, noopFixedLimitSupplier
}

type RateLimiterStrategy interface {
	RateLimit(tenant string) validation.RateLimit
}

func (l *Limiter) RateLimit(tenant string) validation.RateLimit {
	if l.disabled {
		return validation.Unlimited
	}

	return l.limits.PerStreamRateLimit(tenant)
}

type StreamRateLimiter struct {
	recheckPeriod time.Duration
	recheckAt     time.Time
	strategy      RateLimiterStrategy
	tenant        string
	lim           *rate.Limiter
}

func NewStreamRateLimiter(strategy RateLimiterStrategy, tenant string, recheckPeriod time.Duration) *StreamRateLimiter {
	rl := strategy.RateLimit(tenant)
	return &StreamRateLimiter{
		recheckPeriod: recheckPeriod,
		strategy:      strategy,
		tenant:        tenant,
		lim:           rate.NewLimiter(rl.Limit, rl.Burst),
	}
}

func (l *StreamRateLimiter) AllowN(at time.Time, n int) bool {
	now := time.Now()
	if now.After(l.recheckAt) {
		l.recheckAt = now.Add(l.recheckPeriod)

		oldLim := l.lim.Limit()
		oldBurst := l.lim.Burst()

		next := l.strategy.RateLimit(l.tenant)

		if oldLim != next.Limit || oldBurst != next.Burst {
			// Edge case: rate.Inf doesn't advance nicely when reconfigured.
			// To simplify, we just create a new limiter after reconfiguration rather
			// than alter the existing one.
			l.lim = rate.NewLimiter(next.Limit, next.Burst)
		}
	}

	return l.lim.AllowN(at, n)
}
