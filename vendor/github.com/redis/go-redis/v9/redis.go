package redis

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"weak"

	"github.com/redis/go-redis/v9/auth"
	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/auth/streaming"
	"github.com/redis/go-redis/v9/internal/hscan"
	"github.com/redis/go-redis/v9/internal/otel"
	"github.com/redis/go-redis/v9/internal/pool"
	"github.com/redis/go-redis/v9/internal/proto"
	"github.com/redis/go-redis/v9/maintnotifications"
	"github.com/redis/go-redis/v9/push"
)

// Scanner internal/hscan.Scanner exposed interface.
type Scanner = hscan.Scanner

// Nil reply returned by Redis when key does not exist.
const Nil = proto.Nil

// String representations of special float values.
// Values are lowercase for consistency with Redis RESP2 protocol responses.
const (
	NaN  = internal.NaN  // Not a Number
	Inf  = internal.Inf  // Positive infinity
	NInf = internal.NInf // Negative infinity
)

// SetLogger set custom log
// Use with VoidLogger to disable logging.
// If logger is nil, the call is ignored and the existing logger is kept.
func SetLogger(logger internal.Logging) {
	if logger == nil {
		return
	}
	internal.Logger.Store(logger)
}

// SetLogLevel sets the log level for the library.
func SetLogLevel(logLevel internal.LogLevelT) {
	internal.LogLevel.Store(logLevel)
}

//------------------------------------------------------------------------------

type Hook interface {
	DialHook(next DialHook) DialHook
	ProcessHook(next ProcessHook) ProcessHook
	ProcessPipelineHook(next ProcessPipelineHook) ProcessPipelineHook
}

type (
	DialHook            func(ctx context.Context, network, addr string) (net.Conn, error)
	ProcessHook         func(ctx context.Context, cmd Cmder) error
	ProcessPipelineHook func(ctx context.Context, cmds []Cmder) error
)

type hooksMixin struct {
	// Fixed at construction, including custom factories that may enable CSC.
	// Ordinary clients do not traverse request contexts for cache marks.
	captureCacheIntent bool
	// hooksMu serializes writers (AddHook); readers never take it.
	hooksMu *sync.Mutex
	// state holds the immutable hook snapshot. Readers Load it lock-free;
	// writers publish a replacement copy-on-write under hooksMu.
	state *atomic.Pointer[hooksState]
}

// hooksState is an immutable snapshot of the hook configuration. Once stored
// in hooksMixin.state it is never mutated; AddHook builds a fresh copy.
type hooksState struct {
	slice   []Hook
	initial hooks
	current hooks
}

// rebuild recomputes current from initial + slice. It mutates the receiver, so
// it must only run on a state that has not yet been published.
func (s *hooksState) rebuild() {
	s.initial.setDefaults()

	s.current.dial = s.initial.dial
	s.current.process = s.initial.process
	s.current.pipeline = s.initial.pipeline
	s.current.txPipeline = s.initial.txPipeline

	for i := len(s.slice) - 1; i >= 0; i-- {
		if wrapped := s.slice[i].DialHook(s.current.dial); wrapped != nil {
			s.current.dial = wrapped
		}
		if wrapped := s.slice[i].ProcessHook(s.current.process); wrapped != nil {
			s.current.process = wrapped
		}
		if wrapped := s.slice[i].ProcessPipelineHook(s.current.pipeline); wrapped != nil {
			s.current.pipeline = wrapped
		}
		if wrapped := s.slice[i].ProcessPipelineHook(s.current.txPipeline); wrapped != nil {
			s.current.txPipeline = wrapped
		}
	}
}

func (hs *hooksMixin) initHooks(hooks hooks) {
	var slice []Hook
	if hs.state != nil {
		if old := hs.state.Load(); old != nil {
			slice = old.slice
		}
	}

	hs.hooksMu = new(sync.Mutex)
	hs.state = new(atomic.Pointer[hooksState])

	state := &hooksState{slice: slice, initial: hooks}
	state.rebuild()
	hs.state.Store(state)
}

type memberCommandExecutedKey struct{}

// trackMemberCommandExecution marks the terminal process hook for MultiDB.
func (hs *hooksMixin) trackMemberCommandExecution() {
	hs.hooksMu.Lock()
	defer hs.hooksMu.Unlock()

	old := hs.state.Load()
	state := &hooksState{slice: old.slice, initial: old.initial}
	process := state.initial.process
	state.initial.process = func(ctx context.Context, cmd Cmder) error {
		if executed, ok := ctx.Value(memberCommandExecutedKey{}).(*atomic.Bool); ok {
			executed.Store(true)
		}
		return process(ctx, cmd)
	}
	state.rebuild()
	hs.state.Store(state)
}

type hooks struct {
	dial       DialHook
	process    ProcessHook
	pipeline   ProcessPipelineHook
	txPipeline ProcessPipelineHook
}

func (h *hooks) setDefaults() {
	if h.dial == nil {
		h.dial = func(ctx context.Context, network, addr string) (net.Conn, error) { return nil, nil }
	}
	if h.process == nil {
		h.process = func(ctx context.Context, cmd Cmder) error { return nil }
	}
	if h.pipeline == nil {
		h.pipeline = func(ctx context.Context, cmds []Cmder) error { return nil }
	}
	if h.txPipeline == nil {
		h.txPipeline = func(ctx context.Context, cmds []Cmder) error { return nil }
	}
}

// AddHook is to add a hook to the queue.
// Hook is a function executed during network connection, command execution, and pipeline,
// it is a first-in-first-out stack queue (FIFO).
// You need to execute the next hook in each hook, unless you want to terminate the execution of the command.
// For example, you added hook-1, hook-2:
//
//	client.AddHook(hook-1, hook-2)
//
// hook-1:
//
//	func (Hook1) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
//	 	return func(ctx context.Context, cmd Cmder) error {
//		 	print("hook-1 start")
//		 	next(ctx, cmd)
//		 	print("hook-1 end")
//		 	return nil
//	 	}
//	}
//
// hook-2:
//
//	func (Hook2) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
//		return func(ctx context.Context, cmd redis.Cmder) error {
//			print("hook-2 start")
//			next(ctx, cmd)
//			print("hook-2 end")
//			return nil
//		}
//	}
//
// The execution sequence is:
//
//	hook-1 start -> hook-2 start -> exec redis cmd -> hook-2 end -> hook-1 end
//
// Please note: "next(ctx, cmd)" is very important, it will call the next hook,
// if "next(ctx, cmd)" is not executed, the redis command will not be executed.
//
// Contract. A hook runs inside the client's command machinery and must be
// well-behaved, or behavior is undefined:
//   - Call next (as above) unless deliberately terminating the command.
//   - Do not call Close or other client control methods from a hook — the hook
//     runs on the goroutine Close waits for, so this deadlocks.
//   - Do not panic. A panicking hook can crash the process or leave an operation
//     unsettled; wrap fallible work and return an error instead.
//   - Do not mutate client or connection state. A hook may observe and wrap
//     errors (call cmd.SetErr) but must not reconfigure the client mid-flight.
func (hs *hooksMixin) AddHook(hook Hook) {
	hs.hooksMu.Lock()
	defer hs.hooksMu.Unlock()

	old := hs.state.Load()
	state := &hooksState{
		slice:   make([]Hook, len(old.slice)+1),
		initial: old.initial,
	}
	copy(state.slice, old.slice)
	state.slice[len(old.slice)] = hook
	state.rebuild()

	hs.state.Store(state)
}

func (hs *hooksMixin) clone() hooksMixin {
	old := hs.state.Load()
	l := len(old.slice)
	state := &hooksState{
		slice:   old.slice[:l:l],
		initial: old.initial,
		current: old.current,
	}

	clone := hooksMixin{
		captureCacheIntent: hs.captureCacheIntent,
		hooksMu:            new(sync.Mutex),
		state:              new(atomic.Pointer[hooksState]),
	}
	clone.state.Store(state)
	return clone
}

func (hs *hooksMixin) withProcessHook(ctx context.Context, cmd Cmder, hook ProcessHook) error {
	slice := hs.state.Load().slice
	for i := len(slice) - 1; i >= 0; i-- {
		if wrapped := slice[i].ProcessHook(hook); wrapped != nil {
			hook = wrapped
		}
	}
	return hook(ctx, cmd)
}

func (hs *hooksMixin) withProcessPipelineHook(
	ctx context.Context, cmds []Cmder, hook ProcessPipelineHook,
) error {
	slice := hs.state.Load().slice
	for i := len(slice) - 1; i >= 0; i-- {
		if wrapped := slice[i].ProcessPipelineHook(hook); wrapped != nil {
			hook = wrapped
		}
	}
	return hook(ctx, cmds)
}

func (hs *hooksMixin) dialHook(ctx context.Context, network, addr string) (net.Conn, error) {
	return hs.state.Load().current.dial(ctx, network, addr)
}

// hookCount reports how many user hooks are installed. The autopipeliner
// arms its await() self-deadlock guard only when hooks exist, keeping the
// guard a single atomic load on hook-free clients.
func (hs *hooksMixin) hookCount() int {
	return len(hs.state.Load().slice)
}

func (hs *hooksMixin) processHook(ctx context.Context, cmd Cmder) error {
	if hs.captureCacheIntent && cscBegin(ctx, cmd) {
		defer cscLeave(cmd)
	}
	return hs.state.Load().current.process(ctx, cmd)
}

func (hs *hooksMixin) processPipelineHook(ctx context.Context, cmds []Cmder) error {
	return hs.state.Load().current.pipeline(ctx, cmds)
}

func (hs *hooksMixin) processTxPipelineHook(ctx context.Context, cmds []Cmder) error {
	return hs.state.Load().current.txPipeline(ctx, cmds)
}

//------------------------------------------------------------------------------

// Stable identifiers for baseClient.onClose hooks. Each component that
// registers a close callback owns a dedicated id here so the set of known
// hooks is discoverable in one place and id collisions are caught at
// compile time. New ids should be added as additional constants.
const (
	// onCloseHookIDSentinelFailover identifies the close callback installed
	// by NewFailoverClient to tear down sentinel failover background work.
	onCloseHookIDSentinelFailover = "sentinel-failover"

	// onCloseHookIDAutoPipeline / onCloseHookIDAsyncAutoPipeline identify the
	// close callbacks that cancel a cached autopipeliner's engine context when
	// the SHARED pools close through any sharer (e.g. a WithTimeout clone falling
	// through to baseClient.Close). The wrapper's own Close cancels its aps
	// directly, but a clone shares only the pools + onCloseHooks, so without this
	// the original wrapper's flusher/full-duplex goroutines would park on ap.ctx
	// forever. Two ids because one Client caches at most a blocking and an async
	// instance.
	onCloseHookIDAutoPipeline      = "autopipeline"
	onCloseHookIDAsyncAutoPipeline = "autopipeline-async"
)

// onCloseHooks is a small registry of named close callbacks attached to a
// baseClient. Each callback is identified by a stable string id; registering
// the same id twice replaces the previous callback rather than chaining onto
// it. This guarantees the registry stays bounded regardless of how often a
// hook is (re)registered and avoids the unbounded closure chain that
// motivated issue #3772.
//
// Hooks are invoked in registration order. All hooks run regardless of
// individual errors; the first non-nil error is returned.
//
// A zero-value onCloseHooks is ready to use. It is safe for concurrent use.
// Clones of a baseClient share the same *onCloseHooks so registrations and
// close semantics are preserved across WithTimeout / WithContext / etc.
type onCloseHooks struct {
	mu    sync.Mutex
	order []string
	hooks map[string]func() error
	// ran is set once run has taken its snapshot: the owner is closing (or
	// closed), so a callback registered from here on would never be invoked.
	// register reports that instead of silently accepting the registration.
	ran bool
}

// register adds or replaces the callback associated with id. Re-registering
// an existing id overwrites the previous callback in place; new ids are
// appended to the invocation order.
//
// It returns false, and registers nothing, once run has already taken its
// snapshot: the owner is closing, so the callback could never fire. A caller
// that registers lazily against a possibly-closing owner (the cluster FD
// router's per-node evict hook) must treat false as "already closed" and do
// the callback's work itself, or it is left holding state the close will
// never clean up (cursor bugbot on #4002).
func (h *onCloseHooks) register(id string, fn func() error) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ran {
		return false
	}
	if h.hooks == nil {
		h.hooks = make(map[string]func() error)
	}
	if _, exists := h.hooks[id]; !exists {
		h.order = append(h.order, id)
	}
	h.hooks[id] = fn
	return true
}

// unregister removes the callback associated with id, if any. Used by
// AutoPipeliner.Close to detach its per-engine close hook (see
// registerAutoPipelineCloseHook).
func (h *onCloseHooks) unregister(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.hooks[id]; !exists {
		return
	}
	delete(h.hooks, id)
	for i, x := range h.order {
		if x == id {
			h.order = append(h.order[:i], h.order[i+1:]...)
			break
		}
	}
}

