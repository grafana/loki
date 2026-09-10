package fetcher

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/congestion"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

var tracer = otel.Tracer("pkg/storage/chunk/fetcher")

var (
	cacheCorrupt = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "cache_corrupt_chunks_total",
		Help:      "Total count of corrupt chunks found in cache.",
	})
	chunkFetchedSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: constants.Loki,
		Subsystem: "chunk_fetcher",
		Name:      "fetched_size_bytes",
		Help:      "Compressed chunk size distribution fetched from storage.",
		// TODO: expand these buckets if we ever make larger chunks
		// TODO: consider adding `chunk_target_size` to this list in case users set very large chunk sizes
		Buckets: []float64{128, 1024, 16 * 1024, 64 * 1024, 128 * 1024, 256 * 1024, 512 * 1024, 1024 * 1024, 1.5 * 1024 * 1024, 2 * 1024 * 1024, 4 * 1024 * 1024},
	}, []string{"source"})
	storageErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Subsystem: "chunk_fetcher",
		Name:      "storage_errors_total",
		Help:      "Storage errors encountered by the chunk fetcher.",
	}, []string{"reason"})
)

const (
	storageErrorNotFound  = "not_found"
	storageErrorRetryable = "retryable"
	storageErrorOther     = "other"
)

const chunkDecodeParallelism = 16

const (
	traceRequestedChunks        = "chunkstore.requested_chunks"
	traceReturnedChunks         = "chunkstore.returned_chunks"
	traceCacheHits              = "chunkstore.cache_hits"
	traceCacheMisses            = "chunkstore.cache_misses"
	traceCacheBytes             = "chunkstore.cache_bytes"
	traceStorageRequestedChunks = "chunkstore.storage_requested_chunks"
	traceStorageFetchedChunks   = "chunkstore.storage_fetched_chunks"
	traceStorageBytes           = "chunkstore.storage_bytes"
	traceCacheErrors            = "chunkstore.cache_errors"
	traceStorageErrors          = "chunkstore.storage_errors"
)

