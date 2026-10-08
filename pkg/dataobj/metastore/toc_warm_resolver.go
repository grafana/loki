package metastore

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// tocWarmCacheType groups the warm cache's usage statistics.
const tocWarmCacheType = stats.CacheType("dataobj-toc")

// TableOfContentsWarmResolverConfig configures the background ToC warmer.
type TableOfContentsWarmResolverConfig struct {
	// WarmWindow is how far back from now the warmer keeps ToC windows warm. It bounds memory and refresh cost.
	WarmWindow time.Duration `yaml:"warm_window"`

	// RefreshInterval is how often the snapshot is refreshed (jittered per instance). The warm cache TTL is
	// derived from it (slightly below).
	RefreshInterval time.Duration `yaml:"refresh_interval"`

	// Cache is the memcached dedup cache used only by the warmer's refresh loop. The in-memory snapshot
	// serves queries, so the embedded (in-process) tier is left off by default.
	Cache cache.Config `yaml:"cache"`
}

func (cfg *TableOfContentsWarmResolverConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.WarmWindow, prefix+"warm-window", 3*24*time.Hour,
		"How far back from now to keep ToC windows warm.")
	f.DurationVar(&cfg.RefreshInterval, prefix+"refresh-interval", time.Minute,
		"How often to refresh the warm snapshot (jittered per instance). The warm cache TTL is derived from it.")
	cfg.Cache.RegisterFlagsWithPrefix(prefix+"cache.", "", f)
}

// Validate rejects settings that would break the refresh loop: a non-positive RefreshInterval busy-loops
// it (and yields a non-positive derived cache TTL), and a non-positive WarmWindow warms nothing.
func (cfg *TableOfContentsWarmResolverConfig) Validate() error {
	if cfg.RefreshInterval <= 0 {
		return fmt.Errorf("toc-warmer refresh-interval must be positive, got %s", cfg.RefreshInterval)
	}
	if cfg.WarmWindow <= 0 {
		return fmt.Errorf("toc-warmer warm-window must be positive, got %s", cfg.WarmWindow)
	}
	return nil
}

// tocSnapshot maps a tenant-window ToC path, as TableOfContentsPath builds it, to that path's index
// entries. It is published atomically; readers never mutate it. A path that is present (even an empty
// slice) is warm and served from memory; an absent key is not warmed and falls back to the lazy
// resolver. readPathFromBucket deliberately returns a non-nil empty slice for a missing ToC so it
// counts as warm-and-known-empty rather than a per-query lazy read.
type tocSnapshot map[string][]IndexEntry

// TableOfContentsWarmResolver keeps the last warmWindow of ToCs warm in memory so GetIndexes serves them
// without touching object storage. A background loop refreshes the snapshot every refreshInterval, so a
// warm path reflects the last refresh and may lag by up to that interval; the shared memcached cache
// dedups the object-storage reads across index-gateway instances. Any path not in the snapshot falls
// back to the lazy resolver, so no path is ever silently dropped.
type TableOfContentsWarmResolver struct {
	services.Service

	lazy   *TableOfContentsLazyResolver
	bucket objstore.Bucket
	cache  cacheStore
	logger log.Logger

	warmWindow      time.Duration
	refreshInterval time.Duration

	snapshot atomic.Pointer[tocSnapshot]

	source          *prometheus.CounterVec
	refreshDuration prometheus.Histogram
	refreshErrors   prometheus.Counter
	pathsWarmed     prometheus.Gauge
	cacheHits       prometheus.Counter
	cacheMisses     prometheus.Counter
	cacheErrors     *prometheus.CounterVec
	objectStoreGets prometheus.Counter
}

// cacheStore is the subset of cache.Cache the warmer uses; kept small so tests can fake it.
type cacheStore interface {
	Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error)
	Store(ctx context.Context, keys []string, bufs [][]byte) error
	Stop()
}

