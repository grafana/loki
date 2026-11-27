package redis

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/pool"
	"github.com/redis/go-redis/v9/internal/proto"
)

// errCSCRetryUncached settles a coalesced miss when the coalescer bows out for a
// reason unrelated to the command itself: CSC serving was disabled after the
// miss was queued (a RESP3 downgrade, or CLIENT TRACKING rejected during a
// connection re-init), so this session would fetch on an untracked conn or none
// at all. It never reaches the caller — processCached catches it and re-runs the
// command uncached, so a valid cacheable read is not failed with a spurious
// pool.ErrClosed. The reservation is always cancelled before this is settled, so
// no waiter is left IN_PROGRESS.
var errCSCRetryUncached = errors.New("redis: csc miss-coalescer disabled mid-fetch; retry uncached")

// Reader-miss coalescing, behind Options.ClientSideCacheCoalesceMisses.
//
// The two-client CSC shape serves cache HITS locally (no connection) but every
// MISS fetches on the caching client's MAIN pool, one command per connection.
// At a small pool that is the wall: N concurrent misses contend one GET at a
// time (measured: at CSC pool 8 the reader misses alone saturate the pool, p99
// ~420ms at 64 workers, and no refresh policy helps — the readers are the
// bottleneck).
//
// This streams each miss's ORIGINAL command onto a held tracked (main-pool)
// full-duplex connection (see csc_miss_coalesce_modes.go), reads the replies
// back in order, and applies each to its caller's command AND to the shared
// cache. Individual pipelined commands (not MGET), so it is cluster-safe and
// works for any read shape, not just string GET. Misses are CALLER-BLOCKING, so
// the engine is latency-first: a lone miss is written immediately (batching only
// packs what is already queued, never waits for more), and new misses stream out
// while earlier replies are still in flight (~1 RTT per miss, no batch
// phase-lock, no pool Get on the hot path).
//
// Being cache-aware is why it cannot just ride the ordinary autopipeliner: it
// must Reserve/fulfilCached each key so the entry is tracked and
// single-flighted. The caller Reserves in processCached (only shouldFetch==true
// misses reach here); the engine owns the token from that point and settles it —
// fulfilled or cancelled — before waking the caller, or a dangling IN_PROGRESS
// entry blocks every reader of that key for StaleTimeout.
//
// Tradeoff, same as the autopipeline path: a coalesced miss loses per-command
// MaxRetries/backoff/LOADING handling.

const (
	cscMissBatchMax   = 128
	cscMissQueueDepth = 4096
	// cscFullDuplexDepth caps in-flight commands on the single-connection
	// full-duplex engine (flow control for the writer).
	cscFullDuplexDepth = 4096
	// cscMissBatchBytes is the top limit on the serialized size of a coalesced write
	// batch. It bounds the buffered write even when the configured write buffer is
	// very large. The effective cap is the smaller of this value and the write
	// buffer (see cscMissWriteBatchBytes). A batch larger than the write buffer makes
	// bufio flush in the middle of the batch, which can deadlock the write and the
	// read. The first miss always goes, because a single large command must not
	// stall. So the cap bounds only the extra already-queued misses that share the
	// write.
	cscMissBatchBytes = 1 << 20 // 1 MiB
	// cscMissWireBudgetBytes bounds the TOTAL serialized bytes of in-flight coalesced
	// misses. The queue's item cap (cscMissQueueDepth) does not bound memory: every
	// caller serializes and holds a full command copy (req.wire) while blocked on the
	// send, and cacheable commands (e.g. a large MGET) have no small encoded size, so
	// a burst of large misses could exhaust the process. Over this budget a miss sheds
	// to the ordinary pooled path (errCSCRetryUncached) — which is itself bounded by
	// pool turns — instead of coalescing. 8x one batch lets several full batches be in
	// flight before shedding.
	cscMissWireBudgetBytes = 8 * cscMissBatchBytes // 8 MiB
	// cscMissMaxConcurrentSerialize caps how many fetch calls may hold a wire
	// snapshot mid-serialization at once. reserveWireBytes gates on cmdApproxBytes,
	// an ESTIMATE taken before serialization. It sizes string, []byte, *string and
	// BinaryMarshaler args exactly, but can still undercount: a marshaler whose
	// output differs between the sizing call and writeCmd's own MarshalBinary call,
	// or a numeric/time arg whose text exceeds the fixed 8-byte fallback. A
	// concurrent burst of such misses can all pass the byte reservation and each
	// allocate its full wireBuf BEFORE reconcileWireBytes sheds — a transient set of
	// snapshots the budget never sees, which can exhaust memory even though none
	// stay queued. Bounding the concurrent set to budget/batch caps the COUNT of
	// coexisting snapshots, not their bytes: when the estimate is accurate each
	// snapshot is about batch-sized, so the peak is ~O(budget). A single command
	// whose encoded form far exceeds its estimate makes that one snapshot
	// arbitrarily large, so the true worst-case transient peak is cap x (largest
	// single serialized command), NOT O(budget) — a hard per-request byte ceiling
	// enforced during writeCmd is a focused follow-up. A caller that cannot get a
	// slot promptly sheds to the pooled path rather than block. Serialization is
	// CPU-only and brief, so normal small-command bursts pass without shedding.
	cscMissMaxConcurrentSerialize = cscMissWireBudgetBytes / cscMissBatchBytes // 8
)

// cscMissSerializeWait is how long acquireSerialize waits for a serialization slot
// before shedding to the pooled path. Serialization never blocks on I/O (it writes
// to an in-memory buffer), so a slot frees in microseconds; the wait only absorbs
// brief scheduling contention. A var so a test can set it to 0 (shed immediately)
// for a deterministic admission assertion.
var cscMissSerializeWait = 5 * time.Millisecond

