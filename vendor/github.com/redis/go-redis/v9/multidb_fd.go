package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	imultidb "github.com/redis/go-redis/v9/internal/multidb"
)

// multidbFDRouter runs the ordered full-duplex autopipeline on a
// *MultiDBClient.
//
// A MultiDBClient holds no connection of its own; its members do. The router
// keeps one full-duplex child autopipeliner per member — a standalone FD
// engine on a *Client member, a cluster FD router on a *ClusterClient member —
// and sends each command to the ACTIVE member's child. The parent
// AutoPipeliner keeps diversion (blocking, fan-out, HIMPORT, routeCommand), so
// only batchable commands reach here.
//
// MultiDB semantics on the fast path:
//   - Admission: a command rides FD only on a closed-state breaker admission
//     with the failure detector not tripped. Anything else — no active
//     member, a tripped detector, an open circuit, a half-open probe — goes to
//     MultiDB's normal path (core.process), which owns failover and probes.
//   - Accounting: each child reports every command's outcome (fdOutcome) and
//     the router settles it into the member's breaker and the failure
//     detector, as core.process does. The breaker episode and the detector
//     window are the ones the command was admitted into (fdOutcomeToken), so
//     an outcome that outlives a reselect of the same member does not count
//     into the fresh window.
//   - Recovery: a child whose connection drops, or whose member cannot be
//     leased, reroutes its replayable work through MultiDB's pipeline path
//     (fdReroute), which re-gates and fails over to the newly active member.
//     A sent NoRetry command is still failed, never re-sent.
//
// Children are built privately (not through the member's cached autopipeline
// getters), so their owner wiring is always present.
type multidbFDRouter struct {
	parent   *AutoPipeliner
	c        *MultiDBClient
	blocking bool
	// childCfg is the base config every child is built from: the parent's,
	// forced to the ordered single-engine full-duplex combo.
	childCfg AutoPipelineOptions

	mu       sync.RWMutex
	closed   bool
	children map[*multidbDatabase]*AutoPipeliner
	// noFD holds members where full duplex does not engage (no pipeline pool,
	// replica routing on a cluster member, or a build error): their commands
	// divert without rebuilding a child each time.
	noFD map[*multidbDatabase]struct{}
	// retired holds closed children taken out of children before their drain
	// finished (a lane switch closes a child without removing it), so
	// dropChild and close still wait for those drains.
	retired map[*multidbDatabase][]*AutoPipeliner

	// Async face only: one ordered lane at a time (see submitOrdered). lane is
	// the autopipeliner commands go to now: the active member's FD child, or hd,
	// an ordered half-duplex MultiDB autopipeliner for commands that cannot
	// take the fast path. laneSem (a 1-slot semaphore, so a submit can give up
	// on its context while a drain holds it) serializes lane switches and hd
	// writes; hd is atomic so len can read it without waiting out a lane
	// switch's drain.
	lane    atomic.Pointer[AutoPipeliner]
	laneSem chan struct{}
	hd      atomic.Pointer[AutoPipeliner]
	hdCfg   AutoPipelineOptions

	// removeHooks holds the members this router registered an onRemove hook
	// on (see childFor), so close can unregister them. Guarded by mu.
	removeHooks map[*multidbDatabase]struct{}
}

func newMultiDBFDRouter(parent *AutoPipeliner, c *MultiDBClient, cfg *AutoPipelineOptions, blocking bool) *multidbFDRouter {
	child := *cfg
	child.FullDuplex = true
	child.Unordered = false
	child.MaxConcurrentBatches = 1
	child.NumShards = 1
	child.FullDuplexPipes = 1 // ordered single engine per member
	child.contentSharded = false
	child.clusterReprocess = nil
	child.clusterRetryBudget = 0
	hd := *cfg
	hd.FullDuplex = false
	hd.contentSharded = false
	hd.clusterReprocess = nil
	hd.clusterRetryBudget = 0
	return &multidbFDRouter{
		parent:   parent,
		c:        c,
		blocking: blocking,
		childCfg: child,
		children: make(map[*multidbDatabase]*AutoPipeliner),
		noFD:     make(map[*multidbDatabase]struct{}),
		retired:  make(map[*multidbDatabase][]*AutoPipeliner),
		laneSem:  make(chan struct{}, 1),
		hdCfg:    hd,
	}
}

