package redis

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// cacheEntryState tracks the lifecycle of a local cache entry.
type cacheEntryState uint8

const (
	// cacheEntryInProgress marks a placeholder entry while a value is being fetched.
	cacheEntryInProgress cacheEntryState = iota
	// cacheEntryValid marks an entry that contains a value that can be returned.
	cacheEntryValid
)

// cacheEntry represents a cached command reply and its Redis-key associations.
type cacheEntry struct {
	cacheKey  string
	redisKeys []string
	value     []byte
	state     cacheEntryState

	token      uint64
	sizeBytes  int64
	reservedAt time.Time
	waitCh     chan struct{}
	waitClosed bool

	// lastAccessNs orders entries for eviction: a global atomic counter bumped
	// when the entry is created, reserved or fulfilled — all of which happen
	// under the shard write lock, on the MISS path. It is stored atomically
	// because the read path reads it under the shard's RLock.
	//
	// It is NOT bumped on a read any more; a read sets readSinceSweep instead.
	lastAccessNs atomic.Int64

	// readSinceSweep records that a READ has touched this entry since the last
	// eviction sweep. It is the second-chance bit for eviction: a victim is
	// chosen from the entries with the bit CLEAR, and the sweep clears the bits
	// when every candidate has been read.
	//
	// It exists because writing lastAccessNs on every hit was the dominant cost
	// of a cache hit under concurrency. ~40k resident entries at a few hundred
	// bytes each is a working set far larger than L2; the read already touches
	// the entry's cache line, but a STORE dirties it, and a dirty line costs a
	// writeback when it is evicted from the CPU cache. Measured on a 14-core
	// host at 256 concurrent readers, get=90/set=10, 99.94% hit rate:
	// 263,801 -> 328,842 reads/s (+24.7%) and 27.0 -> 18.9 CPU us/op, with the
	// hit rate unchanged. Removing the recency update altogether measured
	// +40.5%, so this recovers about three fifths of the available headroom
	// while keeping eviction honest.
	//
	// A load that finds the bit already set leaves the line SHARED, so every
	// core can hold it at once; only the first read after a sweep writes.
	readSinceSweep atomic.Bool

	// refreshKeep marks an IN_PROGRESS refresh reservation: fulfill publishes
	// it with refreshAccessNs and refreshRead instead of a fresh recency. See
	// stageRefreshAccess. Written and read under the shard write lock.
	refreshKeep     bool
	refreshRead     bool
	refreshAccessNs int64

	// validAt retains time.Now's monotonic component for the MaxStaleness
	// backstop, so wall-clock corrections cannot extend an entry's lifetime.
	// Written under Lock (Set/Fulfill), read under RLock (get).
	validAt time.Time

	// fetchSeq is the global cscFetchSeq value at the moment this entry's fetch was
	// ISSUED (Reserve), carried unchanged through fulfill. It lets a batched
	// invalidation tell "this value predates me" from "this value was refetched
	// after me": an invalidation snapshots cscFetchSeq at OBSERVE time, and a delete
	// is skipped when entry.fetchSeq > that snapshot (the fetch was issued after the
	// invalidate, so it reached a server that had already applied the write). Fetch-
	// ISSUE order is used, not fulfill-COMPLETION order, because the invalidation and
	// the reply travel on different connections with no ordering — a stale reply can
	// fulfill after the invalidate is observed (see deleteByRedisKey). Set/read under
	// the shard Lock.
	fetchSeq uint64

	// ownerConnID is the conn that fetched this entry (set by FulfillOwned; 0 =
	// none). Default CLIENT TRACKING sends a key's invalidation only to that
	// conn, so the entry must be evicted when it goes away (see EvictByConn).
	ownerConnID uint64
}

// lruSequence is the global monotonic counter feeding lastAccessNs. It totally
// orders recency across all entries in all shards for approximate-LRU eviction.
var lruSequence atomic.Int64

// nextLRUToken returns the next strictly-greater LRU token.
func nextLRUToken() int64 {
	return lruSequence.Add(1)
}

// cscFetchSeq is the global monotonic counter feeding cacheEntry.fetchSeq. It
// totally orders fetch-ISSUE (Reserve) events against invalidation OBSERVE
// events so a batched delete can skip an entry refetched after the invalidation
// (see cacheEntry.fetchSeq).
var cscFetchSeq atomic.Uint64

// nextFetchSeq returns the next strictly-greater fetch-issue sequence.
func nextFetchSeq() uint64 {
	return cscFetchSeq.Add(1)
}

// CacheSizer calculates estimated memory usage in bytes for a cache entry.
//
// Experimental: this API may change in a minor release.
type CacheSizer func(cacheKey string, redisKeys []string, value []byte) int64

