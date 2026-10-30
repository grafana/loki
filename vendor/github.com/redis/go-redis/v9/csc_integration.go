package redis

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/pool"
	"github.com/redis/go-redis/v9/internal/proto"
	"github.com/redis/go-redis/v9/internal/util"
	"github.com/redis/go-redis/v9/push"
)

// cscRegisterCleanups arranges for a client dropped without Close to stop its
// background CSC drainer. The drainer's exit path revokes its pool's cache
// coverage; the runtime cleanup itself stays non-blocking and never captures
// *Client, so the wrapper remains collectible.
func cscRegisterCleanups(c *Client) {
	h := c.baseClient.cscDrainHandle
	if h == nil {
		return
	}
	// The weak back-reference (cscClientWeak) is published by the caller BEFORE
	// attachCSC starts the drainer (see NewClient), so the push-handler adapter's
	// canonical close reads it race-free. It is deliberately NOT set here: a write
	// after the drainer is already live would race that read.
	// Capture cscActive (a standalone *atomic.Bool, not *Client) so the cleanup
	// also stops clones from serving once the drainer is gone. Capture the miss
	// coalescer too (set during attach; nil when off): its sessions wait on
	// their own stop channel, so a client dropped WITHOUT Close would leak them
	// and everything they retain (cache, pools). stopWorkers is idempotent and
	// signal-only, keeping the cleanup non-blocking; the drain-cancel then
	// settles any queued request whose reservation would otherwise stay
	// IN_PROGRESS and block a cache-sharing client until StaleTimeout (channel
	// receive gives each request exactly one consumer).
	active := c.baseClient.cscActive
	mc := c.baseClient.cscMissCoalescer.Load()
	// Capture the refresh handle too (set during attach; nil when refresh is off):
	// runCSCRefresher parks on its ticker/queue and holds *baseClient, so a client
	// dropped WITHOUT Close would leak the refresher goroutine and everything it
	// retains (cache, pools) — the coalescer/drainer stops below would not reach it.
	// signalStop is idempotent and non-blocking, matching the other stops here. The
	// handle is channels only (no *Client), so capturing it keeps the wrapper
	// collectible.
	rh := c.baseClient.cscRefreshHandle
	// Capture this client's refresh queue too: signalStop stops the refresher
	// goroutine, but with a SHARED cache+processor the invalidate handler still
	// holds this queue as (possibly) the active refresh binding. If a sibling
	// client survives, invalidations would keep feeding a stopped queue and the
	// shared cache's refresh-on-invalidate would silently degrade to plain
	// eviction. clearRefreshQueue unbinds it and restores the sibling's binding,
	// mirroring stopCSCRefresher on the clean Close path. A queue is only ever
	// created inside attachSharedTrackingCSC, which also builds the drain handle
	// (startBackgroundDrainer), so h.invalidateHandler is set whenever q is
	// non-nil; a Conn() clone bails before startCSCRefresher and never has one.
	q := c.baseClient.cscRefreshQueue
	runtime.AddCleanup(c, func(h *cscDrainHandle) {
		// Order mirrors stopBackgroundDrainer (the clean Close path) but stays
		// fully NON-BLOCKING — a GC finalizer must never wait. Unbind this queue and
		// signal the refresher stop while serving is STILL ON (active==true), then
		// deactivate: refreshInvalidatedBatch bails once cscActive is false, so
		// deactivating first would make the refresher's stop-drain flush a no-op and
		// drop in-window hot keys for a surviving sibling on a shared cache.
		// signalStop only closes a channel, so the final flush runs ASYNCHRONOUSLY;
		// active.Store(false) lands nanoseconds later and the flush may still miss
		// the window — the guaranteed final-flush is the Close path, which joins the
		// refresher before deactivating. This reorder is the free, strictly-better
		// best effort for the drop-without-Close case.
		//
		// clearRefreshQueue detaches+signals the batcher but the finalizer DISCARDS
		// it (no join): the batcher's own run() drains and exits asynchronously. The
		// clean Close/self-disable paths join it instead.
		//
		// This deliberately DIVERGES from stopCSCRefresherAndCoalescer's canonical
		// order (coalescer-conn-release before the refresher flush): the finalizer is
		// signal-only and never joins, so it never waits on a refresher flush and the
		// connection-release ordering is moot here — the pool may already be gone. The
		// blocking, correctly-ordered teardown is the Close / self-disable path.
		if q != nil && h.invalidateHandler != nil {
			h.invalidateHandler.clearRefreshQueue(q)
		}
		if rh != nil {
			rh.signalStop()
		}
		if active != nil {
			active.Store(false)
		}
		if mc != nil {
			mc.stopWorkers()
			// Retry-uncached, matching every other stop path: a clone still
			// alive re-runs the read on the (possibly still open) pool instead
			// of surfacing a spurious ErrClosed.
			mc.drainQueueErr(errCSCRetryUncached)
		}
		h.signalStop()
	}, h)
}

// ClientSideCacheConfig configures the built-in client-side cache. Pass a
// non-nil value to Options.ClientSideCacheConfig to enable caching on a RESP3
// client.
//
// Experimental: this API may change in a minor release.
type ClientSideCacheConfig = CacheConfig

const (
	invalidatePushName = "invalidate"
	// cscNamespaceSep separates fixed-width/logically-delimited namespace parts
	// from the command or Redis key.
	cscNamespaceSep = "\x00"
)

// cscNamespacePrefix scopes a shared cache by database and fixed ACL identity.
// Password rotation does not change identity; provider-backed identities are
// rejected before attachment.
func cscNamespacePrefix(db int, username string) string {
	return strconv.Itoa(db) + cscNamespaceSep +
		strconv.Itoa(len(username)) + ":" + username + cscNamespaceSep
}

func cscNamespacedKey(prefix, key string) string {
	return prefix + key
}

// cscMissKeys returns the namespaced redis keys in args [lo, hi] for Reserve.
// processCached has already checked the span with cscKeySpan and
// cscKeysRenderable.
//
// This is called on the MISS path only. It used to run before the cache-hit
// check, which meant a hit -- the 94-99% case -- allocated a raw key slice,
// this slice, and one string per key, then discarded all three.
func cscMissKeys(cmd Cmder, keyPrefix string, lo, hi int) []string {
	ns := make([]string, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		ns = append(ns, cscNamespacedKey(keyPrefix, cmd.stringArg(i)))
	}
	return ns
}

// invalidateHandler propagates RESP3 "invalidate" push notifications into the
// shared client-side cache. keyPrefix scopes incoming key names so a shared
// cache cannot collide across databases or fixed ACL identities.
//
// The binding (cache, keyPrefix) is mutable under mu: the owning client's teardown
// RELEASES it (cache=nil) instead of unregistering the handler, so the handler
// can stay registered protected — application code holding the processor
// cannot silently unregister invalidation out from under a live client — while
// a successor client on the same processor can still rebind it (see
// registerInvalidateHandler).
type invalidateHandler struct {
	mu        sync.RWMutex
	cache     Cache
	keyPrefix string
	users     int

	// refresh, when set, receives evicted-but-hot entries for immediate refetch.
	// Feeding it must never block the invalidation-delivery path. It is the TOP
	// of refreshStack: clients sharing this handler each attach their own queue
	// and the newest attachment is active; when it clears, the next-newest live
	// binding is RESTORED (closing the newest owner must not sever an older
	// sibling's still-running refresher).
	refresh *cscRefreshQueue

	// refreshStack holds every live refresh binding in attach order (older
	// first). Guarded by mu. Small: one entry per client sharing the handler.
	refreshStack []*cscRefreshQueue

	// batcher offloads invalidation cache-deletes to a windowed background
	// goroutine (Options.ClientSideCacheInvalidationBatchWindow). Lazily started
	// (ensureBatcher) and nil when disabled; guarded by mu. Stopped and cleared
	// when the last user releases (releaseLocked), so its goroutine does not live
	// past the binding re-arming its timer forever; a later re-acquire starts a
	// fresh one (picking up the successor's window).
	batcher *cscInvalBatcher

	// invalBatchWindow is the EFFECTIVE coalescing window for the batcher above:
	// the strictest window folded in across attached clients (see
	// setInvalBatchWindow — 0/inline strictest, then smaller nonzero). 0
	// (default) deletes inline. invalBatchWindowSet distinguishes "no client has
	// attached a window yet" from an explicit 0 (inline) that must win over any
	// later nonzero window. Read under mu alongside cache/keyPrefix/refresh.
	invalBatchWindow    time.Duration
	invalBatchWindowSet bool
}

// setInvalBatchWindow folds one client's window into the shared handler's
// effective window at attach time. Clients sharing a handler may configure
// different windows but the handler runs ONE batcher, so the effective window
// is the STRICTEST attached: smallest nonzero, with explicit zero (inline
// deletes) strictest of all — tightening can never violate a client's staleness
// bound, while taking the latest as-is could loosen an earlier stricter one.
// Not re-loosened on close (bindings carry no identity; staying stricter costs
// only efficiency). On a tighten the running batcher (window fixed at creation)
// is stopped — stop() flushes, so no queued delete is lost — and the next
// invalidation rebuilds via ensureBatcher.
func (h *invalidateHandler) setInvalBatchWindow(w time.Duration) {
	h.mu.Lock()
	if h.invalBatchWindowSet {
		cur := h.invalBatchWindow
		// Strictness: 0 (inline) is strictest; among nonzero, smaller is stricter.
		stricter := (w == 0 && cur != 0) || (w != 0 && cur != 0 && w < cur)
		if !stricter {
			h.mu.Unlock()
			return
		}
	}
	h.invalBatchWindowSet = true
	h.invalBatchWindow = w
	// On a tighten drop the running batcher (window fixed at creation): the next
	// invalidation rebuilds under the NEW (stricter) window. Detach+stop under the
	// lock, then join OUTSIDE it. Joining under h.mu would stall a sibling in
	// ensureBatcher/HandlePushNotification; the join keeps the old worker's
	// stop-drain from applying after the rebuild — a late apply under the looser
	// contract could evict an entry the stricter-window batcher just repopulated.
	b := h.detachBatcherLocked()
	h.mu.Unlock()
	if b != nil {
		b.join()
	}
}

