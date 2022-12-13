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

func Test_LogResultFillingGap(t *testing.T) {
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

	// data requested for just 1 sec, resulting in empty response
	req1 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
		EndTs:   time.Unix(0, time.Minute.Nanoseconds()+31*time.Second.Nanoseconds()),
		Limit:   entriesLimit,
	}

	// data requested for just 1 sec, within the same split but couple seconds apart
	req2 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+35*time.Second.Nanoseconds()),
		EndTs:   time.Unix(0, time.Minute.Nanoseconds()+36*time.Second.Nanoseconds()),
		Limit:   entriesLimit,
	}

	req3 := &LokiRequest{
		StartTs: time.Unix(0, time.Minute.Nanoseconds()+25*time.Second.Nanoseconds()),
		EndTs:   time.Unix(0, time.Minute.Nanoseconds()+26*time.Second.Nanoseconds()),
		Limit:   entriesLimit,
	}

	fake := newFakeResponse([]mockResponse{
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request:  req1,
				Response: emptyResponse(req1),
			},
		},
		// partial request being made for missing interval at the end
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()+31*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+36*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				},
				Response: nonEmptyResponse(&LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()+31*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+36*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				}, time.Unix(31, 0), time.Unix(34, 0), lblFooBar), // data not present for actual query interval i.e req2
			},
		},
		// partial request being made for missing interval at the beginning
		{
			RequestResponse: queryrangebase.RequestResponse{
				Request: &LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()+25*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				},
				Response: nonEmptyResponse(&LokiRequest{
					StartTs: time.Unix(0, time.Minute.Nanoseconds()+25*time.Second.Nanoseconds()),
					EndTs:   time.Unix(0, time.Minute.Nanoseconds()+30*time.Second.Nanoseconds()),
					Limit:   entriesLimit,
				}, time.Unix(27, 0), time.Unix(29, 0), lblFooBar), // data not present for actual query interval i.e req3
			},
		},
	})

	h := lrc.Wrap(fake)

	resp, err := h.Do(ctx, req1)
	require.NoError(t, err)
	require.Equal(t, emptyResponse(req1), resp)

	// although the caching code would request for more data than the actual query, we should have empty response here since we
	// do not have any data for the query we made
	resp, err = h.Do(ctx, req2)
	require.NoError(t, err)
	require.Equal(t, mergeLokiResponse(emptyResponse(req1), emptyResponse(req2)), resp)

	resp, err = h.Do(ctx, req3)
	require.NoError(t, err)
	require.Equal(t, mergeLokiResponse(emptyResponse(req1), emptyResponse(req3)), resp)
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
