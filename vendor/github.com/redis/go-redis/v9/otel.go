package redis

import (
	"context"
	"net"
	"time"
	"weak"

	"github.com/redis/go-redis/v9/internal/otel"
	"github.com/redis/go-redis/v9/internal/pool"
)

// ErrorTypeNil is the error type reported to OTelRecorder.RecordError for a Nil reply.
const ErrorTypeNil = "NIL"

// ConnInfo provides information about a Redis connection for metrics.
type ConnInfo interface {
	RemoteAddr() net.Addr
	PoolName() string
}

type Pooler interface {
	PoolStats() *pool.Stats
}

type PubSubPooler interface {
	Stats() *pool.PubSubStats
}

// OTelRecorder is the interface for recording OpenTelemetry metrics. It is the
// type of the global recorder (SetOTelRecorder) and of the per-client one
// (Options.OTelRecorder).
//
// A recorder may also implement Enabled() bool. go-redis consults it before
// metric work whose inputs are expensive to obtain and skips recording while
// it returns false; a recorder without it is always on. The other optional
// capabilities are OTelConnectionCounter, OTelMultiDBRecorder and
// OTelPoolRegistrar.
type OTelRecorder interface {
	// RecordOperationDuration records the total operation duration (including all retries)
	RecordOperationDuration(ctx context.Context, duration time.Duration, cmd Cmder, attempts int, err error, cn ConnInfo, dbIndex int)

	// RecordPipelineOperationDuration records the total pipeline/transaction duration.
	// operationName should be "PIPELINE" for regular pipelines or "MULTI" for transactions.
	RecordPipelineOperationDuration(ctx context.Context, duration time.Duration, operationName string, cmdCount int, attempts int, err error, cn ConnInfo, dbIndex int)

	// RecordConnectionCreateTime records the time it took to create a new connection
	RecordConnectionCreateTime(ctx context.Context, duration time.Duration, cn ConnInfo)

	// RecordConnectionRelaxedTimeout records when connection timeout is relaxed/unrelaxed
	// delta: +1 for relaxed, -1 for unrelaxed
	// poolName: name of the connection pool (e.g., "main", "pubsub")
	// notificationType: the notification type that triggered the timeout relaxation (e.g., "MOVING", "HANDOFF")
	RecordConnectionRelaxedTimeout(ctx context.Context, delta int, cn ConnInfo, poolName, notificationType string)

	// RecordConnectionHandoff records when a connection is handed off to another node
	// poolName: name of the connection pool (e.g., "main", "pubsub")
	RecordConnectionHandoff(ctx context.Context, cn ConnInfo, poolName string)

	// RecordError records client errors (ASK, MOVED, handshake failures, etc.)
	// errorType: type of error (e.g., "ASK", "MOVED", "HANDSHAKE_FAILED")
	// statusCode: Redis response status code if available (e.g., "MOVED", "ASK")
	// isInternal: whether this is an internal error
	// retryAttempts: number of retry attempts made
	RecordError(ctx context.Context, errorType string, cn ConnInfo, statusCode string, isInternal bool, retryAttempts int)

	// RecordMaintenanceNotification records when a maintenance notification is received
	// notificationType: the type of notification (e.g., "MOVING", "MIGRATING", etc.)
	RecordMaintenanceNotification(ctx context.Context, cn ConnInfo, notificationType string)

	// RecordConnectionWaitTime records the time spent waiting for a connection from the pool
	RecordConnectionWaitTime(ctx context.Context, duration time.Duration, cn ConnInfo)

	// RecordConnectionClosed records when a connection is closed
	// reason: reason for closing (e.g., "idle", "max_lifetime", "error", "pool_closed")
	// err: the error that caused the close (nil for non-error closures)
	RecordConnectionClosed(ctx context.Context, cn ConnInfo, reason string, err error)

	// RecordPubSubMessage records a Pub/Sub message
	// direction: "sent" or "received"
	// channel: channel name (may be hidden for cardinality reduction)
	// sharded: true for sharded pub/sub (SPUBLISH/SSUBSCRIBE)
	RecordPubSubMessage(ctx context.Context, cn ConnInfo, direction, channel string, sharded bool)

	// RecordStreamLag records the lag for stream consumer group processing
	// lag: time difference between message creation and consumption
	// streamName: name of the stream (may be hidden for cardinality reduction)
	// consumerGroup: name of the consumer group
	// consumerName: name of the consumer
	RecordStreamLag(ctx context.Context, lag time.Duration, cn ConnInfo, streamName, consumerGroup, consumerName string)
}

