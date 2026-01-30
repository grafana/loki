package queryrange

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

const (
	lblFooBar    = `{foo="bar"}`
	lblFizzBuzz  = `{fizz="buzz"}`
	entriesLimit = 1000
)

func Test_LogResultCacheSameRange(t *testing.T) {
	var (
		ctx = user.InjectOrgID(context.Background(), "foo")
		lrc = NewLogResultCache(
			log.NewNopLogger(),
			fakeLimits{
				splitDuration: map[string]time.Duration{"foo": time.Minute},
			},
			cache.NewMockCache(),
			nil,
			nil,
			nil,
		)
	)

	req := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
		Limit:   entriesLimit,
	}

	fake := newFakeResponse([]mockResponse{
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request:  req,
				Response: emptyResponse(req),
			},
		},
	})

	h := lrc.Wrap(fake)

	resp, err := h.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req), resp)
	resp, err = h.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req), resp)

	fake.AssertExpectations(t)
}

func Test_LogResultCacheSameRangeNonEmpty(t *testing.T) {
	var (
		ctx = user.InjectOrgID(context.Background(), "foo")
		lrc = NewLogResultCache(
			log.NewNopLogger(),
			fakeLimits{
				splitDuration: map[string]time.Duration{"foo": time.Minute},
			},
			cache.NewMockCache(),
			nil,
			nil,
			nil,
		)
	)

	req := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
		Limit:   entriesLimit,
	}

	fake := newFakeResponse([]mockResponse{
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request:  req,
				Response: nonEmptyResponse(req, time.Unix(61, 0), time.Unix(61, 0), lblFooBar),
			},
		},
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request:  req,
				Response: nonEmptyResponse(req, time.Unix(62, 0), time.Unix(62, 0), lblFooBar),
			},
		},
	})

	h := lrc.Wrap(fake)

	resp, err := h.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, nonEmptyResponse(req, time.Unix(61, 0), time.Unix(61, 0), lblFooBar), resp)
	resp, err = h.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, nonEmptyResponse(req, time.Unix(62, 0), time.Unix(62, 0), lblFooBar), resp)

	fake.AssertExpectations(t)
}

func Test_LogResultCacheSmallerRange(t *testing.T) {
	var (
		ctx = user.InjectOrgID(context.Background(), "foo")
		lrc = NewLogResultCache(
			log.NewNopLogger(),
			fakeLimits{
				splitDuration: map[string]time.Duration{"foo": time.Minute},
			},
			cache.NewMockCache(),
			nil,
			nil,
			nil,
		)
	)

	req := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
		Limit:   entriesLimit,
	}

	fake := newFakeResponse([]mockResponse{
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request:  req,
				Response: emptyResponse(req),
			},
		},
	})

	h := lrc.Wrap(fake)

	resp, err := h.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req), resp)
	resp, err = h.Do(ctx, &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
		Limit:   entriesLimit,
	})
	require.NoError(t, err)
	require.Equal(t, emptyResponse(&LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
		Limit:   entriesLimit,
	}), resp)

	fake.AssertExpectations(t)
}

func Test_LogResultCacheDifferentRange(t *testing.T) {
	var (
		ctx = user.InjectOrgID(context.Background(), "foo")
		lrc = NewLogResultCache(
			log.NewNopLogger(),
			fakeLimits{
				splitDuration: map[string]time.Duration{"foo": time.Minute},
			},
			cache.NewMockCache(),
			nil,
			nil,
			nil,
		)
	)

	req1 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
		Limit:   entriesLimit,
	}

	req2 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
		Limit:   entriesLimit,
	}

	fake := newFakeResponse([]mockResponse{
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request:  req1,
				Response: emptyResponse(req1),
			},
		},
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				},
				Response: emptyResponse(&LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				}),
			},
		},
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
					Limit:   entriesLimit,
				},
				Response: emptyResponse(&LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
					Limit:   entriesLimit,
				}),
			},
		},
	})

	h := lrc.Wrap(fake)

	resp, err := h.Do(ctx, req1)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req1), resp)
	resp, err = h.Do(ctx, req2)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req2), resp)

	fake.AssertExpectations(t)
}

func Test_LogResultCacheDifferentRangeNonEmpty(t *testing.T) {
	var (
		ctx = user.InjectOrgID(context.Background(), "foo")
		lrc = NewLogResultCache(
			log.NewNopLogger(),
			fakeLimits{
				splitDuration: map[string]time.Duration{"foo": time.Minute},
			},
			cache.NewMockCache(),
			nil,
			nil,
			nil,
		)
	)

	req1 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
		Limit:   entriesLimit,
	}

	req2 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
		Limit:   entriesLimit,
	}

	fake := newFakeResponse([]mockResponse{
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request:  req1,
				Response: emptyResponse(req1),
			},
		},
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				},
				Response: nonEmptyResponse(&LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				}, time.Unix(61, 0), time.Unix(61, 0), lblFooBar),
			},
		},
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
					Limit:   entriesLimit,
				},
				Response: nonEmptyResponse(&LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
					Limit:   entriesLimit,
				}, time.Unix(62, 0), time.Unix(62, 0), lblFooBar),
			},
		},
	})

	h := lrc.Wrap(fake)

	resp, err := h.Do(ctx, req1)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req1), resp)
	resp, err = h.Do(ctx, req2)
	require.NoError(t, err)
	require.Equal(t, mergeLokiResponse(
		nonEmptyResponse(&LokiRequest{
			StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
			EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
			Limit:   entriesLimit,
		}, time.Unix(62, 0), time.Unix(62, 0), lblFooBar),
		nonEmptyResponse(&LokiRequest{
			StartTs: time.Unix(0, time.Minute.Nanoseconds()),
			EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
			Limit:   entriesLimit,
		}, time.Unix(61, 0), time.Unix(61, 0), lblFooBar),
	), resp)

	fake.AssertExpectations(t)
}

func Test_LogResultCacheDifferentRangeNonEmptyAndEmpty(t *testing.T) {
	var (
		ctx = user.InjectOrgID(context.Background(), "foo")
		lrc = NewLogResultCache(
			log.NewNopLogger(),
			fakeLimits{
				splitDuration: map[string]time.Duration{"foo": time.Minute},
			},
			cache.NewMockCache(),
			nil,
			nil,
			nil,
		)
	)

	req1 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),   // 90s
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()), // 90s
		Limit:   entriesLimit,
	}

	req2 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()),   // 60s
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()), // 120s
		Limit:   entriesLimit,
	}

	fake := newFakeResponse([]mockResponse{
		// first request is empty
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request:  req1,
				Response: emptyResponse(req1),
			},
		},
		// First req2 call triggers two queries:
		// 1. query for 60s to 90s --> empty response --> cache it
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				},
				Response: emptyResponse(&LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				}),
			},
		},
		// 2. query for 90s to 120s --> non-empty response --> DO NOT cache it
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
					Limit:   entriesLimit,
				},
				Response: nonEmptyResponse(&LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
					Limit:   entriesLimit,
				}, time.Unix(61, 0), time.Unix(61, 0), lblFooBar),
			},
		},
		// Second req2 call triggers one query because 60s to 90s is already cached (empty response)
		// So we expect a request for 90s to 120s --> non-empty response --> DO NOT cache it
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
					Limit:   entriesLimit,
				},
				Response: nonEmptyResponse(&LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
					Limit:   entriesLimit,
				}, time.Unix(61, 0), time.Unix(61, 0), lblFooBar),
			},
		},
	})

	h := lrc.Wrap(fake)

	resp, err := h.Do(ctx, req1)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req1), resp)
	resp, err = h.Do(ctx, req2)
	require.NoError(t, err)
	require.Equal(t, mergeLokiResponse(
		emptyResponse(req1),
		nonEmptyResponse(&LokiRequest{
			StartTs: time.Unix(0, time.Minute.Nanoseconds()),
			EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
			Limit:   entriesLimit,
		}, time.Unix(61, 0), time.Unix(61, 0), lblFooBar),
	), resp)
	resp, err = h.Do(ctx, req2)
	require.NoError(t, err)
	require.Equal(t, mergeLokiResponse(
		emptyResponse(req1),
		nonEmptyResponse(&LokiRequest{
			StartTs: time.Unix(0, time.Minute.Nanoseconds()),
			EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
			Limit:   entriesLimit,
		}, time.Unix(61, 0), time.Unix(61, 0), lblFooBar),
	), resp)
	fake.AssertExpectations(t)
}

