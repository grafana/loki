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
