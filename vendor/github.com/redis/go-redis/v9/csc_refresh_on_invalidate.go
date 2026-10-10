package redis

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/pool"
	"github.com/redis/go-redis/v9/internal/proto"
)

// Refresh-on-invalidate: re-fetch a key as soon as its invalidation arrives,
// instead of dropping it and waiting for a reader to discover the miss.
//
// Why this is the lever worth pulling. The connection requirement of a CSC client
// is roughly `miss_rate x throughput x RTT`, so the hit rate is the only quantity
// that moves throughput and connection count in the same direction. Every other
// item measured in this investigation trades one against the other. Under churn
// the misses are exactly the invalidated keys, and they are known the moment the
// push arrives — a whole round trip before any reader asks.
//
// # Why it re-inserts rather than refreshes in place
//
// The time-based sweeper (csc_revalidate.go) can use RefreshValue, which replaces
// the value of a LIVE entry. This path cannot: the entry must be DELETED the
// instant the invalidation lands, because leaving a known-stale value readable
// while a refresh is in flight is the exact bug this whole line of work exists to
// remove. So the entry is dropped first and re-inserted through the ordinary
// Reserve/Fulfil path.
//
// Residual (not the same bug): because this republishes a value nobody asked for,
// a SECOND write whose invalidation is still on the refresh connection's socket
// behind the reply can be applied after the republish, so a reader briefly gets a
// stale HIT where, without refresh, they'd have taken a fresh MISS. It self-heals
// — the republish is on a CLIENT TRACKING conn, so that pending invalidation is
// delivered and deletes the entry — and is bounded by the drain interval /
// MaxStaleness. This is a narrower window than the "stale value readable for the
// whole refresh RTT" case above, not a reintroduction of it.
//
// That has a useful consequence: Reserve single-flights. If a reader misses the
// key while a background refresh is already in flight, it waits on that
// reservation instead of issuing a second fetch — so the key is fetched once, by
// one connection, no matter how many parties want it.
//
// # On fetching from the "owner" connection
//
// A reasonable worry is that refreshing on an arbitrary connection would register
// the key on several connections at once, so one write would produce several
// invalidations. It does not, because default (non-BCAST) tracking is ONE-SHOT:
// when the server sends `invalidate k` it removes k from its tracking table, so
// at the moment this path runs no connection is registered for k. The refresh
// creates exactly one registration, whichever connection performs it, and Reserve
// guarantees only one refresh runs.
//
// What connection choice does still affect is eviction blast radius: an entry is
// attributed to the connection that fetched it (see EvictByConn), so refreshing
// many entries onto one connection concentrates them there. Pinning the refresh to
// the original owner would fix that, but the pool has no "give me connection N"
// operation — withPipelineConn takes whichever is free. Noted as a follow-up; it
// is a robustness question, not a correctness or invalidation-count one.

// cscNoRefreshOnInvalidate force-disables the feature. The gate is
// Options.ClientSideCacheRefreshOnInvalidate; this stays constant false.
const cscNoRefreshOnInvalidate = false

const (
	// cscRefreshQueueDepth bounds the pending-refresh backlog. Bounded, and it
	// DROPS rather than blocks: the producer is the invalidation drainer, and
	// stalling that would delay the invalidations themselves — trading a hit-rate
	// optimization for a correctness-critical path. A drop just means the key is
	// refetched by whichever reader wants it next, which is today's behaviour.
	cscRefreshQueueDepth = 4096

	// cscRefreshTargetMaxBytes caps the key bytes (cacheKey + Redis keys) a single
	// queued refresh target may retain. The depth cap alone bounds the queue by ITEM
	// count, not memory: with large key encodings, up to cscRefreshQueueDepth targets
	// could pin far more than the cache's own limit. A target over this cap is dropped
	// rather than queued — like a full-queue drop, it just means a later reader does a
	// normal miss fetch. Bounds the queue's retained key memory to roughly
	// cscRefreshQueueDepth * this.
	cscRefreshTargetMaxBytes = 4 << 10 // 4 KiB

	// cscRefreshBatchMax caps how many keys ride one round trip.
	cscRefreshBatchMax = 128

	// cscRefreshReplyBudgetBytes caps one refresh round trip's EXPECTED REPLY
	// volume (sum of the evicted values' sizes, cscRefreshTarget.valBytes). The
	// key-byte budget bounds only what we write; replies for a chunk's early keys
	// stream back while its later keys are still being written, and against a
	// flow-controlling middlebox (a proxy or tunnel that stops reading when its
	// buffer to us fills — vanilla Redis instead buffers replies in RAM) enough
	// in-flight reply bytes stall the connection both ways: the classic pipeline
	// write-read deadlock, which here surfaces as the per-chunk deadline failing
	// the whole chunk. 32 KiB stays comfortably inside typical kernel socket
	// buffering (~64K+64K), so expected replies alone cannot jam the path; a
	// value that grew far past its evicted size since invalidation can still
	// overshoot — the estimate narrows the window, the deadline stays the
	// backstop. The first target of a chunk always goes regardless, so an
	// oversized single value cannot stall the loop.
	cscRefreshReplyBudgetBytes = 32 << 10

	// cscRefreshBatchTimeout bounds one refresh round trip. It caps the batch ctx
	// AND, when the user disabled per-op write timeouts, the batch write deadline —
	// so a stalled deadline-less flush cannot wedge the refresher (the ctx does not
	// interrupt a deadline-less socket write). The two uses must agree, so they
	// share this constant.
	cscRefreshBatchTimeout = 5 * time.Second

	cscRefreshWarnEvery = 30 * time.Second
)

