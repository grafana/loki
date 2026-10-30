package ingester

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"context"

	gokitlog "github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v4/pkg/chunkenc"
	"github.com/grafana/loki/v4/pkg/compression"
	"github.com/grafana/loki/v4/pkg/distributor/writefailures"
	"github.com/grafana/loki/v4/pkg/ingester/client"
	"github.com/grafana/loki/v4/pkg/ingester/wal"
	"github.com/grafana/loki/v4/pkg/iter"
	"github.com/grafana/loki/v4/pkg/logproto"
	"github.com/grafana/loki/v4/pkg/logql"
	"github.com/grafana/loki/v4/pkg/logql/log"
	"github.com/grafana/loki/v4/pkg/logql/syntax"
	"github.com/grafana/loki/v4/pkg/runtime"
	"github.com/grafana/loki/v4/pkg/storage/chunk"
	"github.com/grafana/loki/v4/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v4/pkg/storage/config"
	"github.com/grafana/loki/v4/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v4/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	storagetypes "github.com/grafana/loki/v4/pkg/storage/types"
	"github.com/grafana/loki/v4/pkg/util"
	"github.com/grafana/loki/v4/pkg/util/constants"
	"github.com/grafana/loki/v4/pkg/validation"
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
func (fullWAL) IsDiskThrottled() bool   { return false }

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

// Benchmark_EncodeChunk reports the cost of encoding one full closed chunk — an upper
// bound on how long encode would hold stream.chunkMtx after locking around encodeChunk.
func Benchmark_EncodeChunk(b *testing.B) {
	ctx := user.InjectOrgID(context.Background(), "foo")
	ing := &Ingester{
		cfg:     *dummyConf(),
		logger:  gokitlog.NewNopLogger(),
		metrics: NilMetrics,
	}
	desc := buildChunkDesc(b)
	lbs := labels.FromStrings("app", "bench")
	labelsBuilder := labels.NewBuilder(lbs)
	labelsBuilder.Set(nameLabel, logsValue)
	metric := labelsBuilder.Labels()

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		firstTime, lastTime := util.RoundToMilliseconds(desc.chunk.Bounds())
		ch := chunk.NewChunk(
			"foo", 0, metric,
			chunkenc.NewFacade(desc.chunk, ing.cfg.BlockSize, ing.cfg.TargetChunkSize),
			firstTime,
			lastTime,
		)
		require.NoError(b, ing.encodeChunk(ctx, &ch, desc))
	}
}

// Benchmark_PushDuringEncode measures Push on a stream while another goroutine
// repeatedly encodes a closed filled chunk. unlocked_encode matches today's
// racey flush (encode without chunkMtx); locked_encode is the post-fix shape.
func Benchmark_PushDuringEncode(b *testing.B) {
	for _, tc := range []struct {
		name   string
		locked bool
	}{
		{name: "unlocked_encode", locked: false},
		{name: "locked_encode", locked: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkPushDuringEncode(b, tc.locked)
		})
	}
}

