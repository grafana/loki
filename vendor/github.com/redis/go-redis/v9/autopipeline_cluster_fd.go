package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// clusterFDRouter runs the ordered full-duplex autopipeline natively on a
// *ClusterClient.
//
// Full-duplex needs one held connection with a writer/reader goroutine pair
// (fdEngine), which only a standalone *Client with a dedicated pipeline pool can
// provide — a *ClusterClient has none of its own. But every master node's
// node.Client IS a standalone *Client that gets a pipeline pool by default
// (redis.go creates it whenever PipelinePoolSize >= 0, and osscluster.go passes
// that option through to each node). So instead of the half-duplex shard
// flushers, the router keeps one FD child autopipeliner per master node and
// routes each command to the child that owns its slot. The parent AutoPipeliner
// keeps diversion (blocking / fan-out / ReqSpecial) on the *ClusterClient, so
// cluster-wide commands still fan out and aggregate correctly.
//
// Children are created lazily on first use for a node and cached on the
// node.Client itself (via its AutoPipeline getters), so this router shares one
// child instance per node with anything else that asks that node client for an
// autopipeliner.
//
// Routing uses the live cluster state on every submit, so a stable topology
// routes correctly. Redirects are handled too: a child's FD engine surfaces a
// MOVED/ASK reply to the router's injected reprocess function (childCfg.
// clusterReprocess), which re-runs the command through the redirect-aware
// ClusterClient (cc.process) — MOVED is routed to the target node with a topology
// reload; ASK is followed through cc.process's own redirect loop (which issues
// ASKING). So a slot migration is followed rather than surfaced as an error.
type clusterFDRouter struct {
	parent   *AutoPipeliner
	cc       *ClusterClient
	blocking bool
	// childCfg is the standalone FD config every node child is built with,
	// derived once from the parent's config. Reused verbatim so the node
	// client's "first getter call wins" cache always sees the same config.
	childCfg *AutoPipelineOptions

	mu       sync.RWMutex
	closed   bool
	children map[*Client]*AutoPipeliner
	// evictHooks records every node client this router registered a
	// clusterFDRouterEvict close hook on (see getOrCreateChild), so close can
	// unregister them. Guarded by mu. Tracked separately from children because
	// the stale-child sweep and the hook itself drop children entries while the
	// registration on the node client stays live.
	evictHooks map[*Client]struct{}
}

// clusterFDRouterEvictID is the onClose registry id of this router's evict
// hook on node client nc. Deterministic per (router, node) so a re-register
// replaces rather than accumulates, and so close can address it.
func clusterFDRouterEvictID(r *clusterFDRouter, nc *Client) string {
	return fmt.Sprintf("clusterFDRouterEvict#%p#%p", r, nc)
}

func newClusterFDRouter(parent *AutoPipeliner, cc *ClusterClient, cfg *AutoPipelineOptions, blocking bool) *clusterFDRouter {
	// Derive the per-node child config from the parent's: force the ordered
	// single-shard full-duplex combo the fdOn gate requires, carry the caller's
	// sizing knobs (MaxBatchSize, FullDuplexWindow, MaxFlushDelay, ...) as-is, and
	// strip contentSharded — a cluster-only bit that must not leak onto a
	// standalone node child.
	child := *cfg
	child.FullDuplex = true
	child.Unordered = false
	child.MaxConcurrentBatches = 1
	child.NumShards = 1
	child.contentSharded = false
	// Redirect handling: each child's FD engine re-runs a MOVED/ASK (or retryable)
	// reply through the redirect-aware ClusterClient instead of its own node-local
	// standalone path. cc.process is the cluster redirect loop (MOVED -> target node
	// + LazyReload, ASK -> followed via its own loop with ASKING, bounded by
	// MaxRedirects). One fn serves every node: the redirect target is resolved from
	// the reply, not the source node. startAttempt/writtenAt are unused here —
	// cc.process owns its own MaxRedirects budget.
	child.clusterReprocess = func(ctx context.Context, cmd Cmder, _ int, _ time.Time) error {
		return cc.process(ctx, cmd)
	}
	// Connection-failure recovery budget for each child engine. A node.Client
	// normalizes MaxRetries to -1 (cluster retries live in MaxRedirects), which the
	// FD carry-replay would read as "budget already spent" and fail every in-flight
	// command on the first socket error. Give the child the cluster's MaxRedirects
	// so a transient node blip replays the unacked tail on a fresh connection.
	child.clusterRetryBudget = cc.opt.MaxRedirects
	return &clusterFDRouter{
		parent:   parent,
		cc:       cc,
		blocking: blocking,
		childCfg: &child,
		children: make(map[*Client]*AutoPipeliner),
	}
}

