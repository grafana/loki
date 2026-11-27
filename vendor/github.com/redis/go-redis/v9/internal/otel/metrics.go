package otel

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9/internal/pool"
)

// Cmder is a minimal interface for command information needed for metrics.
// This avoids circular dependencies with the main redis package.
type Cmder interface {
	Name() string
	FullName() string
	Args() []interface{}
	Err() error
}

// Recorder is the interface for recording metrics.
type Recorder interface {
	// RecordOperationDuration records the total operation duration (including all retries)
	// dbIndex is the Redis database index (0-15)
	RecordOperationDuration(ctx context.Context, duration time.Duration, cmd Cmder, attempts int, err error, cn *pool.Conn, dbIndex int)

	// RecordPipelineOperationDuration records the total pipeline/transaction duration.
	// operationName should be "PIPELINE" for regular pipelines or "MULTI" for transactions.
	// cmdCount is the number of commands in the pipeline.
	// err is the error from the pipeline execution (can be nil).
	// dbIndex is the Redis database index (0-15)
	RecordPipelineOperationDuration(ctx context.Context, duration time.Duration, operationName string, cmdCount int, attempts int, err error, cn *pool.Conn, dbIndex int)

	// RecordConnectionCreateTime records the time it took to create a new connection
	RecordConnectionCreateTime(ctx context.Context, duration time.Duration, cn *pool.Conn)

	// RecordConnectionRelaxedTimeout records when connection timeout is relaxed/unrelaxed
	// delta: +1 for relaxed, -1 for unrelaxed
	// poolName: name of the connection pool (e.g., "main", "pubsub")
	// notificationType: the notification type that triggered the timeout relaxation (e.g., "MOVING")
	RecordConnectionRelaxedTimeout(ctx context.Context, delta int, cn *pool.Conn, poolName, notificationType string)

	// RecordConnectionHandoff records when a connection is handed off to another node
	// poolName: name of the connection pool (e.g., "main", "pubsub")
	RecordConnectionHandoff(ctx context.Context, cn *pool.Conn, poolName string)

	// RecordError records client errors (ASK, MOVED, handshake failures, etc.)
	// errorType: type of error (e.g., "ASK", "MOVED", "HANDSHAKE_FAILED")
	// statusCode: Redis response status code if available (e.g., "MOVED", "ASK")
	// isInternal: whether this is an internal error
	// retryAttempts: number of retry attempts made
	RecordError(ctx context.Context, errorType string, cn *pool.Conn, statusCode string, isInternal bool, retryAttempts int)

	// RecordMaintenanceNotification records when a maintenance notification is received
	// notificationType: the type of notification (e.g., "MOVING", "MIGRATING", etc.)
	RecordMaintenanceNotification(ctx context.Context, cn *pool.Conn, notificationType string)

	// RecordConnectionWaitTime records the time spent waiting for a connection from the pool
	RecordConnectionWaitTime(ctx context.Context, duration time.Duration, cn *pool.Conn)

	// RecordConnectionClosed records when a connection is closed
	// reason: reason for closing (e.g., "idle", "max_lifetime", "error", "pool_closed")
	// err: the error that caused the close (nil for non-error closures)
	RecordConnectionClosed(ctx context.Context, cn *pool.Conn, reason string, err error)

	// RecordPubSubMessage records a Pub/Sub message
	// direction: "sent" or "received"
	// channel: channel name (may be hidden for cardinality reduction)
	// sharded: true for sharded pub/sub (SPUBLISH/SSUBSCRIBE)
	RecordPubSubMessage(ctx context.Context, cn *pool.Conn, direction, channel string, sharded bool)

	// RecordStreamLag records the lag for stream consumer group processing
	// lag: time difference between message creation and consumption
	// streamName: name of the stream (may be hidden for cardinality reduction)
	// consumerGroup: name of the consumer group
	// consumerName: name of the consumer
	RecordStreamLag(ctx context.Context, lag time.Duration, cn *pool.Conn, streamName, consumerGroup, consumerName string)

	// RecordMultiDBFailover records a MultiDB (geo-failover) failover from one
	// member database to another. fromFQDN/toFQDN are host-only database FQDNs,
	// reason is "automatic" or "manual", and duration is the wall time of the
	// failover.
	RecordMultiDBFailover(ctx context.Context, fromFQDN, toFQDN, reason string, duration time.Duration)

	// RecordMultiDBActiveDatabaseChange records a change of the active MultiDB
	// member database (failover, fallback, or manual selection). fromFQDN/toFQDN
	// are host-only database FQDNs.
	RecordMultiDBActiveDatabaseChange(ctx context.Context, fromFQDN, toFQDN string)

	// RecordMultiDBCircuitStateChange records a MultiDB circuit breaker state
	// transition for the database identified by dbFQDN.
	RecordMultiDBCircuitStateChange(ctx context.Context, dbFQDN, fromState, toState string)

	// RecordMultiDBHealthCheck records the result of a MultiDB health-check pass
	// for the database identified by dbFQDN. duration is the wall time of the check.
	RecordMultiDBHealthCheck(ctx context.Context, dbFQDN string, success bool, duration time.Duration)

	// RecordPendingRequests records a change in pending requests (UpDownCounter)
	// delta: +1 when request starts waiting, -1 when request stops waiting
	// poolName is passed explicitly because we may not have a connection yet when request starts
	RecordPendingRequests(ctx context.Context, delta int, cn *pool.Conn, poolName string)
}

