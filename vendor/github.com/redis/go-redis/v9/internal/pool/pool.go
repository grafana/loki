package pool

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"weak"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/proto"
)

// Connection close reason constants for metrics.
// These are used as the "reason" parameter in CloseConn() calls.
const (
	// CloseReasonStale indicates the connection was closed because it exceeded
	// the idle timeout or max lifetime.
	CloseReasonStale = "stale"

	// CloseReasonHookError indicates the connection was closed due to an error
	// in a pool hook (OnGet or OnPut).
	CloseReasonHookError = "hook_error"

	// CloseReasonAuthError indicates the connection was closed due to an
	// authentication error during re-authentication.
	CloseReasonAuthError = "auth_error"

	// CloseReasonTest is used in tests when closing connections.
	CloseReasonTest = "test"

	// CloseReasonFailover indicates the connection was closed due to a failover event.
	CloseReasonFailover = "failover"

	// CloseReasonMaintNotificationsDisabled indicates the connection enabled
	// maintenance notifications, but the client later downgraded and disabled
	// maintenance notification handling for the pool.
	CloseReasonMaintNotificationsDisabled = "maintnotifications_disabled"
)

// Metric state constants for connection state tracking.
// These represent the logical state of a connection from a metrics perspective,
// not the internal state machine state (ConnState).
const (
	// MetricStateIdle indicates the connection is idle in the pool,
	// ready to be acquired.
	MetricStateIdle = "idle"

	// MetricStateUsed indicates the connection is currently being used
	// by a client operation.
	MetricStateUsed = "used"
)

var (
	// ErrClosed performs any operation on the closed client will return this error.
	ErrClosed = errors.New("redis: client is closed")

	// ErrPoolExhausted is returned from a pool connection method
	// when the maximum number of database connections in the pool has been reached.
	ErrPoolExhausted = errors.New("redis: connection pool exhausted")

	// ErrPoolTimeout timed out waiting to get a connection from the connection pool.
	ErrPoolTimeout = errors.New("redis: connection pool timeout")

	// ErrPoolTryFull is the result of TryGet (the non-waiting acquire) when the
	// pool has no free turn now. It is not a timeout, because nothing waited. A
	// caller with a fallback uses it to spill at once. The pipeline pool spills to
	// the main pool this way. ErrPoolTryFull is different from ErrPoolTimeout on
	// purpose. Thus getConn does not record a false pool timeout (Stats.Timeouts or
	// POOL_TIMEOUT) for a non-wait. A real wait still returns ErrPoolTimeout.
	ErrPoolTryFull = errors.New("redis: connection pool has no free turn")

	// ErrConnUnusableTimeout is returned when a connection is not usable and we timed out trying to mark it as unusable.
	ErrConnUnusableTimeout = errors.New("redis: timed out trying to mark connection as unusable")

	// errHookRequestedRemoval is returned when a hook requests connection removal.
	errHookRequestedRemoval = errors.New("hook requested removal")

	// errConnNotPooled is returned when trying to return a non-pooled connection to the pool.
	errConnNotPooled = errors.New("connection not pooled")

	// errConnEvictedIdle is passed to OnRemove hooks when a pooled connection is evicted on
	// Put because the idle pool is already at MaxIdleConns.
	errConnEvictedIdle = errors.New("connection evicted: idle pool at capacity")

	// livePoolsMu protects the live pools on the global metric table (GlobalPools).
	// Pools built with their own Options.Metrics are never in these sets. The sets
	// hold weak pointers, so a pool dropped without Close can still be collected.
	livePoolsMu     sync.Mutex
	liveConnPools   = make(map[weak.Pointer[ConnPool]]struct{})
	livePubSubPools = make(map[weak.Pointer[PubSubPool]]struct{})

	// errPanicInDial is returned when a panic occurs in the dial function.
	errPanicInQueuedNewConn = errors.New("panic in queuedNewConn")

	// popAttempts is the maximum number of attempts to find a usable connection
	// when popping from the idle connection pool. This handles cases where connections
	// are temporarily marked as unusable (e.g., during maintenanceNotifications upgrades or network issues).
	// Value of 50 provides sufficient resilience without excessive overhead.
	// This is capped by the idle connection count, so we won't loop excessively.
	popAttempts = 50

	// getAttempts is the maximum number of attempts to get a connection that passes
	// hook validation (e.g., maintenanceNotifications upgrade hooks). This protects against race conditions
	// where hooks might temporarily reject connections during cluster transitions.
	// Value of 3 balances resilience with performance - most hook rejections resolve quickly.
	getAttempts = 3

	minTime      = time.Unix(-2208988800, 0) // Jan 1, 1900
	maxTime      = minTime.Add(1<<63 - 1)
	noExpiration = maxTime
)

// MetricCallbacks holds all metric callback functions.
// Use SetAllMetricCallbacks to register all callbacks atomically.
type MetricCallbacks struct {
	// ConnectionCreateTime is called when a new connection is created
	ConnectionCreateTime func(ctx context.Context, duration time.Duration, cn *Conn)

	// ConnectionRelaxedTimeout is called when connection timeout is relaxed/unrelaxed
	// delta: +1 for relaxed, -1 for unrelaxed
	ConnectionRelaxedTimeout func(ctx context.Context, delta int, cn *Conn, poolName, notificationType string)

	// ConnectionHandoff is called when a connection is handed off to another node
	ConnectionHandoff func(ctx context.Context, cn *Conn, poolName string)

	// Error is called when an error occurs
	Error func(ctx context.Context, errorType string, cn *Conn, statusCode string, isInternal bool, retryAttempts int)

	// MaintenanceNotification is called when a maintenance notification is received
	MaintenanceNotification func(ctx context.Context, cn *Conn, notificationType string)

	// ConnectionWaitTime is called to record time spent waiting for a connection
	ConnectionWaitTime func(ctx context.Context, duration time.Duration, cn *Conn)

	// ConnectionClosed is called when a connection is closed
	ConnectionClosed func(ctx context.Context, cn *Conn, reason string, err error)

	// PendingRequests is called when pending requests count changes (UpDownCounter)
	// delta: +1 when request starts waiting, -1 when request stops waiting
	// poolName is passed explicitly because we may not have a connection yet when request starts
	PendingRequests func(ctx context.Context, delta int, cn *Conn, poolName string)
}

// SetAllMetricCallbacks sets the global metric callbacks atomically. Pools built
// with their own Options.Metrics are not affected.
// Pass nil to clear all callbacks (disable metrics).
//
// Note on thread safety: After returning, there is a small window where
// a concurrent caller may still use the old callback table. This is
// acceptable for metrics - at most one event may go to the old recorder
// or be missed during the transition. The callbacks themselves are immutable
// function pointers, so calling an "old" callback is safe.
func SetAllMetricCallbacks(callbacks *MetricCallbacks) {
	table := noMetrics
	if callbacks != nil {
		cp := *callbacks
		table = &cp
	}
	globalMetrics.Store(table)
}

// GlobalPools returns the live pools that use the global metric table, so a
// recorder installed after they were created can still observe them.
func GlobalPools() ([]*ConnPool, []*PubSubPool) {
	livePoolsMu.Lock()
	defer livePoolsMu.Unlock()
	connPools := make([]*ConnPool, 0, len(liveConnPools))
	for wp := range liveConnPools {
		if p := wp.Value(); p != nil {
			connPools = append(connPools, p)
		}
	}
	pubSubPools := make([]*PubSubPool, 0, len(livePubSubPools))
	for wp := range livePubSubPools {
		if p := wp.Value(); p != nil {
			pubSubPools = append(pubSubPools, p)
		}
	}
	return connPools, pubSubPools
}

var (
	// noMetrics is the table with no callbacks set.
	noMetrics = &MetricCallbacks{}
	// globalMetrics is the table used by pools built without Options.Metrics.
	globalMetrics atomic.Pointer[MetricCallbacks]
)

func init() { globalMetrics.Store(noMetrics) }

// resolveMetrics returns own, or the global table when own is nil. Never nil.
func resolveMetrics(own *MetricCallbacks) *MetricCallbacks {
	if own != nil {
		return own
	}
	return globalMetrics.Load()
}

func (p *ConnPool) metrics() *MetricCallbacks { return resolveMetrics(p.cfg.Metrics) }

// MetricsOf returns the metric callbacks of the pool cn was dialed by: the
// pool's own Options.Metrics, or the global table. Never nil.
func MetricsOf(cn *Conn) *MetricCallbacks { return resolveMetrics(cn.metrics) }

// Stats contains pool state information and accumulated stats.
//
// TODO(cxl): the uint32/int64 fields below will be changed to atomic value
// types (atomic.Uint32/atomic.Int64) in v10, which is a breaking API change.
type Stats struct {
	Hits           uint32 // number of times free connection was found in the pool
	Misses         uint32 // number of times free connection was NOT found in the pool
	Timeouts       uint32 // number of times a wait timeout occurred
	WaitCount      uint32 // number of times a connection was waited
	Unusable       uint32 // number of times a connection was found to be unusable
	WaitDurationNs int64  // total time spent for waiting a connection in nanoseconds

	TotalConns      uint32 // number of total connections in the pool
	IdleConns       uint32 // number of idle connections in the pool
	StaleConns      uint32 // number of stale connections removed from the pool
	PendingRequests uint32 // number of pending requests waiting for a connection

	PubSubStats PubSubStats

	// PipelineStats holds the stats of the separate pipeline connection pool
	// when one is configured (PipelineReadBufferSize/PipelineWriteBufferSize).
	// nil when pipelines share the main pool.
	PipelineStats *Stats
}

type ConnRetirer interface {
	RetireConns(ctx context.Context, conns []*Conn, reason string)
}

type Pooler interface {
	NewConn(context.Context) (*Conn, error)
	CloseConn(ctx context.Context, cn *Conn, reason string, fromState string) error

	Get(context.Context) (*Conn, error)
	Put(context.Context, *Conn)
	Remove(context.Context, *Conn, error)

	Len() int
	IdleLen() int
	Stats() *Stats

	// Size returns the maximum pool size (capacity).
	// This is used by the streaming credentials manager to size the re-auth worker pool.
	Size() int

	AddPoolHook(hook PoolHook)
	RemovePoolHook(hook PoolHook)

	// RemoveWithoutTurn removes a connection from the pool without freeing a turn.
	// This should be used when removing a connection from a context that didn't acquire
	// a turn via Get() (e.g., background workers, cleanup tasks).
	// For normal removal after Get(), use Remove() instead.
	RemoveWithoutTurn(context.Context, *Conn, error)

	Close() error
}

