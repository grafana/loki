package ingester

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	gokit_log "github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

// small util for ensuring data exists as we expect
func ensureIngesterData(ctx context.Context, t *testing.T, start, end time.Time, i Interface) {
	result := mockQuerierServer{
		ctx: ctx,
	}
	err := i.Query(&logproto.QueryRequest{
		Selector: `{foo="bar"}`,
		Limit:    100,
		Start:    start,
		End:      end,
	}, &result)

	ln := int(end.Sub(start) / time.Second)
	require.NoError(t, err)
	// We always send an empty batch to make sure stats are sent, so there will always be one empty response.
	require.Len(t, result.resps, 2)
	require.Len(t, result.resps[0].Streams, 2)
	require.Len(t, result.resps[0].Streams[0].Entries, ln)
	require.Len(t, result.resps[0].Streams[1].Entries, ln)
}

func defaultIngesterTestConfigWithWAL(t *testing.T, walDir string) Config {
	ingesterConfig := defaultIngesterTestConfig(t)
	ingesterConfig.WAL.Enabled = true
	ingesterConfig.WAL.Dir = walDir
	ingesterConfig.WAL.CheckpointDuration = time.Second

	return ingesterConfig
}

func TestIngesterWAL(t *testing.T) {
	walDir := t.TempDir()

	ingesterConfig := defaultIngesterTestConfigWithWAL(t, walDir)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	newStore := func() *mockStore {
		return &mockStore{
			chunks: map[string][]chunk.Chunk{},
		}
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))
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

	start := time.Now()
	steps := 10
	end := start.Add(time.Second * time.Duration(steps))

	for i := 0; i < steps; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Line:      fmt.Sprintf("line %d", i),
		})
		req.Streams[1].Entries = append(req.Streams[1].Entries, logproto.Entry{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	ensureIngesterData(ctx, t, start, end, i)

	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// ensure we haven't checkpointed yet
	expectCheckpoint(t, walDir, false, time.Second)

	// restart the ingester
	i, err = New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// ensure we've recovered data from wal segments
	ensureIngesterData(ctx, t, start, end, i)

	// ensure we have checkpointed now
	expectCheckpoint(t, walDir, true, ingesterConfig.WAL.CheckpointDuration*5) // give a bit of buffer

	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// restart the ingester
	i, err = New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// ensure we've recovered data from checkpoint+wal segments
	ensureIngesterData(ctx, t, start, end, i)
}

func TestIngesterWALIgnoresStreamLimits(t *testing.T) {
	walDir := t.TempDir()

	ingesterConfig := defaultIngesterTestConfigWithWAL(t, walDir)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	newStore := func() *mockStore {
		return &mockStore{
			chunks: map[string][]chunk.Chunk{},
		}
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))
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

	start := time.Now()
	steps := 10
	end := start.Add(time.Second * time.Duration(steps))

	for i := 0; i < steps; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Line:      fmt.Sprintf("line %d", i),
		})
		req.Streams[1].Entries = append(req.Streams[1].Entries, logproto.Entry{
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Line:      fmt.Sprintf("line %d", i),
		})
	}

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	ensureIngesterData(ctx, t, start, end, i)

	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// Limit all streams except those written during WAL recovery.
	limitCfg := defaultLimitsTestConfig()
	limitCfg.MaxLocalStreamsPerUser = -1
	limits, err = validation.NewOverrides(limitCfg, nil)
	require.NoError(t, err)

	// restart the ingester
	i, err = New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// ensure we've recovered data from wal segments
	ensureIngesterData(ctx, t, start, end, i)

	req = logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="new"}`,
				Entries: []logproto.Entry{
					{
						Timestamp: start,
						Line:      "hi",
					},
				},
			},
		},
	}

	ctx = user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	// Ensure regular pushes error due to stream limits.
	require.Error(t, err)
}

func TestUnflushedChunks(t *testing.T) {
	chks := []chunkDesc{
		{
			flushed: time.Now(),
		},
		{},
		{
			flushed: time.Now(),
		},
	}

	require.Equal(t, 1, len(unflushedChunks(chks)))
}

func TestIngesterWALBackpressureSegments(t *testing.T) {
	walDir := t.TempDir()

	ingesterConfig := defaultIngesterTestConfigWithWAL(t, walDir)
	ingesterConfig.WAL.ReplayMemoryCeiling = 1000

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	newStore := func() *mockStore {
		return &mockStore{
			chunks: map[string][]chunk.Chunk{},
		}
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	start := time.Now()
	// Replay data 5x larger than the ceiling.
	totalSize := int(5 * i.cfg.WAL.ReplayMemoryCeiling)
	req, written := mkPush(start, totalSize)
	require.Equal(t, totalSize, written)

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, req)
	require.NoError(t, err)

	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// ensure we haven't checkpointed yet
	expectCheckpoint(t, walDir, false, time.Second)

	// restart the ingester, ensuring we replayed from WAL.
	i, err = New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))
}

func TestIngesterWALBackpressureCheckpoint(t *testing.T) {
	walDir := t.TempDir()

	ingesterConfig := defaultIngesterTestConfigWithWAL(t, walDir)
	ingesterConfig.WAL.ReplayMemoryCeiling = 1000

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	newStore := func() *mockStore {
		return &mockStore{
			chunks: map[string][]chunk.Chunk{},
		}
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	i, err := New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck

	start := time.Now()
	// Replay data 5x larger than the ceiling.
	totalSize := int(5 * i.cfg.WAL.ReplayMemoryCeiling)
	req, written := mkPush(start, totalSize)
	require.Equal(t, totalSize, written)

	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, req)
	require.NoError(t, err)

	// ensure we have checkpointed now
	expectCheckpoint(t, walDir, true, ingesterConfig.WAL.CheckpointDuration*5) // give a bit of buffer

	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// restart the ingester, ensuring we can replay from the checkpoint as well.
	i, err = New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))
}

func expectCheckpoint(t *testing.T, walDir string, shouldExist bool, maxVal time.Duration) {
	once := make(chan struct{}, 1)
	once <- struct{}{}

	deadline := time.After(maxVal)
	for {
		select {
		case <-deadline:
			require.Fail(t, "timeout while waiting for checkpoint existence:", shouldExist)
		case <-once: // Trick to ensure we check immediately before deferring to ticker.
		default:
			<-time.After(maxVal / 10) // check 10x over the duration
		}

		fs, err := os.ReadDir(walDir)
		require.Nil(t, err)
		var found bool
		for _, f := range fs {
			if _, err := checkpointIndex(f.Name(), false); err == nil {
				found = true
			}
		}
		if found == shouldExist {
			return
		}
	}
}

// mkPush makes approximately totalSize bytes of log lines across min(500, totalSize) streams
func mkPush(start time.Time, totalSize int) (*logproto.PushRequest, int) {
	var written int
	req := &logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar",bar="baz1"}`,
			},
		},
	}
	totalStreams := 500
	if totalStreams > totalSize {
		totalStreams = totalSize
	}

	for i := 0; i < totalStreams; i++ {
		req.Streams = append(req.Streams, logproto.Stream{
			Labels: fmt.Sprintf(`{foo="bar",i="%d"}`, i),
		})

		for j := 0; j < totalSize/totalStreams; j++ {
			req.Streams[i].Entries = append(req.Streams[i].Entries, logproto.Entry{
				Timestamp: start.Add(time.Duration(j) * time.Nanosecond),
				Line:      string([]byte{1}),
			})
			written++
		}

	}
	return req, written
}

