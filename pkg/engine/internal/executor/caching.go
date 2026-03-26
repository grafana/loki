package executor

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
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

// newCachingPipeline wraps inner in a cachingPipeline backed by cache.
// If cache is nil, inner is returned unchanged (no caching).
func newCachingPipeline(
	c cache.Cache,
	inner Pipeline,
	key string,
	maxSizeBytes uint64,
	compression string,
	logger log.Logger,
) Pipeline {
	if c == nil {
		return inner
	}

	return &cachingPipeline{
		inner:        inner,
		cache:        c,
		key:          cache.HashKey(key),
		logger:       logger,
		maxSizeBytes: maxSizeBytes,
		compression:  compression,
	}
}

// cachingPipeline wraps a Pipeline and transparently stores and retrieves Arrow
// record batch results via a raw cache.Cache.
//
// On a cache hit: Open decodes the cached payload into a recordDecoder; Read
// iterates records through it without touching the inner pipeline.
// On a cache miss: Read add records through a recordEncoder; on EOF the
// encoded payload is committed to the cache.
type cachingPipeline struct {
	inner        Pipeline
	cache        cache.Cache
	key          string
	logger       log.Logger
	maxSizeBytes uint64
	compression  string

	hit bool

	// For hit path
	decoder *recordDecoder // non-nil on hit path

	// For miss path
	passthrough bool // true once we know we won't cache (size overflow)
	encoder     *recordEncoder

	// Accumulated across whichever path is active
	cachedRows    int64
	cachedRecords int64
}

// Open implements Pipeline.
func (p *cachingPipeline) Open(ctx context.Context) error {
	region := xcap.RegionFromContext(ctx)

	found, buffs, missing, err := p.cache.Fetch(ctx, []string{p.key})
	if err == nil && len(missing) == 0 && len(found) > 0 {
		dec, decErr := newRecordDecoder(buffs[0])
		if decErr == nil {
			p.decoder = dec
			p.hit = true
			region.Record(xcap.TaskCacheHits.Observe(1))
			region.Record(xcap.TaskCacheBytes.Observe(int64(len(buffs[0]))))
			return nil
		}

		// Corrupted or old-format entry — fall back to inner pipeline.
		level.Error(p.logger).Log("msg", "cache decode failed, falling back to inner pipeline", "err", decErr)
	}
	if err != nil {
		level.Error(p.logger).Log("msg", "cache fetch failed, falling back to inner pipeline", "err", err)
	}

	region.Record(xcap.TaskCacheMisses.Observe(1))
	p.encoder = newRecordEncoder(p.compression)
	return p.inner.Open(ctx)
}

// Read implements Pipeline.
func (p *cachingPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if p.hit {
		rec, err := p.decoder.Next()
		if rec != nil {
			p.cachedRows += rec.NumRows()
			p.cachedRecords++
		}
		if errors.Is(err, EOF) {
			region := xcap.RegionFromContext(ctx)
			region.Record(xcap.TaskCacheBatches.Observe(p.cachedRecords))
			region.Record(xcap.TaskCacheRows.Observe(p.cachedRows))
		}
		return rec, err
	}

	rec, err := p.inner.Read(ctx)
	if err != nil {
		// Won't cache or non-EOF error
		if !errors.Is(err, EOF) || p.passthrough {
			return nil, err
		}

		payload, commitErr := p.encoder.Commit()
		if commitErr != nil {
			level.Error(p.logger).Log("msg", "failed to encode records for cache", "err", commitErr)
			return nil, err
		}

		if storeErr := p.cache.Store(ctx, []string{p.key}, [][]byte{payload}); storeErr != nil {
			level.Error(p.logger).Log("msg", "failed to store results in cache", "err", storeErr)
			return nil, err
		}

		region := xcap.RegionFromContext(ctx)
		region.Record(xcap.TaskCacheBatches.Observe(p.cachedRecords))
		region.Record(xcap.TaskCacheRows.Observe(p.cachedRows))
		region.Record(xcap.TaskCacheBytes.Observe(int64(len(payload))))
		return nil, err
	}

	// When passthrough is enabled, we won't cache this response
	if p.passthrough {
		return rec, nil
	}

	// maxSizeBytes==0 means only cache empty responses; bail on the first record with rows.
	if p.maxSizeBytes == 0 && rec.NumRows() > 0 {
		p.disableCache()
		return rec, nil
	}

	if appendErr := p.encoder.Append(rec); appendErr != nil {
		level.Error(p.logger).Log("msg", "failed to encode record for cache, skipping cache", "err", appendErr)
		p.disableCache()
		return rec, nil
	}

	p.cachedRows += rec.NumRows()
	p.cachedRecords++

	// Adding this last record made us go over the max cacheable size, so disable caching for this task result
	if p.encoder.Size() > p.maxSizeBytes {
		level.Debug(p.logger).Log("msg", "cache entry too large, skipping cache", "size", p.encoder.Size(), "max_size", p.maxSizeBytes)
		p.disableCache()
	}

	return rec, nil
}