type Options struct {
	Dialer          func(context.Context) (net.Conn, error)
	ReadBufferSize  int
	WriteBufferSize int

	PoolFIFO                    bool
	PoolSize                    int32
	MaxConcurrentDials          int
	DialTimeout                 time.Duration
	PoolTimeout                 time.Duration
	MinIdleConns                int32
	MaxIdleConns                int32
	MaxActiveConns              int32
	ConnMaxIdleTime             time.Duration
	ConnMaxLifetime             time.Duration
	ConnMaxLifetimeJitter       time.Duration
	ConnMaxLifetimeReapInterval time.Duration
	PushNotificationsEnabled    bool

	// DialerRetries is the maximum number of retry attempts when dialing fails.
	// Default: 5
	DialerRetries int

	// DialerRetryTimeout is the backoff duration between retry attempts.
	// Default: 100ms
	DialerRetryTimeout time.Duration

	// DialerRetryBackoff controls the delay between dial retry attempts.
	// If nil, dial retry backoff is constant and equals DialerRetryTimeout (default: 100ms).
	DialerRetryBackoff func(attempt int) time.Duration

	// Name is a unique identifier for this pool, used in metrics.
	// Format: addr_uniqueID (e.g., "localhost:6379_a1b2c3d4")
	Name string

	// Metrics is this pool's metric callback table. nil means the global table
	// set by SetAllMetricCallbacks. A pool with its own table is bound to it
	// for life and is not recounted when the global table changes.
	Metrics *MetricCallbacks
}

type lastDialErrorWrap struct {
	err error
}

type ConnPool struct {
	cfg *Options

	dialErrorsNum atomic.Uint32
	lastDialError atomic.Value

	dialsInProgress chan struct{}
	dialsQueue      *wantConnQueue
	closedCh        chan struct{}

	// reaperDone is closed when the lifetime reaper goroutine exits. Nil when
	// no reaper was started.
	reaperDone chan struct{}
	// Fast semaphore for connection limiting with eventual fairness
	// Uses fast path optimization to avoid timer allocation when tokens are available
	semaphore *internal.FastSemaphore

	connsMu   sync.Mutex
	conns     map[uint64]*Conn
	idleConns []*Conn

	// pendingConns reserves MaxActiveConns capacity before dialing. Together
	// with conns it counts both pooled and dedicated connections, under connsMu.
	pendingConns int

	poolSize            atomic.Int32
	idleConnsLen        atomic.Int32
	idleCheckInProgress atomic.Bool
	idleCheckNeeded     atomic.Bool

	// minIdleFloor is a runtime lower bound on cfg.MinIdleConns, raised by
	// RaiseMinIdleConns (the full-duplex engine keeps a warm spare). 0 = none.
	minIdleFloor atomic.Int32

	stats          Stats
	waitDurationNs atomic.Int64

	// pending packs the Gets waiting for a connection (low 32 bits) and the
	// Gets started so far (high 32 bits, wraps), so a Get pays one atomic add
	// for both. Recorders read it at collection time (PendingRequests).
	pending atomic.Uint64

	_closed atomic.Uint32

	// Pool hooks manager. atomic.Pointer keeps hot-path reads (Get/Put)
	// lock-free; hookMu serializes Add/RemovePoolHook's read-clone-store so
	// concurrent mutators (e.g. maintnotifications and CSC) can't lose an update.
	hookManager atomic.Pointer[PoolHookManager]
	hookMu      sync.Mutex

	// drainMu/drainDone coordinate the CSC drainer's temporary idle-connection
	// claim with Get. The normal semaphore retains its PoolSize capacity (and
	// therefore the established MaxActiveConns/ErrPoolExhausted behavior); a Get
	// that finds the idle list empty only because the drainer borrowed a conn
	// waits for that short claim to finish instead of opening an overflow conn.
	drainMu         sync.Mutex
	drainDone       chan struct{}
	drainBorrowed   int
	drainGeneration atomic.Uint64
}

var _ Pooler = (*ConnPool)(nil)

func NewConnPool(opt *Options) *ConnPool {
	p := &ConnPool{
		cfg:             opt,
		semaphore:       internal.NewFastSemaphore(opt.PoolSize),
		conns:           make(map[uint64]*Conn),
		closedCh:        make(chan struct{}),
		dialsInProgress: make(chan struct{}, opt.MaxConcurrentDials),
		dialsQueue:      newWantConnQueue(),
		idleConns:       make([]*Conn, 0, opt.PoolSize),
	}

	if opt.Metrics == nil {
		// Only pools on the global table are listed by GlobalPools.
		wp := weak.Make(p)
		livePoolsMu.Lock()
		liveConnPools[wp] = struct{}{}
		livePoolsMu.Unlock()
		// Drops the entry for a pool that is collected without Close.
		runtime.AddCleanup(p, func(wp weak.Pointer[ConnPool]) {
			livePoolsMu.Lock()
			delete(liveConnPools, wp)
			livePoolsMu.Unlock()
		}, wp)
	}

	// Only create MinIdleConns if explicitly requested (> 0)
	// This avoids creating connections during pool initialization for tests
	if opt.MinIdleConns > 0 {
		p.connsMu.Lock()
		p.checkMinIdleConns()
		p.connsMu.Unlock()
	}

	p.startLifetimeReaper()

	return p
}

// initializeHooks sets up the pool hooks system.
func (p *ConnPool) initializeHooks() {
	manager := NewPoolHookManager()
	p.hookManager.Store(manager)
}

// AddPoolHook adds a pool hook to the pool.
func (p *ConnPool) AddPoolHook(hook PoolHook) {
	// Serialize so a concurrent Add/Remove can't clobber this change.
	p.hookMu.Lock()
	defer p.hookMu.Unlock()

	manager := p.hookManager.Load()
	if manager == nil {
		p.initializeHooks()
		manager = p.hookManager.Load()
	}

	// Create new manager with added hook
	newManager := manager.Clone()
	newManager.AddHook(hook)

	// Atomically swap to new manager (hot-path readers load lock-free)
	p.hookManager.Store(newManager)
}

// SupportsPoolHooks reports that AddPoolHook and RemovePoolHook are functional.
// Pooler adapters with no-op hook methods intentionally do not expose this
// optional capability.
func (p *ConnPool) SupportsPoolHooks() bool {
	return true
}

// RemovePoolHook removes a pool hook from the pool.
func (p *ConnPool) RemovePoolHook(hook PoolHook) {
	p.hookMu.Lock()
	defer p.hookMu.Unlock()

	manager := p.hookManager.Load()
	if manager != nil {
		// Create new manager with removed hook
		newManager := manager.Clone()
		newManager.RemoveHook(hook)

		// Atomically swap to new manager
		p.hookManager.Store(newManager)
	}
}

// minIdleConns returns the effective minimum idle connections: the configured
// MinIdleConns, or the runtime floor if higher.
func (p *ConnPool) minIdleConns() int32 {
	if f := p.minIdleFloor.Load(); f > p.cfg.MinIdleConns {
		return f
	}
	return p.cfg.MinIdleConns
}

// RaiseMinIdleConns raises the pool's minimum idle connections to at least n
// and starts filling it in the background. It never lowers the configured
// MinIdleConns or an earlier raise. Like MinIdleConns, the idle connections
// count against PoolSize and are dialed but not initialized: init runs on
// first Get. A raised pool also keeps its spare fresh: see refreshStaleSpare.
func (p *ConnPool) RaiseMinIdleConns(n int32) {
	for {
		cur := p.minIdleFloor.Load()
		if n <= cur {
			return
		}
		if p.minIdleFloor.CompareAndSwap(cur, n) {
			break
		}
	}
	if p.closed() {
		return
	}
	p.checkMinIdleConns()
}

// refreshStaleSpare replaces the oldest idle connection once it is half-way to
// ConnMaxIdleTime or to the end of its ConnMaxLifetime, in a pool whose min-idle
// floor was raised (RaiseMinIdleConns, the full-duplex warm spare). Under LIFO
// such a spare is never leased, so it would age out and be discarded by the
// very lease it exists for, which then dials. Get runs it only after the lease
// has its healthy connection, so it never closes this lease's connection or its
// fallback. O(1): under LIFO the oldest idle connection is idleConns[0].
func (p *ConnPool) refreshStaleSpare(ctx context.Context, nowNs int64) {
	if p.minIdleFloor.Load() == 0 || p.cfg.PoolFIFO ||
		(p.cfg.ConnMaxIdleTime <= 0 && p.cfg.ConnMaxLifetime <= 0) {
		return
	}
	p.connsMu.Lock()
	if len(p.idleConns) == 0 {
		p.connsMu.Unlock()
		return
	}
	oldest := p.idleConns[0]
	if !p.spareAging(oldest, nowNs) || !oldest.TryAcquire() {
		p.connsMu.Unlock()
		return
	}
	removed := p.removeConn(oldest)
	p.connsMu.Unlock()

	if hookManager := p.hookManager.Load(); hookManager != nil {
		hookManager.ProcessOnRemove(ctx, oldest, errors.New(CloseReasonStale))
	}
	if removed {
		p.recordConnectionClosed(ctx, oldest, CloseReasonStale)
	}
	_ = p.closeConn(oldest)
	p.checkMinIdleConns()
}

// spareAging reports whether cn is half-way to ConnMaxIdleTime or has less than
// half of its ConnMaxLifetime left, so a refresh now lands before isHealthyConn
// would reject it.
func (p *ConnPool) spareAging(cn *Conn, nowNs int64) bool {
	if idle := p.cfg.ConnMaxIdleTime; idle > 0 && nowNs-cn.UsedAtNs() >= int64(idle/2) {
		return true
	}
	if life := p.cfg.ConnMaxLifetime; life > 0 && cn.expiresAt.UnixNano()-nowNs < int64(life/2) {
		return true
	}
	return false
}

