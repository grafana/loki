package ingester

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
	"golang.org/x/net/context"
)

type testStore struct {
	mtx sync.Mutex
	// Chunks keyed by userID.
	chunks map[string][]chunk.Chunk
}

func newTestStore(t require.TestingT, cfg Config) (*testStore, *Ingester) {
	store := &testStore{
		chunks: map[string][]chunk.Chunk{},
	}

	ing, err := New(cfg, store)
	require.NoError(t, err)

	return store, ing
}

func newDefaultTestStore(t require.TestingT) (*testStore, *Ingester) {
	return newTestStore(t,
		defaultIngesterTestConfig(),
	)
}

func defaultIngesterTestConfig() Config {
	consul := ring.NewInMemoryKVClient()
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.FlushCheckPeriod = 99999 * time.Hour
	cfg.MaxChunkIdle = 99999 * time.Hour
	cfg.ConcurrentFlushes = 1
	cfg.LifecyclerConfig.RingConfig.Mock = consul
	cfg.LifecyclerConfig.NumTokens = 1
	cfg.LifecyclerConfig.ListenPort = func(i int) *int { return &i }(0)
	cfg.LifecyclerConfig.Addr = "localhost"
	cfg.LifecyclerConfig.ID = "localhost"
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
		for k, v := range chunk.Metric {
			if v == "" {
				return fmt.Errorf("Chunk has blank label %q", k)
			}
		}
	}
	s.chunks[userID] = append(s.chunks[userID], chunks...)
	return nil
}

func (s *testStore) Stop() {}

// check that the store is holding data equivalent to what we expect
func (s *testStore) checkData(t *testing.T, userIDs []string, testData map[string][]*logproto.Stream) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	for _, userID := range userIDs {
		chunks := make([]chunkenc.Chunk, 0, len(s.chunks[userID]))
		labels := make([]string, 0, len(s.chunks[userID]))
		for _, chk := range s.chunks[userID] {
			chunks = append(chunks, chk.Data.(*chunkenc.Facade).LokiChunk())

			delete(chk.Metric, nameLabel)
			labels = append(labels, chk.Metric.String())
		}

		streams := make([]*logproto.Stream, 0, len(chunks))
		for i, chk := range chunks {
			stream := buildStreamsFromChunk(t, labels[i], chk)
			streams = append(streams, stream)
		}
		sort.Slice(streams, func(i, j int) bool {
			return streams[i].Labels < streams[i].Labels
		})

		require.Equal(t, testData[userID], streams)
	}
}

func buildStreamsFromChunk(t *testing.T, labels string, chk chunkenc.Chunk) *logproto.Stream {
	//start, end := chk.Bounds()
	it, err := chk.Iterator(time.Unix(0, 0), time.Unix(1000, 0), logproto.FORWARD)
	require.NoError(t, err)

	stream := &logproto.Stream{}
	stream.Labels = labels
	for it.Next() {
		stream.Entries = append(stream.Entries, it.Entry())
	}
	require.NoError(t, it.Error())

	return stream
}

func buildTestStreams(numSeries int, linesPerSeries int, offset int) []*logproto.Stream {
	m := make([]*logproto.Stream, 0, numSeries)
	for i := 0; i < numSeries; i++ {
		ss := logproto.Stream{
			Labels: model.Metric{
				"name":         model.LabelValue(fmt.Sprintf("testmetric_%d", i)),
				model.JobLabel: "testjob",
			}.String(),
			Entries: make([]logproto.Entry, 0, linesPerSeries),
		}
		for j := 0; j < linesPerSeries; j++ {
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

func pushTestSamples(t *testing.T, ing *Ingester, numSeries, samplesPerSeries int) ([]string, map[string][]*logproto.Stream) {
	userIDs := []string{"1", "2", "3"}

	// Create test samples.
	testData := map[string][]*logproto.Stream{}
	for i, userID := range userIDs {
		testData[userID] = buildTestStreams(numSeries, samplesPerSeries, i)
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

func TestChunkFlushingIdle(t *testing.T) {
	cfg := defaultIngesterTestConfig()
	cfg.FlushCheckPeriod = 20 * time.Millisecond
	cfg.MaxChunkIdle = 100 * time.Millisecond
	cfg.RetainPeriod = 500 * time.Millisecond

	store, ing := newTestStore(t, cfg)

	userIDs, testData := pushTestSamples(t, ing, 4, 100)

	// wait beyond idle time so samples flush
	time.Sleep(cfg.MaxChunkIdle * 2)

	store.checkData(t, userIDs, testData)

}