// Test_LogResultNonOverlappingCache tests the scenario where the cached query does not overlap with the new request
func Test_LogResultNonOverlappingCache(t *testing.T) {
	metrics := NewLogResultCacheMetrics(prometheus.NewPedanticRegistry())
	mockCache := cache.NewMockCache()
	var (
		ctx = user.InjectOrgID(context.Background(), "foo")
		lrc = NewLogResultCache(
			log.NewNopLogger(),
			fakeLimits{
				splitDuration: map[string]time.Duration{"foo": time.Minute},
			},
			mockCache,
			nil,
			nil,
			metrics,
		)
	)

	nonEmptyHits, err := metrics.CacheHit.GetMetricWithLabelValues("non-empty")
	require.NoError(t, err)
	emptyHits, err := metrics.CacheHit.GetMetricWithLabelValues("empty")
	require.NoError(t, err)

	checkCacheMetrics := func(expectedHits, expectedMisses int) {
		require.Equal(t, float64(expectedHits), testutil.ToFloat64(nonEmptyHits)+testutil.ToFloat64(emptyHits))
		require.Equal(t, float64(expectedMisses), testutil.ToFloat64(metrics.CacheMiss))
	}

	// data requested for just 1 sec, resulting in empty response
	req1 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
		EndTs:   time.Unix(0, time.Minute.Nanoseconds()+31*time.Second.Nanoseconds()),
		Limit:   entriesLimit,
	}

	// data requested for just 1 sec(non-overlapping), resulting in empty response
	req2 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+24*time.Second.Nanoseconds()),
		EndTs:   time.Unix(0, time.Minute.Nanoseconds()+25*time.Second.Nanoseconds()),
		Limit:   entriesLimit,
	}

	// data requested for larger interval than req1(overlapping with req2), returns empty response
	req3 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+24*time.Second.Nanoseconds()),
		EndTs:   time.Unix(0, time.Minute.Nanoseconds()+29*time.Second.Nanoseconds()),
		Limit:   entriesLimit,
	}

	// data requested for larger interval than req3(non-overlapping), returns non-empty response
	req4 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+10*time.Second.Nanoseconds()),
		EndTs:   time.Unix(0, time.Minute.Nanoseconds()+20*time.Second.Nanoseconds()),
		Limit:   entriesLimit,
	}

	fake := newFakeResponse([]mockResponse{
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request:  req1,
				Response: emptyResponse(req1),
			},
		},
		// req2 should do query for just its query range and should not update the cache
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()+24*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+25*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				},
				Response: emptyResponse(&LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()+24*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+25*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				}),
			},
		},
		// req3 should do query for just its query range and should update the cache
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()+24*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+29*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				},
				Response: emptyResponse(&LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()+24*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+29*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				}),
			},
		},
		// req4 should do query for its query range. Data would be non-empty so cache should not be updated
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()+10*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+20*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				},
				Response: nonEmptyResponse(&LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()+10*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+20*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				}, time.Unix(71, 0), time.Unix(79, 0), lblFooBar),
			},
		},
	})

	h := lrc.Wrap(fake)

	resp, err := h.Do(ctx, req1)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req1), resp)
	checkCacheMetrics(0, 1)
	require.Equal(t, 1, mockCache.NumKeyUpdates())

	// req2 should not update the cache since it has same length as previously cached query
	resp, err = h.Do(ctx, req2)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req2), resp)
	checkCacheMetrics(1, 1)
	require.Equal(t, 1, mockCache.NumKeyUpdates())

	// req3 should update the cache since it has larger length than previously cached query
	resp, err = h.Do(ctx, req3)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req3), resp)
	checkCacheMetrics(2, 1)
	require.Equal(t, 2, mockCache.NumKeyUpdates())

	// req4 returns non-empty response so it should not update the cache
	resp, err = h.Do(ctx, req4)
	require.NoError(t, err)
	require.Equal(t, nonEmptyResponse(req4, time.Unix(71, 0), time.Unix(79, 0), lblFooBar), resp)
	checkCacheMetrics(3, 1)
	require.Equal(t, 2, mockCache.NumKeyUpdates())

	// req2 should return back empty response from the cache, without updating the cache
	resp, err = h.Do(ctx, req2)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req2), resp)
	checkCacheMetrics(4, 1)
	require.Equal(t, 2, mockCache.NumKeyUpdates())

	fake.AssertExpectations(t)
}

func Test_LogResultCacheDifferentLimit(t *testing.T) {
	var (
		ctx = user.InjectOrgID(context.Background(), "foo")
		lrc = NewLogResultCache(
			log.NewNopLogger(),
			fakeLimits{
				splitDuration: map[string]time.Duration{"foo": time.Minute},
			},
			cache.NewMockCache(),
			nil,
			nil,
			nil,
		)
	)

	req1 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
		Limit:   entriesLimit,
	}

	req2 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
		Limit:   10,
	}

	fake := newFakeResponse([]mockResponse{
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request:  req1,
				Response: emptyResponse(req1),
			},
		},
	})

	h := lrc.Wrap(fake)

	resp, err := h.Do(ctx, req1)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req1), resp)
	resp, err = h.Do(ctx, req2)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req2), resp)

	fake.AssertExpectations(t)
}

