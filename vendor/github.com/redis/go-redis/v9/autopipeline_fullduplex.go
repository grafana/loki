package redis

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/otel"
	"github.com/redis/go-redis/v9/internal/pool"
	"github.com/redis/go-redis/v9/internal/proto"
)

// Ordered full-duplex dispatch for the async and blocking AutoPipeline faces.
//
// Half-duplex sends one batch per round trip. A slow link caps throughput at
// batch/RTT, and a late command waits one RTT behind the in-flight batch.
// Full-duplex holds one pipeline-pool connection with a writer goroutine and a
// reader goroutine. The writer streams command groups without waiting for
// replies. The reader drains replies in FIFO order and completes each command
// when its reply lands. Latency is about 1 RTT; throughput saturates the pipe.
//
// Ordering: each goroutine's commands run in submit order. Order between
// goroutines is not defined. The submit channel is MPSC and the connection is
// FIFO on the wire, so a command's position in the in-flight deque matches its
// reply.
//
// Retries: on a connection failure the engine re-issues the unacked tail, in
// order, on a fresh connection ahead of new work. It honors shouldRetry,
// MaxRetries, backoff, and the per-command NoRetry flag. A NoRetry command in the
// tail fails the tail instead of replaying it (half-duplex uses cmdsContainNoRetry
// for the same result). After the budget is spent it fails those commands and
// keeps serving on a fresh connection. The at-least-once contract matches a normal
// Pipeline: a command whose write landed but whose reply was lost can re-execute.
// Only the unacked tail is ambiguous.
//
// Enable it with AutoPipelineOptions.FullDuplex. It runs on the ordered
// single-shard faces of a standalone *Client with a pipeline pool. Tune it with
// the FullDuplex* options (see their GoDoc). RESP3 push frames are demuxed inline.
// Cluster support and window auto-tune are follow-ups (see
// AP_ORDERED_FULLDUPLEX_DESIGN.md).

var errFDReaderGone = errors.New("redis: autopipeline full-duplex reader exited")

// errFDPanicRecovered marks a session failure from a recovered panic (reply
// decode or batch encode). It wraps with %w so the retry decision recognizes it:
// the connection is desynced like a transport error, so the engine replays the
// unacked tail on a fresh connection instead of failing it. Most of that tail is
// commands the panic never touched. shouldRetry alone would reject these plain
// error values and fail innocent in-flight commands.
var errFDPanicRecovered = errors.New("redis: autopipeline: full-duplex panic recovered")

// errFDPushDrainFailed marks a session failure from a push-notification drain
// error on the reply path. A custom PushNotificationProcessor can consume part of
// a frame and desync the reader. It wraps with %w like errFDPanicRecovered so the
// retry decision recognizes it: the connection is desynced, so the next read would
// misalign the FIFO. The engine fails the session and replays the unacked tail on
// a fresh connection instead of reading shifted bytes.
var errFDPushDrainFailed = errors.New("redis: autopipeline: full-duplex push drain failed")

// fdReplyIsFatal reports whether a per-reply read error must ABORT the FD session
// (stop the reader and leave the unread tail for replay) instead of being treated
// as this command's reply. A push-drain desync is always fatal, even when it wraps
// a Redis-typed cause. errFDPushDrainFailed carries the processor error via %w for
// errors.Is/As, but that cause must not let isRedisError reclassify the desync as a
// normal reply: settling or diverting it would leave the unread frame in the stream
// and shift every later reply. Any other error is fatal only when it is not a Redis
// error (a real transport or protocol failure). A plain Redis error is a real reply
// and is handled inline.
//
// A *RawWriteToCmd is the exception: it streams the raw reply — INCLUDING a
// server error line — straight to the caller's io.Writer and returns nil for a
// server reply, so a non-nil error from its readReply is never a server reply.
// It is a sink or socket failure mid-frame, which leaves the payload unread and
// the socket desynced. That is session-fatal even when the sink error itself
// implements redis.Error (isRedisError would otherwise call it a benign reply,
// advance the reader, and let the next command read the leftover bytes). Classify
// by the command type, not the error, for this one streaming Cmder.
func fdReplyIsFatal(cmd Cmder, e error) bool {
	if _, ok := cmd.(*RawWriteToCmd); ok {
		return true
	}
	return errors.Is(e, errFDPushDrainFailed) || !isRedisError(e)
}

// fdReadReplySafe reads one reply, turning a panic in the decoder (e.g. a
// RawWriteToCmd whose user io.Writer panics) into a fatal read error for that
// command. The grouped reader parses several replies before completing any of
// them, so a panic escaping to the goroutine's recover would skip completing
// the replies already read in the same group, and session recovery would
// replay them. As an error, it takes the fatal-reply path instead: the
// replies before it complete, and recovery starts at this command, which is
// what the per-reply loop did.
func fdReadReplySafe(cmd Cmder, rd *proto.Reader) (err error) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(context.Background(),
				"autopipeline: recovered full-duplex reply decoder panic: %v\n%s", r, debug.Stack())
			err = fmt.Errorf("%w: reply decoder: %v", errFDPanicRecovered, r)
		}
	}()
	return cmd.readReply(rd)
}

// fdPushDrainSafe drains pending push notifications before a grouped reply,
// turning a panic in a push handler (CSC invalidation, a registered or custom
// processor) into a drain error, as fdReadReplySafe does for the decoder.
// Escaping the group's WithReader, the panic left the replies the group had
// already read uncompleted, and session recovery replayed them.
func (fd *fdEngine) fdPushDrainSafe(ctx context.Context, cn *pool.Conn, rd *proto.Reader) (err error) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(ctx,
				"autopipeline: recovered full-duplex push handler panic: %v\n%s", r, debug.Stack())
			err = fmt.Errorf("%w: push handler: %v", errFDPanicRecovered, r)
		}
	}()
	return fd.client.processPendingPushNotificationWithReader(ctx, cn, rd)
}

// errFDRetryBudgetExhausted fails a carried command that has already spent its full
// retry budget (attempts > MaxRetries) when Close routes the unacked tail to
// shutdownFlush. The shutdown pipeline must not grant another MaxRetries+1
// executions and push a mutating command past its budget.
var errFDRetryBudgetExhausted = errors.New("redis: autopipeline: retry budget exhausted before shutdown flush")

// fdPartitionByBudget splits a carried tail into commands that still have retry
// budget (kept) and commands that have spent it (exhausted, attempts > maxRetries —
// one FD attempt plus maxRetries replays). It always allocates a fresh kept slice.
// carry is caller-owned (the unacked tail, or a handoff suffix that may alias the
// in-flight ring), and shutdownFlush appends the queue to kept, so a write into
// carry's backing array would corrupt the caller's slice.
func fdPartitionByBudget(carry []fdReq, maxRetries int) (kept, exhausted []fdReq) {
	kept = make([]fdReq, 0, len(carry))
	for _, r := range carry {
		if r.attempts > maxRetries {
			exhausted = append(exhausted, r)
			continue
		}
		kept = append(kept, r)
	}
	return kept, exhausted
}

// errFDConnMoving signals that carry replay stopped early because the held
// connection was marked for handoff (MOVING/FAILING_OVER) while a recovered carry
// was still being written. The connection is still alive, so writeCarryChunked
// returns the UNWRITTEN suffix out-of-band, not pushed into the in-flight deque.
// The session drains the already-written prefix to completion (those callers get
// real replies and are never re-executed) and Puts the connection back through a
// clean fdRecycle. The maintnotifications OnPut hook then performs the seamless
// handoff: queueHandoff plus MarkQueuedForHandoff clears ShouldHandoff, so the conn
// is reusable and the worker moves it to the new endpoint. Only the never-sent
// suffix replays on the next lease. Contrast errFDReaderGone and write errors,
// where the connection is dead: there the suffix IS pushed into the deque and the
// whole unacked tail replays, because a clean drain is impossible.
var errFDConnMoving = errors.New("redis: autopipeline: full-duplex connection moving")

// errFDMaxHold signals that carry replay stopped early because the connection has
// been held past FullDuplexMaxHold while re-issuing a recovered tail (a long
// replay under continuous load, or one stalled on window backpressure, never
// reaches the serve loop where max-hold is normally observed). Like errFDConnMoving
// the connection is still ALIVE: writeCarryChunked returns the UNWRITTEN suffix
// out-of-band, the session drains the already-written prefix to completion (those
// callers get real replies, never re-executed), and a clean fdRecycle Puts the
// connection back so the hold ends. Only the never-sent suffix replays on the next
// lease. Observed only on the LIVE path (ap.ctx not cancelled); a terminating Close
// bounds its own flush and outranks max-hold.
var errFDMaxHold = errors.New("redis: autopipeline: full-duplex connection max-hold reached")

// Full-duplex tuning defaults, applied by newFDEngine when the corresponding
// AutoPipelineOptions field is zero (rationale in the FullDuplex* GoDoc). The
// window must exceed the bandwidth-delay product (RTT × target rate) or it
// throttles throughput; the deque holds only ACTUAL in-flight, so a generous
// default costs no memory until a stalled peer makes in-flight grow.
const (
	fdDefaultWindow  = 65536
	fdDefaultIdle    = time.Second
	fdDefaultMaxHold = 5 * time.Second
)

// fdCloseFlushWait bounds how long the graceful-Close backlog flush waits for
// the reader to drain below the window before giving up the window bound (see
// writeCarryChunked). Long enough that a live reader always drains within it,
// short enough that a stuck reader (quiet peer, ReadTimeout disabled) does not
// stall Close.
const fdCloseFlushWait = time.Second

// fdResult is why a full-duplex session ended.
type fdResult int

const (
	fdGraceful fdResult = iota // AutoPipeliner Close: engine exits
	fdConnErr                  // connection failure: unacked tail returned for replay
	fdIdle                     // idle: conn returned cleanly; re-lease on next command
	fdRecycle                  // max-hold: conn returned cleanly; re-lease immediately (work pending)
	fdLeaseErr                 // could not lease/init a conn for a new session: retry, then fail carry + backlog after MaxRetries
)

// fdReq pairs a command with the per-command apBatch whose done channel is
// closed once that command's reply has landed (or it is finally failed).
//
// hookDone is non-nil only when the client has process hooks: the command then
// has a host goroutine (hostHook) running the hook chain, and finalizing closes
// hookDone instead of the batch — the host closes the batch after the hook
// returns, so the hook brackets the command and can rewrite its result before the
// waiter wakes. Nil (the hook-free fast path) finalizes the batch directly.
type fdReq struct {
	cmd      Cmder
	batch    *apBatch
	hookDone chan struct{}
	// ctx is the caller's submit context, kept so the per-command OTel metric can
	// be recorded against it (span/baggage correlation), mirroring process().
	ctx context.Context
	// writtenOff is stamped at the command's FIRST flush to the wire and kept across
	// replays; the reader uses write→reply as the command's operation duration for
	// the OTel metric. Anchoring on the first write (not the last replay) makes the
	// duration span the whole retry sequence, matching the normal command path,
	// instead of timing only the final attempt.
	// writtenOff is nanoseconds since the engine epoch (fdEngine.epoch), or 0
	// when the command has not been written yet. An int64 offset rather than a
	// time.Time because time.Time is 24 bytes of a ~100 byte fdReq that is
	// copied at least three times per command (into the queue, out of it with
	// the wave, into the in-flight ring) -- runtime.duffcopy measured 4.5% of
	// all CPU, 46% of it copying fdReq.
	//
	// The offset comes from now.Sub(epoch), so it inherits the MONOTONIC reading
	// that time.Time carries and the duration metric stays immune to a
	// wall-clock step mid-command. A UnixNano() would be 8 bytes too, but would
	// lose exactly that.
	writtenOff int64
	// attempts counts how many times this command has been issued: 1 at submit,
	// incremented on each connection-error replay of the carried tail. Fed to the
	// OTel duration/error callbacks so a command that succeeded on a replacement
	// connection reports its real attempt count (retry_attempts), matching the
	// normal command path instead of always reporting a single attempt.
	attempts int
	// sent is set once the command MAY have reached the wire. writeBatch stamps it
	// before the flush, so a partial write or an encoder panic still counts. It is
	// sticky across replays. The NoRetry gate keys on it: a never-sent NoRetry command
	// is replayable, because issuing it is its FIRST send; a sent NoRetry command must
	// be failed rather than risk a second execution. Without the marker the gate failed
	// never-sent NoRetry commands recovered from a dead connection's backlog, returning
	// an error for a command the server never saw.
	sent bool
	// pipelined marks a command submitted as part of an FD pipeline batch
	// (submitBatch). The reader settles its retryable replies inline instead of
	// diverting them one by one: an individual retry would let later commands
	// of the same pipeline run before it. fdPipelineExec retries the whole batch
	// instead, the way an ordinary pipeline does. Next to sent, so it fits in
	// existing padding and does not grow the struct.
	pipelined bool
	// limReport is the Limiter obligation for the WRITTEN chunk this req closes:
	// non-nil ONLY on the LAST req of a chunk that Allow() admitted and writeBatch
	// then wrote cleanly. It rides the in-flight deque so the reply-side outcome
	// settles the chunk's single ReportResult — nil once every reply of the chunk
	// has been read (reader), or the transport error when the chunk's unread
	// replies are abandoned (settleTail). A write-failed chunk reports at write
	// time and carries no obligation here. See fdLimiterReport.
	limReport *fdLimiterReport
}

// fdLimiterReport is one chunk's outstanding Limiter obligation: the pending
// ReportResult that must fire exactly once for the Allow() that admitted the chunk
// in writeBatch. Reporting the WRITE outcome was wrong for a circuit breaker
// (finding ed53z): a peer that accepts the write but closes before replying, with
// replay writes also succeeding, would only ever feed the breaker success and never
// open it. So the obligation records the REPLY-side outcome. The reader settles
// ReportResult(nil) once every reply of the chunk has landed (a server that answers
// is healthy, reply-level errors included). A transport failure that abandons the
// chunk's unread replies settles the error (settleTail). The CAS makes it
// exactly-once, since the reader and a failure path can both reach the same
// obligation.
type fdLimiterReport struct {
	lim  Limiter
	done atomic.Bool
}

// settle fires ReportResult(err) at most once for this obligation. A nil
// receiver (a chunk with no Limiter) and every call after the first are no-ops.
//
// ReportResult is user code, run on FD background goroutines (the reader's success
// path, plus the write-failure and settleTail paths). A panic must not reach the
// reader's session-failure recovery, which would replay an already-consumed reply
// and execute the command twice, and must not escape fd.run. Recover and swallow it:
// the outcome is already decided, reporting is fire-and-forget, and the CAS already
// fired, so the strict Allow/ReportResult pairing still holds.
func (o *fdLimiterReport) settle(err error) {
	if o == nil {
		return
	}
	if o.done.CompareAndSwap(false, true) {
		defer func() {
			if r := recover(); r != nil {
				internal.Logger.Printf(context.Background(),
					"autopipeline: recovered full-duplex limiter ReportResult panic: %v\n%s", r, debug.Stack())
			}
		}()
		o.lim.ReportResult(err)
	}
}

// complete finalizes a command whose result is already set on it: it wakes the
// caller directly, or (when hooks are present) hands off to the command's host
// goroutine, which runs the hook chain and then wakes the caller.
func (r fdReq) complete() {
	if r.hookDone != nil {
		close(r.hookDone)
		return
	}
	r.batch.close()
}

// fdInflight is an ordered FIFO of written-but-unacknowledged commands. The
// writer appends to the back; the reader reads the front's reply then pops it.
// On a connection failure the remaining entries (front→back) are exactly the
// unacked tail, in order, ready to replay. Two close modes:
//   - graceful: no more pushes, but the reader keeps reading the remaining
//     replies and exits once drained (clean Close).
//   - recover: hard stop; the reader abandons the remaining, which are returned
//     to the retry loop for replay.
type fdInflight struct {
	mu   sync.Mutex
	cond *sync.Cond
	// buf is a ring buffer of in-flight entries: the writer appends at the back
	// (head+count), the reader pops from the front (head). A grow-only slice
	// (append + reslice-off-front) reallocated its backing array on every window
	// churn — the single largest allocation source under load — because the
	// popped-off prefix was never reused. The ring reuses the whole array for the
	// life of the session; it only grows when live count would exceed capacity.
	buf        []fdReq
	head       int           // index of the front (oldest) live entry
	count      int           // number of live entries
	noMorePush bool          // graceful: drain remaining then reader exits
	hardClosed bool          // recover: reader stops immediately, remaining replayed
	room       chan struct{} // cap-1 signal: the reader popped, so there is room
	peak       int           // high-water mark of live count; observability for the backpressure test
	advanced   int           // total entries the reader completed this session (progress signal)
}

//nolint:unused // used by the full-duplex tests; lint runs with tests:false.
func newFDInflight() *fdInflight { return newFDInflightCap(0) }

// newFDInflightCap presizes the ring so a busy session avoids repeated early
// reallocations, capping the initial size at min(maxBatch, window). The window is
// the hard ceiling on live count: the writer blocks once in-flight reaches it and
// caps each drain by the remaining room. So a MaxBatchSize larger than the window
// must not presize beyond it (MaxBatchSize is an uncapped soft per-flush threshold):
// a huge MaxBatchSize with a small window would allocate that many entries up front
// (tens of MB, or an OOM) and again after every idle or recycle. Capping at maxBatch
// keeps the common small-batch default unchanged. grow() is the backstop up to the
// window as load ramps. Tests use the zero-cap no-arg form and let it grow.
func newFDInflightCap(initialCap int) *fdInflight {
	f := &fdInflight{room: make(chan struct{}, 1)}
	if initialCap > 0 {
		f.buf = make([]fdReq, initialCap)
	}
	f.cond = sync.NewCond(&f.mu)
	return f
}

func (f *fdInflight) len() int {
	f.mu.Lock()
	n := f.count
	f.mu.Unlock()
	return n
}

// grow ensures the ring can hold at least need entries, preserving FIFO order
// and normalizing the front to index 0. Caller holds f.mu.
func (f *fdInflight) grow(need int) {
	if need <= len(f.buf) {
		return
	}
	nc := len(f.buf) * 2
	if nc < need {
		nc = need
	}
	if nc < 8 {
		nc = 8
	}
	nb := make([]fdReq, nc)
	// Unwrap the live entries into the new buffer starting at 0.
	for i := 0; i < f.count; i++ {
		nb[i] = f.buf[(f.head+i)%len(f.buf)]
	}
	f.buf = nb
	f.head = 0
}

// pushBatch appends a whole write batch under one lock (fewer lock ops than
// per-command push — matters at loopback op rates).
func (f *fdInflight) pushBatch(reqs []fdReq) {
	f.mu.Lock()
	f.grow(f.count + len(reqs))
	for _, r := range reqs {
		f.buf[(f.head+f.count)%len(f.buf)] = r
		f.count++
	}
	if f.count > f.peak {
		f.peak = f.count
	}
	f.cond.Signal()
	f.mu.Unlock()
}

// peakLen returns the high-water mark of in-flight entries seen so far (test
// observability for the backpressure bound).
//
//nolint:unused // used by the full-duplex backpressure tests; lint runs with tests:false.
func (f *fdInflight) peakLen() int {
	f.mu.Lock()
	n := f.peak
	f.mu.Unlock()
	return n
}