// cscRefreshWindow is the coalescing window. Invalidations are COLLECTED for this
// long before the batch is refetched, instead of firing a round trip as each
// trickle of pushes arrives. The window starts when the first key is collected
// and does NOT slide on later arrivals — under continuous churn a sliding reset
// would never fire and the design would degenerate to the size cap alone. The
// window is flushed early by demand (a reader touching a collected key) or by the
// size cap; whichever comes first. Longer window = fewer, larger round trips (the
// point: it lets refresh work at a small pipeline pool) at the cost of staleness
// bounded by the window for keys nobody reads. A var (not const) so a test can
// take the timer out of the picture.
var cscRefreshWindow = 500 * time.Millisecond

// cscRefreshWindowMaxKeys flushes the window early once this many distinct keys
// have collected, so a burst cannot outrun the window ahead of the queue's own
// bounded backlog. A multiple of the per-round-trip cap.
const cscRefreshWindowMaxKeys = 4 * cscRefreshBatchMax

// cscDemandRefresh gates the demand trigger. On (default): a read for a key still
// sitting in the window flushes the whole window immediately, so an actively-read
// batch refreshes in ~one RTT instead of waiting out the window, while an idle
// batch waits the full window (and nobody is reading it, so the wait is free).
// Off: window + size cap only. On by default.
const cscDemandRefresh = true

// cscRefreshCooldown optionally suppresses a refresh for an entry published within
// this window. DEFAULT 0, meaning off — measurement said to leave it off.
//
// The motivation was real. The same key read on several connections is registered
// by the server once per connection, so ONE write produces one invalidation push
// per registration, and that happens routinely: when a batched read finds another
// fetch already in flight it declines the reservation and goes to the wire anyway
// rather than stall the batch (the miss-coalescer path), registering the key a
// second time. Measured: 34-38% of incoming invalidation pushes match no entry at
// all, which is the signature of exactly this.
//
// But a time window is the wrong instrument, for two reasons.
//
// First, Reserve already dedups the part that costs anything. A duplicate
// invalidation that lands while the refresh is still in flight finds the key
// reserved, is declined, and is dropped for one map operation. Only a duplicate
// arriving AFTER the refresh completed can start a second fetch — which needs the
// refresh to be faster than the gap between duplicates, i.e. loopback, not WAN.
//
// Second, the window cannot tell a duplicate of the write it already handled from a
// genuinely NEW write arriving a few milliseconds later, so it suppresses both. The
// A/B showed the cost of that: at 20k invalidations/s, enabling it cut refresh work
// 36% (289,822 -> 184,780) but LOST throughput (1,347,964 -> 1,326,234) and hit rate
// (93.6% -> 93.0%). Doing the extra work was cheaper than skipping some of the
// useful part.
//
// Left defaulted to 0 (off); measurement said to leave it off.
//
//nolint:unused // deliberately-off, kept as a documented measured knob (see above) for the follow-up that wires cooldown; referencing it now would misrepresent the shipped behavior.
var cscRefreshCooldown time.Duration

// cscRefreshTarget is one entry to re-fetch: the cache key doubles as the wire
// form of the command that produced it, and redisKeys are already namespaced, so
// both can be handed straight back to Reserve.
type cscRefreshTarget struct {
	cacheKey  string
	redisKeys []string
	token     uint64
	// valBytes approximates the refetch reply size: the length of the value the
	// invalidation just evicted (captured under the shard lock in
	// collectHotAndDelete). The refresher budgets each round trip's EXPECTED
	// REPLY bytes with it — the request side is tiny (keys), but replies for the
	// chunk's early keys stream back while later keys are still being written,
	// and unbounded reply volume is what jams a flow-controlling middlebox
	// (proxy/tunnel) into the classic pipeline write-read deadlock. An estimate,
	// not a guarantee: the value may have grown since eviction (often why it was
	// invalidated), so this narrows the jam window rather than closing it.
	valBytes int
	// accessNs is the evicted entry's reader-access token (lastAccessNs), captured
	// under the shard lock in collectHotAndDelete. The refresh republish keeps it
	// so a background refresh does NOT count as a fresh reader access: a new token
	// would keep the key above the refresh horizon forever and make each
	// invalidation refresh it again even after all readers stop (a self-sustaining
	// refetch loop, contrary to the cold-key guard). Keeping the original token
	// means only real reader activity keeps a key eligible for refresh. See
	// stageRefreshAccess.
	accessNs int64
	// read is the evicted entry's second-chance bit, restored with accessNs so
	// the refreshed entry keeps the eviction standing the old one had.
	read bool
}