func TestExtractLokiResponse(t *testing.T) {
	for _, tc := range []struct {
		name           string
		direction      logproto.Direction
		resp           *LokiResponse
		extractFrom    time.Time
		extractThrough time.Time
		expectedResp   *LokiResponse
	}{
		{
			name:           "nothing to extract",
			direction:      logproto.FORWARD,
			resp:           &LokiResponse{},
			extractFrom:    time.Unix(0, 0),
			extractThrough: time.Unix(0, 1),
			expectedResp: &LokiResponse{
				Data: LokiData{Result: logproto.Streams{}},
			},
		},
		{
			name:      "extract interval within response",
			direction: logproto.FORWARD,
			resp: mergeLokiResponse(
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(0, 0), time.Unix(10, 0), lblFooBar),
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(2, 0), time.Unix(8, 0), lblFizzBuzz),
			),
			extractFrom:    time.Unix(4, 0),
			extractThrough: time.Unix(7, 0),
			expectedResp: mergeLokiResponse(
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(4, 0), time.Unix(6, 0), lblFooBar),
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(4, 0), time.Unix(6, 0), lblFizzBuzz),
			),
		},
		{
			name:      "extract part of response in the beginning",
			direction: logproto.FORWARD,
			resp: mergeLokiResponse(
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(0, 0), time.Unix(10, 0), lblFooBar),
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(2, 0), time.Unix(8, 0), lblFizzBuzz),
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(5, 0), time.Unix(8, 0), `{not="included"}`),
			),
			extractFrom:    time.Unix(0, 0),
			extractThrough: time.Unix(4, 0),
			expectedResp: mergeLokiResponse(
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(0, 0), time.Unix(3, 0), lblFooBar),
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(2, 0), time.Unix(3, 0), lblFizzBuzz),
				&LokiResponse{},
			),
		},
		{
			name:      "extract part of response in the end",
			direction: logproto.FORWARD,
			resp: mergeLokiResponse(
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(0, 0), time.Unix(10, 0), lblFooBar),
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(2, 0), time.Unix(8, 0), lblFizzBuzz),
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(0, 0), time.Unix(2, 0), `{not="included"}`),
			),
			extractFrom:    time.Unix(4, 0),
			extractThrough: time.Unix(12, 0),
			expectedResp: mergeLokiResponse(
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(4, 0), time.Unix(10, 0), lblFooBar),
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(4, 0), time.Unix(8, 0), lblFizzBuzz),
				&LokiResponse{},
			),
		},
		{
			name:      "extract interval out of data range",
			direction: logproto.FORWARD,
			resp: mergeLokiResponse(
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(0, 0), time.Unix(10, 0), lblFooBar),
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(2, 0), time.Unix(8, 0), lblFizzBuzz),
				nonEmptyResponse(&LokiRequest{Limit: entriesLimit}, time.Unix(0, 0), time.Unix(2, 0), `{not="included"}`),
			),
			extractFrom:    time.Unix(50, 0),
			extractThrough: time.Unix(52, 0),
			expectedResp: mergeLokiResponse(
				// empty responses here are to avoid failing test due to difference in count of subqueries in query stats
				&LokiResponse{Limit: entriesLimit},
				&LokiResponse{Limit: entriesLimit},
				&LokiResponse{Limit: entriesLimit},
			),
		},
		{
			name:      "BACKWARD: extract from beginning of time range",
			direction: logproto.BACKWARD,
			// Response with entries from T100 to T110 (sorted newest to oldest: T110, T109, ..., T100)
			resp: nonEmptyResponse(&LokiRequest{Limit: entriesLimit, Direction: logproto.BACKWARD}, time.Unix(100, 0), time.Unix(110, 0), lblFooBar),
			// Extract [T100, T105) - at the BEGINNING of the time range
			extractFrom:    time.Unix(100, 0),
			extractThrough: time.Unix(105, 0),
			// Should have 5 entries: T104, T103, T102, T101, T100 (in BACKWARD order)
			expectedResp: nonEmptyResponse(&LokiRequest{Limit: entriesLimit, Direction: logproto.BACKWARD}, time.Unix(100, 0), time.Unix(104, 0), lblFooBar),
		},
		{
			name:      "BACKWARD: extract from middle of time range",
			direction: logproto.BACKWARD,
			// Response with entries from T100 to T120
			resp: nonEmptyResponse(&LokiRequest{Limit: entriesLimit, Direction: logproto.BACKWARD}, time.Unix(100, 0), time.Unix(120, 0), lblFooBar),
			// Extract [T108, T115) from the middle
			extractFrom:    time.Unix(108, 0),
			extractThrough: time.Unix(115, 0),
			// Should have 7 entries: T114, T113, T112, T111, T110, T109, T108 (in BACKWARD order)
			expectedResp: nonEmptyResponse(&LokiRequest{Limit: entriesLimit, Direction: logproto.BACKWARD}, time.Unix(108, 0), time.Unix(114, 0), lblFooBar),
		},
		{
			name:      "BACKWARD: extract from end of time range",
			direction: logproto.BACKWARD,
			// Response with entries from T100 to T110
			resp: nonEmptyResponse(&LokiRequest{Limit: entriesLimit, Direction: logproto.BACKWARD}, time.Unix(100, 0), time.Unix(110, 0), lblFooBar),
			// Extract [T106, T115) - at the END of the time range (extends past data)
			extractFrom:    time.Unix(106, 0),
			extractThrough: time.Unix(115, 0),
			// Should have 5 entries: T110, T109, T108, T107, T106 (in BACKWARD order)
			expectedResp: nonEmptyResponse(&LokiRequest{Limit: entriesLimit, Direction: logproto.BACKWARD}, time.Unix(106, 0), time.Unix(110, 0), lblFooBar),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedResp, extractLokiResponse(tc.extractFrom, tc.extractThrough, tc.direction, tc.resp))
		})
	}
}