// lockLane takes the lane semaphore unconditionally.
func (r *multidbFDRouter) lockLane() { r.laneSem <- struct{}{} }

func (r *multidbFDRouter) unlockLane() { <-r.laneSem }

// submit routes cmd to the active member's FD child, or diverts it to
// MultiDB's normal path. Called from AutoPipeliner.submit after diversion.
func (r *multidbFDRouter) submit(ctx context.Context, cmd Cmder) AutoFuture {
	if !r.blocking {
		return r.submitOrdered(ctx, cmd)
	}
	child, token, reject := r.target()
	if reject {
		return r.rejectClosed(cmd)
	}
	if child == nil {
		return r.divert(ctx, cmd)
	}
	fut := child.submitToken(ctx, cmd, token)
	if fut.batch == completedBatch && errors.Is(cmd.Err(), ErrClosed) {
		// The member was removed or its child closed under us: MultiDB's path
		// routes the command to the live active member.
		cmd.SetErr(nil)
		return r.divert(ctx, cmd)
	}
	return fut
}

// target admits a command: the active member's FD child when the member is
// closed-admitted and the detector has not tripped, nil when the command must
// take MultiDB's normal path, reject when the client is closed. It also
// returns the admission token the outcome must settle into, so a command that
// outlives a reselect of this same member does not count into the fresh
// breaker episode and detector window.
func (r *multidbFDRouter) target() (child *AutoPipeliner, token fdOutcomeToken, reject bool) {
	if r.parent.isClosed() {
		return nil, fdOutcomeToken{}, true
	}
	core := r.c.core
	if core.closed.Load() {
		return nil, fdOutcomeToken{}, true
	}
	db, _ := core.activeSnapshot()
	if db == nil || core.shouldFailoverSafely() {
		return nil, fdOutcomeToken{}, false
	}
	// Sample the detector window before the breaker reservation, as
	// core.process does: a reselect landing between the two then leaves an old
	// detector generation, so the outcome is conservatively dropped instead of
	// polluting the fresh window.
	detectorGen := core.detectorGen.Load()
	// Read the closed admission without reserving a probe slot. Only a closed
	// breaker takes the FD path. Half-open probes (and the open -> half-open
	// transition) belong to MultiDB's path, and a reserve-then-release here
	// would hold a probe slot that a concurrent probe could then not get. The
	// reservation still carries the closed episode's generation, which is what
	// the outcome settles against.
	res, closed := db.cb.CurrentClosedReservation()
	if !closed {
		return nil, fdOutcomeToken{}, false
	}
	child, reject = r.childFor(db)
	if child == nil {
		return nil, fdOutcomeToken{}, reject
	}
	return child, fdOutcomeToken{res: res, detectorGen: detectorGen}, false
}

