package queryrange

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/testutil"
)

func TestChunkSplitIntervalPolicy_rangeQueryInterval(t *testing.T) {
	const query = `sum(count_over_time({app="a"}[1m]))`
	start := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	end := start.Add(12 * time.Hour)
	limits := fakeLimits{splitDuration: map[string]time.Duration{"1": time.Hour, "2": 30 * time.Minute, "3": 0}}
	policy := newChunkSplitIntervalPolicy(limits)

	t.Run("returns the query split duration of the tenant", func(t *testing.T) {
		got := policy.rangeQueryInterval([]string{"1"}, newRangeRequest(query, start, end))

		require.Equal(t, time.Hour, got)
	})

	t.Run("returns the smallest positive query split duration of the tenants", func(t *testing.T) {
		got := policy.rangeQueryInterval([]string{"1", "2", "3"}, newRangeRequest(query, start, end))

		require.Equal(t, 30*time.Minute, got)
	})

	t.Run("returns 0 when the tenant turns splitting off", func(t *testing.T) {
		got := policy.rangeQueryInterval([]string{"3"}, newRangeRequest(query, start, end))

		require.Zero(t, got)
	})

	t.Run("widens the interval to a longer range vector", func(t *testing.T) {
		got := policy.rangeQueryInterval([]string{"1"}, newRangeRequest(`rate({app="a"}[3h])`, start, end))

		require.Equal(t, 3*time.Hour, got)
	})
}

func TestChunkSplitIntervalPolicy_instantQueryInterval(t *testing.T) {
	limits := fakeLimits{instantMetricSplitDuration: map[string]time.Duration{"1": time.Hour, "2": 30 * time.Minute, "3": 0}}
	policy := newChunkSplitIntervalPolicy(limits)

	t.Run("returns the instant split duration of the tenant", func(t *testing.T) {
		require.Equal(t, time.Hour, policy.instantQueryInterval([]string{"1"}))
	})

	t.Run("returns the smallest positive instant split duration of the tenants", func(t *testing.T) {
		require.Equal(t, 30*time.Minute, policy.instantQueryInterval([]string{"1", "2", "3"}))
	})

	t.Run("returns 0 when the tenant turns instant splitting off", func(t *testing.T) {
		require.Zero(t, policy.instantQueryInterval([]string{"3"}))
	})
}

func TestDataObjSplitIntervalPolicy_alignIntervalToMetastoreWindow(t *testing.T) {
	policy := newDataObjSplitIntervalPolicy(Config{}, fakeLimits{})

	t.Run("raises an interval below the window to the window", func(t *testing.T) {
		require.Equal(t, 12*time.Hour, policy.alignIntervalToMetastoreWindow(time.Hour))
		require.Equal(t, 12*time.Hour, policy.alignIntervalToMetastoreWindow(time.Nanosecond))
		require.Equal(t, 12*time.Hour, policy.alignIntervalToMetastoreWindow(0))
	})

	t.Run("keeps an interval that is already a multiple of the window", func(t *testing.T) {
		require.Equal(t, 12*time.Hour, policy.alignIntervalToMetastoreWindow(12*time.Hour))
		require.Equal(t, 24*time.Hour, policy.alignIntervalToMetastoreWindow(24*time.Hour))
		require.Equal(t, 72*time.Hour, policy.alignIntervalToMetastoreWindow(72*time.Hour))
	})

	t.Run("rounds an interval above the window up to a multiple of the window", func(t *testing.T) {
		require.Equal(t, 24*time.Hour, policy.alignIntervalToMetastoreWindow(18*time.Hour))
		require.Equal(t, 24*time.Hour, policy.alignIntervalToMetastoreWindow(12*time.Hour+time.Nanosecond))
		require.Equal(t, 36*time.Hour, policy.alignIntervalToMetastoreWindow(30*time.Hour))
	})
}

