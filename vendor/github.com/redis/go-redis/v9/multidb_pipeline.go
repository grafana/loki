package redis

import (
	"context"
	"errors"
	"math"
	"reflect"

	imultidb "github.com/redis/go-redis/v9/internal/multidb"
)

// This file makes MultiDBClient a full UniversalClient and wires pipelines
// and autopipelining through the MultiDB core, per the MultiDB × AutoPipeline
// design: batches resolve the active database at exec time, feed the circuit
// breaker and failure detector (failures once per distinct error), and are
// retried against the newly selected database after a failover.

var _ UniversalClient = (*MultiDBClient)(nil)

// Stated directly as well: MultiDBClient satisfies Cmdable in its own right
// (Pipeline/Pipelined/TxPipeline/TxPipelined plus the command surface), not
// only through the UniversalClient embedding above.
var _ Cmdable = (*MultiDBClient)(nil)

// resetCmds clears per-command errors from a previous attempt so a retried
// batch does not report stale failures.
func resetCmds(cmds []Cmder) {
	for _, cmd := range cmds {
		cmd.SetErr(nil)
	}
}

// sameErr reports whether a and b are the same error value. Unlike ==, it
// does not panic on non-comparable error types, including a comparable
// struct whose interface field holds a non-comparable value.
func sameErr(a, b error) (same bool) {
	t := reflect.TypeOf(a)
	if t != reflect.TypeOf(b) || t == nil || !t.Comparable() {
		return false
	}
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return a == b
}

// recordBatchOutcomes records batch outcomes, mirroring the single-command
// path: an error reply from the server proves the database is reachable and
// counts as a success; transport-level errors count as failures (once per
// distinct error on the breaker and the detector, see chargeBreaker);
// client-side errors
// (context cancellation, deterministic
// local rejections per shouldRetry) record nothing and do not trigger
// failover. It returns the number of transport-level failures.
//
// Errors are read via rawErr, never Err: this runs on the pipeline execution
// path (including the autopipeliner's batch dispatcher, before the batch's
// done channel closes), where Err on an async command would await the very
// batch being completed and wedge the dispatcher.
func (c *multidbCore) recordBatchOutcomes(ctx context.Context, db *multidbDatabase, cmds []Cmder, batchErr error, executed *executedCmds, res imultidb.Reservation, detectorGen uint64) int {
	// `executed` is the per-command execution marker, not inferred from command
	// state: an executed all-success batch whose error was injected by a
	// post-exec hook looks identical on the commands, and stamping it would turn
	// already-applied writes into phantom transport failures and replay them.
	// It is per-command because a cluster batch fans out into independent node
	// sub-batches — one node executing must not make another node's untouched
	// commands look served. `res` is the gate admission's half-open reservation:
	// the whole batch is ONE admission, so RecordSuccessFor / ReleaseFor settle
	// the breaker's half-open slot exactly once however many commands succeed,
	// and a closed-state admission (res.held == false) holds no slot to settle.
	if !executed.any() {
		if batchErr == nil {
			// A member hook served the batch locally (returned nil without
			// calling next): the results are valid for the caller, but
			// nothing reached Redis — record no health signal and give the
			// gate's probe slot back (ReleaseFor no-ops for a closed admission).
			db.cb.ReleaseFor(res)
			return 0
		}
		// Execution never started: the batch error stands in for every
		// command the aborting hook did not stamp itself. Preserve results
		// already supplied by a successful node hook.
		for _, cmd := range cmds {
			if cmd.rawErr() == nil && executed.state(cmd) != pipelineAnsweredLocally {
				cmd.SetErr(batchErr)
			}
		}
	}
	// Blocking commands carry their own read timeout; their local deadlines
	// are not retryable transport failures — the same per-command rule the
	// single-command path applies (cmd.readTimeout() == nil).
	//
	// The pipeline reader stops at the first transport error and stamps that
	// SAME error onto every unread follower (pipelineReadCmds; the cluster
	// path does the same per node). When the originating command is a
	// blocking command whose local deadline is neutral, the followers carry
	// a propagation of that one neutral event, not N transport failures of
	// their own: counting them would charge the breaker and detector N times
	// and replay the whole batch — blocking command included — off a
	// deadline the blocker itself does not retry. The first carrier of an
	// error in slice order is its originator: stamping only ever reaches
	// later commands, and a node sub-batch keeps the original order.
	//
	// Errors are matched by identity, not errors.Is: the readers stamp the
	// same value, and an independent failure on another node can wrap the
	// same sentinel (e.g. os.ErrDeadlineExceeded) as the blocker's timeout.
	batchOutcome := batchOutcomeFunc(cmds)
	// After the caller's ctx ended, each command is checked on its own: on a
	// cluster fan-out one node's dial can be cut by ctx while another node's
	// read times out, and the batch error carries only one of them.
	ctxTimeouts := db.ctxTimeouts()
	classify := func(cmd Cmder) outcomeKind {
		if callerEnded(ctx, cmd.rawErr(), ctxTimeouts) {
			return outcomeNeutral
		}
		return batchOutcome(cmd)
	}
	// The GLOBAL detector (and successSinceFailover) is fed only while this
	// member is STILL the active — re-checked per recorded outcome, not once up
	// front: a concurrent switchActive can vacate this member partway through
	// recording a large batch, and outcomes recorded after that must not pollute
	// the new active's failover window. The per-member breaker below is always
	// updated — it is member-scoped. Mirrors the single-command path (process).
	transportFailures := 0
	recorded := 0
	// charged holds the failure errors already counted on the breaker in this
	// batch attempt. The reader stamps one transport error on every unread
	// command (per node on a cluster fan-out), so the breaker counts each
	// distinct error once: one connection drop is one member failure, and N
	// failed nodes are N. Matched by identity (sameErr), like the propagation
	// scan above. Identity also merges a bare singleton sentinel: two nodes
	// each failing with a bare io.EOF count once. The detector counts
	// failures the same way (its own set: it is fed only while db is active),
	// so one drop in a large batch does not trip it alone; successes still
	// count per command.
	firstOfErr := func() func(error) bool {
		var seen []error
		return func(err error) bool {
			for _, e := range seen {
				if sameErr(err, e) {
					return false
				}
			}
			seen = append(seen, err)
			return true
		}
	}
	chargeBreaker, chargeDetector := firstOfErr(), firstOfErr()
	recordOne := func(cmd Cmder) {
		switch classify(cmd) {
		case outcomeSuccess:
			if !executed.has(cmd) {
				// This command never reached the server — a hook served it
				// locally, or (cluster fan-out) its node short-circuited while
				// another node executed. Its nil error is no proof the database
				// answered: surface it to the caller, record nothing.
				return
			}
			// Settle the batch's single reservation: RecordSuccessFor releases
			// (or, at SuccessThreshold, closes on) the half-open slot at most
			// once however many commands succeed. For a closed admission
			// (res.held == false) it clears the failure count while the circuit
			// is still closed and never counts toward closing a later half-open
			// episode the batch was not admitted to.
			db.cb.RecordSuccessFor(res)
			// detectorGen: skip the write if a detector reset landed since the
			// batch sampled its window (see multidbCore.detectorGen).
			if cur, _ := c.activeSnapshot(); cur == db {
				// Recovery traffic breaks the consecutive-failed-failover chain
				// for batch-only workloads too (see recordFailedFailoverLocked).
				// The flag has no window semantics, so it is set regardless of
				// the detector generation; only the detector write is gated.
				c.successSinceFailover.Store(true)
				c.recordDetectorSuccess(detectorGen)
			}
			recorded++
		case outcomeFailure:
			// Failures record regardless of the execution marker — unlike
			// successes: a dial failure (the canonical down-member signal)
			// happens BEFORE the marker flips, and it is indistinguishable
			// from a pre-execution hook abort. Guarding failures on the
			// marker would leave breakers closed through real outages on
			// batch-only workloads. Successes are the asymmetric case: a
			// genuine reply cannot exist without execution.
			//
			// Settled through the reservation like successes: a held admission
			// records only while its half-open episode is still current — a
			// batch that outlived a failed recovery must not re-open the NEW
			// episode it was never admitted to; a closed admission always
			// counts toward opening. One breaker and one detector failure per
			// distinct error (see chargeBreaker).
			if chargeBreaker(cmd.rawErr()) {
				db.cb.RecordFailureFor(res)
			}
			if cur, _ := c.activeSnapshot(); cur == db && chargeDetector(cmd.rawErr()) {
				c.recordDetectorFailure(detectorGen, cmd.rawErr())
			}
			transportFailures++
			recorded++
		case outcomeNeutral:
			// Not a database-health signal (client-side error or a locally
			// synthesized Redis error such as ErrCrossSlot); surfaced to the
			// caller as-is.
		}
	}
	if db.cb.State() == imultidb.CircuitHalfOpen {
		// Recovery probe: record failures BEFORE successes, so a batch that
		// ultimately failed cannot close the circuit off its own successful
		// prefix. Everywhere else the arrival order stands — mirroring
		// sequential single commands, where a success resets the closed
		// state's failure count before a later failure increments it.
		for _, cmd := range cmds {
			if classify(cmd) == outcomeFailure {
				recordOne(cmd)
			}
		}
		for _, cmd := range cmds {
			if classify(cmd) != outcomeFailure {
				recordOne(cmd)
			}
		}
	} else {
		for _, cmd := range cmds {
			recordOne(cmd)
		}
	}
	if recorded == 0 {
		// The whole batch was neutral (e.g. a local ErrCrossSlot rejection):
		// nothing was recorded on the breaker, so give back the half-open
		// probe slot the admission reserved (ReleaseFor no-ops otherwise).
		db.cb.ReleaseFor(res)
	}
	return transportFailures
}