// detachBatcherLocked removes the running batcher from the handler and SIGNALS it
// to stop, under h.mu; returns the detached batcher (nil when none). It never
// JOINS: join() waits on the batcher's stop-drain, which must not run while h.mu is
// held (it would stall a sibling on the hot path — ensureBatcher/HandlePushNotification)
// and must never block the GC finalizer. Callers needing a SYNCHRONOUS teardown
// (the Close path) join the returned batcher AFTER releasing h.mu; the GC finalizer
// discards it (signal-only). A repointing caller (set/clearRefreshQueue) stores the
// new refresh binding on the batcher BEFORE calling this, so the stop-drain offers
// hot keys to the surviving refresher.
func (h *invalidateHandler) detachBatcherLocked() *cscInvalBatcher {
	b := h.batcher
	if b == nil {
		return nil
	}
	h.batcher = nil
	b.stop()
	return b
}

// ensureBatcher lazily starts the windowed invalidation batcher. The common
// case (already started) is a shared RLock; only first-start takes the write
// lock, so the hot invalidation path stays cheap.
func (h *invalidateHandler) ensureBatcher() *cscInvalBatcher {
	h.mu.RLock()
	b := h.batcher
	h.mu.RUnlock()
	if b != nil {
		return b
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// Do not start a batcher for a released binding. releaseLocked stops+nils the
	// batcher under this same lock when users hits 0, so a push racing that last
	// release must NOT resurrect a goroutine that nothing would ever stop (once
	// users is 0, release() no longer runs). The caller falls back to the inline
	// delete path when this returns nil.
	if h.users == 0 {
		return nil
	}
	// Read the window under this lock (not a caller snapshot a concurrent
	// tighten could supersede). 0 = batching off: return nil, caller deletes
	// inline.
	w := h.invalBatchWindow
	if w <= 0 {
		return nil
	}
	if h.batcher == nil {
		h.batcher = &cscInvalBatcher{
			window: w,
			// Snapshot the cache for the batcher's lifetime (⊂ the binding's:
			// releaseLocked stops it before clearing it) so the release-time
			// stop-drain still applies queued deletes after h.cache is nilled.
			cache:  h.cache,
			ch:     make(chan cscInvalItem, 8192),
			wake:   make(chan struct{}, 1),
			stopCh: make(chan struct{}),
			done:   make(chan struct{}),
		}
		// refresh is atomic so set/clearRefreshQueue can repoint it before stop;
		// seed it with the current binding before the worker starts.
		h.batcher.refresh.Store(h.refresh)
		go h.batcher.run()
	}
	return h.batcher
}

// clearRefreshQueue clears the handler's refresh binding ONLY while it still
// points at q: two clients sharing one cache and push processor each attach
// their own queue (last attach wins), and a closing client must not clobber
// the binding of a live sibling that re-attached after it — invalidations
// would keep deleting entries but silently stop feeding the survivor's
// refresher.
//
// It returns the detached batcher (nil when none) so the caller can join() it
// OUTSIDE h.mu for a synchronous teardown; the GC finalizer discards it (must not
// block). See detachBatcherLocked.
func (h *invalidateHandler) clearRefreshQueue(q *cscRefreshQueue) *cscInvalBatcher {
	if q == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, b := range h.refreshStack {
		if b == q {
			h.refreshStack = append(h.refreshStack[:i], h.refreshStack[i+1:]...)
			break
		}
	}
	if h.refresh != q {
		return nil // a newer sibling owns the active binding; nothing else changes
	}
	// Restore the next-newest live binding (nil when none): the surviving
	// sibling's refresher keeps getting fed after the newest owner closes.
	h.refresh = nil
	if n := len(h.refreshStack); n > 0 {
		h.refresh = h.refreshStack[n-1]
	}
	// Repoint the batcher at the surviving binding (nil if none) BEFORE stopping it,
	// so the stop-drain offers evicted-hot keys to the live survivor's refresher, not
	// the closing owner's — whose drainer is already gone, so its offers would just be
	// dropped. An in-flight apply already holding the old pointer feeds its current
	// batch to the old queue: a bounded, benign residual.
	if h.batcher != nil {
		h.batcher.refresh.Store(h.refresh)
	}
	return h.detachBatcherLocked()
}

// setRefreshQueue binds q as the active refresh queue and returns the detached
// batcher (nil when none) so the caller can join() it OUTSIDE h.mu — the attach
// stays synchronous without holding the join under the handler lock.
func (h *invalidateHandler) setRefreshQueue(q *cscRefreshQueue) *cscInvalBatcher {
	h.mu.Lock()
	defer h.mu.Unlock()
	if q != nil {
		found := false
		for _, b := range h.refreshStack {
			if b == q {
				found = true
				break
			}
		}
		if !found {
			h.refreshStack = append(h.refreshStack, q)
		}
	}
	if h.refresh == q {
		return nil
	}
	h.refresh = q
	// A running batcher was created with the previous binding; drop it so the next
	// invalidation rebuilds with the new one (stop() flushes, so no queued delete
	// is lost). Repoint it at the new binding first, so the stop-drain feeds hot
	// keys to q rather than the superseded refresher.
	if h.batcher != nil {
		h.batcher.refresh.Store(h.refresh)
	}
	return h.detachBatcherLocked()
}

// HandlePushNotification decodes ["invalidate", <keys>] notifications. A nil
// <keys> payload is emitted on FLUSHDB/FLUSHALL and triggers a full cache flush.
func (h *invalidateHandler) HandlePushNotification(
	_ context.Context, _ push.NotificationHandlerContext, notification []interface{},
) error {
	h.mu.RLock()
	cache, keyPrefix, refresh := h.cache, h.keyPrefix, h.refresh
	window := h.invalBatchWindow
	h.mu.RUnlock()
	if cache == nil || len(notification) < 2 {
		return nil
	}

	switch payload := notification[1].(type) {
	case nil:
		// FLUSHDB/FLUSHALL: supersede the batcher's queued per-key deletes, then wipe
		// the snapshotted cache. See fullFlush for the drop/flush ordering and the
		// binding-pairing invariant.
		h.fullFlush(cache)
	case []interface{}:
		// Count incoming invalidations at the choke point: one per key named in the
		// push, BEFORE batching/dedup/spill. Applied deletes are counted separately
		// (DeleteByRedisKey / deleteByRedisKeyCollectingHot) and diverge from this
		// under dedup — that gap is the signal (see CSCRefreshStats).
		if lc, ok := cache.(*LocalCache); ok {
			var n uint64
			for _, k := range payload {
				switch k.(type) {
				case string, []byte:
					n++
				}
			}
			lc.invalidations.Add(n)
		}
		// Offload path: enqueue keys to the windowed background batcher instead of
		// deleting inline, so invalidation work does not steal time from the
		// coalescer's miss-reply reader (the low-concurrency churn p99 tail).
		if window > 0 {
			if _, ok := cache.(*LocalCache); ok {
				// nil when the binding was just released (users==0) or when a
				// concurrent window change turned batching off: fall through to
				// the inline delete path below rather than enqueue on a nil
				// batcher (which would panic).
				// Pair the batcher with the SNAPSHOT cache: after a last-user release +
				// rebind to a different cache (A->B) between the entry snapshot and here,
				// ensureBatcher returns B's batcher, which would delete B's entries for a
				// push meant for A (A would keep serving stale). fullFlush guards the same
				// A->B pairing. sameCache also returns false for a non-comparable cache
				// type, so such caches fall through to the inline delete path (correct,
				// just not batched).
				if b := h.ensureBatcher(); b != nil && sameCache(b.cache, cache) {
					// Snapshot fetch-order ONCE for the whole push (mirrors the inline delete
					// path below and cscInvalItem.fetchSnap): a per-key load inside enqueue
					// would include a fetch reserved AFTER this push was observed but before
					// the loop reached that key, so apply would not treat it as newer and
					// could evict the fresh value / cancel its in-progress reservation.
					fetchSnap := cscFetchSeq.Load()
					for _, k := range payload {
						var name string
						switch v := k.(type) {
						case string:
							name = v
						case []byte:
							name = string(v)
						default:
							continue
						}
						b.enqueueAt(cscNamespacedKey(keyPrefix, name), fetchSnap)
					}
					return nil
				}
			}
		}
		var hot []cscRefreshTarget
		lc, canRefresh := cache.(*LocalCache)
		canRefresh = canRefresh && refresh != nil
		// Snapshot the fetch-order sequence ONCE, at notification-observe time, and reuse
		// it for every key in this push. A live per-key load would include a fetch
		// reserved AFTER this push was observed but before the loop reached that key, so
		// collectHotAndDelete would not treat it as newer and would evict the fresh value
		// / cancel its in-progress reservation (mirrors cscInvalItem.fetchSnap, taken at
		// enqueue; see cacheEntry.fetchSeq).
		fetchSnap := cscFetchSeq.Load()
		for _, k := range payload {
			var name string
			switch v := k.(type) {
			case string:
				name = v
			case []byte:
				name = string(v)
			default:
				continue
			}
			nsKey := cscNamespacedKey(keyPrefix, name)
			if !canRefresh {
				if lc != nil {
					// *LocalCache with refresh OFF: still honor the fetch-order guard
					// (mirrors the batcher's !canRefresh branch), or a delayed invalidation
					// would delete/cancel a miss reserved AFTER this push was observed
					// (fetchSeq > fetchSnap) and wake coalesced waiters as duplicate misses.
					// cscInvalNoHorizon = no refresh horizon; discard collected targets.
					_ = lc.deleteByRedisKeyCollectingHot(nsKey, cscInvalNoHorizon, fetchSnap, nil)
				} else {
					// Non-*LocalCache: no per-entry fetch sequence to compare; plain delete.
					cache.DeleteByRedisKey(nsKey)
				}
				continue
			}
			// Inline (window==0) path: observe and delete synchronously. An entry with a
			// fetchSeq greater than the observe-time snapshot was reserved by a refetch
			// issued after this push and is correctly kept (see cacheEntry.fetchSeq).
			hot = lc.deleteByRedisKeyCollectingHot(nsKey, refresh.sinceToken.Load(), fetchSnap, hot[:0])
			for i := range hot {
				refresh.offer(hot[i])
			}
		}
	}
	return nil
}

// fullFlush wipes the snapshotted cache for a FLUSHDB/FLUSHALL (nil-payload)
// invalidation and supersedes the batcher's queued per-key deletes.
//
// drop() before Flush() bumps the batcher epoch, so anything already enqueued
// (pre-flush, redundant by the flush) is skipped at apply, while an invalidation
// racing in from another tracked connection AFTER this point carries the new epoch
// and still applies — its post-flush delete must not be lost.
//
// Drop the batcher ONLY when it still belongs to the cache being flushed
// (sameCache(h.cache, cache)). A same-cache batcher rebuild (setInvalBatchWindow /
// set/clearRefreshQueue, all under h.mu.Lock) must have its FRESH batcher dropped, or
// the new batcher's queued deletes survive the flush and evict post-flush
// repopulations. But a last-user release + rebind to a DIFFERENT cache (A->B) between
// the caller's entry snapshot and this RLock leaves h.batcher = B's while cache = A;
// dropping B's batcher would bump B's epoch and skip B's queued deletes while only A is
// flushed, so B would serve stale (#3989). The sameCache guard drops the batcher only
// when the binding is unchanged. sameCache (not ==) also avoids a panic when the cache's
// dynamic type is non-comparable; for such a type it returns false, so the batcher is
// not dropped on flush — a bounded spurious miss, correct versus a crash.
//
// Flush the CACHE SNAPSHOT, not the live h.cache: a last-user releaseLocked can nil
// h.cache, and a guarded `if h.cache != nil` would then silently skip the wipe. RLock
// (not Lock) suffices — it blocks the write-locked rebuilds — and drop()/Flush() take
// their own locks, not h.mu, so there is no lock-order cycle.
func (h *invalidateHandler) fullFlush(cache Cache) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if sameCache(h.cache, cache) && h.batcher != nil {
		h.batcher.drop()
	}
	cache.Flush()
}

