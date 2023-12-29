package queryrange

import (
	"context"
	"fmt"
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
			splitDuration: map[string]time.Duration{
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
