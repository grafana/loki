// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package session

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
)

// Default eviction parameters for TableCache. Vars (not consts) so
// tests can tighten the sweep interval and TTL to milliseconds
// without touching the code under test.
var (
	DefaultTableCacheTTL           = 1 * time.Hour
	DefaultTableCacheSweepInterval = 10 * time.Minute
)

// TableHandle wraps a TableAPI with a back-reference to its cache and
// an atomically-updated last-access timestamp. It IS the cache entry —
// one type does both jobs, so ReadRow / MutateRow implicitly touch
// the entry without requiring the caller (TableShim) to know about
// the cache.
//
// Close() runs the eviction: removes the handle from the cache map
// AND calls the underlying api.Close(), both guarded by closeOnce so
// double-close from any combination of caller + sweeper is safe. The
// underlying Close error is memoized in closeErr and returned by
// every subsequent Close() call — that keeps the observable behavior
// stable for callers that treat Close as a query.
//
// Underlying Close: TableAPI.Close (sessionTable.Close in table.go)
// releases the per-resource read + write pool entries from
// sessionClient.sessionPools. Cache eviction — TTL sweep, explicit
// handle Close, or cache-wide shutdown — therefore actually reclaims
// the session pools for the resource. Safety of per-handle teardown
// depends on this cache's at-most-one-handle-per-key invariant, which
// acts as an implicit refcount of size 1; see sessionTable.Close's
// doc.
type TableHandle struct {
	api TableAPI
	// openFn is the factory captured at cache-miss insertion time
	// (table_cache.go's GetOrOpen, driven from bigtable/open.go via
	// c.sessionImpl.OpenTable / OpenAuthorizedView / OpenMaterializedView).
	// Invoked ONLY through cache.GetOrOpen, which nil-guards on the
	// closed cache; do not call this field directly or the post-close
	// eviction guard is lost. Safe to hold for the process lifetime:
	// *bigtable.Client.sessionImpl is set-once at NewClient and never
	// reassigned, so the closure never goes stale. If that ever
	// becomes reconfigurable, self-heal will stale — revisit here.
	//
	// Invariant: openFn is the one captured at first insertion for the
	// key. resolveSuccessor threads it through unchanged into any
	// successor GetOrOpen call, so all handles that ever share the key
	// share the same closure. Threading a per-call override into
	// GetOrOpen without also updating this invariant would silently
	// leave successors using the original openFn.
	openFn         func() TableAPI
	key            string
	cache          *TableCache
	lastAccessNano atomic.Int64
	// evicted flips to true inside Close() BEFORE api.Close runs. The
	// happy-path cost is one atomic Load per RPC — the cache-map lookup
	// only happens when a reader observes evicted=true. Semantics:
	//
	//   - A reader whose Load observes evicted=true is guaranteed to
	//     see the flag before api.Close begins tearing down, and routes
	//     through dispatch() → resolveSuccessor to find the live handle.
	//     THIS is the fix — TableShim's cached pointer stops going
	//     stale across the many subsequent RPCs that follow eviction.
	//   - A reader whose Load observes evicted=false and then races a
	//     concurrent Close still hits the pre-existing Load-false
	//     TOCTOU on that single RPC — self-heal is a next-RPC recovery,
	//     not a same-RPC one. The Load-false-then-preempt-then-dispatch
	//     window can span arbitrary wall-clock under scheduler pressure;
	//     do not assume it's tight.
	evicted atomic.Bool
	// successor caches this handle's resolved live successor after the
	// first resolveSuccessor call. Subsequent dispatch() calls that
	// observe evicted=true then load a non-nil successor and follow it
	// directly — no cache-map lookup, no cache.mu acquisition. Under
	// TableShim's process-lifetime pointer-caching pattern this matters:
	// without it, EVERY RPC on a stale handle would pay 2× cache.mu
	// (removeEntry + GetOrOpen) forever until process restart. With it,
	// only the FIRST post-eviction RPC pays that cost; all subsequent
	// ones are one extra atomic.Load. dispatch() also applies path
	// compression on the return so a chain of evicted handles collapses
	// on the next observation.
	//
	// Store races are safe: two concurrent dispatches that both call
	// resolveSuccessor race into GetOrOpen, which is single-flight per
	// key (one loads, the other blocks and observes the same handle), so
	// both racers see the same `next` — the Store is idempotent. A
	// divergent-`next` race (successor itself gets evicted between one
	// dispatch's resolveSuccessor and another's Store) writes a stale
	// successor pointer; the next dispatch's outer for loop walks one
	// extra hop through it and updates. No correctness bug, just a
	// lagging compression.
	successor atomic.Pointer[TableHandle]
	closeOnce sync.Once
	closeErr  error
}