func (h *invalidateHandler) release() {
	h.mu.Lock()
	var b *cscInvalBatcher
	if h.users > 0 {
		b = h.releaseLocked()
	}
	h.mu.Unlock()
	// Join OUTSIDE h.mu: the batcher's stop-drain must not run under the handler
	// lock (it would stall a sibling on the hot path). release() is reached only from
	// the drainer goroutine's exit (Close or self-disable), never the GC finalizer, so
	// blocking here is fine and gives Close a synchronous, no-straggler teardown.
	if b != nil {
		b.join()
	}
}

// releaseLocked drops one handler user and, on the last release, tears the binding
// down. It DETACHES and signals the batcher but does not join it (see
// detachBatcherLocked); it returns the detached batcher so release() can join
// OUTSIDE h.mu. Returns nil while other users remain.
func (h *invalidateHandler) releaseLocked() *cscInvalBatcher {
	h.users--
	if h.users != 0 {
		return nil
	}
	h.cache = nil
	h.keyPrefix = ""
	// Stop the windowed batcher so its goroutine does not outlive the binding.
	// Detached+signalled here; release() joins it outside the lock, so the last user
	// closing gets a synchronous teardown (on a shared injected cache a late apply
	// could otherwise evict an entry a successor client just repopulated). A later
	// re-acquire starts a fresh batcher via ensureBatcher.
	b := h.detachBatcherLocked()
	// A fresh binding folds in its own window; do not inherit this one's.
	h.invalBatchWindow = 0
	h.invalBatchWindowSet = false
	// Clear the refresh bindings with the binding itself: a client dropped
	// without Close never runs clearRefreshQueue, and a successor reusing
	// this handler must not inherit (or later restore) a dead queue whose
	// consumer is gone — hot entries offered there would vanish silently.
	h.refresh = nil
	h.refreshStack = nil
	return b
}

// sameCache compares Cache interface values without panicking when an
// implementation uses a non-comparable value type.
func sameCache(a, b Cache) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	typ := reflect.TypeOf(a)
	return typ == reflect.TypeOf(b) && typ.Comparable() && a == b
}

func isNilCache(cache Cache) bool {
	if cache == nil {
		return true
	}
	v := reflect.ValueOf(cache)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// errInvalidateHandlerBound: piggybacking on a handler bound to a live
// different cache would leave the new cache uninvalidated.
var errInvalidateHandlerBound = errors.New(`csc: a different "invalidate" push handler is already registered`)

// bindTo binds the handler to (cache, keyPrefix). Success when that is already the
// binding (a derived Client.Conn sharing the parent's processor and cache) or
// when the handler was released by a previous owner's teardown (rebind);
// errInvalidateHandlerBound otherwise.
func (h *invalidateHandler) bindTo(cache Cache, keyPrefix string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case sameCache(h.cache, cache) && h.keyPrefix == keyPrefix:
		h.users++
		return nil
	case h.cache == nil:
		h.cache, h.keyPrefix = cache, keyPrefix
		h.users = 1
		return nil
	default:
		return errInvalidateHandlerBound
	}
}

// lookupInvalidateHandler returns the processor's CSC invalidate handler, nil
// when absent or foreign.
func lookupInvalidateHandler(p push.NotificationProcessor) *invalidateHandler {
	if p == nil {
		return nil
	}
	h, _ := p.GetHandler(invalidatePushName).(*invalidateHandler)
	return h
}

func registerInvalidateHandler(p push.NotificationProcessor, cache Cache, keyPrefix string) error {
	if p == nil || cache == nil {
		return nil
	}
	if existing := p.GetHandler(invalidatePushName); existing != nil {
		h, ok := existing.(*invalidateHandler)
		if !ok {
			return errInvalidateHandlerBound
		}
		return h.bindTo(cache, keyPrefix)
	}
	// VoidProcessor (RESP2) returns an error here; the caller treats it as
	// "CSC not available" rather than fatal. Registered PROTECTED: application
	// code holding the processor must not be able to unregister invalidation
	// under a live client (that would serve unbounded-stale hits with no
	// signal); owner teardown releases the BINDING instead of the handler.
	err := p.RegisterHandler(invalidatePushName, &invalidateHandler{
		cache:     cache,
		keyPrefix: keyPrefix,
		users:     1,
	}, true)
	if err == nil {
		return nil
	}
	// Another client can register the same protected handler between GetHandler
	// and RegisterHandler. Re-read it and accept the compatible binding.
	if existing := p.GetHandler(invalidatePushName); existing != nil {
		h, ok := existing.(*invalidateHandler)
		if !ok {
			return errInvalidateHandlerBound
		}
		return h.bindTo(cache, keyPrefix)
	}
	return err
}

// attachCSC dispatches to the invalidation strategy in
// Options.ClientSideCacheStrategy. Safe with a nil cache; on failure c.csc stays
// nil and commands fall back to normal round-trips. Adding a strategy: a new
// CSCStrategy constant plus cases in Options.init and here.
func (c *baseClient) attachCSC(ctx context.Context, cache Cache) {
	if isNilCache(cache) || c.opt.Protocol != 3 {
		return
	}
	// Credential providers may return a different ACL identity over the
	// client's lifetime (or per context/connection), while the cache namespace
	// is fixed when the client is created. Fixed credentials remain safe because
	// the ACL username is included in the length-delimited namespace below.
	if c.opt.StreamingCredentialsProvider != nil ||
		c.opt.CredentialsProviderContext != nil ||
		c.opt.CredentialsProvider != nil {
		internal.Logger.Printf(ctx,
			"redis: client-side caching is disabled with credential providers")
		return
	}
	c.cscKeyPrefix = cscNamespacePrefix(c.opt.DB, c.opt.Username)
	switch c.opt.ClientSideCacheStrategy {
	case CSCStrategySharedTracking:
		c.attachSharedTrackingCSC(ctx, cache)
	default:
		// Options.init clamps unknown strategies to SharedTracking; delegate anyway.
		c.attachSharedTrackingCSC(ctx, cache)
	}
}