// PoolRegistrar is implemented by recorders that read pool state at collection
// time (connection count, pending requests). Register may be called more than
// once for the same pool and must be idempotent.
type PoolRegistrar interface {
	// RegisterPool is called when a new client is created with its connection pools,
	// and for every live pool when the recorder becomes the global one.
	// poolName: the pool's own name, the one its connections carry (Conn.PoolName)
	// pool: the connection pool
	RegisterPool(poolName string, pool pool.Pooler)
	// UnregisterPool is called when a client is closed to remove its pool from the registry.
	// pool: the connection pool to unregister
	UnregisterPool(pool pool.Pooler)
	// RegisterPubSubPool is called when a new client is created with a PubSub pool.
	// poolName: the pool's own name (e.g., "localhost:6379_1_pubsub")
	// pool: the PubSub connection pool
	RegisterPubSubPool(poolName string, pool PubSubPooler)
	// UnregisterPubSubPool is called when a PubSub client is closed to remove its pool.
	// pool: the PubSub connection pool to unregister
	UnregisterPubSubPool(pool PubSubPooler)
}

var (
	// setRecorderMu serializes SetGlobalRecorder with RegisterPools and
	// UnregisterPools for global clients, so a new recorder is handed every
	// open pool and never a pool whose client already unregistered it.
	setRecorderMu sync.Mutex

	// globalRecorder is the recorder installed by redis.SetOTelRecorder. Every
	// command of a client without its own recorder reads it, so it is an atomic
	// pointer: a shared RWMutex read lock costs a contended cache line per read.
	globalRecorder atomic.Pointer[recorderRef]
)

type recorderRef struct{ r Recorder }

var noopRef = &recorderRef{noopRecorder{}}

func init() { globalRecorder.Store(noopRef) }

// getRecorder returns the current global recorder.
func getRecorder() Recorder {
	return globalRecorder.Load().r
}

// SetGlobalRecorder installs r as the global recorder and binds the global
// pool metric callbacks to it. nil uninstalls both. Pools created before r was
// installed are registered with r, so they are observed too.
func SetGlobalRecorder(r Recorder) {
	setRecorderMu.Lock()
	defer setRecorderMu.Unlock()
	if r == nil {
		pool.SetAllMetricCallbacks(nil)
		globalRecorder.Store(noopRef)
		return
	}
	globalRecorder.Store(&recorderRef{r})
	pool.SetAllMetricCallbacks(PoolCallbacks(r))
	if registrar, ok := r.(PoolRegistrar); ok {
		connPools, pubSubPools := pool.GlobalPools()
		for _, p := range connPools {
			registrar.RegisterPool(p.Name(), p)
		}
		for _, p := range pubSubPools {
			registrar.RegisterPubSubPool(p.Name(), p)
		}
	}
}

// Enabled reports whether r records anything: false for the noop recorder (or
// a view of a global that is noop) and for a recorder whose own Enabled says
// so. Callers use it to skip metric work whose INPUTS are expensive to obtain,
// e.g. reading a command's result, which on the async autopipeline face blocks
// until the command executes.
func Enabled(r Recorder) bool {
	r = resolve(r)
	if _, noop := r.(noopRecorder); noop {
		return false
	}
	if e, ok := r.(interface{ Enabled() bool }); ok {
		return e.Enabled()
	}
	return true
}

// Scoped returns the recorder a client records to: r itself, or, when r is
// nil, a view of whatever global recorder is installed at call time. This is
// the only place "fall back to the global recorder" is written.
func Scoped(r Recorder) Recorder {
	if r != nil {
		return r
	}
	return scopedGlobal{}
}

// PendingDeltaFilter is implemented by recorders that may not use the pending
// request +1/-1 events, e.g. one that reads the pools at collection time. When
// WantsPendingDeltas returns false, the pool skips RecordPendingRequests.
type PendingDeltaFilter interface {
	WantsPendingDeltas() bool
}

