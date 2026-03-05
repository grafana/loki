package otel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/redis/go-redis/v9/internal/pool"
)

// generateUniqueID generates a short unique identifier for pool names.
func generateUniqueID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

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
}

type PubSubPooler interface {
	Stats() *pool.PubSubStats
}

type PoolRegistrar interface {
	// RegisterPool is called when a new client is created with its connection pools.
	// poolName: identifier for the pool (e.g., "main_abc123")
	// pool: the connection pool
	RegisterPool(poolName string, pool pool.Pooler)
	// UnregisterPool is called when a client is closed to remove its pool from the registry.
	// pool: the connection pool to unregister
	UnregisterPool(pool pool.Pooler)
	// RegisterPubSubPool is called when a new client is created with a PubSub pool.
	// poolName: identifier for the pool (e.g., "main_abc123_pubsub")
	// pool: the PubSub connection pool
	RegisterPubSubPool(poolName string, pool PubSubPooler)
	// UnregisterPubSubPool is called when a PubSub client is closed to remove its pool.
	// pool: the PubSub connection pool to unregister
	UnregisterPubSubPool(pool PubSubPooler)
}

var (
	// recorderMu protects globalRecorder and operation duration callbacks
	recorderMu sync.RWMutex

	// Global recorder instance (initialized by extra/redisotel-native)
	globalRecorder Recorder = noopRecorder{}

	// Callbacks for operation duration metrics
	operationDurationCallback         func(ctx context.Context, duration time.Duration, cmd Cmder, attempts int, err error, cn *pool.Conn, dbIndex int)
	pipelineOperationDurationCallback func(ctx context.Context, duration time.Duration, operationName string, cmdCount int, attempts int, err error, cn *pool.Conn, dbIndex int)
)

// GetOperationDurationCallback returns the callback for operation duration.
func GetOperationDurationCallback() func(ctx context.Context, duration time.Duration, cmd Cmder, attempts int, err error, cn *pool.Conn, dbIndex int) {
	recorderMu.RLock()
	cb := operationDurationCallback
	recorderMu.RUnlock()
	return cb
}

// GetPipelineOperationDurationCallback returns the callback for pipeline operation duration.
func GetPipelineOperationDurationCallback() func(ctx context.Context, duration time.Duration, operationName string, cmdCount int, attempts int, err error, cn *pool.Conn, dbIndex int) {
	recorderMu.RLock()
	cb := pipelineOperationDurationCallback
	recorderMu.RUnlock()
	return cb
}

// getRecorder returns the current global recorder under a read lock.
func getRecorder() Recorder {
	recorderMu.RLock()
	r := globalRecorder
	recorderMu.RUnlock()
	return r
}

// SetGlobalRecorder sets the global recorder (called by Init() in extra/redisotel-native)
func SetGlobalRecorder(r Recorder) {
	recorderMu.Lock()
	if r == nil {
		globalRecorder = noopRecorder{}
		operationDurationCallback = nil
		pipelineOperationDurationCallback = nil
		recorderMu.Unlock()
		// Unregister all pool metric callbacks atomically
		pool.SetAllMetricCallbacks(nil)
		return
	}
	globalRecorder = r

	// Register operation duration callbacks
	// These capture r directly since we want them to use the specific recorder
	// that was set at this point in time
	operationDurationCallback = func(ctx context.Context, duration time.Duration, cmd Cmder, attempts int, err error, cn *pool.Conn, dbIndex int) {
		getRecorder().RecordOperationDuration(ctx, duration, cmd, attempts, err, cn, dbIndex)
	}
	pipelineOperationDurationCallback = func(ctx context.Context, duration time.Duration, operationName string, cmdCount int, attempts int, err error, cn *pool.Conn, dbIndex int) {
		getRecorder().RecordPipelineOperationDuration(ctx, duration, operationName, cmdCount, attempts, err, cn, dbIndex)
	}
	recorderMu.Unlock()

	// Register all pool metric callbacks atomically
	// These use getRecorder() to safely access the current recorder
	pool.SetAllMetricCallbacks(&pool.MetricCallbacks{
		ConnectionCreateTime: func(ctx context.Context, duration time.Duration, cn *pool.Conn) {
			getRecorder().RecordConnectionCreateTime(ctx, duration, cn)
		},
		ConnectionRelaxedTimeout: func(ctx context.Context, delta int, cn *pool.Conn, poolName, notificationType string) {
			getRecorder().RecordConnectionRelaxedTimeout(ctx, delta, cn, poolName, notificationType)
		},
		ConnectionHandoff: func(ctx context.Context, cn *pool.Conn, poolName string) {
			getRecorder().RecordConnectionHandoff(ctx, cn, poolName)
		},
		Error: func(ctx context.Context, errorType string, cn *pool.Conn, statusCode string, isInternal bool, retryAttempts int) {
			getRecorder().RecordError(ctx, errorType, cn, statusCode, isInternal, retryAttempts)
		},
		MaintenanceNotification: func(ctx context.Context, cn *pool.Conn, notificationType string) {
			getRecorder().RecordMaintenanceNotification(ctx, cn, notificationType)
		},
		ConnectionWaitTime: func(ctx context.Context, duration time.Duration, cn *pool.Conn) {
			getRecorder().RecordConnectionWaitTime(ctx, duration, cn)
		},
		ConnectionClosed: func(ctx context.Context, cn *pool.Conn, reason string, err error) {
			getRecorder().RecordConnectionClosed(ctx, cn, reason, err)
		},
	})
}

