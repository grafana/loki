package redis

import (
	"context"
	"encoding"
	"errors"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/cpu"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/otel"
	"github.com/redis/go-redis/v9/internal/pool"
)

// AutoPipelineOptions configures the autopipelining behavior.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
type AutoPipelineOptions struct {
	// MaxBatchSize is the target batch size: the accumulator stops waiting for
	// more commands once the shard queue reaches it, so a batch flushes promptly
	// instead of lingering. It is a soft threshold, not a hard cap — under heavy
	// concurrent enqueue (or while a flush waits on the concurrency semaphore) the
	// queue can grow past it and execute as a single larger pipeline, which is
	// safe and simply yields a deeper pipeline.
	// Default: 200 (the blocking face's no-options preset,
	// DefaultBlockingAutoPipelineOptions, uses 300).
	MaxBatchSize int

	// MaxBatchBytes caps a batch by APPROXIMATE payload volume: the
	// accumulator stops waiting once the queued commands' argument bytes reach
	// it, so many large values flush as several bounded writes instead of one
	// huge burst (300 x 64KiB is ~19MB written down one connection before any
	// reply is read — enough to stall a constrained link past its write
	// deadline). Like MaxBatchSize it is a soft threshold, not a hard cap.
	// The estimate sizes string, []byte, *string and BinaryMarshaler arguments
	// by their encoded length (other argument kinds by a small fixed size) plus
	// a small per-argument overhead.
	//
	// Default: 128 KiB. Only 0 selects the default; a negative value is
	// rejected by Validate rather than silently coerced (the autopipeliner
	// getters return that error). There is no "unbounded" setting — pass a
	// deliberately large value instead. This is a
	// full-duplex safety guardrail, not a throughput knob: with FullDuplex,
	// the reader cannot start draining replies until the writer finishes
	// flushing the WHOLE batch (see autopipeline_fullduplex.go), so a batch
	// with ≥2 large-payload commands can deadlock both directions — Redis
	// blocks writing an early large reply while the client is still blocked
	// writing the rest of the batch, resolved only by WriteTimeout, and
	// recovery may then replay a command Redis already executed (cursor/codex
	// on #4002). The cap bounds this for large-REQUEST commands (big ECHO,
	// large SET values); it does NOT bound expected REPLY size, so a batch of
	// plain GETs against large values is still exposed regardless of this
	// setting — closing that gap needs the reader to drain incrementally, not
	// a byte cap. For ordinary small commands this default rarely binds:
	// MaxBatchSize's 200-command cap already keeps a typical batch under
	// ~15 KiB, far below this threshold.
	MaxBatchBytes int

	// MaxConcurrentBatches is the maximum number of pipeline batches that may
	// execute concurrently.
	//
	// Default: 1, which gives a single ordered command stream — batches execute
	// serially in submit order, so even a windowed caller (submit many, read
	// later) sees strict ordering, while still reaching high throughput via deep
	// pipelines (~3M ops/sec locally).
	//
	// Setting this above 1 runs batches in parallel for maximum throughput, but
	// commands then have NO guaranteed execution order. Because that trades away
	// ordering, it is only allowed together with Unordered: true — otherwise the
	// configuration is rejected (see Validate). This makes the trade-off
	// explicit: you cannot accidentally lose ordering by raising concurrency.
	MaxConcurrentBatches int

	// Unordered must be set to true to allow MaxConcurrentBatches > 1. It is the
	// caller's explicit acknowledgement that parallel batch execution gives up
	// command ordering in exchange for throughput. With the default (false),
	// MaxConcurrentBatches is forced to 1 (an ordered stream) and any value > 1
	// is a configuration error.
	Unordered bool

	// FullDuplex enables the ordered full-duplex dispatch path: one held
	// pipeline-pool connection with a writer+reader goroutine pair streaming the
	// ordered command stream, instead of the half-duplex one-batch-per-round-trip
	// flusher. Its win is a latency-bound (WAN) link under many concurrent
	// goroutines: ~1 RTT latency and pipe-saturated throughput on a single
	// connection. On a fast link (loopback) prefer half-duplex — with no RTT to
	// overlap, full-duplex only adds coordination overhead.
	//
	// Also honored by the full-duplex writer, where it is gated on in-flight
	// depth so it never taxes low-concurrency callers (see fdAccumMinFor).
	//
	// Honored on the ordered (Unordered:false, MaxConcurrentBatches<=1) face of a
	// standalone *Client that has a pipeline pool — BOTH the deferred
	// (AsyncAutoPipeline) and the blocking (AutoPipeline) face. NumShards > 1
	// runs that many full-duplex engines, each on its own held connection; see
	// NumShards for the ordering that mode keeps. A SINGLE
	// blocking caller gains nothing: it has one command in flight, so there is
	// nothing to overlap, and it still pays the held connection and goroutine
	// overhead; the win needs MANY concurrent blocking callers, whose commands then
	// overlap on the shared pipe exactly as on the async face (~1 RTT each instead
	// of batch phase-locking). On a ClusterClient it runs natively per node: the
	// engine keeps one FD child per master (each on that node's node.Client, which
	// has a pipeline pool by default) and routes each command to the child that
	// owns its slot; MOVED/ASK redirects are followed through the redirect-aware
	// cluster path (topology reload included). It falls back to half-duplex only
	// when PipelinePoolSize<0 removes the node pipeline pools. Validate rejects the
	// contradictory standalone combos (FullDuplex with Unordered or MaxConcurrentBatches>1).
	//
	// Ordering caveat: blocking and connection-hostile commands (BLPOP, WAIT,
	// XREAD BLOCK, SUBSCRIBE, MULTI, ...) are diverted to a separate pooled
	// connection so they cannot stall the shared pipe. Managed HIMPORT
	// (PREPARE/SET/DISCARD/DISCARDALL) is diverted too, but only on the full-duplex
	// path: a fieldset is connection-session state that the FD writer does not
	// replay and the FD reader does not track, so it runs through the normal Process
	// path — which injects the registered PREPARE and keeps the registry current —
	// instead of failing with "no such fieldset". A reply that is a retryable Redis
	// error (LOADING/READONLY/…) is likewise re-run through Process, off the FD
	// reader, so the reader keeps completing later replies. A MOVED/ASK redirect is
	// NOT replayed on the standalone full-duplex path — a standalone Client cannot
	// route it — so the redirect surfaces to the caller as the command's error,
	// exactly as it does for a plain standalone command (a cluster-aware FD path
	// could route it instead; that is a follow-up). Per-caller ordering therefore
	// does NOT hold across a diverted command: it may settle AFTER a command
	// submitted later on the same goroutine.
	// That reorders only a caller holding TWO causally-dependent commands in flight
	// WITHOUT awaiting the first (e.g. Set(k) then Get(k) both fired on the async
	// face before reading Set's result); awaiting a result before issuing a
	// dependent one preserves order, and the blocking face waits per command by
	// construction, so its per-goroutine ordering is unaffected. NoRetry commands
	// are never diverted. Half-duplex diverts identically; blocking commands were
	// never part of the ordered stream.
	//
	// Observability: process hooks (redisotel spans/metrics, custom AddHook
	// ProcessHooks) DO fire on the full-duplex path — each command runs the hook
	// chain individually (withProcessHook, not the batch ProcessPipelineHook), the
	// span bracketing its real write→reply latency; with none registered the hosting
	// is skipped entirely (the fast path). Presence is checked per command at submit
	// time, so a hook registered via AddHook is observed only by commands submitted
	// after it (one already in flight is not retroactively spanned). DialHook and
	// pool stats work as usual. Caveat: the write is already queued on the shared
	// stream when the hook host starts, so a hook that SHORT-CIRCUITS (returns
	// without calling next) does NOT cancel execution — the command still runs on
	// the wire and only the hook's returned error reaches the caller, unlike the
	// half-duplex path where next() gates the write. A hook that relies on
	// short-circuiting to BLOCK a command (a policy/ACL/kill-switch hook, or a
	// mock/cache that must not touch the server) therefore does NOT prevent the
	// server write under FullDuplex — run such hooks on a plain client or the
	// half-duplex autopipeline. A hook that calls next and only OBSERVES the
	// command (reads its result after next, optionally rewriting the returned
	// error) is unaffected. But because the write is already queued when the host
	// starts, a hook MUST NOT mutate the command — e.g. cmd.Args() — set its
	// result, or READ its result (cmd.Err(), cmd.Val(), cmd.String(), ...)
	// BEFORE calling next: the write is already queued and the reader may be
	// completing the command concurrently. On the hook-host goroutine the result
	// accessors do not block (it is the batch's executor; blocking there would
	// self-deadlock — see await), so a pre-next read is the not-yet-executed view
	// racing the reader's write, not a wait for the result. Read results only
	// after next returns. Mutate-before-next hooks must run on a plain client or
	// the half-duplex autopipeline, where next() gates the write.
	//
	// A ProcessHook MUST NOT synchronously call Close (Client.Close or
	// AutoPipeliner.Close) from inside the hook: the hook runs on the full-duplex
	// hook-host goroutine and Close waits for that goroutine to finish, so a
	// synchronous Close from the hook deadlocks until the close backstop (~30s).
	// Trigger Close from a separate goroutine if a hook must initiate it.
	//
	// TODO(fullduplex): offer opt-in write-gating for blocking hooks — a per-client
	// or per-command flag that waits for the hook to call next before enqueuing the
	// command onto fd.ch, so a policy hook can veto the write, at the cost of the
	// ~1-RTT concurrency for gated commands (observability-only hooks keep the fast
	// path). Until then the short-circuit-does-not-block semantics above are
	// intentional, not a bug.
	//
	// Limiter: Options.Limiter is admitted (Allow/ReportResult) once PER WRITTEN
	// BATCH — the chunk flushed to the connection in one write, the full-duplex
	// analogue of a pipeline exec or a half-duplex flush — not per command and
	// not per session. A deny fails every command of that chunk with the
	// Limiter's own error, verbatim (examinable via errors.Is); the session and
	// its connection stay alive, and the next chunk pays Allow again, so an open
	// breaker fail-fasts and service resumes as soon as it closes. Because
	// admission happens at write time, a deny surfaces as queue latency on the
	// awaited result, not as a submit-time error — and one deny covers a whole
	// chunk, exactly as one Allow covers a whole pipeline exec elsewhere.
	// ReportResult fires exactly once per admitted chunk, strictly paired with its
	// Allow, and carries the REPLY-side outcome — not the write result. A clean write
	// does NOT report at admission: the obligation rides the in-flight deque on the
	// chunk's last command and reports nil once every reply of the chunk has landed
	// (reply-LEVEL errors such as redis.Nil / WRONGTYPE / MOVED still report nil — a
	// server that answers is healthy). A write failure or encoder panic reports that
	// write error immediately (the replies will never come); a later transport failure
	// that abandons the chunk's unread replies reports that error. A denied (or
	// panicking) Allow grants no permit and reports nothing. So a custom Limiter must
	// expect its permit to be released on the reply side and to observe only transport
	// failures, never reply-level Redis errors.
	FullDuplex bool

	// FullDuplexWindow is the maximum in-flight (written-but-unacknowledged)
	// commands before the writer applies backpressure — a hard memory bound AND the
	// cap on how deep the pipe can fill, so it must exceed the bandwidth-delay
	// product (RTT × target rate) or it throttles throughput. The deque holds only
	// ACTUAL in-flight (self-limited by throughput), so a generous window costs no
	// memory until a stalled peer makes in-flight grow. Only used when FullDuplex is
	// set; 0 means the default (65536, covering ~50ms links at ~1.3M ops/s) and a
	// negative value is rejected by Validate.
	//
	// The window is PER ENGINE. With NumShards > 1 each full-duplex engine gets
	// its own window and submit queue of this size, so the client-wide bound is
	// NumShards × FullDuplexWindow in flight, plus as many queued. Size it for
	// one wire; to cap the total, divide by NumShards.
	FullDuplexWindow int

	// FullDuplexIdleTimeout is how long the held full-duplex connection may sit
	// with no queued work and a drained in-flight before it is returned to the pool
	// (so it is reusable and its per-conn hooks — streaming-creds re-auth,
	// maintnotifications — get a chance to run). Only used when FullDuplex is set.
	// 0 means the default (1s); a negative value is rejected by Validate.
	FullDuplexIdleTimeout time.Duration

	// FullDuplexMaxHold forces the same clean return under continuous load, so the
	// per-conn hooks run at least this often even when the connection never goes
	// idle. Only used when FullDuplex is set. 0 means the default (5s); a negative
	// value is rejected by Validate.
	FullDuplexMaxHold time.Duration

	// contentSharded is set internally by cluster wiring when commands are
	// routed to shards by content (slot), so same-key commands always share a
	// shard and per-key order holds even with several shards. It exempts that
	// wiring from the NumShards ordering check in newAutoPipeliner. Never set
	// by users (unexported).
	contentSharded bool

	// clusterReprocess is set internally by the cluster full-duplex router on each
	// per-node child config. When non-nil, the child's full-duplex engine re-runs a
	// command that came back with a retryable reply or a MOVED/ASK redirect through
	// this function instead of the node client's standalone process path, so the
	// redirect is followed on the redirect-aware ClusterClient (which routes to the
	// target node, sends ASKING, and reloads topology). Never set by users
	// (unexported). See clusterFDRouter and fdEngine.reprocess.
	clusterReprocess func(ctx context.Context, cmd Cmder, startAttempt int, writtenAt time.Time) error

	// clusterRetryBudget is set internally by the cluster full-duplex router to the
	// ClusterClient's MaxRedirects. The child's full-duplex engine uses it as its
	// connection-failure recovery budget (fdEngine.retryBudget) instead of the node
	// client's MaxRetries, which cluster node clients normalize to -1 (cluster
	// retries live in MaxRedirects). Never set by users (unexported).
	clusterRetryBudget int

	// NumShards is the number of independent queue+flusher shards the
	// autopipeliner runs. 0 (the default) means auto: a single shard, which
	// funnels every caller into one queue so batches stay deep — measured
	// throughput and latency are best with one shard even under heavy
	// goroutine concurrency. Cluster clients default to several slot-routed
	// shards instead, so commands for different nodes queue independently
	// (per-key order still holds: a key's slot always maps to the same
	// shard). Raising NumShards splits the queue: it reduces enqueue-mutex
	// contention but fragments batches, which usually costs far more than the
	// contention saves. Every shard always has at least one concurrency
	// permit, so the effective global batch concurrency is
	// max(NumShards, MaxConcurrentBatches) — and because shards flush
	// concurrently, NumShards > 1 on the deferred (async) face requires
	// Unordered: true (construction fails otherwise).
	//
	// With FullDuplex active on a standalone *Client, NumShards means something
	// else: the number of full-duplex engines, each holding one pipeline-pool
	// connection with its own window (see FullDuplexWindow). More engines pay
	// off only with many concurrent callers: measured with 10-command
	// pipelines, 8 engines were 30% slower than 1 at 8 callers, broke even at about
	// 128, and 2.2x faster at 1024. Commands are routed
	// to an engine by a hash of their first key, so Unordered is not required,
	// but order holds only between commands that share a first key. The key is
	// hashed in its string form (fmt for non-string arguments, as Ring does), so
	// pass a key as the same Go type everywhere: true and "1" are the same Redis
	// key but can land on different engines. Commands
	// that touch other keys after the first one (COPY, RENAME, MSET, multi-key
	// DEL, EVAL/FCALL with several keys) and keyless commands (including
	// FLUSHDB and SWAPDB) are not ordered against the rest; await the first
	// future before submitting a dependent one, or keep NumShards at 1, which
	// keeps the whole submit order. A Pipeline() rides the FD wire only when all
	// its keyed commands hash to one engine; otherwise it runs as an ordinary
	// pipeline on a pooled connection, not ordered against unawaited commands on
	// the engines. The pipeline pool must hold at least
	// NumShards connections for every full-duplex autopipeliner in use
	// (construction checks this one). If full duplex cannot engage (no
	// pipeline pool), the Unordered rule above applies.
	NumShards int

	// MaxFlushDelay is the maximum delay after flushing before checking for more commands.
	// A small delay (e.g., 100μs) can significantly reduce CPU usage by allowing
	// more commands to batch together, at the cost of slightly higher latency.
	//
	// Trade-off:
	// - 0 (default): Lowest latency, higher CPU usage
	// - 100μs: Balanced (recommended for most workloads)
	// - 500μs: Lower CPU usage, higher latency
	//
	// Based on benchmarks, 100μs can reduce CPU usage by 50%
	// while adding only ~100μs average latency per command.
	// Default: 0, meaning the flusher applies no coalescing wait — it flushes
	// each batch as soon as the queue is ready and lets in-flight backpressure
	// coalesce concurrent callers (see accumulateBatch). Set a value here to add
	// an explicit accumulation window, trading latency for larger batches / less
	// CPU as described above.
	MaxFlushDelay time.Duration

	// AdaptiveDelay enables smart delay calculation based on queue fill level.
	// When enabled, the delay is automatically adjusted:
	// - Queue ≥75% full: No delay (flush immediately to prevent overflow)
	// - Queue ≥50% full: 25% of MaxFlushDelay (queue filling up)
	// - Queue ≥25% full: 50% of MaxFlushDelay (moderate load)
	// - Queue <25% full: 100% of MaxFlushDelay (low load, maximize batching)
	//
	// This provides automatic adaptation to varying load patterns without
	// manual tuning. Uses integer-only arithmetic for optimal performance.
	// Default: false (use fixed MaxFlushDelay)
	//
	// Half-duplex only. The full-duplex writer has its own policy: it waits
	// MaxFlushDelay only when enough commands are in flight (see MaxFlushDelay)
	// and ignores AdaptiveDelay.
	AdaptiveDelay bool

	// MaxQueuedCommands, when > 0, is a hard limit on commands the
	// autopipeliner has accepted but not yet completed: queued, waiting for a
	// batch permit, executing, and commands run outside the pipeline (Do,
	// blocking and connection-hostile commands). A command submitted at the limit is not
	// queued: it fails at once with ErrAutoPipelineQueueFull. The limit bounds
	// client memory when the server is slower than the producers.
	//
	// Rejection is per command, so it can break submit order on the deferred
	// face: a SET may be rejected while a later GET on the same key is
	// accepted. Check the error of each command that the next one depends on.
	//
	// With FullDuplex the ordered stream is bounded by FullDuplexWindow (a full
	// window blocks the submitter instead of rejecting), so MaxQueuedCommands
	// then limits only the commands run outside the pipeline.
	// Default: 0 (no limit).
	MaxQueuedCommands int
}

// autoPipelinePermitBackstop bounds how long a flush waits for a concurrency
// permit when all are busy. It is only a safety net against a wedged semaphore:
// every permit holder releases it (via defer) and each batch Exec is itself
// bounded by the connection's read/write timeout, so in normal operation a
// permit frees long before this. It is set well above the default ReadTimeout
// and a maintnotifications relaxed window so a legitimately slow in-flight batch
// never makes waiters fail spuriously. The wait deliberately does NOT end on
// Close: commands taken from the queue were already accepted, and Close's
// contract is to flush them (it waits via wg/batchWg), so permit waits run on
// a background context bounded only by this backstop.
const autoPipelinePermitBackstop = 30 * time.Second

// autoPipelineCloseBackstop bounds Close's wait for in-flight dispatches. It
// deliberately carries the same value as the permit backstop but its OWN name:
// the two answer different questions, and this one may want tuning on its own.
//
// Why it is generous rather than snappy: the bound is only ever REACHED when a
// dispatch cannot end by itself — a blocking command with no timeout, or a
// stalled read with ReadTimeout disabled. In every other configuration the
// read timeout ends the dispatch and Close returns the moment it does, well
// under this value. A tighter bound would not speed up healthy shutdowns; it
// would instead make Close report failure while legitimate work is still
// finishing (a large final batch, or a maintnotifications relaxed window
// during a failover), turning a correct slow drain into a spurious error.
const autoPipelineCloseBackstop = 30 * time.Second

// numAutoPipelineShards is the shard-count default used by CLUSTER wiring,
// where commands are routed to shards by slot so different nodes' batches
// queue independently (every shard keeps at least one concurrency permit, so
// several shards can flush to their nodes in parallel regardless of
// MaxConcurrentBatches). It is NOT used for standalone clients: those default
// to one shard (see newAutoPipeliner), because a single deep queue pipelines
// far better than a fragmented one. Deliberately NOT derived from
// MaxConcurrentBatches — coupling shard count to the permit budget silently
// collapsed cluster slot routing to a single shard at the default budget.
func numAutoPipelineShards() int {
	n := runtime.GOMAXPROCS(0)
	if n < 1 {
		n = 1
	}
	const maxShards = 16
	if n > maxShards {
		n = maxShards
	}
	return n
}

// DefaultAutoPipelineOptions returns the default autopipelining configuration.
//
// The default is ordered: MaxConcurrentBatches is 1, so batches execute
// serially in submit order (a single ordered command stream) while still
// reaching high throughput via deep pipelines when callers submit in windows.
// To trade ordering for parallel-batch throughput, set MaxConcurrentBatches > 1
// together with Unordered: true.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
func DefaultAutoPipelineOptions() *AutoPipelineOptions {
	return &AutoPipelineOptions{
		MaxBatchSize:         200,
		MaxBatchBytes:        128 * 1024, // see MaxBatchBytes doc: full-duplex deadlock guardrail, not a throughput knob
		MaxConcurrentBatches: 1,          // ordered by default
		MaxFlushDelay:        0,          // lowest latency; no coalescing wait (batch via in-flight backpressure)
	}
}