// attachSharedTrackingCSC wires SharedTracking: one shared cache, per-conn CLIENT
// TRACKING, a background drainer, and the owning-conn eviction hook. DB-0 only:
// tracking is bound to the conn's DB and a runtime SELECT does not re-key it.
func (c *baseClient) attachSharedTrackingCSC(ctx context.Context, cache Cache) {
	if c.opt.DB != 0 {
		internal.Logger.Printf(ctx,
			"csc: client-side caching is restricted to DB 0; disabling CSC for client configured with DB=%d. "+
				"Use one client per DB if you need caching against non-zero databases.", c.opt.DB)
		return
	}
	// A pooler without idle-conn draining (e.g. Client.Conn's StickyConnPool)
	// can't apply buffered invalidations, so stay uncached.
	if _, ok := c.connPool.(idleConnDrainer); !ok {
		return
	}
	// The lifecycle hook serializes cache publication with connection removal
	// and socket replacement. Without it, a reply can become visible after its
	// tracking coverage is gone.
	reg, ok := c.connPool.(poolHookSupport)
	if !ok || !reg.SupportsPoolHooks() {
		return
	}
	if err := registerInvalidateHandler(c.pushProcessor, cache, c.cscKeyPrefix); err != nil {
		internal.Logger.Printf(ctx, "csc: failed to register invalidate handler: %v", err)
		return
	}
	// Thread the invalidation-batch window from Options before any push can
	// arrive, so the batcher (if enabled) sees the configured window on the very
	// first invalidation rather than a zero default.
	if ih := lookupInvalidateHandler(c.pushProcessor); ih != nil {
		ih.setInvalBatchWindow(c.opt.ClientSideCacheInvalidationBatchWindow)
	}
	c.csc = cache
	c.registerConnEvictHook(cache, reg)
	c.startBackgroundDrainer()
	c.startCSCRefresher()
	c.startCSCMissCoalescer()
}

// cscHook returns the shared evict-on-remove hook, nil when CSC is off.
func (c *baseClient) cscHook() *cscEvictOnRemoveHook {
	h, _ := c.cscPoolHook.(*cscEvictOnRemoveHook)
	return h
}

// cscInstallConnCloseHook evicts cn's owned entries on any close — including the
// ConnMaxLifetime/idle retirement path (CloseConn) that bypasses the OnRemove
// hook — so entries don't outlive the server tracking dropped at close. Uses the
// onCscClose slot so it doesn't clobber streaming-credentials cleanup.
func (c *baseClient) cscInstallConnCloseHook(cn *pool.Conn) {
	cn.SetOnCscClose(func() error {
		c.cscOnConnClose(cn.GetID())
		return nil
	})
}

// cscInstallConnReinitHook invalidates the old socket's cache coverage before
// SetNetConnAndInitConn replaces it. The later init can then safely enable
// tracking for the new socket without a post-swap publication window.
func (c *baseClient) cscInstallConnReinitHook(cn *pool.Conn) {
	cn.SetOnCscReinit(func() {
		c.cscEvictOwnedEntries(cn.GetID())
	})
}

// cscOnConnClose evicts a closing conn's entries: via the shared hook (which
// records the removed-ring, closing the close-before-fulfill race), else scoped
// EvictByConn on the owning cache.
func (c *baseClient) cscOnConnClose(connID uint64) {
	if h := c.cscHook(); h != nil {
		h.markRemoved(connID)
		return
	}
	if c.csc != nil {
		c.csc.EvictByConn(connID)
	}
}

// poolHookSupport is the pool capability SharedTracking needs to serialize
// cache publication with connection removal and reinitialization.
type poolHookSupport interface {
	AddPoolHook(hook pool.PoolHook)
	RemovePoolHook(hook pool.PoolHook)
	SupportsPoolHooks() bool
}

// cscEvictOnRemoveHook evicts a connection's owned entries when the pool removes
// it (the server stops delivering their invalidations — Window 2), and tracks
// per-conn init generations so fulfillCached can catch a value whose owning
// conn was removed or re-initialized mid-fetch.
type cscEvictOnRemoveHook struct {
	evictor Cache

	mu sync.Mutex
	// initGen counts a live conn's socket (re)initializations: bumped by
	// cscEvictOwnedEntries before its eviction (first init included, so every
	// serving conn has gen >= 1), deleted on removal/close. fulfillCached
	// compares it with the generation captured at reply time.
	initGen map[uint64]uint64
}

func (h *cscEvictOnRemoveHook) OnGet(_ context.Context, _ *pool.Conn, _ bool) (bool, error) {
	return true, nil
}

func (h *cscEvictOnRemoveHook) OnPut(_ context.Context, _ *pool.Conn) (shouldPool, shouldRemove bool, err error) {
	return true, false, nil
}

func (h *cscEvictOnRemoveHook) OnRemove(_ context.Context, cn *pool.Conn, _ error) {
	if cn == nil {
		return
	}
	h.markRemoved(cn.GetID())
}

// markRemoved forgets connID's generation, then evicts. Forgetting before
// evicting lets a racing fulfillCached see the change (a served conn's captured
// generation is >= 1, an absent entry reads 0) and drop an entry created after
// the eviction — closing the close-before-fulfill race.
func (h *cscEvictOnRemoveHook) markRemoved(connID uint64) {
	h.forgetConn(connID)
	h.evictor.EvictByConn(connID)
}

// bumpInitGen advances connID's coverage generation. On reinit it is called by
// the pre-swap hook, before the old socket and its server-side tracking table
// are replaced.
func (h *cscEvictOnRemoveHook) bumpInitGen(connID uint64) {
	h.mu.Lock()
	if h.initGen == nil {
		h.initGen = make(map[uint64]uint64)
	}
	h.initGen[connID]++
	h.mu.Unlock()
}

// invalidateConnCoverage revokes all cache coverage associated with connID.
// Bumping before eviction also rejects an in-flight fetch that completed on the
// connection just before it left the parent's invalidation drainer.
func (h *cscEvictOnRemoveHook) invalidateConnCoverage(connID uint64) {
	h.bumpInitGen(connID)
	h.evictor.EvictByConn(connID)
}

// initGenOf returns connID's current init generation (0 if never bumped).
func (h *cscEvictOnRemoveHook) initGenOf(connID uint64) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.initGen[connID]
}

// forgetConn drops connID's init-generation entry: the conn was removed/closed,
// or its init failed before ever serving (the pubsub path would otherwise leak
// the entry — no OnRemove hook, close hook not yet installed).
func (h *cscEvictOnRemoveHook) forgetConn(connID uint64) {
	h.mu.Lock()
	delete(h.initGen, connID)
	h.mu.Unlock()
}

// fulfillOwnedIfCovered linearizes the final coverage check with connection
// removal/re-init generation changes. Holding h.mu through FulfillOwned means
// either the old generation is rejected before the placeholder becomes valid,
// or publication wins first and the subsequent lifecycle path evicts it before
// closing/replacing the tracked socket.
func (h *cscEvictOnRemoveHook) fulfillOwnedIfCovered(
	cacheKey string,
	token, ownerConnID, capturedGen uint64,
	value []byte,
) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.initGen[ownerConnID] != capturedGen {
		return false
	}
	return h.evictor.FulfillOwned(cacheKey, token, ownerConnID, value)
}

// invalidateAllCoverage revokes every connection generation known to this
// client's pool and evicts the entries those connections own. Incrementing
// instead of deleting keeps in-flight fetches that captured an old generation
// from publishing after a drainer stops.
func (h *cscEvictOnRemoveHook) invalidateAllCoverage() {
	h.mu.Lock()
	connIDs := make([]uint64, 0, len(h.initGen))
	for connID := range h.initGen {
		h.initGen[connID]++
		connIDs = append(connIDs, connID)
	}
	h.mu.Unlock()

	for _, connID := range connIDs {
		h.evictor.EvictByConn(connID)
	}
}

// registerConnEvictHook wires the required OnRemove eviction hook.
func (c *baseClient) registerConnEvictHook(cache Cache, reg poolHookSupport) {
	h := &cscEvictOnRemoveHook{evictor: cache, initGen: make(map[uint64]uint64)}
	reg.AddPoolHook(h)
	c.cscPoolHook = h
}

// cscEvictOwnedEntries evicts connID's entries on first init or immediately
// before a reinit/handoff replaces the socket and its tracking table. It
// prefers the shared hook (so Conn/Tx, which carry it but have a nil csc, still
// evict from the parent cache). Scoped only — no removed-ring (the conn keeps
// serving, and the ring never ages out); the fulfill-vs-re-init race is closed
// by the init-generation bump instead. No custom-cache flush (this also runs on
// first init).
func (c *baseClient) cscEvictOwnedEntries(connID uint64) {
	if h := c.cscHook(); h != nil {
		h.invalidateConnCoverage(connID)
		return
	}
	if c.csc == nil {
		return
	}
	c.csc.EvictByConn(connID)
}

// newStickyConnPool creates a derived sticky pool and revokes the claimed
// connection's parent-cache ownership before it becomes unreachable to the
// parent's idle-connection drainer.
func (c *baseClient) newStickyConnPool() *pool.StickyConnPool {
	sticky := pool.NewStickyConnPool(c.connPool)
	if h := c.cscHook(); h != nil {
		sticky.SetOnFirstConn(func(cn *pool.Conn) {
			if cn != nil {
				h.invalidateConnCoverage(cn.GetID())
			}
		})
	}
	return sticky
}

