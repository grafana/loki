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
				Labels: `{foo="bar"}`,
			},
		},
	}
	for i := 0; i < 10; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	_, err = i.Push(user.InjectOrgID(context.Background(), "test"), &req)
	require.NoError(t, err)
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