// Unwrap returns the underlying TableAPI. Intended for test
// inspection — production callers should use ReadRow / MutateRow so
// dispatch()'s self-heal path stays in effect.
func (h *TableHandle) Unwrap() TableAPI { return h.api }

func (h *TableHandle) touch() {
	h.lastAccessNano.Store(h.cache.now().UnixNano())
}

// dispatch returns the live handle that should service this RPC.
//
//   - Happy path (evicted=false): returns h. One atomic Load, no cache
//     map lookup.
//   - Eviction path: consults cur.successor first (one more atomic
//     Load) — the cached pointer set by an earlier post-eviction
//     dispatch. Only when the cache is cold does it fall back to
//     resolveSuccessor, which does the cache-map work. Loops so an
//     aggressive-TTL test where the fresh handle gets evicted mid-
//     flight can still resolve to a live one instead of recursing.
//     Falls through to cur when the cache is closed / openFn refuses
//     so the caller sees the honest terminal-state error from
//     cur.api rather than a hang.
//   - Path compression: on return, stores the final live handle onto
//     the original h so the next dispatch call skips the intermediate
//     evicted handles (union-find with path compression). Intermediate
//     handles become unreachable from h and are eligible for GC.
//
// Callers dispatch onto the returned handle and pay the touch() there,
// so ReadRow / MutateRow avoid the wasted touch on the doomed original.
func (h *TableHandle) dispatch() *TableHandle {
	cur := h
	for cur.evicted.Load() {
		next := cur.successor.Load()
		if next == nil {
			next = cur.resolveSuccessor()
			if next == nil {
				return cur // reopen refused; let cur.api surface the terminal error
			}
			cur.successor.Store(next)
		}
		cur = next
	}
	if cur != h {
		h.successor.Store(cur)
	}
	return cur
}

// ReadRow proxies to the underlying TableAPI on the atomic-Load fast
// path, self-healing via dispatch() on eviction. Happy path adds one
// atomic Load vs a bare api.ReadRow; the cache-map lookup only fires
// on the recovery branch inside dispatch().
func (h *TableHandle) ReadRow(ctx context.Context, req *btpb.SessionReadRowRequest) (*btpb.SessionReadRowResponse, error) {
	d := h.dispatch()
	d.touch()
	return d.api.ReadRow(ctx, req)
}

// MutateRow proxies to the underlying TableAPI, same shape as ReadRow.
// Kept minimal so any future proxied method (BulkMutate, SampleRowKeys,
// etc.) drops in as a 3-line pass-through without duplicating the
// self-heal conditional.
func (h *TableHandle) MutateRow(ctx context.Context, req *btpb.SessionMutateRowRequest) (*btpb.SessionMutateRowResponse, error) {
	d := h.dispatch()
	d.touch()
	return d.api.MutateRow(ctx, req)
}

// resolveSuccessor returns the currently-live handle registered under
// h.key, minting a fresh one via openFn on cache miss. Returns nil when
// the cache is closed or openFn refused. Called exclusively from
// dispatch() on the evicted branch.
//
// The removeEntry call covers the interleaving where Close.Do fired
// evicted.Store(true) but has not yet reached its own removeEntry (h is
// still installed in the map). Without it, our subsequent GetOrOpen
// fast-path would return h itself and dispatch would loop forever. On
// the sweeper path (where sweepOnce already removed h before firing
// Close), this is a harmless no-op — removeEntry is identity-scoped, so
// it won't clobber a peer's fresh install either.
func (h *TableHandle) resolveSuccessor() *TableHandle {
	h.cache.removeEntry(h.key, h)
	api := h.cache.GetOrOpen(h.key, h.openFn)
	if api == nil {
		return nil
	}
	// Type assertion narrows the interface back to the concrete cache
	// entry. Guaranteed non-nil-and-not-h by GetOrOpen's insertion path
	// (the only construction site is inside GetOrOpen's slow path,
	// which mints a fresh *TableHandle) — the assertion is a defensive
	// check against a future GetOrOpen contributor changing what the
	// cache stores. If that ever fires, dispatch() falls through to
	// h.api, which will surface the closed-api error.
	fresh, _ := api.(*TableHandle)
	return fresh
}

