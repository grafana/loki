package ingester

import (
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/log"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/services"

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

	store, ing := newTestStore(t, cfg, nil)
	defer services.StopAndAwaitTerminated(context.Background(), ing) //nolint:errcheck
	testData := pushTestSamples(t, ing)

	// wait beyond idle time so samples flush
	time.Sleep(cfg.MaxChunkIdle * 2)
	store.checkData(t, testData)
}

func TestChunkFlushingShutdown(t *testing.T) {
	store, ing := newTestStore(t, defaultIngesterTestConfig(t), nil)
	testData := pushTestSamples(t, ing)
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), ing))
	store.checkData(t, testData)
}

type fullWAL struct{}

func (fullWAL) Log(_ *WALRecord) error { return &os.PathError{Err: syscall.ENOSPC} }
func (fullWAL) Stop() error            { return nil }

func TestWALFullFlush(t *testing.T) {
	// technically replaced with a fake wal, but the ingester New() function creates a regular wal first,
	// so we enable creation/cleanup even though it remains unused.
	walDir, err := ioutil.TempDir(os.TempDir(), "loki-wal")
	require.Nil(t, err)
	defer os.RemoveAll(walDir)

	store, ing := newTestStore(t, defaultIngesterTestConfigWithWAL(t, walDir), fullWAL{})
	testData := pushTestSamples(t, ing)
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), ing))
	store.checkData(t, testData)
}

func TestFlushingCollidingLabels(t *testing.T) {
	cfg := defaultIngesterTestConfig(t)
	cfg.FlushCheckPeriod = 20 * time.Millisecond
	cfg.MaxChunkIdle = 100 * time.Millisecond
	cfg.RetainPeriod = 500 * time.Millisecond

	store, ing := newTestStore(t, cfg, nil)
	defer store.Stop()

	const userID = "testUser"
	ctx := user.InjectOrgID(context.Background(), userID)

	// checkData only iterates between unix seconds 0 and 1000
	now := time.Unix(0, 0)

	req := &logproto.PushRequest{Streams: []logproto.Stream{
		// some colliding label sets
		{Labels: model.LabelSet{"app": "l", "uniq0": "0", "uniq1": "1"}.String(), Entries: entries(5, now.Add(time.Minute))},
		{Labels: model.LabelSet{"app": "m", "uniq0": "1", "uniq1": "1"}.String(), Entries: entries(5, now)},
		{Labels: model.LabelSet{"app": "l", "uniq0": "1", "uniq1": "0"}.String(), Entries: entries(5, now.Add(time.Minute))},
		{Labels: model.LabelSet{"app": "m", "uniq0": "0", "uniq1": "0"}.String(), Entries: entries(5, now)},
		{Labels: model.LabelSet{"app": "l", "uniq0": "0", "uniq1": "0"}.String(), Entries: entries(5, now.Add(time.Minute))},
		{Labels: model.LabelSet{"app": "m", "uniq0": "1", "uniq1": "0"}.String(), Entries: entries(5, now)},
	}}

	sort.Slice(req.Streams, func(i, j int) bool {
		return req.Streams[i].Labels < req.Streams[j].Labels
	})

	_, err := ing.Push(ctx, req)
	require.NoError(t, err)

	// force flush
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), ing))

	// verify that we get all the data back
	store.checkData(t, map[string][]logproto.Stream{userID: req.Streams})

	// make sure all chunks have different fingerprint, even colliding ones.
	chunkFingerprints := map[model.Fingerprint]bool{}
	for _, c := range store.getChunksForUser(userID) {
		require.False(t, chunkFingerprints[c.Fingerprint])
		chunkFingerprints[c.Fingerprint] = true
	}
}

func TestFlushMaxAge(t *testing.T) {
	cfg := defaultIngesterTestConfig(t)
	cfg.FlushCheckPeriod = time.Millisecond * 100
	cfg.MaxChunkAge = time.Minute
	cfg.MaxChunkIdle = time.Hour

	store, ing := newTestStore(t, cfg, nil)
	defer store.Stop()

	now := time.Unix(0, 0)

	firstEntries := []logproto.Entry{
		{Timestamp: now.Add(time.Nanosecond), Line: "1"},
		{Timestamp: now.Add(time.Minute), Line: "2"},
	}

	secondEntries := []logproto.Entry{
		{Timestamp: now.Add(time.Second * 61), Line: "3"},
	}

	req := &logproto.PushRequest{Streams: []logproto.Stream{
		{Labels: model.LabelSet{"app": "l"}.String(), Entries: firstEntries},
	}}

	const userID = "testUser"
	ctx := user.InjectOrgID(context.Background(), userID)

	_, err := ing.Push(ctx, req)
	require.NoError(t, err)

	time.Sleep(2 * cfg.FlushCheckPeriod)

	// ensure chunk is not flushed after flush period elapses
	store.checkData(t, map[string][]logproto.Stream{})

	req2 := &logproto.PushRequest{Streams: []logproto.Stream{
		{Labels: model.LabelSet{"app": "l"}.String(), Entries: secondEntries},
	}}

	_, err = ing.Push(ctx, req2)
	require.NoError(t, err)

	time.Sleep(2 * cfg.FlushCheckPeriod)

	// assert stream is now both batches
	store.checkData(t, map[string][]logproto.Stream{
		userID: {
			{Labels: model.LabelSet{"app": "l"}.String(), Entries: append(firstEntries, secondEntries...)},
		},
	})

	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), ing))
}

