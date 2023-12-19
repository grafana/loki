package queryrange

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/storage/chunk/cache/resultscache"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/constants"
)

var (
	seriesAPIPath = "/loki/api/v1/series"
)

func TestSeriesCache(t *testing.T) {
	setup := func(seriesResp *LokiSeriesResponse) (*int, queryrangebase.Handler) {
		cfg := queryrangebase.ResultsCacheConfig{
			Config: resultscache.Config{
				CacheConfig: cache.Config{
					Cache: cache.NewMockCache(),
				},
			},
		}
		c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache, constants.Loki)
		require.NoError(t, err)
		cacheMiddleware, err := NewSeriesCacheMiddleware(
			log.NewNopLogger(),
			WithSplitByLimits(fakeLimits{}, 24*time.Hour),
			DefaultCodec,
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

		calls, seriesHandler := seriesResultHandler(seriesResp)
		rc := cacheMiddleware.Wrap(seriesHandler)

		return calls, rc
	}

	t.Run("caches the response for the same request", func(t *testing.T) {
		seriesResp := &LokiSeriesResponse{
			Status:  "success",
			Version: uint32(loghttp.VersionV1),
			Data: []logproto.SeriesIdentifier{
				{
					Labels: map[string]string{"cluster": "eu-west", "namespace": "prod"},
				},
			},
			Statistics: stats.Result{
				Summary: stats.Summary{
					Splits: 1,
				},
			},
		}
		calls, handler := setup(seriesResp)

		from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))
		seriesReq := &LokiSeriesRequest{
			StartTs: from.Time(),
			EndTs:   through.Time(),
			Match:   []string{`{namespace=~".*"}`},
			Path:    seriesAPIPath,
		}

		*calls = 0
		ctx := user.InjectOrgID(context.Background(), "fake")
		resp, err := handler.Do(ctx, seriesReq)
		require.NoError(t, err)
		require.Equal(t, 1, *calls) // called actual handled, as not cached.
		require.Equal(t, seriesResp, resp)

		// Doing same request again shouldn't change anything.
		*calls = 0
		resp, err = handler.Do(ctx, seriesReq)
		require.NoError(t, err)
		require.Equal(t, 0, *calls)
		require.Equal(t, seriesResp, resp)
	})

	t.Run("a new request with overlapping time range should reuse part of the previous request for the overlap", func(t *testing.T) {
		seriesResp := &LokiSeriesResponse{
			Status:  "success",
			Version: uint32(loghttp.VersionV1),
			Data: []logproto.SeriesIdentifier{
				{
					Labels: map[string]string{"cluster": "us-central", "namespace": "dev"},
				},
				{
					Labels: map[string]string{"cluster": "eu-west", "namespace": "prod"},
				},
			},
			Statistics: stats.Result{
				Summary: stats.Summary{
					Splits: 1,
				},
			},
		}
		calls, handler := setup(seriesResp)

		from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))
		seriesReq := &LokiSeriesRequest{
			StartTs: from.Time(),
			EndTs:   through.Time(),
			Match:   []string{`{namespace=~".*"}`},
			Path:    seriesAPIPath,
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		resp, err := handler.Do(ctx, seriesReq)
		require.NoError(t, err)
		require.Equal(t, 1, *calls)
		require.Equal(t, seriesResp, resp)

		*calls = 0
		req := seriesReq.WithStartEnd(seriesReq.GetStart().Add(15*time.Minute), seriesReq.GetEnd().Add(15*time.Minute))

		resp, err = handler.Do(ctx, req)
		require.NoError(t, err)
		require.Equal(t, 1, *calls)
		// two splits as we merge the results from the extent and downstream request
		seriesResp.Statistics.Summary.Splits = 2
		require.Equal(t, seriesResp, resp)
	})

	// t.Run("caches are only valid for the same request parameters", func(t *testing.T) {
	// 	seriesResp := &LokiSeriesResponse{
	// 		Response: &logproto.LokiSeriesResponse{
	// 			Volumes: []logproto.Volume{
	// 				{
	// 					Name:   `{foo="bar"}`,
	// 					Volume: 42,
	// 				},
	// 			},
	// 			Limit: 10,
	// 		},
	// 	}
	// 	calls, handler := setup(seriesResp)

	// 	// initial call to fill cache
	// 	from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))
	// 	seriesReq := &logproto.VolumeRequest{
	// 		From:     from,
	// 		Through:  through,
	// 		Matchers: `{foo="bar"}`,
	// 		Limit:    10,
	// 		Step:     1,
	// 	}

	// 	ctx := user.InjectOrgID(context.Background(), "fake")
	// 	_, err := handler.Do(ctx, seriesReq)
	// 	require.NoError(t, err)
	// 	require.Equal(t, 1, *calls)

	// 	type testCase struct {
	// 		fn func(*logproto.VolumeRequest)
	// 	}
	// 	testCases := map[string]testCase{
	// 		"different step": {
	// 			fn: func(req *logproto.VolumeRequest) {
	// 				req.Step = 2
	// 			},
	// 		},
	// 		"new limit": {
	// 			fn: func(req *logproto.VolumeRequest) {
	// 				req.Limit = 11
	// 			},
	// 		},
	// 		"aggregate by labels": {
	// 			fn: func(req *logproto.VolumeRequest) {
	// 				req.AggregateBy = seriesvolume.Labels
	// 			},
	// 		},
	// 		"target labels": {
	// 			fn: func(req *logproto.VolumeRequest) {
	// 				req.TargetLabels = []string{"foo"}
	// 			},
	// 		},
	// 	}

	// 	for name, tc := range testCases {
	// 		*calls = 0

	// 		seriesReq := &logproto.VolumeRequest{
	// 			From:     from,
	// 			Through:  through,
	// 			Matchers: `{foo="bar"}`,
	// 			Limit:    10,
	// 			Step:     1,
	// 		}
	// 		tc.fn(seriesReq)

	// 		_, err = handler.Do(ctx, seriesReq)
	// 		require.NoError(t, err)
	// 		require.Equal(t, 1, *calls, name)
	// 	}
	// })
}