func TestDataObjSplitIntervalPolicy_shouldUse(t *testing.T) {
	tenants := []string{"1"}
	insideStart, insideEnd := dataObjRoutingNow.Add(-24*time.Hour), dataObjRoutingNow.Add(-12*time.Hour)

	newPolicy := func(cfg Config) *dataObjSplitIntervalPolicy {
		cfg.now = func() time.Time { return dataObjRoutingNow }
		return newDataObjSplitIntervalPolicy(cfg, newSplitTestLimits("1"))
	}

	t.Run("returns false when the frontend config turns data objects off", func(t *testing.T) {
		policy := newPolicy(Config{DataObjStorageLag: dataObjRoutingLag})

		require.False(t, policy.shouldUse(tenants, syntax.MustParseExpr(streamFirstQuery), insideStart, insideEnd))
	})

	t.Run("returns true for a stream-first sum inside the held range", func(t *testing.T) {
		policy := newPolicy(Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag})

		require.True(t, policy.shouldUse(tenants, syntax.MustParseExpr(streamFirstQuery), insideStart, insideEnd))
	})

	t.Run("returns false for a range outside the held range", func(t *testing.T) {
		policy := newPolicy(Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag})

		require.False(t, policy.shouldUse(tenants, syntax.MustParseExpr(streamFirstQuery), dataObjRoutingNow.Add(-2*time.Hour), dataObjRoutingNow.Add(-time.Hour)))
	})

	t.Run("returns false for an eligible query that data objects hold less than half of", func(t *testing.T) {
		policy := newPolicy(Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag})
		// The read window is 100 minutes long, and data objects hold its first 30 minutes. The query
		// has a range vector of 1m, so the request starts 1m after the window starts and ends 1ns
		// before it ends.
		windowStart := dataObjRoutingBandEnd.Add(-30 * time.Minute)
		windowEnd := windowStart.Add(100 * time.Minute)
		start, end := windowStart.Add(time.Minute), windowEnd.Add(-time.Nanosecond)

		require.False(t, policy.shouldUse(tenants, syntax.MustParseExpr(streamFirstQuery), start, end))
	})

	t.Run("returns true for an eligible query that data objects hold more than half of", func(t *testing.T) {
		policy := newPolicy(Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag})
		windowStart := dataObjRoutingBandEnd.Add(-60 * time.Minute)
		windowEnd := windowStart.Add(100 * time.Minute)
		start, end := windowStart.Add(time.Minute), windowEnd.Add(-time.Nanosecond)

		require.True(t, policy.shouldUse(tenants, syntax.MustParseExpr(streamFirstQuery), start, end))
	})

	t.Run("returns false for a query that is not a stream-first sum", func(t *testing.T) {
		policy := newPolicy(Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag})

		require.False(t, policy.shouldUse(tenants, syntax.MustParseExpr(`rate({app="a"}[1m])`), insideStart, insideEnd))
	})

	t.Run("returns true when one leg of a binary expression is a stream-first sum", func(t *testing.T) {
		policy := newPolicy(Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag})
		expr := syntax.MustParseExpr(streamFirstQuery + ` / count(count_over_time({app="b"}[1m]))`)

		require.True(t, policy.shouldUse(tenants, expr, insideStart, insideEnd))
	})

	t.Run("returns true for an instant query with a range vector that data objects hold mostly", func(t *testing.T) {
		policy := newPolicy(Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag})
		ts := dataObjRoutingNow.Add(-6 * time.Hour)

		require.True(t, policy.shouldUse(tenants, syntax.MustParseExpr(`sum(count_over_time({app="a"}[24h]))`), ts, ts))
	})

	t.Run("returns false for an instant query with a range vector that data objects hold less than half of", func(t *testing.T) {
		policy := newPolicy(Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag})
		ts := dataObjRoutingBandEnd.Add(70 * time.Minute)

		require.False(t, policy.shouldUse(tenants, syntax.MustParseExpr(`sum(count_over_time({app="a"}[100m]))`), ts, ts))
	})

	t.Run("returns true for an instant query with a range vector that data objects hold more than half of", func(t *testing.T) {
		policy := newPolicy(Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag})
		ts := dataObjRoutingBandEnd.Add(40 * time.Minute)

		require.True(t, policy.shouldUse(tenants, syntax.MustParseExpr(`sum(count_over_time({app="a"}[100m]))`), ts, ts))
	})
}

