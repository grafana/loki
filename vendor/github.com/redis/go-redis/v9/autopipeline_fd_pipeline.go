package redis

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9/internal/otel"
	"github.com/redis/go-redis/v9/internal/pool"
)

// Batch submission on the full-duplex wire.
//
// WHY THIS EXISTS. AutoPipeliner.Pipeline() delegates to the underlying
// client, so a pipeline runs through processPipeline on a POOLED connection and
// never touches the full-duplex engine — the engine's only entry point is
// submit(), which takes one Cmder. That leaves go-redis without the shape
// rueidis offers as DoMulti: hand over N commands at once, block until every
// reply lands, on ONE multiplexed connection, spending no extra goroutines.
//
// The submit queue already has the primitive for it. fdQueue.pushBatch appends
// a whole slice under one lock, and because the queue is FIFO and the writer can
// only take contiguous prefixes, the commands stay ADJACENT on the wire — no
// other caller's command can land between them. That is a stronger guarantee
// than issuing N single commands gives, and it is what makes this a pipeline
// rather than a loop.
//
// Each command still carries its OWN completion batch. One shared batch would
// complete on the first reply, since the reader closes the batch per command, so
// the caller would return before the rest had landed.

// ErrFDPipelineUnavailable is returned when batch submission is requested on an
// autopipeliner that has no full-duplex engine. It is deliberately an error
// rather than a silent fallback to the pooled pipeline path: a caller asking for
// this wants the commands on the FD wire, and quietly running them somewhere
// else would be measured (or relied upon) as something it is not.
//
// Batch submission is STANDALONE ONLY. A full-duplex autopipeliner on a
// ClusterClient reports Config().FullDuplex true but runs one engine per node,
// and a pipeline can span nodes, so it gets this error and its Pipeline() keeps
// using the cluster pipeline, as it did before FD batches existed.
var ErrFDPipelineUnavailable = errors.New(
	"redis: FDPipelined requires a full-duplex autopipeliner on a standalone Client")

// ErrFDPipelineDiverts is returned when a batch contains a command the engine
// cannot stream (blocking, per-command read timeout, runs-outside-pipeline,
// HIMPORT, or one the caller's own mustDivert rejects). Such a command has to
// leave the pipe, which would break the batch's contiguity, so the whole call is
// refused instead of being silently split. A NoRetry command (e.g. RawWriteTo)
// is refused too: it makes the batch unreplayable as a unit, which the pipe's
// per-command recovery cannot honor.
var ErrFDPipelineDiverts = errors.New(
	"redis: FDPipelined got a command that must be diverted off the full-duplex pipe")

// ErrFDPipelineSpansEngines is returned when NumShards > 1 runs several
// full-duplex engines and the batch's keyed commands hash to different ones.
// The batch can only ride one wire, and keeping per-first-key order needs each
// command on its own key's engine, so it is refused rather than split.
// Pipeline() falls back to an ordinary pipeline for such a batch.
var ErrFDPipelineSpansEngines = errors.New(
	"redis: FDPipelined batch has keys on different full-duplex engines")

// FDPipelined submits cmds as one contiguous batch on the full-duplex
// connection and blocks until every reply has landed. It returns the first
// command error, Nil included, the same as Pipeline.Exec.
//
// Two differences from an ordinary pipeline. Retryable replies (LOADING and
// the like) are not retried here; Pipeline() retries the whole batch on top of
// this, see fdPipelineExec. And once the batch is admitted, ctx no longer bounds
// the wait: the commands share the held connection with other callers, so they
// cannot be abandoned mid-stream. ReadTimeout still bounds each reply.
//
// Compared with the two shapes that already exist:
//
//   - Pipeline().Exec() batches up front and blocks, but on a pooled
//     connection: a separate socket, its own round trip, no FD engine.
//   - Submit()/the async face keeps N commands in flight on the FD wire with
//     individual completion, but the caller issues them one at a time and they
//     may interleave with other callers' commands.
//
// This is the third: batched up front, contiguous, blocking, on the FD wire.
//
// The commands are still completed individually by the reader, so after this
// returns each cmd carries its own result and error.
//
// Hooks and metrics match an ordinary pipeline: the client's
// ProcessPipelineHook chain runs around the batch (per-command ProcessHooks do
// not, as in an ordinary pipeline), and an OTel recorder gets one pipeline
// duration and, on failure, one pipeline-level error rather than per-command
// metrics. A Limiter is the exception: the FD writer asks it once per write
// chunk, and a chunk can mix this batch with other callers' commands, so a
// batch longer than MaxBatchSize (or MaxBatchBytes) can be partly admitted.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
func (ap *AutoPipeliner) FDPipelined(ctx context.Context, cmds []Cmder) error {
	if ap.fd == nil {
		return ErrFDPipelineUnavailable
	}
	return ap.fd.client.wrapPipelineHooks(func(ctx context.Context, cmds []Cmder) error {
		var start time.Time
		if otel.GetPipelineOperationDurationCallback() != nil {
			start = time.Now()
		}
		res, err := ap.fdPipelined(ctx, cmds)
		if res.measured() {
			ap.fdPipelineMetrics(ctx, start, cmds, max(res.attempts, 1), res.cn)
		}
		return err
	})(ctx, cmds)
}

