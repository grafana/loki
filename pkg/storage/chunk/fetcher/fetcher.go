package fetcher

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

var (
	errAsyncBufferFull = errors.New("the async buffer is full")
	skipped            = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loki_chunk_fetcher_cache_skipped_buffer_full_total",
		Help: "Total number of operations against cache that have been skipped.",
	})
	chunkFetcherCacheQueueEnqueue = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loki_chunk_fetcher_cache_enqueued_total",
		Help: "Total number of chunks enqueued to a buffer to be asynchronously written back to the chunk cache.",
	})
	chunkFetcherCacheQueueDequeue = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loki_chunk_fetcher_cache_dequeued_total",
		Help: "Total number of chunks asynchronously dequeued from a buffer and written back to the chunk cache.",
	})
	cacheCorrupt = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "cache_corrupt_chunks_total",
		Help:      "Total count of corrupt chunks found in cache.",
	})
	chunkFetchedSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Subsystem: "chunk_fetcher",
		Name:      "fetched_size_bytes",
		Help:      "Compressed chunk size distribution fetched from storage.",
		// TODO: expand these buckets if we ever make larger chunks
		// TODO: consider adding `chunk_target_size` to this list in case users set very large chunk sizes
		Buckets: []float64{128, 1024, 16 * 1024, 64 * 1024, 128 * 1024, 256 * 1024, 512 * 1024, 1024 * 1024, 1.5 * 1024 * 1024, 2 * 1024 * 1024, 4 * 1024 * 1024},
	}, []string{"source"})
)

const chunkDecodeParallelism = 16

// Fetcher deals with fetching chunk contents from the cache/store,
// and writing back any misses to the cache.  Also responsible for decoding
// chunks from the cache, in parallel.
type Fetcher struct {
	schema     config.SchemaConfig
	storage    client.Client
	cache      cache.Cache
	cachel2    cache.Cache
	cacheStubs bool

	l2CacheHandoff time.Duration

	wait           sync.WaitGroup
	decodeRequests chan decodeRequest

	maxAsyncConcurrency int
	maxAsyncBufferSize  int

	asyncQueue chan []chunk.Chunk
	stopOnce   sync.Once
	stop       chan struct{}
}

type decodeRequest struct {
	chunk     chunk.Chunk
	buf       []byte
	responses chan decodeResponse
}

type decodeResponse struct {
	chunk chunk.Chunk
	err   error
}

// New makes a new ChunkFetcher.
func New(cache cache.Cache, cachel2 cache.Cache, cacheStubs bool, schema config.SchemaConfig, storage client.Client, maxAsyncConcurrency int, maxAsyncBufferSize int, l2CacheHandoff time.Duration) (*Fetcher, error) {
	c := &Fetcher{
		schema:              schema,
		storage:             storage,
		cache:               cache,
		cachel2:             cachel2,
		l2CacheHandoff:      l2CacheHandoff,
		cacheStubs:          cacheStubs,
		decodeRequests:      make(chan decodeRequest),
		maxAsyncConcurrency: maxAsyncConcurrency,
		maxAsyncBufferSize:  maxAsyncBufferSize,
		stop:                make(chan struct{}),
	}

	c.wait.Add(chunkDecodeParallelism)
	for i := 0; i < chunkDecodeParallelism; i++ {
		go c.worker()
	}

	// Start a number of goroutines - processing async operations - equal
	// to the max concurrency we have.
	c.asyncQueue = make(chan []chunk.Chunk, c.maxAsyncBufferSize)
	for i := 0; i < c.maxAsyncConcurrency; i++ {
		go c.asyncWriteBackCacheQueueProcessLoop()
	}

	return c, nil
}

func (c *Fetcher) writeBackCacheAsync(fromStorage []chunk.Chunk) error {
	select {
	case c.asyncQueue <- fromStorage:
		chunkFetcherCacheQueueEnqueue.Add(float64(len(fromStorage)))
		return nil
	default:
		return errAsyncBufferFull
	}
}

func (c *Fetcher) asyncWriteBackCacheQueueProcessLoop() {
	for {
		select {
		case fromStorage := <-c.asyncQueue:
			chunkFetcherCacheQueueDequeue.Add(float64(len(fromStorage)))
			cacheErr := c.WriteBackCache(context.Background(), fromStorage)
			if cacheErr != nil {
				level.Warn(util_log.Logger).Log("msg", "could not write fetched chunks from storage into chunk cache", "err", cacheErr)
			}
		case <-c.stop:
			return
		}
	}
}

// Stop the ChunkFetcher.
func (c *Fetcher) Stop() {
	c.stopOnce.Do(func() {
		close(c.decodeRequests)
		c.wait.Wait()
		c.cache.Stop()
		close(c.stop)
	})
}

