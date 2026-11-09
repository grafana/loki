package metastore

import (
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	dslog "github.com/grafana/dskit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/grafana/loki/v3/pkg/dataobj/metadatacache"
	"github.com/grafana/loki/v3/pkg/util/flagext"
)

const (
	// tocReadTimeout bounds one read of a ToC from object storage, for a query or for a refresh. A disabled resolver
	// does not apply it.
	tocReadTimeout = 10 * time.Second

	// tocMaxConcurrentReads is the largest number of ToCs that a refresh pass reads at the same time.
	tocMaxConcurrentReads = 8

	// tocMaxClockSkew is the largest lead of a read time over the local clock that still counts as fresh. The read
	// time of a value in the shared cache comes from the clock of another querier.
	tocMaxClockSkew = 5 * time.Second

	// tocValueVersion is the first byte of a value in the shared cache. A value with another version counts as stale.
	tocValueVersion byte = 1

	// tocValueHeaderSize is the size of the version byte and the read time.
	tocValueHeaderSize = 9

	tocFailureLogsPerSecond = 1
	tocFailureLogsBurst     = 5

	// minTOCRefreshInterval is the shortest refresh interval that Validate accepts.
	minTOCRefreshInterval = time.Second
)

// TOCResolverConfig configures the TOCResolver.
type TOCResolverConfig struct {
	Enabled             bool             `yaml:"enabled"`
	RefreshInterval     time.Duration    `yaml:"refresh_interval"`
	ActiveWindow        time.Duration    `yaml:"active_window"`
	MaxMemoryCacheBytes flagext.ByteSize `yaml:"max_memory_cache_bytes"`
}

// RegisterFlagsWithPrefix registers the flags of the resolver with the given prefix.
func (c *TOCResolverConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, prefix+"enabled", true, "Experimental: Keep the table of contents (ToC) that queries requested recently in memory. Refresh them in the background, so a query rarely reads them from object storage. It has no effect unless -dataobj.enabled is set. Only the querier module uses it.")
	f.DurationVar(&c.RefreshInterval, prefix+"refresh-interval", time.Minute, "How often the resolver refreshes the ToCs in memory. A query reads a ToC that is older than twice this interval from object storage. A refresh reads through the data object metadata cache when one is configured (-dataobj.metadata-cache.*). The refresh accepts a cached ToC that is at most half this interval old. The interval must be at least one second, and twice the interval must not exceed a tenth of -dataobj.storage-lag.")
	f.DurationVar(&c.ActiveWindow, prefix+"active-window", 5*time.Minute, "A ToC is refreshed while a query requested it within this time. The resolver drops a ToC that no query requested for longer. It must not be shorter than the refresh interval.")
	_ = c.MaxMemoryCacheBytes.Set("64MB")
	f.Var(&c.MaxMemoryCacheBytes, prefix+"max-memory-cache-bytes", "Size limit of the ToCs kept in memory. It must be positive. The resolver does not store a ToC that would take the size over the limit. When the size is over nine tenths of the limit, a refresh drops the least recently requested ToCs. It stops once the size is at most nine tenths of the limit.")
}

// maxStaleness is the age above which the resolver stops serving a ToC from memory.
func (c TOCResolverConfig) maxStaleness() time.Duration {
	return 2 * c.RefreshInterval
}

// evictionTarget is the size that a refresh leaves in memory: the limit minus a tenth of it. The free tenth lets the ToCs
// that queries request before the next refresh fit.
func (c TOCResolverConfig) evictionTarget() int {
	limit := c.MaxMemoryCacheBytes.Val()
	return limit - limit/10
}

// maxCachedAge is the age above which a refresh rejects a ToC from the shared cache.
func (c TOCResolverConfig) maxCachedAge() time.Duration {
	// Refreshes start at most 1.1 intervals apart (10% jitter). A ToC accepted at this age is at most 1.6 intervals plus
	// one pass old when the next refresh reads it. That stays under maxStaleness while a pass takes under 0.4 intervals.
	return c.RefreshInterval / 2
}

