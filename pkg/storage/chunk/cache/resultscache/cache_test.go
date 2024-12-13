package resultscache

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gogo/protobuf/types"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const day = 24 * time.Hour

var (
	parsedRequest = &MockRequest{
		Start: time.UnixMilli(1536673680 * 1e3),
		End:   time.UnixMilli(1536716898 * 1e3),
		Step:  120 * 1e3,
		Query: "sum(container_memory_rss) by (namespace)",
	}

	parsedResponse = &MockResponse{
		Labels: []*MockLabelsPair{
			{Name: "foo", Value: "bar"},
		},
		Samples: []*MockSample{
			{Value: 137, TimestampMs: 1536673680000},
			{Value: 137, TimestampMs: 1536673780000},
		},
	}
)

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
			input: &MockRequest{
				Start: time.UnixMilli(0),
				End:   time.UnixMilli(100),
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
			input: &MockRequest{
				Start: time.UnixMilli(0),
				End:   time.UnixMilli(100),
			},
			prevCachedResponse: []Extent{
				mkExtent(110, 210),
			},
			expectedRequests: []Request{
				&MockRequest{
					Start: time.UnixMilli(0),
					End:   time.UnixMilli(100),
				},
			},
		},
		{
			name: "Test a partial hit.",
			input: &MockRequest{
				Start: time.UnixMilli(0),
				End:   time.UnixMilli(100),
			},
			prevCachedResponse: []Extent{
				mkExtent(50, 100),
			},
			expectedRequests: []Request{
				&MockRequest{
					Start: time.UnixMilli(0),
					End:   time.UnixMilli(50),
				},
			},
			expectedCachedResponse: []Response{
				mkAPIResponse(50, 100, 10),
			},
		},
		{
			name: "Test multiple partial hits.",
			input: &MockRequest{
				Start: time.UnixMilli(100),
				End:   time.UnixMilli(200),
			},
			prevCachedResponse: []Extent{
				mkExtent(50, 120),
				mkExtent(160, 250),
			},
			expectedRequests: []Request{
				&MockRequest{
					Start: time.UnixMilli(120),
					End:   time.UnixMilli(160),
				},
			},
			expectedCachedResponse: []Response{
				mkAPIResponse(100, 120, 10),
				mkAPIResponse(160, 200, 10),
			},
		},
		{
			name: "Partial hits with tiny gap.",
			input: &MockRequest{
				Start: time.UnixMilli(100),
				End:   time.UnixMilli(160),
			},
			prevCachedResponse: []Extent{
				mkExtent(50, 120),
				mkExtent(122, 130),
			},
			expectedRequests: []Request{
				&MockRequest{
					Start: time.UnixMilli(120),
					End:   time.UnixMilli(160),
				},
			},
			expectedCachedResponse: []Response{
				mkAPIResponse(100, 120, 10),
			},
		},
		{
			name: "Extent is outside the range and the request has a single step (same start and end).",
			input: &MockRequest{
				Start: time.UnixMilli(100),
				End:   time.UnixMilli(100),
			},
			prevCachedResponse: []Extent{
				mkExtent(50, 90),
			},
			expectedRequests: []Request{
				&MockRequest{
					Start: time.UnixMilli(100),
					End:   time.UnixMilli(100),
				},
			},
		},
		{
			name: "Test when hit has a large step and only a single sample extent.",
			// If there is a only a single sample in the split interval, start and end will be the same.
			input: &MockRequest{
				Start: time.UnixMilli(100),
				End:   time.UnixMilli(100),
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
			s := ResultsCache{
				extractor:      MockExtractor{},
				minCacheExtent: 10,
			}
			reqs, resps, err := s.partition(tc.input, tc.prevCachedResponse)
			require.Nil(t, err)
			require.Equal(t, tc.expectedRequests, reqs)
			require.Equal(t, tc.expectedCachedResponse, resps)
		})
	}
}

