package queryrange

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/util"
)

func TestVolumeCache(t *testing.T) {
	cfg := queryrangebase.ResultsCacheConfig{
		CacheConfig: cache.Config{
			Cache: cache.NewMockCache(),
		},
	}
	c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache)
	require.NoError(t, err)
	cacheMiddleware, err := NewVolumeCacheMiddleware(
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

	from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))
	volReq := &logproto.VolumeRequest{
		From:     from,
		Through:  through,
		Matchers: `{foo="bar"}`,
		Limit:    10,
	}

	volResp := &VolumeResponse{
		Response: &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:   `{foo="bar"}`,
					Volume: 42,
				},
			},
			Limit: 10,
		},
	}

	calls, volHandler := volumeResultHandler(volResp)
	rc := cacheMiddleware.Wrap(volHandler)

	ctx := user.InjectOrgID(context.Background(), "fake")
	resp, err := rc.Do(ctx, volReq)
	require.NoError(t, err)
	require.Equal(t, 1, *calls)
	require.Equal(t, volResp, resp)

	// Doing same request again shouldn't change anything.
	*calls = 0
	resp, err = rc.Do(ctx, volReq)
	require.NoError(t, err)
	require.Equal(t, 0, *calls)
	require.Equal(t, volResp, resp)

	// Doing a request with new start later than the previous query and end time later than the previous one,
	// should reuse part of the previous request and issue a new request for the remaining time till end.
	// The new start time is 15m (i.e. 25%) in the future with regard to the previous request time span.
	*calls = 0
	req := volReq.WithStartEnd(volReq.GetStart()+(15*time.Minute).Milliseconds(), volReq.GetEnd()+(15*time.Minute).Milliseconds())
	vol := float64(0.75)
	expectedVol := &VolumeResponse{
		Response: &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:   `{foo="bar"}`,
					Volume: uint64(vol*float64(42)) + 42,
				},
			},
			Limit: 10,
		},
	}

	resp, err = rc.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, *calls)
	require.Equal(t, expectedVol, resp)
}

func TestVolumeCache_RecentData(t *testing.T) {
	volumeCacheMiddlewareNowTimeFunc = func() model.Time { return model.Time(testTime.UnixMilli()) }
	now := volumeCacheMiddlewareNowTimeFunc()

	volResp := &VolumeResponse{
		Response: &logproto.VolumeResponse{
			Volumes: []logproto.Volume{
				{
					Name:   `{foo="bar"}`,
					Volume: 42,
				},
			},
			Limit: 10,
		},
	}

	for _, tc := range []struct {
		name                   string
		maxStatsCacheFreshness time.Duration
		req                    *logproto.VolumeRequest

		expectedCallsBeforeCache int
		expectedCallsAfterCache  int
		expectedResp             *VolumeResponse
	}{
		{
			name:                   "MaxStatsCacheFreshness disabled",
			maxStatsCacheFreshness: 0,
			req: &logproto.VolumeRequest{
				From:     now.Add(-1 * time.Hour),
				Through:  now.Add(-5 * time.Minute), // So we don't hit the max_cache_freshness_per_query limit (1m)
				Matchers: `{foo="bar"}`,
				Limit:    10,
			},

			expectedCallsBeforeCache: 1,
			expectedCallsAfterCache:  0,
			expectedResp:             volResp,
		},
		{
			name:                   "MaxStatsCacheFreshness enabled",
			maxStatsCacheFreshness: 30 * time.Minute,
			req: &logproto.VolumeRequest{
				From:     now.Add(-1 * time.Hour),
				Through:  now.Add(-5 * time.Minute), // So we don't hit the max_cache_freshness_per_query limit (1m)
				Matchers: `{foo="bar"}`,
				Limit:    10,
			},

			expectedCallsBeforeCache: 1,
			expectedCallsAfterCache:  1, // The whole request is done since it wasn't cached.
			expectedResp:             volResp,
		},
		{
			name:                   "MaxStatsCacheFreshness enabled, but request before the max freshness",
			maxStatsCacheFreshness: 30 * time.Minute,
			req: &logproto.VolumeRequest{
				From:     now.Add(-1 * time.Hour),
				Through:  now.Add(-45 * time.Minute),
				Matchers: `{foo="bar"}`,
				Limit:    10,
			},

			expectedCallsBeforeCache: 1,
			expectedCallsAfterCache:  0,
			expectedResp:             volResp,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := queryrangebase.ResultsCacheConfig{
				CacheConfig: cache.Config{
					Cache: cache.NewMockCache(),
				},
			}
			c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache)
			defer c.Stop()
			require.NoError(t, err)

			lim := fakeLimits{maxStatsCacheFreshness: tc.maxStatsCacheFreshness}

			cacheMiddleware, err := NewVolumeCacheMiddleware(
				log.NewNopLogger(),
				WithSplitByLimits(lim, 24*time.Hour),
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

			calls, statsHandler := volumeResultHandler(volResp)
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

func volumeResultHandler(v *VolumeResponse) (*int, queryrangebase.Handler) {
	calls := 0
	return &calls, queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		calls++
		return v, nil
	})
}