// cscFetchCapture receives, from the successful attempt's reply read — while
// the serving connection is still held — everything the CSC fetch path needs to
// attribute the cached entry: the raw RESP reply, the conn id, and the conn's
// CSC init generation. The generation must be captured before the conn is
// released: a handoff queued at Put can re-init the socket (bumping the
// generation) before fulfillCached runs.
type cscFetchCapture struct {
	raw     []byte
	connID  uint64
	initGen uint64

	// key/token: the reservation a background refetch (refresh or miss-coalesce)
	// must fulfil. Unused by the single-command path, which passes them to
	// fulfillCached explicitly.
	key   string
	token uint64
}

// cscConnInitGen returns connID's CSC init generation, captured by _process at
// reply time (while the conn is still held) and compared by fulfillCached via
// fulfillOwnedIfCovered. Zero without an active evict-on-remove hook.
func (c *baseClient) cscConnInitGen(connID uint64) uint64 {
	if h := c.cscHook(); h != nil {
		return h.initGenOf(connID)
	}
	return 0
}

// cscForgetConn drops connID's init-generation entry when initialization does
// not establish tracked coverage, either because init failed or tracking was
// rejected and CSC was disabled.
func (c *baseClient) cscForgetConn(connID uint64) {
	if h := c.cscHook(); h != nil {
		h.forgetConn(connID)
	}
}

// errClientTrackingWithCSC rejects CLIENT TRACKING on clients with built-in CSC
// (see the guards in baseClient.process and generalProcessPipeline). The raw
// escape hatches — Do(ctx, "client", "tracking", ...) with string or []byte
// args, and pipelines — are also caught: the guard matches on the command's
// leading args, not the typed method.
var errClientTrackingWithCSC = errors.New(
	"redis: CLIENT TRACKING is not allowed when client-side caching is enabled",
)

// errSelectWithCSC rejects runtime SELECT on clients with built-in CSC. Cache
// keys use Options.DB, while SELECT mutates only the chosen pool connection.
var errSelectWithCSC = errors.New(
	"redis: SELECT is not allowed when client-side caching is enabled",
)

// errAuthWithCSC rejects runtime authentication because it can change one
// connection's ACL identity without changing the client's fixed cache namespace.
var errAuthWithCSC = errors.New(
	"redis: AUTH is not allowed when client-side caching is enabled",
)

// errHelloWithCSC rejects HELLO with arguments because it can switch a tracked
// connection out of RESP3 (and can also change authentication).
var errHelloWithCSC = errors.New(
	"redis: HELLO with arguments is not allowed when client-side caching is enabled",
)

// errResetWithCSC rejects RESET because it disables tracking and switches the
// connection to RESP2.
var errResetWithCSC = errors.New(
	"redis: RESET is not allowed when client-side caching is enabled",
)

// errSubscribeWithCSC rejects raw subscriptions on the ordinary pool. The
// typed Subscribe methods use dedicated PubSub connections and remain allowed.
var errSubscribeWithCSC = errors.New(
	"redis: SUBSCRIBE is not allowed on pooled connections when client-side caching is enabled",
)

// cscCommandError rejects commands that can make a pooled connection's state
// diverge from the assumptions used by CSC.
func (c *baseClient) cscCommandError(cmd Cmder) error {
	// The successful attachment signal is shared with derived clients.
	// initConn's internal command wrapper is exempt during library setup.
	if !c.cscTrackingRequested() || c.allowClientTracking {
		return nil
	}
	switch {
	case isClientTrackingCmd(cmd):
		return errClientTrackingWithCSC
	case isSelectCmd(cmd):
		return errSelectWithCSC
	case isAuthCmd(cmd):
		return errAuthWithCSC
	case isProtocolChangingHelloCmd(cmd):
		return errHelloWithCSC
	case isResetCmd(cmd):
		return errResetWithCSC
	case isSubscribeCmd(cmd):
		return errSubscribeWithCSC
	default:
		return nil
	}
}

// cscDrainHandle owns the drainer lifecycle and serializes client teardown.
// stop signals shutdown; done is closed on exit so Close can join.
type cscDrainHandle struct {
	stop              chan struct{}
	done              chan struct{}
	stopOnce          sync.Once
	teardownOnce      sync.Once
	workersStopOnce   sync.Once
	handlerCloseOnce  sync.Once
	closeOnce         sync.Once
	closeErr          error
	invalidateHandler *invalidateHandler

	// workersMu/workersTornDown close the race between
	// stopCSCRefresherAndCoalescer's one-time teardown and
	// startCSCRefresher/startCSCMissCoalescer's publish step (a bot review
	// flagged this: async conn init can call disableCSCServing, which the
	// drainer goroutine observes on ITS OWN tick and reacts to with the same
	// teardown Close uses — and that tick can in principle land concurrently
	// with construction). workersStopOnce alone is not enough: it only
	// guarantees the teardown body runs once, not that it runs AFTER a
	// worker exists to be stopped. A start function checks workersTornDown
	// under this lock immediately before publishing its queue/handle; if
	// teardown already ran, it declines to start instead of creating a
	// goroutine workersStopOnce can never be Do'd again to stop. See both
	// start functions and stopCSCRefresherAndCoalescer's body.
	//
	// startBackgroundDrainer (attachSharedTrackingCSC's first call, before
	// either start function) needs no matching guard: it is what allocates
	// this struct and assigns it to c.cscDrainHandle, so no teardown path can
	// observe a non-nil handle — let alone race this lock — until after it
	// returns. Every teardown entry point (Close, the drainer's own
	// self-disable) reads c.cscDrainHandle first and no-ops on nil.
	workersMu       sync.Mutex
	workersTornDown bool
}

// signalStop closes stop at most once (so Close and the AddCleanup safety net
// can't double-close) and does not join — a GC cleanup must not block.
func (h *cscDrainHandle) signalStop() {
	h.stopOnce.Do(func() { close(h.stop) })
}

// cscHandlerClient is exposed only through the background drainer's handler
// context. Close must return before the handler does, otherwise it would wait
// for the drainer goroutine that is currently invoking the handler.
type cscHandlerClient struct {
	*baseClient
}

// closeCanonical closes through the canonical *Client wrapper while it is
// still alive — Client.Close also stops the cached autopipeliners, which
// baseClient.Close does not, so closing only the baseClient would leave flush
// goroutines running against closed pools. Falls back to baseClient.Close if
// the wrapper was already collected (weak ref: see baseClient.cscClientWeak).
func (c cscHandlerClient) closeCanonical() error {
	if cl := c.cscClientWeak.Value(); cl != nil {
		return cl.Close()
	}
	return c.baseClient.Close()
}

func (c cscHandlerClient) Close() error {
	h := c.cscDrainHandle
	if h == nil {
		return c.closeCanonical()
	}
	h.handlerCloseOnce.Do(func() {
		// Do NOT deactivate serving or signal the drainer stop here. This runs on
		// the drainer goroutine (a custom push handler called Close), so the
		// canonical close MUST be async — but let IT drive the teardown so
		// stopBackgroundDrainer stops the coalescer and refresher in the canonical
		// order with cscActive STILL TRUE, and the refresher's stop-drain flush
		// re-fetches the in-window invalidated keys. Preemptively storing
		// cscActive=false here made that flush a no-op (refreshInvalidatedBatch
		// bails on the inactive check), dropping the last collection window — the
		// same class the GC finalizer path was reordered to fix. The async close
		// reaches deactivate itself, after the flush; the drainer keeps running the
		// harmless idle drain until stopBackgroundDrainer signals its stop.
		go func() {
			if err := c.closeCanonical(); err != nil {
				internal.Logger.Printf(context.Background(), "csc: deferred client close failed: %v", err)
			}
		}()
	})
	return nil
}

// cscMinDrainInterval floors a user-supplied DrainInterval: sub-millisecond
// timers are unreliable (https://github.com/golang/go/issues/53824).
const cscMinDrainInterval = time.Millisecond

// cscDrainInterval returns DrainInterval clamped to cscMinDrainInterval, or the
// default (cscDrainSkipWindow) when unset.
func (c *baseClient) cscDrainInterval() time.Duration {
	if cfg := c.opt.ClientSideCacheConfig; cfg != nil && cfg.DrainInterval > 0 {
		if cfg.DrainInterval < cscMinDrainInterval {
			return cscMinDrainInterval
		}
		return cfg.DrainInterval
	}
	return cscDrainSkipWindow
}

// idleConnDrainer is the pooler capability the drainer needs (*pool.ConnPool has
// it). attachSharedTrackingCSC leaves a pooler without it uncached, rather than
// serve entries nothing would invalidate.
type idleConnDrainer interface {
	DrainIdleConns(ctx context.Context, st *pool.DrainState, fn func(cn *pool.Conn) error)
}

