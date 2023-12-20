package queryrange

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/loghttp"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/util"
)

var (
	seriesAPIPath = "/loki/api/v1/series"
)

func TestSeriesCache(t *testing.T) {
	setupCacheMW := func() queryrangebase.Middleware {
		cacheMiddleware, err := NewSeriesCacheMiddleware(
			log.NewNopLogger(),
			WithSplitByLimits(fakeLimits{}, 24*time.Hour),
			DefaultCodec,
			cache.NewMockCache(),
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

		return cacheMiddleware
	}

	t.Run("caches the response for the same request", func(t *testing.T) {
		cacheMiddleware := setupCacheMW()
		from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))

		seriesReq := &LokiSeriesRequest{
			StartTs: from.Time(),
			EndTs:   through.Time(),
			Match:   []string{`{namespace=~".*"}`},
			Path:    seriesAPIPath,
		}

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

		called := 0
		handler := cacheMiddleware.Wrap(queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
			called++

			// should request the entire length with no partitioning as nothing is cached yet.
			require.Equal(t, seriesReq.GetStart(), r.GetStart())
			require.Equal(t, seriesReq.GetEnd(), r.GetEnd())

			return seriesResp, nil
		}))

		ctx := user.InjectOrgID(context.Background(), "fake")
		got, err := handler.Do(ctx, seriesReq)
		require.NoError(t, err)
		require.Equal(t, 1, called) // called actual handler, as not cached.
		require.Equal(t, seriesResp, got)

		// Doing same request again shouldn't change anything.
		called = 0
		got, err = handler.Do(ctx, seriesReq)
		require.NoError(t, err)
		require.Equal(t, 0, called)
		require.Equal(t, seriesResp, got)
	})

	t.Run("a new request with overlapping time range should reuse part of the previous request for the overlap", func(t *testing.T) {
		cacheMiddleware := setupCacheMW()

		from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))
		req1 := &LokiSeriesRequest{
			StartTs: from.Time(),
			EndTs:   through.Time(),
			Match:   []string{`{namespace=~".*"}`},
			Path:    seriesAPIPath,
		}
		resp1 := &LokiSeriesResponse{
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

		called := 0
		handler := cacheMiddleware.Wrap(queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
			called++

			// should request the entire length with no partitioning as nothing is cached yet.
			require.Equal(t, req1.GetStart(), r.GetStart())
			require.Equal(t, req1.GetEnd(), r.GetEnd())

			return resp1, nil
		}))

		ctx := user.InjectOrgID(context.Background(), "fake")
		got, err := handler.Do(ctx, req1)
		require.NoError(t, err)
		require.Equal(t, 1, called)
		require.Equal(t, resp1, got)

		req2 := req1.WithStartEnd(req1.GetStart().Add(15*time.Minute), req1.GetEnd().Add(15*time.Minute))

		called = 0
		handler = cacheMiddleware.Wrap(queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
			called++

			// make downstream request only for the non-overlapping portion of the query.
			require.Equal(t, req1.GetEnd(), r.GetStart())
			require.Equal(t, req1.GetEnd().Add(15*time.Minute), r.GetEnd())

			return &LokiSeriesResponse{
				Status:  "success",
				Version: uint32(loghttp.VersionV1),
				Data: []logproto.SeriesIdentifier{
					{
						Labels: map[string]string{"cluster": "us-central", "namespace": "prod"},
					},
				},
				Statistics: stats.Result{
					Summary: stats.Summary{
						Splits: 1,
					},
				},
			}, nil
		}))

		got, err = handler.Do(ctx, req2)
		require.NoError(t, err)
		require.Equal(t, 1, called)
		// two splits as we merge the results from the extent and downstream request
		resp1.Statistics.Summary.Splits = 2
		require.Equal(t, &LokiSeriesResponse{
			Status:  "success",
			Version: uint32(loghttp.VersionV1),
			Data: []logproto.SeriesIdentifier{
				{
					Labels: map[string]string{"cluster": "us-central", "namespace": "dev"},
				},
				{
					Labels: map[string]string{"cluster": "eu-west", "namespace": "prod"},
				},
				{
					Labels: map[string]string{"cluster": "us-central", "namespace": "prod"},
				},
			},
			Statistics: stats.Result{
				Summary: stats.Summary{
					Splits: 2,
				},
			},
		}, got)
	})

	t.Run("caches are only valid for the same request parameters", func(t *testing.T) {
		cacheMiddleware := setupCacheMW()

		from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))
		seriesReq := &LokiSeriesRequest{
			StartTs: from.Time(),
			EndTs:   through.Time(),
			Match:   []string{`{namespace=~".*"}`},
			Path:    seriesAPIPath,
		}
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

		called := 0
		handler := cacheMiddleware.Wrap(queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
			called++

			// should request the entire length as none of the subsequent queries hit the cache.
			require.Equal(t, seriesReq.GetStart(), r.GetStart())
			require.Equal(t, seriesReq.GetEnd(), r.GetEnd())
			return seriesResp, nil
		}))

		// initial call to fill cache
		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err := handler.Do(ctx, seriesReq)
		require.NoError(t, err)
		require.Equal(t, 1, called)

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
			called = 0
			seriesReq := seriesReq

			if tc.fn != nil {
				tc.fn(seriesReq)
			}

			if tc.user != "" {
				ctx = user.InjectOrgID(context.Background(), tc.user)
			}

			_, err = handler.Do(ctx, seriesReq)
			require.NoError(t, err)
			require.Equal(t, 1, called, name)
		}
	})
}
