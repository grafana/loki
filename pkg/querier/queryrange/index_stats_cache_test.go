package queryrange

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

func TestIndexStatsCache(t *testing.T) {
	cfg := queryrangebase.ResultsCacheConfig{
		Config: resultscache.Config{
			CacheConfig: cache.Config{
				Cache: cache.NewMockCache(),
			},
		},
	}
	c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache, constants.Loki)
	require.NoError(t, err)
	cacheMiddleware, err := NewIndexStatsCacheMiddleware(
		log.NewNopLogger(),
		WithSplitByLimits(fakeLimits{}, 24*time.Hour),
		DefaultCodec,
		c,
		nil,
		nil,
		nil,
		func(_ context.Context, _ []string, _ queryrangebase.Request) int {
			return 1
		},
		false,
		nil,
		nil,
	)
	require.NoError(t, err)

	from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))
	statsReq := &logproto.IndexStatsRequest{
		From:     from,
		Through:  through,
		Matchers: `{foo="bar"}`,
	}

	statsResp := &IndexStatsResponse{
		Response: &logproto.IndexStatsResponse{
			Streams: 1,
			Chunks:  2,
			Bytes:   1 << 10,
			Entries: 10,
		},
	}

	calls, statsHandler := indexStatsResultHandler(statsResp)
	rc := cacheMiddleware.Wrap(statsHandler)

	ctx := user.InjectOrgID(context.Background(), "fake")
	resp, err := rc.Do(ctx, statsReq)
	require.NoError(t, err)
	require.Equal(t, 1, *calls)
	require.Equal(t, statsResp, resp)

	// Doing same request again shouldn't change anything.
	*calls = 0
	resp, err = rc.Do(ctx, statsReq)
	require.NoError(t, err)
	require.Equal(t, 0, *calls)
	require.Equal(t, statsResp, resp)

	// Doing a request with new start later than the previous query and end time later than the previous one,
	// should reuse part of the previous request and issue a new request for the remaining time till end.
	// The new start time is 15m (i.e. 25%) in the future with regard to the previous request time span.
	*calls = 0
	req := statsReq.WithStartEnd(statsReq.GetStart().Add(15*time.Minute), statsReq.GetEnd().Add(15*time.Minute))
	// The cached extent is 1h1ms long, because RoundToMilliseconds rounds testTime's 10ns up to the next millisecond.
	// The request reuses the last 45m1ms of it, so the cached result is scaled by 2700001/3600001, just above 0.75,
	// and rounded up. The remaining 15m is a new request, which returns statsResp unscaled, and the two are summed.
	expectedStats := &IndexStatsResponse{
		Response: &logproto.IndexStatsResponse{
			Streams: 2,    // 1 cached (streams aren't scaled) + 1 new
			Chunks:  4,    // 2 cached (2 * 0.75 = 1.5, rounded up) + 2 new
			Bytes:   1793, // 769 cached (1024 * 0.75 = 768, plus a fraction from the extra 1ms, rounded up) + 1024 new
			Entries: 18,   // 8 cached (10 * 0.75 = 7.5, rounded up) + 10 new
		},
	}
	resp, err = rc.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, *calls)
	require.Equal(t, expectedStats, resp)
}

func TestIndexStatsCache_BypassesHintedRequests(t *testing.T) {
	statsCache := &countingCache{Cache: cache.NewMockCache()}
	cacheMiddleware, err := NewIndexStatsCacheMiddleware(
		log.NewNopLogger(),
		WithSplitByLimits(fakeLimits{}, 24*time.Hour),
		DefaultCodec,
		statsCache,
		nil,
		nil,
		nil,
		func(_ context.Context, _ []string, _ queryrangebase.Request) int {
			return 1
		},
		false,
		nil,
		nil,
	)
	require.NoError(t, err)

	calls, statsHandler := indexStatsResultHandler(&IndexStatsResponse{Response: &logproto.IndexStatsResponse{Chunks: 1}})
	rc := cacheMiddleware.Wrap(statsHandler)
	ctx := user.InjectOrgID(context.Background(), "fake")
	from, through := util.RoundToMilliseconds(testTime, testTime.Add(time.Hour))

	for _, hints := range []*logproto.HintTimeRanges{
		{Ranges: []logproto.HintTimeRange{{Start: testTime, End: testTime.Add(time.Minute)}}},
		{},
	} {
		req := &logproto.IndexStatsRequest{From: from, Through: through, Matchers: `{foo="bar"}`, HintRanges: hints}
		for range 2 {
			*calls = 0
			_, err := rc.Do(ctx, req)
			require.NoError(t, err)
			require.Equal(t, 1, *calls)
		}
	}
	require.Zero(t, statsCache.fetches)
	require.Zero(t, statsCache.stores)

	req := &logproto.IndexStatsRequest{From: from, Through: through, Matchers: `{foo="bar"}`}
	_, err = rc.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, statsCache.stores, "requests without hints are still cached")
}