// Validate returns an error when the resolver is enabled with invalid settings. storageLag is the value of
// -dataobj.storage-lag. The refresh interval must be at least one second. The active window must not be shorter than the
// refresh interval. The size limit must be positive. Twice the refresh interval must not exceed a tenth of storageLag.
func (c TOCResolverConfig) Validate(storageLag time.Duration) error {
	if !c.Enabled {
		return nil
	}
	if c.RefreshInterval < minTOCRefreshInterval {
		return fmt.Errorf("the refresh interval of the ToC resolver (%s) must be at least %s", c.RefreshInterval, minTOCRefreshInterval)
	}
	if c.ActiveWindow < c.RefreshInterval {
		return fmt.Errorf("the active window of the ToC resolver (%s) must not be shorter than its refresh interval (%s)", c.ActiveWindow, c.RefreshInterval)
	}
	if c.MaxMemoryCacheBytes == 0 {
		return errors.New("the max memory cache bytes of the ToC resolver must be positive")
	}
	// A ToC can miss the index entries written in the last max staleness. That is harmless while the indexing delay
	// plus max staleness stays under the storage lag. A tenth of the lag leaves the rest for the indexing delay.
	if c.maxStaleness() > storageLag/10 {
		return fmt.Errorf("twice the refresh interval of the ToC resolver (%s) must not exceed a tenth of the storage lag (%s)", c.maxStaleness(), storageLag)
	}
	return nil
}

// tocEntry is the state of one ToC in memory.
type tocEntry struct {
	// A refresh replaces value. So does a query that finds the value stale. If the new value does not fit in the size
	// limit, the ToC leaves memory instead. tocsMx guards value.
	value *tocValue

	// requestedAt is the time of the last request, in Unix nanoseconds. It is atomic because a lookup sets it after it
	// releases the read lock, while a refresh reads it under the write lock.
	requestedAt atomic.Int64
}

// isActive reports whether a request marked the ToC at most window before now.
func (e *tocEntry) isActive(now time.Time, window time.Duration) bool {
	return now.Sub(time.Unix(0, e.requestedAt.Load())) <= window
}

// tocValue is the bytes of a ToC and the time they were read from object storage. It is immutable.
type tocValue struct {
	// data is empty when the ToC did not exist at readAt.
	data   []byte
	readAt time.Time
}

// size returns the memory the resolver counts for the value itself: the bytes of the ToC. The path of the ToC adds to it
// in tocSize.
func (v *tocValue) size() int {
	return len(v.data)
}

// isFresh reports whether the bytes were read at most maxAge before now. A read time ahead of now by more than
// tocMaxClockSkew is not fresh.
func (v *tocValue) isFresh(now time.Time, maxAge time.Duration) bool {
	age := now.Sub(v.readAt)
	return age >= -tocMaxClockSkew && age <= maxAge
}

// marshal returns the value that a refresh stores in the shared cache. It holds one version byte, the read time as 8
// bytes of Unix nanoseconds in big endian, and the bytes of the ToC.
func (v *tocValue) marshal() []byte {
	buf := make([]byte, tocValueHeaderSize+len(v.data))
	buf[0] = tocValueVersion
	binary.BigEndian.PutUint64(buf[1:tocValueHeaderSize], uint64(v.readAt.UnixNano()))
	copy(buf[tocValueHeaderSize:], v.data)
	return buf
}

// unmarshal sets v from a value that marshal wrote. It returns an error when buf is too short or has another version,
// and then leaves v unchanged. The data of v shares memory with buf.
func (v *tocValue) unmarshal(buf []byte) error {
	if len(buf) < tocValueHeaderSize {
		return fmt.Errorf("the ToC value has %d bytes, fewer than its %d byte header", len(buf), tocValueHeaderSize)
	}
	if buf[0] != tocValueVersion {
		return fmt.Errorf("the ToC value has version %d, want version %d", buf[0], tocValueVersion)
	}

	v.readAt = time.Unix(0, int64(binary.BigEndian.Uint64(buf[1:tocValueHeaderSize])))
	v.data = nil
	if len(buf) > tocValueHeaderSize {
		v.data = buf[tocValueHeaderSize:]
	}
	return nil
}