// CacheConfig configures a local cache instance.
//
// Experimental: this API may change in a minor release.
type CacheConfig struct {
	// MaxEntries limits the number of entries. Zero or negative means unlimited.
	MaxEntries int
	// MaxMemoryBytes limits estimated memory usage in bytes. Zero or negative means unlimited.
	//
	// If both MaxEntries and MaxMemoryBytes are unlimited, MaxEntries defaults to
	// defaultCacheMaxEntries so the cache cannot grow without bound. The cache is
	// sharded 16 ways (above small thresholds) and each shard enforces its 1/16
	// share, so an entry larger than MaxMemoryBytes/16 is never admitted —
	// size it to at least 16× your largest reply.
	MaxMemoryBytes int64
	// Sizer estimates memory usage per entry. If nil, a built-in approximation is used.
	//
	// Sizer may be invoked concurrently from multiple goroutines and must be
	// thread-safe. It must return quickly and must not call back into the
	// cache (Get, Set, Delete*, Flush, etc.): some call sites hold an internal
	// shard lock, so re-entry can deadlock.
	Sizer CacheSizer
	// StaleTimeout is the duration after which an IN_PROGRESS placeholder is
	// considered stale and eligible for takeover by a new Reserve call.
	// If zero, defaults to defaultStaleTimeout (5s).
	StaleTimeout time.Duration

	// DrainInterval is the background-drainer period (default 5ms; zero uses the
	// default): how often idle pool conns are swept for buffered "invalidate"
	// frames, roughly bounding cache-hit staleness. Values below 1ms are clamped
	// to 1ms.
	DrainInterval time.Duration

	// MaxStaleness caps how long a cached entry is served after it became valid,
	// regardless of invalidation. It is a correctness
	// BACKSTOP for lost invalidations or connection-lifecycle gaps ("Window 2"), not
	// the primary freshness mechanism. Keep it well above the invalidation round-trip
	// (e.g. seconds); per-entry refetch overhead scales ~1/MaxStaleness.
	//
	// Default: 0 (disabled).
	MaxStaleness time.Duration
}

// Cache is the thread-safe storage contract used by client-side caching.
//
// All methods may be called concurrently. Cache keys and Redis keys are opaque
// strings and must be preserved exactly. Removing a reservation must wake any
// Get calls waiting for it.
//
// Reserve must allow only one caller to fetch a missing key and return a token
// that is valid until FulfillOwned, Cancel, or an eviction removes that
// reservation. FulfillOwned and Cancel must modify only a reservation with the
// matching token. Get may wait for an in-progress reservation and must stop
// waiting when ctx is done.
//
// Experimental: this API may change in a minor release.
type Cache interface {
	Get(ctx context.Context, cacheKey string) ([]byte, bool)
	Reserve(cacheKey string, redisKeys []string) (token uint64, shouldFetch bool)
	// FulfillOwned publishes a reserved value and records the connection that
	// fetched it so the entry can be evicted if that connection loses tracking.
	FulfillOwned(cacheKey string, token, ownerConnID uint64, value []byte) bool
	Cancel(cacheKey string, token uint64) bool
	DeleteByRedisKey(redisKey string) int
	DeleteByCacheKey(cacheKey string) bool
	// EvictByConn removes every entry fetched by connID.
	EvictByConn(connID uint64) int
	Flush() int
}

const (
	defaultStaleTimeout    = 5 * time.Second
	defaultCacheShardCount = 16

	// defaultCacheMaxEntries bounds the cache when the config leaves both
	// MaxEntries and MaxMemoryBytes unlimited (matches the 10k-entry default
	// other Redis clients use, e.g. redis-py).
	defaultCacheMaxEntries = 10000

	// shardingThresholdEntries / shardingThresholdBytes: caches with capacity
	// below these thresholds fall back to a single shard so global LRU /
	// memory-cap semantics behave exactly as a non-sharded cache would.
	shardingThresholdEntries = 64
	shardingThresholdBytes   = 64 * 1024
)