// disableCache switches the pipeline to passthrough mode, discarding any
// accumulated encoder state and resetting cached stats counters.
func (p *cachingPipeline) disableCache() {
	p.encoder.Reset()
	p.cachedRows, p.cachedRecords = 0, 0
	p.passthrough = true
}

// Close implements Pipeline.
func (p *cachingPipeline) Close() {
	if !p.hit {
		p.inner.Close()
	}
}

// TaskCacheRegistry maps TaskCacheName identifiers to backing cache stores.
type TaskCacheRegistry map[physical.TaskCacheName]cache.Cache

// NewTaskCacheRegistry builds a registry with one independent cache per task type,
// all backed by the same resultscache.Config. Returns a zero-value (no-op) registry
// when caching is not configured.
func NewTaskCacheRegistry(cfg resultscache.Config, reg prometheus.Registerer, logger log.Logger) (TaskCacheRegistry, error) {
	if !cache.IsCacheConfigured(cfg.CacheConfig) {
		return nil, nil
	}

	newCache := func(suffix string) (cache.Cache, error) {
		cfgCopy := cfg.CacheConfig
		cfgCopy.Prefix += suffix
		c, err := cache.New(cfgCopy, reg, logger, stats.TaskResultCache, constants.Loki)
		if err != nil {
			return nil, fmt.Errorf("failed to create cache: %w", err)
		}
		return c, nil
	}

	logscan, err := newCache("logscan.")
	if err != nil {
		return nil, fmt.Errorf("creating logscan task cache: %w", err)
	}
	metastore, err := newCache("metastore.")
	if err != nil {
		return nil, fmt.Errorf("creating metastore task cache: %w", err)
	}
	logscanRangeAggr, err := newCache("logscan-rangeaggr.")
	if err != nil {
		return nil, fmt.Errorf("creating logscan-rangeaggr task cache: %w", err)
	}

	return TaskCacheRegistry{
		physical.TaskCacheLogsScan:          logscan,
		physical.TaskCacheLogsScanRangeAggr: logscanRangeAggr,
		physical.TaskCacheMetastore:         metastore,
	}, nil
}

// GetForType returns the raw cache backend for the given cache type.
func (r TaskCacheRegistry) GetForType(cacheType physical.TaskCacheName) (cache.Cache, error) {
	if c, ok := r[cacheType]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("no cache registered for type %q", cacheType)
}

// Compression codec identifiers stored in the per-record wire format.
const (
	compressionNone   byte = 0
	compressionSnappy byte = 1
)

// recordEncoder accumulates Arrow record batches and encodes them incrementally.
// Each record is compressed independently, allowing callers to check Size() after
// every Append and short-circuit before the full payload is committed.
type recordEncoder struct {
	comp   byte
	frames [][]byte // per-record compressed IPC bytes
	size   uint64   // sum of len(frame) across all frames
}

func newRecordEncoder(compression string) *recordEncoder {
	comp := compressionNone
	if strings.EqualFold(compression, "snappy") {
		comp = compressionSnappy
	}
	return &recordEncoder{comp: comp}
}