// TOCResolver serves the raw bytes of ToCs, from memory when it can.
//
// While it runs as a service, it refreshes the ToCs that queries requested within the active window. It drops the other
// ToCs. When the total size is over nine tenths of the limit, it also drops the least recently requested ToCs. A
// resolver that does not run does none of this.
//
// The resolver does not store a ToC that would take the total size over the limit. Queries read that ToC from object
// storage until a refresh frees enough space. A held ToC whose new value does not fit leaves memory.
//
// A resolver whose config is not enabled keeps nothing in memory and reads every ToC from object storage. Its service
// does nothing.
//
// A ToC that the resolver serves is at most twice the refresh interval old. It can miss the entries written since.
//
// Code that deletes an index object must wait after the ToC stops listing it. The wait must exceed twice the refresh
// interval plus the longest query.
//
// The resolver is safe for concurrent use.
type TOCResolver struct {
	services.Service

	bucket objstore.BucketReader

	// cache is the shared cache that refreshes read through. It may be nil.
	cache *metadatacache.Cache

	// cacheKeyPrefix scopes the keys in cache to the index storage prefix of bucket.
	cacheKeyPrefix string

	cfg     TOCResolverConfig
	metrics *tocResolverMetrics
	now     func() time.Time

	// jitter returns how long a refresh pass waits after its tick. It is below a tenth of the refresh interval.
	jitter func() time.Duration

	// failureLogger is rate limited because failing object storage fails every ToC on every pass.
	failureLogger log.Logger

	// tocsMx guards tocs, tocsBytes and the value of every entry, so they always agree. A lookup takes the read lock.
	// Code that adds, replaces or removes a ToC takes the write lock. The caller of a method with the suffix Locked must
	// hold the write lock.
	tocsMx sync.RWMutex

	// tocs maps the path of a ToC to its entry.
	tocs map[string]*tocEntry

	// tocsBytes is the sum of tocSize over tocs.
	tocsBytes int

	// reads shares one read of object storage between concurrent lookups of the same ToC.
	reads singleflight.Group
}

// NewTOCResolver returns a resolver for the ToCs in bucket. Run it as a service.
//
// cfg must pass TOCResolverConfig.Validate. cacheKeyPrefix goes before the path of a ToC in the keys of cache.
// Resolvers that share a cache but read different buckets need different prefixes. cache and reg may be nil. A second
// resolver on one registerer panics. A resolver with a disabled cfg reads object storage only, and it still registers
// its metrics.
func NewTOCResolver(bucket objstore.BucketReader, cfg TOCResolverConfig, cache *metadatacache.Cache, cacheKeyPrefix string, logger log.Logger, reg prometheus.Registerer) *TOCResolver {
	r := &TOCResolver{
		bucket:         bucket,
		cache:          cache,
		cacheKeyPrefix: cacheKeyPrefix,
		cfg:            cfg,
		now:            time.Now,
		tocs:           map[string]*tocEntry{},
		jitter:         func() time.Duration { return rand.N(max(cfg.RefreshInterval/10, time.Nanosecond)) },
		failureLogger:  dslog.NewRateLimitedLogger(logger, tocFailureLogsPerSecond, tocFailureLogsBurst, nil),
	}
	r.metrics = newTOCResolverMetrics(reg)

	if cfg.Enabled {
		r.Service = services.NewTimerService(cfg.RefreshInterval, r.starting, r.iterate, nil)
	} else {
		r.Service = services.NewIdleService(nil, nil)
	}
	return r
}

// ReadTOC returns the bytes of the ToC at path, or no bytes and no error when the ToC does not exist. The slice is
// read-only and shared between callers. A call without an error marks a ToC that is in memory as requested, so refreshes
// keep it.
func (r *TOCResolver) ReadTOC(ctx context.Context, path string) ([]byte, error) {
	if !r.cfg.Enabled {
		v, err := r.readFromBucket(ctx, path)
		if err != nil {
			return nil, err
		}
		return v.data, nil
	}

	now := r.now()

	if data, ok := r.readFromLocalCache(path, now); ok {
		return data, nil
	}

	// A miss or a stale ToC reads object storage directly. This skips a cache round trip before the read.
	return r.readFromBucketSingleFlight(ctx, path, now)
}

