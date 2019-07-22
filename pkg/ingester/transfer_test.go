package ingester

import (
	"fmt"
	"io"
	"io/ioutil"
	"sort"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"golang.org/x/net/context"
)

func TestTransferOut(t *testing.T) {
	f := newTestIngesterFactory(t)

	ing := f.getIngester(time.Duration(0))

	// Push some data into our original ingester
	ctx := user.InjectOrgID(context.Background(), "test")
	_, err := ing.Push(ctx, &logproto.PushRequest{
		Streams: []*logproto.Stream{
			{
				Entries: []logproto.Entry{
					{Line: "line 0", Timestamp: time.Unix(0, 0)},
					{Line: "line 1", Timestamp: time.Unix(1, 0)},
				},
				Labels: `{foo="bar",bar="baz1"}`,
			},
			{
				Entries: []logproto.Entry{
					{Line: "line 2", Timestamp: time.Unix(2, 0)},
					{Line: "line 3", Timestamp: time.Unix(3, 0)},
				},
				Labels: `{foo="bar",bar="baz2"}`,
			},
		},
	})
	require.NoError(t, err)

	assert.Len(t, ing.instances, 1)
	if assert.Contains(t, ing.instances, "test") {
		assert.Len(t, ing.instances["test"].streams, 2)
	}

	// Create a new ingester and trasfer data to it
	ing2 := f.getIngester(time.Second * 60)
	ing.Shutdown()

	assert.Len(t, ing2.instances, 1)
	if assert.Contains(t, ing2.instances, "test") {
		assert.Len(t, ing2.instances["test"].streams, 2)

		lines := []string{}

		// Get all the lines back and make sure the blocks transferred successfully
		for _, stream := range ing2.instances["test"].streams {
			it, err := stream.Iterator(
				time.Unix(0, 0),
				time.Unix(10, 0),
				logproto.FORWARD,
				func([]byte) bool { return true },
			)
			if !assert.NoError(t, err) {
				continue
			}

			for it.Next() {
				entry := it.Entry()
				lines = append(lines, entry.Line)
			}
		}

		sort.Strings(lines)

		assert.Equal(
			t,
			[]string{"line 0", "line 1", "line 2", "line 3"},
			lines,
		)
	}
}

type testIngesterFactory struct {
	t         *testing.T
	store     ring.KVClient
	n         int
	ingesters map[string]*Ingester
}

func newTestIngesterFactory(t *testing.T) *testIngesterFactory {
	return &testIngesterFactory{
		t:         t,
		store:     ring.NewInMemoryKVClient(ring.ProtoCodec{Factory: ring.ProtoDescFactory}),
		ingesters: make(map[string]*Ingester),
	}
}

func (f *testIngesterFactory) getIngester(joinAfter time.Duration) *Ingester {
	f.n++

	cfg := defaultIngesterTestConfig()
	cfg.MaxTransferRetries = 1
	cfg.LifecyclerConfig.ClaimOnRollout = true
	cfg.LifecyclerConfig.ID = fmt.Sprintf("localhost-%d", f.n)
	cfg.LifecyclerConfig.RingConfig.KVStore.Mock = f.store
	cfg.LifecyclerConfig.JoinAfter = joinAfter
	cfg.LifecyclerConfig.Addr = cfg.LifecyclerConfig.ID

	cfg.ingesterClientFactory = func(cfg client.Config, addr string) (grpc_health_v1.HealthClient, error) {
		ingester, ok := f.ingesters[addr]
		if !ok {
			return nil, fmt.Errorf("no ingester %s", addr)
		}

		return struct {
			logproto.PusherClient
			logproto.QuerierClient
			logproto.IngesterClient
			grpc_health_v1.HealthClient
			io.Closer
		}{
			PusherClient:   nil,
			QuerierClient:  nil,
			IngesterClient: &testIngesterClient{t: f.t, i: ingester},
			Closer:         ioutil.NopCloser(nil),
		}, nil
	}

	_, ing := newTestStore(f.t, cfg)
	f.ingesters[fmt.Sprintf("%s:0", cfg.LifecyclerConfig.ID)] = ing

	// NB there's some kind of race condition with the in-memory KV client when
	// we don't give the ingester a little bit of time to initialize. a 100ms
	// wait time seems effective.
	time.Sleep(time.Millisecond * 100)
	return ing
}

type testIngesterClient struct {
	t *testing.T
	i *Ingester
}

func (c *testIngesterClient) TransferChunks(context.Context, ...grpc.CallOption) (logproto.Ingester_TransferChunksClient, error) {
	chunkCh := make(chan *logproto.TimeSeriesChunk)
	respCh := make(chan *logproto.TransferChunksResponse)

	client := &testTransferChunksClient{ch: chunkCh, resp: respCh}
	go func() {
		server := &testTransferChunksServer{ch: chunkCh, resp: respCh}
		err := c.i.TransferChunks(server)
		require.NoError(c.t, err)
	}()

	return client, nil
}

type testTransferChunksClient struct {
	ch   chan *logproto.TimeSeriesChunk
	resp chan *logproto.TransferChunksResponse

	grpc.ClientStream
}

func (c *testTransferChunksClient) Send(chunk *logproto.TimeSeriesChunk) error {
	c.ch <- chunk
	return nil
}

func (c *testTransferChunksClient) CloseAndRecv() (*logproto.TransferChunksResponse, error) {
	close(c.ch)
	resp := <-c.resp
	close(c.resp)
	return resp, nil
}

type testTransferChunksServer struct {
	ch   chan *logproto.TimeSeriesChunk
	resp chan *logproto.TransferChunksResponse

	grpc.ServerStream
}

func (s *testTransferChunksServer) Context() context.Context {
	return context.Background()
}

func (s *testTransferChunksServer) SendAndClose(resp *logproto.TransferChunksResponse) error {
	s.resp <- resp
	return nil
}

func (s *testTransferChunksServer) Recv() (*logproto.TimeSeriesChunk, error) {
	chunk, ok := <-s.ch
	if !ok {
		return nil, io.EOF
	}
	return chunk, nil
}
