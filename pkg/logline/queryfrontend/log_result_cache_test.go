package queryfrontend

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

type noFreshnessLimits struct{}

func (noFreshnessLimits) MaxCacheFreshness(context.Context, string) time.Duration { return 0 }

type queryOnlyCacheKeyGen struct{}

func (queryOnlyCacheKeyGen) GenerateCacheKey(_ context.Context, tenantIDs []string, req *queryrange.LokiRequest) string {
	return tenantIDs[0] + ":" + req.Query
}

// The log result cache sits between the prefetch and filter middlewares. An
// interval the filter skips must not be cached as empty for requests that do
// not consult the logline index.
func TestLogResultCache_LoglineSkippedIntervalNotServedWithoutLogline(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	start, end := now.Add(-time.Hour), now
	hp := &mockHintProvider{
		hints: &hintprovider.Hints{
			// No overlap with [start, end): the filter skips the interval.
			TimeRanges: []hintprovider.HintTimeRange{
				{Start: now.Add(-3 * time.Hour), End: now.Add(-2 * time.Hour)},
			},
		},
	}

	querierCalls := 0
	querier := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		querierCalls++
		return streamResponseWithEntries(logproto.Entry{Timestamp: start.Add(time.Minute), Line: "error"}), nil
	})

	metrics := newTestMetrics()
	cacheMW, err := queryrange.NewLogResultCache(log.NewNopLogger(), noFreshnessLimits{}, cache.NewMockCache(), nil, queryOnlyCacheKeyGen{}, nil)
	require.NoError(t, err)
	handler := queryrangebase.MergeMiddlewares(
		prefetchMiddlewareForTest(hp, Config{RequireOptInHeader: true}, mockLimits{}, metrics, nil),
		cacheMW,
		NewLoglineFilterMiddleware(10*time.Second, metrics, nil),
	).Wrap(querier)

	req := newTestLokiRequest(`{job="test"} |= "error"`, start, end)
	req.Limit = 100

	resp, err := handler.Do(testTenantContextWithLive(), req)
	require.NoError(t, err)
	require.Empty(t, resp.(*queryrange.LokiResponse).Data.Result)
	require.Equal(t, 0, querierCalls, "logline should skip the interval")

	resp, err = handler.Do(testTenantContextWithForceDisable(), req)
	require.NoError(t, err)
	require.Equal(t, 1, querierCalls, "request without logline must reach the querier")
	require.Len(t, resp.(*queryrange.LokiResponse).Data.Result, 1)
}