// DefaultBlockingAutoPipelineOptions returns the default config for the
// blocking face (Client.AutoPipeline). It uses a single ordered batch stream
// (MaxConcurrentBatches: 1). Counterintuitively this maximizes throughput AND
// minimizes latency for the blocking face: with one batch in flight, callers whose
// commands return while it executes re-enqueue and flush together as the next
// batch, so batches stay deep (a near-continuous, double-buffered pipeline),
// while a lone caller flushes promptly in a single round-trip (no coalescing
// wait — see accumulateBatch). More parallel permits (MaxConcurrentBatches>1) do the
// opposite: each command finds a free permit and flushes on its own before
// others accumulate, collapsing batch size — and throughput — toward one command
// per round-trip while latency rises. For maximum throughput use the async face
// (AsyncAutoPipeline) with a window of in-flight commands (inflight>1); it keeps
// MaxConcurrentBatches: 1 as well.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
func DefaultBlockingAutoPipelineOptions() *AutoPipelineOptions {
	return &AutoPipelineOptions{
		MaxBatchSize:         300,
		MaxBatchBytes:        128 * 1024, // see MaxBatchBytes doc: full-duplex deadlock guardrail, not a throughput knob
		MaxConcurrentBatches: 1,
	}
}

// Validate reports whether the configuration is self-consistent. It returns an
// error if MaxConcurrentBatches > 1 without Unordered: true — raising
// concurrency gives up command ordering, so the caller must opt in explicitly.
//
// Validate()==nil does not guarantee construction succeeds: rules that need
// the face (e.g. NumShards>1 requires Unordered on the deferred face) are
// enforced by the AutoPipeline/AsyncAutoPipeline getters. Note also that
// Options.AutoPipelineOptions is validated lazily — on the first getter
// call, not in NewClient.
func (cfg *AutoPipelineOptions) Validate() error {
	if cfg.FullDuplex {
		// Full-duplex matches replies to commands by FIFO position on one connection,
		// which Unordered / parallel batches break. Checked BEFORE the generic
		// MaxConcurrentBatches rule so the message is FullDuplex-specific.
		if cfg.Unordered {
			return fmt.Errorf("redis: AutoPipelineOptions.FullDuplex requires an ordered stream " +
				"(Unordered:false); full-duplex matches replies by in-flight FIFO position, which " +
				"Unordered breaks")
		}
		if cfg.MaxConcurrentBatches > 1 {
			return fmt.Errorf("redis: AutoPipelineOptions.FullDuplex requires MaxConcurrentBatches<=1 "+
				"(an ordered single stream); got %d", cfg.MaxConcurrentBatches)
		}
		// A USER-set NumShards>1 contradicts FullDuplex the same way (one held FIFO
		// connection is one stream); reject it rather than silently falling back to
		// half-duplex. contentSharded is exempt: that flag is set by the CLUSTER
		// wiring (never by users), where the silent fallback IS the documented
		// behavior, since the options type cannot see the client type.
		// NumShards>1 under FullDuplex means N engines, each holding its own
		// connection, routed by key hash so per-key order still holds (see
		// autopipeline_fd_shards.go). It is therefore no longer rejected here.
		//
		// This is one half of a TWO-SITED rule: the fdOn gate in
		// newAutoPipeliner must relax with it. Relaxing only this check leaves
		// full duplex silently OFF for NumShards>1 — commands fall through to
		// the half-duplex shards, which are unordered, and a same-key
		// write-then-read can read the stale value.
		_ = cfg.contentSharded
	}
	if cfg.MaxConcurrentBatches > 1 && !cfg.Unordered {
		return fmt.Errorf("redis: AutoPipelineOptions.MaxConcurrentBatches=%d requires Unordered:true "+
			"(parallel batches do not preserve command ordering); set Unordered:true to allow it, "+
			"or keep MaxConcurrentBatches=1 for an ordered stream", cfg.MaxConcurrentBatches)
	}
	// Reject obviously-wrong negatives so a typo surfaces at construction rather
	// than being silently coerced to a default. Zero is allowed and means "use
	// the default" (MaxBatchSize) or "no delay" (MaxFlushDelay).
	if cfg.MaxBatchSize < 0 {
		return fmt.Errorf("redis: AutoPipelineOptions.MaxBatchSize=%d must be >= 0", cfg.MaxBatchSize)
	}
	if cfg.MaxBatchBytes < 0 {
		return fmt.Errorf("redis: AutoPipelineOptions.MaxBatchBytes=%d must be >= 0", cfg.MaxBatchBytes)
	}
	if cfg.MaxConcurrentBatches < 0 {
		return fmt.Errorf("redis: AutoPipelineOptions.MaxConcurrentBatches=%d must be >= 0", cfg.MaxConcurrentBatches)
	}
	if cfg.MaxFlushDelay < 0 {
		return fmt.Errorf("redis: AutoPipelineOptions.MaxFlushDelay=%s must be >= 0", cfg.MaxFlushDelay)
	}
	if cfg.NumShards < 0 {
		return fmt.Errorf("redis: AutoPipelineOptions.NumShards=%d must be >= 0", cfg.NumShards)
	}
	if cfg.MaxQueuedCommands < 0 {
		return fmt.Errorf("redis: AutoPipelineOptions.MaxQueuedCommands=%d must be >= 0", cfg.MaxQueuedCommands)
	}
	if cfg.AdaptiveDelay && cfg.MaxFlushDelay <= 0 {
		return fmt.Errorf("redis: AutoPipelineOptions.AdaptiveDelay requires MaxFlushDelay > 0 " +
			"(adaptive delay scales MaxFlushDelay by queue fill; with no MaxFlushDelay it would " +
			"silently disable batch accumulation entirely)")
	}
	// The full-duplex tuning fields are consumed only when FullDuplex is enabled
	// (newFDEngine resolves them; the half-duplex path never reads them), so validate
	// them only then. Otherwise a leftover negative on an inactive field would reject
	// an otherwise valid half-duplex config.
	if cfg.FullDuplex {
		if cfg.FullDuplexWindow < 0 {
			return fmt.Errorf("redis: AutoPipelineOptions.FullDuplexWindow=%d must be >= 0 (0 = default)", cfg.FullDuplexWindow)
		}
		if cfg.FullDuplexIdleTimeout < 0 {
			return fmt.Errorf("redis: AutoPipelineOptions.FullDuplexIdleTimeout=%s must be >= 0 (0 = default)", cfg.FullDuplexIdleTimeout)
		}
		if cfg.FullDuplexMaxHold < 0 {
			return fmt.Errorf("redis: AutoPipelineOptions.FullDuplexMaxHold=%s must be >= 0 (0 = default)", cfg.FullDuplexMaxHold)
		}
	}
	return nil
}

// cmdableClient is an interface for clients that support pipelining.
// Both Client and ClusterClient implement this interface. It embeds
// UniversalClient (Cmdable + Process + Do + AddHook + Watch + Subscribe... +
// Close + PoolStats) so the AutoPipeliner can delegate the non-batched surface
// back to the underlying client and itself satisfy UniversalClient.
type cmdableClient interface {
	UniversalClient
	// processPipelineHook is the hook-wrapped []Cmder pipeline entry — the same
	// method Pipeline.Exec is wired to (see Client.Pipeline). The flusher
	// dispatches drained batches through it directly, skipping the per-batch
	// Pipeline construction; hooks/OTel see the identical call.
	processPipelineHook(ctx context.Context, cmds []Cmder) error
	// The async faces additionally dispatch through withProcessPipelineHook /
	// withProcessHook with the base processors as the innermost, so the batch
	// can be completed UNDER the user hooks (results ready the moment exec
	// returns, before hooks unwind). Both *Client and *ClusterClient satisfy
	// these via hooksMixin and their base processors.
	withProcessPipelineHook(ctx context.Context, cmds []Cmder, hook ProcessPipelineHook) error
	hookCount() int
	withProcessHook(ctx context.Context, cmd Cmder, hook ProcessHook) error
	processPipeline(ctx context.Context, cmds []Cmder) error
	process(ctx context.Context, cmd Cmder) error
}

// apBatch is the completion signal shared by every command flushed together.
// Its done channel is closed exactly once, when the batch's pipeline has
// executed. Closing one channel wakes all waiters in a single operation,
// instead of doing one buffered-channel send per command — under high
// concurrency the per-command sends dominated CPU (channel-lock contention and
// one goroutine wake-up apiece).
type apBatch struct {
	done chan struct{}
	// fdAttempts is how many times the full-duplex engine issued a PIPELINED
	// command (1, plus one per connection-error replay), stamped by the reader
	// before it completes the command, so closing done publishes it. The
	// pipeline retry reads it to charge those executions against MaxRetries.
	// Zero on every other path.
	fdAttempts int
	// fdConn is the held connection that carried a PIPELINED command's reply,
	// stamped with fdAttempts, for the pipeline duration metric.
	fdConn *pool.Conn
	// fdGroup marks the commands of one FD pipeline batch: every batch of the
	// pipeline points at the batch of its first command. The Close-time flush
	// uses it to run the pipeline as one, with one whole-batch retry.
	fdGroup *apBatch
	// fdFlushed is set when the Close-time flush ran this pipelined command
	// through the pooled pipeline, which recorded the pipeline metric itself.
	fdFlushed bool
	// closed makes close() idempotent: on the async faces the dispatch closes
	// the batch at the innermost exec seam (under the user hooks, so a hook
	// reading a result after next() does not block on a channel its own
	// goroutine closes — the #3867 deadlock), while the flusher keeps its
	// deferred close as a panic backstop. Whichever runs first wins.
	closed atomic.Bool
	// dispGid is the goroutine id of the dispatcher while the batch is inside
	// the hook chain (0 otherwise). await() consults it before blocking so a
	// hook on the dispatch goroutine reading a result BEFORE next() gets the
	// not-yet-executed view — what a plain pipeline hook sees — instead of a
	// self-deadlock.
	dispGid atomic.Int64
	// nodeGids registers cluster per-node executor goroutines: the cluster
	// pipeline fans a batch out to one goroutine per node, and each runs the
	// NODE client's own hook chain (OnNewNode hooks — redisotel's tracing
	// lives there), which the single dispGid slot cannot vouch for. A node
	// hook reading a result there would block on a batch that completes only
	// after its own return — reproduced as a permanent wedge with a
	// rediscmd-shaped Err() peek. Guarded by nodeMu; entered/left once per
	// node call, consulted only on the guards' slow path (done still open).
	nodeMu   sync.Mutex
	nodeGids []int64
	// nodeCount mirrors len(nodeGids) so isExecutorGoroutine's fast path can
	// skip the goroutine-id parse and the mutex entirely when nobody is
	// registered — which is every standalone batch, always, and a cluster
	// batch outside its node fan-out window.
	nodeCount atomic.Int32
	// pooled marks a batch drawn from fdBlockingBatchPool: its done channel is
	// buffered(1) and completion signals via a non-blocking SEND (see close) so
	// the channel is reusable, instead of the close()-once unbuffered channel
	// every other batch uses. Only the full-duplex BLOCKING face produces these
	// — the one path where the batch is a single-waiter completion signal that
	// is never installed on the command (no setReady) and is discarded after
	// Wait. Immutable for the batch's lifecycle; set at construction.
	pooled bool
}

// enterNodeDispatch registers the calling goroutine as an executor of this
// batch for the duration of a cluster node call; the returned func
// unregisters it. Registered goroutines get the same treatment as the
// dispatcher in the accessor guards: result reads return the current view
// instead of self-deadlocking on the batch's own completion signal.
func (b *apBatch) enterNodeDispatch() func() {
	gid := curGoroutineID()
	b.nodeMu.Lock()
	b.nodeGids = append(b.nodeGids, gid)
	b.nodeCount.Store(int32(len(b.nodeGids)))
	b.nodeMu.Unlock()
	return func() {
		b.nodeMu.Lock()
		for i, g := range b.nodeGids {
			if g == gid {
				b.nodeGids[i] = b.nodeGids[len(b.nodeGids)-1]
				b.nodeGids = b.nodeGids[:len(b.nodeGids)-1]
				break
			}
		}
		b.nodeCount.Store(int32(len(b.nodeGids)))
		b.nodeMu.Unlock()
	}
}

// isExecutorGoroutine reports whether the CALLING goroutine is currently
// executing this batch: the flusher/dispatch goroutine or a registered
// cluster node executor. The no-executor fast path (dispGid unset and no
// node executors) is two atomic loads — no goroutine-id parse, no lock. That
// laziness is load-bearing: every blocking-face command and every pre-done
// future passes here once per wait, and an earlier revision that parsed the
// goroutine id and took the mutex unconditionally cost the blocking face 6x
// of its throughput (measured 830k -> 138k ops/sec on a loopback bench).
func (b *apBatch) isExecutorGoroutine() bool {
	disp := b.dispGid.Load()
	if disp == 0 && b.nodeCount.Load() == 0 {
		return false
	}
	gid := curGoroutineID()
	if disp != 0 && disp == gid {
		return true
	}
	if b.nodeCount.Load() == 0 {
		return false
	}
	b.nodeMu.Lock()
	defer b.nodeMu.Unlock()
	for _, g := range b.nodeGids {
		if g == gid {
			return true
		}
	}
	return false
}

// noopUnregister is registerBatchExecutors' zero-batch result, shared so the
// plain-pipeline path stays allocation-free.
var noopUnregister = func() {}

// registerBatchExecutors marks the calling goroutine as an executor of every
// deferred-face batch among cmds (plain pipeline commands carry none) and
// returns the combined unregister. The cluster pipeline calls it around each
// node's hook chain.
func registerBatchExecutors(cmds []Cmder) func() {
	var undo []func()
	var seenFirst *apBatch
	var seenMore map[*apBatch]struct{}
	for _, cmd := range cmds {
		bc, ok := cmd.(interface{ readyBatch() *apBatch })
		if !ok {
			continue
		}
		b := bc.readyBatch()
		if b == nil || b == seenFirst {
			continue
		}
		if seenFirst == nil {
			seenFirst = b
		} else {
			if seenMore == nil {
				seenMore = make(map[*apBatch]struct{}, 2)
			}
			if _, dup := seenMore[b]; dup {
				continue
			}
			seenMore[b] = struct{}{}
		}
		undo = append(undo, b.enterNodeDispatch())
	}
	if len(undo) == 0 {
		return noopUnregister
	}
	return func() {
		for _, u := range undo {
			u()
		}
	}
}

func newAPBatch() *apBatch { return &apBatch{done: make(chan struct{})} }

// fdBlockingBatchPool recycles apBatch objects for the full-duplex BLOCKING
// face — the single path where a batch is a pure, single-waiter completion
// signal: that face never setReady()s the command (so the batch is invisible to
// await/readyBatch/resultReady — verified: those all read cmd.ready, set only by
// setReady) and discards the batch right after Wait returns. Pooled batches use
// a buffered(1) done channel signalled by a non-blocking SEND (see close), so
// the channel — the bulk of newAPBatch's ~190 B/op — is reused rather than
// closed and thrown away. Every other batch (async face, shared flush batches
// with many/repeat readers) keeps the unbuffered close()-once channel.
var fdBlockingBatchPool = sync.Pool{
	New: func() any { return &apBatch{done: make(chan struct{}, 1), pooled: true} },
}

// getFDBlockingBatch returns a reset pooled batch. Fields are cleared
// individually (go vet copylocks forbids *b = apBatch{} because of nodeMu); a
// stale closed=true would make the next completion signal a no-op and park the
// caller forever, so the reset is not optional.
func getFDBlockingBatch() *apBatch {
	b := fdBlockingBatchPool.Get().(*apBatch)
	b.closed.Store(false)
	b.dispGid.Store(0)
	b.nodeCount.Store(0)
	b.nodeGids = nil
	// Drain any stray signal so the reused channel starts empty. Insurance: the
	// blocking face always drains done in Wait, so this is normally a no-op.
	select {
	case <-b.done:
	default:
	}
	return b
}

// putFDBlockingBatch returns a pooled batch after its single waiter has woken.
// Safe only once the batch is complete and unreferenced (see processBlocking).
func putFDBlockingBatch(b *apBatch) {
	if b == nil || !b.pooled {
		return
	}
	fdBlockingBatchPool.Put(b)
}

// close completes the batch exactly once, waking its waiter(s).
func (b *apBatch) close() {
	if b.closed.CompareAndSwap(false, true) {
		if b.pooled {
			// Buffered(1) done: signal with a non-blocking send so the channel
			// stays reusable (a closed channel cannot be reused). The CAS makes
			// exactly one send and cap 1 makes it never block; the one blocking
			// waiter (AutoFuture.Wait) drains it. Every completer — reader,
			// failReqs, shutdownFlush, flushBacklogForClose — funnels through
			// here, so this single branch covers them all.
			select {
			case b.done <- struct{}{}:
			default:
			}
			return
		}
		close(b.done)
	}
}

// curGoroutineID parses the goroutine id from runtime.Stack's header
// ("goroutine 123 ["). Called only on paths already paying a dispatch or an
// about-to-block round-trip wait — never on await()'s fast path — so the
// microsecond-scale stack read is noise against the batch RTT.
// armSelfDeadlockGuard reports whether async dispatch should stamp the
// dispatcher's goroutine id on the batches (see apBatch.dispGid) — the
// mechanism that lets a hook on the dispatch goroutine read a command
// without deadlocking on a batch only that goroutine completes: before
// next() it sees the not-yet-executed view, after next() the populated
// results (batches complete only when the whole chain has returned). Armed
// when user hooks exist — without hooks nothing can read a command inside
// the chain — and always on cluster clients, whose node clients may carry
// their own hooks (OnNewNode + AddHook, the redisotel pattern) that
// hookCount() cannot see. NOTE: node-level hooks run on node-worker
// goroutines the gid guard cannot identify, so they must not read command
// results on the async face; the same applies to a goroutine a hook spawns
// and joins before returning. A hook added concurrently with an in-flight
// dispatch misses the guard for that one batch. The guard covers result
// READS only: a hook that ISSUES a command on the same AutoPipeliner and
// synchronously waits for it cannot be saved — the nested command needs the
// dispatch slot the hook chain is holding, and the engine recovers only by
// failing the flush after the permit backstops (see
// autoPipelinePermitBackstop) expire.
func (ap *AutoPipeliner) armSelfDeadlockGuard() bool {
	return ap.pipeliner.hookCount() > 0 || ap.config.contentSharded
}

func curGoroutineID() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	const skip = len("goroutine ")
	var id int64
	for _, c := range buf[skip:n] {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + int64(c-'0')
	}
	return id
}

// The shard queue stores bare Cmders. The batch a command waits on is the
// shard's curBatch at enqueue time — read once to wire the command's ready
// channel and never needed per-command afterward (the flusher closes the one
// shared batch). Storing []Cmder removes a per-command wrapper allocation.

var queueSlicePool = sync.Pool{
	New: func() interface{} { s := make([]Cmder, 0, 100); return &s },
}

func getQueueSlice(capacity int) []Cmder {
	slice := (*queueSlicePool.Get().(*[]Cmder))[:0]
	if cap(slice) < capacity {
		queueSlicePool.Put(&slice)
		return make([]Cmder, 0, capacity)
	}
	return slice
}

func putQueueSlice(slice []Cmder) {
	if cap(slice) <= 1000 {
		// Zero only the used prefix: elements beyond len are already nil —
		// slices enter the pool fully zeroed (here) and are only appended to
		// afterwards, so the tail invariant holds. Zeroing the whole capacity
		// memclr'd up to 8 KB per flush for small batches on large recycled
		// arrays.
		for i := range slice {
			slice[i] = nil
		}
		queueSlicePool.Put(&slice)
	}
}

// AutoPipeliner automatically batches commands and executes them in pipelines.
// It's safe for concurrent use by multiple goroutines.
//
// AutoPipeliner works by collecting commands from multiple goroutines into a
// shared queue and flushing them as one Redis pipeline when the batch reaches
// MaxBatchSize or a configured coalescing window (MaxFlushDelay) elapses. By
// default there is no window: each batch flushes as soon as the queue is ready
// and concurrent callers coalesce via in-flight backpressure, so a lone command
// flushes in a single round-trip while batches stay deep under load.
//
// This provides significant performance improvements for workloads with many
// concurrent small operations, as it reduces the number of network round-trips.
//
// AutoPipeliner implements the Cmdable interface, so you can use it like a
// regular client. Prefer the typed methods (Set, Get, ...); Do runs OUTSIDE
// the pipeline on a normal connection (see Do).
// AutoPipeline / AsyncAutoPipeline return an error for an invalid config, so check it once:
//
//	ap, err := client.AutoPipeline()
//	if err != nil {
//		return err
//	}
//	ap.Set(ctx, "key", "value", 0)
//	ap.Get(ctx, "key")
//	ap.Close()
//
// Per-command contexts: a command is batched and executed on the AutoPipeliner's
// own long-lived context, NOT the context passed to the command. A per-command
// deadline or cancellation is therefore not honored once the command is queued
// (this is deliberate — a per-batch timer per command would cost a goroutine
// each). Use a plain client for commands that need their own deadline.
// The one exception is a blocking command (readTimeout() != nil, e.g. BLPOP):
// it is never batched and runs directly on the caller's context, which is
// honored as usual.
//
// Retries: like any pipeline, a batch that fails on a network error is retried
// as a whole (up to Options.MaxRetries). If the connection drops after the
// server executed part of the batch, non-idempotent commands (INCR, LPUSH, ...)
// may execute twice. Run commands that must not be retransmitted on a plain
// client, or set MaxRetries: -1.
//
// Lifetime: AutoPipeline() returns a single, client-owned instance shared by all
// callers. Close()ing it stops the shared pipeliner for everyone; a later
// AutoPipeline() call on the client builds a fresh one. Closing the CLIENT also
// stops it, but permanently: the getters then return ErrClosed.
//
// Formatting: String()/%v on a command issued by the deferred face WAITS for
// execution, exactly like Err()/Val()/Result() — formatting reads the result
// fields, and reading them unsynchronized would race the dispatcher populating
// them. The one exception is a hook formatting a command from the batch's own
// dispatch goroutine: that returns the not-yet-executed view instead of
// self-deadlocking. Use Name()/Args() if you need to log a submission without
// waiting for it.
//
// EXPERIMENTAL: this API is subject to change, use with caution.