func (p *ConnPool) checkMinIdleConns() {
	// If a check is already in progress, publish the request before deciding
	// whether it will handle it or we need to take over.
	if !p.idleCheckInProgress.CompareAndSwap(false, true) {
		p.idleCheckNeeded.Store(true)
		// The previous checker may have exited before our store. Retry
		// ownership after publishing so the request always has a consumer.
		if !p.idleCheckInProgress.CompareAndSwap(false, true) {
			return
		}
	}

	if p.minIdleConns() == 0 {
		p.idleCheckInProgress.Store(false)
		// A RaiseMinIdleConns between the read above and the store may have
		// found the check in progress and only set idleCheckNeeded: run again
		// so the new floor is filled now, not on a later Get.
		if p.idleCheckNeeded.Load() && p.minIdleConns() > 0 {
			p.checkMinIdleConns()
		}
		return
	}

	// Keep checking until no more checks are needed
	// This handles the case where multiple Remove() calls happen concurrently
	for {
		// Clear the "check needed" flag before we start
		p.idleCheckNeeded.Store(false)
		// Re-read each pass: a RaiseMinIdleConns during a check only sets
		// idleCheckNeeded, so the new floor must be picked up here.
		minIdle := p.minIdleConns()

		// Only create idle connections if we haven't reached the total pool size limit
		// MinIdleConns should be a subset of PoolSize, not additional connections
		for p.poolSize.Load() < p.cfg.PoolSize && p.idleConnsLen.Load() < minIdle {
			// Prewarm is opportunistic: never hold a pool turn while waiting
			// behind dedicated dials, which do not acquire turns themselves.
			if !p.tryAcquireDialSlot() {
				break
			}
			// Try to acquire a semaphore token
			if !p.semaphore.TryAcquire() {
				p.releaseDialSlot()
				// Semaphore is full, can't create more connections right now
				// Break out of inner loop to check if we need to retry
				break
			}

			p.poolSize.Add(1)
			p.idleConnsLen.Add(1)
			go func() {
				added := false
				defer func() {
					// Continue filling a floor larger than the dial limit. Do not
					// retry failed dials here, which could spin on persistent errors.
					if added {
						p.releaseDialSlotAndCheckMinIdleConns()
					} else {
						p.releaseDialSlot()
					}
				}()
				defer func() {
					if err := recover(); err != nil {
						p.poolSize.Add(-1)
						p.idleConnsLen.Add(-1)

						p.freeTurn()
						internal.Logger.Printf(context.Background(), "addIdleConn panic: %+v", err)
					}
				}()

				err := p.addIdleConn()
				added = err == nil
				if err != nil && err != ErrClosed {
					p.poolSize.Add(-1)
					p.idleConnsLen.Add(-1)
				}
				p.freeTurn()
			}()
		}

		// If no one requested another check while we were working, we're done
		if !p.idleCheckNeeded.Load() {
			p.idleCheckInProgress.Store(false)
			// A dial completion may request another pass between the check
			// above and clearing in-progress. Do not lose that wakeup.
			if p.idleCheckNeeded.Load() {
				p.checkMinIdleConns()
			}
			return
		}

		// Otherwise, loop again to handle the new requests
	}
}

// addIdleConn is called with a dial slot already held by checkMinIdleConns.
func (p *ConnPool) addIdleConn() error {
	if err := p.reserveConn(); err != nil {
		return err
	}
	reserved := true
	defer func() {
		if reserved {
			p.cancelConnReservation()
		}
	}()
	// Do not apply DialTimeout via context here; dialConn applies DialTimeout per attempt.
	cn, err := p.dialConn(context.Background(), true)
	if err != nil {
		return err
	}

	// NOTE: Connection is in CREATED state and will be initialized by redis.go:initConn()
	// when first acquired from the pool. Do NOT transition to IDLE here - that happens
	// after initialization completes.

	p.connsMu.Lock()
	defer p.connsMu.Unlock()

	// It is not allowed to add new connections to the closed connection pool.
	if p.closed() {
		_ = cn.Close()
		return ErrClosed
	}

	p.conns[cn.GetID()] = cn
	p.pendingConns--
	reserved = false
	p.idleConns = append(p.idleConns, cn)

	return nil
}

// NewConn creates a dedicated connection outside the idle pool. It counts
// against MaxActiveConns and shares the MaxConcurrentDials gate with pooled
// connections, but does not acquire a PoolSize turn. Remove it with
// RemoveWithoutTurn when its owner is finished.
//
// NOTE: If you directly get a connection from the pool, it won't be pooled and won't support maintnotifications upgrades.
func (p *ConnPool) NewConn(ctx context.Context) (*Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := p.acquireDialSlot(ctx); err != nil {
		return nil, err
	}
	defer p.releaseDialSlotAndCheckMinIdleConns()
	return p.newConn(ctx, false)
}

// acquireDialSlot shares queuedNewConn's dial gate without taking a pool turn.
func (p *ConnPool) acquireDialSlot(ctx context.Context) error {
	if p.closed() {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if cap(p.dialsInProgress) == 0 {
		return nil
	}
	select {
	case p.dialsInProgress <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-p.closedCh:
		return ErrClosed
	}
}

func (p *ConnPool) tryAcquireDialSlot() bool {
	if p.closed() {
		return false
	}
	if cap(p.dialsInProgress) == 0 {
		return true
	}
	select {
	case p.dialsInProgress <- struct{}{}:
		return true
	default:
		return false
	}
}

func (p *ConnPool) releaseDialSlot() {
	if cap(p.dialsInProgress) > 0 {
		<-p.dialsInProgress
	}
}

// releaseDialSlotAndCheckMinIdleConns replays any refill skipped while a
// foreground dial held the last permit. Rechecking unconditionally avoids a
// race between recording a skipped check and the concurrent permit release.
func (p *ConnPool) releaseDialSlotAndCheckMinIdleConns() {
	p.releaseDialSlot()
	p.checkMinIdleConns()
}

func (p *ConnPool) reserveConn() error {
	p.connsMu.Lock()
	defer p.connsMu.Unlock()
	if p.closed() {
		return ErrClosed
	}
	if max := p.cfg.MaxActiveConns; max > 0 && len(p.conns)+p.pendingConns >= int(max) {
		return ErrPoolExhausted
	}
	p.pendingConns++
	return nil
}

func (p *ConnPool) cancelConnReservation() {
	p.connsMu.Lock()
	p.pendingConns--
	p.connsMu.Unlock()
}

func (p *ConnPool) newConn(ctx context.Context, pooled bool) (*Conn, error) {
	if err := p.reserveConn(); err != nil {
		return nil, err
	}
	reserved := true
	defer func() {
		if reserved {
			p.cancelConnReservation()
		}
	}()

	// Protect against nil context due to race condition in queuedNewConn
	// where the context can be set to nil after timeout/cancellation
	if ctx == nil {
		ctx = context.Background()
	}

	// Do not apply DialTimeout via context here; dialConn applies DialTimeout per attempt.
	// We still propagate ctx so callers can cancel explicitly.
	cn, err := p.dialConn(ctx, pooled)
	if err != nil {
		return nil, err
	}

	// NOTE: Connection is in CREATED state and will be initialized by redis.go:initConn()
	// when first used. Do NOT transition to IDLE here - that happens after initialization completes.
	// The state machine flow is: CREATED → INITIALIZING (in initConn) → IDLE (after init success)

	p.connsMu.Lock()
	defer p.connsMu.Unlock()
	if p.closed() {
		_ = cn.Close()
		return nil, ErrClosed
	}
	// Check if pool was closed while we were waiting for the lock
	if p.conns == nil {
		p.conns = make(map[uint64]*Conn)
	}
	p.conns[cn.GetID()] = cn
	p.pendingConns--
	reserved = false

	if pooled {
		// If pool is full remove the cn on next Put.
		currentPoolSize := p.poolSize.Load()
		if currentPoolSize >= p.cfg.PoolSize {
			cn.pooled = false
		} else {
			p.poolSize.Add(1)
		}
	}

	return cn, nil
}

type dialExhaustedKey struct{ pool *ConnPool }

// WithDialExhaustion tracks a failed pool dial sequence without wrapping its
// error. The returned marker belongs to this connection acquisition only.
func (p *ConnPool) WithDialExhaustion(ctx context.Context, exhausted *atomic.Bool) context.Context {
	return context.WithValue(ctx, dialExhaustedKey{p}, exhausted)
}

// DialExhaustion retrieves the marker for this pool, if the caller set one.
func (p *ConnPool) DialExhaustion(ctx context.Context) *atomic.Bool {
	exhausted, _ := ctx.Value(dialExhaustedKey{p}).(*atomic.Bool)
	return exhausted
}

func (p *ConnPool) markDialExhaustion(ctx context.Context) {
	if exhausted := p.DialExhaustion(ctx); exhausted != nil {
		exhausted.Store(true)
	}
}

func (p *ConnPool) dialConn(ctx context.Context, pooled bool) (*Conn, error) {
	if p.closed() {
		return nil, ErrClosed
	}

	if p.dialErrorsNum.Load() >= uint32(p.cfg.PoolSize) {
		// The breaker rejected this request before it could start a dial
		// sequence. Keep command-level retries available while tryDial probes
		// for recovery in the background.
		return nil, p.getLastDialError()
	}

	// Record dial start time for connection creation metric
	// This will be used after handshake completes in redis.go _getConn()
	// Only call time.Now() if callback is registered to avoid overhead
	var dialStartNs int64
	if p.metrics().ConnectionCreateTime != nil {
		dialStartNs = time.Now().UnixNano()
	}

	// Retry dialing with backoff
	// Dial timeout is applied per attempt (so retries/backoff don't eat into the next
	// attempt's dial budget), while still honoring caller cancellation via ctx.
	maxRetries := p.cfg.DialerRetries
	if maxRetries <= 0 {
		maxRetries = 5 // Default value
	}

	var lastErr error
	shouldLoop := true
	// when the timeout is reached, we should stop retrying
	// but keep the lastErr to return to the caller
	// instead of a generic context deadline exceeded error
	attempt := 0
	for attempt = 0; (attempt < maxRetries) && shouldLoop; attempt++ {
		attemptCtx := ctx
		var cancel context.CancelFunc
		if p.cfg.DialTimeout > 0 {
			// Apply DialTimeout per attempt, but never extend an existing earlier deadline.
			if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > p.cfg.DialTimeout {
				attemptCtx, cancel = context.WithTimeout(ctx, p.cfg.DialTimeout)
			}
		}

		netConn, err := p.cfg.Dialer(attemptCtx)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			lastErr = err
			// Add backoff delay for retry attempts
			// (not for the first attempt, do at least one)
			// Do not sleep after the last attempt.
			if attempt+1 < maxRetries {
				backoffDuration := p.dialRetryBackoff(attempt)
				select {
				case <-ctx.Done():
					shouldLoop = false
				case <-time.After(backoffDuration):
					// Continue with retry
				}
			}
			continue
		}

		cn := NewConnWithBufferSize(netConn, p.cfg.ReadBufferSize, p.cfg.WriteBufferSize)
		cn.pooled = pooled
		// Store dial start time only if we recorded it
		if dialStartNs > 0 {
			cn.dialStartNs.Store(dialStartNs)
		}
		cn.expiresAt = p.calcConnExpiresAt()
		// Set pool name for metrics
		cn.SetPoolName(p.cfg.Name)
		cn.metrics = p.cfg.Metrics

		return cn, nil
	}

	internal.Logger.Printf(ctx, "redis: connection pool: failed to dial after %d attempts: %v", attempt, lastErr)
	// All retries failed - handle error tracking
	p.setLastDialError(lastErr)
	if p.dialErrorsNum.Add(1) == uint32(p.cfg.PoolSize) {
		go p.tryDial()
	}
	p.markDialExhaustion(ctx)
	return nil, lastErr
}