// NewTableOfContentsWarmResolver builds a warm resolver from cfg, creating its memcached dedup cache. Like
// NewObjectMetastore, it prefixes the raw bucket with metastoreCfg.IndexStoragePrefix itself, so the
// warmer reads ToCs from the same location the metastore writes them. It does not start refreshing until
// the returned service is started. reg may be nil.
func NewTableOfContentsWarmResolver(cfg TableOfContentsWarmResolverConfig, metastoreCfg Config, bucket objstore.Bucket, reg prometheus.Registerer, logger log.Logger) (*TableOfContentsWarmResolver, error) {
	// Guard the invariants the refresh loop and TTL derivation rely on, even when a caller builds the
	// resolver directly rather than through the validated index-gateway config.
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = log.NewNopLogger()
	}
	if metastoreCfg.IndexStoragePrefix != "" {
		bucket = objstore.NewPrefixedBucket(bucket, metastoreCfg.IndexStoragePrefix)
	}
	// Without a shared cache backend the dedup is a no-op: every instance reads every ToC from object
	// storage on every refresh. The warmer still works; warn so the missing backend is not a silent surprise.
	if !cache.IsCacheConfigured(cfg.Cache) {
		level.Warn(logger).Log("msg", "table-of-contents warmer enabled without a dedup cache backend; each index-gateway will read all ToCs from object storage every refresh with no cross-instance deduplication")
	}
	// Expire a cached path just before the next refresh, so one instance repopulates it from object
	// storage while the rest still hit the cache.
	cfg.Cache.DefaultValidity = cfg.RefreshInterval * 9 / 10
	c, err := cache.New(cfg.Cache, reg, logger, tocWarmCacheType, constants.Loki)
	if err != nil {
		return nil, err
	}
	return newTableOfContentsWarmResolver(c, bucket, cfg.WarmWindow, cfg.RefreshInterval, reg, logger), nil
}

// newTableOfContentsWarmResolver wires a resolver over an already-built cache. Tests use it to inject a
// fake cacheStore; production goes through NewTableOfContentsWarmResolver.
func newTableOfContentsWarmResolver(cache cacheStore, bucket objstore.Bucket, warmWindow, refreshInterval time.Duration, reg prometheus.Registerer, logger log.Logger) *TableOfContentsWarmResolver {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	r := &TableOfContentsWarmResolver{
		lazy:            NewTableOfContentsLazyResolver(bucket, logger),
		bucket:          bucket,
		cache:           cache,
		logger:          logger,
		warmWindow:      warmWindow,
		refreshInterval: refreshInterval,

		source: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_metastore_toc_warmer_get_indexes_source_total",
			Help: "Per-path GetIndexes resolutions by source: cache (served from the warm snapshot) or storage (lazy read from object storage).",
		}, []string{"source"}),
		refreshDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_toc_warmer_refresh_duration_seconds",
			Help:                            "Time to refresh the whole warm snapshot.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		refreshErrors: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_metastore_toc_warmer_refresh_errors_total",
			Help: "Tenant-window ToCs that failed to load during a refresh and are resolved lazily until a later refresh warms them.",
		}),
		pathsWarmed: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_metastore_toc_warmer_paths_warmed",
			Help: "Number of tenant-window ToC paths currently held in the warm snapshot.",
		}),
		cacheHits: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_metastore_toc_warmer_cache_hits_total",
			Help: "Warm-cache (memcached) hits when refreshing a path.",
		}),
		cacheMisses: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_metastore_toc_warmer_cache_misses_total",
			Help: "Warm-cache (memcached) misses when refreshing a path.",
		}),
		cacheErrors: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_metastore_toc_warmer_cache_errors_total",
			Help: "Warm-cache errors by operation (fetch, decode, encode, store). A fetch/store failure means the cross-instance dedup is degraded.",
		}, []string{"op"}),
		objectStoreGets: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_metastore_toc_warmer_object_store_gets_total",
			Help: "Full ToC object reads from object storage while warming.",
		}),
	}
	r.Service = services.NewBasicService(nil, r.running, r.stopping)
	return r
}

// GetIndexes serves warm paths from the snapshot and falls back to the lazy resolver for the rest. Its
// filtering and dedup match the lazy path; warm paths reflect the last refresh, so they may lag it.
func (r *TableOfContentsWarmResolver) GetIndexes(ctx context.Context, tablePaths []string, start, end time.Time) ([]IndexEntry, error) {
	snap := r.snapshot.Load()
	batches := make([][]IndexEntry, 0, len(tablePaths))
	var coldPaths []string
	for _, path := range tablePaths {
		var entries []IndexEntry
		var warm bool
		if snap != nil {
			entries, warm = (*snap)[path]
		}
		if !warm {
			coldPaths = append(coldPaths, path)
			r.source.WithLabelValues("storage").Inc()
			continue
		}
		r.source.WithLabelValues("cache").Inc()
		var overlapping []IndexEntry
		for _, e := range entries {
			// Match the lazy path's inclusive overlap: object [Start,End] overlaps query [start,end].
			if !e.Start.After(end) && !e.End.Before(start) {
				overlapping = append(overlapping, e)
			}
		}
		batches = append(batches, overlapping)
	}

	if len(coldPaths) > 0 {
		cold, err := r.lazy.GetIndexes(ctx, coldPaths, start, end)
		if err != nil {
			return nil, err
		}
		batches = append(batches, cold)
	}

	// A data object straddling a 12h boundary appears in both windows' ToCs; dedupe as the lazy path does.
	return dedupeAndSortEntries(batches), nil
}

