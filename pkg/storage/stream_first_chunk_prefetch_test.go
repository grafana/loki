package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/goleak"

	"github.com/grafana/loki/v3/pkg/storage/config"
)

func TestStreamFirstChunkBatcher(t *testing.T) {
	t.Run("bounds each batch by chunk count and yields every chunk in order", func(t *testing.T) {
		chunks := mkRefChunks(10)
		b := newStreamFirstChunkBatcher(chunks, 4)

		var sizes []int
		var got []*LazyChunk
		for {
			batch := b.next()
			if len(batch) == 0 {
				break
			}
			require.LessOrEqual(t, len(batch), 4)
			sizes = append(sizes, len(batch))
			got = append(got, batch...)
		}
		require.Equal(t, []int{4, 4, 2}, sizes)
		requireSameChunks(t, chunks, got)
	})

	t.Run("a dense stream splits across batches (contiguous slices)", func(t *testing.T) {
		chunks := mkRefChunks(7)
		b := newStreamFirstChunkBatcher(chunks, 3)

		var batches [][]*LazyChunk
		var got []*LazyChunk
		for {
			batch := b.next()
			if len(batch) == 0 {
				break
			}
			batches = append(batches, batch)
			got = append(got, batch...)
		}
		require.Equal(t, []int{3, 3, 1}, []int{len(batches[0]), len(batches[1]), len(batches[2])})
		requireSameChunks(t, chunks, got)
	})

	t.Run("fewer chunks than a batch", func(t *testing.T) {
		b := newStreamFirstChunkBatcher(mkRefChunks(2), 10)
		require.Len(t, b.next(), 2)
		require.Empty(t, b.next())
	})

	t.Run("empty input", func(t *testing.T) {
		require.Empty(t, newStreamFirstChunkBatcher(nil, 4).next())
	})

	t.Run("maxChunksPerBatch below 1 is floored to 1", func(t *testing.T) {
		require.Len(t, newStreamFirstChunkBatcher(mkRefChunks(3), 0).next(), 1)
	})
}

func TestStreamFirstBatchLoader_Fetch(t *testing.T) {
	var calls int
	var fetched []*LazyChunk
	fetchFn := func(_ context.Context, _ config.SchemaConfig, chunks []*LazyChunk) error {
		calls++
		fetched = chunks
		return nil
	}
	loader := newStreamFirstBatchLoader(config.SchemaConfig{}, NilMetrics, fetchFn)

	batch := mkRefChunks(3)
	out, err := loader.fetch(context.Background(), batch)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	requireSameChunks(t, batch, out)
	requireSameChunks(t, batch, fetched)
}

func TestStreamFirstBatchLoader_PropagatesFetchError(t *testing.T) {
	boom := errors.New("boom")
	loader := newStreamFirstBatchLoader(config.SchemaConfig{}, NilMetrics, func(_ context.Context, _ config.SchemaConfig, _ []*LazyChunk) error { return boom })
	_, err := loader.fetch(context.Background(), mkRefChunks(2))
	require.ErrorIs(t, err, boom)
}

func TestStreamFirstPrefetchConcurrency(t *testing.T) {
	require.Equal(t, 3, streamFirstPrefetchConcurrency(150, 50))  // exact
	require.Equal(t, 1, streamFirstPrefetchConcurrency(150, 200)) // batch wider than pool
	require.Equal(t, 4, streamFirstPrefetchConcurrency(150, 40))  // round(3.75)
	require.Equal(t, 1, streamFirstPrefetchConcurrency(0, 50))    // guarded
	require.Equal(t, 1, streamFirstPrefetchConcurrency(150, 0))   // guarded
}

// TestStreamFirstChunkPreloader_DeliversInOrder proves batches are delivered in batcher order even
// when their fetches complete out of order: batch 1 finishes before batch 0, yet Next() yields 0 then 1.
func TestStreamFirstChunkPreloader_DeliversInOrder(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	chunks := mkRefChunks(2) // two batches of one chunk (checksums 1 and 2)
	gate0 := make(chan struct{})
	done := make(chan uint32, 2)
	fetchFn := func(_ context.Context, _ config.SchemaConfig, batch []*LazyChunk) error {
		cs := batch[0].Chunk.ChunkRef.Checksum
		if cs == 1 { // batch 0 blocks until released
			<-gate0
		}
		done <- cs
		return nil
	}
	p := newTestPreloader(chunks, 1, 2, fetchFn) // both batches in flight
	defer p.Close()

	require.Equal(t, uint32(2), <-done) // batch 1 completes first
	close(gate0)
	require.Equal(t, uint32(1), <-done) // batch 0 completes second

	require.True(t, p.Next())
	require.Equal(t, uint32(1), p.At()[0].Chunk.ChunkRef.Checksum) // delivered batch 0 first
	require.True(t, p.Next())
	require.Equal(t, uint32(2), p.At()[0].Chunk.ChunkRef.Checksum) // then batch 1
	require.False(t, p.Next())
	require.NoError(t, p.Err())
}