type testStore struct {
	mtx sync.Mutex
	// Chunks keyed by userID.
	chunks map[string][]chunk.Chunk
}

// Note: the ingester New() function creates it's own WAL first which we then override if specified.
// Because of this, ensure any WAL directories exist/are cleaned up even when overriding the wal.
// This is an ugly hook for testing :(
func newTestStore(t require.TestingT, cfg Config, walOverride WAL) (*testStore, *Ingester) {
	store := &testStore{
		chunks: map[string][]chunk.Chunk{},
	}

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	ing, err := New(cfg, client.Config{}, store, limits, nil)
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), ing))

	if walOverride != nil {
		_ = ing.wal.Stop()
		ing.wal = walOverride
	}

	return store, ing
}

// nolint
func defaultIngesterTestConfig(t *testing.T) Config {
	kvClient, err := kv.NewClient(kv.Config{Store: "inmemory"}, ring.GetCodec(), nil)
	require.NoError(t, err)

	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.FlushCheckPeriod = 99999 * time.Hour
	cfg.MaxChunkIdle = 99999 * time.Hour
	cfg.ConcurrentFlushes = 1
	cfg.LifecyclerConfig.RingConfig.KVStore.Mock = kvClient
	cfg.LifecyclerConfig.NumTokens = 1
	cfg.LifecyclerConfig.ListenPort = 0
	cfg.LifecyclerConfig.Addr = "localhost"
	cfg.LifecyclerConfig.ID = "localhost"
	cfg.LifecyclerConfig.FinalSleep = 0
	cfg.LifecyclerConfig.MinReadyDuration = 0
	return cfg
}

func (s *testStore) Put(ctx context.Context, chunks []chunk.Chunk) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}
	for ix, chunk := range chunks {
		for _, label := range chunk.Metric {
			if label.Value == "" {
				return fmt.Errorf("Chunk has blank label %q", label.Name)
			}
		}

		// remove __name__ label
		if chunk.Metric.Has("__name__") {
			labelsBuilder := labels.NewBuilder(chunk.Metric)
			labelsBuilder.Del("__name__")
			chunks[ix].Metric = labelsBuilder.Labels()
		}
	}
	s.chunks[userID] = append(s.chunks[userID], chunks...)
	return nil
}

func (s *testStore) IsLocal() bool {
	return false
}

func (s *testStore) SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
	return nil, nil
}

func (s *testStore) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	return nil, nil
}

func (s *testStore) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*chunk.Fetcher, error) {
	return nil, nil, nil
}

func (s *testStore) GetSchemaConfigs() []chunk.PeriodConfig {
	return nil
}

func (s *testStore) Stop() {}

func pushTestSamples(t *testing.T, ing logproto.PusherServer) map[string][]logproto.Stream {
	userIDs := []string{"1", "2", "3"}

	// Create test samples.
	testData := map[string][]logproto.Stream{}
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
	return testData
}

func buildTestStreams(offset int) []logproto.Stream {
	var m []logproto.Stream
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
		m = append(m, ss)
	}

	sort.Slice(m, func(i, j int) bool {
		return m[i].Labels < m[j].Labels
	})

	return m
}

// check that the store is holding data equivalent to what we expect
func (s *testStore) checkData(t *testing.T, testData map[string][]logproto.Stream) {
	for userID, expected := range testData {
		streams := s.getStreamsForUser(t, userID)
		require.Equal(t, expected, streams)
	}
}

func (s *testStore) getStreamsForUser(t *testing.T, userID string) []logproto.Stream {
	var streams []logproto.Stream
	for _, c := range s.getChunksForUser(userID) {
		lokiChunk := c.Data.(*chunkenc.Facade).LokiChunk()
		streams = append(streams, buildStreamsFromChunk(t, c.Metric.String(), lokiChunk))
	}
	sort.Slice(streams, func(i, j int) bool {
		return streams[i].Labels < streams[j].Labels
	})
	return streams
}

func (s *testStore) getChunksForUser(userID string) []chunk.Chunk {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	return s.chunks[userID]
}

func buildStreamsFromChunk(t *testing.T, lbs string, chk chunkenc.Chunk) logproto.Stream {
	it, err := chk.Iterator(context.TODO(), time.Unix(0, 0), time.Unix(1000, 0), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.Labels{}))
	require.NoError(t, err)

	stream := logproto.Stream{
		Labels: lbs,
	}
	for it.Next() {
		stream.Entries = append(stream.Entries, it.Entry())
	}
	require.NoError(t, it.Error())
	return stream
}
