package queryrange

import (
	"context"
	"fmt"
	"regexp"
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

func TestCacheKeySeries_GenerateCacheKey(t *testing.T) {
	k := cacheKeySeries{
		transformer: nil,
		Limits: fakeLimits{
			metadataSplitDuration: map[string]time.Duration{
				"fake": time.Hour,
			},
		},
	}

	from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))
	req := &LokiSeriesRequest{
		StartTs: from.Time(),
		EndTs:   through.Time(),
		Match:   []string{`{namespace="prod"}`, `{service="foo"}`},
		Path:    seriesAPIPath,
	}

	expectedInterval := testTime.UnixMilli() / time.Hour.Milliseconds()
	require.Equal(t, fmt.Sprintf(`series:fake:{namespace="prod"},{service="foo"}:%d:%d`, expectedInterval, time.Hour.Nanoseconds()), k.GenerateCacheKey(context.Background(), "fake", req))

	t.Run("same set of matchers in any order should result in the same cache key", func(t *testing.T) {
		from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))

		for _, matchers := range [][]string{
			{`{cluster="us-central"}`, `{namespace="prod"}`, `{service=~"foo.*"}`},
			{`{namespace="prod"}`, `{service=~"foo.*"}`, `{cluster="us-central"}`},
		} {
			req := &LokiSeriesRequest{
				StartTs: from.Time(),
				EndTs:   through.Time(),
				Match:   matchers,
				Path:    seriesAPIPath,
			}
			expectedInterval := testTime.UnixMilli() / time.Hour.Milliseconds()
			require.Equal(t, fmt.Sprintf(`series:fake:{cluster="us-central"},{namespace="prod"},{service=~"foo.*"}:%d:%d`, expectedInterval, time.Hour.Nanoseconds()), k.GenerateCacheKey(context.Background(), "fake", req))
		}

	})
}

func TestSeriesCache(t *testing.T) {
	setupCacheMW := func() queryrangebase.Middleware {
		cacheMiddleware, err := NewSeriesCacheMiddleware(
			log.NewNopLogger(),
			fakeLimits{
				metadataSplitDuration: map[string]time.Duration{
					"fake": 24 * time.Hour,
				},
			},
			DefaultCodec,
			cache.NewMockCache(),
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
					Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "cluster", Value: "eu-west"}, {Key: "namespace", Value: "prod"}},
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
					Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "cluster", Value: "us-central"}, {Key: "namespace", Value: "dev"}},
				},
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "cluster", Value: "eu-west"}, {Key: "namespace", Value: "prod"}},
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
						Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "cluster", Value: "us-central"}, {Key: "namespace", Value: "prod"}},
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
					Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "cluster", Value: "us-central"}, {Key: "namespace", Value: "dev"}},
				},
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "cluster", Value: "eu-west"}, {Key: "namespace", Value: "prod"}},
				},
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "cluster", Value: "us-central"}, {Key: "namespace", Value: "prod"}},
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
					Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "cluster", Value: "eu-west"}, {Key: "namespace", Value: "prod"}},
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

func TestSeriesCache_freshness(t *testing.T) {
	testTime := time.Now().Add(-1 * time.Hour)
	from, through := util.RoundToMilliseconds(testTime.Add(-1*time.Hour), testTime)

	for _, tt := range []struct {
		name                      string
		req                       *LokiSeriesRequest
		shouldCache               bool
		maxMetadataCacheFreshness time.Duration
	}{
		{
			name: "max metadata freshness not set",
			req: &LokiSeriesRequest{
				StartTs: from.Time(),
				EndTs:   through.Time(),
				Match:   []string{`{namespace=~".*"}`},
				Path:    seriesAPIPath,
			},
			shouldCache: true,
		},
		{
			name: "req overlaps with max cache freshness window",
			req: &LokiSeriesRequest{
				StartTs: from.Time(),
				EndTs:   through.Time(),
				Match:   []string{`{namespace=~".*"}`},
				Path:    seriesAPIPath,
			},
			maxMetadataCacheFreshness: 24 * time.Hour,
			shouldCache:               false,
		},
		{
			name: "req does not overlap max cache freshness window",
			req: &LokiSeriesRequest{
				StartTs: from.Add(-24 * time.Hour).Time(),
				EndTs:   through.Add(-24 * time.Hour).Time(),
				Match:   []string{`{namespace=~".*"}`},
				Path:    seriesAPIPath,
			},
			maxMetadataCacheFreshness: 24 * time.Hour,
			shouldCache:               true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cacheMiddleware, err := NewSeriesCacheMiddleware(
				log.NewNopLogger(),
				fakeLimits{
					metadataSplitDuration: map[string]time.Duration{
						"fake": 24 * time.Hour,
					},
					maxMetadataCacheFreshness: tt.maxMetadataCacheFreshness,
				},
				DefaultCodec,
				cache.NewMockCache(),
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

			seriesResp := &LokiSeriesResponse{
				Status:  "success",
				Version: uint32(loghttp.VersionV1),
				Data: []logproto.SeriesIdentifier{
					{
						Labels: []logproto.SeriesIdentifier_LabelsEntry{{Key: "cluster", Value: "eu-west"}, {Key: "namespace", Value: "prod"}},
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
				require.Equal(t, tt.req.GetStart(), r.GetStart())
				require.Equal(t, tt.req.GetEnd(), r.GetEnd())

				return seriesResp, nil
			}))

			ctx := user.InjectOrgID(context.Background(), "fake")
			got, err := handler.Do(ctx, tt.req)
			require.NoError(t, err)
			require.Equal(t, 1, called) // called actual handler, as not cached.
			require.Equal(t, seriesResp, got)

			called = 0
			got, err = handler.Do(ctx, tt.req)
			require.NoError(t, err)
			if !tt.shouldCache {
				require.Equal(t, 1, called)
			} else {
				require.Equal(t, 0, called)
			}
			require.Equal(t, seriesResp, got)
		})
	}
}