// fdReadBatch caps how many replies the reader snapshots per lock acquisition:
// enough to amortize the mutex over many reads, small enough that the reader
// advances (and signals writer room) frequently even with a deep in-flight.
const fdReadBatch = 256

// frontBatch blocks until entries are available (or the deque is closing) and
// returns a snapshot of the front (up to fdReadBatch). ok=false means the reader
// should exit. The writer only ever appends at the back, so this prefix stays the
// front until the reader advance()s it. The snapshot is copied into the caller's
// buf (may span the ring's wrap seam as two segments), so it never aliases the
// backing array.
func (f *fdInflight) frontBatch(buf []fdReq) ([]fdReq, bool) {
	f.mu.Lock()
	for f.count == 0 && !f.noMorePush && !f.hardClosed {
		f.cond.Wait()
	}
	if f.hardClosed || f.count == 0 {
		f.mu.Unlock()
		return buf[:0], false
	}
	n := f.count
	if n > fdReadBatch {
		n = fdReadBatch
	}
	buf = buf[:0]
	// First segment: head .. min(end-of-array, head+n).
	seg := len(f.buf) - f.head
	if seg > n {
		seg = n
	}
	buf = append(buf, f.buf[f.head:f.head+seg]...)
	if seg < n {
		// Wrapped: remainder from the start of the array.
		buf = append(buf, f.buf[:n-seg]...)
	}
	f.mu.Unlock()
	return buf, true
}

// advance removes the front n entries the reader has completed and signals the
// writer that in-flight has room.
func (f *fdInflight) advance(n int) {
	if n <= 0 {
		return
	}
	f.mu.Lock()
	if n > f.count {
		n = f.count
	}
	if n == 0 {
		// Nothing to drop (empty ring, or clamped away). Return before the modulo
		// below, which divides by len(f.buf) and would panic on a never-grown ring
		// (nil buf). The slice implementation tolerated advance on an empty queue;
		// preserve that.
		f.mu.Unlock()
		return
	}
	// Zero each consumed entry before dropping it: the ring keeps its backing
	// array for the whole session (curInflight holds the deque while the engine
	// idles), so otherwise a drained burst retains a window's worth of completed
	// fdReq values — command args, caller contexts, batches — until the slot is
	// overwritten by a later push.
	for i := 0; i < n; i++ {
		f.buf[(f.head+i)%len(f.buf)] = fdReq{}
	}
	f.head = (f.head + n) % len(f.buf)
	f.count -= n
	f.advanced += n
	f.mu.Unlock()
	select {
	case f.room <- struct{}{}:
	default:
	}
}

// advancedTotal reports how many commands the reader completed this session —
// the progress signal that resets the reconnect retry budget (a session that
// completed work makes the next connection drop a NEW failure, not a
// consecutive one).
func (f *fdInflight) advancedTotal() int {
	f.mu.Lock()
	n := f.advanced
	f.mu.Unlock()
	return n
}

func (f *fdInflight) empty() bool {
	f.mu.Lock()
	n := f.count
	f.mu.Unlock()
	return n == 0
}

func (f *fdInflight) closeGraceful() {
	f.mu.Lock()
	f.noMorePush = true
	f.cond.Broadcast()
	f.mu.Unlock()
}

// hardClose signals the reader to stop immediately (used on a connection error).
// It deliberately does NOT take the queue: the caller must wait for the reader to
// exit (<-readerDone) and THEN call takeRemaining, so every entry stays owned by
// exactly one of {the reader completed it, recovery replays/fails it}. A
// concurrent grab could scoop an entry the reader had completed but not yet
// advanced, handing an already-executed command to the retry loop — a double
// execution and, on the hooked path, a double close of hookDone (panic).
func (f *fdInflight) hardClose() {
	f.mu.Lock()
	f.hardClosed = true
	f.cond.Broadcast()
	f.mu.Unlock()
}

// takeRemaining returns the entries the reader left unacknowledged, in order,
// and clears the queue. Call ONLY after the reader has exited (<-readerDone):
// the reader advance()s every command it completes, so what remains is exactly
// the unacked tail, and with the reader gone there is no concurrent access. The
// live entries are unwrapped into a fresh contiguous slice (they may span the
// ring's wrap seam); the ring is terminal for the session, so the backing array
// is dropped.
func (f *fdInflight) takeRemaining() []fdReq {
	f.mu.Lock()
	if f.count == 0 {
		f.buf = nil
		f.head = 0
		f.mu.Unlock()
		return nil
	}
	rem := make([]fdReq, f.count)
	for i := 0; i < f.count; i++ {
		rem[i] = f.buf[(f.head+i)%len(f.buf)]
	}
	f.buf = nil
	f.head = 0
	f.count = 0
	f.mu.Unlock()
	return rem
}

// fdAccumMinFor returns the in-flight depth above which the full-duplex writer
// will wait MaxFlushDelay for more commands before flushing (see the drain loop
// in session).
//
// The writer's drain is non-blocking: it flushes whatever is already queued. At
// low concurrency that is exactly right — a lone caller pays one round trip and
// nothing waits on its behalf. Under load it is pathological: measured on a
// 2-vCPU client at 1024 concurrent callers, the writer flushed ~4.6 commands per
// batch and issued ~75k write syscalls/sec, saturating the CPU, while the
// half-duplex path on the same connection batched ~460 commands per flush at
// 130% CPU.
//
// Waiting unconditionally is not the answer either: a fixed delay armed on every
// flush costs every low-concurrency command roughly a round trip (measured: 64
// callers went from 130k ops/sec at p50 0.48ms to 41k at p50 1.59ms). That is the
// same failure mode documented for the half-duplex path's old debounce timer,
// which is why that path coalesces on expected-arrival count instead.
//
// So the wait is gated on in-flight depth: below accumMin the writer never waits
// (low concurrency keeps its 1xRTT behaviour), above it the writer is
// demonstrably syscall-bound and coalescing pays. Derived from the window rather
// than hardcoded, so a caller who shrinks FullDuplexWindow also lowers the
// threshold; floored at 64 so a small window cannot make it trivially easy to
// trip.
//
// Measured with MaxFlushDelay=250us on a 2-vCPU client (64B GET/SET 70/30):
//
//	callers   unpatched          gated wait
//	     64   130k, p50 0.48ms   134k, p50 0.45ms   (unchanged, as intended)
//	    256   237k, 160% CPU     228k, 112% CPU     (same work, 30% less CPU)
//	   1024   345k, p99 4.78ms   420k, p99 3.53ms   (+22% ops, -26% p99)
//	   2048   308k, p99 10.1ms   386k, p99 7.19ms   (+25% ops, -29% p99)
//
// fdAccumGrace is how long the writer waits for the batch to GROW before it
// concludes nothing more is coming and flushes. MaxFlushDelay remains the hard
// ceiling on total wait; this decides how quickly an idle queue is noticed.
//
// It is deliberately absolute rather than a fraction of MaxFlushDelay. Those
// are different quantities: MaxFlushDelay is the caller's latency budget, while
// this is a property of how commands arrive. Tying them would mean a generous
// budget also made the writer slow to notice an idle queue -- re-introducing
// the regression that treating the budget as a real maximum removes, and
// preventing the budget from being raised to capture larger batches.
//
// 30us against a ~200us round trip: long enough that a busy writer keeps
// re-arming and rides to the ceiling, short enough that a writer whose callers
// are all blocked on replies gives up promptly instead of burning the budget.
const fdAccumGrace = 30 * time.Microsecond

// sinceWritten is a command's operation duration: now minus its first write.
// Both ends derive from the same monotonic epoch, so it is unaffected by
// wall-clock adjustments. A zero offset means "never written" and yields 0.
func (fd *fdEngine) sinceWritten(off int64) time.Duration {
	if off == 0 {
		return 0
	}
	return time.Since(fd.epoch) - time.Duration(off)
}

// writtenTime reconstructs the absolute first-write time for the retry path,
// which reports it to the normal command pipeline. A zero offset yields the
// zero Time, matching what an unwritten command carried before.
func (fd *fdEngine) writtenTime(off int64) time.Time {
	if off == 0 {
		return time.Time{}
	}
	return fd.epoch.Add(time.Duration(off))
}

func fdAccumMinFor(window int) int {
	const (
		floor = 64
		// 1/512 of the window: 128 at the default 65536. The right threshold
		// depends on the SUBMIT MECHANISM, not only on the workload. With the
		// channel submit path this same value measured -11.1% at 128 callers
		// (0/3 paired passes won), which is why #4014 gates at window>>7; with
		// the slice queue it measured +21.8% at 256 callers and +15.7% at 512
		// (3/3 passes each) with no harm at 128 (+1.3%), because the queue
		// takes a whole wave per lock instead of waking the writer per command.
		shift = 9
	)
	if m := window >> shift; m > floor {
		return m
	}
	return floor
}

type fdEngine struct {
	ap       *AutoPipeliner
	client   *Client
	pool     pool.Pooler
	q        *fdQueue // MPSC ordered queue: many submitters -> the writer (see fdQueue)
	maxBatch int
	window   int // max in-flight (written, unacked) before the writer waits
	// accumMin is the in-flight depth at which the writer is willing to wait
	// MaxFlushDelay for more commands before flushing. Derived from the window
	// so it scales with the configured pipeline depth instead of being a magic
	// constant; see fdAccumMinFor.
	accumMin int
	idle     time.Duration // return the conn after this idle gap (0 = never)
	maxHold  time.Duration // force a clean return at least this often (0 = never)

	recycles    atomic.Int64               // clean returns (idle + max-hold); observability/tests
	curInflight atomic.Pointer[fdInflight] // current session's in-flight deque; test observability
	curConn     atomic.Pointer[pool.Conn]  // current session's held conn; test observability (handoff)
	// curConnSpilled is true while the current session holds a MAIN-pool connection —
	// a spilled lease, or the no-dedicated-pool case where fd.pool IS the main pool.
	// retryOnNormalConn must not block the reader on retrySem then: the session pins a
	// main-pool conn until the reader drains, so off-pipe retries waiting on that same
	// pool would deadlock (see retryOnNormalConn). Set in attempt before the reader is
	// spawned; biased true when a lease is undecided so an unknown state never blocks.
	curConnSpilled atomic.Bool

	submitMu sync.RWMutex // guards closed; RLock across the submit send, WLock to close the gate
	// epoch anchors fdReq.writtenOff; set once when the engine is built.
	epoch  time.Time
	closed bool // set once run() is tearing down; submit then rejects new work

	retryWg  sync.WaitGroup // tracks off-pipe retries diverted to the normal client path; run() waits it so Close does too
	retrySem chan struct{}  // caps concurrent off-pipe retries at the window (see retryOnNormalConn)
	hostWg   sync.WaitGroup // tracks per-command hook-host goroutines (see hostHook); run() waits it so Close does not return while a post-next ProcessHook is still running

	// runPipeline runs a shutdown-flush chunk through the client's pipeline retry
	// loop. Test seam: nil in production (flushReqs falls back to
	// client.processPipelineRetries), so tests can drive flushReqs's chunk loop
	// without a live server.
	runPipeline func(ctx context.Context, cmds []Cmder, maxRetries int) error

	// reprocess re-runs a command that came back with a retryable reply or a
	// redirect on a NORMAL (non-FD) path. Defaults to client.processStartingAt (the
	// standalone client, which cannot follow a redirect). The cluster full-duplex
	// router overrides it (via AutoPipelineOptions.clusterReprocess) to route
	// through the redirect-aware ClusterClient. Set once in newFDEngine before
	// run() starts, so the reader goroutine reads it race-free.
	reprocess func(ctx context.Context, cmd Cmder, startAttempt int, writtenAt time.Time) error
	// redirectAware is true when reprocess follows MOVED/ASK (cluster mode). The
	// reply path then diverts a redirect to reprocess instead of surfacing it
	// inline. Standalone FD leaves this false: its normal path cannot follow a
	// redirect, so diverting would waste a round trip and return the redirect anyway.
	redirectAware bool
	// clusterRetryBudget is the connection-failure recovery budget used ONLY in
	// cluster mode (redirectAware): the ClusterClient's MaxRedirects, injected by the
	// router. See retryBudget().
	clusterRetryBudget int
}

// retryBudget bounds the engine's OWN connection-failure recovery: the lease retry
// loop, the carried-tail replay (fdPartitionByBudget) and the Close-path flush.
// Standalone FD uses the client's MaxRetries. Cluster node clients normalize
// MaxRetries to -1 (cluster retries live in MaxRedirects), so a cluster child would
// otherwise treat every carried command as budget-spent and fail all in-flight
// commands on the first socket error instead of replaying them; the router injects
// the ClusterClient's MaxRedirects (clusterRetryBudget) for that case. Reading the
// client's option live (rather than caching a field) keeps test-constructed engines
// working: they set client but not the cluster budget. The reply-side standalone
// retryable divert still uses client.opt.MaxRetries directly (only reached when
// !redirectAware).
func (fd *fdEngine) retryBudget() int {
	if fd.redirectAware {
		return fd.clusterRetryBudget
	}
	return fd.client.opt.MaxRetries
}

func newFDEngine(ap *AutoPipeliner, client *Client) *fdEngine {
	mb := ap.config.MaxBatchSize
	if mb <= 0 {
		mb = 200
	}
	// Resolve tuning ONCE here: a zero field means "use the default". In
	// particular window must never be 0 — the writer's backpressure gate is
	// `for inflight.len() >= window`, so window==0 (0 >= 0) would block the
	// writer on the very first submit. Validate rejects negatives.
	w := ap.config.FullDuplexWindow
	if w <= 0 {
		w = fdDefaultWindow
	}
	// Publish every resolved value so Config() reports what the engine actually
	// enforces (a zero field -> its default), not the raw 0 the user passed. Runs
	// once at construction before ap escapes newAutoPipeliner, so no Config() reader
	// races these writes. Validate rejects negatives. (MaxBatchSize is already
	// defaulted upstream in newAutoPipeliner, so it needs no write-back here.)
	ap.config.FullDuplexWindow = w
	idle := ap.config.FullDuplexIdleTimeout
	if idle <= 0 {
		idle = fdDefaultIdle
	}
	ap.config.FullDuplexIdleTimeout = idle
	maxHold := ap.config.FullDuplexMaxHold
	if maxHold <= 0 {
		maxHold = fdDefaultMaxHold
	}
	ap.config.FullDuplexMaxHold = maxHold
	// The submit queue is bounded by the window itself.
	//
	// It used to be capped at 4096 instead, because the submit path was a BUFFERED
	// CHANNEL and a channel allocates its whole capacity up front: several MiB per
	// engine at the default window, before a single command is submitted. The slice
	// queue that replaced it starts at 64 entries and grows to the live depth, so
	// that cost is gone and with it the reason for the cap.
	//
	// The cap was not free. It bounds ADMISSION, not memory: a caller whose batch
	// does not fit parks on the queue's cap-1 room signal and is woken one at a
	// time. With a 4096-slot queue, 4096 callers pipelining 10 commands each want
	// 40960 slots — 10x the queue — so ~3700 of them serialize on that relay.
	// Measured: 7,637 ops/s and a 5.1 s p50, against 1.6M ops/s and 25 ms once the
	// bound is the window.
	//
	// Total outstanding is still bounded, now by 2*window (queue + in-flight), and
	// the queue's buffer stays within about twice its live depth (see fdQueue).
	qCap := w
	// The off-pipe retry bound is a GOROUTINE budget, not a memory window. Diverted
	// retries serialize on the main pool's PoolSize connections, so slots beyond about
	// 2x the pool only hold 8 KiB stacks and pool-wait turns. Sizing it to w (default
	// 65536) let a retryable-reply storm — e.g. LOADING during a server restart, which
	// diverts EVERY reply — park tens of thousands of goroutines. 2x the pool keeps
	// backoff sleepers overlapping with pool waiters. The reader blocking on a full sem
	// is the designed end-to-end backpressure (see retryOnNormalConn); it now just
	// engages earlier.
	retryCap := 2 * client.opt.PoolSize
	if retryCap > w {
		retryCap = w
	}
	if retryCap < 1 {
		retryCap = 1
	}
	fd := &fdEngine{
		ap:     ap,
		client: client,
		pool:   client.getPipelinePool(),
		// Anchors every fdReq.writtenOff. Taken once here so the offsets carry a
		// monotonic reading: with the zero Time, Sub would saturate (the wall
		// clock is ~2000 years past it, far beyond a Duration's ~292-year range)
		// and time.Since would fall back to the wall clock, which is exactly the
		// property the offset exists to preserve.
		epoch:    time.Now(),
		q:        newFDQueue(qCap),
		maxBatch: mb,
		window:   w,
		accumMin: fdAccumMinFor(w),
		idle:     idle,
		maxHold:  maxHold,
		retrySem: make(chan struct{}, retryCap),
	}
	// Redirect/retry reprocess target. Default: the standalone client's own retry
	// path (cannot follow a redirect). Cluster mode injects a redirect-aware
	// ClusterClient path via config, which also flips redirectAware so the reply
	// path diverts MOVED/ASK. Set before run() starts (below, in the caller), so
	// the reader reads both fields without a race.
	if ap.config.clusterReprocess != nil {
		fd.reprocess = ap.config.clusterReprocess
		fd.redirectAware = true
		// Cluster node clients normalize MaxRetries to -1, which would make the
		// carry-replay budget treat every command as spent. Use the cluster's
		// MaxRedirects (where cluster retries live), injected by the router.
		// retryBudget() reads this when redirectAware.
		fd.clusterRetryBudget = ap.config.clusterRetryBudget
	} else {
		fd.reprocess = client.processStartingAt
	}
	return fd
}

