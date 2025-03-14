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

func TestCacheKeyLabels_GenerateCacheKey(t *testing.T) {
	k := cacheKeyLabels{
		transformer: nil,
		Limits: fakeLimits{
			metadataSplitDuration: map[string]time.Duration{
				"fake": time.Hour,
			},
		},
	}

	from, through := util.RoundToMilliseconds(testTime, testTime.Add(1*time.Hour))
	start := from.Time()
	end := through.Time()

	req := LabelRequest{
		LabelRequest: logproto.LabelRequest{
			Start: &start,
			End:   &end,
		},
	}

	expectedInterval := testTime.UnixMilli() / time.Hour.Milliseconds()

	t.Run("labels", func(t *testing.T) {
		require.Equal(t, fmt.Sprintf(`labels:fake::%d:%d`, expectedInterval, time.Hour.Nanoseconds()), k.GenerateCacheKey(context.Background(), "fake", &req))

		req.Query = `{cluster="eu-west1"}`
		require.Equal(t, fmt.Sprintf(`labels:fake:{cluster="eu-west1"}:%d:%d`, expectedInterval, time.Hour.Nanoseconds()), k.GenerateCacheKey(context.Background(), "fake", &req))
	})

	t.Run("label values", func(t *testing.T) {
		req := req
		req.Name = "foo"
		req.Values = true
		req.Query = ``
		require.Equal(t, fmt.Sprintf(`labelvalues:fake:foo::%d:%d`, expectedInterval, time.Hour.Nanoseconds()), k.GenerateCacheKey(context.Background(), "fake", &req))

		req.Query = `{cluster="eu-west1"}`
		require.Equal(t, fmt.Sprintf(`labelvalues:fake:foo:{cluster="eu-west1"}:%d:%d`, expectedInterval, time.Hour.Nanoseconds()), k.GenerateCacheKey(context.Background(), "fake", &req))
	})
}

func TestLabelsCache(t *testing.T) {
	setupCacheMW := func() queryrangebase.Middleware {
		cacheMiddleware, err := NewLabelsCacheMiddleware(
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

	composeLabelsResp := func(lbls []string, splits int64) *LokiLabelNamesResponse {
		return &LokiLabelNamesResponse{
			Status:  "success",
			Version: uint32(loghttp.VersionV1),
			Data:    lbls,
			Statistics: stats.Result{
				Summary: stats.Summary{
					Splits: splits,
				},
			},
		}

	}

	start := testTime.Truncate(time.Millisecond)
	end := start.Add(time.Hour)
	labelsReq := &LabelRequest{
		LabelRequest: logproto.LabelRequest{
			Start: &start,
			End:   &end,
		},
	}
	labelsResp := composeLabelsResp([]string{"bar", "buzz"}, 1)

	var downstreamHandlerFunc func(context.Context, queryrangebase.Request) (queryrangebase.Response, error)
	downstreamHandler := &mockDownstreamHandler{fn: func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		return downstreamHandlerFunc(ctx, req)
	}}

	for _, values := range []bool{false, true} {
		labelsReq := labelsReq
		prefix := "labels"

		if values {
			prefix = "label values: "
			labelsReq.Values = true
			labelsReq.Name = "foo"
			labelsReq.Query = `{cluster="eu-west1"}`
		}

		for _, tc := range []struct {
			name                                 string
			req                                  queryrangebase.Request
			expectedQueryStart, expectedQueryEnd time.Time
			downstreamResponse                   *LokiLabelNamesResponse
			downstreamCalls                      int
			expectedReponse                      *LokiLabelNamesResponse
		}{
			{
				name:            "return cached response for the same request",
				downstreamCalls: 0,
				expectedReponse: labelsResp,
				req:             labelsReq,
			},
			{
				name:               "a new request with overlapping time range should reuse results of the previous request",
				req:                labelsReq.WithStartEnd(labelsReq.GetStart(), labelsReq.GetEnd().Add(15*time.Minute)),
				expectedQueryStart: labelsReq.GetEnd(),
				expectedQueryEnd:   labelsReq.GetEnd().Add(15 * time.Minute),
				downstreamCalls:    1,
				downstreamResponse: composeLabelsResp([]string{"fizz"}, 1),
				expectedReponse:    composeLabelsResp([]string{"bar", "buzz", "fizz"}, 2),
			},
			{
				// To avoid returning incorrect results, we only use extents that are entirely within the requested query range.
				name:               "cached response not entirely within the requested range",
				req:                labelsReq.WithStartEnd(labelsReq.GetStart().Add(15*time.Minute), labelsReq.GetEnd().Add(-15*time.Minute)),
				expectedQueryStart: labelsReq.GetStart().Add(15 * time.Minute),
				expectedQueryEnd:   labelsReq.GetEnd().Add(-15 * time.Minute),
				downstreamCalls:    1,
				downstreamResponse: composeLabelsResp([]string{"buzz", "fizz"}, 1),
				expectedReponse:    composeLabelsResp([]string{"buzz", "fizz"}, 1),
			},
		} {
			t.Run(prefix+tc.name, func(t *testing.T) {
				cacheMiddleware := setupCacheMW()
				downstreamHandler.ResetCount()
				downstreamHandlerFunc = func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
					// should request the entire length with no partitioning as nothing is cached yet.
					require.Equal(t, labelsReq.GetStart(), r.GetStart())
					require.Equal(t, labelsReq.GetEnd(), r.GetEnd())

					got := r.(*LabelRequest)
					require.Equal(t, labelsReq.GetName(), got.GetName())
					require.Equal(t, labelsReq.GetValues(), got.GetValues())
					require.Equal(t, labelsReq.GetQuery(), got.GetQuery())

					return labelsResp, nil
				}

				handler := cacheMiddleware.Wrap(downstreamHandler)

				ctx := user.InjectOrgID(context.Background(), "fake")
				got, err := handler.Do(ctx, labelsReq)
				require.NoError(t, err)
				require.Equal(t, 1, downstreamHandler.Called()) // call downstream handler, as not cached.
				require.Equal(t, labelsResp, got)

				downstreamHandler.ResetCount()
				downstreamHandlerFunc = func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
					require.Equal(t, tc.expectedQueryStart, r.GetStart())
					require.Equal(t, tc.expectedQueryEnd, r.GetEnd())

					got := r.(*LabelRequest)
					require.Equal(t, labelsReq.GetName(), got.GetName())
					require.Equal(t, labelsReq.GetValues(), got.GetValues())
					require.Equal(t, labelsReq.GetQuery(), got.GetQuery())

					return tc.downstreamResponse, nil
				}

				got, err = handler.Do(ctx, tc.req)
				require.NoError(t, err)
				require.Equal(t, tc.downstreamCalls, downstreamHandler.Called())
				require.Equal(t, tc.expectedReponse, got)

			})
		}
	}
}