func (p *ConnPool) dialRetryBackoff(attempt int) time.Duration {
	if p.cfg.DialerRetryBackoff != nil {
		d := p.cfg.DialerRetryBackoff(attempt)
		if d < 0 {
			return 0
		}
		return d
	}

	base := p.cfg.DialerRetryTimeout
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	return base
}

// calcConnExpiresAt calculates the expiration time for a connection.
// It applies random jitter to prevent all connections from expiring simultaneously,
// avoiding the "thundering herd" problem where all connections expire at once.
// Returns noExpiration if ConnMaxLifetime is not set.
func (p *ConnPool) calcConnExpiresAt() time.Time {
	if p.cfg.ConnMaxLifetime <= 0 {
		return noExpiration
	}

	if p.cfg.ConnMaxLifetimeJitter <= 0 {
		return time.Now().Add(p.cfg.ConnMaxLifetime)
	}

	jitter := p.cfg.ConnMaxLifetimeJitter
	jitterRange := jitter.Nanoseconds() * 2
	jitterNs := rand.Int63n(jitterRange) - jitter.Nanoseconds()
	return time.Now().Add(p.cfg.ConnMaxLifetime + time.Duration(jitterNs))
}

func (p *ConnPool) tryDial() {
	for {
		if p.closed() {
			return
		}

		// Probe dialing even when dialErrorsNum is saturated. Apply DialTimeout per probe
		// attempt so custom dialers can't hang indefinitely.
		ctx := context.Background()
		var cancel context.CancelFunc
		if p.cfg.DialTimeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, p.cfg.DialTimeout)
		}

		conn, err := p.cfg.Dialer(ctx)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			p.setLastDialError(err)
			time.Sleep(time.Second)
			continue
		}

		p.dialErrorsNum.Store(0)
		_ = conn.Close()
		return
	}
}

func (p *ConnPool) setLastDialError(err error) {
	p.lastDialError.Store(&lastDialErrorWrap{err: err})
}

func (p *ConnPool) getLastDialError() error {
	err, _ := p.lastDialError.Load().(*lastDialErrorWrap)
	if err != nil {
		return err.err
	}
	return nil
}

// Get returns existed connection from the pool or creates a new one.
func (p *ConnPool) Get(ctx context.Context) (*Conn, error) {
	return p.getConn(ctx, true)
}

// TryGet returns a connection only if a pool turn is free now. A free turn is an
// open slot or room to dial a new connection. TryGet never waits out PoolTimeout
// for a full pool. It returns ErrPoolTryFull at once instead. This lets a caller
// with a fallback (the pipeline pool) spill without a stall. ErrPoolTryFull is
// different from ErrPoolTimeout: no wait happened, so getConn does not count the
// spill as a pool timeout. Re-authentication at the hard MaxActiveConns limit
// returns ErrPoolTryFull, while exhaustion without re-auth returns
// ErrPoolExhausted. TryGet dials under an acquired turn when capacity remains.
func (p *ConnPool) TryGet(ctx context.Context) (*Conn, error) {
	return p.getConn(ctx, false)
}

// usableIdlePollInterval bounds the readiness check by both the pool budget
// and the caller's deadline, without changing which timer expires the wait.
func usableIdlePollInterval(ctx context.Context, poolRemaining time.Duration) time.Duration {
	pollBudget := poolRemaining
	if ctxDeadline, ok := ctx.Deadline(); ok {
		pollBudget = min(pollBudget, time.Until(ctxDeadline))
	}
	if pollBudget <= 0 {
		return 0
	}
	interval := min(10*time.Millisecond, pollBudget/4)
	if interval <= 0 {
		return pollBudget
	}
	return interval
}

// waitForUsableIdle polls the lightweight connection state rather than
// repeatedly running popIdle (which rearranges the idle slice). A maintenance
// worker updates state without calling Put, so there is no pool queue event to
// wait for here. Waiting stops as soon as the hard cap has room to dial.
func (p *ConnPool) waitForUsableIdle(ctx context.Context, deadline time.Time, hookRejected bool) error {
	isReady := func() bool {
		p.connsMu.Lock()
		defer p.connsMu.Unlock()
		if p.cfg.MaxActiveConns == 0 || len(p.conns)+p.pendingConns < int(p.cfg.MaxActiveConns) {
			return true
		}
		// Return to acquisition when nothing can become ready without a
		// fresh dial. Handoff-owned entries are not candidates for this wait.
		hasReauth := false
		for _, cn := range p.idleConns {
			state := cn.GetStateMachine().GetState()
			if state == StateIdle || state == StateCreated {
				return true
			}
			if (state == StateUnusable || state == StateInUse) && cn.GetHandoffEndpoint() == "" {
				hasReauth = true // AUTH via SingleConnPool briefly enters StateInUse
			}
		}
		return !hasReauth
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.closed() {
		return ErrClosed
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return ErrPoolTimeout
	}
	// An OnGet hook can reject StateIdle connections while re-authentication
	// is scheduled but not yet running. After such a rejection, wait for at
	// least one tick rather than immediately retrying the same idle entry.
	if !hookRejected && isReady() {
		return nil
	}
	// Poll promptly when the caller has a shorter deadline than PoolTimeout.
	// The pool timer still owns the pool budget; ctx.Done owns the caller's.
	interval := usableIdlePollInterval(ctx, remaining)
	if interval <= 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		return context.DeadlineExceeded
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	timer := time.NewTimer(remaining)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.closedCh:
			return ErrClosed
		case <-timer.C:
			if err := ctx.Err(); err != nil {
				return err
			}
			if p.closed() {
				return ErrClosed
			}
			return ErrPoolTimeout
		case <-ticker.C:
			if err := ctx.Err(); err != nil {
				return err
			}
			if p.closed() {
				return ErrClosed
			}
			if isReady() {
				return nil
			}
		}
	}
}