type ingesterInstancesFunc func() []*instance

func (i ingesterInstancesFunc) getInstances() []*instance {
	return i()
}

var currentSeries *Series

func buildStreams() []logproto.Stream {
	streams := make([]logproto.Stream, 10)
	for i := range streams {
		labels := makeRandomLabels().String()
		entries := make([]logproto.Entry, 15*1e3)
		for j := range entries {
			entries[j] = logproto.Entry{
				Timestamp: time.Unix(0, int64(j)),
				Line:      fmt.Sprintf("entry for line %d", j),
			}
		}
		streams[i] = logproto.Stream{
			Labels:  labels,
			Entries: entries,
		}
	}
	return streams
}

var (
	stream1 = logproto.Stream{
		Labels: labels.FromStrings("stream", "1").String(),
		Entries: []logproto.Entry{
			{
				Timestamp:          time.Unix(0, 1),
				Line:               "1",
				StructuredMetadata: logproto.EmptyLabelAdapters(),
				Parsed:             logproto.EmptyLabelAdapters(),
			},
			{
				Timestamp:          time.Unix(0, 2),
				Line:               "2",
				StructuredMetadata: logproto.EmptyLabelAdapters(),
				Parsed:             logproto.EmptyLabelAdapters(),
			},
		},
	}
	stream2 = logproto.Stream{
		Labels: labels.FromStrings("stream", "2").String(),
		Entries: []logproto.Entry{
			{
				Timestamp:          time.Unix(0, 1),
				Line:               "3",
				StructuredMetadata: logproto.EmptyLabelAdapters(),
				Parsed:             logproto.EmptyLabelAdapters(),
			},
			{
				Timestamp:          time.Unix(0, 2),
				Line:               "4",
				StructuredMetadata: logproto.EmptyLabelAdapters(),
				Parsed:             logproto.EmptyLabelAdapters(),
			},
		},
	}
)

