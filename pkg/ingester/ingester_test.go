package ingester

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util/validation"
)

func TestIngester(t *testing.T) {
	ingesterConfig := defaultIngesterTestConfig(t)
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}

	i, err := New(ingesterConfig, client.Config{}, store, limits)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar",bar="baz1"}`,
			},
			{
				Labels: `{foo="bar",bar="baz2"}`,
			},
		},
	}
	for i := 0; i < 10; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
		req.Streams[1].Entries = append(req.Streams[1].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	result := mockQuerierServer{
		ctx: ctx,
	}
	err = i.Query(&logproto.QueryRequest{
		Selector: `{foo="bar"}`,
		Limit:    100,
		Start:    time.Unix(0, 0),
		End:      time.Unix(1, 0),
	}, &result)
	require.NoError(t, err)
	require.Len(t, result.resps, 1)
	require.Len(t, result.resps[0].Streams, 2)

	result = mockQuerierServer{
		ctx: ctx,
	}
	err = i.Query(&logproto.QueryRequest{
		Selector: `{foo="bar",bar="baz1"}`,
		Limit:    100,
		Start:    time.Unix(0, 0),
		End:      time.Unix(1, 0),
	}, &result)
	require.NoError(t, err)
	require.Len(t, result.resps, 1)
	require.Len(t, result.resps[0].Streams, 1)
	require.Equal(t, `{bar="baz1", foo="bar"}`, result.resps[0].Streams[0].Labels)

	result = mockQuerierServer{
		ctx: ctx,
	}
	err = i.Query(&logproto.QueryRequest{
		Selector: `{foo="bar",bar="baz2"}`,
		Limit:    100,
		Start:    time.Unix(0, 0),
		End:      time.Unix(1, 0),
	}, &result)
	require.NoError(t, err)
	require.Len(t, result.resps, 1)
	require.Len(t, result.resps[0].Streams, 1)
	require.Equal(t, `{bar="baz2", foo="bar"}`, result.resps[0].Streams[0].Labels)

	// Series

	// empty matchers
	_, err = i.Series(ctx, &logproto.SeriesRequest{
		Start: time.Unix(0, 0),
		End:   time.Unix(1, 0),
	})
	require.Error(t, err)

	// wrong matchers fmt
	_, err = i.Series(ctx, &logproto.SeriesRequest{
		Start:  time.Unix(0, 0),
		End:    time.Unix(1, 0),
		Groups: []string{`{a="b`},
	})
	require.Error(t, err)

	// no selectors
	_, err = i.Series(ctx, &logproto.SeriesRequest{
		Start:  time.Unix(0, 0),
		End:    time.Unix(1, 0),
		Groups: []string{`{foo="bar"}`, `{}`},
	})
	require.Error(t, err)

	// foo=bar
	resp, err := i.Series(ctx, &logproto.SeriesRequest{
		Start:  time.Unix(0, 0),
		End:    time.Unix(1, 0),
		Groups: []string{`{foo="bar"}`},
	})
	require.Nil(t, err)
	require.ElementsMatch(t, []logproto.SeriesIdentifier{
		{
			Labels: map[string]string{
				"foo": "bar",
				"bar": "baz1",
			},
		},
		{
			Labels: map[string]string{
				"foo": "bar",
				"bar": "baz2",
			},
		},
	}, resp.GetSeries())

	// foo=bar, bar=~"baz[2-9]"
	resp, err = i.Series(ctx, &logproto.SeriesRequest{
		Start:  time.Unix(0, 0),
		End:    time.Unix(1, 0),
		Groups: []string{`{foo="bar", bar=~"baz[2-9]"}`},
	})
	require.Nil(t, err)
	require.ElementsMatch(t, []logproto.SeriesIdentifier{
		{
			Labels: map[string]string{
				"foo": "bar",
				"bar": "baz2",
			},
		},
	}, resp.GetSeries())

	// foo=bar, bar=~"baz[2-9]" in different groups should OR the results
	resp, err = i.Series(ctx, &logproto.SeriesRequest{
		Start:  time.Unix(0, 0),
		End:    time.Unix(1, 0),
		Groups: []string{`{foo="bar"}`, `{bar=~"baz[2-9]"}`},
	})
	require.Nil(t, err)
	require.ElementsMatch(t, []logproto.SeriesIdentifier{
		{
			Labels: map[string]string{
				"foo": "bar",
				"bar": "baz1",
			},
		},
		{
			Labels: map[string]string{
				"foo": "bar",
				"bar": "baz2",
			},
		},
	}, resp.GetSeries())
}