// getConn returns a connection from the pool. When wait is false it does not block
// for a turn (see waitTurn / TryGet).
func (p *ConnPool) getConn(ctx context.Context, wait bool) (cn *Conn, err error) {
	if p.closed() {
		return nil, ErrClosed
	}

	// Track pending requests in pool stats
	p.pending.Add(pendingStart)
	// Record pending request increment (UpDownCounter)
	// Pass pool name explicitly since we don't have a connection yet
	poolName := p.cfg.Name
	// Read once so the -1 goes to the same callback as the +1, even if the recorder changes while waiting.
	pendingCb := p.metrics().PendingRequests
	if pendingCb != nil {
		pendingCb(ctx, 1, nil, poolName)
	}
	defer func() {
		if err != nil {
			// Failed to get connection, decrement pending requests
			p.pending.Add(pendingDone)
			// Record pending request decrement on failure
			if pendingCb != nil {
				pendingCb(ctx, -1, nil, poolName)
			}
		}
		if err == ErrPoolTimeout {
			atomic.AddUint32(&p.stats.Timeouts, 1)
			if cb := p.metrics().Error; cb != nil {
				cb(ctx, "POOL_TIMEOUT", nil, "POOL_TIMEOUT", true, 0)
			}
		}
	}()

	// PoolTimeout is one budget for the pool turn and temporary maintenance waits.
	waitStart := time.Now()
	poolDeadline := waitStart.Add(p.cfg.PoolTimeout)

	// Connection wait time measures only semaphore acquisition.
	var waitDuration time.Duration
	waitTimeCallback := p.metrics().ConnectionWaitTime
	if err = p.waitTurn(ctx, wait); err != nil {
		return nil, err
	}
	if waitTimeCallback != nil {
		waitDuration = time.Since(waitStart)
	}

	// Use cached time for health checks (max 50ms staleness is acceptable)
	nowNs := getCachedTimeNs()

	// Lock-free atomic read - no mutex overhead!
	hookManager := p.hookManager.Load()

	// idleRechecked bounds the hard-cap idle recheck below to one extra
	// scan per Get: it closes the window where a connection finishes
	// re-authentication between the popIdle scan and the capacity check,
	// without turning that recheck into a spin loop.
	idleRechecked := false

retryIdle:
	if err := ctx.Err(); err != nil {
		p.freeTurn()
		return nil, err
	}
	if p.closed() {
		p.freeTurn()
		return nil, ErrClosed
	}
	drainGeneration := p.drainGeneration.Load()
	hookRejected := false
	for attempts := 0; attempts < getAttempts; attempts++ {
		// A blocked OnGet can return after cancellation; do not reacquire
		// its requeued connection on the next attempt.
		if err := ctx.Err(); err != nil {
			p.freeTurn()
			return nil, err
		}
		p.connsMu.Lock()
		cn, err = p.popIdle()
		p.connsMu.Unlock()

		if err != nil {
			p.freeTurn()
			return nil, err
		}

		if cn == nil {
			break
		}

		if !p.isHealthyConn(cn, nowNs) {
			// Connection was already transitioned to MetricStateUsed under the lock above.
			_ = p.CloseConn(ctx, cn, CloseReasonStale, MetricStateUsed)
			continue
		}

		// Process connection using the hooks system
		// Combine error and rejection checks to reduce branches
		if hookManager != nil {
			acceptConn, hookErr := hookManager.ProcessOnGet(ctx, cn, false)
			if hookErr != nil || !acceptConn {
				if hookErr != nil {
					internal.Logger.Printf(ctx, "redis: connection pool: failed to process idle connection by hook: %v", hookErr)
					// Connection was already transitioned to MetricStateUsed under the lock above.
					_ = p.CloseConn(ctx, cn, CloseReasonHookError, MetricStateUsed)
				} else {
					hookRejected = true
					internal.Logger.Printf(ctx, "redis: connection pool: conn[%d] rejected by hook, returning to pool", cn.GetID())
					// Connection is already in MetricStateUsed (transitioned under the lock above).
					// Return connection to pool without freeing the turn that this Get() call holds.
					// putConnWithoutTurn will emit used→idle transition.
					p.putConnWithoutTurn(ctx, cn)
					cn = nil
				}
				continue
			}
		}

		atomic.AddUint32(&p.stats.Hits, 1)

		// Record wait time (use cached callback from above)
		if waitTimeCallback != nil {
			waitTimeCallback(ctx, waitDuration, cn)
		}

		// Decrement pending requests (connection acquired successfully)
		p.pending.Add(pendingDone)
		// Record pending request decrement (UpDownCounter)
		if pendingCb != nil {
			pendingCb(ctx, -1, cn, poolName)
		}

		// Only now, with this lease's connection taken and checked.
		p.refreshStaleSpare(ctx, nowNs)
		return cn, nil
	}

	// If the CSC drainer removed the only idle connection during this scan,
	// wait for that bounded maintenance claim and retry. The generation closes
	// the race where the drainer returns the connection between popIdle and this
	// check. Normal MaxActiveConns exhaustion still proceeds to newConn and
	// returns ErrPoolExhausted immediately, preserving the existing contract.
	if done, retry := p.drainerWaitState(drainGeneration); done != nil {
		if !wait {
			// TryGet must not wait out the drainer's bounded claim (up to PoolTimeout)
			// either — spill immediately, like the pool-turn and dial-permit
			// non-blocking paths, so a pipeline falls back to the main pool at once.
			p.freeTurn()
			return nil, ErrPoolTryFull
		}
		if err = p.waitForDrainer(ctx, done, poolDeadline); err != nil {
			p.freeTurn()
			return nil, err
		}
		nowNs = time.Now().UnixNano()
		goto retryIdle
	} else if retry {
		nowNs = time.Now().UnixNano()
		goto retryIdle
	}

	// Preserve the normal dial path whenever MaxActiveConns has spare room.
	// Only a hard-cap exhaustion with an idle re-auth candidate should wait
	// for that candidate to become usable instead of returning ErrPoolExhausted.
	// In particular, PoolSize is a soft limit and must not trigger this wait.
	//
	// A connection can also finish re-authentication between the popIdle
	// scan above and this check: the scan skipped it while the re-auth
	// worker had it in use, but by now it is idle and acquirable. Recheck
	// for such a connection once and retry acquisition instead of dialing
	// into the hard cap. This is not the re-auth wait below: the connection
	// needs no waiting, and treating every idle entry as a re-auth
	// candidate would add spurious waits (a hook may reject the connection
	// anyway). A hook-rejected idle entry therefore stays on the wait path.
	p.connsMu.Lock()
	capReached := p.cfg.MaxActiveConns > 0 && len(p.conns)+p.pendingConns >= int(p.cfg.MaxActiveConns)
	hasReauthIdle := false
	hasAcquirableIdle := false
	if capReached {
		for _, idle := range p.idleConns {
			state := idle.GetStateMachine().GetState()
			// OnGet can reject before the re-auth worker marks it unusable.
			// Handoff-owned entries must still use the usual dial/error path.
			if (hookRejected && state == StateIdle) ||
				((state == StateUnusable || state == StateInUse) && idle.GetHandoffEndpoint() == "") {
				hasReauthIdle = true
			}
			if !hookRejected && (state == StateIdle || state == StateCreated) {
				hasAcquirableIdle = true
			}
			if hasReauthIdle && hasAcquirableIdle {
				break
			}
		}
	}
	p.connsMu.Unlock()
	if hasAcquirableIdle && !idleRechecked {
		idleRechecked = true
		// The connection became acquirable after the cached-time
		// snapshot; check expiration against the current clock when we
		// next inspect an idle connection.
		nowNs = time.Now().UnixNano()
		goto retryIdle
	}
	if hasReauthIdle {
		if !wait {
			p.freeTurn()
			return nil, ErrPoolTryFull
		}
		if err = p.waitForUsableIdle(ctx, poolDeadline, hookRejected); err != nil {
			p.freeTurn()
			return nil, err
		}
		// Maintenance can outlast the cached-time window. Check expiration
		// against the current clock when we next inspect an idle connection.
		nowNs = time.Now().UnixNano()
		goto retryIdle
	}

	atomic.AddUint32(&p.stats.Misses, 1)

	var newcn *Conn
	newcn, err = p.queuedNewConn(ctx, wait)
	if err != nil {
		return nil, err
	}

	// Process connection using the hooks system
	// This includes the handshake (HELLO/AUTH) via initConn hook
	if hookManager != nil {
		var acceptConn bool
		acceptConn, err = hookManager.ProcessOnGet(ctx, newcn, true)
		// both errors and accept=false mean a hook rejected the connection
		// this should not happen with a new connection, but we handle it gracefully
		if err != nil || !acceptConn {
			internal.Logger.Printf(ctx, "redis: connection pool: failed to process new connection conn[%d] by hook: accept=%v, err=%v", newcn.GetID(), acceptConn, err)
			// newConn emitted +1 used; CloseConn will emit -1 used if we own the removal.
			_ = p.CloseConn(ctx, newcn, CloseReasonHookError, MetricStateUsed)
			// The delivered connection came with this Get's turn; CloseConn does not free it.
			p.freeTurn()
			return nil, err
		}

		// Record connection creation time metric when hooks are used.
		// When hookManager is set, ProcessOnGet initializes the connection (AUTH/HELLO),
		// causing IsInited()=true. This means _getConn() in redis.go will take the
		// early return path and never reach its create time recording.
		// When hookManager is nil, _getConn() handles both initialization and create time recording.
		if dialStartNs := newcn.GetDialStartNs(); newcn.IsInited() && dialStartNs > 0 {
			if cb := p.metrics().ConnectionCreateTime; cb != nil {
				duration := time.Duration(time.Now().UnixNano() - dialStartNs)
				cb(ctx, duration, newcn)
			}
		}
	}

	// newConn already emitted +1 used, so no transition needed here.

	// Record wait time (use cached callback from above)
	if waitTimeCallback != nil {
		waitTimeCallback(ctx, waitDuration, newcn)
	}

	// Decrement pending requests (connection acquired successfully)
	p.pending.Add(pendingDone)
	// Record pending request decrement (UpDownCounter)
	if pendingCb != nil {
		pendingCb(ctx, -1, newcn, poolName)
	}

	return newcn, nil
}

func (p *ConnPool) queuedNewConn(ctx context.Context, wait bool) (*Conn, error) {
	if wait {
		select {
		case p.dialsInProgress <- struct{}{}:
			// Got permission, proceed to create connection
		case <-ctx.Done():
			p.freeTurn()
			return nil, ctx.Err()
		}
	} else {
		// TryGet: MaxConcurrentDials can be below PoolSize, so a pool turn may be
		// free while every dial permit is held by an in-flight dial. A blocking send
		// here would make TryGet wait out another caller's whole dial retry sequence,
		// violating its non-blocking contract. Give up immediately (spill) instead.
		select {
		case p.dialsInProgress <- struct{}{}:
			// Got permission, proceed to create connection
		default:
			p.freeTurn()
			return nil, ErrPoolTryFull
		}
	}

	// Don't apply DialTimeout via context here; dialConn applies DialTimeout per attempt.
	base := context.Background()
	// A queued dial can lose a race to another worker that supplies an idle
	// connection. Do not let a failed losing dial mark a successful Get.
	var dialExhausted *atomic.Bool
	if p.DialExhaustion(ctx) != nil {
		dialExhausted = new(atomic.Bool)
		base = p.WithDialExhaustion(base, dialExhausted)
	}
	dialCtx, cancel := context.WithCancel(base)

	w := &wantConn{
		ctx:       dialCtx,
		cancelCtx: cancel,
		result:    make(chan wantConnResult, 1),
	}
	var err error
	defer func() {
		if err != nil {
			if cn := w.cancel(); cn != nil && p.putIdleConn(ctx, cn) {
				p.freeTurn()
			}
		}
	}()

	p.dialsQueue.discardDoneAtFront()
	p.dialsQueue.enqueue(w)

	go func(w *wantConn) {
		var freeTurnCalled bool
		// Run after panic recovery so any abandoned turn is freed before
		// the refill retries. The caller may keep a delivered conn in use.
		defer p.releaseDialSlotAndCheckMinIdleConns()
		defer func() {
			if err := recover(); err != nil {
				w.tryDeliver(nil, errPanicInQueuedNewConn)
				p.dialsQueue.discardDoneAtFront()
				if !freeTurnCalled {
					p.freeTurn()
				}
				internal.Logger.Printf(context.Background(), "queuedNewConn panic: %+v", err)
			}
		}()

		defer w.cancelCtx()

		dialCtx := w.getCtxForDial()
		cn, cnErr := p.newConn(dialCtx, true)
		if cnErr != nil {
			w.tryDeliver(nil, cnErr) // deliver error to caller, notify connection creation failed
			p.dialsQueue.discardDoneAtFront()
			p.freeTurn()
			freeTurnCalled = true
			return
		}

		delivered := w.tryDeliver(cn, cnErr)
		p.dialsQueue.discardDoneAtFront()
		if !delivered && p.putIdleConn(dialCtx, cn) {
			p.freeTurn()
			freeTurnCalled = true
		}
	}(w)

	select {
	case <-ctx.Done():
		err = ctx.Err()
		return nil, err
	case result := <-w.result:
		err = result.err
		if err != nil && dialExhausted != nil && dialExhausted.Load() {
			p.markDialExhaustion(ctx)
		}
		return result.cn, err
	}
}

// putIdleConn puts a connection back to the pool or passes it to the next waiting request.
//
// It returns true if the connection was put back to the pool,
// which means the turn needs to be freed directly by the caller,
// or false if the connection was passed to the next waiting request,
// which means the turn will be freed by the waiting goroutine after it returns.
func (p *ConnPool) putIdleConn(ctx context.Context, cn *Conn) bool {
	for {
		w, ok := p.dialsQueue.dequeue()
		if !ok {
			break
		}
		if w.tryDeliver(cn, nil) {
			return false
		}
	}

	p.connsMu.Lock()
	defer p.connsMu.Unlock()

	if p.closed() {
		// Don't close here — this connection is still in p.conns and Close()
		// will handle closing it and emitting the correct metric decrements.
		// We just skip adding it to idleConns.
		return true
	}

	p.idleConns = append(p.idleConns, cn)
	p.idleConnsLen.Add(1)

	return true
}

func (p *ConnPool) waitTurn(ctx context.Context, wait bool) error {
	// Fast path: check context first
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Fast path: try to acquire without blocking
	if p.semaphore.TryAcquire() {
		return nil
	}

	// Non-waiting acquire (TryGet): the pool is full and no turn is free now.
	// Return at once. Do not wait out PoolTimeout. The caller (the pipeline pool)
	// then spills to the main pool at once. Return ErrPoolTryFull, not
	// ErrPoolTimeout, because nothing waited. Thus getConn does not count a timeout.
	if !wait {
		return ErrPoolTryFull
	}

	// Slow path: need to wait
	start := time.Now()
	err := p.semaphore.Acquire(ctx, p.cfg.PoolTimeout, ErrPoolTimeout)
	if err != nil {
		return err
	}

	p.waitDurationNs.Add(time.Now().UnixNano() - start.UnixNano())
	atomic.AddUint32(&p.stats.WaitCount, 1)
	return nil
}