// cscMissWriteBatchBytes returns the effective per-batch size cap. It is the
// connection's write buffer, limited to at most cscMissBatchBytes. When a batch
// fits the write buffer, bufio flushes it once, at the end of WithWriter, not in
// the middle. So the reader, which waits until the whole batch is written, starts
// to drain replies before any request that could block the writer is on the wire.
// This closes the write and read deadlock window for large payloads.
func cscMissWriteBatchBytes(opt *Options) int {
	wb := 0
	if opt != nil {
		wb = opt.WriteBufferSize
	}
	if wb <= 0 {
		wb = proto.DefaultBufferSize
	}
	if wb > cscMissBatchBytes {
		wb = cscMissBatchBytes
	}
	return wb
}

// cscCoalesceMissesEnabled is read once, at coalescer construction (per client).
func cscCoalesceMissesEnabled(opt *Options) bool {
	return opt != nil && opt.ClientSideCacheCoalesceMisses
}

// cscReq* are the states of cscMissReq.apply, the single-word interlock over who
// owns req.cmd: the fetching caller or the reader applying the reply. Exactly
// one side wins the pending->X CAS, so the reader never writes cmd after the
// caller has returned and may be reusing it.
const (
	cscReqPending   uint32 = iota // no one has claimed cmd yet
	cscReqAbandoned               // caller's ctx (or Close) fired first: cmd is the caller's again
	cscReqApplying                // reader claimed cmd first: it will write and settle
)

// cscMissReq is one caller waiting for its missed key. done carries the fetch
// result back (the reply is already applied to cmd by the time done fires).
type cscMissReq struct {
	cmd      Cmder
	cacheKey string
	token    uint64
	done     chan error
	// servedBy is the session connection that served (or failed) this request,
	// set by the engine BEFORE settling done (the channel receive is the
	// happens-before edge for the caller's read). Feeds the native OTel
	// recorder: processCached stamps it into processState so a coalesced miss
	// reports its serving connection and an attempt, like any other command.
	// Nil when the request never reached a session (enqueue reject, abandon).
	servedBy *pool.Conn
	// sentConn is servedBy published atomically: the session stores it when the batch
	// is assigned to a connection (at send), so a caller that abandons on ctx-cancel
	// BEFORE the req.done happens-before edge can still attribute the metric to the
	// serving conn instead of reporting nil for a command Redis actually received
	// (#3989). Plain servedBy is unsafe to read on that branch (it races the session's
	// write); this atomic is the safe view for it. Nil until the batch reaches a conn.
	sentConn atomic.Pointer[pool.Conn]
	// wire is cmd's RESP encoding, snapshotted at enqueue while the caller
	// still owned cmd. The session writer writes ONLY these engine-owned bytes
	// — it never reads cmd — so an abandoning caller may reuse mutable args
	// (e.g. a []byte key) the moment fetch returns, and a post-abandon
	// mutation can neither reach the wire nor publish under the original
	// cache key. Only the reply side touches cmd, gated by the apply interlock.
	wire []byte
	// reserved is the wire-budget byte reservation for this request (see
	// reserveWireBytes). It is charged in fetch before serialization and released
	// exactly once — by the settle helper when the request is consumed (its wire
	// leaves mc.ch/inflight), or by fetch itself if the request never reaches the
	// queue. Tracking the WIRE's lifetime (not the caller's) keeps a cancelled but
	// still-queued snapshot counted so the budget cannot be reused while thousands
	// of copies remain retained.
	reserved int64
	// apply interlocks ownership of cmd between the fetching caller and the
	// reader. The caller abandons (CAS pending->abandoned) if its context is
	// cancelled — or Close races its enqueue — before a reply is applied; the
	// reader claims (CAS pending->applying) before it writes the reply into cmd. A
	// plain flag would still race: the reader could read "not abandoned", begin
	// writing cmd, and the caller cancel mid-write. Either way the reader still
	// settles the token and publishes to the shared cache (the fetch is never
	// wasted) — only the cmd write is gated.
	apply atomic.Uint32
}

// claimAbandon is the caller's side of the cmd interlock: it succeeds only if no
// reply is being applied, in which case the reader skips the cmd write.
func (r *cscMissReq) claimAbandon() bool {
	return r.apply.CompareAndSwap(cscReqPending, cscReqAbandoned)
}

// claimApply is the reader's side: it succeeds only if the caller has not
// abandoned, in which case it is safe to write the caller's cmd.
func (r *cscMissReq) claimApply() bool {
	return r.apply.CompareAndSwap(cscReqPending, cscReqApplying)
}