func (c *Fetcher) worker() {
	defer c.wait.Done()
	decodeContext := chunk.NewDecodeContext()
	for req := range c.decodeRequests {
		err := req.chunk.Decode(decodeContext, req.buf)
		if err != nil {
			cacheCorrupt.Inc()
		}
		req.responses <- decodeResponse{
			chunk: req.chunk,
			err:   err,
		}
	}
}

func (c *Fetcher) Cache() cache.Cache {
	return c.cache
}

func (c *Fetcher) Client() client.Client {
	return c.storage
}

// FetchChunks fetches a set of chunks from cache and store. Note, returned chunks are not in the same order they are passed in
func (c *Fetcher) FetchChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	sp, ctx := opentracing.StartSpanFromContext(ctx, "ChunkStore.FetchChunks")
	defer sp.Finish()
	log := spanlogger.FromContext(ctx)
	defer log.Span.Finish()

	// processCacheResponse expects the chunks to be sorted lexographically by external key
	sort.Slice(chunks, func(x, y int) bool {
		return c.schema.ExternalKey(chunks[x].ChunkRef) < c.schema.ExternalKey(chunks[y].ChunkRef)
	})

	// Extend the extendedHandoff to be 10% larger to allow for some overlap becasue this is a sliding window
	// and the l1 cache may be oversized enough to allow for some extra chunks
	extendedHandoff := c.l2CacheHandoff + (c.l2CacheHandoff / 10)

	keys := make([]string, 0, len(chunks))
	l2OnlyChunks := make([]chunk.Chunk, 0, len(chunks))

	for _, m := range chunks {
		// Similar to below, this is an optimization to not bother looking in the l1 cache if there isn't a reasonable
		// expectation to find it there.
		if c.l2CacheHandoff > 0 && m.From.Time().Before(time.Now().UTC().Add(-extendedHandoff)) {
			l2OnlyChunks = append(l2OnlyChunks, m)
			continue
		}
		chunkKey := c.schema.ExternalKey(m.ChunkRef)
		keys = append(keys, chunkKey)
	}

	// Fetch from L1 chunk cache
	cacheHits, cacheBufs, _, err := c.cache.Fetch(ctx, keys)
	if err != nil {
		level.Warn(log).Log("msg", "error fetching from cache", "err", err)
	}

	for _, buf := range cacheBufs {
		chunkFetchedSize.WithLabelValues("cache").Observe(float64(len(buf)))
	}

	if c.l2CacheHandoff > 0 {
		// Fetch missing from L2 chunks cache
		missingL1Keys := make([]string, 0, len(l2OnlyChunks))
		for _, m := range l2OnlyChunks {
			// A small optimization to prevent looking up a chunk in l2 cache that can't possibly be there
			if m.From.Time().After(time.Now().UTC().Add(-c.l2CacheHandoff)) {
				continue
			}
			chunkKey := c.schema.ExternalKey(m.ChunkRef)
			missingL1Keys = append(missingL1Keys, chunkKey)
		}

		cacheHitsL2, cacheBufsL2, _, err := c.cachel2.Fetch(ctx, missingL1Keys)
		if err != nil {
			level.Warn(log).Log("msg", "error fetching from cache", "err", err)
		}

		for _, buf := range cacheBufsL2 {
			chunkFetchedSize.WithLabelValues("cache_l2").Observe(float64(len(buf)))
		}

		cacheHits = append(cacheHits, cacheHitsL2...)
		cacheBufs = append(cacheBufs, cacheBufsL2...)
	}

	// processCacheResponse expects all the keys to be sorted lexographically
	crs := cacheResultSorter{cacheHits, cacheBufs}
	sort.Sort(crs)

	// processCacheResponse will decode all the fetched chunks and also provide us with a list of
	// missing chunks that we need to fetch from the storage layer
	fromCache, missing, err := c.processCacheResponse(ctx, chunks, cacheHits, cacheBufs)
	if err != nil {
		level.Warn(log).Log("msg", "error process response from cache", "err", err)
	}

	// Fetch missing from storage
	var fromStorage []chunk.Chunk
	if len(missing) > 0 {
		fromStorage, err = c.storage.GetChunks(ctx, missing)
	}

	// normally these stats would be collected by the cache.statsCollector wrapper, but chunks are written back
	// to the cache asynchronously in the background and we lose the context
	var bytes int
	for _, c := range fromStorage {
		size := c.Data.Size()
		bytes += size

		chunkFetchedSize.WithLabelValues("store").Observe(float64(size))
	}

	st := stats.FromContext(ctx)
	st.AddCacheEntriesStored(stats.ChunkCache, len(fromStorage))
	st.AddCacheBytesSent(stats.ChunkCache, bytes)

	// Always cache any chunks we did get
	if cacheErr := c.writeBackCacheAsync(fromStorage); cacheErr != nil {
		if cacheErr == errAsyncBufferFull {
			skipped.Inc()
		}
		level.Warn(log).Log("msg", "could not store chunks in chunk cache", "err", cacheErr)
	}

	if err != nil {
		// Don't rely on Cortex error translation here.
		return nil, promql.ErrStorage{Err: err}
	}

	allChunks := append(fromCache, fromStorage...)
	return allChunks, nil
}