func (p *ConnPool) freeTurn() {
	p.semaphore.Release()
}

func (p *ConnPool) beginDrainerBorrow() {
	p.drainMu.Lock()
	if p.drainBorrowed == 0 {
		p.drainDone = make(chan struct{})
	}
	p.drainBorrowed++
	p.drainMu.Unlock()
}

func (p *ConnPool) endDrainerBorrow() {
	p.drainMu.Lock()
	p.drainBorrowed--
	if p.drainBorrowed == 0 {
		close(p.drainDone)
		p.drainDone = nil
		p.drainGeneration.Add(1)
	}
	p.drainMu.Unlock()
}

// drainerWaitState returns the current drain epoch's completion channel. If no
// drain is active, retry reports whether an epoch completed during the caller's
// idle scan and the idle list therefore needs to be checked again.
func (p *ConnPool) drainerWaitState(generation uint64) (done <-chan struct{}, retry bool) {
	p.drainMu.Lock()
	defer p.drainMu.Unlock()
	if p.drainBorrowed > 0 {
		return p.drainDone, false
	}
	return nil, p.drainGeneration.Load() != generation
}

func (p *ConnPool) waitForDrainer(
	ctx context.Context, done <-chan struct{}, poolDeadline time.Time,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-done:
		return nil
	default:
	}
	remaining := time.Until(poolDeadline)
	if remaining <= 0 {
		return ErrPoolTimeout
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		// Prefer a caller cancellation that raced with the pool timeout.
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrPoolTimeout
	}
}

func (p *ConnPool) popIdle() (*Conn, error) {
	if p.closed() {
		return nil, ErrClosed
	}
	defer p.checkMinIdleConns()

	n := len(p.idleConns)
	if n == 0 {
		return nil, nil
	}

	var cn *Conn
	attempts := 0

	maxAttempts := min(popAttempts, n)
	for attempts < maxAttempts {
		if len(p.idleConns) == 0 {
			return nil, nil
		}

		if p.cfg.PoolFIFO {
			cn = p.idleConns[0]
			copy(p.idleConns, p.idleConns[1:])
			p.idleConns = p.idleConns[:len(p.idleConns)-1]
		} else {
			idx := len(p.idleConns) - 1
			cn = p.idleConns[idx]
			p.idleConns = p.idleConns[:idx]
		}
		attempts++

		// Hot path optimization: try IDLE → IN_USE or CREATED → IN_USE transition
		// Using inline TryAcquire() method for better performance (avoids pointer dereference)
		if cn.TryAcquire() {
			// Successfully acquired the connection
			p.idleConnsLen.Add(-1)
			return cn, nil
		}

		// Connection is in UNUSABLE, INITIALIZING, or other state - skip it

		// Connection is not in a valid state (might be UNUSABLE for handoff/re-auth, INITIALIZING, etc.)
		// Put it back in the pool and try the next one
		if p.cfg.PoolFIFO {
			// FIFO: put at end (will be picked up last since we pop from front)
			p.idleConns = append(p.idleConns, cn)
		} else {
			// LIFO: put at beginning (will be picked up last since we pop from end)
			p.idleConns = append([]*Conn{cn}, p.idleConns...)
		}
		cn = nil
	}

	// If we exhausted all attempts without finding a usable connection, return nil
	if attempts > 1 && attempts >= maxAttempts && int32(attempts) >= p.poolSize.Load() {
		internal.Logger.Printf(context.Background(), "redis: connection pool: failed to get a usable connection after %d attempts", attempts)
		return nil, nil
	}

	return cn, nil
}

func (p *ConnPool) Put(ctx context.Context, cn *Conn) {
	p.putConn(ctx, cn, true)
}

// putConnWithoutTurn is an internal method that puts a connection back to the pool
// without freeing a turn. This is used when returning a rejected connection from
// within Get(), where the turn is still held by the Get() call.
func (p *ConnPool) putConnWithoutTurn(ctx context.Context, cn *Conn) {
	p.putConn(ctx, cn, false)
}

// putConn is the internal implementation of Put that optionally frees a turn.
func (p *ConnPool) putConn(ctx context.Context, cn *Conn, freeTurn bool) {
	// Guard against nil connection
	if cn == nil {
		internal.Logger.Printf(ctx, "putConn called with nil connection")
		if freeTurn {
			p.freeTurn()
		}
		return
	}

	// Process connection using the hooks system
	shouldPool := true
	shouldRemove := false
	var err error

	if reason := cn.CloseOnPutReason(); reason != "" {
		p.removeConnInternal(ctx, cn, errors.New(reason), freeTurn)
		return
	}

	if cn.HasBufferedData() {
		// Peek at the reply type to check if it's a push notification
		if replyType, err := cn.PeekReplyTypeSafe(); err != nil || replyType != proto.RespPush {
			// Not a push notification or error peeking, remove connection
			internal.Logger.Printf(ctx, "Conn has unread data (not push notification), removing it")
			p.removeConnInternal(ctx, cn, err, freeTurn)
			return
		}
		// It's a push notification, allow pooling (client will handle it)
	}

	// Lock-free atomic read - no mutex overhead!
	hookManager := p.hookManager.Load()

	// Read before the hooks: the maintnotifications OnPut hook can hand the
	// connection to a handoff worker, which may start at once (see below).
	ownerGen := cn.HandoffOwnerGen()

	if hookManager != nil {
		shouldPool, shouldRemove, err = hookManager.ProcessOnPut(ctx, cn)
		if err != nil {
			internal.Logger.Printf(ctx, "Connection hook error: %v", err)
			p.removeConnInternal(ctx, cn, err, freeTurn)
			return
		}
	}

	// Combine all removal checks into one - reduces branches
	if shouldRemove || !shouldPool {
		p.removeConnInternal(ctx, cn, errHookRequestedRemoval, freeTurn)
		return
	}

	if !cn.pooled {
		p.removeConnInternal(ctx, cn, errConnNotPooled, freeTurn)
		return
	}

	var shouldCloseConn bool
	var removedFromPool bool

	if p.cfg.MaxIdleConns == 0 || p.idleConnsLen.Load() < p.cfg.MaxIdleConns {
		// Hot path optimization: try fast IN_USE → IDLE transition
		// Using inline Release() method for better performance (avoids pointer dereference)
		//
		// Not when a hook handed the connection to a handoff worker: the worker
		// owns it now and may already hold it IN_USE for its init commands, so
		// Release would free the worker's IN_USE and a Get would take the
		// connection mid-init. Pool it as-is; the worker makes it IDLE when done.
		handedOff := cn.HandedOffSince(ownerGen)
		transitionedToIdle := !handedOff && cn.Release()

		// Handle unexpected state changes
		if !transitionedToIdle && !handedOff {
			// Fast path failed - hook might have changed state (e.g., to UNUSABLE for handoff)
			// Keep the state set by the hook and pool the connection anyway
			sm := cn.GetStateMachine()
			if sm == nil {
				// State machine is nil - connection is in an invalid state, remove it
				internal.Logger.Printf(ctx, "conn[%d] has nil state machine, removing it", cn.GetID())
				p.removeConnInternal(ctx, cn, errConnNotPooled, freeTurn)
				return
			}
			currentState := sm.GetState()
			switch currentState {
			case StateUnusable:
				// expected state, don't log it
			case StateClosed:
				internal.Logger.Printf(ctx, "Unexpected conn[%d] state changed by hook to %v, closing it", cn.GetID(), currentState)
				if hookManager != nil {
					hookManager.ProcessOnRemove(ctx, cn, errHookRequestedRemoval)
				}
				shouldCloseConn = true
				removedFromPool = p.removeConnWithLock(cn)
			default:
				// Pool as-is
				internal.Logger.Printf(ctx, "Unexpected conn[%d] state changed by hook to %v, pooling as-is", cn.GetID(), currentState)
			}
		}

		// unusable conns are expected to become usable at some point (background process is reconnecting them)
		// put them at the opposite end of the queue
		// Check even if Release() succeeded: it notifies waiters, which may have
		// already moved the conn out of IDLE (e.g. re-auth to UNUSABLE).
		// Skipped when the StateClosed case above already removed the conn.
		if !shouldCloseConn && !cn.IsUsable() {
			p.connsMu.Lock()
			if reason := cn.CloseOnPutReason(); reason != "" {
				// Marked for retirement between the check at the top of this
				// function and here: RetireConns saw the connection as in use
				// (not yet in idleConns) and marked it instead of removing it,
				// so the removal is ours. Re-checked under connsMu, the lock
				// RetireConns marks under, so a marked connection is never
				// idled and left in the pool until its next put.
				p.connsMu.Unlock()
				p.removeConnInternal(ctx, cn, errors.New(reason), freeTurn)
				return
			}
			// Check if Close() already removed this connection from p.conns.
			// If so, skip the append and metrics — Close() already accounted for it.
			if _, inPool := p.conns[cn.GetID()]; inPool {
				if p.cfg.PoolFIFO {
					p.idleConns = append(p.idleConns, cn)
				} else {
					p.idleConns = append([]*Conn{cn}, p.idleConns...)
				}
				// Count it before unlocking so a reaper pass that removes this
				// conn right after cannot decrement before the increment.
				p.idleConnsLen.Add(1)
				p.connsMu.Unlock()
			} else {
				shouldCloseConn = true
				p.connsMu.Unlock()
			}
		} else if !shouldCloseConn {
			p.connsMu.Lock()
			if reason := cn.CloseOnPutReason(); reason != "" {
				// Same late-retirement check as above (see there).
				p.connsMu.Unlock()
				p.removeConnInternal(ctx, cn, errors.New(reason), freeTurn)
				return
			}
			if _, inPool := p.conns[cn.GetID()]; inPool {
				p.idleConns = append(p.idleConns, cn)
				// Count it before unlocking so a reaper pass that removes this
				// conn right after cannot decrement before the increment.
				p.idleConnsLen.Add(1)
				p.connsMu.Unlock()
			} else {
				shouldCloseConn = true
				p.connsMu.Unlock()
			}
		}
	} else {
		shouldCloseConn = true
		if hookManager != nil {
			hookManager.ProcessOnRemove(ctx, cn, errConnEvictedIdle)
		}
		removedFromPool = p.removeConnWithLock(cn)
	}

	if freeTurn {
		p.freeTurn()
	}

	if shouldCloseConn {
		// Only emit connection closed if we actually owned the removal.
		// If removedFromPool is false, Close() already emitted connectionClosed for this conn.
		if removedFromPool {
			if cb := p.metrics().ConnectionClosed; cb != nil {
				reason := "conn_pool_close"
				if r := cn.closeReason.Load(); r != "" {
					reason = r
				}
				cb(ctx, cn, reason, nil)
			}
		}
		_ = p.closeConn(cn)
	}

	cn.SetLastPutAtNs(getCachedTimeNs())
}