// RecordOperationDuration records the total operation duration.
// dbIndex is the Redis database index (0-15).
func RecordOperationDuration(ctx context.Context, duration time.Duration, cmd Cmder, attempts int, err error, cn *pool.Conn, dbIndex int) {
	getRecorder().RecordOperationDuration(ctx, duration, cmd, attempts, err, cn, dbIndex)
}

// RecordPipelineOperationDuration records the total pipeline/transaction duration.
// This is called from redis.go after pipeline/transaction execution completes.
// operationName should be "PIPELINE" for regular pipelines or "MULTI" for transactions.
// err is the error from the pipeline execution (can be nil).
// dbIndex is the Redis database index (0-15).
func RecordPipelineOperationDuration(ctx context.Context, duration time.Duration, operationName string, cmdCount int, attempts int, err error, cn *pool.Conn, dbIndex int) {
	getRecorder().RecordPipelineOperationDuration(ctx, duration, operationName, cmdCount, attempts, err, cn, dbIndex)
}

// RecordConnectionCreateTime records the time it took to create a new connection.
func RecordConnectionCreateTime(ctx context.Context, duration time.Duration, cn *pool.Conn) {
	getRecorder().RecordConnectionCreateTime(ctx, duration, cn)
}

// RecordPubSubMessage records a Pub/Sub message sent or received.
func RecordPubSubMessage(ctx context.Context, cn *pool.Conn, direction, channel string, sharded bool) {
	getRecorder().RecordPubSubMessage(ctx, cn, direction, channel, sharded)
}

// RecordStreamLag records the lag between message creation and consumption in a stream.
func RecordStreamLag(ctx context.Context, lag time.Duration, cn *pool.Conn, streamName, consumerGroup, consumerName string) {
	getRecorder().RecordStreamLag(ctx, lag, cn, streamName, consumerGroup, consumerName)
}

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

// RegisterPools registers connection pools with the global recorder.
func RegisterPools(connPool pool.Pooler, pubSubPool PubSubPooler, addr string) {
	// Check if the global recorder implements PoolRegistrar
	if registrar, ok := globalRecorder.(PoolRegistrar); ok {
		// Generate a unique ID for this client's pools
		uniqueID := generateUniqueID()

		if connPool != nil {
			poolName := addr + "_" + uniqueID
			registrar.RegisterPool(poolName, connPool)
		}
		if pubSubPool != nil {
			poolName := addr + "_" + uniqueID + "_pubsub"
			registrar.RegisterPubSubPool(poolName, pubSubPool)
		}
	}
}

// UnregisterPools removes connection pools from the global recorder
func UnregisterPools(connPool pool.Pooler, pubSubPool PubSubPooler) {
	// Check if the global recorder implements PoolRegistrar
	if registrar, ok := globalRecorder.(PoolRegistrar); ok {
		if connPool != nil {
			registrar.UnregisterPool(connPool)
		}
		if pubSubPool != nil {
			registrar.UnregisterPubSubPool(pubSubPool)
		}
	}
}
