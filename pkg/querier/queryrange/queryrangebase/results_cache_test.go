package queryrangebase

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gogo/protobuf/types"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

const (
	query        = "/api/v1/query_range?end=1536716898&query=sum%28container_memory_rss%29+by+%28namespace%29&start=1536673680&step=120"
	responseBody = `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"foo":"bar"},"values":[[1536673680,"137"],[1536673780,"137"]]}]}}`
)

var (
	parsedRequest = &PrometheusRequest{
		Path:  "/api/v1/query_range",
		Start: 1536673680 * 1e3,
		End:   1536716898 * 1e3,
		Step:  120 * 1e3,
		Query: "sum(container_memory_rss) by (namespace)",
	}
	reqHeaders = []*PrometheusRequestHeader{
		{
			Name:   "Test-Header",
			Values: []string{"test"},
		},
	}
	noCacheRequest = &PrometheusRequest{
		Path:           "/api/v1/query_range",
		Start:          1536673680 * 1e3,
		End:            1536716898 * 1e3,
		Step:           120 * 1e3,
		Query:          "sum(container_memory_rss) by (namespace)",
		CachingOptions: CachingOptions{Disabled: true},
	}
	respHeaders = []*PrometheusResponseHeader{
		{
			Name:   "Content-Type",
			Values: []string{"application/json"},
		},
	}
	parsedResponse = &PrometheusResponse{
		Status: "success",
		Data: PrometheusData{
			ResultType: model.ValMatrix.String(),
			Result: []SampleStream{
				{
					Labels: []logproto.LabelAdapter{
						{Name: "foo", Value: "bar"},
					},
					Samples: []logproto.LegacySample{
						{Value: 137, TimestampMs: 1536673680000},
						{Value: 137, TimestampMs: 1536673780000},
					},
				},
			},
		},
	}
)

func mkAPIResponse(start, end, step int64) *PrometheusResponse {
	var samples []logproto.LegacySample
	for i := start; i <= end; i += step {
		samples = append(samples, logproto.LegacySample{
			TimestampMs: i,
			Value:       float64(i),
		})
	}

	return &PrometheusResponse{
		Status: StatusSuccess,
		Data: PrometheusData{
			ResultType: matrix,
			Result: []SampleStream{
				{
					Labels: []logproto.LabelAdapter{
						{Name: "foo", Value: "bar"},
					},
					Samples: samples,
				},
			},
		},
	}
}

func mkExtent(start, end int64) Extent {
	return mkExtentWithStep(start, end, 10)
}

func mkExtentWithStep(start, end, step int64) Extent {
	res := mkAPIResponse(start, end, step)
	any, err := types.MarshalAny(res)
	if err != nil {
		panic(err)
	}
	return Extent{
		Start:    start,
		End:      end,
		Response: any,
	}
}

