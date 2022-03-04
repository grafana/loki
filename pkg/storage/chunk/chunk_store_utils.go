package chunk

import (
	"context"
	"sync"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
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
)

const chunkDecodeParallelism = 16

func filterChunksByTime(from, through model.Time, chunks []Chunk) []Chunk {
	filtered := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.Through < from || through < chunk.From {
			continue
		}
		filtered = append(filtered, chunk)
	}
	return filtered
}

func keysFromChunks(s SchemaConfig, chunks []Chunk) []string {
	keys := make([]string, 0, len(chunks))
	for _, chk := range chunks {
		keys = append(keys, s.ExternalKey(chk))
	}

	return keys
}

func labelNamesFromChunks(chunks []Chunk) []string {
	var result UniqueStrings
	for _, c := range chunks {
		for _, l := range c.Metric {
			result.Add(l.Name)
		}
	}
	return result.Strings()
}

func filterChunksByUniqueFingerprint(s SchemaConfig, chunks []Chunk) ([]Chunk, []string) {
	filtered := make([]Chunk, 0, len(chunks))
	keys := make([]string, 0, len(chunks))
	uniqueFp := map[model.Fingerprint]struct{}{}

	for _, chunk := range chunks {
		if _, ok := uniqueFp[chunk.Fingerprint]; ok {
			continue
		}
		filtered = append(filtered, chunk)
		keys = append(keys, s.ExternalKey(chunk))
		uniqueFp[chunk.Fingerprint] = struct{}{}
	}
	return filtered, keys
}

func filterChunksByMatchers(chunks []Chunk, filters []*labels.Matcher) []Chunk {
	filteredChunks := make([]Chunk, 0, len(chunks))
outer:
	for _, chunk := range chunks {
		for _, filter := range filters {
			if !filter.Matches(chunk.Metric.Get(filter.Name)) {
				continue outer
			}
		}
		filteredChunks = append(filteredChunks, chunk)
	}
	return filteredChunks
}

// Fetcher deals with fetching chunk contents from the cache/store,
// and writing back any misses to the cache.  Also responsible for decoding
// chunks from the cache, in parallel.
type Fetcher struct {
	schema     SchemaConfig
	storage    Client
	cache      cache.Cache
	cacheStubs bool

	wait           sync.WaitGroup
	decodeRequests chan decodeRequest

	maxAsyncConcurrency int
	maxAsyncBufferSize  int

	asyncQueue chan []Chunk
	stop       chan struct{}
}

type decodeRequest struct {
	chunk     Chunk
	buf       []byte
	responses chan decodeResponse
}

type decodeResponse struct {
	chunk Chunk
	err   error
}

// NewChunkFetcher makes a new ChunkFetcher.
func NewChunkFetcher(cacher cache.Cache, cacheStubs bool, schema SchemaConfig, storage Client, maxAsyncConcurrency int, maxAsyncBufferSize int) (*Fetcher, error) {
	c := &Fetcher{
		schema:              schema,
		storage:             storage,
		cache:               cacher,
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
	c.asyncQueue = make(chan []Chunk, c.maxAsyncBufferSize)
	for i := 0; i < c.maxAsyncConcurrency; i++ {
		go c.asyncWriteBackCacheQueueProcessLoop()
	}

	return c, nil
}

func (c *Fetcher) writeBackCacheAsync(fromStorage []Chunk) error {
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
			cacheErr := c.writeBackCache(context.Background(), fromStorage)
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
	close(c.decodeRequests)
	c.wait.Wait()
	c.cache.Stop()
	close(c.stop)
}

func (c *Fetcher) worker() {
	defer c.wait.Done()
	decodeContext := NewDecodeContext()
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

// FetchChunks fetches a set of chunks from cache and store. Note that the keys passed in must be
// lexicographically sorted, while the returned chunks are not in the same order as the passed in chunks.
func (c *Fetcher) FetchChunks(ctx context.Context, chunks []Chunk, keys []string) ([]Chunk, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	log, ctx := spanlogger.New(ctx, "ChunkStore.FetchChunks")
	defer log.Span.Finish()

	// Now fetch the actual chunk data from Memcache / S3
	cacheHits, cacheBufs, _, err := c.cache.Fetch(ctx, keys)
	if err != nil {
		level.Warn(log).Log("msg", "error fetching from cache", "err", err)
	}
	fromCache, missing, err := c.processCacheResponse(ctx, chunks, cacheHits, cacheBufs)
	if err != nil {
		level.Warn(log).Log("msg", "error process response from cache", "err", err)
	}

	var fromStorage []Chunk
	if len(missing) > 0 {
		fromStorage, err = c.storage.GetChunks(ctx, missing)
	}

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

func (c *Fetcher) writeBackCache(ctx context.Context, chunks []Chunk) error {
	keys := make([]string, 0, len(chunks))
	bufs := make([][]byte, 0, len(chunks))
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

		keys = append(keys, c.schema.ExternalKey(chunks[i]))
		bufs = append(bufs, encoded)
	}

	err := c.cache.Store(ctx, keys, bufs)
	if err != nil {
		level.Warn(util_log.Logger).Log("msg", "writeBackCache cache store fail", "err", err)
	}
	return nil
}

// ProcessCacheResponse decodes the chunks coming back from the cache, separating
// hits and misses.
func (c *Fetcher) processCacheResponse(ctx context.Context, chunks []Chunk, keys []string, bufs [][]byte) ([]Chunk, []Chunk, error) {
	var (
		requests  = make([]decodeRequest, 0, len(keys))
		responses = make(chan decodeResponse)
		missing   []Chunk
		logger    = util_log.WithContext(ctx, util_log.Logger)
	)

	i, j := 0, 0
	for i < len(chunks) && j < len(keys) {
		chunkKey := c.schema.ExternalKey(chunks[i])

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
		found []Chunk
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
