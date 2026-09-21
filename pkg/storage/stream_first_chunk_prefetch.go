package storage

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/grafana/loki/v3/pkg/storage/config"
)

// streamFirstPrefetchConcurrency returns how many chunk batches the preloader should fetch
// concurrently.
//
// Each worker fetches one batch of up to maxChunkBatchSize chunks at a time. It fetches those
// chunks in parallel. So this many workers give an aggregate parallel-GET width of about
// maxParallelGetChunk.
//
// maxParallelGetChunk is a per-fetch cap, not a store-wide pool. The timestamp-first reader
// only ever has one batch fetch in flight. So it only ever reaches maxChunkBatchSize of that
// cap. The stream-first reader uses more of it by fetching several batches at once instead.
func streamFirstPrefetchConcurrency(maxParallelGetChunk, maxChunkBatchSize int) int {
	if maxParallelGetChunk < 1 || maxChunkBatchSize < 1 {
		return 1
	}
	return max(1, int(math.Round(float64(maxParallelGetChunk)/float64(maxChunkBatchSize))))
}

// streamFirstChunkBatcher slices an ordered chunk-ref list into batches bounded by chunk count.
//
// A batch may hold a partial stream: a stream dense enough to exceed maxChunksPerBatch splits
// across batches. The bound is on count, not size, because a chunk ref carries no size.
type streamFirstChunkBatcher struct {
	chunks            []*LazyChunk
	maxChunksPerBatch int
	pos               int
}

// newStreamFirstChunkBatcher returns a batcher over chunks already ordered stream-first.
//
// Chunks within one stream need not be From-ascending: newTimestampFirstSampleBatchIterator sorts
// and dedups each stream's chunks again once it decodes them.
func newStreamFirstChunkBatcher(chunks []*LazyChunk, maxChunksPerBatch int) *streamFirstChunkBatcher {
	if maxChunksPerBatch < 1 {
		maxChunksPerBatch = 1
	}
	return &streamFirstChunkBatcher{chunks: chunks, maxChunksPerBatch: maxChunksPerBatch}
}

// next returns the next batch of chunk refs, or nil once the input is exhausted. A returned batch
// is never empty, so nil unambiguously means "done".
func (b *streamFirstChunkBatcher) next() []*LazyChunk {
	if b.pos >= len(b.chunks) {
		// We completed iterating chunks.
		return nil
	}

	end := b.pos + b.maxChunksPerBatch
	if end > len(b.chunks) {
		end = len(b.chunks)
	}

	batch := b.chunks[b.pos:end]
	b.pos = end
	return batch
}

// chunkFetchFunc fetches the given chunks in place, populating Data and IsValid.
type chunkFetchFunc func(ctx context.Context, schemas config.SchemaConfig, chunks []*LazyChunk) error

// streamFirstBatchLoader fetches one batch of chunks and records the load duration of a
// successful fetch. A failed fetch records nothing, since its duration is not a load time.
//
// It does no stream-level matcher or filterer pruning: a fetched batch holds chunks with Data
// populated (compressed, undecoded) and IsValid set. It holds no mutable state, so it is safe to
// call concurrently from the preloader's workers.
type streamFirstBatchLoader struct {
	schemas config.SchemaConfig
	metrics *ChunkMetrics
	fetchFn chunkFetchFunc
}

func newStreamFirstBatchLoader(schemas config.SchemaConfig, metrics *ChunkMetrics, fetchFn chunkFetchFunc) *streamFirstBatchLoader {
	return &streamFirstBatchLoader{schemas: schemas, metrics: metrics, fetchFn: fetchFn}
}

// fetch loads the given non-empty batch in place and returns it.
func (l *streamFirstBatchLoader) fetch(ctx context.Context, batch []*LazyChunk) ([]*LazyChunk, error) {
	start := time.Now()
	if err := l.fetchFn(ctx, l.schemas, batch); err != nil {
		return nil, err
	}
	l.metrics.streamFirstBatchLoad.Observe(time.Since(start).Seconds())
	return batch, nil
}

// preloadedChunkBatch is the result of loading one batch: either the fetched chunks, or a
// terminal error.
type preloadedChunkBatch struct {
	chunks []*LazyChunk
	err    error
}

// preloadChunkBatchJob is a batch handed to a worker, plus the future its result is delivered on.
type preloadChunkBatchJob struct {
	batch  []*LazyChunk
	future chan preloadedChunkBatch
}

