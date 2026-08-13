package fetcher

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/minio/minio-go/v7"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/errclass"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/storage/config"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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
			f, err := New(c1, c2, false, sc, chunkClient, test.handoff, test.skipQueryWriteback)
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

	// Every chunk is a cache hit that cannot be decoded, so storage is never asked.
	storeInCache(t, sc, l1, nil, chunks)

	f, err := New(l1, l2, false, sc, chunkClient, 0, 0)
	require.NoError(t, err)
	t.Cleanup(f.Stop)

	logs := captureLogs(t)
	beforeFailures := readFailureCounters(t)

	got, err := f.FetchChunks(context.Background(), chunks)
	require.NoError(t, err)
	require.Empty(t, got)

	require.Equal(t, 1, strings.Count(logs.String(), `msg="error process response from cache"`))
	require.Equal(t, 0, strings.Count(logs.String(), `msg="failed downloading chunks"`))
	require.Empty(t, failureCounterDeltas(t, beforeFailures))
}

// TestFetchChunks_LosesChunksSilently asserts that the loss is still silent.
// Every case gets fewer chunks than it asked for and a nil error. A change that
// makes FetchChunks report the loss must change this test.
func TestFetchChunks_LosesChunksSilently(t *testing.T) {
	var (
		slowDown  = minio.ErrorResponse{Code: "SlowDown", StatusCode: http.StatusServiceUnavailable}
		noSuchKey = minio.ErrorResponse{Code: "NoSuchKey", StatusCode: http.StatusNotFound}
	)

	// The store wraps every error twice, so the classification has to survive it.
	wrapped := func(err error) error {
		return errors.WithStack(errors.Wrapf(err, "boom"))
	}

	tests := []struct {
		name         string
		chunks       []c
		notInStore   []int
		inject       func(objects *testutils.FailingObjectClient, keys []string)
		wantReturned int
		wantFailures map[string]float64
	}{
		{
			name:   "storage throttles two of three",
			chunks: []c{{time.Hour, 2 * time.Hour}, {2 * time.Hour, 3 * time.Hour}, {3 * time.Hour, 4 * time.Hour}},
			inject: func(objects *testutils.FailingObjectClient, keys []string) {
				objects.Fail(wrapped(slowDown), keys[0], keys[1])
			},
			wantReturned: 1,
			wantFailures: map[string]float64{errclass.Throttled: 2},
		},
		{
			name:         "chunk absent from the store",
			chunks:       []c{{time.Hour, 2 * time.Hour}, {2 * time.Hour, 3 * time.Hour}},
			notInStore:   []int{1},
			wantReturned: 1,
			// The client predicate has to answer this one. errclass sees an
			// unrecognised error, because the backend error is private to the client.
			wantFailures: map[string]float64{errclass.NotFound: 1},
		},
		{
			name:   "storage returns NoSuchKey",
			chunks: []c{{time.Hour, 2 * time.Hour}, {2 * time.Hour, 3 * time.Hour}},
			inject: func(objects *testutils.FailingObjectClient, keys []string) {
				objects.Fail(noSuchKey, keys[0])
			},
			wantReturned: 1,
			wantFailures: map[string]float64{errclass.NotFound: 1},
		},
		{
			name:   "body stops arriving part way through",
			chunks: []c{{time.Hour, 2 * time.Hour}, {2 * time.Hour, 3 * time.Hour}},
			inject: func(objects *testutils.FailingObjectClient, keys []string) {
				objects.Truncate(keys[0], 8)
			},
			wantReturned: 1,
			wantFailures: map[string]float64{errclass.ConnReset: 1},
		},
		{
			name:   "every chunk fails",
			chunks: []c{{time.Hour, 2 * time.Hour}, {2 * time.Hour, 3 * time.Hour}, {3 * time.Hour, 4 * time.Hour}},
			inject: func(objects *testutils.FailingObjectClient, keys []string) {
				objects.Fail(slowDown, keys...)
			},
			wantReturned: 0,
			wantFailures: map[string]float64{errclass.Throttled: 3},
		},
		{
			name:   "two reasons in one batch",
			chunks: []c{{time.Hour, 2 * time.Hour}, {2 * time.Hour, 3 * time.Hour}, {3 * time.Hour, 4 * time.Hour}},
			inject: func(objects *testutils.FailingObjectClient, keys []string) {
				objects.Fail(slowDown, keys[0])
				objects.Fail(noSuchKey, keys[1])
			},
			wantReturned: 1,
			// GetParallelChunks keeps only the last error, so both lost chunks take
			// the reason of the last one to fail. The throttled chunk is misreported.
			// PR-1b collects every error and this expectation then splits in two.
			wantFailures: map[string]float64{errclass.NotFound: 2},
		},
	}

	for _, test := range tests {
		// The counters are process wide, so these cases must stay sequential.
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			sc := testSchemaConfig()
			chunks := makeChunks(time.Now(), test.chunks...)

			keys := make([]string, len(chunks))
			for i := range chunks {
				keys[i] = sc.ExternalKey(chunks[i].ChunkRef)
			}

			inStore := make([]chunk.Chunk, 0, len(chunks))
			for i := range chunks {
				if !slices.Contains(test.notInStore, i) {
					inStore = append(inStore, chunks[i])
				}
			}

			objects := testutils.NewFailingObjectClient(testutils.NewInMemoryObjectClient())
			chunkClient := client.NewClientWithMaxParallel(objects, nil, 1, sc)
			require.NoError(t, chunkClient.PutChunks(ctx, inStore))

			if test.inject != nil {
				test.inject(objects, keys)
			}

			f, err := New(cache.NewMockCache(), cache.NewMockCache(), false, sc, chunkClient, 0, 0)
			require.NoError(t, err)
			t.Cleanup(f.Stop)

			beforeFailures := readFailureCounters(t)

			got, err := f.FetchChunks(ctx, chunks)

			require.NoError(t, err)
			require.Len(t, got, test.wantReturned)

			deltas := failureCounterDeltas(t, beforeFailures)
			require.Equal(t, test.wantFailures, deltas)

			// Written by hand per case, so it cannot restate the subtraction the
			// code performs.
			var counted float64
			for _, delta := range deltas {
				counted += delta
			}
			require.Equal(t, float64(len(chunks)-test.wantReturned), counted)
		})
	}
}