// PoolCallbacks returns the pool metric callback table that records to r.
// Every callback captures r, so a -1 reaches the recorder that saw the +1.
func PoolCallbacks(r Recorder) *pool.MetricCallbacks {
	cb := &pool.MetricCallbacks{
		ConnectionCreateTime:     r.RecordConnectionCreateTime,
		ConnectionRelaxedTimeout: r.RecordConnectionRelaxedTimeout,
		ConnectionHandoff:        r.RecordConnectionHandoff,
		Error:                    r.RecordError,
		MaintenanceNotification:  r.RecordMaintenanceNotification,
		ConnectionWaitTime:       r.RecordConnectionWaitTime,
		ConnectionClosed:         r.RecordConnectionClosed,
	}
	if f, ok := r.(PendingDeltaFilter); !ok || f.WantsPendingDeltas() {
		cb.PendingRequests = r.RecordPendingRequests
	}
	return cb
}

// resolve returns the recorder behind r: the current global one for a Scoped(nil) view.
func resolve(r Recorder) Recorder {
	if _, ok := r.(scopedGlobal); ok {
		return getRecorder()
	}
	return r
}

// scopedGlobal forwards every call to the global recorder installed at call time.
type scopedGlobal struct{}

func (scopedGlobal) RecordOperationDuration(ctx context.Context, duration time.Duration, cmd Cmder, attempts int, err error, cn *pool.Conn, dbIndex int) {
	getRecorder().RecordOperationDuration(ctx, duration, cmd, attempts, err, cn, dbIndex)
}

func (scopedGlobal) RecordPipelineOperationDuration(ctx context.Context, duration time.Duration, operationName string, cmdCount int, attempts int, err error, cn *pool.Conn, dbIndex int) {
	getRecorder().RecordPipelineOperationDuration(ctx, duration, operationName, cmdCount, attempts, err, cn, dbIndex)
}

func (scopedGlobal) RecordConnectionCreateTime(ctx context.Context, duration time.Duration, cn *pool.Conn) {
	getRecorder().RecordConnectionCreateTime(ctx, duration, cn)
}

func (scopedGlobal) RecordConnectionRelaxedTimeout(ctx context.Context, delta int, cn *pool.Conn, poolName, notificationType string) {
	getRecorder().RecordConnectionRelaxedTimeout(ctx, delta, cn, poolName, notificationType)
}

func (scopedGlobal) RecordConnectionHandoff(ctx context.Context, cn *pool.Conn, poolName string) {
	getRecorder().RecordConnectionHandoff(ctx, cn, poolName)
}

func (scopedGlobal) RecordError(ctx context.Context, errorType string, cn *pool.Conn, statusCode string, isInternal bool, retryAttempts int) {
	getRecorder().RecordError(ctx, errorType, cn, statusCode, isInternal, retryAttempts)
}

func (scopedGlobal) RecordMaintenanceNotification(ctx context.Context, cn *pool.Conn, notificationType string) {
	getRecorder().RecordMaintenanceNotification(ctx, cn, notificationType)
}

func (scopedGlobal) RecordConnectionWaitTime(ctx context.Context, duration time.Duration, cn *pool.Conn) {
	getRecorder().RecordConnectionWaitTime(ctx, duration, cn)
}

func (scopedGlobal) RecordConnectionClosed(ctx context.Context, cn *pool.Conn, reason string, err error) {
	getRecorder().RecordConnectionClosed(ctx, cn, reason, err)
}

func (scopedGlobal) RecordPubSubMessage(ctx context.Context, cn *pool.Conn, direction, channel string, sharded bool) {
	getRecorder().RecordPubSubMessage(ctx, cn, direction, channel, sharded)
}

func (scopedGlobal) RecordStreamLag(ctx context.Context, lag time.Duration, cn *pool.Conn, streamName, consumerGroup, consumerName string) {
	getRecorder().RecordStreamLag(ctx, lag, cn, streamName, consumerGroup, consumerName)
}

func (scopedGlobal) RecordMultiDBFailover(ctx context.Context, fromFQDN, toFQDN, reason string, duration time.Duration) {
	getRecorder().RecordMultiDBFailover(ctx, fromFQDN, toFQDN, reason, duration)
}

func (scopedGlobal) RecordMultiDBActiveDatabaseChange(ctx context.Context, fromFQDN, toFQDN string) {
	getRecorder().RecordMultiDBActiveDatabaseChange(ctx, fromFQDN, toFQDN)
}

func (scopedGlobal) RecordMultiDBCircuitStateChange(ctx context.Context, dbFQDN, fromState, toState string) {
	getRecorder().RecordMultiDBCircuitStateChange(ctx, dbFQDN, fromState, toState)
}