// submit routes cmd to the FD child that owns its slot. It is called from
// AutoPipeliner.submit AFTER diversion and preflight have been decided, so cmd
// is a batchable single-node command. On any routing miss (keyless command,
// unresolved slot, topology not loaded, or a node whose client turns out not to
// be FD-capable) it falls back to the normal cluster Process path — correct,
// just not pipelined.
func (r *clusterFDRouter) submit(ctx context.Context, cmd Cmder) AutoFuture {
	// Mirror enqueue's closed contract: reject once the parent is closing rather
	// than route to a child that Close is tearing down.
	if r.parent.isClosed() {
		return r.rejectClosed(cmd)
	}

	child, closed := r.childFor(ctx, cmd)
	if closed {
		// close() ran between the parent.isClosed() check above and here (the router
		// is tearing down). Mirror the closed contract rather than route to — or
		// create — a child the drain has already swept; getOrCreateChild discards any
		// child it built in this window so it cannot leak.
		return r.rejectClosed(cmd)
	}
	if child == nil {
		return r.divertToProcess(ctx, cmd)
	}
	// Submit straight to the child's FD engine, skipping child.submit
	// (AutoPipeliner.submit). The parent already decided this command is
	// pipelineable — diversion (readTimeout/runsOutsidePipeline/isBlockingCmd/
	// HIMPORT/mustDivert) and preflight ran above — so the child's submit would only
	// re-run that same classification, hit its nil preflight, and build a finish
	// closure: pure per-command CPU on the hot path (measured ~1/3 of submit cost is
	// this second pass). child.fd is non-nil (getOrCreateChild screened it).
	b := child.fd.submit(ctx, cmd)
	if b == completedBatch && errors.Is(cmd.Err(), ErrClosed) {
		// Topology GC (a cluster reload dropping this node, or the node's own pool
		// close hook) can close this child at any point up to and including while
		// fd.submit is parked on a full fd.ch waiting for room — the fd.ap.ctx.Done()
		// / fd.closed arms in fdEngine.submit. Those keep the race SAFE (ErrClosed,
		// no hang), but silently failing the command instead of falling back
		// contradicts this router's own contract (see the doc comment above): a
		// routing miss should divert to Process, not error. Distinguish from the
		// CALLER's own ctx cancelling mid-backpressure (fdEngine.submit's separate
		// ctx.Done() arm, which sets ctx.Err(), not ErrClosed) — that one must NOT
		// be retried; the caller asked to stop.
		//
		// Neither setReady (below) nor any waiter has observed cmd yet — child.fd's
		// rejection ran on this goroutine, before the async face is armed — so it is
		// still safe to override. Re-resolve once: childFor rebuilds a fresh child
		// for a node still in the topology (self-healing, same as its own
		// cached-but-closed handling in getOrCreateChild), reports a genuine miss
		// for a node that is gone, or reports the ROUTER itself closing (in which
		// case rejectClosed below is the same outcome this rejection would have
		// been anyway). One retry only, no loop: SetErr on the second attempt
		// (accept, or fail again) simply overwrites this one.
		child, closed = r.childFor(ctx, cmd)
		if closed {
			return r.rejectClosed(cmd)
		}
		if child == nil {
			return r.divertToProcess(ctx, cmd)
		}
		// Clear the stale ErrClosed from the first attempt before retrying: a
		// live engine's OWN synchronous rejection paths (lease failure, limiter
		// deny, budget exhaustion) only stamp their real error when rawErr() is
		// nil, so without this reset a second, different failure would leave the
		// first attempt's ErrClosed in place and misreport a live engine as
		// closed. A successful retry is unaffected either way — the reader's
		// inline completion unconditionally overwrites cmd's error with the
		// reply outcome.
		cmd.SetErr(nil)
		b = child.fd.submit(ctx, cmd)
	}
	if !r.blocking {
		cmd.setReady(b)
	}
	return AutoFuture{cmd: cmd, batch: b}
}

// rejectClosed mirrors the parent AutoPipeliner's closed-submit contract: fail
// cmd with ErrClosed and, on the async face, mark it ready against the shared
// completedBatch sentinel immediately (no host goroutine, nothing to wait on).
func (r *clusterFDRouter) rejectClosed(cmd Cmder) AutoFuture {
	cmd.SetErr(ErrClosed)
	if !r.blocking {
		cmd.setReady(completedBatch)
	}
	return AutoFuture{cmd: cmd, batch: completedBatch}
}