// submitOrdered is the async face's submit. The ordered async face promises
// submit order, so commands go to ONE lane at a time — the active member's FD
// child, or the ordered half-duplex MultiDB autopipeliner (hd) when the fast
// path is not available — and switching lanes first drains the old one: its
// Close completes every accepted command, in order (a dead member's backlog
// reroutes through MultiDB), before any command reaches the new lane. Lane
// switches happen on failover, fallback and breaker transitions; the switch
// stalls submitters for the drain.
//
// A retryable reply (READONLY, LOADING, ...) on a child is retried through
// MultiDB. The child reroutes such replies on one worker, in reply order, and
// a later tail reroute or Close-time flush waits for them, so retried
// commands keep submit order. As with pipeline retries, a later command that
// already ran on the first member (or settled with its budget spent)
// completes before an earlier command's retry.
//
// Fast path: the lane picked now is the current lane, so submit without
// laneSem. Anything else (a lane switch, a lane closed under the command) takes
// submitLocked, which picks, switches and submits under laneSem, so no caller
// can act on a stale pick.
func (r *multidbFDRouter) submitOrdered(ctx context.Context, cmd Cmder) AutoFuture {
	want, token, reject := r.target()
	if reject {
		return r.rejectClosed(cmd)
	}
	if want == nil {
		want = r.hd.Load()
	}
	if want != nil && r.lane.Load() == want {
		fut := want.submitToken(ctx, cmd, token)
		if fut.batch != completedBatch || !errors.Is(cmd.Err(), ErrClosed) {
			return fut
		}
		// The lane closed under this command (a switch by another caller):
		// pick again under laneSem.
		cmd.SetErr(nil)
	}
	return r.submitLocked(ctx, cmd)
}

// fdLaneAttempts bounds submitLocked's retries when the picked lane keeps
// closing under the command.
const fdLaneAttempts = 4

// submitLocked picks the lane, switches to it and submits cmd, all under
// laneSem. If no lane can take the command (no hd lane could be built, or the
// picked lane kept closing), it drains the current lane, runs cmd on
// MultiDB's normal path and waits for it, still under laneSem: every command
// accepted before it has completed by then, and none after it can start
// first, so the fallback keeps submit order.
//
// Waiting for laneSem honors ctx: a drain can hold it for seconds during a
// failover. A command whose ctx ends first was never submitted and completes
// with the context error.
func (r *multidbFDRouter) submitLocked(ctx context.Context, cmd Cmder) AutoFuture {
	select {
	case r.laneSem <- struct{}{}:
	default:
		select {
		case r.laneSem <- struct{}{}:
		case <-ctx.Done():
			cmd.SetErr(ctx.Err())
			cmd.setReady(completedBatch)
			return AutoFuture{cmd: cmd, batch: completedBatch}
		}
	}
	defer r.unlockLane()
	for attempt := 0; ; attempt++ {
		want, token, reject := r.target()
		if !reject && want == nil {
			want, reject = r.hdLaneLocked()
			token = fdOutcomeToken{}
		}
		if reject {
			return r.rejectClosed(cmd)
		}
		if want == nil || attempt >= fdLaneAttempts {
			// Drain first, then wait for the diverted command itself while
			// laneSem is held: the async divert runs on its own goroutine, and a
			// later command must not reach a new lane before it completes.
			r.drainLaneLocked()
			fut := r.divert(ctx, cmd)
			<-fut.batch.done
			return fut
		}
		if r.lane.Load() != want {
			r.drainLaneLocked()
			r.lane.Store(want)
		}
		fut := want.submitToken(ctx, cmd, token)
		if fut.batch != completedBatch || !errors.Is(cmd.Err(), ErrClosed) {
			return fut
		}
		cmd.SetErr(nil)
	}
}

// hdLaneLocked returns the ordered half-duplex lane, building it on first
// use. reject is true when the router is closed. laneSem MUST be held.
func (r *multidbFDRouter) hdLaneLocked() (ap *AutoPipeliner, reject bool) {
	if r.isClosed() {
		return nil, true
	}
	if hd := r.hd.Load(); hd != nil && !hd.IsClosed() {
		return hd, false
	}
	cfg := r.hdCfg
	hd, err := r.c.newMultiDBAutoPipeliner(&cfg, false)
	if err != nil {
		return nil, false
	}
	r.hd.Store(hd)
	return hd, false
}

// drainLaneLocked closes the current lane and waits for its drain: every
// command it accepted completes (or is rerouted through MultiDB) before this
// returns. A closed member child is rebuilt by childFor on its next use; a
// closed hd by hdLaneLocked. laneSem MUST be held.
func (r *multidbFDRouter) drainLaneLocked() {
	cur := r.lane.Load()
	if cur == nil {
		return
	}
	_ = cur.Close()
	_ = cur.WaitClosed()
	r.hd.CompareAndSwap(cur, nil)
	r.lane.Store(nil)
}