type AutoPipeliner struct {
	cmdable // Embed cmdable to get all Redis command methods

	pipeliner cmdableClient
	config    *AutoPipelineOptions
	// fd, when non-nil, is the ordered full-duplex dispatch engine. When set,
	// submit() streams on one held connection instead of the sharded batch queue
	// and no shard flusher is started. See autopipeline_fullduplex.go.
	fd *fdEngine
	// fds holds every full-duplex engine when NumShards>1 puts several held
	// connections behind one autopipeliner (see autopipeline_fd_shards.go).
	// fds[0] is always fd, so every existing `ap.fd != nil` check still reads as
	// "full duplex is on" and the single-engine path is unchanged.
	fds  []*fdEngine
	fdRR atomic.Uint32 // round-robin cursor, keyless commands only
	// clusterFD, when non-nil, runs ordered full-duplex natively on a
	// *ClusterClient by routing each command to a per-node FD child autopipeliner
	// (one held connection per master). Mutually exclusive with fd and with the
	// half-duplex shard flushers: when set, submit() routes to the owning node's
	// child and no shard flusher is started. See autopipeline_cluster_fd.go.
	clusterFD *clusterFDRouter
	// pipelinePool is the connection pool that backs autopipelined batch
	// dispatch (distinct from the client's main pool). Captured once at
	// construction via an in-package assertion; nil when the underlying client
	// does not expose one (e.g. *ClusterClient). The straggler-hold reads it to
	// tell whether flushing a tiny batch now would contend for a scarce pooled
	// connection — see awaitExpectedArrivals / pipelineHasFreeConn.
	pipelinePool pool.Pooler
	// cscActiveFn reports whether client-side caching is CURRENTLY active on the
	// underlying client (nil when the client type exposes none). Consulted per
	// solo dispatch — not captured as a bool — because CSC can disable itself
	// mid-life (RESP3 fallback, processor damping), after which cacheable solos
	// should return to the pipeline pool instead of the main pool. Gates the
	// cacheable-solo routing: only an active-CSC client routes through Process
	// (which honors the cache).
	cscActiveFn func() bool
	// blocking selects how the typed command surface (Set, Get, ...) behaves:
	// when true the command call itself blocks until the command has executed
	// (drop-in, synchronous shape); when false the call returns immediately and
	// the result accessors (Val/Result/Err) block. See AutoPipeline (blocking)
	// vs AsyncAutoPipeline (deferred).
	blocking bool

	// Sharded command queues. Each shard has its own queue, mutex and flusher
	// goroutine, so enqueues from many goroutines spread across shards instead
	// of all contending on a single mutex and being drained by a single
	// flusher. Commands are assigned to shards round-robin; per-goroutine
	// ordering is still guaranteed because Do blocks for each command's result
	// before issuing the next one.
	shards []*apShard
	next   atomic.Uint32 // round-robin shard selector
	// shardFn, when set, picks a command's shard from its content (cluster mode
	// sets it to route by slot so all commands for one node land in the same
	// shard's batch — keeping per-node pipelines deep instead of splitting every
	// batch across nodes). When nil, commands are assigned round-robin.
	shardFn func(Cmder) int

	// preflight, when set, can reject a command at submit time, before it is
	// enqueued or dispatched (cluster mode refuses fan-out-policy commands
	// that cannot ride a pipeline, so one caller's command cannot poison a
	// merged batch). The returned error is set on the command.
	preflight func(ctx context.Context, cmd Cmder) error

	// mustDivert, when set, forces a command off the batching path even though
	// it is otherwise batchable — cluster mode uses it for commands whose
	// routing is NOT slot-derived (ReqSpecial, e.g. FT.CURSOR READ, which is
	// sticky to the node that owns the cursor). Batched, mapCmdsByNode would
	// route them by slot and reach the wrong shard; diverted, they go through
	// Client/ClusterClient.Process and keep their special routing.
	mustDivert func(ctx context.Context, cmd Cmder) bool

	// sharedClosed, when non-nil, is the owning client's pool-set closed flag
	// (shared across WithTimeout clones). The getters refuse to build a fresh
	// pipeliner once it is set; this reference makes an ALREADY-built
	// pipeliner refuse new work too — without it, a clone's Close would leave
	// a cached pipeliner accepting enqueues against closed pools, failing
	// them one dispatch at a time instead of with ErrClosed at submit.
	sharedClosed *atomic.Bool

	// expectedArrivals counts how many commands the engine expects to arrive
	// at any moment: a completed batch of N≥2 commands wakes its N waiters
	// together, and in a closed loop each immediately submits its next command
	// — so completion announces N expected arrivals, and every enqueue accounts
	// for one. The default coalescing wait (awaitExpectedArrivals) holds the
	// flusher while arrivals are still expected, so the whole wakeup wave
	// flushes as one deep pipeline — an exact count, not a smoothed estimate,
	// which cannot ratchet into fragmentation. Single-command batches announce
	// nothing, so a lone caller and open-loop traffic never wait. May
	// transiently go negative (arrivals nobody announced); readers clamp to
	// zero. Pipeliner-global, not per-shard: cluster routing may land a
	// follow-up on a different shard than the batch that woke its caller.
	expectedArrivals atomic.Int64

	// maxQueued is config.MaxQueuedCommands (0 = no limit). queued counts the
	// accepted, not-yet-completed commands it limits: admitQueued adds one,
	// releaseQueued subtracts a batch's commands before the batch closes.
	maxQueued int64
	queued    atomic.Int64

	// execEWMA is an exponentially-weighted moving average (alpha 1/8) of
	// batch execution time in nanoseconds — the engine's own view of the
	// server round-trip. It scales awaitExpectedArrivals's silence fallback so a
	// wave staggered by scheduling on a slow link is not split mid-landing. Updates
	// are racy read-modify-writes by design: losing an occasional sample is
	// harmless for a smoothing heuristic. 0 means "no sample yet".
	execEWMA atomic.Int64

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	// closeHooks / closeHookID: the shared baseClient onClose registry this engine
	// registered a cancel callback on, and the UNIQUE id it used. Any pool-sharing
	// wrapper's Close runs the registry and cancels this engine (so a clone closing
	// the shared pools reaps it); ap.Close unregisters so hooks stay bounded and a
	// closed engine's stale callback does not linger. The id is unique per engine —
	// a client and its clone can both cache the same face and must not collide on a
	// per-slot constant id (that would overwrite one hook and leak its engine).
	closeHooks  *onCloseHooks
	closeHookID string
	wg          sync.WaitGroup // Tracks flusher goroutines
	batchWg     sync.WaitGroup // Tracks batch execution goroutines
	// divertWg tracks the goroutines that execute DIVERTED commands (blocking
	// and connection-hostile ones, which never enter a batch). Close waits on
	// it exactly like batchWg so a diverted command's pooled connection is not
	// left in flight after Close returns — bounded, see Close.
	//
	// divertMu serializes "observe not-closed, then register" against Close's
	// "mark closed, then wait": without it a diverted command could pass the
	// closed check, Close could see a zero counter and return, and only then
	// would the goroutine register — leaving an accepted command holding a
	// pooled connection past Close (and racing WaitGroup Add against Wait).
	divertMu sync.Mutex
	divertWg sync.WaitGroup
	closed   atomic.Bool
	// closeDone is closed by the single Close that wins the closed CAS, once its
	// drain has completed; closeErr then holds that drain's result. A concurrent
	// Close that loses the CAS blocks on closeDone and returns closeErr, so no
	// caller observes the engine as closed — and starts tearing down the pools
	// underneath it — while accepted commands are still being flushed.
	closeDone chan struct{}
	closeErr  error
	// drainOnce memoizes cancelAndDrain's body: two closers can reach it for the
	// same engine (an explicit AutoPipeliner.Close racing a pool-sharing wrapper's
	// shared-pool close hook, which deliberately leaves ap.closed false). The body
	// runs exactly once and writes closeErr inside the Once (so closeErr has no
	// concurrent writer and WaitClosed reads it safely); both callers return that
	// one result.
	drainOnce sync.Once
	// drainRuns counts drain-body executions. The Once holds it at 1 however many
	// closers race, so a value > 1 means two closers double-drained the engine — the
	// invariant this counter guards (and a duplicate-close diagnostic).
	drainRuns atomic.Int64
}

// apShard is one queue + flusher. Its fields are touched only by enqueuing
// goroutines (under mu) and by its own single flusher goroutine.
// apEnqueueStripes is how many enqueue stripes a shard runs when striping is
// safe (unordered configs, and every blocking-face shard — a blocking caller
// waits for each command, so stripes cannot reorder its stream). The
// enqueue mutex is the hottest lock in the engine (128 concurrent callers on
// one shard spend ~half their CPU in lock slow paths); striping the queue
// spreads that contention while the flusher still drains every stripe into ONE
// merged pipeline, so batches stay deep. Ordered shards always use a single
// stripe: with several stripes a caller's consecutive commands can land in
// stripes on opposite sides of an in-progress drain and execute out of order.
const apEnqueueStripes = 8

// apStripe is one striped slice of a shard's enqueue queue. Each stripe has
// its own batch-completion signal so a drain can take stripes one lock at a
// time; every batch taken in one drain completes together after the merged
// pipeline executes. Padded so neighbouring stripes' mutexes do not share a
// cache line.
type apStripe struct {
	mu       sync.Mutex
	queue    []Cmder
	queueLen atomic.Int32
	// queueBytes approximates the queued commands' payload volume; maintained
	// only when MaxBatchBytes is configured (see cmdApproxBytes).
	queueBytes atomic.Int64
	curBatch   *apBatch // completion signal for currently-queued cmds
	// Pad each stripe onto its own cache line(s). Without it, one stripe's hot
	// fields (queueLen/curBatch) share a cache line with the NEXT stripe's
	// contended mutex, so a lock-free counter bump on stripe i invalidates the
	// line a different core is trying to lock stripe i+1 on — false sharing
	// that measured ~16x on a contended microbenchmark. cpu.CacheLinePad is
	// sized per GOARCH (64 B on x86-64/arm64, 128 B on ppc64, 256 B on s390x),
	// so this is correct on every target rather than a hand-tuned constant.
	_ cpu.CacheLinePad
}

type apShard struct {
	ap *AutoPipeliner

	next    atomic.Uint32           // round-robin stripe pick (unordered mode)
	stripes []apStripe              // 1 stripe when ordered, apEnqueueStripes when Unordered
	notify  chan struct{}           // buffered (cap 1) enqueue wake-up
	sem     *internal.FIFOSemaphore // per-shard concurrent-batch budget

	// inFlight counts this shard's dispatched-but-unfinished batches. When it
	// is zero and no arrivals are expected, the shard is idle and a
	// new command flushes immediately; when batches are in flight, arrivals
	// are mid-stream and the flusher holds them briefly to coalesce (see
	// awaitExpectedArrivals).
	inFlight atomic.Int32
}

// stripe picks the enqueue stripe for the next command: the single stripe in
// ordered mode (preserving strict FIFO), round-robin in unordered mode.
func (s *apShard) stripe() *apStripe {
	if len(s.stripes) == 1 {
		return &s.stripes[0]
	}
	return &s.stripes[s.next.Add(1)%uint32(len(s.stripes))]
}

// getOrCreateAutoPipeliner is the shared caching protocol behind the four
// AutoPipeline/AsyncAutoPipeline getters (Client and ClusterClient, each
// face): return the cached live instance, refuse on a closed client, or build
// and cache a new one. The caller supplies its cached-slot pointer, its
// closed flag (both guarded by the mutex), the explicit-config override, the
// fallback config, and a build closure (the cluster one wraps
// clusterAutoPipelineOptions and installs slot sharding).
func getOrCreateAutoPipeliner(
	mu *sync.Mutex,
	slot **AutoPipeliner,
	closed *bool,
	sharedClosed *atomic.Bool,
	onClose *onCloseHooks,
	closeHookBaseID string,
	override *AutoPipelineOptions,
	fallback func() *AutoPipelineOptions,
	build func(*AutoPipelineOptions) (*AutoPipeliner, error),
) (*AutoPipeliner, error) {
	mu.Lock()
	defer mu.Unlock()
	// closed covers THIS wrapper's Close; sharedClosed covers the shared
	// pools closing through ANY sharer (e.g. a WithTimeout clone falling
	// through to baseClient.Close) — a fresh pipeliner against closed pools
	// would leak flushers that error forever.
	if *closed || (sharedClosed != nil && sharedClosed.Load()) {
		return nil, ErrClosed
	}
	if *slot != nil && !(*slot).closed.Load() {
		return *slot, nil
	}
	cfg := override
	if cfg == nil {
		cfg = fallback()
	}
	ap, err := build(cfg)
	if err != nil {
		return nil, err
	}
	// Thread the shared pool-set closed flag into the pipeliner so an
	// ALREADY-cached instance also refuses enqueues once any sharer closes
	// the pools (the check above only protects fresh builds).
	ap.sharedClosed = sharedClosed
	// Register the shared-pool close hook ONCE, here under mu on the FRESH build —
	// not per call outside the lock, which would race concurrent first-callers and
	// register a hook per caller. A UNIQUE id per engine: a client and its
	// WithTimeout clone share onClose, so a per-slot constant id would overwrite one
	// registration and leak its engine. ap.Close unregisters by this id. onClose is
	// nil for cluster/ring (they pass a nil shared flag and have no clone-close leak).
	registered := true
	if onClose != nil {
		id := fmt.Sprintf("%s#%d", closeHookBaseID, apCloseHookSeq.Add(1))
		ap.closeHooks = onClose
		ap.closeHookID = id
		registered = onClose.register(id, func() error {
			// cancelAndDrain, not bare cancel: a pool-sharing wrapper's Close must WAIT
			// for this engine's shutdown flush to finish before closeResources tears the
			// shared pools down — cancel-and-return would let the pools close mid-flush
			// and fail accepted work. It is bounded (the drainAll backstop) so a wedged
			// flush cannot hang the closing wrapper. It deliberately does NOT set
			// ap.closed (the engine is rejected via the shared-closed flag) nor detach
			// this hook, so a later owner Close still runs the full teardown.
			return ap.cancelAndDrain()
		})
	}
	// Re-check after registering: a concurrent sharer Close sets sharedClosed and
	// runs onClose (snapshotting its callbacks) without this slot's mutex, so it can
	// pass the entry check above, snapshot the hooks, and miss the registration just
	// made — leaving this freshly built engine's goroutines parked on already-closed
	// pools forever. Two signals catch it: register reports false once run has taken
	// its snapshot, and baseClient.Close sets sharedClosed BEFORE running onClose, so
	// a close that has started — snapshot taken or not yet — shows in the flag.
	//
	// Either way the engine must be fully stopped before this returns, not merely
	// cancelled and abandoned (cursor bugbot on #4002): closeResources is tearing the
	// shared pools down, or is about to having missed the hook that would make it
	// wait, so it cannot be relied on to wait for this engine. cancelAndDrain does
	// the waiting here instead — the engine accepted no work (never published), so
	// this is its flushers observing the cancel, bounded by the drainAll backstop;
	// drainOnce makes it safe alongside a close that did snapshot the hook. Then
	// detach the hook and refuse rather than cache a doomed instance.
	if !registered || (sharedClosed != nil && sharedClosed.Load()) {
		_ = ap.cancelAndDrain()
		if onClose != nil {
			onClose.unregister(ap.closeHookID)
		}
		return nil, ErrClosed
	}
	*slot = ap
	return ap, nil
}