// divertToProcess routes cmd through the parent's normal (non-FD) diverted
// path — correct, just not pipelined. runOutsidePipeline sets the command
// ready itself on the deferred face and returns a batch that completes when
// the command has executed.
func (r *clusterFDRouter) divertToProcess(ctx context.Context, cmd Cmder) AutoFuture {
	return AutoFuture{cmd: cmd, batch: r.parent.runOutsidePipeline(ctx, cmd)}
}

// childFor resolves the FD child for cmd's owning master node. It returns
// (nil, false) when the command should divert to Process (keyless, unresolved
// slot, topology not loaded, or a non-FD node), and (nil, true) when the router
// is closing (submit must then reject with ErrClosed rather than divert).
func (r *clusterFDRouter) childFor(ctx context.Context, cmd Cmder) (*AutoPipeliner, bool) {
	slot := r.cc.cmdSlot(cmd, -1)
	if slot < 0 {
		// Keyless command: no single owning node. Let it run through Process, which
		// applies the configured ShardPicker.
		return nil, false
	}
	state, err := r.cc.state.Get(ctx)
	if err != nil {
		return nil, false
	}
	node, err := state.slotMasterNode(slot)
	if err != nil || node == nil {
		return nil, false
	}
	return r.getOrCreateChild(node.Client)
}

// getOrCreateChild returns the FD child autopipeliner for a node client,
// creating it on first use. The second return value reports that the router is
// closed. It returns (nil, false) when the node client is not FD-capable (its
// child engine did not engage) so the caller diverts that node to Process, and
// (nil, true) when the router has been closed (the caller must reject, not
// divert — the drain has already swept the children).
func (r *clusterFDRouter) getOrCreateChild(nc *Client) (*AutoPipeliner, bool) {
	r.mu.RLock()
	ch := r.children[nc]
	closed := r.closed
	r.mu.RUnlock()
	if closed {
		return nil, true
	}
	if ch != nil && !ch.IsClosed() {
		return ch, false
	}

	// Build (or fetch the node client's cached) child WITHOUT holding the router
	// lock: the node getter is idempotent (first call wins, cached on nc, and it
	// rebuilds when its cached instance is closed — see getOrCreateAutoPipeliner),
	// so a concurrent build just returns the same live instance. Keeping the
	// getter off r.mu means a non-FD node (child.fd == nil) does not serialize
	// every later submit on the write lock re-calling it.
	var (
		child *AutoPipeliner
		err   error
	)
	if r.blocking {
		child, err = nc.AutoPipelineWithOptions(r.childCfg)
	} else {
		child, err = nc.AsyncAutoPipelineWithOptions(r.childCfg)
	}
	// Honesty check: only treat this node as an FD node if the engine actually
	// engaged, is live, AND is redirect-aware. A node client without a pipeline pool
	// (PipelinePoolSize < 0) falls back to half-duplex (child.fd == nil); a
	// just-closed instance must not be cached (fd is not nilled on close). The
	// redirectAware requirement guards the first-call-wins node getter: application
	// code may have already created a plain standalone autopipeliner on this
	// node.Client (e.g. via ForEachMaster), and the getter would hand that instance
	// back. It has no clusterReprocess, so a MOVED/ASK reply on it would be surfaced
	// to the caller instead of routed through the cluster client. Divert all of
	// these to Process rather than dispatch through an engine that cannot follow
	// redirects.
	if err != nil || child == nil || child.fd == nil || child.IsClosed() || !child.fd.redirectAware {
		return nil, false
	}

	r.mu.Lock()
	if r.closed {
		// close() ran while we built this child outside the lock: it already
		// snapshotted+cleared the map, so storing ours would leak a held FD conn and
		// its writer/reader goroutines (nothing would ever close it). Discard it.
		// Close AFTER releasing the lock so a concurrent close() draining the
		// snapshot does not serialize behind this child's own drain (Close blocks on
		// it). Close is idempotent, so double-closing a node-client-cached instance
		// close() also holds is harmless.
		r.mu.Unlock()
		_ = child.Close()
		return nil, true
	}
	// Evict stale entries for OTHER node clients while the write lock is
	// already held: topology GC can close a node client (and its cached
	// child) while this router stays up, and once the cluster's slot map
	// stops pointing at that *Client, getOrCreateChild is never called with
	// it again — nothing else would notice it went stale. Without this sweep
	// a closed child (and its FD engine, submit channel, held connection)
	// sits retained in this map for the router's whole life on a cluster that
	// scales down or replaces node addresses (cursor bugbot on #4002). The
	// child is already closed (its own node-client close hook got there
	// first), so this only drops the reference for GC — no Close() needed.
	for k, v := range r.children {
		if k != nc && v.IsClosed() {
			delete(r.children, k)
		}
	}
	// Prefer a live child another goroutine cached first; otherwise store ours.
	if cached := r.children[nc]; cached != nil && !cached.IsClosed() {
		child = cached
	} else {
		// Prune THIS node's entry the moment nc itself closes (topology GC, or
		// any other Close), instead of relying solely on the sweep above: that
		// sweep only runs when some OTHER node's cache miss takes this same
		// write-lock path, so a cluster whose live submits only ever hit
		// already-cached nodes would never prune a removed node's entry (cursor
		// bugbot on #4002 — a gap in the sweep it sits next to). nc.onClose is
		// the same registry AutoPipelineWithOptions above already wired the
		// child's own drain to.
		//
		// Register BEFORE publishing the child, and under r.mu. The hook takes
		// r.mu, so a node close racing this section resolves one of two ways:
		// its run snapshot was taken before this register, in which case
		// register reports false (the hook would never fire) and the node is
		// already closed — discard the child and divert; or the snapshot comes
		// later and the hook blocks on r.mu until this store is published, then
		// deletes it. Registering after the unlock left a window where the
		// close ran in between and the entry stayed for the router's life
		// (cursor bugbot on #4002). The id is deterministic per (router, node),
		// so a rebuild for this node replaces the same closure.
		if !nc.onClose.register(clusterFDRouterEvictID(r, nc), func() error {
			r.mu.Lock()
			delete(r.children, nc)
			delete(r.evictHooks, nc)
			r.mu.Unlock()
			return nil
		}) {
			r.mu.Unlock()
			_ = child.Close()
			return nil, false
		}
		if r.evictHooks == nil {
			r.evictHooks = make(map[*Client]struct{})
		}
		r.evictHooks[nc] = struct{}{}
		r.children[nc] = child
	}
	r.mu.Unlock()
	return child, false
}