func (p *ConnPool) Remove(ctx context.Context, cn *Conn, reason error) {
	p.removeConnInternal(ctx, cn, reason, true)
}

// RemoveWithoutTurn removes a connection from the pool without freeing a turn.
// This should be used when removing a connection from a context that didn't acquire
// a turn via Get() (e.g., background workers, cleanup tasks).
// For normal removal after Get(), use Remove() instead.
func (p *ConnPool) RemoveWithoutTurn(ctx context.Context, cn *Conn, reason error) {
	p.removeConnInternal(ctx, cn, reason, false)
}

// removeConnInternal is the internal implementation of Remove that optionally frees a turn.
func (p *ConnPool) removeConnInternal(ctx context.Context, cn *Conn, reason error, freeTurn bool) {
	// Lock-free atomic read - no mutex overhead!
	hookManager := p.hookManager.Load()

	if hookManager != nil {
		hookManager.ProcessOnRemove(ctx, cn, reason)
	}

	removed := p.removeConnWithLock(cn)

	if freeTurn {
		p.freeTurn()
	}

	// Only emit connection closed if we actually owned the removal.
	// If removed is false, Close() already emitted connectionClosed for this conn.
	if removed {
		if cb := p.metrics().ConnectionClosed; cb != nil {
			reasonStr := "unknown"
			if reason != nil {
				reasonStr = reason.Error()
			}
			cb(ctx, cn, reasonStr, reason)
		}
	}

	_ = p.closeConn(cn)

	// Check if we need to create new idle connections to maintain MinIdleConns
	p.checkMinIdleConns()
}

// CloseConn closes a connection and records metrics.
// Parameters:
//   - ctx: context for metric callbacks (enables trace-to-metric correlation)
//   - cn: the connection to close
//   - reason: why the connection is being closed (use CloseReason* constants)
//   - fromState: unused; db.client.connection.count is read from Stats at collection time
func (p *ConnPool) CloseConn(ctx context.Context, cn *Conn, reason string, fromState string) error {
	if hookManager := p.hookManager.Load(); hookManager != nil {
		hookManager.ProcessOnRemove(ctx, cn, errors.New(reason))
	}

	// Only emit connection closed if we actually owned the removal.
	// If removed is false, Close() already emitted connectionClosed for this conn.
	if p.removeConnWithLock(cn) {
		p.recordConnectionClosed(ctx, cn, reason)
	}

	return p.closeConn(cn)
}

func (p *ConnPool) recordConnectionClosed(ctx context.Context, cn *Conn, reason string) {
	if cb := p.metrics().ConnectionClosed; cb != nil {
		cb(ctx, cn, reason, nil)
	}
}

// removeConnWithLock removes a connection from the pool under the connsMu lock.
// Returns true if the connection was actually present in p.conns and was removed,
// false if it was already gone (e.g., removed by Close()). Callers must use the
// return value to decide whether to emit connection-closed metrics; this eliminates
// the shutdown race between Close() and concurrent removal paths.
func (p *ConnPool) removeConnWithLock(cn *Conn) bool {
	p.connsMu.Lock()
	defer p.connsMu.Unlock()
	return p.removeConn(cn)
}

// forgetConn drops cn from p.conns and updates the stale and pool-size
// counters. It reports false if cn was already gone (e.g. removed by Close()).
// It does not touch idleConns. Caller holds connsMu.
func (p *ConnPool) forgetConn(cn *Conn) bool {
	cid := cn.GetID()
	if _, exists := p.conns[cid]; !exists {
		return false
	}
	delete(p.conns, cid)
	atomic.AddUint32(&p.stats.StaleConns, 1)

	// Decrement pool size counter when removing a connection
	if cn.pooled {
		p.poolSize.Add(-1)
	}
	return true
}

// removeConn removes a connection from the pool's internal data structures.
// Returns whether the connection was present and removed.
func (p *ConnPool) removeConn(cn *Conn) bool {
	if !p.forgetConn(cn) {
		return false
	}
	// An unpooled conn can be idle too: putIdleConn parks an undelivered
	// overflow dial in idleConns.
	for idx, ic := range p.idleConns {
		if ic == cn {
			p.idleConns = append(p.idleConns[:idx], p.idleConns[idx+1:]...)
			p.idleConnsLen.Add(-1)
			break
		}
	}
	return true
}

func (p *ConnPool) closeConn(cn *Conn) error {
	return cn.Close()
}

// Len returns total number of connections.
func (p *ConnPool) Len() int {
	p.connsMu.Lock()
	n := len(p.conns)
	p.connsMu.Unlock()
	return n
}

// IdleLen returns number of idle connections.
func (p *ConnPool) IdleLen() int {
	p.connsMu.Lock()
	n := p.idleConnsLen.Load()
	p.connsMu.Unlock()
	return int(n)
}

// Name returns the pool's configured name, which is stamped on every
// connection it creates (Conn.PoolName). Callers holding a Pooler can type
// assert to interface{ Name() string } to find which pool owns a connection —
// used by maintnotifications to route a handoff to the hook that owns the
// conn's pool rather than always the primary one.
func (p *ConnPool) Name() string { return p.cfg.Name }

// Size returns the maximum pool size (capacity).
//
// This is used by the streaming credentials manager to size the re-auth worker pool,
// ensuring that re-auth operations don't exhaust the connection pool.
func (p *ConnPool) Size() int {
	return int(p.cfg.PoolSize)
}

// HasFreeCapacity reports whether the pool can likely serve another Get without
// blocking — a best-effort probe for the autopipeline straggler-hold gate (a
// false positive only costs a spill / short hold, never correctness). Checks,
// in order: a truly servable idle conn (usableIdleLen — plain IdleLen counts
// not-usable and handoff-marked entries an OnGet hook would divert); a free
// pool turn (accounts for in-use conns AND in-flight dials); room under
// PoolSize and MaxActiveConns (newConn's hard dial gate); a free dial slot
// (MaxConcurrentDials below PoolSize can saturate slots while a turn is free);
// and the dial circuit breaker not being open (a saturated dialErrorsNum makes
// the Get fail fast rather than serve).
func (p *ConnPool) HasFreeCapacity() bool {
	// Turn check comes FIRST: Get acquires a turn before popIdle, so with the
	// semaphore exhausted even a usable idle conn is not immediately servable
	// (putConnWithoutTurn can re-pool a conn while its turn stays held, so
	// idle > 0 does not imply a free turn).
	if p.semaphore.Available() <= 0 {
		return false
	}
	if p.usableIdleLen() > 0 {
		return true
	}
	size := p.poolSize.Load()
	if size >= p.cfg.PoolSize {
		return false
	}
	if m := p.cfg.MaxActiveConns; m > 0 {
		p.connsMu.Lock()
		full := len(p.conns)+p.pendingConns >= int(m)
		p.connsMu.Unlock()
		if full {
			return false
		}
	}
	if c := cap(p.dialsInProgress); c > 0 && len(p.dialsInProgress) >= c {
		return false
	}
	// Dial circuit breaker open (dialConn fails fast until a background probe
	// succeeds): the Get would need this dial and error immediately, so there is
	// no capacity to report.
	if p.dialErrorsNum.Load() >= uint32(p.cfg.PoolSize) {
		return false
	}
	return true
}

// usableIdleLen counts idle connections a Get would actually serve, unlike
// IdleLen(): it excludes not-usable conns (mid state transition) and
// handoff-marked conns (still StateIdle/usable, but the OnGet pool hook diverts
// them to handoff). Best-effort — a re-auth-marked conn is a rare residual
// false positive. The idle slice is bounded by PoolSize, so the scan is cheap.
func (p *ConnPool) usableIdleLen() int {
	p.connsMu.Lock()
	n := 0
	for _, cn := range p.idleConns {
		if cn.IsUsable() && !cn.ShouldHandoff() {
			n++
		}
	}
	p.connsMu.Unlock()
	return n
}

func (p *ConnPool) Stats() *Stats {
	p.connsMu.Lock()
	totalConns := len(p.conns)
	// idleConnsLen also includes reservations for MinIdleConns dials.
	idleConns := len(p.idleConns)
	p.connsMu.Unlock()

	return &Stats{
		Hits:            atomic.LoadUint32(&p.stats.Hits),
		Misses:          atomic.LoadUint32(&p.stats.Misses),
		Timeouts:        atomic.LoadUint32(&p.stats.Timeouts),
		WaitCount:       atomic.LoadUint32(&p.stats.WaitCount),
		Unusable:        atomic.LoadUint32(&p.stats.Unusable),
		WaitDurationNs:  p.waitDurationNs.Load(),
		PendingRequests: uint32(p.pending.Load()),

		TotalConns: uint32(totalConns),
		IdleConns:  uint32(idleConns),
		StaleConns: atomic.LoadUint32(&p.stats.StaleConns),
	}
}

const (
	pendingStart = 1<<32 | 1  // a Get started and is waiting
	pendingDone  = ^uint64(0) // a Get stopped waiting: -1 on the low half
)

// PendingRequests returns the Gets waiting for a connection now and the Gets
// started since the pool was created (wraps at 2^32), from one atomic load.
func (p *ConnPool) PendingRequests() (pending, started uint32) {
	v := p.pending.Load()
	return uint32(v), uint32(v >> 32)
}

func (p *ConnPool) closed() bool {
	return p._closed.Load() == 1
}

func (p *ConnPool) RetireConns(ctx context.Context, conns []*Conn, reason string) {
	if len(conns) == 0 {
		return
	}

	idleConnSet := make(map[*Conn]struct{})
	toClose := make([]*Conn, 0, len(conns))

	p.connsMu.Lock()
	for _, ic := range p.idleConns {
		idleConnSet[ic] = struct{}{}
	}
	for _, cn := range conns {
		if cn == nil {
			continue
		}
		if _, ok := p.conns[cn.GetID()]; !ok {
			continue
		}
		if _, isIdle := idleConnSet[cn]; isIdle {
			if p.removeConn(cn) {
				toClose = append(toClose, cn)
			}
			continue
		}
		cn.MarkCloseOnPut(reason)
	}
	p.connsMu.Unlock()

	if hookManager := p.hookManager.Load(); hookManager != nil {
		for _, cn := range toClose {
			hookManager.ProcessOnRemove(ctx, cn, errors.New(reason))
		}
	}
	for _, cn := range toClose {
		p.recordConnectionClosed(ctx, cn, reason)
		_ = p.closeConn(cn)
	}
	p.checkMinIdleConns()
}