func TestShouldCache(t *testing.T) {
	maxCacheTime := int64(150 * 1000)
	c := &resultsCache{logger: log.NewNopLogger(), cacheGenNumberLoader: newMockCacheGenNumberLoader(),
		metrics: NewResultsCacheMetrics(nil)}
	for _, tc := range []struct {
		name                   string
		request                Request
		input                  Response
		cacheGenNumberToInject string
		expected               bool
	}{
		// Tests only for cacheControlHeader
		{
			name:    "does not contain the cacheControl header",
			request: &PrometheusRequest{Query: "metric"},
			input: Response(&PrometheusResponse{
				Headers: []*PrometheusResponseHeader{
					{
						Name:   "meaninglessheader",
						Values: []string{},
					},
				},
			}),
			expected: true,
		},
		{
			name:    "does contain the cacheControl header which has the value",
			request: &PrometheusRequest{Query: "metric"},
			input: Response(&PrometheusResponse{
				Headers: []*PrometheusResponseHeader{
					{
						Name:   cacheControlHeader,
						Values: []string{noStoreValue},
					},
				},
			}),
			expected: false,
		},
		{
			name:    "cacheControl header contains extra values but still good",
			request: &PrometheusRequest{Query: "metric"},
			input: Response(&PrometheusResponse{
				Headers: []*PrometheusResponseHeader{
					{
						Name:   cacheControlHeader,
						Values: []string{"foo", noStoreValue},
					},
				},
			}),
			expected: false,
		},
		{
			name:     "broken response",
			request:  &PrometheusRequest{Query: "metric"},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:    "nil headers",
			request: &PrometheusRequest{Query: "metric"},
			input: Response(&PrometheusResponse{
				Headers: []*PrometheusResponseHeader{nil},
			}),
			expected: true,
		},
		{
			name:    "had cacheControl header but no values",
			request: &PrometheusRequest{Query: "metric"},
			input: Response(&PrometheusResponse{
				Headers: []*PrometheusResponseHeader{{Name: cacheControlHeader}},
			}),
			expected: true,
		},

		// Tests only for cacheGenNumber header
		{
			name:    "cacheGenNumber not set in both header and store",
			request: &PrometheusRequest{Query: "metric"},
			input: Response(&PrometheusResponse{
				Headers: []*PrometheusResponseHeader{
					{
						Name:   "meaninglessheader",
						Values: []string{},
					},
				},
			}),
			expected: true,
		},
		{
			name:    "cacheGenNumber set in store but not in header",
			request: &PrometheusRequest{Query: "metric"},
			input: Response(&PrometheusResponse{
				Headers: []*PrometheusResponseHeader{
					{
						Name:   "meaninglessheader",
						Values: []string{},
					},
				},
			}),
			cacheGenNumberToInject: "1",
			expected:               false,
		},
		{
			name:    "cacheGenNumber set in header but not in store",
			request: &PrometheusRequest{Query: "metric"},
			input: Response(&PrometheusResponse{
				Headers: []*PrometheusResponseHeader{
					{
						Name:   ResultsCacheGenNumberHeaderName,
						Values: []string{"1"},
					},
				},
			}),
			expected: false,
		},
		{
			name:    "cacheGenNumber in header and store are the same",
			request: &PrometheusRequest{Query: "metric"},
			input: Response(&PrometheusResponse{
				Headers: []*PrometheusResponseHeader{
					{
						Name:   ResultsCacheGenNumberHeaderName,
						Values: []string{"1", "1"},
					},
				},
			}),
			cacheGenNumberToInject: "1",
			expected:               true,
		},
		{
			name:    "inconsistency between cacheGenNumber in header and store",
			request: &PrometheusRequest{Query: "metric"},
			input: Response(&PrometheusResponse{
				Headers: []*PrometheusResponseHeader{
					{
						Name:   ResultsCacheGenNumberHeaderName,
						Values: []string{"1", "2"},
					},
				},
			}),
			cacheGenNumberToInject: "1",
			expected:               false,
		},
		{
			name:    "cacheControl header says not to catch and cacheGenNumbers in store and headers have consistency",
			request: &PrometheusRequest{Query: "metric"},
			input: Response(&PrometheusResponse{
				Headers: []*PrometheusResponseHeader{
					{
						Name:   cacheControlHeader,
						Values: []string{noStoreValue},
					},
					{
						Name:   ResultsCacheGenNumberHeaderName,
						Values: []string{"1", "1"},
					},
				},
			}),
			cacheGenNumberToInject: "1",
			expected:               false,
		},
		// @ modifier on vector selectors.
		{
			name:     "@ modifier on vector selector, before end, before maxCacheTime",
			request:  &PrometheusRequest{Query: "metric @ 123", End: 125000},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:     "@ modifier on vector selector, after end, before maxCacheTime",
			request:  &PrometheusRequest{Query: "metric @ 127", End: 125000},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on vector selector, before end, after maxCacheTime",
			request:  &PrometheusRequest{Query: "metric @ 151", End: 200000},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on vector selector, after end, after maxCacheTime",
			request:  &PrometheusRequest{Query: "metric @ 151", End: 125000},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on vector selector with start() before maxCacheTime",
			request:  &PrometheusRequest{Query: "metric @ start()", Start: 100000, End: 200000},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:     "@ modifier on vector selector with end() after maxCacheTime",
			request:  &PrometheusRequest{Query: "metric @ end()", Start: 100000, End: 200000},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		// @ modifier on matrix selectors.
		{
			name:     "@ modifier on matrix selector, before end, before maxCacheTime",
			request:  &PrometheusRequest{Query: "rate(metric[5m] @ 123)", End: 125000},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:     "@ modifier on matrix selector, after end, before maxCacheTime",
			request:  &PrometheusRequest{Query: "rate(metric[5m] @ 127)", End: 125000},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on matrix selector, before end, after maxCacheTime",
			request:  &PrometheusRequest{Query: "rate(metric[5m] @ 151)", End: 200000},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on matrix selector, after end, after maxCacheTime",
			request:  &PrometheusRequest{Query: "rate(metric[5m] @ 151)", End: 125000},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on matrix selector with start() before maxCacheTime",
			request:  &PrometheusRequest{Query: "rate(metric[5m] @ start())", Start: 100000, End: 200000},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:     "@ modifier on matrix selector with end() after maxCacheTime",
			request:  &PrometheusRequest{Query: "rate(metric[5m] @ end())", Start: 100000, End: 200000},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		// @ modifier on subqueries.
		{
			name:     "@ modifier on subqueries, before end, before maxCacheTime",
			request:  &PrometheusRequest{Query: "sum_over_time(rate(metric[1m])[10m:1m] @ 123)", End: 125000},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:     "@ modifier on subqueries, after end, before maxCacheTime",
			request:  &PrometheusRequest{Query: "sum_over_time(rate(metric[1m])[10m:1m] @ 127)", End: 125000},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on subqueries, before end, after maxCacheTime",
			request:  &PrometheusRequest{Query: "sum_over_time(rate(metric[1m])[10m:1m] @ 151)", End: 200000},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on subqueries, after end, after maxCacheTime",
			request:  &PrometheusRequest{Query: "sum_over_time(rate(metric[1m])[10m:1m] @ 151)", End: 125000},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on subqueries with start() before maxCacheTime",
			request:  &PrometheusRequest{Query: "sum_over_time(rate(metric[1m])[10m:1m] @ start())", Start: 100000, End: 200000},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:     "@ modifier on subqueries with end() after maxCacheTime",
			request:  &PrometheusRequest{Query: "sum_over_time(rate(metric[1m])[10m:1m] @ end())", Start: 100000, End: 200000},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
	} {
		{
			t.Run(tc.name, func(t *testing.T) {
				ctx := cache.InjectCacheGenNumber(context.Background(), tc.cacheGenNumberToInject)
				ret := c.shouldCacheResponse(ctx, tc.request, tc.input, maxCacheTime)
				require.Equal(t, tc.expected, ret)
			})
		}
	}
}

func TestPartition(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		input                  Request
		prevCachedResponse     []Extent
		expectedRequests       []Request
		expectedCachedResponse []Response
	}{
		{
			name: "Test a complete hit.",
			input: &PrometheusRequest{
				Start: 0,
				End:   100,
			},
			prevCachedResponse: []Extent{
				mkExtent(0, 100),
			},
			expectedCachedResponse: []Response{
				mkAPIResponse(0, 100, 10),
			},
		},

		{
			name: "Test with a complete miss.",
			input: &PrometheusRequest{
				Start: 0,
				End:   100,
			},
			prevCachedResponse: []Extent{
				mkExtent(110, 210),
			},
			expectedRequests: []Request{
				&PrometheusRequest{
					Start: 0,
					End:   100,
				},
			},
		},
		{
			name: "Test a partial hit.",
			input: &PrometheusRequest{
				Start: 0,
				End:   100,
			},
			prevCachedResponse: []Extent{
				mkExtent(50, 100),
			},
			expectedRequests: []Request{
				&PrometheusRequest{
					Start: 0,
					End:   50,
				},
			},
			expectedCachedResponse: []Response{
				mkAPIResponse(50, 100, 10),
			},
		},
		{
			name: "Test multiple partial hits.",
			input: &PrometheusRequest{
				Start: 100,
				End:   200,
			},
			prevCachedResponse: []Extent{
				mkExtent(50, 120),
				mkExtent(160, 250),
			},
			expectedRequests: []Request{
				&PrometheusRequest{
					Start: 120,
					End:   160,
				},
			},
			expectedCachedResponse: []Response{
				mkAPIResponse(100, 120, 10),
				mkAPIResponse(160, 200, 10),
			},
		},
		{
			name: "Partial hits with tiny gap.",
			input: &PrometheusRequest{
				Start: 100,
				End:   160,
			},
			prevCachedResponse: []Extent{
				mkExtent(50, 120),
				mkExtent(122, 130),
			},
			expectedRequests: []Request{
				&PrometheusRequest{
					Start: 120,
					End:   160,
				},
			},
			expectedCachedResponse: []Response{
				mkAPIResponse(100, 120, 10),
			},
		},
		{
			name: "Extent is outside the range and the request has a single step (same start and end).",
			input: &PrometheusRequest{
				Start: 100,
				End:   100,
			},
			prevCachedResponse: []Extent{
				mkExtent(50, 90),
			},
			expectedRequests: []Request{
				&PrometheusRequest{
					Start: 100,
					End:   100,
				},
			},
		},
		{
			name: "Test when hit has a large step and only a single sample extent.",
			// If there is a only a single sample in the split interval, start and end will be the same.
			input: &PrometheusRequest{
				Start: 100,
				End:   100,
			},
			prevCachedResponse: []Extent{
				mkExtent(100, 100),
			},
			expectedCachedResponse: []Response{
				mkAPIResponse(100, 105, 10),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := resultsCache{
				extractor:      PrometheusResponseExtractor{},
				minCacheExtent: 10,
			}
			reqs, resps, err := s.partition(tc.input, tc.prevCachedResponse)
			require.Nil(t, err)
			require.Equal(t, tc.expectedRequests, reqs)
			require.Equal(t, tc.expectedCachedResponse, resps)
		})
	}
}

func TestHandleHit(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		input                      Request
		cachedEntry                []Extent
		expectedUpdatedCachedEntry []Extent
	}{
		{
			name: "Should drop tiny extent that overlaps with non-tiny request only",
			input: &PrometheusRequest{
				Start: 100,
				End:   120,
				Step:  5,
			},
			cachedEntry: []Extent{
				mkExtentWithStep(0, 50, 5),
				mkExtentWithStep(60, 65, 5),
				mkExtentWithStep(100, 105, 5),
				mkExtentWithStep(110, 150, 5),
				mkExtentWithStep(160, 165, 5),
			},
			expectedUpdatedCachedEntry: []Extent{
				mkExtentWithStep(0, 50, 5),
				mkExtentWithStep(60, 65, 5),
				mkExtentWithStep(100, 150, 5),
				mkExtentWithStep(160, 165, 5),
			},
		},
		{
			name: "Should replace tiny extents that are cover by bigger request",
			input: &PrometheusRequest{
				Start: 100,
				End:   200,
				Step:  5,
			},
			cachedEntry: []Extent{
				mkExtentWithStep(0, 50, 5),
				mkExtentWithStep(60, 65, 5),
				mkExtentWithStep(100, 105, 5),
				mkExtentWithStep(110, 115, 5),
				mkExtentWithStep(120, 125, 5),
				mkExtentWithStep(220, 225, 5),
				mkExtentWithStep(240, 250, 5),
			},
			expectedUpdatedCachedEntry: []Extent{
				mkExtentWithStep(0, 50, 5),
				mkExtentWithStep(60, 65, 5),
				mkExtentWithStep(100, 200, 5),
				mkExtentWithStep(220, 225, 5),
				mkExtentWithStep(240, 250, 5),
			},
		},
		{
			name: "Should not drop tiny extent that completely overlaps with tiny request",
			input: &PrometheusRequest{
				Start: 100,
				End:   105,
				Step:  5,
			},
			cachedEntry: []Extent{
				mkExtentWithStep(0, 50, 5),
				mkExtentWithStep(60, 65, 5),
				mkExtentWithStep(100, 105, 5),
				mkExtentWithStep(160, 165, 5),
			},
			expectedUpdatedCachedEntry: nil, // no cache update need, request fulfilled using cache
		},
		{
			name: "Should not drop tiny extent that partially center-overlaps with tiny request",
			input: &PrometheusRequest{
				Start: 106,
				End:   108,
				Step:  2,
			},
			cachedEntry: []Extent{
				mkExtentWithStep(60, 64, 2),
				mkExtentWithStep(104, 110, 2),
				mkExtentWithStep(160, 166, 2),
			},
			expectedUpdatedCachedEntry: nil, // no cache update need, request fulfilled using cache
		},
		{
			name: "Should not drop tiny extent that partially left-overlaps with tiny request",
			input: &PrometheusRequest{
				Start: 100,
				End:   106,
				Step:  2,
			},
			cachedEntry: []Extent{
				mkExtentWithStep(60, 64, 2),
				mkExtentWithStep(104, 110, 2),
				mkExtentWithStep(160, 166, 2),
			},
			expectedUpdatedCachedEntry: []Extent{
				mkExtentWithStep(60, 64, 2),
				mkExtentWithStep(100, 110, 2),
				mkExtentWithStep(160, 166, 2),
			},
		},
		{
			name: "Should not drop tiny extent that partially right-overlaps with tiny request",
			input: &PrometheusRequest{
				Start: 100,
				End:   106,
				Step:  2,
			},
			cachedEntry: []Extent{
				mkExtentWithStep(60, 64, 2),
				mkExtentWithStep(98, 102, 2),
				mkExtentWithStep(160, 166, 2),
			},
			expectedUpdatedCachedEntry: []Extent{
				mkExtentWithStep(60, 64, 2),
				mkExtentWithStep(98, 106, 2),
				mkExtentWithStep(160, 166, 2),
			},
		},
		{
			name: "Should merge fragmented extents if request fills the hole",
			input: &PrometheusRequest{
				Start: 40,
				End:   80,
				Step:  20,
			},
			cachedEntry: []Extent{
				mkExtentWithStep(0, 20, 20),
				mkExtentWithStep(80, 100, 20),
			},
			expectedUpdatedCachedEntry: []Extent{
				mkExtentWithStep(0, 100, 20),
			},
		},
		{
			name: "Should left-extend extent if request starts earlier than extent in cache",
			input: &PrometheusRequest{
				Start: 40,
				End:   80,
				Step:  20,
			},
			cachedEntry: []Extent{
				mkExtentWithStep(60, 160, 20),
			},
			expectedUpdatedCachedEntry: []Extent{
				mkExtentWithStep(40, 160, 20),
			},
		},
		{
			name: "Should right-extend extent if request ends later than extent in cache",
			input: &PrometheusRequest{
				Start: 100,
				End:   180,
				Step:  20,
			},
			cachedEntry: []Extent{
				mkExtentWithStep(60, 160, 20),
			},
			expectedUpdatedCachedEntry: []Extent{
				mkExtentWithStep(60, 180, 20),
			},
		},
		{
			name: "Should not throw error if complete-overlapped smaller Extent is erroneous",
			input: &PrometheusRequest{
				// This request is carefully crated such that cachedEntry is not used to fulfill
				// the request.
				Start: 160,
				End:   180,
				Step:  20,
			},
			cachedEntry: []Extent{
				{
					Start: 60,
					End:   80,

					// if the optimization of "sorting by End when Start of 2 Extents are equal" is not there, this nil
					// response would cause error during Extents merge phase. With the optimization
					// this bad Extent should be dropped. The good Extent below can be used instead.
					Response: nil,
				},
				mkExtentWithStep(60, 160, 20),
			},
			expectedUpdatedCachedEntry: []Extent{
				mkExtentWithStep(60, 180, 20),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sut := resultsCache{
				extractor:         PrometheusResponseExtractor{},
				minCacheExtent:    10,
				limits:            mockLimits{},
				merger:            PrometheusCodec,
				parallelismForReq: func(_ context.Context, tenantIDs []string, r Request) int { return 1 },
				next: HandlerFunc(func(_ context.Context, req Request) (Response, error) {
					return mkAPIResponse(req.GetStart(), req.GetEnd(), req.GetStep()), nil
				}),
			}

			ctx := user.InjectOrgID(context.Background(), "1")
			response, updatedExtents, err := sut.handleHit(ctx, tc.input, tc.cachedEntry, 0)
			require.NoError(t, err)

			expectedResponse := mkAPIResponse(tc.input.GetStart(), tc.input.GetEnd(), tc.input.GetStep())
			require.Equal(t, expectedResponse, response, "response does not match the expectation")
			require.Equal(t, tc.expectedUpdatedCachedEntry, updatedExtents, "updated cache entry does not match the expectation")
		})
	}
}

func TestResultsCache(t *testing.T) {
	calls := 0
	cfg := ResultsCacheConfig{
		CacheConfig: cache.Config{
			Cache: cache.NewMockCache(),
		},
	}
	c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache)
	require.NoError(t, err)
	rcm, err := NewResultsCacheMiddleware(
		log.NewNopLogger(),
		c,
		constSplitter(day),
		mockLimits{},
		PrometheusCodec,
		PrometheusResponseExtractor{},
		nil,
		nil,
		func(_ context.Context, tenantIDs []string, r Request) int {
			return mockLimits{}.MaxQueryParallelism(context.Background(), "fake")
		},
		false,
		nil,
	)
	require.NoError(t, err)

	rc := rcm.Wrap(HandlerFunc(func(_ context.Context, req Request) (Response, error) {
		calls++
		return parsedResponse, nil
	}))
	ctx := user.InjectOrgID(context.Background(), "1")
	resp, err := rc.Do(ctx, parsedRequest)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, parsedResponse, resp)

	// Doing same request again shouldn't change anything.
	resp, err = rc.Do(ctx, parsedRequest)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, parsedResponse, resp)

	// Doing request with new end time should do one more query.
	req := parsedRequest.WithStartEnd(parsedRequest.GetStart(), parsedRequest.GetEnd()+100)
	_, err = rc.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestResultsCacheRecent(t *testing.T) {
	var cfg ResultsCacheConfig
	flagext.DefaultValues(&cfg)
	cfg.CacheConfig.Cache = cache.NewMockCache()
	c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache)
	require.NoError(t, err)
	rcm, err := NewResultsCacheMiddleware(
		log.NewNopLogger(),
		c,
		constSplitter(day),
		mockLimits{maxCacheFreshness: 10 * time.Minute},
		PrometheusCodec,
		PrometheusResponseExtractor{},
		nil,
		nil,
		func(_ context.Context, tenantIDs []string, r Request) int {
			return mockLimits{}.MaxQueryParallelism(context.Background(), "fake")
		},
		false,
		nil,
	)
	require.NoError(t, err)

	req := parsedRequest.WithStartEnd(int64(model.Now())-(60*1e3), int64(model.Now()))

	calls := 0
	rc := rcm.Wrap(HandlerFunc(func(_ context.Context, r Request) (Response, error) {
		calls++
		assert.Equal(t, r, req)
		return parsedResponse, nil
	}))
	ctx := user.InjectOrgID(context.Background(), "1")

	// Request should result in a query.
	resp, err := rc.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, parsedResponse, resp)

	// Doing same request again should result in another query.
	resp, err = rc.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, parsedResponse, resp)
}

