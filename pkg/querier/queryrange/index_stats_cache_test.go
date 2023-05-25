package queryrange

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/util"
)

var cfg = IndexStatsCacheConfig{
	ResultsCacheConfig: queryrangebase.ResultsCacheConfig{
		CacheConfig: cache.Config{
			Cache: cache.NewMockCache(),
		},
	},
}

func TestIndexStatsCache(t *testing.T) {
	c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache)
	require.NoError(t, err)
	cacheMiddleware, err := NewIndexStatsCacheMiddleware(
		cfg,
		log.NewNopLogger(),
		WithSplitByLimits(fakeLimits{}, 24*time.Hour),
		LokiCodec,
		c,
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
	req := statsReq.WithStartEnd(statsReq.GetStart()+(15*time.Minute).Milliseconds(), statsReq.GetEnd()+(15*time.Minute).Milliseconds())
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
	for _, tc := range []struct {
		name                    string
		doNotCacheRequestWithin time.Duration
		expectedCalls           int
	}{
		{
			name:                    "DoNotCacheRequestWithin disabled",
			doNotCacheRequestWithin: 0,
			expectedCalls:           0,
		},
		{
			name:                    "DoNotCacheRequestWithin enabled",
			doNotCacheRequestWithin: 30 * time.Minute,
			expectedCalls:           1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache)
			defer c.Stop()
			require.NoError(t, err)

			statsCacheCfg := cfg
			statsCacheCfg.DoNotCacheRequestWithin = tc.doNotCacheRequestWithin

			cacheMiddleware, err := NewIndexStatsCacheMiddleware(
				statsCacheCfg,
				log.NewNopLogger(),
				WithSplitByLimits(fakeLimits{}, 24*time.Hour),
				LokiCodec,
				c,
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

			statsCacheMiddlewareNowTimeFunc = func() model.Time { return model.Time(testTime.UnixMilli()) }
			now := statsCacheMiddlewareNowTimeFunc()

			statsReq := &logproto.IndexStatsRequest{
				From:     now.Add(-1 * time.Hour),
				Through:  now.Add(-5 * time.Minute), // So we don't hit the max_cache_freshness_per_query limit (1m)
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

			// Doing same request again
			*calls = 0
			resp, err = rc.Do(ctx, statsReq)
			require.NoError(t, err)
			require.Equal(t, tc.expectedCalls, *calls)
			require.Equal(t, statsResp, resp)
		})
	}
}

func indexStatsResultHandler(v *IndexStatsResponse) (*int, queryrangebase.Handler) {
	calls := 0
	return &calls, queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		calls++
		return v, nil
	})
}
