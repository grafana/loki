package chunk

import (
	"context"
	"sync"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/cortexproject/cortex/pkg/chunk/cache"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
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

func keysFromChunks(chunks []Chunk) []string {
	keys := make([]string, 0, len(chunks))
	for _, chk := range chunks {
		keys = append(keys, chk.ExternalKey())
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

func filterChunksByUniqueFingerprint(chunks []Chunk) ([]Chunk, []string) {
	filtered := make([]Chunk, 0, len(chunks))
	keys := make([]string, 0, len(chunks))
	uniqueFp := map[model.Fingerprint]struct{}{}

	for _, chunk := range chunks {
		if _, ok := uniqueFp[chunk.Fingerprint]; ok {
			continue
		}
		filtered = append(filtered, chunk)
		keys = append(keys, chunk.ExternalKey())
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
	storage    Client
	cache      cache.Cache
	cacheStubs bool

	wait           sync.WaitGroup
	decodeRequests chan decodeRequest
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
func NewChunkFetcher(cacher cache.Cache, cacheStubs bool, storage Client) (*Fetcher, error) {
	c := &Fetcher{
		storage:        storage,
		cache:          cacher,
		cacheStubs:     cacheStubs,
		decodeRequests: make(chan decodeRequest),
	}

	c.wait.Add(chunkDecodeParallelism)
	for i := 0; i < chunkDecodeParallelism; i++ {
		go c.worker()
	}

	return c, nil
}

// Stop the ChunkFetcher.
func (c *Fetcher) Stop() {
	close(c.decodeRequests)
	c.wait.Wait()
	c.cache.Stop()
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
	log, ctx := spanlogger.New(ctx, "ChunkStore.FetchChunks")
	defer log.Span.Finish()

	// Now fetch the actual chunk data from Memcache / S3
	cacheHits, cacheBufs, _ := c.cache.Fetch(ctx, keys)

	fromCache, missing, err := c.processCacheResponse(ctx, chunks, cacheHits, cacheBufs)
	if err != nil {
		level.Warn(log).Log("msg", "error fetching from cache", "err", err)
	}

	var fromStorage []Chunk
	if len(missing) > 0 {
		fromStorage, err = c.storage.GetChunks(ctx, missing)
	}

	// Always cache any chunks we did get
	if cacheErr := c.writeBackCache(ctx, fromStorage); cacheErr != nil {
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

		keys = append(keys, chunks[i].ExternalKey())
		bufs = append(bufs, encoded)
	}

	c.cache.Store(ctx, keys, bufs)
	return nil
}

// ProcessCacheResponse decodes the chunks coming back from the cache, separating
// hits and misses.
func (c *Fetcher) processCacheResponse(ctx context.Context, chunks []Chunk, keys []string, bufs [][]byte) ([]Chunk, []Chunk, error) {
	var (
		requests  = make([]decodeRequest, 0, len(keys))
		responses = make(chan decodeResponse)
		missing   []Chunk
	)
	log, _ := spanlogger.New(ctx, "Fetcher.processCacheResponse")
	defer log.Span.Finish()

	i, j := 0, 0
	for i < len(chunks) && j < len(keys) {
		chunkKey := chunks[i].ExternalKey()

		if chunkKey < keys[j] {
			missing = append(missing, chunks[i])
			i++
		} else if chunkKey > keys[j] {
			level.Warn(util_log.Logger).Log("msg", "got chunk from cache we didn't ask for")
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
	level.Debug(log).Log("chunks", len(chunks), "decodeRequests", len(requests), "missing", len(missing))

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