func benchmarkPushDuringEncode(b *testing.B, lockDuringEncode bool) {
	ls := labels.FromStrings(
		"namespace", "loki-dev",
		"cluster", "dev-us-central1",
		"job", "loki-dev/ingester",
		"container", "ingester",
	)

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(b, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})
	chunkfmt, headfmt := defaultChunkFormat(b)
	retentionHours := util.RetentionHours(limiter.limits.RetentionPeriod("fake"))
	s := newStream(chunkfmt, headfmt, &Config{MaxChunkAge: 24 * time.Hour}, limiter.rateLimitStrategy, "fake", model.Fingerprint(0), ls, NewStreamRateCalculator(), NilMetrics, nil, nil, retentionHours, noPolicy)

	ing := &Ingester{
		cfg:     *dummyConf(),
		logger:  gokitlog.NewNopLogger(),
		metrics: NilMetrics,
	}
	desc := buildChunkDesc(b)
	labelsBuilder := labels.NewBuilder(ls)
	labelsBuilder.Set(nameLabel, logsValue)
	metric := labelsBuilder.Labels()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var encodes atomic.Int64
	var encodeErr atomic.Error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			firstTime, lastTime := util.RoundToMilliseconds(desc.chunk.Bounds())
			ch := chunk.NewChunk(
				"fake", s.fp, metric,
				chunkenc.NewFacade(desc.chunk, ing.cfg.BlockSize, ing.cfg.TargetChunkSize),
				firstTime,
				lastTime,
			)

			if lockDuringEncode {
				s.chunkMtx.Lock()
			}
			err := ing.encodeChunk(ctx, &ch, desc)
			if lockDuringEncode {
				s.chunkMtx.Unlock()
			}
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				encodeErr.Store(err)
				cancel()
				return
			}
			encodes.Add(1)
		}
	}()

	pushCtx := context.Background()
	e := entries(10, time.Now())
	b.ReportAllocs()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		rec := recordPool.GetRecord()
		_, err := s.Push(pushCtx, e, rec, 0, true, false, nil, "loki")
		recordPool.PutRecord(rec)
		if err != nil {
			b.Fatal(err)
		}
		// Advance timestamps so subsequent pushes are not duplicates.
		for i := range e {
			e[i].Timestamp = e[i].Timestamp.Add(time.Millisecond)
		}
	}

	b.StopTimer()
	cancel()
	wg.Wait()
	require.NoError(b, encodeErr.Load())
	b.ReportMetric(float64(encodes.Load())/float64(b.N), "encodes/op")
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
		require.NoError(t, ing.doFlushOp(gokitlog.NewNopLogger(), &flushOp{
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
		require.EqualError(t, ing.doFlushOp(gokitlog.NewNopLogger(), &flushOp{
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
		res[i] = buildChunkDesc(t)
	}
	return res
}

func buildChunkDesc(t testing.TB) *chunkDesc {
	t.Helper()
	desc := &chunkDesc{
		closed: true,
		chunk:  chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.Snappy, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, dummyConf().BlockSize, dummyConf().TargetChunkSize),
	}
	fillChunk(t, desc.chunk)
	require.NoError(t, desc.chunk.Close())
	return desc
}

func TestMaybeSetIngestedAt(t *testing.T) {
	v14 := []config.PeriodConfig{{From: config.DayTime{Time: 0}, IndexType: storagetypes.IndexTypeTSDB, Schema: "v14"}}
	v13 := []config.PeriodConfig{{From: config.DayTime{Time: 0}, IndexType: storagetypes.IndexTypeTSDB, Schema: "v13"}}

	backfill := labels.FromStrings("app", "foo", constants.BackfillLabel, "true")
	live := labels.FromStrings("app", "foo")

	for _, tc := range []struct {
		name    string
		periods []config.PeriodConfig
		metric  labels.Labels
		wantSet bool
	}{
		{"backfill stream under v14 records ingestion time", v14, backfill, true},
		{"live stream under v14 stays zero", v14, live, false},
		{"backfill stream under v13 stays zero", v13, backfill, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := &Ingester{periodicConfigs: tc.periods}
			ch := chunk.Chunk{Metric: tc.metric}

			before := model.Now()
			i.maybeSetIngestedAt(&ch, model.Now())

			if tc.wantSet {
				require.NotEqual(t, model.Time(0), ch.IngestedAt)
				require.GreaterOrEqual(t, int64(ch.IngestedAt), int64(before))
			} else {
				require.Equal(t, model.Time(0), ch.IngestedAt)
			}
		})
	}
}

func TestFlushChunksSetsIngestedAtForBackfill(t *testing.T) {
	for _, tc := range []struct {
		name    string
		labels  labels.Labels
		wantSet bool
	}{
		{"backfill stream", labels.FromStrings("app", "foo", constants.BackfillLabel, "true"), true},
		{"live stream", labels.FromStrings("app", "foo"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ing := newTestStore(t, defaultIngesterTestConfig(t), nil)
			// Use a v14 period so the chunk's index would persist IngestedAt.
			ing.periodicConfigs = []config.PeriodConfig{{From: config.DayTime{Time: 0}, IndexType: storagetypes.IndexTypeTSDB, Schema: "v14"}}
			ctx := user.InjectOrgID(context.Background(), "foo")

			var captured []chunk.Chunk
			store.onPut = func(_ context.Context, chunks []chunk.Chunk) error {
				captured = append(captured, chunks...)
				return nil
			}

			before := model.Now()
			require.NoError(t, ing.flushChunks(ctx, 0, tc.labels, buildChunkDecs(t), &sync.RWMutex{}))

			require.NotEmpty(t, captured)
			for _, c := range captured {
				if tc.wantSet {
					require.NotEqual(t, model.Time(0), c.IngestedAt)
					require.GreaterOrEqual(t, int64(c.IngestedAt), int64(before))
				} else {
					require.Equal(t, model.Time(0), c.IngestedAt)
				}
			}
		})
	}
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
	instance.ownedStreamsSvc.trackStreamOwnership(fingerprint, false, noPolicy)

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

func TestParseFlushTenantParams(t *testing.T) {
	for _, tc := range []struct {
		name               string
		query              string
		expectedErr        string
		expectedTenant     string
		expectedMatchers   string
		expectedFlushIndex bool
	}{
		{name: "tenant only", query: "tenant=foo", expectedTenant: "foo"},
		{name: "tenant and matchers", query: `tenant=foo&matchers={app="a"}`, expectedTenant: "foo", expectedMatchers: `{app="a"}`},
		{name: "bare flush_index", query: "tenant=foo&flush_index", expectedTenant: "foo", expectedFlushIndex: true},
		{name: "flush_index true", query: "tenant=foo&flush_index=true", expectedTenant: "foo", expectedFlushIndex: true},
		{name: "flush_index false", query: "tenant=foo&flush_index=false", expectedTenant: "foo"},
		{name: "all params", query: `tenant=foo&matchers={app="a"}&flush_index`, expectedTenant: "foo", expectedMatchers: `{app="a"}`, expectedFlushIndex: true},
		{name: "no params", expectedErr: "the tenant param is required"},
		{name: "empty tenant", query: "tenant=", expectedErr: "the tenant param is required"},
		{name: "matchers without tenant", query: `matchers={app="a"}`, expectedErr: "the tenant param is required"},
		{name: "invalid matchers", query: "tenant=foo&matchers=not-a-matcher", expectedErr: "invalid matchers param"},
		{name: "invalid flush_index", query: "tenant=foo&flush_index=maybe", expectedErr: "invalid flush_index param"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, err := url.ParseQuery(tc.query)
			require.NoError(t, err)

			tenantID, matchers, flushIndex, err := parseFlushTenantParams(q)
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expectedTenant, tenantID)
			require.Equal(t, tc.expectedFlushIndex, flushIndex)
			if tc.expectedMatchers == "" {
				require.Empty(t, matchers)
				return
			}
			expected, err := syntax.ParseMatchers(tc.expectedMatchers, true)
			require.NoError(t, err)
			require.Equal(t, expected, matchers)
		})
	}
}

func TestFlushTenantHandler(t *testing.T) {
	const userID = "testUser"

	for _, tc := range []struct {
		name string
		// pushStreams are pushed for userID before the request (built with makeStream).
		pushStreams []logproto.Stream
		// query is the raw query string of the request.
		query string

		expectedFlushStatusCode int
		// expectedStreams are the label sets expected to have been flushed to the store.
		expectedStreams []string
		// expectedFlushes is the number of forced index ships (FlushIndexes calls).
		expectedFlushes int32
	}{
		{
			name:                    "matchers scope flush to matching streams",
			pushStreams:             []logproto.Stream{makeStream(`{app="a"}`, 1), makeStream(`{app="b"}`, 1)},
			query:                   "tenant=" + userID + "&matchers=" + url.QueryEscape(`{app="a"}`) + "&flush_index",
			expectedFlushStatusCode: http.StatusNoContent,
			expectedStreams:         []string{`{app="a"}`},
			expectedFlushes:         1,
		},
		{
			name:                    "no matchers flushes the whole tenant",
			pushStreams:             []logproto.Stream{makeStream(`{app="a"}`, 1), makeStream(`{app="b"}`, 1)},
			query:                   "tenant=" + userID + "&flush_index=true",
			expectedFlushStatusCode: http.StatusNoContent,
			expectedStreams:         []string{`{app="a"}`, `{app="b"}`},
			expectedFlushes:         1,
		},
		{
			name:                    "the index is not flushed by default",
			pushStreams:             []logproto.Stream{makeStream(`{app="a"}`, 1)},
			query:                   "tenant=" + userID,
			expectedFlushStatusCode: http.StatusNoContent,
			expectedStreams:         []string{`{app="a"}`},
		},
		{
			name:                    "other tenants are not flushed",
			pushStreams:             []logproto.Stream{makeStream(`{app="a"}`, 1)},
			query:                   "tenant=other-tenant",
			expectedFlushStatusCode: http.StatusNoContent,
		},
		{
			name:                    "missing id is a bad request",
			pushStreams:             []logproto.Stream{makeStream(`{app="a"}`, 1)},
			expectedFlushStatusCode: http.StatusBadRequest,
		},
		{
			name:                    "invalid matchers is a bad request",
			query:                   "tenant=" + userID + "&matchers=not-a-matcher",
			expectedFlushStatusCode: http.StatusBadRequest,
		},
		{
			name:                    "invalid flush_index is a bad request",
			query:                   "tenant=" + userID + "&flush_index=maybe",
			expectedFlushStatusCode: http.StatusBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ing := newTestStore(t, defaultIngesterTestConfig(t), nil)
			defer func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) }()

			if len(tc.pushStreams) > 0 {
				_, err := ing.Push(user.InjectOrgID(context.Background(), userID), &logproto.PushRequest{Streams: tc.pushStreams})
				require.NoError(t, err)
			}

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/flush/tenant?"+tc.query, nil)

			ing.FlushTenantHandler(w, r)

			require.Equal(t, tc.expectedFlushStatusCode, w.Code)

			// The flush is only synchronous with flush_index; otherwise it is enqueued
			// and the workers finish it after the handler returns.
			if !strings.Contains(tc.query, "flush_index") {
				require.Eventually(t, func() bool {
					return len(flushedStreams(store, userID)) == len(tc.expectedStreams)
				}, 5*time.Second, 10*time.Millisecond)
			}
			require.ElementsMatch(t, tc.expectedStreams, flushedStreams(store, userID))
			require.Equal(t, tc.expectedFlushes, store.flushIndexCalls.Load())
		})
	}
}