func Test_LogResultCacheResponseSize(t *testing.T) {
	// Configure limits for tenant1 (enabled, max 1024) and tenant2 (disabled)
	limits := fakeLimits{
		splitDuration: map[string]time.Duration{
			"tenant1": time.Minute,
			"tenant2": time.Minute,
		},
		logResultCacheMaxResponseSize: map[string]int{
			"tenant1": 1024,
		},
		logResultCacheStoreNonEmptyResponse: map[string]bool{
			"tenant1": true,
			"tenant2": false,
		},
	}

	// Shared cache and handler setup
	mockCache := cache.NewMockCache()
	lrc := NewLogResultCache(
		log.NewNopLogger(),
		limits,
		mockCache,
		nil,
		nil,
		nil,
	)

	t.Run("response cache disabled", func(t *testing.T) {
		reqTenant2 := &LokiRequest{
			StartTs: time.Unix(0, time.Minute.Nanoseconds()),
			EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
			Limit:   entriesLimit,
			Query:   lblFooBar,
		}
		respTenant2 := nonEmptyResponse(reqTenant2, time.Unix(61, 0), time.Unix(65, 0), lblFooBar)
		ctx := user.InjectOrgID(context.Background(), "tenant2")

		fakeFirstRequest := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqTenant2,
					Response: respTenant2,
				},
			},
		})
		h := lrc.Wrap(fakeFirstRequest)

		resp, err := h.Do(ctx, reqTenant2)
		require.NoError(t, err)
		require.Equal(t, respTenant2, resp)
		fakeFirstRequest.AssertExpectations(t)

		fakeSecondRequest := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqTenant2,
					Response: respTenant2,
				},
			},
		})
		h = lrc.Wrap(fakeSecondRequest)

		resp, err = h.Do(ctx, reqTenant2)
		require.NoError(t, err)
		require.Equal(t, respTenant2, resp)
		fakeSecondRequest.AssertExpectations(t)
	})
	t.Run("response too big to cache", func(t *testing.T) {
		// Tenant1 runs a request that returns a response bigger than the max size (1024)
		reqTenant1Large := &LokiRequest{
			StartTs: time.Unix(0, time.Minute.Nanoseconds()),
			EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
			Limit:   entriesLimit,
			Query:   lblFooBar,
		}
		respTenant1Large := nonEmptyResponse(reqTenant1Large, time.Unix(61, 0), time.Unix(200, 0), lblFooBar)
		ctx := user.InjectOrgID(context.Background(), "tenant1")

		fakeFirstRequest := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqTenant1Large,
					Response: respTenant1Large,
				},
			},
		})
		h := lrc.Wrap(fakeFirstRequest)

		resp, err := h.Do(ctx, reqTenant1Large)
		require.NoError(t, err)
		require.Equal(t, respTenant1Large, resp)
		fakeFirstRequest.AssertExpectations(t)

		// Tenant1 runs the same request. Cache is not hit because the previous response was too big to cache.
		fakeSecondRequest := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqTenant1Large,
					Response: respTenant1Large,
				},
			},
		})

		h = lrc.Wrap(fakeSecondRequest)

		resp, err = h.Do(ctx, reqTenant1Large)
		require.NoError(t, err)
		require.Equal(t, respTenant1Large, resp)
		fakeSecondRequest.AssertExpectations(t)
	})

	t.Run("cached response", func(t *testing.T) {
		reqTenant1Small := &LokiRequest{
			StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()), // 2 minutes (120 seconds) - same start as step 7 to test limit difference
			EndTs:   time.Unix(0, 3*time.Minute.Nanoseconds()), // 3 minutes (180 seconds)
			Limit:   entriesLimit,
			Query:   lblFooBar,
		}
		respTenant1Small := nonEmptyResponse(reqTenant1Small, time.Unix(121, 0), time.Unix(125, 0), lblFooBar)

		ctx := user.InjectOrgID(context.Background(), "tenant1")
		fakeFirstRequest := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqTenant1Small,
					Response: respTenant1Small,
				},
			},
		})
		h := lrc.Wrap(fakeFirstRequest)

		resp, err := h.Do(ctx, reqTenant1Small)
		require.NoError(t, err)
		require.Equal(t, respTenant1Small, resp)
		fakeFirstRequest.AssertExpectations(t)

		// Tenant1 runs the same request. Cache is hit.
		fakeSecondRequest := newFakeResponse([]mockResponse{})
		h = lrc.Wrap(fakeSecondRequest)

		resp, err = h.Do(ctx, reqTenant1Small)
		require.NoError(t, err)
		require.Equal(t, respTenant1Small, resp)
		fakeSecondRequest.AssertExpectations(t)
	})

	t.Run("request with different limit than cached", func(t *testing.T) {
		reqTenant1DifferentLimit := &LokiRequest{
			StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()), // 2 minutes (120 seconds) - same as reqTenant1Small
			EndTs:   time.Unix(0, 3*time.Minute.Nanoseconds()), // 3 minutes (180 seconds) - same as reqTenant1Small
			Limit:   500,                                       // Different limit than entriesLimit (1000)
			Query:   lblFooBar,
		}
		respTenant1DifferentLimit := nonEmptyResponse(reqTenant1DifferentLimit, time.Unix(121, 0), time.Unix(125, 0), lblFooBar)

		ctx := user.InjectOrgID(context.Background(), "tenant1")
		fakeFirstRequest := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqTenant1DifferentLimit,
					Response: respTenant1DifferentLimit,
				},
			},
		})
		h := lrc.Wrap(fakeFirstRequest)

		resp, err := h.Do(ctx, reqTenant1DifferentLimit)
		require.NoError(t, err)
		require.Equal(t, respTenant1DifferentLimit, resp)
		fakeFirstRequest.AssertExpectations(t)

		// Tenant1 runs the same request. Now cache is hit.
		fakeSecondRequest := newFakeResponse([]mockResponse{})
		h = lrc.Wrap(fakeSecondRequest)

		resp, err = h.Do(ctx, reqTenant1DifferentLimit)
		require.NoError(t, err)
		require.Equal(t, respTenant1DifferentLimit, resp)
	})

	t.Run("Overlapping request", func(t *testing.T) {
		// Tenant1 requests a time range that overlaps and extends beyond the previous cached request (previous test case).
		// reqTenant1DifferentLimit: 120-180 seconds (2-3 minutes), response has entries 121-125 seconds, limit 500
		// reqTenant1Overlap: 120-360 seconds (2-6 minutes), same start as cached range, extends beyond it
		// Since it has the same cache key (same aligned start), it will find the cached response and extend it.
		reqTenant1Overlap := &LokiRequest{
			StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()), // 2 minutes (120 seconds) - same aligned start as cached request
			EndTs:   time.Unix(0, 6*time.Minute.Nanoseconds()), // 6 minutes (360 seconds) - extends beyond cached range
			Limit:   500,                                       // Same limit as reqTenant1DifferentLimit so we can reuse cached response
			Query:   lblFooBar,
		}

		ctx := user.InjectOrgID(context.Background(), "tenant1")
		// The request overlaps with cached reqTenant1DifferentLimit (request range 120-180s, entries 121-125s)
		// Cache will:
		// 1. Find the cached response (same cache key, same limit)
		// 2. Extract overlapping part from cache: 120-180s range -> entries 121-125s
		// 3. Fetch missing end part: 180-360s
		// handleResponseHit should be called to fetch only the end part (180-360s)
		// Create the expected end request that will be made by handleResponseHit
		expectedEndRequest := reqTenant1Overlap.WithStartEnd(
			time.Unix(0, 3*time.Minute.Nanoseconds()), // cachedRequest.GetEndTs() = 180s
			time.Unix(0, 6*time.Minute.Nanoseconds()), // lokiReq.GetEndTs() = 360s
		).(*LokiRequest)
		expectedEndResponse := nonEmptyResponse(expectedEndRequest, time.Unix(180, 0), time.Unix(190, 0), lblFooBar)

		fakeFirstRequest := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  expectedEndRequest,
					Response: expectedEndResponse,
				},
			},
		})
		h := lrc.Wrap(fakeFirstRequest)

		resp, err := h.Do(ctx, reqTenant1Overlap)
		require.NoError(t, err)
		expectedResp := mergeLokiResponse(
			nonEmptyResponse(&LokiRequest{Limit: 500}, time.Unix(121, 0), time.Unix(125, 0), lblFooBar), // From previous test case (cached)
			expectedEndResponse,
		)
		require.Equal(t, expectedResp, resp)
		fakeFirstRequest.AssertExpectations(t)

		// Now the cache should hit completely since the cached response/request covers the entire request range.
		fakeSecondRequest := newFakeResponse([]mockResponse{})
		h = lrc.Wrap(fakeSecondRequest)

		resp, err = h.Do(ctx, reqTenant1Overlap)
		require.NoError(t, err)
		require.Equal(t, expectedResp, resp)
		fakeSecondRequest.AssertExpectations(t)
	})

	t.Run("Overlapping empty request", func(t *testing.T) {
		reqTenant1PartiallyEmpty := &LokiRequest{
			StartTs: time.Unix(0, 10*time.Minute.Nanoseconds()), // 10 minutes (600 seconds)
			EndTs:   time.Unix(0, 15*time.Minute.Nanoseconds()), // 15 minutes (900 seconds)
			Limit:   entriesLimit,
			Query:   lblFooBar,
		}
		// Contains some entries from minute 13 (780 seconds) to 14 (840 seconds)
		respTenant1PartiallyEmpty := nonEmptyResponse(reqTenant1PartiallyEmpty, time.Unix(790, 0), time.Unix(810, 0), lblFooBar)

		ctx := user.InjectOrgID(context.Background(), "tenant1")
		fakeFirstRequest := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqTenant1PartiallyEmpty,
					Response: respTenant1PartiallyEmpty,
				},
			},
		})
		h := lrc.Wrap(fakeFirstRequest)
		resp, err := h.Do(ctx, reqTenant1PartiallyEmpty)
		require.NoError(t, err)
		require.Equal(t, respTenant1PartiallyEmpty, resp)
		fakeFirstRequest.AssertExpectations(t)

		// A request from 10 to 12 should hit the cache and return an empty response
		reqTenant1Empty := &LokiRequest{
			StartTs: time.Unix(0, 10*time.Minute.Nanoseconds()),
			EndTs:   time.Unix(0, 12*time.Minute.Nanoseconds()),
			Limit:   entriesLimit,
			Query:   lblFooBar,
		}
		respTenant1Empty := emptyResponse(reqTenant1Empty)
		fakeSecondRequest := newFakeResponse([]mockResponse{})

		h = lrc.Wrap(fakeSecondRequest)
		resp, err = h.Do(ctx, reqTenant1Empty)
		require.NoError(t, err)
		require.Equal(t, respTenant1Empty, resp)
		fakeSecondRequest.AssertExpectations(t)

		// But it should not update the cache again
		fakeThirdRequest := newFakeResponse([]mockResponse{})
		h = lrc.Wrap(fakeThirdRequest)
		resp, err = h.Do(ctx, reqTenant1PartiallyEmpty)
		require.NoError(t, err)
		require.Equal(t, respTenant1PartiallyEmpty, resp)
		fakeThirdRequest.AssertExpectations(t)
	})

	t.Run("Different direction should not hit cache", func(t *testing.T) {
		// First request with FORWARD direction
		reqTenant1Forward := &LokiRequest{
			StartTs:   time.Unix(0, 20*time.Minute.Nanoseconds()), // 20 minutes (1200 seconds)
			EndTs:     time.Unix(0, 21*time.Minute.Nanoseconds()), // 21 minutes (1260 seconds)
			Limit:     entriesLimit,
			Query:     lblFooBar,
			Direction: logproto.FORWARD,
		}
		respTenant1Forward := nonEmptyResponse(reqTenant1Forward, time.Unix(1201, 0), time.Unix(1205, 0), lblFooBar)

		ctx := user.InjectOrgID(context.Background(), "tenant1")
		fakeFirstRequest := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqTenant1Forward,
					Response: respTenant1Forward,
				},
			},
		})
		h := lrc.Wrap(fakeFirstRequest)

		resp, err := h.Do(ctx, reqTenant1Forward)
		require.NoError(t, err)
		require.Equal(t, respTenant1Forward, resp)
		fakeFirstRequest.AssertExpectations(t)

		// Second request with BACKWARD direction (same time range, limit, query) should not hit cache
		reqTenant1Backward := &LokiRequest{
			StartTs:   time.Unix(0, 20*time.Minute.Nanoseconds()), // Same time range
			EndTs:     time.Unix(0, 21*time.Minute.Nanoseconds()), // Same time range
			Limit:     entriesLimit,                               // Same limit
			Query:     lblFooBar,                                  // Same query
			Direction: logproto.BACKWARD,                          // Different direction
		}
		respTenant1Backward := nonEmptyResponse(reqTenant1Backward, time.Unix(1201, 0), time.Unix(1205, 0), lblFooBar)

		// Should call the handler (cache miss due to different direction)
		fakeSecondRequest := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqTenant1Backward,
					Response: respTenant1Backward,
				},
			},
		})
		h = lrc.Wrap(fakeSecondRequest)

		resp, err = h.Do(ctx, reqTenant1Backward)
		require.NoError(t, err)
		require.Equal(t, respTenant1Backward, resp)
		fakeSecondRequest.AssertExpectations(t)

		// Third request with BACKWARD direction again should hit cache
		fakeThirdRequest := newFakeResponse([]mockResponse{})
		h = lrc.Wrap(fakeThirdRequest)

		resp, err = h.Do(ctx, reqTenant1Backward)
		require.NoError(t, err)
		require.Equal(t, respTenant1Backward, resp)
		fakeThirdRequest.AssertExpectations(t)
	})
}