type cscMissCoalescer struct {
	c        *baseClient
	ch       chan *cscMissReq
	stop     chan struct{}
	stopOnce sync.Once // guards close(stop): Close and the GC cleanup can both stop
	wg       sync.WaitGroup

	// stopDrainBudget overrides the Close-time drain cap. Zero in all production paths
	// (never set outside tests) — cscMissStopDrainIntervals × interval applies. Set only
	// by tests, which cannot otherwise reach the cap in bounded time: interval is floored
	// at batchBudget()+1s >= 6s, so the default cap first fires ~48s in.
	stopDrainBudget time.Duration

	// maxBatchBytes bounds the serialized size of one coalesced write batch to the
	// connection's write buffer (see cscMissWriteBatchBytes). This keeps bufio from
	// a flush in the middle of a batch. The reason: the reader waits until the whole
	// batch is written, because the writer fills inflight only after the write. A
	// mid-batch flush would put request bytes on the wire and let the server reply
	// into buffers that nobody drains yet. On large payloads this can deadlock the
	// write and the read. Set once at construction.
	maxBatchBytes int

	// wireBytes is the total serialized size of in-flight coalesced misses, bounded
	// by cscMissWireBudgetBytes. A fetch adds its approximate encoded size before
	// serializing and subtracts it when done; over budget it sheds to the pooled path.
	wireBytes atomic.Int64

	// serializeSem bounds how many fetch calls hold a wire snapshot mid-serialization
	// at once (see cscMissMaxConcurrentSerialize). Buffered to the cap; a slot is held
	// ONLY across writeCmd+reconcile and released before the enqueue send, so it
	// bounds the transient allocation set without coupling to queue backpressure — no
	// slot is ever held across a channel send or a reply wait, so it cannot deadlock
	// the single-worker session. Nil in test-constructed literals: acquire/release
	// treat a nil sem as unbounded and skip the serializing/maxSerializing counters
	// too (so they stay paired), so those keep working; production always sizes it.
	serializeSem chan struct{}
	// serializing/maxSerializing track the current and peak concurrent serializations
	// (updated under a held slot), so a test can assert the bound holds. Internal,
	// like abandonedApplies.
	serializing    atomic.Int64
	maxSerializing atomic.Uint64

	batched    atomic.Uint64 // reqs that went through a batch
	batches    atomic.Uint64 // batches flushed
	failed     atomic.Uint64 // reqs settled as errors (conn failure)
	maxBatchSz atomic.Uint64
	// abandonedApplies counts replies whose caller had already abandoned the req,
	// so the cmd write was skipped. Read only by the -race abandoned-path test, to
	// assert it hit the window it guards.
	abandonedApplies atomic.Uint64
}

