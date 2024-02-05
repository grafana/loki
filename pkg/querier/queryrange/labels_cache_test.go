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
		require.Equal(t, fmt.Sprintf(`labels:fake:%d:%d`, expectedInterval, time.Hour.Nanoseconds()), k.GenerateCacheKey(context.Background(), "fake", &req))
	})

	t.Run("label values", func(t *testing.T) {
		req := req
		req.Name = "foo"
		req.Values = true
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

	cacheMiddleware := setupCacheMW()
	for _, values := range []bool{false, true} {
		prefix := "labels"
		if values {
			prefix = "label values"
		}
		t.Run(prefix+": cache the response for the same request", func(t *testing.T) {
			start := testTime.Truncate(time.Millisecond)
			end := start.Add(time.Hour)

			labelsReq := LabelRequest{
				LabelRequest: logproto.LabelRequest{
					Start: &start,
					End:   &end,
				},
			}

			if values {
				labelsReq.Values = true
				labelsReq.Name = "foo"
				labelsReq.Query = `{cluster="eu-west1"}`
			}

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
				require.Equal(t, labelsReq.GetStart(), r.GetStart())
				require.Equal(t, labelsReq.GetEnd(), r.GetEnd())

				got := r.(*LabelRequest)
				require.Equal(t, labelsReq.GetName(), got.GetName())
				require.Equal(t, labelsReq.GetValues(), got.GetValues())
				require.Equal(t, labelsReq.GetQuery(), got.GetQuery())

				return labelsResp, nil
			}))

			ctx := user.InjectOrgID(context.Background(), "fake")
			got, err := handler.Do(ctx, &labelsReq)
			require.NoError(t, err)
			require.Equal(t, 1, called) // called actual handler, as not cached.
			require.Equal(t, labelsResp, got)

			// Doing same request again shouldn't change anything.
			called = 0
			got, err = handler.Do(ctx, &labelsReq)
			require.NoError(t, err)
			require.Equal(t, 0, called)
			require.Equal(t, labelsResp, got)
		})
	}

	// reset cacheMiddleware
	cacheMiddleware = setupCacheMW()
	for _, values := range []bool{false, true} {
		prefix := "labels"
		if values {
			prefix = "label values"
		}
		t.Run(prefix+": a new request with overlapping time range should reuse part of the previous request for the overlap", func(t *testing.T) {
			cacheMiddleware := setupCacheMW()

			start := testTime.Truncate(time.Millisecond)
			end := start.Add(time.Hour)

			labelsReq1 := LabelRequest{
				LabelRequest: logproto.LabelRequest{
					Start: &start,
					End:   &end,
				},
			}

			if values {
				labelsReq1.Values = true
				labelsReq1.Name = "foo"
				labelsReq1.Query = `{cluster="eu-west1"}`
			}

			labelsResp1 := &LokiLabelNamesResponse{
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
				require.Equal(t, labelsReq1.GetStart(), r.GetStart())
				require.Equal(t, labelsReq1.GetEnd(), r.GetEnd())

				got := r.(*LabelRequest)
				require.Equal(t, labelsReq1.GetName(), got.GetName())
				require.Equal(t, labelsReq1.GetValues(), got.GetValues())
				require.Equal(t, labelsReq1.GetQuery(), got.GetQuery())

				return labelsResp1, nil
			}))

			ctx := user.InjectOrgID(context.Background(), "fake")
			got, err := handler.Do(ctx, &labelsReq1)
			require.NoError(t, err)
			require.Equal(t, 1, called)
			require.Equal(t, labelsResp1, got)

			labelsReq2 := labelsReq1.WithStartEnd(labelsReq1.GetStart().Add(15*time.Minute), labelsReq1.GetEnd().Add(15*time.Minute))

			called = 0
			handler = cacheMiddleware.Wrap(queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
				called++

				// make downstream request only for the non-overlapping portion of the query.
				require.Equal(t, labelsReq1.GetEnd(), r.GetStart())
				require.Equal(t, labelsReq2.GetEnd(), r.GetEnd())

				got := r.(*LabelRequest)
				require.Equal(t, labelsReq1.GetName(), got.GetName())
				require.Equal(t, labelsReq1.GetValues(), got.GetValues())
				require.Equal(t, labelsReq1.GetQuery(), got.GetQuery())

				return &LokiLabelNamesResponse{
					Status:  "success",
					Version: uint32(loghttp.VersionV1),
					Data:    []string{"fizz"},
					Statistics: stats.Result{
						Summary: stats.Summary{
							Splits: 1,
						},
					},
				}, nil
			}))

			got, err = handler.Do(ctx, labelsReq2)
			require.NoError(t, err)
			require.Equal(t, 1, called)
			// two splits as we merge the results from the extent and downstream request
			labelsResp1.Statistics.Summary.Splits = 2
			require.Equal(t, &LokiLabelNamesResponse{
				Status:  "success",
				Version: uint32(loghttp.VersionV1),
				Data:    []string{"bar", "buzz", "fizz"},
				Statistics: stats.Result{
					Summary: stats.Summary{
						Splits: 2,
					},
				},
			}, got)
		})
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

	for _, values := range []bool{true, false} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s (values: %v)", tc.name, values), func(t *testing.T) {
				keyGen := cacheKeyLabels{l, nil, tc.iqo}

				r := &LabelRequest{
					LabelRequest: logproto.LabelRequest{
						Start: &tc.start,
						End:   &tc.end,
					},
				}

				const labelName = "foo"
				const query = `{cluster="eu-west1"}`

				if values {
					r.LabelRequest.Values = true
					r.LabelRequest.Name = labelName
					r.LabelRequest.Query = query
				}

				// we use regex here because cache key always refers to the current time to get the ingester query window,
				// and therefore we can't know the current interval apriori without duplicating the logic
				var pattern *regexp.Regexp
				if values {
					pattern = regexp.MustCompile(fmt.Sprintf(`labelvalues:%s:%s:%s:(\d+):%d`, tc.tenantID, labelName, regexp.QuoteMeta(query), tc.expectedSplit))
				} else {
					pattern = regexp.MustCompile(fmt.Sprintf(`labels:%s:(\d+):%d`, tc.tenantID, tc.expectedSplit))
				}

				require.Regexp(t, pattern, keyGen.GenerateCacheKey(context.Background(), tc.tenantID, r))
			})
		}
	}
}