func Test_LogResultCacheLimitHittingForward(t *testing.T) {
	// Test the scenario from the bug report:
	// 1. Request A from T60s to T120s with limit 5, FORWARD direction
	// 2. Response contains 5 entries (hits limit) with entries from T61s to T65s
	// 3. Cache should store time range T60s to T65s (not T60s to T120s)
	// 4. Request B from T70s to T120s should NOT get empty response from cache (no overlap with cached range)
	// 5. Request C from T60s to T120s should partially hit cache (T60s-T65s) and fetch T65s-T120s

	t.Run("cache miss for non-overlapping range", func(t *testing.T) {
		mockCache := cache.NewMockCache()
		var (
			ctx = user.InjectOrgID(context.Background(), "foo")
			lrc = NewLogResultCache(
				log.NewNopLogger(),
				fakeLimits{
					splitDuration:                       map[string]time.Duration{"foo": time.Minute},
					logResultCacheStoreNonEmptyResponse: map[string]bool{"foo": true},
				},
				mockCache,
				nil,
				nil,
				nil,
			)
		)

		// Request A: T60s to T120s with limit 5, FORWARD
		reqA := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()),   // T60s
			EndTs:     time.Unix(0, 2*time.Minute.Nanoseconds()), // T120s
			Limit:     5,
			Direction: logproto.FORWARD,
			Query:     lblFooBar,
		}

		// Response A: 5 entries from T61s to T65s (hits limit)
		respA := nonEmptyResponse(reqA, time.Unix(61, 0), time.Unix(65, 0), lblFooBar)

		fake := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqA,
					Response: respA,
				},
			},
		})

		h := lrc.Wrap(fake)

		// First request - should cache with adjusted time range (T60s to T65s, not T60s to T120s)
		resp, err := h.Do(ctx, reqA)
		require.NoError(t, err)
		require.Equal(t, respA, resp)
		fake.AssertExpectations(t)

		// Request B: T70s to T120s with same limit and direction
		// This should NOT hit cache because cached range is T60s to T65s (no overlap with T70s-T120s)
		reqB := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()+10*time.Second.Nanoseconds()), // T70s
			EndTs:     time.Unix(0, 2*time.Minute.Nanoseconds()),                            // T120s
			Limit:     5,
			Direction: logproto.FORWARD,
			Query:     lblFooBar,
		}

		// Response B: real response for this range
		respB := nonEmptyResponse(reqB, time.Unix(75, 0), time.Unix(79, 0), lblFooBar)

		fake2 := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqB,
					Response: respB,
				},
			},
		})

		h2 := lrc.Wrap(fake2)

		// Second request - should call handler (cache miss because range not covered)
		resp2, err := h2.Do(ctx, reqB)
		require.NoError(t, err)
		require.Equal(t, respB, resp2)
		fake2.AssertExpectations(t)
	})

	t.Run("partial cache hit for overlapping range", func(t *testing.T) {
		mockCache := cache.NewMockCache()
		var (
			ctx = user.InjectOrgID(context.Background(), "foo")
			lrc = NewLogResultCache(
				log.NewNopLogger(),
				fakeLimits{
					splitDuration:                       map[string]time.Duration{"foo": time.Minute},
					logResultCacheStoreNonEmptyResponse: map[string]bool{"foo": true},
				},
				mockCache,
				nil,
				nil,
				nil,
			)
		)

		// Request A: T60s to T120s with limit 5, FORWARD
		reqA := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()),   // T60s
			EndTs:     time.Unix(0, 2*time.Minute.Nanoseconds()), // T120s
			Limit:     5,
			Direction: logproto.FORWARD,
			Query:     lblFooBar,
		}

		// Response A: 5 entries from T61s to T65s (hits limit)
		respA := nonEmptyResponse(reqA, time.Unix(61, 0), time.Unix(65, 0), lblFooBar)

		// The cache will store adjusted range [T60s, T65s] (based on max entry timestamp)
		// When reqC comes in for [T60s, T120s], cache will request missing part [T65s, T120s]
		reqCMissingPart := reqA.WithStartEnd(time.Unix(65, 0), time.Unix(0, 2*time.Minute.Nanoseconds())).(*LokiRequest)

		// Response for missing part: entries from T80s to T84s (5 entries, which will be ignored after the merge since the cached entries are already 5)
		respCMissingPart := nonEmptyResponse(reqCMissingPart, time.Unix(80, 0), time.Unix(84, 0), lblFooBar)

		fake := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqA,
					Response: respA,
				},
			},
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqCMissingPart,
					Response: respCMissingPart,
				},
			},
		})

		h := lrc.Wrap(fake)

		// First request - should cache with adjusted time range (T60s to T65s, not T60s to T120s)
		resp, err := h.Do(ctx, reqA)
		require.NoError(t, err)
		require.Equal(t, respA, resp)

		// Request C: T60s to T120s again (same as reqA)
		// This should partially hit cache for T60s to T65s, and make a call for T65s to T120s
		reqC := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()),   // T60s
			EndTs:     time.Unix(0, 2*time.Minute.Nanoseconds()), // T120s
			Limit:     5,
			Direction: logproto.FORWARD,
			Query:     lblFooBar,
		}

		// Second request - should hit cache partially (T60s-T65s) and fetch T65s-T120s
		resp2, err := h.Do(ctx, reqC)
		require.NoError(t, err)

		// Verify the response contains entries
		lokiResp2 := resp2.(*LokiResponse)
		require.Equal(t, loghttp.QueryStatusSuccess, lokiResp2.Status)

		// Collect all entry timestamps
		var allTimestamps []int64
		for _, stream := range lokiResp2.Data.Result {
			for _, entry := range stream.Entries {
				allTimestamps = append(allTimestamps, entry.Timestamp.Unix())
			}
		}

		// Verify the merge happened correctly:
		// - Cached entries (T61s-T64s): T65s is excluded because extractLokiResponse uses [start, end) interval
		//   The cached range ends at T65s (max entry timestamp), so extraction excludes entries at exactly T65s
		// - Plus entries from fetched missing part (starting at T80s)
		// - Limited to 5 entries (the request limit) via mergeLokiResponse
		require.Len(t, allTimestamps, 5, "Should have exactly 5 entries (the limit)")

		// Verify we have entries from BOTH the cached portion AND the fetched portion
		// This proves the cache correctly stored the adjusted range and fetched missing data
		hasCachedEntry := false
		hasFetchedEntry := false
		for _, ts := range allTimestamps {
			if ts >= 61 && ts <= 64 { // Cached entries
				hasCachedEntry = true
			}
			if ts >= 80 { // Fetched entries from missing part
				hasFetchedEntry = true
			}
		}
		require.True(t, hasCachedEntry, "Response should include cached entries")
		require.True(t, hasFetchedEntry, "Response should include entries from fetched missing part")

		// Verify mock expectations (both requests were made with correct parameters)
		fake.AssertExpectations(t)
	})

	t.Run("cache extension with under-limit responses", func(t *testing.T) {
		// Test scenario:
		// 1. Request A with limit 10, from T60s to T70s, returns 5 entries from T60s to T64s
		//    (no entries between T65s-T70s, that's why only 5 were returned)
		// 2. Response is cached from T60s to T70s (full request range, since response is under limit)
		//    When under limit, we cache the full request range because we know there are no more entries
		// 3. Request B with limit 10, from T62s to T80s
		// 4. Should hit cache from T60s to T70s and extract T62s to T70s (entries T62s-T64s)
		// 5. Should complete with subrequest T71s to T80s, returning 8 entries from T71s to T78s
		// 6. Should extend cache from T60s to T78s with 13 entries total (5 from A + 8 from B)
		// 7. Should return first 10 entries due to limit

		mockCache := cache.NewMockCache()
		var (
			ctx = user.InjectOrgID(context.Background(), "foo")
			lrc = NewLogResultCache(
				log.NewNopLogger(),
				fakeLimits{
					splitDuration:                       map[string]time.Duration{"foo": time.Minute},
					logResultCacheStoreNonEmptyResponse: map[string]bool{"foo": true},
				},
				mockCache,
				nil,
				nil,
				nil,
			)
		)

		// Request A: T60s to T70s with limit 10, FORWARD
		reqA := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()),                              // T60s
			EndTs:     time.Unix(0, time.Minute.Nanoseconds()+10*time.Second.Nanoseconds()), // T70s
			Limit:     10,
			Direction: logproto.FORWARD,
			Query:     lblFooBar,
		}

		// Response A: 5 entries from T60s to T64s (under limit of 10, no entries in T65s-T70s)
		respA := nonEmptyResponse(reqA, time.Unix(60, 0), time.Unix(64, 0), lblFooBar)

		// Request B: T62s to T80s with limit 10, FORWARD
		reqB := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()+2*time.Second.Nanoseconds()),  // T62s
			EndTs:     time.Unix(0, time.Minute.Nanoseconds()+20*time.Second.Nanoseconds()), // T80s
			Limit:     10,
			Direction: logproto.FORWARD,
			Query:     lblFooBar,
		}

		// The cache stores T60s to T70s (full request range since under limit)
		// Request B will need to fetch missing part from T70s to T80s
		// (cached range is [T60s, T70s) with exclusive end, so missing part starts at T70s)
		reqBMissingPart := reqB.WithStartEnd(
			time.Unix(0, time.Minute.Nanoseconds()+10*time.Second.Nanoseconds()), // T70s
			time.Unix(0, time.Minute.Nanoseconds()+20*time.Second.Nanoseconds()), // T80s
		).(*LokiRequest)

		// Response for missing part: 8 entries from T71s to T78s
		respBMissingPart := nonEmptyResponse(reqBMissingPart, time.Unix(71, 0), time.Unix(78, 0), lblFooBar)

		fake := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqA,
					Response: respA,
				},
			},
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqBMissingPart,
					Response: respBMissingPart,
				},
			},
		})

		h := lrc.Wrap(fake)

		// First request - populates cache with T60s to T70s (full request range since under limit)
		resp, err := h.Do(ctx, reqA)
		require.NoError(t, err)
		require.Equal(t, respA, resp)

		// Second request - should hit cache for T62s-T70s and fetch T70s-T80s
		resp2, err := h.Do(ctx, reqB)
		require.NoError(t, err)

		// Verify the response
		lokiResp2 := resp2.(*LokiResponse)
		require.Equal(t, loghttp.QueryStatusSuccess, lokiResp2.Status)

		// Count entries - should have limit of 10
		var totalEntries int
		for _, stream := range lokiResp2.Data.Result {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 10, totalEntries, "Should have exactly 10 entries (the limit)")

		// Third request - reqA again, should hit cache completely (no subquery)
		// Cache should now have T60s to T80s (extended by reqB's response)
		resp3, err := h.Do(ctx, reqA)
		require.NoError(t, err)
		lokiResp3 := resp3.(*LokiResponse)
		require.Equal(t, loghttp.QueryStatusSuccess, lokiResp3.Status)

		// Fourth request - reqB again, should hit cache completely (no subquery)
		resp4, err := h.Do(ctx, reqB)
		require.NoError(t, err)
		lokiResp4 := resp4.(*LokiResponse)
		require.Equal(t, loghttp.QueryStatusSuccess, lokiResp4.Status)

		// Verify mock expectations so far - should be 2 calls (reqA and reqBMissingPart)
		fake.AssertNumberOfCalls(t, "Do", 2)

		// Request C: T79s to T90s with limit 10, FORWARD
		// Cache has T60s-T80s with entries at T60-T64 and T71-T78
		// T79s-T80s is in cache but has no entries
		reqC := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()+19*time.Second.Nanoseconds()), // T79s
			EndTs:     time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()), // T90s
			Limit:     10,
			Direction: logproto.FORWARD,
			Query:     lblFooBar,
		}

		// The cache covers T60s to T80s, so missing part is T80s to T90s
		reqCMissingPart := reqC.WithStartEnd(
			time.Unix(0, time.Minute.Nanoseconds()+20*time.Second.Nanoseconds()), // T80s
			time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()), // T90s
		).(*LokiRequest)

		// Response for missing part: empty (no entries from T80s to T90s)
		respCMissingPart := emptyResponse(reqCMissingPart)

		// Add mock expectation for reqCMissingPart
		fake.On("Do", mock.Anything, reqCMissingPart).Return(respCMissingPart, nil)

		// Fifth request - reqC, should hit cache for T79s-T80s (no entries) and fetch T80s-T90s (empty)
		resp5, err := h.Do(ctx, reqC)
		require.NoError(t, err)
		lokiResp5 := resp5.(*LokiResponse)
		require.Equal(t, loghttp.QueryStatusSuccess, lokiResp5.Status)

		// Verify mock expectations - should now be 3 calls
		fake.AssertNumberOfCalls(t, "Do", 3)

		// Sixth request - reqC again, should hit cache completely (no subquery)
		// Cache should now be extended to T60s-T90s
		resp6, err := h.Do(ctx, reqC)
		require.NoError(t, err)
		lokiResp6 := resp6.(*LokiResponse)
		require.Equal(t, loghttp.QueryStatusSuccess, lokiResp6.Status)

		// Verify mock expectations - should still be 3 calls (reqC again should be served from cache)
		fake.AssertExpectations(t)
		fake.AssertNumberOfCalls(t, "Do", 3)
	})
}