// readFromLocalCache returns the ToC at path from memory. ok is false when memory holds no ToC, or one older than the max
// staleness. A hit marks the ToC as requested at now.
func (r *TOCResolver) readFromLocalCache(path string, now time.Time) (data []byte, ok bool) {
	r.tocsMx.RLock()
	entry, found := r.tocs[path]
	var v *tocValue
	if found {
		v = entry.value
	}
	r.tocsMx.RUnlock()

	if !found {
		r.metrics.lookups.WithLabelValues(lookupMiss).Inc()
		return nil, false
	}

	// The value is immutable, so it is safe to use after the unlock.
	if !v.isFresh(now, r.cfg.maxStaleness()) {
		r.metrics.lookups.WithLabelValues(lookupStale).Inc()
		return nil, false
	}

	entry.requestedAt.Store(now.UnixNano())
	r.metrics.lookups.WithLabelValues(lookupHit).Inc()
	return v.data, true
}

// readFromBucketSingleFlight reads the ToC from object storage and keeps it in memory. Concurrent calls for one path
// share one read.
func (r *TOCResolver) readFromBucketSingleFlight(ctx context.Context, path string, requestedAt time.Time) ([]byte, error) {
	ch := r.reads.DoChan(path, func() (any, error) {
		// The shared read ignores the cancellation of the caller that started it, so that caller giving up does not
		// fail the others. Each caller still returns when its own context ends.
		readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tocReadTimeout)
		defer cancel()

		v, err := r.readFromBucket(readCtx, path)
		if err != nil {
			// The query API turns any error that wraps context.DeadlineExceeded into a 504. The message tells the user to
			// shorten the query. A shorter query does not fix a slow ToC read. So the error names the read. It does not
			// wrap the deadline error, on purpose.
			if errors.Is(readCtx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("reading the ToC %s from object storage took longer than %s", path, tocReadTimeout)
			}
			return nil, err
		}
		r.metrics.loads.WithLabelValues(sourceBucket, triggerMiss).Inc()
		r.writeToLocalCache(path, v, requestedAt)
		return v, nil
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*tocValue).data, nil
	}
}

// readFromBucket reads the ToC at path from object storage and stamps it with the read time. The value has no bytes and
// there is no error when the ToC does not exist.
func (r *TOCResolver) readFromBucket(ctx context.Context, path string) (*tocValue, error) {
	// Take the time before the read, so it is never later than the moment object storage served the bytes.
	readAt := r.now()

	rc, err := r.bucket.Get(ctx, path)
	if err != nil {
		if r.bucket.IsObjNotFoundErr(err) {
			return &tocValue{readAt: readAt}, nil
		}
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("reading metastore object: %w", err)
	}

	// A ToC that exists is never empty. A store that truncates an object before it writes the new one can serve an
	// empty read. That read must not count as a missing ToC.
	if len(data) == 0 {
		return nil, fmt.Errorf("metastore object %s is empty", path)
	}
	return &tocValue{data: data, readAt: readAt}, nil
}

// writeToLocalCache stores v for path and marks the ToC as requested at requestedAt. An entry that holds a newer value
// keeps it. A new ToC that would take the total size over the size limit does not enter memory. When v replaces an
// entry but does not fit, the entry leaves memory.
func (r *TOCResolver) writeToLocalCache(path string, v *tocValue, requestedAt time.Time) {
	r.tocsMx.Lock()
	defer r.tocsMx.Unlock()

	// An entry already exists when the ToC is stale or a concurrent call stored it. Keep the entry and let replaceLocked
	// compare the values. An older value then never overwrites a newer one.
	if e, ok := r.tocs[path]; ok {
		e.requestedAt.Store(requestedAt.UnixNano())
		r.replaceLocked(path, e, v, r.now())
		return
	}

	size := tocSize(path, v)
	if r.tocsBytes+size > r.cfg.MaxMemoryCacheBytes.Val() {
		r.metrics.evictions.WithLabelValues(evictionRejected).Inc()
		return
	}

	e := &tocEntry{value: v}
	e.requestedAt.Store(requestedAt.UnixNano())
	r.tocs[path] = e
	r.tocsBytes += size
}

