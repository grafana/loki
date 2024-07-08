package ingester

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"syscall"
	"testing"
	"time"

	gokitlog "github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	numSeries        = 10
	samplesPerSeries = 100
)

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

func (fullWAL) Log(_ *wal.Record) error { return &os.PathError{Err: syscall.ENOSPC} }
func (fullWAL) Start()                  {}
func (fullWAL) Stop() error             { return nil }

func Benchmark_FlushLoop(b *testing.B) {
	var (
		size   = 5
		descs  [][]*chunkDesc
		lbs    = makeRandomLabels()
		ctx    = user.InjectOrgID(context.Background(), "foo")
		_, ing = newTestStore(b, defaultIngesterTestConfig(b), nil)
	)

	for i := 0; i < size; i++ {
		descs = append(descs, buildChunkDecs(b))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		var wg sync.WaitGroup
		for i := 0; i < size; i++ {
			wg.Add(1)
			go func(loop int) {
				defer wg.Done()
				require.NoError(b, ing.flushChunks(ctx, 0, lbs, descs[loop], &sync.RWMutex{}))
			}(i)
		}
		wg.Wait()
	}
}

func Test_FlushOp(t *testing.T) {
	t.Run("no error", func(t *testing.T) {
		cfg := defaultIngesterTestConfig(t)
		cfg.FlushOpBackoff.MinBackoff = time.Second
		cfg.FlushOpBackoff.MaxBackoff = 10 * time.Second
		cfg.FlushOpBackoff.MaxRetries = 1
		cfg.FlushCheckPeriod = 100 * time.Millisecond

		_, ing := newTestStore(t, cfg, nil)

		ctx := user.InjectOrgID(context.Background(), "foo")
		ins, err := ing.GetOrCreateInstance("foo")
		require.NoError(t, err)

		lbs := makeRandomLabels()
		req := &logproto.PushRequest{Streams: []logproto.Stream{{
			Labels:  lbs.String(),
			Entries: entries(5, time.Now()),
		}}}
		require.NoError(t, ins.Push(ctx, req))

		time.Sleep(cfg.FlushCheckPeriod)
		require.NoError(t, ing.flushOp(gokitlog.NewNopLogger(), &flushOp{
			immediate: true,
			userID:    "foo",
			fp:        ins.getHashForLabels(lbs),
		}))
	})

	t.Run("max retries exceeded", func(t *testing.T) {
		cfg := defaultIngesterTestConfig(t)
		cfg.FlushOpBackoff.MinBackoff = time.Second
		cfg.FlushOpBackoff.MaxBackoff = 10 * time.Second
		cfg.FlushOpBackoff.MaxRetries = 1
		cfg.FlushCheckPeriod = 100 * time.Millisecond

		store, ing := newTestStore(t, cfg, nil)
		store.onPut = func(_ context.Context, _ []chunk.Chunk) error {
			return errors.New("failed to write chunks")
		}

		ctx := user.InjectOrgID(context.Background(), "foo")
		ins, err := ing.GetOrCreateInstance("foo")
		require.NoError(t, err)

		lbs := makeRandomLabels()
		req := &logproto.PushRequest{Streams: []logproto.Stream{{
			Labels:  lbs.String(),
			Entries: entries(5, time.Now()),
		}}}
		require.NoError(t, ins.Push(ctx, req))

		time.Sleep(cfg.FlushCheckPeriod)
		require.EqualError(t, ing.flushOp(gokitlog.NewNopLogger(), &flushOp{
			immediate: true,
			userID:    "foo",
			fp:        ins.getHashForLabels(lbs),
		}), "terminated after 1 retries")
	})
}

func Test_Flush(t *testing.T) {
	var (
		store, ing = newTestStore(t, defaultIngesterTestConfig(t), nil)
		lbs        = makeRandomLabels()
		ctx        = user.InjectOrgID(context.Background(), "foo")
	)
	store.onPut = func(_ context.Context, chunks []chunk.Chunk) error {
		for _, c := range chunks {
			buf, err := c.Encoded()
			require.Nil(t, err)
			if err := c.Decode(chunk.NewDecodeContext(), buf); err != nil {
				return err
			}
		}
		return nil
	}
	require.NoError(t, ing.flushChunks(ctx, 0, lbs, buildChunkDecs(t), &sync.RWMutex{}))
}

func buildChunkDecs(t testing.TB) []*chunkDesc {
	res := make([]*chunkDesc, 10)
	for i := range res {
		res[i] = &chunkDesc{
			closed: true,
			chunk:  chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncSnappy, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, dummyConf().BlockSize, dummyConf().TargetChunkSize),
		}
		fillChunk(t, res[i].chunk)
		require.NoError(t, res[i].chunk.Close())
	}
	return res
}

func TestWALFullFlush(t *testing.T) {
	// technically replaced with a fake wal, but the ingester New() function creates a regular wal first,
	// so we enable creation/cleanup even though it remains unused.
	walDir := t.TempDir()

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
		require.False(t, chunkFingerprints[c.FingerprintModel()])
		chunkFingerprints[c.FingerprintModel()] = true
	}
}