// NewLocalCache creates a thread-safe local cache with approximate-LRU
// eviction. The cache is internally sharded by cache-key hash to reduce
// mutex contention under high concurrent access.
//
// Experimental: this API may change in a minor release.
func NewLocalCache(cfg CacheConfig) *LocalCache {
	sizer := cfg.Sizer
	if sizer == nil {
		sizer = defaultCacheSizer
	}

	staleTimeout := cfg.StaleTimeout
	if staleTimeout <= 0 {
		staleTimeout = defaultStaleTimeout
	}

	maxEntries := cfg.MaxEntries
	maxMemoryBytes := cfg.MaxMemoryBytes
	// An unbounded cache can grow until the process OOMs; require at least
	// one limit.
	if maxEntries <= 0 && maxMemoryBytes <= 0 {
		maxEntries = defaultCacheMaxEntries
	}

	shardCount := defaultCacheShardCount
	if maxEntries > 0 && maxEntries < shardingThresholdEntries {
		shardCount = 1
	}
	if maxMemoryBytes > 0 && maxMemoryBytes < int64(shardingThresholdBytes) {
		shardCount = 1
	}

	c := &LocalCache{
		shards:     make([]cacheShard, shardCount),
		shardCount: uint32(shardCount),
		shardMask:  uint32(shardCount - 1),
		sizer:      sizer,
	}
	for i := range c.shards {
		s := &c.shards[i]
		s.entries = make(map[string]*cacheEntry)
		s.byRedisKey = make(map[string]map[string]struct{})
		s.byConnID = make(map[uint64]map[string]struct{})
		// Distribute capacity so the per-shard caps sum to exactly the
		// configured limits; a ceil-per-shard split would let total residency
		// exceed MaxEntries/MaxMemoryBytes.
		if maxEntries > 0 {
			s.maxEntries = maxEntries / shardCount
			if i < maxEntries%shardCount {
				s.maxEntries++
			}
		}
		if maxMemoryBytes > 0 {
			s.maxMemoryBytes = maxMemoryBytes / int64(shardCount)
			if int64(i) < maxMemoryBytes%int64(shardCount) {
				s.maxMemoryBytes++
			}
		}
		s.maxStaleness = cfg.MaxStaleness
		s.sizer = sizer
		s.staleTimeout = staleTimeout
	}
	return c
}

// effectiveMaxStaleness reports the cache's staleness bound (0 = none). Every
// shard carries the same value, so shard 0 is authoritative. Used by
// Options.init to run the batch-window-vs-staleness sanity warning for an
// INJECTED *LocalCache too, where no ClientSideCacheConfig exists to read.
func (c *LocalCache) effectiveMaxStaleness() time.Duration {
	if len(c.shards) == 0 {
		return 0
	}
	return c.shards[0].maxStaleness
}

// LocalCache is the built-in sharded approximate-LRU cache.
//
// Experimental: this API may change in a minor release.
type LocalCache struct {
	shards     []cacheShard
	shardCount uint32
	shardMask  uint32
	sizer      CacheSizer

	nextToken atomic.Uint64
	hits      atomic.Uint64
	misses    atomic.Uint64

	// Invalidation accounting for refresh-on-invalidate (see CSCRefreshStats).
	// invalidations counts keys named in INCOMING pushes, tallied once at the
	// handler choke point before dedup/batching. deletions/deletionsNoop count
	// APPLIED deletes (post-dedup) and the subset that matched no live entry. The
	// gap between invalidations and deletions is the direct measure of dedup +
	// duplicate invalidations (and, under a flood, the spill-cap full-Flush).
	invalidations atomic.Uint64
	deletions     atomic.Uint64
	deletionsNoop atomic.Uint64
}

var _ Cache = (*LocalCache)(nil)

// cacheShard holds the state for one shard of LocalCache. The mutex
// protects entries, byRedisKey, byConnID, and usedBytes.
type cacheShard struct {
	mu         sync.RWMutex
	entries    map[string]*cacheEntry
	byRedisKey map[string]map[string]struct{}
	// byConnID is the owning-conn reverse index (twin of byRedisKey): conn id ->
	// its cache keys. Populated by FulfillOwned, cleaned in removeEntryLocked,
	// consumed by EvictByConn.
	byConnID  map[uint64]map[string]struct{}
	usedBytes int64

	maxEntries     int
	maxMemoryBytes int64
	maxStaleness   time.Duration
	sizer          CacheSizer
	staleTimeout   time.Duration
}

// collectHotAndDeleteBatch applies a whole invalidation batch under ONE
// acquisition of this shard's lock.
//
// The per-key work is identical to collectHotAndDelete; only the locking
// granularity changes. The caller loops shards on the outside and keys on the
// inside, so a batch of N costs 16 lock acquisitions instead of 16*N -- which
// is what let the single batcher worker fall permanently behind a 20k/sec
// invalidation stream and leave the cache serving stale entries.
//
// keys and the parallel guards are indexed together: sinceTokens[i] and
// fetchSnaps[i] belong to keys[i].
//
// matched[i] is set when this shard removed something for keys[i]. The caller
// ORs it across shards, because one redis key can appear in several shards: a
// multi-key entry is filed under its CACHE key's shard, so each of its redis
// keys is indexed wherever that entry lives. Per-key match tracking is what
// keeps the no-op deletion count equal to the single-key path's.
func (s *cacheShard) collectHotAndDeleteBatch(keys []string, sinceTokens []int64, fetchSnaps []uint64, dst []cscRefreshTarget, matched []bool) ([]cscRefreshTarget, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for i, redisKey := range keys {
		cacheKeys, ok := s.byRedisKey[redisKey]
		if !ok {
			continue
		}
		sinceToken, fetchSnap := sinceTokens[i], fetchSnaps[i]
		toRemove := make([]string, 0, len(cacheKeys))
		for cacheKey := range cacheKeys {
			toRemove = append(toRemove, cacheKey)
		}
		for _, cacheKey := range toRemove {
			entry, exists := s.entries[cacheKey]
			if exists && entry.fetchSeq > fetchSnap {
				continue
			}
			if exists && entry.state == cacheEntryValid && entry.lastAccessNs.Load() > sinceToken {
				ks := make([]string, len(entry.redisKeys))
				copy(ks, entry.redisKeys)
				dst = append(dst, cscRefreshTarget{
					cacheKey:  cacheKey,
					redisKeys: ks,
					accessNs:  entry.lastAccessNs.Load(),
					// The second-chance bit, as collectHotAndDelete keeps it:
					// the refresh republishes the entry with it.
					read:     entry.readSinceSweep.Load(),
					valBytes: len(entry.value),
				})
			}
			if s.removeEntryLocked(cacheKey) {
				matched[i] = true
				removed++
			}
		}
	}
	return dst, removed
}

