package executor

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	arrowcodec "github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire/arrow"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// CacheStats bundles the xcap statistics recorded by a [cachingPipeline].
// Using a struct lets different cache types record to separate stat variables.
type CacheStats struct {
	Hits    *xcap.StatisticInt64
	Misses  *xcap.StatisticInt64
	Batches *xcap.StatisticInt64
	Rows    *xcap.StatisticInt64
	Bytes   *xcap.StatisticInt64
}

// newCachingPipeline wraps inner in a [cachingPipeline] backed by store.
// If store is nil, inner is returned unchanged (no caching).
func newCachingPipeline(store ArrowCache, inner Pipeline, key string, logger log.Logger, stats CacheStats) Pipeline {
	if store == nil {
		return inner
	}

	hashedKey := cache.HashKey(key)
	return &cachingPipeline{
		inner:  inner,
		store:  store,
		key:    hashedKey,
		logger: log.With(logger, "pipeline", "caching", "cache", store.Name(), "key", hashedKey),
		stats:  stats,
	}
}

// cachingPipeline wraps a [Pipeline] and transparently stores and retrieves
// [arrow.RecordBatch] results from an [arrowCacheAdapter].
//
// On a cache hit: Open reads from the store; Read serves stored records without calling Read on the inner pipeline.
// On a cache miss: Read streams through the inner pipeline and the store is populated on EOF.
type cachingPipeline struct {
	inner       Pipeline
	store       ArrowCache
	key         string
	logger      log.Logger
	stats       CacheStats
	cached      []arrow.RecordBatch // records served from cache (hit path) or collected to put them on the cache (miss path)
	pos         int                 // current position in cached slice (hit path)
	hit         bool                // true when Open found a cache hit
	passthrough bool                // true once we know we won't cache (store.MaxSizeBytes()==0 + non-empty)
}

// Open implements Pipeline.
func (p *cachingPipeline) Open(ctx context.Context) error {
	region := xcap.RegionFromContext(ctx)

	records, bufSize, hit, err := p.store.Get(ctx, p.key)
	if err == nil && hit {
		p.cached = records
		p.hit = true
		region.Record(p.stats.Hits.Observe(1))
		region.Record(p.stats.Batches.Observe(int64(len(records))))
		region.Record(p.stats.Bytes.Observe(int64(bufSize)))
		var rows int64
		for _, rec := range records {
			rows += rec.NumRows()
		}
		region.Record(p.stats.Rows.Observe(rows))
		return nil
	}

	// On error, log it and fall through to the inner pipeline
	if err != nil {
		level.Error(p.logger).Log("msg", "cache fetch failed, falling back to inner pipeline", "err", err)
	}

	region.Record(p.stats.Misses.Observe(1))
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
		// Any non-EOF error, return it to the caller right away
		if !errors.Is(err, EOF) {
			return nil, err
		}

		region := xcap.RegionFromContext(ctx)
		// Caching disabled, we just set the stats and move on
		if p.passthrough {
			region.Record(p.stats.Batches.Observe(0))
			region.Record(p.stats.Bytes.Observe(0))
			region.Record(p.stats.Rows.Observe(0))
			level.Debug(p.logger).Log("msg", "caching pass-though, won't cache result")
			return nil, err
		}

		bufSize, setErr := p.store.Set(ctx, p.key, p.cached)
		if setErr != nil {
			level.Error(p.logger).Log("msg", "failed to store results in cache", "err", setErr)
			// We return the EOF err so we don't fail the task just because of caching
			return nil, err
		}

		region.Record(p.stats.Batches.Observe(int64(len(p.cached))))
		region.Record(p.stats.Bytes.Observe(int64(bufSize)))
		var rows int64
		for _, r := range p.cached {
			rows += r.NumRows()
		}
		region.Record(p.stats.Rows.Observe(rows))
		level.Debug(p.logger).Log("msg", "caching result", "size", humanize.Bytes(uint64(bufSize)), "rows", rows, "batches", len(p.cached))
		return nil, err
	}

	// short-circuit: maxSizeBytes==0 means only cache empty responses.
	// Once we see a non-nil batch the response is non-empty; stop buffering
	// and pass subsequent reads straight through.
	if !p.passthrough && p.store.MaxSizeBytes() == 0 && rec.NumRows() > 0 {
		p.passthrough = true
		p.cached = nil // release any accumulated memory
	}

	if p.passthrough {
		return rec, nil // don't buffer; skip store.Set on EOF path
	}

	// Only cache non-empty records
	if rec.NumRows() > 0 {
		p.cached = append(p.cached, rec)
	}

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
	logger         log.Logger
	cache          cache.Cache
	name           string
	snappyCompress bool

	// maxSizeBytes is the maximum size of the cache entry.
	// A value of 0 means only empty responses are cached.
	maxSizeBytes uint64
}

