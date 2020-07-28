package queryrange

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/go-kit/kit/log"
	"github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
)

var (
	defaultTime   = time.Date(2019, 12, 02, 11, 10, 10, 0, time.UTC)
	defaultlimits = WithDefaultLimits(fakeLimits{}, testConfig.Config)
)

func TestPartiton(t *testing.T) {
	for i, tc := range []struct {
		input                  queryrange.Request
		prevCachedResponse     []queryrange.Extent
		expectedRequests       []queryrange.Request
		expectedCachedResponse []queryrange.Response
	}{
		// 1. Test a complete hit.
		{
			input: &LokiSeriesRequest{
				StartTs: defaultTime,
				EndTs:   defaultTime.Add(2 * time.Hour),
				Match:   []string{`job="varlogs"`},
				Path:    "/api/v1/series",
			},
			prevCachedResponse: []queryrange.Extent{
				makeExtent(defaultTime.UnixNano(), defaultTime.Add(2*time.Hour).UnixNano()),
			},
			expectedCachedResponse: []queryrange.Response{
				makeAPIResponse(defaultTime.UnixNano(), defaultTime.Add(2*time.Hour).UnixNano()),
			},
		},

		// Test with a complete miss.
		{
			input: &LokiSeriesRequest{
				StartTs: defaultTime,
				EndTs:   defaultTime.Add(2 * time.Hour),
				Match:   []string{`job="varlogs"`},
				Path:    "/api/v1/series",
			},
			prevCachedResponse: []queryrange.Extent{
				makeExtent(defaultTime.Add(3*time.Hour).UnixNano(), defaultTime.Add(4*time.Hour).UnixNano()),
			},
			expectedRequests: []queryrange.Request{
				&LokiSeriesRequest{
					StartTs: defaultTime,
					EndTs:   defaultTime.Add(2 * time.Hour),
					Match:   []string{`job="varlogs"`},
					Path:    "/api/v1/series",
				},
			},
			expectedCachedResponse: nil,
		},

		// Test a partial hit.
		{
			input: &LokiSeriesRequest{
				StartTs: defaultTime,
				EndTs:   defaultTime.Add(2 * time.Hour),
				Match:   []string{`job="varlogs"`},
				Path:    "/api/v1/series",
			},
			prevCachedResponse: []queryrange.Extent{
				makeExtent(defaultTime.Add(1*time.Hour).UnixNano(), defaultTime.Add(2*time.Hour).UnixNano()),
			},
			expectedRequests: []queryrange.Request{
				&LokiSeriesRequest{
					StartTs: defaultTime,
					EndTs:   defaultTime.Add(1 * time.Hour),
					Match:   []string{`job="varlogs"`},
					Path:    "/api/v1/series",
				},
			},
			expectedCachedResponse: []queryrange.Response{
				makeAPIResponse(defaultTime.Add(1*time.Hour).UnixNano(), defaultTime.Add(2*time.Hour).UnixNano()),
			},
		},

		// Test multiple partial hits, requests start or end within extent range.
		{
			input: &LokiSeriesRequest{
				StartTs: defaultTime.Add(2 * time.Hour),
				EndTs:   defaultTime.Add(6 * time.Hour),
				Match:   []string{`job="varlogs"`},
				Path:    "/api/v1/series",
			},
			prevCachedResponse: []queryrange.Extent{
				makeExtent(defaultTime.Add(1*time.Hour).UnixNano(), defaultTime.Add(3*time.Hour).UnixNano()),
				makeExtent(defaultTime.Add(4*time.Hour).UnixNano(), defaultTime.Add(7*time.Hour).UnixNano()),
			},
			expectedRequests: []queryrange.Request{
				&LokiSeriesRequest{
					StartTs: defaultTime.Add(2 * time.Hour),
					EndTs:   defaultTime.Add(6 * time.Hour),
					Match:   []string{`job="varlogs"`},
					Path:    "/api/v1/series",
				},
			},
			expectedCachedResponse: nil,
		},
		// Test multiple partial hits, requests start or end outside extent range.
		{
			input: &LokiSeriesRequest{
				StartTs: defaultTime.Add(2 * time.Hour),
				EndTs:   defaultTime.Add(6 * time.Hour),
				Match:   []string{`job="varlogs"`},
				Path:    "/api/v1/series",
			},
			prevCachedResponse: []queryrange.Extent{
				makeExtent(defaultTime.Add(3*time.Hour).UnixNano(), defaultTime.Add(5*time.Hour).UnixNano()),
				makeExtent(defaultTime.Add(4*time.Hour).UnixNano(), defaultTime.Add(7*time.Hour).UnixNano()),
			},
			expectedRequests: []queryrange.Request{
				&LokiSeriesRequest{
					StartTs: defaultTime.Add(2 * time.Hour),
					EndTs:   defaultTime.Add(3 * time.Hour),
					Match:   []string{`job="varlogs"`},
					Path:    "/api/v1/series",
				},
				&LokiSeriesRequest{
					StartTs: defaultTime.Add(5 * time.Hour),
					EndTs:   defaultTime.Add(6 * time.Hour),
					Match:   []string{`job="varlogs"`},
					Path:    "/api/v1/series",
				},
			},
			expectedCachedResponse: []queryrange.Response{
				makeAPIResponse(defaultTime.Add(3*time.Hour).UnixNano(), defaultTime.Add(5*time.Hour).UnixNano()),
			},
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			reqs, resps, err := partition(tc.input, tc.prevCachedResponse)
			require.Nil(t, err)
			require.Equal(t, tc.expectedRequests, reqs)
			require.Equal(t, tc.expectedCachedResponse, resps)
		})
	}
}