func TestFlushHandler(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string

		expectedStatusCode int
		// expectedFlushed is whether the chunks of every tenant are flushed.
		expectedFlushed bool
		// expectedIndexFlushes is the number of forced index ships (FlushIndexes calls).
		expectedIndexFlushes int32
	}{
		{
			name:               "the index is not flushed by default",
			expectedStatusCode: http.StatusNoContent,
			expectedFlushed:    true,
		},
		{
			name:                 "bare flush_index flushes the index",
			query:                "flush_index",
			expectedStatusCode:   http.StatusNoContent,
			expectedFlushed:      true,
			expectedIndexFlushes: 1,
		},
		{
			name:                 "flush_index=true flushes the index",
			query:                "flush_index=true",
			expectedStatusCode:   http.StatusNoContent,
			expectedFlushed:      true,
			expectedIndexFlushes: 1,
		},
		{
			name:               "flush_index=false does not flush the index",
			query:              "flush_index=false",
			expectedStatusCode: http.StatusNoContent,
			expectedFlushed:    true,
		},
		{
			name:               "invalid flush_index is a bad request",
			query:              "flush_index=maybe",
			expectedStatusCode: http.StatusBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ing := newTestStore(t, defaultIngesterTestConfig(t), nil)
			defer func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) }()

			tenants := []string{"tenant-a", "tenant-b"}
			for _, userID := range tenants {
				_, err := ing.Push(user.InjectOrgID(context.Background(), userID), &logproto.PushRequest{
					Streams: []logproto.Stream{makeStream(`{app="a"}`, 1), makeStream(`{app="b"}`, 1)},
				})
				require.NoError(t, err)
			}

			w := httptest.NewRecorder()
			ing.FlushHandler(w, httptest.NewRequest(http.MethodPost, "/flush?"+tc.query, nil))
			require.Equal(t, tc.expectedStatusCode, w.Code)

			// Without flush_index the flush is only enqueued, so wait for the flush
			// workers to drain it. With flush_index it is complete on return, which
			// the exact assertion below also checks.
			for _, userID := range tenants {
				if tc.expectedFlushed && tc.expectedIndexFlushes == 0 {
					require.Eventually(t, func() bool {
						return len(flushedStreams(store, userID)) == 2
					}, 5*time.Second, 10*time.Millisecond)
				}
				if tc.expectedFlushed {
					require.ElementsMatch(t, []string{`{app="a"}`, `{app="b"}`}, flushedStreams(store, userID))
				} else {
					require.Empty(t, flushedStreams(store, userID))
				}
			}
			require.Equal(t, tc.expectedIndexFlushes, store.flushIndexCalls.Load())
		})
	}
}