// Close evicts the handle from its cache and Close()s the underlying
// TableAPI. Fully idempotent: subsequent calls return the error
// captured on the first call without invoking api.Close again. Safe
// to call from multiple paths (explicit caller Close, TTL sweep,
// cache-wide shutdown) concurrently.
//
// The three steps run in strict order inside closeOnce.Do:
//
//  1. evicted.Store(true) — readers observing true after this point
//     route through dispatch() → resolveSuccessor instead of taking
//     the fast path onto the doomed api.
//  2. cache.removeEntry — future GetOrOpen callers stop finding this
//     handle in the map, so cache misses fall through to openFn and
//     mint a fresh live successor.
//  3. api.Close — actually tears down the underlying pool.
//
// The (1) → (3) ordering is the load-bearing invariant for self-heal.
// (2) between them just tightens the window in which a GetOrOpen
// fast-path could still return this now-evicted handle.
func (h *TableHandle) Close() error {
	h.closeOnce.Do(func() {
		h.evicted.Store(true)
		h.cache.removeEntry(h.key, h)
		h.closeErr = h.api.Close()
	})
	return h.closeErr
}

// TableCache holds per-resource TableHandles with TTL-on-idle
// eviction. Zero size cap — cardinality is naturally bounded by the
// caller's Open* pattern.
//
// The cache is opener-agnostic: GetOrOpen takes an openFn per call
// so a single cache can back tables, authorized views, and
// materialized views without needing to encode the resource kind
// in the key or dispatch on a prefix. Keys are opaque strings; the
// consumer (bigtable.Client) uses the fully-qualified resource name
// (projects/P/instances/I/tables/T, etc.) so the cache key is the
// same identity Cloud Bigtable uses over the wire.
type TableCache struct {
	ttl           time.Duration
	sweepInterval time.Duration

	mu      sync.Mutex
	entries map[string]*TableHandle
	// loading tracks the in-flight openFn call per key so GetOrOpen is
	// single-flight: the first miss for a key installs a loadState and
	// runs openFn; concurrent callers for the same key block on the
	// loadState's ready channel and re-check the map once it closes,
	// finding the winner's handle instead of each dialing their own
	// pool. With no losers, a storm of post-eviction RPCs mints exactly
	// one successor pool rather than N-1 throwaways.
	loading map[string]*loadState
	// closed is flipped by Close() under mu. GetOrOpen's slow path
	// re-checks it before inserting the freshly-opened handle so a
	// slow-path insert straddling Close() can't orphan a live
	// underlying api (which would leak its per-resource session pool
	// once sessionTable.Close does real teardown).
	closed bool

	stopOnce sync.Once
	stop     chan struct{}
	sweeperG sync.WaitGroup

	// now is a func for tests to inject synthetic time. Defaults to
	// time.Now when nil is passed to NewTableCache.
	now func() time.Time
}

// loadState is the single-flight token for one in-flight openFn call.
// The loader installs it in TableCache.loading under mu, runs openFn
// outside the lock, then deletes it and closes ready. Waiters for the
// same key block on ready and retry the cache lookup.
type loadState struct {
	ready chan struct{}
}

// NewTableCache constructs a cache and starts its background sweeper.
// Production callers pass nil for now (→ time.Now); tests inject a
// controllable clock.
func NewTableCache(ttl, sweepInterval time.Duration, now func() time.Time) *TableCache {
	if now == nil {
		now = time.Now
	}
	c := &TableCache{
		ttl:           ttl,
		sweepInterval: sweepInterval,
		entries:       make(map[string]*TableHandle),
		loading:       make(map[string]*loadState),
		stop:          make(chan struct{}),
		now:           now,
	}
	c.sweeperG.Add(1)
	go c.sweeperLoop()
	return c
}