// OTelConnectionCounter is an optional capability interface for recording
// pending request changes via an UpDownCounter.
// Implementations of OTelRecorder can optionally implement this interface
// to receive pending request delta notifications.
// This is kept separate from OTelRecorder to avoid breaking existing
// third-party implementations when new methods are added.
type OTelConnectionCounter interface {
	// RecordConnectionCount is no longer called. A recorder that reports
	// connection counts implements OTelPoolRegistrar and reads PoolStats
	// (TotalConns, IdleConns) and PubSubStats.Active when it collects.
	//
	// Deprecated: kept so existing implementations still compile.
	RecordConnectionCount(ctx context.Context, delta int, cn ConnInfo, state string, isPubSub bool)

	// RecordPendingRequests records a change in pending requests (UpDownCounter)
	// delta: +1 when request starts waiting, -1 when request stops waiting
	// poolName is passed explicitly because we may not have a connection yet when request starts
	RecordPendingRequests(ctx context.Context, delta int, cn ConnInfo, poolName string)
}

// OTelMultiDBRecorder is an optional capability interface for recording
// MultiDB (geo-failover) metrics. Implementations of OTelRecorder can
// optionally implement this interface to receive MultiDB notifications.
// This is kept separate from OTelRecorder to avoid breaking existing
// third-party implementations when new methods are added.
type OTelMultiDBRecorder interface {
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
}

// OTelPoolRegistrar is an optional capability interface for recorders that
// read pool state when they collect, e.g. db.client.connection.count.
//
// The pools passed in are comparable: UnregisterPool receives a value equal to
// the one RegisterPool did, so they can be used as map keys. They do not keep
// a client that is dropped without Close alive. Register may be called more
// than once for the same pool (a recorder installed by SetOTelRecorder is
// handed every live pool). A Pooler for a go-redis connection pool also has
// PendingRequests() (pending, started uint32), which reads the waiting Gets
// without the pool lock.
type OTelPoolRegistrar interface {
	// RegisterPool is called when a new client is created with its main (or
	// pipeline) connection pool, and for every live pool when the recorder is
	// installed with SetOTelRecorder.
	// poolName: the pool's name, as carried by its connections (ConnInfo.PoolName)
	RegisterPool(poolName string, pool Pooler)
	// UnregisterPool is called when a client is closed to remove its pool from the registry.
	UnregisterPool(pool Pooler)
	// RegisterPubSubPool is called when a new client is created with a PubSub pool.
	// poolName: the pool's name, as carried by its connections (e.g., "localhost:6379_1_pubsub")
	RegisterPubSubPool(poolName string, pool PubSubPooler)
	// UnregisterPubSubPool is called when a PubSub client is closed to remove its pool.
	UnregisterPubSubPool(pool PubSubPooler)
}

// SetOTelRecorder sets the global OpenTelemetry recorder.
func SetOTelRecorder(r OTelRecorder) {
	if r == nil {
		otel.SetGlobalRecorder(nil)
		return
	}
	otel.SetGlobalRecorder(&otelRecorderAdapter{r})
}

type otelRecorderAdapter struct {
	recorder OTelRecorder
}

// clientRecorder resolves a client's recorder once, at construction: r wrapped
// for internal use, or a view of the global recorder when r is nil. The pool
// callback table is nil in the global case, so the client's pools follow the
// global table and its recount.
func clientRecorder(r OTelRecorder) (otel.Recorder, *pool.MetricCallbacks) {
	if r == nil {
		return otel.Scoped(nil), nil
	}
	rec := &otelRecorderAdapter{r}
	return rec, otel.PoolCallbacks(rec)
}

// scopedRecorder is clientRecorder for clients that own no connection pool.
func scopedRecorder(r OTelRecorder) otel.Recorder {
	rec, _ := clientRecorder(r)
	return rec
}

// WantsPendingDeltas implements otel.PendingDeltaFilter: only a recorder that
// implements OTelConnectionCounter receives pending request deltas, so pools
// skip the per-Get call for any other.
func (a *otelRecorderAdapter) WantsPendingDeltas() bool {
	_, ok := a.recorder.(OTelConnectionCounter)
	return ok
}