func TestResultsCacheMaxFreshness(t *testing.T) {
	modelNow := model.Now()
	for i, tc := range []struct {
		fakeLimits       Limits
		Handler          HandlerFunc
		expectedResponse *PrometheusResponse
	}{
		{
			fakeLimits:       mockLimits{maxCacheFreshness: 5 * time.Second},
			Handler:          nil,
			expectedResponse: mkAPIResponse(int64(modelNow)-(50*1e3), int64(modelNow)-(10*1e3), 10),
		},
		{
			// should not lookup cache because per-tenant override will be applied
			fakeLimits: mockLimits{maxCacheFreshness: 10 * time.Minute},
			Handler: HandlerFunc(func(_ context.Context, _ Request) (Response, error) {
				return parsedResponse, nil
			}),
			expectedResponse: parsedResponse,
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			var cfg ResultsCacheConfig
			flagext.DefaultValues(&cfg)
			cfg.CacheConfig.Cache = cache.NewMockCache()
			c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache)
			require.NoError(t, err)
			fakeLimits := tc.fakeLimits
			rcm, err := NewResultsCacheMiddleware(
				log.NewNopLogger(),
				c,
				constSplitter(day),
				fakeLimits,
				PrometheusCodec,
				PrometheusResponseExtractor{},
				nil,
				nil,
				func(_ context.Context, tenantIDs []string, r Request) int {
					return tc.fakeLimits.MaxQueryParallelism(context.Background(), "fake")
				},
				false,
				nil,
			)
			require.NoError(t, err)

			// create cache with handler
			rc := rcm.Wrap(tc.Handler)
			ctx := user.InjectOrgID(context.Background(), "1")

			// create request with start end within the key extents
			req := parsedRequest.WithStartEnd(int64(modelNow)-(50*1e3), int64(modelNow)-(10*1e3))

			// fill cache
			key := constSplitter(day).GenerateCacheKey(context.Background(), "1", req)
			rc.(*resultsCache).put(ctx, key, []Extent{mkExtent(int64(modelNow)-(600*1e3), int64(modelNow))})

			resp, err := rc.Do(ctx, req)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResponse, resp)
		})
	}
}

