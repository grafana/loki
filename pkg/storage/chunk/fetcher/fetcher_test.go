package fetcher

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/congestion"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

func Test(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name               string
		handoff            time.Duration
		skipQueryWriteback time.Duration
		storeStart         []chunk.Chunk
		l1Start            []chunk.Chunk
		l2Start            []chunk.Chunk
		fetch              []chunk.Chunk
		l1KeysRequested    int
		l1End              []chunk.Chunk
		l2KeysRequested    int
		l2End              []chunk.Chunk
	}{
		{
			name:            "all found in L1 cache",
			handoff:         0,
			storeStart:      []chunk.Chunk{},
			l1Start:         makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2Start:         []chunk.Chunk{},
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1KeysRequested: 3,
			l1End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2End:           []chunk.Chunk{},
		},
		{
			name:            "all found in L2 cache",
			handoff:         1, // Only needs to be greater than zero so that we check L2 cache
			storeStart:      []chunk.Chunk{},
			l1Start:         []chunk.Chunk{},
			l2Start:         makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1End:           []chunk.Chunk{},
			l2KeysRequested: 3,
			l2End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
		},
		{
			name:            "some in L1, some in L2",
			handoff:         5 * time.Hour,
			storeStart:      []chunk.Chunk{},
			l1Start:         makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2Start:         makeChunks(now, c{7 * time.Hour, 8 * time.Hour}, c{8 * time.Hour, 9 * time.Hour}, c{9 * time.Hour, 10 * time.Hour}),
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{7 * time.Hour, 8 * time.Hour}, c{8 * time.Hour, 9 * time.Hour}, c{9 * time.Hour, 10 * time.Hour}),
			l1KeysRequested: 3,
			l1End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2KeysRequested: 3,
			l2End:           makeChunks(now, c{7 * time.Hour, 8 * time.Hour}, c{8 * time.Hour, 9 * time.Hour}, c{9 * time.Hour, 10 * time.Hour}),
		},
		{
			name:            "some in L1, some in L2, some in store",
			handoff:         5 * time.Hour,
			storeStart:      makeChunks(now, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{8 * time.Hour, 9 * time.Hour}, c{9 * time.Hour, 10 * time.Hour}),
			l1Start:         makeChunks(now, c{time.Hour, 2 * time.Hour}),
			l2Start:         makeChunks(now, c{7 * time.Hour, 8 * time.Hour}),
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{7 * time.Hour, 8 * time.Hour}, c{8 * time.Hour, 9 * time.Hour}, c{9 * time.Hour, 10 * time.Hour}),
			l1KeysRequested: 3,
			l1End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2KeysRequested: 3,
			l2End:           makeChunks(now, c{7 * time.Hour, 8 * time.Hour}, c{8 * time.Hour, 9 * time.Hour}, c{9 * time.Hour, 10 * time.Hour}),
		},
		{
			name:               "skipQueryWriteback",
			handoff:            24 * time.Hour,
			skipQueryWriteback: 3 * 24 * time.Hour,
			storeStart:         makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{5 * 24 * time.Hour, 6 * 24 * time.Hour}, c{5 * 24 * time.Hour, 6 * 24 * time.Hour}),
			l1Start:            []chunk.Chunk{},
			l2Start:            []chunk.Chunk{},
			fetch:              makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{5 * 24 * time.Hour, 6 * 24 * time.Hour}, c{5 * 24 * time.Hour, 6 * 24 * time.Hour}),
			l1KeysRequested:    3,
			l1End:              makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2KeysRequested:    0,
			l2End:              []chunk.Chunk{},
		},
		{
			name:            "writeback l1",
			handoff:         24 * time.Hour,
			storeStart:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1Start:         []chunk.Chunk{},
			l2Start:         []chunk.Chunk{},
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1KeysRequested: 3,
			l1End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2End:           []chunk.Chunk{},
		},
		{
			name:            "writeback l2",
			handoff:         24 * time.Hour,
			storeStart:      makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1Start:         []chunk.Chunk{},
			l2Start:         []chunk.Chunk{},
			fetch:           makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1End:           []chunk.Chunk{},
			l2KeysRequested: 3,
			l2End:           makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
		},
		{
			name:            "writeback l1 and l2",
			handoff:         24 * time.Hour,
			storeStart:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1Start:         []chunk.Chunk{},
			l2Start:         []chunk.Chunk{},
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1KeysRequested: 3,
			l1End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2KeysRequested: 3,
			l2End:           makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
		},
		{
			name:            "verify l1 skip optimization",
			handoff:         24 * time.Hour,
			storeStart:      makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1Start:         []chunk.Chunk{},
			l2Start:         []chunk.Chunk{},
			fetch:           makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1KeysRequested: 0,
			l1End:           []chunk.Chunk{},
			l2KeysRequested: 3,
			l2End:           makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
		},
		{
			name:            "verify l1 skip optimization plus extended",
			handoff:         20 * time.Hour, // 20 hours, 10% extension should be 22 hours
			storeStart:      makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1Start:         makeChunks(now, c{20 * time.Hour, 21 * time.Hour}, c{21 * time.Hour, 22 * time.Hour}, c{22 * time.Hour, 23 * time.Hour}),
			l2Start:         makeChunks(now, c{21 * time.Hour, 22 * time.Hour}, c{22 * time.Hour, 23 * time.Hour}),
			fetch:           makeChunks(now, c{20 * time.Hour, 21 * time.Hour}, c{21 * time.Hour, 22 * time.Hour}, c{22 * time.Hour, 23 * time.Hour}),
			l1KeysRequested: 2,
			l1End:           makeChunks(now, c{20 * time.Hour, 21 * time.Hour}, c{21 * time.Hour, 22 * time.Hour}, c{22 * time.Hour, 23 * time.Hour}),
			l2KeysRequested: 1, // We won't look for the extended handoff key in L2, so only one lookup should go to L2
			l2End:           makeChunks(now, c{21 * time.Hour, 22 * time.Hour}, c{22 * time.Hour, 23 * time.Hour}),
		},
		{
			name:            "verify l2 skip optimization",
			handoff:         24 * time.Hour,
			storeStart:      makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1Start:         makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2Start:         []chunk.Chunk{},
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1KeysRequested: 3,
			l1End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2KeysRequested: 0,
			l2End:           []chunk.Chunk{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c1 := cache.NewMockCache()
			c2 := cache.NewMockCache()
			s := testutils.NewMockStorage()
			sc := config.SchemaConfig{
				Configs: s.GetSchemaConfigs(),
			}
			chunkClient := client.NewClientWithMaxParallel(s, nil, 1, sc)

			// Prepare l1 cache
			keys := make([]string, 0, len(test.l1Start))
			chunks := make([][]byte, 0, len(test.l1Start))
			for _, c := range test.l1Start {
				// Encode first to set the checksum
				b, err := c.Encoded()
				assert.NoError(t, err)

				k := sc.ExternalKey(c.ChunkRef)
				keys = append(keys, k)
				chunks = append(chunks, b)
			}
			assert.NoError(t, c1.Store(context.Background(), keys, chunks))

			// Prepare l2 cache
			keys = make([]string, 0, len(test.l2Start))
			chunks = make([][]byte, 0, len(test.l2Start))
			for _, c := range test.l2Start {
				b, err := c.Encoded()
				assert.NoError(t, err)

				k := sc.ExternalKey(c.ChunkRef)
				keys = append(keys, k)
				chunks = append(chunks, b)
			}
			assert.NoError(t, c2.Store(context.Background(), keys, chunks))

			// Prepare store
			assert.NoError(t, chunkClient.PutChunks(context.Background(), test.storeStart))

			// Build fetcher
			f, err := New(c1, c2, false, sc, chunkClient, test.handoff, test.skipQueryWriteback, false)
			assert.NoError(t, err)

			// Run the test
			chks, err := f.FetchChunks(context.Background(), test.fetch)
			assert.NoError(t, err)
			assertChunks(t, test.fetch, chks)
			l1actual, err := makeChunksFromMapKeys(c1.GetKeys())
			assert.NoError(t, err)
			assert.Equal(t, test.l1KeysRequested, c1.KeysRequested())
			assertChunks(t, test.l1End, l1actual)
			l2actual, err := makeChunksFromMapKeys(c2.GetKeys())
			assert.NoError(t, err)
			assert.Equal(t, test.l2KeysRequested, c2.KeysRequested())
			assertChunks(t, test.l2End, l2actual)
		})
	}
}