// Enabled forwards to the public recorder's optional Enabled method (see
// otel.Enabled); a recorder without one is always on.
func (a *otelRecorderAdapter) Enabled() bool {
	if e, ok := a.recorder.(interface{ Enabled() bool }); ok {
		return e.Enabled()
	}
	return true
}

// toConnInfo converts *pool.Conn to ConnInfo interface properly.
// This ensures that a nil *pool.Conn becomes a true nil interface,
// not a non-nil interface containing a nil pointer.
func toConnInfo(cn *pool.Conn) ConnInfo {
	if cn == nil {
		return nil
	}
	return cn
}

func (a *otelRecorderAdapter) RecordOperationDuration(ctx context.Context, duration time.Duration, cmd otel.Cmder, attempts int, err error, cn *pool.Conn, dbIndex int) {
	// Convert internal Cmder to public Cmder
	if publicCmd, ok := cmd.(Cmder); ok {
		a.recorder.RecordOperationDuration(ctx, duration, publicCmd, attempts, err, toConnInfo(cn), dbIndex)
	}
}

func (a *otelRecorderAdapter) RecordPipelineOperationDuration(ctx context.Context, duration time.Duration, operationName string, cmdCount int, attempts int, err error, cn *pool.Conn, dbIndex int) {
	a.recorder.RecordPipelineOperationDuration(ctx, duration, operationName, cmdCount, attempts, err, toConnInfo(cn), dbIndex)
}

func (a *otelRecorderAdapter) RecordConnectionCreateTime(ctx context.Context, duration time.Duration, cn *pool.Conn) {
	a.recorder.RecordConnectionCreateTime(ctx, duration, toConnInfo(cn))
}

func (a *otelRecorderAdapter) RecordConnectionRelaxedTimeout(ctx context.Context, delta int, cn *pool.Conn, poolName, notificationType string) {
	a.recorder.RecordConnectionRelaxedTimeout(ctx, delta, toConnInfo(cn), poolName, notificationType)
}

func (a *otelRecorderAdapter) RecordConnectionHandoff(ctx context.Context, cn *pool.Conn, poolName string) {
	a.recorder.RecordConnectionHandoff(ctx, toConnInfo(cn), poolName)
}

func (a *otelRecorderAdapter) RecordError(ctx context.Context, errorType string, cn *pool.Conn, statusCode string, isInternal bool, retryAttempts int) {
	a.recorder.RecordError(ctx, errorType, toConnInfo(cn), statusCode, isInternal, retryAttempts)
}

func (a *otelRecorderAdapter) RecordMaintenanceNotification(ctx context.Context, cn *pool.Conn, notificationType string) {
	a.recorder.RecordMaintenanceNotification(ctx, toConnInfo(cn), notificationType)
}

func (a *otelRecorderAdapter) RecordConnectionWaitTime(ctx context.Context, duration time.Duration, cn *pool.Conn) {
	a.recorder.RecordConnectionWaitTime(ctx, duration, toConnInfo(cn))
}

func (a *otelRecorderAdapter) RecordConnectionClosed(ctx context.Context, cn *pool.Conn, reason string, err error) {
	a.recorder.RecordConnectionClosed(ctx, toConnInfo(cn), reason, err)
}

func (a *otelRecorderAdapter) RecordPubSubMessage(ctx context.Context, cn *pool.Conn, direction, channel string, sharded bool) {
	a.recorder.RecordPubSubMessage(ctx, toConnInfo(cn), direction, channel, sharded)
}

func (a *otelRecorderAdapter) RecordStreamLag(ctx context.Context, lag time.Duration, cn *pool.Conn, streamName, consumerGroup, consumerName string) {
	a.recorder.RecordStreamLag(ctx, lag, toConnInfo(cn), streamName, consumerGroup, consumerName)
}

func (a *otelRecorderAdapter) RecordMultiDBFailover(ctx context.Context, fromFQDN, toFQDN, reason string, duration time.Duration) {
	if r, ok := a.recorder.(OTelMultiDBRecorder); ok {
		r.RecordMultiDBFailover(ctx, fromFQDN, toFQDN, reason, duration)
	}
}