// GetOrOpen returns the cached handle for key, opening a fresh one
// via openFn on cache miss. Returns nil when openFn returns nil or the
// cache is closed.
//
// Single-flight: at most one openFn runs per key at a time. The first
// caller to miss becomes the loader — it installs a loadState, runs
// openFn OUTSIDE the cache mutex (safe for open paths that take their
// own locks — avoids lock inversion), then installs the handle. Any
// concurrent caller for the same key finds the loadState and blocks on
// its ready channel, then loops to re-check the map — it never dials a
// throwaway pool. A post-eviction RPC storm therefore mints exactly one
// successor pool rather than dialing N and discarding N-1.
func (c *TableCache) GetOrOpen(key string, openFn func() TableAPI) TableAPI {
	if c == nil {
		return nil
	}

	for {
		c.mu.Lock()
		// Fast path — cache hit.
		if h, ok := c.entries[key]; ok {
			c.mu.Unlock()
			h.touch()
			return h
		}
		if c.closed {
			c.mu.Unlock()
			return nil
		}
		// Another caller is already loading this key. Release the lock,
		// wait for it to finish, then loop to re-check: it either
		// installed a handle (fast-path hit next iteration) or failed
		// (openFn returned nil / cache closed), in which case we become
		// the next loader.
		if ls, ok := c.loading[key]; ok {
			c.mu.Unlock()
			<-ls.ready
			continue
		}
		// Become the loader for this key.
		ls := &loadState{ready: make(chan struct{})}
		c.loading[key] = ls
		c.mu.Unlock()

		// Clear the loading slot and wake waiters in a defer so a panic in
		// openFn (deep in buildLazyOpener → createSessionPoolForPayload →
		// pool construction) can't leave the slot wedged forever — that
		// would block every subsequent GetOrOpen(key), and since every RPC
		// routes through here, wedge the process on that resource. The
		// loader path always returns in this iteration, so the defer fires
		// exactly once. Waiters re-check entries after ready closes, so the
		// handle (happy path) is already installed by the time they wake.
		defer func() {
			c.mu.Lock()
			delete(c.loading, key)
			c.mu.Unlock()
			close(ls.ready)
		}()

		api := openFn()

		c.mu.Lock()
		if api == nil {
			c.mu.Unlock()
			return nil
		}
		if c.closed {
			// Close() ran between the slow-path openFn and our re-lock.
			// If we inserted this handle now, nothing would ever call
			// its Close (sweeper stopped, Close() already snapshotted an
			// empty map, cache-Close is stopOnce-guarded). Release the
			// freshly-opened api ourselves and return nil so the caller
			// falls back to classic (TableShim treats nil session-side
			// as classic-only).
			c.mu.Unlock()
			_ = api.Close()
			return nil
		}
		// Single-flight guarantees no concurrent loader installed a
		// handle for this key while we held the loading slot, so this is
		// always a fresh install.
		h := &TableHandle{api: api, openFn: openFn, key: key, cache: c}
		h.lastAccessNano.Store(c.now().UnixNano())
		c.entries[key] = h
		c.mu.Unlock()
		return h
	}
}

// removeEntry deletes key from the map iff the current entry is
// exactly h. Guards against the race where Close and a concurrent
// Open both raced: Close should not evict the freshly-inserted
// handle installed by the new Open.
func (c *TableCache) removeEntry(key string, h *TableHandle) {
	c.mu.Lock()
	if c.entries[key] == h {
		delete(c.entries, key)
	}
	c.mu.Unlock()
}

// sweeperLoop walks the cache every sweepInterval and evicts entries
// idle for > ttl. Exits when Close() signals stop.
func (c *TableCache) sweeperLoop() {
	defer c.sweeperG.Done()
	t := time.NewTicker(c.sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.sweepOnce()
		}
	}
}

// sweepOnce snapshots the currently-idle handles under the mutex,
// releases the mutex, then Close()s each one outside the lock.
// Handle.Close is idempotent (closeOnce) and self-removes from the
// map, so a concurrent Open on the same key mid-sweep sees either
// (a) the old handle before we snapshot — its Close will no-op the
// removeEntry because we already deleted, or (b) a fresh one after
// we deleted — protected by the identity check in removeEntry.
//
// Sweep is O(N) over the cache map. Fine for this cache: entry count
// is bounded by the caller's Open* pattern (typically tens, not
// millions), sweep runs every 10 minutes (default), and the cache
// isn't on the RPC hot path. If cardinality ever grows to matter, a
// time-ordered heap keyed by lastAccess would trim this to O(k) per
// sweep where k = evicted count.
func (c *TableCache) sweepOnce() {
	cutoff := c.now().Add(-c.ttl).UnixNano()

	c.mu.Lock()
	var evicted []*TableHandle
	for k, h := range c.entries {
		if h.lastAccessNano.Load() < cutoff {
			evicted = append(evicted, h)
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()

	for _, h := range evicted {
		_ = h.Close()
	}
}

// Close stops the sweeper, waits for it to exit, then Close()s every
// remaining handle. Safe to call multiple times.
func (c *TableCache) Close() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		close(c.stop)
	})
	c.sweeperG.Wait()

	c.mu.Lock()
	c.closed = true
	remaining := make([]*TableHandle, 0, len(c.entries))
	for k, h := range c.entries {
		remaining = append(remaining, h)
		delete(c.entries, k)
	}
	c.mu.Unlock()

	for _, h := range remaining {
		_ = h.Close()
	}
}
