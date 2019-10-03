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
	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"golang.org/x/net/context"
)

func TestTransferOut(t *testing.T) {
	f := newTestIngesterFactory(t)

	ing := f.getIngester(time.Duration(0), t)

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

	// verify we get out of order exception on adding an entry with older timestamps
	_, err2 := ing.Push(ctx, &logproto.PushRequest{
		Streams: []*logproto.Stream{
			{
				Entries: []logproto.Entry{
					{Line: "out of order line", Timestamp: time.Unix(0, 0)},
					{Line: "line 4", Timestamp: time.Unix(2, 0)},
				},
				Labels: `{foo="bar",bar="baz1"}`,
			},
		},
	})

	require.Error(t, err2)
	require.Contains(t, err2.Error(), "out of order")
	require.Contains(t, err2.Error(), "total ignored: 1 out of 2")

	// Create a new ingester and transfer data to it
	ing2 := f.getIngester(time.Second*60, t)
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
			[]string{"line 0", "line 1", "line 2", "line 3", "line 4"},
			lines,
		)
	}
}

type testIngesterFactory struct {
	t         *testing.T
	store     kv.Client
	n         int
	ingesters map[string]*Ingester
}

func newTestIngesterFactory(t *testing.T) *testIngesterFactory {
	kvClient, err := kv.NewClient(kv.Config{Store: "inmemory"}, codec.Proto{Factory: ring.ProtoDescFactory})
	require.NoError(t, err)

	return &testIngesterFactory{
		t:         t,
		store:     kvClient,
		ingesters: make(map[string]*Ingester),
	}
}

func (f *testIngesterFactory) getIngester(joinAfter time.Duration, t *testing.T) *Ingester {
	f.n++

	cfg := defaultIngesterTestConfig(t)
	cfg.MaxTransferRetries = 1
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
	waitCh := make(chan bool)

	client := &testTransferChunksClient{ch: chunkCh, resp: respCh, wait: waitCh}
	go func() {
		server := &testTransferChunksServer{ch: chunkCh, resp: respCh}
		err := c.i.TransferChunks(server)
		require.NoError(c.t, err)
	}()

	// After 50ms, we try killing the target ingester's lifecycler to verify
	// that it obtained a lock on the shutdown process. This operation should
	// block until the transfer completes.
	//
	// Then after another 50ms, we also allow data to start sending. This tests an issue
	// where an ingester is shut down before it completes the handoff and ends up in an
	// unhealthy state, permanently stuck in the handler for claiming tokens.
	go func() {
		time.Sleep(time.Millisecond * 50)
		c.i.lifecycler.Shutdown()
	}()

	go func() {
		time.Sleep(time.Millisecond * 100)
		close(waitCh)
	}()

	return client, nil
}

type testTransferChunksClient struct {
	wait chan bool
	ch   chan *logproto.TimeSeriesChunk
	resp chan *logproto.TransferChunksResponse

	grpc.ClientStream
}

func (c *testTransferChunksClient) Send(chunk *logproto.TimeSeriesChunk) error {
	<-c.wait
	c.ch <- chunk
	return nil
}

func (c *testTransferChunksClient) CloseAndRecv() (*logproto.TransferChunksResponse, error) {
	<-c.wait
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