func TestFetchChunks_CacheDecodeIsNotLoggedAsDownloadFailure(t *testing.T) {
	sc := testutils.SchemaConfig("inmemory", "v11", model.Now().Add(-100*24*time.Hour))
	chunks := makeChunks(time.Now(), c{time.Hour, 2 * time.Hour})

	l1 := cache.NewMockCache()
	l2 := cache.NewMockCache()
	chunkClient := client.NewClientWithMaxParallel(testutils.NewInMemoryObjectClient(), nil, 1, sc)
	require.NoError(t, chunkClient.PutChunks(context.Background(), chunks))

	key := sc.ExternalKey(chunks[0].ChunkRef)
	require.NoError(t, l1.Store(context.Background(), []string{key}, [][]byte{[]byte("not a chunk")}))

	f, err := New(l1, l2, false, sc, chunkClient, 0, 0, true)
	require.NoError(t, err)
	t.Cleanup(f.Stop)

	beforeFailures := readStorageErrorCounters(t)

	statsCtx, ctx := stats.NewContext(context.Background())
	got, err := f.FetchChunks(ctx, chunks)
	require.NoError(t, err)
	require.Empty(t, got)

	require.Empty(t, storageErrorCounterDeltas(t, beforeFailures))
	// Cache decode failures are silently dropped (never retried from storage,
	// see processCacheResponse), so they aren't counted as chunk fetch failures.
	require.Equal(t, int64(0), statsCtx.Store().ChunkFetchFailures)
}