func Test_LogResultCacheLimitHittingBackward(t *testing.T) {
	// Test BACKWARD direction limit hitting:
	// 1. Request A from T60s to T120s with limit 5, BACKWARD direction
	// 2. Response contains 5 entries (hits limit) with entries from T115s to T119s
	// 3. Cache should store time range T115s to T120s (not T60s to T120s)
	// 4. Request B from T60s to T110s should NOT get empty response from cache (no overlap with cached range)
	// 5. Request C from T60s to T120s should partially hit cache (T115s-T120s) and fetch T60s-T115s

	t.Run("cache miss for non-overlapping range", func(t *testing.T) {
		mockCache := cache.NewMockCache()
		var (
			ctx = user.InjectOrgID(context.Background(), "foo")
			lrc = NewLogResultCache(
				log.NewNopLogger(),
				fakeLimits{
					splitDuration:                       map[string]time.Duration{"foo": time.Minute},
					logResultCacheStoreNonEmptyResponse: map[string]bool{"foo": true},
				},
				mockCache,
				nil,
				nil,
				nil,
			)
		)

		// Request A: T60s to T120s with limit 5, BACKWARD
		reqA := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()),   // T60s
			EndTs:     time.Unix(0, 2*time.Minute.Nanoseconds()), // T120s
			Limit:     5,
			Direction: logproto.BACKWARD,
			Query:     lblFooBar,
		}

		// Response A: 5 entries from T115s to T119s (hits limit, scanning backward)
		respA := nonEmptyResponse(reqA, time.Unix(115, 0), time.Unix(119, 0), lblFooBar)

		fake := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqA,
					Response: respA,
				},
			},
		})

		h := lrc.Wrap(fake)

		// First request - should cache with adjusted time range (T115s to T120s, not T60s to T120s)
		resp, err := h.Do(ctx, reqA)
		require.NoError(t, err)
		require.Equal(t, respA, resp)
		fake.AssertExpectations(t)

		// Request B: T60s to T110s with same limit and direction
		// This should NOT hit cache because cached range is T115s to T120s (no overlap with T60s-T110s)
		reqB := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()),                              // T60s
			EndTs:     time.Unix(0, time.Minute.Nanoseconds()+50*time.Second.Nanoseconds()), // T110s
			Limit:     5,
			Direction: logproto.BACKWARD,
			Query:     lblFooBar,
		}

		// Response B: real response for this range (not empty from bad cache)
		respB := nonEmptyResponse(reqB, time.Unix(105, 0), time.Unix(109, 0), lblFooBar)

		fake2 := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqB,
					Response: respB,
				},
			},
		})

		h2 := lrc.Wrap(fake2)

		// Second request - should call handler (cache miss because range not covered)
		resp2, err := h2.Do(ctx, reqB)
		require.NoError(t, err)
		require.Equal(t, respB, resp2)
		fake2.AssertExpectations(t)
	})

	t.Run("partial cache hit for overlapping range", func(t *testing.T) {
		mockCache := cache.NewMockCache()
		var (
			ctx = user.InjectOrgID(context.Background(), "foo")
			lrc = NewLogResultCache(
				log.NewNopLogger(),
				fakeLimits{
					splitDuration:                       map[string]time.Duration{"foo": time.Minute},
					logResultCacheStoreNonEmptyResponse: map[string]bool{"foo": true},
				},
				mockCache,
				nil,
				nil,
				nil,
			)
		)

		// Request A: T60s to T120s with limit 5, BACKWARD
		reqA := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()),   // T60s
			EndTs:     time.Unix(0, 2*time.Minute.Nanoseconds()), // T120s
			Limit:     5,
			Direction: logproto.BACKWARD,
			Query:     lblFooBar,
		}

		// Response A: 5 entries from T115s to T119s (hits limit, scanning backward)
		respA := nonEmptyResponse(reqA, time.Unix(115, 0), time.Unix(119, 0), lblFooBar)

		// The cache will store adjusted range [T115s, T120s] (based on min entry timestamp for BACKWARD)
		// When reqC comes in for [T60s, T120s], cache will request missing part [T60s, T115s]
		reqCMissingPart := reqA.WithStartEnd(time.Unix(0, time.Minute.Nanoseconds()), time.Unix(115, 0)).(*LokiRequest)

		// Response for missing part: entries from T80s to T84s (5 entries)
		respCMissingPart := nonEmptyResponse(reqCMissingPart, time.Unix(80, 0), time.Unix(84, 0), lblFooBar)

		fake := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqA,
					Response: respA,
				},
			},
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqCMissingPart,
					Response: respCMissingPart,
				},
			},
		})

		h := lrc.Wrap(fake)

		// First request - should cache with adjusted time range (T115s to T120s, not T60s to T120s)
		resp, err := h.Do(ctx, reqA)
		require.NoError(t, err)
		require.Equal(t, respA, resp)

		// Request C: T60s to T120s again (same as reqA)
		// This should partially hit cache for T115s to T120s, and make a call for T60s to T115s
		reqC := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()),   // T60s
			EndTs:     time.Unix(0, 2*time.Minute.Nanoseconds()), // T120s
			Limit:     5,
			Direction: logproto.BACKWARD,
			Query:     lblFooBar,
		}

		// Second request - should hit cache partially (T115s-T120s) and fetch T60s-T115s
		resp2, err := h.Do(ctx, reqC)
		require.NoError(t, err)

		// Verify the response contains entries
		lokiResp2 := resp2.(*LokiResponse)
		require.Equal(t, loghttp.QueryStatusSuccess, lokiResp2.Status)

		// Verify the response has entries (the merge logic handles ordering by direction)
		var totalEntries int
		for _, stream := range lokiResp2.Data.Result {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 5, totalEntries, "Should have exactly 5 entries (the limit)")

		// Verify mock expectations - both requests were made with correct parameters:
		// 1. First call for reqA - populates cache with adjusted range [T115s, T120s]
		// 2. Second call for missing part [T60s, T115s] - proves cache correctly identified missing data
		// This confirms the cache is storing the adjusted time range for BACKWARD queries
		fake.AssertExpectations(t)
	})

	t.Run("cache extension with under-limit responses", func(t *testing.T) {
		// Test scenario (BACKWARD direction):
		// 1. Request A with limit 10, from T110s to T120s, returns 5 entries from T116s to T120s
		//    (no entries between T110s-T115s, that's why only 5 were returned)
		// 2. Response is cached from T110s to T120s (full request range, since response is under limit)
		//    When under limit, we cache the full request range because we know there are no more entries
		// 3. Request B with limit 10, from T100s to T118s
		// 4. Should hit cache from T110s to T118s and extract entries T116s-T118s (3 entries)
		// 5. Should complete with subrequest T100s to T110s, returning 8 entries from T102s to T109s
		// 6. Should extend cache from T100s to T120s with 13 entries total (5 from A + 8 from B)
		// 7. Should return first 10 entries due to limit (BACKWARD returns newest first)

		mockCache := cache.NewMockCache()
		var (
			ctx = user.InjectOrgID(context.Background(), "foo")
			lrc = NewLogResultCache(
				log.NewNopLogger(),
				fakeLimits{
					splitDuration:                       map[string]time.Duration{"foo": time.Minute},
					logResultCacheStoreNonEmptyResponse: map[string]bool{"foo": true},
				},
				mockCache,
				nil,
				nil,
				nil,
			)
		)

		// Request A: T110s to T120s with limit 10, BACKWARD
		reqA := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()+50*time.Second.Nanoseconds()), // T110s
			EndTs:     time.Unix(0, 2*time.Minute.Nanoseconds()),                            // T120s
			Limit:     10,
			Direction: logproto.BACKWARD,
			Query:     lblFooBar,
		}

		// Response A: 5 entries from T116s to T120s (under limit of 10, no entries in T110s-T115s)
		respA := nonEmptyResponse(reqA, time.Unix(116, 0), time.Unix(120, 0), lblFooBar)

		// Request B: T100s to T118s with limit 10, BACKWARD
		reqB := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()+40*time.Second.Nanoseconds()), // T100s
			EndTs:     time.Unix(0, time.Minute.Nanoseconds()+58*time.Second.Nanoseconds()), // T118s
			Limit:     10,
			Direction: logproto.BACKWARD,
			Query:     lblFooBar,
		}

		// The cache stores T110s to T120s (full request range since under limit)
		// Request B will need to fetch missing part from T100s to T110s
		// (cached range starts at T110s, so missing part is before that)
		reqBMissingPart := reqB.WithStartEnd(
			time.Unix(0, time.Minute.Nanoseconds()+40*time.Second.Nanoseconds()), // T100s
			time.Unix(0, time.Minute.Nanoseconds()+50*time.Second.Nanoseconds()), // T110s
		).(*LokiRequest)

		// Response for missing part: 8 entries from T102s to T109s
		respBMissingPart := nonEmptyResponse(reqBMissingPart, time.Unix(102, 0), time.Unix(109, 0), lblFooBar)

		fake := newFakeResponse([]mockResponse{
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqA,
					Response: respA,
				},
			},
			{
				RequestResponse: queryrangebase.RequestResponse{
					Request:  reqBMissingPart,
					Response: respBMissingPart,
				},
			},
		})

		h := lrc.Wrap(fake)

		// First request - populates cache with T110s to T120s (full request range since under limit)
		resp, err := h.Do(ctx, reqA)
		require.NoError(t, err)
		require.Equal(t, respA, resp)

		// Second request - should hit cache for T110s-T118s and fetch T100s-T110s
		resp2, err := h.Do(ctx, reqB)
		require.NoError(t, err)

		// Verify the response
		lokiResp2 := resp2.(*LokiResponse)
		require.Equal(t, loghttp.QueryStatusSuccess, lokiResp2.Status)

		// Count entries - should have limit of 10
		var totalEntries int
		for _, stream := range lokiResp2.Data.Result {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 10, totalEntries, "Should have exactly 10 entries (the limit)")

		// Verify mock expectations so far - should be 2 calls (reqA and reqBMissingPart)
		fake.AssertNumberOfCalls(t, "Do", 2)

		// Third request - reqA again, should hit cache completely (no subquery)
		// Cache should now have T100s to T120s (extended by reqB's response)
		resp3, err := h.Do(ctx, reqA)
		require.NoError(t, err)
		lokiResp3 := resp3.(*LokiResponse)
		require.Equal(t, loghttp.QueryStatusSuccess, lokiResp3.Status)

		// Fourth request - reqB again, should hit cache completely (no subquery)
		resp4, err := h.Do(ctx, reqB)
		require.NoError(t, err)
		lokiResp4 := resp4.(*LokiResponse)
		require.Equal(t, loghttp.QueryStatusSuccess, lokiResp4.Status)

		// Verify mock expectations - should still be 2 calls
		fake.AssertNumberOfCalls(t, "Do", 2)

		// Request C: T90s to T105s with limit 10, BACKWARD
		// Cache has T100s-T120s with entries at T102-T109 and T116-T120
		// T100s-T105s is partially in cache (T100s-T105s has entries T102-T105)
		reqC := &LokiRequest{
			StartTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()), // T90s
			EndTs:     time.Unix(0, time.Minute.Nanoseconds()+45*time.Second.Nanoseconds()), // T105s
			Limit:     10,
			Direction: logproto.BACKWARD,
			Query:     lblFooBar,
		}

		// The cache covers T100s to T120s, so missing part is T90s to T100s
		reqCMissingPart := reqC.WithStartEnd(
			time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()), // T90s
			time.Unix(0, time.Minute.Nanoseconds()+40*time.Second.Nanoseconds()), // T100s
		).(*LokiRequest)

		// Response for missing part: empty (no entries from T90s to T100s)
		respCMissingPart := emptyResponse(reqCMissingPart)

		// Add mock expectation for reqCMissingPart
		fake.On("Do", mock.Anything, reqCMissingPart).Return(respCMissingPart, nil)

		// Fifth request - reqC, should hit cache for T100s-T105s and fetch T90s-T100s (empty)
		resp5, err := h.Do(ctx, reqC)
		require.NoError(t, err)
		lokiResp5 := resp5.(*LokiResponse)
		require.Equal(t, loghttp.QueryStatusSuccess, lokiResp5.Status)

		// Verify mock expectations - should now be 3 calls
		fake.AssertNumberOfCalls(t, "Do", 3)

		// Sixth request - reqC again, should hit cache completely (no subquery)
		// Cache should now be extended to T90s-T120s
		resp6, err := h.Do(ctx, reqC)
		require.NoError(t, err)
		lokiResp6 := resp6.(*LokiResponse)
		require.Equal(t, loghttp.QueryStatusSuccess, lokiResp6.Status)

		// Verify mock expectations - should still be 3 calls (reqC again should be served from cache)
		fake.AssertExpectations(t)
		fake.AssertNumberOfCalls(t, "Do", 3)
	})
}

