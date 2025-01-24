package drain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewLimiter(t *testing.T) {
	maxPercentage := 0.5
	l := newLimiter(maxPercentage)
	require.NotNil(t, l, "expected non-nil limiter")
	require.Equal(t, maxPercentage, l.maxPercentage, "expected maxPercentage to match")
	require.Equal(t, int64(0), l.added, "expected added to be 0")
	require.Equal(t, int64(0), l.evicted, "expected evicted to be 0")
	require.True(t, l.blockedUntil.IsZero(), "expected blockedUntil to be zero")
}

func TestLimiterAllow(t *testing.T) {
	maxPercentage := 0.5
	l := newLimiter(maxPercentage)

	// Test allowing when no evictions
	require.True(t, l.Allow(), "expected Allow to return true initially")

	// Test allowing until evictions exceed maxPercentage
	for i := 0; i < 2; i++ {
		require.True(t, l.Allow(), "expected Allow to return true %d", i)
		l.Evict()
	}

	// Evict to exceed maxPercentage
	l.Evict()
	require.False(t, l.Allow(), "expected Allow to return false after evictions exceed maxPercentage")

	// Test blocking time
	require.False(t, l.blockedUntil.IsZero(), "expected blockedUntil to be set")

	// Fast forward time to simulate block duration passing
	l.blockedUntil = time.Now().Add(-1 * time.Minute)
	require.True(t, l.Allow(), "expected Allow to return true after block duration")
}

func TestLimiterEvict(t *testing.T) {
	l := newLimiter(0.5)
	l.Evict()
	require.Equal(t, int64(1), l.evicted, "expected evicted to be 1")
	l.Evict()
	require.Equal(t, int64(2), l.evicted, "expected evicted to be 2")
}

func TestLimiterReset(t *testing.T) {
	l := newLimiter(0.5)
	l.added = 10
	l.evicted = 5
	l.blockedUntil = time.Now().Add(10 * time.Minute)
	l.reset()
	require.Equal(t, int64(0), l.added, "expected added to be 0")
	require.Equal(t, int64(0), l.evicted, "expected evicted to be 0")
	require.True(t, l.blockedUntil.IsZero(), "expected blockedUntil to be zero")
}

func TestLimiterBlock(t *testing.T) {
	l := newLimiter(0.5)
	l.block()
	require.False(t, l.blockedUntil.IsZero(), "expected blockedUntil to be set")
	require.False(t, l.Allow())
	require.True(t, l.blockedUntil.After(time.Now()), "expected blockedUntil to be in the future")
}