// updateInLocalCache sets the value of the ToC at path to v when the ToC is in memory and v was read after its value.
// A ToC that leaves memory while the read runs does not come back.
func (r *TOCResolver) updateInLocalCache(path string, v *tocValue, now time.Time) {
	r.tocsMx.Lock()
	defer r.tocsMx.Unlock()

	if e, ok := r.tocs[path]; ok {
		r.replaceLocked(path, e, v, now)
	}
}

// replaceLocked sets the value of e, the entry at path, to v. It does so when v was read after the value that e holds.
// It also does so when that value is stamped ahead of the clock by more than tocMaxClockSkew. If v would take the total
// size over the size limit, replaceLocked drops the ToC from memory instead.
func (r *TOCResolver) replaceLocked(path string, e *tocEntry, v *tocValue, now time.Time) {
	// The new value can be older than the held one. A read for a query can finish after a refresh replaced the entry. A
	// refresh can also read a value from the shared cache that another querier read earlier. An older value must not
	// replace the held one. One exception: a held value whose read time is ahead of the clock by more than
	// tocMaxClockSkew. A clock that stepped back leaves such values. They count as stale, so any new value replaces them.
	if !v.readAt.After(e.value.readAt) && e.value.readAt.Sub(now) <= tocMaxClockSkew {
		return
	}

	// The path of the entry does not change, so only the size of the value counts.
	delta := v.size() - e.value.size()
	if r.tocsBytes+delta > r.cfg.MaxMemoryCacheBytes.Val() {
		// The new value does not fit, so the resolver cannot keep this ToC fresh. The held value would go stale. Drop it,
		// so its space can hold other ToCs.
		r.removeLocked(path, evictionOversized)
		return
	}

	r.tocsBytes += delta
	e.value = v
}

// removeLocked drops the ToC at path from memory and counts it as evicted for reason.
func (r *TOCResolver) removeLocked(path, reason string) {
	e, ok := r.tocs[path]
	if !ok {
		return
	}
	delete(r.tocs, path)
	r.tocsBytes -= tocSize(path, e.value)
	r.metrics.evictions.WithLabelValues(reason).Inc()
}

func (r *TOCResolver) starting(context.Context) error {
	if r.cfg.RefreshInterval <= 0 {
		return errors.New("the refresh interval of the ToC resolver must be positive")
	}
	return nil
}

// iterate runs one refresh pass after a short random wait. It returns nil when ctx ends during the wait.
func (r *TOCResolver) iterate(ctx context.Context) error {
	// The wait spreads the passes of queriers that started together. A querier that refreshes first fills the
	// shared cache, and the others accept its value instead of reading object storage. The service stops on an error,
	// so iterate never returns one.
	timer := time.NewTimer(r.jitter())
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil
	case <-timer.C:
		r.refreshTOCs(ctx)
		return nil
	}
}

// refreshTOCs runs one pass over the ToCs in memory.
func (r *TOCResolver) refreshTOCs(ctx context.Context) {
	type activeTOC struct {
		path   string
		readAt time.Time
	}

	now := r.now()

	// Free memory first: drop the ToCs that no query requested within the active window, then the ToCs that were
	// requested least recently while the size is over the eviction target. The pass refreshes only the ToCs that stay.
	var active []activeTOC
	r.tocsMx.Lock()
	for path, e := range r.tocs {
		if !e.isActive(now, r.cfg.ActiveWindow) {
			r.removeLocked(path, evictionInactive)
		}
	}
	r.evictOverTargetLocked()
	for path, e := range r.tocs {
		active = append(active, activeTOC{path: path, readAt: e.value.readAt})
	}
	r.tocsMx.Unlock()

	// Read the oldest ToCs first. When a pass outlasts the refresh interval, the ToCs closest to the max staleness then
	// get read first.
	slices.SortFunc(active, func(a, b activeTOC) int { return a.readAt.Compare(b.readAt) })

	var g errgroup.Group
	g.SetLimit(tocMaxConcurrentReads)
	for _, toc := range active {
		g.Go(func() error {
			r.refreshTOC(ctx, toc.path)
			return nil
		})
	}
	_ = g.Wait()
}