// cscRefreshQueue carries invalidated-but-hot keys from the drainer to the
// refresher.
type cscRefreshQueue struct {
	ch chan cscRefreshTarget

	// pendingSet mirrors the cache keys currently sitting in the refresher's
	// collection window, so the read path (processCached, miss branch) can test
	// membership without a lock and signal demand. Written and cleared only by
	// the refresher goroutine; read concurrently by every reader — hence sync.Map.
	pendingSet sync.Map
	// demandCh carries a single "flush now" nudge from a reader that touched a
	// pending key, STAMPED with the window generation it belongs to (see demandGen).
	// Buffered depth 1 and sent non-blocking: one pending nudge is all the refresher
	// needs, and the window timer is the backstop if it is dropped.
	demandCh chan uint64
	// demandGen tags the current collection window. The refresher bumps it every
	// time it clears a window (in flush); signalDemand stamps the value it reads
	// BEFORE the pendingSet membership check onto its nudge. A reader that passed the
	// check just before a clear then sends AFTER it: its nudge carries the retired
	// generation, so the refresher compares it unequal to the current one and ignores
	// it — a stale nudge can no longer flush the NEXT window early, which would defeat
	// the coalescing window and inflate DemandFlushes.
	demandGen atomic.Uint64

	// sinceToken is the "recently read" horizon: only entries whose last read is
	// newer than this are worth refetching. Without it the refresher would chase
	// cold keys, and because a refresh RE-REGISTERS the key with the server, that
	// would both pin the server's tracking table and manufacture the next
	// invalidation — a feedback loop that generates its own work. Same guard the
	// time-based sweeper uses for the same reason.
	//
	// Pinned at cscInvalNoHorizon (-1): every Valid entry is "hot" by
	// construction, so an invalidation always schedules a refetch. The
	// feedback-loop tradeoff above is therefore accepted unconditionally —
	// refreshing re-registers the key with the server's tracking table, so a
	// write-heavy, rarely-read key keeps refreshing itself off write traffic
	// for as long as its entry survives capacity eviction.
	//
	// The field is kept rather than dropped because the sentinel has a SECOND
	// meaning that still applies: an invalidation enqueued while no refresh
	// binding existed carries it too (see cscInvalItem.sinceToken).
	sinceToken atomic.Int64

	enqueued      atomic.Uint64
	dropped       atomic.Uint64
	refreshed     atomic.Uint64
	refreshFailed atomic.Uint64
	demandFlushes atomic.Uint64
}

// signalDemand nudges the refresher to flush its collection window now, but only
// if cacheKey is actually sitting in that window — i.e. this read is a miss for a
// key whose invalidation we already collected. Called on the read miss path, so
// it must stay cheap: one sync.Map load and, on the rare hit, one non-blocking
// send. The read is already paying a server round trip for its own key; the
// nudge just warms the rest of the co-invalidated batch a window earlier.
func (q *cscRefreshQueue) signalDemand(cacheKey string) {
	if q == nil || !cscDemandRefresh {
		return
	}
	// Read the generation BEFORE the membership check, so a window cleared between
	// here and the send retires this stamp: the refresher then sees a stale
	// generation and ignores the nudge (see demandGen). Reading it AFTER the check
	// would stamp the NEW window and flush it early — the bug this guards.
	gen := q.demandGen.Load()
	if _, ok := q.pendingSet.Load(cacheKey); !ok {
		return
	}
	select {
	case q.demandCh <- gen:
	default:
	}
}

// demandIsCurrent reports whether a demand nudge stamped with gen still refers to
// the window the refresher is collecting now. A nudge from a retired generation
// (its window was already flushed) is ignored, so it cannot flush the next window
// early. Extracted so the generation check is unit-tested (like cscRefreshChunkEnd).
func (q *cscRefreshQueue) demandIsCurrent(gen uint64) bool {
	return gen == q.demandGen.Load()
}

// offer enqueues without blocking, counting what it had to drop. A target whose
// key bytes exceed cscRefreshTargetMaxBytes is dropped too, so the item-bounded
// queue cannot pin unbounded key memory (a dropped target is refetched by the next
// reader that wants it).
func (q *cscRefreshQueue) offer(t cscRefreshTarget) {
	if cscRefreshTargetBytes(t) > cscRefreshTargetMaxBytes {
		q.dropped.Add(1)
		return
	}
	select {
	case q.ch <- t:
		q.enqueued.Add(1)
	default:
		q.dropped.Add(1)
	}
}

// cscRefreshTargetBytes approximates the key memory a queued target retains: its
// cache key plus every Redis key string.
func cscRefreshTargetBytes(t cscRefreshTarget) int {
	n := len(t.cacheKey)
	for _, k := range t.redisKeys {
		n += len(k)
	}
	return n
}

// cscRefreshChunkEnd returns the exclusive end index of the next refresh round
// trip starting at `start`: at most cscRefreshBatchMax targets, request bytes
// (cache key sans prefix = wire form) within writeBudget, and expected reply
// bytes (valBytes of the evicted values) within cscRefreshReplyBudgetBytes. The
// first target always goes, so a single oversized key or value cannot stall the
// loop. Pure; unit-tested like fdBatchEnd.
func cscRefreshChunkEnd(targets []cscRefreshTarget, start, prefixLen, writeBudget int) int {
	end, reqBytes, replyBytes := start, 0, 0
	for end < len(targets) && end-start < cscRefreshBatchMax {
		w := len(targets[end].cacheKey) - prefixLen
		r := targets[end].valBytes
		if end > start && (reqBytes+w > writeBudget || replyBytes+r > cscRefreshReplyBudgetBytes) {
			break
		}
		reqBytes += w
		replyBytes += r
		end++
	}
	return end
}