// newAutoPipeliner builds an autopipeliner in either blocking or deferred mode.
// It is unexported on purpose: the public entry points are
// Client/ClusterClient.AutoPipeline and AsyncAutoPipeline, which also install
// cluster slot-sharding. Constructing one directly would skip that wiring and
// give a *ClusterClient degraded (cross-node) batching.
func newAutoPipeliner(pipeliner cmdableClient, config *AutoPipelineOptions, blocking bool) (*AutoPipeliner, error) {
	if config == nil {
		config = DefaultAutoPipelineOptions()
	} else {
		// Copy so default-filling below doesn't mutate the caller's struct — the
		// same *AutoPipelineOptions may be shared across clients (e.g. a reused
		// Options.AutoPipelineOptions), and callers may inspect it afterward.
		cfgCopy := *config
		config = &cfgCopy
	}

	// Validate BEFORE default-filling: Validate treats zero as "use the
	// default" but rejects negatives, and coercing first would silently
	// swallow a negative typo the documented contract promises to error on.
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Apply defaults for zero values
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = 200
	}

	if config.MaxBatchBytes <= 0 {
		// Full-duplex deadlock guardrail, not a throughput knob — see the
		// MaxBatchBytes field doc. Applies here too so a caller-constructed
		// config that leaves this zero (rather than going through
		// DefaultAutoPipelineOptions) still gets the safety net.
		config.MaxBatchBytes = 128 * 1024
	}

	if config.MaxConcurrentBatches <= 0 {
		// Default to an ordered single stream. Callers raise this (with
		// Unordered:true) to opt into parallel-batch throughput.
		config.MaxConcurrentBatches = 1
	}

	// NumShards > 1 on the deferred (async) face distributes commands
	// round-robin across shards that flush concurrently, so submit order is
	// not preserved — require the explicit Unordered opt-in, exactly like
	// MaxConcurrentBatches > 1. The blocking face is exempt (each caller waits
	// per command, and Submit is rejected there), as is cluster slot sharding
	// (contentSharded: same-key commands always land in the same shard, so
	// per-key order holds).
	//
	// Full duplex is exempt for the SAME reason contentSharded is: its engines
	// are chosen by key hash, so same-key commands always land on one wire and
	// per-key order holds without an Unordered opt-in. The exemption follows
	// the EFFECTIVE state (fdOn), not the requested FullDuplex: on a client
	// without a pipeline pool full duplex does not engage, construction falls
	// back to the round-robin shards below, and those need the opt-in.
	//
	// Ordered full-duplex: the ordered single-shard face on a standalone *Client
	// with a pipeline pool, async or blocking. When on, submit() streams on one
	// held connection and no shard flusher runs. The blocking face needs nothing
	// extra: submit's fd branch skips setReady (the blocking contract) and
	// processBlocking Waits on the returned batch, as for a half-duplex enqueue.
	var fdClient *Client
	fdOn := false
	// NOT gated on nShards: NumShards>1 selects several engines rather than
	// disabling full duplex. This is the second half of the two-sited rule
	// noted in Validate — while this line also required nShards==1, a
	// NumShards>1 caller got half-duplex round-robin shards instead of the
	// ordered engine it asked for, silently.
	if config.FullDuplex && !config.Unordered && config.MaxConcurrentBatches <= 1 {
		if c, ok := pipeliner.(*Client); ok && c.getPipelinePool() != nil {
			fdOn, fdClient = true, c
		}
	}
	if config.NumShards > 1 && !config.Unordered && !blocking &&
		!config.contentSharded && !fdOn {
		return nil, fmt.Errorf(
			"redis: AutoPipelineOptions.NumShards=%d requires Unordered:true on the deferred (async) face "+
				"(commands are distributed round-robin across shards, which flush concurrently and do not preserve submit order)",
			config.NumShards,
		)
	}

	ctx, cancel := context.WithCancel(context.Background())

	ap := &AutoPipeliner{
		pipeliner: pipeliner,
		config:    config,
		blocking:  blocking,
		ctx:       ctx,
		cancel:    cancel,
		closeDone: make(chan struct{}),
		maxQueued: int64(config.MaxQueuedCommands),
	}
	// Capture the pipeline pool (in-package, promoted to *Client). nil for a
	// client that has none (e.g. *ClusterClient) — the straggler-hold then
	// keeps its conservative long hold rather than guess at pool pressure.
	if pp, ok := pipeliner.(interface{ getPipelinePool() pool.Pooler }); ok {
		ap.pipelinePool = pp.getPipelinePool()
	}
	// CSC probe (in-package assertion; *ClusterClient does not expose it) — see
	// the cscActiveFn field doc.
	if cc, ok := pipeliner.(interface{ autopipelineCSCActive() bool }); ok {
		ap.cscActiveFn = cc.autopipelineCSCActive
	}

	// Route the typed command surface. Blocking: the command call blocks until
	// executed (synchronous drop-in shape). Deferred: the call returns at once
	// and the result accessors block until the batch executes.
	if blocking {
		ap.cmdable = ap.processBlocking
	} else {
		ap.cmdable = ap.processAsync
	}

	// Pick the shard count. NumShards=0 (auto) means ONE shard: a single deep
	// queue outperforms a sharded one because batches stay large — sharding by
	// core count coupled batch fragmentation to MaxConcurrentBatches and
	// collapsed pipelining (measured: 16 shards cut async throughput ~4x and
	// tripled latency versus one shard at the same permit count). Cluster
	// wiring passes an explicit NumShards so slot-routed shards keep each
	// batch on one node.
	nShards := config.NumShards
	if nShards <= 0 {
		nShards = 1
	}
	// Cluster full-duplex: a *ClusterClient cannot host an fdEngine (it has no
	// pipeline pool of its own), but every master node's node.Client is a
	// standalone *Client that gets one by default. When full-duplex is requested
	// on the ordered single-face cluster autopipeliner, route each command to a
	// per-node FD child (clusterFDRouter) instead of the half-duplex shard
	// flushers. Gate on cc.opt.PipelinePoolSize >= 0 — the exact predicate that
	// decides whether every node.Client gets a pipeline pool (osscluster.go passes
	// it through, redis.go creates the pool on it) — so the check is synchronous
	// and needs no topology load under the getter's mutex. Force a single
	// flusherless shard, exactly like the fdOn path: no half-duplex flusher runs
	// and enqueue's shard indexing stays safe (never a %0), even though submit
	// routes past it.
	var clusterFDCC *ClusterClient
	clusterFDOn := false
	if config.FullDuplex && !config.Unordered && config.MaxConcurrentBatches <= 1 {
		// The router always routes to the slot's MASTER node child (slotMasterNode).
		// A client configured for replica routing — ReadOnly, RouteByLatency, or
		// RouteRandomly — would have those options silently ignored under cluster FD,
		// pinning reads to masters. Fall back to the half-duplex shard flushers, which
		// route through Process and honor the configured ShardPicker, and let
		// Config().FullDuplex report false (honest: FD is not the effective mode).
		// RouteByLatency/RouteRandomly auto-enable ReadOnly at option init (before this
		// gate reads them), so !ReadOnly alone would cover all three; the explicit
		// three document intent and are robust to any init reordering.
		if cc, ok := pipeliner.(*ClusterClient); ok &&
			cc.opt.PipelinePoolSize >= 0 &&
			!cc.opt.ReadOnly && !cc.opt.RouteByLatency && !cc.opt.RouteRandomly {
			clusterFDOn, clusterFDCC = true, cc
			nShards = 1
			// Report the actual shard count (1), not the cluster default, from Config().
			config.NumShards = 1
			// Resolve the FD tuning defaults on the PARENT config now, mirroring
			// newFDEngine (which only writes them onto each child's config). Without
			// this, Config() on a default-constructed cluster-FD autopipeliner would
			// report zero for these while the children enforce nonzero defaults —
			// breaking the effective-defaults contract the standalone FD path honors.
			// config is the same pointer Config() reads; this runs at construction
			// before ap escapes, so no Config() reader races these writes.
			if config.FullDuplexWindow <= 0 {
				config.FullDuplexWindow = fdDefaultWindow
			}
			if config.FullDuplexIdleTimeout <= 0 {
				config.FullDuplexIdleTimeout = fdDefaultIdle
			}
			if config.FullDuplexMaxHold <= 0 {
				config.FullDuplexMaxHold = fdDefaultMaxHold
			}
		}
	}
	// Split the concurrent-batch budget across shards so each shard has its own
	// semaphore. A single shared semaphore became a contention point once the
	// per-shard queue mutexes were no longer the bottleneck. Integer division
	// drops a remainder, so hand the leftover permits to the first shards: the
	// per-shard permits then sum to exactly MaxConcurrentBatches.
	perShard := config.MaxConcurrentBatches / nShards
	remainder := config.MaxConcurrentBatches % nShards
	if perShard < 1 {
		// Budget smaller than the shard count: give every shard one permit so
		// each flusher can still make progress. The sum then exceeds the
		// configured budget, which is unavoidable with per-shard semaphores.
		perShard = 1
		remainder = 0
	}
	// fdOn was decided above, before the ordering check. Engines are
	// flusherless, so nShards is forced to 1 below to keep enqueue's shard
	// indexing safe; the engine count lives in ap.fds.
	if fdOn {
		// Every engine holds one connection leased from the pipeline pool for
		// as long as it runs, so more engines than that pool can hold would
		// leave the surplus permanently spilling to the main pool — quietly
		// competing with ordinary commands instead of pipelining. Fail loudly
		// instead: the caller can raise PipelinePoolSize or ask for fewer
		// engines. DefaultPipelinePoolSize is 10, so this bites at 11+.
		//
		// The check is per autopipeliner. The blocking and async faces and
		// WithTimeout clones each run their own engines on the SAME pipeline
		// pool, so the pool must hold the sum of their NumShards; two faces at
		// NumShards 8 pass here against a pool of 10, and the surplus spills.
		// A client-wide engine count is a follow-up; until then size
		// PipelinePoolSize for every full-duplex autopipeliner in use.
		if n := fdShardCount(config); n > 1 {
			if pp := fdClient.getPipelinePool(); pp != nil && n > pp.Size() {
				return nil, fmt.Errorf(
					"redis: AutoPipelineOptions.NumShards=%d needs a pipeline pool of at "+
						"least %d connections (each full-duplex engine holds one), but "+
						"PipelinePoolSize gives %d; raise Options.PipelinePoolSize or lower "+
						"NumShards", n, n, pp.Size())
			}
		}
		// One flusherless shard, exactly as the single-engine path does: no
		// half-duplex flusher runs and enqueue never does a %0, even though
		// submit routes past it entirely.
		nShards = 1
	}
	// Publish the EFFECTIVE full-duplex state, not the requested one: FullDuplex
	// engages on a standalone *Client with a pipeline pool (fdOn) or on a
	// *ClusterClient whose node clients have pipeline pools (clusterFDOn, via the
	// per-node router). On a client with no pipeline pool it is a no-op and the
	// engine falls back to the half-duplex shard flushers. Config() promises what
	// the engine actually runs, so a requested-but-inactive FullDuplex must report
	// false rather than claim a mode the instance is not in. Only Config() reads
	// this after here.
	ap.config.FullDuplex = fdOn || clusterFDOn

	ap.shards = make([]*apShard, nShards)
	for i := range ap.shards {
		permits := perShard
		if i < remainder {
			permits++
		}
		// Stripe when reordering is impossible or waived: a BLOCKING caller
		// waits for each command before issuing its next, so its per-goroutine
		// order holds no matter which stripe each command lands in; the async
		// face may only stripe when the user set Unordered. The remaining case
		// (async, ordered) keeps one stripe to preserve strict submit order.
		nStripes := 1
		if config.Unordered || blocking {
			nStripes = apEnqueueStripes
		}
		s := &apShard{
			ap:      ap,
			notify:  make(chan struct{}, 1),
			stripes: make([]apStripe, nStripes),
			sem:     internal.NewFIFOSemaphore(int32(permits)),
		}
		for j := range s.stripes {
			// In full-duplex mode submissions go straight to the FD engine (fd.ch),
			// or — on a cluster — to a per-node FD child (clusterFD); the shard
			// queues are never enqueued to and no flusher drains them, so do NOT
			// preallocate them to MaxBatchSize. Otherwise a large MaxBatchSize with
			// a small FullDuplexWindow would allocate MaxBatchSize slots per stripe
			// (times apEnqueueStripes on the blocking face) up front — tens of MB or an
			// OOM before any command is sent. A nil queue is safe: nothing appends to
			// it while a full-duplex engine is active, and Len reads the atomic
			// counter, not the slice.
			if !fdOn && !clusterFDOn {
				s.stripes[j].queue = getQueueSlice(config.MaxBatchSize)
			}
			s.stripes[j].curBatch = newAPBatch()
		}
		ap.shards[i] = s
		if !fdOn && !clusterFDOn {
			ap.wg.Add(1)
			go s.flusher()
		}
	}

	if fdOn {
		// NumShards>1 runs N engines on THIS client, each leasing its own
		// connection from the pipeline pool. fds[0] is fd, so nothing
		// downstream needs to know the difference when N==1.
		n := fdShardCount(config)
		ap.fds = make([]*fdEngine, 0, n)
		for i := 0; i < n; i++ {
			e := newFDEngine(ap, fdClient)
			ap.fds = append(ap.fds, e)
			ap.wg.Add(1)
			go e.run()
		}
		ap.fd = ap.fds[0]
	}
	if clusterFDOn {
		ap.clusterFD = newClusterFDRouter(ap, clusterFDCC, config, blocking)
	}

	return ap, nil
}

// Do executes a raw command on a NORMAL connection, outside the pipeline.
// Arbitrary command names can carry connection state (SELECT, MULTI, SUBSCRIBE,
// CLIENT ...) or block the connection (BLPOP ...); batching those onto a shared
// pipeline connection would silently poison it for every later batch, or stall
// unrelated commands. (Submit enforces the same rule for raw Cmders: names in
// the connection-hostile set are diverted off the pipeline automatically.)
// The typed surface (ap.Set, ap.Get, ...) is safe by
// construction and IS batched — prefer it. Do carries the same caveats as
// Client.Do: a stateful command still affects the (normal, non-pipeline)
// pooled connection it runs on. Do keeps each face's call shape: on
// a blocking autopipeliner the call blocks until the command has executed; on a
// deferred (async) one it returns immediately and the command's result
// accessors (Err/Val/Result) block until it completes.
func (ap *AutoPipeliner) Do(ctx context.Context, args ...interface{}) *Cmd {
	cmd := NewCmd(ctx, args...)
	if len(args) == 0 {
		cmd.SetErr(errDoNoArgs)
		return cmd
	}
	if ap.isClosed() {
		cmd.SetErr(ErrClosed)
		return cmd
	}

	// Both faces go through runOutsidePipeline: it applies the divert
	// registration gate, so Close cannot conclude "nothing in flight" while an
	// accepted raw command — a blocking one on the blocking face runs inline on
	// the caller's goroutine — is still holding a pooled connection.
	_ = ap.runOutsidePipeline(ctx, cmd)
	return cmd
}

// runOutsidePipeline executes an escape-hatch command (Do, DoRaw,
// DoRawWriteTo) on a normal pooled connection, outside the batching engine,
// following the face's call shape. Blocking face: synchronous Process.
// Deferred face: returns-immediately — the command runs on a background
// goroutine and a ready batch makes its result accessors block until it
// completes. The batch completes at the innermost seam (under the user
// hooks) so a ProcessHook reading the result cannot self-deadlock; the
// deferred close is the panic backstop. Tracked by divertWg under divertMu,
// so Close waits for accepted diverted work (bounded — see Close) instead of
// returning while it still holds a pooled connection.
func (ap *AutoPipeliner) runOutsidePipeline(ctx context.Context, cmd Cmder) *apBatch {
	if ap.blocking {
		// The blocking face runs it inline, so the caller's own goroutine holds
		// the connection; still take the gate so Close cannot decide "nothing
		// in flight" while this command is executing.
		ap.divertMu.Lock()
		if ap.isClosed() {
			ap.divertMu.Unlock()
			cmd.SetErr(ErrClosed)
			return completedBatch
		}
		// Counts against MaxQueuedCommands too: it holds a pooled connection
		// until it completes.
		if !ap.admitQueued() {
			ap.divertMu.Unlock()
			cmd.SetErr(ErrAutoPipelineQueueFull)
			return completedBatch
		}
		ap.divertWg.Add(1)
		ap.divertMu.Unlock()
		defer ap.divertWg.Done()
		defer ap.releaseQueued(1)
		_ = ap.pipeliner.Process(ctx, cmd)
		return completedBatch
	}
	// Register under divertMu with a closed re-check, so registration and the
	// close transition cannot interleave (see the divertMu comment). A command
	// that loses the race is rejected here rather than running after Close.
	// The gate comes BEFORE setReady: publishing the fresh batch first and then
	// rejecting would leave the command gated on a batch nobody ever closes,
	// hanging every accessor.
	ap.divertMu.Lock()
	if ap.isClosed() {
		ap.divertMu.Unlock()
		cmd.SetErr(ErrClosed)
		cmd.setReady(completedBatch)
		return completedBatch
	}
	// Each diverted command holds a goroutine until it completes, so it counts
	// against MaxQueuedCommands like a queued one.
	if !ap.admitQueued() {
		ap.divertMu.Unlock()
		cmd.SetErr(ErrAutoPipelineQueueFull)
		cmd.setReady(completedBatch)
		return completedBatch
	}
	b := newAPBatch()
	cmd.setReady(b)
	ap.divertWg.Add(1)
	ap.divertMu.Unlock()
	go func() {
		defer ap.divertWg.Done()
		defer b.close()
		defer ap.releaseQueued(1)
		defer recoverDispatchPanic([]Cmder{cmd})
		if ap.armSelfDeadlockGuard() {
			b.dispGid.Store(curGoroutineID())
		}
		// A hook that returns nil WITHOUT calling next has short-circuited
		// SUCCESSFULLY (it served the command itself); plain Client hooks may do
		// that, so nothing here synthesizes an error for it — see dispatchCmds.
		err := ap.pipeliner.withProcessHook(ctx, cmd, func(ctx context.Context, cmd Cmder) error {
			return ap.pipeliner.process(ctx, cmd)
		})
		// The chain's final verdict, exactly like Client.Process — recorded
		// before the deferred close wakes the reader, so short-circuits,
		// post-next rewrites and suppressions are all honored.
		cmd.SetErr(err)
	}()
	return b
}

// DoRaw mirrors Do for raw RESP access: AutoPipeliner embeds cmdable, so
// without this override DoRaw would ride the batching engine — but raw
// commands carry Do's caveats and DoRawWriteTo-style streaming must not run
// inside a shared batch's reply loop. Runs outside the pipeline, following
// the face's call shape (see Do).
func (ap *AutoPipeliner) DoRaw(ctx context.Context, args ...interface{}) *RawCmd {
	cmd := NewRawCmd(ctx, args...)
	if len(args) == 0 {
		cmd.SetErr(errDoNoArgs)
		return cmd
	}
	if ap.isClosed() {
		cmd.SetErr(ErrClosed)
		return cmd
	}
	_ = ap.runOutsidePipeline(ctx, cmd)
	return cmd
}

// DoRawWriteTo mirrors Do for streamed raw RESP access (see DoRaw). On the
// deferred face the write to w happens when the command executes; use the
// result accessors (Err/Written) to wait before reading w.
func (ap *AutoPipeliner) DoRawWriteTo(ctx context.Context, w io.Writer, args ...interface{}) *RawWriteToCmd {
	cmd := NewRawWriteToCmd(ctx, w, args...)
	if len(args) == 0 {
		cmd.SetErr(errDoNoArgs)
		return cmd
	}
	if ap.isClosed() {
		cmd.SetErr(ErrClosed)
		return cmd
	}
	_ = ap.runOutsidePipeline(ctx, cmd)
	return cmd
}

// Process queues a command for autopipelined execution, following the
// autopipeliner's mode like the typed methods and Do: on a blocking
// autopipeliner the call blocks until the command has executed; on a deferred
// (async) one it returns immediately and reading the command's result
// (Val/Result/Err) blocks until its batch is flushed.
func (ap *AutoPipeliner) Process(ctx context.Context, cmd Cmder) error {
	return ap.cmdable(ctx, cmd)
}

// The methods below complete the UniversalClient surface by delegating to the
// underlying client. They are NOT autopipelined — pub/sub, transactions (Watch),
// hooks, Do and pool stats cannot be batched — so an AutoPipeliner used as a
// UniversalClient batches only the typed data commands; everything here runs on
// the underlying client exactly as it would there.
//
// Note on lifecycle: Close() (defined elsewhere) closes the AUTOPIPELINER —
// drains in-flight batches and stops flushers — but does NOT close the
// underlying client, whose lifecycle is owned by whoever created it.

// AddHook adds a hook to the underlying client. Autopipelined batches are hooked
// too, since dispatch goes through the hook-wrapped pipeline entry.
//
// Hook contract:
//   - Short-circuiting differs by dispatch mode. In half-duplex (batched) mode a
//     hook MAY return without calling next to skip the server — a supported pattern
//     for a mock or cache. In full-duplex mode the command is already queued on the
//     held connection before the hook runs, so returning without calling next does
//     NOT prevent the server write; a hook cannot cancel a full-duplex command.
//   - Do not call Close, or any other client control method, from inside a hook. A
//     hook runs on the engine's dispatch goroutine; in full-duplex mode a synchronous
//     Close from there blocks until the close backstop, because Close waits on the
//     very hook host it is running on. Trigger Close from a separate goroutine (see
//     the FullDuplex GoDoc).
//   - Do not panic. A panic in a batch hook is recovered so it cannot crash the
//     process, but the affected batch fails.
//   - Do not mutate client or connection state.
func (ap *AutoPipeliner) AddHook(hook Hook) { ap.pipeliner.AddHook(hook) }

// The four commands below have CLUSTER-WIDE overrides on ClusterClient
// (DBSize sums every master, the Script commands fan out to every shard).
// The embedded generic cmdable would route them as ordinary keyless commands
// to one picked shard — partial results, scripts missing on other shards —
// so they delegate to the underlying client instead of batching. On a
// standalone client the delegation is semantically identical to the generic
// path; these are rare admin/script-management commands, not data-path.

// DBSize delegates to the underlying client (cluster-wide sum on ClusterClient).
func (ap *AutoPipeliner) DBSize(ctx context.Context) *IntCmd {
	return ap.pipeliner.DBSize(ctx)
}

// ScriptLoad delegates to the underlying client (loads every shard on ClusterClient).
func (ap *AutoPipeliner) ScriptLoad(ctx context.Context, script string) *StringCmd {
	return ap.pipeliner.ScriptLoad(ctx, script)
}

// ScriptFlush delegates to the underlying client (flushes every shard on ClusterClient).
func (ap *AutoPipeliner) ScriptFlush(ctx context.Context) *StatusCmd {
	return ap.pipeliner.ScriptFlush(ctx)
}

// ScriptExists delegates to the underlying client (ANDs results across shards
// on ClusterClient).
func (ap *AutoPipeliner) ScriptExists(ctx context.Context, hashes ...string) *BoolSliceCmd {
	return ap.pipeliner.ScriptExists(ctx, hashes...)
}

// HImportPrepare, HImportDiscard and HImportDiscardAll are the remaining
// cluster-wide overrides (see the delegation note above): ClusterClient fans
// them out to every master and updates the shared fieldset registry, so
// running them on a single routed node would let a later HImportSet for a key
// on another master fail with "no such fieldset". TestAPDelegatesClusterWideOverrides
// fails if a future ClusterClient override is added without a delegate here.
func (ap *AutoPipeliner) HImportPrepare(ctx context.Context, fieldsetName string, fields ...string) *StatusCmd {
	return ap.pipeliner.HImportPrepare(ctx, fieldsetName, fields...)
}

func (ap *AutoPipeliner) HImportDiscard(ctx context.Context, fieldsetName string) *IntCmd {
	return ap.pipeliner.HImportDiscard(ctx, fieldsetName)
}

func (ap *AutoPipeliner) HImportDiscardAll(ctx context.Context) *IntCmd {
	return ap.pipeliner.HImportDiscardAll(ctx)
}

// Watch runs a transactional function on the underlying client (not batched).
func (ap *AutoPipeliner) Watch(ctx context.Context, fn func(*Tx) error, keys ...string) error {
	return ap.pipeliner.Watch(ctx, fn, keys...)
}

// Subscribe opens a pub/sub on the underlying client (not batched — pub/sub
// needs a dedicated connection).
func (ap *AutoPipeliner) Subscribe(ctx context.Context, channels ...string) *PubSub {
	return ap.pipeliner.Subscribe(ctx, channels...)
}

// PSubscribe opens a pattern pub/sub on the underlying client (not batched).
func (ap *AutoPipeliner) PSubscribe(ctx context.Context, channels ...string) *PubSub {
	return ap.pipeliner.PSubscribe(ctx, channels...)
}

// SSubscribe opens a sharded pub/sub on the underlying client (not batched).
func (ap *AutoPipeliner) SSubscribe(ctx context.Context, channels ...string) *PubSub {
	return ap.pipeliner.SSubscribe(ctx, channels...)
}

// PoolStats returns the underlying client's connection pool statistics.
func (ap *AutoPipeliner) PoolStats() *PoolStats { return ap.pipeliner.PoolStats() }

// AutoPipeline delegates to the underlying client, which returns its cached
// autopipeliner (typically this same instance). Present to satisfy the
// UniversalClient surface.
func (ap *AutoPipeliner) AutoPipeline() (*AutoPipeliner, error) {
	return ap.pipeliner.AutoPipeline()
}

// AutoPipelineWithOptions delegates to the underlying client.
func (ap *AutoPipeliner) AutoPipelineWithOptions(config *AutoPipelineOptions) (*AutoPipeliner, error) {
	return ap.pipeliner.AutoPipelineWithOptions(config)
}

// AsyncAutoPipeline delegates to the underlying client. Present to satisfy the
// UniversalClient surface.
func (ap *AutoPipeliner) AsyncAutoPipeline() (*AutoPipeliner, error) {
	return ap.pipeliner.AsyncAutoPipeline()
}

// AsyncAutoPipelineWithOptions delegates to the underlying client.
func (ap *AutoPipeliner) AsyncAutoPipelineWithOptions(config *AutoPipelineOptions) (*AutoPipeliner, error) {
	return ap.pipeliner.AsyncAutoPipelineWithOptions(config)
}

// AutoFuture is the handle returned by Submit. Call Wait (or Result on the
// command after Wait) once the result is needed; it blocks only until the
// command's batch has executed.
type AutoFuture struct {
	cmd   Cmder
	batch *apBatch
}