// deleteManyByRedisKeyCollectingHot is the cache-level batch entry point: one
// pass over the shards, every key of the batch handled under each shard's
// single lock.
func (c *LocalCache) deleteManyByRedisKeyCollectingHot(keys []string, sinceTokens []int64, fetchSnaps []uint64, dst []cscRefreshTarget) ([]cscRefreshTarget, int) {
	if len(keys) == 0 {
		return dst, 0
	}
	removed := 0
	matched := make([]bool, len(keys))
	for i := range c.shards {
		var n int
		dst, n = c.shards[i].collectHotAndDeleteBatch(keys, sinceTokens, fetchSnaps, dst, matched)
		removed += n
	}
	// Applied-delete accounting, per KEY, exactly as the single-key path does
	// it: every key is one deletion, and a key that removed nothing is one
	// no-op. Deriving the no-op count from the batch total instead would
	// undercount -- a batch where one key matched and nine did not would
	// record zero no-ops rather than nine.
	c.deletions.Add(uint64(len(keys)))
	noop := 0
	for _, ok := range matched {
		if !ok {
			noop++
		}
	}
	if noop > 0 {
		c.deletionsNoop.Add(uint64(noop))
	}
	return dst, removed
}

// shardFor returns the shard responsible for cacheKey.
func (c *LocalCache) shardFor(cacheKey string) *cacheShard {
	if c.shardCount == 1 {
		return &c.shards[0]
	}
	return &c.shards[fnv1a32(cacheKey)&c.shardMask]
}

// fnv1a32 returns the FNV-1a 32-bit hash of s. Allocation-free.
func fnv1a32(s string) uint32 {
	const (
		offset uint32 = 2166136261
		prime  uint32 = 16777619
	)
	h := offset
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}

const defaultCacheEntryOverhead int64 = 96

func defaultCacheSizer(cacheKey string, redisKeys []string, value []byte) int64 {
	size := defaultCacheEntryOverhead + int64(len(cacheKey)+len(value))
	for _, key := range redisKeys {
		size += int64(len(key)) + 16
	}
	if size < 0 {
		return 0
	}
	return size
}

// Get returns a copy of a cached value, waiting for an in-progress fetch when
// necessary.
func (c *LocalCache) Get(ctx context.Context, cacheKey string) ([]byte, bool) {
	value, ok := c.get(ctx, cacheKey, true)
	return value, ok
}

// getShared is Get without the defensive copy. The returned slice ALIASES the
// cache entry, so the caller must neither mutate nor retain it; it is valid
// only until the caller returns.
//
// Safe because a published value is immutable: cacheShard.get never writes
// through the slice, and a refetch REPLACES entry.value wholesale under the
// shard write lock rather than editing it in place, so an old slice a reader
// already holds keeps its contents.
//
// Not on the Cache interface, and deliberately so: Get's []byte return means a
// third-party implementation's caller may legitimately retain what it gets
// back, so the copy has to stay there. Only the built-in cache paired with the
// built-in read path (processCached, which parses the bytes and drops them)
// can skip it -- the same "only the built-in *LocalCache" gate miss coalescing
// uses.
func (c *LocalCache) getShared(ctx context.Context, cacheKey string) ([]byte, bool) {
	return c.get(ctx, cacheKey, false)
}

func (c *LocalCache) get(ctx context.Context, cacheKey string, clone bool) ([]byte, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	value, ok := c.shardFor(cacheKey).get(ctx, cacheKey, clone)
	if ok {
		c.hits.Add(1)
	} else {
		c.misses.Add(1)
	}
	return value, ok
}