func (r *multidbFDRouter) isClosed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.closed
}

func (r *multidbFDRouter) rejectClosed(cmd Cmder) AutoFuture {
	cmd.SetErr(ErrClosed)
	if !r.blocking {
		cmd.setReady(completedBatch)
	}
	return AutoFuture{cmd: cmd, batch: completedBatch}
}

// divert runs cmd on MultiDB's normal path (MultiDBClient.Process ->
// core.process), outside the pipeline.
func (r *multidbFDRouter) divert(ctx context.Context, cmd Cmder) AutoFuture {
	return AutoFuture{cmd: cmd, batch: r.parent.runOutsidePipeline(ctx, cmd)}
}

// childFor returns db's FD child, building it on first use. It returns
// (nil, false) when the member cannot run full duplex (divert), and
// (nil, true) when the router is closed (reject).
func (r *multidbFDRouter) childFor(db *multidbDatabase) (*AutoPipeliner, bool) {
	r.mu.RLock()
	child, closed := r.children[db], r.closed
	_, noFD := r.noFD[db]
	r.mu.RUnlock()
	if closed {
		return nil, true
	}
	if child != nil && !child.IsClosed() {
		return child, false
	}
	if noFD {
		return nil, false
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, true
	}
	if child := r.children[db]; child != nil && !child.IsClosed() {
		return child, false
	}
	// Drop closed children while the write lock is held anyway (a lane switch
	// closed them). One still draining moves to retired: IsClosed is true
	// before the drain ends, and dropChild must still wait for it before
	// RemoveDatabase closes the member's client. A removed member's open
	// child is left alone: its onRemove hook (dropChild) drains it.
	r.pruneRetiredLocked()
	for m, ch := range r.children {
		if ch.IsClosed() {
			delete(r.children, m)
			if !ch.drained() {
				r.retired[m] = append(r.retired[m], ch)
			}
		}
	}
	if db.removed.Load() {
		return nil, false
	}
	if _, ok := r.noFD[db]; ok {
		return nil, false
	}
	// Tie the entry (child or noFD mark) to the member's removal before
	// building it, under r.mu. A building child already runs its engine on
	// the member's pool, so it must not exist once the removal ran. The hook
	// takes r.mu: a removal racing this either has already run (register
	// reports false, nothing is built) or waits and drains the published
	// entry.
	if !r.registerRemoveLocked(db) {
		return nil, false
	}
	child, err := r.newChild(db)
	if err != nil || child == nil {
		r.noFD[db] = struct{}{}
		return nil, false
	}
	r.children[db] = child
	return child, false
}

// registerRemoveLocked registers this router's onRemove hook on db (dropChild,
// which clears both the child and the noFD mark). It reports false when db's
// removal already ran. r.mu MUST be held.
func (r *multidbFDRouter) registerRemoveLocked(db *multidbDatabase) bool {
	if !db.onRemove.register(r.removeHookID(), func() error {
		r.dropChild(db)
		return nil
	}) {
		return false
	}
	if r.removeHooks == nil {
		r.removeHooks = make(map[*multidbDatabase]struct{})
	}
	r.removeHooks[db] = struct{}{}
	return true
}

// pruneRetiredLocked drops retired children whose drain finished. r.mu MUST
// be held.
func (r *multidbFDRouter) pruneRetiredLocked() {
	for m, list := range r.retired {
		kept := list[:0]
		for _, ch := range list {
			if !ch.drained() {
				kept = append(kept, ch)
			}
		}
		if len(kept) == 0 {
			delete(r.retired, m)
		} else {
			r.retired[m] = kept
		}
	}
}

func (r *multidbFDRouter) removeHookID() string {
	return fmt.Sprintf("multidbFDRouter#%p", r)
}