func TestSeriesQueryCacheKey(t *testing.T) {
	const (
		defaultTenant       = "a"
		alternateTenant     = "b"
		defaultSplit        = time.Hour
		ingesterSplit       = 90 * time.Minute
		ingesterQueryWindow = defaultSplit * 3
	)

	l := fakeLimits{
		metadataSplitDuration: map[string]time.Duration{defaultTenant: defaultSplit, alternateTenant: defaultSplit},
		ingesterSplitDuration: map[string]time.Duration{defaultTenant: ingesterSplit},
	}

	cases := []struct {
		name, tenantID string
		start, end     time.Time
		expectedSplit  time.Duration
		iqo            util.IngesterQueryOptions
		values         bool
	}{
		{
			name:          "outside ingester query window",
			tenantID:      defaultTenant,
			start:         time.Now().Add(-6 * time.Hour),
			end:           time.Now().Add(-5 * time.Hour),
			expectedSplit: defaultSplit,
			iqo: ingesterQueryOpts{
				queryIngestersWithin: ingesterQueryWindow,
				queryStoreOnly:       false,
			},
		},
		{
			name:          "within ingester query window",
			tenantID:      defaultTenant,
			start:         time.Now().Add(-6 * time.Hour),
			end:           time.Now().Add(-ingesterQueryWindow / 2),
			expectedSplit: ingesterSplit,
			iqo: ingesterQueryOpts{
				queryIngestersWithin: ingesterQueryWindow,
				queryStoreOnly:       false,
			},
		},
		{
			name:          "within ingester query window, but query store only",
			tenantID:      defaultTenant,
			start:         time.Now().Add(-6 * time.Hour),
			end:           time.Now().Add(-ingesterQueryWindow / 2),
			expectedSplit: defaultSplit,
			iqo: ingesterQueryOpts{
				queryIngestersWithin: ingesterQueryWindow,
				queryStoreOnly:       true,
			},
		},
		{
			name:          "within ingester query window, but no ingester split duration configured",
			tenantID:      alternateTenant,
			start:         time.Now().Add(-6 * time.Hour),
			end:           time.Now().Add(-ingesterQueryWindow / 2),
			expectedSplit: defaultSplit,
			iqo: ingesterQueryOpts{
				queryIngestersWithin: ingesterQueryWindow,
				queryStoreOnly:       false,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matchers := []string{`{namespace="prod"}`, `{service="foo"}`}

			keyGen := cacheKeySeries{l, nil, tc.iqo}

			r := &LokiSeriesRequest{
				StartTs: tc.start,
				EndTs:   tc.end,
				Match:   matchers,
				Path:    seriesAPIPath,
			}

			// we use regex here because cache key always refers to the current time to get the ingester query window,
			// and therefore we can't know the current interval apriori without duplicating the logic
			pattern := regexp.MustCompile(fmt.Sprintf(`series:%s:%s:(\d+):%d`, tc.tenantID, regexp.QuoteMeta(keyGen.joinMatchers(matchers)), tc.expectedSplit))

			require.Regexp(t, pattern, keyGen.GenerateCacheKey(context.Background(), tc.tenantID, r))
		})
	}
}