// submit enqueues a command onto the ordered stream and returns its batch.
// Blocks when the queue is full (backpressure) or bails if the engine is
// closing. The caller (AutoPipeliner.submit) stamps setReady on the async face.
// With process hooks installed a per-command host goroutine runs the hook chain
// (see hostHook) and ctx parents its span; the hook-free path skips that
// goroutine and channel entirely.
func (fd *fdEngine) submit(ctx context.Context, cmd Cmder) *apBatch {
	if fd.ap.isClosed() {
		cmd.SetErr(ErrClosed)
		return completedBatch
	}
	var hookDone chan struct{}
	// With process hooks installed, run the chain on a per-command host goroutine
	// (see hostHook). Hooks are added before the client serves traffic, so the host
	// loads live hook state like the synchronous path. hookCount is one atomic load.
	if fd.ap.pipeliner.hookCount() > 0 {
		hookDone = make(chan struct{})
	}
	// Hook-free blocking face: the batch is a single-waiter completion signal
	// discarded after Wait, so draw it from the pool (buffered done, recycled in
	// processBlocking). Every other shape — the async face (batch installed on
	// the command, read repeatedly) and the hooked path (host goroutine owns
	// completion) — needs the close()-once channel.
	var b *apBatch
	if hookDone == nil && fd.ap.blocking {
		b = getFDBlockingBatch()
	} else {
		b = newAPBatch()
	}
	req := fdReq{cmd: cmd, batch: b, hookDone: hookDone, ctx: ctx, attempts: 1}

	// Send under RLock and re-check closed so a send can never win the race with
	// run()'s shutdown drain (takeQueue: WLock, set closed, drain fd.ch). Once the
	// final drain has run no new req can land in fd.ch, where it would never be
	// completed and would hang its caller forever. A send blocked on a full channel
	// is released by the ctx.Done() branch below, so holding the RLock cannot wedge
	// the WLock.
	fd.submitMu.RLock()
	if fd.closed {
		fd.submitMu.RUnlock()
		// Recycle the pooled completion batch drawn above but never admitted, so a
		// rejection does not discard one pooled batch + channel per command (a no-op
		// for the newAPBatch shape, which is not pooled).
		putFDBlockingBatch(b)
		cmd.SetErr(ErrClosed)
		// Submit-time rejection: return the shared completedBatch sentinel (no host
		// was started) so processAsync surfaces the error from raw Process(ctx,cmd),
		// matching every other submit-time-rejection path.
		return completedBatch
	}
	// Enqueue. push is a mutex plus an append, so it cannot block unless the queue
	// is at its bound; there is no select to wait on. On a full queue, wait for the
	// writer to take a wave.
	//
	// This is also why there is no fast-submit knob here. The channel this queue
	// replaced needed a blocking three-arm select on every submit, and skipping it
	// was worth enough throughput to justify an option that traded submit fairness
	// for it. A mutex-plus-append has no such wait to skip, so every submit already
	// takes the cheap path and the fairness trade is not on the table.
	for {
		res := fd.q.push(req)
		if res == fdPushOK {
			// Accepted. Start the hook host ONLY now: a submission that is never admitted
			// (the cancel paths below) must not leak a host goroutine. The Add happens under
			// the gate, so it is ordered before the shutdown drain's WLock and run()'s
			// hostWg.Wait never races an Add on a zero counter.
			//
			// The readiness gate (setReady, stamped by the caller after we return) is
			// deliberately NOT installed here first: it would change nothing for a hook
			// on the host goroutine, which is the batch's executor and whose result
			// accessors never block (await's executor guard — blocking there would
			// self-deadlock, since only the host closes b.done). A pre-next read on the
			// host is the not-yet-executed view whether or not the gate is set; the
			// FullDuplex contract forbids it (see the FullDuplex field doc).
			if hookDone != nil {
				fd.hostWg.Add(1)
				go fd.hostHook(ctx, cmd, b, hookDone)
			}
			fd.submitMu.RUnlock()
			return b
		}
		if res == fdPushClosed {
			fd.submitMu.RUnlock()
			putFDBlockingBatch(b) // recycle the unadmitted pooled batch (no-op if not pooled)
			cmd.SetErr(ErrClosed)
			return completedBatch
		}
		// Queue at its bound. Wait for the writer to take a wave, then retry. The
		// release conditions are the same ones the blocking channel send had, so
		// holding submitMu.RLock across this wait still cannot wedge the shutdown
		// WLock.
		select {
		case <-fd.q.roomCh():
			// Chain the signal: room is cap-1, so with several submitters blocked only
			// one is released per take. Whoever wakes re-signals while space remains.
			// Confined to this saturated path, so steady state pays nothing. Room a
			// waiting batch has reserved does not count: no single can use it, and
			// passing the wake on for it would only spin this chain until the batch
			// is admitted (each holder has its own wake channel).
			if fd.q.roomFor(1) {
				fd.q.signalRoom()
			}
		case <-ctx.Done():
			// Caller's ctx expired while backpressured (window/queue full): honor it
			// instead of blocking until room or Close (#3964). Not admitted and no host
			// started, so this is a submit-time failure — return the completedBatch sentinel
			// so raw Process(ctx,cmd) reports the ctx error.
			fd.submitMu.RUnlock()
			putFDBlockingBatch(b) // recycle the unadmitted pooled batch (no-op if not pooled)
			cmd.SetErr(ctx.Err())
			return completedBatch
		case <-fd.ap.ctx.Done():
			fd.submitMu.RUnlock()
			putFDBlockingBatch(b) // recycle the unadmitted pooled batch (no-op if not pooled)
			cmd.SetErr(ErrClosed)
			return completedBatch
		}
	}
}

// hostHook runs the user process-hook chain for one full-duplex command on its
// own goroutine, started only when hookCount()>0. The chain starts at ≈ submit
// time and its next() blocks until the reader (or a failure/close path) signals
// hookDone, so an observing hook spans the command's real write→reply latency and
// a hook that rewrites the result is honored before the waiter wakes. Each
// command is reported individually (withProcessHook), not as a pipeline batch.
func (fd *fdEngine) hostHook(ctx context.Context, cmd Cmder, b *apBatch, hookDone chan struct{}) {
	// Declared first so it runs last: Close waits hostWg (via run()), and the host
	// is done only after the recover defer below has also run.
	defer fd.hostWg.Done()
	// Mark this goroutine as the batch's executor so a hook that reads its own
	// command's result after next() (cmd.Err(), a documented pattern) sees the
	// just-executed view instead of blocking on batch.done — which only THIS
	// goroutine closes, below, so without the mark such a hook self-deadlocks.
	// Mirrors runOutsidePipeline's async dispatch guard.
	if fd.ap.armSelfDeadlockGuard() {
		b.dispGid.Store(curGoroutineID())
	}
	// A user ProcessHook runs on this goroutine; an unrecovered panic here would
	// crash the process (and leave the caller blocked on b). Recover, fail the
	// command, and close the batch so the waiter always wakes — mirroring the
	// dispatch path's recoverDispatchPanic.
	// awaited tracks whether hookDone has already been received, so no path awaits
	// it twice. hookDone is closed (not sent) by complete(), so a second receive is
	// harmless in practice, but tracking it keeps the recover path unambiguously
	// free of a redundant await regardless of where a panic lands.
	awaited := false
	defer func() {
		if r := recover(); r != nil {
			// The command was already streamed, so the reader still owns cmd and will
			// write its reply into it. If hookDone was not yet awaited (a panic before
			// next(), or before the short-circuit await below), await it here so the
			// reader's writes happen-before the caller's reads.
			if !awaited {
				<-hookDone
			}
			if cmd.rawErr() == nil {
				// fdSetErrSafe, not a raw SetErr: this call itself is already inside a
				// panic recovery for a custom Cmder whose SetErr panicked (the normal
				// assignment below), so a second unguarded call here would panic again
				// mid-unwind — unrecovered, since this defer's own recover() has already
				// fired — and kill the hostHook goroutine before b.close() wakes the
				// waiter.
				fdSetErrSafe(cmd, fmt.Errorf("redis: autopipeline: panic in full-duplex process hook: %v", r))
			}
			internal.Logger.Printf(ctx, "autopipeline: recovered full-duplex hook panic: %v\n%s", r, debug.Stack())
			b.close()
		}
	}()
	err := fd.ap.pipeliner.withProcessHook(ctx, cmd, func(context.Context, Cmder) error {
		<-hookDone // reply landed (or the command was failed)
		awaited = true
		return cmd.rawErr() // direct read: cmd.Err() would await batch.done, which
		//                     this goroutine itself closes below → self-deadlock.
	})
	// A hook that SHORT-CIRCUITS (returns without calling next) never received
	// hookDone, but the command is already on the wire and the reader will still
	// write into cmd: await that before releasing the caller. The command still
	// executed; the hook's error is honored anyway (see the FullDuplex GoDoc).
	if !awaited {
		<-hookDone
		awaited = true
	}
	// fdSetErrSafe: a panicking custom Cmder here must not escape unrecovered —
	// the recover above only guards its OWN callers, not this line, and an
	// escaping panic would skip b.close() and leave the waiter blocked forever.
	fdSetErrSafe(cmd, err) // honor a hook that rewrote / short-circuited the result
	b.close()              // now wake the waiter
}

// retryStartAttempt returns the normal-path retry loop's starting attempt for an FD
// command diverted to it. A MOVING/ASK redirect returns 0: the command did not
// execute on the FD socket, so it gets the full MaxRetries+1 budget. A retryable
// reply such as LOADING/READONLY/TRYAGAIN returns 1: the initial attempt was already
// spent on the FD socket, so counting it keeps the total at MaxRetries+1, not +2.
func retryStartAttempt(moved, ask bool) int {
	if moved || ask {
		return 0
	}
	return 1
}

// emitMetricsGuarded runs a fire-and-forget user metric callback under panic
// recovery. Every FD metric emit funnels through here: reportReplyMetrics on the
// reader's inline completion, and the failure paths failReqs and failQueue. So a
// panicking user callback is logged and swallowed, not propagated. An escaped panic
// would leave accepted commands unsettled (callers wedged forever) and crash fd.run;
// with process hooks it would deadlock Close's hostWg.Wait while callers block on
// hookDone. On the reader path it would also reach the session-failure recovery
// BEFORE the reply is advanced out of the in-flight deque, which treats an unadvanced
// req as an unacked tail and replays it — an already-consumed mutating command run
// twice. The reply outcome is already decided; reporting is advisory.
func (fd *fdEngine) emitMetricsGuarded(octx context.Context, emit func()) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(octx,
				"autopipeline: recovered full-duplex metric-callback panic: %v\n%s", r, debug.Stack())
		}
	}()
	emit()
}

// reportReplyMetrics runs the inline-completed command's user-settable metric
// callbacks (OTel operation-duration; native error callback for a non-retryable
// Redis error, reporting attempts-1 retries like processWithRetry) for parity
// with the process() path the FD reader bypasses. Guarded by emitMetricsGuarded
// (panic containment; see there).
func (fd *fdEngine) reportReplyMetrics(octx context.Context, req fdReq, e error, cn *pool.Conn) {
	fd.emitMetricsGuarded(octx, func() {
		if cb := otel.GetOperationDurationCallback(); cb != nil {
			// The reader has set req.cmd's final result but has NOT completed the
			// batch yet — req.complete() runs only after this returns. A custom
			// duration callback that reads its own command (cmd.Err()/cmd.String())
			// would await batch.done and wedge the reader, since the batch completes
			// only after this returns and the reader is not otherwise the batch's
			// executor. Register the reader as the batch's executor for the call so
			// the accessor guard hands back the just-set view without blocking — the
			// same escape a dispatch hook reading its own command uses. hostHook gets
			// this SAME batch from submit, so one registration covers the hook and
			// hook-free async faces alike. NOT moved after complete(): complete()
			// hands off to the hook host, which may rewrite/free the command, racing
			// the callback's read. The curGoroutineID() cost is paid only when a
			// duration callback is registered.
			if req.batch != nil {
				unregister := req.batch.enterNodeDispatch()
				defer unregister()
			}
			cb(octx, fd.sinceWritten(req.writtenOff), req.cmd, req.attempts, e, cn, fd.client.opt.DB)
		}
		if e != nil {
			if errorCallback := pool.GetMetricErrorCallback(); errorCallback != nil {
				errorType, statusCode, isInternal := classifyCommandError(e)
				errorCallback(octx, errorType, cn, statusCode, isInternal, req.attempts-1)
			}
		}
	})
}

// retryOnNormalConn re-runs a full-duplex command that came back with a retryable
// Redis error (LOADING/READONLY/…) or a redirect (MOVED/ASK) on the client's NORMAL
// path. That path routes redirects to the proper node and applies the standard
// retry/backoff, neither of which the fixed single-conn FD socket can do. It runs on
// its own goroutine so it does not stall the FD reader, is tracked by retryWg so
// Close waits for it, and settles the FD request with the outcome. process() is the
// raw exec (no hook chain); with hooks installed the FD hostHook still brackets the
// command and reports via req.complete(). Background ctx: the command was already
// accepted, so it completes even under a Close.
func (fd *fdEngine) retryOnNormalConn(req fdReq, startAttempt int) {
	// Bound concurrent off-pipe retries to about 2x the main pool (see newFDEngine's
	// retryCap): a sustained retryable stream would otherwise spawn one goroutine per
	// reply, all parked in backoff/pool acquisition. Blocking here blocks the READER,
	// which stops advancing the deque, which fills the window and blocks the writer and
	// then submitters — end-to-end backpressure. No cycle: retries drain on the main
	// pool, independent of the reader waiting here.
	//
	// Take a free slot if one is available. Otherwise, by state:
	//   - Close (ap.ctx done): FAIL ErrClosed. Never block (a spilled session pins a
	//     main-pool conn until the reader drains, so parking the reader deadlocks the
	//     retries that need that pool) and never run slot-less (a Close-time storm would
	//     spawn a goroutine per in-flight reply, up to FullDuplexWindow, and OOM). The
	//     engine is closing; retryWg still covers granted slots.
	//   - spilled session (curConnSpilled): run SLOT-LESS rather than block, for the
	//     same deadlock reason — the reader must keep draining so the session releases
	//     its pinned main-pool conn (fCCni). Residual: up to a window of transient retry
	//     goroutines while spilled, reachable only when the pipeline pool is saturated
	//     at a small PoolSize; documented, and the lesser evil versus a hang.
	//   - otherwise: BLOCK for a slot — end-to-end backpressure, safe because a
	//     pipeline-pool session does not compete with the main-pool retries.
	slot := false
	select {
	case fd.retrySem <- struct{}{}:
		slot = true
	case <-fd.ap.ctx.Done():
		select {
		case fd.retrySem <- struct{}{}:
			slot = true
		default:
			// fdSetErrSafe: this runs synchronously on the READER goroutine, before
			// the retry's own recover (below) is even in play — a panicking custom
			// Cmder here would escape to the reader's session-failure recovery,
			// tearing down the whole session and replaying the unacked tail,
			// including requests already written (double execution).
			fdSetErrSafe(req.cmd, ErrClosed)
			req.complete()
			return
		}
	default:
		if fd.curConnSpilled.Load() {
			// slot stays false: run slot-less, do NOT block the reader.
		} else {
			// Block for a slot, but stay cancellable. A Close that arrives while
			// the reader is parked here must not deadlock: cancelAndDrain waits on
			// the reader to advance the deque, and the reader is the goroutine
			// blocked on this send. On ctx cancel, FAIL ErrClosed (same as the
			// ctx.Done arm above) rather than park forever.
			select {
			case fd.retrySem <- struct{}{}:
				slot = true
			case <-fd.ap.ctx.Done():
				fdSetErrSafe(req.cmd, ErrClosed) // same reason as the ctx.Done arm above
				req.complete()
				return
			}
		}
	}
	fd.retryWg.Add(1)
	go func() {
		defer func() {
			if slot {
				<-fd.retrySem
			}
			fd.retryWg.Done()
		}()
		// process runs user code (hooks, arg encoders) and can panic; without
		// recovery the batch never completes and the caller (and a hooked command's
		// host, parked on hookDone) waits forever.
		defer func() {
			if r := recover(); r != nil {
				// fdSetErrSafe, not a raw SetErr: this recover is itself the backstop for
				// a panicking Cmder, so a SECOND panic from the same custom SetErr here —
				// e.g. triggered by the retry's own req.cmd.SetErr(err) below — must not
				// escape a deferred function mid-unwind, which Go cannot recover from
				// (it kills the process, not just this goroutine).
				fdSetErrSafe(req.cmd, fmt.Errorf("redis: autopipeline: panic in full-duplex off-pipe retry: %v", r))
				internal.Logger.Printf(context.Background(),
					"autopipeline: recovered full-duplex retry panic: %v\n%s", r, debug.Stack())
				req.complete()
			}
		}()
		// Retry on the caller's context with cancellation removed (like the FD
		// lease init): a CredentialsProviderContext derives credentials from
		// context values, so context.Background() here would reject the retry or
		// authenticate it as the wrong identity even though the FD session
		// initialized correctly. WithoutCancel keeps the values but drops the
		// caller's deadline/cancel, so the accepted command still completes its
		// retry under a Close.
		rctx := context.Background()
		if req.ctx != nil {
			rctx = context.WithoutCancel(req.ctx)
		}
		// reprocess (default client.processStartingAt): fd.client IS the pipeliner
		// (fdClient is the *Client behind it), so this is the same raw exec, but it
		// starts the retry loop at startAttempt — 1 for a retryable reply that already
		// spent an attempt on the FD socket, 0 for a redirect that did not execute.
		// Pass the first-write time as the operation start so the metric spans the
		// initial FD write, not just this diverted attempt (the attempt count already
		// includes the FD attempt).
		// Register this retry goroutine as the batch's executor for the call, mirroring
		// reportReplyMetrics: a custom RecordOperationDuration callback inside
		// processStartingAt that reads its own command (cmd.Err()/cmd.String()) awaits
		// batch.done, which req.complete() closes only AFTER this returns — so without the
		// guard the callback wedges this goroutine and Close waits for the backstop. On the
		// executor goroutine the accessor hands back the just-set view instead. The reader's
		// reply-side guard does not cover this off-pipe retry path.
		err := func() error {
			if req.batch != nil {
				defer req.batch.enterNodeDispatch()()
			}
			return fd.reprocess(rctx, req.cmd, startAttempt, fd.writtenTime(req.writtenOff))
		}()
		// fdSetErrSafe: same custom-Cmder-panic hazard as the reader's reply path
		// (see fdSetErrSafe's doc comment) — a panic here would otherwise reach the
		// recover above, which itself calls SetErr and would then have no outer
		// boundary of its own.
		fdSetErrSafe(req.cmd, err)
		req.complete()
	}()
}