func (p *ConnPool) Filter(fn func(*Conn) bool) error {
	ctx := context.Background()

	p.connsMu.Lock()

	idleConnSet := make(map[*Conn]struct{}, len(p.idleConns))
	for _, ic := range p.idleConns {
		idleConnSet[ic] = struct{}{}
	}

	var firstErr error
	var toClose []*Conn
	for _, cn := range p.conns {
		if fn(cn) {
			if _, isIdle := idleConnSet[cn]; isIdle {
				// Idle connection - remove from pool, close it below.
				p.removeConn(cn)
				toClose = append(toClose, cn)
			} else {
				// Used connection - set closeReason and close the connection.
				// The connection remains in p.conns. When putConn() is called later,
				// it will close the connection instead of pooling it.
				cn.closeReason.Store(CloseReasonFailover)
				if err := cn.Close(); err != nil && firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	p.connsMu.Unlock()

	// Run OnRemove so hooks drop per-connection state (e.g. streaming
	// credentials listeners) for the idle conns removed here.
	if hookManager := p.hookManager.Load(); hookManager != nil {
		for _, cn := range toClose {
			hookManager.ProcessOnRemove(ctx, cn, errors.New(CloseReasonFailover))
		}
	}
	for _, cn := range toClose {
		p.recordConnectionClosed(ctx, cn, CloseReasonFailover)
		if err := p.closeConn(cn); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type drainConn struct {
	conn      *Conn
	idleIndex int
}

// DrainState carries the cross-pass round bookkeeping for the CSC drainer.
// It is owned by one drainer goroutine, so no synchronization is needed.
type DrainState struct {
	// round contains only initialized idle connections. Entries are processed
	// from the end so their snapshot indexes remain stable as connections are
	// removed and returned. Connections that go idle mid-round are deferred.
	round []drainConn
	next  int
}

// DrainIdleConns runs one pass of the CSC invalidation drainer over the current
// round, holding AT MOST ONE connection and its pool turn at a time (ctx is the
// per-cycle deadline). A round = idle conn ids snapshotted at start; mid-round
// arrivals are deferred. Each member is drainerPop'd and drained by fn, or — if no
// longer a claimable idle conn — reconciled (marked visited) so it can't hang the
// round. The drainer yields when no turn is immediately available, giving command
// traffic priority. Handles at least one member before honoring ctx (so a tiny
// DrainInterval can't stall it). No-ops if the pool is closed.
func (p *ConnPool) DrainIdleConns(ctx context.Context, st *DrainState, fn func(cn *Conn) error) {
	if st == nil || fn == nil || p.closed() {
		return
	}

	if st.round == nil {
		st.round = p.idleConnsSnapshot()
		st.next = len(st.round)
		if len(st.round) == 0 {
			st.round = nil
			return
		}
	}

	handled := 0
	for st.next > 0 {
		// Min-progress: handle at least one member — drained OR reconciled — before
		// honoring the per-cycle deadline, so a pass does a bounded amount of work
		// while ignoring an expired ctx. A deadline-truncated round resumes on the
		// next pass.
		if handled > 0 && ctx.Err() != nil {
			return
		}

		// Account for the borrowed connection exactly like Get. Without a turn,
		// a concurrent Get can observe the temporarily-empty idle pool and either
		// exceed PoolSize or fail at MaxActiveConns. Maintenance never waits for a
		// turn, so command traffic wins under contention.
		if !p.semaphore.TryAcquire() {
			return
		}
		st.next--
		cn := p.drainerPop(ctx, st.round[st.next])
		if cn == nil {
			p.freeTurn()
			// Reconcile: not a claimable idle member right now (closed, in use,
			// unusable, or moved in idleConns by concurrent traffic). Covered by
			// the command-path drain and/or the next round.
			handled++
			continue
		}

		func() {
			defer p.endDrainerBorrow()
			if err := fn(cn); err != nil {
				// Fatal drain error (read/protocol/connection).
				p.removeConnInternal(ctx, cn, err, true)
			} else {
				// Normal return: runs OnPut (queues any maintenance handoff).
				p.putConn(ctx, cn, true)
			}
		}()
		handled++
	}

	// Every member handled — round complete; snapshot a fresh round next pass.
	st.round = nil
	st.next = 0
}

// idleConnsSnapshot returns initialized idle connections and their current
// indexes. StateCreated MinIdleConns are intentionally excluded.
func (p *ConnPool) idleConnsSnapshot() []drainConn {
	p.connsMu.Lock()
	defer p.connsMu.Unlock()
	if len(p.idleConns) == 0 {
		return nil
	}
	round := make([]drainConn, 0, len(p.idleConns))
	for idx, cn := range p.idleConns {
		if cn.stateMachine.GetState() == StateIdle {
			round = append(round, drainConn{conn: cn, idleIndex: idx})
		}
	}
	return round
}

// drainerPop claims a snapshotted connection (strict IDLE->IN_USE) and removes
// it from idleConns in O(1). Entries are processed in reverse index order, so
// swap removal cannot move an unprocessed round member. If concurrent pool
// traffic changed the slot, the member is deferred to the next round.
func (p *ConnPool) drainerPop(ctx context.Context, member drainConn) *Conn {
	p.connsMu.Lock()
	defer p.connsMu.Unlock()
	if p.closed() {
		return nil
	}
	idx := member.idleIndex
	if idx < 0 || idx >= len(p.idleConns) || p.idleConns[idx] != member.conn {
		return nil
	}
	cn := member.conn
	if !cn.stateMachine.TryTransitionFast(StateIdle, StateInUse) {
		return nil
	}
	p.beginDrainerBorrow()
	last := len(p.idleConns) - 1
	p.idleConns[idx] = p.idleConns[last]
	p.idleConns[last] = nil
	p.idleConns = p.idleConns[:last]
	p.idleConnsLen.Add(-1)
	return cn
}

func (p *ConnPool) Close() error {
	if !p._closed.CompareAndSwap(0, 1) {
		return ErrClosed
	}
	close(p.closedCh)
	// Let an in-flight reaper pass finish closing the conns it already took
	// out of the pool, so none outlive Close.
	if p.reaperDone != nil {
		<-p.reaperDone
	}

	var firstErr error
	nowNs := time.Now().UnixNano()

	livePoolsMu.Lock()
	delete(liveConnPools, weak.Make(p))
	livePoolsMu.Unlock()
	p.connsMu.Lock()
	idleSet := make(map[uint64]struct{}, len(p.idleConns))
	for _, cn := range p.idleConns {
		idleSet[cn.GetID()] = struct{}{}
	}
	ctx := context.Background()
	for _, cn := range p.conns {
		// Check health before closing, since closeConn invalidates the
		// underlying fd and would make connCheck (inside isHealthyConn)
		// always fail with EBADF.
		// Only check health for idle connections to avoid data races when
		// peeking at the socket/reader while another goroutine is reading from it.
		// Non-idle connections are either in use or in transitional states and
		// shouldn't be health-checked during shutdown.
		_, isIdle := idleSet[cn.GetID()]
		var healthy bool
		if isIdle {
			healthy = p.isHealthyConn(cn, nowNs)
		} else {
			healthy = true
		}
		if closedCb := p.metrics().ConnectionClosed; closedCb != nil {
			closedCb(ctx, cn, "pool_shutdown", nil)
		}
		if err := p.closeConn(cn); err != nil && firstErr == nil {
			// Suppress close errors for stale connections, consistent
			// with how Get() handles them (see CloseReasonStale path).
			if healthy {
				firstErr = err
			}
		}
	}
	p.conns = nil
	p.poolSize.Store(0)
	p.idleConns = nil
	p.idleConnsLen.Store(0)
	p.connsMu.Unlock()

	return firstErr
}

func (p *ConnPool) isHealthyConn(cn *Conn, nowNs int64) bool {
	// Performance optimization: check conditions from cheapest to most expensive,
	// and from most likely to fail to least likely to fail.

	// Only fails if ConnMaxLifetime is set AND connection is old.
	// Most pools don't set ConnMaxLifetime, so this rarely fails.
	if p.cfg.ConnMaxLifetime > 0 {
		if cn.expiresAt.UnixNano() < nowNs {
			return false // Connection has exceeded max lifetime
		}
	}

	// Most pools set ConnMaxIdleTime, and idle connections are common.
	// Checking this first allows us to fail fast without expensive syscalls.
	if p.cfg.ConnMaxIdleTime > 0 {
		if nowNs-cn.UsedAtNs() >= int64(p.cfg.ConnMaxIdleTime) {
			return false // Connection has been idle too long
		}
	}

	// Only run this if the cheap checks passed.
	if err := connCheck(cn.getNetConn()); err != nil {
		// If there's unexpected data, it might be push notifications (RESP3)
		if p.cfg.PushNotificationsEnabled && err == errUnexpectedRead {
			// Peek at the reply type to check if it's a push notification.
			// Use the readerMu-guarded peek: a concurrent handoff may be
			// resetting cn.rd via SetNetConn on a connection popped by Get
			// before the OnGet state check rejects it.
			if replyType, err := cn.PeekReplyTypeForCheck(); err == nil && replyType == proto.RespPush {
				// For RESP3 connections with push notifications, we allow some buffered data
				// The client will process these notifications before using the connection.
				// This is the normal healthy path for any client with server-side
				// invalidation or maintenance notifications (client-side caching parks
				// an invalidate frame on idle conns after every tracked write), so it
				// logs at debug level only — at default level it would flood the log
				// on every pool Get under a write-heavy tracked workload.
				if internal.LogLevel.DebugOrAbove() {
					internal.Logger.Printf(
						context.Background(),
						"push: conn[%d] has buffered data, likely push notifications - will be processed by client",
						cn.GetID(),
					)
				}

				// Update timestamp for healthy connection
				cn.SetUsedAtNs(nowNs)

				// Connection is healthy, client will handle notifications
				return true
			}
			// Not a push notification - treat as unhealthy
			return false
		}
		// Connection failed health check
		return false
	}

	// Only update UsedAt if connection is healthy (avoids unnecessary atomic store)
	cn.SetUsedAtNs(nowNs)
	return true
}