// get is the read-side hot path. Holds only the shard's read lock; updates
// the LRU recency timestamp via atomic store on the entry — no write-lock
// upgrade is needed.
// get is the read-side hot path. clone=false returns a slice ALIASING the
// entry (see LocalCache.getShared for why that is safe and who may use it).
func (s *cacheShard) get(ctx context.Context, cacheKey string, clone bool) ([]byte, bool) {
	for {
		s.mu.RLock()
		entry, ok := s.entries[cacheKey]
		if !ok {
			s.mu.RUnlock()
			return nil, false
		}

		if entry.state == cacheEntryInProgress {
			waitCh := entry.waitCh
			// Bound the wait by the placeholder's remaining stale window so an
			// abandoned reservation cannot block waiters indefinitely.
			remaining := s.staleTimeout - time.Since(entry.reservedAt)
			s.mu.RUnlock()
			if waitCh == nil {
				// Defensive: treat a missing waitCh as a miss to avoid busy-looping.
				return nil, false
			}
			if remaining <= 0 {
				// Placeholder already stale; miss so the caller refetches.
				return nil, false
			}
			// Wait for the in-flight fetch to either publish (Fulfill) or abort (Cancel/Delete/Flush).
			timer := time.NewTimer(remaining)
			select {
			case <-waitCh:
				timer.Stop()
			case <-ctx.Done():
				timer.Stop()
				return nil, false
			case <-timer.C:
				return nil, false
			}
			continue
		}

		if entry.state != cacheEntryValid {
			s.mu.RUnlock()
			return nil, false
		}

		// Max-staleness backstop: a Valid entry older than maxStaleness is treated
		// as a miss and evicted, so a lost invalidation or connection-lifecycle
		// staleness (Window 2) cannot keep a stale value resident past MaxStaleness.
		// Evict under the write lock so the next access re-fetches — a stale-but-present
		// entry would otherwise suppress the re-fetch via Reserve.
		if s.maxStaleness > 0 && time.Since(entry.validAt) > s.maxStaleness {
			s.mu.RUnlock()
			s.mu.Lock()
			if cur, ok := s.entries[cacheKey]; ok && cur == entry {
				s.removeEntryLocked(cacheKey)
			}
			s.mu.Unlock()
			return nil, false
		}

		value := entry.value
		if clone {
			value = cloneBytes(value)
		}
		// Mark the second-chance bit instead of stamping a recency token. The
		// load short-circuits every read after the first since the last sweep,
		// which is the overwhelming majority, and leaves the entry's cache line
		// SHARED rather than taking it exclusively per hit. See
		// cacheEntry.readSinceSweep for the measurements.
		//
		// The store is skipped entirely on a shard that is nowhere near its
		// cap, because the bit is only ever read by eviction. That guard is
		// not a micro-optimisation: the surviving store was still 9.7% of all
		// CPU (15.24s of 157.84s, against 50ms for this Load). It stays hot
		// under churn because invalidated entries are refetched constantly and
		// every fresh entry's first read finds a clear bit, and each such
		// store invalidates, across every core, a line the readers hold
		// SHARED. len(s.entries) is safe to read here: writers hold the write
		// lock, and this path holds the read lock.
		if !entry.readSinceSweep.Load() && s.nearCapacityLocked() {
			entry.readSinceSweep.Store(true)
		}
		s.mu.RUnlock()
		return value, true
	}
}

// Stats returns cumulative activity and current residency.
func (c *LocalCache) Stats() CSCStats {
	return CSCStats{
		Hits:             c.hits.Load(),
		Misses:           c.misses.Load(),
		Entries:          c.Len(),
		MemoryUsageBytes: c.MemoryUsage(),
	}
}

// Reserve claims a missing cache key for fetching.
func (c *LocalCache) Reserve(cacheKey string, redisKeys []string) (token uint64, shouldFetch bool) {
	keysCopy := cloneStrings(redisKeys)
	waitCh := make(chan struct{})
	reservedAt := time.Now()
	sizeBytes := c.sizer(cacheKey, keysCopy, nil)
	if sizeBytes < 0 {
		sizeBytes = 0
	}
	newToken := c.nextToken.Add(1)

	s := c.shardFor(cacheKey)
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.entries[cacheKey]; ok {
		switch entry.state {
		case cacheEntryValid:
			// Existing-VALID hit: record access; caller will re-Get to
			// retrieve. This IS an access, so it also earns the second chance
			// the read path grants -- it is under the write lock and off the
			// hot path, so stamping the token here costs nothing.
			entry.lastAccessNs.Store(nextLRUToken())
			entry.readSinceSweep.Store(true)
			return 0, false
		case cacheEntryInProgress:
			if time.Since(entry.reservedAt) < s.staleTimeout {
				return 0, false
			}
			s.removeEntryLocked(cacheKey)
		default:
			return 0, false
		}
	}

	if s.maxMemoryBytes > 0 && sizeBytes > s.maxMemoryBytes {
		return 0, true
	}

	entry := &cacheEntry{
		cacheKey:   cacheKey,
		redisKeys:  keysCopy,
		state:      cacheEntryInProgress,
		token:      newToken,
		reservedAt: reservedAt,
		waitCh:     waitCh,
		sizeBytes:  sizeBytes,
		// Stamp fetch-ISSUE order now so a later invalidation can tell a value
		// refetched after it (keep) from one that predates it (evict). Carried
		// through fulfill unchanged. See cacheEntry.fetchSeq.
		fetchSeq: nextFetchSeq(),
	}
	entry.lastAccessNs.Store(nextLRUToken())

	s.setEntryLocked(entry)
	// Evict only Valid victims. If still over capacity the shard holds only
	// in-flight placeholders: rather than abort a peer's fetch, drop this
	// reservation (the caller fetches uncached). The hard cap holds either way.
	s.evictValidLocked()
	if s.overCapacityLocked() {
		s.removeEntryLocked(cacheKey)
		return 0, true
	}
	if s.entries[cacheKey] != entry {
		return 0, true
	}
	return newToken, true
}