// run owns the engine for the AutoPipeliner's lifetime: acquire a pipeline-pool
// connection, run one full-duplex attempt on it, and on connection failure
// replay the unacked tail on a fresh connection (bounded by MaxRetries/backoff)
// while continuing to serve the queue. Exits only on graceful Close.
func (fd *fdEngine) run() {
	defer fd.ap.wg.Done()
	// Runs before wg.Done (LIFO), so Close — which waits ap.wg — also waits for
	// any off-pipe retries still running on the normal client path.
	defer fd.retryWg.Wait()
	// Same for per-command hook hosts: a ProcessHook doing work after next()
	// closes the command's batch on its host goroutine, so Close must not return
	// while one runs. Every hostWg.Add is gated behind submitMu+closed, and every
	// run() return follows the shutdown drain, so this never races a live Add.
	//
	// Reentrancy caveat: this wait CANNOT exclude its own caller, so a ProcessHook
	// that synchronously calls Close (Client.Close or AutoPipeliner.Close) from its
	// host goroutine deadlocks here until the close backstop (autoPipelineCloseBackstop):
	// Close waits ap.wg -> run() -> this hostWg.Wait, which waits for the very host
	// blocked inside Close. A reentrancy fix was rejected as unsafe: cancelAndDrain is
	// NOT once-only (the shared-pool close hook leaves ap.closed false and can run
	// again), and an early hostWg.Done races submit's hostWg.Add. The contract is
	// documented on the FullDuplex GoDoc instead: such a hook must call Close from a
	// separate goroutine.
	defer fd.hostWg.Wait()
	// Release the last session's in-flight ring when the engine exits (Close /
	// ctx-cancel): curInflight is a grow-only deque that can hold up to fd.window
	// commands (~MiB at the default), and it would otherwise stay resident for as
	// long as the caller holds the Client. Safe against the progress read below —
	// this defer only fires once run() has returned, after that read is done.
	defer fd.curInflight.Store(nil)
	bg := context.Background()
	var carry []fdReq // unacked tail to re-issue at the start of the next attempt
	// Two SEPARATE budgets, each counting only CONSECUTIVE failures of its own
	// kind: a shared counter would let transient lease failures eat the reconnect
	// budget, so the first genuine mid-session drop would fail the whole unacked
	// tail with zero replay attempts. leaseAttempts resets whenever a session
	// actually ran; retryAttempts resets on a clean session end (idle/recycle).
	leaseAttempts := 0 // consecutive fdLeaseErr acquisition failures
	retryAttempts := 0 // consecutive fdConnErr tail-replay failures
	for {
		if fd.ap.ctx.Err() != nil {
			fd.shutdownFlush(bg, carry)
			return
		}
		// Never lease a connection (or dial) without work in hand: block for the
		// first command whenever the carry is empty. That covers the initial entry,
		// the fdIdle return, and the fail-fast exits below (exhausted fdLeaseErr /
		// failed fdConnErr tail), which would otherwise loop straight back into
		// attempt against an empty queue — dialing a down server forever. Work
		// already queued makes this non-blocking, so fdRecycle re-leases
		// immediately; an empty recycle parks here.
		if len(carry) == 0 {
			// About to park with no session running: release the previous session's
			// in-flight ring so a drained deque does not stay resident through the idle
			// gap until the next session stores a fresh one. The fdConnErr progress read
			// (fd.curInflight.Load, below) already ran for the prior iteration, and the
			// next session re-stores before that read runs again, so this never races it.
			fd.curInflight.Store(nil)
			// Park for the first command of the next session. park() re-checks the
			// queue under the same mutex a submitter needs in order to signal, so it
			// returns false when work is already queued and a wake can never be lost.
			for len(carry) == 0 {
				carry = fd.q.takeInto(carry, 1)
				if len(carry) > 0 {
					break
				}
				if !fd.q.park(1) {
					continue // work landed between the take and the park
				}
				select {
				case <-fd.q.wakeCh():
					fd.q.unpark()
				case <-fd.ap.ctx.Done():
					fd.q.unpark()
					fd.shutdownFlush(bg, nil)
					return
				}
			}
		}
		unacked, result, aerr := fd.attempt(bg, carry)
		switch result {
		case fdGraceful:
			// Close: attempt() drained the written work and released the session conn
			// via its defer. The defer Puts the conn, so the OnPut hook can hand a
			// marked conn off, or Removes it if the release drain failed. Here unacked
			// is the never-WRITTEN handoff suffix (nil on a plain Close), NOT written
			// commands to re-execute; complete it on a fresh connection now that
			// attempt() freed this one. Doing it here rather than inside session avoids
			// failing the suffix against a saturated pool while the session conn is
			// still held.
			if len(unacked) > 0 {
				fd.shutdownFlush(bg, unacked)
			}
			return
		case fdIdle:
			// Conn returned cleanly to the pool (its per-conn hooks can run); the
			// loop-top wait keeps an idle engine from churning Get/Put.
			carry, leaseAttempts, retryAttempts = nil, 0, 0
		case fdRecycle:
			// Conn returned cleanly (max-hold, or a mid-carry handoff clean recycle).
			// unacked carries the never-sent suffix from a handoff recycle (nil for a
			// plain max-hold recycle); replay it on the next lease — it was never
			// written, so no command is re-executed. A handoff-marked conn was handed
			// off by the OnPut hook when attempt() Put it.
			fd.recycles.Add(1)
			carry, leaseAttempts, retryAttempts = unacked, 0, 0
		case fdLeaseErr:
			// Could not lease/init a connection for a new session (server down, pool
			// saturated). Retry for a transient outage; once retries are exhausted,
			// fail-fast the carry tail AND the fd.ch backlog rather than leaving accepted
			// commands buffered indefinitely, and stay alive to serve again once the
			// server/pool recovers. Replaying the carry wholesale is safe: any SENT
			// NoRetry was already failed by the split (a never-sent NoRetry in the carry
			// gets its first send), and nothing here was written on a new conn.
			// Close racing the lease surfaces here as a lease failure (the acquisition ctx
			// is cancelled), so flush the accepted work through the normal pipeline path
			// instead of failing it with a canceled error.
			if fd.ap.ctx.Err() != nil {
				fd.shutdownFlush(bg, carry)
				return
			}
			if shouldRetry(aerr, true) && leaseAttempts < fd.retryBudget() {
				leaseAttempts++
				fd.sleepBackoff(leaseAttempts)
				continue // carry unchanged; re-lease
			}
			fd.failReqs(carry, aerr)
			fd.failQueue(aerr)
			carry = nil
			leaseAttempts++
			fd.sleepBackoff(leaseAttempts)
			if leaseAttempts >= fd.retryBudget() {
				leaseAttempts = 0
			}
		default: // fdConnErr — a real connection error occurred
			// A session ran, so the lease succeeded: acquisition failures are no
			// longer consecutive.
			leaseAttempts = 0
			// A session that COMPLETED work (advanced the deque — including a
			// successful carry replay) makes this drop a new failure, not a
			// consecutive one: reset the reconnect budget so long-lived sessions
			// under continuous traffic do not inherit stale failure counts.
			if fi := fd.curInflight.Load(); fi != nil && fi.advancedTotal() > 0 {
				retryAttempts = 0
			}
			// retryTimeout=true: a read/write timeout is a retryable connection
			// failure here (re-issue the unacked tail on a fresh conn), matching
			// the cluster pipeline retry paths — otherwise a single WAN timeout
			// fails the whole tail. The engine's internal failure markers
			// (recovered panics, reader-gone) desync the conn exactly like a
			// transport error. The tail is mostly commands they never touched, so
			// they are replayable too. shouldRetry alone would reject them and
			// permanently fail innocent in-flight commands. The NoRetry guard
			// below still protects non-idempotent writes.
			replayable := shouldRetry(aerr, true) ||
				errors.Is(aerr, errFDReaderGone) || errors.Is(aerr, errFDPanicRecovered) ||
				errors.Is(aerr, errFDPushDrainFailed)
			// Bound each command by its OWN attempt count, not the session-level
			// retryAttempts (which resets on any session progress — see advancedTotal
			// above — so a flaky peer that acks some replies then drops could hand the
			// tail a fresh budget on every partial success). Partition the tail: a
			// command that has spent its budget (attempts > MaxRetries) is failed; the
			// rest stay eligible to replay. Gating the whole tail on the OLDEST command's
			// count instead would deny a newer command — written behind an exhausted one,
			// so carrying fewer attempts — the retries it is still owed. Carried commands
			// are the oldest (written first, attempts bumped together), so the exhausted
			// set is the leading run and the eligible suffix keeps FIFO order.
			// retryAttempts still drives the backoff escalation only.
			eligible := unacked
			if replayable && len(unacked) > 0 {
				// unacked is PRE-BUMP here (a command sent A times carries attempts==A),
				// so attempts > retryBudget is exactly the spent-budget set. The Close-path
				// flush (flushCarryBudgeted) instead sees POST-BUMP carry, where the same
				// command carries attempts==A+1 — do not unify the two thresholds.
				var exhausted []fdReq
				eligible, exhausted = fdPartitionByBudget(unacked, fd.retryBudget())
				if len(exhausted) > 0 {
					// Spent MaxRetries+1 attempts; fail with the real cause, and with
					// them the rest of their pipelines.
					eligible = fd.failPipelines(exhausted, eligible, aerr)
				}
				// Split the eligible suffix at the first SENT NoRetry command: replay the
				// prefix and fail that command plus everything ordered after it (a NoRetry
				// command whose bytes may have reached the wire must never be re-sent). A
				// never-sent NoRetry stays in the replay prefix — issuing it is its first
				// send (see fdReq.sent). With a sent NoRetry at the eligible head (n==0)
				// nothing ahead of it is retryable, so fall through and fail whatever
				// eligible remains (exhausted was already failed above). If the scan
				// itself panicked (a custom Cmder's NoRetry() is user code on this
				// recover-less serve loop), we cannot classify the tail: skip the replay
				// and fall through to fail it — a command that may be a sent NoRetry must
				// never be re-sent when in doubt.
				if n, scanPanic := fdFirstNoRetrySafe(eligible); !scanPanic && n > 0 {
					carry = eligible[:n]
					if n < len(eligible) {
						carry = fd.failPipelines(eligible[n:], carry, aerr)
					}
					retryAttempts++
					fd.sleepBackoff(retryAttempts)
					// Already issued on the failed connection and about to be re-issued;
					// bump attempts so a later success/failure reports the real
					// retry_attempts (not always 1). The clean-recycle suffix path
					// (fdRecycle) leaves attempts at 1 — that tail was never sent, so its
					// replay is a first attempt.
					for i := range carry {
						carry[i].attempts++
						// Keep a pipelined command's batch in step, so the pipeline
						// retry charges this replay even if the command then fails
						// here instead of completing through the reader.
						if carry[i].pipelined {
							carry[i].batch.fdAttempts = carry[i].attempts
						}
					}
					continue
				}
			}
			// Not retrying (not replayable, none eligible, or NoRetry-headed): fail the
			// remaining unfailed commands, then ALWAYS back off before re-leasing so a
			// dead server cannot spin this loop. Keep the engine alive to serve new work
			// when it recovers.
			fd.failPipelines(eligible, nil, aerr)
			carry = nil
			retryAttempts++
			fd.sleepBackoff(retryAttempts)
			if retryAttempts >= fd.retryBudget() {
				retryAttempts = 0 // reset so backoff restarts small once we're serving again
			}
		}
	}
}

// attempt acquires a connection, runs one full-duplex session (re-issuing carry
// first), and releases the connection. Returns the unacked tail + error on
// connection failure, or graceful=true on Close.
func (fd *fdEngine) attempt(bg context.Context, carry []fdReq) (unacked []fdReq, result fdResult, aerr error) {
	// Options.Limiter is deliberately NOT consulted here: the lease is not the
	// Limiter's unit. Admission is per written chunk, in writeBatch — see the
	// comment there for the rationale.
	var cn *pool.Conn
	// connPool records which pool cn was leased from — the pipeline pool normally, or
	// the main pool on a spill (see the acquire below) — so the deferred remove/release
	// returns it to the pool that owns it.
	connPool := fd.pool
	// Bias to spilled==true until the lease is decided below: an unknown state must
	// never let retryOnNormalConn block the reader (a leftover true only makes off-pipe
	// retries slot-less, which is safe; a stale false is the deadlock this guards).
	fd.curConnSpilled.Store(true)
	defer func() {
		if cn == nil {
			return // nothing acquired, or already Removed inline below
		}
		// ANY connection-error end (result==fdConnErr) leaves the conn desynced —
		// an unread reply tail, a partial write, or a reader protocol error — so it
		// MUST be removed; Put()ing it would poison the pool. Keying on result (not
		// isBadConn) is deliberate: errFDReaderGone or a plain write timeout are not
		// classified bad-conn, yet the conn is still unusable. Clean ends (including a
		// handoff recycle) go through releaseConnToPool: it drains pending pushes and
		// Puts, so the OnPut hook can perform the maintenance handoff on a marked conn.
		if result == fdConnErr {
			connPool.Remove(bg, cn, aerr)
		} else {
			// releaseConnToPool drains pending pushes (a custom PushNotificationProcessor
			// runs user code) and Puts. This defer runs LAST (LIFO — the attempt-init
			// recover below is registered after it and runs FIRST), so a panic here has no
			// outer boundary: it would escape the sole fd.run goroutine, crash the process,
			// and leak the leased conn. Contain it — the conn's drain/Put state is unknown
			// after a panic, so Put would poison the pool: Remove it instead.
			func() {
				defer func() {
					if r := recover(); r != nil {
						internal.Logger.Printf(bg, "autopipeline: recovered full-duplex release panic: %v\n%s", r, debug.Stack())
						connPool.Remove(bg, cn, fmt.Errorf("%w: release: %v", errFDPanicRecovered, r))
					}
				}()
				fd.client.releaseConnToPool(bg, connPool, cn, nil)
			}()
		}
	}()

	// Panic boundary for the ACQUISITION/INITIALIZATION phase. initPooledConn runs
	// user-controlled init (Options.OnConnect, credentials providers); a panic there
	// would otherwise escape the sole fd.run goroutine and crash the process, and the
	// release defer above — seeing the zero-value result (fdGraceful) — would Put the
	// half-initialized conn back into the pool. Registered AFTER that defer so it runs
	// FIRST (LIFO): retire the leased conn (Remove), set cn=nil so the release defer is a
	// no-op, and return the carry as fdLeaseErr — the SAME disposition an initPooledConn
	// error gets below, so run() applies the lease-retry budget and fails accepted work
	// fast on a deterministic panic instead of poisoning the pool. A session-body panic
	// is contained by session()'s own recover, which returns fdConnErr normally, so this
	// boundary fires only for the lease/init phase (and as a last-resort backstop).
	defer func() {
		if r := recover(); r != nil {
			aerr = fmt.Errorf("%w: full-duplex attempt: %v", errFDPanicRecovered, r)
			internal.Logger.Printf(bg, "autopipeline: recovered full-duplex attempt panic: %v\n%s", r, debug.Stack())
			if cn != nil {
				connPool.Remove(bg, cn, aerr)
				cn = nil
			}
			unacked = carry
			result = fdLeaseErr
		}
	}()

	// initCtx: initialize with the SESSION-INITIATING caller's context (values only,
	// via WithoutCancel), not context.Background(). A CredentialsProviderContext
	// derives credentials from context values, and Background made those invisible so
	// such providers rejected FD sessions or authed with fallback identity. Full-duplex
	// holds ONE connection for MANY callers, so credentials are session-scoped (the
	// first caller's), like any shared/pooled connection (documented on FullDuplex).
	// WithoutCancel keeps the values but drops the caller's deadline/cancel, so one
	// caller's ctx expiry cannot abort an init the whole session depends on. init goes
	// through initPooledConn (shared with the main/pipeline paths): it records the
	// create-time metric and Removes the conn on any failure, so the defer, seeing
	// cn=nil, does not double-release.
	initCtx := bg
	if len(carry) > 0 && carry[0].ctx != nil {
		initCtx = context.WithoutCancel(carry[0].ctx)
	}

	// Acquire+init from the pipeline pool; SPILL to the main pool when the pipeline
	// pool cannot serve the lease, mirroring withPipelineConn — an FD lease must not
	// fail already-accepted commands while the main pool has idle capacity. Unlike a
	// per-round-trip pipeline borrow, a spilled FD session holds the main-pool conn for
	// its whole lifetime (until idle/maxHold); that is the accepted cost of not
	// stranding the backlog. TryGet (non-blocking) so a saturated pipeline pool spills
	// at once instead of stalling up to PoolTimeout.
	//
	// Spill on ANY acquisition failure EXCEPT a hard stop — saturation (ErrPoolTryFull
	// / ErrPoolExhausted) AND a transient DIAL error while the pipeline pool is growing
	// a connection (TryGet dials when the pool has no idle conn), plus a pipeline-conn
	// init failure below. Only a cancelled ap.ctx (Close) or a closed pool surface as
	// fdLeaseErr, because the main pool would fail the same way; every other error may
	// clear on the main pool, which can hand back an idle conn or dial cleanly, so it
	// must not fail the accepted backlog (codex on #4002 — a deny-list, not an
	// allow-list: a dial error is not saturation but must still spill). Acquire under
	// ap.ctx (not bg) so a Close cancelling ap.ctx returns at once instead of waiting
	// out PoolTimeout; init and session I/O stay on bg so accepted commands still
	// complete during Close.
	if ref := fd.client.loadPipelinePool(); ref != nil {
		spill := false
		cn, aerr = ref.pool.TryGet(fd.ap.ctx)
		if aerr != nil {
			cn = nil
			if errors.Is(aerr, pool.ErrClosed) ||
				errors.Is(aerr, context.Canceled) ||
				errors.Is(aerr, context.DeadlineExceeded) {
				// Hard stop: the main pool cannot do better (closed pool, or the caller/
				// Close cancelled the acquire ctx). Surface it rather than spill.
				return carry, fdLeaseErr, aerr
			}
			spill = true
		} else if e := fd.client.initPooledConn(initCtx, ref.pool, cn); e != nil {
			cn = nil // initPooledConn already Removed it from the pipeline pool
			spill = true
		}
		if spill {
			cn, aerr = fd.client.connPool.Get(fd.ap.ctx)
			if aerr != nil {
				cn = nil
				return carry, fdLeaseErr, aerr
			}
			connPool = fd.client.connPool
			if e := fd.client.initPooledConn(initCtx, fd.client.connPool, cn); e != nil {
				cn = nil // initPooledConn already Removed it from the main pool
				return carry, fdLeaseErr, e
			}
		}
	} else {
		// No dedicated pipeline pool (PipelinePoolSize < 0, or an internal wrapper
		// client): fd.pool IS the main pool, so acquire directly with no spill.
		cn, aerr = fd.pool.Get(fd.ap.ctx)
		if aerr != nil {
			cn = nil
			return carry, fdLeaseErr, aerr
		}
		if e := fd.client.initPooledConn(initCtx, fd.pool, cn); e != nil {
			cn = nil // initPooledConn already Removed it
			return carry, fdLeaseErr, e
		}
	}

	// The lease is decided: spilled iff the conn came from the main pool (a spill, or
	// no dedicated pipeline pool). Stored before session() spawns the reader, so the
	// reader's retryOnNormalConn sees the right value (happens-before).
	fd.curConnSpilled.Store(connPool == fd.client.connPool)

	unacked, result, aerr = fd.session(bg, cn, carry)
	return unacked, result, aerr
}