type fakeResponse struct {
	*mock.Mock
}

type mockResponse struct {
	queryrangebase.RequestResponse
	err error
}

func newFakeResponse(responses []mockResponse) fakeResponse {
	m := &mock.Mock{}
	for _, r := range responses {
		m.On("Do", mock.Anything, r.Request).Return(r.Response, r.err).Once()
	}

	return fakeResponse{
		Mock: m,
	}
}

func (f fakeResponse) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	var (
		resp queryrangebase.Response
		err  error
		args = f.Called(ctx, r)
	)
	if args.Get(0) != nil {
		resp = args.Get(0).(queryrangebase.Response)
	}
	if args.Get(1) != nil {
		err = args.Get(1).(error)
	}
	return resp, err
}

// nonEmptyResponse builds a response from [start, end] with 1s step.
func nonEmptyResponse(lokiReq *LokiRequest, start, end time.Time, labels string) *LokiResponse {
	r := &LokiResponse{
		Status:     loghttp.QueryStatusSuccess,
		Statistics: stats.Result{},
		Direction:  lokiReq.Direction,
		Limit:      lokiReq.Limit,
		Version:    uint32(loghttp.GetVersion(lokiReq.Path)),
		Data: LokiData{
			ResultType: loghttp.ResultTypeStream,
			Result: []logproto.Stream{
				{
					Labels: labels,
				},
			},
		},
	}

	if lokiReq.Direction == logproto.BACKWARD {
		// For BACKWARD direction, entries are sorted newest to oldest
		for ts := end; !ts.Before(start); ts = ts.Add(-time.Second) {
			r.Data.Result[0].Entries = append(r.Data.Result[0].Entries, logproto.Entry{
				Timestamp: ts,
				Line:      fmt.Sprintf("%d", ts.Unix()),
			})
		}
	} else {
		// For FORWARD direction, entries are sorted oldest to newest
		for ts := start; !ts.After(end); ts = ts.Add(time.Second) {
			r.Data.Result[0].Entries = append(r.Data.Result[0].Entries, logproto.Entry{
				Timestamp: ts,
				Line:      fmt.Sprintf("%d", ts.Unix()),
			})
		}
	}
	return r
}