func TestFlushStreams(t *testing.T) {
	const userID = "testUser"

	t.Run("synchronous flush covers every stream of every queue", func(t *testing.T) {
		cfg := defaultIngesterTestConfig(t)
		cfg.ConcurrentFlushes = 2
		store, ing := newTestStore(t, cfg, nil)
		defer func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) }()

		numStreams := 300
		streams := make([]logproto.Stream, 0, numStreams)
		for n := 0; n < numStreams; n++ {
			streams = append(streams, makeStream(fmt.Sprintf(`{app="app-%d"}`, n), 1))
		}
		_, err := ing.Push(user.InjectOrgID(context.Background(), userID), &logproto.PushRequest{Streams: streams})
		require.NoError(t, err)

		require.NoError(t, ing.flushStreams(context.Background(), userID, nil, true))
		require.Len(t, flushedStreams(store, userID), numStreams)
	})

	t.Run("an empty tenant flushes all tenants", func(t *testing.T) {
		store, ing := newTestStore(t, defaultIngesterTestConfig(t), nil)
		defer func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) }()

		for _, id := range []string{"tenant-a", "tenant-b"} {
			_, err := ing.Push(user.InjectOrgID(context.Background(), id), &logproto.PushRequest{Streams: []logproto.Stream{makeStream(`{app="a"}`, 1)}})
			require.NoError(t, err)
		}

		require.NoError(t, ing.flushStreams(context.Background(), "", nil, true))
		require.Len(t, flushedStreams(store, "tenant-a"), 1)
		require.Len(t, flushedStreams(store, "tenant-b"), 1)
	})

	t.Run("an unknown tenant is a no-op", func(t *testing.T) {
		_, ing := newTestStore(t, defaultIngesterTestConfig(t), nil)
		defer func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) }()

		require.NoError(t, ing.flushStreams(context.Background(), "no-such-tenant", nil, true))
	})

	t.Run("a flush failure is returned", func(t *testing.T) {
		cfg := defaultIngesterTestConfig(t)
		cfg.FlushOpBackoff.MinBackoff = time.Millisecond
		cfg.FlushOpBackoff.MaxBackoff = time.Millisecond
		cfg.FlushOpBackoff.MaxRetries = 1
		store, ing := newTestStore(t, cfg, nil)
		defer func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) }()

		_, err := ing.Push(user.InjectOrgID(context.Background(), userID), &logproto.PushRequest{Streams: []logproto.Stream{makeStream(`{app="a"}`, 1)}})
		require.NoError(t, err)

		// The shutdown flush retries failed immediate flushes until they succeed, so
		// the store must recover before the ingester is stopped, even if this test fails.
		defer func() { store.onPut = nil }()
		store.onPut = func(context.Context, []chunk.Chunk) error { return errors.New("store down") }

		require.Error(t, ing.flushStreams(context.Background(), userID, nil, true))

		// The failure was reported to the caller, so the op must not be retried in
		// the background even once the store has recovered.
		store.onPut = nil
		require.Never(t, func() bool {
			return len(flushedStreams(store, userID)) > 0
		}, 300*time.Millisecond, 20*time.Millisecond, "a failed op that belongs to a job must not be requeued")
	})

	t.Run("a job merges into an op that is already queued", func(t *testing.T) {
		cfg := defaultIngesterTestConfig(t)
		cfg.ConcurrentFlushes = 1
		store, ing := newTestStore(t, cfg, nil)
		defer func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) }()

		_, err := ing.Push(user.InjectOrgID(context.Background(), userID), &logproto.PushRequest{
			Streams: []logproto.Stream{makeStream(`{app="a"}`, 1), makeStream(`{app="b"}`, 1)},
		})
		require.NoError(t, err)

		// Hold the only worker inside the first Put, so one stream's op is in flight
		// and the other's is queued.
		putStarted, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		var completedPuts atomic.Int32
		store.onPut = func(context.Context, []chunk.Chunk) error {
			once.Do(func() { close(putStarted) })
			<-release
			completedPuts.Add(1)
			return nil
		}

		errCh := make(chan error, 2)
		go func() { errCh <- ing.flushStreams(context.Background(), userID, nil, true) }()
		<-putStarted
		require.Eventually(t, func() bool { return ing.flushQueues[0].Length() == 1 }, 5*time.Second, 5*time.Millisecond)

		// The second job finds the queued op and attaches to it, but can't merge into
		// the one already running, so the queue holds two ops, not three.
		go func() { errCh <- ing.flushStreams(context.Background(), userID, nil, true) }()
		require.Eventually(t, func() bool { return ing.flushQueues[0].Length() == 2 }, 5*time.Second, 5*time.Millisecond)

		close(release)
		require.NoError(t, <-errCh)
		require.NoError(t, <-errCh)
		require.Equal(t, int32(2), completedPuts.Load(), "each stream is flushed once, the second op for the running stream finds nothing to flush")
	})

	t.Run("concurrent jobs flush each stream exactly once", func(t *testing.T) {
		cfg := defaultIngesterTestConfig(t)
		cfg.ConcurrentFlushes = 4
		store, ing := newTestStore(t, cfg, nil)
		defer func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) }()

		const numStreams = 50
		streams := make([]logproto.Stream, 0, numStreams)
		for n := 0; n < numStreams; n++ {
			streams = append(streams, makeStream(fmt.Sprintf(`{app="app-%d"}`, n), 1))
		}
		_, err := ing.Push(user.InjectOrgID(context.Background(), userID), &logproto.PushRequest{Streams: streams})
		require.NoError(t, err)

		var g errgroup.Group
		for n := 0; n < 8; n++ {
			g.Go(func() error { return ing.flushStreams(context.Background(), userID, nil, true) })
		}
		require.NoError(t, g.Wait())
		require.Len(t, flushedStreams(store, userID), numStreams)
	})

	t.Run("a cancelled context stops the wait but not the flush", func(t *testing.T) {
		store, ing := newTestStore(t, defaultIngesterTestConfig(t), nil)
		defer func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) }()

		_, err := ing.Push(user.InjectOrgID(context.Background(), userID), &logproto.PushRequest{Streams: []logproto.Stream{makeStream(`{app="a"}`, 1)}})
		require.NoError(t, err)

		putStarted, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		var completedPuts atomic.Int32
		store.onPut = func(_ context.Context, _ []chunk.Chunk) error {
			once.Do(func() { close(putStarted) })
			<-release
			completedPuts.Add(1)
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() { errCh <- ing.flushStreams(ctx, userID, nil, true) }()

		<-putStarted
		cancel()
		require.ErrorIs(t, <-errCh, context.Canceled)

		// The op was already enqueued, so it still completes once unblocked.
		close(release)
		require.Eventually(t, func() bool {
			return completedPuts.Load() == 1
		}, 5*time.Second, 10*time.Millisecond)
	})
}