// session runs the writer (this goroutine) + reader (spawned) on one connection
// until Close (graceful) or a connection error (returns the unacked tail).
func (fd *fdEngine) session(bg context.Context, cn *pool.Conn, carry []fdReq) (unacked []fdReq, result fdResult, aerr error) {
	inflight := newFDInflightCap(min(fd.maxBatch, fd.window)) // capped by the window (peak) and the batch; grow() backstops
	fd.curInflight.Store(inflight)                            // test observability (peak in-flight)
	fd.curConn.Store(cn)                                      // test observability (handoff)
	defer fd.curConn.Store(nil)
	readerDone := make(chan struct{})

	// Honor opt.ReadTimeout as-is for each per-reply read: options.go maps a
	// disabled timeout (-1) to 0 and WithReader treats <= 0 as "no deadline", so
	// disabled stays disabled instead of being clamped to some fixed value (a
	// default client keeps its 5s, which bounds each read). A genuinely stuck read
	// is still unblocked by the conn Close on the fdConnErr path.
	readTimeout := fd.client.opt.ReadTimeout
	var errOnce sync.Once
	var sharedErr error
	failOnce := func(e error) { errOnce.Do(func() { sharedErr = e }) }

	// Reader: read replies in FIFO order, completing each command as its reply
	// lands. Works a bounded front-snapshot per lock (amortizes the mutex over
	// many reads), then advances. On a connection/protocol error it stops and
	// leaves the unread tail in the deque (it becomes the unacked recovery set).
	go func() {
		defer close(readerDone)
		// done counts commands completed in the CURRENT frontBatch snapshot that
		// have not yet been advanced out of the in-flight deque; it is 0 outside the
		// inner read loop (reset at the top of each iteration, advanced at the end).
		done := 0
		// A reply decoder can panic (e.g. a RawWriteToCmd whose user io.Writer panics
		// while readReply streams the raw reply). Recover and mark the session failed
		// (failOnce) so run() takes the connection-error path: the reader exits, the
		// unacked tail is recovered and the conn is removed. advance(done) FIRST so
		// commands already completed in the panicking snapshot leave the deque.
		// Otherwise recovery re-owns and re-completes them, overwriting good results
		// and double-closing hookDone (a second panic) when hooks are installed.
		defer func() {
			if r := recover(); r != nil {
				inflight.advance(done)
				failOnce(fmt.Errorf("%w: reader: %v", errFDPanicRecovered, r))
				internal.Logger.Printf(bg, "autopipeline: recovered full-duplex reader panic: %v\n%s", r, debug.Stack())
			}
		}()
		var buf []fdReq
		var readErrsBuf []error // reused across read groups
		for {
			done = 0
			var ok bool
			buf, ok = inflight.frontBatch(buf)
			if !ok {
				return
			}
			var rerr error
			// Read each reply as it lands (one WithReader per reply). Reading the
			// whole snapshot inside a single WithReader was measurably slower on
			// loopback: it blocks on commands the writer has pushed but not yet
			// flushed, collapsing writer/reader overlap.
			// BATCHED READ: one WithReader per already-buffered GROUP of replies.
			//
			// The older per-reply WithReader re-armed the socket read deadline on
			// every reply, which measured 5.85% of all CPU in SetReadDeadline plus
			// 1.95% in deadline() at ~400k replies/s. Reading the WHOLE snapshot in
			// one WithReader was tried before and was slower, because it blocks on
			// commands the writer has pushed but not yet flushed. This cannot do
			// that: after the first reply of a group it continues only while the
			// next reply is whole in the buffer, so it never waits on the socket
			// inside a group.
			for i := 0; i < len(buf); {
				readErrs := readErrsBuf[:0]
				grp := 0
				// The whole group reads under the one deadline WithReader arms
				// here, so only its first reply may read from the socket. The
				// group goes on only while the next reply is already whole in the
				// buffer (HasBufferedReply); Buffered() > 0 is not enough, since a
				// partial frame still needs a socket read. A reply that needs one
				// starts the next group, whose WithReader arms a full deadline.
				ge := cn.WithReader(bg, readTimeout, func(rd *proto.Reader) error {
					for i+grp < len(buf) {
						// Same push-drain contract as before: a partial-frame drain
						// desyncs the stream, so it is fatal for the session rather than
						// logged and skipped. A push handler panic is one too
						// (fdPushDrainSafe), so the replies read before it complete.
						if perr := fd.fdPushDrainSafe(bg, cn, rd); perr != nil {
							internal.Logger.Printf(bg, "autopipeline: full-duplex push drain: %v", perr)
							readErrs = append(readErrs, fmt.Errorf("%w: %w", errFDPushDrainFailed, perr))
							grp++
							return nil
						}
						err := fdReadReplySafe(buf[i+grp].cmd, rd)
						readErrs = append(readErrs, err)
						grp++
						if err != nil {
							// Stop the group on ANY error: reading further after a
							// transport or protocol fault would consume shifted bytes.
							return nil
						}
						if !rd.HasBufferedReply() {
							return nil // next WithReader arms a full deadline
						}
					}
					return nil
				})
				if grp == 0 {
					// WithReader failed before any reply was read; attribute it to the
					// head command so the existing fatal/divert logic still sees it.
					readErrs = append(readErrs, ge)
					grp = 1
				}
				readErrsBuf = readErrs
				for k := 0; k < grp; k++ {
					req := buf[i+k]
					e := readErrs[k]
					if e != nil && fdReplyIsFatal(req.cmd, e) {
						// Connection/protocol error, OR a push-drain desync (fatal even when it
						// wraps a Redis-typed cause — see fdReplyIsFatal): stop; the unread tail
						// stays in the deque and becomes the unacked recovery set for replay.
						rerr = e
						break
					}
					// The reply landed (nil, or a reply-LEVEL Redis error / redirect — a
					// server that answers is healthy, NOT a transport failure). If this req
					// closes an admitted chunk, settle its Limiter obligation with success:
					// exactly one ReportResult(nil) per Allow, on the reply side. Fires for
					// both the inline completion below and the retryable-divert branch (the
					// reply WAS read; the divert re-runs the command elsewhere under its own
					// getConn Allow/Report pairing).
					if req.limReport != nil {
						req.limReport.settle(nil)
					}
					// A retryable Redis error or a redirect (MOVED/ASK) is NOT the caller's
					// final answer: the FD conn is one fixed socket/node, so re-run the
					// command on the client's NORMAL path, which routes redirects and applies
					// the standard retry/backoff. Done off the reader goroutine so it does not
					// stall other in-flight replies, and counted in `done` so the reader
					// advances past it now. Per-caller ordering is NOT promised across this
					// divert (same exception as the blocking-command divert).
					if e != nil {
						moved, ask, _ := isMovedError(e)
						// Cluster full-duplex redirect: a MOVED/ASK is followable for EVERY
						// command, including NoRetry ones (e.g. GetToBuffer, RawWriteTo). NoRetry
						// guards against replaying a command whose partial response was already
						// consumed, but a MOVED/ASK reply carries no payload — the command did
						// NOT execute on this node — so there is nothing to replay, and the normal
						// ClusterClient.process follows redirects for all commands before
						// consulting NoRetry. So divert a redirect independent of the NoRetry gate
						// below. reprocess re-routes MOVED to the target node (LazyReload) and
						// follows ASK through cc.process's own loop (ASKING on the next hop),
						// bounded by MaxRedirects; startAttempt is unused by the cluster reprocess
						// (it re-runs the full loop from the base), so pass the redirect value (0).
						// isMovedError/e here are reply-level only: a transport or protocol failure
						// is !isRedisError and already broke the read loop via fdReplyIsFatal above.
						//
						// Standalone FD (redirectAware == false) cannot follow a MOVED/ASK (it
						// neither re-routes to the target node nor sends ASKING), so it falls
						// through to the inline settle and surfaces the redirect, as before.
						if fd.redirectAware && (moved || ask) {
							fd.retryOnNormalConn(req, retryStartAttempt(moved, ask))
							done++
							continue
						}
						// A RETRYABLE execution error (not a redirect) may have produced a
						// partially consumed response, so it stays gated on NoRetry.
						// A pipelined command is never diverted alone (see
						// fdReq.pipelined); its reply settles inline below.
						if !req.pipelined && !fdNoRetrySafe(req.cmd) {
							// Cluster full-duplex: divert a retryable server reply
							// (LOADING/READONLY/TRYAGAIN/CLUSTERDOWN/MASTERDOWN/NOREPLICAS/
							// max-clients) to the redirect-aware ClusterClient. It consults NO
							// FD-side budget and — the key difference from the standalone branch
							// below — does NOT gate on the node client's MaxRetries: cluster node
							// clients default MaxRetries to -1 (osscluster.go), which is <= 0, so a
							// MaxRetries>0 gate would wrongly settle the reply inline and fail the
							// caller instead of recovering it the way half-duplex does. cc.process
							// owns the whole cluster retry budget; startAttempt is unused by the
							// cluster reprocess. shouldRetry(e) here matches only reply-level Redis
							// errors (see the fdReplyIsFatal note above).
							if fd.redirectAware && shouldRetry(e, false) {
								fd.retryOnNormalConn(req, retryStartAttempt(false, false))
								done++
								continue
							}
							// Standalone FD: divert a RETRYABLE reply only while retries are
							// enabled AND the budget is not already spent. req.attempts counts FD
							// attempts spent (1 at submit, +1 on each fdConnErr carry replay). Once
							// it reaches MaxRetries+1 another execution would exceed the budget, so
							// fall through to the inline settle, which surfaces the reply as the
							// final error and reports the true attempt count. Without this guard the
							// startAttempt clamp in processWithRetry would turn an exhausted budget
							// into one more send.
							if !moved && !ask &&
								shouldRetry(e, false) &&
								fd.client.opt.MaxRetries > 0 &&
								req.attempts <= fd.client.opt.MaxRetries {
								// The retryable reply executed on the FD socket, so the divert starts
								// one attempt in; add req.attempts-1 for FD attempts already spent on
								// carry replays so a carried-then-diverted command does not run the
								// full loop from the base. The guard above keeps this within
								// MaxRetries+1.
								fd.retryOnNormalConn(req, retryStartAttempt(false, false)+req.attempts-1)
								done++
								continue
							}
						}
					}
					fdSetErrSafe(req.cmd, e) // nil, a redirect (MOVED/ASK), or a non-retryable Redis error; panic-safe (see fdSetErrSafe)
					// Per-command OTel duration (write→reply): the FD reader bypasses
					// process, which is what normally emits it. Inline-completed commands
					// only — a diverted command emits its own through process.
					// req.ctx carries the caller's span for telemetry correlation
					// (exemplars, context-scoped attrs); fall back to bg only when nil.
					// Shared by the duration and error callbacks so both attribute to the
					// request context, matching process().
					octx := req.ctx
					if octx == nil {
						octx = bg
					}
					// Emit the per-command metric callbacks under a recover boundary (see
					// reportReplyMetrics): they are user-settable, and an unrecovered panic
					// here would reach the reader's session-failure recovery BEFORE this req
					// is advanced, so recovery would re-own the already-consumed reply and
					// replay it — a mutating command twice.
					if req.pipelined {
						// A pipelined command is measured with its batch, as in an
						// ordinary pipeline (fdPipelineMetrics), not per command.
						req.batch.fdAttempts = req.attempts // published by complete()
						req.batch.fdConn = cn
					} else {
						fd.reportReplyMetrics(octx, req, e, cn)
					}
					req.complete() // wake the caller, or hand off to the hook host
					done++
				}
				// A fatal reply ends the whole snapshot, not only its group: the
				// stream is desynced, so another WithReader would attach shifted
				// bytes to later commands. Stopping here also keeps `done` a
				// contiguous prefix, which advance(done) relies on to drop only
				// completed commands.
				if rerr != nil {
					break
				}
				i += grp
			}
			inflight.advance(done)
			if rerr != nil {
				failOnce(rerr)
				return
			}
		}
	}()

	// Panic boundary for the WRITER path. The reader goroutine above has its own recover;
	// the writer (this goroutine) runs writeCarryChunked, the serve loop and the
	// Close-backlog flush with no top-level recover, so an unguarded user-code panic (a
	// Cmder Args()/encoder, a Limiter, a metrics callback) would kill the sole fd.run
	// goroutine and leave attempt()'s defer to Put a live conn. Registered AFTER the
	// reader is spawned, so readerDone is guaranteed to close. On a panic, run the
	// fdConnErr teardown (stop the reader, wait it out, recover the unacked tail) and
	// return fdConnErr NORMALLY, so attempt()'s release defer Removes the desynced conn
	// and run() replays the eligible tail (errFDPanicRecovered is replayable, bounded by
	// each command's own attempt budget). The known sizing/limiter/metrics panics are
	// already contained at their sites (cmdApproxBytesSafe, fdBatchEndSafe, fdAllow,
	// reportReplyMetrics); this backstop guarantees the goroutine survives any other.
	defer func() {
		if r := recover(); r != nil {
			e := fmt.Errorf("%w: full-duplex session: %v", errFDPanicRecovered, r)
			internal.Logger.Printf(bg, "autopipeline: recovered full-duplex session writer panic: %v\n%s", r, debug.Stack())
			failOnce(e)
			inflight.hardClose()
			_ = cn.Close()
			<-readerDone
			unacked = fd.settleTail(inflight.takeRemaining(), e)
			result = fdConnErr
			aerr = e
		}
	}()

	// Idle / max-hold timers arm the clean-return paths. A disabled timer uses a
	// nil channel (never selected).
	var idleC, maxC <-chan time.Time
	var idleT, maxT *time.Timer
	if fd.idle > 0 {
		idleT = time.NewTimer(fd.idle)
		idleC = idleT.C
		defer idleT.Stop()
	}
	if fd.maxHold > 0 {
		maxT = time.NewTimer(fd.maxHold)
		maxC = maxT.C
		defer maxT.Stop()
	}
	resetIdle := func() {
		if idleT == nil {
			return
		}
		if !idleT.Stop() {
			select {
			case <-idleT.C:
			default:
			}
		}
		idleT.Reset(fd.idle)
	}

	result = fdConnErr // default until a break sets otherwise

	// Writer: re-issue the recovered tail first, then serve the queue. The tail goes
	// in the SAME MaxBatchSize/MaxBatchBytes-capped chunks as freshly drained work —
	// it can hold up to fd.window commands, so one flush would ignore MaxBatchBytes
	// and hit a write-timeout/burst on the new connection.
	carrySuffix, writeErr := fd.writeCarryChunked(bg, cn, inflight, carry, readerDone, maxC)
	if writeErr == nil {
		// Cap the drain scratch at the window, not MaxBatchSize: the writer can never
		// have more than fd.window commands in flight, so a large MaxBatchSize with a
		// small window would over-allocate (up to OOM) for no gain. Matches the
		// in-flight ring's min(maxBatch, window) cap.
		scratch := make([]fdReq, 0, min(fd.maxBatch, fd.window))
		byteLimit := int64(fd.ap.config.MaxBatchBytes) // 0 = disabled
		// Reused across drains so the accumulation wait allocates nothing on
		// the hot path. Nil until the first wait actually happens.
		var accumTimer *time.Timer
	serve:
		for {
			// Backpressure: bound the in-flight (written-but-unacked) deque. Wait
			// for the reader to drain below the window BEFORE taking new work, so
			// a slow/stalled peer cannot grow in-flight without bound. Done here
			// (not mid-batch) so no drained work is ever held during the wait.
			for inflight.len() >= fd.window {
				// Poll handoff here too, not just after the gate: under sustained
				// backpressure the writer can sit in this loop, and room fires on
				// every reader advance, so a MOVING mark is observed within one
				// drained reply instead of waiting for max-hold.
				if cn.ShouldHandoff() {
					result = fdRecycle
					break serve
				}
				select {
				case <-inflight.room:
				case <-readerDone:
					break serve // reader hit a connection error
				case <-fd.ap.ctx.Done():
					result = fdGraceful
					break serve
				case <-maxC:
					result = fdRecycle
					break serve
				}
			}
			// Go's select picks randomly among ready cases, so with work queued AND
			// the reader gone (decode panic, protocol error) the main select below
			// could write a batch to a connection known to have no reader — needlessly
			// enlarging the ambiguous at-least-once set. Check readerDone first.
			select {
			case <-readerDone:
				break serve // result stays fdConnErr; unacked tail is recovered
			default:
			}
			// A maintenance MOVING/FAILING_OVER push (drained by the reader) marks
			// the held connection for handoff. The pool queues the handoff only when
			// the conn is Put back, so end this session promptly with a CLEAN recycle
			// (drain in-flight to a RESP boundary, then Put) instead of continuing to
			// write to a node known to be moving until idle/max-hold. Otherwise the
			// handoff can miss its deadline. ShouldHandoff() is an atomic load, so it
			// is safe to poll here while the reader sets it; room fires on every
			// reader advance, so a writer parked on the window gate above re-checks
			// this within one drained reply.
			if cn.ShouldHandoff() {
				result = fdRecycle
				break serve
			}
			// Park for the next wave, then take it in ONE lock. park() asks the queue
			// to wake this goroutine only once it holds minDepth commands, and returns
			// false when that many are already queued, so no wake is lost and no
			// arrival is missed. A channel could only express "wake me on the next
			// arrival", which woke the writer once per command and re-entered its
			// select ~112 times per 250 us window: that single select measured 3.25 s,
			// 6.9% of all CPU, at ~450k ops/s.
			var wakeC <-chan struct{}
			if fd.q.park(1) {
				wakeC = fd.q.wakeCh()
			} else {
				wakeC = fdQueueReady // work already queued: proceed without waiting
			}
			select {
			case <-wakeC:
				fd.q.unpark()
				// Cap this batch by the REMAINING window room, not just MaxBatchSize:
				// the gate above only ensures in-flight < window before draining, so a
				// window smaller than MaxBatchSize would let one drain blow through it
				// (window=1, batch=200 -> 200 in flight). The first command always goes
				// (room is >= 1 after the gate).
				limit := fd.maxBatch
				if room := fd.window - inflight.len(); room < limit {
					limit = room
				}
				if limit < 1 {
					limit = 1
				}
				batch := fd.q.takeInto(scratch[:0], limit)
				if len(batch) == 0 {
					// Stale wake: a drain path (failQueue/backlog flush) emptied the
					// queue between the signal and the take. Do not resetIdle — no
					// session activity happened.
					continue serve
				}
				// accumMaxHold: max-hold fired DURING the accumulation wait; its
				// one-shot tick was consumed there, so end the session after the flush.
				accumMaxHold := false
				// Accumulate to the flush deadline. ONE park covers the whole window:
				// park(need) asks to be woken only when the batch can be FILLED, and
				// submitters below that depth stay silent, so a window costs one park
				// and one wake instead of one per arriving command. The timer releases a
				// wave that never reaches the threshold. Gated on in-flight depth (see
				// fdAccumMinFor): at low concurrency the queue is empty because there is
				// genuinely nothing to send, and waiting would cost every command a
				// round trip. MaxFlushDelay defaults to 0, so this is opt-in.
				if fd.ap.config.MaxFlushDelay > 0 && len(batch) < limit &&
					len(batch) < fd.maxBatch && inflight.len() >= fd.accumMin {
					// MaxFlushDelay is a MAXIMUM, so arm the timer for a short grace and
					// re-arm it whenever the batch actually grows, bounded by accumCap.
					// A wave that keeps arriving rides to the full cap (which is what
					// earns the win at high concurrency); a wave that stops arriving
					// flushes after the grace instead of idling out the whole window.
					accumCap := time.Now().Add(fd.ap.config.MaxFlushDelay)
					// The grace is ABSOLUTE, not a fraction of the budget: coupling them
					// means a generous budget also makes the writer slow to notice an
					// idle queue, which is the regression the budget is supposed to be
					// safe from. Clamped so a tiny budget is still honoured.
					accumGrace := fdAccumGrace
					if accumGrace > fd.ap.config.MaxFlushDelay {
						accumGrace = fd.ap.config.MaxFlushDelay
					}
					if accumGrace <= 0 {
						accumGrace = time.Microsecond
					}
					if accumTimer == nil {
						accumTimer = time.NewTimer(accumGrace)
					} else {
						// Reset on an expired, undrained timer is the classic timer-reuse
						// bug. Drain NON-BLOCKINGLY: whether a value is buffered depends on
						// who won the previous select, so a plain <-accumTimer.C wedges the
						// writer (and Close) forever in the case where the timer already
						// lost the race and no value is pending.
						if !accumTimer.Stop() {
							select {
							case <-accumTimer.C:
							default:
							}
						}
						accumTimer.Reset(accumGrace)
					}
				accum:
					for len(batch) < limit {
						need := limit - len(batch)
						if !fd.q.park(need) {
							batch = fd.q.takeInto(batch, need)
							continue accum
						}
						select {
						case <-fd.q.wakeCh():
							fd.q.unpark()
							grew := len(batch)
							batch = fd.q.takeInto(batch, need)
							if len(batch) > grew {
								// Progress. Extend the grace, but never past the cap: that is
								// what keeps MaxFlushDelay an upper bound on added latency.
								rem := time.Until(accumCap)
								if rem <= 0 {
									break accum
								}
								d := accumGrace
								if d > rem {
									d = rem
								}
								if !accumTimer.Stop() {
									select {
									case <-accumTimer.C:
									default:
									}
								}
								accumTimer.Reset(d)
							}
						case <-accumTimer.C:
							// Deadline reached. Sweep up anything that arrived below the
							// wake threshold, then flush.
							fd.q.unpark()
							batch = fd.q.takeInto(batch, limit-len(batch))
							break accum
						case <-readerDone:
							// Reader is gone. Stop waiting and flush the prefix already
							// taken off the queue; the top of the serve loop re-observes
							// readerDone (a closed channel, so the signal is not consumed
							// here) and ends the session through the existing
							// connection-error path, which recovers this batch through the
							// carry/replay logic.
							fd.q.unpark()
							break accum
						case <-fd.ap.ctx.Done():
							// Close() is waiting on this writer. Flush what is in hand and
							// let the serve loop take the graceful path; ctx.Done() is a
							// closed channel, so nothing is consumed.
							fd.q.unpark()
							break accum
						case <-maxC:
							// Max-hold reached mid-wait. maxC comes from a ONE-SHOT
							// time.Timer, so this receive consumes the only tick: record it
							// and end the session after the flush rather than dropping it
							// and holding the connection past its deadline. Without this
							// case a MaxFlushDelay longer than FullDuplexMaxHold parks the
							// writer beyond max-hold.
							fd.q.unpark()
							accumMaxHold = true
							break accum
						}
					}
				}
				// Size the wave in ONE pass, applying MaxBatchSize and the soft
				// MaxBatchBytes cap. cmd.Args() is user code running on this
				// recover-less serve loop, so fdBatchEndSafe wraps each sizing call in a
				// recover and reports the clean prefix plus the offending index.
				end, bad, sizeErr := fdBatchEndSafe(batch, 0, limit, byteLimit)
				if sizeErr != nil {
					// A command's Args() panicked. Fail and DROP just that command, then
					// flush the clean prefix: letting it reach writeBatch would trip that
					// path's write-time recover, tearing down the whole session and
					// replaying its batch-mates at-least-once.
					fd.failReqs(batch[bad:bad+1], sizeErr)
					rest := batch[bad+1:]
					batch = batch[:bad]
					// The suffix past the offender was already dequeued, so return it to
					// the HEAD of the queue: it keeps its place in FIFO order and goes out
					// in the next flush.
					if len(rest) > 0 {
						fd.q.pushFront(rest)
					}
					if len(batch) == 0 {
						// Do not resetIdle: a dropped command is not session activity, and
						// the idle timer firing normally is harmless.
						continue serve
					}
				} else if end < len(batch) {
					// The byte cap tripped mid-wave. Hand the untaken tail back to the
					// head of the queue instead of buffering an unbounded write.
					fd.q.pushFront(batch[end:])
					batch = batch[:end]
				}
				// Keep the batch unsent when the session is already ending. The
				// accumulation wait above returns on the reader's exit, and a
				// handoff mark can land while it waits. Written now, the batch would
				// run on a connection whose replies nobody reads, and recovery would
				// replay it: one command, two executions. It is recovered as never
				// sent instead (copied: batch may reuse a buffer).
				select {
				case <-readerDone:
					carrySuffix = append([]fdReq(nil), batch...)
					break serve // fdConnErr: recovered behind the unacked tail
				default:
				}
				if cn.ShouldHandoff() {
					carrySuffix = append([]fdReq(nil), batch...)
					result = fdRecycle // replayed on the next lease
					break serve
				}
				if e := fd.writeBatch(bg, cn, inflight, batch); e != nil {
					writeErr = e
					break serve
				}
				resetIdle()
				if accumMaxHold {
					// Mirror the <-maxC arm of the serve select, whose tick the
					// accumulation wait above consumed: idle when the pipe drained,
					// recycle when work remains.
					if inflight.empty() && fd.q.depth() == 0 {
						result = fdIdle
					} else {
						result = fdRecycle
					}
					break serve
				}
			case <-readerDone:
				break serve // reader hit a connection error (result stays fdConnErr)
			case <-fd.ap.ctx.Done():
				result = fdGraceful
				break serve
			case <-idleC:
				// Only return the conn when genuinely idle: nothing queued AND the
				// in-flight drained. Otherwise the timer fired mid-stream (e.g. a long
				// flush) — re-arm and keep the hot session.
				if inflight.empty() && fd.q.depth() == 0 {
					result = fdIdle
					break serve
				}
				resetIdle()
			case <-maxC:
				// Max-hold reached. With the pipe drained (nothing in-flight, nothing
				// queued) return fdIdle so run() blocks for the next command: otherwise a
				// quiet engine with FullDuplexMaxHold < FullDuplexIdleTimeout would
				// Get/Put-churn (and re-run the session hooks) every interval.
				// With work pending, recycle to keep serving.
				if inflight.empty() && fd.q.depth() == 0 {
					result = fdIdle
				} else {
					result = fdRecycle
				}
				break serve
			}
		}
	}
	if writeErr != nil {
		if errors.Is(writeErr, errFDConnMoving) || errors.Is(writeErr, errFDMaxHold) {
			// Handoff (errFDConnMoving) or max-hold (errFDMaxHold) mid-carry-replay on a
			// LIVE conn. Route to the clean fdRecycle arm below: the reader drains the
			// already-written prefix so those callers complete normally (not re-executed),
			// attempt then PUTS the conn — the maintnotifications OnPut hook performs the
			// seamless handoff for a moving conn, or the conn simply returns to the pool for
			// max-hold so the hold ends — and run() replays only the never-sent carrySuffix.
			// Do NOT failOnce — nothing failed.
			result = fdRecycle
		} else {
			failOnce(writeErr)
			result = fdConnErr
		}
	}

	switch result {
	case fdGraceful:
		// Clean Close: flush the accepted-but-unwritten fd.ch backlog on this
		// connection first, so Close honors "accepted ⇒ completes" instead of failing
		// it ErrClosed, then let the reader drain every in-flight reply to a RESP
		// boundary.
		unwritten, e := fd.flushBacklogForClose(bg, cn, inflight, readerDone)
		if e != nil && !errors.Is(e, errFDConnMoving) {
			// A real write error (dead conn) failed the backlog partway: some flushed
			// commands have no reply coming and the unwritten suffix is already in
			// inflight (writeCarryChunked pushed it), so closeGraceful would park the
			// reader on replies that never arrive. Close the conn to wake the reader,
			// then RETURN the unacked tail as the recovery set instead of failing it
			// here. As fdConnErr it flows through run()'s standard tail recovery — the
			// per-command budget partition and the NoRetry split. Because ap.ctx
			// is cancelled (this is Close), the eligible prefix is then executed by
			// shutdownFlush on a fresh connection. That honors "accepted ⇒ completes"
			// for the never-sent fd.ch backlog (attempts==1, never touched the dead
			// socket) instead of failing it errFDReaderGone, while the NoRetry split
			// still keeps a written-but-unacked NoRetry command from a second execution
			// (it is failed by the split, never reaching shutdownFlush). takeRemaining
			// holds only UNACKED commands — the reader advanced completed ones out — so
			// nothing already settled is re-run.
			failOnce(e)
			inflight.hardClose()
			_ = cn.Close()
			<-readerDone
			return fd.settleTail(inflight.takeRemaining(), e), fdConnErr, e
		}
		// e == nil (fully flushed) OR errFDConnMoving (handoff mid-flush on a LIVE
		// conn). Either way the connection is healthy: drain the already-written prefix
		// to a boundary so those callers complete with real replies (never failed with a
		// synthetic moving error), then let attempt() Put the conn — a handoff-marked
		// conn is handed off seamlessly by OnPut, exactly like the serve-loop and
		// carry-replay recycles.
		inflight.closeGraceful()
		<-readerDone
		if sharedErr != nil {
			// Reader failed during the final drain: RECOVER the stranded tail (plus the
			// never-sent handoff suffix, in order) instead of failing it wholesale, and
			// report the error so attempt() removes the desynced conn. As fdConnErr the
			// set flows through run()'s standard tail recovery — per-command budget
			// partition and the sent-NoRetry split. Because ap.ctx is cancelled
			// (this is Close), the eligible prefix is then executed by shutdownFlush on a
			// fresh connection, honoring "accepted ⇒ completes" exactly like the
			// backlog-flush failure branch above. Failing here handed every acked-write
			// (replayable reads included) the read error just because Close raced a slow
			// reply.
			return fd.settleTail(fdRecoverTail(inflight.takeRemaining(), unwritten), sharedErr), fdConnErr, sharedErr
		}
		// Handoff mid-flush: the prefix drained cleanly and the conn will be Put
		// (OnPut handoff). RETURN the never-sent suffix so run() completes it on
		// ANOTHER connection AFTER attempt() has Put this conn — accepted ⇒
		// completes. Flushing it HERE would run while attempt() still holds this
		// conn, so with both pools saturated the suffix would deterministically
		// fail. Empty on a plain Close with no handoff. Mirrors the recycle
		// path, which likewise returns its suffix for run() to replay.
		return unwritten, fdGraceful, nil
	case fdIdle, fdRecycle:
		// Clean return: no more pushes, the reader drains the remaining replies (the
		// already-written carry PREFIX included, so those callers complete normally and
		// are NOT replayed), then the conn is at a RESP boundary and safe to Put back —
		// a handoff-marked conn is handed off seamlessly by the OnPut hook, and run()
		// replays only the never-sent suffix (carrySuffix).
		inflight.closeGraceful()
		<-readerDone
		if sharedErr != nil {
			// Reader failed while draining for the clean return: recover the unacked tail
			// for replay and report the error so the conn is removed instead of reused
			// poisoned. carrySuffix (never-sent, e.g. the unwritten tail of a handoff
			// recycle) rides behind the drained tail, in order, and is REFUNDED by
			// fdRecoverTail: writeCarryChunked deliberately did not refund it (the clean
			// fdRecycle return below does not re-bump), but this path re-enters run()'s
			// fdConnErr recovery, which does — without the refund the suffix would be
			// charged for a send that never happened and could be declared
			// budget-exhausted one replay early (e.g. MaxRetries=1: one real send on a
			// dropped session, then MOVING mid-replay plus a reader failure here).
			return fd.settleTail(fdRecoverTail(inflight.takeRemaining(), carrySuffix), sharedErr), fdConnErr, sharedErr
		}
		// carrySuffix is the never-sent suffix to replay on the next lease (nil for a
		// plain idle/recycle; the unwritten tail on a handoff recycle).
		return carrySuffix, result, nil
	default: // fdConnErr
		// Stop the reader, wait for it to exit, THEN take the unacked tail: the reader
		// advances every command it completes, so taking only after <-readerDone is
		// what keeps an entry from being owned by both sides.
		//
		// Close the connection before waiting: on a WRITE error the reader is
		// typically parked in WithReader awaiting a reply that will never arrive, and
		// hardClose only wakes a reader parked in frontBatch. Closing makes that read
		// return at once so recovery does not stall for the read deadline; attempt()
		// removes this conn right after, so the close is safe and idempotent.
		inflight.hardClose()
		_ = cn.Close()
		<-readerDone
		// carrySuffix is a batch the serve loop kept unsent because the reader
		// had exited; it rides behind the unacked tail, refunded (never sent).
		unacked = fdRecoverTail(inflight.takeRemaining(), carrySuffix)
		if sharedErr == nil {
			sharedErr = errFDReaderGone
		}
		return fd.settleTail(unacked, sharedErr), fdConnErr, sharedErr
	}
}

