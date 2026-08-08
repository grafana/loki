package storage

import (
	"context"
	"math"
	"time"

	"github.com/grafana/loki/v3/pkg/storage/config"
)

// streamFirstPrefetchConcurrency derives how many chunk batches the preloader fetches concurrently from
// the two existing store knobs, so the aggregate parallel-GET width is ~MaxParallelGetChunk while
// each batch stays MaxChunkBatchSize.
func streamFirstPrefetchConcurrency(maxParallelGetChunk, maxChunkBatchSize int) int {
	if maxParallelGetChunk < 1 || maxChunkBatchSize < 1 {
		return 1
	}
	return max(1, int(math.Round(float64(maxParallelGetChunk)/float64(maxChunkBatchSize))))
}

// streamFirstChunkBatcher slices an ordered chunk-ref list into batches bounded by chunk count.
// A batch may hold a partial stream (a dense stream splits across batches). Bounding by count only
// is deliberate: a plain chunk ref carries no size, so count is the only reliable bound.
type streamFirstChunkBatcher struct {
	chunks            []*LazyChunk
	maxChunksPerBatch int
	pos               int
}

// newStreamFirstChunkBatcher returns a batcher over chunks that must already be ordered
// stream-first: every stream's chunks contiguous and streams in stream-hash ASC order, and within
// a stream by chunk start time (From) ASC.
func newStreamFirstChunkBatcher(chunks []*LazyChunk, maxChunksPerBatch int) *streamFirstChunkBatcher {
	if maxChunksPerBatch < 1 {
		maxChunksPerBatch = 1
	}
	return &streamFirstChunkBatcher{chunks: chunks, maxChunksPerBatch: maxChunksPerBatch}
}

// next returns the next batch of (unfetched) chunk refs, or nil once the input is exhausted.
// A produced batch is never empty, so a nil result unambiguously means "done".
func (b *streamFirstChunkBatcher) next() []*LazyChunk {
	if b.pos >= len(b.chunks) {
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

// chunkFetchFunc fetches the given chunks in place (populating Data and IsValid).
type chunkFetchFunc func(ctx context.Context, schemas config.SchemaConfig, chunks []*LazyChunk) error

// streamFirstBatchLoader fetches one batch of chunks in a single round (parallel GETs within the
// batch), recording the load duration. It does no stream-level matcher/filterer pruning; a fetched
// batch holds chunks with Data populated (compressed, undecoded) and IsValid set. It is stateless
// and safe to call concurrently from the preloader's workers.
type streamFirstBatchLoader struct {
	schemas config.SchemaConfig
	metrics *ChunkMetrics
	fetchFn chunkFetchFunc
}

func newStreamFirstBatchLoader(schemas config.SchemaConfig, metrics *ChunkMetrics, fetchFn chunkFetchFunc) *streamFirstBatchLoader {
	return &streamFirstBatchLoader{schemas: schemas, metrics: metrics, fetchFn: fetchFn}
}

// fetch loads the (non-empty) batch in place and returns it.
func (l *streamFirstBatchLoader) fetch(ctx context.Context, batch []*LazyChunk) ([]*LazyChunk, error) {
	start := time.Now()
	if err := l.fetchFn(ctx, l.schemas, batch); err != nil {
		return nil, err
	}
	l.metrics.streamOrderedBatchLoad.Observe(time.Since(start).Seconds())
	return batch, nil
}

// preloadedChunkBatch is the result of loading one batch: the fetched chunks or a terminal error.
type preloadedChunkBatch struct {
	chunks []*LazyChunk
	err    error
}

// preloadChunkBatchJob is a batch handed to a worker plus the future its result is delivered on.
type preloadChunkBatchJob struct {
	batch  []*LazyChunk
	future chan preloadedChunkBatch
}

// streamFirstChunkPreloader fetches chunk batches ahead of the consumer using a fixed pool of
// maxConcurrentBatches workers, delivering them in batcher order.
type streamFirstChunkPreloader struct {
	ctx    context.Context
	cancel context.CancelFunc

	// results holds an ordered queue of futures, to allow the consumer to read preloaded batches
	// in order.
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

	// Run maxConcurrentBatches workers, so at most that many fetches are in flight — the worker count
	// is the concurrency bound.
	jobs := make(chan preloadChunkBatchJob)
	for i := 0; i < maxConcurrentBatches; i++ {
		go p.runWorker(loader, jobs)
	}
	go p.runDispatcher(batcher, jobs)

	return p
}

// runWorker fetches each job's batch and fulfills its future.
func (p *streamFirstChunkPreloader) runWorker(loader *streamFirstBatchLoader, jobs <-chan preloadChunkBatchJob) {
	for job := range jobs {
		chunks, err := loader.fetch(p.ctx, job.batch)
		job.future <- preloadedChunkBatch{chunks: chunks, err: err} // future is 1-buffered; never blocks
	}
}

// runDispatcher pulls batches in order and, for each, hands the job to a worker and THEN enqueues
// its future on results. Job-before-future guarantees every future the consumer can see already has
// a dispatched job, so it is always fulfilled (no orphan on cancellation). It ends the workers and
// the consumer on exhaustion or cancellation.
func (p *streamFirstChunkPreloader) runDispatcher(batcher *streamFirstChunkBatcher, jobs chan<- preloadChunkBatchJob) {
	defer close(p.results) // ends the consumer
	defer close(jobs)      // ends the workers

	for {
		batch := batcher.next()
		if len(batch) == 0 {
			return
		}

		future := make(chan preloadedChunkBatch, 1)

		// Hand the job to a free worker first.
		select {
		case jobs <- preloadChunkBatchJob{batch: batch, future: future}:
		case <-p.ctx.Done():
			return
		}

		// Then enqueue in order; blocks when maxConcurrentBatches ahead.
		select {
		case p.results <- future:
		case <-p.ctx.Done():
			return
		}
	}
}

// Next advances to the next preloaded batch, in batcher order. It returns false when the input is
// exhausted or a fetch errored (surfaced via Err).
func (p *streamFirstChunkPreloader) Next() bool {
	future, ok := <-p.results
	if !ok {
		return false
	}

	// Reading from the future is guaranteed to always resolve, because
	// the future's job gets dispatched before the future is enqueued.
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

// Close stops the background goroutines. It is safe to call more than once.
func (p *streamFirstChunkPreloader) Close() error {
	p.cancel()
	return nil
}