// wrapPipelineHooks folds the client's current hooks around next, in the same
// order the hook state builds its pipeline chain, so next runs exactly where
// processPipeline runs in an ordinary pipeline. It reads the live hook
// snapshot, so a hook added after the autopipeliner was built is honored.
func (hs *hooksMixin) wrapPipelineHooks(next ProcessPipelineHook) ProcessPipelineHook {
	st := hs.state.Load()
	for i := len(st.slice) - 1; i >= 0; i-- {
		if wrapped := st.slice[i].ProcessPipelineHook(next); wrapped != nil {
			next = wrapped
		}
	}
	return next
}

// fdPipelineMetrics records an FD batch the way generalProcessPipeline records
// an ordinary pipeline: one duration with the command count and attempts, and
// one error when the pipeline as a whole failed. The reader skips its
// per-command metrics for pipelined commands, so nothing is counted twice.
//
// attempts counts the times the batch was issued. One difference from an
// ordinary pipeline: connection-lease retries are not counted. The lease
// belongs to the engine, not to the batch; a batch queued while the engine
// retries a lease waits and is issued once. So a batch issued after lease
// retries reports 1 attempt, and a batch failed because the lease retries ran
// out reports 0 retries.
func (ap *AutoPipeliner) fdPipelineMetrics(ctx context.Context, start time.Time, cmds []Cmder, attempts int, cn *pool.Conn) {
	perr := fdPipelineLevelErr(cmds)
	db := ap.fd.client.opt.DB
	ap.fd.emitMetricsGuarded(ctx, func() {
		// A zero start means no duration callback existed when the operation
		// began (a recorder installed mid-flight). Record no duration then, as
		// an ordinary pipeline does, rather than time.Since(time.Time{}).
		if cb := otel.GetPipelineOperationDurationCallback(); cb != nil && !start.IsZero() {
			cb(ctx, time.Since(start), "PIPELINE", len(cmds), attempts, perr, cn, db)
		}
		if perr != nil {
			if errorCallback := pool.GetMetricErrorCallback(); errorCallback != nil {
				errorType, statusCode, isInternal := classifyCommandErrorGuarded(perr)
				errorCallback(ctx, errorType, cn, statusCode, isInternal, attempts-1)
			}
		}
	})
}

// fdPipelineLevelErr is the error an ordinary pipeline would report for the
// batch: the first transport (non-Redis) error, else the first command's reply
// error. pipelineReadCmds returns exactly that.
func fdPipelineLevelErr(cmds []Cmder) error {
	if err := fdPipelineTransportErr(cmds); err != nil {
		return err
	}
	if len(cmds) > 0 {
		return cmds[0].rawErr()
	}
	return nil
}

// fdPipelineTransportErr returns the first error in cmds that is not a reply
// from the server: a connection, protocol or push-drain failure, classified
// as the reader classifies a fatal reply (fdReplyIsFatal). nil when every
// command got a reply.
func fdPipelineTransportErr(cmds []Cmder) error {
	for _, cmd := range cmds {
		if e := cmd.rawErr(); e != nil && (errors.Is(e, errFDPushDrainFailed) || !isRedisError(e)) {
			return e
		}
	}
	return nil
}

// fdPipeResult is what fdPipelined reports about a batch it ran.
type fdPipeResult struct {
	// attempts is how many times the engine issued the batch: 1, plus one per
	// connection-error replay (the highest across its commands); 0 when nothing
	// was admitted.
	attempts int
	// cn is the held connection its replies came back on.
	cn *pool.Conn
	// flushed reports that the Close-time flush ran every batch of it through
	// the pooled pipeline, which already recorded the pipeline metric.
	flushed bool
	// rejected reports that it was refused before admission (closed, or ctx
	// done), so nothing ran.
	rejected bool
}