// FulfillOwned publishes a reserved value and records ownerConnID so
// EvictByConn can drop it when that connection is removed. ownerConnID == 0
// leaves the value unowned.
func (c *LocalCache) FulfillOwned(cacheKey string, token, ownerConnID uint64, value []byte) bool {
	return c.fulfill(cacheKey, token, ownerConnID, value)
}

func (c *LocalCache) fulfill(cacheKey string, token, ownerConnID uint64, value []byte) bool {
	valueCopy := cloneBytes(value)

	s := c.shardFor(cacheKey)
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[cacheKey]
	if !ok || entry.state != cacheEntryInProgress || entry.token != token {
		return false
	}

	valueSize := s.sizer(cacheKey, entry.redisKeys, valueCopy)
	if valueSize < 0 {
		valueSize = 0
	}
	if s.maxMemoryBytes > 0 && valueSize > s.maxMemoryBytes {
		s.removeEntryLocked(cacheKey)
		return false
	}

	s.usedBytes += valueSize - entry.sizeBytes
	entry.value = valueCopy
	entry.sizeBytes = valueSize
	entry.state = cacheEntryValid
	entry.validAt = time.Now()
	entry.token = 0
	if entry.refreshKeep {
		// A refresh republish keeps the old entry's standing, set here in the
		// same lock as the publish so the eviction pass below, and any insert
		// after it, already see it.
		entry.lastAccessNs.Store(entry.refreshAccessNs)
		entry.readSinceSweep.Store(entry.refreshRead)
		entry.refreshKeep = false
	} else {
		entry.lastAccessNs.Store(nextLRUToken())
	}
	if ownerConnID != 0 {
		entry.ownerConnID = ownerConnID
		s.indexConnLocked(ownerConnID, cacheKey)
	}
	s.closeWaitersLocked(entry)

	// The entry just fulfilled is not a candidate in its own eviction pass. It
	// is born with its second-chance bit clear, so in a warm shard (every
	// resident entry read) it would be the only clear-bit candidate and be
	// evicted right here: a memory-capped shard, where the value only goes
	// over the cap now, could then admit nothing, and since a clear candidate
	// always existed the sweep that resets the others never ran. Excluding it
	// lets that sweep run and take the oldest entry instead.
	s.evictIfNeededLocked(entry)
	current, stillExists := s.entries[cacheKey]
	return stillExists && current == entry && entry.state == cacheEntryValid
}

// stageRefreshAccess sets the recency a refresh republish keeps on the
// IN_PROGRESS reservation token: lastAccessNs becomes accessNs and the
// second-chance bit becomes read, both taken from the invalidated entry.
// fulfill applies them in the same lock as the publish.
//
// The refresh does not count as a reader access: a fresh token would keep the
// key above the refresh horizon, so every later invalidation would refresh it
// again after all readers stop -- a self-sustaining refetch loop. Keeping the
// bit stops a hot key from being the first eviction victim only because the
// refresh republished it. Setting both before the publish, not after it,
// closes the gap in which an insert could evict the republished entry while
// its bit was still clear. No-op when the reservation is gone.
func (c *LocalCache) stageRefreshAccess(cacheKey string, token uint64, accessNs int64, read bool) {
	s := c.shardFor(cacheKey)
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.entries[cacheKey]; ok && entry.state == cacheEntryInProgress && entry.token == token {
		entry.refreshKeep = true
		entry.refreshAccessNs = accessNs
		entry.refreshRead = read
	}
}

// EvictByConn removes every entry fetched by connID and returns the count.
// Called when a conn is removed/swapped: the server stops delivering those
// keys' invalidations, so keeping them risks stale serves. Errs toward a miss.
func (c *LocalCache) EvictByConn(connID uint64) int {
	if connID == 0 {
		return 0
	}
	removed := 0
	for i := range c.shards {
		removed += c.shards[i].evictByConn(connID)
	}
	return removed
}

func (s *cacheShard) evictByConn(connID uint64) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cacheKeys, ok := s.byConnID[connID]
	if !ok {
		return 0
	}
	toRemove := make([]string, 0, len(cacheKeys))
	for cacheKey := range cacheKeys {
		toRemove = append(toRemove, cacheKey)
	}
	removed := 0
	for _, cacheKey := range toRemove {
		if s.removeEntryLocked(cacheKey) {
			removed++
		}
	}
	return removed
}