func Test_resultsCache_MissingData(t *testing.T) {
	cfg := ResultsCacheConfig{
		CacheConfig: cache.Config{
			Cache: cache.NewMockCache(),
		},
	}
	c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache)
	require.NoError(t, err)
	rm, err := NewResultsCacheMiddleware(
		log.NewNopLogger(),
		c,
		constSplitter(day),
		mockLimits{},
		PrometheusCodec,
		PrometheusResponseExtractor{},
		nil,
		nil,
		func(_ context.Context, tenantIDs []string, r Request) int {
			return mockLimits{}.MaxQueryParallelism(context.Background(), "fake")
		},
		false,
		nil,
	)
	require.NoError(t, err)
	rc := rm.Wrap(nil).(*resultsCache)
	ctx := context.Background()

	// fill up the cache
	rc.put(ctx, "empty", []Extent{{
		Start:    100,
		End:      200,
		Response: nil,
	}})
	rc.put(ctx, "notempty", []Extent{mkExtent(100, 120)})
	rc.put(ctx, "mixed", []Extent{mkExtent(100, 120), {
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

func toMs(t time.Duration) int64 {
	return t.Nanoseconds() / (int64(time.Millisecond) / int64(time.Nanosecond))
}

func TestConstSplitter_generateCacheKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		r        Request
		interval time.Duration
		want     string
	}{
		{"0", &PrometheusRequest{Start: 0, Step: 10, Query: "foo{}"}, 30 * time.Minute, "fake:foo{}:10:0"},
		{"<30m", &PrometheusRequest{Start: toMs(10 * time.Minute), Step: 10, Query: "foo{}"}, 30 * time.Minute, "fake:foo{}:10:0"},
		{"30m", &PrometheusRequest{Start: toMs(30 * time.Minute), Step: 10, Query: "foo{}"}, 30 * time.Minute, "fake:foo{}:10:1"},
		{"91m", &PrometheusRequest{Start: toMs(91 * time.Minute), Step: 10, Query: "foo{}"}, 30 * time.Minute, "fake:foo{}:10:3"},
		{"0", &PrometheusRequest{Start: 0, Step: 10, Query: "foo{}"}, 24 * time.Hour, "fake:foo{}:10:0"},
		{"<1d", &PrometheusRequest{Start: toMs(22 * time.Hour), Step: 10, Query: "foo{}"}, 24 * time.Hour, "fake:foo{}:10:0"},
		{"4d", &PrometheusRequest{Start: toMs(4 * 24 * time.Hour), Step: 10, Query: "foo{}"}, 24 * time.Hour, "fake:foo{}:10:4"},
		{"3d5h", &PrometheusRequest{Start: toMs(77 * time.Hour), Step: 10, Query: "foo{}"}, 24 * time.Hour, "fake:foo{}:10:3"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s - %s", tt.name, tt.interval), func(t *testing.T) {
			if got := constSplitter(tt.interval).GenerateCacheKey(context.Background(), "fake", tt.r); got != tt.want {
				t.Errorf("generateKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResultsCacheShouldCacheFunc(t *testing.T) {
	testcases := []struct {
		name         string
		shouldCache  ShouldCacheFn
		requests     []Request
		expectedCall int
	}{
		{
			name:         "normal",
			shouldCache:  nil,
			requests:     []Request{parsedRequest, parsedRequest},
			expectedCall: 1,
		},
		{
			name: "always no cache",
			shouldCache: func(r Request) bool {
				return false
			},
			requests:     []Request{parsedRequest, parsedRequest},
			expectedCall: 2,
		},
		{
			name: "check cache based on request",
			shouldCache: func(r Request) bool {
				return !r.GetCachingOptions().Disabled
			},
			requests:     []Request{noCacheRequest, noCacheRequest},
			expectedCall: 2,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var cfg ResultsCacheConfig
			flagext.DefaultValues(&cfg)
			cfg.CacheConfig.Cache = cache.NewMockCache()
			c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache)
			require.NoError(t, err)
			rcm, err := NewResultsCacheMiddleware(
				log.NewNopLogger(),
				c,
				constSplitter(day),
				mockLimits{maxCacheFreshness: 10 * time.Minute},
				PrometheusCodec,
				PrometheusResponseExtractor{},
				nil,
				tc.shouldCache,
				func(_ context.Context, tenantIDs []string, r Request) int {
					return mockLimits{}.MaxQueryParallelism(context.Background(), "fake")
				},
				false,
				nil,
			)
			require.NoError(t, err)
			rc := rcm.Wrap(HandlerFunc(func(_ context.Context, req Request) (Response, error) {
				calls++
				return parsedResponse, nil
			}))

			for _, req := range tc.requests {
				ctx := user.InjectOrgID(context.Background(), "1")
				_, err := rc.Do(ctx, req)
				require.NoError(t, err)
			}

			require.Equal(t, tc.expectedCall, calls)
		})
	}
}

type mockCacheGenNumberLoader struct{}

func newMockCacheGenNumberLoader() CacheGenNumberLoader {
	return mockCacheGenNumberLoader{}
}

func (mockCacheGenNumberLoader) GetResultsCacheGenNumber(tenantIDs []string) string {
	return ""
}

func (l mockCacheGenNumberLoader) Stop() {}