// measured reports whether the caller records the pipeline metric: for a
// batch that ran on the FD wire, or one refused before admission (an
// ordinary pipeline records that case when withPipelineConn fails), but not
// for one the Close-time flush measured in full.
func (r fdPipeResult) measured() bool {
	return !r.flushed && (r.attempts > 0 || r.rejected)
}

// fdPipelineAllFlushed reports whether the Close-time flush ran every batch of
// a pipeline. Only then did the pooled flush record the whole pipeline. When
// it ran only an unread tail, it recorded that tail, and the FD metric still
// covers the whole operation.
func fdPipelineAllFlushed(batches []*apBatch) bool {
	if len(batches) == 0 {
		return false
	}
	for _, b := range batches {
		if !b.fdFlushed {
			return false
		}
	}
	return true
}

// fdPipelined runs an eligible batch on the FD path.
func (ap *AutoPipeliner) fdPipelined(ctx context.Context, cmds []Cmder) (fdPipeResult, error) {
	if len(cmds) == 0 {
		return fdPipeResult{}, nil
	}
	// A redirect-aware engine is a cluster node child. It is reachable (the
	// child is the node client's cached async autopipeliner, which
	// ForEachMaster hands out), but it follows MOVED/ASK by re-running ONE
	// command off the pipe, which would let the rest of the pipeline run ahead
	// of it. Such a batch keeps the node client's ordinary pipeline, as before.
	if ap.fd == nil || ap.fd.redirectAware {
		return fdPipeResult{}, ErrFDPipelineUnavailable
	}
	// Refuse a batch containing anything that would leave the pipe. Checked for
	// EVERY command before anything is submitted, so the call either goes as one
	// contiguous unit or not at all — a half-submitted batch would have the
	// diverted command complete out of order relative to its neighbours.
	for _, cmd := range cmds {
		if cmd == nil {
			return fdPipeResult{}, ErrFDPipelineDiverts
		}
		if cmd.readTimeout() != nil || runsOutsidePipeline(cmd.Name()) ||
			isBlockingCmd(cmd) || isHImportCmd(cmd) ||
			(ap.mustDivert != nil && ap.mustDivert(ctx, cmd)) {
			return fdPipeResult{}, ErrFDPipelineDiverts
		}
		// A NoRetry command makes the whole batch unreplayable: an ordinary
		// pipeline holding one retries nothing after a connection error. The FD
		// tail recovery replays per command, up to the first sent NoRetry one,
		// so the commands before it could run twice. Keep such a batch off the
		// pipe.
		if fdNoRetrySafe(cmd) {
			return fdPipeResult{}, ErrFDPipelineDiverts
		}
	}
	// The WHOLE batch goes to ONE engine: a pipeline split across engines would
	// lose its internal order, which is the one thing a pipeline guarantees. That
	// engine must also be the one every keyed command hashes to, or a command
	// could run ahead of an earlier, unawaited command for the same key on that
	// key's engine. A batch that spans engines is refused.
	e, ok := ap.fdForBatch(cmds)
	if !ok {
		return fdPipeResult{}, ErrFDPipelineSpansEngines
	}
	batches, err := e.submitBatch(ctx, cmds)
	if err != nil {
		return fdPipeResult{}, err
	}
	if batches == nil {
		// Submit-time rejection (closed, or ctx expired while backpressured).
		// Every command carries its own error; report the first.
		return fdPipeResult{rejected: true}, cmdsFirstErr(cmds)
	}
	// Mirror submit()'s contract on the deferred face: the batch is installed on
	// the command so its result accessors self-gate, which matters for a caller
	// that keeps the Cmder after this returns.
	if !ap.blocking {
		for i, cmd := range cmds {
			cmd.setReady(batches[i])
		}
	}
	var first error
	res := fdPipeResult{attempts: 1}
	for i, cmd := range cmds {
		// AutoFuture.Wait carries the executor-goroutine self-deadlock guard, so
		// waiting through it rather than on batch.done keeps a pipeline hook that
		// calls this from behaving differently than it does elsewhere.
		if werr := (AutoFuture{cmd: cmd, batch: batches[i]}).Wait(); werr != nil && first == nil {
			first = werr
		}
		// Wait returned, so the reader's stamp is visible.
		if a := batches[i].fdAttempts; a > res.attempts {
			res.attempts = a
		}
		if c := batches[i].fdConn; c != nil {
			res.cn = c
		}
	}
	res.flushed = fdPipelineAllFlushed(batches)
	// A transport failure comes first, as in processPipeline: it tells the
	// caller the later commands have no known result.
	if err := fdPipelineTransportErr(cmds); err != nil {
		return res, err
	}
	return res, first
}