// indexConnLocked records cacheKey under connID in the owning-connection index.
func (s *cacheShard) indexConnLocked(connID uint64, cacheKey string) {
	cacheKeys := s.byConnID[connID]
	if cacheKeys == nil {
		cacheKeys = make(map[string]struct{})
		s.byConnID[connID] = cacheKeys
	}
	cacheKeys[cacheKey] = struct{}{}
}

// Cancel removes the reservation matching token.
func (c *LocalCache) Cancel(cacheKey string, token uint64) bool {
	s := c.shardFor(cacheKey)
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[cacheKey]
	if !ok || entry.state != cacheEntryInProgress || entry.token != token {
		return false
	}

	s.removeEntryLocked(cacheKey)
	return true
}

// DeleteByRedisKey removes entries associated with redisKey. It is the applied-
// delete path used when refresh-on-invalidate is off (the collecting variant is
// used when it is on); counting deletions here keeps DeletionStats accurate on
// both paths. The two are disjoint — neither calls the other — so no double count.
func (c *LocalCache) DeleteByRedisKey(redisKey string) int {
	removed := 0
	for i := range c.shards {
		removed += c.shards[i].deleteByRedisKey(redisKey)
	}
	c.deletions.Add(1)
	if removed == 0 {
		c.deletionsNoop.Add(1)
	}
	return removed
}

func (s *cacheShard) deleteByRedisKey(redisKey string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cacheKeys, ok := s.byRedisKey[redisKey]
	if !ok {
		return 0
	}

	// Remove IN_PROGRESS placeholders too: an invalidation can arrive on a
	// different stream than the in-flight reply (the background drainer), so the
	// fetch may predate the write. Removing makes the racing Fulfill fail and
	// waiters refetch, so a raced-invalidation value is never published.
	toRemove := make([]string, 0, len(cacheKeys))
	for cacheKey := range cacheKeys {
		toRemove = append(toRemove, cacheKey)
	}

	removed := 0
	for _, cacheKey := range toRemove {
		if s.removeEntryLocked(cacheKey) {
			removed++
		}
	}
	return removed
}

// DeleteByCacheKey removes one entry by its internal cache key.
func (c *LocalCache) DeleteByCacheKey(cacheKey string) bool {
	s := c.shardFor(cacheKey)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.removeEntryLocked(cacheKey)
}

// Flush removes all entries.
func (c *LocalCache) Flush() int {
	removed := 0
	for i := range c.shards {
		removed += c.shards[i].flush()
	}
	return removed
}

func (s *cacheShard) flush() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Flush placeholders too (see deleteByRedisKey): a flush (FLUSHDB, or the
	// owned-cache flush on Close) means everything, including in-flight fetches,
	// may be stale.
	removed := 0
	for cacheKey := range s.entries {
		if s.removeEntryLocked(cacheKey) {
			removed++
		}
	}
	return removed
}

// Len returns the current number of entries and reservations.
func (c *LocalCache) Len() int {
	n := 0
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.RLock()
		n += len(s.entries)
		s.mu.RUnlock()
	}
	return n
}

// MemoryUsage returns the cache's estimated memory usage in bytes.
func (c *LocalCache) MemoryUsage() int64 {
	var total int64
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.RLock()
		total += s.usedBytes
		s.mu.RUnlock()
	}
	return total
}

func (s *cacheShard) setEntryLocked(entry *cacheEntry) {
	if old, exists := s.entries[entry.cacheKey]; exists {
		s.removeEntryLocked(old.cacheKey)
	}

	s.entries[entry.cacheKey] = entry
	s.usedBytes += entry.sizeBytes

	for _, redisKey := range entry.redisKeys {
		cacheKeys := s.byRedisKey[redisKey]
		if cacheKeys == nil {
			cacheKeys = make(map[string]struct{})
			s.byRedisKey[redisKey] = cacheKeys
		}
		cacheKeys[entry.cacheKey] = struct{}{}
	}
}

func (s *cacheShard) removeEntryLocked(cacheKey string) bool {
	entry, exists := s.entries[cacheKey]
	if !exists {
		return false
	}

	delete(s.entries, cacheKey)
	s.usedBytes -= entry.sizeBytes
	if s.usedBytes < 0 {
		s.usedBytes = 0
	}

	for _, redisKey := range entry.redisKeys {
		cacheKeys := s.byRedisKey[redisKey]
		if cacheKeys == nil {
			continue
		}
		delete(cacheKeys, cacheKey)
		if len(cacheKeys) == 0 {
			delete(s.byRedisKey, redisKey)
		}
	}

	if entry.ownerConnID != 0 {
		if cacheKeys := s.byConnID[entry.ownerConnID]; cacheKeys != nil {
			delete(cacheKeys, cacheKey)
			if len(cacheKeys) == 0 {
				delete(s.byConnID, entry.ownerConnID)
			}
		}
	}

	s.closeWaitersLocked(entry)
	return true
}

