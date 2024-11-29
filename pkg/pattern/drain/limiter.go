package drain

import (
	"time"
)

type limiter struct {
	added         int64
	evicted       int64
	maxPercentage float64
	blockedUntil  time.Time
}

func newLimiter(maxPercentage float64) *limiter {
	return &limiter{
		maxPercentage: maxPercentage,
	}
}

func (l *limiter) Allow() bool {
	if !l.blockedUntil.IsZero() {
		if time.Now().Before(l.blockedUntil) {
			return false
		}
		l.reset()
	}
	if l.added == 0 {
		l.added++
		return true
	}
	if float64(l.evicted)/float64(l.added) > l.maxPercentage {
		l.block()
		return false
	}
	l.added++
	return true
}

func (l *limiter) Evict() {
	l.evicted++
}

func (l *limiter) reset() {
	l.added = 0
	l.evicted = 0
	l.blockedUntil = time.Time{}
}

func (l *limiter) block() {
	l.blockedUntil = time.Now().Add(10 * time.Minute)
}
