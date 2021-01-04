package ingester

import (
	"fmt"
	"math"
	"sync"

	"github.com/grafana/loki/pkg/util/validation"
)

const (
	errMaxStreamsPerUserLimitExceeded = "tenant '%v' per-user streams limit exceeded, streams: %d exceeds calculated limit: %d (local limit: %d, global limit: %d, global/ingesters: %d)"
)

// RingCount is the interface exposed by a ring implementation which allows
// to count members
type RingCount interface {
	HealthyInstancesCount() int
}

// Limiter implements primitives to get the maximum number of streams
// an ingester can handle for a specific tenant
type Limiter struct {
	limits            *validation.Overrides
	ring              RingCount
	replicationFactor int

	mtx      sync.RWMutex
	disabled bool
}

func (l *Limiter) Disable() {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.disabled = true
}

func (l *Limiter) Enable() {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.disabled = false
}

// NewLimiter makes a new limiter
func NewLimiter(limits *validation.Overrides, ring RingCount, replicationFactor int) *Limiter {
	return &Limiter{
		limits:            limits,
		ring:              ring,
		replicationFactor: replicationFactor,
	}
}

// AssertMaxStreamsPerUser ensures limit has not been reached compared to the current
// number of streams in input and returns an error if so.
func (l *Limiter) AssertMaxStreamsPerUser(userID string, streams int) error {
	// Until the limiter actually starts, all accesses are successful.
	// This is used to disable limits while recovering from the WAL.
	l.mtx.RLock()
	defer l.mtx.RUnlock()
	if l.disabled {
		return nil
	}

	// Start by setting the local limit either from override or default
	localLimit := l.limits.MaxLocalStreamsPerUser(userID)

	// We can assume that streams are evenly distributed across ingesters
	// so we do convert the global limit into a local limit
	globalLimit := l.limits.MaxGlobalStreamsPerUser(userID)
	adjustedGlobalLimit := l.convertGlobalToLocalLimit(globalLimit)

	// Set the calculated limit to the lesser of the local limit or the new calculated global limit
	calculatedLimit := l.minNonZero(localLimit, adjustedGlobalLimit)

	// If both the local and global limits are disabled, we just
	// use the largest int value
	if calculatedLimit == 0 {
		calculatedLimit = math.MaxInt32
	}

	if streams < calculatedLimit {
		return nil
	}

	return fmt.Errorf(errMaxStreamsPerUserLimitExceeded, userID, streams, calculatedLimit, localLimit, globalLimit, adjustedGlobalLimit)
}

func (l *Limiter) convertGlobalToLocalLimit(globalLimit int) int {
	if globalLimit == 0 {
		return 0
	}

	// Given we don't need a super accurate count (ie. when the ingesters
	// topology changes) and we prefer to always be in favor of the tenant,
	// we can use a per-ingester limit equal to:
	// (global limit / number of ingesters) * replication factor
	numIngesters := l.ring.HealthyInstancesCount()

	// May happen because the number of ingesters is asynchronously updated.
	// If happens, we just temporarily ignore the global limit.
	if numIngesters > 0 {
		return int((float64(globalLimit) / float64(numIngesters)) * float64(l.replicationFactor))
	}

	return 0
}

func (l *Limiter) minNonZero(first, second int) int {
	if first == 0 || (second != 0 && first > second) {
		return second
	}

	return first
}