func TestFetchChunks_HandlesStorageErrors(t *testing.T) {
	storageErr := errors.New("storage failed")
	tests := []struct {
		name       string
		client     *storageErrorClient
		wantReason string
	}{
		{name: "not found", client: &storageErrorClient{err: storageErr, notFound: true, retryable: true}, wantReason: storageErrorNotFound},
		{name: "retryable", client: &storageErrorClient{err: storageErr, retryable: true}, wantReason: storageErrorRetryable},
		{name: "other", client: &storageErrorClient{err: storageErr}, wantReason: storageErrorOther},
		{name: "checksum", client: &storageErrorClient{err: fmt.Errorf("decode chunk: %w", chunk.ErrInvalidChecksum)}, wantReason: storageErrorOther},
		{name: "chunkenc checksum", client: &storageErrorClient{err: fmt.Errorf("decode chunk: %w", chunkenc.ErrInvalidChecksum)}, wantReason: storageErrorOther},
		{name: "retries exceeded", client: &storageErrorClient{err: congestion.RetriesExceeded}, wantReason: storageErrorRetryable},
		{name: "canceled", client: &storageErrorClient{err: context.Canceled}},
		{name: "deadline", client: &storageErrorClient{err: context.DeadlineExceeded}},
		{name: "no error", client: &storageErrorClient{}},
	}

	for _, test := range tests {
		for _, propagate := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/propagate=%t", test.name, propagate), func(t *testing.T) {
				chunks := makeChunks(time.Now(), c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour})
				test.client.chunks = chunks
				if test.client.err != nil {
					test.client.chunks = chunks[:1]
				}
				f, err := New(cache.NewMockCache(), cache.NewMockCache(), false, testSchemaConfig(), test.client, 0, 0, propagate)
				require.NoError(t, err)
				t.Cleanup(f.Stop)

				before := readStorageErrorCounters(t)
				statsCtx, ctx := stats.NewContext(context.Background())
				got, err := f.FetchChunks(ctx, chunks)

				if propagate && test.client.err != nil {
					require.ErrorIs(t, err, test.client.err)
					require.Nil(t, got)
				} else {
					require.NoError(t, err)
					require.Equal(t, test.client.chunks, got)
				}
				if test.wantReason == "" {
					require.Empty(t, storageErrorCounterDeltas(t, before))
				} else {
					require.Equal(t, map[string]float64{test.wantReason: 1}, storageErrorCounterDeltas(t, before))
				}

				// One of the two requested chunks fails whenever the client
				// returns an error, except cancellation/deadline: those aren't
				// counted as data-loss failures.
				wantFailures := int64(0)
				if test.client.err != nil && test.wantReason != "" {
					wantFailures = 1
				}
				require.Equal(t, wantFailures, statsCtx.Store().ChunkFetchFailures)
			})
		}
	}
}