// startCSCMissCoalescer launches the coalescer sessions. No-op unless
// Options.ClientSideCacheCoalesceMisses is set and CSC is active, and — see the
// option's GoDoc — unless the cache is the built-in *LocalCache: the publish path
// (fulfillCached capture, refresh integration, hot-entry collection) is
// LocalCache-specific, so a custom Cache falls back to per-caller fetches.
func (c *baseClient) startCSCMissCoalescer() {
	if !cscCoalesceMissesEnabled(c.opt) || c.cscMissCoalescer.Load() != nil {
		return
	}
	if _, ok := c.csc.(*LocalCache); !ok {
		return
	}
	// Cheap fast path: if CSC serving is already known inactive, skip building
	// anything. Not the safety mechanism on its own — see the workersMu check
	// below, which is what actually closes the construction/teardown race.
	if a := c.cscActive; a != nil && !a.Load() {
		return
	}
	mc := &cscMissCoalescer{
		c:             c,
		ch:            make(chan *cscMissReq, cscMissQueueDepth),
		stop:          make(chan struct{}),
		maxBatchBytes: cscMissWriteBatchBytes(c.opt),
		serializeSem:  make(chan struct{}, cscMissMaxConcurrentSerialize),
	}

	// publish stores the coalescer and launches its N sessions. Extracted so
	// both the guarded and the no-drain-handle path below run the exact same
	// sequence.
	publish := func() {
		c.cscMissCoalescer.Store(mc)
		// N independent full-duplex sessions, each holding its own connection and
		// pulling misses from the shared queue. Order-free: coalesced misses are
		// standalone per-key fetches with no cross-request contract, and each
		// session's per-conn reader still matches replies to its own requests.
		for i := 0; i < cscFullDuplexConnsDefault; i++ {
			mc.wg.Add(1)
			go mc.fullDuplexLoop()
		}
	}

	// Run publish under the drain handle's lock, checked against
	// workersTornDown (see cscDrainHandle, stopCSCRefresherAndCoalescer, and
	// the mirrored check in startCSCRefresher): a concurrent teardown may
	// already have consumed workersStopOnce — reachable via the drainer's own
	// self-disable tick reacting to an async conn init's disableCSCServing,
	// racing this construction. That teardown must not be handed a coalescer
	// it will never get another chance to stop (it would then hold a pool
	// connection for the client's remaining life), so decline to start at all
	// if it already ran. The WHOLE of publish runs inside the lock, not just
	// the Store: teardown's body sets workersTornDown as its own first action
	// under this same lock, so holding it across the wg.Add/go loop guarantees
	// teardown cannot call mc.wg.Wait() (in stopCSCMissCoalescer) concurrently
	// with an in-progress mc.wg.Add — sync.WaitGroup's documented misuse case,
	// which can panic — because teardown cannot even read the published
	// pointer until every Add has happened (a second bot review caught that
	// the Store-only version left this window open). dh is nil only for tests
	// that call this directly without a drain handle; there is nothing to race
	// against in that case.
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

func (c *baseClient) stopCSCMissCoalescer() {
	// Swap, not load-then-nil: exactly one caller wins even if Close races the
	// GC cleanup path, so mc.stop is closed once.
	mc := c.cscMissCoalescer.Swap(nil)
	if mc == nil {
		return
	}
	mc.stopWorkers()
	mc.wg.Wait()
	// Any request still queued after stop is drained and settled so no caller
	// hangs on its done channel. Retry-uncached, matching fetch's stop paths:
	// teardown deactivates serving before stopping the coalescer, so a caller
	// woken here re-runs its read on the (possibly still open) pool instead of
	// surfacing a spurious ErrClosed mid-window; on a truly closed client the
	// uncached re-run fails with the real error.
	for {
		select {
		case req := <-mc.ch:
			mc.c.csc.Cancel(req.cacheKey, req.token)
			mc.settle(req, errCSCRetryUncached)
		default:
			return
		}
	}
}

// stopWorkers signals every coalescer goroutine to exit; idempotent, does not
// wait. Called by stopCSCMissCoalescer (Close) and by the GC cleanup for a client
// dropped without Close — the sessions retain the base client (cache, pools), so
// leaving them running leaks all of it per forgotten client.
func (mc *cscMissCoalescer) stopWorkers() {
	mc.stopOnce.Do(func() { close(mc.stop) })
}

// fetch hands a reserved miss to the coalescer and waits for the result. The
// caller has already Reserved (shouldFetch==true); the coalescer owns the token
// from here. Honors the caller's context: if it cancels while waiting, the
// session still settles the token and populates the cache (the fetch is not
// wasted), the caller just returns early.
//
// served is the session connection that produced the settle (nil when the
// request never reached one); processCached stamps it into processState so the
// native OTel recorder attributes the miss like any other command.
// reserveWireBytes reserves n bytes of the in-flight wire budget. It returns false
// (reserving nothing) when the reservation would exceed cscMissWireBudgetBytes, so
// the caller sheds to the pooled path instead of holding a wire copy while blocked
// on the send. releaseWireBytes returns a prior reservation.
func (mc *cscMissCoalescer) reserveWireBytes(n int64) bool {
	if mc.wireBytes.Add(n) > cscMissWireBudgetBytes {
		mc.wireBytes.Add(-n)
		return false
	}
	return true
}

func (mc *cscMissCoalescer) releaseWireBytes(n int64) { mc.wireBytes.Add(-n) }

// acquireSerialize takes a serialization slot (see serializeSem/
// cscMissMaxConcurrentSerialize) and reports whether it succeeded. It tries once
// without blocking, then waits up to cscMissSerializeWait — a slot frees in
// microseconds because serialization never blocks on I/O — and sheds (returns
// false) on the wait or on Close rather than blocking unbounded. A nil sem (test
// literal) is unbounded. Every true return is paired with exactly one
// releaseSerialize.
func (mc *cscMissCoalescer) acquireSerialize() bool {
	if mc.serializeSem == nil {
		return true
	}
	select {
	case mc.serializeSem <- struct{}{}:
		mc.noteSerializing()
		return true
	default:
	}
	if cscMissSerializeWait <= 0 {
		return false
	}
	t := time.NewTimer(cscMissSerializeWait)
	defer t.Stop()
	select {
	case mc.serializeSem <- struct{}{}:
		mc.noteSerializing()
		return true
	case <-t.C:
		return false
	case <-mc.stop:
		return false
	}
}

// releaseSerialize returns a slot taken by a successful acquireSerialize.
func (mc *cscMissCoalescer) releaseSerialize() {
	if mc.serializeSem == nil {
		return
	}
	mc.serializing.Add(-1)
	<-mc.serializeSem
}

// noteSerializing records the peak concurrent serialization count for the
// admission-bound test; called only under a freshly taken slot.
func (mc *cscMissCoalescer) noteSerializing() {
	n := uint64(mc.serializing.Add(1))
	for {
		m := mc.maxSerializing.Load()
		if n <= m || mc.maxSerializing.CompareAndSwap(m, n) {
			return
		}
	}
}

// reconcileWireBytes corrects req's reservation to the ACTUAL serialized size once
// req.wire is built, and reports whether the request still fits the budget. The
// reservation is charged from cmdApproxBytes BEFORE serialization (so an
// over-budget miss never allocates the wire). That estimate sizes string, []byte,
// *string and BinaryMarshaler args exactly, but it is still an estimate: a
// marshaler whose output differs between the sizing call and writeCmd's own
// MarshalBinary call, or a numeric/time arg whose text exceeds the fixed 8-byte
// fallback, serializes larger than charged. Without a recheck, N concurrent such
// misses all pass the pre-serialize gate, and then charging their true size drifts
// the in-flight total past cscMissWireBudgetBytes while every snapshot stays
// queued.
//
// Returns true (keep) when the actual size fits: the delta is charged and
// req.reserved advanced to actual, so settle() releases exactly once. Returns
// false (shed) when charging the actual size would exceed the budget: the delta is
// rolled back, req.reserved is LEFT at the estimate (fetch's !enqueued defer
// releases that), and the caller sheds to the pooled path — so the over-budget
// wire is dropped, not retained in mc.ch/inflight. A shrinking delta (actual <
// estimate) always keeps.
func (mc *cscMissCoalescer) reconcileWireBytes(req *cscMissReq) bool {
	actual := int64(len(req.wire))
	delta := actual - req.reserved
	if delta <= 0 {
		if delta != 0 {
			mc.wireBytes.Add(delta)
			req.reserved = actual
		}
		return true
	}
	if mc.wireBytes.Add(delta) > cscMissWireBudgetBytes {
		mc.wireBytes.Add(-delta) // roll back; reserved stays the estimate for the defer release
		return false
	}
	req.reserved = actual
	return true
}

// settle releases req's wire-budget reservation and wakes its caller, exactly
// once. Every path that finishes a QUEUED request (applyAndSettle, settleErr, and
// the shutdown drain) funnels through here, so the budget a request holds while its
// wire sits in mc.ch/inflight is returned precisely when that wire is done. Requests
// that never reach the queue release their reservation in fetch instead.
func (mc *cscMissCoalescer) settle(req *cscMissReq, err error) {
	mc.releaseWireBytes(req.reserved)
	req.done <- err
}

// emitReplyErr records the native error metric for a settled REPLY error (e.g.
// redis.Nil, WRONGTYPE) at the point the caller consumes it — the single
// emission site for coalesced reply errors, giving parity with processWithRetry
// on the uncoalesced path while staying exactly-once per operation. Excluded:
// session errors and shed retries (processCached re-runs those through
// processWithRetry, which emits its own outcome) and the cancellation branch (a
// caller returning ctx.Err() already emitted its cancellation; emitting the late
// reply too recorded two conflicting error types for one operation). Reading
// req.servedBy is safe here: the caller's req.done receive is the
// happens-before edge for that field.
func (mc *cscMissCoalescer) emitReplyErr(ctx context.Context, req *cscMissReq, err error) {
	if err == nil || err == errCSCRetryUncached {
		return
	}
	var sessErr cscSessionError
	if errors.As(err, &sessErr) {
		return
	}
	errorCallback := pool.GetMetricErrorCallback()
	if errorCallback == nil {
		return
	}
	errorType, statusCode, isInternal := classifyCommandError(err)
	errorCallback(ctx, errorType, req.servedBy, statusCode, isInternal, 0)
}

func (mc *cscMissCoalescer) fetch(ctx context.Context, cmd Cmder, cacheKey string, token uint64) (served *pool.Conn, err error) {
	req := &cscMissReq{cmd: cmd, cacheKey: cacheKey, token: token, done: make(chan error, 1)}
	// Bound total in-flight serialized bytes before allocating this command's wire
	// snapshot: reserve its approximate encoded size and, if that would exceed
	// cscMissWireBudgetBytes, shed to the ordinary pooled path (errCSCRetryUncached)
	// rather than serialize-and-block — so a burst of large misses cannot exhaust
	// memory with wire copies. Reserving BEFORE serialization means over-budget
	// callers never allocate the wire. A single command larger than the whole budget
	// sheds too (the pooled path runs it, so there is no progress hazard).
	approxBytes := cmdApproxBytes(cmd)
	if !mc.reserveWireBytes(approxBytes) {
		mc.c.csc.Cancel(cacheKey, token)
		return nil, errCSCRetryUncached
	}
	req.reserved = approxBytes
	// Release the reservation HERE only if the request never gets queued (serialize
	// error, or Close/ctx before enqueue). Once enqueued, ownership transfers to the
	// request and settle() releases it when its wire actually leaves mc.ch/inflight —
	// so a caller cancelling after enqueue does not free the budget while its wire
	// snapshot is still retained (the OOM vector a caller-lifetime release allowed).
	enqueued := false
	defer func() {
		if !enqueued {
			mc.releaseWireBytes(req.reserved)
			// Release the cache reservation too when the request never got queued,
			// including the panic path (writeCmd serializing a user arg that panics),
			// which skips the explicit Cancel on the error returns and would otherwise
			// leave the key IN_PROGRESS until StaleTimeout, blocking later readers.
			// Cancel is token-guarded, so this is a no-op next to the explicit Cancels.
			mc.c.csc.Cancel(cacheKey, token)
		}
	}()
	// Honor ContextTimeoutEnabled for the REPLY wait below (an enqueued request's
	// socket I/O): when it is false the ordinary command path drives socket I/O on
	// context.Background() (bounded by ReadTimeout, not the caller deadline), so a
	// coalesced miss must not surface context.DeadlineExceeded on the caller's
	// deadline. c.context returns the caller ctx when the policy is on and Background
	// when off. The ENQUEUE-admission wait instead honors the original ctx directly
	// (pre-I/O backpressure aborts on caller cancellation regardless of the policy —
	// see that select). mc.stop stays the unconditional shutdown signal.
	wctx := mc.c.context(ctx)
	// Bound the number of callers serializing a wire snapshot AT ONCE before
	// allocating this one: reserveWireBytes above gates on cmdApproxBytes, an
	// estimate that can still undercount (see reconcileWireBytes), so a concurrent
	// burst can all pass it and each allocate a full multi-MiB wireBuf before
	// reconcileWireBytes sheds — a transient set the byte budget never sees (#3965
	// F3). The slot is held ONLY across
	// writeCmd+reconcile and released before the enqueue send below, so it never
	// couples to queue backpressure. A caller that cannot get a slot sheds to the
	// pooled path (the !enqueued defer releases the byte reservation).
	if !mc.acquireSerialize() {
		mc.c.csc.Cancel(cacheKey, token)
		return nil, errCSCRetryUncached
	}
	// Release the slot even if writeCmd panics while serializing a user arg (a
	// panicking Marshaler): without this the slot leaks and counts against
	// cscMissMaxConcurrentSerialize forever, shedding later misses to the pooled
	// path. Cleared after each explicit release below, so the normal path still
	// releases before the enqueue select (the defer then no-ops).
	serializeHeld := true
	defer func() {
		if serializeHeld {
			mc.releaseSerialize()
		}
	}()
	// Snapshot the wire form NOW, while the caller still owns cmd: the session
	// writer writes these engine-owned bytes and never reads cmd again, so an
	// abandoning caller can immediately reuse mutable args (e.g. a []byte key)
	// without racing arg serialization, a mutated key can never be sent or
	// published under the original cache key, and the fetch always completes —
	// the reservation always settles. The reply side still claims cmd (apply
	// interlock) before writing the result into it.
	var wireBuf bytes.Buffer
	if err := writeCmd(proto.NewWriter(&wireBuf), cmd); err != nil {
		mc.releaseSerialize()
		serializeHeld = false
		mc.c.csc.Cancel(cacheKey, token)
		return nil, err
	}
	req.wire = wireBuf.Bytes()
	// Reconcile the reservation to the real serialized size now that the wire exists:
	// cmdApproxBytes is an estimate and can still undercount (see reconcileWireBytes).
	// If the true size would push the in-flight total over budget, shed to the pooled
	// path instead of enqueueing an over-budget snapshot (the !enqueued defer releases
	// the estimate). Without this, concurrent undercounted misses all pass the
	// pre-serialize gate and then blow the budget while every wire stays queued.
	fits := mc.reconcileWireBytes(req)
	// Release the serialization slot BEFORE the enqueue select: the snapshot exists
	// and is reconciled, so the transient-allocation concern the slot bounds is over.
	mc.releaseSerialize()
	serializeHeld = false
	if !fits {
		mc.c.csc.Cancel(cacheKey, token)
		return nil, errCSCRetryUncached
	}
	// Reject early if the coalescer is already stopping, so a send does not win the
	// select race against a closed mc.stop and land in mc.ch after the shutdown
	// drain, where nothing would pick it up. This narrows but cannot close that
	// race; the mc.stop case in the wait select below is what guarantees the caller
	// never hangs on a post-drain req.
	select {
	case <-mc.stop:
		// Retry-uncached, not ErrClosed: the coalescer stopping does not mean
		// the CLIENT is closed (teardown deactivates serving before stopping
		// the coalescer, and a clone can race that window) — the uncached
		// re-run either succeeds on the open pool or surfaces the real error.
		mc.c.csc.Cancel(cacheKey, token)
		return nil, errCSCRetryUncached
	default:
	}
	select {
	case mc.ch <- req:
		enqueued = true
	case <-mc.stop:
		mc.c.csc.Cancel(cacheKey, token)
		return nil, errCSCRetryUncached
	case <-ctx.Done():
		// Enqueue-admission backpressure is PRE-I/O (nothing written yet), so honor
		// the caller's cancellation directly on ctx — NOT wctx. wctx follows
		// ContextTimeoutEnabled because it gates SOCKET I/O (the reply-wait select
		// below); a caller cancelling while merely waiting for a queue slot must abort
		// regardless of that policy, matching the ordinary path passing ctx to
		// ConnPool.Get.
		mc.c.csc.Cancel(cacheKey, token)
		return nil, ctx.Err()
	}
	// The reply wait honors caller cancellation in two phases. PRE-I/O (the batch has
	// not reached a connection, req.sentConn == nil) a caller cancel aborts on the RAW
	// ctx regardless of ContextTimeoutEnabled — matching the ordinary path, which passes
	// ctx to ConnPool.Get and only drops to the policy ctx for socket I/O. Once I/O has
	// begun (sentConn set) cancellation follows wctx (the policy): ctxDone is disabled so
	// an enqueued command Redis is already serving is not abandoned early on a deadline
	// the ordinary socket-I/O path would not enforce.
	abandon := func() (*pool.Conn, error) {
		// Report the cancellation metric here, like processWithRetry: the reader may
		// still complete the background fetch (success via applyAndSettle, or its own
		// error via settleErr), so neither records THIS caller's cancellation — without
		// this it is undercounted whenever coalescing is on. Attribute to the SENT conn
		// via req.sentConn (an atomic view safe on this pre-req.done branch, unlike
		// servedBy, which would race the session's write): if the command was already
		// sent, Redis received it, so report that conn; nil only for a true pre-send cancel.
		sentConn := req.sentConn.Load()
		if errorCallback := pool.GetMetricErrorCallback(); errorCallback != nil {
			errorType, statusCode, isInternal := classifyCommandError(ctx.Err())
			errorCallback(ctx, errorType, sentConn, statusCode, isInternal, 0)
		}
		// Lost the interlock: the reader claimed cmd and is mid-apply — wait (the receive
		// is a happens-before edge) rather than race by reusing cmd, and return its
		// result. Won it: the reader skips the cmd write; in the LIVE case it still
		// publishes and settles the token, so leave it — except when stopping, where the
		// req may be stranded past the shutdown drain (cancelIfStopping). Return ctx.Err()
		// either way, matching processWithRetry.
		if !req.claimAbandon() {
			<-req.done
			return req.servedBy, ctx.Err()
		}
		mc.cancelIfStopping(cacheKey, token)
		return sentConn, ctx.Err()
	}
	ctxDone := ctx.Done()
	for {
		select {
		case err := <-req.done:
			mc.emitReplyErr(ctx, req, err)
			return req.servedBy, err
		case <-mc.stop:
			// Close raced our enqueue: the req may have landed in mc.ch after the
			// shutdown drain, so nothing will settle req.done. Winning the interlock
			// means no reply is being applied — cancel the reservation and return instead
			// of hanging (a duplicate Cancel, if the drain also got this req, is a no-op
			// on a settled token).
			if req.claimAbandon() {
				mc.c.csc.Cancel(cacheKey, token)
				// The req may be sitting in mc.ch AFTER the shutdown drain ran, where
				// no worker will ever dequeue it — a live WithTimeout clone's pointer
				// would then retain it (and its Cmder) indefinitely. Re-run the
				// non-blocking drain to empty the queue; settling our own abandoned
				// req is harmless (done is buffered, the duplicate Cancel is a no-op).
				mc.drainQueueErr(errCSCRetryUncached)
				return nil, errCSCRetryUncached
			}
			// The reader claimed cmd and is mid-apply: wait so we do not read/reuse
			// cmd concurrently (the receive is a happens-before edge), then return
			// the settle result itself — the reply may have been applied successfully.
			e := <-req.done
			mc.emitReplyErr(ctx, req, e)
			return req.servedBy, e
		case <-wctx.Done():
			// Caller stopped waiting under ContextTimeoutEnabled (otherwise wctx is
			// Background and never fires — matching the ordinary path's I/O timeout policy).
			return abandon()
		case <-ctxDone:
			// Raw caller cancellation. Honor it only PRE-I/O (sentConn == nil); once the
			// batch reached a conn, disable this case and fall through to the wctx policy,
			// so a command Redis is already serving is not abandoned early when
			// ContextTimeoutEnabled is off (wctx == Background there).
			if req.sentConn.Load() != nil {
				ctxDone = nil
				continue
			}
			return abandon()
		}
	}
}

// cancelIfStopping releases an abandoned request's reservation, but ONLY when the
// coalescer is stopping. On a caller-deadline abandon (fetch's wctx.Done branch,
// after the caller won the cmd interlock) the live reader still dequeues the req
// and publishes to the cache, so the token is left to it. Once mc.stop is closed
// though, the req may be stranded in mc.ch past the shutdown drain with nothing to
// settle it: its reservation would stay IN_PROGRESS and block a later reader of the
// key until StaleTimeout (worse on a shared cache not flushed on close). So mirror
// the mc.stop wait branch — cancel the token and re-drain the queue. The
// non-blocking select keeps the live path's publish intact; Cancel is token-guarded,
// so a later reader's fresh reservation is unaffected.
func (mc *cscMissCoalescer) cancelIfStopping(cacheKey string, token uint64) {
	select {
	case <-mc.stop:
		mc.c.csc.Cancel(cacheKey, token)
		mc.drainQueueErr(errCSCRetryUncached)
	default:
	}
}

// applyAndSettle applies raw to the caller's command, publishes to the cache when
// cacheable (under the reading connection's tracking generation), and wakes the
// caller. Sends req.done exactly once.
//
// It claims the cmd interlock first: an abandoned caller (context cancelled, or
// Close raced its enqueue) has its Cmder back and may be reading or reusing it,
// so the reply must NOT be written there. The fetch is still not wasted — the
// reply is classified from a throwaway parse and, when cacheable, published for
// the next reader; only the caller's cmd write is skipped.
func (mc *cscMissCoalescer) applyAndSettle(req *cscMissReq, raw []byte, connID, capturedGen uint64) {
	// A user CacheSizer (via fulfillCached) or a custom Cmder's reply parse (via
	// applyCachedReply) runs on the full-duplex reader goroutine, which has no other
	// recover. A panic here would crash the process AND leave this caller blocked on
	// req.done forever (the req is already off inflight, so the teardown drain can
	// never reach it). Recover, then settleErr: it cancels the reservation, tags a
	// cscSessionError so the caller re-runs on the pooled path, and wakes the caller.
	// The normal settle below is the terminal statement, so a panic never double-settles.
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(context.Background(),
				"csc: miss-coalesce apply/publish panic (recovered): %v", r)
			mc.settleErr(req, fmt.Errorf("csc: apply panic: %v", r))
		}
	}()
	c := mc.c
	var applyErr error
	if req.claimApply() {
		applyErr = applyCachedReply(req.cmd, raw)
	} else {
		// Abandoned: classify without touching the caller's cmd.
		applyErr = classifyCachedReply(raw)
		mc.abandonedApplies.Add(1)
	}
	if isCacheableReplyResult(applyErr) {
		fc := &cscFetchCapture{
			raw:     raw,
			connID:  connID,
			initGen: capturedGen,
			key:     req.cacheKey,
			token:   req.token,
		}
		c.fulfillCached(req.cacheKey, req.token, fc)
	} else {
		// WRONGTYPE / NOPERM / ...: returned to the caller, not cached.
		c.csc.Cancel(req.cacheKey, req.token)
	}
	// No error-metric emission here: the CALLER emits at its req.done receive
	// (emitReplyErr), which makes emission exactly-once per operation. Emitting on
	// this side too double-counted an op whose caller cancelled late — the caller
	// recorded its context cancellation while this reply error was recorded here,
	// two conflicting error types for one operation. An abandoned caller's op is
	// accounted by its cancellation alone.
	mc.settle(req, applyErr)
}