// TestStreamFirstChunkPreloader_BoundsConcurrentFetches proves at most maxConcurrentBatches fetches
// run at once (the worker count is the concurrency bound).
func TestStreamFirstChunkPreloader_BoundsConcurrentFetches(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	const k = 3
	var inFlight, maxInFlight atomic.Int64
	started := make(chan struct{}, 100)
	release := make(chan struct{})
	fetchFn := func(_ context.Context, _ config.SchemaConfig, _ []*LazyChunk) error {
		n := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		started <- struct{}{}
		<-release
		inFlight.Add(-1)
		return nil
	}
	p := newTestPreloader(mkRefChunks(10), 1, k, fetchFn) // 10 batches, k workers
	defer p.Close()

	for i := 0; i < k; i++ {
		<-started // k workers are now blocked inside fetch
	}
	require.Equal(t, int64(k), inFlight.Load()) // no (k+1)th fetch can start until one frees
	close(release)

	var n int
	for p.Next() {
		n++
	}
	require.NoError(t, p.Err())
	require.Equal(t, 10, n)
	require.LessOrEqual(t, maxInFlight.Load(), int64(k))
}

func TestStreamFirstChunkPreloader_DrainsAllInOrder(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	for _, tc := range []struct{ n, batchSize, k int }{
		{0, 1, 3}, {1, 1, 3}, {2, 4, 3}, {7, 2, 3}, {10, 3, 2},
	} {
		chunks := mkRefChunks(tc.n)
		p := newTestPreloader(chunks, tc.batchSize, tc.k,
			func(_ context.Context, _ config.SchemaConfig, _ []*LazyChunk) error { return nil })
		var got []*LazyChunk
		for p.Next() {
			got = append(got, p.At()...)
		}
		require.NoError(t, p.Err())
		requireSameChunks(t, chunks, got)
		require.NoError(t, p.Close())
	}
}

func TestStreamFirstChunkPreloader_PropagatesError(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	boom := errors.New("fetch failed")
	fetchFn := func(_ context.Context, _ config.SchemaConfig, batch []*LazyChunk) error {
		if batch[0].Chunk.ChunkRef.Checksum == 3 { // the third batch (checksum 3) fails
			return boom
		}
		return nil
	}
	p := newTestPreloader(mkRefChunks(5), 1, 1, fetchFn) // 1 worker -> strictly ordered/deterministic
	defer p.Close()

	require.True(t, p.Next())  // batch 0
	require.True(t, p.Next())  // batch 1
	require.False(t, p.Next()) // batch 2 errored
	require.ErrorIs(t, p.Err(), boom)
}

// TestStreamFirstChunkPreloader_CloseStopsGoroutines proves Close cancels in-flight fetches and all
// preloader goroutines (dispatcher + workers) exit, even when fetches would otherwise block forever.
func TestStreamFirstChunkPreloader_CloseStopsGoroutines(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	started := make(chan struct{}, 100)
	fetchFn := func(ctx context.Context, _ config.SchemaConfig, _ []*LazyChunk) error {
		started <- struct{}{}
		<-ctx.Done() // block until cancelled
		return ctx.Err()
	}
	p := newTestPreloader(mkRefChunks(10), 1, 3, fetchFn)
	<-started // at least one worker is inside fetch
	require.NoError(t, p.Close())
	// goleak (deferred) asserts the dispatcher and workers have exited.
}

func TestStreamFirstChunkPreloader_ParentContextCancel(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	ctx, cancel := context.WithCancel(context.Background())
	fetchFn := func(ctx context.Context, _ config.SchemaConfig, _ []*LazyChunk) error {
		<-ctx.Done()
		return ctx.Err()
	}
	b := newStreamFirstChunkBatcher(mkRefChunks(5), 1)
	l := newStreamFirstBatchLoader(config.SchemaConfig{}, NilMetrics, fetchFn)
	p := newStreamFirstChunkPreloader(ctx, b, l, 2)
	defer p.Close()

	cancel()
	require.False(t, p.Next())
}

// newTestPreloader wires a batcher + loader + preloader with an injected fetch fn.
func newTestPreloader(chunks []*LazyChunk, batchSize, maxConcurrentBatches int, fetchFn chunkFetchFunc) *streamFirstChunkPreloader {
	b := newStreamFirstChunkBatcher(chunks, batchSize)
	l := newStreamFirstBatchLoader(config.SchemaConfig{}, NilMetrics, fetchFn)
	return newStreamFirstChunkPreloader(context.Background(), b, l, maxConcurrentBatches)
}

// mkRefChunks builds n placeholder LazyChunks (no Data), each with a distinct identity (unique
// Checksum) so tests can verify batch order and per-chunk identity, not just counts.
func mkRefChunks(n int) []*LazyChunk {
	out := make([]*LazyChunk, n)
	for i := range out {
		c := &LazyChunk{}
		c.Chunk.ChunkRef.Checksum = uint32(i + 1)
		out[i] = c
	}
	return out
}

// requireSameChunks asserts got is exactly want: same *LazyChunk objects, in the same order.
func requireSameChunks(t *testing.T, want, got []*LazyChunk) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.Samef(t, want[i], got[i], "chunk at position %d differs", i)
	}
}