func TestResultsCache(t *testing.T) {
	request := &LokiSeriesRequest{
		Path:    "/api/v1/series",
		StartTs: defaultTime,
		EndTs:   defaultTime.Add(2 * time.Hour),
		Match:   []string{`{jobs="varlogs"}`},
	}

	response := makeAPIResponse(defaultTime.UnixNano(), defaultTime.Add(2*time.Hour).UnixNano())

	calls := 0
	rcm, _, err := NewLokiResultsCacheMiddleware(
		log.NewNopLogger(),
		testConfig.ResultsCacheConfig,
		cacheKeyLimits{defaultlimits},
		fakeLimits{},
		lokiCodec,
	)
	require.NoError(t, err)

	rc := rcm.Wrap(queryrange.HandlerFunc(func(_ context.Context, req queryrange.Request) (queryrange.Response, error) {
		calls++
		return response, nil
	}))
	ctx := user.InjectOrgID(context.Background(), "1")
	resp, err := rc.Do(ctx, request)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, response, resp)

	// Doing same request again shouldn't change anything.
	resp, err = rc.Do(ctx, request)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, response, resp)

	// Doing request with new end time should do one more query.
	req := request.WithStartEnd(request.GetStart(), request.GetEnd()+100)
	_, err = rc.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func Test_resultsCache_MissingData(t *testing.T) {
	rm, _, err := NewLokiResultsCacheMiddleware(
		log.NewNopLogger(),
		testConfig.ResultsCacheConfig,
		cacheKeyLimits{defaultlimits},
		fakeLimits{},
		lokiCodec,
	)
	require.NoError(t, err)
	rc := rm.Wrap(nil).(*lokiResultsCache)
	ctx := context.Background()

	// fill up the cache
	rc.put(ctx, "empty", []queryrange.Extent{{
		Start:    100,
		End:      200,
		Response: nil,
	}})
	rc.put(ctx, "notempty", []queryrange.Extent{makeExtent(100, 120)})
	rc.put(ctx, "mixed", []queryrange.Extent{makeExtent(100, 120), {
		Start:    120,
		End:      200,
		Response: nil,
	}})

	extents, hit := rc.get(ctx, "empty")
	require.Empty(t, extents)
	require.False(t, hit)

	extents, hit = rc.get(ctx, "notempty")
	require.Equal(t, len(extents), 1)
	require.True(t, hit)

	extents, hit = rc.get(ctx, "mixed")
	require.Equal(t, len(extents), 0)
	require.False(t, hit)
}

func makeAPIResponse(start, end int64) *LokiSeriesResponse {
	if start == defaultTime.UnixNano() && end == defaultTime.Add(2*time.Hour).UnixNano() {
		return &LokiSeriesResponse{
			Status: "success",
			Data: []logproto.SeriesIdentifier{
				{
					Labels: map[string]string{"filename": "/var/hostlog/apport.log", "job": "varlogs"},
				},
				{
					Labels: map[string]string{"filename": "/var/hostlog/test.log", "job": "varlogs"},
				},
			},
		}
	}
	return &LokiSeriesResponse{
		Status: "success",
		Data: []logproto.SeriesIdentifier{
			{
				Labels: map[string]string{"filename": "/var/hostlog/other.log", "job": "varlogs"},
			},
		},
	}
}

func makeExtent(start, end int64) queryrange.Extent {
	res := makeAPIResponse(start, end)
	any, err := types.MarshalAny(res)
	if err != nil {
		panic(err)
	}
	return queryrange.Extent{
		Start:    start / 1e6,
		End:      end / 1e6,
		Response: any,
	}
}