// CSCRefreshStats reports refresh-on-invalidate activity: keys queued, keys
// dropped because the backlog was full, and values actually republished.
//
// Experimental: this API may change in a minor release.
type CSCRefreshStats struct {
	Enqueued  uint64
	Dropped   uint64
	Refreshed uint64

	// RefreshFailed counts refresh round trips that errored (a connection or
	// protocol failure during the batch). Those keys stay evicted and a later read
	// repopulates them, so a rising count means refresh is degrading to plain
	// eviction. Counted per errored batch, not per key.
	RefreshFailed uint64

	// DemandFlushes counts collection windows flushed early because a reader
	// missed a key still sitting in the window (vs flushed by the window timer or
	// the size cap). High relative to total flushes means the demand trigger is
	// doing its job — actively-read batches refresh in ~one RTT instead of
	// waiting out the window.
	DemandFlushes uint64

	// Invalidations counts keys named in INCOMING invalidation pushes, tallied at
	// the handler before dedup/batching. Deletions counts keys the cache actually
	// processed for removal; under duplicate pushes Deletions < Invalidations
	// because the batcher dedups (and, under an invalidation flood, the spill-cap
	// full-Flush supersedes queued deletes wholesale — those count as invalidations
	// but not deletions). So Invalidations - Deletions measures dedup + flood
	// fallback, not dedup alone. DeletionsNoop counts applied deletes that matched
	// no live entry, the direct duplicate-invalidation signature.
	Invalidations uint64
	Deletions     uint64
	DeletionsNoop uint64
}

// CSCRefreshStats returns the client's refresh-on-invalidate counters.
//
// SCOPE on a SHARED cache: Invalidations/Deletions/DeletionsNoop are cache-global
// (read from the shared LocalCache, so every client attached to it reports the
// same totals), while Enqueued/Dropped/Refreshed/RefreshFailed/DemandFlushes come
// from THIS client's refresh queue. With a shared cache+processor the active
// refresh binding is the last-attached client's queue, so an earlier client can
// report every invalidation but ~zero refresh work — not a defect, that client
// genuinely is not the one refreshing. Treat a refresh/invalidation RATIO as
// meaningful only for a single-client cache or for the client holding the active
// binding; across a shared cache the two groups are different scopes.
//
// Experimental: this API may change in a minor release.
func (c *Client) CSCRefreshStats() CSCRefreshStats {
	var st CSCRefreshStats
	// Cache-level counters (Invalidations, Deletions, DeletionsNoop) are recorded
	// by the invalidation handler whenever CSC is on, even with
	// ClientSideCacheRefreshOnInvalidate off (the default) and only batching
	// enabled. Read them independently of the refresh queue, or a batch-only setup
	// would always report zero for activity it clearly performs.
	if lc, ok := c.baseClient.csc.(*LocalCache); ok {
		st.Invalidations = lc.InvalidationStats()
		st.Deletions, st.DeletionsNoop = lc.DeletionStats()
	}
	// The remaining counters exist only when the refresh queue is running.
	if q := c.baseClient.cscRefreshQueue; q != nil {
		st.Enqueued = q.enqueued.Load()
		st.Dropped = q.dropped.Load()
		st.Refreshed = q.refreshed.Load()
		st.RefreshFailed = q.refreshFailed.Load()
		st.DemandFlushes = q.demandFlushes.Load()
	}
	return st
}

// cscRefreshOnInvalidateEnabled reports whether this client should re-fetch
// invalidated keys in the background.
func (c *baseClient) cscRefreshOnInvalidateEnabled() bool {
	if cscNoRefreshOnInvalidate || c.csc == nil || c.opt == nil {
		return false
	}
	if !c.opt.ClientSideCacheRefreshOnInvalidate {
		return false
	}
	// The collect step needs the concrete cache: a Cache implementation that only
	// satisfies the interface cannot report which entries it removed.
	_, ok := c.csc.(*LocalCache)
	return ok
}

