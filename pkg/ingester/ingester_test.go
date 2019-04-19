package ingester

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

func TestIngester(t *testing.T) {
	var ingesterConfig Config
	flagext.DefaultValues(&ingesterConfig)
	ingesterConfig.LifecyclerConfig.RingConfig.Mock = ring.NewInMemoryKVClient()
	ingesterConfig.LifecyclerConfig.NumTokens = 1
	ingesterConfig.LifecyclerConfig.ListenPort = func(i int) *int { return &i }(0)
	ingesterConfig.LifecyclerConfig.Addr = "localhost"
	ingesterConfig.LifecyclerConfig.ID = "localhost"

	store := &mockStore{
		chunks: map[string][]chunk.Chunk{},
	}

	i, err := New(ingesterConfig, store)
	require.NoError(t, err)
	defer i.Shutdown()

	req := logproto.PushRequest{
		Streams: []*logproto.Stream{
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
		Query: `{foo="bar"}`,
		Limit: 100,
		Start: time.Unix(0, 0),
		End:   time.Unix(1, 0),
	}, &result)
	require.NoError(t, err)
	require.Len(t, result.resps, 1)
	require.Len(t, result.resps[0].Streams, 2)

	result = mockQuerierServer{
		ctx: ctx,
	}
	err = i.Query(&logproto.QueryRequest{
		Query: `{foo="bar",bar="baz1"}`,
		Limit: 100,
		Start: time.Unix(0, 0),
		End:   time.Unix(1, 0),
	}, &result)
	require.NoError(t, err)
	require.Len(t, result.resps, 1)
	require.Len(t, result.resps[0].Streams, 1)
	require.Equal(t, `{bar="baz1", foo="bar"}`, result.resps[0].Streams[0].Labels)

	result = mockQuerierServer{
		ctx: ctx,
	}
	err = i.Query(&logproto.QueryRequest{
		Query: `{foo="bar",bar="baz2"}`,
		Limit: 100,
		Start: time.Unix(0, 0),
		End:   time.Unix(1, 0),
	}, &result)
	require.NoError(t, err)
	require.Len(t, result.resps, 1)
	require.Len(t, result.resps[0].Streams, 1)
	require.Equal(t, `{bar="baz2", foo="bar"}`, result.resps[0].Streams[0].Labels)
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

type mockQuerierServer struct {
	ctx   context.Context
	resps []*logproto.QueryResponse
	grpc.ServerStream
}

func (m *mockQuerierServer) Send(resp *logproto.QueryResponse) error {
	m.resps = append(m.resps, resp)
	return nil
}

func (m *mockQuerierServer) Context() context.Context {
	return m.ctx
}