func TestFetchChunksTracing(t *testing.T) {
	previousProvider := otel.GetTracerProvider()
	previousTracer := tracer
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	tracer = provider.Tracer("pkg/storage/chunk/fetcher")
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		tracer = previousTracer
		_ = provider.Shutdown(context.Background())
	})

	t.Run("aggregate attributes stay bounded", func(t *testing.T) {
		for _, count := range []int{2, 64} {
			t.Run(strconv.Itoa(count), func(t *testing.T) {
				now := time.Now()
				fetch := make([]chunk.Chunk, 0, count)
				for i := 0; i < count; i++ {
					fetch = append(fetch, makeChunks(now, c{time.Duration(i) * time.Hour, time.Duration(i+1) * time.Hour})...)
				}
				sc := testSchemaConfig()
				rawCache := cache.NewMockCache()
				cacheChunks := fetch[:count/2]
				storageChunks := fetch[count/2:]
				keys := make([]string, 0, len(cacheChunks))
				bufs := make([][]byte, 0, len(cacheChunks))
				for _, chk := range cacheChunks {
					buf, err := chk.Encoded()
					require.NoError(t, err)
					keys = append(keys, sc.ExternalKey(chk.ChunkRef))
					bufs = append(bufs, buf)
				}
				require.NoError(t, rawCache.Store(context.Background(), keys, bufs))
				instrumentedCache := cache.Instrument("fetcher-test", rawCache, prometheus.NewRegistry())
				rawStorage := testutils.NewInMemoryObjectClient()
				storage := client.NewClientWithMaxParallel(rawStorage, nil, 1, sc)
				require.NoError(t, storage.PutChunks(context.Background(), storageChunks))
				fetcher, err := New(instrumentedCache, cache.NewMockCache(), false, sc, storage, 0, 0, false)
				require.NoError(t, err)
				t.Cleanup(fetcher.Stop)

				recorder.Reset()
				got, err := fetcher.FetchChunks(context.Background(), fetch)
				require.NoError(t, err)
				require.Len(t, got, count)

				spans := recorder.Ended()
				require.Len(t, spans, 1)
				span := spans[0]
				require.Equal(t, "ChunkStore.FetchChunks", span.Name())
				require.Empty(t, span.Events())
				require.Equal(t, codes.Unset, span.Status().Code)
				require.Len(t, span.Attributes(), 10)
				attrs := spanAttributeInts(span)
				require.Equal(t, int64(count), attrs[traceRequestedChunks])
				require.Equal(t, int64(count), attrs[traceReturnedChunks])
				require.Equal(t, int64(count/2), attrs[traceCacheHits])
				require.Equal(t, int64(count/2), attrs[traceCacheMisses])
				require.Greater(t, attrs[traceCacheBytes], int64(0))
				require.Equal(t, int64(count/2), attrs[traceStorageRequestedChunks])
				require.Equal(t, int64(count/2), attrs[traceStorageFetchedChunks])
				require.Greater(t, attrs[traceStorageBytes], int64(0))
				require.Equal(t, int64(0), attrs[traceCacheErrors])
				require.Equal(t, int64(0), attrs[traceStorageErrors])
			})
		}
	})

	t.Run("propagated storage error", func(t *testing.T) {
		recorder.Reset()
		chunks := makeChunks(time.Now(), c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour})
		storageErr := errors.New("storage failed")
		storage := &storageErrorClient{err: storageErr, chunks: chunks[:1]}
		fetcher, err := New(cache.NewMockCache(), cache.NewMockCache(), false, testSchemaConfig(), storage, 0, 0, true)
		require.NoError(t, err)
		t.Cleanup(fetcher.Stop)

		got, err := fetcher.FetchChunks(context.Background(), chunks)
		require.ErrorIs(t, err, storageErr)
		require.Nil(t, got)

		spans := recorder.Ended()
		require.Len(t, spans, 1)
		span := spans[0]
		require.Equal(t, codes.Error, span.Status().Code)
		require.Equal(t, storageErr.Error(), span.Status().Description)
		var recordedException bool
		for _, event := range span.Events() {
			if event.Name == "exception" {
				recordedException = true
			}
			require.NotContains(t, event.Name, "chunk")
		}
		require.True(t, recordedException)
		attrs := spanAttributeInts(span)
		require.Equal(t, int64(2), attrs[traceRequestedChunks])
		require.Equal(t, int64(0), attrs[traceReturnedChunks])
		require.Equal(t, int64(2), attrs[traceCacheMisses])
		require.Equal(t, int64(2), attrs[traceStorageRequestedChunks])
		require.Equal(t, int64(1), attrs[traceStorageFetchedChunks])
		require.Equal(t, int64(1), attrs[traceStorageErrors])
	})

	t.Run("cache store failure is aggregated without changing fetch result", func(t *testing.T) {
		recorder.Reset()
		chunks := makeChunks(time.Now(), c{time.Hour, 2 * time.Hour})
		sc := testSchemaConfig()
		rawStorage := testutils.NewInMemoryObjectClient()
		storage := client.NewClientWithMaxParallel(rawStorage, nil, 1, sc)
		require.NoError(t, storage.PutChunks(context.Background(), chunks))
		storeErr := errors.New("cache store failed")
		failingCache := cache.Instrument("fetcher-test-store-error", &storeErrorCache{Cache: cache.NewMockCache(), err: storeErr}, prometheus.NewRegistry())
		fetcher, err := New(failingCache, cache.NewMockCache(), false, sc, storage, 0, 0, false)
		require.NoError(t, err)
		t.Cleanup(fetcher.Stop)

		got, err := fetcher.FetchChunks(context.Background(), chunks)
		require.NoError(t, err)
		assertChunks(t, chunks, got)

		spans := recorder.Ended()
		require.Len(t, spans, 1)
		span := spans[0]
		require.Equal(t, "ChunkStore.FetchChunks", span.Name())
		require.Equal(t, codes.Unset, span.Status().Code)
		require.Empty(t, span.Events())
		require.Len(t, span.Attributes(), 10)
		attrs := spanAttributeInts(span)
		require.Equal(t, int64(1), attrs[traceRequestedChunks])
		require.Equal(t, int64(1), attrs[traceReturnedChunks])
		require.Equal(t, int64(0), attrs[traceCacheHits])
		require.Equal(t, int64(1), attrs[traceCacheMisses])
		require.Equal(t, int64(0), attrs[traceCacheBytes])
		require.Equal(t, int64(1), attrs[traceStorageRequestedChunks])
		require.Equal(t, int64(1), attrs[traceStorageFetchedChunks])
		require.Greater(t, attrs[traceStorageBytes], int64(0))
		require.Equal(t, int64(1), attrs[traceCacheErrors])
		require.Equal(t, int64(0), attrs[traceStorageErrors])
	})
}

