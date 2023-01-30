package limiter

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiterStrategy defines the interface which a pluggable strategy should
// implement. The returned limit and burst can change over the time, and the
// local rate limiter will apply them every recheckPeriod.
type RateLimiterStrategy interface {
	Limit(tenantID string) float64
	Burst(tenantID string) int
}

// RateLimiter is a multi-tenant local rate limiter based on golang.org/x/time/rate.
// It requires a custom strategy in input, which is used to get the limit and burst
// settings for each tenant.
type RateLimiter struct {
	strategy      RateLimiterStrategy
	recheckPeriod time.Duration

	tenantsLock sync.RWMutex
	tenants     map[string]*tenantLimiter
}

type tenantLimiter struct {
	limiter   *rate.Limiter
	recheckAt time.Time
}

// NewRateLimiter makes a new multi-tenant rate limiter. Each per-tenant limiter
// is configured using the input strategy and its limit/burst is rechecked (and
// reconfigured if changed) every recheckPeriod.
func NewRateLimiter(strategy RateLimiterStrategy, recheckPeriod time.Duration) *RateLimiter {
	return &RateLimiter{
		strategy:      strategy,
		recheckPeriod: recheckPeriod,
		tenants:       map[string]*tenantLimiter{},
	}
}

// AllowN reports whether n tokens may be consumed happen at time now.
func (l *RateLimiter) AllowN(now time.Time, tenantID string, n int) bool {
	return l.getTenantLimiter(now, tenantID).AllowN(now, n)
}

// Limit returns the currently configured maximum overall tokens rate.
func (l *RateLimiter) Limit(now time.Time, tenantID string) float64 {
	return float64(l.getTenantLimiter(now, tenantID).Limit())
}

// Burst returns the currently configured maximum burst size.
func (l *RateLimiter) Burst(now time.Time, tenantID string) int {
	return l.getTenantLimiter(now, tenantID).Burst()
}

func (l *RateLimiter) getTenantLimiter(now time.Time, tenantID string) *rate.Limiter {
	recheck := false

	// Check if the per-tenant limiter already exists and if should
	// be rechecked because the recheck period has elapsed
	l.tenantsLock.RLock()
	entry, ok := l.tenants[tenantID]
	if ok && !now.Before(entry.recheckAt) {
		recheck = true
	}
	l.tenantsLock.RUnlock()

	// If the limiter already exist, we return it, making sure to recheck it
	// if the recheck period has elapsed
	if ok && recheck {
		return l.recheckTenantLimiter(now, tenantID)
	} else if ok {
		return entry.limiter
	}

	// Create a new limiter
	limit := rate.Limit(l.strategy.Limit(tenantID))
	burst := l.strategy.Burst(tenantID)
	limiter := rate.NewLimiter(limit, burst)

	l.tenantsLock.Lock()
	if entry, ok = l.tenants[tenantID]; !ok {
		entry = &tenantLimiter{limiter, now.Add(l.recheckPeriod)}
		l.tenants[tenantID] = entry
	}
	l.tenantsLock.Unlock()

	return entry.limiter
}

func (l *RateLimiter) recheckTenantLimiter(now time.Time, tenantID string) *rate.Limiter {
	limit := rate.Limit(l.strategy.Limit(tenantID))
	burst := l.strategy.Burst(tenantID)

	l.tenantsLock.Lock()
	defer l.tenantsLock.Unlock()

	entry := l.tenants[tenantID]

	// We check again if the recheck period elapsed, cause it may
	// have already been rechecked in the meanwhile.
	if now.Before(entry.recheckAt) {
		return entry.limiter
	}

	// Ensure the limiter's limit and burst match the expected value
	if entry.limiter.Limit() != limit {
		entry.limiter.SetLimitAt(now, limit)
	}

	if entry.limiter.Burst() != burst {
		entry.limiter.SetBurstAt(now, burst)
	}

	entry.recheckAt = now.Add(l.recheckPeriod)

	return entry.limiter
}