func (c *Fetcher) WriteBackCache(ctx context.Context, chunks []chunk.Chunk) error {
	keys := make([]string, 0, len(chunks))
	bufs := make([][]byte, 0, len(chunks))
	keysL2 := make([]string, 0, len(chunks))
	bufsL2 := make([][]byte, 0, len(chunks))
	for i := range chunks {
		var encoded []byte
		var err error
		if !c.cacheStubs {
			encoded, err = chunks[i].Encoded()
			// TODO don't fail, just log and continue?
			if err != nil {
				return err
			}
		}
		// Determine which cache we should write to
		if c.l2CacheHandoff == 0 || chunks[i].From.Time().After(time.Now().UTC().Add(-c.l2CacheHandoff)) {
			// Write to L1 cache
			keys = append(keys, c.schema.ExternalKey(chunks[i].ChunkRef))
			bufs = append(bufs, encoded)
		} else {
			// Write to L2 cache
			keysL2 = append(keysL2, c.schema.ExternalKey(chunks[i].ChunkRef))
			bufsL2 = append(bufsL2, encoded)
		}
	}

	err := c.cache.Store(ctx, keys, bufs)
	if err != nil {
		level.Warn(util_log.Logger).Log("msg", "writeBackCache cache store fail", "err", err)
	}
	if len(keysL2) > 0 {
		err = c.cachel2.Store(ctx, keysL2, bufsL2)
		if err != nil {
			level.Warn(util_log.Logger).Log("msg", "writeBackCacheL2 cache store fail", "err", err)
		}
	}

	return nil
}

// ProcessCacheResponse decodes the chunks coming back from the cache, separating
// hits and misses.
func (c *Fetcher) processCacheResponse(ctx context.Context, chunks []chunk.Chunk, keys []string, bufs [][]byte) ([]chunk.Chunk, []chunk.Chunk, error) {
	var (
		requests  = make([]decodeRequest, 0, len(keys))
		responses = make(chan decodeResponse)
		missing   []chunk.Chunk
		logger    = util_log.WithContext(ctx, util_log.Logger)
	)

	i, j := 0, 0
	for i < len(chunks) && j < len(keys) {
		chunkKey := c.schema.ExternalKey(chunks[i].ChunkRef)

		if chunkKey < keys[j] {
			missing = append(missing, chunks[i])
			i++
		} else if chunkKey > keys[j] {
			level.Warn(logger).Log("msg", "got chunk from cache we didn't ask for")
			j++
		} else {
			requests = append(requests, decodeRequest{
				chunk:     chunks[i],
				buf:       bufs[j],
				responses: responses,
			})
			i++
			j++
		}
	}
	for ; i < len(chunks); i++ {
		missing = append(missing, chunks[i])
	}
	level.Debug(logger).Log("chunks", len(chunks), "decodeRequests", len(requests), "missing", len(missing))

	go func() {
		for _, request := range requests {
			c.decodeRequests <- request
		}
	}()

	var (
		err   error
		found []chunk.Chunk
	)
	for i := 0; i < len(requests); i++ {
		response := <-responses

		// Don't exit early, as we don't want to block the workers.
		if response.err != nil {
			err = response.err
		} else {
			found = append(found, response.chunk)
		}
	}
	return found, missing, err
}

func (c *Fetcher) IsChunkNotFoundErr(err error) bool {
	return c.storage.IsChunkNotFoundErr(err)
}

// cacheResultSorter is a helper type to sort the results of a cache lookup.
type cacheResultSorter struct {
	keys  []string
	buffs [][]byte
}

func (crs cacheResultSorter) Len() int {
	return len(crs.keys)
}

func (crs cacheResultSorter) Swap(i, j int) {
	crs.keys[i], crs.keys[j] = crs.keys[j], crs.keys[i]
	crs.buffs[i], crs.buffs[j] = crs.buffs[j], crs.buffs[i]
}

func (crs cacheResultSorter) Less(i, j int) bool {
	return crs.keys[i] < crs.keys[j]
}