// cscMissStopDrainIntervals caps the whole Close-time (stopping) drain at N
// stall-detection intervals. Without a total cap the per-interval progress check
// renews forever, so a server trickling one reply per interval keeps a deep in-flight
// pipeline (up to cscFullDuplexDepth) draining and blocks Close in wg.Wait for hours.
// This is an explicit TRADEOFF against the unbounded-progress rule the non-stopping
// drain keeps (see drainBackstop): a deep, legitimately slow-but-progressing drain
// CAN exceed N intervals, and past that budget Close-never-wedges outranks completing
// the remainder — some accepted in-flight replies may be cut. N × interval scales the
// budget with the configured (and maintenance-relaxed) timeouts instead of a fixed
// wall-clock; N=8 keeps a normal Close prompt while bounding the trickle case to tens
// of seconds. Only the stopping path is capped; an age-out/handoff recycle stays
// unbounded (the client is alive, only this conn's recycle waits).
const cscMissStopDrainIntervals = 8

// batchBudget bounds one coalesced batch's write + reads (connection acquisition
// is bounded separately, by acquireCtx). It follows the client's configured
// timeouts instead of a fixed cap: a client that deliberately sets
// ReadTimeout/WriteTimeout high for slow cacheable reads must not see only its
// coalesced misses cut off early (WithReader clamps the read to min(ctx deadline,
// ReadTimeout), so a shorter ctx would win). The 5s floor covers scheduling
// overhead when the configured timeouts are tiny or disabled, and also keeps the
// drainBackstop stall interval (batchBudget + 1s) safely above the reader's per-read
// bound so a single slow reply is never mistaken for a stall.
func (mc *cscMissCoalescer) batchBudget() time.Duration {
	opt := mc.c.opt
	var budget time.Duration
	if opt.WriteTimeout > 0 {
		budget += opt.WriteTimeout
	}
	if opt.ReadTimeout > 0 {
		budget += opt.ReadTimeout
	}
	if budget < 5*time.Second {
		budget = 5 * time.Second
	}
	return budget
}