// Wait blocks until the submitted command has executed, then returns its error.
// The zero AutoFuture (no submitted command) returns an error rather than
// panicking.
func (f AutoFuture) Wait() error {
	if f.batch == nil {
		if f.cmd != nil {
			return f.cmd.Err()
		}
		return errZeroAutoFuture
	}
	select {
	case <-f.batch.done:
	default:
		// Same self-deadlock guard as baseCmd.await(): a pipeline hook on
		// the batch's own dispatch goroutine waiting a future pre-next()
		// would block a channel only its goroutine can close. Give it the
		// not-yet-executed view instead.
		if f.batch.isExecutorGoroutine() {
			return f.cmd.rawErr()
		}
		<-f.batch.done
	}
	return f.cmd.Err()
}

// WaitContext is like Wait but stops waiting when ctx is done. The command
// still executes and its result remains readable once its batch completes —
// ctx abandons only this wait, it does not cancel the command (per-command
// contexts are not honored after enqueue; see the AutoPipeliner doc).
//
// After a ctx error the result may simply not be there YET: the batch is
// still in flight and may populate the command at any moment, so do not read
// Cmd()'s value or error directly — that races the executing batch. Call Wait
// (or WaitContext with a fresh context) again; once it returns a non-context
// error, the command's result is complete and safe to read.
func (f AutoFuture) WaitContext(ctx context.Context) error {
	if f.batch == nil {
		if f.cmd != nil {
			return f.cmd.Err()
		}
		return errZeroAutoFuture
	}
	select {
	case <-f.batch.done:
		return f.cmd.Err()
	default:
		if f.batch.isExecutorGoroutine() {
			return f.cmd.rawErr() // see Wait: dispatch-goroutine self-deadlock guard
		}
	}
	select {
	case <-f.batch.done:
		return f.cmd.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Cmd returns the underlying command (call Wait first before reading results).
func (f AutoFuture) Cmd() Cmder { return f.cmd }

// outsidePipelineCommands lists commands that must never ride a SHARED
// pipeline connection. SHUTDOWN terminates the server before replying (its
// batchmates would all fail with EOF and the batch would retry against a
// dead server); MONITOR rebinds the connection into a monitor stream,
// desyncing every reply behind it; the rest change per-connection state
// (database, auth, protocol, transaction, subscription mode) that would
// leak to every unrelated caller sharing the pipeline conn afterwards. The
// typed surface cannot produce most of the stateful ones (they live on
// statefulCmdable) — but ReadOnly/ReadWrite ARE on cmdable, and raw
// Submit/Do accept any Cmder. Diverted commands execute directly on their
// own pooled connection — the same semantics (including the same footguns)
// as plain Client.Do.
var outsidePipelineCommands = map[string]struct{}{
	"shutdown": {}, "monitor": {},
	"select": {}, "auth": {}, "hello": {}, "reset": {}, "quit": {},
	"multi": {}, "exec": {}, "discard": {}, "watch": {}, "unwatch": {},
	"subscribe": {}, "unsubscribe": {}, "psubscribe": {}, "punsubscribe": {},
	"ssubscribe": {}, "sunsubscribe": {},
	"client": {},
	// Connection-scoped cluster state: queued onto a shared pipeline conn
	// they would leak replica-reads (or a pending redirect) to every later
	// batch on that conn.
	"readonly": {}, "readwrite": {}, "asking": {},
}

func runsOutsidePipeline(name string) bool {
	_, ok := outsidePipelineCommands[name]
	return ok
}

// blockingCommands are commands that park on the server until data arrives or
// their own timeout expires. The TYPED helpers set a per-command read timeout
// (see cmdable.BLPop), which submit already diverts on; a RAW Cmder built by
// hand — NewCmd(ctx, "blpop", key, 0) via Submit/Process/Do — carries no such
// marker, so without this set it would be queued onto a shared pipeline
// connection and hold the whole batch for the block duration.
// Derived from the typed helpers rather than guessed: every cmdable method that
// calls cmd.setReadTimeout parks the connection, so
//
//	grep -rn 'setReadTimeout' --include='*.go' . | grep -v _test
//
// enumerates exactly the wire names that belong here (the arg-driven ones are
// handled in isBlockingCmd instead). Re-run that grep when adding a blocking
// command.
var blockingCommands = map[string]struct{}{
	"blpop": {}, "brpop": {}, "brpoplpush": {},
	"blmove": {}, "blmovem": {}, "blmpop": {},
	"bzpopmin": {}, "bzpopmax": {}, "bzmpop": {},
	"wait": {}, "waitaof": {},
	// MIGRATE blocks the source instance for up to its timeout.
	"migrate": {},
}

// isHImportCmd reports whether cmd is an HIMPORT command
// (PREPARE/SET/DISCARD/DISCARDALL): a managed one — the himportCmder marker, the
// same predicate himportInjectedCmds uses to spot HIMPORT in a batch — OR a raw
// one built with NewCmd/NewStatusCmd(ctx, "himport", ...), matched by name. The
// raw form carries no marker, but it rides the same connection-session state (a
// PREPARE registered on the physical connection) that the full-duplex writer
// never injects, so after a session recycle, handoff or reconnect a raw HIMPORT
// SET would land on a connection whose PREPARE never ran and fail with "no such
// fieldset" (codex on #4002). Divert it off the shared pipe like the managed
// form: Process runs it on a pooled connection, as a plain client would. Used
// only on the full-duplex path (see submit).
func isHImportCmd(cmd Cmder) bool {
	if _, ok := cmd.(himportCmder); ok {
		return true
	}
	return cmd.Name() == "himport"
}

// isBlockingCmd reports whether cmd parks the connection. XREAD/XREADGROUP are
// decided by ARGUMENTS, not by name: only the BLOCK form blocks, and
// blanket-diverting the (far more common) non-blocking form would drop it out
// of batching for nothing.
func isBlockingCmd(cmd Cmder) bool {
	name := cmd.Name()
	if _, ok := blockingCommands[name]; ok {
		return true
	}
	// Arg-driven: these block only in their BLOCK form, and blanket-diverting
	// the far more common non-blocking form would drop it out of batching for
	// nothing. TS.READ takes BLOCK the same way (see TSReadWithArgs).
	args := cmd.Args()
	switch name {
	case "xread":
		return blockOptionBeforeStreams(args, 1)
	case "xreadgroup":
		// args[1:4] are the mandatory GROUP keyword plus the group and
		// consumer names. Those names are arbitrary values — a consumer
		// literally named "streams" must not be mistaken for the STREAMS
		// terminator (which would hide a real, later BLOCK and let the
		// command ride the shared pipe — cursor bugbot on #4002) — so skip
		// them positionally rather than matching on value.
		return blockOptionBeforeStreams(args, 4)
	case "ts.read":
		// TS.READ has no STREAMS terminator, but it DOES have two fixed
		// positional args before the option section: args[1] is the key and
		// args[2] is the timestamp (see TSReadWithArgs), and either can be
		// arbitrary user data (e.g. a key literally named "block"). Scanning
		// from args[0] would mistake that key for the BLOCK option and divert
		// a non-blocking TS.READ off the ordered pipe (codex on #4002).
		if len(args) < 3 {
			return false
		}
		for _, arg := range args[3:] {
			if internal.ToLower(blockingArgString(arg)) == "block" {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// blockOptionBeforeStreams scans the option section of XREAD/XREADGROUP
// (COUNT/MAXCOUNT/MAXSIZE/BLOCK/NOACK/CLAIM, in any combination) for the
// BLOCK keyword, stopping at the STREAMS keyword that always terminates the
// option section. start must already be past any positional arguments
// (XREADGROUP's GROUP clause) that cannot be told apart from a keyword by
// value alone. Match the token the way the encoder does: a raw Cmder may
// carry RESP tokens as []byte or *string (see baseCmd.stringArg), and a type
// switch on string alone would let NewCmd(ctx, "xread", []byte("BLOCK"), 0,
// ...) be batched onto a shared connection.
func blockOptionBeforeStreams(args []interface{}, start int) bool {
	if start > len(args) {
		return false
	}
	for _, arg := range args[start:] {
		switch internal.ToLower(blockingArgString(arg)) {
		case "streams":
			return false
		case "block":
			return true
		}
	}
	return false
}

// blockingArgString renders a command argument as the string the encoder will
// write for the token comparisons above. Only the forms that can carry a RESP
// keyword are handled; anything else cannot be the BLOCK token.
func blockingArgString(arg interface{}) string {
	switch v := arg.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case *string:
		if v == nil {
			return ""
		}
		return *v
	default:
		return ""
	}
}

// submit queues a command without blocking and returns its completion future.
func (ap *AutoPipeliner) submit(ctx context.Context, cmd Cmder) AutoFuture {
	// finish marks the command ready on the deferred face so its result
	// accessors (Val/Result/Err) self-gate through await() — whether the
	// caller goes through the typed surface or raw Submit. Reading a
	// Submit()-ed command before Wait() was previously a silent data race
	// with the dispatch goroutine. The blocking face deliberately never
	// carries a batch: its callers only regain control after execution, and
	// the dispatcher-gid deadlock guard relies on that.
	finish := func(f AutoFuture) AutoFuture {
		if !ap.blocking {
			cmd.setReady(f.batch)
		}
		return f
	}
	// Decide DIVERSION first. The cluster preflight rejects commands whose
	// request policy cannot ride a pipeline (ReqAllNodes/ReqAllShards), but a
	// diverted command never rides one: it goes through the underlying
	// Client/ClusterClient.Process, which performs the normal cluster-wide
	// fan-out and aggregation. Running the preflight first therefore rejected
	// commands that would have worked — typed WAIT/WAITAOF on a cluster with
	// command policies enabled (review finding by codex on #3942).
	diverted := cmd.readTimeout() != nil || runsOutsidePipeline(cmd.Name()) || isBlockingCmd(cmd) ||
		(ap.mustDivert != nil && ap.mustDivert(ctx, cmd)) ||
		// HIMPORT — managed or raw (see isHImportCmd) — rides connection-session
		// state (the registered PREPARE) that the full-duplex writer never injects,
		// so an HIMPORT SET on the FD pipe can fail "no such fieldset". Divert it to
		// the normal Process path, which injects the PREPARE for the managed form
		// (and updates the registry) and runs a raw form on a pooled connection as a
		// plain client would. The half-duplex sharded path injects inline
		// (himportInjectedCmds) and stays on the pipeline. Cluster full-duplex
		// (clusterFD) routes to per-node FD children whose engines have the same
		// limitation, so divert there too.
		((ap.fd != nil || ap.clusterFD != nil) && isHImportCmd(cmd))
	if !diverted && ap.preflight != nil {
		if err := ap.preflight(ctx, cmd); err != nil {
			cmd.SetErr(err)
			return finish(AutoFuture{cmd: cmd, batch: completedBatch})
		}
	}
	if diverted {
		// Blocking commands (and the conn-hostile ones above) are executed
		// directly, outside the pipeline — via runOutsidePipeline, which
		// keeps each face's call shape: the blocking face runs the command
		// synchronously, the deferred face runs it on its own goroutine so
		// this call returns immediately and the result accessors block (a
		// BLPOP submitted on the async face must not stall the submitter,
		// exactly like Do). They still must respect a closed AutoPipeliner:
		// enqueue() rejects on the batched path, so mirror that here instead
		// of running after Close().
		if ap.isClosed() {
			cmd.SetErr(ErrClosed)
			return finish(AutoFuture{cmd: cmd, batch: completedBatch})
		}
		// runOutsidePipeline sets the command ready itself on the deferred
		// face; the returned batch completes when the command has executed.
		return AutoFuture{cmd: cmd, batch: ap.runOutsidePipeline(ctx, cmd)}
	}
	// No finish here: enqueue stamps ready under the stripe lock, before the
	// command is visible to any drain (the error paths above still go through
	// finish for uniform accessor behavior).
	if ap.clusterFD != nil {
		// Cluster full-duplex: route to the per-node FD child that owns this
		// command's slot. The child is a standalone FD autopipeliner sharing this
		// face's blocking flag, so its submit honors the same setReady/blocking
		// contract — return its AutoFuture directly.
		return ap.clusterFD.submit(ctx, cmd)
	}
	if ap.fd != nil {
		// Ordered full-duplex: stream on one held connection. enqueue's async
		// setReady is replicated here since we bypass it. ctx is threaded so a
		// per-command process-hook host can parent its span correctly.
		b := ap.fdFor(cmd).submit(ctx, cmd)
		if !ap.blocking {
			cmd.setReady(b)
		}
		return AutoFuture{cmd: cmd, batch: b}
	}
	return AutoFuture{cmd: cmd, batch: ap.enqueue(cmd)}
}

// ErrSubmitBlockingFace rejects Submit on the blocking face: Submit does not
// wait, so a windowed caller could have several commands in flight at once —
// but the blocking face stripes its enqueue queue on the strength of every
// caller waiting per command, and a non-waiting window there can be reordered.
// The deferred face (AsyncAutoPipeline) is built for exactly that usage.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
var ErrSubmitBlockingFace = errors.New(
	"redis: Submit requires the deferred autopipeliner (AsyncAutoPipeline); on the blocking face use the typed methods or Do",
)

// errZeroAutoFuture is returned by Wait/WaitContext on a zero AutoFuture.
var errZeroAutoFuture = errors.New("redis: Wait on a zero AutoFuture")

// errDoNoArgs is returned by Do when called without a command.
var errDoNoArgs = errors.New("redis: AutoPipeliner.Do requires at least one argument")

// ErrAutoPipelineTimeout is set on drained commands when a flush could not
// obtain a batch permit within the engine's internal backstop — the engine is
// overloaded or an in-flight batch is wedged (e.g. read timeouts disabled on
// a dead peer). It is deliberately NOT context.DeadlineExceeded: the caller's
// own context did not expire, and errors.Is(err, context.DeadlineExceeded)
// must not fire for an internal engine timeout.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
var ErrAutoPipelineTimeout = errors.New(
	"redis: autopipeline: no batch permit within the internal backstop (engine overloaded or a batch is wedged)",
)

// ErrAutoPipelineQueueFull is set on a command rejected because the
// autopipeliner already holds AutoPipelineOptions.MaxQueuedCommands accepted
// commands. The command was not sent. The caller may retry it later.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
var ErrAutoPipelineQueueFull = errors.New(
	"redis: autopipeline: queue full (MaxQueuedCommands reached)",
)

// Submit queues a command without blocking and returns an AutoFuture; Wait on
// it when the result is needed. This is the explicit form for working with raw
// Cmders on the deferred (async) face, where the typed methods (Set, Get, ...)
// provide the same deferred behaviour returning the usual *XxxCmd. The
// command's own result accessors (Err/Val/Result) are safe to use instead of
// Wait — they block until the command has executed. Connection-hostile
// command names (SHUTDOWN, MONITOR, SELECT, AUTH, MULTI, SUBSCRIBE, CLIENT,
// ...) never ride a shared pipeline connection: they are diverted to a
// normal pooled connection with plain Client.Do semantics. On a BLOCKING
// autopipeliner Submit is rejected (the future's Wait returns an error): the
// blocking face's ordering relies on every caller waiting for each command
// before issuing the next, which Submit by design does not do.
func (ap *AutoPipeliner) Submit(ctx context.Context, cmd Cmder) AutoFuture {
	if ap.blocking {
		cmd.SetErr(ErrSubmitBlockingFace)
		return AutoFuture{cmd: cmd, batch: completedBatch}
	}
	return ap.submit(ctx, cmd)
}

// processAsync is the cmdable backing the typed command surface: it queues a
// command without blocking the caller and marks it ready so the command's
// result accessors (Val/Result/Err) block until the batch executes. This gives
// the autopipeliner the full typed surface (ap.Set, ap.Get, ...) with the exact
// same call shape as a normal client — only the wait is deferred to the point a
// result is read.
func (ap *AutoPipeliner) processAsync(ctx context.Context, cmd Cmder) error {
	// submit marks the command ready (see the finish closure there): a hook
	// that reads the command before that store lands sees a nil ready — the
	// non-blocking not-yet-executed view — while the caller always sees its
	// own store before any await.
	f := ap.submit(ctx, cmd)
	// Report SUBMIT-time rejections (a closed pipeliner, a cluster preflight
	// refusal): those paths set the error on the command and hand back the
	// shared completed batch without queueing anything, so returning nil made
	// Process claim success for a command that will never run — and callers
	// reaching the engine through UniversalClient.Process see only this return
	// value (review finding by codex on #3942). Execution errors are NOT
	// reported here: the deferred face's contract is that this call does not
	// wait, so those stay on the command for its accessors. rawErr keeps the
	// check non-blocking.
	if f.batch == completedBatch {
		return cmd.rawErr()
	}
	return nil
}

// processBlocking is the cmdable backing the blocking face: it queues the
// command and blocks until its batch has executed, so the command call has the
// same synchronous shape as a normal client (the returned *XxxCmd already holds
// its result). The flusher still batches this command with other concurrent
// callers' commands into a pipeline, so throughput is far above a plain client
// even though each caller waits. Per-goroutine ordering holds regardless of
// MaxConcurrentBatches: a caller cannot issue its next command until this one
// returns, so its commands execute in submit order.
func (ap *AutoPipeliner) processBlocking(ctx context.Context, cmd Cmder) error {
	f := ap.submit(ctx, cmd)
	err := f.Wait()
	// Recycle the pooled completion batch. After Wait the batch is complete and
	// unreferenced: the blocking face never installs it on the command (no
	// setReady), and the reader drops its fdReq — and with it the batch pointer —
	// as it advances past the just-completed command, so processBlocking is the
	// last holder. Gate on pooled (excludes completedBatch and every non-FD /
	// diverted / async path, all of which use newAPBatch) and dispGid==0
	// (insurance: a stamped dispatcher gid would mean an executor-goroutine Wait
	// path that can return without draining done).
	if b := f.batch; b != nil && b.pooled && b.dispGid.Load() == 0 {
		putFDBlockingBatch(b)
	}
	return err
}

// completedBatch is a reusable already-completed batch: returned both for
// commands that already executed directly (blocking commands, Submit-time
// rejections) and for error cases like enqueue-after-Close, so Wait returns
// immediately and the command's own error tells the story.
var completedBatch = func() *apBatch {
	b := newAPBatch()
	b.close()
	return b
}()

// enqueue queues a command and returns the batch whose done channel completes
// when it has executed. On a closed autopipeliner it errors the command and
// returns the already-closed batch.
// isClosed reports whether this pipeliner (or the shared pool set it rides
// on) has been closed. Two atomic loads; no locks.
//
// EVERY closed check that gates accepting new work must go through this, not
// ap.closed directly: a WithTimeout clone's Close sets only the shared flag,
// so a guard reading ap.closed alone would accept commands against pools that
// are already gone and surface pool-closed errors instead of ErrClosed.
// (Close's own CompareAndSwap on ap.closed is the one deliberate direct use:
// it claims the shutdown for this instance.)
func (ap *AutoPipeliner) isClosed() bool {
	return ap.closed.Load() || (ap.sharedClosed != nil && ap.sharedClosed.Load())
}

// admitQueued takes one MaxQueuedCommands slot. It reports false when the
// pipeliner is at its limit; the caller then rejects the command. Always true
// when no limit is set.
func (ap *AutoPipeliner) admitQueued() bool {
	if ap.maxQueued <= 0 {
		return true
	}
	if ap.queued.Add(1) > ap.maxQueued {
		ap.queued.Add(-1)
		return false
	}
	return true
}

// releaseQueued frees n slots taken by admitQueued. Call it before the
// commands' batch closes, so a woken caller can submit again at once.
func (ap *AutoPipeliner) releaseQueued(n int) {
	if ap.maxQueued > 0 {
		ap.queued.Add(-int64(n))
	}
}

// queueFull reports, without taking a slot, whether MaxQueuedCommands is
// already reached. enqueue checks it before cluster slot routing and sizing,
// so a rejected submit does not run user Args/String/MarshalBinary code.
// admitQueued still makes the final, atomic decision.
func (ap *AutoPipeliner) queueFull() bool {
	return ap.maxQueued > 0 && ap.queued.Load() >= ap.maxQueued
}

// rejectQueueFull fails cmd with ErrAutoPipelineQueueFull and accounts it as an
// arrival (see consumeExpectedArrival).
func (ap *AutoPipeliner) rejectQueueFull(cmd Cmder) *apBatch {
	if ap.consumeExpectedArrival() {
		// This rejection was the last expected arrival. A flusher waiting for
		// the wave only re-checks the count when woken, and a rejected command
		// never calls wake, so wake every shard (the count is pipeliner-wide):
		// the waiting one flushes now instead of after the silence fallback.
		for _, s := range ap.shards {
			s.wake()
		}
	}
	cmd.SetErr(ErrAutoPipelineQueueFull)
	return completedBatch
}

// consumeExpectedArrival accounts a rejected command as an arrival, so the
// flusher does not wait out the silence fallback for a resubmission that will
// never land. Unlike enqueue's plain decrement it never goes below zero: a
// caller retrying a rejection in a loop would otherwise push the counter far
// negative, and the next announced wave would vanish into that deficit. It
// reports whether it took the count from 1 to 0.
func (ap *AutoPipeliner) consumeExpectedArrival() bool {
	for {
		v := ap.expectedArrivals.Load()
		if v <= 0 {
			return false
		}
		if ap.expectedArrivals.CompareAndSwap(v, v-1) {
			return v == 1
		}
	}
}

func (ap *AutoPipeliner) enqueue(cmd Cmder) *apBatch {
	if ap.isClosed() {
		cmd.SetErr(ErrClosed)
		return completedBatch
	}

	// Already full: reject before slot routing and sizing run user code (see
	// queueFull).
	if ap.queueFull() {
		return ap.rejectQueueFull(cmd)
	}

	// Pick a shard. With shardFn (cluster mode) route by command content so all
	// commands for one node collect in the same shard's batch; otherwise spread
	// round-robin to keep each shard's mutex lightly contended.
	var s *apShard
	if ap.shardFn != nil {
		// uint conversion instead of negation: -math.MinInt overflows back to
		// itself and a negative modulo would panic the index. The unsigned
		// modulo is deterministic for every int, including MinInt.
		idx := ap.shardFn(cmd)
		s = ap.shards[uint(idx)%uint(len(ap.shards))]
	} else if len(ap.shards) == 1 {
		// Single shard (the standalone default): skip the round-robin counter —
		// it is a shared cache line bumped by every enqueue for a pick that is
		// constant. Same guard the stripe pick already has.
		s = ap.shards[0]
	} else {
		// Unsigned modulo: converting to int first goes negative after the
		// uint32 counter passes 2^31 on 32-bit platforms and panics.
		s = ap.shards[int((ap.next.Add(1)-1)%uint32(len(ap.shards)))]
	}

	// Size the command BEFORE taking the stripe lock, and panic-safely. Sizing
	// runs user code — cmd.Args() on a custom Cmder, and MarshalBinary on a
	// BinaryMarshaler argument — and MaxBatchBytes is on by default, so this is
	// on every enqueue. A panic while holding st.mu would never reach
	// st.mu.Unlock: the stripe stays locked, every later enqueue on it parks
	// forever, and Close can only time out (cursor bugbot + codex on #4002).
	// Outside the lock, a panic just fails this one command, exactly as the
	// full-duplex serve loop does with the same helper; nothing was queued.
	var cmdBytes int64
	if ap.config.MaxBatchBytes > 0 {
		n, err := cmdApproxBytesSafe(cmd)
		if err != nil {
			cmd.SetErr(err)
			return completedBatch
		}
		cmdBytes = n
	}

	if !ap.admitQueued() {
		return ap.rejectQueueFull(cmd)
	}

	st := s.stripe()
	st.mu.Lock()
	// Re-check closed under the stripe lock (see Close): either we win the lock
	// first and the shutdown drain flushes us, or the drain ran first and we
	// reject here — so a late enqueue never hangs on an unclosed done.
	if ap.isClosed() {
		st.mu.Unlock()
		ap.releaseQueued(1)
		cmd.SetErr(ErrClosed)
		return completedBatch
	}
	batch := st.curBatch
	if !ap.blocking {
		// Publish the gating batch BEFORE the command becomes visible to a
		// drain (the drain takes this same stripe lock): a flush racing the
		// submitter's return path must observe ready already set, or the
		// cluster node-executor registration would skip this command's batch
		// and a node hook reading the command mid-dispatch could block on a
		// batch its own call chain completes. The blocking face deliberately
		// never carries a batch (see submit).
		cmd.setReady(batch)
	}
	st.queue = append(st.queue, cmd)
	st.queueLen.Store(int32(len(st.queue)))
	if ap.config.MaxBatchBytes > 0 {
		st.queueBytes.Add(cmdBytes)
	}
	st.mu.Unlock()

	// One expected arrival has landed (see expectedArrivals).
	ap.expectedArrivals.Add(-1)

	s.wake()
	return batch
}

// wake signals the shard's flusher that work is available without blocking.
func (s *apShard) wake() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// IsBlocking reports which face this autopipeliner is: true for the blocking
// face (Client.AutoPipeline — calls wait for execution), false for the
// deferred face (AsyncAutoPipeline — calls return immediately and result
// accessors block). The two faces reject different usage (Submit is
// blocking-face-rejected), so code handed an *AutoPipeliner can branch on
// this instead of probing with errors.
func (ap *AutoPipeliner) IsBlocking() bool { return ap.blocking }

// Config returns a copy of the effective configuration (defaults filled in).
func (ap *AutoPipeliner) Config() AutoPipelineOptions {
	cfg := *ap.config
	// Strip internal-only fields. contentSharded is set by cluster wiring and
	// tells Validate that shards are slot-routed, so same-key commands cannot
	// be reordered — which exempts the config from the NumShards>1 ordering
	// requirement. Handing that bit back to a caller who copies this config
	// into a STANDALONE async autopipeliner would silence that check for
	// round-robin shards, which really do flush concurrently and really do
	// break submit order (review finding by codex on #3942).
	cfg.contentSharded = false
	// Same hazard for clusterReprocess: it holds a live ClusterClient.process
	// closure set by the cluster FD router. Round-tripping this config into a
	// STANDALONE *Client would carry that closure onto an unrelated client's FD
	// engine (flipping redirectAware on and routing its retries through the wrong
	// ClusterClient). Strip it.
	cfg.clusterReprocess = nil
	// clusterRetryBudget is internal cluster-FD wiring (the ClusterClient's
	// MaxRedirects, used as the child engine's recovery budget); it has no meaning
	// on a config a caller copies into a standalone client, so strip it too.
	cfg.clusterRetryBudget = 0
	return cfg
}

// IsClosed reports whether the AutoPipeliner has been closed, either by an
// explicit Close or by closing the owning client. A closed AutoPipeliner
// rejects new commands with ErrClosed.
func (ap *AutoPipeliner) IsClosed() bool {
	return ap.isClosed()
}

// numShards reports how many shards this autopipeliner runs.
func (ap *AutoPipeliner) numShards() int { return len(ap.shards) }

// setShardFn installs a content-based shard selector. In cluster mode it maps
// a command's SLOT to a shard, which is a batch-depth heuristic, not an
// invariant: slot ranges are assigned to shards proportionally, so when a
// node's slots are non-contiguous one shard's batch can still span nodes and
// mapCmdsByNode splits it (correctness is unaffected — that router resolves
// every command's own slot — but those per-node pipelines are shallower).
// What the mapping DOES guarantee is that a given key always lands on the same
// shard, so a caller's relative order for that key is preserved regardless of
// how the shard's batch is split. Must be called before the autopipeliner is
// used. Not safe to change concurrently with enqueues.
func (ap *AutoPipeliner) setShardFn(fn func(Cmder) int) { ap.shardFn = fn }

// setPreflight installs a submit-time command filter (cluster wiring rejects
// commands whose request policy cannot ride a pipeline). Called once during
// construction, before the AutoPipeliner is published.
func (ap *AutoPipeliner) setPreflight(fn func(ctx context.Context, cmd Cmder) error) {
	ap.preflight = fn
}

// setMustDivert installs a predicate that forces a command off the batching
// path (see the mustDivert field). Called once during construction, before the
// AutoPipeliner is published.
func (ap *AutoPipeliner) setMustDivert(fn func(ctx context.Context, cmd Cmder) bool) {
	ap.mustDivert = fn
}

// Close stops the autopipeliner and flushes any pending commands. Worst
// case it blocks up to the internal permit backstop (~30s) PER SHARD if
// in-flight batches are wedged (e.g. read timeouts disabled against a dead
// peer) — healthy shutdowns take one round trip per shard with commands
// queued, near-zero otherwise.
func (ap *AutoPipeliner) Close() error {
	if !ap.closed.CompareAndSwap(false, true) {
		// Another Close already claimed the shutdown. Return immediately and do
		// NOT wait for its drain: a re-entrant Close from an in-flight dispatch
		// (a batch's hook calling Close on its own executor goroutine) runs on a
		// goroutine the winner's drain is itself waiting for, so waiting here
		// would self-wait until the backstop. Callers that must observe the
		// drain complete before acting on "closed" — e.g. a wrapping client's
		// Close, before it tears down shared connection pools — call WaitClosed
		// after Close instead.
		return nil
	}
	// Winner: run the drain, publish its result, and release any WaitClosed
	// waiters exactly once (even if the drain panics).
	defer close(ap.closeDone)

	// Detach this engine's shared-pool close hook, but only AFTER the drain: a
	// pool-sharing wrapper that closes the shared pool concurrently with this Close
	// must still find the hook registered and block on this engine's shutdown flush
	// (via the hook's cancelAndDrain, serialized by drainOnce) before the pools are
	// torn down. Unregistering first would let the pool close race ahead of our
	// flush and tear the pools down mid-write. Detach via defer so a drain panic
	// does not leave the per-engine callback lingering in the shared onClose
	// registry (bounded registrations; no stale cancel on a later sharer close).
	// Only the full Close detaches — the hook's own cancelAndDrain path deliberately
	// leaves ap.closed false, so a later owner Close still reaches here.
	if ap.closeHooks != nil {
		defer ap.closeHooks.unregister(ap.closeHookID)
	}
	return ap.cancelAndDrain()
}

// cancelAndDrain cancels the engine and waits (bounded by the drainAll backstop)
// for its flushers, shutdown flush, and final shard sweep — WITHOUT flipping
// ap.closed or detaching the close hook. The shared-pool close hook uses this: when
// a pool-sharing clone closes the shared pools, this engine's shutdown flush must
// finish BEFORE closeResources tears the pools down, yet ap.closed must stay false
// (the engine is then rejected via the shared-closed flag, and a later owner Close
// still runs the full teardown). Close layers the closed-CAS + hook detach on top.
func (ap *AutoPipeliner) cancelAndDrain() error {
	// Run the cancel+drain body exactly ONCE, even when two closers reach it for the
	// same engine — an explicit AutoPipeliner.Close racing a pool-sharing wrapper's
	// shared-pool close hook (the hook path deliberately leaves ap.closed false, so
	// the closed-CAS does not serialize the two). Two concurrent drains are unsafe:
	// drainAll orders its stages flushers -> shard sweep -> batchWg.Wait precisely so
	// every batchWg.Add the sweep issues happens-before the Wait; interleaved, one
	// drain's batchWg.Wait can run while the other's sweep is still dispatching and
	// calling Add — "sync: WaitGroup misuse: Add called concurrently with Wait", a
	// runtime panic. The Once also hands both callers the SAME close error and avoids
	// a duplicate, spuriously-logged shutdown-permit acquisition. Once.Do blocks the
	// loser until the winner's drain finishes and publishes closeErr (single writer,
	// inside the Once, so WaitClosed reads it race-free), so this is safe without an
	// extra channel. A single sweep is a
	// sufficient barrier against late enqueues: whichever caller wins has already set
	// the flag enqueue checks (Close sets ap.closed, the hook sets sharedClosed)
	// before reaching here, so the sweep still closes the lost-command race.
	ap.drainOnce.Do(func() { ap.closeErr = ap.drainBody() })
	return ap.closeErr
}

// drainBody is cancelAndDrain's actual cancel+drain work, invoked exactly once via
// drainOnce. Split out only so the once wrapper stays trivial.
func (ap *AutoPipeliner) drainBody() error {
	ap.drainRuns.Add(1)

	// Cancel context to stop flushers
	ap.cancel()

	// Wake every shard's flusher so each observes the cancelled context promptly.
	for _, s := range ap.shards {
		s.wake()
	}

	// Cluster full-duplex: close the per-node FD children before the (empty) shard
	// sweep and before clusterNodes closes the node clients, so each child flushes
	// its accepted commands on the still-open node connection. Idempotent if a
	// child was already reaped by its node client's close hook.
	var clusterFDErr error
	if ap.clusterFD != nil {
		// Capture the child-drain error: a stalled/failed per-node child means
		// accepted commands were not flushed, which the caller must be able to
		// detect. Joined into closeErr below so it is not masked by the (empty)
		// shard sweep's nil result.
		clusterFDErr = ap.clusterFD.close()
	}

	// Pass through the divert gate once: by the time this runs the engine is
	// already rejecting new work (Close set ap.closed before calling here; the
	// shared-pool hook path has sharedClosed set before onClose runs), so any
	// diverted registration either completed before this (the counter already sees
	// it) or observes closed/shared-closed and rejects. Without this handshake the
	// wait below could read a zero counter while a diverted command was between its
	// closed check and its Add.
	ap.divertMu.Lock()
	ap.divertMu.Unlock() //nolint:staticcheck // handshake, not a critical section

	// Drain everything that remains, BOUNDED AS ONE UNIT: the flusher exit, the
	// final shard sweep, and the batch/diverted dispatch waits.
	//
	// None of it can be cancelled: commands taken from a queue (or accepted for
	// diverted execution) were already ACCEPTED, and Close's contract is to
	// flush them, so ap.cancel() deliberately does not reach an in-flight
	// dispatch. With ReadTimeout disabled — a supported configuration — a
	// stalled read against a dead peer, or a diverted BLPOP with a zero
	// timeout, has nothing to end it. Bounding only the LAST wait would not
	// help: the wedged dispatch can just as easily sit in a flusher that
	// ap.wg.Wait() is waiting for, or in the shutdown sweep's own dispatch, so
	// Close would hang before ever reaching the bound it documents (review
	// finding by codex on #3942). On expiry, report what is still outstanding
	// instead of blocking the caller: the engine is already closed to new work,
	// and the leaked goroutines end when the server or the OS breaks the
	// connection. See autoPipelineCloseBackstop for why the bound is generous.
	ap.closeErr = errors.Join(ap.drainAll(autoPipelineCloseBackstop), clusterFDErr)
	return ap.closeErr
}

// WaitClosed blocks until the Close that claimed the shutdown has finished its
// drain, then returns that drain's result. Close itself returns immediately for
// any caller that loses the shutdown CAS (so a re-entrant Close from an
// in-flight dispatch cannot self-wait); a caller that must not act on "closed"
// until accepted commands have been flushed calls Close and then WaitClosed.
// The canonical use is a wrapping client whose own Close tears down shared
// connection pools: Close, WaitClosed, then close the pools — so the pools are
// never torn down under the winning Close's in-flight drain. Call after Close;
// with no Close in progress it blocks until one happens.
func (ap *AutoPipeliner) WaitClosed() error {
	<-ap.closeDone
	return ap.closeErr
}

// drainAll runs Close's whole drain tail under a single bound and returns an
// error naming every stage that was still outstanding when it expired. Split
// out of Close so the bound is testable without a real stalled connection.
//
// The stages are ordered as Close needs them — the shard sweep must not start
// before the flushers are provably gone — but they are waited on
// CONCURRENTLY with the timer, which is the whole point: any stage can be the
// one that never finishes.
func (ap *AutoPipeliner) drainAll(timeout time.Duration) error {
	flushers := make(chan struct{})
	go func() { defer close(flushers); ap.wg.Wait() }()

	// swept: after the flushers are gone, drain each shard once more under its
	// lock. A command can pass enqueue's under-lock closed-recheck just before
	// Close's CompareAndSwap and append to a shard AFTER that shard's flusher
	// has already drained and exited — leaving its batch.done unclosed and the
	// caller's accessor blocked forever. s.mu serializes the two, so either the
	// late enqueue appends first and this sweep flushes it, or the sweep runs
	// first and the enqueue then observes closed==true and rejects.
	swept := make(chan struct{})
	go func() {
		defer close(swept)
		<-flushers
		for _, s := range ap.shards {
			s.flushBatchSliceShutdown()
		}
	}()

	batches := make(chan struct{})
	go func() {
		defer close(batches)
		<-swept
		ap.batchWg.Wait()
	}()

	diverted := make(chan struct{})
	go func() { defer close(diverted); ap.divertWg.Wait() }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	batchesDone, divertedDone := false, false
	for !batchesDone || !divertedDone {
		select {
		case <-batches:
			batchesDone = true
			batches = nil // a closed channel is always ready; stop selecting it
		case <-diverted:
			divertedDone = true
			diverted = nil
		case <-timer.C:
			var outstanding []string
			if !batchesDone {
				// Name the precise stage: a wedged flusher and a wedged batch
				// dispatch need different operator responses.
				select {
				case <-flushers:
					select {
					case <-swept:
						outstanding = append(outstanding, "batch dispatches")
					default:
						outstanding = append(outstanding, "the shutdown flush")
					}
				default:
					outstanding = append(outstanding, "the flusher drain")
				}
			}
			if !divertedDone {
				outstanding = append(outstanding, "diverted (blocking) commands")
			}
			return fmt.Errorf(
				"redis: autopipeline: Close timed out after %s with %s still in flight; "+
					"they hold pooled connections until the server or the OS ends them "+
					"(most often a blocking command with no timeout, or ReadTimeout disabled)",
				timeout, strings.Join(outstanding, " and "),
			)
		}
	}
	return nil
}

// flusher is the per-shard background goroutine that flushes batches.
func (s *apShard) flusher() {
	defer s.ap.wg.Done()
	ap := s.ap

	for {
		// Wait for a command to arrive (or shutdown). The notify channel is a
		// cheap buffered wake-up; no lock is taken on the hot enqueue path.
		if s.Len() == 0 {
			select {
			case <-s.notify:
			case <-ap.ctx.Done():
			}
		}

		// Check if context is cancelled
		if ap.ctx.Err() != nil {
			// Final flush before shutdown - use background context to avoid immediate cancellation
			s.flushBatchSliceShutdown()
			return
		}

		// Apply the coalescing window if one is configured (MaxFlushDelay /
		// AdaptiveDelay). With the default config this returns at once: batching
		// under concurrent load comes from in-flight backpressure, not a wait —
		// see accumulateBatch.
		s.accumulateBatch()

		// Flush all pending commands
		for s.Len() > 0 {
			select {
			case <-ap.ctx.Done():
				// Final flush before shutdown
				s.flushBatchSliceShutdown()
				return
			default:
			}

			s.flushBatchSlice()

			// Between batches, apply the configured window again so the next
			// pipeline is also full. A no-op with the default config (see
			// accumulateBatch); the next drain picks up whatever has queued.
			if s.Len() > 0 && s.Len() < ap.config.MaxBatchSize && !s.bytesFull() {
				s.accumulateBatch()
			}
		}
	}
}

// accumulateBatch lets commands pile up before the flusher drains the queue,
// so pipelines carry many commands instead of one. It returns as soon as any
// of these holds:
//
//   - the queue reaches MaxBatchSize (batch is full);
//   - a configured MaxFlushDelay / AdaptiveDelay window elapses; or
//   - with no configured window (the default), the expected resubmission
//     wave of arrivals has landed — see awaitExpectedArrivals.
//
// A configured MaxFlushDelay / AdaptiveDelay is an intentional accumulation
// window and is waited in full (AdaptiveDelay scales it down as the queue fills
// and returns 0 — flush now — once the queue is ≥75% full).
func (s *apShard) accumulateBatch() {
	ap := s.ap
	batchSize := ap.config.MaxBatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	if s.Len() >= batchSize || s.bytesFull() {
		return
	}

	// Pick the accumulation window. calculateDelay returns 0 both when no
	// MaxFlushDelay is configured (the default) and when AdaptiveDelay resolves
	// the current fill level to "flush immediately". The fill level is this
	// shard's own length — each shard flushes independently, so a global count
	// would mis-tune a quiet shard while another is busy.
	window := ap.calculateDelay(s.Len())
	if window <= 0 {
		if ap.config.MaxFlushDelay == 0 && !ap.config.AdaptiveDelay {
			// Default: coalesce by expected-arrival count, not by wall-clock.
			s.awaitExpectedArrivals(batchSize)
		}
		return
	}

	// Explicit window: wait the whole delay (or until the batch fills). Each
	// enqueue sends on notify, so we re-check the queue length on every wake-up
	// and return once the batch is full.
	deadline := time.NewTimer(window)
	defer deadline.Stop()
	for {
		select {
		case <-ap.ctx.Done():
			return
		case <-deadline.C:
			return
		case <-s.notify:
			if s.Len() >= batchSize || s.bytesFull() {
				return
			}
		}
	}
}

// silenceGapFloor / silenceGapCeil bound awaitExpectedArrivals's silence fallback.
// The floor covers fast links; the RTT-scaled value (execEWMA/8) takes over on
// slow ones, where a wakeup wave staggered by goroutine scheduling can pause
// longer than the floor mid-landing and a premature flush is expensive (each
// batch fragment occupies a pipeline connection for a full round trip). The
// ceiling bounds how long a stale expectation (callers that left) can delay a
// flush.
const (
	silenceGapFloor = 200 * time.Microsecond
	silenceGapCeil  = 2 * time.Millisecond
)

// coalesceMinFlush is the smallest pipeline worth dispatching while other
// batches are still executing. Below it, a gap-fire holds the queued
// stragglers for the next wave instead of burning a connection on a
// near-empty flush; once nothing is in flight, any size flushes immediately.
const coalesceMinFlush = 8

// stragglerHoldGaps bounds the straggler-hold WHEN the pipeline pool has a free
// connection (see awaitExpectedArrivals): at most this many silence gaps (each
// clamp(execEWMA/8, 200µs, 2ms), so the bound tracks the round trip) pass before
// queued stragglers flush. The old behavior re-armed until
// autoPipelinePermitBackstop — effectively until the in-flight batch's reply
// landed, ~1 RTT — so a straggler that enqueued behind an in-flight batch waited
// a full round trip BEFORE its own, i.e. every such op paid ~2x RTT (measured
// with a phase trace on a deterministic 50ms link: uncached p95 pinned at 2x RTT
// / 107ms at low-to-mid concurrency, straggler-hold avg == 1 RTT; the bound
// collapsed that to ~1 RTT / 62ms). The bound is applied ONLY when a pooled
// connection is idle or dial-able — when the pool is saturated the long hold is
// kept, because flushing tiny batches into a full pool thrashes it and cuts
// throughput (measured ~7x at a squeezed pool). The 30s
// autoPipelinePermitBackstop remains the absolute safety ceiling on the flush
// path itself (a wedged connection).
const stragglerHoldGaps = 3

// observeBatchExec folds one batch execution duration into execEWMA.
func (ap *AutoPipeliner) observeBatchExec(d time.Duration) {
	sample := int64(d)
	if sample <= 0 {
		return
	}
	old := ap.execEWMA.Load()
	if old == 0 {
		ap.execEWMA.Store(sample)
		return
	}
	ap.execEWMA.Store(old + (sample-old)/8)
}

// silenceGap returns the silence fallback for awaitExpectedArrivals, scaled to the
// observed batch round-trip: clamp(execEWMA/8, floor, ceil).
func (ap *AutoPipeliner) silenceGap() time.Duration {
	g := time.Duration(ap.execEWMA.Load() / 8)
	if g < silenceGapFloor {
		return silenceGapFloor
	}
	if g > silenceGapCeil {
		return silenceGapCeil
	}
	return g
}

// pipelineHasFreeConn reports whether the pipeline pool can serve another batch
// without blocking: an idle connection is ready, or the pool has not yet dialed
// to capacity (the pipeline pool runs MinIdleConns=0, so it dials on demand up
// to Size). When the pool is unknown (nil — e.g. a cluster client) it returns
// false, so the straggler-hold keeps its conservative long hold. Called only on
// a gap fire, not per command.
//
// Prefer the pool's own HasFreeCapacity probe (all *ConnPool implement it): the
// plain IdleLen()/Len()<Size() heuristic below ignores MaxActiveConns, so a pool
// with MaxActiveConns < PoolSize and no idle conn would report free even though
// the flush's Get would hit ErrPoolExhausted (codex #3962). The heuristic stays
// as a fallback for any Pooler that does not implement the probe.
func (ap *AutoPipeliner) pipelineHasFreeConn() bool {
	p := ap.pipelinePool
	if p == nil {
		return false
	}
	if hc, ok := p.(interface{ HasFreeCapacity() bool }); ok {
		return hc.HasFreeCapacity()
	}
	return p.IdleLen() > 0 || p.Len() < p.Size()
}

// awaitExpectedArrivals holds the flusher while related work is in motion, so
// commands flush as deep pipelines instead of fragmenting into small batches
// (each fragment costs a pipeline connection for a full round trip). Two
// signals — both facts the engine already has, not wall-clock guesses — decide
// whether anything is imminent:
//
//   - expectedArrivals: a completed batch of N commands wakes its N waiters
//     together, and in a closed loop each immediately submits its next
//     command. Completion announces the exact count; every enqueue accounts
//     for one; the wait ends the moment the count drains — the wave of
//     arrivals has fully landed. An exact per-wave count has no failure mode
//     where an averaged estimate undershoots the true wave and locks the
//     engine into fragmented flushes.
//   - inFlight: batches still executing mean their waiters will wake shortly
//     and stragglers are mid-stream — worth holding a moment to coalesce with,
//     bounded by the silence gap. This also recovers a fragmented state (many
//     singles in flight, which announce nothing): their staggered returns land
//     within one gap, merge into a real batch, and arrival tracking resumes.
//
// When neither holds, the shard is idle and the flush happens immediately: a
// lone caller pays a single round trip with no timer armed. That is the point
// of the design — the previous fixed ~20µs debounce timer armed on every flush
// fires ~1ms late on an idle or low-core host (wakeup latency dominates the
// requested delay), taxing every low-concurrency command ~5x its round trip.
// Here the gap timer never fires in steady state, closed loop or open; it only
// ends waits for callers that left.
func (s *apShard) awaitExpectedArrivals(batchSize int) {
	ap := s.ap
	expected := ap.expectedArrivals.Load()
	if expected < 0 {
		// Arrivals outran what was announced (open-loop traffic); re-zero so
		// the deficit does not mask the next wave. CAS: only clear the value
		// we saw, never a concurrent announcement.
		ap.expectedArrivals.CompareAndSwap(expected, 0)
		expected = 0
	}
	expectingWave := expected > 0
	if !expectingWave && s.inFlight.Load() == 0 {
		// Idle shard: nothing imminent, flush in one round trip.
		return
	}

	gap := ap.silenceGap()
	// Reset is drain-safe on Go 1.23+ (see go.mod: go 1.24).
	fallback := time.NewTimer(gap)
	defer fallback.Stop()
	lastSeenExpected := expected // count as of the most recent timer (re)arm
	var holdStart time.Time      // set on the first straggler-hold gap fire
	for {
		select {
		case <-ap.ctx.Done():
			return
		case <-fallback.C:
			if !expectingWave && s.Len() < coalesceMinFlush && s.inFlight.Load() > 0 {
				// Only stragglers queued while batches are still executing:
				// flushing a near-empty pipeline burns a connection for a full
				// round trip (measured at high WAN concurrency: straggler
				// flushes of 1-3 commands starved the connection pool and
				// doubled p50). How long to hold depends on whether the pipeline
				// pool has a connection to spare:
				//
				//   - a connection is free -> bound the hold at stragglerHoldGaps
				//     silence gaps (a few ms). Flushing then costs an otherwise-
				//     idle connection and saves the straggler ~1 RTT. Waiting a
				//     whole round trip here (the old behavior) is what pinned
				//     low-concurrency stragglers at 2x RTT.
				//   - the pool is saturated -> keep the original long hold. Tiny
				//     flushes into a full pool thrash it: they cannot coalesce
				//     into the deep pipelines the scarce connections need, and
				//     throughput collapses (measured at a squeezed pool: an
				//     unconditional few-ms bound cut throughput ~7x). Holding
				//     lets the next completed batch's wave sweep the stragglers
				//     along.
				//
				// A wedged in-flight batch cannot hang the held stragglers past
				// the bound (the free-conn case caps at a few ms; the flush path
				// keeps its own autoPipelinePermitBackstop safety ceiling).
				if holdStart.IsZero() {
					holdStart = time.Now()
				}
				stragCap := autoPipelinePermitBackstop
				if ap.pipelineHasFreeConn() {
					stragCap = stragglerHoldGaps * gap
				}
				if time.Since(holdStart) < stragCap {
					lastSeenExpected = ap.expectedArrivals.Load()
					fallback.Reset(gap)
					continue
				}
			}
			if expectingWave {
				// A whole gap passed with no arrivals on this shard: the
				// expected callers left (workload shrank), so clear the stale
				// expectation or future flushes will wait for ghosts. But only
				// if it did not GROW during the silent gap — growth means a
				// batch elsewhere (another shard, or racing this fire)
				// announced a fresh wave, and erasing that would fragment a
				// wave that is really coming. CAS, never a blind store, so an
				// announcement racing the reset itself also survives.
				if d := ap.expectedArrivals.Load(); d > 0 && d <= lastSeenExpected {
					ap.expectedArrivals.CompareAndSwap(d, 0)
				}
			}
			return
		case <-s.notify:
			if s.Len() >= batchSize || s.bytesFull() {
				return
			}
			if d := ap.expectedArrivals.Load(); d > 0 {
				// An in-flight batch completed mid-wait: its wave is now the
				// thing to wait out, with the exact-count exit below.
				expectingWave = true
				lastSeenExpected = d
			} else if expectingWave {
				// The wave has fully landed; flush it as one batch.
				return
			} else if s.inFlight.Load() == 0 {
				// Nothing executing, no wave expected: no completion will
				// wake more callers, so flush what we have now.
				return
			}
			fallback.Reset(gap)
		}
	}
}

// dispatchCmds executes the drained stripe queues as one pipeline without
// constructing a Pipeline object: the queue slices go straight to the client's
// hook-wrapped pipeline processor (the exact entry Pipeline.Exec is wired to),
// so hooks and OTel behave identically while the per-batch Pipeline allocation,
// its append-growth reallocations and the per-command Process calls disappear.
// A single-stripe drain (every ordered shard, and any drain that found one
// non-empty stripe) passes its queue zero-copy; multi-stripe drains merge into
// one pooled slice.
// The batches stay OPEN throughout: completion happens at the caller's
// deferred closes, after the whole hook chain has returned. Hooks on the
// dispatch goroutine can still read results without deadlocking via the
// dispGid guard in await() (pre-next: the not-yet-executed view; post-next:
// the populated results), and — exactly like a plain pipeline — they may
// even adjust results before any waiter wakes.
//
// The innermost records whether execution actually happened. Two hook
// behaviours the chain's return value can carry are surfaced, both while the
// batches are still open (the callers' deferred closes run after this
// returns, so no waiter is reading yet):
//   - short-circuit (hook returned without calling next): nothing set the
//     commands' results — the chain's error, if any, is set
//     on every command;
//   - post-next verdict (exec ran, a hook still returned an error): applied
//     to the commands ONLY when every one of them is error-free — the case
//     where the hook's verdict would otherwise vanish entirely. A plain
//     pipeline hands that verdict to the Exec caller without rewriting
//     per-command results; with no Exec caller here, per-command errors
//     recorded by the exec always win and are never overwritten.
func (ap *AutoPipeliner) dispatchCmds(ctx context.Context, queues [][]Cmder, total int) {
	cmds := queues[0]
	if len(queues) > 1 {
		cmds = getQueueSlice(total)
		for i := range queues {
			cmds = append(cmds, queues[i]...)
		}
	}
	// A command that forbids retries (today: the zero-copy reads, whose reply
	// decodes into a caller buffer that a retry could not un-write) disables
	// retries for the WHOLE slice it is dispatched in — see cmdsContainNoRetry.
	// In a shared batch that would silently strip retries from unrelated
	// callers' ordinary commands, so a mixed batch is dispatched as several
	// pipelines instead of one.
	//
	// Split into CONTIGUOUS RUNS, in order, never into two policy groups:
	// grouping would reorder the stream — a zero-copy read submitted before a
	// SET to the same key would execute after it, so the read observes the new
	// value on a face that promises submit order. Runs preserve every relative
	// position while still keeping each dispatched slice policy-uniform (both
	// findings by codex on #3942; the grouping bug was introduced by the first
	// fix for the retry leak).
	if runs := splitRetryRuns(cmds); runs != nil {
		ap.dispatchSequential(ctx, runs)
		if len(queues) > 1 {
			putQueueSlice(cmds)
		}
		return
	}
	executed := false
	chainErr := ap.pipeliner.withProcessPipelineHook(ctx, cmds, func(ctx context.Context, cmds []Cmder) error {
		executed = true
		return ap.pipeliner.processPipeline(ctx, cmds)
	})
	// NOTE: a hook that returns nil WITHOUT calling next has short-circuited
	// SUCCESSFULLY — it served the batch itself (a cache, a mock) and set the
	// command values. Plain Pipeline/Client hooks are allowed to do exactly
	// that, so no error is synthesized for it: doing so made a hook that works
	// on a pipeline fail on an autopipelined batch (review finding by codex on
	// #3942). Only the hook's own error propagates, below.
	if chainErr != nil {
		if !executed {
			setCmdsErr(cmds, chainErr)
		} else if cmdsFirstErr(cmds) == nil {
			// Post-next error on an all-clean batch: the exec fully succeeded,
			// so the error can only be the hook's own verdict — apply it.
			// On a mixed batch it is applied to nothing: hooks conventionally
			// return next's error (`err := next(...); return err`), so after a
			// partial failure the chain error is presumed to be that echo, and
			// stamping it on the commands that DID succeed would overwrite
			// valid replies with their batchmates' failure. Exec-recorded
			// per-command outcomes always win over a post-next rewrap.
			setCmdsErr(cmds, chainErr)
		}
	}
	if len(queues) > 1 {
		putQueueSlice(cmds)
	}
}

// dispatchCmdsMaybeChunked dispatches a drained batch, splitting it into
// byte-bounded chunks when MaxBatchBytes is configured: each chunk is its own
// pipeline write+read cycle, so a batch of many large values becomes several
// bounded bursts instead of one huge write that can stall a constrained link
// past its deadline. The commands' batches still complete only after ALL
// chunks executed (the caller's deferred closes), exactly like an unchunked
// dispatch — chunking bounds the wire bursts, it does not change completion
// semantics. Each chunk runs the full hook chain, like consecutive pipelines.
func (ap *AutoPipeliner) dispatchCmdsMaybeChunked(ctx context.Context, queues [][]Cmder, total int) {
	limit := int64(ap.config.MaxBatchBytes)
	if limit <= 0 {
		ap.dispatchCmds(ctx, queues, total)
		return
	}

	// Merge (borrowed from dispatchCmds's multi-queue path) so chunk
	// boundaries can cross stripe queues.
	cmds := queues[0]
	merged := false
	if len(queues) > 1 {
		cmds = getQueueSlice(total)
		for i := range queues {
			cmds = append(cmds, queues[i]...)
		}
		merged = true
	}

	// Cut the byte-bounded chunks, then hand the ordered sequence to the shared
	// dispatcher — which stops after a chunk dies on a transport-class failure,
	// so later commands cannot overtake a failed prefix (see
	// dispatchSequential; the retry-policy runs go through the same helper).
	chunks := make([][]Cmder, 0, 4)
	start := 0
	var chunkBytes int64
	for i, cmd := range cmds {
		chunkBytes += cmdApproxBytes(cmd)
		if chunkBytes >= limit && i+1 > start {
			chunks = append(chunks, cmds[start:i+1])
			start = i + 1
			chunkBytes = 0
		}
	}
	if start < len(cmds) {
		chunks = append(chunks, cmds[start:])
	}
	ap.dispatchSequential(ctx, chunks)
	if merged {
		putQueueSlice(cmds)
	}
}

// dispatchSequential dispatches an ORDERED sequence of sub-batches, stopping
// once one of them dies on a transport-class failure and failing the rest with
// that error.
//
// The stop is the same contract the unchunked path has: it fails or retries the
// batch as a UNIT, so in an ordered stream later commands must never overtake a
// prefix that died (retries exhausted, hook abort). Per-command redis errors
// (WRONGTYPE, nil) are normal outcomes and do not stop the sequence.
//
// Both callers that break a batch into ordered pieces — the MaxBatchBytes
// chunker and the retry-policy runs — go through here, because the first
// version of each got this wrong independently (review findings by codex on
// #3942).
func (ap *AutoPipeliner) dispatchSequential(ctx context.Context, groups [][]Cmder) {
	var abortErr error
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		if abortErr != nil {
			setCmdsErr(group, abortErr)
			continue
		}
		ap.dispatchCmds(ctx, [][]Cmder{group}, len(group))
		for _, cmd := range group {
			if err := cmd.rawErr(); err != nil && !isRedisError(err) {
				abortErr = err
				break
			}
		}
	}
}

// splitRetryRuns slices cmds into maximal CONTIGUOUS runs of one retry policy,
// preserving order: run i's commands all precede run i+1's, exactly as
// submitted. It returns nil when the whole batch is already policy-uniform —
// the overwhelmingly common case — so uniform batches allocate nothing and are
// dispatched as one pipeline.
//
// Runs are sub-slices of cmds, not copies, so they must be dispatched before
// cmds is recycled and must not be returned to the slice pool individually.
func splitRetryRuns(cmds []Cmder) [][]Cmder {
	if len(cmds) < 2 {
		return nil
	}
	first := cmds[0].NoRetry()
	boundary := -1
	for i := 1; i < len(cmds); i++ {
		if cmds[i].NoRetry() != first {
			boundary = i
			break
		}
	}
	if boundary < 0 {
		return nil // uniform: one dispatch, no split
	}
	runs := make([][]Cmder, 0, 4)
	start := 0
	policy := first
	for i := 1; i < len(cmds); i++ {
		if p := cmds[i].NoRetry(); p != policy {
			runs = append(runs, cmds[start:i])
			start = i
			policy = p
		}
	}
	return append(runs, cmds[start:])
}

// recoverDispatchPanic converts a panic on a dispatch goroutine (a hook or
// command-encoder panic inside Process/Exec) into per-command errors instead
// of crashing the process. On a plain client the same panic unwinds into the
// CALLER, who can recover; the engine's dispatch goroutines have no caller,
// so an unrecovered panic here would kill the whole program on behalf of one
// bad command. Registered LAST at each dispatch site so it runs FIRST on
// unwind (LIFO) — the errors are stamped before the deferred batch closes
// wake the waiters. setCmdsErr fills only commands without an error, so
// exec-recorded outcomes for commands that finished are preserved.
func recoverDispatchPanic(cmds ...[]Cmder) {
	r := recover()
	if r == nil {
		return
	}
	err := fmt.Errorf("redis: autopipeline: panic during dispatch: %v", r)
	for _, batch := range cmds {
		setCmdsErr(batch, err)
	}
	internal.Logger.Printf(context.Background(), "autopipeline: recovered dispatch panic: %v\n%s", r, debug.Stack())
}

// flushBatchSlice takes the shard's currently-queued commands as one batch,
// swaps in a fresh batch for subsequent enqueues, and dispatches the taken
// batch. Completion is signalled by closing the batch's done channel once
// (waking every waiter in a single operation) rather than one channel send
// per command.
func (s *apShard) flushBatchSlice() {
	ap := s.ap

	// Drain every stripe into one combined batch and roll fresh queues for the
	// commands enqueued after this point. Striped enqueue spreads the hot
	// mutex; one merged flush keeps the pipeline deep. accumulateBatch already
	// bounds the total to roughly MaxBatchSize before we get here.
	queues := make([][]Cmder, 0, len(s.stripes))
	batches := make([]*apBatch, 0, len(s.stripes))
	total := 0
	for i := range s.stripes {
		st := &s.stripes[i]
		// Skip provably-empty stripes without taking their mutex. Safe in
		// THIS path only: an enqueue publishes queueLen under the stripe lock
		// and wakes the flusher after unlocking, so a command that appears
		// concurrently with this unlocked read is re-observed by the
		// flusher's Len() loop or the buffered notify — the same protocol the
		// flusher already relies on. The shutdown drain must keep locking
		// unconditionally (see flushBatchSliceShutdown).
		if st.queueLen.Load() == 0 {
			continue
		}
		st.mu.Lock()
		if len(st.queue) > 0 {
			queues = append(queues, st.queue)
			batches = append(batches, st.curBatch)
			total += len(st.queue)
			st.queue = getQueueSlice(ap.config.MaxBatchSize)
			st.curBatch = newAPBatch()
			st.queueLen.Store(0)
			st.queueBytes.Store(0)
		}
		st.mu.Unlock()
	}
	if total == 0 {
		return
	}

	// Acquire a concurrency permit. The wait runs on a background context with
	// a generous backstop deadline against a wedged semaphore: commands taken
	// from the queue were already ACCEPTED, so a concurrent Close must not
	// cancel them mid-acquire — Close's contract is to flush pending commands
	// (it waits for this dispatch via wg/batchWg before tearing anything
	// down). The backstop is deliberately well above both the default
	// ReadTimeout and a maintnotifications relaxed window, so a legitimately
	// slow batch (e.g. during a failover) holding a permit does not cause
	// waiters to spuriously fail.
	if !s.sem.TryAcquire() {
		err := s.sem.Acquire(context.Background(), autoPipelinePermitBackstop, ErrAutoPipelineTimeout)
		if err != nil {
			// A permit not freeing within the backstop means the in-flight
			// batch is wedged well past any configured timeout — leave an
			// operator breadcrumb before failing the drained commands.
			internal.Logger.Printf(context.Background(),
				"redis: autopipeline: no batch permit after %s; failing %d queued commands",
				autoPipelinePermitBackstop, total)
			batchErr := err
			for i := range queues {
				for _, qc := range queues[i] {
					qc.SetErr(batchErr)
				}
			}
			// Release only once every command has its error, so the limit
			// holds until the batch is really done, and before the closes
			// wake the waiters.
			ap.releaseQueued(total)
			for i := range queues {
				batches[i].close()
				putQueueSlice(queues[i])
			}
			return
		}

		// Wave merge. We took the queue and then waited a full batch round
		// trip for the permit; callers whose replies landed just after our
		// take re-submitted into the FRESH queue during that wait. Executing
		// without them splits the group into two alternating waves — each
		// observing two round trips, at half throughput — a state that is
		// stable once entered (measured: p50 pinned at 2xRTT for entire runs
		// at mid worker counts on a 52ms link). On the default window, let the
		// wave of follow-ups land and fold it into this batch before
		// executing, which merges the waves back into one batch per round
		// trip. Explicit-delay configs keep their own timing.
		if ap.config.MaxFlushDelay == 0 && !ap.config.AdaptiveDelay {
			s.awaitExpectedArrivals(ap.config.MaxBatchSize)
			for i := range s.stripes {
				st := &s.stripes[i]
				if st.queueLen.Load() == 0 {
					continue
				}
				st.mu.Lock()
				if len(st.queue) > 0 {
					queues = append(queues, st.queue)
					batches = append(batches, st.curBatch)
					total += len(st.queue)
					st.queue = getQueueSlice(ap.config.MaxBatchSize)
					st.curBatch = newAPBatch()
					st.queueLen.Store(0)
					st.queueBytes.Store(0)
				}
				st.mu.Unlock()
			}
		}
	}

	// Fast path for single command: skip the pipeline and Process directly, in
	// its own goroutine. The dispatch MUST NOT run inline in the flusher: a
	// synchronous Process blocks the flusher for a full round trip, and on a
	// slow link a solo straggler then holds up an entire landed wave for one
	// RTT — whose flush then delays the straggler's next command in turn, a
	// stable phase-lock where everyone pays 2x RTT (measured: ~25% of runs on
	// a 57ms link locked at exactly 2x RTT until perturbed).
	// No expectedArrivals announcement: a single waiter waking is the
	// lone-caller case, which must keep flushing immediately.
	if total == 1 {
		ap.batchWg.Add(1)
		s.inFlight.Add(1)
		go func() {
			// Defer order matters: the batch close is registered BEFORE the
			// permit release and inFlight decrement so it runs AFTER them
			// (LIFO) — a woken lone caller's next command then observes an
			// idle shard and takes the immediate-flush path instead of
			// arming the silence-gap wait.
			defer ap.batchWg.Done()
			defer batches[0].close()
			defer ap.releaseQueued(1)
			defer s.inFlight.Add(-1)
			defer s.sem.Release()
			defer putQueueSlice(queues[0])
			defer recoverDispatchPanic(queues[0])
			// Background for the same reason as the batch goroutine below:
			// accepted commands execute even under a concurrent Close.
			execStart := time.Now()
			b := batches[0]
			if !ap.blocking && ap.armSelfDeadlockGuard() {
				b.dispGid.Store(curGoroutineID())
			}
			solo := queues[0][0]
			// Both faces run the user-hook chain via withProcessHook. The
			// command records the CHAIN's final verdict — exactly what
			// Client.Process does — before the deferred close wakes the
			// waiter, so a hook that short-circuits, rewrites, or suppresses
			// the error is honored. Hooks on this goroutine read the command
			// deadlock-free via the dispGid guard stamped above.
			// A successful short-circuit stays successful (see dispatchCmds).
			err := ap.pipeliner.withProcessHook(context.Background(), solo, func(ctx context.Context, cmd Cmder) error {
				// A cacheable solo on a CSC client goes through Process so the cache
				// is honored (processPipeline bypasses processCached). Gated on
				// the LIVE CSC state: without active CSC, Process would just run on
				// the MAIN pool, ignoring the pipeline pool the straggler gate probed.
				if ap.cscActiveFn != nil && ap.cscActiveFn() && isCacheable(cmd) {
					return ap.pipeliner.process(ctx, cmd)
				}
				// One-command pipeline on the PIPELINE pool (falls back to the main
				// pool when none exists).
				return ap.pipeliner.processPipeline(ctx, []Cmder{cmd})
			})
			solo.SetErr(err)
			ap.observeBatchExec(time.Since(execStart))
		}()
		return
	}

	// Track this goroutine in the batchWg so Close() waits for it.
	// IMPORTANT: Add to WaitGroup AFTER semaphore is acquired to avoid deadlock.
	ap.batchWg.Add(1)
	s.inFlight.Add(1)
	go func() {
		defer ap.batchWg.Done()
		defer s.inFlight.Add(-1)
		defer s.sem.Release()
		// Signal completion with one close per taken stripe. Deferred so a
		// panic in Process/Exec (e.g. a malformed command or encoder panic)
		// still wakes every waiter in await() instead of hanging them forever;
		// the closes run after Exec on the happy path, so results are
		// populated first.
		defer func() {
			ap.releaseQueued(total)
			for i := range queues {
				batches[i].close()
				putQueueSlice(queues[i])
			}
		}()
		defer recoverDispatchPanic(queues...)

		// Execute on a background context: these commands were accepted before
		// any concurrent Close, and Close waits for this goroutine (batchWg)
		// before the client tears down its pools — cancelling here would
		// error already-accepted commands while the shutdown sweep flushes
		// later ones, an inverted outcome. The wire timeouts (Read/Write
		// Timeout, or maintnotifications relaxed windows) still bound the
		// execution; no per-batch timer is allocated.
		ctx := context.Background()

		// The batches complete at the deferred closes, AFTER the whole hook
		// chain has returned — so a hook's post-next verdict is honored and,
		// like a plain pipeline, a hook may adjust results before any waiter
		// wakes. Hooks on this goroutine read results deadlock-free via the
		// dispGid guard in await() (armed below when hooks can exist).
		if !ap.blocking && ap.armSelfDeadlockGuard() {
			gid := curGoroutineID()
			for i := range batches {
				batches[i].dispGid.Store(gid)
			}
		}

		execStart := time.Now()
		ap.dispatchCmdsMaybeChunked(ctx, queues, total)
		ap.observeBatchExec(time.Since(execStart))

		// Announce the expected arrivals BEFORE the deferred closes wake this
		// batch's waiters, so the flusher knows the wave size the moment its
		// first command lands (see expectedArrivals).
		ap.expectedArrivals.Add(int64(total))
	}()
}

// flushBatchSliceShutdown flushes commands during shutdown.
// Unlike flushBatchSlice, this doesn't use ap.ctx for semaphore acquisition
// because ap.ctx is already cancelled during shutdown.
// Executes synchronously to preserve command order.
func (s *apShard) flushBatchSliceShutdown() {
	ap := s.ap
	// Flush all remaining commands synchronously to preserve order.
	//
	// The loop condition is checked UNDER each stripe's lock (not via the
	// unlocked s.Len()): a late enqueue appends to a stripe's queue and updates
	// its queueLen under that stripe's mutex, so reading queueLen without the
	// lock could miss a command that was just appended (seeing 0 and exiting
	// while a command sits in the queue). Locking first makes "is the stripe
	// empty?" and "take the stripe" atomic against that enqueue — this is what
	// closes the lost-command race on Close.
	for {
		// Take every stripe's queue as one merged batch and roll fresh queues.
		queues := make([][]Cmder, 0, len(s.stripes))
		batches := make([]*apBatch, 0, len(s.stripes))
		total := 0
		for i := range s.stripes {
			st := &s.stripes[i]
			st.mu.Lock()
			if len(st.queue) > 0 {
				queues = append(queues, st.queue)
				batches = append(batches, st.curBatch)
				total += len(st.queue)
				st.queue = getQueueSlice(ap.config.MaxBatchSize)
				st.curBatch = newAPBatch()
				st.queueLen.Store(0)
				st.queueBytes.Store(0)
			}
			st.mu.Unlock()
		}
		if total == 0 {
			return
		}

		// Serialize with any still-running in-flight batch: the shutdown drain
		// used to bypass the per-shard permit, so under MaxConcurrentBatches:1
		// a drained command could execute CONCURRENTLY with the in-flight
		// batch during Close and be observed out of order. Acquire the permit
		// (bounded by the backstop, on a background context — ap.ctx is
		// already cancelled here); if the backstop expires the permit holder
		// is wedged and we proceed anyway rather than strand the commands.
		acquired := s.sem.TryAcquire()
		if !acquired {
			acquired = s.sem.Acquire(context.Background(), autoPipelinePermitBackstop, ErrAutoPipelineTimeout) == nil
			if !acquired {
				internal.Logger.Printf(context.Background(),
					"redis: autopipeline: no batch permit after %s during shutdown; flushing unserialized",
					autoPipelinePermitBackstop)
			}
		}

		// Execute each batch in a func so close(batch.done) is deferred: a panic
		// in Process/Exec still signals completion (waking await()) before it
		// propagates, instead of leaving shutdown waiters hung.
		func() {
			if acquired {
				defer s.sem.Release()
			}
			defer func() {
				ap.releaseQueued(total)
				for i := range queues {
					batches[i].close()
					putQueueSlice(queues[i])
				}
			}()
			defer recoverDispatchPanic(queues...)

			// ap.ctx is already cancelled here (Close cancels it before draining),
			// so use a fresh background context with no artificial deadline. The
			// wire timeout is then governed by the connection's ReadTimeout /
			// WriteTimeout — exactly like the normal flush path and a plain client
			// Exec. Crucially this lets a relaxed timeout (set by maintnotifications
			// during a failover/migration) take effect; a hardcoded short deadline
			// here would cap that relaxed window and time out in-flight commands the
			// relaxation was meant to protect. (A user who wants shutdown bounded
			// sets ReadTimeout/WriteTimeout on the client, as for any command.)
			if !ap.blocking && ap.armSelfDeadlockGuard() {
				gid := curGoroutineID()
				for i := range batches {
					batches[i].dispGid.Store(gid)
				}
			}
			ap.dispatchCmdsMaybeChunked(context.Background(), queues, total)
		}()
	}
}

// Len returns the number of queued commands in this shard.
func (s *apShard) Len() int {
	n := 0
	for i := range s.stripes {
		n += int(s.stripes[i].queueLen.Load())
	}
	return n
}

// bytesFull reports whether the shard's queued payload volume has reached the
// configured MaxBatchBytes (false when the cap is disabled). Like the
// MaxBatchSize trigger it is soft: enqueues racing the check can overshoot.
func (s *apShard) bytesFull() bool {
	limit := int64(s.ap.config.MaxBatchBytes)
	if limit <= 0 {
		return false
	}
	var n int64
	for i := range s.stripes {
		n += s.stripes[i].queueBytes.Load()
		if n >= limit {
			return true
		}
	}
	return false
}

// cmdApproxBytes estimates a command's wire payload for MaxBatchBytes
// accounting: string/[]byte argument lengths plus a small fixed overhead per
// argument (type marker, length line, CRLFs). Exactness doesn't matter — the
// cap bounds burst size, it is not a protocol calculation.
func cmdApproxBytes(cmd Cmder) int64 {
	const perArgOverhead = 16
	// unknownArgBytes stands in for an arg whose encoded size cannot be
	// determined here (a BinaryMarshaler that errored). Deliberately large so
	// such an arg isolates into its own chunk rather than silently
	// undercounting and letting several of them coalesce past MaxBatchBytes —
	// the same command would fail at write time anyway, so over-counting it
	// here is free (codex on #4002).
	const unknownArgBytes = 1 << 20
	n := int64(0)
	for _, a := range cmd.Args() {
		switch v := a.(type) {
		case string:
			n += int64(len(v))
		case []byte:
			n += int64(len(v))
		case *string:
			// proto.Writer dereferences and writes *v (empty string for nil),
			// same as the string case above — see Writer.WriteArg.
			if v != nil {
				n += int64(len(*v))
			}
		case encoding.BinaryMarshaler:
			// proto.Writer's default arm marshals any other type through this
			// interface (int/float/bool/time.Time/etc. all have their own,
			// small, fixed-size case above and never reach here). A large
			// custom Cmder argument marshaled through it must be sized by its
			// actual encoded length, not the untyped 8-byte fallback below —
			// that fallback previously let several large marshaled args
			// coalesce past MaxBatchBytes and reopen the large-payload
			// write/reply deadlock the cap exists to bound.
			if b, err := v.MarshalBinary(); err == nil {
				n += int64(len(b))
			} else {
				n += unknownArgBytes
			}
		default:
			n += 8
		}
		n += perArgOverhead
	}
	return n
}

// cmdApproxBytesSafe wraps cmdApproxBytes with a recover. Sizing runs user code
// — cmd.Args() on a custom Cmder, MarshalBinary on a BinaryMarshaler argument —
// and can panic. Two callers need that contained: the full-duplex serve loop,
// which has no top-level recover (a panic would kill the engine and strand every
// in-flight and future command), and the half-duplex enqueue, which sizes before
// taking the stripe lock (a panic under that lock would leave it held forever).
// On panic it returns a non-nil error wrapping errFDPanicRecovered so the caller
// can fail and DROP just that command before anything is queued or written — the
// alternative (letting it reach the write path, whose write-time recover tears
// the session down and replays the batch) forces at-least-once re-execution of
// the poisoned command's innocent batch-mates.
func cmdApproxBytesSafe(cmd Cmder) (n int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: Args() sizing: %v", errFDPanicRecovered, r)
			internal.Logger.Printf(context.Background(),
				"autopipeline: recovered Args() sizing panic: %v\n%s", r, debug.Stack())
		}
	}()
	return cmdApproxBytes(cmd), nil
}

// Len returns the current number of queued commands across all shards.
func (ap *AutoPipeliner) Len() int {
	total := 0
	for _, s := range ap.shards {
		total += s.Len()
	}
	// Full-duplex accepts commands onto its submit queue instead of the shard
	// queues, so include that backlog — otherwise Len() reports 0 while accepted
	// commands are buffered behind a backpressured/stalled FD writer, and callers
	// using Len() for monitoring or local backpressure lose the signal in
	// FullDuplex mode.
	for _, e := range ap.fds {
		total += e.q.depth()
	}
	// Cluster full-duplex accepts commands onto per-node FD children, not the
	// shard queues; include their backlog for the same monitoring reason.
	if ap.clusterFD != nil {
		total += ap.clusterFD.len()
	}
	return total
}

// calculateDelay calculates the delay based on the given queue length (the
// caller's own shard, not the global total, so each shard tunes independently).
// Uses integer-only arithmetic for optimal performance (no float operations).
// Returns 0 if MaxFlushDelay is 0.
func (ap *AutoPipeliner) calculateDelay(queueLen int) time.Duration {
	maxDelay := ap.config.MaxFlushDelay
	if maxDelay == 0 {
		return 0
	}

	// If adaptive delay is disabled, return fixed delay
	if !ap.config.AdaptiveDelay {
		return maxDelay
	}

	if queueLen == 0 {
		return 0
	}

	maxBatch := ap.config.MaxBatchSize

	// Use integer arithmetic to avoid float operations
	// Calculate thresholds: 75%, 50%, 25% of maxBatch
	// Multiply by 4 to avoid division: queueLen * 4 vs maxBatch * 3 (75%)
	//
	// Adaptive delay strategy:
	// - ≥75% full: No delay (flush immediately to prevent overflow)
	// - ≥50% full: 25% of max delay (queue filling up)
	// - ≥25% full: 50% of max delay (moderate load)
	// - <25% full: 100% of max delay (low load, maximize batching)
	switch {
	case queueLen*4 >= maxBatch*3: // queueLen >= 75% of maxBatch
		return 0 // Flush immediately
	case queueLen*2 >= maxBatch: // queueLen >= 50% of maxBatch
		return maxDelay >> 2 // Divide by 4 using bit shift (faster)
	case queueLen*4 >= maxBatch: // queueLen >= 25% of maxBatch
		return maxDelay >> 1 // Divide by 2 using bit shift (faster)
	default:
		return maxDelay
	}
}

// Pipeline returns a new pipeline that uses the underlying pipeliner.
// This allows you to create a traditional pipeline from an autopipeliner.
//
// On a FULL-DUPLEX autopipeliner the pipeline runs on the HELD connection
// rather than taking one of its own: see fdPipelineExec. Without that, a
// pipeline built from an autopipeliner would quietly leave the pipe, take a
// pooled connection per Exec, and lose the coalescing that is the whole point
// of the engine — measured at 150 active sockets and less than a third of the
// throughput of the same commands submitted through the engine.
func (ap *AutoPipeliner) Pipeline() Pipeliner {
	if ap.fd != nil {
		pipe := Pipeline{exec: pipelineExecer(ap.fdPipelineHooked)}
		pipe.init()
		return &pipe
	}
	return ap.pipeliner.Pipeline()
}

// fdPipelineExec runs a whole pipeline through the full-duplex engine as one
// contiguous batch, and falls back to a pooled pipeline when the batch cannot
// ride the held connection.
//
// Contiguity is what makes this safe: FDPipelined admits the batch all-or-
// nothing, so the commands occupy consecutive in-flight slots and their replies
// come back in submit order. A pipeline's internal ordering therefore holds
// exactly as it does on a dedicated connection.
//
// The fallback covers the cases the engine must divert — a blocking command, a
// command with its own read timeout, anything the divert predicate claims, or a
// batch larger than the whole submit queue. Those go to the ordinary pipeline
// path, which is where they would have gone before this existed.
//
// fdPipelineExec is the TERMINAL of the client's ProcessPipelineHook chain
// (see fdPipelineHooked), the place processPipeline takes in an ordinary
// pipeline. So hooks wrap the whole execution, fallback and retry included,
// exactly once, and the pooled paths below call the hookless processPipeline.
//
// On the FD path it records what an ordinary pipeline records: one pipeline
// duration and, on failure, one pipeline-level error, instead of the reader's
// per-command metrics.
//
// A Limiter is asked per WRITE CHUNK on the FD path, not once per batch: a
// chunk can mix this batch with other callers' commands, so a batch longer
// than MaxBatchSize (or MaxBatchBytes) can be partly admitted. An ordinary
// pipeline makes one decision per attempt.
//
// Also:
//
//   - A retryable reply (LOADING and the like) on the FIRST command: an
//     ordinary pipeline retries the whole batch, in order. FDPipelined settles
//     retryable replies inline, so the batch is re-run the ordinary way, unless
//     it holds a NoRetry command, which an ordinary pipeline does not retry
//     either. A retryable reply on a later command is returned, as an ordinary
//     pipeline does.
//
// fdPipelineHooked is Pipeline().Exec on a full-duplex autopipeliner: the
// client's pipeline hooks around fdPipelineExec.
//
// ContextTimeoutEnabled keeps the pooled path: an ordinary pipeline bounds its
// socket I/O by the caller's ctx, and the FD wait cannot abandon an admitted
// batch on a shared connection.
func (ap *AutoPipeliner) fdPipelineHooked(ctx context.Context, cmds []Cmder) error {
	c := ap.fd.client
	if c.opt.ContextTimeoutEnabled {
		return ap.pipeliner.processPipelineHook(ctx, cmds)
	}
	return c.wrapPipelineHooks(ap.fdPipelineExec)(ctx, cmds)
}

func (ap *AutoPipeliner) fdPipelineExec(ctx context.Context, cmds []Cmder) error {
	opt := ap.fd.client.opt
	var start time.Time
	if otel.GetPipelineOperationDurationCallback() != nil {
		start = time.Now()
	}
	res, err := ap.fdPipelined(ctx, cmds)
	used, cn := res.attempts, res.cn
	// An ordinary pipeline allows MaxRetries+1 executions. The FD engine may
	// already have issued the batch more than once (a connection-error replay),
	// so the re-run gets what is left.
	remaining := opt.MaxRetries - used
	ineligible, retry := false, false
	switch {
	case errors.Is(err, ErrFDPipelineDiverts),
		errors.Is(err, ErrFDPipelineUnavailable),
		errors.Is(err, ErrFDPipelineTooLarge),
		errors.Is(err, ErrFDPipelineSpansEngines):
		// Not a command failure: the batch is simply not eligible.
		ineligible = true
	case len(cmds) > 0 && remaining >= 0 && !cmdsContainNoRetry(cmds):
		// Only a batch the engine issued exactly once. After a connection-error
		// replay, the replay already was this batch's retry: it re-ran the unread
		// tail, which an ordinary pipeline re-runs as its one whole-batch retry
		// after the same network error. A whole-batch re-run on top would run
		// that tail a third time (a mutation beyond what an ordinary pipeline
		// does), so the first command's stale retryable reply is returned instead.
		// The Close-time flush stamps fdAttempts the same way, so a flushed batch
		// is not re-run either.
		//
		// A transport or protocol failure anywhere in the batch rules the
		// re-run out too. The engine did not replay that command, since it may
		// already have run, and an ordinary pipeline returns that read error
		// instead of retrying.
		if e := cmds[0].rawErr(); used == 1 && isRedisError(e) && shouldRetry(e, false) &&
			fdPipelineTransportErr(cmds) == nil {
			retry = true
		}
	}
	if !ineligible && !retry {
		// Run on the FD path, or refused before admission. A batch the
		// Close-time flush ran in full was measured by that pooled pipeline.
		if res.measured() {
			ap.fdPipelineMetrics(ctx, start, cmds, max(used, 1), cn)
		}
		return err
	}
	// Clear the per-command errors FDPipelined may have stamped, then run the
	// batch the ordinary way so the caller sees one authoritative outcome.
	for _, cmd := range cmds {
		cmd.SetErr(nil)
	}
	// Hookless processPipeline: this is already the terminal of the hook
	// chain. The pooled run records its own pipeline metric.
	if ineligible {
		return ap.fd.client.processPipeline(ctx, cmds)
	}
	// The re-run runs remaining+1 times at most, so FD attempts plus re-runs
	// never exceed MaxRetries+1, after the backoff an ordinary pipeline sleeps
	// before its next retry. Inside the hook chain, like generalProcessPipeline's
	// own retries. It continues this operation's measurement (start and the FD
	// attempts), so one pipeline metric covers the whole operation.
	c := ap.fd.client
	if serr := internal.Sleep(ctx, c.retryBackoff(used)); serr != nil {
		setCmdsErr(cmds, serr)
		ap.fdPipelineMetrics(ctx, start, cmds, used, cn)
		return serr
	}
	return c.processPipelineRetriesAfter(ctx, cmds, remaining, start, used)
}

// Pipelined executes a function in a pipeline context.
// This is a convenience method that creates a pipeline, executes the function,
// and returns the results.
//
// Uses Pipeline, so on a full-duplex autopipeliner this also runs on the held
// connection rather than taking one of its own.
func (ap *AutoPipeliner) Pipelined(ctx context.Context, fn func(Pipeliner) error) ([]Cmder, error) {
	return ap.Pipeline().Pipelined(ctx, fn)
}

// TxPipelined executes a function in a transaction pipeline context.
// This is a convenience method that creates a transaction pipeline, executes the function,
// and returns the results. It delegates to the underlying client's TxPipeline.
func (ap *AutoPipeliner) TxPipelined(ctx context.Context, fn func(Pipeliner) error) ([]Cmder, error) {
	return ap.pipeliner.TxPipeline().Pipelined(ctx, fn)
}

// TxPipeline returns a new transaction pipeline that uses the underlying pipeliner.
// This allows you to create a traditional transaction pipeline from an autopipeliner.
// It delegates to the underlying client's TxPipeline.
func (ap *AutoPipeliner) TxPipeline() Pipeliner {
	return ap.pipeliner.TxPipeline()
}

// validate AutoPipeliner implements Cmdable
var _ Cmdable = (*AutoPipeliner)(nil)