func (a *otelRecorderAdapter) RecordMultiDBActiveDatabaseChange(ctx context.Context, fromFQDN, toFQDN string) {
	if r, ok := a.recorder.(OTelMultiDBRecorder); ok {
		r.RecordMultiDBActiveDatabaseChange(ctx, fromFQDN, toFQDN)
	}
}

func (a *otelRecorderAdapter) RecordMultiDBCircuitStateChange(ctx context.Context, dbFQDN, fromState, toState string) {
	if r, ok := a.recorder.(OTelMultiDBRecorder); ok {
		r.RecordMultiDBCircuitStateChange(ctx, dbFQDN, fromState, toState)
	}
}

func (a *otelRecorderAdapter) RecordMultiDBHealthCheck(ctx context.Context, dbFQDN string, success bool, duration time.Duration) {
	if r, ok := a.recorder.(OTelMultiDBRecorder); ok {
		r.RecordMultiDBHealthCheck(ctx, dbFQDN, success, duration)
	}
}

func (a *otelRecorderAdapter) RecordPendingRequests(ctx context.Context, delta int, cn *pool.Conn, poolName string) {
	if counter, ok := a.recorder.(OTelConnectionCounter); ok {
		counter.RecordPendingRequests(ctx, delta, toConnInfo(cn), poolName)
	}
}

func (a *otelRecorderAdapter) RegisterPool(poolName string, p pool.Pooler) {
	if registrar, ok := a.recorder.(OTelPoolRegistrar); ok {
		registrar.RegisterPool(poolName, poolerOf(p))
	}
}

func (a *otelRecorderAdapter) UnregisterPool(p pool.Pooler) {
	if registrar, ok := a.recorder.(OTelPoolRegistrar); ok {
		registrar.UnregisterPool(poolerOf(p))
	}
}

func (a *otelRecorderAdapter) RegisterPubSubPool(poolName string, p otel.PubSubPooler) {
	if registrar, ok := a.recorder.(OTelPoolRegistrar); ok {
		registrar.RegisterPubSubPool(poolName, pubSubPoolerOf(p))
	}
}

func (a *otelRecorderAdapter) UnregisterPubSubPool(p otel.PubSubPooler) {
	if registrar, ok := a.recorder.(OTelPoolRegistrar); ok {
		registrar.UnregisterPubSubPool(pubSubPoolerOf(p))
	}
}

// poolerOf returns the Pooler a registrar sees for p (see OTelPoolRegistrar).
func poolerOf(p pool.Pooler) Pooler {
	if cp, ok := p.(*pool.ConnPool); ok {
		return connPoolRef{weak.Make(cp)}
	}
	return poolerAdapter{p}
}

func pubSubPoolerOf(p otel.PubSubPooler) PubSubPooler {
	if pp, ok := p.(*pool.PubSubPool); ok {
		return pubSubPoolRef{weak.Make(pp)}
	}
	return pubSubPoolerAdapter{p}
}

// connPoolRef holds a pool weakly. Collected reports that the pool was
// garbage collected; PoolStats then returns zero stats.
type connPoolRef struct{ p weak.Pointer[pool.ConnPool] }

func (r connPoolRef) PoolStats() *pool.Stats {
	if p := r.p.Value(); p != nil {
		return p.Stats()
	}
	return &pool.Stats{}
}

func (r connPoolRef) Collected() bool { return r.p.Value() == nil }

// PendingRequests returns the pool's waiting Gets and the Gets started since
// it was created (wraps at 2^32), without taking the pool lock; zeros after
// the pool is collected.
func (r connPoolRef) PendingRequests() (pending, started uint32) {
	if p := r.p.Value(); p != nil {
		return p.PendingRequests()
	}
	return 0, 0
}

type pubSubPoolRef struct{ p weak.Pointer[pool.PubSubPool] }

func (r pubSubPoolRef) Stats() *pool.PubSubStats {
	if p := r.p.Value(); p != nil {
		return p.Stats()
	}
	return &pool.PubSubStats{}
}

func (r pubSubPoolRef) Collected() bool { return r.p.Value() == nil }

type poolerAdapter struct {
	p pool.Pooler
}

func (a poolerAdapter) PoolStats() *pool.Stats {
	return a.p.Stats()
}

type pubSubPoolerAdapter struct {
	p otel.PubSubPooler
}

func (a pubSubPoolerAdapter) Stats() *pool.PubSubStats {
	return a.p.Stats()
}
