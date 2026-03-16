package executor

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	arrowcodec "github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire/arrow"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// newCachingPipeline wraps inner in a [cachingPipeline] backed by c.
// If c is nil, inner is returned unchanged (no caching).
func newCachingPipeline(c cache.Cache, inner Pipeline, key string, logger log.Logger) Pipeline {
	if c == nil {
		return inner
	}
	return &cachingPipeline{
		inner:  inner,
		store:  &arrowCacheAdapter{cache: c},
		key:    key,
		logger: logger,
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
	logger log.Logger
	cached []arrow.RecordBatch // records served from cache (hit path) or collected to put them on the cache (miss path)
	pos    int                 // current position in cached slice (hit path)
	hit    bool                // true when Open found a cache hit
}

// Open implements Pipeline.
func (p *cachingPipeline) Open(ctx context.Context) error {
	records, hit, err := p.store.get(ctx, p.key)
	if err == nil && hit {
		p.cached = records
		p.hit = true
		return nil
	}

	// On error, log it and fall through to the inner pipeline
	if err != nil {
		level.Error(p.logger).Log("msg", "cache fetch failed, falling back to inner pipeline", "err", err)
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
			if setErr := p.store.set(ctx, p.key, p.cached); setErr != nil {
				level.Error(p.logger).Log("msg", "failed to store results in cache", "err", setErr)
			}
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
	if len(bufs[0]) == 0 {
		return nil, true, nil // cached empty result
	}
	records, err := decodeRecords(bufs[0])
	if err != nil {
		return nil, false, fmt.Errorf("decoding cached records: %w", err)
	}
	return records, true, nil
}

func (s *arrowCacheAdapter) set(ctx context.Context, key string, records []arrow.RecordBatch) error {
	if len(records) == 0 {
		return s.cache.Store(ctx, []string{key}, [][]byte{{}})
	}
	buf, err := encodeRecords(records)
	if err != nil {
		return fmt.Errorf("encoding records for cache: %w", err)
	}
	return s.cache.Store(ctx, []string{key}, [][]byte{buf})
}

// encodeRecords serializes a slice of Arrow record batches into a single byte
// buffer using the following framed format:
//
//	[8 bytes: record count (big-endian uint64)]
//	for each record:
//	  [8 bytes: record length (big-endian uint64)]
//	  [N bytes: Arrow IPC stream encoding of the record]
func encodeRecords(records []arrow.RecordBatch) ([]byte, error) {
	var buf bytes.Buffer
	var hdr [8]byte

	binary.BigEndian.PutUint64(hdr[:], uint64(len(records)))
	buf.Write(hdr[:])

	for i, rec := range records {
		data, err := arrowcodec.DefaultArrowCodec.SerializeArrowRecord(rec)
		if err != nil {
			return nil, fmt.Errorf("encoding record %d: %w", i, err)
		}
		binary.BigEndian.PutUint64(hdr[:], uint64(len(data)))
		buf.Write(hdr[:])
		buf.Write(data)
	}
	return buf.Bytes(), nil
}

// decodeRecords is the inverse of encodeRecords; see encodeRecords for the
// wire format.
func decodeRecords(data []byte) ([]arrow.RecordBatch, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("cache buffer too short")
	}
	r := bytes.NewReader(data)
	var hdr [8]byte

	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint64(hdr[:]))

	records := make([]arrow.RecordBatch, n)
	for i := range records {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return nil, fmt.Errorf("reading length of record %d: %w", i, err)
		}
		sz := int(binary.BigEndian.Uint64(hdr[:]))
		buf := make([]byte, sz)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("reading record %d: %w", i, err)
		}
		rec, err := arrowcodec.DefaultArrowCodec.DeserializeArrowRecord(buf)
		if err != nil {
			return nil, fmt.Errorf("decoding record %d: %w", i, err)
		}
		records[i] = rec
	}
	return records, nil
}

// TaskCacheRegistry maps TaskCacheType identifiers to backing cache stores.
type TaskCacheRegistry map[physical.TaskCacheName]cache.Cache

// NewTaskCacheRegistry builds a registry that routes each TaskCacheType
// to an instrumented view of underlying.
func NewTaskCacheRegistry(underlying cache.Cache, reg prometheus.Registerer) TaskCacheRegistry {
	return TaskCacheRegistry{
		physical.TaskCacheDataObjScan:  cache.Instrument("task-cache-dataobj", underlying, reg),
		physical.TaskCachePointersScan: cache.Instrument("task-cache-metastore", underlying, reg),
	}
}

// GetForType returns the cache for cacheType, or an error if none is registered.
func (r TaskCacheRegistry) GetForType(cacheType physical.TaskCacheName) (cache.Cache, error) {
	if r != nil {
		if c, ok := r[cacheType]; ok {
			return c, nil
		}
	}
	return nil, fmt.Errorf("no cache registered for type %q", cacheType)
}