// startCSCRefresher launches the refresher goroutine and hands the drainer the
// queue to feed.
func (c *baseClient) startCSCRefresher() {
	if !c.cscRefreshOnInvalidateEnabled() || c.cscRefreshQueue != nil {
		return
	}
	// Cheap fast path: if CSC serving is already known inactive, skip building
	// anything. Not the safety mechanism on its own — see the workersMu check
	// below, which is what actually closes the construction/teardown race.
	if a := c.cscActive; a != nil && !a.Load() {
		return
	}
	lc := c.csc.(*LocalCache)
	q := &cscRefreshQueue{
		ch:       make(chan cscRefreshTarget, cscRefreshQueueDepth),
		demandCh: make(chan uint64, 1),
	}
	// Refresh every invalidated Valid entry, regardless of how recently it was
	// read. cscInvalNoHorizon (-1) is less than any real lastAccessNs token, so
	// it marks everything hot, and it never changes.
	q.sinceToken.Store(cscInvalNoHorizon)

	h := &cscRevalidateHandle{stop: make(chan struct{}), done: make(chan struct{})}

	// publish attaches the queue to the invalidate handler (so pushes start
	// feeding it, joining any detached predecessor batcher's stop-drain into it
	// first) and launches the goroutine. Extracted so both the guarded and the
	// no-drain-handle path below run the exact same sequence.
	publish := func() {
		c.cscRefreshQueue = q
		c.cscRefreshHandle = h
		// The invalidate handler is what sees the pushes, so it owns the producer
		// end. Join the detached batcher (setRefreshQueue only detaches + signals)
		// so the attach is synchronous — the drain has fully applied before the
		// goroutine launches below.
		if ih := lookupInvalidateHandler(c.opt.PushNotificationProcessor); ih != nil {
			if b := ih.setRefreshQueue(q); b != nil {
				b.join()
			}
		}
		go c.runCSCRefresher(h, lc, q)
	}

	// Run publish under the drain handle's lock, checked against
	// workersTornDown (see cscDrainHandle, stopCSCRefresherAndCoalescer): a
	// concurrent teardown may already have consumed workersStopOnce —
	// reachable via the drainer's own self-disable tick reacting to an async
	// conn init's disableCSCServing, racing this construction (a bot review
	// flagged this). That teardown must not be handed a handle it will never
	// get another chance to stop, so decline to publish (and start nothing) if
	// it already ran. The WHOLE of publish runs inside the lock, not just the
	// field writes: teardown's body sets workersTornDown as its own first
	// action under this same lock, so holding it across setRefreshQueue's
	// batcher join and the goroutine launch guarantees teardown cannot observe
	// a published handle/coalescer whose goroutine isn't fully running yet — a
	// second bot review caught that the field-writes-only version still let
	// stopCSCRefresher block on a <-h.done that nothing had started yet
	// (bounded by however long the join took, not a permanent deadlock, but
	// unnecessary and worth just closing). dh is nil only for tests that call
	// this directly without a drain handle; there is nothing to race against in
	// that case.
	//
	// Safety of holding workersMu this long: publish only calls internal,
	// panic-free code (setRefreshQueue/join, a launch of our own
	// runCSCRefresher) — lookupInvalidateHandler's GetHandler is a plain
	// registry map lookup, never user code, because CSC requires the built-in
	// push processor (a custom one is rejected at init). A panic here would
	// leave workersMu held forever and deadlock every later teardown, so this
	// only holds because publish cannot panic. Lock order while held:
	// workersMu -> (invalidateHandler.mu acquired and released entirely inside
	// setRefreshQueue) -> block on the detached batcher's done. Nothing on
	// that path re-enters workersMu; keep it that way if either lock's scope
	// grows.
	if dh := c.cscDrainHandle; dh != nil {
		dh.workersMu.Lock()
		if !dh.workersTornDown {
			publish()
		}
		dh.workersMu.Unlock()
		return
	}
	publish()
}

