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

	t.Run("caches are only valid for the same request parameters", func(t *testing.T) {
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

		// initial call to fill cache
		from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))
		seriesReq := &LokiSeriesRequest{
			StartTs: from.Time(),
			EndTs:   through.Time(),
			Match:   []string{`{namespace=~".*"}`},
			Path:    seriesAPIPath,
		}

		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err := handler.Do(ctx, seriesReq)
		require.NoError(t, err)
		require.Equal(t, 1, *calls)

		type testCase struct {
			fn   func(*LokiSeriesRequest)
			user string
		}
		testCases := map[string]testCase{
			"different match": {
				fn: func(req *LokiSeriesRequest) {
					req.Match = append(req.Match, `{foo="bar"}`)
				},
			},
			"different user": {
				user: "fake2s",
			},
		}

		for name, tc := range testCases {
			*calls = 0

			seriesReq := &LokiSeriesRequest{
				StartTs: from.Time(),
				EndTs:   through.Time(),
				Match:   []string{`{foo="bar"}`},
			}
			if tc.fn != nil {
				tc.fn(seriesReq)
			}

			if tc.user != "" {
				ctx = user.InjectOrgID(context.Background(), tc.user)
			}

			_, err = handler.Do(ctx, seriesReq)
			require.NoError(t, err)
			require.Equal(t, 1, *calls, name)
		}
	})
}

func seriesResultHandler(v *LokiSeriesResponse) (*int, queryrangebase.Handler) {
	calls := 0
	return &calls, queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		calls++
		return v, nil
	})
}