// dropChild drains and closes db's children (the current one and any retired
// one still draining) when db is removed. It runs before the member's client
// is closed, so the children flush their accepted commands while the member
// is still open.
func (r *multidbFDRouter) dropChild(db *multidbDatabase) {
	r.mu.Lock()
	children := r.retired[db]
	if ch := r.children[db]; ch != nil {
		children = append(children, ch)
	}
	delete(r.children, db)
	delete(r.retired, db)
	delete(r.noFD, db)
	delete(r.removeHooks, db)
	r.mu.Unlock()
	for _, ch := range children {
		_ = ch.Close()
		_ = ch.WaitClosed()
	}
}

// newChild builds db's FD child with the owner wiring: outcomes settle into
// db's breaker and the detector, recovery reroutes through MultiDB. It
// returns nil when full duplex does not engage on the member (no pipeline
// pool, replica routing on a cluster member), so that member diverts.
func (r *multidbFDRouter) newChild(db *multidbDatabase) (*AutoPipeliner, error) {
	core := r.c.core
	cfg := r.childCfg
	cfg.fdOutcome = func(ctx context.Context, cmd Cmder, err error, grp *fdFailGroup, token fdOutcomeToken) {
		core.settleFDOutcome(ctx, db, cmd, err, grp, token)
	}
	// The engine passes the commands' remaining budget (derived from
	// fdRetryBudget below) as the retry bound, so a rerouted command never
	// runs more than CommandRetries+1 times in total.
	cfg.fdReroute = core.processPipelineRetries
	// Recovery follows MultiDB's retry policy, not the member's MaxRetries
	// (members commonly run with retries off): a dropped session's tail and a
	// retryable reply (READONLY, LOADING, ...) get re-issued through MultiDB as
	// often as core.process would retry.
	cfg.fdOwnerRetries = true
	cfg.fdRetryBudget = core.opts.CommandRetries

	var (
		child *AutoPipeliner
		err   error
	)
	if db.cc != nil {
		child, err = newAutoPipeliner(db.cc, clusterAutoPipelineOptions(&cfg), r.blocking)
		if err == nil {
			db.cc.installAutoPipelineSharding(child)
			if child.clusterFD != nil {
				// Routing misses (keyless command, topology miss, a node
				// without FD) take MultiDB's path, not db.cc.Process.
				child.clusterFD.ownerDivert = r.divert
			}
		}
	} else {
		child, err = newAutoPipeliner(db.c, &cfg, r.blocking)
	}
	if err != nil {
		return nil, err
	}
	if child.fd == nil && child.clusterFD == nil {
		_ = child.Close()
		return nil, nil
	}
	return child, nil
}

// len sums the backlog of the children and of the half-duplex lane, for
// AutoPipeliner.Len. hd is never one of the children.
func (r *multidbFDRouter) len() int {
	r.mu.RLock()
	total := 0
	for _, ch := range r.children {
		total += ch.Len()
	}
	r.mu.RUnlock()
	if hd := r.hd.Load(); hd != nil {
		total += hd.Len()
	}
	return total
}