func Test_SeriesIterator(t *testing.T) {
	var instances []*instance

	// NB (owen-d): Not sure why we have these overrides
	l := defaultLimitsTestConfig()
	l.MaxLocalStreamsPerUser = 1000
	l.IngestionRateMB = 1e4
	l.IngestionBurstSizeMB = 1e4

	limits, err := validation.NewOverrides(l, nil)
	require.NoError(t, err)

	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})
	tenantsRetention := retention.NewTenantsRetention(limits)

	for i := 0; i < 3; i++ {
		inst, err := newInstance(defaultConfig(), defaultPeriodConfigs, fmt.Sprintf("%d", i), limiter, runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, nil, nil, nil, nil, NewStreamRateCalculator(), nil, nil, tenantsRetention)
		require.Nil(t, err)
		require.NoError(t, inst.Push(context.Background(), &logproto.PushRequest{Streams: []logproto.Stream{stream1}}))
		require.NoError(t, inst.Push(context.Background(), &logproto.PushRequest{Streams: []logproto.Stream{stream2}}))
		instances = append(instances, inst)
	}

	iter := newStreamsIterator(ingesterInstancesFunc(func() []*instance {
		return instances
	}))

	for i := 0; i < 3; i++ {
		var streams []logproto.Stream
		for j := 0; j < 2; j++ {
			iter.Next()
			assert.Equal(t, fmt.Sprintf("%d", i), iter.Stream().UserID)
			memchunk, err := chunkenc.MemchunkFromCheckpoint(iter.Stream().Chunks[0].Data, iter.Stream().Chunks[0].Head, chunkenc.UnorderedHeadBlockFmt, 0, 0)
			require.NoError(t, err)
			it, err := memchunk.Iterator(context.Background(), time.Unix(0, 0), time.Unix(0, 100), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.EmptyLabels()))
			require.NoError(t, err)
			stream := logproto.Stream{
				Labels: logproto.FromLabelAdaptersToLabels(iter.Stream().Labels).String(),
			}
			for it.Next() {
				stream.Entries = append(stream.Entries, it.At())
			}
			require.NoError(t, it.Close())
			streams = append(streams, stream)
		}
		sort.Slice(streams, func(i, j int) bool { return streams[i].Labels < streams[j].Labels })
		require.Equal(t, stream1, streams[0])
		require.Equal(t, stream2, streams[1])
	}

	require.False(t, iter.Next())
	require.Nil(t, iter.Error())
}