// streamFirstChunkPreloader fetches chunk batches ahead of the consumer, using a fixed pool of
// maxConcurrentBatches workers, and delivers them to the consumer in batcher order.
type streamFirstChunkPreloader struct {
	ctx    context.Context
	cancel context.CancelFunc

	// wg tracks the dispatcher and every worker goroutine, so Close can wait for them all to
	// exit instead of merely asking them to.
	wg sync.WaitGroup

	// results is an ordered queue of futures, so the consumer reads preloaded batches in order
	// even though the workers may finish them out of order.
	results chan chan preloadedChunkBatch

	currBatch []*LazyChunk
	err       error
}

func newStreamFirstChunkPreloader(ctx context.Context, batcher *streamFirstChunkBatcher, loader *streamFirstBatchLoader, maxConcurrentBatches int) *streamFirstChunkPreloader {
	if maxConcurrentBatches < 1 {
		maxConcurrentBatches = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	p := &streamFirstChunkPreloader{
		ctx:     ctx,
		cancel:  cancel,
		results: make(chan chan preloadedChunkBatch, maxConcurrentBatches),
	}

	// maxConcurrentBatches workers bound the number of fetches in flight at once.
	jobs := make(chan preloadChunkBatchJob)
	p.wg.Add(maxConcurrentBatches + 1)
	for i := 0; i < maxConcurrentBatches; i++ {
		go p.runWorker(loader, jobs)
	}
	go p.runDispatcher(batcher, jobs)

	return p
}

// runWorker fetches each job's batch and fulfills its future.
func (p *streamFirstChunkPreloader) runWorker(loader *streamFirstBatchLoader, jobs <-chan preloadChunkBatchJob) {
	defer p.wg.Done()

	for job := range jobs {
		chunks, err := loader.fetch(p.ctx, job.batch)
		job.future <- preloadedChunkBatch{chunks: chunks, err: err} // future is 1-buffered; never blocks
	}
}

// runDispatcher pulls batches in order and, for each, hands the job to a worker and only then
// enqueues its future on results.
//
// Enqueuing the job before the future guarantees every future the consumer can see already has
// a dispatched job. So every future is always fulfilled, and none is orphaned on cancellation.
//
// It ends the workers and the consumer once the input is exhausted or the context is canceled.
func (p *streamFirstChunkPreloader) runDispatcher(batcher *streamFirstChunkBatcher, jobs chan<- preloadChunkBatchJob) {
	defer p.wg.Done()
	defer close(p.results) // ends the consumer
	defer close(jobs)      // ends the workers

	for {
		// Check before dispatching a new job. The select below only catches cancellation once
		// it's already blocked sending. Without this check, a context canceled before the loop's
		// first iteration could still race a ready worker and dispatch a job anyway.
		if p.ctx.Err() != nil {
			return
		}

		batch := batcher.next()
		if len(batch) == 0 {
			return
		}

		future := make(chan preloadedChunkBatch, 1)

		select {
		case jobs <- preloadChunkBatchJob{batch: batch, future: future}:
		case <-p.ctx.Done():
			return
		}

		select {
		case p.results <- future: // blocks once maxConcurrentBatches batches are ahead
		case <-p.ctx.Done():
			return
		}
	}
}

// Next advances to the next preloaded batch, in batcher order. It returns false once the input is
// exhausted, a fetch errored, or the context was canceled; check Err to tell those apart.
//
// Next is sticky once it returns false: a repeat call returns false again immediately.
func (p *streamFirstChunkPreloader) Next() bool {
	if p.err != nil {
		return false
	}

	future, ok := <-p.results
	if !ok {
		// results closes both on clean exhaustion and on a context canceled before any batch was
		// ever dispatched. The two look identical here, so fall back to ctx.Err to tell them apart.
		if p.err == nil {
			p.err = p.ctx.Err()
		}
		return false
	}

	// This read always resolves: runDispatcher dispatches a future's job before enqueuing it.
	res := <-future
	if res.err != nil {
		p.err = res.err
		return false
	}

	p.currBatch = res.chunks
	return true
}

func (p *streamFirstChunkPreloader) At() []*LazyChunk { return p.currBatch }

func (p *streamFirstChunkPreloader) Err() error { return p.err }

// Close stops preloader and blocks all its goroutines have exited. It is safe to
// call it more than once.
func (p *streamFirstChunkPreloader) Close() error {
	p.cancel()
	p.wg.Wait()
	return nil
}