func (s *cacheShard) closeWaitersLocked(entry *cacheEntry) {
	if entry.waitCh != nil && !entry.waitClosed {
		close(entry.waitCh)
		entry.waitClosed = true
	}
}

// nearCapacityLocked reports whether this shard is close enough to its cap
// that an eviction could plausibly happen soon.
//
// The second-chance bit only ever matters as an eviction input. A shard well
// below its cap will not evict, so marking reads there is pure cost; the
// measurement is in cacheShard.get, at the call site. The threshold is
// deliberately loose (three quarters) so the bit is already being maintained
// by the time eviction actually starts choosing victims.
//
// The trade: a read made while the shard is below the threshold leaves no
// trace. When a gradually warming shard later reaches its cap, an entry read
// only during that warm-up looks as cold as one never read, so the first
// evictions go by insertion order among them and a key hot early on can take
// one avoidable miss. Recovering those reads would need the per-read store
// this gate exists to skip (9.7% of CPU under churn, measured at the call
// site); reads from the threshold on are tracked as usual.
func (s *cacheShard) nearCapacityLocked() bool {
	if s.maxEntries > 0 && len(s.entries)*4 >= s.maxEntries*3 {
		return true
	}
	if s.maxMemoryBytes > 0 && s.usedBytes*4 >= s.maxMemoryBytes*3 {
		return true
	}
	return false
}

func (s *cacheShard) overCapacityLocked() bool {
	if s.maxEntries > 0 && len(s.entries) > s.maxEntries {
		return true
	}
	if s.maxMemoryBytes > 0 && s.usedBytes > s.maxMemoryBytes {
		return true
	}
	return false
}

// evictIfNeededLocked evicts by approximate LRU (O(N) scan; rare in
// well-sized caches) until under capacity. Used by Set/Fulfill: it prefers a
// Valid victim but falls back to the oldest IN_PROGRESS placeholder to keep the
// hard cap (that placeholder's Fulfill then fails and its waiters refetch).
//
// keep, when non-nil, is never chosen: it is the entry the caller just
// published and must not evict in the same pass.
func (s *cacheShard) evictIfNeededLocked(keep *cacheEntry) {
	for s.overCapacityLocked() {
		victim := s.oldestLocked(cacheEntryValid, keep)
		if victim == nil {
			victim = s.oldestLocked(cacheEntryInProgress, keep)
		}
		if victim == nil {
			return
		}
		s.removeEntryLocked(victim.cacheKey)
	}
}

// evictValidLocked evicts only Valid entries until under capacity. Unlike
// evictIfNeededLocked it never evicts a placeholder, so Reserve can't abort a
// peer's in-flight fetch.
func (s *cacheShard) evictValidLocked() {
	for s.overCapacityLocked() {
		victim := s.oldestLocked(cacheEntryValid, nil)
		if victim == nil {
			return
		}
		s.removeEntryLocked(victim.cacheKey)
	}
}

// oldestLocked returns the eviction victim in the given state, or nil when no
// entry is in that state. keep, when non-nil, is skipped entirely: it is
// neither a victim nor part of the sweep.
//
// Second chance. A read no longer stamps a recency token (that store was the
// dominant cost of a cache hit; see cacheEntry.readSinceSweep), so recency is
// carried by the readSinceSweep bit and ordering by lastAccessNs, which is the
// token assigned when the entry was created, reserved or fulfilled.
//
// The victim is the oldest entry whose bit is CLEAR — never read since the last
// sweep. When every candidate has been read, that is the sweep: clear all their
// bits, give them a fresh chance, and fall back to the oldest by token. So a
// read protects an entry from exactly one eviction pass, which is what
// approximate LRU asks for.
//
// Ordering by lastAccessNs rather than by map order is what keeps the choice
// DETERMINISTIC. Go randomises map iteration, so picking "any entry with a
// clear bit" would evict a different key run to run, which callers (and the
// LRU tests) reasonably do not expect.
func (s *cacheShard) oldestLocked(state cacheEntryState, keep *cacheEntry) *cacheEntry {
	var victim, fallback *cacheEntry
	var oldestNs, oldestAny int64 = math.MaxInt64, math.MaxInt64
	swept := false
	for _, e := range s.entries {
		if e.state != state || e == keep {
			continue
		}
		ns := e.lastAccessNs.Load()
		if ns < oldestAny {
			oldestAny, fallback = ns, e
		}
		if e.readSinceSweep.Load() {
			swept = true
			continue
		}
		if ns < oldestNs {
			oldestNs, victim = ns, e
		}
	}
	if victim != nil {
		return victim
	}
	if swept {
		// Every candidate had been read: consume their second chances so the
		// next pass can distinguish them again.
		for _, e := range s.entries {
			if e.state == state && e != keep {
				e.readSinceSweep.Store(false)
			}
		}
	}
	return fallback
}

func cloneBytes(src []byte) []byte {
	if src == nil {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

func cloneStrings(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}