// len sums the pending backlog across all node children, for AutoPipeliner.Len.
func (r *clusterFDRouter) len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	total := 0
	for _, ch := range r.children {
		total += ch.Len()
	}
	return total
}

// close closes every node child and waits for each drain to finish. Called from
// the parent's drain before the parent's own (empty) shard sweep, and before
// clusterNodes closes the node clients — so each child flushes its accepted
// commands on the still-open node connection. A child may already have been
// closed by its node client's shared-pool close hook; AutoPipeliner.Close is
// idempotent, so the second close returns immediately.
func (r *clusterFDRouter) close() error {
	r.mu.Lock()
	// Mark closed under the lock BEFORE snapshotting: a submit racing past the
	// parent.isClosed() check that then builds a child outside the router lock will
	// see r.closed when it takes the lock to store, and discard its orphan instead
	// of leaking it (getOrCreateChild). Children created and stored before this
	// point are in the snapshot below and get closed here.
	r.closed = true
	children := make([]*AutoPipeliner, 0, len(r.children))
	for _, ch := range r.children {
		children = append(children, ch)
	}
	r.children = make(map[*Client]*AutoPipeliner)
	// Detach this router's evict hooks from every node client it registered on
	// — not just the nodes still in children (the sweep and the hook itself drop
	// entries while the registration stays live). Left registered, each hook
	// keeps the closed router, and through parent the AutoPipeliner and its
	// clusterReprocess closure, reachable until that node client itself closes,
	// and a cluster that cycles autopipeliners accumulates one per cycle per
	// node (codex + cursor bugbot on #4002). Snapshot under the lock, unregister
	// outside it: unregister takes only the registry's own mutex.
	hooked := make([]*Client, 0, len(r.evictHooks))
	for nc := range r.evictHooks {
		hooked = append(hooked, nc)
	}
	r.evictHooks = nil
	r.mu.Unlock()
	for _, nc := range hooked {
		nc.onClose.unregister(clusterFDRouterEvictID(r, nc))
	}

	var firstErr error
	for _, ch := range children {
		// WaitClosed is the authoritative drain result: Close returns the drain error
		// only to the caller that WINS the close CAS, and returns nil to a loser. If
		// application code already started Close on this node autopipeliner (exposed
		// via ForEachMaster), our ch.Close() loses and returns nil while the real
		// error surfaces here. So prefer WaitClosed's result and fall back to Close's.
		cerr := ch.Close()
		if werr := ch.WaitClosed(); werr != nil {
			cerr = werr
		}
		if cerr != nil && firstErr == nil {
			firstErr = cerr
		}
	}
	return firstErr
}