// func TestVolumeCache_RecentData(t *testing.T) {
// 	volumeCacheMiddlewareNowTimeFunc = func() model.Time { return model.Time(testTime.UnixMilli()) }
// 	now := volumeCacheMiddlewareNowTimeFunc()

// 	seriesResp := &LokiSeriesResponse{
// 		Response: &logproto.LokiSeriesResponse{
// 			Volumes: []logproto.Volume{
// 				{
// 					Name:   `{foo="bar"}`,
// 					Volume: 42,
// 				},
// 			},
// 			Limit: 10,
// 		},
// 	}

// 	for _, tc := range []struct {
// 		name                   string
// 		maxStatsCacheFreshness time.Duration
// 		req                    *logproto.VolumeRequest

// 		expectedCallsBeforeCache int
// 		expectedCallsAfterCache  int
// 		expectedResp             *LokiSeriesResponse
// 	}{
// 		{
// 			name:                   "MaxStatsCacheFreshness disabled",
// 			maxStatsCacheFreshness: 0,
// 			req: &logproto.VolumeRequest{
// 				From:     now.Add(-1 * time.Hour),
// 				Through:  now.Add(-5 * time.Minute), // So we don't hit the max_cache_freshness_per_query limit (1m)
// 				Matchers: `{foo="bar"}`,
// 				Limit:    10,
// 			},

// 			expectedCallsBeforeCache: 1,
// 			expectedCallsAfterCache:  0,
// 			expectedResp:             seriesResp,
// 		},
// 		{
// 			name:                   "MaxStatsCacheFreshness enabled",
// 			maxStatsCacheFreshness: 30 * time.Minute,
// 			req: &logproto.VolumeRequest{
// 				From:     now.Add(-1 * time.Hour),
// 				Through:  now.Add(-5 * time.Minute), // So we don't hit the max_cache_freshness_per_query limit (1m)
// 				Matchers: `{foo="bar"}`,
// 				Limit:    10,
// 			},

// 			expectedCallsBeforeCache: 1,
// 			expectedCallsAfterCache:  1, // The whole request is done since it wasn't cached.
// 			expectedResp:             seriesResp,
// 		},
// 		{
// 			name:                   "MaxStatsCacheFreshness enabled, but request before the max freshness",
// 			maxStatsCacheFreshness: 30 * time.Minute,
// 			req: &logproto.VolumeRequest{
// 				From:     now.Add(-1 * time.Hour),
// 				Through:  now.Add(-45 * time.Minute),
// 				Matchers: `{foo="bar"}`,
// 				Limit:    10,
// 			},

// 			expectedCallsBeforeCache: 1,
// 			expectedCallsAfterCache:  0,
// 			expectedResp:             seriesResp,
// 		},
// 	} {
// 		t.Run(tc.name, func(t *testing.T) {
// 			cfg := queryrangebase.ResultsCacheConfig{
// 				Config: resultscache.Config{
// 					CacheConfig: cache.Config{
// 						Cache: cache.NewMockCache(),
// 					},
// 				},
// 			}
// 			c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache, constants.Loki)
// 			defer c.Stop()
// 			require.NoError(t, err)

// 			lim := fakeLimits{maxStatsCacheFreshness: tc.maxStatsCacheFreshness}

// 			cacheMiddleware, err := NewVolumeCacheMiddleware(
// 				log.NewNopLogger(),
// 				WithSplitByLimits(lim, 24*time.Hour),
// 				DefaultCodec,
// 				c,
// 				nil,
// 				nil,
// 				func(_ context.Context, _ []string, _ queryrangebase.Request) int {
// 					return 1
// 				},
// 				false,
// 				nil,
// 				nil,
// 			)
// 			require.NoError(t, err)

// 			calls, statsHandler := volumeResultHandler(seriesResp)
// 			rc := cacheMiddleware.Wrap(statsHandler)

// 			ctx := user.InjectOrgID(context.Background(), "fake")
// 			resp, err := rc.Do(ctx, tc.req)
// 			require.NoError(t, err)
// 			require.Equal(t, tc.expectedCallsBeforeCache, *calls)
// 			require.Equal(t, tc.expectedResp, resp)

// 			// Doing same request again
// 			*calls = 0
// 			resp, err = rc.Do(ctx, tc.req)
// 			require.NoError(t, err)
// 			require.Equal(t, tc.expectedCallsAfterCache, *calls)
// 			require.Equal(t, tc.expectedResp, resp)
// 		})
// 	}
// }

func seriesResultHandler(v *LokiSeriesResponse) (*int, queryrangebase.Handler) {
	calls := 0
	return &calls, queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		calls++
		return v, nil
	})
}