func (c *baseClient) runCSCRefresher(h *cscRevalidateHandle, lc *LocalCache, q *cscRefreshQueue) {
	defer close(h.done)

	// The collection window. It starts (timer armed) when the first key of a
	// batch is collected and is NOT re-armed on later arrivals — a sliding reset
	// would never fire under continuous churn, leaving only the size cap. Flushed
	// by the timer, by demand, or by the size cap; whichever fires first.
	window := time.NewTimer(cscRefreshWindow)
	defer window.Stop()
	if !window.Stop() {
		<-window.C
	}
	windowArmed := false

	// pending is keyed by cache key so a key invalidated twice within one window
	// is refetched once. Owned solely by this goroutine.
	pending := make(map[string]cscRefreshTarget, cscRefreshWindowMaxKeys)
	var lastWarn time.Time

	collect := func(t cscRefreshTarget) {
		if _, dup := pending[t.cacheKey]; dup {
			return
		}
		pending[t.cacheKey] = t
		q.pendingSet.Store(t.cacheKey, struct{}{})
		if !windowArmed {
			windowArmed = true
			window.Reset(cscRefreshWindow)
		}
	}

	// flush applies the collected window. When stopping is true (Close's
	// stop-drain) it skips the refetch below entirely instead of running it:
	// stopCSCRefresherAndCoalescer's teardown (step 2 doc, invalidateAllCoverage,
	// csc_integration.go:1305-1316) evicts every entry attributed to this
	// client's refresh connection right after this call returns, regardless of
	// whether the refetch would have succeeded — so running it first only spends
	// network round trips on a result that is discarded a moment later. Against a
	// healthy server that cost used to go unbounded: nothing here ever fails, so
	// the previous "bail after the first failed chunk" bound (#3965 F2) never
	// triggered, and a full backlog could cost thousands of round trips — one per
	// chunk once the reply-byte budget shrinks each chunk to a single target
	// (codex #3989 P1). The abandoned targets stay evicted; a reader repopulates
	// them normally, the same outcome a failed refetch already produced. The
	// normal (non-stopping) path is unchanged: it refetches every chunk, only
	// aborting if Close begins while it's still running (see the h.stop check
	// below).
	flush := func(demand, stopping bool) {
		if windowArmed {
			windowArmed = false
			if !window.Stop() {
				// Drain a possibly-already-fired timer so the next Reset is clean.
				select {
				case <-window.C:
				default:
				}
			}
		}
		if len(pending) == 0 {
			return
		}
		if demand {
			q.demandFlushes.Add(1)
		}
		// Snapshot to a slice and clear the collection state before any network
		// I/O, so keys invalidated during the refetch start a fresh window rather
		// than being lost or double-counted.
		targets := make([]cscRefreshTarget, 0, len(pending))
		for k, t := range pending {
			targets = append(targets, t)
			q.pendingSet.Delete(k)
			delete(pending, k)
		}
		// Retire this window's generation: a demand nudge stamped with it (a reader
		// that passed signalDemand's pendingSet check just before the clear above,
		// then sends AFTER it) now compares unequal to the current generation and is
		// ignored by the demandCh case — a stale nudge can no longer flush the NEXT
		// window early, defeating the coalescing window and inflating DemandFlushes.
		q.demandGen.Add(1)
		// Then drop any nudge already buffered for THIS just-retired window: collect
		// runs only on this goroutine, so nothing can have stamped the new generation
		// yet, making a buffered value provably stale. Draining keeps the depth-1
		// buffer clear for the next window's first legit nudge (the generation check
		// above is the correctness guard; this stays buffer hygiene).
		select {
		case <-q.demandCh:
		default:
		}
		if stopping {
			// Abandoned by choice, not by network failure: leave RefreshFailed (one
			// count per errored ROUND TRIP, per its doc) alone. These targets simply
			// stay evicted, same as an errored refetch's targets already do.
			return
		}
		// A window can hold more than one round trip's worth; chunk it. Keys that
		// self-healed during the window (a reader missed and repopulated them) are
		// now Valid, so Reserve inside refreshInvalidatedBatch declines them for
		// free — no MGET slot is spent rewriting fresh data.
		// Recover inside a closure so a panic in refreshInvalidatedBatch (cache/RESP/
		// network path) does not kill the refresher goroutine — which would silently
		// and permanently degrade refresh-on-invalidate to plain eviction for the
		// client's lifetime. Mirrors the invalidation batcher's flush guard. The
		// per-round-trip deadline is created PER CHUNK below, not once for the whole
		// window, so a slow early chunk cannot expire later chunks' own budget.
		func() {
			defer func() {
				if r := recover(); r != nil {
					q.refreshFailed.Add(1)
					internal.Logger.Printf(context.Background(),
						"csc: refresh-on-invalidate batch panic (recovered): %v", r)
				}
			}()
			// Chunk by count (cscRefreshBatchMax), by serialized REQUEST bytes (the
			// write buffer), and by expected REPLY bytes (cscRefreshReplyBudgetBytes —
			// see its doc: the reply side, not the request side, is what jams a
			// flow-controlled path). Like the miss-coalescer, the refresh writes the
			// whole chunk before it reads the replies. The first target in a chunk
			// always goes, even if it alone exceeds a budget, so the loop always makes
			// progress. Boundary logic extracted pure (cscRefreshChunkEnd) and
			// unit-tested.
			writeBudget := cscMissWriteBatchBytes(c.opt)
			prefixLen := len(c.cscKeyPrefix)
			for start := 0; start < len(targets); {
				// Abort this (always non-stopping — see the stopping check above) flush
				// if Close begins while it's still running: continuing to give each
				// remaining chunk a fresh cscRefreshBatchTimeout against a dead/stalled
				// server would delay Close by minutes (with reply budgeting producing
				// one-target chunks, ~43 min for a full window). This covers a flush that
				// was ENTERED before Close and is still running when h.stop closes (#3965
				// F2 follow-up); the stop-drain's OWN flush call never reaches here at all
				// now. Bailed targets stay evicted — a reader repopulates them and this
				// client's coverage is revoked on Close anyway.
				select {
				case <-h.stop:
					return
				default:
				}
				end := cscRefreshChunkEnd(targets, start, prefixLen, writeBudget)
				// Per-chunk deadline: each round trip gets its own cscRefreshBatchTimeout,
				// so aggregate latency across chunks cannot expire the later chunks (#3989).
				rctx, rcancel := context.WithTimeout(context.Background(), cscRefreshBatchTimeout)
				n, err := c.refreshInvalidatedBatch(rctx, targets[start:end])
				rcancel()
				q.refreshed.Add(uint64(n))
				if err != nil {
					// One count per errored round trip: those keys were not refreshed and
					// stay evicted (a reader repopulates them). This is the signal that
					// refresh is degrading to plain eviction.
					q.refreshFailed.Add(1)
					if time.Since(lastWarn) > cscRefreshWarnEvery {
						lastWarn = time.Now()
						internal.Logger.Printf(context.Background(),
							"csc: refresh-on-invalidate batch failed: %v", err)
					}
				}
				start = end
			}
		}()
	}

	// drainQueue moves targets already waiting in q.ch into the window without
	// blocking, up to the window cap. Returns true when q.ch was empty (nothing
	// more to take), false when it stopped at the cap with the channel possibly
	// still holding more.
	drainQueue := func() (empty bool) {
		for len(pending) < cscRefreshWindowMaxKeys {
			select {
			case more := <-q.ch:
				collect(more)
			default:
				return true
			}
		}
		return false
	}

	for {
		select {
		case <-h.stop:
			// Pull everything buffered in q.ch (offered but not yet collected) and
			// hand it to flush, which (stopping==true) abandons it without a refetch
			// — see flush's doc: a refetch here would only be discarded by the
			// teardown that runs right after this join anyway. stopCSCRefresher
			// rebinds the handler away from this queue BEFORE signalling stop, so
			// nothing new arrives here, making this loop finite. Loop until q.ch is
			// fully drained rather than stopping after one window-cap-sized batch:
			// c.cscRefreshQueue is never nilled on stop (see stopCSCRefresher), so a
			// caller that keeps a reference to the closed *Client would otherwise
			// keep every undrained target's key bytes reachable through it — up to
			// cscRefreshQueueDepth entries at cscRefreshTargetMaxBytes each. Cheap
			// now that flush does no network I/O: at most
			// cscRefreshQueueDepth/cscRefreshWindowMaxKeys rounds.
			for {
				empty := drainQueue()
				flush(false, true)
				if empty {
					return
				}
			}

		case t := <-q.ch:
			collect(t)
			// Opportunistically take whatever else is already waiting so one wake
			// drains the backlog into the window.
			drainQueue()
			if len(pending) >= cscRefreshWindowMaxKeys {
				flush(false, false)
			}

		case g := <-q.demandCh:
			// A reader missed a key still in the window: the batch is being used, so
			// refetch it now instead of waiting out the window. Ignore a nudge stamped
			// with a retired generation — its window was already flushed, and acting on
			// it would flush the current (unrelated) window early (see signalDemand).
			if q.demandIsCurrent(g) {
				flush(true, false)
			}

		case <-window.C:
			flush(false, false)
		}
	}
}