func TestDataObjSplitIntervalPolicy_rangeQueryInterval(t *testing.T) {
	tenants := []string{"1"}
	insideStart, insideEnd := dataObjRoutingNow.Add(-24*time.Hour), dataObjRoutingNow.Add(-12*time.Hour)

	t.Run("returns 0 when the tenant turns splitting off", func(t *testing.T) {
		limits := newSplitTestLimits("1")
		limits.splitDuration["1"] = 0

		got := newDataObjSplitIntervalPolicy(Config{}, limits).rangeQueryInterval(tenants, newRangeRequest(streamFirstQuery, insideStart, insideEnd))

		require.Zero(t, got)
	})

	t.Run("raises an interval below the window to the window", func(t *testing.T) {
		got := newDataObjSplitIntervalPolicy(Config{}, newSplitTestLimits("1")).rangeQueryInterval(tenants, newRangeRequest(streamFirstQuery, insideStart, insideEnd))

		require.Equal(t, 12*time.Hour, got)
	})

	t.Run("rounds an interval above the window up to a multiple of the window", func(t *testing.T) {
		limits := newSplitTestLimits("1")
		limits.splitDuration["1"] = 18 * time.Hour

		got := newDataObjSplitIntervalPolicy(Config{}, limits).rangeQueryInterval(tenants, newRangeRequest(streamFirstQuery, insideStart, insideEnd))

		require.Equal(t, 24*time.Hour, got)
	})

	t.Run("widens to the range vector first and then rounds up to a multiple of the window", func(t *testing.T) {
		got := newDataObjSplitIntervalPolicy(Config{}, newSplitTestLimits("1")).rangeQueryInterval(tenants, newRangeRequest(`sum(count_over_time({app="a"}[30h]))`, insideStart, insideEnd))

		require.Equal(t, 36*time.Hour, got)
	})
}

func TestDataObjSplitIntervalPolicy_instantQueryInterval(t *testing.T) {
	tenants := []string{"1"}

	t.Run("returns 0 when the tenant turns instant splitting off", func(t *testing.T) {
		limits := newSplitTestLimits("1")
		limits.instantMetricSplitDuration["1"] = 0

		require.Zero(t, newDataObjSplitIntervalPolicy(Config{}, limits).instantQueryInterval(tenants))
	})

	t.Run("raises an interval below the window to the window", func(t *testing.T) {
		require.Equal(t, 12*time.Hour, newDataObjSplitIntervalPolicy(Config{}, newSplitTestLimits("1")).instantQueryInterval(tenants))
	})

	t.Run("rounds an interval above the window up to a multiple of the window", func(t *testing.T) {
		limits := newSplitTestLimits("1")
		limits.instantMetricSplitDuration["1"] = 18 * time.Hour

		require.Equal(t, 24*time.Hour, newDataObjSplitIntervalPolicy(Config{}, limits).instantQueryInterval(tenants))
	})
}

func newSplitTestLimits(tenants ...string) fakeLimits {
	limits := dataObjTenantLimits(tenants...)
	limits.splitDuration = map[string]time.Duration{}
	limits.instantMetricSplitDuration = map[string]time.Duration{}
	for _, tenant := range tenants {
		limits.splitDuration[tenant] = time.Hour
		limits.instantMetricSplitDuration[tenant] = time.Hour
	}
	return limits
}

func newRangeRequest(query string, start, end time.Time) *LokiRequest {
	return &LokiRequest{
		Query:   query,
		Plan:    testutil.MustPlan(query),
		Step:    60000,
		StartTs: start,
		EndTs:   end,
		Path:    "/loki/api/v1/query_range",
	}
}

func newInstantRequest(query string, ts time.Time) *LokiInstantRequest {
	return &LokiInstantRequest{Query: query, Plan: testutil.MustPlan(query), TimeTs: ts, Path: "/loki/api/v1/query"}
}
