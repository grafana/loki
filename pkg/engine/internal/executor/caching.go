package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// newCachingPipeline wraps inner in a [cachingPipeline] backed by c.
// If c is nil, inner is returned unchanged (no caching).
func newCachingPipeline(c cache.Cache, inner Pipeline, key string) Pipeline {
	if c == nil {
		return inner
	}
	return &cachingPipeline{
		inner: inner,
		store: &arrowCacheAdapter{cache: c},
		key:   key,
	}
}

// cachingPipeline wraps a [Pipeline] and transparently stores and retrieves
// [arrow.RecordBatch] results from an [arrowCacheAdapter].
//
// On a cache hit: Open reads from the store; Read serves stored records without calling Read on the inner pipeline.
// On a cache miss: Read streams through the inner pipeline and the store is populated on EOF.
type cachingPipeline struct {
	inner  Pipeline
	store  *arrowCacheAdapter
	key    string
	cached []arrow.RecordBatch // records served from cache (hit path) or collected to put them on the cache (miss path)
	pos    int                 // current position in cached slice (hit path)
	hit    bool                // true when Open found a cache hit
}

// Open implements Pipeline.
func (p *cachingPipeline) Open(ctx context.Context) error {
	records, hit, err := p.store.get(ctx, p.key)
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
			_ = p.store.set(ctx, p.key, p.cached)
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

// arrowCacheAdapter adapts a [cache.Cache] (byte-oriented) to Arrow record
// batch reads/writes using Arrow IPC stream encoding.
type arrowCacheAdapter struct {
	cache cache.Cache
}

func (s *arrowCacheAdapter) get(ctx context.Context, key string) ([]arrow.RecordBatch, bool, error) {
	found, bufs, missing, err := s.cache.Fetch(ctx, []string{key})
	if err != nil {
		return nil, false, err
	}
	if len(missing) > 0 || len(found) == 0 {
		return nil, false, nil
	}
	records, err := decodeRecords(bufs[0])
	if err != nil {
		return nil, false, fmt.Errorf("decoding cached records: %w", err)
	}
	return records, true, nil
}

func (s *arrowCacheAdapter) set(ctx context.Context, key string, records []arrow.RecordBatch) error {
	if len(records) == 0 {
		return nil
	}
	buf, err := encodeRecords(records)
	if err != nil {
		return fmt.Errorf("encoding records for cache: %w", err)
	}
	return s.cache.Store(ctx, []string{key}, [][]byte{buf})
}

func encodeRecords(records []arrow.RecordBatch) ([]byte, error) {
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(records[0].Schema()))
	for _, rec := range records {
		if err := w.Write(rec); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeRecords(data []byte) ([]arrow.RecordBatch, error) {
	r, err := ipc.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Release()

	var records []arrow.RecordBatch
	for r.Next() {
		rec := r.Record()
		rec.Retain()
		records = append(records, rec)
	}
	return records, nil
}