// stopCSCRefresher joins the refresher goroutine.
func (c *baseClient) stopCSCRefresher() {
	h := c.cscRefreshHandle
	if h == nil {
		return
	}
	c.cscRefreshHandle = nil
	// Clear only OUR binding: a sibling client sharing this cache/processor may
	// have re-attached its own queue after ours, and an unconditional nil here
	// would sever the survivor's refresher (see clearRefreshQueue).
	if ih := lookupInvalidateHandler(c.opt.PushNotificationProcessor); ih != nil {
		// Join the detached batcher OUTSIDE h.mu (clearRefreshQueue only detaches +
		// signals): the Close path needs a synchronous, no-straggler teardown, and
		// joining under the handler lock would stall a sibling on the hot path.
		if b := ih.clearRefreshQueue(c.cscRefreshQueue); b != nil {
			b.join()
		}
	}
	h.signalStop()
	<-h.done
}

// cscRefreshReplyCacheable reports whether a refetched raw reply may be published
// as a fresh cache entry. It classifies the FULL reply (like the coalescer's
// classifyCachedReply), not raw[0]: a RESP3 attribute-prefixed error leads with
// RespAttr ('|'), so a first-byte error check would treat an attributed error as
// cacheable and publish it as a false success (bumping Refreshed; the next reader
// would then have to evict and refetch). A nil reply IS cacheable (a negative
// lookup, or a since-deleted key caching as missing); an empty reply never is.
// Extracted pure so the classification is unit-tested (like cscRefreshChunkEnd).
func cscRefreshReplyCacheable(raw []byte) bool {
	return len(raw) > 0 && isCacheableReplyResult(classifyCachedReply(raw))
}

