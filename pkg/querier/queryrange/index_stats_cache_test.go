package queryrange

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
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
	expectedStats := &IndexStatsResponse{
		Response: &logproto.IndexStatsResponse{
			Streams: 2,
			Chunks:  4,
			Bytes:   (1<<10)*0.75 + (1 << 10),
			Entries: uint64(math.Floor(10*0.75) + 10),
		},
	}
	resp, err = rc.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, *calls)
	require.Equal(t, expectedStats, resp)
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