func TestPartition_onlyUseEntireExtent(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		input                  Request
		prevCachedResponse     []Extent
		expectedRequests       []Request
		expectedCachedResponse []Response
	}{
		{
			name: "overlapping extent - right",
			input: &MockRequest{
				Start: time.UnixMilli(0),
				End:   time.UnixMilli(100),
			},
			prevCachedResponse: []Extent{
				mkExtent(60, 120),
			},
			expectedRequests: []Request{
				&MockRequest{
					Start: time.UnixMilli(0),
					End:   time.UnixMilli(100),
				},
			},
		},
		{
			name: "overlapping extent - left",
			input: &MockRequest{
				Start: time.UnixMilli(20),
				End:   time.UnixMilli(100),
			},
			prevCachedResponse: []Extent{
				mkExtent(0, 50),
			},
			expectedRequests: []Request{
				&MockRequest{
					Start: time.UnixMilli(20),
					End:   time.UnixMilli(100),
				},
			},
		},
		{
			name: "overlapping extent larger than the request",
			input: &MockRequest{
				Start: time.UnixMilli(20),
				End:   time.UnixMilli(100),
			},
			prevCachedResponse: []Extent{
				mkExtent(0, 120),
			},
			expectedRequests: []Request{
				&MockRequest{
					Start: time.UnixMilli(20),
					End:   time.UnixMilli(100),
				},
			},
		},
		{
			name: "overlapping extent within the requested query range",
			input: &MockRequest{
				Start: time.UnixMilli(0),
				End:   time.UnixMilli(120),
			},
			prevCachedResponse: []Extent{
				mkExtent(0, 100),
			},
			expectedRequests: []Request{
				&MockRequest{
					Start: time.UnixMilli(100),
					End:   time.UnixMilli(120),
				},
			},
			expectedCachedResponse: []Response{
				mkAPIResponse(0, 100, 10),
			},
		},
		{
			name: "multiple overlapping extents",
			input: &MockRequest{
				Start: time.UnixMilli(50),
				End:   time.UnixMilli(200),
			},
			prevCachedResponse: []Extent{
				mkExtent(0, 80),
				mkExtent(100, 150),
				mkExtent(150, 180),
				mkExtent(200, 250),
			},
			expectedRequests: []Request{
				&MockRequest{
					Start: time.UnixMilli(50),
					End:   time.UnixMilli(100),
				},
				&MockRequest{
					Start: time.UnixMilli(180),
					End:   time.UnixMilli(200),
				},
			},
			expectedCachedResponse: []Response{
				mkAPIResponse(100, 150, 10),
				mkAPIResponse(150, 180, 10),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ResultsCache{
				extractor:           MockExtractor{},
				minCacheExtent:      10,
				onlyUseEntireExtent: true,
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
			input: &MockRequest{
				Start: time.UnixMilli(100),
				End:   time.UnixMilli(120),
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
			input: &MockRequest{
				Start: time.UnixMilli(100),
				End:   time.UnixMilli(200),
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
			input: &MockRequest{
				Start: time.UnixMilli(100),
				End:   time.UnixMilli(105),
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
			input: &MockRequest{
				Start: time.UnixMilli(106),
				End:   time.UnixMilli(108),
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
			input: &MockRequest{
				Start: time.UnixMilli(100),
				End:   time.UnixMilli(106),
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
			input: &MockRequest{
				Start: time.UnixMilli(100),
				End:   time.UnixMilli(106),
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
			input: &MockRequest{
				Start: time.UnixMilli(40),
				End:   time.UnixMilli(80),
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
			input: &MockRequest{
				Start: time.UnixMilli(40),
				End:   time.UnixMilli(80),
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
			input: &MockRequest{
				Start: time.UnixMilli(100),
				End:   time.UnixMilli(180),
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
			input: &MockRequest{
				// This request is carefully crated such that cachedEntry is not used to fulfill
				// the request.
				Start: time.UnixMilli(160),
				End:   time.UnixMilli(180),
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
			sut := ResultsCache{
				cache:             cache.NewMockCache(),
				extractor:         MockExtractor{},
				minCacheExtent:    10,
				limits:            mockLimits{},
				merger:            MockMerger{},
				parallelismForReq: func(_ context.Context, _ []string, _ Request) int { return 1 },
				next: HandlerFunc(func(_ context.Context, req Request) (Response, error) {
					return mkAPIResponse(req.GetStart().UnixMilli(), req.GetEnd().UnixMilli(), req.GetStep()), nil
				}),
			}

			ctx := user.InjectOrgID(context.Background(), "1")
			response, updatedExtents, err := sut.handleHit(ctx, tc.input, tc.cachedEntry, 0)
			require.NoError(t, err)

			expectedResponse := mkAPIResponse(tc.input.GetStart().UnixMilli(), tc.input.GetEnd().UnixMilli(), tc.input.GetStep())
			require.Equal(t, expectedResponse, response, "response does not match the expectation")
			require.Equal(t, tc.expectedUpdatedCachedEntry, updatedExtents, "updated cache entry does not match the expectation")
		})
	}
}

func TestHandleHit_queryLengthServed(t *testing.T) {
	rc := ResultsCache{
		cache:             &mockResultsCache{},
		extractor:         MockExtractor{},
		limits:            mockLimits{},
		merger:            MockMerger{},
		parallelismForReq: func(_ context.Context, _ []string, _ Request) int { return 1 },
		next: HandlerFunc(func(_ context.Context, req Request) (Response, error) {
			return mkAPIResponse(req.GetStart().UnixMilli(), req.GetEnd().UnixMilli(), req.GetStep()), nil
		}),
	}

	statsCtx, ctx := stats.NewContext(context.Background())
	ctx = user.InjectOrgID(ctx, "1")

	input := &MockRequest{
		Start: time.UnixMilli(100),
		End:   time.UnixMilli(130),
		Step:  5,
	}

	// no cached response
	_, _, err := rc.handleHit(ctx, input, nil, 0)
	require.NoError(t, err)
	require.Equal(t, int64(0), statsCtx.Caches().Result.QueryLengthServed)

	// partial hit
	extents := []Extent{
		mkExtentWithStep(60, 80, 5),
		mkExtentWithStep(90, 105, 5),
	}
	statsCtx, ctx = stats.NewContext(ctx)
	_, _, err = rc.handleHit(ctx, input, extents, 0)
	require.NoError(t, err)
	require.Equal(t, int64(5*time.Millisecond), statsCtx.Caches().Result.QueryLengthServed)

	extents = []Extent{
		mkExtentWithStep(90, 105, 5),
		mkExtentWithStep(110, 120, 5),
	}
	statsCtx, ctx = stats.NewContext(ctx)
	_, _, err = rc.handleHit(ctx, input, extents, 0)
	require.NoError(t, err)
	require.Equal(t, int64(15*time.Millisecond), statsCtx.Caches().Result.QueryLengthServed)

	// entire query served from cache
	extents = []Extent{
		mkExtentWithStep(90, 110, 5),
		mkExtentWithStep(110, 130, 5),
	}
	statsCtx, ctx = stats.NewContext(ctx)
	_, _, err = rc.handleHit(ctx, input, extents, 0)
	require.NoError(t, err)
	require.Equal(t, int64(30*time.Millisecond), statsCtx.Caches().Result.QueryLengthServed)
}

func TestResultsCacheMaxFreshness(t *testing.T) {
	modelNow := model.Now()
	for i, tc := range []struct {
		fakeLimits       Limits
		Handler          HandlerFunc
		expectedResponse *MockResponse
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
			var cfg Config
			flagext.DefaultValues(&cfg)
			cfg.CacheConfig.Cache = cache.NewMockCache()
			c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache, constants.Loki)
			require.NoError(t, err)
			fakeLimits := tc.fakeLimits
			rc := NewResultsCache(
				log.NewNopLogger(),
				c,
				tc.Handler,
				ConstSplitter(day),
				fakeLimits,
				MockMerger{},
				MockExtractor{},
				nil,
				nil,
				func(_ context.Context, _ []string, _ Request) int {
					return 10
				},
				nil,
				false,
				false,
			)
			require.NoError(t, err)

			// create cache with handler
			ctx := user.InjectOrgID(context.Background(), "1")

			// create request with start end within the key extents
			req := parsedRequest.WithStartEndForCache(time.UnixMilli(int64(modelNow)-(50*1e3)), time.UnixMilli(int64(modelNow)-(10*1e3)))

			// fill cache
			key := ConstSplitter(day).GenerateCacheKey(context.Background(), "1", req)
			rc.put(ctx, key, []Extent{mkExtent(int64(modelNow)-(600*1e3), int64(modelNow))})

			resp, err := rc.Do(ctx, req)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResponse, resp)
		})
	}
}

func Test_resultsCache_MissingData(t *testing.T) {
	cfg := Config{
		CacheConfig: cache.Config{
			Cache: cache.NewMockCache(),
		},
	}
	c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache, constants.Loki)
	require.NoError(t, err)
	rc := NewResultsCache(
		log.NewNopLogger(),
		c,
		nil,
		ConstSplitter(day),
		mockLimits{},
		MockMerger{},
		MockExtractor{},
		nil,
		nil,
		func(_ context.Context, _ []string, _ Request) int {
			return 10
		},
		nil,
		false,
		false,
	)
	require.NoError(t, err)
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

func Test_shouldCacheReq(t *testing.T) {
	cfg := Config{
		CacheConfig: cache.Config{
			Cache: cache.NewMockCache(),
		},
	}
	c, err := cache.New(cfg.CacheConfig, nil, log.NewNopLogger(), stats.ResultCache, constants.Loki)
	require.NoError(t, err)
	rc := NewResultsCache(
		log.NewNopLogger(),
		c,
		nil,
		ConstSplitter(day),
		mockLimits{},
		MockMerger{},
		MockExtractor{},
		nil,
		nil,
		func(_ context.Context, _ []string, _ Request) int {
			return 10
		},
		nil,
		false,
		false,
	)
	require.NoError(t, err)

	// create cache with handler
	ctx := user.InjectOrgID(context.Background(), "1")

	// create request with start end within the key extents
	req := parsedRequest.WithStartEndForCache(time.UnixMilli(50), time.UnixMilli(120))

	// fill cache
	key := ConstSplitter(day).GenerateCacheKey(context.Background(), "1", req)
	rc.put(ctx, key, []Extent{mkExtent(50, 120)})

	// Asserts (when `shouldLookupCache` is non-nil and set to return false (should not cache), resultcache should get result from upstream handler (mockHandler))
	// 1. With `shouldLookupCache` non-nil and set `noCacheReq`, should get result from `next` handler
	// 2. With `shouldLookupCache` non-nil and set `cacheReq`, should get result from cache
	// 3. With `shouldLookupCache` nil, should get result from cache

	cases := []struct {
		name              string
		shouldLookupCache ShouldCacheReqFn
		// expected number of times, upstream `next` handler is called inside results cache
		expCount int
	}{
		{
			name:              "don't lookup cache",
			shouldLookupCache: noLookupCache,
			expCount:          1,
		},
		{
			name:              "lookup cache",
			shouldLookupCache: lookupCache,
			expCount:          0,
		},
		{
			name:              "nil",
			shouldLookupCache: nil,
			expCount:          0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mh := &mockHandler{}
			rc.next = mh
			rc.shouldCacheReq = tc.shouldLookupCache

			_, err = rc.Do(ctx, req)
			require.NoError(t, err)
			require.Equal(t, tc.expCount, mh.called)

		})
	}
}

type mockHandler struct {
	called int
	res    Response
}

func (mh *mockHandler) Do(_ context.Context, _ Request) (Response, error) {
	mh.called++
	return mh.res, nil
}

// noLookupCache is of type `ShouldCacheReq` that always returns false (do not cache)
func noLookupCache(_ context.Context, _ Request) bool {
	return false
}

// lookupCache is of type `ShouldCacheReq` that always returns true (cache the result)
func lookupCache(_ context.Context, _ Request) bool {
	return true
}

func mkAPIResponse(start, end, step int64) *MockResponse {
	var samples []*MockSample
	for i := start; i <= end; i += step {
		samples = append(samples, &MockSample{
			TimestampMs: i,
			Value:       float64(i),
		})
	}

	return &MockResponse{
		Labels: []*MockLabelsPair{
			{Name: "foo", Value: "bar"},
		},
		Samples: samples,
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

func (r *MockRequest) WithStartEndForCache(start time.Time, end time.Time) Request {
	m := *r
	m.Start = start
	m.End = end
	return &m
}

type MockMerger struct{}

func (m MockMerger) MergeResponse(responses ...Response) (Response, error) {
	samples := make([]*MockSample, 0, len(responses)*2)
	for _, response := range responses {
		samples = append(samples, response.(*MockResponse).Samples...)
	}

	// Merge samples by:
	// 1. Sorting them by time.
	// 2. Removing duplicates.
	slices.SortFunc(samples, func(a, b *MockSample) int {
		if a.TimestampMs == b.TimestampMs {
			return 0
		}
		if a.TimestampMs < b.TimestampMs {
			return -1
		}
		return 1
	})
	samples = slices.CompactFunc(samples, func(a, b *MockSample) bool {
		return a.TimestampMs == b.TimestampMs
	})

	return &MockResponse{
		Labels:  responses[0].(*MockResponse).Labels,
		Samples: samples,
	}, nil
}

type MockExtractor struct{}

func (m MockExtractor) Extract(start, end int64, res Response, _, _ int64) Response {
	mockRes := res.(*MockResponse)

	result := MockResponse{
		Labels:  mockRes.Labels,
		Samples: make([]*MockSample, 0, len(mockRes.Samples)),
	}

	for _, sample := range mockRes.Samples {
		if start <= sample.TimestampMs && sample.TimestampMs <= end {
			result.Samples = append(result.Samples, sample)
		}
	}
	return &result
}

type mockLimits struct {
	maxCacheFreshness time.Duration
}

func (m mockLimits) MaxCacheFreshness(context.Context, string) time.Duration {
	return m.maxCacheFreshness
}

type mockResultsCache struct{}

func (m mockResultsCache) Store(context.Context, []string, [][]byte) error {
	panic("not implemented")
}
func (m mockResultsCache) Fetch(context.Context, []string) ([]string, [][]byte, []string, error) {
	panic("not implemented")
}
func (m mockResultsCache) Stop() {
	panic("not implemented")
}

func (m mockResultsCache) GetCacheType() stats.CacheType {
	// returning results cache since we do not support storing stats for "mock" cache
	return stats.ResultCache
}