// submitBatch enqueues cmds as one contiguous run and returns their completion
// batches, one per command. A nil slice with a nil error means submit-time
// rejection, with each command's own error already set (same convention as
// submit()'s completedBatch return).
//
// Admission is all-or-nothing: pushBatch either takes the whole slice or takes
// none of it, so the run cannot be split across two waves by a queue that fills
// halfway through.
//
// A refused batch holds its slots while it waits (fdQueue.hold). Without that a
// batch longer than the writer's wave could starve: each take frees at most a
// wave, single submitters refill any free slot, and the batch never sees its
// whole length free at once. With the reservation the freed room accumulates
// for the batch, and the singles resume once it is admitted (or gives up).
// Holders are admitted in arrival order: a later, smaller batch that could be
// admitted with the room the head is waiting for would otherwise starve the
// head.
func (fd *fdEngine) submitBatch(ctx context.Context, cmds []Cmder) ([]*apBatch, error) {
	// A batch larger than the queue itself can NEVER be admitted, and the
	// room-wait loop below would spin forever waiting for space that cannot
	// exist. Report it instead of hanging: the caller can split, or raise
	// FullDuplexWindow.
	if n := fd.q.capacity(); n > 0 && len(cmds) > n {
		return nil, ErrFDPipelineTooLarge
	}
	if fd.ap.isClosed() {
		setCmdsErr(cmds, ErrClosed)
		return nil, nil
	}
	// No per-command ProcessHooks here: the batch runs inside the client's
	// ProcessPipelineHook chain (FDPipelined, fdPipelineHooked), and an ordinary
	// pipeline runs only that chain, not a ProcessHook per command.
	reqs := make([]fdReq, len(cmds))
	batches := make([]*apBatch, len(cmds))
	for i, cmd := range cmds {
		b := newAPBatch() // never pooled: pooled batches are the blocking face's single-waiter signal
		b.fdAttempts = 1  // the first issue; replays and the reader raise it
		b.fdGroup = batches[0]
		if i == 0 {
			b.fdGroup = b
		}
		batches[i] = b
		reqs[i] = fdReq{cmd: cmd, batch: b, ctx: ctx, attempts: 1, pipelined: true}
	}

	// held: this batch holds len(reqs) reserved slots under id and waits on
	// wake, which the queue signals only while this batch is the head of the
	// holder line (and on close). Every exit that does not admit the batch must
	// give the slots back, after the RLock is dropped.
	held, id := false, uint64(0)
	var wake <-chan struct{}
	reject := func(err error) ([]*apBatch, error) {
		fd.submitMu.RUnlock()
		if held {
			fd.q.unhold(id)
		}
		setCmdsErr(cmds, err)
		return nil, nil
	}
	fd.submitMu.RLock()
	for {
		if fd.closed {
			return reject(ErrClosed)
		}
		// A done ctx is refused before admission, not only while waiting for
		// room: once admitted the batch runs, and an ordinary pipeline rejects a
		// canceled call before writing anything.
		if cerr := ctx.Err(); cerr != nil {
			return reject(cerr)
		}
		var res fdPushResult
		if held {
			res = fd.q.pushHeld(reqs, id) // consumes the reservation on success
		} else {
			res = fd.q.pushBatch(reqs)
		}
		switch res {
		case fdPushOK:
			fd.submitMu.RUnlock()
			return batches, nil
		case fdPushClosed:
			return reject(ErrClosed)
		}
		// Queue full. Reserve the batch's slots, then retry at once rather than
		// sleeping: a take between the refused push and the hold saw no holder
		// to wake, and if it emptied the queue there would be no later take to
		// wake this batch. The retry under the reservation either fits, or
		// observes a queue that still holds work (so a take, which wakes the
		// head, is guaranteed), or finds another holder ahead (whose departure
		// wakes the new head).
		if !held {
			id, wake = fd.q.hold(len(reqs))
			held = true
			continue
		}
		// Wait to be woken as the head of the line (a take freed room, or the
		// previous head left), then retry the whole batch. The wake is this
		// batch's own cap-1 channel, so it cannot be taken by another waiter and
		// a wake that lands while this batch is mid-retry is kept for the next
		// wait. Same release conditions as submit()'s backpressure wait.
		select {
		case <-wake:
		case <-ctx.Done():
			return reject(ctx.Err())
		case <-fd.ap.ctx.Done():
			return reject(ErrClosed)
		}
	}
}

// ErrFDPipelineTooLarge is returned when a batch cannot fit the submit queue
// even when the queue is empty.
var ErrFDPipelineTooLarge = errors.New(
	"redis: FDPipelined batch is larger than the full-duplex submit queue")