func Benchmark_SeriesIterator(b *testing.B) {
	streams := buildStreams()
	instances := make([]*instance, 10)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(b, err)

	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})
	tenantsRetention := retention.NewTenantsRetention(limits)

	for i := range instances {
		inst, _ := newInstance(defaultConfig(), defaultPeriodConfigs, fmt.Sprintf("instance %d", i), limiter, runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, nil, nil, nil, nil, NewStreamRateCalculator(), nil, nil, tenantsRetention)

		require.NoError(b,
			inst.Push(context.Background(), &logproto.PushRequest{
				Streams: streams,
			}),
		)
		instances[i] = inst
	}
	it := newIngesterSeriesIter(ingesterInstancesFunc(func() []*instance {
		return instances
	}))
	defer it.Stop()

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		iter := it.Iter()
		for iter.Next() {
			currentSeries = iter.Stream()
		}
		require.NoError(b, iter.Error())
	}
}

type noOpWalLogger struct{}

func (noOpWalLogger) Log(_ ...[]byte) error { return nil }
func (noOpWalLogger) Close() error          { return nil }
func (noOpWalLogger) Dir() string           { return "" }

func Benchmark_CheckpointWrite(b *testing.B) {
	writer := WALCheckpointWriter{
		metrics:       NilMetrics,
		checkpointWAL: noOpWalLogger{},
	}
	lbs := labels.FromStrings("foo", "bar")
	chunks := buildChunks(b, 10)
	b.ReportAllocs()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		require.NoError(b, writer.Write(&Series{
			UserID:      "foo",
			Fingerprint: labels.StableHash(lbs),
			Labels:      logproto.FromLabelsToLabelAdapters(lbs),
			Chunks:      chunks,
		}))
	}
}

func buildChunks(t testing.TB, size int) []Chunk {
	descs := make([]chunkDesc, 0, size)
	chks := make([]Chunk, size)

	for i := 0; i < size; i++ {
		// build chunks of 256k blocks, 1.5MB target size. Same as default config.
		c := chunkenc.NewMemChunk(chunkenc.ChunkFormatV3, compression.GZIP, chunkenc.UnorderedHeadBlockFmt, 256*1024, 1500*1024)
		fillChunk(t, c)
		descs = append(descs, chunkDesc{
			chunk: c,
		})
	}

	there, err := toWireChunks(descs, nil)
	require.NoError(t, err)
	for i := range there {
		chks[i] = there[i].Chunk
	}
	return chks
}

func TestIngesterWALReplaysUnorderedToOrdered(t *testing.T) {
	for _, waitForCheckpoint := range []bool{false, true} {
		t.Run(fmt.Sprintf("checkpoint-%v", waitForCheckpoint), func(t *testing.T) {
			walDir := t.TempDir()

			ingesterConfig := defaultIngesterTestConfigWithWAL(t, walDir)

			// First launch the ingester with unordered writes enabled
			dft := defaultLimitsTestConfig()
			dft.UnorderedWrites = true
			limits, err := validation.NewOverrides(dft, nil)
			require.NoError(t, err)

			newStore := func() *mockStore {
				return &mockStore{
					chunks: map[string][]chunk.Chunk{},
				}
			}

			readRingMock := mockReadRingWithOneActiveIngester()

			i, err := New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
			require.NoError(t, err)
			require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))
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

			start := time.Now()
			steps := 10
			end := start.Add(time.Second * time.Duration(steps))

			// Write data out of order
			for i := steps - 1; i >= 0; i-- {
				req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
					Timestamp: start.Add(time.Duration(i) * time.Second),
					Line:      fmt.Sprintf("line %d", i),
				})
				req.Streams[1].Entries = append(req.Streams[1].Entries, logproto.Entry{
					Timestamp: start.Add(time.Duration(i) * time.Second),
					Line:      fmt.Sprintf("line %d", i),
				})
			}

			ctx := user.InjectOrgID(context.Background(), "test")
			_, err = i.Push(ctx, &req)
			require.NoError(t, err)

			if waitForCheckpoint {
				// Ensure we have checkpointed now
				expectCheckpoint(t, walDir, true, ingesterConfig.WAL.CheckpointDuration*10) // give a bit of buffer

				// Add some more data after the checkpoint
				tmp := end
				end = end.Add(time.Second * time.Duration(steps))
				req.Streams[0].Entries = nil
				req.Streams[1].Entries = nil
				// Write data out of order again
				for i := steps - 1; i >= 0; i-- {
					req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
						Timestamp: tmp.Add(time.Duration(i) * time.Second),
						Line:      fmt.Sprintf("line %d", steps+i),
					})
					req.Streams[1].Entries = append(req.Streams[1].Entries, logproto.Entry{
						Timestamp: tmp.Add(time.Duration(i) * time.Second),
						Line:      fmt.Sprintf("line %d", steps+i),
					})
				}

				_, err = i.Push(ctx, &req)
				require.NoError(t, err)
			}

			ensureIngesterData(ctx, t, start, end, i)

			require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

			// Now disable unordered writes
			limitCfg := defaultLimitsTestConfig()
			limitCfg.UnorderedWrites = false
			limits, err = validation.NewOverrides(limitCfg, nil)
			require.NoError(t, err)

			// restart the ingester
			i, err = New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
			require.NoError(t, err)
			defer services.StopAndAwaitTerminated(context.Background(), i) //nolint:errcheck
			require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

			// ensure we've recovered data from wal segments
			ensureIngesterData(ctx, t, start, end, i)
		})
	}
}