// startBackgroundDrainer launches the per-client invalidation drainer: each tick
// runs one pool.DrainIdleConns pass, draining idle conns' buffered push frames.
// No-op for poolers that don't implement idleConnDrainer.
func (c *baseClient) startBackgroundDrainer() {
	cp, ok := c.connPool.(idleConnDrainer)
	if !ok {
		return
	}
	if c.cscDrainHandle != nil {
		return // already running (startBackgroundDrainer runs once, in NewClient)
	}
	h := &cscDrainHandle{
		stop:              make(chan struct{}),
		done:              make(chan struct{}),
		invalidateHandler: lookupInvalidateHandler(c.pushProcessor),
	}
	c.cscDrainHandle = h
	active := &atomic.Bool{}
	active.Store(true)
	c.cscActive = active
	interval := c.cscDrainInterval()
	// Custom-processor drain errors are connection-fatal (drainPushNotifications),
	// so a PERSISTENTLY failing custom processor would turn every tick into a
	// conn removal + redial — a sustained dial storm. Damping: after
	// cscDrainCustomErrCap consecutive fatal custom-processor drains, disable
	// CSC serving and stop the drainer (with one log line) instead of churning.
	// Built-in processor errors are real conn desyncs and are never damped.
	_, builtinProc := c.pushProcessor.(*push.Processor)
	go func() {
		defer func() {
			// Self-disable exits (custom-processor damping, or a RESP3/tracking
			// downgrade flipping cscActive that a tick then observes) run the SAME
			// refresher+coalescer teardown as the clean Close path, so a client that
			// KEEPS RUNNING after turning CSC off does not leak those goroutines for
			// its life — the refresher parked on its window, and a coalescer session
			// still holding a pool connection (F1). stopCSCRefresherAndCoalescer is
			// idempotent with the Close path (workersStopOnce), and it joins the
			// refresher and coalescer goroutines — never THIS drainer — so running it
			// here cannot self-block. It also deactivates serving and UNBINDS this
			// client's refresh queue (stopCSCRefresher -> clearRefreshQueue), which on
			// a shared cache+processor restores a surviving sibling's binding, so this
			// defer no longer needs its own active.Store(false)/clearRefreshQueue.
			// Both self-disable paths reach here with cscActive already false
			// (disableCSCServing set it; the damping branch above deactivates before
			// returning), so the helper's refresher flush no-ops here — the ordering is
			// kept only because it is free and uniform with the Close path.
			c.stopCSCRefresherAndCoalescer()
			if c.cscPoolHook != nil {
				if reg, ok := c.connPool.(poolHookSupport); ok {
					reg.RemovePoolHook(c.cscPoolHook)
				}
			}
			if hook := c.cscHook(); hook != nil {
				hook.invalidateAllCoverage()
			}
			if h.invalidateHandler != nil {
				h.invalidateHandler.release()
			}
			close(h.done)
		}()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		// st persists round/visited across ticks; single-goroutine, no lock.
		var st pool.DrainState
		consecFatal := 0
		drain := func(cn *pool.Conn) error {
			processorSucceeded, err := c.drainPushNotifications(cn)
			switch {
			case err != nil:
				consecFatal++
			case processorSucceeded:
				// A successful processor invocation resets consecutive
				// failures. A conn skipped without invoking the processor —
				// including a clean replacement after a fatal drain — does
				// not reset the counter.
				consecFatal = 0
			}
			return err
		}
		for {
			select {
			case <-h.stop:
				return
			case <-ticker.C:
				if !active.Load() {
					return
				}
				// ctx bounds the whole pass; the drain read has its own hard deadline.
				cycleCtx, cancel := context.WithTimeout(context.Background(), interval/2)
				cp.DrainIdleConns(cycleCtx, &st, drain)
				cancel()
				if !builtinProc && consecFatal >= cscDrainCustomErrCap {
					internal.Logger.Printf(context.Background(),
						"csc: disabling client-side caching: the custom push notification processor failed %d consecutive drains "+
							"(each failure removes a connection because the reader may be mid-frame); "+
							"caching cannot be kept fresh safely with this processor", consecFatal)
					// Deactivate BEFORE the defer's teardown so nothing else (ordinary reads
					// via processCached, a normal in-flight flush's refreshInvalidatedBatch
					// bail) keeps treating a persistently-broken custom processor as still
					// serving. The refresher's OWN stop-drain flush no longer depends on this
					// timing either way: it now unconditionally skips its refetch when
					// stopping (see runCSCRefresher's h.stop case), so the exit stays prompt
					// regardless. The defer still stops and joins both workers (F1).
					active.Store(false)
					return
				}
			}
		}
	}()
}

// disableCSCServing atomically stops cache hits and revokes all tracked
// connection coverage. The owner drainer observes the shared active flag on its
// next tick, including when a derived Conn or Tx discovered the incompatibility.
func (c *baseClient) disableCSCServing(ctx context.Context, reason string) {
	active := c.cscActive
	if active == nil || !active.CompareAndSwap(true, false) {
		return
	}
	if hook := c.cscHook(); hook != nil {
		hook.invalidateAllCoverage()
	}
	internal.Logger.Printf(ctx, "csc: disabling client-side caching: %s", reason)
}

// stopBackgroundDrainer joins the drainer goroutine and flushes an owned cache.
// The drainer's exit path releases its handler binding and pool hook, including
// when it stops itself. Owner-only: clones have no handle and return early.
// The fields are never cleared here — fulfillCached reads cscPoolHook on the hot
// path, so niling under a concurrent Close would race; teardownOnce makes repeat
// Close idempotent instead.
func (c *baseClient) stopBackgroundDrainer() {
	h := c.cscDrainHandle
	if h == nil {
		return
	}
	h.teardownOnce.Do(func() {
		// Stop the coalescer and refresher in the shared canonical order (coalescer
		// connection released first, then the refresher's final flush runs while
		// serving is still active, then deactivate). Idempotent with the drainer's
		// own self-disable defer via workersStopOnce. The pool is still open here
		// (closeResources tears it down after this), so the final refetch can run.
		c.stopCSCRefresherAndCoalescer()
		h.signalStop()
		<-h.done
		// The drainer's exit defer revoked and evicted this pool's coverage
		// before closing done, including for injected caches shared elsewhere.
		if c.cscOwnsCache && c.csc != nil {
			c.csc.Flush()
		}
	})
}

// stopCSCRefresherAndCoalescer tears down the reader-miss coalescer and the
// refresh-on-invalidate goroutine in the one order that satisfies every teardown
// constraint, exactly once. It is shared by the clean Close path
// (stopBackgroundDrainer) and the SELF-DISABLE exit (the drainer goroutine's
// defer), so a client that turns CSC off itself stops these goroutines instead of
// leaking them for its lifetime (F1). workersStopOnce makes the two callers
// idempotent and race-free — whichever reaches it first runs the body, the other
// is a no-op — so cscRefreshHandle/cscMissCoalescer are never torn down twice
// concurrently. Neither caller is the refresher or a coalescer goroutine, so the
// joins below never self-block.
//
// Order:
//  1. Stop the coalescer FIRST, while cscActive is still TRUE. stopCSCMissCoalescer
//     swaps its pointer to nil (new misses take the ordinary pooled path), signals
//     its sessions, and JOINS them — RELEASING the held pool connection. On a small
//     pool (PoolSize 1) under continuous miss traffic the coalescer can otherwise
//     hold the only connection, so the refresher's final flush in step 2 would wait
//     its full per-chunk deadline for every chunk (~160s Close stall) and lose the
//     warming (F3). Stopping the coalescer with serving STILL ACTIVE is safe: every
//     fetch stop path returns errCSCRetryUncached and every session stop path settles
//     the same (settleErr tags all other failures cscSessionError), so processCached
//     re-runs each read on the still-open pool — a caller never sees a raw ErrClosed.
//     A teardown-window miss then takes the ordinary pooled path and contends with
//     the step-2 flush for the connection, which is strictly better than the coalescer
//     holding it (bounded by one RTT per miss, not the session's idle/recycle hold).
//     This is why the earlier "deactivate before stopping the coalescer" step is gone:
//     the errCSCRetryUncached backstop it relied on already makes deactivate-first
//     unnecessary, and it conflicted with releasing the connection before the flush.
//  2. Stop the refresher. Its stop-drain flush no longer re-fetches the in-window
//     invalidated keys over the network at all (runCSCRefresher's h.stop case):
//     the drainer defer's invalidateAllCoverage revokes this pool's coverage and
//     evicts every entry attributed to this client's refresh connection right
//     after (pre-existing; see TestStopBackgroundDrainerEvictsSharedCacheCoverage),
//     so a refetch here would only be discarded a moment later — it used to run
//     anyway and could cost thousands of pointless round trips against a healthy
//     server (codex #3989 P1). The targets are abandoned instead: counted into
//     RefreshFailed, left evicted for a reader to repopulate. Because the flush no
//     longer touches the network, the coalescer-first order in step 1 (freeing the
//     pool connection) is no longer LOAD-BEARING for this step, but stays: it is
//     still the one canonical order for Close and the self-disable defer, and
//     still never serves a stale value.
//  3. Deactivate serving.
//
// STARTUP WINDOW: attachSharedTrackingCSC launches the drainer before it assigns
// cscRefreshHandle/cscMissCoalescer, so this can in principle run (via the
// drainer's self-disable defer, reacting to a HELLO/tracking downgrade an
// async conn init observed) WHILE startCSCRefresher/startCSCMissCoalescer are
// still constructing. workersStopOnce alone only guarantees body runs once —
// it does not guarantee a worker exists yet to be stopped by it. The
// workersMu/workersTornDown flag below (set FIRST, before either stop call)
// closes that gap: a start function checks the flag under the same lock
// immediately before publishing, so either it sees teardown hasn't happened
// yet and publishes (and this body's later stop call correctly joins it), or
// it sees the flag already set and declines to start at all — never leaving
// a worker workersStopOnce can no longer reach.
func (c *baseClient) stopCSCRefresherAndCoalescer() {
	// Owner-only. Every worker (drainer, refresher, coalescer) is created together
	// under a cscDrainHandle; a caller without one is a clone (clone() copies neither
	// the handle nor the workers, see redis.go) or a client where CSC never attached,
	// so it has nothing of its own to stop. Running body() here would call
	// stopCSCRefresher, whose read-then-nil of cscRefreshHandle is UNSYNCHRONIZED
	// (single-owner by design), racing the real owner's teardown.
	h := c.cscDrainHandle
	if h == nil {
		return
	}
	body := func() {
		h.workersMu.Lock()
		h.workersTornDown = true
		h.workersMu.Unlock()
		c.stopCSCMissCoalescer()
		c.stopCSCRefresher()
		if c.cscActive != nil {
			c.cscActive.Store(false)
		}
	}
	h.workersStopOnce.Do(body)
}