// Append serializes rec, optionally compresses it, and stores the resulting frame.
func (e *recordEncoder) Append(rec arrow.RecordBatch) error {
	data, err := arrowcodec.DefaultArrowCodec.SerializeArrowRecord(rec)
	if err != nil {
		return fmt.Errorf("serializing record: %w", err)
	}
	if e.comp == compressionSnappy {
		data = snappy.Encode(nil, data)
	}
	e.frames = append(e.frames, data)
	e.size += uint64(len(data))
	return nil
}

// Size returns the total byte size of all encoded frames accumulated so far.
// This does not include the fixed-size buffer header.
func (e *recordEncoder) Size() uint64 { return e.size }

// Commit serializes all accumulated frames into a single framed buffer.
//
// Wire format:
//
//	[8 bytes: record count (big-endian uint64)]
//	[1 byte:  compression codec (0=none, 1=snappy)]
//	for each record:
//	  [8 bytes: compressed frame length (big-endian uint64)]
//	  [N bytes: compressed Arrow IPC stream]
func (e *recordEncoder) Commit() ([]byte, error) {
	var buf bytes.Buffer
	var hdr [8]byte

	binary.BigEndian.PutUint64(hdr[:], uint64(len(e.frames)))
	buf.Write(hdr[:])
	buf.WriteByte(e.comp)

	for _, frame := range e.frames {
		binary.BigEndian.PutUint64(hdr[:], uint64(len(frame)))
		buf.Write(hdr[:])
		buf.Write(frame)
	}
	return buf.Bytes(), nil
}

// Reset discards all accumulated frames and resets counters, freeing their memory.
func (e *recordEncoder) Reset() {
	e.frames = nil
	e.size = 0
}

// recordDecoder iterates over a framed buffer produced by [recordEncoder.Commit].
type recordDecoder struct {
	data []byte
	pos  int
	n    int  // total record count from header
	comp byte // compression codec from header
	read int  // records consumed so far
}

// newRecordDecoder parses the buffer header and returns a ready decoder.
// A zero-length buffer represents an empty cached result (n=0).
func newRecordDecoder(data []byte) (*recordDecoder, error) {
	if len(data) == 0 {
		return &recordDecoder{}, nil
	}
	if len(data) < 9 {
		return nil, fmt.Errorf("cache buffer too short (%d bytes)", len(data))
	}
	n := int(binary.BigEndian.Uint64(data[:8]))
	comp := data[8]
	return &recordDecoder{data: data, pos: 9, n: n, comp: comp}, nil
}

// Len returns the total number of records declared in the buffer header.
func (d *recordDecoder) Len() int { return d.n }

// Next returns the next Arrow record batch from the buffer.
// Returns (nil, [EOF]) when all records have been consumed.
func (d *recordDecoder) Next() (arrow.RecordBatch, error) {
	if d.read >= d.n {
		return nil, EOF
	}
	if d.pos >= len(d.data) {
		return nil, fmt.Errorf("unexpected end of cache buffer at record %d", d.read)
	}

	if d.pos+8 > len(d.data) {
		return nil, fmt.Errorf("truncated length header for record %d", d.read)
	}
	recordSize := int(binary.BigEndian.Uint64(d.data[d.pos : d.pos+8]))
	d.pos += 8

	if d.pos+recordSize > len(d.data) {
		return nil, fmt.Errorf("truncated data for record %d (want %d bytes, have %d)", d.read, recordSize, len(d.data)-d.pos)
	}
	frame := d.data[d.pos : d.pos+recordSize]
	d.pos += recordSize
	d.read++

	var ipcBytes []byte
	switch d.comp {
	case compressionNone:
		ipcBytes = frame
	case compressionSnappy:
		var err error
		ipcBytes, err = snappy.Decode(nil, frame)
		if err != nil {
			return nil, fmt.Errorf("snappy-decode record %d: %w", d.read-1, err)
		}
	default:
		return nil, fmt.Errorf("unknown compression codec %d for record %d", d.comp, d.read-1)
	}

	rec, err := arrowcodec.DefaultArrowCodec.DeserializeArrowRecord(ipcBytes)
	if err != nil {
		return nil, fmt.Errorf("deserializing record %d: %w", d.read-1, err)
	}

	return rec, nil
}