func TestIngesterStreamLimitExceeded(t *testing.T) {
	ingesterConfig := defaultIngesterTestConfig(t)
	defaultLimits := defaultLimitsTestConfig()
	defaultLimits.MaxLocalStreamsPerUser = 1
	overrides, err := validation.NewOverrides(defaultLimits, nil)

	require.NoError(t, err)

	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}

	i, err := New(ingesterConfig, client.Config{}, store, overrides)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar",bar="baz1"}`,
			},
		},
	}
	for i := 0; i < 10; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	req.Streams[0].Labels = `{foo="bar",bar="baz2"}`

	_, err = i.Push(ctx, &req)
	if resp, ok := httpgrpc.HTTPResponseFromError(err); !ok || resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected error about exceeding metrics per user, got %v", err)
	}
}

type mockStore struct {
	mtx    sync.Mutex
	chunks map[string][]chunk.Chunk
}

func (s *mockStore) Put(ctx context.Context, chunks []chunk.Chunk) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	userid, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}

	s.chunks[userid] = append(s.chunks[userid], chunks...)
	return nil
}

func (s *mockStore) LazyQuery(ctx context.Context, req logql.SelectParams) (iter.EntryIterator, error) {
	return nil, nil
}

type mockQuerierServer struct {
	ctx   context.Context
	resps []*logproto.QueryResponse
	grpc.ServerStream
}

func (*mockQuerierServer) SetTrailer(metadata.MD) {}

func (m *mockQuerierServer) Send(resp *logproto.QueryResponse) error {
	m.resps = append(m.resps, resp)
	return nil
}

func (m *mockQuerierServer) Context() context.Context {
	return m.ctx
}

func defaultLimitsTestConfig() validation.Limits {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	return limits
}

func TestIngester_buildStoreRequest(t *testing.T) {
	ingesterQueryRequest := logproto.QueryRequest{
		Selector: `{foo="bar"}`,
		Limit:    100,
	}

	now := time.Now()

	for _, tc := range []struct {
		name                      string
		queryStore                bool
		maxLookBackPeriod         time.Duration
		ingesterQueryRequest      *logproto.QueryRequest
		expectedStoreQueryRequest *logproto.QueryRequest
	}{
		{
			name:                      "do not query store",
			queryStore:                false,
			ingesterQueryRequest:      recreateRequestWithTime(ingesterQueryRequest, now.Add(-time.Minute), now),
			expectedStoreQueryRequest: nil,
		},
		{
			name:                      "query store with max look back covering whole request duration",
			queryStore:                true,
			maxLookBackPeriod:         time.Hour,
			ingesterQueryRequest:      recreateRequestWithTime(ingesterQueryRequest, now.Add(-10*time.Minute), now),
			expectedStoreQueryRequest: recreateRequestWithTime(ingesterQueryRequest, now.Add(-10*time.Minute), now),
		},
		{
			name:                      "query store with max look back covering partial request duration",
			queryStore:                true,
			maxLookBackPeriod:         time.Hour,
			ingesterQueryRequest:      recreateRequestWithTime(ingesterQueryRequest, now.Add(-2*time.Hour), now),
			expectedStoreQueryRequest: recreateRequestWithTime(ingesterQueryRequest, now.Add(-time.Hour), now),
		},
		{
			name:                      "query store with max look back not covering request duration at all",
			queryStore:                true,
			maxLookBackPeriod:         time.Hour,
			ingesterQueryRequest:      recreateRequestWithTime(ingesterQueryRequest, now.Add(-4*time.Hour), now.Add(-2*time.Hour)),
			expectedStoreQueryRequest: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ingesterConfig := defaultIngesterTestConfig(t)
			ingesterConfig.QueryStore = tc.queryStore
			ingesterConfig.QueryStoreMaxLookBackPeriod = tc.maxLookBackPeriod
			storeRequest := buildStoreRequest(ingesterConfig, tc.ingesterQueryRequest)
			if tc.expectedStoreQueryRequest == nil {
				require.Nil(t, storeRequest)
				return
			}

			// because start time of store could be changed and built based on time when function is called we can't predict expected start time.
			// So allowing upto 1s difference between expected and actual start time of store query request.
			require.Equal(t, tc.expectedStoreQueryRequest.Selector, storeRequest.Selector)
			require.Equal(t, tc.expectedStoreQueryRequest.Limit, storeRequest.Limit)
			require.Equal(t, tc.expectedStoreQueryRequest.End, storeRequest.End)

			if storeRequest.Start.Sub(tc.expectedStoreQueryRequest.Start) > time.Second {
				t.Fatalf("expected upto 1s difference in expected and actual store request end time but got %d", storeRequest.End.Sub(tc.expectedStoreQueryRequest.End))
			}
		})
	}
}

func recreateRequestWithTime(req logproto.QueryRequest, start, end time.Time) *logproto.QueryRequest {
	newReq := req
	newReq.Start = start
	newReq.End = end

	return &newReq
}
