package queryrange

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

func Test_LogResultCacheSameRange(t *testing.T) {
	var (
		ctx = user.InjectOrgID(context.Background(), "foo")
		lrc = NewLogResultCache(
			log.NewNopLogger(),
			fakeLimits{
				splits: map[string]time.Duration{"foo": time.Minute},
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
				splits: map[string]time.Duration{"foo": time.Minute},
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
	}

	fake := newFakeResponse([]mockResponse{
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request:  req,
				Response: nonEmptyResponse(req, 1),
			},
		},
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request:  req,
				Response: nonEmptyResponse(req, 2),
			},
		},
	})

	h := lrc.Wrap(fake)

	resp, err := h.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, nonEmptyResponse(req, 1), resp)
	resp, err = h.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, nonEmptyResponse(req, 2), resp)

	fake.AssertExpectations(t)
}

func Test_LogResultCacheSmallerRange(t *testing.T) {
	var (
		ctx = user.InjectOrgID(context.Background(), "foo")
		lrc = NewLogResultCache(
			log.NewNopLogger(),
			fakeLimits{
				splits: map[string]time.Duration{"foo": time.Minute},
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
	})
	require.NoError(t, err)
	require.Equal(t, emptyResponse(&LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
	}), resp)

	fake.AssertExpectations(t)
}

func Test_LogResultCacheDifferentRange(t *testing.T) {
	var (
		ctx = user.InjectOrgID(context.Background(), "foo")
		lrc = NewLogResultCache(
			log.NewNopLogger(),
			fakeLimits{
				splits: map[string]time.Duration{"foo": time.Minute},
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
	}

	req2 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
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
				},
				Response: emptyResponse(&LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
				}),
			},
		},
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
				},
				Response: emptyResponse(&LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
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
				splits: map[string]time.Duration{"foo": time.Minute},
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
	}

	req2 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
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
				},
				Response: nonEmptyResponse(&LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
				}, 1),
			},
		},
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
				},
				Response: nonEmptyResponse(&LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
				}, 2),
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
		}, 2),
		nonEmptyResponse(&LokiRequest{
			StartTs: time.Unix(0, time.Minute.Nanoseconds()),
			EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
		}, 1),
	), resp)

	fake.AssertExpectations(t)
}

func Test_LogResultCacheDifferentRangeNonEmptyAndEmpty(t *testing.T) {
	var (
		ctx = user.InjectOrgID(context.Background(), "foo")
		lrc = NewLogResultCache(
			log.NewNopLogger(),
			fakeLimits{
				splits: map[string]time.Duration{"foo": time.Minute},
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
	}

	req2 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()),
		EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
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
				},
				Response: emptyResponse(&LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
				}),
			},
		},
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
				},
				Response: nonEmptyResponse(&LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
				}, 2),
			},
		},
		// we call it twice
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
				},
				Response: nonEmptyResponse(&LokiRequest{
					StartTs: time.Unix(0, 2*time.Minute.Nanoseconds()-30*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, 2*time.Minute.Nanoseconds()),
				}, 2),
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
		}, 1),
	), resp)
	resp, err = h.Do(ctx, req2)
	require.NoError(t, err)
	require.Equal(t, mergeLokiResponse(
		emptyResponse(req1),
		nonEmptyResponse(&LokiRequest{
			StartTs: time.Unix(0, time.Minute.Nanoseconds()),
			EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
		}, 1),
	), resp)
	fake.AssertExpectations(t)
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

func nonEmptyResponse(lokiReq *LokiRequest, i int) *LokiResponse {
	return &LokiResponse{
		Status:     loghttp.QueryStatusSuccess,
		Statistics: stats.Result{},
		Direction:  lokiReq.Direction,
		Limit:      lokiReq.Limit,
		Version:    uint32(loghttp.GetVersion(lokiReq.Path)),
		Data: LokiData{
			ResultType: loghttp.ResultTypeStream,
			Result: []logproto.Stream{
				{
					Labels: `{foo="bar"}`,
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(1, 0),
							Line:      fmt.Sprintf("%d", i),
						},
					},
				},
			},
		},
	}
}
