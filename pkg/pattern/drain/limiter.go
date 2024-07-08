package drain

import "time"

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
	if l.blockedUntil != (time.Time{}) {
		if time.Now().Before(l.blockedUntil) {
			return false
		}
		l.reset()
	}
	l.added++
	if float64(l.evicted)/float64(l.added) > l.maxPercentage {
		l.block()
		return false
	}
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
	l.blockedUntil = time.Now().Add(1 * time.Minute)
}
