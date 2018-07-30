package chunk

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	ot "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/weaveworks/cortex/pkg/chunk/cache"
	"github.com/weaveworks/cortex/pkg/util"
)

func filterChunksByTime(from, through model.Time, chunks []Chunk) ([]Chunk, []string) {
	filtered := make([]Chunk, 0, len(chunks))
	keys := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.Through < from || through < chunk.From {
			continue
		}
		filtered = append(filtered, chunk)
		keys = append(keys, chunk.ExternalKey())
	}
	return filtered, keys
}

func filterChunksByMatchers(chunks []Chunk, filters []*labels.Matcher) []Chunk {
	filteredChunks := make([]Chunk, 0, len(chunks))
outer:
	for _, chunk := range chunks {
		for _, filter := range filters {
			if !filter.Matches(string(chunk.Metric[model.LabelName(filter.Name)])) {
				continue outer
			}
		}
		filteredChunks = append(filteredChunks, chunk)
	}
	return filteredChunks
}

// spanLogger unifies tracing and logging, to reduce repetition.
type spanLogger struct {
	log.Logger
	ot.Span
}

func newSpanLogger(ctx context.Context, method string, kvps ...interface{}) (*spanLogger, context.Context) {
	span, ctx := ot.StartSpanFromContext(ctx, method)
	logger := &spanLogger{
		Logger: log.With(util.WithContext(ctx, util.Logger), "method", method),
		Span:   span,
	}
	if len(kvps) > 0 {
		logger.Log(kvps...)
	}
	return logger, ctx
}

func (s *spanLogger) Log(kvps ...interface{}) error {
	s.Logger.Log(kvps...)
	fields, err := otlog.InterleavedKVToFields(kvps...)
	if err != nil {
		return err
	}
	s.Span.LogFields(fields...)
	return nil
}

// chunkFetcher deals with fetching chunk contents from the cache/store,
// and writing back any misses to the cache.
type chunkFetcher struct {
	storage StorageClient
	cache   cache.Cache
}

func newChunkFetcher(cfg cache.Config, storage StorageClient) (*chunkFetcher, error) {
	cache, err := cache.New(cfg)
	if err != nil {
		return nil, err
	}

	return &chunkFetcher{
		storage: storage,
		cache:   cache,
	}, nil
}

func (c *chunkFetcher) Stop() {
	c.cache.Stop()
}

func (c *chunkFetcher) fetchChunks(ctx context.Context, chunks []Chunk, keys []string) ([]Chunk, error) {
	log, ctx := newSpanLogger(ctx, "ChunkStore.fetchChunks")
	defer log.Span.Finish()

	// Now fetch the actual chunk data from Memcache / S3
	cacheHits, cacheBufs, _, err := c.cache.FetchChunkData(ctx, keys)
	if err != nil {
		level.Warn(log).Log("msg", "error fetching from cache", "err", err)
	}

	fromCache, missing, err := ProcessCacheResponse(chunks, cacheHits, cacheBufs)
	if err != nil {
		level.Warn(log).Log("msg", "error fetching from cache", "err", err)
	}

	fromStorage, err := c.storage.GetChunks(ctx, missing)

	// Always cache any chunks we did get
	if cacheErr := c.writeBackCache(ctx, fromStorage); cacheErr != nil {
		level.Warn(log).Log("msg", "could not store chunks in chunk cache", "err", cacheErr)
	}

	if err != nil {
		return nil, promql.ErrStorage(err)
	}

	allChunks := append(fromCache, fromStorage...)
	return allChunks, nil
}

func (c *chunkFetcher) writeBackCache(ctx context.Context, chunks []Chunk) error {
	for i := range chunks {
		encoded, err := chunks[i].Encode()
		if err != nil {
			return err
		}
		if err := c.cache.StoreChunk(ctx, chunks[i].ExternalKey(), encoded); err != nil {
			return err
		}
	}
	return nil
}

// ProcessCacheResponse decodes the chunks coming back from the cache, separating
// hits and misses.
func ProcessCacheResponse(chunks []Chunk, keys []string, bufs [][]byte) (found []Chunk, missing []Chunk, err error) {
	decodeContext := NewDecodeContext()

	i, j := 0, 0
	for i < len(chunks) && j < len(keys) {
		chunkKey := chunks[i].ExternalKey()

		if chunkKey < keys[j] {
			missing = append(missing, chunks[i])
			i++
		} else if chunkKey > keys[j] {
			level.Debug(util.Logger).Log("msg", "got chunk from cache we didn't ask for")
			j++
		} else {
			chunk := chunks[i]
			err = chunk.Decode(decodeContext, bufs[j])
			if err != nil {
				cacheCorrupt.Inc()
				return
			}
			found = append(found, chunk)
			i++
			j++
		}
	}

	for ; i < len(chunks); i++ {
		missing = append(missing, chunks[i])
	}

	return
}
