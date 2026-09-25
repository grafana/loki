package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"go.uber.org/goleak"

	"github.com/grafana/loki/v3/pkg/storage/config"
)

func TestStreamFirstPrefetchConcurrency(t *testing.T) {
	require.Equal(t, 3, streamFirstPrefetchConcurrency(150, 50))  // exact
	require.Equal(t, 1, streamFirstPrefetchConcurrency(150, 200)) // batch wider than pool
	require.Equal(t, 4, streamFirstPrefetchConcurrency(150, 40))  // round(3.75)
	require.Equal(t, 1, streamFirstPrefetchConcurrency(0, 50))    // guarded
	require.Equal(t, 1, streamFirstPrefetchConcurrency(150, 0))   // guarded
}

func TestStreamFirstChunkBatcher(t *testing.T) {
	for name, tc := range map[string]struct {
		numChunks          int
		expectedBatchSizes []int
	}{
		"bounds each batch by chunk count, with a partial batch at the end": {
			numChunks:          10,
			expectedBatchSizes: []int{4, 4, 2},
		},
		"an exact multiple of the batch size leaves no partial batch": {
			numChunks:          8,
			expectedBatchSizes: []int{4, 4},
		},
	} {
		t.Run(name, func(t *testing.T) {
			chunks := mkRefChunks(tc.numChunks)
			batcher := newStreamFirstChunkBatcher(chunks, 4)

			var sizes []int
			var got []*LazyChunk
			for {
				batch := batcher.next()
				if len(batch) == 0 {
					break
				}

				require.LessOrEqual(t, len(batch), 4)
				sizes = append(sizes, len(batch))
				got = append(got, batch...)
			}

			require.Equal(t, tc.expectedBatchSizes, sizes)
			requireSameChunks(t, chunks, got)
		})
	}

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

func TestStreamFirstBatchLoader(t *testing.T) {
	t.Run("fetches a batch and returns it", func(t *testing.T) {
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
	})

	t.Run("propagates a fetch error", func(t *testing.T) {
		boom := errors.New("boom")
		loader := newStreamFirstBatchLoader(config.SchemaConfig{}, NilMetrics, func(_ context.Context, _ config.SchemaConfig, _ []*LazyChunk) error { return boom })
		_, err := loader.fetch(context.Background(), mkRefChunks(2))
		require.ErrorIs(t, err, boom)
	})
}

func TestStreamFirstChunkPreloader(t *testing.T) {
	// newTestPreloader wires a batcher, loader, and preloader with an injected fetch function.
	newTestPreloader := func(chunks []*LazyChunk, batchSize, maxConcurrentBatches int, fetchFn chunkFetchFunc) *streamFirstChunkPreloader {
		b := newStreamFirstChunkBatcher(chunks, batchSize)
		l := newStreamFirstBatchLoader(config.SchemaConfig{}, NilMetrics, fetchFn)
		return newStreamFirstChunkPreloader(context.Background(), b, l, maxConcurrentBatches)
	}

	t.Run("delivers batches in batcher order even when fetches complete out of order", func(t *testing.T) {
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

		require.Equal(t, uint32(2), requireReceive(t, done, "batch 1 to complete")) // batch 1 completes first
		close(gate0)
		require.Equal(t, uint32(1), requireReceive(t, done, "batch 0 to complete")) // batch 0 completes second

		require.True(t, p.Next())
		require.Equal(t, uint32(1), p.At()[0].Chunk.ChunkRef.Checksum) // delivered batch 0 first
		require.True(t, p.Next())
		require.Equal(t, uint32(2), p.At()[0].Chunk.ChunkRef.Checksum) // then batch 1
		require.False(t, p.Next())
		require.NoError(t, p.Err())
	})

	// At most maxConcurrentBatches fetches run at once, since the worker count is the
	// concurrency bound.
	t.Run("bounds concurrent fetches to maxConcurrentBatches", func(t *testing.T) {
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
			requireReceive(t, started, "a worker to block inside fetch")
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
	})

	t.Run("drains all chunks in order across varied batch sizes and worker counts", func(t *testing.T) {
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
	})

	t.Run("propagates a fetch error and stops delivering further batches", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

		boom := errors.New("fetch failed")
		reachedFailingBatch := make(chan struct{})
		releaseFailingBatch := make(chan struct{})
		release := sync.OnceFunc(func() { close(releaseFailingBatch) })
		defer release() // let the worker return even if an assertion below fails first
		var calledChecksums []uint32
		fetchFn := func(_ context.Context, _ config.SchemaConfig, batch []*LazyChunk) error {
			cs := batch[0].Chunk.ChunkRef.Checksum
			calledChecksums = append(calledChecksums, cs)
			if cs == 3 { // batch 2 (checksum 3) fails
				close(reachedFailingBatch)
				<-releaseFailingBatch // hold the single worker here so it can't reach a later batch
				return boom
			}
			return nil
		}
		p := newTestPreloader(mkRefChunks(5), 1, 1, fetchFn) // 1 worker -> strictly ordered/deterministic
		defer p.Close()

		require.True(t, p.Next()) // batch 0
		require.True(t, p.Next()) // batch 1

		requireReceive(t, reachedFailingBatch, "the failing batch's fetch to start")
		// The single worker is parked inside the failing batch's fetch, so it cannot yet have
		// started a later one: this is a real guarantee here, not a race won by chance.
		require.Equal(t, []uint32{1, 2, 3}, calledChecksums, "must not fetch batches after the failing one")
		release()

		require.False(t, p.Next()) // batch 2 errored
		require.ErrorIs(t, p.Err(), boom)
	})

	t.Run("stops permanently on the first failure, even if a later batch already succeeded", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

		boom := errors.New("batch 0 failed")
		releaseFailing := make(chan struct{})
		laterBatchDone := make(chan struct{})
		fetchFn := func(_ context.Context, _ config.SchemaConfig, batch []*LazyChunk) error {
			if batch[0].Chunk.ChunkRef.Checksum == 1 {
				<-releaseFailing
				return boom
			}
			close(laterBatchDone)
			return nil
		}
		p := newTestPreloader(mkRefChunks(2), 1, 2, fetchFn) // 2 batches, 2 workers: both run at once
		defer p.Close()

		requireReceive(t, laterBatchDone, "the later batch to finish while the earlier one is still blocked")
		close(releaseFailing)

		require.False(t, p.Next(), "the earlier batch's own error must be reported first")
		require.ErrorIs(t, p.Err(), boom)

		require.False(t, p.Next(), "Next must stay false, even though a later batch already succeeded")
		require.ErrorIs(t, p.Err(), boom, "a repeat call must not lose the original error")
	})

	t.Run("Close cancels in-flight fetches and stops every goroutine", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

		started := make(chan struct{}, 100)
		fetchFn := func(ctx context.Context, _ config.SchemaConfig, _ []*LazyChunk) error {
			started <- struct{}{}
			<-ctx.Done() // block until canceled
			return ctx.Err()
		}
		p := newTestPreloader(mkRefChunks(10), 1, 3, fetchFn)
		requireReceive(t, started, "a worker to be inside fetch")
		require.NoError(t, p.Close())
	})

	t.Run("Close blocks until every in-flight fetch actually returns", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

		started := make(chan struct{})
		release := make(chan struct{})
		fetchFn := func(ctx context.Context, _ config.SchemaConfig, _ []*LazyChunk) error {
			close(started)
			<-release
			return ctx.Err()
		}
		p := newTestPreloader(mkRefChunks(1), 1, 1, fetchFn)
		requireReceive(t, started, "the worker to be inside fetch")

		closeDone := make(chan error, 1)
		go func() { closeDone <- p.Close() }()

		select {
		case <-closeDone:
			t.Fatal("Close returned while a worker's fetch was still running")
		case <-time.After(100 * time.Millisecond):
		}

		close(release)
		require.NoError(t, requireReceive(t, closeDone, "Close to return once the blocked fetch does"))
	})

	t.Run("stops immediately when the parent context is already canceled", func(t *testing.T) {
		defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // canceled before the preloader is even constructed

		var calls atomic.Int64
		fetchFn := func(ctx context.Context, _ config.SchemaConfig, _ []*LazyChunk) error {
			calls.Add(1)
			<-ctx.Done()
			return ctx.Err()
		}
		// Built by hand, not via newTestPreloader: this needs its own pre-canceled ctx, and
		// newTestPreloader always wires context.Background().
		b := newStreamFirstChunkBatcher(mkRefChunks(5), 1)
		l := newStreamFirstBatchLoader(config.SchemaConfig{}, NilMetrics, fetchFn)
		p := newStreamFirstChunkPreloader(ctx, b, l, 2)
		defer p.Close()

		require.False(t, p.Next())
		require.ErrorIs(t, p.Err(), context.Canceled, "must report why it stopped, not look like a clean empty input")
		require.Zero(t, calls.Load(), "must not fetch any batch once the context is already canceled")
	})
}

// mkRefChunks builds n placeholder LazyChunks with no Data, each with a distinct Checksum, so
// tests can verify batch order and per-chunk identity, not just counts.
func mkRefChunks(n int) []*LazyChunk {
	out := make([]*LazyChunk, n)
	for i := range out {
		c := &LazyChunk{}
		c.Chunk.ChunkRef.Checksum = uint32(i + 1)
		out[i] = c
	}
	return out
}

// requireReceive waits for a value from ch and fails the test if none arrives within 5 seconds.
// This turns a regression in the code under test into a test failure, not a hung test binary.
// Call it only from the goroutine running the test: on timeout it calls t.Fatalf, which must not
// run on any other goroutine.
func requireReceive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	const timeout = 5 * time.Second
	select {
	case v := <-ch:
		return v
	case <-time.After(timeout):
		t.Fatalf("timed out after %s waiting for %s", timeout, what)
		var zero T
		return zero
	}
}

// requireSameChunks asserts got is exactly want: the same *LazyChunk objects, in the same order.
func requireSameChunks(t *testing.T, want, got []*LazyChunk) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.Samef(t, want[i], got[i], "chunk at position %d differs", i)
	}
}