// close closes every child and waits for each drain. Called from the parent's
// drain, which MultiDBClient.Close runs before it closes the members, so each
// child flushes its accepted commands while its member is still open.
//
// The children and their removal hooks stay registered until the drains are
// done: a RemoveDatabase racing this then still finds its member's child in
// dropChild and waits on the same drain before it closes the member's client.
// closed (set first) stops childFor from building new children meanwhile.
func (r *multidbFDRouter) close() error {
	r.mu.Lock()
	r.closed = true
	children := make([]*AutoPipeliner, 0, len(r.children))
	for _, ch := range r.children {
		children = append(children, ch)
	}
	for _, list := range r.retired {
		children = append(children, list...)
	}
	r.mu.Unlock()
	r.lockLane()
	if hd := r.hd.Swap(nil); hd != nil {
		children = append(children, hd)
	}
	r.lane.Store(nil)
	r.unlockLane()

	var firstErr error
	for _, ch := range children {
		cerr := ch.Close()
		if werr := ch.WaitClosed(); werr != nil {
			cerr = werr
		}
		if cerr != nil && firstErr == nil {
			firstErr = cerr
		}
	}

	// Drained: drop the router-owned maps and the removal hooks. The noFD
	// marks go too, since no hook would clear them later.
	r.mu.Lock()
	r.children = make(map[*multidbDatabase]*AutoPipeliner)
	r.retired = make(map[*multidbDatabase][]*AutoPipeliner)
	r.noFD = make(map[*multidbDatabase]struct{})
	hooked := make([]*multidbDatabase, 0, len(r.removeHooks))
	for db := range r.removeHooks {
		hooked = append(hooked, db)
	}
	r.removeHooks = nil
	r.mu.Unlock()
	for _, db := range hooked {
		db.onRemove.unregister(r.removeHookID())
	}
	return firstErr
}

// fdOutcomeToken is the owner admission a full-duplex command's outcome
// belongs to: the circuit-breaker reservation and failure-detector generation
// sampled when the router admitted it to the FD path. Carrying it from submit
// to settle keeps an outcome that outlives an operator reselect of the SAME
// member out of the fresh breaker episode and detector window, exactly as
// core.process carries its admission through the attempt. Rerouted commands do
// not use it: the owner's path admits them again when it runs them.
type fdOutcomeToken struct {
	res         imultidb.Reservation
	detectorGen uint64
}

// settleFDOutcome records a full-duplex command's final outcome on db, the way
// core.process does for a command it ran: the member's breaker always, the
// failure detector only while db is still the active member. The breaker
// episode and the detector window are the ones the command was admitted into
// (token, captured by multidbFDRouter.target), not the current ones: an
// operator reselect of the same member must not let a pre-reset outcome
// reopen the fresh breaker or trip failover away from the member just selected.
// Runs on the child's reader or engine goroutine: it must not block.
//
// grp groups the failures of one connection drop: the breaker and the
// detector each count the group once (the first failure recorded), as
// recordBatchOutcomes does for a batch. Successes count per command.
func (c *multidbCore) settleFDOutcome(ctx context.Context, db *multidbDatabase, cmd Cmder, err error, grp *fdFailGroup, token fdOutcomeToken) {
	if callerEnded(ctx, err, false) {
		// The caller's own context ended the command (a context error or a
		// cut-short dial): a client-side signal, not a health verdict
		// (core.process skips it the same way). A context error with the
		// caller's context still live (a member-side dial or limiter deadline)
		// is classified like any other error, and so is a member read/write
		// failure that lands after the caller's context ended. ctxTimeouts is
		// false: the shared FD reader and writer arm socket deadlines from
		// ReadTimeout/WriteTimeout, never from a command's ctx.
		return
	}
	kind := classifyOutcome(err, cmd.readTimeout() == nil)
	if kind == outcomeNeutral {
		return
	}
	// Settle against the admission token. RecordSuccessFor/RecordFailureFor
	// drop the outcome when the breaker cycled since admission (an operator
	// reselect bumps its generation), so a late failure cannot reopen the
	// freshly reset member.
	if kind == outcomeSuccess {
		db.cb.RecordSuccessFor(token.res)
	} else if grp.take(token.res.Generation()) {
		db.cb.RecordFailureFor(token.res)
	}
	// The detector write is window-gated by the generation sampled at
	// admission: a reselect of the same member (which leaves the identity
	// check below unchanged) voids it inside recordDetector*.
	if cur, _ := c.activeSnapshot(); cur != db {
		return
	}
	if kind == outcomeSuccess {
		c.successSinceFailover.Store(true)
		c.recordDetectorSuccess(token.detectorGen)
	} else if grp.takeDetector(token.detectorGen) {
		c.recordDetectorFailure(token.detectorGen, err)
	}
}