// TestFetchChunks_LosesChunksSilently_OnCancellation covers the shape that
// produces most of the volume in production. A query that stops early cancels
// the context of a batch nobody will read.
func TestFetchChunks_LosesChunksSilently_OnCancellation(t *testing.T) {
	sc := testSchemaConfig()
	chunks := makeChunks(time.Now(), c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour})

	objects := testutils.NewFailingObjectClient(testutils.NewInMemoryObjectClient())
	signal := &signalOnGet{ObjectClient: objects, started: make(chan struct{})}
	chunkClient := client.NewClientWithMaxParallel(signal, nil, 1, sc)
	require.NoError(t, chunkClient.PutChunks(context.Background(), chunks))

	for _, chk := range chunks {
		release := objects.Block(sc.ExternalKey(chk.ChunkRef))
		t.Cleanup(func() { close(release) })
	}

	f, err := New(cache.NewMockCache(), cache.NewMockCache(), false, sc, chunkClient, 0, 0)
	require.NoError(t, err)
	t.Cleanup(f.Stop)

	before := readFailureCounters(t)

	ctx, cancel := context.WithCancel(context.Background())
	var (
		got      []chunk.Chunk
		fetchErr error
		done     = make(chan struct{})
	)
	go func() {
		defer close(done)
		got, fetchErr = f.FetchChunks(ctx, chunks)
	}()

	<-signal.started
	cancel()
	<-done

	require.NoError(t, fetchErr)
	require.Empty(t, got)
	require.Equal(t, map[string]float64{errclass.Canceled: 2}, failureCounterDeltas(t, before))
}

