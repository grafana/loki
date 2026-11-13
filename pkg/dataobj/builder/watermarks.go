package builder

import (
	"context"
	"fmt"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// defaultMaxWatermarks caps the entries of the watermark cache.
const defaultMaxWatermarks = 100_000

// watermarkCache remembers the watermark that the ToC of a tenant and window
// holds for one topic and partition. An entry comes from a read of the ToC or
// from [watermarkCache.set].
//
// The cache evicts the least recently used entry past its cap, and a miss
// reads the ToC again. It is safe for concurrent use.
type watermarkCache struct {
	topic     string
	partition int32
	bucket    objstore.BucketReader
	metrics   *WatermarkMetrics

	entriesMu sync.Mutex
	// entries holds a nil value for a ToC that has no row for the partition.
	entries *lru.Cache[builderScope, *int64]
}

// newWatermarkCache returns an empty cache. It reads the ToCs from bucket, which
// must be the bucket that the ToC writer uses.
func newWatermarkCache(topic string, partition int32, bucket objstore.BucketReader, metrics *WatermarkMetrics) *watermarkCache {
	return &watermarkCache{
		topic:     topic,
		partition: partition,
		bucket:    bucket,
		metrics:   metrics,
		entries:   newWatermarkEntries(defaultMaxWatermarks),
	}
}

func newWatermarkEntries(size int) *lru.Cache[builderScope, *int64] {
	// New fails only for a size below 1.
	entries, err := lru.New[builderScope, *int64](size)
	if err != nil {
		panic(err)
	}
	return entries
}

// get returns the watermark of the scope. A scope that is not in the cache costs
// one read of the ToC. The error of a failed read is never cached.
func (c *watermarkCache) get(ctx context.Context, scope builderScope) (*int64, error) {
	c.entriesMu.Lock()
	value, ok := c.entries.Get(scope)
	c.entriesMu.Unlock()
	if ok {
		c.metrics.cacheLookups.WithLabelValues(lookupHit).Inc()
		return value, nil
	}

	c.metrics.cacheLookups.WithLabelValues(lookupMiss).Inc()
	value, err := c.read(ctx, scope)
	if err != nil {
		return nil, err
	}

	c.entriesMu.Lock()
	defer c.entriesMu.Unlock()
	defer c.updateEntriesLocked()
	// A set can land while the read runs, and the read can be older.
	if existing, found, _ := c.entries.PeekOrAdd(scope, value); found {
		return existing, nil
	}
	return value, nil
}

// set stores that the watermark of the scope is at least offset. It never lowers
// an entry.
func (c *watermarkCache) set(scope builderScope, offset int64) {
	c.entriesMu.Lock()
	defer c.entriesMu.Unlock()
	defer c.updateEntriesLocked()

	if current, ok := c.entries.Peek(scope); ok && current != nil && *current >= offset {
		return
	}
	c.entries.Add(scope, &offset)
}

func (c *watermarkCache) updateEntriesLocked() {
	c.metrics.cacheEntries.Set(float64(c.entries.Len()))
}

func (c *watermarkCache) read(ctx context.Context, scope builderScope) (*int64, error) {
	start := time.Now()
	value, err := metastore.ReadWatermark(ctx, c.bucket, scope.tenant, scope.window, c.topic, c.partition)
	c.metrics.readDuration.WithLabelValues(resultOf(err)).Observe(time.Since(start).Seconds())
	if err != nil {
		return nil, fmt.Errorf("read ToC watermark of %s: %w", scope, err)
	}
	return value, nil
}

const (
	lookupHit  = "hit"
	lookupMiss = "miss"
)

// WatermarkMetrics are the metrics of the ToC watermark cache of a builder.
type WatermarkMetrics struct {
	readDuration *prometheus.HistogramVec
	cacheLookups *prometheus.CounterVec
	cacheEntries prometheus.Gauge
}

// NewWatermarkMetrics registers the watermark metrics with reg.
func NewWatermarkMetrics(reg prometheus.Registerer) *WatermarkMetrics {
	m := &WatermarkMetrics{
		readDuration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_builder_toc_watermark_read_duration_seconds",
			Help:                            "Time taken to read the watermark of a ToC from the bucket.",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"result"}),
		cacheLookups: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_builder_toc_watermark_cache_lookups_total",
			Help: "Total number of lookups of a ToC watermark in the cache of the builder. A miss reads the ToC from the bucket.",
		}, []string{"result"}),
		cacheEntries: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_builder_toc_watermark_cache_entries",
			Help: "Number of ToC watermarks in the cache of the builder. The cache evicts the least recently used entry past a cap.",
		}),
	}

	// Report the results from the start, so that a rate over failures reads
	// as zero rather than going missing until the first one happens.
	m.readDuration.WithLabelValues(resultOK)
	m.readDuration.WithLabelValues(resultError)
	m.readDuration.WithLabelValues(resultCancelled)
	m.cacheLookups.WithLabelValues(lookupHit)
	m.cacheLookups.WithLabelValues(lookupMiss)
	return m
}