func TestLabelCache_freshness(t *testing.T) {
	testTime := time.Now().Add(-1 * time.Hour)
	from, through := util.RoundToMilliseconds(testTime.Add(-1*time.Hour), testTime)
	start, end := from.Time(), through.Time()
	nonOverlappingStart, nonOverlappingEnd := from.Add(-24*time.Hour).Time(), through.Add(-24*time.Hour).Time()

	for _, tt := range []struct {
		name                      string
		req                       *LabelRequest
		shouldCache               bool
		maxMetadataCacheFreshness time.Duration
	}{
		{
			name: "max metadata freshness not set",
			req: &LabelRequest{
				LabelRequest: logproto.LabelRequest{
					Start: &start,
					End:   &end,
				},
			},
			shouldCache: true,
		},
		{
			name: "req overlaps with max cache freshness window",
			req: &LabelRequest{
				LabelRequest: logproto.LabelRequest{
					Start: &start,
					End:   &end,
				},
			},
			maxMetadataCacheFreshness: 24 * time.Hour,
			shouldCache:               false,
		},
		{
			name: "req does not overlap max cache freshness window",
			req: &LabelRequest{
				LabelRequest: logproto.LabelRequest{
					Start: &nonOverlappingStart,
					End:   &nonOverlappingEnd,
				},
			},
			maxMetadataCacheFreshness: 24 * time.Hour,
			shouldCache:               true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cacheMiddleware, err := NewLabelsCacheMiddleware(
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

			labelsResp := &LokiLabelNamesResponse{
				Status:  "success",
				Version: uint32(loghttp.VersionV1),
				Data:    []string{"bar", "buzz"},
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

				return labelsResp, nil
			}))

			ctx := user.InjectOrgID(context.Background(), "fake")
			got, err := handler.Do(ctx, tt.req)
			require.NoError(t, err)
			require.Equal(t, 1, called) // called actual handler, as not cached.
			require.Equal(t, labelsResp, got)

			called = 0
			got, err = handler.Do(ctx, tt.req)
			require.NoError(t, err)
			if !tt.shouldCache {
				require.Equal(t, 1, called)
			} else {
				require.Equal(t, 0, called)
			}
			require.Equal(t, labelsResp, got)
		})
	}
}

func TestLabelQueryCacheKey(t *testing.T) {
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

	for _, values := range []bool{true, false} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s (values: %v)", tc.name, values), func(t *testing.T) {
				keyGen := cacheKeyLabels{tc.limits, nil}

				const labelName = "foo"
				const query = `{cluster="eu-west1"}`

				r := &LabelRequest{
					LabelRequest: logproto.LabelRequest{
						Start: &tc.start,
						End:   &tc.end,
						Query: query,
					},
				}

				if values {
					r.LabelRequest.Values = true
					r.LabelRequest.Name = labelName
				}

				// we use regex here because cache key always refers to the current time to get the ingester query window,
				// and therefore we can't know the current interval apriori without duplicating the logic
				var pattern *regexp.Regexp
				if values {
					pattern = regexp.MustCompile(fmt.Sprintf(`labelvalues:%s:%s:%s:(\d+):%d`, tenantID, labelName, regexp.QuoteMeta(query), tc.expectedSplit))
				} else {
					pattern = regexp.MustCompile(fmt.Sprintf(`labels:%s:%s:(\d+):%d`, tenantID, regexp.QuoteMeta(query), tc.expectedSplit))
				}

				require.Regexp(t, pattern, keyGen.GenerateCacheKey(context.Background(), tenantID, r))
			})
		}
	}
}