func TestCheckpointCleanupStaleTmpDirectories(t *testing.T) {
	walDir := t.TempDir()
	ingesterConfig := defaultIngesterTestConfigWithWAL(t, walDir)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	newStore := func() *mockStore {
		return &mockStore{
			chunks: map[string][]chunk.Chunk{},
		}
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	// Create some fake stale .tmp checkpoint directories
	staleTmpDirs := []string{
		"checkpoint.000100.tmp",
		"checkpoint.000200.tmp",
		"checkpoint.000300.tmp",
	}
	for _, dir := range staleTmpDirs {
		tmpPath := filepath.Join(walDir, dir)
		require.NoError(t, os.MkdirAll(tmpPath, 0750))
	}

	// Verify the stale .tmp directories exist
	files, err := os.ReadDir(walDir)
	require.NoError(t, err)
	tmpCount := 0
	for _, f := range files {
		if f.IsDir() && filepath.Ext(f.Name()) == ".tmp" {
			tmpCount++
		}
	}
	require.Equal(t, 3, tmpCount, "expected 3 .tmp directories before starting ingester")

	// Start the ingester - this should trigger checkpoint cleanup
	i, err := New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// Push some data to trigger checkpoint
	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar"}`,
				Entries: []logproto.Entry{
					{
						Timestamp: time.Now(),
						Line:      "test line",
					},
				},
			},
		},
	}
	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	// Wait for a checkpoint to be created
	expectCheckpoint(t, walDir, true, ingesterConfig.WAL.CheckpointDuration*10)

	// Stop the ingester to ensure no new checkpoints are being created
	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// Verify all stale .tmp directories have been cleaned up
	files, err = os.ReadDir(walDir)
	require.NoError(t, err)
	tmpCount = 0
	for _, f := range files {
		if f.IsDir() && filepath.Ext(f.Name()) == ".tmp" {
			tmpCount++
		}
	}
	require.Equal(t, 0, tmpCount, "expected all .tmp directories to be cleaned up after checkpoint")
}