// countBatch records batch-size accounting. The max is a CAS loop: with
// concurrent sessions a load-then-store could commit a smaller value over a
// larger one that landed in between, permanently underreporting the maximum.
func (mc *cscMissCoalescer) countBatch(batch []*cscMissReq) {
	mc.batches.Add(1)
	mc.batched.Add(uint64(len(batch)))
	n := uint64(len(batch))
	for {
		cur := mc.maxBatchSz.Load()
		if n <= cur || mc.maxBatchSz.CompareAndSwap(cur, n) {
			return
		}
	}
}

// cscSessionError tags an error that comes from a coalescer session or transport
// failure (settleErr), not from the command's own reply (applyAndSettle).
// processCached uses this tag to tell the two apart. It re-runs a session failure
// on the normal path, which applies MaxRetries and backoff. It returns a
// reply-level result as-is (for example redis.Nil, WRONGTYPE, or a retryable reply
// such as LOADING), because that result is the answer. cscSessionError unwraps, so
// errors.Is and errors.As still see the cause.
type cscSessionError struct{ err error }

func (e cscSessionError) Error() string { return e.err.Error() }
func (e cscSessionError) Unwrap() error { return e.err }

// settleErr cancels the reservation and fails one waiting caller. It tags the
// error as cscSessionError, except the retry-uncached sentinel, which
// processCached matches by identity. The tag makes the caller re-run the read on
// the normal, fully instrumented path instead of surfacing a raw transport
// failure. That re-run emits the native error metric, so settleErr does not. To
// emit it here as well would double-count a re-run that fails, and would wrongly
// flag a re-run that succeeds. The retry-uncached path already used this rule.
// Every session failure now re-runs, so the rule applies to all of them.
func (mc *cscMissCoalescer) settleErr(req *cscMissReq, err error) {
	mc.c.csc.Cancel(req.cacheKey, req.token)
	if err != errCSCRetryUncached {
		err = cscSessionError{err}
	}
	mc.settle(req, err)
	mc.failed.Add(1)
}