// TestFetchChunks_LosesChunksSilently_OnShortReturn covers a store that drops a
// chunk and reports no error. This is what mockChunkStoreClient in pkg/storage
// does, which is why no batch test there can catch the loss.
func TestFetchChunks_LosesChunksSilently_OnShortReturn(t *testing.T) {
	ctx := context.Background()
	sc := testSchemaConfig()
	chunks := makeChunks(time.Now(), c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour})

	inner := client.NewClientWithMaxParallel(testutils.NewInMemoryObjectClient(), nil, 1, sc)
	require.NoError(t, inner.PutChunks(ctx, chunks))

	f, err := New(cache.NewMockCache(), cache.NewMockCache(), false, sc, shortReturnClient{Client: inner, drop: 1}, 0, 0)
	require.NoError(t, err)
	t.Cleanup(f.Stop)

	before := readFailureCounters(t)

	got, err := f.FetchChunks(ctx, chunks)

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, map[string]float64{errclass.Unknown: 1}, failureCounterDeltas(t, before))
}

// TestLossMetricsAreExposed pins the storage failure reasons.
func TestLossMetricsAreExposed(t *testing.T) {
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() == "loki_chunk_fetcher_failures_total" {
			require.Len(t, family.GetMetric(), len(errclass.Reasons()))
			return
		}
	}
	t.Fatal("loki_chunk_fetcher_failures_total is not registered")
}

func readFailureCounters(t *testing.T) map[string]float64 {
	t.Helper()

	out := make(map[string]float64, len(errclass.Reasons()))
	for _, reason := range errclass.Reasons() {
		out[reason] = testutil.ToFloat64(chunkFetchFailures.WithLabelValues(reason))
	}
	return out
}

func failureCounterDeltas(t *testing.T, before map[string]float64) map[string]float64 {
	t.Helper()

	out := map[string]float64{}
	for reason, after := range readFailureCounters(t) {
		if delta := after - before[reason]; delta != 0 {
			out[reason] = delta
		}
	}
	return out
}

// signalOnGet reports the start of the first chunk read, so a test can cancel at
// a known point rather than sleep.
type signalOnGet struct {
	client.ObjectClient

	once    sync.Once
	started chan struct{}
}

func (s *signalOnGet) GetObject(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	s.once.Do(func() { close(s.started) })
	return s.ObjectClient.GetObject(ctx, key)
}

// shortReturnClient drops chunks and reports success.
type shortReturnClient struct {
	client.Client

	drop int
}

func (s shortReturnClient) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	got, _ := s.Client.GetChunks(ctx, chunks)
	if len(got) < s.drop {
		return nil, nil
	}
	return got[:len(got)-s.drop], nil
}

func testSchemaConfig() config.SchemaConfig {
	return testutils.SchemaConfig("inmemory", "v11", model.Now().Add(-100*24*time.Hour))
}

// captureLogs redirects the logger the fetcher falls back to when the context
// carries none. The logger is a global, so callers must not run in parallel.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	buf := &bytes.Buffer{}
	previous := util_log.Logger
	util_log.Logger = log.NewLogfmtLogger(log.NewSyncWriter(buf))
	t.Cleanup(func() { util_log.Logger = previous })

	return buf
}

func storeInCache(t *testing.T, sc config.SchemaConfig, c cache.Cache, valid, corrupt []chunk.Chunk) {
	t.Helper()

	keys := make([]string, 0, len(valid)+len(corrupt))
	bufs := make([][]byte, 0, len(valid)+len(corrupt))
	for _, chk := range valid {
		encoded, err := chk.Encoded()
		require.NoError(t, err)
		keys = append(keys, sc.ExternalKey(chk.ChunkRef))
		bufs = append(bufs, encoded)
	}
	for _, chk := range corrupt {
		keys = append(keys, sc.ExternalKey(chk.ChunkRef))
		bufs = append(bufs, []byte("not a chunk"))
	}
	require.NoError(t, c.Store(context.Background(), keys, bufs))
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
	f, _ := New(c1, c2, false, sc, chunkClient, test.handoff, test.skipQueryWriteback)

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