func spanAttributeInts(span sdktrace.ReadOnlySpan) map[string]int64 {
	attrs := make(map[string]int64, len(span.Attributes()))
	for _, attr := range span.Attributes() {
		attrs[string(attr.Key)] = attr.Value.AsInt64()
	}
	return attrs
}

func readStorageErrorCounters(t *testing.T) map[string]float64 {
	t.Helper()

	out := make(map[string]float64, 3)
	for _, reason := range []string{storageErrorNotFound, storageErrorRetryable, storageErrorOther} {
		out[reason] = testutil.ToFloat64(storageErrors.WithLabelValues(reason))
	}
	return out
}

func storageErrorCounterDeltas(t *testing.T, before map[string]float64) map[string]float64 {
	t.Helper()

	out := map[string]float64{}
	for reason, after := range readStorageErrorCounters(t) {
		if delta := after - before[reason]; delta != 0 {
			out[reason] = delta
		}
	}
	return out
}

type storageErrorClient struct {
	client.Client
	err                 error
	chunks              []chunk.Chunk
	notFound, retryable bool
}

type storeErrorCache struct {
	cache.Cache
	err error
}

func (c *storeErrorCache) Store(context.Context, []string, [][]byte) error {
	return c.err
}

func (s *storageErrorClient) GetChunks(context.Context, []chunk.Chunk) ([]chunk.Chunk, error) {
	return s.chunks, s.err
}