// settleAllErr fails every request in batch from index `from` onward.
func (mc *cscMissCoalescer) settleAllErr(batch []*cscMissReq, from int, err error) {
	for i := from; i < len(batch); i++ {
		mc.settleErr(batch[i], err)
	}
}

// drainQueueErr fails every request currently queued (non-blocking). Used when a
// connection cannot be acquired, so a caller on a context without a deadline is
// not blocked forever and no reservation is left IN_PROGRESS.
func (mc *cscMissCoalescer) drainQueueErr(err error) {
	for {
		select {
		case req := <-mc.ch:
			mc.settleErr(req, err)
		default:
			return
		}
	}
}

// settlePlain cancels the reservation and fails one waiting caller with err
// UNTAGGED — no cscSessionError, no retry-uncached sentinel — so processCached
// returns it as-is instead of re-running on the pooled path. Used for a
// Limiter.Allow rejection at session acquire: a re-run would call Limiter.Allow a
// SECOND time and a stateful/token limiter could admit the very operation it just
// denied. The caller's req.done receive still emits the error metric once
// (emitReplyErr), matching the ordinary path's single emission for a rejection.
func (mc *cscMissCoalescer) settlePlain(req *cscMissReq, err error) {
	mc.c.csc.Cancel(req.cacheKey, req.token)
	mc.settle(req, err)
	mc.failed.Add(1)
}