func (r *TableOfContentsWarmResolver) running(ctx context.Context) error {
	// Warm on start rather than waiting a full interval. The service reports healthy before this returns,
	// so queries before the first refresh publishes fall back to the lazy resolver.
	r.refresh(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(r.nextRefreshDelay()):
			r.refresh(ctx)
		}
	}
}

// nextRefreshDelay returns the refresh interval plus a positive-only jitter, so instances spread their
// refreshes across the interval. The jitter never subtracts: the delay stays at or above refreshInterval,
// so a cached path (TTL just under the interval) always expires before the same instance refreshes
// again and repopulates it. A shorter period could bring an instance back to its own still-cached path,
// leaving nobody to refresh it from object storage.
func (r *TableOfContentsWarmResolver) nextRefreshDelay() time.Duration {
	jitter := int64(r.refreshInterval) / 10
	if jitter <= 0 {
		return r.refreshInterval
	}
	return r.refreshInterval + time.Duration(rand.Int63n(jitter)) //#nosec G404 -- Jitter does not require CSPRNG -- nosemgrep: math-random-used
}

func (r *TableOfContentsWarmResolver) stopping(_ error) error {
	r.cache.Stop()
	return nil
}

// warmPath is one tenant-window ToC the refresh loop loads: its object-storage path, the window it
// belongs to (its read bounds when warming), and the tenant that owns it.
type warmPath struct {
	tenant string
	window time.Time
	path   string
}

// refresh rebuilds the snapshot for every tenant-window ToC overlapping [now-warmWindow, now] and
// publishes it. A path that fails to load is left out of the snapshot rather than served stale: an
// absent path routes GetIndexes to the lazy resolver, a slower per-query read that is always correct
// and surfaces a persistent load failure to the caller instead of hiding it behind the last good value.
// The refreshErrors counter makes a path that keeps failing visible.
//
// Listing which tenants have a ToC for a window is itself an object-storage list call, one per window,
// done up front and not deduplicated by the memcached cache (ListTableOfContentsTenants has no byte
// payload to cache) — negligible next to the per-tenant ToC reads it feeds.
//
// The paths load concurrently with no bound. There are at most a few tenants times a few windows
// (warmWindow / MetastoreWindowSize), and each is a single cache or object-storage read, so one
// goroutine per path keeps the whole refresh as short as the slowest path rather than the sum. Each
// load is bounded by refreshInterval so a stuck read fails its path (dropping it to lazy) instead of
// blocking the whole refresh loop forever.
func (r *TableOfContentsWarmResolver) refresh(ctx context.Context) {
	start := time.Now()

	now := time.Now()
	minWindow := now.Add(-r.warmWindow).Truncate(MetastoreWindowSize).UTC()
	maxWindow := now.Truncate(MetastoreWindowSize).UTC()

	var windows []time.Time
	for w := minWindow; !w.After(maxWindow); w = w.Add(MetastoreWindowSize) {
		windows = append(windows, w)
	}

	loadCtx, cancel := context.WithTimeout(ctx, r.refreshInterval)
	defer cancel()

	var paths []warmPath
	for _, window := range windows {
		tenants, err := ListTableOfContentsTenants(loadCtx, r.bucket, window)
		if err != nil {
			level.Warn(r.logger).Log("msg", "failed to list table-of-contents tenants for window; will resolve it lazily", "window", window, "err", err)
			r.refreshErrors.Inc()
			continue
		}
		for _, tenant := range tenants {
			paths = append(paths, warmPath{tenant: tenant, window: window, path: TableOfContentsPath(tenant, window)})
		}
	}

	entries := make([][]IndexEntry, len(paths))
	errs := make([]error, len(paths))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entries[i], errs[i] = r.loadPath(loadCtx, p)
		}()
	}
	wg.Wait()

	// The service is stopping: don't publish a degraded snapshot or log the cancellation as a warm failure.
	if ctx.Err() != nil {
		return
	}

	next := make(tocSnapshot, len(paths))
	for i, p := range paths {
		if errs[i] != nil {
			level.Warn(r.logger).Log("msg", "failed to warm table of contents; will resolve it lazily", "path", p.path, "err", errs[i])
			r.refreshErrors.Inc()
			continue
		}
		next[p.path] = entries[i]
	}

	r.snapshot.Store(&next)
	r.pathsWarmed.Set(float64(len(next)))
	r.refreshDuration.Observe(time.Since(start).Seconds())
}