// run invokes all registered callbacks in REVERSE registration order (LIFO) and
// returns the first non-nil error encountered. All callbacks are executed even if an
// earlier one returns an error.
//
// LIFO is a dependency-teardown order: a hook registered LATER is a consumer of state
// that earlier registrations provide, so it must run FIRST. Concretely, the Sentinel
// failover teardown is registered at client construction (first), while an
// autopipeliner drain hook is registered lazily on first use (later). The drain needs
// the failover client alive to resolve the master address / dial a replacement
// connection for accepted-but-unsent work; running the Sentinel teardown first would
// set failover.closed, make MasterAddr return pool.ErrClosed, and fail replayable
// commands even though Redis and the pools are still up (a sibling pool-sharing clone
// triggering Close was enough). Draining consumers before tearing down discovery keeps
// those commands serviceable.
func (h *onCloseHooks) run() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	// From here on a late register would never be invoked; make it say so.
	h.ran = true
	fns := make([]func() error, 0, len(h.order))
	for i := len(h.order) - 1; i >= 0; i-- {
		if fn := h.hooks[h.order[i]]; fn != nil {
			fns = append(fns, fn)
		}
	}
	h.mu.Unlock()

	var firstErr error
	for _, fn := range fns {
		if err := fn(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// pipelinePoolRef bundles the dedicated pipeline pool with the pool name its
// connections carry (pool.Conn.PoolName()), so poolForConn can route a
// connection back to the pool that owns it — e.g. streaming-credentials
// re-auth must close/account a failed pipeline connection against the
// pipeline pool, not connPool. Bundling keeps the pair consistent: it is set
// once in baseClient.pipelinePool before the client is visible, never mutated.
type pipelinePoolRef struct {
	pool *pool.ConnPool
	name string
}

type baseClient struct {
	fixedDialEndpoint bool
	// For a sticky transaction, the pool that actually performs dials.
	dialExhaustionPool *pool.ConnPool
	cscPolicy          cscPolicy
	// apClosed flips when the shared pools begin closing; every wrapper and
	// every clone SHARING those pools refuses to build a new autopipeliner
	// from then on. A pointer: withTimeout/clone copy it, so the flag is one
	// per pool-set, not one per wrapper. See baseClient.Close.
	apClosed *atomic.Bool

	opt        *Options
	optLock    sync.RWMutex
	connPool   pool.Pooler
	pubSubPool *pool.PubSubPool
	// pipelinePool is the dedicated connection pool for pipelining
	// operations (Pipeline, TxPipeline and autopipeline batches), created
	// unconditionally at NewClient/NewFailoverClient — like pubSubPool — with
	// pipeline-appropriate options (see pipelinePoolOptions): larger buffers,
	// no pre-dialing by default (PipelineMinIdleConns), a small connection cap.
	// It is pure burst capacity: an unused pipeline pool holds zero connections
	// (a full-duplex engine keeps one idle spare). PipelinePoolSize < 0 opts
	// out; nil means pipelines use connPool (opt-out, and the internal
	// Conn/Tx/Sentinel wrappers which never create one). The field is set
	// before the client is visible to any goroutine and never mutated after,
	// so plain reads are safe; WithTimeout clones copy the pointer and share
	// the pool.
	pipelinePool *pipelinePoolRef
	hooksMixin

	// rec is where this client's metrics go: its Options.OTelRecorder, or a view
	// of the global recorder (see clientRecorder). poolMetrics is the matching
	// pool callback table, nil when rec is the global view. Both are set before
	// the pools are built and never change.
	rec         otel.Recorder
	poolMetrics *pool.MetricCallbacks

	// onClose holds named callbacks invoked when the client is closed.
	// Registering a new callback never removes previously registered ones;
	// only re-registering the same id replaces the existing callback. This
	// lets composing components (e.g. sentinel failover) add close logic
	// safely without fear of overwriting each other and without building
	// unbounded closure chains on repeated registration.
	onClose *onCloseHooks

	// Push notification processing
	pushProcessor push.NotificationProcessor

	// Maintenance notifications manager
	maintNotificationsManager     *maintnotifications.Manager
	maintNotificationsManagerLock sync.RWMutex

	// streamingCredentialsManager is used to manage streaming credentials
	streamingCredentialsManager *streaming.Manager

	// himport is the client-side registry of HIMPORT fieldsets, used to
	// lazily replay HIMPORT PREPARE onto pooled connections (see himport.go).
	// Shared by clones and by Conn instances derived from the same pool.
	himport *himportRegistry

	// csc is the shared client-side cache; nil when CSC is disabled.
	csc Cache

	// cscKeyPrefix namespaces a shared cache by DB and fixed authentication
	// identity. It is computed once during attachment and copied with the cache.
	cscKeyPrefix string

	// cmdMeta publishes the client's command-metadata views (see
	// command_metadata.go); nil when the client uses the shared static
	// default. Shared with clones; only the owner's Close stops its worker.
	cmdMeta *commandMetadataStore
	// metadataSource is read-only access to a parent's store. Only cmdMeta is owned.
	metadataSource *commandMetadataStore
	// staticCmdMeta caches the lazy default when CSC attaches without a store,
	// so serving a cached command does not need to call sync.OnceValue.
	staticCmdMeta *commandMetadataView

	// Refresh-on-invalidate + reader-miss coalescing (nil unless enabled).
	// cscRefreshQueue IS copied by clone() (a clone signals demand on the owner's
	// queue, see clone()); cscRefreshHandle is owner-only (it drives the goroutine
	// that owns/stops the queue) and stays nil in a clone, so a clone never stops it.
	cscRefreshQueue  *cscRefreshQueue
	cscRefreshHandle *cscRevalidateHandle
	// cscClientWeak points (weakly) back at the canonical *Client wrapper, so
	// the CSC push-handler adapter can close through Client.Close — which also
	// stops the cached autopipeliners — instead of baseClient.Close. WEAK on
	// purpose: a strong back-reference would make the wrapper reachable from
	// the drainer goroutine and the drop-without-Close cleanup
	// (cscRegisterCleanups) could never fire.
	cscClientWeak weak.Pointer[Client]
	// cscMissCoalescer is atomic: a miss (processCached) races Close, which
	// Swaps it to nil — readers Load once (a double-load could call fetch on a
	// nil receiver) and exactly one closer wins the Swap.
	cscMissCoalescer atomic.Pointer[cscMissCoalescer]

	// allowClientTracking exempts a client from the CLIENT TRACKING guard (see
	// process and generalProcessPipeline). Set only on initConn's internal conn
	// wrapper, whose init pipeline legitimately issues CLIENT TRACKING ON;
	// never set on user-visible clients.
	allowClientTracking bool

	// limiterExempt exempts a client from Options.Limiter. Set only on
	// initConn's internal conn wrapper: the handshake it runs (HELLO, AUTH,
	// SELECT, CLIENT SETINFO, CLIENT MAINT_NOTIFICATIONS) belongs to the
	// command that dials the connection, which the caller already admitted and
	// reports as one operation. The OnConnect callback is exempt for the same
	// reason: it sends its commands on that wrapper. Admitting each handshake
	// command again double counts against a rate limiter and, with a circuit
	// breaker in half-open, lets a handshake exhaust the probe budget and report
	// the breaker's own rejection back as a failure, so the breaker never closes.
	limiterExempt bool

	// The following are OWNER-ONLY and NOT copied by clone(): derived clients
	// (Conn/WithTimeout) share the cache but must not stop the owner's
	// goroutines or flush its cache on their own Close.

	// cscOwnsCache is true only when this client constructed its LocalCache (not
	// an injected/shared one); it gates the defensive flush on drainer stop.
	cscOwnsCache bool

	// cscDrainHandle is the background drainer handle (nil when none). Held
	// on the client, not a global registry, so an un-Closed client stays
	// GC-collectible and a runtime.AddCleanup net can stop the goroutine. Its
	// presence also identifies the owner (the only client that runs the drainer
	// and thus the one that deregisters cscPoolHook).
	cscDrainHandle *cscDrainHandle

	// cscPoolHook is the evict-on-remove pool hook (nil when CSC is off). Unlike
	// the owner-only fields above it IS copied by clone(): a clone reads it in
	// processCached to attribute fetches to the shared hook. Only the owner (the
	// one with cscDrainHandle) deregisters it when the drainer exits.
	cscPoolHook pool.PoolHook

	// cscActive is allocated only after CSC attaches successfully and becomes
	// false once the drainer stops (owner Close, GC cleanup, or damping). It is
	// shared with derived clients so they initialize borrowed pool connections
	// with tracking only while the parent's CSC is actually operational.
	cscActive *atomic.Bool
}

func (c *baseClient) clone() *baseClient {
	c.maintNotificationsManagerLock.RLock()
	maintNotificationsManager := c.maintNotificationsManager
	c.maintNotificationsManagerLock.RUnlock()

	clone := &baseClient{
		apClosed:    c.apClosed,
		opt:         c.opt,
		rec:         c.rec,
		poolMetrics: c.poolMetrics,
		connPool:    c.connPool,
		// Pointer copy on purpose: the clone shares the parent's already-created
		// pipeline pool over the one shared pool set, rather than building its own.
		pipelinePool:                c.pipelinePool,
		pubSubPool:                  c.pubSubPool,
		onClose:                     c.onClose,
		pushProcessor:               c.pushProcessor,
		maintNotificationsManager:   maintNotificationsManager,
		streamingCredentialsManager: c.streamingCredentialsManager,
		himport:                     c.himport,
		csc:                         c.csc,
		// cscPoolHook and cscActive travel with the cache (read in processCached);
		// the owner-only fields — cscDrainHandle, cscOwnsCache — do not, so a clone's
		// Close never tears down the owner's resources.
		cscPoolHook:       c.cscPoolHook,
		cscActive:         c.cscActive,
		cscKeyPrefix:      c.cscKeyPrefix,
		cscPolicy:         c.cscPolicy,
		fixedDialEndpoint: c.fixedDialEndpoint,
		// Derived clients share the owner's immutable metadata view.
		cmdMeta:        c.cmdMeta,
		metadataSource: c.metadataSource,
		staticCmdMeta:  c.staticCmdMeta,

		// cscRefreshQueue is SHARED (pointer copy), like cscPoolHook: processCached
		// calls signalDemand on it so a miss for a key still in the refresher's
		// window flushes that window early. Without the share a clone's field is
		// nil and signalDemand no-ops, so the clone's miss waits the full window
		// (#3965 F4). Lifecycle stays owner-only: signalDemand is a nil-safe,
		// non-blocking buffered send that does no I/O, and teardown keys off the
		// owner-only cscRefreshHandle/cscDrainHandle (both nil in a clone), so a
		// clone only SIGNALS the queue and never stops it — even a clone outliving
		// the owner's Close just sends into a buffered channel whose worker has
		// exited, which is harmless.
		cscRefreshQueue: c.cscRefreshQueue,
	}
	// A baseClient built without NewClient (e.g. internal CSC tests) has no
	// hook state; keep the zero mixin instead of nil-dereferencing in clone.
	if c.hooksMixin.state != nil {
		clone.hooksMixin = c.hooksMixin.clone()
	}
	// The miss coalescer travels with the cache (an atomic.Pointer cannot appear
	// in the literal above); a clone shares the OWNER's coalescer, whose
	// lifecycle stays owner-only — a clone's Close does not stop it.
	clone.cscMissCoalescer.Store(c.cscMissCoalescer.Load())
	return clone
}

// cloneOpt clones c.opt while holding optLock to prevent races with initConn
// which writes to MaintNotificationsConfig.Mode under the same lock.
func (c *baseClient) cloneOpt() *Options {
	c.optLock.RLock()
	clone := c.opt.clone()
	c.optLock.RUnlock()
	return clone
}

func (c *baseClient) withTimeout(timeout time.Duration) *baseClient {
	opt := c.cloneOpt()
	opt.ReadTimeout = timeout
	opt.WriteTimeout = timeout

	clone := c.clone()
	clone.opt = opt

	// Do not route the clone's misses through the OWNER's coalescer when the
	// timeouts diverge: the shared engine's writer/reader/acquire budgets and
	// backstops all read the owner's options, so a coalesced miss would honor
	// the owner's deadlines, not the clone's — defeating WithTimeout's whole
	// point. The clone still serves cache hits and still caches its uncached
	// fetches (the pre-coalescer CSC path); only miss COALESCING is bypassed.
	if timeout != c.opt.ReadTimeout || timeout != c.opt.WriteTimeout {
		clone.cscMissCoalescer.Store(nil)
	}
	// cscRefreshQueue is NOT dropped here even when timeouts diverge: unlike the
	// coalescer it does no I/O on this client's behalf — signalDemand is a
	// non-blocking buffered send — so there is no deadline to inherit, and the
	// refresh work itself runs on the owner's connection under the owner's options
	// regardless of who nudged it.

	return clone
}

func (c *baseClient) String() string {
	return fmt.Sprintf("Redis<%s db:%d>", c.getAddr(), c.opt.DB)
}

// limiter returns Options.Limiter, or nil for the handshake conn wrapper that
// initConn builds (see limiterExempt).
func (c *baseClient) limiter() Limiter {
	if c.limiterExempt {
		return nil
	}
	return c.opt.Limiter
}

var errInitPanic = errors.New("redis: connection init panic")

// admitted runs fn as one admitted operation against limiter: one Allow and
// one ReportResult, settled from a defer when fn panics so the admission is
// never left unpaired. denied runs when Allow rejects, before the error
// returns. A nil limiter runs fn as is.
func admitted(limiter Limiter, denied func(error), fn func() error) error {
	if limiter == nil {
		return fn()
	}
	if err := limiter.Allow(); err != nil {
		if denied != nil {
			denied(err)
		}
		return err
	}
	settled := false
	defer func() {
		if !settled {
			limiter.ReportResult(errInitPanic)
		}
	}()
	err := fn()
	settled = true
	limiter.ReportResult(err)
	return err
}

// newPubSubConn dials, initializes, and tracks a PubSub connection as one
// admitted operation against Options.Limiter.
func (c *baseClient) newPubSubConn(ctx context.Context, addr string, channels []string, onInit ...func(*pool.Conn)) (*pool.Conn, error) {
	var cn *pool.Conn
	err := admitted(c.limiter(), nil, func() error {
		var err error
		cn, err = c.pubSubPool.NewConn(ctx, c.opt.Network, addr, channels)
		if err == nil {
			for _, fn := range onInit {
				if fn != nil {
					fn(cn)
				}
			}
			err = c.initConn(ctx, cn)
		}
		if err == nil {
			err = c.pubSubPool.TrackConn(cn)
		}
		if err != nil && cn != nil {
			_ = cn.Close()
			c.removeStreamingCredentialsState(cn)
			cn = nil
		}
		return err
	})
	return cn, err
}

func (c *baseClient) getConn(ctx context.Context) (*pool.Conn, error) {
	cn, _, err := c.getConnLimited(ctx)
	return cn, err
}

// getConnLimited is getConn with the Limiter admission split from the dial: the
// `limited` return reports that Limiter.Allow REJECTED the operation (before any
// dial), so a caller can tell an explicit admission denial apart from a
// dial/transport failure. getConn wraps it for the common case, leaving every
// other caller unchanged. Allow is reported (ReportResult) only for dial results,
// never for admission denials — matching the original getConn. A panic inside
// _getConn settles the admission too, as admitted() does for the other paths, so
// an Allow is never left unpaired.
func (c *baseClient) getConnLimited(ctx context.Context) (cn *pool.Conn, limited bool, err error) {
	limiter := c.limiter()
	if limiter == nil {
		cn, err = c._getConn(ctx)
		return cn, false, err
	}
	if err := limiter.Allow(); err != nil {
		return nil, true, err
	}

	settled := false
	defer func() {
		if !settled {
			limiter.ReportResult(errInitPanic)
		}
	}()

	cn, err = c._getConn(ctx)
	settled = true
	if err != nil {
		limiter.ReportResult(err)
		return nil, false, err
	}

	return cn, false, nil
}

// initConnWaitContext caps ctx at dialTimeout, but never extends a deadline the
// caller already set. A non-positive dialTimeout means the option is disabled
// (ParseURL uses a value of 0 or less for that), so ctx is returned unchanged:
// context.WithTimeout would expire the wait immediately.
func initConnWaitContext(ctx context.Context, dialTimeout time.Duration) (context.Context, context.CancelFunc) {
	if dialTimeout <= 0 {
		return ctx, nil
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < dialTimeout {
		return ctx, nil
	}
	return context.WithTimeout(ctx, dialTimeout)
}

func (c *baseClient) _getConn(ctx context.Context) (*pool.Conn, error) {
	cn, err := c.connPool.Get(ctx)
	if err != nil {
		return nil, err
	}

	if err := c.initPooledConn(ctx, c.connPool, cn); err != nil {
		return nil, err
	}

	return cn, nil
}

// initPooledConnLimited runs initPooledConn as one admitted operation against
// Options.Limiter, for a caller that leases outside getConnLimited. A denial
// removes the leased conn, the same disposition as a failed init. limited is
// true only when Allow rejects, so callers can avoid falling back on a denial.
func (c *baseClient) initPooledConnLimited(ctx context.Context, p pool.Pooler, cn *pool.Conn) (limited bool, err error) {
	if cn.IsInited() {
		return false, nil
	}
	err = admitted(c.limiter(), func(err error) {
		limited = true
		p.Remove(ctx, cn, err)
	}, func() error {
		return c.initPooledConn(ctx, p, cn)
	})
	return limited, err
}

// initPooledConn brings a conn freshly obtained from p to a usable state: it
// runs the connection handshake if needed, records the connection-create-time
// metric, and re-acquires the conn after initConn parks it IDLE. On failure
// the conn is Removed from p (never leaked) and the error is unwrapped to the
// caller-visible cause. Shared by the main-pool path (_getConn) and the
// dedicated pipeline-pool path (withPipelineConn) so the two cannot drift.
func (c *baseClient) initPooledConn(ctx context.Context, p pool.Pooler, cn *pool.Conn) error {
	if cn.IsInited() {
		return nil
	}

	if err := c.initConn(ctx, cn); err != nil {
		p.Remove(ctx, cn, err)
		if unwrapped := errors.Unwrap(err); unwrapped != nil {
			return unwrapped
		}
		return err
	}

	if dialStartNs := cn.GetDialStartNs(); dialStartNs > 0 && otel.Enabled(c.recorder()) {
		c.recorder().RecordConnectionCreateTime(ctx, time.Duration(time.Now().UnixNano()-dialStartNs), cn)
	}

	// initConn will transition to IDLE state, so we need to acquire it
	// before returning it to the user.
	if !cn.TryAcquire() {
		err := fmt.Errorf("redis: connection is not usable")
		// Remove rather than abandon: an unacquirable conn left outside the
		// pool's accounting would leak its slot.
		p.Remove(ctx, cn, err)
		return err
	}

	return nil
}

// loadPipelinePool returns the pipeline-pool ref, or nil when pipelines use
// the main pool (PipelinePoolSize < 0, or an internal wrapper client).
func (c *baseClient) loadPipelinePool() *pipelinePoolRef {
	return c.pipelinePool
}

// getPipelinePool returns the dedicated pipeline pool as a pool.Pooler, or a
// true nil interface when there is none. Callers must use this rather than
// wrapping loadPipelinePool().pool themselves where a pool.Pooler is expected:
// a typed-nil *pool.ConnPool inside the interface would defeat `!= nil` checks.
func (c *baseClient) getPipelinePool() pool.Pooler {
	if ref := c.loadPipelinePool(); ref != nil {
		return ref.pool
	}
	return nil
}

// isPipelinePoolConn reports whether cn was dialed by the dedicated pipeline
// pool, identified by the pool name its connections carry.
func (c *baseClient) isPipelinePoolConn(cn *pool.Conn) bool {
	ref := c.loadPipelinePool()
	return ref != nil && cn.PoolName() == ref.name
}

// poolForConn returns the pool that owns cn — the dedicated pipeline pool when
// cn was dialed there, otherwise the main pool. Re-auth close/accounting must
// target the owning pool so a failed pipeline connection is removed from the
// pipeline pool's books, not the main pool's.
func (c *baseClient) poolForConn(cn *pool.Conn) pool.Pooler {
	if ref := c.loadPipelinePool(); ref != nil && cn.PoolName() == ref.name {
		return ref.pool
	}
	return c.connPool
}

// pipelinePoolOptions resolves the Options the dedicated pipeline pool is
// built with. Pure function of the client options, so the resolution rules are
// testable without dialing anything:
//
//   - Buffers: the explicit pipeline buffer size when set; otherwise the
//     LARGER of the regular buffer size and DefaultPipelineBufferSize.
//     Pipelines move whole batches per round trip, so their connections earn
//     bigger buffers than regular per-command traffic (measured: throughput
//     plateaus around 64 KiB and very large buffers can regress it). The
//     RESP3 minimum clamp applies as on the main pool.
//   - PoolSize: PipelinePoolSize when set, DefaultPipelinePoolSize otherwise.
//   - MinIdleConns: PipelineMinIdleConns (negative means 0), never the main
//     pool's MinIdleConns. The pipeline pool is burst capacity — by default its
//     connections dial on demand. Inheriting MinIdleConns would pre-dial that
//     many pipeline connections at creation, silently doubling a client's idle
//     footprint. A full-duplex engine raises the floor at runtime
//     (RaiseMinIdleConns) to keep a warm spare.
func pipelinePoolOptions(opt *Options) *Options {
	pipelineOpt := opt.clone()
	if opt.PipelineReadBufferSize > 0 {
		pipelineOpt.ReadBufferSize = opt.PipelineReadBufferSize
	} else if pipelineOpt.ReadBufferSize < DefaultPipelineBufferSize {
		pipelineOpt.ReadBufferSize = DefaultPipelineBufferSize
	}
	// Same clamp Options.init applies to the main pool: RESP3 push parsing
	// needs a minimum read buffer, and a tiny pipeline reader would break
	// push-notification handling on pipeline conns.
	if pipelineOpt.Protocol == 3 && pipelineOpt.ReadBufferSize < proto.MinRESP3ReadBufferSize {
		pipelineOpt.ReadBufferSize = proto.MinRESP3ReadBufferSize
	}
	if opt.PipelineWriteBufferSize > 0 {
		pipelineOpt.WriteBufferSize = opt.PipelineWriteBufferSize
	} else if pipelineOpt.WriteBufferSize < DefaultPipelineBufferSize {
		pipelineOpt.WriteBufferSize = DefaultPipelineBufferSize
	}
	if opt.PipelinePoolSize > 0 {
		pipelineOpt.PoolSize = opt.PipelinePoolSize
	} else {
		pipelineOpt.PoolSize = DefaultPipelinePoolSize
	}
	// No pre-dial by default (an unused pipeline pool holds nothing); a negative
	// value means 0 here and also turns off the full-duplex spare.
	pipelineOpt.MinIdleConns = max(opt.PipelineMinIdleConns, 0)
	// Re-resolve MaxConcurrentDials for the pipeline pool. Options.init capped it to
	// the MAIN pool size, so a pipeline pool larger than the main pool (e.g.
	// PoolSize:1 with the default 10-slot pipeline pool) would otherwise dial one
	// connection at a time — an initial burst serializes slow dials and can hit
	// PoolTimeout or spill despite idle pipeline slots. If the caller set an EXPLICIT
	// dial cap, honor it (bounded by the pipeline pool size); otherwise default to
	// the pipeline pool size. Keyed on maxConcurrentDialsSet, NOT equality with
	// PoolSize, so an explicit MaxConcurrentDials==PoolSize (e.g. PoolSize:1,
	// MaxConcurrentDials:1) is not mistaken for the default and silently widened.
	if opt.maxConcurrentDialsSet {
		if pipelineOpt.MaxConcurrentDials > pipelineOpt.PoolSize {
			pipelineOpt.MaxConcurrentDials = pipelineOpt.PoolSize
		}
	} else {
		pipelineOpt.MaxConcurrentDials = pipelineOpt.PoolSize
	}
	// Do NOT inherit MaxActiveConns. Inheriting it verbatim would roughly DOUBLE
	// the client's socket ceiling (e.g. MaxActiveConns 100 -> up to ~200 across
	// the two pools). Reset to 0 so the pipeline pool is bounded only by its own
	// PoolSize: the effective ceiling becomes MaxActiveConns + PipelinePoolSize —
	// a small, bounded addition rather than a doubling, and the main pool a burst
	// spills to still enforces MaxActiveConns. (Consequence: with MaxActiveConns
	// 0 the pipeline pool never itself returns ErrPoolExhausted; a burst wider
	// than PoolSize spills on ErrPoolTimeout instead — see withPipelineConn.)
	pipelineOpt.MaxActiveConns = 0
	// Ordinary pipelines acquire with TryGet and spill at once when the pool
	// is full, without waiting out this timeout. Native full-duplex sessions
	// with CSC use Get instead so they stay on untracked connections. Bound
	// that wait while respecting a shorter positive timeout from the caller.
	pipelineOpt.PoolTimeout = DefaultPipelinePoolTimeout
	if opt.PoolTimeout > 0 && opt.PoolTimeout < DefaultPipelinePoolTimeout {
		pipelineOpt.PoolTimeout = opt.PoolTimeout
	}
	return pipelineOpt
}

// buildPipelinePool constructs the dedicated pipeline pool from the client's
// options as resolved by pipelinePoolOptions. Shared by the NewClient and
// NewFailoverClient creation paths so they cannot drift.
func (c *baseClient) buildPipelinePool(poolName string) (*pipelinePoolRef, error) {
	p, err := newConnPool(pipelinePoolOptions(c.opt), c.dialHook, poolName, c.poolMetrics)
	if err != nil {
		return nil, err
	}
	return &pipelinePoolRef{pool: p, name: poolName}, nil
}

// initStreamingCredentials creates the streaming credentials manager and
// registers its pool hook on the main and pipeline pools. Shared by the
// NewClient and NewFailoverClient creation paths so they cannot drift; must
// run after the pipeline pool is built.
func (c *baseClient) initStreamingCredentials() {
	if c.opt.StreamingCredentialsProvider == nil {
		return
	}
	// Size the re-auth worker semaphore for the COMBINED ceiling of every pool
	// the hook is registered on: the same hook drives the main pool AND the
	// (possibly larger) dedicated pipeline pool, so a credential rotation must
	// be able to re-AUTH connections from both concurrently. Sizing to only the
	// main PoolSize would serialize pipeline re-auths behind PoolSize workers,
	// leaving pipeline capacity unavailable well past reAuthTimeout.
	workers := c.connPool.Size()
	if pp := c.getPipelinePool(); pp != nil {
		workers += pp.Size()
	}
	c.streamingCredentialsManager = streaming.NewManagerWithWorkers(c.connPool, c.opt.PoolTimeout, workers)
	c.connPool.AddPoolHook(c.streamingCredentialsManager.PoolHook())
	if pp := c.getPipelinePool(); pp != nil {
		pp.AddPoolHook(c.streamingCredentialsManager.PoolHook())
	}
}

func (c *baseClient) reAuthConnection() func(poolCn *pool.Conn, credentials auth.Credentials) error {
	return func(poolCn *pool.Conn, credentials auth.Credentials) error {
		username, password := credentials.BasicAuth()
		if poolCn.IsPubSub() {
			if fn := poolCn.GetPubSubReAuth(); fn != nil {
				return fn(username, password)
			}
			return errors.New("redis: pubsub connection is not ready for re-authentication")
		}

		var err error

		// Use background context - timeout is handled by ReadTimeout in WithReader/WithWriter
		ctx := context.Background()

		connPool := pool.NewSingleConnPool(c.poolForConn(poolCn), poolCn)

		// Pass hooks so that reauth commands are recorded/traced; share the
		// HIMPORT registry for the same reason as in initConn.
		cn := newConn(c.opt, connPool, &c.hooksMixin, c.himport)

		if username != "" {
			err = cn.AuthACL(ctx, username, password).Err()
		} else {
			err = cn.Auth(ctx, password).Err()
		}

		return err
	}
}

func (c *baseClient) onAuthenticationErr() func(poolCn *pool.Conn, err error) {
	return func(poolCn *pool.Conn, err error) {
		if err != nil {
			if poolCn.IsPubSub() {
				internal.Logger.Printf(context.Background(), "redis: pubsub re-authentication failed: %v", err)
				return
			}
			// A server-side rejection of the re-auth AUTH (WRONGPASS, NOPERM) is
			// not a bad-conn error, so isBadConn alone would leave the connection
			// pooled while it is still authenticated with the credentials the
			// provider has already replaced. Retire it too, so the reconnect
			// authenticates with the provider's current credentials. Other server
			// errors keep the current path.
			if isBadConn(err, false, c.opt.Addr) || IsAuthError(err) || IsPermissionError(err) {
				// Close the connection to force a reconnection.
				// Re-auth happens on connections that were idle in the pool (the pool hook
				// waits for IDLE state before transitioning to UNUSABLE for re-auth).
				// From metrics perspective, the connection was never "used" by a client.
				// Note: Using context.Background() as this callback doesn't have access to caller's context.
				// Mark the conn CLOSED first so a concurrent popIdle cannot acquire it
				// between OnRemove and its removal from the idle list.
				poolCn.GetStateMachine().Transition(pool.StateClosed)
				err := c.poolForConn(poolCn).CloseConn(context.Background(), poolCn, pool.CloseReasonAuthError, pool.MetricStateIdle)
				if err != nil {
					internal.Logger.Printf(context.Background(), "redis: failed to close connection: %v", err)
					// try to close the network connection directly
					// so that no resource is leaked
					err := poolCn.Close()
					if err != nil {
						internal.Logger.Printf(context.Background(), "redis: failed to close network connection: %v", err)
					}
				}
			}
			internal.Logger.Printf(context.Background(), "redis: re-authentication failed: %v", err)
		}
	}
}

// resolveCredentials returns the username/password to authenticate with, using
// the non-streaming credential sources in precedence order:
// CredentialsProviderContext, then CredentialsProvider, then the static
// Username/Password fields. The StreamingCredentialsProvider path is handled
// separately by initConn (it requires per-connection listener wiring) and is
// intentionally not covered here. Returns empty strings when no credentials
// are configured.
func (opt *Options) resolveCredentials(ctx context.Context) (username, password string, err error) {
	switch {
	case opt.CredentialsProviderContext != nil:
		return opt.CredentialsProviderContext(ctx)
	case opt.CredentialsProvider != nil:
		username, password = opt.CredentialsProvider()
	case opt.Username != "" || opt.Password != "":
		username, password = opt.Username, opt.Password
	}
	return username, password, nil
}

// sendAuth reports whether initConn authenticates with the given credentials:
// always with a password, and with a username alone only when
// AuthEmptyPassword is set.
func (opt *Options) sendAuth(username, password string) bool {
	return password != "" || (username != "" && opt.AuthEmptyPassword)
}

func (c *baseClient) initConn(ctx context.Context, cn *pool.Conn) error {
	// Dial paths such as PubSub do not pass through command submission.
	// Handshake commands and OnConnect must not inherit application cache intent.
	ctx = cscUnmarkedContext(ctx)
	// This function is called in two scenarios:
	// 1. First-time init: Connection is in CREATED state (from pool.Get())
	//    - We need to transition CREATED → INITIALIZING and do the initialization
	//    - If another goroutine is already initializing, we WAIT for it to finish
	// 2. Re-initialization: Connection is in INITIALIZING state (from SetNetConnAndInitConn())
	//    - We're already in INITIALIZING, so just proceed with initialization

	currentState := cn.GetStateMachine().GetState()
	if currentState == pool.StateClosed {
		return pool.ErrClosed
	}

	// Fast path: Check if already initialized (IDLE or IN_USE)
	if currentState == pool.StateIdle || currentState == pool.StateInUse {
		return nil
	}

	// If in CREATED state, try to transition to INITIALIZING
	if currentState == pool.StateCreated {
		finalState, err := cn.GetStateMachine().TryTransition([]pool.ConnState{pool.StateCreated}, pool.StateInitializing)
		if err != nil {
			// Another goroutine is initializing or connection is in unexpected state
			// Check what state we're in now
			if finalState == pool.StateIdle || finalState == pool.StateInUse {
				// Already initialized by another goroutine
				return nil
			}

			if finalState == pool.StateInitializing {
				// Another goroutine is initializing - WAIT for it to complete.
				// The wait is capped at DialTimeout while respecting a sooner command
				// deadline. A non-positive DialTimeout means the option is disabled
				// (ParseURL uses 0 or less for that), so it must not cap the wait at
				// all: context.WithTimeout(ctx, 0) expires the wait immediately, the
				// same way it would have with the -1 sentinel. pool.go guards its own
				// WithTimeout calls with "DialTimeout > 0"; this path does the same.
				waitCtx, cancel := initConnWaitContext(ctx, c.opt.DialTimeout)
				if cancel != nil {
					defer cancel()
				}

				finalState, err := cn.GetStateMachine().AwaitAndTransition(
					waitCtx,
					[]pool.ConnState{pool.StateIdle, pool.StateInUse},
					pool.StateIdle, // Target is IDLE (but we're already there, so this is a no-op)
				)
				if err != nil {
					return err
				}
				// Verify we're now initialized
				if finalState == pool.StateIdle || finalState == pool.StateInUse {
					return nil
				}
				// Unexpected state after waiting
				return fmt.Errorf("connection in unexpected state after initialization: %s", finalState)
			}

			// Unexpected state (CLOSED, UNUSABLE, etc.)
			return err
		}
	}

	// At this point, we're in INITIALIZING state and we own the initialization
	// If we fail, we must transition to CLOSED
	var initErr error
	connPool := pool.NewSingleConnPool(c.connPool, cn)
	// The handshake Conn (handed to OnConnect) must share the client's
	// HIMPORT registry: a private registry restarts versions at 1, so an
	// OnConnect prepare would mark the pooled connection with a version
	// number that collides with the client registry's and silently skips
	// the replay of a different fieldset definition.
	conn := newConn(c.opt, connPool, &c.hooksMixin, c.himport)
	// The internal wrapper does not serve cached reads, but it needs the
	// successful-attachment signal both to issue CLIENT TRACKING during init
	// and to guard the user-visible OnConnect callback below.
	conn.baseClient.cscActive = c.cscActive
	// Share the policy and tracking generations so uncached OnConnect reads
	// can suppress tracking on an OPTOUT connection.
	conn.baseClient.cscPolicy = c.cscPolicy
	conn.baseClient.cscPoolHook = c.cscPoolHook
	// Custom CommandMetadata lives on the parent store; without it, OnConnect
	// reads of overridden commands fall back to the static table and skip NO.
	c.shareCommandMetadata(&conn.baseClient)
	// This internal conn's init pipeline issues CLIENT TRACKING ON itself;
	// exempt it from the guard that blocks user-issued CLIENT TRACKING. Setting
	// the field after newConn is safe: initHooks bound the pipeline hook as a
	// method value on the addressable baseClient, so the guard reads the
	// updated field.
	conn.baseClient.allowClientTracking = true
	conn.baseClient.limiterExempt = true

	username, password := "", ""
	if c.opt.StreamingCredentialsProvider != nil {
		credListener, initErr := c.streamingCredentialsManager.Listener(
			cn,
			c.reAuthConnection(),
			c.onAuthenticationErr(),
		)
		if initErr != nil {
			cn.GetStateMachine().Transition(pool.StateClosed)
			return fmt.Errorf("failed to create credentials listener: %w", initErr)
		}

		credentials, unsubscribeFromCredentialsProvider, initErr := subscribeStreamingCredentials(
			ctx, c.opt.StreamingCredentialsProvider, credListener,
		)
		if initErr != nil {
			cn.GetStateMachine().Transition(pool.StateClosed)
			return fmt.Errorf("failed to subscribe to streaming credentials: %w", initErr)
		}

		// Per-connection unsubscribe is attached to the connection itself so it
		// runs when this specific connection is closed. Do not register it on
		// c.onClose: initConn runs for every (re)initialized connection, and
		// attaching per-connection state to the shared baseClient registry would
		// either leak entries (one per connection id, never trimmed) or — with
		// the pre-fix wrappedOnClose approach — build an unbounded closure chain
		// retaining every prior connection's unsubscribe (see issue #3772).
		//
		// Note: pool.Conn.SetOnClose OVERWRITES any prior callback (see the
		// doc on that method). That is safe here because the streaming
		// credentials Manager deduplicates listeners by connection id, so a
		// second initConn on the same cn re-Subscribes the SAME listener and
		// the returned unsubscribe is equivalent to the one already installed.
		// Any future code path that could hand out a distinct unsubscribe on
		// re-initialization must first invoke the existing one to avoid
		// orphaning the old subscription on the credentials provider.
		onClose := unsubscribeFromCredentialsProvider
		if cn.IsPubSub() {
			// A pub/sub connection never returns to the pool, so the re-auth
			// pool hook never removes its state. Remove it on every close
			// path: PubSub.Close, a reconnect and PubSubPool.Close. The
			// connection is already closed here, so a concurrent OnNext can
			// no longer add state afterwards (see MarkForReAuth).
			manager, connID := c.streamingCredentialsManager, cn.GetID()
			onClose = func() error {
				err := unsubscribeFromCredentialsProvider()
				manager.RemoveConn(connID)
				return err
			}
		}
		cn.SetOnClose(onClose)
		// Close may have consumed the callback before Subscribe returned.
		// Re-run teardown after publishing it so a late subscription is not lost.
		if cn.IsClosed() {
			_ = cn.Close()
			return fmt.Errorf("connection closed during streaming credentials setup: %w", pool.ErrClosed)
		}

		username, password = credentials.BasicAuth()
	} else {
		username, password, initErr = c.opt.resolveCredentials(ctx)
		if initErr != nil {
			cn.GetStateMachine().Transition(pool.StateClosed)
			return fmt.Errorf("failed to resolve credentials: %w", initErr)
		}
	}

	// for redis-server versions that do not support the HELLO command,
	// RESP2 will continue to be used.
	// helloOK tracks whether HELLO succeeded. If it did not, the connection
	// falls back to RESP2 regardless of c.opt.Protocol, and features that
	// require RESP3 (e.g. maintenance notifications) must be skipped.
	helloOK := false
	// For redis-server versions that do not support HELLO, RESP2 continues to
	// be used. Remember that negotiated fallback: configured Protocol remains 3,
	// but CSC must not serve without RESP3 invalidations.
	helloFallbackToRESP2 := false
	sendAuth := c.opt.sendAuth(username, password)
	helloCmd := conn.hello(ctx, c.opt.Protocol, username, password, c.opt.ClientName, sendAuth)
	if initErr = helloCmd.Err(); initErr == nil {
		// Authentication successful with HELLO command
		helloOK = true
	} else if !isRedisError(initErr) {
		// When the server responds with the RESP protocol and the result is not a normal
		// execution result of the HELLO command, we consider it to be an indication that
		// the server does not support the HELLO command.
		// The server may be a redis-server that does not support the HELLO command,
		// or it could be DragonflyDB or a third-party redis-proxy. They all respond
		// with different error string results for unsupported commands, making it
		// difficult to rely on error strings to determine all results.
		cn.GetStateMachine().Transition(pool.StateClosed)
		return initErr
	} else {
		helloFallbackToRESP2 = c.opt.Protocol == 3
		if sendAuth {
			// Try legacy AUTH command if HELLO failed.
			if username != "" {
				initErr = conn.AuthACL(ctx, username, password).Err()
			} else {
				initErr = conn.Auth(ctx, password).Err()
			}
			if initErr != nil {
				cn.GetStateMachine().Transition(pool.StateClosed)
				return fmt.Errorf("failed to authenticate: %w", initErr)
			}
		}
	}
	if helloFallbackToRESP2 {
		c.disableCSCServing(ctx, "HELLO 3 was rejected and the connection negotiated RESP2")
	}
	cn.SetResp3(c.opt.Protocol == 3 && helloOK)

	// trackingEnabled reports whether THIS pool connection must issue
	// CLIENT TRACKING ON during init. True when CSC (SharedTracking) is enabled:
	// the shared cache is fed by per-connection tracking + the background
	// drainer. Once CSC serving stops (owner Close, GC cleanup, or drainer
	// damping), new and re-inited conns skip tracking — nothing consumes the
	// pushes into the cache anymore.
	// Pipeline-pool connections are excluded from CLIENT TRACKING: pipelined
	// commands never consult or populate the client-side cache (only the
	// single-command cached path on main-pool connections does), so tracking
	// reads made on pipeline connections would only grow the server's tracking
	// table and produce invalidation pushes for keys the cache does not hold.
	trackingEnabled := !helloFallbackToRESP2 && !cn.IsPubSub() && c.cscTrackingRequested() &&
		!c.isPipelinePoolConn(cn)
	if trackingEnabled && c.cscConnInitGen(cn.GetID()) == 0 {
		// First initialization establishes generation 1. Reinitialization
		// already bumped and evicted through onCscReinit before replacing the
		// socket, so it must not bump a second time here.
		c.cscEvictOwnedEntries(cn.GetID())
	} else if !trackingEnabled && c.cscConnInitGen(cn.GetID()) != 0 {
		// A re-init after CSC stopped skips TRACKING ON: the new socket is
		// untracked, so it must not keep the OPTOUT NO prefix (#4236).
		c.cscForgetConn(cn.GetID())
	}
	var trackingCmd *StatusCmd
	initCmds, initErr := conn.Pipelined(ctx, func(pipe Pipeliner) error {
		if c.opt.DB > 0 {
			pipe.Select(ctx, c.opt.DB)
		}

		if c.opt.readOnly {
			pipe.ReadOnly(ctx)
		}

		if c.opt.ClientName != "" {
			pipe.ClientSetName(ctx, c.opt.ClientName)
		}

		if trackingEnabled {
			// Must run before any cacheable command is issued on this conn.
			trackingCmd = pipe.ClientTrackingOn(ctx, &ClientTrackingOptions{OptIn: c.cscPolicy.mode == TrackingModeOptIn, OptOut: c.cscPolicy.mode == TrackingModeOptOut})
		}

		return nil
	})
	// The exemption is init-only. OnConnect is user code and must go through
	// the same CSC connection-state guard as every other public command path.
	conn.baseClient.allowClientTracking = false
	trackingRejected := trackingCmd != nil && isRedisError(trackingCmd.Err())
	for _, cmd := range initCmds {
		if cmd != trackingCmd && cmd.Err() != nil {
			trackingRejected = false
			break
		}
	}
	if trackingRejected {
		// A server-side rejection means tracking is unavailable, but the
		// connection and the preceding init commands are still usable. Disable
		// CSC globally and continue without caching. Transport and protocol
		// failures still take the normal connection-failure path below.
		c.disableCSCServing(ctx, fmt.Sprintf("CLIENT TRACKING ON was rejected: %v", trackingCmd.Err()))
		c.cscForgetConn(cn.GetID())
		trackingEnabled = false
		initErr = nil
	}
	if initErr != nil {
		if trackingEnabled {
			// cscEvictOwnedEntries above bumped this conn's init generation; a
			// failed init never serves, and the pubsub path has no OnRemove
			// hook (and the close hook below is not yet installed), so drop
			// the entry here to keep the map bounded to live conns.
			c.cscForgetConn(cn.GetID())
		}
		cn.GetStateMachine().Transition(pool.StateClosed)
		return fmt.Errorf("failed to initialize connection options: %w", initErr)
	}

	if trackingEnabled {
		// Evict this conn's entries on any close (incl. the ConnMaxLifetime/idle
		// path that bypasses the OnRemove hook), since the server drops its
		// tracking table on close.
		c.cscInstallConnCloseHook(cn)
		// A handoff replaces the socket before initConn runs. Bump and evict at
		// the pre-swap boundary so fulfillCached cannot publish an old-socket
		// reply during that gap.
		c.cscInstallConnReinitHook(cn)
	}

	// Enable maintnotifications if maintnotifications are configured
	c.optLock.RLock()
	maintNotifEnabled := c.opt.MaintNotificationsConfig != nil && c.opt.MaintNotificationsConfig.Mode != maintnotifications.ModeDisabled
	protocol := c.opt.Protocol
	var endpointType maintnotifications.EndpointType
	var maintNotifMode maintnotifications.Mode
	if maintNotifEnabled {
		endpointType = c.opt.MaintNotificationsConfig.EndpointType
		maintNotifMode = c.opt.MaintNotificationsConfig.Mode
	}
	c.optLock.RUnlock()

	// Maintenance notifications require RESP3 push frames. If HELLO failed
	// and the connection fell back to RESP2, there is no point in sending
	// CLIENT MAINT_NOTIFICATIONS: the server either rejects it (making the
	// error misleading) or accepts it silently, leaving the client unable
	// to receive any notifications. Decide based on the actual negotiated
	// protocol rather than the requested one.
	if maintNotifEnabled && protocol == 3 && !helloOK {
		if maintNotifMode == maintnotifications.ModeEnabled {
			// Explicitly requested - fail fast with a clear reason.
			cn.GetStateMachine().Transition(pool.StateClosed)
			c.recorder().RecordError(ctx, "HANDSHAKE_FAILED", cn, "HANDSHAKE_FAILED", true, 0)
			return fmt.Errorf("failed to enable maintnotifications: server does not support RESP3 (HELLO command failed)")
		}
		// auto/other modes: silently disable maintnotifications for this client.
		c.optLock.Lock()
		c.opt.MaintNotificationsConfig.Mode = maintnotifications.ModeDisabled
		c.optLock.Unlock()
		if err := c.disableMaintNotificationsUpgrades(); err != nil {
			internal.Logger.Printf(ctx, "failed to disable maintnotifications in auto mode: %v", err)
		}
		maintNotifEnabled = false
	}

	var maintNotifHandshakeErr error
	if maintNotifEnabled && protocol == 3 {
		// Hold the manager read lock across the handshake and tracking so a
		// concurrent downgrade cannot remove pool-level listeners before a
		// successfully enabled connection is tracked for retirement.
		c.maintNotificationsManagerLock.RLock()
		manager := c.maintNotificationsManager
		maintNotifHandshakeErr = conn.ClientMaintNotifications(
			ctx,
			true,
			endpointType.String(),
		).Err()
		// A successful handshake enables maintnotifications for this connection,
		// but must not promote ModeAuto to ModeEnabled. ModeEnabled is the
		// explicit fail-closed policy; ModeAuto must remain able to downgrade if a
		// later reconnect/failover reaches an endpoint that rejects the command.
		if maintNotifHandshakeErr == nil && manager != nil {
			manager.TrackMaintNotificationsConn(cn)
		}
		c.maintNotificationsManagerLock.RUnlock()
		if maintNotifHandshakeErr != nil {
			if !isRedisError(maintNotifHandshakeErr) {
				// if not redis error, fail the connection
				cn.GetStateMachine().Transition(pool.StateClosed)
				return maintNotifHandshakeErr
			}
			c.optLock.Lock()
			// handshake failed - check and modify config atomically
			switch c.opt.MaintNotificationsConfig.Mode {
			case maintnotifications.ModeEnabled:
				// enabled mode, fail the connection
				c.optLock.Unlock()
				cn.GetStateMachine().Transition(pool.StateClosed)

				// Record handshake failure metric
				c.recorder().RecordError(ctx, "HANDSHAKE_FAILED", cn, "HANDSHAKE_FAILED", true, 0)

				return fmt.Errorf("failed to enable maintnotifications: %w", maintNotifHandshakeErr)
			default: // will handle auto and any other
				// Disabling logging here as it's too noisy.
				// TODO: Enable when we have a better logging solution for log levels
				// internal.Logger.Printf(ctx, "auto mode fallback: maintnotifications disabled due to handshake error: %v", maintNotifHandshakeErr)
				c.opt.MaintNotificationsConfig.Mode = maintnotifications.ModeDisabled
				c.optLock.Unlock()
				// auto mode, disable maintnotifications and continue
				if initErr := c.disableMaintNotificationsUpgrades(); initErr != nil {
					// Log error but continue - auto mode should be resilient
					internal.Logger.Printf(ctx, "failed to disable maintnotifications in auto mode: %v", initErr)
				}
			}
		}
	}

	if !c.opt.DisableIdentity && !c.opt.DisableIndentity {
		libName := ""
		libVer := Version()
		if c.opt.IdentitySuffix != "" {
			libName = c.opt.IdentitySuffix
		}
		p := conn.Pipeline()
		p.ClientSetInfo(ctx, WithLibraryName(libName))
		p.ClientSetInfo(ctx, WithLibraryVersion(libVer))
		// Handle network errors (e.g. timeouts) in CLIENT SETINFO to avoid
		// out of order responses later on.
		if _, initErr = p.Exec(ctx); initErr != nil && !isRedisError(initErr) {
			cn.GetStateMachine().Transition(pool.StateClosed)
			return initErr
		}
	}

	// Set the connection initialization function for potential reconnections
	// This must be set before transitioning to IDLE so that a handoff can use it
	cn.SetInitConnFunc(c.createInitConnFunc())

	// Initialization succeeded - transition to IDLE state
	// This marks the connection as initialized and ready for use
	// NOTE: The connection is still owned by the calling goroutine at this point
	// and won't be available to other goroutines until it's Put() back into the pool
	// Do not overwrite a Close that raced the final handshake reply.
	for {
		state := cn.GetStateMachine().GetState()
		if state == pool.StateClosed {
			return pool.ErrClosed
		}
		if _, err := cn.GetStateMachine().TryTransition([]pool.ConnState{state}, pool.StateIdle); err == nil {
			break
		}
	}

	// Call OnConnect hook if configured
	// The connection is in IDLE state but still owned by this goroutine
	// If OnConnect needs to send commands, it can use the connection safely
	if c.opt.OnConnect != nil {
		if initErr = c.opt.OnConnect(ctx, conn); initErr != nil {
			// OnConnect failed - transition to closed
			cn.GetStateMachine().Transition(pool.StateClosed)
			return initErr
		}
	}

	if c.cmdMeta.wantsServerHello() {
		// Without HELLO, a reconnect must verify the old identity in the
		// background.
		if helloOK {
			c.cmdMeta.onServerHello(helloServerFingerprint(helloCmd.Val()))
		} else {
			c.cmdMeta.requestVerification()
		}
	}
	if c.opt.onServerHello != nil {
		if helloOK {
			c.opt.onServerHello(helloCmd.Val())
		} else {
			c.opt.onServerHello(nil)
		}
	}

	return nil
}

// errConnUnusable marks a connection whose reply stream is desynchronized (a
// partial push-frame drain, or a panic mid-serialization) but whose error is not a
// transport bad-conn error. releaseConnToPool removes such a conn instead of
// returning it to the pool. Wrap the real cause with %w so callers still see it.
var errConnUnusable = errors.New("redis: connection unusable (reply stream desynchronized)")

func (c *baseClient) releaseConn(ctx context.Context, cn *pool.Conn, err error) {
	if limiter := c.limiter(); limiter != nil {
		limiter.ReportResult(err)
	}
	c.releaseConnToPool(ctx, c.connPool, cn, err)
}

// errWithServingEndpoint wraps a MOVED/ASK (or other) error with the logical
// endpoint of the connection that produced it. Cluster redirect resolution
// prefers this over Options.Addr after a maintenance-notification handoff
// (#4230 / #4233 review). Allocates only on the redirect error path.
type errWithServingEndpoint struct {
	endpoint string
	err      error
}

func (e *errWithServingEndpoint) Error() string { return e.err.Error() }

func (e *errWithServingEndpoint) Unwrap() error { return e.err }

// RedisError keeps typed redis.Error checks working if a caller still sees the
// wrapper (hooks / Process return value). Prefer storing the unwrapped error on
// cmd.Err(); see Client.Process and pipelineReadCmdsWithFlags.
func (e *errWithServingEndpoint) RedisError() {}

func unwrapServingEndpoint(err error) error {
	if e, ok := err.(*errWithServingEndpoint); ok {
		return e.err
	}
	return err
}

func wrapServingEndpoint(cn *pool.Conn, err error) error {
	if err == nil || cn == nil {
		return err
	}
	moved, ask, _ := isMovedError(err)
	if !moved && !ask {
		return err
	}
	ep := cn.ServingEndpoint()
	if ep == "" {
		return err
	}
	return &errWithServingEndpoint{endpoint: ep, err: err}
}

// releaseConnToPool returns a conn to p after a command or pipeline ran on
// it: bad conns are Removed, pending push notifications are drained (a
// mid-frame drain failure also Removes — the reply stream may be
// desynchronized), and a client-side-cache post-read probe is requested when
// tracking is on. Limiter accounting stays with the callers, whose shapes
// differ. Shared by releaseConn and withPipelineConn so the two cannot drift.
func (c *baseClient) releaseConnToPool(ctx context.Context, p pool.Pooler, cn *pool.Conn, err error) {
	// errConnUnusable is wrapped around errors that leave the connection's reply
	// stream desynchronized even though they are not transport (bad-conn) errors:
	// a push-notification drain that consumed part of a RESP3 frame, or a panic
	// while serializing a command's args mid-write. Such a conn MUST be removed —
	// reusing it would decode leftover bytes as the next caller's replies.
	connAddr := c.opt.Addr
	if ep := cn.ServingEndpoint(); ep != "" {
		connAddr = ep
	}
	if isBadConn(err, false, connAddr) || errors.Is(err, errConnUnusable) {
		p.Remove(ctx, cn, err)
		return
	}
	// process any pending push notifications before returning the connection to the pool
	if err := c.processPushNotifications(ctx, cn); err != nil {
		internal.Logger.Printf(ctx, "push: error processing pending notifications before releasing connection: %v", err)
		// Any drain error may leave the reply stream desynchronized: a mid-frame
		// read failure, or a custom PushNotificationProcessor that consumed part of a
		// frame and returned a non-transport error. Remove the conn rather than
		// relying on isBadConn to recognize the cause — reusing it would decode
		// leftover bytes as the next caller's reply. The built-in processor returns
		// nil on a nothing-consumed drain, so its normal path still Puts the conn.
		p.Remove(ctx, cn, err)
		return
	}
	if c.cscTrackingRequested() {
		// A TLS-like wrapper can retain decrypted bytes after the command
		// reply even when its raw socket is empty. Ask the background
		// drainer for one bounded post-read probe before relying on raw
		// socket peeks again.
		cn.MarkCscReadPending()
	}
	p.Put(ctx, cn)
}

func (c *baseClient) withConn(
	ctx context.Context, fn func(context.Context, *pool.Conn) error,
) error {
	cn, err := c.getConn(ctx)
	if err != nil {
		return err
	}

	var fnErr error
	defer func() {
		// A panic inside fn (e.g. a user BinaryMarshaler panicking while writeCmd
		// serializes args) can leave a partial write on the wire, desyncing the conn.
		// Mark it unusable so releaseConn REMOVES it instead of returning a poisoned
		// conn to the pool, then re-panic to preserve the caller's panic propagation.
		if r := recover(); r != nil {
			fnErr = fmt.Errorf("%w: panic: %v", errConnUnusable, r)
			c.releaseConn(ctx, cn, fnErr)
			panic(r)
		}
		c.releaseConn(ctx, cn, fnErr)
	}()

	fnErr = fn(ctx, cn)

	return fnErr
}

// withPipelineConn executes fn with a connection from the pipeline pool when
// one is configured (PipelineReadBufferSize/PipelineWriteBufferSize set),
// otherwise it falls back to the regular pool via withConn.
// withPipelineConn is withConn/releaseConn for the DEDICATED pipeline pool.
// Conn preparation and release go through the shared pool-parameterized
// helpers (initPooledConn, releaseConnToPool) — the paths used to mirror each
// other by hand and drifted three times (a Limiter-ordering divergence, a
// missed drain-error removal, a missed client-side-cache probe), so only the
// Limiter shape is allowed to live here.
func (c *baseClient) withPipelineConn(
	ctx context.Context, fn func(context.Context, *pool.Conn) error,
) (retErr error) {
	// Use pipeline pool if available, otherwise fall back to regular pool.
	// Read the ref once so every use below sees the same pool (it is set once at
	// construction and never mutated, so a plain read is safe).
	ref := c.loadPipelinePool()
	if ref == nil {
		return c.withConn(ctx, fn)
	}
	pipelinePool := ref.pool

	// Honor the Limiter on the dedicated pipeline-pool path too, mirroring
	// getConn/releaseConn: Allow() before acquiring and ReportResult() on every
	// exit (including the early init/re-acquire failures below). Without this,
	// enabling the pipeline pool would silently bypass throttling and failure
	// reporting for callers that set a Limiter.
	limiter := c.limiter()
	if limiter != nil {
		if err := limiter.Allow(); err != nil {
			return err
		}
	}

	// One deferred exit for both concerns, because their ORDER is part of the
	// contract: releaseConn reports the result BEFORE the connection becomes
	// available again, so a limiter or circuit breaker observes the failure
	// before it can admit the next operation. Two separate defers would run
	// LIFO and release first, letting another pipelined operation through
	// against a breaker that has not seen the failure yet (review finding by
	// codex on #3942). cn is nil on the acquire/init failure paths, which still
	// must report.
	var cn *pool.Conn
	// connPool records which pool cn was acquired from — the pipeline pool
	// normally, or the main pool on a spill — so the single deferred
	// ReportResult+release below runs against the right pool, in report-before-
	// release order, without re-entering the Limiter a second time.
	var connPool pool.Pooler = pipelinePool
	var fnErr error
	defer func() {
		// A panic inside fn (a user encoder panicking mid-write) can desync the conn;
		// mark it unusable so it is REMOVED, report the failure to the Limiter, and
		// re-panic after releasing so the caller's panic propagation is preserved.
		// Recover once, then re-panic at the very end (report-before-release order is
		// part of the contract — see below — so the release must run first).
		var pv any
		panicked := false
		if r := recover(); r != nil {
			panicked = true
			pv = r
			fnErr = fmt.Errorf("%w: panic: %v", errConnUnusable, r)
			retErr = fnErr
		}
		if limiter != nil {
			limiter.ReportResult(retErr)
		}
		if cn != nil {
			c.releaseConnToPool(ctx, connPool, cn, fnErr)
		}
		if panicked {
			panic(pv)
		}
	}()

	// Acquire+init from the pipeline pool; SPILL to the main pool when the
	// pipeline pool cannot serve this pipeline. Acquire the main pool DIRECTLY
	// (not via withConn, which would call Limiter.Allow()/ReportResult() a second
	// time on top of the outer pair above — the #3959 double-count, which could
	// also spuriously reject the spill); the outer Allow accounts this op and the
	// deferred ReportResult reports it once.
	//
	// Spill on EVERY acquisition failure EXCEPT a hard stop — a closed pool
	// (pool.ErrClosed) or a cancelled/expired acquire ctx (context.Canceled/
	// DeadlineExceeded) — because there the main pool would fail the same way. This
	// is a deny-list, not an allow-list: TryGet also DIALS when the pipeline pool
	// has no idle conn, and a transient dial failure there is neither ErrPoolTryFull
	// nor ErrPoolExhausted, so an allow-list would surface it and fail the pipeline
	// even though the main pool has an idle conn or could dial cleanly. Saturation
	// (ErrPoolTryFull/ErrPoolExhausted), a dial error, and a pipeline-conn init
	// failure all spill; the main pool may hand back an idle conn and avoid it.
	// Spilled pipelines run with the regular buffer sizes (a throughput detail).
	//
	// TryGet, not Get: on a saturated pipeline pool TryGet returns ErrPoolTryFull
	// AT ONCE (no PoolTimeout wait, and it is not counted as a pool timeout), so the
	// pipeline spills to the main pool immediately instead of stalling up to
	// DefaultPipelinePoolTimeout and recording a spurious Stats.Timeouts.
	spill := false
	cn, retErr = pipelinePool.TryGet(ctx)
	if retErr != nil {
		cn = nil
		if errors.Is(retErr, pool.ErrClosed) ||
			errors.Is(retErr, context.Canceled) ||
			errors.Is(retErr, context.DeadlineExceeded) {
			// Hard stop: the main pool cannot do better (closed pool, or the caller
			// cancelled/expired the acquire ctx). Surface it rather than spill.
			return retErr
		}
		spill = true
	} else if err := c.initPooledConn(ctx, pipelinePool, cn); err != nil {
		cn = nil // initPooledConn already removed it from the pipeline pool
		retErr = err
		spill = true
	}

	if spill {
		// The pipeline pool's failed dial sequence is not the result of this
		// acquisition: the main pool may only be temporarily full, so a
		// subsequent command retry must still be allowed. If the main pool
		// also exhausts a dial sequence it will set the marker again.
		if marker := pipelinePool.DialExhaustion(ctx); marker != nil {
			marker.Store(false)
		}
		cn, retErr = c.connPool.Get(ctx)
		if retErr != nil {
			cn = nil
			return retErr
		}
		connPool = c.connPool
		if err := c.initPooledConn(ctx, c.connPool, cn); err != nil {
			cn = nil // initPooledConn already removed it from the main pool
			retErr = err
			return retErr
		}
	}

	fnErr = fn(ctx, cn)
	retErr = fnErr
	return retErr
}

func (c *baseClient) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	return c.opt.Dialer(ctx, network, addr)
}

// cscTrackingRequested reports whether initConn must issue CLIENT TRACKING ON.
// cscActive is allocated only after attachment succeeds and is shared with
// derived clients: a conn initialized by Conn/Tx may later return to the
// parent's pool, but a configured cache whose attachment failed must not turn
// tracking on.
func (c *baseClient) cscTrackingRequested() bool {
	if c.opt.Protocol != 3 || c.cscActive == nil || !c.cscActive.Load() {
		return false
	}
	return c.opt.DB == 0
}

// autopipelineCSCActive reports whether client-side caching can currently serve
// this client. AutoPipeline checks it before command eligibility at dispatch
// time because CSC can disable itself after client construction.
func (c *baseClient) autopipelineCSCActive() bool {
	return c.csc != nil && c.cscActive != nil && c.cscActive.Load()
}

// autopipelineCSCEligible reports whether cmd must take AutoPipeliner's
// cache-honoring Process path under the client's current metadata view. The
// view is resolved per dispatch so live metadata upgrades and application
// overrides take effect without rebuilding the AutoPipeliner.
func (c *baseClient) autopipelineCSCEligible(cmd Cmder) bool {
	meta, ok := cscEligibleMeta(c.metadataView(), cmd)
	return ok && commandArgsRepeatable(cmd) && cscCanExtractRedisKeys(meta, cmd)
}

func (c *baseClient) process(ctx context.Context, cmd Cmder) error {
	return c.processStartingAt(ctx, cmd, 0, time.Time{})
}

// processStartingAt runs cmd like process() but starts the retry loop at
// startAttempt. The full-duplex divert (retryOnNormalConn) passes 1 for a command
// that already spent its initial attempt on the FD socket and came back with a
// retryable reply, so the retry budget (MaxRetries) is not exceeded by one; it
// passes 0 for a redirect, where the FD attempt did not execute the command.
//
// start is the operation start time for the OTel duration metric. The FD divert
// passes req.writtenAt so the reported duration spans the initial FD write to
// final completion, matching the inline FD path and the attempt count (which
// already includes the FD attempt). A zero start defaults to now, so the normal
// path measures from here.
func (c *baseClient) processStartingAt(ctx context.Context, cmd Cmder, startAttempt int, start time.Time) error {
	if !otel.Enabled(c.recorder()) {
		return c.processCommand(ctx, cmd, nil, startAttempt)
	}

	if start.IsZero() {
		start = time.Now()
	}
	var state processState
	err := c.processCommand(ctx, cmd, &state, startAttempt)
	c.recordCommand(ctx, time.Since(start), cmd, state.attempts, err, state.lastConn)
	return err
}

// recordCommand records a finished command to the client's recorder: its
// duration and, on success, its per-command samples (recordCommandSamples).
// It runs on the single-command path and the FD reader, and reads only the
// raw result (never Val()/Err(), which on the FD reader would await the batch
// this call completes).
func (c *baseClient) recordCommand(ctx context.Context, d time.Duration, cmd Cmder, attempts int, err error, cn *pool.Conn) {
	c.recorder().RecordOperationDuration(ctx, d, cmd, attempts, err, cn, c.opt.DB)
	if err != nil {
		return
	}
	c.recordCommandSamples(ctx, cmd, cn)
}

// recordCommandSamples records the "sent" Pub/Sub message of a successful
// PUBLISH/SPUBLISH and the stream lag of every message a successful XREADGROUP
// read. recordCommand calls it for single commands; the pipeline read loops
// call it for each command whose reply was read without error.
func (c *baseClient) recordCommandSamples(ctx context.Context, cmd Cmder, cn *pool.Conn) {
	switch cmd.Name() {
	case "publish", "spublish":
		if args := cmd.Args(); len(args) > 1 {
			if channel, ok := args[1].(string); ok {
				c.recorder().RecordPubSubMessage(ctx, cn, "sent", channel, cmd.Name() == "spublish")
			}
		}
	case "xreadgroup":
		if x, ok := cmd.(*XStreamSliceCmd); ok {
			c.recordStreamLag(ctx, cn, x)
		}
	}
}

// recordCommandSamplesGuarded is recordCommandSamples under a recover, for the
// pipeline read loops: a panicking recorder there would abort the loop with
// replies still unread and return a desynced connection to the pool. The FD
// reader guards its samples the same way (emitMetricsGuarded).
func (c *baseClient) recordCommandSamplesGuarded(ctx context.Context, cmd Cmder, cn *pool.Conn) {
	defer func() {
		if r := recover(); r != nil {
			internal.Logger.Printf(ctx, "redis: recovered metric recorder panic in pipeline read loop: %v", r)
		}
	}()
	c.recordCommandSamples(ctx, cmd, cn)
}

// recordStreamLag records, for each message read by an XREADGROUP, the time
// since the message was created (its ID's millisecond timestamp).
func (c *baseClient) recordStreamLag(ctx context.Context, cn *pool.Conn, cmd *XStreamSliceCmd) {
	args := cmd.Args() // xreadgroup group <group> <consumer> ...
	if len(args) < 4 {
		return
	}
	group, _ := args[2].(string)
	consumer, _ := args[3].(string)
	for _, stream := range cmd.val {
		for _, msg := range stream.Messages {
			ms, _, found := strings.Cut(msg.ID, "-")
			if !found {
				continue
			}
			if created, err := strconv.ParseInt(ms, 10, 64); err == nil {
				c.recorder().RecordStreamLag(ctx, time.Since(time.UnixMilli(created)), cn, stream.Stream, group, consumer)
			}
		}
	}
}

// recorder returns where the client's metrics go. A hand-built value (tests) leaves
// rec nil, which means the global recorder.
func (c *baseClient) recorder() otel.Recorder {
	if c.rec == nil {
		return otel.Scoped(nil)
	}
	return c.rec
}

type processState struct {
	attempts int
	lastConn *pool.Conn
}

func (c *baseClient) processCommand(ctx context.Context, cmd Cmder, state *processState, startAttempt int) error {
	cs := cmd.cacheState()
	policyCall := c.cscPolicy.configured() || cs.mark() != cacheUnmarked || cs.has(cscForceBypass) || cs.has(cscAsk)
	if policyCall {
		cscEnter(cmd)
		defer cscLeave(cmd)
	}
	// Reject commands that would make one pooled connection diverge from CSC's
	// tracking or database assumptions. Pipelines mirror this guard below.
	if err := c.cscCommandError(cmd); err != nil {
		return err
	}
	if !policyCall {
		if c.csc != nil {
			view := c.cscInvocationView(cmd)
			if meta, ok := cscEligibleMeta(view, cmd); ok {
				return c.processCached(ctx, cmd, state, view, meta, startAttempt)
			}
		}
		return c.processWithRetry(ctx, cmd, nil, state, startAttempt)
	}
	if c.csc != nil || cmd.cacheState().mark() != cacheUnmarked || c.cscPolicy.configured() {
		view := c.cscInvocationView(cmd)
		meta, use := c.cscDecide(ctx, cmd, view)
		if use {
			return c.processCached(ctx, cmd, state, view, meta, startAttempt)
		}
	}
	return c.processWithRetry(ctx, cmd, nil, state, startAttempt)
}

// processWithRetry runs cmd through the retry loop. capture (optional) is
// filled by the successful attempt's reply read for the CSC fetch path (see
// cscFetchCapture).
func (c *baseClient) processWithRetry(
	ctx context.Context, cmd Cmder, capture *cscFetchCapture, state *processState, startAttempt int,
) error {
	var lastConn *pool.Conn
	if state != nil {
		// Keep a connection an earlier stage already attributed to this command
		// (processCached: the coalesced fetch's session conn) when this loop never
		// reaches one, e.g. the re-run fails to acquire a pooled connection. The
		// duration metric then names the last server that saw the command rather
		// than none.
		lastConn = state.lastConn
	}

	var lastErr error
	maxRetries := c.opt.MaxRetries
	himportRetried := false
	// startAttempt > 0 accounts for attempts already spent elsewhere: the
	// full-duplex divert passes 1 when a command already used its initial attempt
	// on the FD socket (a retryable reply), so the total (FD attempt + this loop)
	// does not exceed MaxRetries+1. Clamp so a caller can never disable execution:
	// startAttempt <= maxRetries guarantees the loop runs at least once.
	if startAttempt < 0 {
		startAttempt = 0
	}
	if startAttempt > maxRetries {
		startAttempt = maxRetries
	}
	// Seed with startAttempt so state.attempts (reported to the OTel duration
	// callback) counts attempts already spent before this loop — e.g. the FD socket
	// attempt on a diverted retryable command — not just this loop's iterations.
	totalAttempts := startAttempt
	// Pre-write retirements re-run without spending the retry budget, capped by
	// the pool size so a pool full of retired conns still terminates.
	preWriteRetirements := 0
	for attempt := startAttempt; attempt <= maxRetries; attempt++ {
		totalAttempts++

		retry, forced, cn, err := c._process(ctx, cmd, attempt, capture)
		if cn != nil {
			lastConn = cn
		}
		if state != nil {
			state.attempts = totalAttempts
			state.lastConn = lastConn
		}
		// A conn retired by the pre-command drain never saw the command. It is
		// typically an idle conn the server closed (over TLS, the drain reads the
		// close_notify then EOF), which the pool health check cannot detect, so
		// several can be handed out in a row. Re-run on the next conn at attempt
		// 0: no backoff and no retry budget spent.
		for err != nil && forced && isClosedConnErr(err) && preWriteRetirements < c.opt.PoolSize {
			preWriteRetirements++
			retry, forced, cn, err = c._process(ctx, cmd, 0, capture)
			if cn != nil {
				lastConn = cn
			}
			if state != nil {
				state.lastConn = lastConn
			}
		}
		// A "no such fieldset" reply for a registered fieldset means the
		// connection lost its server session state (e.g. RESET, concurrent
		// discard). The stale prepared flag was invalidated inside _process
		// while the connection was still held; grant a single extra attempt
		// so the retry re-prepares lazily on whichever connection it lands.
		if err != nil && !retry && !himportRetried && !cmd.NoRetry() &&
			c.himportShouldRetrySet(cmd, err) {
			himportRetried = true
			if attempt == maxRetries {
				maxRetries++
			}
			lastErr = err
			continue
		}
		// Don't retry if the command explicitly disables retries (e.g. RawWriteToCmd,
		// which writes directly to an io.Writer and cannot undo partial writes) — UNLESS
		// the failure was a pre-write desync (forced): the command never reached the
		// wire, so replaying it is its first execution, which NoRetry does not forbid
		// (NoRetry guards against a SECOND execution of a possibly-written command).
		if err == nil || !retry || (cmd.NoRetry() && !forced) {
			if err != nil {
				c.recordCommandError(ctx, err, lastConn, totalAttempts-1)
			}
			return err
		}

		lastErr = err
	}

	// Record error metric for exhausted retries
	c.recordCommandError(ctx, lastErr, lastConn, totalAttempts-1)

	return lastErr
}

// isClosedConnErr reports whether err means the connection was closed by the
// peer or the network. Timeouts are excluded: a stalled mid-frame drain is a
// desync, not a dead conn.
func isClosedConnErr(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && !opErr.Timeout()
}

// recordCommandError emits the native error metric for a command's terminal
// failure: err classified, the connection that served the last attempt (nil when
// none did), and the retries spent (attempts beyond the first). Every exit that
// ends a command with an error shares it: the two in processWithRetry and the
// exhausted-budget return in processCached.
func (c *baseClient) recordCommandError(ctx context.Context, err error, cn *pool.Conn, retries int) {
	if !otel.Enabled(c.recorder()) {
		return
	}
	errorType, statusCode, isInternal := classifyCommandError(err)
	c.recorder().RecordError(ctx, errorType, cn, statusCode, isInternal, retries)
}

// classifyCommandError classifies an error for metrics reporting.
// Returns: errorType, statusCode, isInternal
// - errorType: A string describing the error type (e.g., "TIMEOUT", "NETWORK", "ERR")
// - statusCode: The Redis error prefix or error category
// - isInternal: true for network/timeout errors, false for Redis server errors
func classifyCommandError(err error) (errorType, statusCode string, isInternal bool) {
	if err == nil {
		return "", "", false
	}

	// A Nil reply is a successful command with no value. It gets its own type so
	// the recorder can tell it apart from a real failure.
	if errors.Is(err, Nil) {
		return ErrorTypeNil, ErrorTypeNil, false
	}

	errStr := err.Error()

	// Check for timeout errors
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return "TIMEOUT", "TIMEOUT", true
	}

	// Check for network errors
	if _, ok := err.(net.Error); ok {
		return "NETWORK", "NETWORK", true
	}

	// Check for context errors
	if errors.Is(err, context.Canceled) {
		return "CONTEXT_CANCELED", "CONTEXT_CANCELED", true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "CONTEXT_TIMEOUT", "CONTEXT_TIMEOUT", true
	}

	// Check for Redis errors
	// Examples: "ERR ...", "WRONGTYPE ...", "CLUSTERDOWN ..."
	if len(errStr) > 0 {
		// Find the first space to extract the prefix
		spaceIdx := 0
		for i, c := range errStr {
			if c == ' ' {
				spaceIdx = i
				break
			}
		}
		if spaceIdx == 0 {
			spaceIdx = len(errStr)
		}
		prefix := errStr[:spaceIdx]
		isUppercase := true
		for _, c := range prefix {
			if c < 'A' || c > 'Z' {
				isUppercase = false
				break
			}
		}
		if isUppercase && len(prefix) > 0 {
			return prefix, prefix, false
		}
	}

	return "UNKNOWN", "UNKNOWN", true
}

// captureFixedDialExhaustion lets an outer router observe the same attempt
// outcome as the client without altering the returned error.
func (c *baseClient) captureFixedDialExhaustion(ctx context.Context) (context.Context, *atomic.Bool) {
	if c.fixedDialEndpoint {
		cp, ok := c.connPool.(*pool.ConnPool)
		if !ok {
			cp = c.dialExhaustionPool
		}
		if cp != nil {
			if existing := cp.DialExhaustion(ctx); existing != nil {
				return ctx, existing
			}
			marker := new(atomic.Bool)
			return cp.WithDialExhaustion(ctx, marker), marker
		}
	}
	return ctx, nil
}

// Pipelines may use a dedicated connection pool. Observe exhaustion from
// either pool with one marker, including an ASK follow-up's pipeline.
func (c *baseClient) captureFixedPipelineDialExhaustion(ctx context.Context) (context.Context, *atomic.Bool) {
	ctx, marker := c.captureFixedDialExhaustion(ctx)
	if !c.fixedDialEndpoint {
		return ctx, nil
	}
	if ref := c.loadPipelinePool(); ref != nil {
		if marker == nil {
			marker = new(atomic.Bool)
		}
		ctx = ref.pool.WithDialExhaustion(ctx, marker)
	}
	return ctx, marker
}

// _process runs one attempt. It returns retry (the error is retryable), forced (the
// error is a PRE-WRITE desync: the conn was closed before the command reached the
// wire, so replay is its FIRST execution and safe even for a NoRetry command), the
// conn used, and the error.
func (c *baseClient) _process(ctx context.Context, cmd Cmder, attempt int, capture *cscFetchCapture) (retry, forced bool, cn *pool.Conn, err error) {
	if attempt > 0 {
		if err := internal.Sleep(ctx, c.retryBackoff(attempt)); err != nil {
			return false, false, nil, err
		}
	}

	var usedConn *pool.Conn
	var retryTimeout atomic.Uint32
	// forceRetry marks the returned error as retryable regardless of its type, AND
	// (surfaced as the _process "forced" return) lets the retry bypass a NoRetry
	// command's gate. Set only when we have already CLOSED the connection before the
	// command reached the wire (a pre-command push-drain desync), so re-running on a
	// fresh conn is its FIRST execution — safe even for an error shouldRetry would
	// reject (a custom push-processor sentinel) and even for a NoRetry command (whose
	// gate guards a SECOND execution, not a first).
	var forceRetry atomic.Bool
	ctx, dialExhausted := c.captureFixedDialExhaustion(ctx)
	if err := c.withConn(ctx, func(ctx context.Context, cn *pool.Conn) error {
		usedConn = cn
		// Process any pending push notifications before executing the command. A
		// non-nil error means the drain may have stopped mid-frame (a fragmented
		// push straddling its hard read cap), leaving the reader desynced. Do NOT
		// ignore it: reading this command's reply on the conn would then return
		// shifted bytes — a wrong result, or a wrong-key cache on the CSC capture
		// path. Close the conn so releaseConn removes it, mark the error retryable,
		// and let processWithRetry re-run on a fresh conn. Benign cases return nil
		// (peekAndProcessPushNotifications gates on MaybeHasData; the built-in
		// processor propagates only genuine mid-frame errors and a custom processor
		// is peeked first), so this fires only on a real desync.
		if err := c.processPushNotifications(ctx, cn); err != nil {
			internal.Logger.Printf(ctx, "push: pre-command drain failed, retiring conn: %v", err)
			_ = cn.Close()
			// Force a retry: the conn is closed so re-running on a fresh conn is safe,
			// and the drain error may be a custom-processor sentinel that shouldRetry
			// would reject (retryTimeout only affects timeout errors). The command was
			// not written yet, so replay is clean.
			forceRetry.Store(true)
			return err
		}

		if capture != nil {
			capture.noStore = false
		}
		// Borrowed Conn/Tx wrappers do not serve cache hits, but can still hold
		// a tracked connection and need NO for an ordinary uncached OPTOUT read.
		// Per-socket init generation (not live client-level CSC) decides whether
		// the socket still carries TRACKING after disableCSCServing (#4236 item 1).
		flag := byte(0)
		if c.cscPolicy.mode != TrackingModePlain {
			tracked := c.cscConnInitGen(cn.GetID()) != 0
			if capture != nil && !tracked {
				capture.noStore = true
			}
			if tracked {
				flag = c.cscFlag(cmd, capture != nil)
			}
		}

		// HIMPORT bookkeeping: pending discards for this session and the
		// PREPARE for an HIMPORT SET's registered fieldset are written in
		// the same round trip, right before the command.
		var injected []Cmder
		if _, ok := cmd.(himportCmder); ok {
			injected = c.himportInjectedCmds(ctx, cn, []Cmder{cmd})
		}

		if err := cn.WithWriter(c.context(ctx), c.opt.WriteTimeout, func(wr *proto.Writer) error {
			for _, ic := range injected {
				if err := writeCmd(wr, ic); err != nil {
					return err
				}
			}
			if err := writeCSCPrefix(wr, flag); err != nil {
				return err
			}
			return writeCmd(wr, cmd)
		}); err != nil {
			// Even an encoder error typed as a Redis error must retire a partial
			// pair: its prefix may already be buffered or on the wire.
			if flag != 0 {
				_ = cn.Close()
			}
			retryTimeout.Store(1)
			return err
		}
		readReplyFunc := cmd.readReply
		// When the caller requested raw-reply capture (client-side cache),
		// read the reply as raw RESP bytes and re-parse them through the
		// command's normal reply handler. This reuses proto.Reader rather
		// than duplicating parsing logic in a bespoke cache serializer.
		if capture != nil {
			origRead := readReplyFunc
			readReplyFunc = func(rd *proto.Reader) error {
				raw, err := rd.ReadRawReply()
				if err != nil {
					return err
				}
				capture.raw = raw
				return origRead(proto.NewReaderSize(bytes.NewReader(raw), len(raw)+1))
			}
		}
		readErr := cn.WithReader(c.context(ctx), c.cmdTimeout(cmd), func(rd *proto.Reader) error {
			// To be sure there are no buffered push notifications, we process them before reading the reply
			if err := c.processPendingPushNotificationWithReader(ctx, cn, rd); err != nil {
				if flag != 0 {
					_ = cn.Close()
					return err
				}
				internal.Logger.Printf(ctx, "push: error processing pending notifications before reading reply: %v", err)
			}
			if len(injected) > 0 {
				if err := c.himportReadInjectedReplies(ctx, cn, rd, injected); err != nil {
					return err
				}
				// A push notification can arrive between the injected
				// replies and the command reply; drain again so the
				// reply read below does not consume it as the command's.
				if err := c.processPendingPushNotificationWithReader(ctx, cn, rd); err != nil {
					internal.Logger.Printf(ctx, "push: error processing pending notifications before reading reply: %v", err)
				}
			}
			if flag != 0 {
				ok, err := c.readCSCAck(ctx, cn, rd)
				if err != nil {
					_ = cn.Close()
					return err
				}
				if capture != nil && !ok {
					capture.noStore = true
				}
				if err := c.drainPushFrames(ctx, cn, rd, true); err != nil {
					_ = cn.Close()
					return err
				}
			}
			err := readReplyFunc(rd)
			// Assert the command type before touching the error: the
			// errors.As chain inside himportNoSuchFieldset allocates, and
			// this is the per-command hot path.
			if set, ok := cmd.(*HImportSetCmd); ok && himportNoSuchFieldset(err) {
				// A failed injected PREPARE is the root cause of the
				// command's "no such fieldset" reply (drained above).
				for _, ic := range injected {
					if prep, ok := ic.(*HImportPrepareCmd); ok &&
						prep.fieldsetName == set.fieldsetName && prep.Err() != nil {
						err = prep.Err()
						break
					}
				}
				// The session lost a registered fieldset the flags claim is
				// prepared — and the same event (failover, cross-region
				// switch, reset storm) may have wiped other sessions whose
				// flags also still look current. Bump the fieldset version
				// so every connection re-prepares before its next use,
				// wherever the retry granted by process() lands.
				if himportNoSuchFieldset(err) {
					if fs, registered := c.himport.lookup(set.fieldsetName); registered {
						c.himport.refreshVersion(set.fieldsetName, fs.version)
					}
				}
			}
			return err
		})
		// redis.Nil is a complete, valid negative reply. For a CSC fetch, retain
		// its connection attribution before returning Nil to the caller so the
		// raw reply can be cached and invalidated like any other read result.
		if readErr != nil && (capture == nil || readErr != Nil) {
			if cmd.readTimeout() == nil {
				retryTimeout.Store(1)
			} else {
				retryTimeout.Store(0)
			}
			// Carry the held connection's logical endpoint with hostless redirects
			// so ClusterClient can resolve them after release (#4233 review).
			return wrapServingEndpoint(cn, readErr)
		}
		if capture != nil {
			// Attribute while the conn is still held: once it is released, a
			// queued handoff may swap the socket and bump the generation, and
			// this capture is what fulfillCached compares against.
			capture.connID = cn.GetID()
			capture.initGen = c.cscConnInitGen(capture.connID)
		}

		if hc, ok := cmd.(himportCmder); ok {
			c.himportAfterCmd(cn, hc)
		}
		return readErr
	}); err != nil {
		retry := forceRetry.Load() || ((dialExhausted == nil || !dialExhausted.Load()) && shouldRetry(err, retryTimeout.Load() == 1))
		return retry, forceRetry.Load(), usedConn, err
	}

	return false, false, usedConn, nil
}

func (c *baseClient) retryBackoff(attempt int) time.Duration {
	return internal.RetryBackoff(attempt, c.opt.MinRetryBackoff, c.opt.MaxRetryBackoff)
}

func (c *baseClient) cmdTimeout(cmd Cmder) time.Duration {
	if timeout := cmd.readTimeout(); timeout != nil {
		t := *timeout
		if t == 0 {
			return 0
		}
		return t + 10*time.Second
	}
	return c.opt.ReadTimeout
}

// context returns the context for the current connection.
// If the context timeout is enabled, it returns the original context.
// Otherwise, it returns a new background context.
func (c *baseClient) context(ctx context.Context) context.Context {
	if c.opt.ContextTimeoutEnabled {
		return ctx
	}
	return context.Background()
}

// createInitConnFunc creates a connection initialization function that can be used for reconnections.
// Only the maintenance handoff runs it, through SetNetConnAndInitConn: it dials outside
// getConnLimited, so the handshake it runs is admitted once and reported once against
// Options.Limiter. Streaming re-auth does not come through here: it sends AUTH on the
// pooled connection through the Conn built by reAuthConnection, which getConnLimited
// admits per command.
func (c *baseClient) createInitConnFunc() func(context.Context, *pool.Conn) error {
	return func(ctx context.Context, cn *pool.Conn) error {
		closeConn := func(error) { cn.GetStateMachine().Transition(pool.StateClosed) }
		return admitted(c.limiter(), closeConn, func() error {
			return c.initConn(ctx, cn)
		})
	}
}

// enableMaintNotificationsUpgrades initializes the maintnotifications upgrade manager and pool hook.
// This function is called during client initialization.
// will register push notification handlers for all maintenance upgrade events.
// will start background workers for handoff processing in the pool hook.
func (c *baseClient) enableMaintNotificationsUpgrades() error {
	// Create client adapter
	clientAdapterInstance := newClientAdapter(c)

	// Create maintnotifications manager directly
	manager, err := maintnotifications.NewManager(clientAdapterInstance, c.connPool, c.opt.MaintNotificationsConfig)
	if err != nil {
		return err
	}
	// Set the manager reference and initialize pool hook
	c.maintNotificationsManagerLock.Lock()
	c.maintNotificationsManager = manager
	c.maintNotificationsManagerLock.Unlock()

	// Initialize pool hook (safe to call without lock since manager is now set)
	manager.InitPoolHook(c.dialHook)
	// If a dedicated pipeline connection pool is in use, attach an independent
	// maintnotifications hook to it as well. Otherwise autopipelined/pipelined
	// commands run on pipeline-pool connections that never receive MOVING/
	// MIGRATING handoff handling.
	if pp := c.getPipelinePool(); pp != nil {
		manager.InitPoolHookForPool(pp, c.dialHook)
	}
	return nil
}

func (c *baseClient) disableMaintNotificationsUpgrades() error {
	c.maintNotificationsManagerLock.Lock()
	defer c.maintNotificationsManagerLock.Unlock()

	// Close the maintnotifications manager
	if c.maintNotificationsManager != nil {
		// Closing the manager will also shutdown the pool hook
		// and remove it from the pool
		if err := c.maintNotificationsManager.Close(); err != nil {
			return err
		}
		c.maintNotificationsManager = nil
	}
	return nil
}

// Close closes the client, releasing any open resources.
//
// It is rare to Close a Client, as the Client is meant to be
// long-lived and shared between many goroutines.
func (c *baseClient) Close() error {
	// The pools this baseClient owns are shared with every WithTimeout/
	// WithReadTimeout clone. Once ANY sharer closes them, no wrapper may
	// build a fresh autopipeliner against them — its flushers would run
	// against closed pools forever. The atomic is checked by the
	// AutoPipeline getters of every wrapper sharing this base.
	if c.apClosed != nil {
		c.apClosed.Store(true)
	}
	if h := c.cscDrainHandle; h != nil {
		h.closeOnce.Do(func() {
			h.closeErr = c.closeResources()
		})
		return h.closeErr
	}
	return c.closeResources()
}

func (c *baseClient) closeResources() error {
	var firstErr error

	// Stop metadata and CSC workers before closing their pools.
	c.stopBackgroundDrainer()
	c.cmdMeta.stopAndJoin()

	// Close maintnotifications manager first
	if err := c.disableMaintNotificationsUpgrades(); err != nil {
		firstErr = err
	}

	if err := c.onClose.run(); err != nil && firstErr == nil {
		firstErr = err
	}

	if c.connPool != nil {
		if err := c.connPool.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if pp := c.getPipelinePool(); pp != nil {
		if err := pp.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	var pubSubPool otel.PubSubPooler
	if c.pubSubPool != nil {
		if err := c.pubSubPool.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		pubSubPool = c.pubSubPool
	}
	// After closing, so a recorder installed meanwhile is not handed a closing pool.
	otel.UnregisterPools(c.recorder(), c.connPool, pubSubPool, c.getPipelinePool())
	return firstErr
}

func (c *baseClient) getAddr() string {
	return c.opt.Addr
}

func (c *baseClient) processPipeline(ctx context.Context, cmds []Cmder) error {
	if err := c.generalProcessPipeline(ctx, cmds, c.pipelineProcessCmds, "PIPELINE", c.opt.MaxRetries); err != nil {
		return err
	}
	return cmdsFirstErr(cmds)
}

// processPipelineRetries runs a pipeline with an explicit retry bound instead of
// the client's configured MaxRetries. The autopipeliner's shutdown flush passes
// maxRetries==0 to give an already-attempted carried command exactly one final
// execution, so replaying it on Close cannot exceed its per-command retry budget.
func (c *baseClient) processPipelineRetries(ctx context.Context, cmds []Cmder, maxRetries int) error {
	if err := c.generalProcessPipeline(ctx, cmds, c.pipelineProcessCmds, "PIPELINE", maxRetries); err != nil {
		return err
	}
	return cmdsFirstErr(cmds)
}

func (c *baseClient) processTxPipeline(ctx context.Context, cmds []Cmder) error {
	if err := c.generalProcessPipeline(ctx, cmds, c.txPipelineProcessCmds, "MULTI", c.opt.MaxRetries); err != nil {
		return err
	}
	return cmdsFirstErr(cmds)
}

type pipelineProcessor func(context.Context, *pool.Conn, []Cmder) (bool, error)

// pipelineErrShouldStamp reports whether a pipeline-level error must be stamped
// onto every command (setCmdsErr). A per-command Redis reply error (LOADING,
// WRONGTYPE, ...) is left alone so each command keeps its own reply - EXCEPT when
// the error is errConnUnusable, which marks a desynchronized reply stream and may
// WRAP a redis.Error (a custom PushNotificationProcessor that returns a redis.Error
// on a failed push drain). isRedisError unwraps to that inner redis.Error and would
// otherwise treat the whole batch as a normal reply, leaving every command with
// Err()==nil while Exec returns an error - so the errConnUnusable marker takes
// precedence and the transport failure is stamped onto every command.
func pipelineErrShouldStamp(err error) bool {
	return errors.Is(err, errConnUnusable) || !isRedisError(err)
}

func (c *baseClient) generalProcessPipeline(
	ctx context.Context, cmds []Cmder, p pipelineProcessor, operationName string, maxRetries int,
) error {
	return c.generalProcessPipelineFrom(ctx, cmds, p, operationName, maxRetries, time.Time{}, 0)
}

// processPipelineRetriesAfter is processPipelineRetries continuing an operation
// that already ran: start and priorAttempts carry over into the one pipeline
// metric, so a full-duplex attempt re-run on the pooled path is measured as a
// single operation, the way an ordinary pipeline's own retries are.
func (c *baseClient) processPipelineRetriesAfter(ctx context.Context, cmds []Cmder, maxRetries int, start time.Time, priorAttempts int) error {
	if err := c.generalProcessPipelineFrom(ctx, cmds, c.pipelineProcessCmds, "PIPELINE", maxRetries, start, priorAttempts); err != nil {
		return err
	}
	return cmdsFirstErr(cmds)
}

// generalProcessPipelineFrom is generalProcessPipeline with its measurement
// started at start (zero: now) and priorAttempts already spent.
func (c *baseClient) generalProcessPipelineFrom(
	ctx context.Context, cmds []Cmder, p pipelineProcessor, operationName string, maxRetries int,
	start time.Time, priorAttempts int,
) error {
	// Pipeline commands never pass through process, so apply the same CSC state
	// guard here. initConn's internal client is exempt.
	for _, cmd := range cmds {
		if err := c.cscCommandError(cmd); err != nil {
			setCmdsErr(cmds, err)
			return err
		}
	}
	// Only call time.Now() if a recorder is enabled to avoid overhead
	var operationStart time.Time
	recordDuration := otel.Enabled(c.recorder())
	if recordDuration {
		operationStart = start
		if operationStart.IsZero() {
			if priorAttempts > 0 {
				// A continued operation (a full-duplex attempt) that began
				// with no recorder: its start is unknown, so record
				// no duration, as the FD path does.
				recordDuration = false
			} else {
				operationStart = time.Now()
			}
		}
	}
	var lastConn *pool.Conn
	totalAttempts := priorAttempts

	var lastErr error
	// connAcquired reports whether the latest attempt obtained a connection:
	// the withPipelineConn callback only runs after getConn (including
	// initConn) succeeds. When setup fails, no command was sent and no
	// reply was read, so the failure must be stamped onto every command
	// even when it is a Redis error reply (e.g. WRONGPASS from AUTH or a
	// SELECT failure), which pipelineErrShouldStamp would otherwise skip
	// on the assumption that per-command replies were read.
	var connAcquired bool
	for attempt := 0; attempt <= maxRetries; attempt++ {
		totalAttempts++
		connAcquired = false
		if attempt > 0 {
			// Continue the backoff of an operation that already ran
			// priorAttempts times (a full-duplex attempt), as its own retries
			// would: 0 for an ordinary pipeline.
			if err := internal.Sleep(ctx, c.retryBackoff(priorAttempts+attempt)); err != nil {
				setCmdsErr(cmds, err)
				if recordDuration {
					c.recorder().RecordPipelineOperationDuration(ctx, time.Since(operationStart), operationName, len(cmds), totalAttempts, err, lastConn, c.opt.DB)
				}
				return err
			}
		}

		// Enable retries by default to retry dial errors returned by withConn.
		canRetry := true
		// Route pipelines through the dedicated pipeline pool when configured;
		// withPipelineConn falls back to the regular pool when it is not.
		attemptCtx, dialExhausted := c.captureFixedPipelineDialExhaustion(ctx)
		lastErr = c.withPipelineConn(attemptCtx, func(ctx context.Context, cn *pool.Conn) error {
			connAcquired = true
			lastConn = cn
			// Drain pending push notifications before executing the pipeline. A drain
			// error can mean a custom processor consumed part of a RESP3 push frame,
			// leaving the reply stream desynchronized — writing the batch now would
			// misread every reply (silent cross-command result shift). Fail the batch
			// with the error instead of logging on: errConnUnusable makes
			// releaseConnToPool remove the desynced conn, and shouldRetry does not
			// treat it as retryable, so the caller sees an error rather than shifted
			// results. (Failing loud is deliberately preferred over teaching the shared
			// retry classifier a new sentinel.)
			if err := c.processPushNotifications(ctx, cn); err != nil {
				return fmt.Errorf("%w: pipeline push drain: %w", errConnUnusable, err)
			}
			var err error
			canRetry, err = p(ctx, cn, cmds)
			return err
		})
		// A successful acquisition can still yield a retryable Redis reply.
		// Earlier failed spill attempts must not suppress that reply retry.
		if connAcquired && dialExhausted != nil {
			dialExhausted.Store(false)
		}
		// Don't retry if any command in the pipeline explicitly disables retries
		// (e.g., RawWriteToCmd which writes directly to an io.Writer and cannot
		// undo partial writes on retry)
		if lastErr == nil || !canRetry || (dialExhausted != nil && dialExhausted.Load()) || !shouldRetry(lastErr, true) || cmdsContainNoRetry(cmds) {
			// The error should be set here only when failing to obtain the conn.
			// A setup failure (!connAcquired) stamps even Redis errors: no
			// command was sent, so no per-command reply exists to preserve.
			if !connAcquired || pipelineErrShouldStamp(lastErr) {
				setCmdsErr(cmds, lastErr)
			}
			if recordDuration {
				c.recorder().RecordPipelineOperationDuration(ctx, time.Since(operationStart), operationName, len(cmds), totalAttempts, lastErr, lastConn, c.opt.DB)
			}

			if lastErr != nil {
				c.recordCommandError(ctx, lastErr, lastConn, totalAttempts-1)
			}
			return lastErr
		}
	}

	// Retries exhausted on a retryable error: the loop fell through without the
	// early-exit branch running, so the commands were never populated with the
	// failure. Mirror that branch here so callers that observe results only
	// per-command — notably AutoPipeline, which discards this function's returned
	// error — see the error instead of a nil error and a zero value. Guard on
	// !isRedisError so a per-command redis error (e.g. LOADING) keeps its own
	// reply rather than being overwritten, except when connection setup
	// failed and no command was sent at all.
	if !connAcquired || pipelineErrShouldStamp(lastErr) {
		setCmdsErr(cmds, lastErr)
	}

	if recordDuration {
		c.recorder().RecordPipelineOperationDuration(ctx, time.Since(operationStart), operationName, len(cmds), totalAttempts, lastErr, lastConn, c.opt.DB)
	}

	c.recordCommandError(ctx, lastErr, lastConn, totalAttempts-1)

	return lastErr
}

// pipelineExecutedKey carries an *executedCmds that execution paths populate
// the moment a batch's commands may have reached the server. MultiDBClient uses
// it to distinguish "a hook aborted before execution" from "the batch executed
// and a hook then replaced the error": command state alone cannot tell those
// apart, and confusing them either fabricates phantom outcomes or replays
// writes that already applied.
type pipelineExecutedKey struct{}

type pipelineProgress uint8

const (
	pipelinePending pipelineProgress = iota
	pipelineAttempted
	pipelineAnsweredLocally
)

// executedCmds tracks per-command progress across the node sub-batches of a
// cluster pipeline. A local answer can survive a retry without counting as
// a successful server response.
type executedCmds struct {
	mu sync.Mutex
	// Keyed by the command's embedded baseCmd, not by the Cmder interface
	// value: a value-type decorator that embeds a command plus a slice or map
	// field is not comparable and cannot be a map key, and skipping such
	// commands left a batch of them looking never-executed after a transport
	// error, so a retry could replay writes that had applied. Every command
	// embeds a baseCmd, and a decorator promotes base() from the command it
	// embeds, so this key identifies the command that executed.
	progress  map[*baseCmd]pipelineProgress
	attempted bool
}

func newExecutedCmds(size int) *executedCmds {
	return &executedCmds{progress: make(map[*baseCmd]pipelineProgress, size)}
}

func (e *executedCmds) mark(cmds []Cmder) {
	e.markProgress(cmds, pipelineAttempted)
}

func (e *executedCmds) markProgress(cmds []Cmder, progress pipelineProgress) {
	e.mu.Lock()
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		e.progress[cmd.base()] = progress
		e.attempted = e.attempted || progress == pipelineAttempted
	}
	e.mu.Unlock()
}

func (e *executedCmds) has(cmd Cmder) bool {
	return e.state(cmd) == pipelineAttempted
}

func (e *executedCmds) state(cmd Cmder) pipelineProgress {
	if cmd == nil {
		return pipelinePending
	}
	e.mu.Lock()
	progress := e.progress[cmd.base()]
	e.mu.Unlock()
	return progress
}

func (e *executedCmds) any() bool {
	e.mu.Lock()
	attempted := e.attempted
	e.mu.Unlock()
	return attempted
}

// markPipelineExecuted records that cmds reached (or attempted) the server for
// the batch tracked in ctx, if any. Safe to call from multiple cluster-node
// goroutines concurrently.
func markPipelineExecuted(ctx context.Context, cmds []Cmder) {
	if ec, ok := ctx.Value(pipelineExecutedKey{}).(*executedCmds); ok {
		ec.mark(cmds)
	}
}

func (c *baseClient) pipelineProcessCmds(
	ctx context.Context, cn *pool.Conn, cmds []Cmder,
) (bool, error) {
	// Drain pending push notifications before writing the pipeline. A drain error
	// may mean the reply stream is desynchronized (a processor consumed part of a
	// RESP3 frame), so writing now would misread replies. Fail the batch and remove
	// the conn (errConnUnusable) rather than logging on. (generalProcessPipeline
	// drains once more before calling this, so this is the belt-and-suspenders gate
	// for a direct/again-buffered push.)
	if err := c.processPushNotifications(ctx, cn); err != nil {
		err = fmt.Errorf("%w: pipeline push drain: %w", errConnUnusable, err)
		setCmdsErr(cmds, err)
		return false, err
	}
	markPipelineExecuted(ctx, cmds)

	// HIMPORT bookkeeping: pending discards for this session and PREPAREs
	// for registered fieldsets the batch references get written ahead of
	// the batch.
	injected := c.himportInjectedCmds(ctx, cn, cmds)
	flags := c.cscPipelineFlags(cn, cmds)

	if err := cn.WithWriter(c.context(ctx), c.opt.WriteTimeout, func(wr *proto.Writer) error {
		for _, ic := range injected {
			if err := writeCmd(wr, ic); err != nil {
				return err
			}
		}
		return writeCSCPipelineCmds(wr, cmds, flags)
	}); err != nil {
		if flags != nil {
			_ = cn.Close()
		}
		setCmdsErr(cmds, err)
		return true, err
	}

	var readErr error
	if err := cn.WithReader(c.context(ctx), c.opt.ReadTimeout, func(rd *proto.Reader) error {
		if err := c.himportReadInjectedReplies(ctx, cn, rd, injected); err != nil {
			// Transport error with every batch reply unreadXX: stamp the
			// batch like a write failure. The outer retry loop stamps only
			// on its exit branch, not when attempts run out, so without
			// this a batch that keeps dying here would surface an Exec
			// error while every command still reports Err() == nil.
			setCmdsErr(cmds, err)
			return err
		}
		// read all replies
		readErr = c.pipelineReadCmdsWithFlags(ctx, cn, rd, cmds, flags)
		if readErr != nil && !isRedisError(readErr) {
			return readErr
		}
		c.himportAfterBatch(cn, injected, cmds)
		return nil
	}); err != nil {
		return true, err
	}

	// Registered fieldsets whose SETs came back "no such fieldset" (the
	// session was lost between prepare and use) are re-prepared and those
	// SETs re-issued once on the same connection; the error must not
	// surface for managed fieldsets.
	//
	// A transport failure here must neither retry nor fail the batch: the
	// first round trip was fully consumed and its results delivered, so
	// re-executing would double-apply non-idempotent commands and failing
	// would stamp a spurious error onto commands that succeeded. The
	// re-issue errors stay on the retried SETs; the connection, which may
	// hold unread replies, is marked for removal when released.
	if err := c.himportRetryFailedSets(ctx, cn, cmds); err != nil {
		internal.Logger.Printf(ctx, "himport: pipeline set re-issue failed: %v", err)
		cn.MarkCloseOnPut("himport: transport error during set re-issue")
	}

	// Preserve retryable first-command errors (e.g. LOADING) for the outer
	// loop; the re-issue above may have cleared it. rawErr: this runs on the
	// execution path; never await here (an async autopipeline command's ready
	// channel is closed by this very batch — Err() would self-deadlock).
	if readErr != nil {
		readErr = cmds[0].rawErr()
	}
	return readErr != nil, readErr
}

func (c *baseClient) pipelineReadCmds(ctx context.Context, cn *pool.Conn, rd *proto.Reader, cmds []Cmder) error {
	return c.pipelineReadCmdsWithFlags(ctx, cn, rd, cmds, nil)
}

func (c *baseClient) pipelineReadCmdsWithFlags(ctx context.Context, cn *pool.Conn, rd *proto.Reader, cmds []Cmder, flags []byte) error {
	samples := otel.Enabled(c.recorder())
	for i, cmd := range cmds {
		if flags != nil && flags[i] != 0 {
			if err := c.readCSCPipelinePrefix(ctx, cn, rd); err != nil {
				setCmdsErr(cmds[i:], err)
				return err
			}
		}
		// To be sure there are no buffered push notifications, we process them before reading the reply
		if err := c.processPendingPushNotificationWithReader(ctx, cn, rd); err != nil {
			internal.Logger.Printf(ctx, "push: error processing pending notifications before reading reply: %v", err)
		}
		err := cmd.readReply(rd)
		cmd.SetErr(err)
		if err == nil && samples {
			c.recordCommandSamplesGuarded(ctx, cmd, cn)
		}
		if err != nil && !isRedisError(err) {
			setCmdsErr(cmds[i+1:], err)
			return wrapServingEndpoint(cn, err)
		}
	}
	// Retry errors like "LOADING redis is loading the dataset in memory".
	// rawErr: this runs on the execution path; never await here (an async
	// autopipeline command's ready channel is closed by this very batch).
	// Wrap the batch error so hostless MOVED/ASK from a handed-off connection
	// still carry the serving endpoint for ClusterClient (#4233 review).
	return wrapServingEndpoint(cn, cmds[0].rawErr())
}

func (c *baseClient) txPipelineProcessCmds(
	ctx context.Context, cn *pool.Conn, cmds []Cmder,
) (bool, error) {
	// Drain pending push notifications before writing the transaction. A drain error
	// may leave the reply stream desynchronized, so fail the batch and remove the
	// conn (errConnUnusable) rather than logging on. (generalProcessPipeline drains
	// once more before calling this — belt-and-suspenders gate.)
	if err := c.processPushNotifications(ctx, cn); err != nil {
		err = fmt.Errorf("%w: txpipeline push drain: %w", errConnUnusable, err)
		setCmdsErr(cmds, err)
		return false, err
	}
	markPipelineExecuted(ctx, cmds)

	// HIMPORT bookkeeping: pending discards for this session and PREPAREs
	// for registered fieldsets the transaction references get written ahead
	// of MULTI; the session state is visible inside the transaction.
	injected := c.himportInjectedCmds(ctx, cn, cmds)
	flags := c.cscPipelineFlags(cn, cmds)

	if err := cn.WithWriter(c.context(ctx), c.opt.WriteTimeout, func(wr *proto.Writer) error {
		for _, ic := range injected {
			if err := writeCmd(wr, ic); err != nil {
				return err
			}
		}
		return writeCSCPipelineCmds(wr, cmds, flags)
	}); err != nil {
		if flags != nil {
			_ = cn.Close()
		}
		setCmdsErr(cmds, err)
		return true, err
	}

	if err := cn.WithReader(c.context(ctx), c.opt.ReadTimeout, func(rd *proto.Reader) error {
		if err := c.himportReadInjectedReplies(ctx, cn, rd, injected); err != nil {
			// Transport error with every transaction reply unread: stamp
			// the batch like a write failure (see pipelineProcessCmds).
			setCmdsErr(cmds, err)
			return err
		}

		if flags != nil && flags[0] != 0 {
			if err := c.readCSCPipelinePrefix(ctx, cn, rd); err != nil {
				setCmdsErr(cmds, err)
				return err
			}
		}
		statusCmd := cmds[0].(*StatusCmd)
		// Trim multi and exec.
		trimmedCmds := cmds[1 : len(cmds)-1]

		if err := c.txPipelineReadQueued(ctx, cn, rd, statusCmd, trimmedCmds); err != nil {
			setCmdsErr(cmds, err)
			return err
		}

		// Read replies.
		err := c.pipelineReadCmds(ctx, cn, rd, trimmedCmds)
		if err == nil || isRedisError(err) {
			c.himportAfterBatch(cn, injected, trimmedCmds)
		}
		return err
	}); err != nil {
		return false, err
	}

	return false, nil
}

// txPipelineReadQueued reads queued replies from the Redis server.
// It returns an error if the server returns an error or if the number of replies does not match the number of commands.
func (c *baseClient) txPipelineReadQueued(ctx context.Context, cn *pool.Conn, rd *proto.Reader, statusCmd *StatusCmd, cmds []Cmder) error {
	// To be sure there are no buffered push notifications, we process them before reading the reply
	if err := c.processPendingPushNotificationWithReader(ctx, cn, rd); err != nil {
		internal.Logger.Printf(ctx, "push: error processing pending notifications before reading reply: %v", err)
	}
	// Parse +OK.
	if err := statusCmd.readReply(rd); err != nil {
		// A rejected MULTI (e.g. NOPERM) does not stop the server from
		// replying to the commands that follow it and to EXEC, so those
		// replies are still owed on this connection.
		cn.MarkCloseOnPut("tx: MULTI rejected")
		return err
	}

	// Parse +QUEUED.
	for _, cmd := range cmds {
		// To be sure there are no buffered push notifications, we process them before reading the reply
		if err := c.processPendingPushNotificationWithReader(ctx, cn, rd); err != nil {
			internal.Logger.Printf(ctx, "push: error processing pending notifications before reading reply: %v", err)
		}
		if err := statusCmd.readReply(rd); err != nil {
			cmd.SetErr(err)
			if !isRedisError(err) {
				return err
			}
		}
	}

	// To be sure there are no buffered push notifications, we process them before reading the reply
	if err := c.processPendingPushNotificationWithReader(ctx, cn, rd); err != nil {
		internal.Logger.Printf(ctx, "push: error processing pending notifications before reading reply: %v", err)
	}
	// Parse number of replies.
	line, err := rd.ReadLine()
	if err != nil {
		if err == Nil {
			err = TxFailedErr
		} else if !IsExecAbortError(err) {
			// A refused EXEC can leave MULTI active even though its error
			// is a complete Redis reply. Do not let another borrower inherit
			// that state, including when no WATCH was issued.
			cn.MarkCloseOnPut("tx: EXEC did not clear transaction state")
		}
		return err
	}

	if line[0] != proto.RespArray {
		return fmt.Errorf("redis: expected '*', but got line %q", line)
	}
	// EXEC replies once per queued command. A command the server rejects
	// without queuing (WATCH or MULTI inside MULTI) shortens the array, so the
	// replies would be read into the wrong commands; a longer array would leave
	// replies unread on the connection.
	if n, err := strconv.Atoi(string(line[1:])); err != nil || n != len(cmds) {
		return fmt.Errorf("redis: EXEC returned %q for %d commands", line, len(cmds))
	}

	return nil
}

//------------------------------------------------------------------------------

// Client is a Redis client representing a pool of zero or more underlying connections.
// It's safe for concurrent use by multiple goroutines.
//
// Client creates and frees connections automatically; it also maintains a free pool
// of idle connections. You can control the pool size with Config.PoolSize option.
type Client struct {
	*baseClient
	cmdable

	// lifecycleOwner keeps the client that owns shared CSC/metadata workers
	// reachable from WithTimeout clones.
	lifecycleOwner *Client

	autopipelinerMu     *sync.Mutex    // guards the autopipeliner fields against concurrent first-call creation
	autopipeliner       *AutoPipeliner // blocking face (Client.AutoPipeline)
	asyncAutopipeliner  *AutoPipeliner // deferred face (Client.AsyncAutoPipeline)
	autopipelinerClosed bool           // set by Close: refuse to resurrect a pipeliner on a closed client
}

// NewClient returns a client to the Redis Server specified by Options.
// Passing nil Options will cause a panic.
func NewClient(opt *Options) *Client {
	if opt == nil {
		panic("redis: NewClient nil options")
	}
	// clone to not share options with the caller
	opt = opt.clone()
	fixedDialEndpoint := opt.Dialer == nil
	opt.init()

	// Push notifications are always enabled for RESP3 (cannot be disabled)

	c := Client{
		baseClient: &baseClient{
			apClosed:          &atomic.Bool{},
			opt:               opt,
			fixedDialEndpoint: fixedDialEndpoint,
			cscPolicy:         opt.cachePolicy(),
			onClose:           &onCloseHooks{},
			himport:           newHImportRegistry(),
		},
	}
	c.rec, c.poolMetrics = clientRecorder(opt.OTelRecorder)
	c.init()

	// Close a partially-built client if a construction step below panics before
	// NewClient returns. The pools (and their background goroutines) are created
	// below, but several later steps can panic — maintnotifications in
	// ModeEnabled failing, or a custom push processor rejecting handler
	// registration. The panic propagates to the caller (some callers, e.g.
	// MultiDB AddDatabase, recover it), yet &c is never returned, so without this
	// those pools would leak with no reference left to Close them. closeResources
	// is nil-safe for a partially-built client, and the panic still propagates:
	// this defer runs during unwind and does not recover.
	built := false
	defer func() {
		if !built {
			_ = c.Close()
		}
	}()

	// Initialize push notification processor using shared helper
	// Use void processor for RESP2 connections (push notifications not available)
	c.pushProcessor = initializePushProcessor(opt)
	// set opt push processor for child clients
	c.opt.PushNotificationProcessor = c.pushProcessor

	// Generate unique pool names for metrics
	uniqueID := generateUniqueID()
	mainPoolName := opt.Addr + "_" + uniqueID
	pubsubPoolName := opt.Addr + "_" + uniqueID + "_pubsub"

	// Create connection pools. Assign the fields only AFTER the error check:
	// newConnPool/newPubSubPool return a nil *pool on error, and assigning that
	// straight to the pool.Pooler field would leave a typed-nil interface that
	// closeResources treats as present (its != nil check passes), so the
	// panic-cleanup defer above would nil-deref inside ConnPool.Close instead of
	// surfacing the original construction error. A local var keeps the field nil
	// on failure. The pipeline pool below already follows this pattern.
	connPool, err := newConnPool(opt, c.dialHook, mainPoolName, c.poolMetrics)
	if err != nil {
		panic(fmt.Errorf("redis: failed to create connection pool: %w", err))
	}
	c.connPool = connPool
	pubSubPool, err := newPubSubPool(opt, c.dialHook, pubsubPoolName, c.poolMetrics)
	if err != nil {
		panic(fmt.Errorf("redis: failed to create pubsub pool: %w", err))
	}
	c.pubSubPool = pubSubPool

	// Create the dedicated pipeline pool unconditionally, like pubSubPool: it
	// is pure burst capacity (no pre-dialing by default, small cap, larger buffers — see
	// pipelinePoolOptions), so an unused pipeline pool holds zero connections
	// and costs nothing. Pipelines stop competing with regular commands for
	// main-pool connections; a burst wider than the pool's cap spills back to
	// the main pool (see withPipelineConn). PipelinePoolSize < 0 opts out.
	if opt.PipelinePoolSize >= 0 {
		ref, err := c.buildPipelinePool(opt.Addr + "_" + uniqueID + "_pipeline")
		if err != nil {
			panic(fmt.Errorf("redis: failed to create pipeline connection pool: %w", err))
		}
		c.pipelinePool = ref
	}

	c.initStreamingCredentials()

	// Create metadata independently of CSC. The zero config reuses the package
	// default without allocating a store.
	c.baseClient.metadataSource = opt.cscMetadataSource
	if c.baseClient.metadataSource == nil {
		c.baseClient.cmdMeta = newCommandMetadataStore(opt.CommandMetadata, c.fetchCommandMetadata)
	}

	// CSC wiring (SharedTracking): shared cache + per-connection CLIENT TRACKING +
	// background drainer. attachCSC is the strategy dispatch entry.
	if opt.Protocol == 3 {
		var cache Cache
		if explicit := opt.ClientSideCache; explicit != nil {
			cache = explicit
		} else if cfg := opt.ClientSideCacheConfig; cfg != nil {
			cache = NewLocalCache(*cfg)
			// We constructed it, so we own it (may flush on drainer stop).
			c.baseClient.cscOwnsCache = true
		}
		// CSC is only supported with the built-in push processor: it depends on the
		// built-in draining kernel-only readability and treating a boundary read timeout
		// as benign. Warn (do not fail) when a custom processor is paired with CSC — the
		// contract is that behavior is otherwise undefined. See .claude/specs/push.md.
		if cache != nil {
			if _, builtin := c.pushProcessor.(*push.Processor); !builtin {
				internal.Logger.Printf(context.Background(),
					"redis: client-side caching enabled with a custom PushNotificationProcessor; "+
						"CSC is only supported with the built-in processor, behavior is otherwise undefined "+
						"(invalidation freshness and idle-connection health not guaranteed)")
			}
			// Publish the weak back-reference to the canonical *Client BEFORE attachCSC
			// starts the drainer. The push-handler adapter's canonical close reads
			// cscClientWeak on the drainer goroutine; setting it afterwards (it used to be
			// set only later, in cscRegisterCleanups) raced that read and could miss the
			// close for a push that arrived in the window. Weak so a dropped *Client stays
			// collectible and its GC cleanup still fires.
			c.baseClient.cscClientWeak = weak.Make(&c)
		}
		c.baseClient.attachCSC(context.Background(), cache)
	}
	// Stop metadata and CSC workers if the client is collected without Close.
	cscRegisterCleanups(&c)

	// Initialize maintnotifications first if enabled and protocol is RESP3
	if opt.MaintNotificationsConfig != nil && opt.MaintNotificationsConfig.Mode != maintnotifications.ModeDisabled && opt.Protocol == 3 {
		err := c.enableMaintNotificationsUpgrades()
		if err != nil {
			internal.Logger.Printf(context.Background(), "failed to initialize maintnotifications: %v", err)
			if opt.MaintNotificationsConfig.Mode == maintnotifications.ModeEnabled {
				/*
					Design decision: panic here to fail fast if maintnotifications cannot be enabled when explicitly requested.
					We choose to panic instead of returning an error to avoid breaking the existing client API, which does not expect
					an error from NewClient. This ensures that misconfiguration or critical initialization failures are surfaced
					immediately, rather than allowing the client to continue in a partially initialized or inconsistent state.
					Clients relying on maintnotifications should be aware that initialization errors will cause a panic, and should
					handle this accordingly (e.g., via recover or by validating configuration before calling NewClient).
					This approach is only used when MaintNotificationsConfig.Mode is MaintNotificationsEnabled, indicating that maintnotifications
					upgrades are required for correct operation. In other modes, initialization failures are logged but do not panic.
				*/
				panic(fmt.Errorf("failed to enable maintnotifications: %w", err))
			}
		}
	}

	// Register pools with OTel recorder if it supports pool registration
	// This allows async gauge metrics to pull stats from pools periodically
	otel.RegisterPools(c.recorder(), c.connPool, c.pubSubPool, c.getPipelinePool())

	built = true
	return &c
}

func (c *Client) init() {
	// Fresh per-Client guard and no inherited autopipeliner: a WithTimeout clone
	// (clone := *c) must not share the parent's mutex or AutoPipeliner instance.
	c.autopipelinerMu = &sync.Mutex{}
	c.autopipeliner = nil
	c.asyncAutopipeliner = nil
	c.cmdable = c.Process
	c.captureCacheIntent = c.opt.ClientSideCacheConfig != nil || c.opt.ClientSideCache != nil
	c.initHooks(hooks{
		dial:       c.baseClient.dial,
		process:    c.baseClient.process,
		pipeline:   c.baseClient.processPipeline,
		txPipeline: c.baseClient.processTxPipeline,
	})
}

// WithTimeout returns a clone sharing the parent's connection pools with the
// given read/write timeout. The clone caches its own autopipeliners, separate
// from the parent's: an AutoPipeline()/AsyncAutoPipeline() created on the clone
// registers a close hook on the shared pool. Closing any pool-sharing wrapper
// (the parent or another clone) drains and stops every registered engine's
// background flusher before the shared pools are torn down, so no flusher
// outlives the pool. That shared-pool drain does not mark the clone's
// autopipeliner closed, so an explicit Close on the clone still fully tears it
// (and its autopipeliners) down; Close is idempotent.
func (c *Client) WithTimeout(timeout time.Duration) *Client {
	// Snapshot under the guard: AutoPipeline()/Close() mutate the
	// autopipeliner fields concurrently, so a bare struct copy of them is a
	// data race (init below discards the copied values either way).
	c.autopipelinerMu.Lock()
	clone := *c
	c.autopipelinerMu.Unlock()
	if c.lifecycleOwner != nil {
		clone.lifecycleOwner = c.lifecycleOwner
	} else if c.baseClient.cscDrainHandle != nil || c.baseClient.cmdMeta != nil {
		clone.lifecycleOwner = c
	}
	clone.baseClient = c.baseClient.withTimeout(timeout)
	// Route the clone's CSC push-handler Close through the OWNER wrapper.
	// clone() copies neither cscDrainHandle nor cscClientWeak, so without this a
	// custom push handler calling Close() on the cscHandlerClient installed for a
	// held-conn drain on this clone would fall through to baseClient.Close —
	// closing the SHARED pools while the owner's drainer and cached autopipeliners
	// keep running against them. lifecycleOwner is the canonical wrapper and is
	// kept alive by this clone's strong ref, so closeCanonical resolves it and
	// calls owner.Close() (the full CSC + autopipeliner teardown).
	if clone.lifecycleOwner != nil {
		clone.baseClient.cscClientWeak = weak.Make(clone.lifecycleOwner)
	}
	clone.init()
	return &clone
}

// Close closes the client, stopping both cached autopipeliners (the blocking
// AutoPipeline instance and the async AsyncAutoPipeline instance, if created)
// before releasing the underlying resources, so their background flusher
// goroutines don't outlive the client. AutoPipeliner.Close is idempotent and
// safe to call here even if autopipelining was never used.
// WithTimeout clones delegate shared-resource teardown to their owner.
func (c *Client) Close() error {
	c.autopipelinerMu.Lock()
	ap, async := c.autopipeliner, c.asyncAutopipeliner
	c.autopipeliner, c.asyncAutopipeliner = nil, nil
	// A later AutoPipeline()/AsyncAutoPipeline() call must not build a fresh
	// pipeliner against the closed pools: nothing would ever close it and its
	// flusher goroutines would leak. The getters check this flag.
	c.autopipelinerClosed = true
	c.autopipelinerMu.Unlock()
	var firstErr error
	for _, p := range []*AutoPipeliner{ap, async} {
		if p != nil {
			if err := p.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	if c.lifecycleOwner != nil {
		// Delegate through the OWNER's *Client.Close, not its baseClient:
		// the owner may hold cached autopipeliners of its own whose flusher
		// goroutines must stop with the shared pools, and its
		// autopipelinerClosed flag must flip so later owner getters cannot
		// resurrect a pipeliner against closed pools. Client.Close is
		// idempotent through baseClient.Close, so an owner also closed
		// directly is fine.
		if err := c.lifecycleOwner.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	}
	if err := c.baseClient.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// Conn returns a Conn that runs every command on a single connection borrowed
// from this client's pool. Close returns that connection to the pool, so any
// session state left on it (AUTH, SELECT, CLIENT SETNAME, CLIENT TRACKING, ...)
// is still there for the next pooled caller. Use EphemeralConn when the Conn
// changes session state.
func (c *Client) Conn() *Conn {
	return c.conn(false)
}

// EphemeralConn is Conn, except Close discards the underlying connection
// instead of returning it to the pool. Session state a Conn sets (AUTH,
// SELECT, CLIENT SETNAME, CLIENT TRACKING, RESET, ...) cannot be undone
// generically on release, so a connection that carried it must not go on to
// serve unrelated callers. Without client-side caching, the first command
// reuses an idle pooled connection if available.
//
// With client-side caching active, the first command dials a fresh, untracked
// connection so session commands cannot disrupt the parent's cache. Reads on
// this connection always hit the server. It obeys MaxActiveConns and
// MaxConcurrentDials without acquiring a PoolSize turn, and does not support
// maintenance handoff.
// Close must be called to release the connection.
func (c *Client) EphemeralConn() *Conn {
	return c.conn(true)
}

func (c *Client) conn(discardOnClose bool) *Conn {
	isolated := discardOnClose && c.baseClient.cscTrackingRequested()
	connOpt := c.opt
	if isolated {
		// This socket has no maintenance handoff manager. Keep its skipped
		// maintenance negotiation from downgrading the shared parent config.
		connOpt = c.baseClient.cloneOpt()
		if connOpt.MaintNotificationsConfig != nil {
			connOpt.MaintNotificationsConfig.Mode = maintnotifications.ModeDisabled
		}
	}
	var sticky *pool.StickyConnPool
	if isolated {
		sticky = pool.NewStickyConnPool(c.connPool)
		sticky.NewConnOnGet()
	} else {
		sticky = c.baseClient.newStickyConnPool()
	}
	if discardOnClose {
		sticky.DiscardOnClose()
	}
	// Share the HIMPORT fieldset registry: the sticky pool borrows
	// connections from this client's pool, so fieldsets prepared on them
	// stay valid after the connections are returned.
	conn := newConn(connOpt, sticky, &c.hooksMixin, c.himport)
	// The sticky wrapper borrows this client's fixed endpoint and dial budget.
	conn.baseClient.fixedDialEndpoint = c.fixedDialEndpoint
	conn.baseClient.dialExhaustionPool, _ = c.connPool.(*pool.ConnPool)
	if isolated {
		// Never enable tracking or attach the parent's cache to a socket whose
		// session state the caller may change freely.
		// Credential providers disable CSC in attachCSC, so isolated sessions
		// cannot require the streaming-credentials manager copied below.
		return conn
	}
	// A sticky client does not serve cache hits, but a new pool connection first
	// initialized through it may later be reused by the parent. Share the
	// successful-attachment signal so that connection is tracked exactly when
	// the parent's CSC is active.
	conn.baseClient.cscActive = c.baseClient.cscActive
	// A cold borrowed connection must select the parent's immutable mode before
	// returning to its pool, even though this wrapper never serves cache hits.
	conn.baseClient.cscPolicy = c.baseClient.cscPolicy
	// No-op today: the strategy needs an idle-conn drainer and a StickyConnPool
	// has none, so CSC isn't active on a Conn() (its reads hit the server). Kept
	// so a future sticky-pool-capable strategy attaches here.
	conn.baseClient.attachCSC(context.Background(), c.csc)
	// Carry the parent's shared eviction hook so that if this derived client
	// initializes a pool conn, the close hook it installs still evicts from the
	// parent cache (its own csc is nil).
	conn.baseClient.cscPoolHook = c.baseClient.cscPoolHook
	// Carry the parent's streaming-credentials manager so initConn can create a
	// credentials listener when a cold Conn/EphemeralConn initializes a fresh
	// pool connection (issue #4154).
	conn.baseClient.streamingCredentialsManager = c.streamingCredentialsManager
	// Conn() never serves cache hits, but OPTOUT still prefixes uncached reads
	// when cscTrackable is true — that lookup needs the parent's overrides.
	c.baseClient.shareCommandMetadata(&conn.baseClient)
	return conn
}

func (c *Client) Process(ctx context.Context, cmd Cmder) error {
	err := c.processHook(ctx, cmd)
	// Keep serving-endpoint metadata on the returned error for ClusterClient,
	// but never expose the wrapper via cmd.Err() (#4233 review).
	cmd.SetErr(unwrapServingEndpoint(err))
	return err
}

// Options returns read-only *Options that were used to create the client.
// Any alteration of the returned *Options may result in undefined behaviour.
func (c *Client) Options() *Options {
	return c.opt
}

// NodeAddress returns the address of the Redis node as reported by the server.
// For cluster clients, this is the endpoint from CLUSTER SLOTS before any transformation
// (e.g., loopback replacement). For standalone clients, this defaults to Addr.
//
// This is useful for matching the source field in maintenance notifications
// (e.g. SMIGRATED).
func (c *Client) NodeAddress() string {
	return c.opt.NodeAddress
}

// GetMaintNotificationsManager returns the maintnotifications manager instance for monitoring and control.
// Returns nil if maintnotifications are not enabled.
func (c *Client) GetMaintNotificationsManager() *maintnotifications.Manager {
	c.maintNotificationsManagerLock.RLock()
	defer c.maintNotificationsManagerLock.RUnlock()
	return c.maintNotificationsManager
}

// initializePushProcessor initializes the push notification processor for any client type.
// This is a shared helper to avoid duplication across NewClient, NewFailoverClient, and NewSentinelClient.
func initializePushProcessor(opt *Options) push.NotificationProcessor {
	// Always use custom processor if provided
	if opt.PushNotificationProcessor != nil {
		return opt.PushNotificationProcessor
	}

	// Push notifications are always enabled for RESP3, disabled for RESP2
	if opt.Protocol == 3 {
		// Create default processor for RESP3 connections
		return NewPushNotificationProcessor()
	}

	// Create void processor for RESP2 connections (push notifications not available)
	return NewVoidPushNotificationProcessor()
}

// RegisterPushNotificationHandler registers a handler for a specific push notification name.
// Returns an error if a handler is already registered for this push notification name.
// If protected is true, the handler cannot be unregistered.
func (c *Client) RegisterPushNotificationHandler(pushNotificationName string, handler push.NotificationHandler, protected bool) error {
	return c.pushProcessor.RegisterHandler(pushNotificationName, handler, protected)
}

// GetPushNotificationHandler returns the handler for a specific push notification name.
// Returns nil if no handler is registered for the given name.
func (c *Client) GetPushNotificationHandler(pushNotificationName string) push.NotificationHandler {
	return c.pushProcessor.GetHandler(pushNotificationName)
}

type PoolStats pool.Stats

// PoolStats returns connection pool stats.
func (c *Client) PoolStats() *PoolStats {
	stats := c.connPool.Stats()
	stats.PubSubStats = *c.pubSubPool.Stats()
	if pp := c.getPipelinePool(); pp != nil {
		stats.PipelineStats = pp.Stats()
	}
	return (*PoolStats)(stats)
}

func (c *Client) Pipelined(ctx context.Context, fn func(Pipeliner) error) ([]Cmder, error) {
	return c.Pipeline().Pipelined(ctx, fn)
}

func (c *Client) Pipeline() Pipeliner {
	pipe := Pipeline{
		exec: pipelineExecer(c.processPipelineHook),
	}
	pipe.init()
	return &pipe
}

// AutoPipeline returns the blocking autopipeliner for this client: a drop-in
// replacement for the normal command surface where each command call (ap.Set,
// ap.Get, ...) blocks until executed, exactly like a plain client — but the
// engine batches concurrent callers' commands into pipelines, so throughput is
// far higher (measured locally over loopback: ~1M+ SET/sec vs ~100k; indicative, not a guarantee). Commands keep per-goroutine order.
//
// By default, Options.AutoPipelineOptions is used if set,
// otherwise DefaultBlockingAutoPipelineOptions (a single ordered batch stream,
// which maximizes throughput and minimizes latency for the blocking face — see
// its doc). The instance is cached and shared; the first
// call's config wins and later calls return the same instance until it is closed.
// It must be closed (or close the client) to release its goroutines.
//
// It returns an error if the supplied config is invalid (e.g. MaxConcurrentBatches>1
// without Unordered, or a negative size); on error no instance is cached.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
func (c *Client) AutoPipeline() (*AutoPipeliner, error) {
	return c.AutoPipelineWithOptions(nil)
}

// AutoPipelineWithOptions is AutoPipeline with explicit options instead of
// Options.AutoPipelineOptions / the default. The instance is cached and shared;
// the first call's config wins.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
func (c *Client) AutoPipelineWithOptions(config *AutoPipelineOptions) (*AutoPipeliner, error) {
	return getOrCreateAutoPipeliner(c.autopipelinerMu, &c.autopipeliner, &c.autopipelinerClosed, c.baseClient.apClosed, c.baseClient.onClose, onCloseHookIDAutoPipeline, config,
		func() *AutoPipelineOptions {
			if c.opt.AutoPipelineOptions != nil {
				return c.opt.AutoPipelineOptions
			}
			return DefaultBlockingAutoPipelineOptions()
		},
		func(cfg *AutoPipelineOptions) (*AutoPipeliner, error) { return newAutoPipeliner(c, cfg, true) })
}

// apCloseHookSeq makes each autopipeliner engine's onClose hook id unique so a
// client and its WithTimeout clone (which share the onClose registry) do not
// collide on a per-slot constant id. The hook is registered once, under the
// getOrCreateAutoPipeliner mutex, on the fresh build; ap.Close unregisters it.
var apCloseHookSeq atomic.Uint64

// AsyncAutoPipeline returns the deferred (async) autopipeliner: command calls
// return immediately and the result accessors (Val/Result/Err) block until the
// command has executed. Submit a window of commands, then read their results, to
// keep each pipeline deep and reach the highest throughput (measured locally over loopback: ~2-3M SET/sec; indicative).
//
// By default, Options.AutoPipelineOptions is used if set,
// otherwise DefaultAutoPipelineOptions (ordered, MaxConcurrentBatches: 1) — a
// single goroutine's deferred commands execute in submit order. Use AsyncAutoPipelineWithOptions
// to override (and, for parallel batches, set Unordered). The instance is
// cached and shared; the first call's config wins. Close it (or the client) to
// release its goroutines.
//
// It returns an error if the supplied config is invalid (e.g. MaxConcurrentBatches>1
// without Unordered, or a negative size); on error no instance is cached.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
func (c *Client) AsyncAutoPipeline() (*AutoPipeliner, error) {
	return c.AsyncAutoPipelineWithOptions(nil)
}

// AsyncAutoPipelineWithOptions is AsyncAutoPipeline with an explicit config
// instead of Options.AutoPipelineOptions / the default. The instance is cached
// and shared; the first call's config wins.
//
// EXPERIMENTAL: this API is subject to change, use with caution.
func (c *Client) AsyncAutoPipelineWithOptions(config *AutoPipelineOptions) (*AutoPipeliner, error) {
	return getOrCreateAutoPipeliner(c.autopipelinerMu, &c.asyncAutopipeliner, &c.autopipelinerClosed, c.baseClient.apClosed, c.baseClient.onClose, onCloseHookIDAsyncAutoPipeline, config,
		func() *AutoPipelineOptions {
			if c.opt.AutoPipelineOptions != nil {
				return c.opt.AutoPipelineOptions
			}
			return DefaultAutoPipelineOptions()
		},
		func(cfg *AutoPipelineOptions) (*AutoPipeliner, error) { return newAutoPipeliner(c, cfg, false) })
}

func (c *Client) TxPipelined(ctx context.Context, fn func(Pipeliner) error) ([]Cmder, error) {
	return c.TxPipeline().Pipelined(ctx, fn)
}

// TxPipeline acts like Pipeline, but wraps queued commands with MULTI/EXEC.
func (c *Client) TxPipeline() Pipeliner {
	pipe := Pipeline{
		exec: func(ctx context.Context, cmds []Cmder) error {
			cmds = wrapMultiExec(ctx, cmds)
			return c.processTxPipelineHook(ctx, cmds)
		},
	}
	pipe.init()
	return &pipe
}

// removeStreamingCredentialsState drops the re-auth state of a pub/sub
// connection whose initConn failed before it installed the close callback
// that removes it.
func (c *baseClient) removeStreamingCredentialsState(cn *pool.Conn) {
	if c.streamingCredentialsManager != nil {
		c.streamingCredentialsManager.RemoveConn(cn.GetID())
	}
}

func (c *Client) pubSub() *PubSub {
	pubsub := &PubSub{
		opt: c.cloneOpt(),
		rec: c.rec,
		newConn: func(ctx context.Context, addr string, channels []string, onInit ...func(*pool.Conn)) (*pool.Conn, error) {
			return c.newPubSubConn(ctx, addr, channels, onInit...)
		},
		closeConn: func(cn *pool.Conn) error {
			// Untrack connection from PubSubPool
			c.pubSubPool.UntrackConn(cn)
			_ = cn.Close()
			return nil
		},
		pushProcessor: c.pushProcessor,
	}
	pubsub.init()

	return pubsub
}

// Subscribe subscribes the client to the specified channels.
// Channels can be omitted to create empty subscription.
// Note that this method does not wait on a response from Redis, so the
// subscription may not be active immediately. To force the connection to wait,
// you may call the Receive() method on the returned *PubSub like so:
//
//	sub := client.Subscribe(queryResp)
//	iface, err := sub.Receive()
//	if err != nil {
//	    // handle error
//	}
//
//	// Should be *Subscription, but others are possible if other actions have been
//	// taken on sub since it was created.
//	switch iface.(type) {
//	case *Subscription:
//	    // subscribe succeeded
//	case *Message:
//	    // received first message
//	case *Pong:
//	    // pong received
//	default:
//	    // handle error
//	}
//
//	ch := sub.Channel()
func (c *Client) Subscribe(ctx context.Context, channels ...string) *PubSub {
	pubsub := c.pubSub()
	if len(channels) > 0 {
		_ = pubsub.Subscribe(ctx, channels...)
	}
	return pubsub
}

// PSubscribe subscribes the client to the given patterns.
// Patterns can be omitted to create empty subscription.
func (c *Client) PSubscribe(ctx context.Context, channels ...string) *PubSub {
	pubsub := c.pubSub()
	if len(channels) > 0 {
		_ = pubsub.PSubscribe(ctx, channels...)
	}
	return pubsub
}

// SSubscribe Subscribes the client to the specified shard channels.
// Channels can be omitted to create empty subscription.
func (c *Client) SSubscribe(ctx context.Context, channels ...string) *PubSub {
	pubsub := c.pubSub()
	if len(channels) > 0 {
		_ = pubsub.SSubscribe(ctx, channels...)
	}
	return pubsub
}

//------------------------------------------------------------------------------

// Conn represents a single Redis connection rather than a pool of connections.
// Prefer running commands from Client unless there is a specific need
// for a continuous single Redis connection.
type Conn struct {
	baseClient
	cmdable
	statefulCmdable
}

// newConn is a helper func to create a new Conn instance.
// The Conn instance is not thread-safe and should not be shared between goroutines.
// The parentHooks will be cloned, no need to clone before passing it.
// himport is the HIMPORT fieldset registry the Conn participates in — pass
// the owning client's registry (a private one would restart versions at 1
// and collide with the client's version space on the shared pooled
// connections); nil disables HIMPORT tracking.
func newConn(opt *Options, connPool pool.Pooler, parentHooks *hooksMixin, himport *himportRegistry) *Conn {
	c := Conn{
		baseClient: baseClient{
			apClosed: &atomic.Bool{},
			opt:      opt,
			// The handshake commands run through here record where the owning
			// client does; opt is the client's (or a copy of it).
			rec:      scopedRecorder(opt.OTelRecorder),
			connPool: connPool,
			onClose:  &onCloseHooks{},
			himport:  himport,
		},
	}

	if parentHooks != nil {
		c.hooksMixin = parentHooks.clone()
	}

	// Initialize push notification processor using shared helper
	// Use void processor for RESP2 connections (push notifications not available)
	c.pushProcessor = initializePushProcessor(opt)

	c.cmdable = c.Process
	c.statefulCmdable = c.Process
	c.initHooks(hooks{
		dial:       c.baseClient.dial,
		process:    c.baseClient.process,
		pipeline:   c.baseClient.processPipeline,
		txPipeline: c.baseClient.processTxPipeline,
	})

	return &c
}

func (c *Conn) Process(ctx context.Context, cmd Cmder) error {
	err := c.processHook(ctx, cmd)
	// Same as Client.Process: keep the serving-endpoint wrapper off cmd.Err().
	cmd.SetErr(unwrapServingEndpoint(err))
	return err
}

// RegisterPushNotificationHandler registers a handler for a specific push notification name.
// Returns an error if a handler is already registered for this push notification name.
// If protected is true, the handler cannot be unregistered.
func (c *Conn) RegisterPushNotificationHandler(pushNotificationName string, handler push.NotificationHandler, protected bool) error {
	return c.pushProcessor.RegisterHandler(pushNotificationName, handler, protected)
}

func (c *Conn) Pipelined(ctx context.Context, fn func(Pipeliner) error) ([]Cmder, error) {
	return c.Pipeline().Pipelined(ctx, fn)
}

func (c *Conn) Pipeline() Pipeliner {
	pipe := Pipeline{
		exec: c.processPipelineHook,
	}
	pipe.init()
	return &pipe
}

func (c *Conn) TxPipelined(ctx context.Context, fn func(Pipeliner) error) ([]Cmder, error) {
	return c.TxPipeline().Pipelined(ctx, fn)
}

// TxPipeline acts like Pipeline, but wraps queued commands with MULTI/EXEC.
func (c *Conn) TxPipeline() Pipeliner {
	pipe := Pipeline{
		exec: func(ctx context.Context, cmds []Cmder) error {
			cmds = wrapMultiExec(ctx, cmds)
			return c.processTxPipelineHook(ctx, cmds)
		},
	}
	pipe.init()
	return &pipe
}

// processPushNotifications processes all pending push notifications on a connection
// This ensures that cluster topology changes are handled immediately before the connection is used
// This method should be called by the client before using WithReader for command execution
//
// Performance optimization: Skip the expensive MaybeHasData() syscall if a health check
// was performed recently (within 5 seconds). The health check already verified the connection
// is healthy and checked for unexpected data (push notifications).
func (c *baseClient) processPushNotifications(ctx context.Context, cn *pool.Conn) error {
	// Only process push notifications for RESP3 connections with a processor
	if c.opt.Protocol != 3 || c.pushProcessor == nil {
		return nil
	}

	// Performance optimization: Skip MaybeHasData() syscall if health check was recent
	// If the connection was health-checked within the last 5 seconds, we can skip the
	// expensive syscall since the health check already verified no unexpected data.
	// This is safe because:
	// 0. lastHealthCheckNs is set in pool/conn.go:putConn() after a successful health check
	// 1. Health check (connCheck) uses the same syscall (Recvfrom with MSG_PEEK)
	// 2. If push notifications arrived, they would have been detected by health check
	// 3. 5 seconds is short enough that connection state is still fresh
	// 4. Push notifications will be processed by the next WithReader call
	// used it is set on getConn, so we should use another timer (lastPutAt?)
	lastHealthCheckNs := cn.LastPutAtNs()
	if lastHealthCheckNs > 0 {
		// Use pool's cached time to avoid expensive time.Now() syscall
		nowNs := pool.GetCachedTimeNs()
		if nowNs-lastHealthCheckNs < int64(5*time.Second) {
			// Recent health check confirmed no unexpected data, skip the syscall
			return nil
		}
	}

	return c.peekAndProcessPushNotifications(ctx, cn)
}

// peekAndProcessPushNotifications peeks the socket and processes any pending
// push notifications on cn unconditionally, bypassing the recent-health-check
// shortcut in processPushNotifications. Required on paths that do not follow
// up with a reply read on the same connection (e.g. the CSC cache-hit drain),
// where the shortcut would otherwise suppress invalidations buffered since the
// last health check.
func (c *baseClient) peekAndProcessPushNotifications(ctx context.Context, cn *pool.Conn) error {
	if c.opt.Protocol != 3 || c.pushProcessor == nil {
		return nil
	}

	// Also drain when the reader already holds buffered bytes (HasBufferedData),
	// not only when the socket is readable (MaybeHasData). A reply and a trailing
	// invalidation can arrive in one socket read. The invalidation then stays in
	// the reader buffer while the socket is empty. MaybeHasData alone would skip
	// it and serve stale data until the next miss or MaxStaleness (#3965).
	buffered := cn.HasBufferedData()
	if !buffered && !cn.MaybeHasData() {
		return nil
	}

	// Drain even for a custom processor only when the reader already holds buffered
	// bytes. NOTE: HasBufferedData is byte-level (rd.Buffered() > 0), NOT proof of a
	// COMPLETE frame — a reply plus a partial trailing push in one socket read
	// leaves a partial push buffered. Handing that to a custom processor lets it
	// block to complete the frame under the hard cap; a mid-frame timeout surfaces
	// an error and the caller retires the (now-desynced) connection. That outcome
	// is SAFE (retiring a desynced conn is correct), but it does NOT eliminate
	// conn churn for custom processors — the earlier "buffered == a complete, safe
	// frame" claim was wrong.
	//
	// The gate is still worth it against the WORSE case: MaybeHasData WITHOUT
	// buffered data proves only that the raw socket is readable, which over TLS can
	// be a post-handshake control record with zero RESP bytes (conn_check.go
	// refuses to unwrap TLS for this reason; maybeHasData does). A custom processor
	// handed that empty input times out on an empty read → the FD session reader
	// treats the timeout as fatal and closes a HEALTHY connection, failing its
	// in-flight misses. The built-in buffered processor accepts the empty read. So
	// a custom processor drains only on real buffered data. Tradeoff: a push that
	// arrives as bare socket readiness on a custom, idle FD session waits for the
	// next reply read, the session recycle, or the MaxStaleness backstop; the
	// connection stays open. Mirrors the built-in-only guard on the opaque-
	// transport probe in the FD session tick (csc_miss_coalesce_modes.go).
	if !buffered {
		if _, builtin := c.pushProcessor.(*push.Processor); !builtin {
			return nil
		}
	}

	// Readiness is established, so a frame is (probably) present: drain under the
	// longer fragmented-frame budget the background drainer uses, NOT the 1ms
	// no-data probe cap. A push can arrive in fragments more than 1ms apart —
	// notably over TLS, where MaybeHasData only proves that some ciphertext is
	// readable — and the 1ms cap would then time out after consuming the prefix,
	// which is treated as fatal and fatally closes a healthy session (failing its
	// in-flight misses). The 1ms cap is reserved for speculative no-data probes
	// (timedPushDrain, the opaque-transport fallback).
	return c.pushDrainWithin(ctx, cn, cscDrainHardReadCap)
}

// timedPushDrain runs a speculative no-data push probe under a 1ms hard read
// deadline: for callers WITHOUT a readiness signal (the opaque-transport
// fallback on a held full-duplex connection), which pay at most 1ms when
// nothing is pending. Callers that HAVE established readiness use
// pushDrainWithin(cscDrainHardReadCap) instead, to tolerate fragmented frames.
func (c *baseClient) timedPushDrain(ctx context.Context, cn *pool.Conn) error {
	return c.pushDrainWithin(ctx, cn, time.Millisecond)
}

// pushDrainWithin runs one push drain on cn. HARD read deadlines (not WithReader) so
// an ordinary drain cannot silently inherit an unrelated deadline, and
// WithReaderHardDeadline clears the deadline on exit, so no residue poisons a later
// deadline-less read.
//
// The FIRST-byte wait is bounded by the short cap d; only a frame already begun gets
// the relaxation-aware budget (pushDrainBudget) to finish. Applying the relaxed budget
// to the first-byte wait would let a TLS control record — socket-readable with zero
// RESP bytes — block a speculative drain for the full relaxed timeout (seconds),
// stalling a ready command or the idle drainer (#3989). So under active relaxation,
// probe one byte under d first; a timeout there means no frame began (benign). The
// relaxed budget exists only to tolerate a fragmented frame mid-flight during a
// failover — see pushDrainBudget.
func (c *baseClient) pushDrainWithin(ctx context.Context, cn *pool.Conn, d time.Duration) error {
	budget := pushDrainBudget(cn, d)
	if budget > d {
		// Relaxation active: confirm a RESP byte under the short cap before granting the
		// longer budget. The peeked byte stays buffered for the drain below.
		if err := cn.WithReaderHardDeadline(d, func(rd *proto.Reader) error {
			_, perr := rd.Peek(1)
			return perr
		}); err != nil {
			if isTimeout, hasFlag := isTimeoutError(err); isTimeout && hasFlag {
				return nil // no frame began within the short cap
			}
			return err
		}
	}
	return cn.WithReaderHardDeadline(budget, func(rd *proto.Reader) error {
		// A speculative probe on a possibly-idle conn: stop when the reader buffer
		// empties (Buffered variant) rather than block waiting for a reply.
		return c.drainPushFrames(ctx, cn, rd, false)
	})
}

// pushDrainBudget returns the read budget for one push drain: the caller's hard
// cap d, raised to the connection's relaxed timeout while maintenance relaxation
// is active. The raise matters exactly when a push frame is mid-flight on a slow
// server: during a failover/migration — the very window relaxation covers — a
// frame can fragment past a small cap, and a mid-frame timeout is a desync
// (partial frame consumed), so the caller retires the connection and, on the
// pre-command path, fails the command outright when MaxRetries=0. Holding the
// conn up to the relaxed budget is the lesser cost, and it is paid only when
// bytes are actually mid-frame: an empty or completed drain returns without
// blocking regardless of the budget. Without active relaxation
// EffectiveReadTimeout returns d unchanged, keeping the small hard cap that
// protects a small pool from a parked speculative drain.
func pushDrainBudget(cn *pool.Conn, d time.Duration) time.Duration {
	if rel := cn.EffectiveReadTimeout(d); rel > d {
		return rel
	}
	return d
}

// drainPushFrames processes pending push notifications on rd. The caller already
// holds rd under a read deadline (WithReader or WithReaderHardDeadline); this
// helper does not set one, so it is safe to call from a reader that must keep
// reading afterward (the CSC miss reader reads the command reply next).
//
// blocking selects the built-in processor's drain discipline:
//
//   - blocking=true (reply-expected readers: the CSC miss and refresh readers):
//     block on the socket and skip push frames until a NON-push frame — the
//     command reply — is next, then return leaving it buffered. This is the same
//     non-buffered discipline the full-duplex reader already uses. The Buffered
//     variant instead stops the instant the reader buffer empties; if a second
//     invalidation is still on the socket ahead of the reply, the caller's
//     ReadRawReply would then read THAT push as the reply and cache it under the
//     wrong key — a one-frame shift that cascades to every later reply. Note
//     PeekReplyType is attribute-aware (it discards a RESP3 attribute prefix and
//     recurses), so a fragmented attribute needs no separate buffered scan. A
//     boundary-peek TIMEOUT the non-buffered loop swallows is caught by the
//     caller's shared read deadline: ReadRawReply hits the same expired deadline
//     and fails the session. This is the same non-buffered discipline the
//     full-duplex reader already runs; a swallowed NON-timeout peek error would
//     need a malformed RESP3 attribute mid-push (a server protocol bug), so this
//     path is no weaker than that one. Pub/sub-named pushes, which the drain
//     leaves buffered, cannot reach a CSC-held conn: subscriptions route to
//     PubSub-owned connections.
//
//   - blocking=false (readiness-established probes: pushDrainWithin, the FD
//     session tick, the background drainer): use the Buffered variant, which
//     drains the current batch and stops when the reader buffer empties so the
//     probe never blocks waiting for a reply that will not come on an idle conn.
//     An error AFTER partially consuming a fragmented frame is PROPAGATED, not
//     swallowed, so a mid-frame desync cannot later be read as a command reply.
//
// A custom processor is handed only a confirmed push frame in either mode: the
// interface does not promise the built-in's peek-and-consume-only-push behavior,
// and the next frame can be a coalesced REPLY. Peek the type first, propagate the
// peek error (PeekReplyType can partially consume an attribute via DiscardNext
// before it errors), and skip a non-push frame so a custom processor cannot
// consume the reply; ProcessPendingNotifications then blocks and skips pushes
// until a non-push is next, matching the built-in blocking discipline.
func (c *baseClient) drainPushFrames(ctx context.Context, cn *pool.Conn, rd *proto.Reader, blocking bool) error {
	handlerCtx := c.pushNotificationHandlerContext(cn)
	// Nonblocking Close adapter: this drain runs on CSC-held paths (the FD
	// session reader and tick among them), where a custom handler calling Close()
	// on the raw client would deadlock — Close waits on the coalescer's
	// WaitGroup, which includes the very goroutine parked in the handler.
	handlerCtx.Client = cscHandlerClient{baseClient: c}
	if processor, ok := c.pushProcessor.(*push.Processor); ok {
		if blocking {
			return processor.ProcessPendingNotifications(ctx, handlerCtx, rd)
		}
		return processor.ProcessPendingNotificationsBuffered(ctx, handlerCtx, rd)
	}
	// Custom processor: peek-then-process in a LOOP for blocking mode. A single
	// peek+process drained only the first push (and whatever that one invocation
	// consumed); a SECOND invalidation still on the socket ahead of the reply would
	// then be read by the caller's ReadRawReply as the command value — published to
	// the cache under the wrong key and shifting every later reply. PeekReplyType
	// blocks on the socket for the next frame, so looping skips every push (buffered
	// OR socket-pending) until a non-push (the reply) is next, matching the built-in
	// blocking discipline. Non-blocking probes keep the single-pass behavior (one
	// drain, never block waiting for a reply that will not come on an idle conn).
	for {
		t, err := rd.PeekReplyType()
		if err != nil {
			return err
		}
		if t != proto.RespPush {
			return nil
		}
		if err := c.pushProcessor.ProcessPendingNotifications(ctx, handlerCtx, rd); err != nil {
			return err
		}
		if !blocking {
			return nil
		}
	}
}

// cscFallbackProbeInterval bounds how often an idle connection without a
// portable readiness mechanism is subjected to a timed read. Post-command
// probes remain immediate; this is only the eventual invalidation fallback.
const cscFallbackProbeInterval = 100 * time.Millisecond

// drainPushNotifications drains push frames buffered on a connection the CSC
// drainer has claimed, under a HARD read deadline. processorSucceeded reports a
// successful processor invocation; it resets custom-processor damping even when
// the frame was hidden inside a transport wrapper. A non-nil error is
// connection-fatal (the drainer removes the conn), including a read timeout
// after reply consumption starts: the reader may be desynchronized. A custom
// processor's error is also fatal because its contract cannot prove no bytes
// were consumed.
func (c *baseClient) drainPushNotifications(cn *pool.Conn) (processorSucceeded bool, err error) {
	if c.opt.Protocol != 3 || c.pushProcessor == nil {
		return false, nil
	}
	// Skip only when nothing is buffered (reader) AND nothing on the socket:
	// MaybeHasData peeks only the socket, but an invalidate can sit in cn.rd.
	readPending := cn.TakeCscReadPending()
	periodicReadPending := cn.TakeCscPeriodicReadPending(cscFallbackProbeInterval)
	socketData, socketErr := cn.CheckForData()
	if socketErr != nil {
		return false, socketErr
	}
	// Capture the already-buffered state BEFORE the probe below: it decides whether a
	// custom processor may run (see the custom-processor gate). The probe can peek a
	// byte into the buffer, but a byte fetched from the socket/wrapper is NOT the
	// "real bytes already buffered" the spec requires for a custom processor.
	buffered := cn.HasBufferedData()
	hasData := buffered || socketData
	if !readPending && !periodicReadPending && !hasData {
		return false, nil
	}

	// No deadline cleanup is needed here: WithReaderHardDeadline (the probe and
	// drain reads below) already restores a cleared read deadline in its own defer
	// (SetReadDeadline(time.Time{}) — see its doc). A previous WithReader(ctx, 0)
	// cleanup here was both redundant and wrong: under an active relaxed
	// maintenance timeout it re-armed a relaxed deadline (getEffectiveReadTimeout
	// returns the relaxed value even for timeout 0), which a ReadTimeout<0 conn
	// then never clears on its next read, causing a spurious timeout later.

	if !hasData {
		// TLS and opaque wrappers can hide bytes from the socket readiness
		// check. Probe one byte without consuming it under a tiny deadline;
		// only a confirmed byte gets the longer fragmented-frame budget below.
		err := cn.WithReaderHardDeadline(cscDrainProbeReadCap, func(rd *proto.Reader) error {
			_, err := rd.Peek(1)
			return err
		})
		if err != nil {
			if isTimeout, hasTimeoutFlag := isTimeoutError(err); isTimeout && hasTimeoutFlag {
				return false, nil
			}
			return false, err
		}
	}

	// Custom-processor gate (spec: .claude/specs/push.md): a custom processor
	// implements only the blocking loop, so it gets work only when real RESP bytes
	// were ALREADY buffered — never on kernel-only readability, which over TLS can be
	// a control record with zero RESP bytes, and never on a byte the probe above just
	// fetched from the socket/wrapper. Handing it non-buffered data would run the
	// blocking loop into the hard cap; its error is always fatal for a custom
	// processor and would retire a healthy conn (the spurious-retire the spec warns
	// of). Skipping is bounded: the periodic fallback re-probes and MaxStaleness
	// backstops. The built-in buffered processor accepts kernel-only readability (a
	// boundary timeout is benign), so it still proceeds. Gate on `buffered` captured
	// before the probe, mirroring peekAndProcessPushNotifications.
	if !buffered {
		if _, builtin := c.pushProcessor.(*push.Processor); !builtin {
			return false, nil
		}
	}

	handlerCtx := c.pushNotificationHandlerContext(cn)
	handlerCtx.Client = cscHandlerClient{baseClient: c}
	// Relaxation-aware cap (spec: pushDrainBudget): during a failover/migration a push
	// frame can fragment past the small hard cap, and a mid-frame timeout is a fatal
	// desync that would evict this conn's CSC coverage in the very window relaxation
	// covers. Raise the cap to the relaxed timeout while relaxation is active; without
	// it EffectiveReadTimeout returns the unchanged hard cap.
	budget := pushDrainBudget(cn, cscDrainHardReadCap)
	if budget > cscDrainHardReadCap {
		// Relaxation active: confirm a RESP byte under the short cap before granting the
		// relaxed budget. Otherwise a TLS control record — socket-readable (hasData) with
		// zero RESP bytes and nothing buffered — makes the drain's Peek(1) block for the
		// full relaxed timeout, and the single-goroutine idle drainer stalls the whole
		// pass for seconds (#3989). A byte already buffered (the common case, including
		// the !hasData probe above) makes this Peek return immediately. Mirrors
		// pushDrainWithin, which the reply-path checkpoint already routes through.
		if perr := cn.WithReaderHardDeadline(cscDrainHardReadCap, func(rd *proto.Reader) error {
			_, e := rd.Peek(1)
			return e
		}); perr != nil {
			if isTimeout, hasFlag := isTimeoutError(perr); isTimeout && hasFlag {
				return false, nil // no frame began within the short cap — benign
			}
			return false, perr
		}
	}
	err = cn.WithReaderHardDeadline(budget, func(rd *proto.Reader) error {
		if processor, ok := c.pushProcessor.(*push.Processor); ok {
			return processor.ProcessPendingNotificationsBuffered(
				context.Background(), handlerCtx, rd,
			)
		}
		return c.pushProcessor.ProcessPendingNotifications(context.Background(), handlerCtx, rd)
	})
	if err != nil {
		// The built-in processor surfaces mid-frame ReadReply errors (a benign
		// boundary peek timeout returns nil). allowTimeout=false: such an error
		// means bytes were consumed mid-frame, leaving the conn desynced —
		// re-pooling would corrupt the next command's reply, so remove it.
		if _, builtin := c.pushProcessor.(*push.Processor); builtin {
			if isBadConn(err, false, c.opt.Addr) {
				return true, err // fatal read/protocol/connection error — remove the conn
			}
			return true, nil
		}
		// A CUSTOM processor's error contract is unknown: it may have consumed
		// part of a frame before failing, and a mid-frame reader silently
		// corrupts the next command's reply. The conn is idle and held solely
		// by the drainer, so the safe default — removal — costs one reconnect;
		// persistent failures are damped by the drainer (cscDrainCustomErrCap).
		internal.Logger.Printf(context.Background(), "csc: drain: custom push processor error (removing conn): %v", err)
		return true, err
	}
	// The processor ran successfully. This is stronger evidence than a clean
	// connection on which it was never invoked, and prevents successful TLS-
	// buffered drains from being counted as if failures were consecutive.
	return true, nil
}

// processPendingPushNotificationWithReader processes all pending push notifications on a connection
// This method should be called by the client in WithReader before reading the reply
func (c *baseClient) processPendingPushNotificationWithReader(ctx context.Context, cn *pool.Conn, rd *proto.Reader) error {
	// if we have the reader, we don't need to check for data on the socket, we are waiting
	// for either a reply or a push notification, so we can block until we get a reply or reach the timeout
	if c.opt.Protocol != 3 || c.pushProcessor == nil {
		return nil
	}

	// Create handler context with client, connection pool, and connection information
	handlerCtx := c.pushNotificationHandlerContext(cn)
	return c.pushProcessor.ProcessPendingNotifications(ctx, handlerCtx, rd)
}

// pushNotificationHandlerContext creates a handler context for push notification processing
func (c *baseClient) pushNotificationHandlerContext(cn *pool.Conn) push.NotificationHandlerContext {
	// Report the pool that actually owns cn, not always the main pool: with a
	// dedicated pipeline pool (now created for default clients), a notification
	// received while running an ordinary pipeline arrives on a pipeline-owned
	// connection, and a handler that inspects or operates on ConnPool must target
	// that pool — otherwise it modifies the main pool while the real pipeline
	// connection is left untouched. poolForConn derefs cn, so guard nil.
	connPool := pool.Pooler(c.connPool)
	if cn != nil {
		connPool = c.poolForConn(cn)
	}
	return push.NotificationHandlerContext{
		Client:   c,
		ConnPool: connPool,
		Conn:     cn, // Wrap in adapter for easier interface access
	}
}