// Fetcher deals with fetching chunk contents from the cache/store,
// and writing back any misses to the cache.  Also responsible for decoding
// chunks from the cache, in parallel.
type Fetcher struct {
	schema     config.SchemaConfig
	storage    client.Client
	cache      cache.Cache
	cachel2    cache.Cache
	cacheStubs bool

	l2CacheHandoff                   time.Duration
	skipQueryWritebackCacheOlderThan time.Duration
	propagateChunkFetchErrors        bool

	wait           sync.WaitGroup
	decodeRequests chan decodeRequest

	stopOnce sync.Once
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
func New(cache cache.Cache, cachel2 cache.Cache, cacheStubs bool, schema config.SchemaConfig, storage client.Client, l2CacheHandoff time.Duration, skipQueryWritebackOlderThan time.Duration, propagateChunkFetchErrors bool) (*Fetcher, error) {
	c := &Fetcher{
		schema:                           schema,
		storage:                          storage,
		cache:                            cache,
		cachel2:                          cachel2,
		l2CacheHandoff:                   l2CacheHandoff,
		cacheStubs:                       cacheStubs,
		skipQueryWritebackCacheOlderThan: skipQueryWritebackOlderThan,
		propagateChunkFetchErrors:        propagateChunkFetchErrors,
		decodeRequests:                   make(chan decodeRequest),
	}

	c.wait.Add(chunkDecodeParallelism)
	for i := 0; i < chunkDecodeParallelism; i++ {
		go c.worker()
	}

	return c, nil
}

// Stop the ChunkFetcher.
func (c *Fetcher) Stop() {
	c.stopOnce.Do(func() {
		close(c.decodeRequests)
		c.wait.Wait()
		c.cache.Stop()
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
func (c *Fetcher) FetchChunks(ctx context.Context, chunks []chunk.Chunk) (result []chunk.Chunk, operationErr error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	ctx, sp := tracer.Start(ctx, "ChunkStore.FetchChunks")
	log := spanlogger.FromContext(ctx, util_log.Logger)
	defer log.Finish()

	var traceStats fetchTraceStats
	traceStats.requestedChunks = len(chunks)
	defer func() {
		sp.SetAttributes(
			attribute.Int(traceRequestedChunks, traceStats.requestedChunks),
			attribute.Int(traceReturnedChunks, len(result)),
			attribute.Int(traceCacheHits, traceStats.cacheHits),
			attribute.Int(traceCacheMisses, traceStats.cacheMisses),
			attribute.Int(traceCacheBytes, traceStats.cacheBytes),
			attribute.Int(traceStorageRequestedChunks, traceStats.storageRequestedChunks),
			attribute.Int(traceStorageFetchedChunks, traceStats.storageFetchedChunks),
			attribute.Int(traceStorageBytes, traceStats.storageBytes),
			attribute.Int(traceCacheErrors, traceStats.cacheErrors),
			attribute.Int(traceStorageErrors, traceStats.storageErrors),
		)
		if operationErr != nil {
			sp.SetStatus(codes.Error, operationErr.Error())
			sp.RecordError(operationErr)
		}
		sp.End()
	}()

	// Extend the extendedHandoff to be 10% larger to allow for some overlap because this is a sliding window
	// and the l1 cache may be oversized enough to allow for some extra chunks
	extendedHandoff := c.l2CacheHandoff + (c.l2CacheHandoff / 10)

	keys := make([]string, 0, len(chunks))
	l2OnlyChunks := make([]chunk.Chunk, 0, len(chunks))

	for _, m := range chunks {
		if c.skipQueryWritebackCacheOlderThan > 0 && m.From.Time().Before(time.Now().UTC().Add(-c.skipQueryWritebackCacheOlderThan)) {
			continue
		}
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
	cacheHits, cacheBufs, _, l1CacheErr := c.cache.Fetch(ctx, keys)
	if l1CacheErr != nil {
		traceStats.cacheErrors++
		level.Warn(log).Log("msg", "error fetching from cache", "err", l1CacheErr)
	}
	traceStats.cacheHits += len(cacheHits)
	traceStats.cacheBytes += cacheBufferBytes(cacheBufs)

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
			traceStats.cacheErrors++
			level.Warn(log).Log("msg", "error fetching from cache", "err", err)
		}
		traceStats.cacheHits += len(cacheHitsL2)
		traceStats.cacheBytes += cacheBufferBytes(cacheBufsL2)

		for _, buf := range cacheBufsL2 {
			chunkFetchedSize.WithLabelValues("cache_l2").Observe(float64(len(buf)))
		}

		cacheHits = append(cacheHits, cacheHitsL2...)
		cacheBufs = append(cacheBufs, cacheBufsL2...)
	}

	// processCacheResponse will decode all the fetched chunks and also provide us with a list of
	// missing chunks that we need to fetch from the storage layer
	fromCache, missing, cacheDecodeErr := c.processCacheResponse(ctx, chunks, cacheHits, cacheBufs)
	if cacheDecodeErr != nil {
		traceStats.cacheErrors++
		level.Warn(log).Log("msg", "error process response from cache", "err", cacheDecodeErr)
	}
	traceStats.cacheMisses = len(missing)
	traceStats.storageRequestedChunks = len(missing)

	var (
		fromStorage []chunk.Chunk
		storageErr  error
	)
	if len(missing) > 0 {
		fromStorage, storageErr = c.storage.GetChunks(ctx, missing)
	}

	// normally these stats would be collected by the cache.statsCollector wrapper, but chunks are written back
	// to the cache asynchronously in the background and we lose the context
	var bytes int
	for _, c := range fromStorage {
		size := c.Data.Size()
		bytes += size

		chunkFetchedSize.WithLabelValues("store").Observe(float64(size))
	}
	traceStats.storageFetchedChunks = len(fromStorage)
	traceStats.storageBytes = bytes

	st := stats.FromContext(ctx)
	st.AddCacheEntriesStored(stats.ChunkCache, len(fromStorage))
	st.AddCacheBytesSent(stats.ChunkCache, bytes)

	if storageErr != nil {
		traceStats.storageErrors++
		if !errors.Is(storageErr, context.Canceled) && !errors.Is(storageErr, context.DeadlineExceeded) {
			storageErrors.WithLabelValues(c.storageErrorReason(storageErr)).Inc()
			if failures := len(missing) - len(fromStorage); failures > 0 {
				st.AddChunkFetchFailures(int64(failures))
			}
		}
		level.Error(log).Log("msg", "failed downloading chunks", "err", storageErr)

		if c.propagateChunkFetchErrors {
			operationErr = storageErr
			return nil, operationErr
		}
	}

	cacheStoreErrors, cacheErr := c.writeBackCache(ctx, fromStorage)
	traceStats.cacheErrors += cacheStoreErrors
	if cacheErr != nil {
		traceStats.cacheErrors++
		level.Warn(log).Log("msg", "could not store chunks in chunk cache", "err", cacheErr)
	}

	return append(fromCache, fromStorage...), nil
}

type fetchTraceStats struct {
	requestedChunks        int
	cacheHits              int
	cacheMisses            int
	cacheBytes             int
	storageRequestedChunks int
	storageFetchedChunks   int
	storageBytes           int
	cacheErrors            int
	storageErrors          int
}

func cacheBufferBytes(bufs [][]byte) int {
	bytes := 0
	for _, buf := range bufs {
		bytes += len(buf)
	}
	return bytes
}

func (c *Fetcher) WriteBackCache(ctx context.Context, chunks []chunk.Chunk) error {
	_, err := c.writeBackCache(ctx, chunks)
	return err
}

func (c *Fetcher) writeBackCache(ctx context.Context, chunks []chunk.Chunk) (int, error) {
	keys := make([]string, 0, len(chunks))
	bufs := make([][]byte, 0, len(chunks))
	keysL2 := make([]string, 0, len(chunks))
	bufsL2 := make([][]byte, 0, len(chunks))
	for i := range chunks {
		if c.skipQueryWritebackCacheOlderThan > 0 && chunks[i].From.Time().Before(time.Now().UTC().Add(-c.skipQueryWritebackCacheOlderThan)) {
			continue
		}

		var encoded []byte
		var err error
		if !c.cacheStubs {
			encoded, err = chunks[i].Encoded()
			// TODO don't fail, just log and continue?
			if err != nil {
				return 0, err
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

	storeErrors := 0
	err := c.cache.Store(ctx, keys, bufs)
	if err != nil {
		storeErrors++
		level.Warn(util_log.Logger).Log("msg", "writeBackCache cache store fail", "err", err)
	}
	if len(keysL2) > 0 {
		err = c.cachel2.Store(ctx, keysL2, bufsL2)
		if err != nil {
			storeErrors++
			level.Warn(util_log.Logger).Log("msg", "writeBackCacheL2 cache store fail", "err", err)
		}
	}

	return storeErrors, nil
}

// ProcessCacheResponse decodes the chunks coming back from the cache, separating
// hits and misses. A chunk that fails to decode is in neither result, so the
// caller never sees it and never asks storage for it.
func (c *Fetcher) processCacheResponse(ctx context.Context, chunks []chunk.Chunk, keys []string, bufs [][]byte) ([]chunk.Chunk, []chunk.Chunk, error) {
	var (
		requests  = make([]decodeRequest, 0, len(keys))
		responses = make(chan decodeResponse)
		missing   []chunk.Chunk
		logger    = util_log.WithContext(ctx, util_log.Logger)
	)

	cm := make(map[string][]byte, len(chunks))
	for i, k := range keys {
		cm[k] = bufs[i]
	}

	for i, ck := range chunks {
		if b, ok := cm[c.schema.ExternalKey(ck.ChunkRef)]; ok {
			requests = append(requests, decodeRequest{
				chunk:     chunks[i],
				buf:       b,
				responses: responses,
			})
		} else {
			missing = append(missing, chunks[i])
		}
	}

	level.Debug(logger).Log("chunks", len(chunks), "decodeRequests", len(requests), "missing", len(missing))

	go func() {
		for _, request := range requests {
			c.decodeRequests <- request
		}
	}()

	var err error
	found := make([]chunk.Chunk, 0, len(requests))
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

func (c *Fetcher) storageErrorReason(err error) string {
	if c.storage.IsChunkNotFoundErr(err) {
		return storageErrorNotFound
	}
	if c.storage.IsRetryableErr(err) {
		return storageErrorRetryable
	}

	if errors.Is(err, congestion.RetriesExceeded) {
		return storageErrorRetryable
	}
	return storageErrorOther
}

func (c *Fetcher) IsChunkNotFoundErr(err error) bool {
	return c.storage.IsChunkNotFoundErr(err)
}
