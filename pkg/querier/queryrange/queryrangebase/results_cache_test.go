package queryrangebase

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gogo/protobuf/types"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	query        = "/api/v1/query_range?end=1536716898&query=sum%28container_memory_rss%29+by+%28namespace%29&start=1536673680&step=120"
	responseBody = `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"foo":"bar"},"values":[[1536673680,"137"],[1536673780,"137"]]}]},"warnings":["this is a warning"]}`
)

var (
	parsedRequest = &PrometheusRequest{
		Path:  "/api/v1/query_range",
		Start: time.UnixMilli(1536673680 * 1e3),
		End:   time.UnixMilli(1536716898 * 1e3),
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
		Start:          time.UnixMilli(1536673680 * 1e3),
		End:            time.UnixMilli(1536716898 * 1e3),
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
		Status:   "success",
		Warnings: []string{"this is a warning"},
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
	anyRes, err := types.MarshalAny(res)
	if err != nil {
		panic(err)
	}
	return Extent{
		Start:    start,
		End:      end,
		Response: anyRes,
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
			request:  &PrometheusRequest{Query: "metric @ 123", End: time.UnixMilli(125000)},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:     "@ modifier on vector selector, after end, before maxCacheTime",
			request:  &PrometheusRequest{Query: "metric @ 127", End: time.UnixMilli(125000)},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on vector selector, before end, after maxCacheTime",
			request:  &PrometheusRequest{Query: "metric @ 151", End: time.UnixMilli(200000)},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on vector selector, after end, after maxCacheTime",
			request:  &PrometheusRequest{Query: "metric @ 151", End: time.UnixMilli(125000)},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on vector selector with start() before maxCacheTime",
			request:  &PrometheusRequest{Query: "metric @ start()", Start: time.UnixMilli(100000), End: time.UnixMilli(200000)},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:     "@ modifier on vector selector with end() after maxCacheTime",
			request:  &PrometheusRequest{Query: "metric @ end()", Start: time.UnixMilli(100000), End: time.UnixMilli(200000)},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		// @ modifier on matrix selectors.
		{
			name:     "@ modifier on matrix selector, before end, before maxCacheTime",
			request:  &PrometheusRequest{Query: "rate(metric[5m] @ 123)", End: time.UnixMilli(125000)},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:     "@ modifier on matrix selector, after end, before maxCacheTime",
			request:  &PrometheusRequest{Query: "rate(metric[5m] @ 127)", End: time.UnixMilli(125000)},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on matrix selector, before end, after maxCacheTime",
			request:  &PrometheusRequest{Query: "rate(metric[5m] @ 151)", End: time.UnixMilli(200000)},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on matrix selector, after end, after maxCacheTime",
			request:  &PrometheusRequest{Query: "rate(metric[5m] @ 151)", End: time.UnixMilli(125000)},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on matrix selector with start() before maxCacheTime",
			request:  &PrometheusRequest{Query: "rate(metric[5m] @ start())", Start: time.UnixMilli(100000), End: time.UnixMilli(200000)},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:     "@ modifier on matrix selector with end() after maxCacheTime",
			request:  &PrometheusRequest{Query: "rate(metric[5m] @ end())", Start: time.UnixMilli(100000), End: time.UnixMilli(200000)},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		// @ modifier on subqueries.
		{
			name:     "@ modifier on subqueries, before end, before maxCacheTime",
			request:  &PrometheusRequest{Query: "sum_over_time(rate(metric[1m])[10m:1m] @ 123)", End: time.UnixMilli(125000)},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:     "@ modifier on subqueries, after end, before maxCacheTime",
			request:  &PrometheusRequest{Query: "sum_over_time(rate(metric[1m])[10m:1m] @ 127)", End: time.UnixMilli(125000)},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on subqueries, before end, after maxCacheTime",
			request:  &PrometheusRequest{Query: "sum_over_time(rate(metric[1m])[10m:1m] @ 151)", End: time.UnixMilli(200000)},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on subqueries, after end, after maxCacheTime",
			request:  &PrometheusRequest{Query: "sum_over_time(rate(metric[1m])[10m:1m] @ 151)", End: time.UnixMilli(125000)},
			input:    Response(&PrometheusResponse{}),
			expected: false,
		},
		{
			name:     "@ modifier on subqueries with start() before maxCacheTime",
			request:  &PrometheusRequest{Query: "sum_over_time(rate(metric[1m])[10m:1m] @ start())", Start: time.UnixMilli(100000), End: time.UnixMilli(200000)},
			input:    Response(&PrometheusResponse{}),
			expected: true,
		},
		{
			name:     "@ modifier on subqueries with end() after maxCacheTime",
			request:  &PrometheusRequest{Query: "sum_over_time(rate(metric[1m])[10m:1m] @ end())", Start: time.UnixMilli(100000), End: time.UnixMilli(200000)},
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

func TestResultsCache(t *testing.T) {
	calls := 0
	cfg := ResultsCacheConfig{
		Config: resultscache.Config{
			CacheConfig: cache.Config{
				Cache: cache.NewMockCache(),
			},
		},
	}
	c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache, constants.Loki)
	require.NoError(t, err)
	rcm, err := NewResultsCacheMiddleware(
		log.NewNopLogger(),
		c,
		resultscache.ConstSplitter(day),
		mockLimits{},
		PrometheusCodecForRangeQueries,
		PrometheusResponseExtractor{},
		nil,
		nil,
		func(_ context.Context, _ []string, _ Request) int {
			return mockLimits{}.MaxQueryParallelism(context.Background(), "fake")
		},
		false,
		false,
		nil,
	)
	require.NoError(t, err)

	rc := rcm.Wrap(HandlerFunc(func(_ context.Context, _ Request) (Response, error) {
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
	req := parsedRequest.WithStartEnd(parsedRequest.GetStart(), parsedRequest.GetEnd().Add(100*time.Millisecond))
	_, err = rc.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestResultsCacheRecent(t *testing.T) {
	var cfg ResultsCacheConfig
	flagext.DefaultValues(&cfg)
	cfg.CacheConfig.Cache = cache.NewMockCache()
	c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache, constants.Loki)
	require.NoError(t, err)
	rcm, err := NewResultsCacheMiddleware(
		log.NewNopLogger(),
		c,
		resultscache.ConstSplitter(day),
		mockLimits{maxCacheFreshness: 10 * time.Minute},
		PrometheusCodecForRangeQueries,
		PrometheusResponseExtractor{},
		nil,
		nil,
		func(_ context.Context, _ []string, _ Request) int {
			return mockLimits{}.MaxQueryParallelism(context.Background(), "fake")
		},
		false,
		false,
		nil,
	)
	require.NoError(t, err)

	req := parsedRequest.WithStartEnd(time.Now().Add(-60*1e3*time.Millisecond), time.Now())

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
		{"0", &PrometheusRequest{Start: time.UnixMilli(0), Step: 10, Query: "foo{}"}, 30 * time.Minute, "fake:foo{}:10:0"},
		{"<30m", &PrometheusRequest{Start: time.UnixMilli(toMs(10 * time.Minute)), Step: 10, Query: "foo{}"}, 30 * time.Minute, "fake:foo{}:10:0"},
		{"30m", &PrometheusRequest{Start: time.UnixMilli(toMs(30 * time.Minute)), Step: 10, Query: "foo{}"}, 30 * time.Minute, "fake:foo{}:10:1"},
		{"91m", &PrometheusRequest{Start: time.UnixMilli(toMs(91 * time.Minute)), Step: 10, Query: "foo{}"}, 30 * time.Minute, "fake:foo{}:10:3"},
		{"0", &PrometheusRequest{Start: time.UnixMilli(0), Step: 10, Query: "foo{}"}, 24 * time.Hour, "fake:foo{}:10:0"},
		{"<1d", &PrometheusRequest{Start: time.UnixMilli(toMs(22 * time.Hour)), Step: 10, Query: "foo{}"}, 24 * time.Hour, "fake:foo{}:10:0"},
		{"4d", &PrometheusRequest{Start: time.UnixMilli(toMs(4 * 24 * time.Hour)), Step: 10, Query: "foo{}"}, 24 * time.Hour, "fake:foo{}:10:4"},
		{"3d5h", &PrometheusRequest{Start: time.UnixMilli(toMs(77 * time.Hour)), Step: 10, Query: "foo{}"}, 24 * time.Hour, "fake:foo{}:10:3"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s - %s", tt.name, tt.interval), func(t *testing.T) {
			if got := resultscache.ConstSplitter(tt.interval).GenerateCacheKey(context.Background(), "fake", tt.r.(resultscache.Request)); got != tt.want {
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
			shouldCache: func(_ context.Context, _ Request) bool {
				return false
			},
			requests:     []Request{parsedRequest, parsedRequest},
			expectedCall: 2,
		},
		{
			name: "check cache based on request",
			shouldCache: func(_ context.Context, r Request) bool {
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
			c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache, constants.Loki)
			require.NoError(t, err)
			rcm, err := NewResultsCacheMiddleware(
				log.NewNopLogger(),
				c,
				resultscache.ConstSplitter(day),
				mockLimits{maxCacheFreshness: 10 * time.Minute},
				PrometheusCodecForRangeQueries,
				PrometheusResponseExtractor{},
				nil,
				tc.shouldCache,
				func(_ context.Context, _ []string, _ Request) int {
					return mockLimits{}.MaxQueryParallelism(context.Background(), "fake")
				},
				false,
				false,
				nil,
			)
			require.NoError(t, err)
			rc := rcm.Wrap(HandlerFunc(func(_ context.Context, _ Request) (Response, error) {
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

func (mockCacheGenNumberLoader) GetResultsCacheGenNumber(_ []string) string {
	return ""
}

func (l mockCacheGenNumberLoader) Stop() {}