func TestCheckpointCleanupOldCheckpoints(t *testing.T) {
	walDir := t.TempDir()
	ingesterConfig := defaultIngesterTestConfigWithWAL(t, walDir)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	newStore := func() *mockStore {
		return &mockStore{
			chunks: map[string][]chunk.Chunk{},
		}
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	// Phase 1: Start ingester and create some WAL segments and a checkpoint
	i, err := New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// Push some data to create WAL segments
	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar"}`,
				Entries: []logproto.Entry{
					{
						Timestamp: time.Now(),
						Line:      "test line",
					},
				},
			},
		},
	}
	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	// Wait for a checkpoint to be created
	expectCheckpoint(t, walDir, true, ingesterConfig.WAL.CheckpointDuration*10)

	// Stop the ingester
	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// Phase 2: Manually create a very old checkpoint that should have been deleted
	// This simulates the scenario where repeated checkpoint failures prevented normal cleanup.
	// In a healthy system, when checkpoint.000002 (or higher) completes, checkpoint.000001 would
	// be deleted by deleteCheckpoints(). But if checkpoints kept failing, old checkpoints accumulate.
	// The cleanup function should remove these superseded old checkpoints.
	oldCheckpointDir := filepath.Join(walDir, "checkpoint.000001")
	require.NoError(t, os.MkdirAll(oldCheckpointDir, 0750))

	// Verify the old checkpoint exists
	_, err = os.Stat(oldCheckpointDir)
	require.NoError(t, err, "old checkpoint should exist before cleanup")

	// Phase 3: Restart the ingester
	i, err = New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// Push more data to trigger another checkpoint which will clean up old checkpoints
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	// Wait for another checkpoint to be created (this triggers cleanup via deleteCheckpoints())
	time.Sleep(ingesterConfig.WAL.CheckpointDuration * 2)

	// Stop the ingester
	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// Phase 4: Verify the old checkpoint has been cleaned up
	_, err = os.Stat(oldCheckpointDir)
	require.True(t, os.IsNotExist(err), "old checkpoint should be deleted after new checkpoint completes")

	// Double-check by listing directory
	files, err := os.ReadDir(walDir)
	require.NoError(t, err)
	for _, f := range files {
		require.NotEqual(t, "checkpoint.000001", f.Name(), "old checkpoint should not exist in directory listing")
	}
}

func TestCheckpointCleanupOldCheckpointsAtStartup(t *testing.T) {
	walDir := t.TempDir()
	ingesterConfig := defaultIngesterTestConfigWithWAL(t, walDir)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	newStore := func() *mockStore {
		return &mockStore{
			chunks: map[string][]chunk.Chunk{},
		}
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	// Phase 1: Start ingester and create some WAL segments and a checkpoint
	i, err := New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// Push some data to create WAL segments
	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar"}`,
				Entries: []logproto.Entry{
					{
						Timestamp: time.Now(),
						Line:      "test line",
					},
				},
			},
		},
	}
	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	// Wait for a checkpoint to be created
	expectCheckpoint(t, walDir, true, ingesterConfig.WAL.CheckpointDuration*10)

	// Push more data and wait for another checkpoint to ensure we have multiple checkpoints
	// and segments have been truncated
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)
	time.Sleep(ingesterConfig.WAL.CheckpointDuration * 2)

	// Stop the ingester
	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// Phase 2: Manually create a very old checkpoint that should have been deleted
	// This simulates the scenario where repeated checkpoint failures prevented normal cleanup.
	// In a healthy system, when newer checkpoints complete, checkpoint.000000 would be deleted
	// by deleteCheckpoints(). But if checkpoints kept failing, old checkpoints accumulate.
	// This test verifies that startup cleanup removes these superseded old checkpoints.
	oldCheckpointDir := filepath.Join(walDir, "checkpoint.000000")
	require.NoError(t, os.MkdirAll(oldCheckpointDir, 0750))

	// Verify the old checkpoint exists
	_, err = os.Stat(oldCheckpointDir)
	require.NoError(t, err, "old checkpoint should exist before startup")

	// Count checkpoints before restart
	files, err := os.ReadDir(walDir)
	require.NoError(t, err)
	checkpointsBefore := 0
	for _, f := range files {
		if _, cpErr := checkpointIndex(f.Name(), false); cpErr == nil && f.IsDir() {
			checkpointsBefore++
		}
	}
	require.GreaterOrEqual(t, checkpointsBefore, 2, "should have at least 2 checkpoints before restart")

	// Phase 3: Restart the ingester WITHOUT pushing new data
	// This tests that startup cleanup works independently of new checkpoints
	i, err = New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// Phase 4: Verify the old checkpoint was cleaned up AT STARTUP
	// (without needing to create a new checkpoint)
	_, err = os.Stat(oldCheckpointDir)
	require.True(t, os.IsNotExist(err), "old checkpoint should be deleted at startup")

	// Verify it's actually gone from directory listing
	files, err = os.ReadDir(walDir)
	require.NoError(t, err)
	for _, f := range files {
		require.NotEqual(t, "checkpoint.000000", f.Name(), "old checkpoint should not exist after startup")
	}

	// Verify we still have at least one checkpoint (the valid one wasn't deleted)
	checkpointsAfter := 0
	for _, f := range files {
		if _, cpErr := checkpointIndex(f.Name(), false); cpErr == nil && f.IsDir() {
			checkpointsAfter++
		}
	}
	require.GreaterOrEqual(t, checkpointsAfter, 1, "should still have at least one valid checkpoint")
	require.Less(t, checkpointsAfter, checkpointsBefore, "should have fewer checkpoints after startup cleanup")

	// Stop the ingester
	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))
}