// refreshInvalidatedBatch re-fetches one chunk of invalidated keys in a single
// round trip and publishes each reply as a fresh entry. Returns how many entries
// it published.
//
// Every reservation this takes is either published or released before returning.
// An abandoned reservation is worse than no refresh at all: LocalCache.Get WAITS
// on an in-progress entry, so one orphan blocks every reader of that key until
// StaleTimeout. (Measured elsewhere in this work: that mistake cost 30x and 8500x
// throughput in the intra-batch rewrites.)
func (c *baseClient) refreshInvalidatedBatch(ctx context.Context, targets []cscRefreshTarget) (int, error) {
	prefix := c.cscKeyPrefix
	if prefix == "" {
		return 0, nil
	}
	if a := c.cscActive; a != nil && !a.Load() {
		return 0, nil
	}

	// Reserve before touching the network, so only keys this batch owns are sent.
	// A declined reservation means a reader is already fetching it — leave it to
	// them rather than duplicating the work.
	kept := make([]cscRefreshTarget, 0, len(targets))
	// Arm the cancellation defer BEFORE the reservation loop. A user CacheSizer can
	// panic inside Reserve; the outer refresher recovery keeps the worker alive, so
	// without the defer already installed the targets reserved so far would stay
	// IN_PROGRESS and block every reader of those keys until StaleTimeout (#3989). Any
	// reservation still holding a token when this returns OR unwinds was never settled;
	// release it rather than leave a placeholder readers would block on.
	defer func() {
		for i := range kept {
			if kept[i].token != 0 {
				c.csc.Cancel(kept[i].cacheKey, kept[i].token)
			}
		}
	}()
	for _, t := range targets {
		token, shouldFetch := c.csc.Reserve(t.cacheKey, t.redisKeys)
		// token==0 with shouldFetch==true is Reserve's "fetch uncached" signal
		// (oversized entry / over-capacity / lost race), not an owned reservation.
		// Skipping it avoids spending an MGET slot on a reply we'd discard and, more
		// importantly, registering server-side tracking for a key we never cache
		// (which would manufacture one spurious future invalidation).
		if !shouldFetch || token == 0 {
			continue
		}
		t.token = token
		kept = append(kept, t)
	}
	if len(kept) == 0 {
		return 0, nil
	}

	published := 0
	// Publish on the MAIN pool (tracked). On this base the dedicated pipeline pool
	// is deliberately EXCLUDED from CLIENT TRACKING (PR #3959), so a refetch
	// published via withPipelineConn would be un-invalidatable and serve stale
	// until TTL. withConn lands on a CLIENT TRACKING ON connection, same as the
	// miss-coalescer.
	err := c.withConn(ctx, func(ctx context.Context, cn *pool.Conn) error {
		connID := cn.GetID()
		// Coverage generation captured BEFORE the reads: if this conn loses
		// tracking mid-batch, every reply is discarded rather than published, the
		// same discipline fulfillCached and revalidateBatch use.
		capturedGen := c.cscConnInitGen(connID)

		// Pass the refresher's internally-bounded ctx (5s) DIRECTLY, not
		// c.context(ctx): c.context applies the user's ContextTimeoutEnabled policy,
		// which — when disabled (the default) — swaps our ctx for context.Background,
		// stripping our own deadline. With ReadTimeout/WriteTimeout also disabled
		// (-1/-2) a stalled Redis would then hang this reader forever, and Close
		// (stopCSCRefresher waits on the refresher goroutine) would wedge. The
		// miss-coalescer passes its session ctx directly for the same reason.
		// Bound the write even when per-op timeouts are disabled. The refresh writes
		// the whole chunk before it reads, so a deadline-less flush the server cannot
		// drain (transport backpressure) would wedge the refresher: the 5s ctx does
		// not interrupt a deadline-less socket write. A positive timeout makes
		// WithWriter set a write deadline (still capped by the 5s ctx), so a stalled
		// flush fails the refresh and the entries degrade to plain eviction
		// (self-healing) instead of hanging the goroutine.
		writeTimeout := c.opt.WriteTimeout
		if writeTimeout <= 0 {
			writeTimeout = cscRefreshBatchTimeout
		}
		if err := cn.WithWriter(ctx, writeTimeout, func(wr *proto.Writer) error {
			for i := range kept {
				// The cache key is the namespaced RESP encoding of the command that
				// produced the entry; strip the namespace and it is already wire form.
				if _, err := wr.Write([]byte(kept[i].cacheKey[len(prefix):])); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}

		// Bound the read the same way as the write above: with per-op timeouts
		// disabled WithReader skips SetReadDeadline (options.go maps -2 to -1, which
		// WithReader treats as no deadline), so a stalled reply or push drain would
		// park refreshInvalidatedBatch forever — and stopCSCRefresher waits on this
		// goroutine, so Client.Close would never return. A positive deadline (still
		// capped by the 5s ctx) turns that into a timeout that fails the refresh.
		readTimeout := c.opt.ReadTimeout
		if readTimeout <= 0 {
			readTimeout = cscRefreshBatchTimeout
		}
		return cn.WithReader(ctx, readTimeout, func(rd *proto.Reader) error {
			for i := range kept {
				// Invalidation pushes share this connection with replies, so drain
				// them first or a push frame would be read as a value and cached.
				// Use the nonblocking Close adapter (like the miss-coalescer and
				// background drainer): this reader is part of the refresher's
				// waitgroup, so a custom push handler calling Close() on the raw
				// client would self-deadlock (Close waits on the very goroutine
				// parked in the handler). And PROPAGATE a processor error instead of
				// logging and continuing: a surfaced error means bytes may have been
				// consumed mid-frame, so reading the next reply on a desynced stream
				// could publish a push fragment under the wrong cache key — abort so
				// withConn retires the connection.
				if c.opt.Protocol == 3 && c.pushProcessor != nil {
					// Route through the shared helper in BLOCKING mode: block on the
					// socket and skip pushes until the refetch reply is the next frame, so
					// a second invalidation still on the socket ahead of the reply is not
					// read by ReadRawReply below and published under the wrong cache key.
					// (The Buffered variant stopped at buffer-empty and let such a
					// socket-pending push through.) A custom processor is handed only a
					// confirmed push frame (peek first); PeekReplyType is attribute-aware,
					// so a fragmented attribute prefix is handled without a separate
					// buffered scan. The helper sets no read deadline, so this reader's
					// ReadRawReply is unaffected.
					if err := c.drainPushFrames(ctx, cn, rd, true); err != nil {
						internal.Logger.Printf(ctx, "csc: refresh push drain: %v", err)
						return err
					}
				}
				raw, err := rd.ReadRawReply()
				if err != nil {
					// The reply stream is now out of step with kept; abort so the
					// connection is retired rather than reused mid-batch. The deferred
					// release cancels every reservation still outstanding.
					return err
				}
				if !cscRefreshReplyCacheable(raw) {
					// Not cacheable (WRONGTYPE after a type change, NOPERM, ...). A nil
					// reply is NOT an error: a negative lookup is cacheable, and a key
					// that has since been deleted should cache as missing. See
					// cscRefreshReplyCacheable: it classifies the FULL frame, so a RESP3
					// attribute-prefixed error is not mis-read as cacheable.
					c.csc.Cancel(kept[i].cacheKey, kept[i].token)
					kept[i].token = 0
					continue
				}
				fc := &cscFetchCapture{
					raw:     raw,
					connID:  connID,
					initGen: capturedGen,
					key:     kept[i].cacheKey,
					token:   kept[i].token,
				}
				// Keep the invalidated entry's recency: a background refresh is not a
				// reader read, so it must not renew the key's refresh eligibility (see
				// stageRefreshAccess). It is staged on the reservation so the publish
				// applies it atomically. Refresh runs only with the built-in
				// *LocalCache; the hook path fulfills that same cache.
				if lc, ok := c.csc.(*LocalCache); ok {
					lc.stageRefreshAccess(kept[i].cacheKey, kept[i].token, kept[i].accessNs, kept[i].read)
				}
				if c.fulfillCached(kept[i].cacheKey, kept[i].token, fc) {
					published++
				}
				// fulfillCached cancels on its own failure paths, so the token is
				// settled either way.
				kept[i].token = 0
			}
			return nil
		})
	})
	return published, err
}
