package executor

import (
	"context"
	"errors"

	"github.com/apache/arrow-go/v18/arrow"
)

// CacheStore is the interface for reading and writing cached task results.
type CacheStore interface {
	// Get retrieves cached records for the given key. It returns the records,
	// true if a cache hit occurred, or an error.
	Get(ctx context.Context, key string) ([]arrow.RecordBatch, bool, error)
	// Set stores records in the cache for the given key.
	Set(ctx context.Context, key string, records []arrow.RecordBatch) error
}

// cachingPipeline wraps a [Pipeline] and transparently stores and retrieves
// [arrow.RecordBatch] results from a [CacheStore].
//
// On a cache hit: Open reads from the store; Read serves stored records without calling Read on the inner pipeline.
// On a cache miss: Read streams through the inner pipeline and CacheStore.Set is called
// on EOF to populate the cache.
type cachingPipeline struct {
	inner  Pipeline
	store  CacheStore
	key    string
	cached []arrow.RecordBatch // records served from cache (hit path) or collected to put them on the cache (miss path)
	pos    int                 // current position in cached slice (hit path)
	hit    bool                // true when Open found a cache hit
}

// Open implements Pipeline.
func (p *cachingPipeline) Open(ctx context.Context) error {
	records, hit, err := p.store.Get(ctx, p.key)
	if err != nil {
		return err
	}
	if hit {
		p.cached = records
		p.hit = true
		return nil
	}

	// Cache miss; open the inner pipeline for reads
	return p.inner.Open(ctx)
}

// Read implements Pipeline.
func (p *cachingPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if p.hit {
		// We already returned all cached records
		if p.pos >= len(p.cached) {
			return nil, EOF
		}

		// Return the cached record and move to the next one
		rec := p.cached[p.pos]
		p.pos++
		return rec, nil
	}

	// Cache miss; read from the inner pipeline
	rec, err := p.inner.Read(ctx)
	if err != nil {
		if errors.Is(err, EOF) {
			// Best-effort: populate the cache. Ignore errors so the caller
			// still receives EOF and can proceed.
			_ = p.store.Set(ctx, p.key, p.cached)
		}
		return nil, err
	}

	// Store response for caching once Read completes (EOF from the inner pipeline)
	p.cached = append(p.cached, rec)
	return rec, nil
}

// Close implements Pipeline.
func (p *cachingPipeline) Close() {
	if !p.hit {
		p.inner.Close()
	}
}