func Test_flush_not_owned_stream(t *testing.T) {
	cfg := defaultIngesterTestConfig(t)
	cfg.FlushCheckPeriod = time.Millisecond * 100
	cfg.MaxChunkAge = time.Minute
	cfg.MaxChunkIdle = time.Hour

	store, ing := newTestStore(t, cfg, nil)
	defer store.Stop()

	now := time.Unix(0, 0)

	entries := []logproto.Entry{
		{Timestamp: now.Add(time.Nanosecond), Line: "1"},
		{Timestamp: now.Add(time.Minute), Line: "2"},
	}

	labelSet := model.LabelSet{"app": "l"}
	req := &logproto.PushRequest{Streams: []logproto.Stream{
		{Labels: labelSet.String(), Entries: entries},
	}}

	const userID = "testUser"
	ctx := user.InjectOrgID(context.Background(), userID)

	_, err := ing.Push(ctx, req)
	require.NoError(t, err)

	time.Sleep(2 * cfg.FlushCheckPeriod)

	// ensure chunk is not flushed after flush period elapses
	store.checkData(t, map[string][]logproto.Stream{})

	instance, found := ing.getInstanceByID(userID)
	require.True(t, found)
	fingerprint := instance.getHashForLabels(labels.FromStrings("app", "l"))
	require.Equal(t, model.Fingerprint(16794418009594958), fingerprint)
	instance.ownedStreamsSvc.trackStreamOwnership(fingerprint, false)

	time.Sleep(2 * cfg.FlushCheckPeriod)

	// assert stream is now both batches
	store.checkData(t, map[string][]logproto.Stream{
		userID: {
			{Labels: labelSet.String(), Entries: entries},
		},
	})

	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), ing))
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

func TestFlushLoopCanExitDuringInitialWait(t *testing.T) {
	cfg := defaultIngesterTestConfig(t)
	// This gives us an initial delay of max 48s
	// 60s * 0.8 = 48s
	cfg.FlushCheckPeriod = time.Minute

	start := time.Now()
	store, ing := newTestStore(t, cfg, nil)
	defer store.Stop()
	// immediately stop
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), ing))
	duration := time.Since(start)
	require.True(t, duration < 5*time.Second, "ingester could not shut down while waiting for initial delay")
}

type testStore struct {
	mtx sync.Mutex
	// Chunks keyed by userID.
	chunks map[string][]chunk.Chunk
	onPut  func(ctx context.Context, chunks []chunk.Chunk) error
}

// Note: the ingester New() function creates it's own WAL first which we then override if specified.
// Because of this, ensure any WAL directories exist/are cleaned up even when overriding the wal.
// This is an ugly hook for testing :(
func newTestStore(t require.TestingT, cfg Config, walOverride WAL) (*testStore, *Ingester) {
	store := &testStore{
		chunks: map[string][]chunk.Chunk{},
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	ing, err := New(cfg, client.Config{}, store, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokitlog.NewNopLogger(), nil, readRingMock)
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), ing))

	if walOverride != nil {
		_ = ing.wal.Stop()
		ing.wal = walOverride
	}

	return store, ing
}

// nolint
func defaultIngesterTestConfig(t testing.TB) Config {
	kvClient, err := kv.NewClient(kv.Config{Store: "inmemory"}, ring.GetCodec(), nil, gokitlog.NewNopLogger())
	require.NoError(t, err)

	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.FlushOpBackoff.MinBackoff = 100 * time.Millisecond
	cfg.FlushOpBackoff.MaxBackoff = 10 * time.Second
	cfg.FlushOpBackoff.MaxRetries = 1
	cfg.FlushOpTimeout = 15 * time.Second
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
	cfg.BlockSize = 256 * 1024
	cfg.TargetChunkSize = 1500 * 1024
	cfg.WAL.Enabled = false
	cfg.OwnedStreamsCheckInterval = 1 * time.Second
	return cfg
}

func (s *testStore) Put(ctx context.Context, chunks []chunk.Chunk) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.onPut != nil {
		return s.onPut(ctx, chunks)
	}
	userID, err := tenant.TenantID(ctx)
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

func (s *testStore) PutOne(_ context.Context, _, _ model.Time, _ chunk.Chunk) error {
	return nil
}

func (s *testStore) IsLocal() bool {
	return false
}

func (s *testStore) SelectLogs(_ context.Context, _ logql.SelectLogParams) (iter.EntryIterator, error) {
	return nil, nil
}

func (s *testStore) SelectSamples(_ context.Context, _ logql.SelectSampleParams) (iter.SampleIterator, error) {
	return nil, nil
}

func (s *testStore) SelectSeries(_ context.Context, _ logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	return nil, nil
}

func (s *testStore) GetChunks(_ context.Context, _ string, _, _ model.Time, _ chunk.Predicate, _ *logproto.ChunkRefGroup) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	return nil, nil, nil
}

func (s *testStore) GetShards(_ context.Context, _ string, _, _ model.Time, _ uint64, _ chunk.Predicate) (*logproto.ShardsResponse, error) {
	return nil, nil
}

func (s *testStore) HasForSeries(_, _ model.Time) (sharding.ForSeries, bool) {
	return nil, false
}

func (s *testStore) GetSchemaConfigs() []config.PeriodConfig {
	return defaultPeriodConfigs
}

func (s *testStore) Stop() {}

func (s *testStore) SetChunkFilterer(_ chunk.RequestChunkFilterer) {}

func (s *testStore) Stats(_ context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) (*stats.Stats, error) {
	return &stats.Stats{}, nil
}

func (s *testStore) Volume(_ context.Context, _ string, _, _ model.Time, _ int32, _ []string, _ string, _ ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	return &logproto.VolumeResponse{}, nil
}

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
		stream.Entries = append(stream.Entries, it.At())
	}
	require.NoError(t, it.Err())
	return stream
}