// Get fetches cached records for key. Returns the decoded records, the raw
// buffer size in bytes, whether a cache entry was found, and any error.
func (s *arrowCacheAdapter) Get(ctx context.Context, key string) ([]arrow.RecordBatch, int, bool, error) {
	found, buffs, missing, err := s.cache.Fetch(ctx, []string{key})
	if err != nil {
		return nil, 0, false, err
	}
	if len(missing) > 0 || len(found) == 0 {
		return nil, 0, false, nil
	}
	if len(buffs[0]) == 0 {
		return nil, 0, true, nil // cached empty result
	}

	payload := buffs[0]
	if s.snappyCompress {
		var decErr error
		payload, decErr = snappy.Decode(nil, buffs[0])
		if decErr != nil {
			return nil, 0, false, fmt.Errorf("snappy-decode cache entry: %w", decErr)
		}
	}

	records, err := s.decodeRecords(payload)
	if err != nil {
		return nil, 0, false, fmt.Errorf("decoding cached records: %w", err)
	}
	return records, len(buffs[0]), true, nil
}

// Set encodes and stores records under key. Returns the encoded buffer size in
// bytes and any error.
func (s *arrowCacheAdapter) Set(ctx context.Context, key string, records []arrow.RecordBatch) (int, error) {
	if len(records) == 0 {
		return 0, s.cache.Store(ctx, []string{key}, [][]byte{{}})
	}

	// maxSizeBytes == 0 means only empty responses may be cached.
	if s.maxSizeBytes == 0 {
		if s.logger != nil {
			level.Debug(s.logger).Log("msg", "non-empty result won't be cached (max_cacheable_size=0)", "key", key)
		}
		return 0, nil
	}

	buf, err := s.encodeRecords(records)
	if err != nil {
		return 0, fmt.Errorf("encoding records for cache: %w", err)
	}

	if s.snappyCompress {
		buf = snappy.Encode(nil, buf)
	}

	if s.maxSizeBytes > 0 && uint64(len(buf)) > s.maxSizeBytes {
		level.Debug(s.logger).Log("msg", "cache entry too large. won't cache", "key", key, "size", len(buf), "max_size", s.maxSizeBytes)
		return 0, nil
	}

	if err := s.cache.Store(ctx, []string{key}, [][]byte{buf}); err != nil {
		return 0, err
	}

	return len(buf), nil
}

// MaxSizeBytes implements ArrowCache.
func (s *arrowCacheAdapter) MaxSizeBytes() uint64 {
	return s.maxSizeBytes
}

func (s *arrowCacheAdapter) Name() string {
	return s.name
}

