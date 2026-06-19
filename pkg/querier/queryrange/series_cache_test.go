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

	"github.com/grafana/loki/v3/pkg/loghttp"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util"
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

	composeSeriesResp := func(series [][]logproto.SeriesIdentifier_LabelsEntry, splits int64) *LokiSeriesResponse {
		var data []logproto.SeriesIdentifier
		for _, v := range series {
			data = append(data, logproto.SeriesIdentifier{Labels: v})
		}

		return &LokiSeriesResponse{
			Status:  "success",
			Version: uint32(loghttp.VersionV1),
			Data:    data,
			Statistics: stats.Result{
				Summary: stats.Summary{
					Splits: splits,
				},
			},
		}
	}

	var downstreamHandlerFunc func(context.Context, queryrangebase.Request) (queryrangebase.Response, error)
	downstreamHandler := &mockDownstreamHandler{fn: func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		return downstreamHandlerFunc(ctx, req)
	}}

	from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))
	seriesReq := &LokiSeriesRequest{
		StartTs: from.Time(),
		EndTs:   through.Time(),
		Match:   []string{`{namespace=~".*"}`},
		Path:    seriesAPIPath,
	}
	seriesResp := composeSeriesResp([][]logproto.SeriesIdentifier_LabelsEntry{
		{{Key: "cluster", Value: "us-central"}, {Key: "namespace", Value: "dev"}},
		{{Key: "cluster", Value: "eu-west"}, {Key: "namespace", Value: "prod"}},
	}, 1)

	for _, tc := range []struct {
		name                                 string
		req                                  queryrangebase.Request
		expectedQueryStart, expectedQueryEnd time.Time
		downstreamResponse                   *LokiSeriesResponse
		downstreamCalls                      int
		expectedReponse                      *LokiSeriesResponse
	}{
		{
			name:            "return cached response for the same request",
			downstreamCalls: 0,
			expectedReponse: seriesResp,
			req:             seriesReq,
		},
		{
			name:               "a new request with overlapping time range should reuse results of the previous request",
			req:                seriesReq.WithStartEnd(seriesReq.GetStart(), seriesReq.GetEnd().Add(15*time.Minute)),
			expectedQueryStart: seriesReq.GetEnd(),
			expectedQueryEnd:   seriesReq.GetEnd().Add(15 * time.Minute),
			downstreamCalls:    1,
			downstreamResponse: composeSeriesResp([][]logproto.SeriesIdentifier_LabelsEntry{
				{{Key: "cluster", Value: "us-central"}, {Key: "namespace", Value: "prod"}},
			}, 1),
			expectedReponse: composeSeriesResp([][]logproto.SeriesIdentifier_LabelsEntry{
				{{Key: "cluster", Value: "us-central"}, {Key: "namespace", Value: "dev"}},
				{{Key: "cluster", Value: "eu-west"}, {Key: "namespace", Value: "prod"}},
				{{Key: "cluster", Value: "us-central"}, {Key: "namespace", Value: "prod"}},
			}, 2),
		},
		{
			// To avoid returning incorrect results, we only use extents that are entirely within the requested query range.
			name:               "cached response not entirely within the requested range",
			req:                seriesReq.WithStartEnd(seriesReq.GetStart().Add(15*time.Minute), seriesReq.GetEnd().Add(-15*time.Minute)),
			expectedQueryStart: seriesReq.GetStart().Add(15 * time.Minute),
			expectedQueryEnd:   seriesReq.GetEnd().Add(-15 * time.Minute),
			downstreamCalls:    1,
			downstreamResponse: composeSeriesResp([][]logproto.SeriesIdentifier_LabelsEntry{
				{{Key: "cluster", Value: "us-central"}, {Key: "namespace", Value: "prod"}},
			}, 1),
			expectedReponse: composeSeriesResp([][]logproto.SeriesIdentifier_LabelsEntry{
				{{Key: "cluster", Value: "us-central"}, {Key: "namespace", Value: "prod"}},
			}, 1),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cacheMiddleware := setupCacheMW()
			downstreamHandler.ResetCount()
			downstreamHandlerFunc = func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
				require.Equal(t, seriesReq.GetStart(), r.GetStart())
				require.Equal(t, seriesReq.GetEnd(), r.GetEnd())

				return seriesResp, nil
			}

			handler := cacheMiddleware.Wrap(downstreamHandler)

			ctx := user.InjectOrgID(context.Background(), "fake")
			got, err := handler.Do(ctx, seriesReq)
			require.NoError(t, err)
			require.Equal(t, 1, downstreamHandler.Called()) // calls downstream handler, as not cached.
			require.Equal(t, seriesResp, got)

			downstreamHandler.ResetCount()
			downstreamHandlerFunc = func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
				require.Equal(t, tc.expectedQueryStart, r.GetStart())
				require.Equal(t, tc.expectedQueryEnd, r.GetEnd())

				return tc.downstreamResponse, nil
			}

			got, err = handler.Do(ctx, tc.req)
			require.NoError(t, err)
			require.Equal(t, tc.downstreamCalls, downstreamHandler.Called())
			require.Equal(t, tc.expectedReponse, got)
		})
	}

	t.Run("caches are only valid for the same request parameters", func(t *testing.T) {
		cacheMiddleware := setupCacheMW()
		downstreamHandler.ResetCount()
		downstreamHandlerFunc = func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
			require.Equal(t, seriesReq.GetStart(), r.GetStart())
			require.Equal(t, seriesReq.GetEnd(), r.GetEnd())

			return seriesResp, nil
		}

		handler := cacheMiddleware.Wrap(downstreamHandler)

		// initial call to fill cache
		ctx := user.InjectOrgID(context.Background(), "fake")
		_, err := handler.Do(ctx, seriesReq)
		require.NoError(t, err)
		require.Equal(t, 1, downstreamHandler.Called())

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
			downstreamHandler.ResetCount()
			seriesReq := seriesReq

			if tc.fn != nil {
				tc.fn(seriesReq)
			}

			if tc.user != "" {
				ctx = user.InjectOrgID(context.Background(), tc.user)
			}

			_, err = handler.Do(ctx, seriesReq)
			require.NoError(t, err)
			require.Equal(t, 1, downstreamHandler.Called(), name)
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
		defaultSplit                = time.Hour
		recentMetadataSplitDuration = 30 * time.Minute
		recentMetadataQueryWindow   = time.Hour
	)

	l := fakeLimits{
		metadataSplitDuration:       map[string]time.Duration{tenantID: defaultSplit},
		recentMetadataSplitDuration: map[string]time.Duration{tenantID: recentMetadataSplitDuration},
		recentMetadataQueryWindow:   map[string]time.Duration{tenantID: recentMetadataQueryWindow},
	}

	cases := []struct {
		name          string
		start, end    time.Time
		expectedSplit time.Duration
		values        bool
		limits        Limits
	}{
		{
			name:          "outside recent metadata query window",
			start:         time.Now().Add(-3 * time.Hour),
			end:           time.Now().Add(-2 * time.Hour),
			expectedSplit: defaultSplit,
			limits:        l,
		},
		{
			name:          "within recent metadata query window",
			start:         time.Now().Add(-30 * time.Minute),
			end:           time.Now(),
			expectedSplit: recentMetadataSplitDuration,
			limits:        l,
		},
		{
			name:          "within recent metadata query window, but recent split duration is not configured",
			start:         time.Now().Add(-30 * time.Minute),
			end:           time.Now(),
			expectedSplit: defaultSplit,
			limits: fakeLimits{
				metadataSplitDuration:     map[string]time.Duration{tenantID: defaultSplit},
				recentMetadataQueryWindow: map[string]time.Duration{tenantID: recentMetadataQueryWindow},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matchers := []string{`{namespace="prod"}`, `{service="foo"}`}

			keyGen := cacheKeySeries{tc.limits, nil}

			r := &LokiSeriesRequest{
				StartTs: tc.start,
				EndTs:   tc.end,
				Match:   matchers,
				Path:    seriesAPIPath,
			}

			// we use regex here because cache key always refers to the current time to get the ingester query window,
			// and therefore we can't know the current interval apriori without duplicating the logic
			pattern := regexp.MustCompile(fmt.Sprintf(`series:%s:%s:(\d+):%d`, tenantID, regexp.QuoteMeta(keyGen.joinMatchers(matchers)), tc.expectedSplit))

			require.Regexp(t, pattern, keyGen.GenerateCacheKey(context.Background(), tenantID, r))
		})
	}
}

type mockDownstreamHandler struct {
	called int
	fn     func(context.Context, queryrangebase.Request) (queryrangebase.Response, error)
}

func (m *mockDownstreamHandler) Called() int {
	return m.called
}

func (m *mockDownstreamHandler) ResetCount() {
	m.called = 0
}

func (m *mockDownstreamHandler) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	m.called++
	return m.fn(ctx, req)
}
