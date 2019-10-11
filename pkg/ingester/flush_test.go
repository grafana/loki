package ingester

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
	"github.com/cortexproject/cortex/pkg/util/flagext"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util/validation"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
	"golang.org/x/net/context"
)

const (
	numSeries        = 10
	samplesPerSeries = 100
)

func init() {
	//util.Logger = log.NewLogfmtLogger(os.Stdout)
}

func TestChunkFlushingIdle(t *testing.T) {
	cfg := defaultIngesterTestConfig(t)
	cfg.FlushCheckPeriod = 20 * time.Millisecond
	cfg.MaxChunkIdle = 100 * time.Millisecond
	cfg.RetainPeriod = 500 * time.Millisecond

	store, ing := newTestStore(t, cfg)
	userIDs, testData := pushTestSamples(t, ing)

	// wait beyond idle time so samples flush
	time.Sleep(cfg.MaxChunkIdle * 2)
	store.checkData(t, userIDs, testData)
}

func TestChunkFlushingShutdown(t *testing.T) {
	store, ing := newTestStore(t, defaultIngesterTestConfig(t))
	userIDs, testData := pushTestSamples(t, ing)
	ing.Shutdown()
	store.checkData(t, userIDs, testData)
}

type testStore struct {
	mtx sync.Mutex
	// Chunks keyed by userID.
	chunks map[string][]chunk.Chunk
}

func newTestStore(t require.TestingT, cfg Config) (*testStore, *Ingester) {
	store := &testStore{
		chunks: map[string][]chunk.Chunk{},
	}

	limits, err := validation.NewOverrides(defaultLimitsTestConfig())
	require.NoError(t, err)

	ing, err := New(cfg, client.Config{}, store, limits)
	require.NoError(t, err)

	return store, ing
}

// nolint
func defaultIngesterTestConfig(t *testing.T) Config {
	kvClient, err := kv.NewClient(kv.Config{Store: "inmemory"}, codec.Proto{Factory: ring.ProtoDescFactory})
	require.NoError(t, err)

	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.FlushCheckPeriod = 99999 * time.Hour
	cfg.MaxChunkIdle = 99999 * time.Hour
	cfg.ConcurrentFlushes = 1
	cfg.LifecyclerConfig.RingConfig.KVStore.Mock = kvClient
	cfg.LifecyclerConfig.NumTokens = 1
	cfg.LifecyclerConfig.ListenPort = func(i int) *int { return &i }(0)
	cfg.LifecyclerConfig.Addr = "localhost"
	cfg.LifecyclerConfig.ID = "localhost"
	cfg.LifecyclerConfig.FinalSleep = 0
	return cfg
}

func (s *testStore) Put(ctx context.Context, chunks []chunk.Chunk) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}
	for _, chunk := range chunks {
		for _, label := range chunk.Metric {
			if label.Value == "" {
				return fmt.Errorf("Chunk has blank label %q", label.Name)
			}
		}
	}
	s.chunks[userID] = append(s.chunks[userID], chunks...)
	return nil
}

func (s *testStore) IsLocal() bool {
	return false
}

func (s *testStore) LazyQuery(ctx context.Context, req *logproto.QueryRequest) (iter.EntryIterator, error) {
	return nil, nil
}

func (s *testStore) Stop() {}

func pushTestSamples(t *testing.T, ing logproto.PusherServer) ([]string, map[string][]*logproto.Stream) {
	userIDs := []string{"1", "2", "3"}

	// Create test samples.
	testData := map[string][]*logproto.Stream{}
	for i, userID := range userIDs {
		testData[userID] = buildTestStreams(i)
	}

	// Append samples.
	for _, userID := range userIDs {
		ctx := user.InjectOrgID(context.Background(), userID)
		_, err := ing.Push(ctx, &logproto.PushRequest{
			Streams: testData[userID],
		})
		require.NoError(t, err)
	}
	return userIDs, testData
}

func buildTestStreams(offset int) []*logproto.Stream {
	var m []*logproto.Stream
	for i := 0; i < numSeries; i++ {
		ss := logproto.Stream{
			Labels: model.Metric{
				"name":         model.LabelValue(fmt.Sprintf("testmetric_%d", i)),
				model.JobLabel: "testjob",
			}.String(),
		}
		for j := 0; j < samplesPerSeries; j++ {
			ss.Entries = append(ss.Entries, logproto.Entry{
				Timestamp: time.Unix(int64(i+j+offset), 0),
				Line:      "line",
			})
		}
		m = append(m, &ss)
	}

	sort.Slice(m, func(i, j int) bool {
		return m[i].Labels < m[j].Labels
	})

	return m
}

// check that the store is holding data equivalent to what we expect
func (s *testStore) checkData(t *testing.T, userIDs []string, testData map[string][]*logproto.Stream) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	for _, userID := range userIDs {
		chunks := s.chunks[userID]
		streams := []*logproto.Stream{}
		for _, chunk := range chunks {
			lokiChunk := chunk.Data.(*chunkenc.Facade).LokiChunk()
			if chunk.Metric.Has("__name__") {
				labelsBuilder := labels.NewBuilder(chunk.Metric)
				labelsBuilder.Del("__name__")
				chunk.Metric = labelsBuilder.Labels()
			}
			labels := chunk.Metric.String()
			streams = append(streams, buildStreamsFromChunk(t, labels, lokiChunk))
		}
		sort.Slice(streams, func(i, j int) bool {
			return streams[i].Labels < streams[j].Labels
		})
		require.Equal(t, testData[userID], streams)
	}
}

func buildStreamsFromChunk(t *testing.T, labels string, chk chunkenc.Chunk) *logproto.Stream {
	it, err := chk.Iterator(time.Unix(0, 0), time.Unix(1000, 0), logproto.FORWARD, nil)
	require.NoError(t, err)

	stream := &logproto.Stream{
		Labels: labels,
	}
	for it.Next() {
		stream.Entries = append(stream.Entries, it.Entry())
	}
	require.NoError(t, it.Error())
	return stream
}