// refreshTOC reads the ToC at path again and replaces the value in memory when the new one is newer.
func (r *TOCResolver) refreshTOC(ctx context.Context, path string) {
	start := time.Now()

	readCtx, cancel := context.WithTimeout(ctx, tocReadTimeout)
	defer cancel()

	v, source, err := r.readForRefresh(readCtx, path)
	if err != nil {
		// The old value stays in use until it is older than the max staleness.
		if ctx.Err() == nil {
			level.Warn(r.failureLogger).Log("msg", "refreshing a ToC failed", "path", path, "err", err)
		}
		r.metrics.refreshed(refreshError, time.Since(start))
		return
	}

	r.updateInLocalCache(path, v, r.now())

	result := refreshOK
	if len(v.data) == 0 {
		result = refreshNotFound
	}
	r.metrics.loads.WithLabelValues(source, triggerRefresh).Inc()
	r.metrics.refreshed(result, time.Since(start))
}

// readForRefresh reads the ToC through the shared cache when there is one, and from object storage otherwise. It also
// returns the source of the bytes: sourceCache or sourceBucket.
func (r *TOCResolver) readForRefresh(ctx context.Context, path string) (*tocValue, string, error) {
	if r.cache == nil {
		v, err := r.readFromBucket(ctx, path)
		return v, sourceBucket, err
	}

	// The cache runs load on its own goroutine, which can outlive this call.
	var loaded atomic.Bool

	accept := func(buf []byte) bool {
		var v tocValue
		return v.unmarshal(buf) == nil && v.isFresh(r.now(), r.cfg.maxCachedAge())
	}
	load := func(ctx context.Context) ([]byte, error) {
		ctx, cancel := context.WithTimeout(ctx, tocReadTimeout)
		defer cancel()

		v, err := r.readFromBucket(ctx, path)
		if err != nil {
			return nil, err
		}
		loaded.Store(true)
		return v.marshal(), nil
	}

	// The first querier of a refresh round stores the ToC in the shared cache, and the others accept it. So all queriers
	// together read each ToC from object storage about once per refresh interval. Half an interval is the upper bound on
	// how long the others reuse a value. A ToC larger than the item limit of the cache never stays in it, so every
	// refresh reads that ToC from object storage.
	buf, err := r.cache.GetOrLoad(ctx, r.cacheKeyPrefix+path, accept, load)
	if err != nil {
		return nil, "", err
	}
	v := &tocValue{}
	if err := v.unmarshal(buf); err != nil {
		return nil, "", fmt.Errorf("the shared cache returned a ToC value that does not decode: %w", err)
	}

	source := sourceCache
	if loaded.Load() {
		source = sourceBucket
	}
	return v, source, nil
}

// evictOverTargetLocked drops the ToCs that were requested least recently until the total size is at most the eviction
// target. It drops nothing when the size is at most the target already.
func (r *TOCResolver) evictOverTargetLocked() {
	target := r.cfg.evictionTarget()
	if r.tocsBytes <= target {
		return
	}

	type candidate struct {
		path        string
		requestedAt int64
	}
	candidates := make([]candidate, 0, len(r.tocs))
	for path, e := range r.tocs {
		candidates = append(candidates, candidate{path: path, requestedAt: e.requestedAt.Load()})
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		return cmp.Compare(a.requestedAt, b.requestedAt)
	})

	for _, c := range candidates {
		if r.tocsBytes <= target {
			return
		}
		r.removeLocked(c.path, evictionSize)
	}
}

// tocSize returns the memory the resolver counts for a ToC: its bytes and its path.
func tocSize(path string, v *tocValue) int {
	return len(path) + v.size()
}