// loadPath returns one tenant-window ToC's entries: from memcached on a hit, else from object storage
// (repopulating memcached). A missing ToC is an empty slice, not an error.
func (r *TableOfContentsWarmResolver) loadPath(ctx context.Context, p warmPath) ([]IndexEntry, error) {
	key := tocWarmCacheKey(p.path)
	if entries, ok := r.loadFromCache(ctx, key, p.path); ok {
		return entries, nil
	}
	entries, err := r.readPathFromBucket(ctx, p)
	if err != nil {
		return nil, err
	}
	r.storeToCache(ctx, key, p.path, entries)
	return entries, nil
}

// loadFromCache returns the cached entries, or false to fall through to a bucket read. It records the
// cache outcome: a hit, a genuine miss, or an error. A fetch error or an undecodable entry is logged and
// counted under cacheErrors (not as a miss), so a cache outage is not misread as a cold cache on the
// hit-ratio panel while every instance re-reads object storage.
func (r *TableOfContentsWarmResolver) loadFromCache(ctx context.Context, key, path string) ([]IndexEntry, bool) {
	_, bufs, _, err := r.cache.Fetch(ctx, []string{key})
	if err != nil {
		level.Warn(r.logger).Log("msg", "warm cache fetch failed", "path", path, "err", err)
		r.cacheErrors.WithLabelValues("fetch").Inc()
		return nil, false
	}
	// Nothing cached: a genuine miss, not an error. (A zero-length buffer is the valid encoding of an
	// empty path and decodes to a known-empty hit below.)
	if len(bufs) != 1 {
		r.cacheMisses.Inc()
		return nil, false
	}
	entries, err := decodeCachedToC(bufs[0])
	if err != nil {
		level.Warn(r.logger).Log("msg", "discarding undecodable warmed table of contents", "path", path, "err", err)
		r.cacheErrors.WithLabelValues("decode").Inc()
		return nil, false
	}
	r.cacheHits.Inc()
	return entries, true
}

func (r *TableOfContentsWarmResolver) storeToCache(ctx context.Context, key, path string, entries []IndexEntry) {
	b, err := encodeCachedToC(entries)
	if err != nil {
		level.Warn(r.logger).Log("msg", "encoding warmed table of contents failed", "path", path, "err", err)
		r.cacheErrors.WithLabelValues("encode").Inc()
		return
	}
	if err := r.cache.Store(ctx, []string{key}, [][]byte{b}); err != nil {
		level.Warn(r.logger).Log("msg", "warm cache store failed", "path", path, "err", err)
		r.cacheErrors.WithLabelValues("store").Inc()
	}
}

// readPathFromBucket reads one tenant-window ToC in full. It reuses the lazy resolver's reader, scoped
// to p's tenant and to p's whole window, so a warmed entry is byte-for-byte what the lazy path would
// have produced for the same inputs.
func (r *TableOfContentsWarmResolver) readPathFromBucket(ctx context.Context, p warmPath) ([]IndexEntry, error) {
	r.objectStoreGets.Inc()

	ctx = user.InjectOrgID(ctx, p.tenant)
	windowStart := p.window
	windowEnd := p.window.Add(MetastoreWindowSize)
	sStart := scalar.NewTimestampScalar(arrow.Timestamp(windowStart.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(windowEnd.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)

	entries, err := r.lazy.listObjects(ctx, p.path, sStart, sEnd)
	if err != nil {
		if r.bucket.IsObjNotFoundErr(err) {
			return []IndexEntry{}, nil
		}
		return nil, err
	}
	if entries == nil {
		entries = []IndexEntry{}
	}
	return entries, nil
}

const tocWarmCacheKeyPrefix = "metastore-toc/v1:"

func tocWarmCacheKey(path string) string { return tocWarmCacheKeyPrefix + path }

func encodeCachedToC(entries []IndexEntry) ([]byte, error) {
	msg := CachedToC{Entries: make([]*CachedIndexEntry, len(entries))}
	for i, e := range entries {
		msg.Entries[i] = &CachedIndexEntry{Path: e.Path, StartNanos: e.Start.UnixNano(), EndNanos: e.End.UnixNano()}
	}
	return msg.Marshal()
}

func decodeCachedToC(b []byte) ([]IndexEntry, error) {
	var msg CachedToC
	if err := msg.Unmarshal(b); err != nil {
		return nil, err
	}
	entries := make([]IndexEntry, len(msg.Entries))
	for i, e := range msg.Entries {
		// time.Unix without .UTC(), matching the lazy/bucket read path, so a path decoded from the cache
		// is byte-for-byte equal to one read from the bucket, not just instant-equal.
		entries[i] = IndexEntry{Path: e.Path, Start: time.Unix(0, e.StartNanos), End: time.Unix(0, e.EndNanos)}
	}
	return entries, nil
}