// fdAllow calls the user Limiter's Allow under a recovery boundary. Allow is user
// code that runs on the engine's background writer goroutine, BEFORE writeBatch
// arms its serialize-panic defer, so a panicking Allow would otherwise escape
// fd.run and crash the process with the whole accepted chunk unsettled (half-duplex
// wraps user code via recoverDispatchPanic). A recovered panic is converted to a
// wrapped error and handled EXACTLY like a deny: no permit was granted, so the
// caller reports nothing (strict Allow/ReportResult pairing) and fails only this
// chunk, leaving the connection healthy and untouched.
func fdAllow(ctx context.Context, lim Limiter) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: limiter Allow: %v", errFDPanicRecovered, r)
			internal.Logger.Printf(ctx, "autopipeline: recovered full-duplex limiter Allow panic: %v\n%s", r, debug.Stack())
		}
	}()
	return lim.Allow()
}

// writeBatch pushes each req onto the in-flight FIFO (so it is tracked as
// unacked even if the flush then fails) and writes the whole batch in one
// buffered flush. A write error leaves the reqs in the deque for recovery.
func (fd *fdEngine) writeBatch(bg context.Context, cn *pool.Conn, inflight *fdInflight, reqs []fdReq) (err error) {
	if len(reqs) == 0 {
		return nil
	}
	// Per-chunk Limiter admission. The Limiter's unit everywhere else in the
	// client is one connection-acquiring wire operation — a single-command
	// attempt, a pipeline exec, a half-duplex autopipeline flush — and the FD
	// equivalent of that unit is one written chunk, so Allow/ReportResult pair
	// here, per chunk. The session LEASE deliberately does not pay: a lease-long
	// permit pinned a breaker's half-open probe budget for the whole session
	// lifetime (probe starvation) and gave near-zero failure-signal density (one
	// report per session, however long it ran). The obligation is settled on the
	// REPLY side (see fdLimiterReport): reply-LEVEL errors report success (nil) — a
	// server that answers is healthy — while a transport failure that abandons the
	// chunk's unread replies reports that error (settleTail); further failures also
	// surface through the next chunk's write attempt on the replacement conn and
	// through diverted retries, which keep their own Allow/Report pairing via
	// getConn (parity with single-command retries paying per attempt).
	//
	// A deny fails ONLY this chunk, with the Limiter's error verbatim (failReqs
	// sets it on each command, emits the error metric, and completes the
	// callers): the connection is healthy and untouched — nothing stamped sent,
	// nothing pushed in-flight — so the session continues and the NEXT chunk
	// pays Allow again (fast-fail while a breaker is open, automatic resume when
	// it closes). This early return sits BEFORE the recovery defer below is
	// armed, so a denied chunk can never be pushed into the in-flight deque. A
	// panicking Allow (user code on the writer goroutine) is caught by fdAllow and
	// folded into this same deny path — no permit, so no ReportResult.
	var report *fdLimiterReport
	if lim := fd.client.opt.Limiter; lim != nil {
		if aerr := fdAllow(bg, lim); aerr != nil {
			fd.failReqs(reqs, aerr)
			return nil
		}
		// Admitted: one obligation for this chunk's Allow, settled exactly once on
		// the REPLY side, not at write time (finding ed53z: a peer that accepts the
		// write then drops before replying must be a FAILURE the breaker sees). A
		// clean write hands the obligation to the reader via the chunk's last req
		// (settle nil once every reply lands); a transport failure that abandons the
		// unread replies settles the error (settleTail). A WRITE failure/panic HERE
		// means the replies will never come, so report the write error now — this
		// defer is registered BEFORE the recovery defer below so it runs AFTER it
		// (LIFO) and sees the final err (an encoder panic converted to
		// errFDPanicRecovered is a failed write, not a skipped report). On a clean
		// write it is a no-op; the obligation rides the deque. A denied Allow reports
		// nothing, per the Limiter contract.
		report = &fdLimiterReport{lim: lim}
		defer func() {
			if err != nil {
				report.settle(err)
			}
		}()
	}
	// Publish the batch to the in-flight deque only AFTER a clean serialize+flush
	// (see below). Then the reader arms its per-reply ReadTimeout once the write has
	// reached the socket, not while a slow user encoder (a BinaryMarshaler) is still
	// running. A slow encoder could time out a healthy connection and trigger a
	// spurious replay that duplicates a mutating command. On a partial write or an
	// encoder panic the conn is desynced, so the batch must still land in the deque
	// for the normal conn-error recovery to settle every caller; this defer does that
	// if the happy-path push below did not run. A command encoder panic (writeCmd on a
	// bad BinaryMarshaler) runs on the writer goroutine, where it would otherwise
	// crash the process — convert it to a connection error.
	pushed := false
	// written is the count of commands the serialize loop REACHED this call (index+1
	// of the last one it touched): the attempt-local twin of the lifetime `sent`
	// stamp, reset every call. The recovery defer refunds by it, not by `sent`.
	written := 0
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: encoding batch: %v", errFDPanicRecovered, r)
			internal.Logger.Printf(bg, "autopipeline: recovered full-duplex write panic: %v\n%s", r, debug.Stack())
		}
		if !pushed {
			// Recovery push (partial write or encoder panic): refund the optimistic
			// submit/replay attempt for every command the serializer never REACHED
			// THIS call (index >= written). run()'s fdConnErr recovery partitions the
			// tail by attempt count (fdPartitionByBudget) BEFORE it consults the
			// NoRetry/sent gate, and that partition keys on attempts, not sent. So a
			// command left at its pre-charged attempt count is declared budget-exhausted
			// and FAILED without ever executing (acute at MaxRetries<=1).
			//
			// Refund by `written`, NOT by the lifetime `sent` flag: `sent` is sticky
			// across replays, so on a SECOND-session replay whose earlier command's
			// encoder panics the later commands still carry sent==true from the first
			// session. A `!sent` refund would skip that suffix, leave it over-charged,
			// and lose a retry it never got (MaxRetries==1: exhausted after only its
			// original send). `written` is attempt-local, so it refunds exactly the
			// suffix this call did not reach, regardless of a prior session's send. The
			// prefix reached this call (< written, including a command whose own writeCmd
			// panicked) keeps its charge. It was attempted at-least-once. Mirrors the
			// never-written-suffix refunds in writeCarryChunked / fdRecoverTail.
			fdRefundUnsentAttempt(reqs[written:])
			inflight.pushBatch(reqs)
		}
	}()
	// Stamp the wire-write time and mark each command SENT per-command, immediately
	// before its writeCmd runs, NOT in a bulk loop before the flush. Only commands
	// the serializer actually REACHES are marked sent: if an earlier command's encoder
	// panics (a bad BinaryMarshaler) or a partial write aborts the loop, the
	// never-serialized suffix keeps sent=false, so the NoRetry gate replays it
	// (issuing it is its FIRST send) instead of failing a command the server never
	// saw. A command whose own writeCmd fails/panics stays sent=true: its bytes may
	// have reached the buffer/wire, so the conservative choice avoids re-sending a
	// NoRetry twice; a WithWriter flush error after N commands serialized leaves those
	// N sent. Stamped before pushBatch so the deque copies the reader reads carry both.
	now := time.Now()
	err = cn.WithWriter(bg, fd.client.opt.WriteTimeout, func(wr *proto.Writer) error {
		for i := range reqs {
			if reqs[i].writtenOff == 0 {
				// First write anchors the duration; replays keep it. Floored at 1
				// so a command written within a nanosecond of the epoch is not
				// mistaken for "never written".
				if off := int64(now.Sub(fd.epoch)); off > 0 {
					reqs[i].writtenOff = off
				} else {
					reqs[i].writtenOff = 1
				}
			}
			reqs[i].sent = true
			written = i + 1 // reached this command this call (attempt-local; see refund defer)
			if e := writeCmd(wr, reqs[i].cmd); e != nil {
				return e
			}
		}
		return nil
	})
	if err == nil {
		// Clean flush: attach this chunk's Limiter obligation to its LAST req so the
		// reader settles ReportResult(nil) once every reply lands, then publish — the
		// reader only starts its reply deadline once the bytes are on the wire. On
		// error/panic the report defer above fires the write error and the recovery
		// defer pushes the reqs WITHOUT an obligation, so nothing double-reports.
		if report != nil {
			reqs[len(reqs)-1].limReport = report
		}
		inflight.pushBatch(reqs)
		pushed = true
	}
	return err
}