func (scopedGlobal) RecordMultiDBHealthCheck(ctx context.Context, dbFQDN string, success bool, duration time.Duration) {
	getRecorder().RecordMultiDBHealthCheck(ctx, dbFQDN, success, duration)
}

func (scopedGlobal) RecordPendingRequests(ctx context.Context, delta int, cn *pool.Conn, poolName string) {
	getRecorder().RecordPendingRequests(ctx, delta, cn, poolName)
}

// NoopRecorder returns the recorder that discards everything. A test that
// installs a partial recorder embeds it so the methods the test does not
// care about still have an implementation.
func NoopRecorder() Recorder { return noopRecorder{} }

type noopRecorder struct{}

func (noopRecorder) RecordOperationDuration(context.Context, time.Duration, Cmder, int, error, *pool.Conn, int) {
}

func (noopRecorder) RecordPipelineOperationDuration(context.Context, time.Duration, string, int, int, error, *pool.Conn, int) {
}
func (noopRecorder) RecordConnectionCreateTime(context.Context, time.Duration, *pool.Conn) {}
func (noopRecorder) RecordConnectionRelaxedTimeout(context.Context, int, *pool.Conn, string, string) {
}
func (noopRecorder) RecordConnectionHandoff(context.Context, *pool.Conn, string)        {}
func (noopRecorder) RecordError(context.Context, string, *pool.Conn, string, bool, int) {}
func (noopRecorder) RecordMaintenanceNotification(context.Context, *pool.Conn, string)  {}

func (noopRecorder) RecordConnectionWaitTime(context.Context, time.Duration, *pool.Conn) {}
func (noopRecorder) RecordConnectionClosed(context.Context, *pool.Conn, string, error)   {}

func (noopRecorder) RecordPubSubMessage(context.Context, *pool.Conn, string, string, bool) {}

func (noopRecorder) RecordStreamLag(context.Context, time.Duration, *pool.Conn, string, string, string) {
}

func (noopRecorder) RecordMultiDBFailover(context.Context, string, string, string, time.Duration) {}
func (noopRecorder) RecordMultiDBActiveDatabaseChange(context.Context, string, string)            {}
func (noopRecorder) RecordMultiDBCircuitStateChange(context.Context, string, string, string)      {}
func (noopRecorder) RecordMultiDBHealthCheck(context.Context, string, bool, time.Duration)        {}

func (noopRecorder) RecordPendingRequests(context.Context, int, *pool.Conn, string) {}

// PubSubPooler is a PubSub pool as RegisterPools sees it.
type PubSubPooler interface {
	Stats() *pool.PubSubStats
	Name() string
}

// RegisterPools registers a client's connection pools with r when it is a
// PoolRegistrar, under each pool's own name. pipelinePool is the optional
// dedicated pipeline connection pool (nil when not configured).
func RegisterPools(r Recorder, connPool pool.Pooler, pubSubPool PubSubPooler, pipelinePool pool.Pooler) {
	if _, global := r.(scopedGlobal); global {
		// Hold setRecorderMu so a concurrent SetGlobalRecorder cannot miss
		// these pools or register them after UnregisterPools.
		setRecorderMu.Lock()
		defer setRecorderMu.Unlock()
	}
	// A Scoped(nil) view resolves to the global recorder under its lock:
	// SetGlobalRecorder writes it while clients are created and closed, and
	// an unlocked read here is a data race -race reports.
	registrar, ok := resolve(r).(PoolRegistrar)
	if !ok {
		return
	}
	for _, p := range []pool.Pooler{connPool, pipelinePool} {
		if p != nil {
			registrar.RegisterPool(poolName(p), p)
		}
	}
	if pubSubPool != nil {
		registrar.RegisterPubSubPool(pubSubPool.Name(), pubSubPool)
	}
}

// UnregisterPools removes a client's connection pools from r (see
// RegisterPools). Call it after the pools are closed: a closed pool is no
// longer handed to a recorder installed later.
func UnregisterPools(r Recorder, connPool pool.Pooler, pubSubPool PubSubPooler, pipelinePool pool.Pooler) {
	if _, global := r.(scopedGlobal); global {
		setRecorderMu.Lock()
		defer setRecorderMu.Unlock()
	}
	registrar, ok := resolve(r).(PoolRegistrar)
	if !ok {
		return
	}
	for _, p := range []pool.Pooler{connPool, pipelinePool} {
		if p != nil {
			registrar.UnregisterPool(p)
		}
	}
	if pubSubPool != nil {
		registrar.UnregisterPubSubPool(pubSubPool)
	}
}

func poolName(p pool.Pooler) string {
	if n, ok := p.(interface{ Name() string }); ok {
		return n.Name()
	}
	return ""
}