func TestFlushRejectedWhenNotRunning(t *testing.T) {
	const userID = "testUser"

	for _, tc := range []struct {
		name string
		// stop moves the ingester into the state under test.
		stop func(t *testing.T, ing *Ingester)
	}{
		{
			name: "shutdown has marked the ingester read-only",
			stop: func(_ *testing.T, ing *Ingester) { ing.stopIncomingRequests() },
		},
		{
			name: "ingester is stopped and its flush queues are closed",
			stop: func(t *testing.T, ing *Ingester) {
				require.NoError(t, services.StopAndAwaitTerminated(context.Background(), ing))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ing := newTestStore(t, defaultIngesterTestConfig(t), nil)
			defer func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) }()

			_, err := ing.Push(user.InjectOrgID(context.Background(), userID), &logproto.PushRequest{
				Streams: []logproto.Stream{makeStream(`{app="a"}`, 1)},
			})
			require.NoError(t, err)

			tc.stop(t, ing)
			putsBefore := len(flushedStreams(store, userID))

			w := httptest.NewRecorder()
			ing.FlushHandler(w, httptest.NewRequest(http.MethodPost, "/flush", nil))
			require.Equal(t, http.StatusServiceUnavailable, w.Code)

			w = httptest.NewRecorder()
			ing.FlushTenantHandler(w, httptest.NewRequest(http.MethodPost, "/flush/tenant?tenant="+userID+"&flush_index", nil))
			require.Equal(t, http.StatusServiceUnavailable, w.Code)

			require.Len(t, flushedStreams(store, userID), putsBefore, "a rejected flush must not flush anything")
			require.Zero(t, store.flushIndexCalls.Load(), "a rejected flush must not flush the index")
		})
	}

	t.Run("ingester has not started", func(t *testing.T) {
		store := &testStore{chunks: map[string][]chunk.Chunk{}}
		limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
		require.NoError(t, err)
		ing, err := New(defaultIngesterTestConfig(t), client.Config{}, store, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokitlog.NewNopLogger(), nil, mockReadRingWithOneActiveIngester(), nil)
		require.NoError(t, err)

		// The flush queues don't exist yet, so enqueueing would panic.
		w := httptest.NewRecorder()
		ing.FlushHandler(w, httptest.NewRequest(http.MethodPost, "/flush", nil))
		require.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

// flushedStreams returns the label sets of the chunks the store received for a tenant.
func flushedStreams(store *testStore, userID string) []string {
	var out []string
	for _, c := range store.getChunksForUser(userID) {
		out = append(out, c.Metric.String())
	}
	return out
}

// makeStream builds a stream with the given labels and numLogs log lines.
func makeStream(labels string, numLogs int) logproto.Stream {
	now := time.Now()
	entries := make([]logproto.Entry, 0, numLogs)
	for i := 0; i < numLogs; i++ {
		entries = append(entries, logproto.Entry{Timestamp: now, Line: fmt.Sprintf("line-%d", i)})
	}
	return logproto.Stream{Labels: labels, Entries: entries}
}

type testStore struct {
	mtx sync.Mutex
	// Chunks keyed by userID.
	chunks map[string][]chunk.Chunk
	onPut  func(ctx context.Context, chunks []chunk.Chunk) error

	// flushIndexCalls counts how many times FlushIndexes was invoked, so tests can
	// assert the handler forces the index ship after flushing chunks.
	flushIndexCalls atomic.Int32
}

func (s *testStore) FlushIndexes(_ context.Context) error {
	s.flushIndexCalls.Add(1)
	return nil
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

	ing, err := New(cfg, client.Config{}, store, limits, runtime.DefaultTenantConfigs(), nil, writefailures.Cfg{}, constants.Loki, gokitlog.NewNopLogger(), nil, readRingMock, nil)
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
		var err error
		chunk.Metric.Range(func(l labels.Label) {
			if err != nil {
				return
			}
			if l.Value == "" {
				err = fmt.Errorf("Chunk has blank label %q", l.Name)
			}
		})
		if err != nil {
			return err
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

func (s *testStore) HasChunkSizingInfo(_, _ model.Time) bool {
	return false
}

func (s *testStore) GetChunkRefsWithSizingInfo(_ context.Context, _ string, _, _ model.Time, _ chunk.Predicate) ([]logproto.ChunkRefWithSizingInfo, error) {
	return nil, nil
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

func (s *testStore) Series(_ context.Context, _ logql.SelectLogParams) ([]logproto.SeriesIdentifier, error) {
	return nil, nil
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
		// Ensure all empty label sets use an empty set of adapters, rather than a nil slice, to make the assertion below easier.
		for _, stream := range expected {
			for i := range stream.Entries {
				if len(stream.Entries[i].Parsed) == 0 {
					stream.Entries[i].Parsed = logproto.EmptyLabelAdapters()
				}

				if len(stream.Entries[i].StructuredMetadata) == 0 {
					stream.Entries[i].StructuredMetadata = logproto.EmptyLabelAdapters()
				}
			}
		}

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

// storedEntries returns the number of entries the store holds per tenant and app.
func storedEntries(store *testStore) map[string]int {
	store.mtx.Lock()
	defer store.mtx.Unlock()

	out := map[string]int{}
	for userID, chunks := range store.chunks {
		for _, c := range chunks {
			out[userID+"/"+c.Metric.Get("app")] += c.Data.(*chunkenc.Facade).LokiChunk().Size()
		}
	}
	return out
}

// FuzzFlushStreams interleaves pushes with concurrent flushes of random tenants
// and streams, some asynchronous and some with an already cancelled context. Each
// input byte is one step. It checks that nothing hangs, panics or fails, and that
// once all flushes have settled every pushed entry is stored exactly once.
func FuzzFlushStreams(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 1, 2, 3, 4, 0, 7, 2, 12, 3})
	f.Add([]byte{0, 5, 10, 2, 2, 3, 4, 1, 6, 2, 90, 3, 4, 0, 2})

	f.Fuzz(func(t *testing.T, steps []byte) {
		tenants := []string{"t0", "t1"}
		apps := []string{"a0", "a1", "a2"}

		cfg := defaultIngesterTestConfig(t)
		cfg.ConcurrentFlushes = 1 + len(steps)%3
		store, ing := newTestStore(t, cfg, nil)
		defer func() { _ = services.StopAndAwaitTerminated(context.Background(), ing) }()

		pushed := map[string]int{}
		var wg sync.WaitGroup

		// settle waits for the running flushes, then flushes everything and checks
		// that every pushed entry has been stored once. Pushing while a flush is
		// running can flush a chunk twice, an existing bug unrelated to flush jobs,
		// so pushes only happen on a settled ingester.
		settle := func() {
			wg.Wait()
			require.NoError(t, ing.flushStreams(context.Background(), "", nil, true))
			require.Equal(t, pushed, storedEntries(store))
		}

		for i, b := range steps {
			kind := int(b) % 5
			tenant := tenants[int(b/5)%len(tenants)]
			app := apps[int(b/10)%len(apps)]

			if kind < 2 {
				if i > 0 && int(steps[i-1])%5 >= 2 {
					settle()
				}
				_, err := ing.Push(user.InjectOrgID(context.Background(), tenant), &logproto.PushRequest{
					Streams: []logproto.Stream{{
						Labels:  fmt.Sprintf(`{app="%s"}`, app),
						Entries: []logproto.Entry{{Timestamp: time.Unix(0, int64(i+1)), Line: fmt.Sprintf("line-%d", i)}},
					}},
				})
				require.NoError(t, err)
				pushed[tenant+"/"+app]++
				continue
			}

			// kind 2 is a synchronous flush, 3 an asynchronous one and 4 a
			// synchronous one whose context is already cancelled. An odd byte
			// restricts the flush to one app of the tenant, an even one flushes
			// all tenants.
			var flushTenant string
			var matchers []*labels.Matcher
			if b%2 == 1 {
				flushTenant = tenant
				matchers = []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", app)}
			}
			ctx := context.Background()
			if kind == 4 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := ing.flushStreams(ctx, flushTenant, matchers, kind != 3); err != nil && !errors.Is(err, context.Canceled) {
					t.Errorf("flushStreams: %v", err)
				}
			}()
		}

		settle()
	})
}