// fdBatchEnd returns the exclusive end index of the next write chunk starting at
// `start`, applying the same caps as the drain loop: at most maxBatch commands,
// and (when byteLimit > 0) stop once the accumulated approximate payload reaches
// the limit — but always include the first command, so a lone oversized command
// still goes. Pure; the boundary logic is unit-tested.
func fdBatchEnd(reqs []fdReq, start, maxBatch int, byteLimit int64) int {
	end := start + 1
	bytes := cmdApproxBytes(reqs[start].cmd)
	for end < len(reqs) && end-start < maxBatch {
		if byteLimit > 0 && bytes >= byteLimit {
			break
		}
		bytes += cmdApproxBytes(reqs[end].cmd)
		end++
	}
	return end
}

// fdBatchEndSafe is fdBatchEnd with a per-command recover. cmd.Args() (used by
// cmdApproxBytes) is user code and may panic. A carried chunk can include commands
// that never passed the serve loop's cmdApproxBytesSafe admission — the session-start
// command handed straight from fd.ch (run() blocks on the first command and re-issues
// it as carry) and an unacked tail carried between sessions — so a deterministic
// panicking Args() can reach here; it must be contained WITHOUT tearing down a healthy
// connection (nothing in the chunk is written yet, so the conn is not desynced). This
// is the LIVE-session write path (writeCarryChunked only); the terminal Close backlog
// drained by takeQueue does not reach here — it flushes through shutdownFlush/flushReqs,
// which contains a panic with its own recover and aborts the ordered flush. On a panic
// it returns the clean prefix end (start..end, end>=start) and
// bad = the index of the offending command, plus the wrapped error; the caller writes
// [start:end), fails+drops carry[bad], and resumes at bad+1. bad == -1 means the whole
// chunk sized cleanly.
func fdBatchEndSafe(reqs []fdReq, start, maxBatch int, byteLimit int64) (end, bad int, err error) {
	end, bad = start, -1
	idx := start // the command currently being sized; the recover reports it as bad
	defer func() {
		if r := recover(); r != nil {
			end, bad = idx, idx // clean prefix is [start:idx); idx is what panicked
			err = fmt.Errorf("%w: Args: %v", errFDPanicRecovered, r)
			internal.Logger.Printf(context.Background(),
				"autopipeline: recovered full-duplex carry Args() panic: %v\n%s", r, debug.Stack())
		}
	}()
	bytes := cmdApproxBytes(reqs[start].cmd)
	end = start + 1
	for end < len(reqs) && end-start < maxBatch {
		if byteLimit > 0 && bytes >= byteLimit {
			break
		}
		idx = end
		bytes += cmdApproxBytes(reqs[end].cmd)
		end++
	}
	return end, -1, nil
}

// writeCarryChunked re-issues a recovered tail on a fresh connection in the same
// capped chunks as freshly drained work (see fdBatchEnd), so a large recovered
// window is not flushed in one oversized write.
//
// maxC is the session's FullDuplexMaxHold timer (nil when disabled or on the Close
// flush, where a terminating Close bounds its own wait and outranks max-hold). On the
// LIVE path a long or backpressured replay that would hold the conn past max-hold
// stops early, exactly like the ShouldHandoff poll below.
//
// Returns (unwritten, err):
//   - (nil, nil): the whole carry was written.
//   - (suffix, errFDConnMoving): the connection was marked for handoff mid-replay
//     on a still-alive connection. The unwritten suffix is returned OUT-OF-BAND
//     (not pushed into inflight) so the caller can drain the already-written prefix
//     to completion, REMOVE the moving connection, and replay ONLY the never-sent
//     suffix on a fresh connection — the written prefix is not re-executed.
//   - (suffix, errFDMaxHold): the LIVE connection was held past FullDuplexMaxHold
//     mid-replay. Same out-of-band suffix handling as errFDConnMoving; the caller
//     clean-recycles (drains the prefix, Puts the conn so the hold ends) and replays
//     only the never-sent suffix on the next lease.
//   - (nil, errFDReaderGone | write error): the connection is dead; the unwritten
//     suffix is pushed into inflight so the whole unacked tail is recovered and
//     replayed at-least-once (a clean drain is impossible).
func (fd *fdEngine) writeCarryChunked(bg context.Context, cn *pool.Conn, inflight *fdInflight, carry []fdReq, readerDone <-chan struct{}, maxC <-chan time.Time) (unwritten []fdReq, err error) {
	byteLimit := int64(fd.ap.config.MaxBatchBytes) // 0 = disabled
	// stuck becomes true if the reader is observed not draining under a full
	// window; from then on we stop window-gating and just write, so a graceful
	// Close cannot hang here waiting on a reader that never advances (a quiet peer
	// with ReadTimeout disabled). A truly stuck reader is caught downstream by the
	// graceful-drain <-readerDone / Close backstop.
	stuck := false
	for i := 0; i < len(carry); {
		// Between chunks, stop if the reader is gone (decode panic, protocol
		// error mid-replay): writing further chunks to a reader-less connection
		// only enlarges the ambiguous at-least-once set — same priority rule as
		// the serve loop. Push the un-written remainder so takeRemaining recovers
		// the whole accepted set.
		if readerDone != nil {
			select {
			case <-readerDone:
				fdRefundUnsentAttempt(carry[i:]) // never written: do not charge this send
				inflight.pushBatch(carry[i:])
				return nil, errFDReaderGone
			default:
			}
		}
		// Between chunks, stop if the connection was marked for handoff, exactly as
		// the serve loop polls ShouldHandoff: a MOVING/FAILING_OVER push drained by
		// the reader marks cn, and carry replay runs BEFORE the serve loop, so a
		// large replay must not keep streaming to a moving node. The connection is
		// still ALIVE, so return the unwritten suffix OUT-OF-BAND (do NOT push it into
		// inflight): the caller drains the already-written prefix to completion (no
		// re-execution), REMOVES the moving connection, and replays only the suffix on
		// a fresh one. ShouldHandoff() is an atomic load, safe to poll here.
		if cn.ShouldHandoff() {
			return carry[i:], errFDConnMoving
		}
		// Between chunks, stop if the connection has been held past FullDuplexMaxHold, so
		// a long replay under continuous load recycles the conn instead of pinning it (the
		// serve loop's max-hold select is never reached while replay runs). LIVE path only:
		// a terminating Close (ap.ctx cancelled) bounds its own flush and outranks max-hold.
		// Same out-of-band suffix handling as the ShouldHandoff poll above — the conn is
		// still alive.
		if maxC != nil && fd.ap.ctx.Err() == nil {
			select {
			case <-maxC:
				return carry[i:], errFDMaxHold
			default:
			}
		}
		// Bound in-flight to the window between chunks, like the serve loop — which
		// uses a PREDICATE LOOP, not a one-shot wait: inflight.room is a cap-1
		// channel that can hold a STALE signal (the reader popped, the writer
		// observed the room and refilled it without consuming the signal). So after
		// each wake recheck inflight.len() >= fd.window before writing. A one-shot if
		// could consume that stale signal with the deque still full, compute zero
		// remaining room, leave lim at MaxBatchSize, and write past FullDuplexWindow.
		// On graceful Close this writes the backlog on top of replies still in
		// flight, so without the gate a small FullDuplexWindow is exceeded by up to
		// ~2x (window + buffered backlog). Two wait modes, split on ap.ctx:
		//   - LIVE engine (ordinary carry replay at session start): honor the window
		//     like the serve loop — wait for room with no bail-out. The wait is
		//     bounded by the reader itself (a dead peer trips its ReadTimeout, the
		//     reader exits, readerDone fires), and ap.ctx.Done switches a concurrent
		//     Close to the bounded mode without waiting on a drain.
		//   - CLOSE-time flush (ap.ctx cancelled): the wait is BOUNDED instead — if
		//     the reader does not drain within fdCloseFlushWait, stop gating (set
		//     stuck) so Close cannot block forever; correctness of a terminating
		//     Close outranks a transient teardown overshoot.
		for !stuck && readerDone != nil && inflight.len() >= fd.window {
			// Poll handoff on every wake too, like the serve loop's window gate, so a
			// MOVING mark under backpressure recycles within one drained reply. Suffix
			// out-of-band (no push), same as the between-chunk check above.
			if cn.ShouldHandoff() {
				return carry[i:], errFDConnMoving
			}
			if fd.ap.ctx.Err() == nil {
				select {
				case <-inflight.room:
				case <-readerDone:
					fdRefundUnsentAttempt(carry[i:]) // never written: do not charge this send
					inflight.pushBatch(carry[i:])
					return nil, errFDReaderGone
				case <-maxC:
					// Held past FullDuplexMaxHold while waiting for window room: recycle the
					// live conn (out-of-band suffix), same as the between-chunk poll above. A
					// nil maxC (disabled, or the Close flush) never fires.
					return carry[i:], errFDMaxHold
				case <-fd.ap.ctx.Done():
					// Close raced in: re-enter the loop in bounded mode.
				}
				continue
			}
			timer := time.NewTimer(fdCloseFlushWait)
			select {
			case <-inflight.room:
			case <-readerDone:
				timer.Stop()
				fdRefundUnsentAttempt(carry[i:]) // never written: do not charge this send
				inflight.pushBatch(carry[i:])
				return nil, errFDReaderGone
			case <-timer.C:
				stuck = true
			}
			timer.Stop()
		}
		// Cap this chunk to the remaining window room (not just MaxBatchSize): right
		// after the wait releases, a full MaxBatchSize chunk on top of window-1
		// in-flight would still nearly double the bound. Once stuck, write full
		// chunks to finish the flush promptly.
		lim := fd.maxBatch
		if !stuck {
			if room := fd.window - inflight.len(); room > 0 && room < lim {
				lim = room
			}
		}
		// Carry re-sizing uses cmd.Args() (user code), guarded by fdBatchEndSafe. A
		// carried chunk CAN include commands that never passed the serve loop's
		// cmdApproxBytesSafe admission — the session-start command taken straight from
		// fd.ch (run() blocks on the first command and hands it in as carry) and an
		// unacked tail carried between sessions — so a deterministic panicking Args()
		// can reach here. (The terminal Close backlog drained by takeQueue does NOT reach
		// here; it flushes through shutdownFlush/flushReqs, whose own recover aborts the
		// ordered flush.) Contain it WITHOUT tearing the session down: nothing in this chunk
		// is written yet, so the conn is healthy. Fail+drop just the offending command
		// (like the serve loop's sizing guard) and resume with the rest, instead of
		// killing the engine goroutine and letting attempt()'s defer Put a live conn.
		// The contract (see AddHook / a Cmder's Args) still requires deterministic,
		// panic-free Args(); this only stops one bad command from stranding a whole
		// accepted backlog.
		end, bad, sizeErr := fdBatchEndSafe(carry, i, lim, byteLimit)
		if end > i {
			if e := fd.writeBatch(bg, cn, inflight, carry[i:end]); e != nil {
				// writeBatch pushed carry[i:end] into inflight before the failed write (it
				// was attempted — at-least-once — so the serialized prefix keeps its bumped count), but
				// the suffix carry[end:] was never pushed. Push it too, or it sits in neither
				// fd.ch nor inflight and its callers hang: on fdConnErr takeRemaining replays
				// it, on Close the caller fails it. It is only ever settled via failReqs or
				// replayed — never completed inline by the reader — so its zero writtenOff
				// never reaches the write→reply metric. carry[end:] was NEVER written, so
				// refund its optimistic attempt bump here; the never-serialized tail of
				// carry[i:end] (behind an encoder panic) is refunded by writeBatch itself, so
				// only its serialized prefix keeps the charge.
				if end < len(carry) {
					fdRefundUnsentAttempt(carry[end:])
					inflight.pushBatch(carry[end:])
				}
				return nil, e
			}
		}
		if bad >= 0 {
			// carry[bad] (== carry[end]) panicked while sizing. It was never written and
			// is not in inflight, so failing it here cannot double-complete it. Refund the
			// optimistic attempt bump (run() charged the whole carry a send before
			// re-issuing it), fail just this command, and resume past it — the healthy
			// conn keeps serving the rest of the carry.
			fdRefundUnsentAttempt(carry[bad : bad+1])
			fd.failReqs(carry[bad:bad+1], sizeErr)
			i = bad + 1
			continue
		}
		i = end
	}
	return nil, nil
}

// fdRefundUnsentAttempt undoes the optimistic attempt bump for a carried suffix
// that was NEVER written this session (the reader died or the connection broke
// before this chunk was sent). run() bumps the whole carry's attempts before
// re-issuing it, charging each command for a send; a command that was not actually
// sent must not keep that charge, or with a tight MaxRetries it can be declared
// budget-exhausted one replay early. The next replay re-bumps it when it is really
// sent. Floored at 0. Callers apply this to the never-sent suffix BEFORE pushing it
// into the in-flight deque, so the recovered copies carry the corrected count.
func fdRefundUnsentAttempt(reqs []fdReq) {
	for i := range reqs {
		if reqs[i].attempts > 0 {
			reqs[i].attempts--
		}
	}
}

// fdFirstNoRetry returns the index of the first command that must not be
// (re-)issued: a NoRetry command that was already SENT (its bytes may have
// reached the wire — see fdReq.sent), or len(reqs) when there is none. The
// unacked tail is retried up to this index and failed from it on: retryable
// commands ahead of it still get their network retries, while the sent NoRetry
// command and anything ordered after it is never re-sent. A NEVER-SENT NoRetry
// command does not split the tail — replaying it is its first send, so failing
// it would error a command the server never saw.
func fdFirstNoRetry(reqs []fdReq) int {
	for i := range reqs {
		if reqs[i].cmd.NoRetry() && reqs[i].sent {
			return i
		}
	}
	return len(reqs)
}

// fdFirstNoRetrySafe wraps fdFirstNoRetry with a recover. cmd.NoRetry() may be a
// custom Cmder's user code, and the retry-classification scan runs on run()'s
// serve loop, which has no top-level recover — a panic there would kill the
// engine and strand every in-flight and future command. On panic it returns
// panicked=true; the caller then declines to replay and fails the tail (the
// conservative choice: a command that cannot be classified as retryable, and may
// be a sent NoRetry, must never be re-sent).
func fdFirstNoRetrySafe(reqs []fdReq) (n int, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(context.Background(),
				"autopipeline: recovered full-duplex NoRetry() scan panic: %v\n%s", r, debug.Stack())
			n, panicked = 0, true
		}
	}()
	return fdFirstNoRetry(reqs), false
}

// fdNoRetrySafe wraps a single cmd.NoRetry() call with a recover. On the reader's
// reply path NoRetry() is consulted AFTER the reply has already been consumed but
// BEFORE the request is counted complete; a custom Cmder whose NoRetry() panics there
// would otherwise reach the reader's session-failure recover, which treats the request
// as an unacked tail and REPLAYS it — running an already-answered mutating command
// twice. Recover locally and report the command as non-retryable (true) so the caller
// surfaces the already-landed reply inline and never diverts or replays it.
func fdNoRetrySafe(cmd Cmder) (noRetry bool) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(context.Background(),
				"autopipeline: recovered full-duplex NoRetry() panic: %v\n%s", r, debug.Stack())
			noRetry = true
		}
	}()
	return cmd.NoRetry()
}

// fdSetErrSafe wraps a single cmd.SetErr() call with a recover. Same hazard as
// fdNoRetrySafe: every caller settles a request that is about to be marked
// complete regardless of outcome (the reader's reply path, and the off-pipe
// retry goroutine's own recover) — completion must happen unconditionally so
// the request is never left in an unacked, replayable state. A custom Cmder
// whose SetErr() panics here would otherwise escape to whichever recover is
// the caller's own outer boundary (for the reader, the session-failure
// recover, which treats an incomplete request as an unacked tail and REPLAYS
// it — running an already-executed mutating command twice; for the retry
// goroutine's recover, there is no outer boundary at all, so a second panic
// mid-unwind would kill the process). Recover locally and log; the caller
// proceeds to complete the request either way.
func fdSetErrSafe(cmd Cmder, err error) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(context.Background(),
				"autopipeline: recovered full-duplex SetErr() panic: %v\n%s", r, debug.Stack())
		}
	}()
	cmd.SetErr(err)
}

// fdRecoverTail builds an fdConnErr recovery set from a drained unacked tail and
// a never-sent suffix, in order. Fresh slice: rem is takeRemaining's deque-owned
// backing array, so appending onto it could corrupt the deque. The suffix is
// refunded (fdRefundUnsentAttempt): it was never written this session, and run()'s
// fdConnErr recovery bumps the whole replay set for the NEXT issue — without the
// refund a never-sent command would be charged for a send that never happened and
// could be declared budget-exhausted one replay early.
func fdRecoverTail(rem, suffix []fdReq) []fdReq {
	out := make([]fdReq, 0, len(rem)+len(suffix))
	out = append(out, rem...)
	out = append(out, suffix...)
	fdRefundUnsentAttempt(out[len(rem):])
	return out
}

// settleTail settles every Limiter obligation carried by an fdConnErr recovery
// set with err, exactly once, and returns the SAME slice so it wraps a recovery
// expression inline. Called at each session() point that hands a
// written-but-unacked tail back for replay/failure: the reader never completed
// these chunks, so their reply-side outcome is this transport error. Settling
// here — inside session(), inseparable from producing the recovery set — keeps
// "no obligation outlives its session" readable in one function and pairs every
// Allow whose replies never arrived. The field is cleared so the obligation
// never travels into the replay (the rewrite's writeBatch mints a fresh Allow +
// obligation).
func (fd *fdEngine) settleTail(reqs []fdReq, err error) []fdReq {
	for i := range reqs {
		if reqs[i].limReport != nil {
			reqs[i].limReport.settle(err)
			reqs[i].limReport = nil
		}
	}
	return reqs
}

// failReqs completes a set of commands with err (used on retry exhaustion / Close).
// classifyCommandErrorGuarded wraps classifyCommandError with a recover: it calls
// err.Error(), which is user-reachable (e.g. an error returned by a custom Limiter) and
// can panic. The failing paths classify BEFORE completing their requests, and the chunk
// is not in the in-flight queue, so an escaping panic here would leave every caller
// blocked forever (session recovery cannot reclaim it). On panic, fall back to empty
// classification so the requests still settle.
func classifyCommandErrorGuarded(err error) (errorType, statusCode string, isInternal bool) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(context.Background(),
				"autopipeline: recovered full-duplex error classification panic: %v", r)
			errorType, statusCode, isInternal = "", "", false
		}
	}()
	return classifyCommandError(err)
}