// grabInto appends first, plus any misses already queued, into dst. It does not
// block. It stops at cscMissBatchMax commands or at mc.maxBatchBytes of serialized
// payload. It reuses dst's backing array. first always goes, whatever its size,
// because a single large command must not stall. The byte cap bounds only the
// extra already-queued misses that share the write.
//
// grabInto returns the batch and a carry. The carry is the one request it pulled
// that would push the batch past the byte cap. To pack that request on would
// exceed the write buffer and risk the mid-batch-flush deadlock. grabInto does NOT
// put the carry back on mc.ch; it returns it to the writer, which sends it as the
// first request of the next batch. This keeps the request in the writer's own
// state (never stranded on mc.ch during shutdown) and guarantees progress: a lone
// large request ships next as its own batch. The carry is nil when nothing was
// deferred.
func (mc *cscMissCoalescer) grabInto(dst []*cscMissReq, first *cscMissReq) ([]*cscMissReq, *cscMissReq) {
	dst = append(dst, first)
	nbytes := len(first.wire)
	for len(dst) < cscMissBatchMax {
		select {
		case more := <-mc.ch:
			if nbytes+len(more.wire) > mc.maxBatchBytes {
				return dst, more // defer: carry it to the next batch
			}
			dst = append(dst, more)
			nbytes += len(more.wire)
		default:
			return dst, nil
		}
	}
	return dst, nil
}

// Coalescer observability is the client's normal telemetry (the otel operation
// and error callbacks fire for coalesced misses like any other command path).
// The engine's internal counters (batched/batches/failed/maxBatchSz on
// cscMissCoalescer) exist for in-package tests, which read them directly off
// c.cscMissCoalescer — deliberately NOT exported as a stats API.
