package redis

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/pool"
	"github.com/redis/go-redis/v9/internal/proto"
	"github.com/redis/go-redis/v9/push"
)

// The full-duplex miss-coalescer engine — THE engine behind
// Options.ClientSideCacheCoalesceMisses (misses are caller-blocking, so the
// latency-first streaming engine is the only one; see csc_miss_coalesce.go for
// the removed alternatives).
//
// fullDuplexLoop runs sessions holding one tracked connection with concurrent
// writer + reader goroutines: commands stream out while replies stream back, so
// many commands are in flight on a single socket (the rueidis pipelining
// model) — hides RTT with one connection instead of N.
//
// A session holds its connection outside the normal pool Get/Put cycle and
// redials on error. Correctness note: a held connection spans reconnects, so on
// ANY connection error every in-flight request is failed (token cancelled,
// caller woken) rather than matched to a reply across tracking generations, and
// a published reply is gated on the reading connection's id+generation still
// matching the one captured when the command was written.
//
// (An earlier "pinned" engine — one held conn, serial half-duplex batches, NO
// idle invalidation drain — was a benchmark-only prototype and has been
// removed: it could serve stale after an invalidation while idle.)

const cscModeBackoff = 5 * time.Millisecond

func opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// acquireCtx bounds a session connection acquisition. Unlike opCtx's fixed 5s
// it honors the larger of the configured PoolTimeout (a saturated pool may
// legitimately make Get wait that long) and the pool's full dial budget
// (DialerRetries attempts of DialTimeout plus backoffs — a shorter outer
// deadline would cancel Get mid-dial-sequence and forfeit configured retries).
// Cancelled when the coalescer stops, so Close does not stall behind a blocked
// Get.
func (mc *cscMissCoalescer) acquireCtx() (context.Context, context.CancelFunc) {
	opt := mc.c.opt
	d := 5 * time.Second
	if pt := opt.PoolTimeout; pt > d {
		d = pt
	}
	// Options.init resolves the dial knobs to nonzero defaults. Use a DETERMINISTIC
	// estimate that never invokes DialerRetryBackoff: calling a randomized/stateful
	// backoff here (e.g. DialRetryBackoffExponential) consumes different jitter
	// samples than the pool's actual retries, so the estimate can still be wrong —
	// summing it out-of-band is not sound. A custom backoff whose real delays
	// exceed this flat estimate should raise PoolTimeout; this deadline is only a
	// floor anyway, since mc.stop also cancels the Get on Close.
	if db := time.Duration(opt.DialerRetries)*(opt.DialTimeout+opt.DialerRetryTimeout) + opt.DialTimeout; db > d {
		d = db
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	done := make(chan struct{})
	go func() {
		select {
		case <-mc.stop:
			cancel()
		case <-done:
		}
	}()
	return ctx, func() { close(done); cancel() }
}

// ---- full-duplex engine (feature-complete) ----

// cscFullDuplexConnsDefault is how many independent full-duplex sessions (each
// its own held connection) the fullduplex engine runs. 1 keeps the footprint
// minimal (optimal at high concurrency). More would shard the miss +
// invalidation-drain load across connections, cutting the low-concurrency p99
// tail. Order-free: coalesced misses are independent per-key fetches.
const cscFullDuplexConnsDefault = 1

// cscFullDuplexIdleProbe is how often the reader drains server-initiated pushes
// on the held connection while no reply is pending. A var (not const) so a test
// can disable the idle drain (set it huge) as a negative control.
var cscFullDuplexIdleProbe = 5 * time.Millisecond

// cscFullDuplexSessionIdle: a session with no miss for this long ends and
// returns its held connection (at PoolSize:1 an idle hold would block
// non-cacheable commands until PoolTimeout). The next miss re-acquires; under
// steady load the timer never fires. Var for tests.
var cscFullDuplexSessionIdle = time.Second

// fullDuplexRecycleAge bounds how long a session holds cn so OnPut pool hooks
// (metrics, health, queued maintenance handoff) run periodically. Capped by the
// conn's REMAINING lifetime (ExpiresAt, jitter included): an old conn must not
// be held past the expiry the pool's reaper enforces.
func (c *baseClient) fullDuplexRecycleAge(cn *pool.Conn) time.Duration {
	age := 30 * time.Second
	// Lifetime is honored via the conn's ACTUAL absolute expiry only —
	// ExpiresAt already includes ConnMaxLifetimeJitter. Capping at the raw
	// ConnMaxLifetime first would collapse every positive-jitter connection
	// back to the unjittered lifetime, re-synchronizing recycles across
	// clients started together (the herd the jitter option exists to prevent).
	if exp := cn.ExpiresAt(); !exp.IsZero() {
		if remain := time.Until(exp); remain < age {
			age = remain
		}
	}
	// A conn at (or past) expiry still serves the session it was just leased
	// for — recycle promptly rather than with a zero/negative timer.
	if age < 10*time.Millisecond {
		age = 10 * time.Millisecond
	}
	return age
}

// fullDuplexLoop runs full-duplex sessions back to back: each session holds one
// tracked connection until it errors, is recycled (handoff/close/age), or stop
// is requested. Exits on stop.
func (mc *cscMissCoalescer) fullDuplexLoop() {
	defer mc.wg.Done()
	for {
		select {
		case <-mc.stop:
			return
		default:
		}
		stopped, errored := mc.runFullDuplexSession()
		if stopped {
			return
		}
		// Back off only after an ERROR end, so a persistent dial failure does not
		// hot-spin; a clean idle/recycle end continues immediately — the next
		// session blocks on the miss queue anyway, and sleeping here would add a
		// flat cscModeBackoff of latency to a miss already waiting in mc.ch.
		if !errored {
			continue
		}
		select {
		case <-mc.stop:
			return
		case <-time.After(cscModeBackoff):
		}
	}
}

// runFullDuplexSession acquires one tracked connection and pipelines misses on
// it with concurrent writer and reader goroutines. It ends when:
//   - a socket read/write errors (fail path: fail in-flight, Close + Remove);
//   - the connection is marked for handoff/close, the session ages out, or stop
//     is requested (graceful path: drain in-flight, Put so OnPut runs pool hooks
//     and any queued maintenance handoff).
//
// Returns true iff stop was requested (the loop should exit).
func (mc *cscMissCoalescer) runFullDuplexSession() (stopped, backoff bool) {
	c := mc.c

	// Do not hold a pool connection while idle: wait for the first miss BEFORE
	// acquiring. An eagerly-held session connection would, at a small pool
	// (PoolSize:1), starve non-cacheable commands (PING/SET/uncached reads) until
	// PoolTimeout while the session sat waiting for work. The pulled miss is
	// written first by the writer below.
	var first *cscMissReq
	select {
	case <-mc.stop:
		return true, false
	case first = <-mc.ch:
	}

	getCtx, getCancel := mc.acquireCtx()
	cn, limited, err := c.getConnLimited(getCtx)
	getCancel()
	if err != nil {
		// Distinguish a teardown cancellation from a genuine acquire failure by
		// checking mc.stop DIRECTLY, not by err's value: getConn runs the dialer,
		// credentials callbacks, and init hooks, any of which can return
		// context.Canceled from their OWN context while the coalescer is NOT
		// stopping. mc.stop is closed only by stopWorkers, and close(mc.stop) is
		// ordered before acquireCtx cancels its context (which is ordered before
		// getConn returns), so a real teardown is always visible here.
		select {
		case <-mc.stop:
			// Tearing down: settle retry-uncached — like the other stop paths (fetch,
			// the serving-disabled branch below, stopCSCMissCoalescer) — so processCached
			// re-runs each read on the still-open pool instead of surfacing a spurious
			// cancellation, and report stopped so the loop exits without backing off.
			mc.settleErr(first, errCSCRetryUncached)
			mc.drainQueueErr(errCSCRetryUncached)
			return true, false // stop requested: exit the loop
		default:
		}
		if limited {
			// Limiter.Allow REJECTED admission for the session connection (before any
			// dial): first was the ONE request that woke the session and triggered that
			// Allow call. Surface the rejection to IT unchanged — settle without the
			// cscSessionError tag so processCached returns it as-is instead of
			// re-running on the pooled path, whose getConn would call Limiter.Allow a
			// SECOND time and could admit (defeat) the denial the limiter just issued.
			//
			// The requests already queued behind first never called Allow themselves —
			// they were just waiting for the session to write their command. Failing
			// them with first's raw, non-retryable error would deny operations the
			// limiter was never asked about. Settle them retry-uncached instead: each
			// re-runs on the ordinary per-command path and calls Allow independently,
			// which is what would have happened had they never been queued (cursor
			// review #3989). Still an error end for the session loop (backoff also
			// damps Allow frequency under sustained rejection).
			mc.settlePlain(first, err)
			mc.drainQueueErr(errCSCRetryUncached)
			return false, true
		}
		// Genuine acquire failure (dial error, pool exhaustion, an unrelated
		// context.Canceled from a custom dialer/creds/init hook). Fail the first miss
		// plus everything queued so a caller on a deadline-less context is not blocked
		// forever and no reservation leaks IN_PROGRESS, then back off and retry so the
		// sole worker stays alive — requests arriving during backoff are failed on the
		// next attempt's drain.
		mc.settleErr(first, err)
		mc.drainQueueErr(err)
		return false, true // error end: the loop backs off before re-acquiring
	}
	// CSC serving may have been disabled after the miss was queued (e.g. a HELLO 3
	// downgrade or CLIENT TRACKING rejected during initConn). Holding the conn to
	// wait for more misses would be pointless — none get routed here once serving
	// is off — and could tie up a small pool. Release it cleanly and fail pending.
	if a := c.cscActive; a != nil && !a.Load() {
		relCtx, relCancel := opCtx()
		c.releaseConn(relCtx, cn, nil)
		relCancel()
		// CSC is off now, but the commands are fine — settle with the retry-uncached
		// sentinel so processCached re-runs each on the normal path instead of
		// surfacing a spurious pool.ErrClosed for a valid cacheable read.
		mc.settleErr(first, errCSCRetryUncached)
		mc.drainQueueErr(errCSCRetryUncached)
		return false, false // clean end: no new misses route here while serving is off
	}
	connID := cn.GetID()
	gen := c.cscConnInitGen(connID)

	inflight := make(chan *cscMissReq, cscFullDuplexDepth)
	// readsDone counts replies the reader has fully consumed. The drain
	// backstop samples it as its progress signal: unlike len(inflight), it sees
	// the ACTIVE read too (the reader pops a request before reading its reply,
	// so a lone deep read under a maintenance-relaxed timeout would otherwise
	// look like a zero-progress stall and get its socket closed mid-reply).
	var readsDone atomic.Uint64
	sctx, scancel := context.WithCancel(context.Background())
	defer scancel()

	var sessErr atomic.Value // error, set only on I/O failure
	var failOnce sync.Once
	fail := func(e error) {
		failOnce.Do(func() {
			if e == nil {
				e = pool.ErrClosed
			}
			sessErr.Store(e)
			scancel()
		})
	}
	errored := func() bool { _, ok := sessErr.Load().(error); return ok }
	reasonErr := func() error {
		if e, ok := sessErr.Load().(error); ok {
			return e
		}
		return pool.ErrClosed
	}

	recycle := make(chan struct{})
	var recycleOnce sync.Once
	doRecycle := func() { recycleOnce.Do(func() { close(recycle) }) }
	var stopFlag atomic.Bool

	// handoffWanted reports whether the connection should be returned to the pool
	// so the OnPut hooks (including a queued maintenance handoff) can run.
	handoffWanted := func() bool {
		return !cn.IsUsable() || cn.ShouldHandoff() || cn.CloseOnPutReason() != ""
	}

	var swg sync.WaitGroup
	swg.Add(2)

	// Writer: pull a request (plus whatever else is queued), write the batch,
	// then enqueue each written request to the reader in wire order. The writer
	// is the SOLE closer of inflight and closes it as its last act on every exit
	// path, which is how the reader learns the session drained.
	go func() {
		defer swg.Done()
		defer close(inflight)
		idleT := time.NewTimer(cscFullDuplexSessionIdle)
		defer idleT.Stop()
		buf := make([]*cscMissReq, 0, cscMissBatchMax)
		pending := first // the miss that woke the session; write it before blocking
		for {
			var req *cscMissReq
			if pending != nil {
				// A carry from the previous batch. Still honor recycle/stop/cancel
				// between batches even under a sustained carry stream, or a
				// persistently over-budget producer would starve a maintenance handoff
				// or lifetime recycle (the OnPut hooks would never run). Settle the
				// carry on bail: it is the writer's own state, so the teardown drain
				// (which covers only inflight) would otherwise leak it and hang its
				// caller.
				select {
				case <-recycle:
					mc.settleErr(pending, errCSCRetryUncached)
					return
				case <-mc.stop:
					stopFlag.Store(true)
					doRecycle()
					mc.settleErr(pending, errCSCRetryUncached)
					return
				case <-sctx.Done():
					mc.settleErr(pending, reasonErr())
					return
				default:
				}
				// grabInto below overwrites pending with the next carry, so do not
				// clear it here (that store would be dead).
				req = pending
			} else {
				if !idleT.Stop() {
					select {
					case <-idleT.C:
					default:
					}
				}
				idleT.Reset(cscFullDuplexSessionIdle)
				select {
				case <-recycle:
					return
				case <-mc.stop:
					stopFlag.Store(true)
					doRecycle()
					return
				case <-sctx.Done():
					return
				case <-idleT.C:
					// No miss for the grace period: end the session cleanly so the
					// held connection goes back to the pool instead of blocking
					// non-cacheable traffic at a small pool until the recycle age.
					// The reader drains anything still in flight (close(inflight)
					// is the graceful no-more signal); the next miss starts a fresh
					// session, which acquires only when work is in hand.
					return
				case req = <-mc.ch:
				}
			}
			// grabInto may defer one over-budget request; carry it in pending so the
			// next iteration writes it as the first request of its own batch. Held in
			// the writer's own state, never put back on mc.ch, so a shutdown cannot
			// strand it there. The error paths below settle it if this batch fails.
			buf, pending = mc.grabInto(buf[:0], req)
			mc.countBatch(buf)

			// Attribute every request in this batch to the session connection up
			// front. A write-side failure settles the batch before the reply path
			// runs, so without this the error metric and processCached would report a
			// nil connection and zero attempts for commands that did reach cn. buf is
			// []*cscMissReq, so this persists to the reply path (which re-sets it).
			for _, r := range buf {
				r.servedBy = cn
				r.sentConn.Store(cn) // atomic view for a caller abandoning before req.done
			}

			// Hand the batch to the reader BEFORE writing it, so the reader drains
			// reply k while the writer is still writing and flushing k+1. If the
			// reader started only after the whole batch was written (the old order),
			// a batch larger than the transport send capacity could deadlock: the
			// server blocks writing early replies that nobody reads while the writer
			// blocks flushing later requests. With the reader draining concurrently,
			// those early replies are consumed and the server keeps accepting the
			// rest of the write.
			//
			// This send cannot deadlock the writer. It blocks only when inflight is
			// full (cscFullDuplexDepth), and a batch is at most cscMissBatchMax
			// (128 << 4096). The writer is one goroutine and runs sequentially, so
			// every PRIOR batch was already flushed by its own WithWriter before this
			// iteration. A full channel therefore holds at least
			// cscFullDuplexDepth-cscMissBatchMax already-flushed requests, and the
			// reader consumes FIFO, so its front is always a flushed request whose
			// reply is on the wire: the reader advances, frees a slot, and the send
			// proceeds. (Mirrors the autopipeline full-duplex engine, which likewise
			// enqueues each batch before it writes.)
			//
			// A stalled or exited reader cannot strand the writer here either: every
			// reader-exit path either cancels sctx (readOne/tick failure via fail, or
			// the reader's own sctx.Done case) — so the select below escapes — or
			// happens only after the writer already exited and closed inflight. Keep
			// that true: a reader return added after doRecycle without cancelling sctx
			// would break this escape.
			sent := 0
			for _, r := range buf {
				select {
				case inflight <- r:
					sent++
				case <-sctx.Done():
					// Session cancelled mid-handoff. The reader and the teardown settle
					// whatever reached inflight; the writer settles the rest and the
					// carry (never in inflight).
					mc.settleAllErr(buf, sent, reasonErr())
					if pending != nil {
						mc.settleErr(pending, reasonErr())
					}
					return
				}
			}

			// sctx directly, NOT c.context(sctx): c.context() strips the engine's
			// cancellable session ctx under ContextTimeoutEnabled=false, and with
			// per-op timeouts disabled a blocked read/write could then never be
			// interrupted (the supervisor's conn-close is the backstop).
			werr := cn.WithWriter(sctx, c.opt.WriteTimeout, func(wr *proto.Writer) error {
				for _, r := range buf {
					// r.wire was snapshotted at enqueue while the caller owned
					// cmd (see cscMissReq.wire): the writer never reads cmd, so
					// an abandoned caller's args are never touched here, and an
					// abandoned fetch still completes — its reply publishes to
					// the cache and settles the reservation.
					if _, e := wr.Write(r.wire); e != nil {
						return e
					}
				}
				return nil
			})
			if werr != nil {
				fail(werr)
				// The batch is already in inflight, so the reader (and the teardown
				// drain) settle it. The writer only owns the carry, which never
				// entered inflight.
				if pending != nil {
					mc.settleErr(pending, werr)
				}
				return
			}
		}
	}()

	// Reader: settle replies in the order the writer enqueued them (== wire
	// order), and drain server-initiated pushes (invalidations, maintenance)
	// even while idle. Publishes only while the connection's id+generation still
	// match the session's.
	go func() {
		defer swg.Done()
		ticker := time.NewTicker(cscFullDuplexIdleProbe)
		defer ticker.Stop()

		readOne := func(req *cscMissReq) bool { // false => fatal, reader must exit
			// batchBudget (write + read), not bare ReadTimeout: the writer hands the batch
			// to this reader BEFORE flushing it (deadlock avoidance), so a reply read can
			// arm while its request is still being written — an oversized first command
			// (allowed past the batch byte cap) or a slow/WAN link then charges write-flush
			// time against a bare ReadTimeout and spuriously times out (and tears down) a
			// session whose write is within WriteTimeout (#3989). batchBudget is exactly the
			// batch's write+read bound; relaxation still raises it further via EffectiveReadTimeout.
			rerr := cn.WithReader(sctx, mc.batchBudget(), func(rd *proto.Reader) error {
				// Push handling with the nonblocking Close adapter: this reader
				// is part of mc.wg, so a custom push handler calling Close() on
				// the raw client would self-deadlock (Close waits on mc.wg while
				// the reader is parked inside the handler). The adapter's Close
				// signals and defers the blocking teardown to a goroutine.
				// Drain server pushes before reading this request's reply, through the
				// shared helper in BLOCKING mode: block on the socket and skip pushes
				// until the reply is the next frame, so a second invalidation still on
				// the socket ahead of the reply is not read by ReadRawReply as this
				// request's reply. (The Buffered variant stopped at buffer-empty and let
				// such a socket-pending push through, shifting the reply stream by one
				// frame.) A custom processor is handed only a confirmed push frame (peek
				// first). FATAL, not log-and-continue: a drain error means bytes may have
				// been consumed mid-frame, so continuing into ReadRawReply on a
				// desynchronized stream could apply a push fragment as this request's
				// reply and publish it to the cache under the wrong key. Fail the session
				// so the connection is removed.
				if e := c.drainPushFrames(sctx, cn, rd, true); e != nil {
					internal.Logger.Printf(sctx, "csc: miss-coalesce push drain: %v", e)
					return e
				}
				raw, e := rd.ReadRawReply()
				if e != nil {
					return e
				}
				// Deliver the reply the caller is waiting for and let applyAndSettle
				// gate only the CACHE PUBLISH on the connection's id/generation
				// (fulfillCached checks the captured gen). Previously FD failed the
				// caller with ErrClosed on a mid-flight id/gen change even though the
				// reply was read fine, losing a good result (#3965).
				req.servedBy = cn // before settle: done is the happens-before edge
				mc.applyAndSettle(req, raw, connID, gen)
				return nil
			})
			if rerr != nil {
				fail(rerr)
				req.servedBy = cn // attribute the failed read to the session conn
				mc.settleErr(req, rerr)
				return false
			}
			readsDone.Add(1)
			return true
		}

		// handoff/close is a rare, non-latency-critical event, so it is checked off
		// the per-reply hot path: on every idle tick, and once every readsPerHandoffCheck
		// replies (~a few ms of traffic at any throughput).
		const readsPerHandoffCheck = 128
		reads := 0
		for {
			select {
			case req, ok := <-inflight:
				if !ok {
					return // writer closed: session drained clean
				}
				if !readOne(req) {
					return
				}
				reads++
				if reads%readsPerHandoffCheck == 0 && handoffWanted() {
					doRecycle()
				}
			case <-ticker.C:
				// No reply was ready this tick: drain any server-initiated pushes
				// (invalidations, MOVING, ...) unconditionally — the held connection
				// is out of the pool, so the background drainer never visits it. Safe
				// even if a reply is on the wire: the push processor peeks the reply
				// type and consumes only push frames, leaving the reply for the next
				// inflight read.
				if e := c.peekAndProcessPushNotifications(sctx, cn); e != nil {
					fail(e)
					return
				}
				// Opaque-transport fallback, HELD-CONN ONLY: where readiness cannot
				// be inspected (Windows; wrappers exposing neither syscall.Conn nor
				// NetConn) the gated peek never fires and nothing else drains this
				// conn. Throttled timed drain (no-op on inspectable transports).
				// Deliberately not in peekAndProcessPushNotifications: pooled paths
				// have drainer coverage, and a blanket fallback perturbs sequenced
				// push handling.
				// Built-in processor ONLY: this probe reads speculatively (no
				// readiness signal exists on an opaque transport), and the
				// built-in processor swallows the no-data boundary timeout. A
				// CUSTOM processor's contract documents being invoked when
				// notifications are KNOWN present — a natural implementation
				// surfaces the empty-probe timeout, which would fail (and
				// redial) a healthy session on every idle probe. Custom +
				// opaque conns forgo the speculative drain; their invalidations
				// land on session end/recycle at the latest.
				if _, builtin := c.pushProcessor.(*push.Processor); builtin {
					if cn.TakeCscPeriodicReadPending(cscFallbackProbeInterval) {
						if e := c.timedPushDrain(sctx, cn); e != nil {
							fail(e)
							return
						}
					}
				}
				if handoffWanted() {
					doRecycle()
				}
			case <-sctx.Done():
				return
			}
		}
	}()

	// Supervisor: request a graceful recycle on stop or when the session ages
	// out. A handoff/close request comes from the reader via doRecycle().
	recycleTimer := time.NewTimer(c.fullDuplexRecycleAge(cn))
	defer recycleTimer.Stop()
	superDone := make(chan struct{})
	supDead := make(chan struct{}) // closed when the supervisor exits (joined before release)
	// drainBackstop bounds a graceful drain by PROGRESS, not wall clock: no
	// context can interrupt a deadline-less socket read, so a stalled drain must
	// force the I/O out with a conn close (else Close wedges in wg.Wait) — but a
	// HEALTHY drain of a deep in-flight pipeline (up to cscFullDuplexDepth
	// replies, ~in-flight × RTT, possibly under a maintenance-relaxed timeout)
	// may legitimately exceed any fixed budget. Progress is COMPLETED READS
	// (readsDone), not queue length: the reader pops a request before reading
	// its reply, so len(inflight) is blind to the active read and would call a
	// lone deep read a stall. Each budget interval that completed at least one
	// reply extends the wait; only a zero-progress interval closes the conn.
	drainBackstop := func(stopping bool) { mc.drainBackstopRun(cn, &readsDone, superDone, stopping) }
	go func() {
		defer close(supDead)
		select {
		case <-mc.stop:
			stopFlag.Store(true)
			doRecycle()
			drainBackstop(true)
		case <-recycleTimer.C:
			doRecycle()
			drainBackstop(false)
		case <-recycle:
			// Reader-triggered recycle (handoffWanted: unusable / handoff-marked /
			// close-on-put). The writer exits, but replies may remain in flight —
			// without a backstop a stalled read would hold the conn (and postpone
			// the requested handoff) until Close. Same bounded drain as the timer
			// path; doRecycle already ran (this channel IS the recycle signal).
			drainBackstop(false)
		case <-sctx.Done(): // I/O error: unblock a reader/writer parked on the socket
			_ = cn.Close()
		case <-superDone:
		}
	}()

	swg.Wait()
	close(superDone)
	// JOIN the supervisor before releasing cn: the deferred scancel makes
	// sctx.Done() ready, and a still-parked supervisor could pick it over the
	// equally-ready superDone and close a healthy conn already back in the pool.
	<-supDead

	// Teardown. Error path: the socket was (or is being) closed; Remove it.
	// Graceful path: the connection is clean (every written reply was read), so
	// Put it — OnPut runs the pool hooks and any queued maintenance handoff, and
	// the handoff/reinit path evicts this connection's now-uncovered CSC entries.
	relCtx, relCancel := opCtx()
	if errored() {
		_ = cn.Close()
		c.releaseConn(relCtx, cn, reasonErr())
	} else {
		c.releaseConn(relCtx, cn, nil)
	}
	relCancel()

	// Fail anything the writer enqueued that the reader never consumed (only
	// possible on the error path; the graceful path drains inflight fully). The
	// writer always closes inflight on exit, so a receive here can report the
	// channel closed (ok=false) — do not settle a nil request.
	for {
		select {
		case r, ok := <-inflight:
			if !ok {
				return stopFlag.Load(), errored()
			}
			mc.settleErr(r, reasonErr())
		default:
			return stopFlag.Load(), errored()
		}
	}
}

// drainBackstop bounds a graceful drain of the full-duplex miss-coalescer session
// by PROGRESS (completed reads, readsDone), not wall clock — see the block comment at
// the call site and cscMissStopDrainIntervals for the stopping-path total cap.
// Extracted from runFullDuplexSession so the stopping-drain budget is unit-testable.
func (mc *cscMissCoalescer) drainBackstopRun(cn *pool.Conn, readsDone *atomic.Uint64, superDone <-chan struct{}, stopping bool) {
	prev := readsDone.Load()
	// On the stopping (Close) path the progress branch below must not renew forever:
	// a server trickling one reply per interval keeps readsDone advancing, so with up
	// to cscFullDuplexDepth in-flight, Close would block in wg.Wait for hours. Cap the
	// whole stopping drain with a total deadline, armed ONCE (the first stopping loop),
	// at cscMissStopDrainIntervals × interval. This is a TRADEOFF, not a free win: a
	// deep, legitimately slow-but-progressing drain CAN exceed that budget (see the
	// unbounded-progress rationale below), and past it Close-never-wedges outranks
	// completing the remainder — some accepted in-flight replies may be cut. Non-stopping
	// (age-out / handoff recycle) keeps the unbounded rule: the client stays alive, only
	// this conn's recycle waits.
	var stopDeadline time.Time
	for {
		// One blocked read may legitimately wait out its full reply deadline
		// (readOne arms batchBudget, and a maintenance-relaxed timeout can exceed
		// even that), so the zero-progress verdict must not fire before the reader's
		// own deadline. Keep the interval strictly ABOVE batchBudget — the reader's
		// per-read bound — so a single legitimately-slow reply is never mistaken for a
		// stall and its connection force-closed. Recomputed per interval: an expired
		// relaxation shrinks it back.
		interval := mc.batchBudget() + time.Second
		rt := cn.EffectiveReadTimeout(mc.c.opt.ReadTimeout)
		wt := cn.EffectiveWriteTimeout(mc.c.opt.WriteTimeout)
		if rt+time.Second > interval {
			interval = rt + time.Second
		}
		if wt+time.Second > interval {
			interval = wt + time.Second
		}
		// Arm the total stopping-drain deadline once (see cscMissStopDrainIntervals):
		// N intervals from the first stopping loop, so a trickling server cannot renew
		// the drain past a bounded budget on Close. mc.stopDrainBudget overrides the cap
		// (tests only; zero in production).
		if stopping && stopDeadline.IsZero() {
			budget := cscMissStopDrainIntervals * interval
			if mc.stopDrainBudget > 0 {
				budget = mc.stopDrainBudget
			}
			stopDeadline = time.Now().Add(budget)
		}
		// Deadline-less I/O (no active relaxation): the user explicitly
		// disabled the read and/or write deadline, so the RECYCLE path must
		// not cut a session whose reader is in a legitimately long reply OR
		// whose writer is blocked flushing a large request — readsDone
		// cannot show progress before a reply exists, so a deadline-free
		// blocked WRITE looks exactly like a stall. Wait for the session
		// (or Close) instead. Close still terminates: once stop fires, the
		// bounded zero-progress close below applies, which is the shutdown
		// contract (Close never wedges). NOTE the <= 0: Options.init
		// normalizes -1 (indefinite) to 0 and -2 (no deadline calls) to
		// -1, so both deadline-less modes land at <= 0 here.
		if !stopping && (rt <= 0 || wt <= 0) {
			select {
			case <-superDone:
				return
			case <-mc.stop:
				stopping = true
			}
			continue
		}
		// Watch mc.stop on the non-stopping (recycle) path too: if Close arrives AFTER an
		// age-out/handoff recycle drain has already started here, flip to the stopping path
		// so the next loop arms the total cap. Without this, a recycle-then-Close race stays
		// on this unbounded progress wait and a trickling server hangs Close in wg.Wait —
		// the exact hang cscMissStopDrainIntervals was added to prevent. nil once stopping:
		// mc.stop is then closed and would busy-spin the select.
		var stopCh <-chan struct{}
		if !stopping {
			stopCh = mc.stop
		}
		select {
		case <-superDone:
			return
		case <-stopCh:
			stopping = true
			continue
		case <-time.After(interval):
			cur := readsDone.Load()
			if cur != prev {
				prev = cur // still consuming replies; give it another interval
				// ...unless the total stopping-drain budget is spent: Close-never-wedges
				// outranks completing the remaining in-flight against a trickling server.
				if !stopDeadline.IsZero() && !time.Now().Before(stopDeadline) {
					_ = cn.Close()
					return
				}
				continue
			}
			_ = cn.Close()
			return
		}
	}
}