func (s *storageErrorClient) IsChunkNotFoundErr(error) bool { return s.notFound }
func (s *storageErrorClient) IsRetryableErr(error) bool     { return s.retryable }

func testSchemaConfig() config.SchemaConfig {
	return testutils.SchemaConfig("inmemory", "v11", model.Now().Add(-100*24*time.Hour))
}

func BenchmarkFetch(b *testing.B) {
	now := time.Now()

	numchunks := 100
	l1Start := make([]chunk.Chunk, 0, numchunks/3)
	for i := 0; i < numchunks/3; i++ {
		l1Start = append(l1Start, makeChunks(now, c{time.Duration(i) * time.Hour, time.Duration(i+1) * time.Hour})...)
	}
	l2Start := make([]chunk.Chunk, 0, numchunks/3)
	for i := numchunks/3 + 1000; i < (numchunks/3)+numchunks/3+1000; i++ {
		l2Start = append(l2Start, makeChunks(now, c{time.Duration(i) * time.Hour, time.Duration(i+1) * time.Hour})...)
	}
	storeStart := make([]chunk.Chunk, 0, numchunks/3)
	for i := numchunks/3 + 10000; i < (numchunks/3)+numchunks/3+10000; i++ {
		storeStart = append(storeStart, makeChunks(now, c{time.Duration(i) * time.Hour, time.Duration(i+1) * time.Hour})...)
	}
	fetch := make([]chunk.Chunk, 0, numchunks)
	fetch = append(fetch, l1Start...)
	fetch = append(fetch, l2Start...)
	fetch = append(fetch, storeStart...)

	test := struct {
		name               string
		handoff            time.Duration
		skipQueryWriteback time.Duration
		storeStart         []chunk.Chunk
		l1Start            []chunk.Chunk
		l2Start            []chunk.Chunk
		fetch              []chunk.Chunk
		l1KeysRequested    int
		l1End              []chunk.Chunk
		l2KeysRequested    int
		l2End              []chunk.Chunk
	}{
		name:       "some in L1, some in L2",
		handoff:    time.Duration(numchunks/3+100) * time.Hour,
		storeStart: storeStart,
		l1Start:    l1Start,
		l2Start:    l2Start,
		fetch:      fetch,
	}

	c1 := cache.NewMockCache()
	c2 := cache.NewMockCache()
	s := testutils.NewMockStorage()
	sc := config.SchemaConfig{
		Configs: s.GetSchemaConfigs(),
	}
	chunkClient := client.NewClientWithMaxParallel(s, nil, 1, sc)

	// Prepare l1 cache
	keys := make([]string, 0, len(test.l1Start))
	chunks := make([][]byte, 0, len(test.l1Start))
	for _, c := range test.l1Start {
		// Encode first to set the checksum
		b, _ := c.Encoded()

		k := sc.ExternalKey(c.ChunkRef)
		keys = append(keys, k)
		chunks = append(chunks, b)
	}
	_ = c1.Store(context.Background(), keys, chunks)

	// Prepare l2 cache
	keys = make([]string, 0, len(test.l2Start))
	chunks = make([][]byte, 0, len(test.l2Start))
	for _, c := range test.l2Start {
		b, _ := c.Encoded()

		k := sc.ExternalKey(c.ChunkRef)
		keys = append(keys, k)
		chunks = append(chunks, b)
	}
	_ = c2.Store(context.Background(), keys, chunks)

	// Prepare store
	_ = chunkClient.PutChunks(context.Background(), test.storeStart)

	// Build fetcher
	f, _ := New(c1, c2, false, sc, chunkClient, test.handoff, test.skipQueryWriteback, false)

	for i := 0; i < b.N; i++ {
		_, err := f.FetchChunks(context.Background(), test.fetch)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
}

type c struct {
	from, through time.Duration
}

func makeChunks(now time.Time, tpls ...c) []chunk.Chunk {
	var chks []chunk.Chunk
	for _, chk := range tpls {
		from := int(chk.from) / int(time.Hour)
		// This is only here because it's helpful for debugging.
		// This isn't even the write format for Loki but we dont' care for the sake of these tests.
		memChk := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.None, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, 256*1024, 0)
		// To make sure the fetcher doesn't swap keys and buffers each chunk is built with different, but deterministic data
		for i := 0; i < from; i++ {
			_, _ = memChk.Append(&logproto.Entry{
				Timestamp: time.Unix(int64(i), 0),
				Line:      fmt.Sprintf("line ts=%d", i),
			})
		}
		data := chunkenc.NewFacade(memChk, 0, 0)
		c := chunk.Chunk{
			ChunkRef: logproto.ChunkRef{
				UserID:  "fake",
				From:    model.TimeFromUnix(now.Add(-chk.from).UTC().Unix()),
				Through: model.TimeFromUnix(now.Add(-chk.through).UTC().Unix()),
			},
			Metric:   labels.New(labels.Label{Name: "start", Value: strconv.Itoa(from)}),
			Data:     data,
			Encoding: data.Encoding(),
		}
		// Encode to set the checksum
		if err := c.Encode(); err != nil {
			panic(err)
		}
		chks = append(chks, c)
	}

	return chks
}

func makeChunksFromMapKeys(keys []string) ([]chunk.Chunk, error) {
	chks := make([]chunk.Chunk, 0, len(keys))
	for _, k := range keys {
		c, err := chunk.ParseExternalKey("fake", k)
		if err != nil {
			return nil, err
		}
		chks = append(chks, c)
	}

	return chks, nil
}

func sortChunks(chks []chunk.Chunk) {
	slices.SortFunc(chks, func(i, j chunk.Chunk) int {
		if i.From.Before(j.From) {
			return -1
		}
		return 1
	})
}

func assertChunks(t *testing.T, expected, actual []chunk.Chunk) {
	assert.Eventually(t, func() bool {
		return len(expected) == len(actual)
	}, 2*time.Second, time.Millisecond*100, "expected %d chunks, got %d", len(expected), len(actual))
	sortChunks(expected)
	sortChunks(actual)
	for i := range expected {
		assert.Equal(t, expected[i].ChunkRef, actual[i].ChunkRef)
	}
}