func TestIndexStatsCache_RecentData(t *testing.T) {
	statsCacheMiddlewareNowTimeFunc = func() model.Time { return model.Time(testTime.UnixMilli()) }
	now := statsCacheMiddlewareNowTimeFunc()

	statsResp := &IndexStatsResponse{
		Response: &logproto.IndexStatsResponse{
			Streams: 1,
			Chunks:  2,
			Bytes:   1 << 10,
			Entries: 10,
		},
	}

	for _, tc := range []struct {
		name                   string
		maxStatsCacheFreshness time.Duration
		req                    *logproto.IndexStatsRequest

		expectedCallsBeforeCache int
		expectedCallsAfterCache  int
		expectedResp             *IndexStatsResponse
	}{
		{
			name:                   "MaxStatsCacheFreshness disabled",
			maxStatsCacheFreshness: 0,
			req: &logproto.IndexStatsRequest{
				From:     now.Add(-1 * time.Hour),
				Through:  now.Add(-5 * time.Minute), // So we don't hit the max_cache_freshness_per_query limit (1m)
				Matchers: `{foo="bar"}`,
			},

			expectedCallsBeforeCache: 1,
			expectedCallsAfterCache:  0,
			expectedResp:             statsResp,
		},
		{
			name:                   "MaxStatsCacheFreshness enabled",
			maxStatsCacheFreshness: 30 * time.Minute,
			req: &logproto.IndexStatsRequest{
				From:     now.Add(-1 * time.Hour),
				Through:  now.Add(-5 * time.Minute), // So we don't hit the max_cache_freshness_per_query limit (1m)
				Matchers: `{foo="bar"}`,
			},

			expectedCallsBeforeCache: 1,
			expectedCallsAfterCache:  1, // The whole request is done since it wasn't cached.
			expectedResp:             statsResp,
		},
		{
			name:                   "MaxStatsCacheFreshness enabled, but request before the max freshness",
			maxStatsCacheFreshness: 30 * time.Minute,
			req: &logproto.IndexStatsRequest{
				From:     now.Add(-1 * time.Hour),
				Through:  now.Add(-45 * time.Minute),
				Matchers: `{foo="bar"}`,
			},

			expectedCallsBeforeCache: 1,
			expectedCallsAfterCache:  0,
			expectedResp:             statsResp,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := queryrangebase.ResultsCacheConfig{
				Config: resultscache.Config{
					CacheConfig: cache.Config{
						Cache: cache.NewMockCache(),
					},
				},
			}
			c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache, constants.Loki)
			defer c.Stop()
			require.NoError(t, err)

			lim := fakeLimits{maxStatsCacheFreshness: tc.maxStatsCacheFreshness}

			cacheMiddleware, err := NewIndexStatsCacheMiddleware(
				log.NewNopLogger(),
				WithSplitByLimits(lim, 24*time.Hour),
				DefaultCodec,
				c,
				nil,
				nil,
				nil,
				func(_ context.Context, _ []string, _ queryrangebase.Request) int {
					return 1
				},
				false,
				nil,
				nil,
			)
			require.NoError(t, err)

			calls, statsHandler := indexStatsResultHandler(statsResp)
			rc := cacheMiddleware.Wrap(statsHandler)

			ctx := user.InjectOrgID(context.Background(), "fake")
			resp, err := rc.Do(ctx, tc.req)
			require.NoError(t, err)
			require.Equal(t, tc.expectedCallsBeforeCache, *calls)
			require.Equal(t, tc.expectedResp, resp)

			// Doing same request again
			*calls = 0
			resp, err = rc.Do(ctx, tc.req)
			require.NoError(t, err)
			require.Equal(t, tc.expectedCallsAfterCache, *calls)
			require.Equal(t, tc.expectedResp, resp)
		})
	}
}

func indexStatsResultHandler(v *IndexStatsResponse) (*int, queryrangebase.Handler) {
	calls := 0
	return &calls, queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		calls++
		return v, nil
	})
}