func TestCheckpointCleanupStaleTmpDirectoriesAtStartup(t *testing.T) {
	walDir := t.TempDir()
	ingesterConfig := defaultIngesterTestConfigWithWAL(t, walDir)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	newStore := func() *mockStore {
		return &mockStore{
			chunks: map[string][]chunk.Chunk{},
		}
	}

	readRingMock := mockReadRingWithOneActiveIngester()

	// Phase 1: Start ingester and create some WAL data, then stop
	i, err := New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// Push some data to create WAL segments
	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar"}`,
				Entries: []logproto.Entry{
					{
						Timestamp: time.Now(),
						Line:      "test line",
					},
				},
			},
		},
	}
	ctx := user.InjectOrgID(context.Background(), "test")
	_, err = i.Push(ctx, &req)
	require.NoError(t, err)

	// Wait for a checkpoint to be created
	expectCheckpoint(t, walDir, true, ingesterConfig.WAL.CheckpointDuration*10)

	// Stop the ingester
	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))

	// Phase 2: Manually create stale .tmp checkpoint directories to simulate crashed checkpoints
	staleTmpDirs := []string{
		"checkpoint.000100.tmp",
		"checkpoint.000200.tmp",
		"checkpoint.000300.tmp",
	}
	for _, dir := range staleTmpDirs {
		tmpPath := filepath.Join(walDir, dir)
		require.NoError(t, os.MkdirAll(tmpPath, 0750))
	}

	// Verify the stale .tmp directories exist before restart
	files, err := os.ReadDir(walDir)
	require.NoError(t, err)
	tmpCountBefore := 0
	for _, f := range files {
		if f.IsDir() && filepath.Ext(f.Name()) == ".tmp" {
			tmpCountBefore++
		}
	}
	require.Equal(t, 3, tmpCountBefore, "expected 3 .tmp directories before restarting ingester")

	// Phase 3: Restart the ingester - this should trigger IMMEDIATE cleanup at startup
	i, err = New(ingesterConfig, client.Config{}, newStore(), limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokit_log.NewNopLogger(), nil, readRingMock, nil)
	require.NoError(t, err)
	require.Nil(t, services.StartAndAwaitRunning(context.Background(), i))

	// Phase 4: Verify all stale .tmp directories have been cleaned up IMMEDIATELY
	// (without waiting for a new checkpoint to be created)
	files, err = os.ReadDir(walDir)
	require.NoError(t, err)
	tmpCountAfter := 0
	for _, f := range files {
		if f.IsDir() && filepath.Ext(f.Name()) == ".tmp" {
			tmpCountAfter++
		}
	}
	require.Equal(t, 0, tmpCountAfter, "expected all .tmp directories to be cleaned up immediately at startup")

	// Stop the ingester
	require.Nil(t, services.StopAndAwaitTerminated(context.Background(), i))
}