// encodeRecords serializes a slice of Arrow record batches into a single byte
// buffer using the following framed format:
//
//	[8 bytes: record count (big-endian uint64)]
//	for each record:
//	  [8 bytes: record length (big-endian uint64)]
//	  [N bytes: Arrow IPC stream encoding of the record]
func (s *arrowCacheAdapter) encodeRecords(records []arrow.RecordBatch) ([]byte, error) {
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
func (s *arrowCacheAdapter) decodeRecords(data []byte) ([]arrow.RecordBatch, error) {
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

type ArrowCache interface {
	// Get returns the cached records for key, if any.
	// It also returns the cached buffer size in bytes, and a boolean indicating whether a cache entry was found.
	Get(ctx context.Context, key string) ([]arrow.RecordBatch, int, bool, error)
	// Set stores records under key.
	// It returns the encoded buffer size in bytes.
	Set(ctx context.Context, key string, records []arrow.RecordBatch) (int, error)
	// MaxSizeBytes returns the maximum encoded size of a cache entry.
	// A value of 0 means only empty responses are cached.
	// Useful for callers to short-circuit caching when the response is known to exceed the max cache size.
	MaxSizeBytes() uint64
	Name() string
}

// TaskCacheRegistry maps TaskCacheType identifiers to backing cache stores and their stats.
type TaskCacheRegistry struct {
	adapters map[physical.TaskCacheName]*arrowCacheAdapter
	stats    map[physical.TaskCacheName]CacheStats
}

// NewTaskCacheRegistry builds a registry that creates one independent cache per
// task type, using type-specific prefixes derived from cfg.CacheConfig.Prefix.
func NewTaskCacheRegistry(cfg resultscache.Config, reg prometheus.Registerer, logger log.Logger) (TaskCacheRegistry, error) {
	if !cache.IsCacheConfigured(cfg.CacheConfig) {
		return TaskCacheRegistry{}, nil
	}

	newCache := func(name string) (*arrowCacheAdapter, error) {
		cfgCopy := cfg.CacheConfig
		cfgCopy.Prefix += name + "."
		c, err := cache.New(cfgCopy, reg, logger, stats.TaskResultCache, constants.Loki)
		if err != nil {
			return nil, fmt.Errorf("failed to create cache: %w", err)
		}

		// We don't wrap the cache with cache.NewSnappy, but defer the snappy encoding to arrowCacheAdapter,
		// So we can keep track of the size of the entry we put in the cache.
		// This is so we can support configuring the maximum size of the cache entry.
		return &arrowCacheAdapter{
			cache:          c,
			name:           name,
			snappyCompress: strings.EqualFold(cfg.Compression, "snappy"),
			logger:         logger,
		}, nil
	}

	logscan, err := newCache("logscan")
	if err != nil {
		return TaskCacheRegistry{}, fmt.Errorf("creating logscan task cache: %w", err)
	}
	metastore, err := newCache("metastore")
	if err != nil {
		return TaskCacheRegistry{}, fmt.Errorf("creating metastore task cache: %w", err)
	}
	logscanRangeAggr, err := newCache("logscan-rangeaggr")
	if err != nil {
		return TaskCacheRegistry{}, fmt.Errorf("creating logscan-rangeaggr task cache: %w", err)
	}
	dataObjScanResult, err := newCache("dataobjscan-result")
	if err != nil {
		return TaskCacheRegistry{}, fmt.Errorf("creating dataobjscan-result task cache: %w", err)
	}

	taskCacheStats := CacheStats{
		Hits:    xcap.TaskCacheHits,
		Misses:  xcap.TaskCacheMisses,
		Batches: xcap.TaskCacheBatches,
		Rows:    xcap.TaskCacheRows,
		Bytes:   xcap.TaskCacheBytes,
	}
	dataObjScanCacheStats := CacheStats{
		Hits:    xcap.DataObjScanCacheHits,
		Misses:  xcap.DataObjScanCacheMisses,
		Batches: xcap.DataObjScanCacheBatches,
		Rows:    xcap.DataObjScanCacheRows,
		Bytes:   xcap.DataObjScanCacheBytes,
	}

	return TaskCacheRegistry{
		adapters: map[physical.TaskCacheName]*arrowCacheAdapter{
			physical.TaskCacheLogsScan:          logscan,
			physical.TaskCacheLogsScanRangeAggr: logscanRangeAggr,
			physical.TaskCacheMetastore:         metastore,
			physical.TaskCacheDataObjScanResult: dataObjScanResult,
		},
		stats: map[physical.TaskCacheName]CacheStats{
			physical.TaskCacheLogsScan:          taskCacheStats,
			physical.TaskCacheLogsScanRangeAggr: taskCacheStats,
			physical.TaskCacheMetastore:         taskCacheStats,
			physical.TaskCacheDataObjScanResult: dataObjScanCacheStats,
		},
	}, nil
}

func (r TaskCacheRegistry) GetForTypeWithMaxSize(cacheType physical.TaskCacheName, maxSizeBytes uint64) (ArrowCache, CacheStats, error) {
	if a, ok := r.adapters[cacheType]; ok {
		a.maxSizeBytes = maxSizeBytes
		return a, r.stats[cacheType], nil
	}
	return nil, CacheStats{}, fmt.Errorf("no cache registered for type %q", cacheType)
}