// errMultiDBHImportCmd matches the HIMPORT command family, which is rejected
// on MultiDBClient (see errMultiDBHImport): the direct methods are overridden,
// and the batch paths below must reject them too — pipeline builders dispatch
// through their own cmdable and would otherwise hand the command to a single
// member's registry.
func cmdsContainHImport(cmds []Cmder) bool {
	for _, cmd := range cmds {
		if isHImportCmd(cmd) {
			return true
		}
	}
	return false
}

// clearRoutingDecisions drops cluster routing decisions pinned on cmds by a
// member's autopipeliner, so the member that runs them resolves its own.
func clearRoutingDecisions(cmds []Cmder) {
	for _, cmd := range cmds {
		cmd.setClusterRoutingDecision(nil)
	}
}

// processPipeline is the batch analogue of process: an attempt loop bounded
// by CommandRetries that snapshots the active database, executes the whole
// batch through the member's hook-wrapped pipeline path, records per-command
// outcomes, and retries against the newly selected database after a
// failover. A retry resends only the commands without a definitive result
// (see unsettledCmds), so member pipeline hooks may see a partial batch on a
// retry. Delivery is at-least-once when retries are enabled: a connection
// that broke mid-pipeline may have executed a prefix server-side.
func (c *multidbCore) processPipeline(ctx context.Context, cmds []Cmder) error {
	return c.processPipelineRetries(ctx, cmds, c.opts.CommandRetries)
}