// cscGet reads a cached value, skipping the defensive copy when the cache is
// the built-in one.
//
// LocalCache.Get copies because Cache is a public interface whose []byte
// return a third-party implementation's caller may retain. The built-in read
// path does not retain it: every caller hands the bytes straight to
// applyCachedReply, which decodes and drops them. Pairing the built-in cache
// with the built-in read path is therefore the one place the copy is provably
// unnecessary -- the same "only the built-in *LocalCache" gate miss coalescing
// already uses.
//
// The returned slice must not be retained or mutated.
func (c *baseClient) cscGet(ctx context.Context, key string) ([]byte, bool) {
	if lc, ok := c.csc.(*LocalCache); ok {
		return lc.getShared(ctx, key)
	}
	return c.csc.Get(ctx, key)
}

// applyCachedReply populates cmd from a previously captured raw RESP reply by
// replaying it through the command's own readReply.
//
// The generic path allocates a bytes.Reader and a proto.Reader (with its bufio
// buffer) per hit, then re-parses. applyCachedFast skips both for the two
// reply shapes that dominate the cacheable command set.
func applyCachedReply(cmd Cmder, raw []byte) error {
	if err, done := applyCachedFast(cmd, raw); done {
		return err
	}
	return cmd.readReply(proto.NewReaderSize(bytes.NewReader(raw), len(raw)+1))
}

// applyCachedFast decodes the simple single-frame replies directly into the
// command, returning done=false for anything it does not fully recognise so
// the caller falls back to the generic reader.
//
// Only shapes whose decoded value is an immutable Go string are handled:
// a blob string into *StringCmd (get, getrange, hget, ...) and a status into
// *StatusCmd (type). Container replies -- mget, hgetall, smembers, zrange and
// the rest of the allow-list -- keep re-parsing, deliberately: their decoded
// form is a slice or map, and handing the same one to two callers would let
// either mutate what the other reads, and the cached entry with it. For those,
// re-parsing IS the defensive copy.
//
// raw must not be retained: it aliases the cache entry's value (see
// LocalCache.getShared). Everything produced here is a fresh string.
func applyCachedFast(cmd Cmder, raw []byte) (err error, done bool) {
	if len(raw) < 3 || raw[len(raw)-2] != '\r' || raw[len(raw)-1] != '\n' {
		return nil, false
	}
	switch raw[0] {
	case proto.RespNil: // RESP3 null: "_\r\n"
		if len(raw) != 3 {
			return nil, false
		}
		switch c := cmd.(type) {
		case *StringCmd:
			c.SetVal("")
			return Nil, true
		case *StatusCmd:
			c.SetVal("")
			return Nil, true
		}
		return nil, false

	case proto.RespStatus: // "+OK\r\n"
		c, ok := cmd.(*StatusCmd)
		if !ok {
			return nil, false
		}
		// Single frame only: a CR or a bare LF before the terminator would mean
		// more than one line, which this path does not handle. The reader stops
		// at the first LF and rejects the frame, and that rejection is what
		// makes processCached drop the bad entry and refetch.
		if bytes.ContainsAny(raw[:len(raw)-2], "\r\n") {
			return nil, false
		}
		c.SetVal(string(raw[1 : len(raw)-2]))
		return nil, true

	case proto.RespString: // "$5\r\nhello\r\n", or "$-1\r\n" for RESP2 null
		c, ok := cmd.(*StringCmd)
		if !ok {
			return nil, false
		}
		i := bytes.IndexByte(raw, '\r')
		if i < 1 || i+1 >= len(raw) || raw[i+1] != '\n' {
			return nil, false
		}
		n, perr := util.ParseInt(raw[1:i], 10, 64)
		if perr != nil {
			return nil, false
		}
		if n < 0 {
			// Only -1 is the RESP2 null bulk string. The reader rejects any
			// other negative length, so decline it: the generic path then
			// fails and processCached drops the bad entry and refetches.
			if n != -1 || i+2 != len(raw) {
				return nil, false
			}
			c.SetVal("")
			return Nil, true
		}
		// Payload must be exactly n bytes followed by the trailing CRLF.
		start := i + 2
		if int64(len(raw)) != int64(start)+n+2 {
			return nil, false
		}
		c.SetVal(string(raw[start : start+int(n)]))
		return nil, true
	}
	return nil, false
}

// classifyCachedReply reports the same error applyCachedReply would, without a
// caller command to populate. The miss coalescer uses it on the abandoned path
// (the caller returned and owns its Cmder again) to decide cache-vs-cancel: a
// value or Nil is cacheable, a top-level RESP error is not. It reads the frame
// generically, so it can only diverge from a concrete cmd's readReply on a
// well-formed reply of an unexpected shape — which the next reader re-parses and
// drops (see processCached), so a rare mis-cache self-heals.
func classifyCachedReply(raw []byte) error {
	_, err := proto.NewReaderSize(bytes.NewReader(raw), len(raw)+1).ReadReply()
	return err
}

// isCacheableReplyResult reports whether a fully read Redis reply can be
// cached. redis.Nil is a normal negative lookup, not a transport/protocol
// failure; tracking will invalidate it if the key is later created.
func isCacheableReplyResult(err error) bool {
	return err == nil || err == Nil
}

// cscDrainSkipWindow is the default SharedTracking drain period (overridable via
// ClientSideCacheConfig.DrainInterval). A buffered invalidation is picked up within
// roughly one round; MaxStaleness, when configured, is the hard time-based backstop.
const cscDrainSkipWindow = 5 * time.Millisecond

// cscDrainHardReadCap is the hard socket read deadline the drainer applies via
// Conn.WithReaderHardDeadline. It bounds only a rare partial-frame mid-read. A
// var (not const) so the tuning harness can sweep it.
var cscDrainHardReadCap = 50 * time.Millisecond

// cscDrainProbeReadCap bounds the non-consuming one-byte probe used only when
// an opaque transport may hold data that the socket readiness check cannot see.
const cscDrainProbeReadCap = 50 * time.Microsecond

// cscDrainCustomErrCap is the number of CONSECUTIVE fatal custom-processor
// drain errors after which the drainer disables CSC instead of removing (and
// redialing) a connection per tick indefinitely.
const cscDrainCustomErrCap = 8

