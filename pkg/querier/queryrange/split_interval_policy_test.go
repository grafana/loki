package queryrange

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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

	t.Run("rounds up the smallest positive interval of the tenants", func(t *testing.T) {
		limits := newSplitTestLimits("1", "2", "3")
		limits.splitDuration["1"] = 30 * time.Hour
		limits.splitDuration["2"] = 6 * time.Hour
		limits.splitDuration["3"] = 0

		got := newDataObjSplitIntervalPolicy(Config{}, limits).rangeQueryInterval([]string{"1", "2", "3"}, newRangeRequest(streamFirstQuery, insideStart, insideEnd))

		require.Equal(t, 12*time.Hour, got)
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

	t.Run("rounds up the smallest positive interval of the tenants", func(t *testing.T) {
		limits := newSplitTestLimits("1", "2", "3")
		limits.instantMetricSplitDuration["1"] = 30 * time.Hour
		limits.instantMetricSplitDuration["2"] = 6 * time.Hour
		limits.instantMetricSplitDuration["3"] = 0

		require.Equal(t, 12*time.Hour, newDataObjSplitIntervalPolicy(Config{}, limits).instantQueryInterval([]string{"1", "2", "3"}))
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