// failPipelines fails failed with err, and with it the rest of every FD
// pipeline that has a request in failed: its members in keep and its tail
// still at the head of the queue (the writer may have taken only a prefix).
// A pipeline fails as one, as on a dedicated connection: once part of it has
// failed, the rest must not run. Returns keep without those members.
func (fd *fdEngine) failPipelines(failed, keep []fdReq, err error) []fdReq {
	var groups map[*apBatch]struct{}
	for _, r := range failed {
		if r.pipelined && r.batch != nil && r.batch.fdGroup != nil {
			if groups == nil {
				groups = make(map[*apBatch]struct{})
			}
			groups[r.batch.fdGroup] = struct{}{}
		}
	}
	if groups == nil {
		fd.failReqs(failed, err)
		return keep
	}
	failed = failed[:len(failed):len(failed)] // may be a sub-slice of keep's array: append copies
	inGroup := func(r fdReq) bool {
		if !r.pipelined || r.batch == nil {
			return false
		}
		_, ok := groups[r.batch.fdGroup]
		return ok
	}
	out := make([]fdReq, 0, len(keep))
	for _, r := range keep {
		if inGroup(r) {
			failed = append(failed, r)
		} else {
			out = append(out, r)
		}
	}
	if n := fd.q.headRun(inGroup); n > 0 {
		failed = fd.q.takeInto(failed, n)
	}
	fd.failReqs(failed, err)
	return out
}

func (fd *fdEngine) failReqs(reqs []fdReq, err error) {
	// Error-metric parity: commands terminated here (lease failure, retry
	// exhaustion, a NoRetry tail, Close) never reach the reader's inline
	// completion, so emit the native error callback per command. One classification
	// for the whole set (every req fails with the same err), and no duration metric
	// — many of these were never written.
	errorCallback := pool.GetMetricErrorCallback()
	var errorType, statusCode string
	var isInternal bool
	if errorCallback != nil && len(reqs) > 0 {
		errorType, statusCode, isInternal = classifyCommandErrorGuarded(err)
	}
	for i := range reqs {
		// rawErr(), not Err(): this runs on the engine goroutine, and Err()
		// awaits batch.done — the very channel complete() closes just below — so
		// awaiting here would self-deadlock (the same trap hostHook documents).
		if reqs[i].cmd.rawErr() == nil {
			// fdSetErrSafe: a panicking custom Cmder must not escape here — this
			// runs on the sole fd.run goroutine with no outer recover, so an
			// unguarded panic would kill the engine and leave every later req in
			// reqs unsettled.
			fdSetErrSafe(reqs[i].cmd, err)
		}
		// A pipelined command's failure is reported once for its batch
		// (fdPipelineMetrics), as an ordinary pipeline does.
		if errorCallback != nil && !reqs[i].pipelined {
			octx := reqs[i].ctx
			if octx == nil {
				octx = context.Background()
			}
			// Report attempts-1 retries (like processWithRetry), so a carried tail
			// failed after replays is not undercounted as zero. max() guards a req that
			// somehow carries attempts==0. Guarded per-req (not around the loop) so a
			// panicking callback still lets THIS req and every later one settle below.
			retries := max(0, reqs[i].attempts-1)
			fd.emitMetricsGuarded(octx, func() {
				errorCallback(octx, errorType, nil, statusCode, isInternal, retries)
			})
		}
		reqs[i].complete()
	}
}

// takeQueue closes the submit gate and returns everything buffered in fd.ch. The
// WLock blocks until in-flight submit sends finish (each either landed in fd.ch,
// is drained below, or took its ctx.Done() branch), so after the drain no submit
// can enqueue work that would be left un-completed.
//
// INVARIANT: every takeQueue call is a terminal shutdown drain — run() exits
// right after, past a ctx-cancel check. Never call it on a non-close path: a
// submit blocked on a full channel is unwedged only by its ctx.Done() branch, so
// without a cancelled ctx the WLock deadlocks against the RLock held across that
// send.
func (fd *fdEngine) takeQueue() []fdReq {
	fd.submitMu.Lock()
	fd.closed = true
	fd.submitMu.Unlock()
	// closeQueue rejects any push that slipped past the gate check, and releases
	// submitters blocked on a full queue so they observe the closed state.
	fd.q.closeQueue()
	return fd.q.drainAll(nil)
}

// shutdownFlush is the between-sessions Close flush: accepted commands in carry
// (an unacked tail from a failed session, never re-leased) and in fd.ch
// (accepted while no session held a connection) are executed on the client's
// normal pipeline path, honoring the "accepted ⇒ completes" Close contract
// instead of failing them ErrClosed just because Close won the race between
// sessions (#3964). Uses a background ctx (ap.ctx is already cancelled);
// processPipeline bounds it with the client's own timeouts/retries and setCmdsErr
// puts any failure on every command, so callers always settle.
func (fd *fdEngine) shutdownFlush(bg context.Context, carry []fdReq) {
	// Drain and close the queue now (before flushing carry) so no new submit lands
	// mid-flush. fresh commands (attempts == 1) have not run yet.
	fresh := fd.takeQueue()
	// The writer may have taken only a prefix of an FD pipeline before the
	// session failed: the prefix is then at the end of carry and the tail at
	// the head of fresh. Move the tail into carry so the pipeline flushes as
	// one, with one retry budget.
	if n := len(carry); n > 0 && len(fresh) > 0 && fdSameGroup(carry[n-1], fresh[0]) {
		k := 1
		for k < len(fresh) && fdSameGroup(fresh[k-1], fresh[k]) {
			k++
		}
		carry = append(carry[:n:n], fresh[:k]...) // copy: carry may share a backing array
		fresh = fresh[k:]
	}
	// The flush below runs pipelined commands through the pooled pipeline with
	// its own retry loop; flushReqs counts that as a further issue on each batch
	// it runs, so fdPipelineExec does not run them again.
	// Flush the carried tail honoring EACH command's remaining retry budget across
	// the Close boundary (flushCarryBudgeted), then the fresh queue at the full
	// budget. Flushing carry first keeps FIFO order across the two sets.
	if err := fd.flushCarryBudgeted(bg, carry); err != nil {
		// Carry hit an unreachable endpoint; do not run the fresh queue through a full
		// retry cycle against the same dead endpoint (Close would otherwise stall for
		// chunks × retries × backoff) — fail it with the same transport error.
		fd.failReqs(fresh, err)
		return
	}
	// Last flush: a transport failure is already handled inside flushReqs (it fails
	// the remainder), and there is nothing after it, so the returned error is moot.
	_ = fd.flushReqs(bg, fresh, fd.retryBudget())
}

// fdSameGroup reports whether b follows a in the same FD pipeline batch.
func fdSameGroup(a, b fdReq) bool {
	return a.pipelined && b.pipelined && a.batch != nil && b.batch != nil &&
		a.batch.fdGroup != nil && a.batch.fdGroup == b.batch.fdGroup
}

// fdCarryRemainingRetries returns the retry bound for a carried command flushed on
// Close. carry commands are POST-BUMP: run() bumps attempts before re-issuing, so a
// command carried at attempts=A has completed A-1 executions (contrast the pre-bump
// unacked tail in run(), where A executions are done). Of its MaxRetries+1 total
// budget it may still run MaxRetries+2-A times, i.e. a retry bound of
// MaxRetries+1-A. A negative result means the budget is spent (drop the command).
// attempts is clamped to >=1: attempts==0 is only reachable from test-constructed
// fdReq literals, and the clamp keeps such a command at full budget rather than
// granting MaxRetries+2.
func fdCarryRemainingRetries(attempts, maxRetries int) int {
	if attempts < 1 {
		attempts = 1
	}
	return maxRetries + 1 - attempts
}

// flushCarryBudgeted flushes the carried tail on Close so no command exceeds — or
// falls short of — its configured MaxRetries+1 total executions. Commands are
// flushed in contiguous groups of equal attempt count, each with its own remaining
// budget (fdCarryRemainingRetries); a group whose budget is spent is failed, not
// re-run. Grouping equal-attempt RUNS is correct regardless of ordering (carry is
// normally attempts-descending, so groups are few, but a non-sorted slice just
// yields more groups). Returns a transport error that aborted the remainder.
func (fd *fdEngine) flushCarryBudgeted(bg context.Context, carry []fdReq) error {
	mr := fd.retryBudget()
	for i := 0; i < len(carry); {
		a := carry[i].attempts
		j := i + 1
		if carry[i].pipelined {
			// One FD pipeline batch, even if its commands carry different
			// attempt counts, so flushReqs can run it as one pipeline.
			for j < len(carry) && fdSameGroup(carry[j-1], carry[j]) {
				j++
			}
		} else {
			// A run of other commands with the same attempt count. It stops at
			// a pipeline batch, so no command shares a pipeline's budget.
			for j < len(carry) && !carry[j].pipelined && carry[j].attempts == a {
				j++
			}
		}
		group := carry[i:j]
		i = j
		// The group runs as one pipeline, so it gets the smallest remaining
		// budget of its commands: none may run past its MaxRetries+1
		// executions. A command with more budget left may therefore get fewer
		// retries than it alone would, as in an ordinary pipeline, whose retry
		// budget covers the whole batch.
		rem := fdCarryRemainingRetries(a, mr)
		for k := range group {
			rem = min(rem, fdCarryRemainingRetries(group[k].attempts, mr))
		}
		if rem < 0 {
			fd.failReqs(group, errFDRetryBudgetExhausted) // budget spent; do not re-run
			continue
		}
		if err := fd.flushReqs(bg, group, rem); err != nil {
			if i < len(carry) {
				fd.failReqs(carry[i:], err) // transport failure: fail the rest of the carry too
			}
			return err
		}
	}
	return nil
}

// flushReqs runs reqs through the client pipeline in the same
// MaxBatchSize/MaxBatchBytes chunks as normal FD writes and completes each
// request. maxRetries bounds each chunk's retry loop (0 = a single execution,
// used for already-attempted carried commands so their per-command budget is not
// exceeded). Returns a non-nil error when the remainder was aborted and failed
// here: a transport failure, a desynchronized reply stream (errConnUnusable — e.g.
// a custom push processor errored during the drain, so the chunk was never sent),
// or a recovered serialize panic. A plain per-command Redis error is a normal
// result and does not abort.
//
// Unlike the live-session write path (writeCarryChunked, which sizes with
// fdBatchEndSafe and isolates a single panicking command), this terminal Close
// flush does NOT isolate a per-command Args()/NoRetry() panic: the outer recover
// fails the remainder so an ordered flush stops rather than running later chunks
// out of order. Every accepted command still settles (it is failed, not left
// hanging), honoring accepted⇒completes. See TestFDShutdownFlushAbortsAfterRecoveredPanic.
func (fd *fdEngine) flushReqs(bg context.Context, reqs []fdReq, maxRetries int) (retErr error) {
	if len(reqs) == 0 {
		return nil
	}
	// A panic here (user arg encoder inside the pipeline) runs on the engine
	// goroutine with no other recovery; fail and complete the remainder so no caller
	// hangs. Set the NAMED return so an ordered shutdown flush aborts like a
	// transport failure: an unnamed result would zero to nil after recovery, and
	// flushCarryBudgeted/shutdownFlush would then treat the failed group as success
	// and run later groups + the fresh queue even though an earlier command never
	// completed.
	i := 0
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("%w: shutdown flush: %v", errFDPanicRecovered, r)
			internal.Logger.Printf(bg, "autopipeline: recovered shutdown-flush panic: %v\n%s", r, debug.Stack())
			fd.failReqs(reqs[i:], retErr)
		}
	}()
	// Test seam: nil in production, so this is exactly client.processPipelineRetries.
	run := fd.runPipeline
	if run == nil {
		run = fd.client.processPipelineRetries
	}
	// Same MaxBatchSize/MaxBatchBytes chunking as normal FD writes: reqs can hold a
	// large window, and one unchunked pipeline would ignore MaxBatchBytes and burst
	// the connection.
	byteLimit := int64(fd.ap.config.MaxBatchBytes) // 0 = disabled
	for i < len(reqs) {
		end := fdBatchEnd(reqs, i, fd.maxBatch, byteLimit)
		if reqs[i].pipelined {
			// An FD pipeline batch flushes as ONE pipeline, whatever its size, as
			// Pipeline.Exec sends it: its first command's retryable reply then
			// retries the whole batch together, not only the first chunk.
			end = i + 1
			for end < len(reqs) && fdSameGroup(reqs[end-1], reqs[end]) {
				end++
			}
		} else {
			// A chunk of ordinary commands stops where a pipeline batch starts,
			// so the pipeline is not split across chunks.
			for k := i + 1; k < end; k++ {
				if reqs[k].pipelined {
					end = k
					break
				}
			}
			// Do not mix retry policies in one chunk: generalProcessPipeline
			// disables retries for the WHOLE chunk if any command is NoRetry
			// (cmdsContainNoRetry). That would strip retryable commands in the same
			// accepted backlog of their budget. Break the chunk at the first
			// NoRetry-policy change so a NoRetry command (e.g. RawWriteToCmd) is
			// isolated from its retryable neighbors, like the half-duplex
			// dispatcher's contiguous retry-policy runs. The clamp starts at i+1,
			// so end stays > i and the chunk is never empty (no infinite loop).
			policy := reqs[i].cmd.NoRetry()
			for k := i + 1; k < end; k++ {
				if reqs[k].cmd.NoRetry() != policy {
					end = k
					break
				}
			}
		}
		cmds := make([]Cmder, end-i)
		for j := i; j < end; j++ {
			cmds[j-i] = reqs[j].cmd
			// The pooled pipeline below runs this chunk again and records its
			// pipeline metric. Stamped here, where the chunk runs: one more
			// issue (so fdPipelineExec does not re-run it), and flushed. A batch
			// that never runs (a dead endpoint fails the rest) keeps its issue
			// count and stays unmarked, so fdPipelineExec records its failure.
			if reqs[j].pipelined && reqs[j].batch != nil {
				reqs[j].batch.fdAttempts = reqs[j].attempts + 1
				reqs[j].batch.fdFlushed = true
			}
		}
		// Initialize the flush with a request's own context (cancellation removed), not
		// the engine's background context: if this flush initializes a fresh pooled
		// connection, a CredentialsProviderContext resolves credentials from the
		// request's context values, so it authenticates as the right tenant.
		// WithoutCancel because these contexts may already be cancelled (often what
		// triggered Close), yet accepted-⇒-completes still requires the write. A chunk
		// can mix callers; the first request is the representative — a documented
		// approximation, matching the diverted-retry and session-init paths.
		fctx := bg
		if c := reqs[i].ctx; c != nil {
			fctx = context.WithoutCancel(c)
		}
		err := run(fctx, cmds, maxRetries) // per-command results/errors set inside
		for j := i; j < end; j++ {
			reqs[j].complete()
		}
		i = end
		// Stop and fail the remaining chunks when the error means the earlier chunk
		// may never have been written correctly: a transport failure that survived
		// the retry loop (dead endpoint), OR a desynchronized reply stream marked
		// errConnUnusable (e.g. a custom push processor errored during the close-time
		// drain, so the chunk was never sent). Continuing would run an ordered
		// shutdown flush out of order. pipelineErrShouldStamp is the same
		// errConnUnusable precedence used in generalProcessPipeline; a plain
		// per-command Redis error is a normal result and does not abort.
		if err != nil && pipelineErrShouldStamp(err) {
			fd.failReqs(reqs[i:], err)
			return err
		}
	}
	return nil
}

// failQueue fails every command currently buffered in fd.ch with err WITHOUT
// closing the engine (unlike takeQueue, the shutdown drain, which sets closed).
// Used on fdLeaseErr, where the carry goes through failReqs and this drains the
// accepted backlog — both halves emit the native error metric. The engine stays
// alive, so a command submitted after this returns is served once the
// server/pool recovers, or failed when the next lease exhausts its retries. The
// channel receive is safe against a concurrent submit send, so no lock is taken
// here.
func (fd *fdEngine) failQueue(err error) {
	errorCallback := pool.GetMetricErrorCallback()
	var errorType, statusCode string
	var isInternal bool
	classified := false
	// drainAll takes the whole backlog under one lock, replacing the old
	// drain-until-empty receive loop. The engine stays open, so a command
	// submitted after this returns is queued normally.
	for _, r := range fd.q.drainAll(nil) {
		// fdSetErrSafe: same hazard as failReqs — a panicking custom Cmder must
		// not escape on the sole fd.run goroutine with no outer recover.
		fdSetErrSafe(r.cmd, err)
		// A pipelined command's failure is reported once for its batch
		// (fdPipelineMetrics), as in failReqs.
		if errorCallback != nil && !r.pipelined {
			if !classified {
				errorType, statusCode, isInternal = classifyCommandErrorGuarded(err)
				classified = true
			}
			octx := r.ctx
			if octx == nil {
				octx = context.Background()
			}
			// Guarded per-req so a panicking callback still lets r.complete() run.
			fd.emitMetricsGuarded(octx, func() {
				errorCallback(octx, errorType, nil, statusCode, isInternal, 0)
			})
		}
		r.complete()
	}
}

// flushBacklogForClose is the graceful-Close flush: it stops new submits (sets
// closed) and writes every command still buffered in fd.ch on the current
// connection, in the same MaxBatchSize/MaxBatchBytes chunks as normal writes, so
// ACCEPTED commands complete instead of failing ErrClosed. The caller then
// closeGraceful()s the deque so the reader drains these replies before exiting.
// Returns (unwritten, err) from writeCarryChunked: on errFDConnMoving (handoff
// mid-flush, live conn) unwritten is the never-sent suffix the caller flushes
// elsewhere; on a real write error unwritten is nil (that suffix is already in
// inflight) and the caller degrades to the conn-error path.
func (fd *fdEngine) flushBacklogForClose(bg context.Context, cn *pool.Conn, inflight *fdInflight, readerDone <-chan struct{}) ([]fdReq, error) {
	fd.submitMu.Lock()
	fd.closed = true
	fd.submitMu.Unlock()
	fd.q.closeQueue()
	backlog := fd.q.drainAll(nil)
	// Return the unwritten suffix OUT-OF-BAND (do not push it into inflight) so
	// the caller can tell a live handoff (errFDConnMoving) from a dead-conn write
	// error: on handoff it clean-recycles — drains the written prefix, Puts the
	// conn for the OnPut maintenance handoff, and completes the never-sent suffix
	// on another connection — instead of failing accepted work. The dead-conn
	// paths inside writeCarryChunked already pushed their suffix into inflight and
	// return an empty one here.
	// nil maxC: this is the Close flush; a terminating Close bounds its own wait
	// (fdCloseFlushWait) and outranks max-hold, which applies only to a LIVE session.
	return fd.writeCarryChunked(bg, cn, inflight, backlog, readerDone, nil)
}

// sleepBackoff waits the retry backoff, interruptible by Close.
func (fd *fdEngine) sleepBackoff(attempt int) {
	d := internal.RetryBackoff(attempt, fd.client.opt.MinRetryBackoff, fd.client.opt.MaxRetryBackoff)
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-fd.ap.ctx.Done():
	}
}