// processCached runs the Get-Reserve-Fulfill lifecycle for a cacheable command.
// The caller (process) first makes sure that CSC is active and that cmd is
// eligible.
//
// startAttempt is the number of attempts already spent before this call. It is
// not zero only on the full-duplex divert (retryOnNormalConn) of a cacheable
// command. Such a command spent its first attempt on the FD socket and got a
// retryable reply. Give startAttempt to every processWithRetry fallback and to
// the fetch. If you do not, a diverted cache miss runs MaxRetries+1 retries
// after the FD attempt. That is one attempt too many. A cache HIT does no network
// attempt and returns before the retry loop, so startAttempt does not affect its
// retry BUDGET — but it still seeds the reported attempt COUNT (see below) so a
// diverted hit reports the FD attempt it already spent, not zero.
func (c *baseClient) processCached(ctx context.Context, cmd Cmder, state *processState, startAttempt int) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Seed the reported attempt count for the OTel duration metric. A cache hit
	// returns below without entering processWithRetry's loop (which is what sets
	// state.attempts), so without this a diverted hit (startAttempt>0) would report
	// zero attempts, hiding the FD socket attempt it already spent. A miss falls
	// through to processWithRetry, whose loop overwrites this.
	if state != nil {
		state.attempts = startAttempt
	}

	// Once the drainer has stopped (owner Close, or the owner dropped without
	// Close), no invalidations flow — a surviving clone must not serve stale hits.
	if a := c.cscActive; a != nil && !a.Load() {
		return c.processWithRetry(ctx, cmd, nil, state, startAttempt)
	}

	// Read the namespace BEFORE building the key: the key is now built with
	// the namespace already in it, in one allocation, so the prefix has to be
	// known first.
	keyPrefix := c.cscKeyPrefix
	if keyPrefix == "" {
		// A successfully attached client always has a namespace. Fail closed if
		// an incomplete custom baseClient reaches this path.
		return c.processWithRetry(ctx, cmd, nil, state, startAttempt)
	}
	// Check the key arguments BEFORE building the cache key. Building it
	// encodes every argument, which calls MarshalBinary on a marshaler key; an
	// uncacheable command would then marshal its key twice, once here and once
	// for the request. The check allocates nothing.
	keyLo, keyHi, ok := cscKeySpan(cmd)
	if !ok || !cscKeysRenderable(cmd, keyLo, keyHi) {
		// Without a key list we cannot react to invalidations for this
		// command, so it must not be cached.
		return c.processWithRetry(ctx, cmd, nil, state, startAttempt)
	}
	key, ok := buildCacheKeyNS(cmd, keyPrefix)
	if !ok {
		return c.processWithRetry(ctx, cmd, nil, state, startAttempt)
	}

	// The key strings are deliberately NOT built here. Only Reserve needs
	// them, and Reserve is on the miss path -- so at a 94-99% hit rate this
	// used to allocate a key slice, a namespaced slice and one string per key
	// on every read and discard all of it. By object count that was ~18% of
	// everything the client allocated. They are built in cscMissKeys below,
	// after the hit check.

	// Serve hits straight from the cache.
	if data, ok := c.cscGet(ctx, key); ok {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := applyCachedReply(cmd, data); isCacheableReplyResult(err) {
			return err
		}
		c.csc.DeleteByCacheKey(key)
	}

	// Demand trigger: a miss for a key still in the refresher's collection window
	// flushes that window now (no-op when refresh/coalescing is off). Scope: this
	// signals THIS client's own refresh queue. With a shared cache+processor across
	// clients the active refresh binding is the last-attached client's queue, so a
	// miss on one client does not early-flush a sibling's window — the sibling's
	// window timer is the backstop. Correctness is unaffected (the key still
	// refreshes); only the early-flush latency is client-local.
	c.cscRefreshQueue.signalDemand(key)

	// Miss path: now the key list is actually needed.
	nsRedisKeys := cscMissKeys(cmd, keyPrefix, keyLo, keyHi)

	token, shouldFetch := c.csc.Reserve(key, nsRedisKeys)
	if !shouldFetch {
		// Another goroutine is fetching; Get below waits until it completes.
		if data, ok := c.cscGet(ctx, key); ok {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := applyCachedReply(cmd, data); isCacheableReplyResult(err) {
				return err
			}
			c.csc.DeleteByCacheKey(key)
		}
		// Original fetcher cancelled or its value was invalidated; try to take
		// over so later waiters still benefit from the cache. This is the 2x-RTT
		// path under churn: we waited a round trip and still must fetch ourselves.
		token, shouldFetch = c.csc.Reserve(key, nsRedisKeys)
	}

	// Reader-miss coalescing: hand the reserved miss to the batcher (no-op when
	// off). Load the pointer ONCE: a concurrent Close swaps it to nil, and a
	// second load after the nil-check would call fetch on a nil receiver.
	if mc := c.cscMissCoalescer.Load(); shouldFetch && mc != nil {
		served, err := mc.fetch(ctx, cmd, key, token)
		// Re-run on the normal path when the coalescer bailed (errCSCRetryUncached)
		// or hit a session or transport failure (tagged cscSessionError by
		// settleErr). Then a coalesced miss gets the same MaxRetries and backoff as
		// any other command, instead of a raw io.EOF or ErrClosed to the caller (Ofek
		// review #3989). The coalescer always cancels the reservation on failure, so
		// the re-run starts clean, and processWithRetry handles a cancelled caller
		// context itself. A command result is not tagged (for example redis.Nil,
		// WRONGTYPE, or a retryable reply such as LOADING). It is the command's
		// answer, already applied to cmd, so it is returned as-is. This keeps the
		// coalesced path's documented tradeoff: no per-command retry for reply-level
		// errors.
		var sessErr cscSessionError
		if err == errCSCRetryUncached || errors.As(err, &sessErr) {
			// The coalescer bailed and cancelled its reservation. Re-reserve and FALL
			// THROUGH to the capture path below so a successful retry repopulates the
			// cache (a nil-capture processWithRetry returned the value but stored
			// nothing, so connection blips and wire-budget sheds left the key uncached
			// and later readers missed).
			//
			// Carry the coalesced attempt into the re-run's accounting. served is
			// non-nil exactly when the request reached a session connection (the
			// writer attributes every request of a batch to its conn before the
			// write; a pre-queue shed or a session that never acquired a conn leaves
			// it nil), which is the same "one attempt on that conn" rule the success
			// branch below applies. processWithRetry seeds both its reported attempt
			// count and its retry-budget position from startAttempt, so the failed
			// coalesced attempt shows up in retry_attempts and the error callback, and
			// the re-run does not get MaxRetries+1 fresh attempts on top of the one
			// already spent (codex on #3989; needs the explicit-start plumbing from the
			// full-duplex autopipeline branch, now merged).
			if served != nil {
				startAttempt++
				// Record it on state too, not only through processWithRetry: two exits
				// below never reach that loop — the exhausted-budget return here and
				// the takeover-hit return after the re-Reserve — and without this they
				// reported the attempts seeded before the fetch and a nil connection
				// for a request that did reach one (codex on #4002). processWithRetry
				// overwrites both on its first iteration, and keeps lastConn when the
				// re-run never acquires a connection.
				if state != nil {
					state.attempts = startAttempt
					state.lastConn = served
				}
				// Budget check. The command has now spent startAttempt of its
				// MaxRetries+1 attempts; when that was the last one (an FD attempt plus
				// this coalesced one with MaxRetries=1, or any coalesced session failure
				// with retries disabled), a re-run would execute it once more:
				// processWithRetry clamps startAttempt back into its loop so a caller can
				// never disable execution, which here is an attempt over budget (codex on
				// #4002). Stop with the coalescer's cause instead — the same raw error
				// processWithRetry returns when its own loop runs out — and emit the error
				// metric the skipped re-run would have (settleErr leaves that to the
				// re-run so a re-run that succeeds is not flagged).
				if startAttempt > c.opt.MaxRetries {
					cause := err
					if sessErr.err != nil {
						cause = sessErr.err
					}
					cmd.SetErr(cause)
					recordCommandError(ctx, cause, served, startAttempt-1)
					return cause
				}
			}
			token, shouldFetch = c.csc.Reserve(key, nsRedisKeys)
			if !shouldFetch {
				// Another waiter won the re-Reserve race and is fetching. WAIT on it
				// (Get parks on the in-progress entry) instead of falling through to an
				// independent pooled request: a session failure wakes every same-key
				// waiter at once, and each running its own processWithRetry would turn
				// one hot key into a pool stampede mid-recovery — defeating the
				// coalescing this path exists for. Same shape (and 2x-RTT churn
				// tradeoff) as the first-Reserve loser path above.
				if data, ok := c.cscGet(ctx, key); ok {
					if err := ctx.Err(); err != nil {
						return err
					}
					if err := applyCachedReply(cmd, data); isCacheableReplyResult(err) {
						return err
					}
					c.csc.DeleteByCacheKey(key)
				}
				// The new owner cancelled or its value was invalidated; try to take
				// over. A second loss falls through to the plain pooled path — after
				// two waits, forward progress outranks another round of parking.
				token, shouldFetch = c.csc.Reserve(key, nsRedisKeys)
			}
		} else {
			// Native-recorder attribution: the coalesced fetch contacted Redis on
			// the session's held connection — report it as one attempt on that conn
			// so operation-duration metrics carry a real server.address instead of
			// zero attempts and a nil connection.
			if state != nil && served != nil {
				state.attempts++
				state.lastConn = served
			}
			return err
		}
	}

	// With shouldFetch still false, this caller lost the reservation twice and
	// reads the server directly, uncached. That read can return a newer value
	// than a fetch another caller reserved earlier and is still running. That
	// fetch may store its older value afterwards, and a later hit returns it
	// until the key's invalidation arrives, so reads are not monotonic per
	// caller. This stays within the staleness every hit already has until its
	// invalidation arrives. Dropping the older fetch would cost each of its
	// waiters a miss, under the churn that leads here, so it is documented
	// instead (README, client-side caching).
	var fc cscFetchCapture
	var capture *cscFetchCapture
	if shouldFetch {
		capture = &fc
		// Release the placeholder if processWithRetry panics; Cancel on a
		// stale token is a no-op.
		defer func() {
			if capture != nil {
				c.csc.Cancel(key, token)
			}
		}()
	}

	err := c.processWithRetry(ctx, cmd, capture, state, startAttempt)

	if shouldFetch {
		capture = nil // disarm the deferred Cancel
		if isCacheableReplyResult(err) {
			c.fulfillCached(key, token, &fc)
		} else {
			c.csc.Cancel(key, token)
		}
	}
	return err
}

// fulfillCached stores a fetched value, attributing it to its serving conn when
// an evict-on-remove hook is active so EvictByConn can drop it if that conn is
// removed. It also closes the attribute-vs-coverage races: the conn is released
// before this runs, so its OnRemove eviction — or a handoff re-init's scoped
// eviction — may fire before the entry exists. Publication is serialized with
// the hook's init-generation changes, so a reply whose invalidation coverage
// was already lost never becomes visible and never wakes waiters with stale
// data.
func (c *baseClient) fulfillCached(key string, token uint64, fc *cscFetchCapture) bool {
	if active := c.cscActive; active != nil && !active.Load() {
		c.csc.Cancel(key, token)
		return false
	}
	if hook := c.cscHook(); hook != nil {
		if fc.connID == 0 {
			// Invariant: an active hook always gets a real conn id (>=1). A zero id
			// would leave the entry unattributed and un-evictable, so fail closed.
			c.csc.Cancel(key, token)
			return false
		}
		if !hook.fulfillOwnedIfCovered(key, token, fc.connID, fc.initGen, fc.raw) {
			// A coverage mismatch leaves the reservation IN_PROGRESS because
			// FulfillOwned was deliberately skipped. Cancel wakes its waiters
			// as misses so one can safely refetch on a covered connection.
			c.csc.Cancel(key, token)
			return false
		}
		return true
	}
	return c.csc.FulfillOwned(key, token, 0, fc.raw)
}