// processPipelineRetries is processPipeline with an explicit retry bound in
// place of CommandRetries: the batch runs at most maxRetries+1 times. The
// full-duplex router reroutes through it with the commands' remaining budget,
// so a command already issued on the member's FD connection does not get a
// fresh CommandRetries+1 executions.
func (c *multidbCore) processPipelineRetries(ctx context.Context, cmds []Cmder, maxRetries int) error {
	if len(cmds) == 0 {
		return nil
	}
	// The full-duplex reroute reaches here with decisions pinned by the old
	// member's cluster router; the next member resolves its own.
	clearRoutingDecisions(cmds)
	if c.closed.Load() {
		setCmdsErr(cmds, ErrClosed)
		return ErrClosed
	}
	// orig keeps the caller's slice for the final error: rejected HIMPORT
	// commands stay in it, so cmdsFirstErr reports the POSITIONALLY first
	// failure like Pipeline.Exec would.
	orig := cmds
	himportRejected := false
	if cmdsContainHImport(cmds) {
		// Reject only the HIMPORT commands themselves: the autopipeliner
		// coalesces unrelated callers into one flush, and poisoning the
		// whole batch would fail innocent commands that merely shared it.
		kept := make([]Cmder, 0, len(cmds))
		for _, cmd := range cmds {
			if isHImportCmd(cmd) {
				cmd.SetErr(errMultiDBHImport)
				continue
			}
			kept = append(kept, cmd)
		}
		if len(kept) == 0 {
			return errMultiDBHImport
		}
		cmds = kept
		himportRejected = true
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	attempts := maxRetries + 1
	if maxRetries == math.MaxInt {
		// Guard the +1 against wrapping to a negative attempt count — which
		// would skip the loop and silently drop the batch (returning nil).
		// Mirrors the single-command path in process().
		attempts = math.MaxInt
	}
	// A batch containing a non-retryable command (e.g. a streaming
	// RawWriteToCmd) must be executed at most once: retrying it after a
	// transport failure could duplicate execution or corrupt a partial write.
	if cmdsContainNoRetry(cmds) {
		attempts = 1
	}

	attempt := 0
	// Bound consecutive gate rejections, mirroring the single-command path:
	// with every selectable member half-open and probe budgets exhausted,
	// failover keeps handing back members the gate rejects — surface
	// unavailability instead of busy-looping the active index.
	gateRejections := 0
	// narrowed is set once cmds holds only the unsettled commands of a failed
	// attempt: the others keep definitive results in orig.
	narrowed := false

	// exitErr picks the batch error for every exit: with a rejected HIMPORT
	// in the batch the positionally first error over the ORIGINAL slice wins,
	// like Pipeline.Exec reports it — including early gate exits, whose
	// stamped error would otherwise displace a rejection that precedes the
	// stamped commands positionally.
	exitErr := func(err error) error {
		if himportRejected {
			if ferr := cmdsFirstErr(orig); ferr != nil {
				return ferr
			}
		}
		return err
	}
	// gateExitErr is exitErr for a gate failure: after a narrowed retry the
	// earlier attempts' errors are the batch result, as when attempts run out.
	gateExitErr := func(err error) error {
		if narrowed {
			if ferr := cmdsFirstErr(orig); ferr != nil {
				return ferr
			}
		}
		return exitErr(err)
	}

	// The current attempt's admission, released on any exit that did not
	// settle it: a panic in a member hook or the member path unwinds past
	// recordBatchOutcomes and the cancel branch, and a leaked half-open slot
	// would wedge the breaker at MaxHalfOpenRequests. ReleaseFor settles at
	// most once and no-ops for a closed admission, so it is harmless after a
	// normal settle too (Watch uses the same defer).
	var pendingDB *multidbDatabase
	var pendingRes imultidb.Reservation
	defer func() {
		if pendingDB != nil {
			pendingDB.cb.ReleaseFor(pendingRes)
		}
	}()

	for attempt < attempts {
		if c.closed.Load() {
			// Close landed mid-retry: report the terminal state instead of
			// escalating through the drained membership. Only stamp the commands
			// when no attempt has executed yet; once a prior attempt applied a
			// prefix, its per-command results are the caller's evidence and must
			// not be overwritten (see the post-exec cancel branch below).
			if attempt == 0 {
				resetCmds(cmds)
				setCmdsErr(cmds, ErrClosed)
			}
			return exitErr(ErrClosed)
		}
		if err := ctx.Err(); err != nil {
			// Only stamp the cancellation when no attempt has executed yet. Once
			// a prior attempt applied a prefix (delivery is at-least-once with
			// retries), overwriting it would report an executed command as
			// canceled and invite a replay — the same reasoning as the post-exec
			// cancel branch below.
			if attempt == 0 {
				resetCmds(cmds)
				setCmdsErr(cmds, err)
			}
			return exitErr(err)
		}

		// Detector before the breaker admission: a half-open admission
		// reserves a bounded probe slot, and a tripped detector routes to
		// failover without executing the batch — the reservation would leak.
		db, idx := c.activeSnapshot()
		// Sample the detector window with the member (see multidbCore.detectorGen).
		dg := c.detectorGen.Load()
		admitted := false
		var res imultidb.Reservation
		if db != nil && !c.shouldFailoverSafely() {
			admitted, res = db.cb.AllowReserve()
		}
		if !admitted {
			gateRejections++
			if gateRejections > c.memberCount()+1 {
				err := ErrTemporarilyNotAvailable
				if attempt == 0 {
					resetCmds(cmds)
					setCmdsErr(cmds, err)
				}
				return gateExitErr(err)
			}
			if err := c.tryFailover(ctx, idx); err != nil {
				// Stamp only when nothing has executed yet; a prior attempt's
				// applied prefix must survive (see the ctx branch above).
				if attempt == 0 {
					resetCmds(cmds)
					setCmdsErr(cmds, err)
				}
				return gateExitErr(err)
			}
			// Re-enter the gate on the newly selected database — mirroring
			// the single-command path: its breaker may be half-open and the
			// AllowReserve call above is what reserves the bounded probe slot.
			// Re-gating does not consume a retry attempt.
			continue
		}
		gateRejections = 0
		pendingDB, pendingRes = db, res

		// Clear any per-command error before executing. On a retry this drops
		// the previous attempt's stamps; on the first attempt it drops an error
		// a caller left on a reused command, so a batch that aborts before
		// execution (e.g. a connection-acquire failure) still records the
		// transport failure through setCmdsErr instead of being masked by stale
		// per-command errors setCmdsErr refuses to overwrite. The rejected
		// HIMPORT stamps live in orig, not in cmds (kept is a fresh slice), so
		// this cannot clear them.
		resetCmds(cmds)
		attempt++
		// The marker tells recordBatchOutcomes whether execution started
		// (fresh per attempt): command state alone cannot distinguish a
		// pre-execution hook abort from a post-execution hook error. It rides
		// on the context, so a member hook must pass the received ctx (or a
		// child of it) to next — see AddDatabaseHook.
		executed := newExecutedCmds(len(cmds))
		err := db.processPipelineHook(context.WithValue(ctx, pipelineExecutedKey{}, executed), cmds)
		if err != nil && ctx.Err() != nil {
			// The caller's own context ended (deadline/cancel) while the batch
			// ran. Record per command, not by the batch error: a context error
			// or a dial cut short (callerEnded) is a client-side signal and
			// records nothing, while any other error (a member read timeout, a
			// reset) is recorded, since member socket I/O does not watch ctx
			// unless ContextTimeoutEnabled is set. On a cluster fan-out the
			// batch error can be one node's cut dial while another node failed
			// on its own. Then release any half-open reservation.
			c.recordBatchOutcomes(ctx, db, cmds, err, executed, res, dg)
			db.cb.ReleaseFor(res)
			pendingDB = nil
			// No retry either way: the caller's ctx is done.
			//
			// Do NOT reset or overwrite the commands. The deadline may have
			// interrupted reading a LATER reply while earlier commands already
			// received definitive results, and those are the caller's only
			// evidence of what applied — an already-executed INCR must not
			// resurface as canceled and prompt a replay. The member pipeline
			// stamped the interrupted/unread commands itself. Unlike the
			// pre-execution gate checks above (which reset because nothing ran,
			// so no result can exist), execution here may have produced
			// results; this mirrors the tx and single-command paths, which also
			// return without touching command state on caller cancellation.
			return exitErr(ctx.Err())
		}
		transportFailures := c.recordBatchOutcomes(ctx, db, cmds, err, executed, res, dg)
		// recordBatchOutcomes settled the reservation (recorded or released).
		// ReleaseFor here is what the defer would do on return: it settles at
		// most once and, after a recorded half-open failure, is state- and
		// generation-gated so it cannot touch the new episode. Then drop the
		// pair from the safety net so a retry's new pair does not orphan it and
		// the defer fires only for a reservation a panic left unsettled.
		db.cb.ReleaseFor(res)
		pendingDB = nil

		if transportFailures == 0 {
			if errors.Is(err, ErrClosed) && db.removed.Load() && !c.closed.Load() {
				// A concurrent control op removed the snapshotted member (closing
				// its client) between the gate and execution, so its pool returned
				// the terminal ErrClosed, classified neutral. The MultiDBClient is
				// still open: surface the retryable ErrTemporarilyNotAvailable
				// instead of a client-closed sentinel, as process() does, and let
				// the caller retry as it would for a transport failure. Nothing is
				// replayed here — a cluster fan-out may have applied one node's
				// commands while another node's connection acquisition hit the
				// removed member, and replaying would duplicate those writes. Only
				// when nothing executed are the per-command stand-in errors rewritten
				// to match the aggregate; a partially executed batch keeps its results.
				if !executed.any() {
					for _, cmd := range cmds {
						if executed.state(cmd) != pipelineAnsweredLocally {
							cmd.SetErr(ErrTemporarilyNotAvailable)
						}
					}
				}
				return exitErr(ErrTemporarilyNotAvailable)
			}
			if err != nil && attempt < attempts && !executed.any() && shouldRetry(err, true) {
				// A pool wait timeout before any server attempt is neutral.
				// Retry only commands that still need a result (#3289).
				if next := unsettledCmds(cmds, executed); len(next) != 0 {
					narrowed = narrowed || len(next) < len(cmds)
					cmds = next
					continue
				}
			}
			// Only server replies (or clean success) — done, whatever the
			// user-level outcome is.
			if attempt > 1 {
				// A retry ran a subset: report the positionally first error
				// over the whole batch, as Pipeline.Exec does.
				if ferr := cmdsFirstErr(orig); ferr != nil {
					err = ferr
				}
			}
			return exitErr(err)
		}
		// Retry only the commands without a definitive result.
		next := unsettledCmds(cmds, executed)
		if len(next) == 0 {
			return cmdsFirstErr(orig)
		}
		// Only a smaller retry set leaves definitive results behind; a whole
		// batch resent (e.g. a standalone drop) keeps the gate error as the
		// result if the retry is gated.
		narrowed = narrowed || len(next) < len(cmds)
		cmds = next
	}
	return cmdsFirstErr(orig)
}

// processTxPipeline executes a MULTI/EXEC pipeline against the active
// database exactly once: EXEC may have committed before a connection broke,
// so a blind retry could double-apply the transaction. Failures are still
// recorded so the breaker and detector can move traffic for subsequent
// operations.
func (c *multidbCore) processTxPipeline(ctx context.Context, cmds []Cmder) error {
	if len(cmds) == 0 {
		return nil
	}
	clearRoutingDecisions(cmds)
	if c.closed.Load() {
		setCmdsErr(cmds, ErrClosed)
		return ErrClosed
	}
	if cmdsContainHImport(cmds) {
		// Reject the whole transaction (MULTI/EXEC is all-or-nothing) and report
		// the positionally-first error over the USER commands, like Pipeline.Exec
		// — not the synthetic MULTI that wrapMultiExec prepends. cmds is always
		// the wrapped [MULTI, user..., EXEC]: TxPipeline().Exec is the only caller
		// and wraps before dispatch, and transactions are never autopipelined, so
		// the user commands are cmds[1:len-1] (the minimum wrapped length is 3).
		// setCmdsErr fills only empty slots, so a user command the caller queued
		// with a pre-existing error keeps it and wins over errMultiDBHImport when
		// it comes first — stamping the fresh MULTI (index 0) instead would let it
		// win every time.
		user := cmds
		if len(cmds) >= 3 {
			user = cmds[1 : len(cmds)-1]
		}
		setCmdsErr(user, errMultiDBHImport)
		return cmdsFirstErr(user)
	}
	// A context that is already done must not reach the failover gate: with
	// ProbeTargetBeforeFailover the doomed probes would damage candidate
	// breakers and advance the escalation state, and without it the active
	// database could be switched for an operation that cannot run anyway.
	if err := ctx.Err(); err != nil {
		setCmdsErr(cmds, err)
		return err
	}
	// The gate loops like the other paths (detector before IsAllowed; a
	// denied member triggers another failover, bounded by the rejection
	// cap) — only the EXECUTION below stays single-shot, so at-most-once
	// holds: no MULTI/EXEC has been sent while the gate is still choosing.
	gateRejections := 0
	var db *multidbDatabase
	var res imultidb.Reservation
	var dg uint64 // detector window sampled with the admitted member
	for {
		if c.closed.Load() {
			resetCmds(cmds)
			setCmdsErr(cmds, ErrClosed)
			return ErrClosed
		}
		if err := ctx.Err(); err != nil {
			resetCmds(cmds)
			setCmdsErr(cmds, err)
			return err
		}
		var idx int
		db, idx = c.activeSnapshot()
		dg = c.detectorGen.Load()
		admitted := false
		if db != nil && !c.shouldFailoverSafely() {
			admitted, res = db.cb.AllowReserve()
		}
		if !admitted {
			gateRejections++
			if gateRejections > c.memberCount()+1 {
				err := ErrTemporarilyNotAvailable
				resetCmds(cmds)
				setCmdsErr(cmds, err)
				return err
			}
			if err := c.tryFailover(ctx, idx); err != nil {
				resetCmds(cmds)
				setCmdsErr(cmds, err)
				return err
			}
			continue
		}
		break
	}

	// Release the admission on any exit that did not settle it: a panic in a
	// member hook or the member path skips both recordBatchOutcomes and the
	// cancel branch. ReleaseFor settles at most once and no-ops for a closed
	// admission — the same defer Watch uses.
	defer db.cb.ReleaseFor(res)
	// Record outcomes only for the user's commands: cmds arrives wrapped by
	// wrapMultiExec (MULTI ... EXEC), and counting the synthetic envelope
	// would advance the breaker by three per single-command transaction.
	user := cmds
	if len(cmds) >= 3 {
		user = cmds[1 : len(cmds)-1]
	}
	// Clear any error a caller left on a reused command before executing, so a
	// transaction that aborts before execution (e.g. a connection-acquire
	// failure) is recorded as a transport failure instead of being masked by
	// stale per-command errors setCmdsErr refuses to overwrite. Only the user
	// slice can carry caller staleness; the MULTI/EXEC envelope is built fresh
	// per call.
	resetCmds(user)
	// The execution marker rides on the context (see processPipeline and
	// AddDatabaseHook): a member hook must pass the received ctx to next.
	executed := newExecutedCmds(len(cmds))
	err := db.processTxPipelineHook(context.WithValue(ctx, pipelineExecutedKey{}, executed), cmds)
	if err != nil && ctx.Err() != nil {
		// Caller's context ended mid-transaction (see processPipeline): a
		// context error, a cut-short dial, or (ContextTimeoutEnabled) a socket
		// timeout is a client-side signal and is not recorded; any other member
		// error is, per command (see callerEnded). Do NOT reset the commands
		// here — EXEC may have committed, and their state is the caller's only
		// evidence of that. Return the cancellation as the aggregate error (like
		// processPipeline) rather than the stale transport error, so a caller
		// does not mistake a deliberate cancel for a retryable disconnect.
		// Record per command even when the aggregate error is caller-ended:
		// EXEC may have committed, and those results are health evidence.
		c.recordBatchOutcomes(ctx, db, user, err, executed, res, dg)
		db.cb.ReleaseFor(res)
		return ctx.Err()
	}
	c.recordBatchOutcomes(ctx, db, user, err, executed, res, dg)
	if errors.Is(err, ErrClosed) && db.removed.Load() && !c.closed.Load() {
		// A concurrent control op removed the snapshotted member (closing its
		// client) between the gate and execution, so its pool returned the
		// terminal ErrClosed though the MultiDBClient is still open. Surface the
		// retryable ErrTemporarilyNotAvailable instead of a client-closed
		// sentinel, as process() does. Nothing is replayed here — EXEC may have
		// committed, so at-most-once forbids it — and recordBatchOutcomes
		// recorded no health outcome for the neutral ErrClosed. Only when nothing
		// executed are the per-command stand-in errors rewritten to match the
		// aggregate; a partially executed transaction keeps its evidence.
		if !executed.any() {
			resetCmds(user)
			setCmdsErr(user, ErrTemporarilyNotAvailable)
		}
		return ErrTemporarilyNotAvailable
	}
	return err
}

func (db *multidbDatabase) processPipelineHook(ctx context.Context, cmds []Cmder) error {
	if db.cc != nil {
		return db.cc.processPipelineHook(ctx, cmds)
	}
	return db.c.processPipelineHook(ctx, cmds)
}

func (db *multidbDatabase) processTxPipelineHook(ctx context.Context, cmds []Cmder) error {
	if db.cc != nil {
		return db.cc.processTxPipelineHook(ctx, cmds)
	}
	return db.c.processTxPipelineHook(ctx, cmds)
}

// process / processPipeline are the unhooked base entry points required by
// the autopipeliner's backend interface (cmdableClient); the hook-wrapped
// variants come from hooksMixin.
func (c *MultiDBClient) process(ctx context.Context, cmd Cmder) error {
	return c.core.process(ctx, cmd)
}

// hookCount reports one more hook than are installed on the MultiDBClient
// itself: member clients can carry their own hooks (AddDatabaseHook), which
// the autopipeliner cannot see, and its async dispatcher only arms the
// batch-executor self-deadlock guards when hookCount is non-zero. Always
// reporting at least one keeps those guards armed for member-level hooks
// that read command results.
func (c *MultiDBClient) hookCount() int {
	return c.hooksMixin.hookCount() + 1
}

func (c *MultiDBClient) processPipeline(ctx context.Context, cmds []Cmder) error {
	return c.core.processPipeline(ctx, cmds)
}

// Do creates a Cmd from the args and routes it through Process.
func (c *MultiDBClient) Do(ctx context.Context, args ...interface{}) *Cmd {
	cmd := NewCmd(ctx, args...)
	_ = c.Process(ctx, cmd)
	return cmd
}

// Pipeline returns a pipeline whose Exec routes the whole batch to the active
// database, with failover and retry semantics (see the MultiDB design).
func (c *MultiDBClient) Pipeline() Pipeliner {
	pipe := Pipeline{
		exec: pipelineExecer(c.processPipelineHook),
	}
	pipe.init()
	return &pipe
}

// Pipelined executes fn inside a Pipeline and returns the queued commands.
func (c *MultiDBClient) Pipelined(ctx context.Context, fn func(Pipeliner) error) ([]Cmder, error) {
	return c.Pipeline().Pipelined(ctx, fn)
}

// TxPipeline returns a MULTI/EXEC pipeline against the active database.
// Transactions are executed at most once: they are never automatically
// retried on another database after a failure.
func (c *MultiDBClient) TxPipeline() Pipeliner {
	pipe := Pipeline{
		exec: func(ctx context.Context, cmds []Cmder) error {
			cmds = wrapMultiExec(ctx, cmds)
			// Suppress the member client's own retry loop for the wrapped
			// batch (cmdsContainNoRetry): EXEC may have committed before a
			// transport error surfaced, and at-most-once must hold for the
			// whole stack, not only the MultiDB retry layer. Every command
			// is marked — cluster members trim the MULTI/EXEC envelope
			// before their retry check, so a marker only on the synthetic
			// MULTI would be lost there. The prior NoRetry is restored after
			// execution: cmds are caller-owned and may be reused, and a plain
			// client's TxPipeline does not leave them permanently non-retryable.
			// setNoRetry is part of Cmder, so a caller's decorator that embeds
			// a Cmder is marked too (through the embedded command); a type
			// assertion here would skip such a command and leave a cluster
			// member's retry check with no NoRetry marker at all.
			prev := make([]bool, len(cmds))
			for i, cmd := range cmds {
				prev[i] = cmd.NoRetry()
				cmd.setNoRetry(true)
			}
			// Restore via defer so a panic in the hook or member path still
			// clears the temporary noRetry on the caller-owned commands. Restore
			// in REVERSE order: if the same Cmder was queued twice, its second
			// occurrence snapshotted prev=true (already set by the first), so a
			// forward restore would end on that value and leave the shared command
			// permanently non-retryable. Reversing makes the first occurrence's
			// genuine original state the last write.
			defer func() {
				for i := len(cmds) - 1; i >= 0; i-- {
					cmds[i].setNoRetry(prev[i])
				}
			}()
			return c.processTxPipelineHook(ctx, cmds)
		},
	}
	pipe.init()
	return &pipe
}

// TxPipelined executes fn inside a TxPipeline.
func (c *MultiDBClient) TxPipelined(ctx context.Context, fn func(Pipeliner) error) ([]Cmder, error) {
	return c.TxPipeline().Pipelined(ctx, fn)
}

// Watch runs a WATCH/MULTI/EXEC transaction on the database that is active
// when Watch is called; if the active database's circuit is open or the
// failure detector has tripped, a failover is attempted first so the
// transaction does not start on a known-unhealthy member. The transaction is
// then bound to that member for its whole lifetime: it does NOT follow a
// MultiDB failover, its outcome does not feed the breaker or detector, and it
// is never automatically retried on another database. If the bound member
// fails while the transaction is open, the transaction errors like it would
// on a plain client; MultiDB moves only subsequent operations.
//
// MultiDB-level hooks (AddHook on MultiDBClient) do not wrap the transaction:
// the Tx runs on the member client, so install member-level hooks via
// AddDatabaseHook when WATCH traffic must be instrumented.
func (c *MultiDBClient) Watch(ctx context.Context, fn func(*Tx) error, keys ...string) error {
	if c.core.closed.Load() {
		return ErrClosed
	}
	// A done context must not reach the failover gate (see processTxPipeline).
	if err := ctx.Err(); err != nil {
		return err
	}
	// The gate loops like the batch paths (detector before the breaker
	// admission; a denied member triggers another failover, bounded by the
	// rejection cap), so a half-open candidate with an exhausted probe budget
	// does not fail the call while a healthy member is still selectable — no
	// WATCH has been sent while the gate is still choosing. A half-open
	// admission reserves a probe slot, so MaxHalfOpenRequests bounds
	// concurrent WATCH transactions too; the slot is released after the call
	// because the WATCH outcome deliberately never records on the breaker.
	gateRejections := 0
	var db *multidbDatabase
	var res imultidb.Reservation
	for {
		if c.core.closed.Load() {
			return ErrClosed
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var idx int
		db, idx = c.core.activeSnapshot()
		admitted := false
		if db != nil && !c.core.shouldFailoverSafely() {
			admitted, res = db.cb.AllowReserve()
		}
		if !admitted {
			gateRejections++
			if gateRejections > c.core.memberCount()+1 {
				return ErrTemporarilyNotAvailable
			}
			if err := c.core.tryFailover(ctx, idx); err != nil {
				return err
			}
			continue
		}
		break
	}
	// Release the reservation after the call: the WATCH outcome deliberately
	// never records on the breaker. ReleaseFor settles once and only within the
	// reservation's own half-open episode — so a closed-state admission (no
	// slot) is a no-op, and a transaction that outlives an open -> half-open
	// cycle cannot free a slot a real recovery probe is holding in the new one.
	defer db.cb.ReleaseFor(res)
	var err error
	if db.cc != nil {
		err = db.cc.Watch(ctx, fn, keys...)
	} else {
		err = db.c.Watch(ctx, fn, keys...)
	}
	// A member removed by a concurrent control op mid-transaction has a closed
	// client that returns ErrClosed although the MultiDBClient is still open:
	// surface the retryable error instead, as the command, pipeline and Pub/Sub
	// paths do. The transaction is NOT replayed — fn may already have run
	// against the old member. Close() marks every member removed, so the
	// closed check keeps the terminal ErrClosed for a client that shut down.
	if errors.Is(err, ErrClosed) && db.removed.Load() && !c.core.closed.Load() {
		return ErrTemporarilyNotAvailable
	}
	return err
}

// SSubscribe subscribes to shard channels on the database that is active at
// call time. Unlike Subscribe/PSubscribe, sharded subscriptions do not follow
// the active database across failovers yet. Because the subscription stays
// pinned, the active is admitted first like a command: an active whose
// breaker is open or whose failure detector has tripped is failed over
// before the subscription binds, so it is not pinned to a member already
// known to be unhealthy.
func (c *MultiDBClient) SSubscribe(ctx context.Context, channels ...string) *PubSub {
	// The closed check keeps a Close race from delegating to a member that
	// is being torn down: the MultiDB PubSub path below fails dials with
	// the terminal ErrClosed instead.
	if !c.core.closed.Load() {
		// A gate failure (the active is rejected and nothing can replace it)
		// falls through to the follower below rather than pinning the
		// subscription to the rejected member: the follower re-dials on the
		// next active change, so it recovers when a member does. The follower
		// dials standalone members only, though. With no standalone member
		// at all, it would fail terminally, so an all-cluster deployment binds
		// to the active cluster member instead: rejected now, but the
		// subscription recovers with it, which a terminal failure never does.
		db, err := c.activeAfterGate(ctx)
		if err == nil && db != nil && db.cc != nil {
			return db.cc.SSubscribe(ctx, channels...)
		}
		if err == nil && db != nil {
			return db.c.SSubscribe(ctx, channels...)
		}
		if err != nil && !c.core.hasStandaloneMember() {
			if cur, _ := c.core.activeSnapshot(); cur != nil && cur.cc != nil {
				return cur.cc.SSubscribe(ctx, channels...)
			}
		}
	}
	// No active database (or closed, or gate failure): return a PubSub that
	// fails to connect — with the requested channels registered, like
	// Subscribe and PSubscribe, so the failure surfaces on use instead of
	// silently subscribing to nothing.
	pubsub := c.core.newPubSub()
	if len(channels) > 0 {
		_ = pubsub.SSubscribe(ctx, channels...)
	}
	return pubsub
}

// PoolStats returns the connection pool statistics of the active database.
func (c *MultiDBClient) PoolStats() *PoolStats {
	db, _ := c.core.activeSnapshot()
	if db == nil {
		return &PoolStats{}
	}
	if db.cc != nil {
		return db.cc.PoolStats()
	}
	return db.c.PoolStats()
}

// canPipeline reports whether cmd may join a pipeline batch on this client. It
// asks each cluster member and answers false if any of them refuses; standalone
// members impose no restriction, so a deployment with none answers true without
// resolving a single policy.
//
// It asks EVERY cluster member rather than only the active one on purpose. The
// active member can change between this call and the batch's flush, and a
// command judged pipelineable against a standalone member would then reach a
// cluster member inside a merged batch — where one unfit command fails the whole
// mapping, and with it the unrelated callers sharing that batch.
//
// The cluster members are copied under dbMu and asked with it released: a
// member can wait for its first command-metadata fetch here, and holding dbMu
// across that would stall every control operation and later submit behind
// it. A member removed meanwhile is still safe to ask: its closed metadata
// store falls back to the static view.
func (c *MultiDBClient) canPipeline(ctx context.Context, cmd Cmder) bool {
	c.core.dbMu.RLock()
	var buf [4]*ClusterClient // no allocation per submit for a few members
	clusters := buf[:0]
	for _, db := range c.core.dbs {
		if db.cc != nil {
			clusters = append(clusters, db.cc)
		}
	}
	c.core.dbMu.RUnlock()
	for _, cc := range clusters {
		if !cc.canPipeline(ctx, cmd) {
			return false
		}
	}
	return true
}

// AutoPipeline returns the blocking autopipeliner for this MultiDB client:
// batches are flushed against whichever database is active at exec time, and
// a batch that fails at transport level is retried against the newly selected
// database (bounded by CommandRetries). The instance is cached and shared; the
// first call's config wins.
//
// Cluster member databases are supported. A command that cannot ride a pipeline
// on a cluster member — a fan-out request policy, or routing that is not derived
// from the slot — is executed on its own connection instead of joining a batch
// (see canPipeline). Such a command keeps full MultiDB semantics, but not its
// position relative to the batch around it.
//
// The check runs when a command is accepted, against the members of that
// moment. A cluster member added with AddDatabase afterwards is not applied
// to commands already queued: see AddDatabase for the window and how to
// avoid it.
//
// AutoPipelineOptions.FullDuplex is supported (ordered, single engine:
// NumShards and FullDuplexPipes are forced to 1): each command streams on a held connection of the
// ACTIVE member — a full-duplex engine on a standalone member, the cluster
// full-duplex router on a cluster member. A command rides it only while the
// member's breaker is closed and the failure detector has not tripped;
// otherwise it runs through MultiDB's normal command path, which owns failover
// and half-open probes. Outcomes still feed the breaker and the detector, and
// work in flight on a member that drops is re-issued through MultiDB to the
// newly active member (bounded by CommandRetries; a command that may have
// reached the server and is NoRetry is failed instead). Differences from the
// half-duplex path:
//   - On the async face, submit order still holds across a failover or a
//     breaker transition: the switch to the new member waits until the
//     commands already accepted for the old one have completed, so submitters
//     stall briefly at that point. As on any autopipeliner, submit order means
//     a command submitted after another's submit returned runs after it;
//     submits that overlap (concurrent callers) have no order between them. On a cluster member, order holds per key
//     (slot), as with full duplex on a plain ClusterClient: keyless commands
//     and routing misses (topology not loaded, a node without full duplex)
//     run on MultiDB's normal path and are not ordered against the lane.
//   - Process hooks added with MultiDBClient.AddHook do not run on the
//     full-duplex path. A standalone member's process hooks
//     (AddDatabaseHook) do. On a cluster member, AddDatabaseHook installs
//     the hook on the *ClusterClient, which the full-duplex path bypasses:
//     only the node clients' hooks run, as with full duplex on a plain
//     ClusterClient. Register such hooks per node with
//     ClusterOptions.OnNewNode in the member's config.
//   - Pipeline() and TxPipeline() keep using MultiDB's pipeline path.
//   - The breaker episode and detector window an outcome counts toward are
//     the ones current when the reply lands.
//   - Config().FullDuplex reports that the full-duplex router is installed,
//     not that every member runs full duplex. A member whose own config
//     cannot host it (PipelinePoolSize < 0; a cluster member with ReadOnly,
//     RouteByLatency or RouteRandomly; a child build error) always takes
//     MultiDB's half-duplex path. Any member does so for a while when there
//     is no active member, a failover is pending, or its breaker is open or
//     half-open.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
func (c *MultiDBClient) AutoPipeline() (*AutoPipeliner, error) {
	return c.AutoPipelineWithOptions(nil)
}

// AutoPipelineWithOptions is AutoPipeline with explicit options.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
func (c *MultiDBClient) AutoPipelineWithOptions(config *AutoPipelineOptions) (*AutoPipeliner, error) {
	return getOrCreateAutoPipeliner(c.autopipelinerMu, &c.autopipeliner, &c.autopipelinerClosed, nil, nil, "", config,
		DefaultBlockingAutoPipelineOptions,
		func(cfg *AutoPipelineOptions) (*AutoPipeliner, error) {
			return c.trackAutopipelinerLocked(c.newMultiDBAutoPipeliner(cfg, true))
		})
}

// newMultiDBAutoPipeliner builds an instance over this client and installs the
// only MultiDB-specific wiring it needs: keep commands that cannot ride a
// pipeline out of merged batches. Everything else — routing to the active
// member, the failover gate, breaker and detector accounting, retries — comes
// from dispatching through this client's own pipeline path.
func (c *MultiDBClient) newMultiDBAutoPipeliner(cfg *AutoPipelineOptions, blocking bool) (*AutoPipeliner, error) {
	ap, err := newAutoPipeliner(c, cfg, blocking)
	if err != nil {
		return nil, err
	}
	// Divert rather than reject. The cluster pipeline path fails the WHOLE
	// mapping on an unfit command, which in a merged batch would fail the
	// unrelated callers sharing it. Diverted commands run through
	// MultiDBClient.Process, so a fan-out command still fans out on the active
	// member and still passes the gate.
	ap.routeCommand = func(ctx context.Context, cmd Cmder) (bool, error) {
		return !c.canPipeline(ctx, cmd), nil
	}
	return ap, nil
}

// trackAutopipelinerLocked records a freshly built autopipeliner so Close can
// wait for its drain even after it leaves the cache, and drops the entries
// whose drains have already finished. It passes the build result through so a
// build func can wrap its constructor call directly. autopipelinerMu MUST be
// held: the build funcs run under it (see builtAutopipeliners).
func (c *MultiDBClient) trackAutopipelinerLocked(ap *AutoPipeliner, err error) (*AutoPipeliner, error) {
	if err != nil {
		return nil, err
	}
	kept := c.builtAutopipeliners[:0]
	for _, p := range c.builtAutopipeliners {
		if p != nil && !p.drained() {
			kept = append(kept, p)
		}
	}
	// Drop the references left behind the new length: the backing array is
	// reused, and drained instances would otherwise stay reachable through
	// it until overwritten.
	clear(c.builtAutopipeliners[len(kept):])
	c.builtAutopipeliners = append(kept, ap)
	return ap, nil
}

// AsyncAutoPipeline returns the deferred autopipeliner for this MultiDB
// client (see AutoPipeline for the failover semantics).
//
// EXPERIMENTAL: this API is subject to change, use with caution.
func (c *MultiDBClient) AsyncAutoPipeline() (*AutoPipeliner, error) {
	return c.AsyncAutoPipelineWithOptions(nil)
}

// AsyncAutoPipelineWithOptions is AsyncAutoPipeline with explicit options.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
func (c *MultiDBClient) AsyncAutoPipelineWithOptions(config *AutoPipelineOptions) (*AutoPipeliner, error) {
	return getOrCreateAutoPipeliner(c.autopipelinerMu, &c.asyncAutopipeliner, &c.autopipelinerClosed, nil, nil, "", config,
		DefaultAutoPipelineOptions,
		func(cfg *AutoPipelineOptions) (*AutoPipeliner, error) {
			return c.trackAutopipelinerLocked(c.newMultiDBAutoPipeliner(cfg, false))
		})
}

// unsettledCmds returns, in order, the commands of a failed batch attempt that
// a retry must send again: those that never executed, and those whose outcome
// is a member failure (a transport error, or a retryable reply such as LOADING
// or READONLY, which the server did not apply). A command with a reply (nil
// error or any other Redis error reply) keeps its result and is not resent, so
// a cluster member's healthy-node writes do not run twice. Commands answered
// by a node-level hook likewise keep their completed results. A standalone member
// stamps a mid-pipeline drop on every command of the batch, replied or not, so
// there the whole batch is resent, as before.
//
// Followers carrying a blocking command's neutral deadline are not resent,
// matching recordBatchOutcomes (see batchOutcomeFunc).
func unsettledCmds(cmds []Cmder, executed *executedCmds) []Cmder {
	outcome := batchOutcomeFunc(cmds)
	out := make([]Cmder, 0, len(cmds))
	for _, cmd := range cmds {
		if executed.state(cmd) == pipelinePending || outcome(cmd) == outcomeFailure {
			out = append(out, cmd)
		}
	}
	return out
}

// batchOutcomeFunc returns the per-command outcome classifier of a batch: a
// command carrying the same error as a blocking command's neutral deadline
// that first carried it (the reader's propagation) is neutral too; anything
// else is classified on its own read timeout.
func batchOutcomeFunc(cmds []Cmder) func(Cmder) outcomeKind {
	var neutralPropagated []error
	var seen []error
	for _, cmd := range cmds {
		err := cmd.rawErr()
		if err == nil || isRedisError(err) {
			continue
		}
		known := false
		for _, s := range seen {
			if sameErr(err, s) {
				known = true
				break
			}
		}
		if known {
			continue
		}
		seen = append(seen, err)
		if cmd.readTimeout() != nil && classifyOutcome(err, false) == outcomeNeutral {
			neutralPropagated = append(neutralPropagated, err)
		}
	}
	return func(cmd Cmder) outcomeKind {
		err := cmd.rawErr()
		for _, origin := range neutralPropagated {
			if sameErr(err, origin) {
				return outcomeNeutral
			}
		}
		return classifyOutcome(err, cmd.readTimeout() == nil)
	}
}
