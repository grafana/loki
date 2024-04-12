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
				Response: nonEmptyResponse(&LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
					Limit:   entriesLimit,
				}, time.Unix(61, 0), time.Unix(61, 0), lblFooBar),
			},
		},
		// we call it twice
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

	checkCacheMetrics := func(expectedHits, expectedMisses int) {
		require.Equal(t, float64(expectedHits), testutil.ToFloat64(metrics.CacheHit))
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
		resp           *LokiResponse
		extractFrom    time.Time
		extractThrough time.Time
		expectedResp   *LokiResponse
	}{
		{
			name:           "nothing to extract",
			resp:           &LokiResponse{},
			extractFrom:    time.Unix(0, 0),
			extractThrough: time.Unix(0, 1),
			expectedResp: &LokiResponse{
				Data: LokiData{Result: logproto.Streams{}},
			},
		},
		{
			name: "extract interval within response",
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
			name: "extract part of response in the beginning",
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
			name: "extract part of response in the end",
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
			name: "extract interval out of data range",
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedResp, extractLokiResponse(tc.extractFrom, tc.extractThrough, tc.resp))
		})
	}
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
		args = f.Mock.Called(ctx, r)
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

	for ; !start.After(end); start = start.Add(time.Second) {
		r.Data.Result[0].Entries = append(r.Data.Result[0].Entries, logproto.Entry{
			Timestamp: start,
			Line:      fmt.Sprintf("%d", start.Unix()),
		})
	}
	return r
}
