package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9/auth"
	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/pool"
	"github.com/redis/go-redis/v9/internal/proto"
	"github.com/redis/go-redis/v9/internal/util"
	"github.com/redis/go-redis/v9/maintnotifications"
	"github.com/redis/go-redis/v9/push"
)

// poolIDCounter is a global auto-increment counter for generating unique pool IDs.
var poolIDCounter atomic.Uint64

// generateUniqueID generates a short unique identifier for pool names using auto-increment.
// This makes it easier to identify and track pools in order of creation.
func generateUniqueID() string {
	id := poolIDCounter.Add(1)
	return strconv.FormatUint(id, 10)
}

// Limiter is the interface of a rate limiter or a circuit breaker.
type Limiter interface {
	// Allow returns nil if operation is allowed or an error otherwise.
	// If operation is allowed client must ReportResult of the operation
	// whether it is a success or a failure.
	Allow() error
	// ReportResult reports the result of the previously allowed operation.
	// nil indicates a success, non-nil error usually indicates a failure.
	ReportResult(result error)
}

// Options keeps the settings to set up redis connection.
type Options struct {
	// Network type, either tcp or unix.
	//
	// default: is tcp.
	Network string

	// Addr is the address formated as host:port
	Addr string

	// NodeAddress is the address of the Redis node as reported by the server.
	// For cluster clients, this is the exact endpoint string returned by CLUSTER SLOTS
	// before any resolution or transformation (e.g., loopback replacement).
	// For standalone clients, this defaults to Addr.
	//
	// This is used to match the source endpoint in maintenance notifications
	// (e.g. SMIGRATED).
	//
	// Use Client.NodeAddress() to access this value.
	NodeAddress string

	// ClientName will execute the `CLIENT SETNAME ClientName` command for each conn.
	ClientName string

	// Dialer creates new network connection and has priority over
	// Network and Addr options.
	Dialer func(ctx context.Context, network, addr string) (net.Conn, error)

	// Hook that is called when new connection is established.
	OnConnect func(ctx context.Context, cn *Conn) error

	// Protocol 2 or 3. Use the version to negotiate RESP version with redis-server.
	//
	// default: 3.
	Protocol int

	// Username is used to authenticate the current connection
	// with one of the connections defined in the ACL list when connecting
	// to a Redis 6.0 instance, or greater, that is using the Redis ACL system.
	Username string

	// Password is an optional password. Must match the password specified in the
	// `requirepass` server configuration option (if connecting to a Redis 5.0 instance, or lower),
	// or the User Password when connecting to a Redis 6.0 instance, or greater,
	// that is using the Redis ACL system.
	Password string

	// CredentialsProvider allows the username and password to be updated
	// before reconnecting. It should return the current username and password.
	CredentialsProvider func() (username string, password string)

	// CredentialsProviderContext is an enhanced parameter of CredentialsProvider,
	// done to maintain API compatibility. In the future,
	// there might be a merge between CredentialsProviderContext and CredentialsProvider.
	// There will be a conflict between them; if CredentialsProviderContext exists, we will ignore CredentialsProvider.
	CredentialsProviderContext func(ctx context.Context) (username string, password string, err error)

	// StreamingCredentialsProvider is used to retrieve the credentials
	// for the connection from an external source. Those credentials may change
	// during the connection lifetime. This is useful for managed identity
	// scenarios where the credentials are retrieved from an external source.
	//
	// Currently, this is a placeholder for the future implementation.
	StreamingCredentialsProvider auth.StreamingCredentialsProvider

	// DB is the database to be selected after connecting to the server.
	DB int

	// MaxRetries is the maximum number of retries before giving up.
	// -1 (not 0) disables retries.
	//
	// default: 3 retries
	MaxRetries int

	// MinRetryBackoff is the minimum backoff between each retry.
	// -1 disables backoff.
	//
	// default: 10 milliseconds
	MinRetryBackoff time.Duration

	// MaxRetryBackoff is the maximum backoff between each retry.
	// -1 disables backoff.
	// default: 1 second;
	MaxRetryBackoff time.Duration

	// DialTimeout for establishing new connections.
	//
	// default: 5 seconds
	DialTimeout time.Duration

	// DialerRetries is the maximum number of retry attempts when dialing fails.
	// A value <= 0 uses the default.
	//
	// default: 5
	DialerRetries int

	// DialerRetryTimeout is the backoff duration between retry attempts.
	//
	// default: 100 milliseconds
	DialerRetryTimeout time.Duration

	// DialerRetryBackoff controls the delay between dial retry attempts.
	//
	// attempt is 0-based: attempt=0 is the delay after the 1st failed dial (before the 2nd attempt).
	//
	// If nil, dial retry backoff is constant and equals DialerRetryTimeout (default: 100ms).
	DialerRetryBackoff func(attempt int) time.Duration

	// ReadTimeout for socket reads. If reached, commands will fail
	// with a timeout instead of blocking. Supported values:
	//
	//	- `-1` - no timeout (block indefinitely).
	//	- `-2` - disables SetReadDeadline calls completely.
	//
	// default: 5 seconds
	ReadTimeout time.Duration

	// WriteTimeout for socket writes. If reached, commands will fail
	// with a timeout instead of blocking.  Supported values:
	//
	//	- `-1` - no timeout (block indefinitely).
	//	- `-2` - disables SetWriteDeadline calls completely.
	//
	// default: 5 seconds (same as ReadTimeout, which it follows when unset)
	WriteTimeout time.Duration

	// ContextTimeoutEnabled controls whether the client respects context timeouts and deadlines.
	// See https://redis.uptrace.dev/guide/go-redis-debugging.html#timeouts
	ContextTimeoutEnabled bool

	// ReadBufferSize is the size of the bufio.Reader buffer for each connection.
	// Larger buffers can improve performance for commands that return large responses.
	// Smaller buffers can improve memory usage for larger pools.
	//
	// default: 32KiB (32768 bytes)
	ReadBufferSize int

	// WriteBufferSize is the size of the bufio.Writer buffer for each connection.
	// Larger buffers can improve performance for large pipelines and commands with many arguments.
	// Smaller buffers can improve memory usage for larger pools.
	//
	// default: 32KiB (32768 bytes)
	WriteBufferSize int

	// PipelineReadBufferSize is the size of the bufio.Reader buffer for pipeline
	// connections — the dedicated pipeline pool that serves Pipeline, AutoPipeline
	// and AsyncAutoPipeline. That pool always exists (see PipelinePoolSize); this
	// field only sizes its read buffers.
	//
	// This allows you to use large buffers for pipelining (to reduce syscalls and improve
	// throughput) while keeping regular command buffers small (to save memory).
	//
	// If not set (0), the pipeline pool's read buffer is the larger of
	// ReadBufferSize and DefaultPipelineBufferSize (128 KiB). The pipeline pool is
	// always created and a pipeline uses it whenever it has a free turn; when it
	// is saturated the pipeline spills to the regular pool without waiting (a
	// non-blocking TryGet), and that connection has the regular ReadBufferSize.
	// Size the pipeline pool (PipelinePoolSize) for the pipeline concurrency you
	// expect if every pipeline must get this buffer.
	//
	// Recommended: 64–128 KiB for high-throughput pipelining. The benefit here is
	// on the READ side: a batch's replies arrive as one large stream, and a bigger
	// buffer consumes them in fewer syscalls instead of refilling repeatedly
	// mid-batch. Size it to roughly the reply volume of a typical batch — which
	// for read-heavy pipelines is dominated by value sizes, not command count.
	// (The write-side counterpart, sizing to the outgoing wire bytes so the batch
	// flushes without overflowing mid-write, belongs to PipelineWriteBufferSize.)
	// Benchmarks show throughput climbs from the 32 KiB default up to ~64 KiB and
	// then plateaus; going beyond ~128 KiB gives no further gain and very large
	// buffers (≥512 KiB) can regress throughput and waste memory. Bigger is not
	// better.
	//
	// Example:
	//   client := redis.NewClient(&redis.Options{
	//       Addr:                    "localhost:6379",
	//       ReadBufferSize:          32 * 1024,   // 32 KiB for regular commands
	//       PipelineReadBufferSize:  128 * 1024,  // 128 KiB for pipelining
	//       PipelineWriteBufferSize: 128 * 1024,
	//   })
	//
	// Memory impact: With PoolSize=100 and PipelinePoolSize=10:
	//   - Raising ReadBufferSize to 128 KiB instead: 100 conns × 128 KiB = 12.8 MB
	//   - Leaving it at 32 KiB, pipeline pool at its 128 KiB default:
	//     (100 × 32 KiB) + (10 × 128 KiB) = 4.5 MB (~65% savings)
	//
	// default: 0 (the larger of ReadBufferSize and DefaultPipelineBufferSize)
	PipelineReadBufferSize int

	// PipelineWriteBufferSize is the size of the bufio.Writer buffer for pipeline
	// connections — the dedicated pipeline pool that serves Pipeline, AutoPipeline
	// and AsyncAutoPipeline. That pool always exists (see PipelinePoolSize); this
	// field only sizes its write buffers.
	//
	// This allows you to use large buffers for pipelining (to reduce syscalls and improve
	// throughput) while keeping regular command buffers small (to save memory).
	//
	// If not set (0), the pipeline pool's write buffer is the larger of
	// WriteBufferSize and DefaultPipelineBufferSize (128 KiB). As with the read
	// buffer, a pipeline that finds the pipeline pool saturated spills to the
	// regular pool without waiting and then writes through the regular
	// WriteBufferSize; size PipelinePoolSize for your pipeline concurrency if
	// every pipeline must get this buffer.
	//
	// Recommended: 64–128 KiB for high-throughput pipelining (size to roughly
	// MaxBatchSize × average-command-bytes). Throughput plateaus past ~64 KiB and
	// gains nothing beyond ~128 KiB; very large buffers (≥512 KiB) can regress it.
	// See PipelineReadBufferSize for the full rationale.
	//
	// default: 0 (the larger of WriteBufferSize and DefaultPipelineBufferSize)
	PipelineWriteBufferSize int

	// PipelinePoolSize is the pool size for the separate pipeline connection pool.
	// Setting this alone still sizes the (now always-created) dedicated pipeline
	// pool; its buffers default to the larger of the regular buffer size and
	// DefaultPipelineBufferSize (128 KiB), unless PipelineReadBufferSize /
	// PipelineWriteBufferSize are set.
	//
	// Pipelining typically needs fewer connections than regular operations because
	// batching reduces connection contention. A smaller pool saves memory while
	// maintaining high throughput.
	//
	// The dedicated pipeline pool is created unconditionally at NewClient —
	// like the pubsub pool — so pipelines never compete with regular commands
	// for main-pool connections. It never pre-dials (MinIdleConns is forced
	// to 0 on it), so the size is a cap on burst capacity, not a standing
	// footprint: an unused pipeline pool holds zero connections. A burst of
	// concurrent pipelines wider than the cap spills to the main pool IMMEDIATELY
	// (a non-blocking TryGet on the pipeline pool) rather than waiting a grace
	// period — so a saturated pipeline pool never adds latency before falling back,
	// and DefaultPipelinePoolTimeout does not gate that spill. Its connections use
	// DefaultPipelineBufferSize buffers unless
	// the pipeline buffer sizes are set explicitly. It does not inherit
	// MaxActiveConns: rather than the ~2x total ceiling that inheriting it
	// verbatim would allow, the pipeline pool adds at most PipelinePoolSize
	// connections on top of the main pool's MaxActiveConns (so the effective
	// ceiling is MaxActiveConns + PipelinePoolSize — a small, bounded addition),
	// and the main pool the burst spills to still enforces MaxActiveConns.
	//
	// Set to a negative value to opt out of the dedicated pool entirely:
	// pipelines then run on the main pool, as they did before the pool
	// existed.
	//
	// default: DefaultPipelinePoolSize (10) connections
	PipelinePoolSize int

	// AutoPipelineOptions is the default config for BOTH autopipeliner faces:
	// AutoPipeline and AsyncAutoPipeline use it when called without an
	// explicit config, falling back to their per-face defaults
	// (DefaultBlockingAutoPipelineOptions / DefaultAutoPipelineOptions) when it
	// is nil. Pass a config to either method to override. Commands issued
	// through an autopipeliner are batched into pipelines to cut round-trips
	// and raise throughput.
	//
	// EXPERIMENTAL: this API is subject to change, use with caution.
	AutoPipelineOptions *AutoPipelineOptions

	// PoolFIFO type of connection pool.
	//
	//	- true for FIFO pool
	//	- false for LIFO pool.
	//
	// Note that FIFO has slightly higher overhead compared to LIFO,
	// but it helps closing idle connections faster reducing the pool size.
	// default: false
	PoolFIFO bool

	// PoolSize is the base number of socket connections.
	// Default is 10 connections per every available CPU as reported by runtime.GOMAXPROCS.
	// If there is not enough connections in the pool, new connections will be allocated in excess of PoolSize,
	// you can limit it through MaxActiveConns
	//
	// default: 10 * runtime.GOMAXPROCS(0)
	PoolSize int

	// MaxConcurrentDials is the maximum number of concurrent connection creation goroutines.
	// If <= 0, defaults to PoolSize. If > PoolSize, it will be capped at PoolSize.
	MaxConcurrentDials int

	// maxConcurrentDialsSet records whether MaxConcurrentDials was set explicitly
	// by the caller (>0) BEFORE init() normalized it. init() rewrites a 0 to
	// PoolSize, which makes an explicit MaxConcurrentDials==PoolSize afterward
	// indistinguishable from the default; pipelinePoolOptions consults this to
	// preserve an explicit dial cap for the pipeline pool instead of expanding it.
	maxConcurrentDialsSet bool

	// maxConcurrentDialsInit latches maxConcurrentDialsSet on the first init().
	// A caller may reuse one *Options across more than one NewClient call. The
	// first init() normalizes an unset MaxConcurrentDials (0) to PoolSize, so a
	// second init() would recompute maxConcurrentDialsSet from the normalized value
	// and wrongly mark it explicit, which stops the pipeline pool from widening its
	// dial cap. The latch preserves the first decision. Clones copy both flags.
	maxConcurrentDialsInit bool

	// PoolTimeout is the amount of time client waits for connection if all connections
	// are busy before returning an error.
	//
	// default: ReadTimeout + 1 second
	PoolTimeout time.Duration

	// MinIdleConns is the minimum number of idle connections which is useful when establishing
	// new connection is slow. The idle connections are not closed by default.
	//
	// default: 0
	MinIdleConns int

	// MaxIdleConns is the maximum number of idle connections.
	// The idle connections are not closed by default.
	//
	// default: 0
	MaxIdleConns int

	// MaxActiveConns is the maximum number of connections allocated by the pool at a given time.
	// When zero, there is no limit on the number of connections in the pool.
	// If the pool is full, the next call to Get() will block until a connection is released.
	//
	// default: 0
	MaxActiveConns int

	// ConnMaxIdleTime is the maximum amount of time a connection may be idle.
	// Should be less than server's timeout.
	//
	// Expired connections may be closed lazily before reuse.
	// If d <= 0, connections are not closed due to a connection's idle time.
	// -1 disables idle timeout check.
	//
	// default: 30 minutes
	ConnMaxIdleTime time.Duration

	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	//
	// Expired connections may be closed lazily before reuse.
	// If <= 0, connections are not closed due to a connection's age.
	//
	// default: 0
	ConnMaxLifetime time.Duration

	// ConnMaxLifetimeJitter is the absolute jitter duration applied to ConnMaxLifetime
	// to prevent all connections from expiring simultaneously.
	//
	// The jitter is applied as a random offset in the range [-jitter, +jitter].
	// For example, if ConnMaxLifetime is 1 hour and ConnMaxLifetimeJitter is 6 minutes,
	// connections will expire between 54 minutes and 66 minutes.
	//
	// If <= 0, no jitter is applied.
	// If > ConnMaxLifetime, it will be capped at ConnMaxLifetime.
	//
	// default: 0
	ConnMaxLifetimeJitter time.Duration

	// TLSConfig to use. When set, TLS will be negotiated.
	TLSConfig *tls.Config

	// Limiter interface used to implement circuit breaker or rate limiter.
	Limiter Limiter

	// readOnly enables read only queries on slave/follower nodes.
	readOnly bool

	// DisableIndentity - Disable set-lib on connect.
	//
	// default: false
	//
	// Deprecated: Use DisableIdentity instead.
	DisableIndentity bool

	// DisableIdentity is used to disable CLIENT SETINFO command on connect.
	//
	// default: false
	DisableIdentity bool

	// Add suffix to client name. Default is empty.
	// IdentitySuffix - add suffix to client name.
	IdentitySuffix string

	// Deprecated: All RediSearch commands now have stable RESP3 parsing and this
	// flag is a no-op. It is kept for backwards compatibility and will be removed
	// in a future release.
	UnstableResp3 bool

	// Push notifications are always enabled for RESP3 connections (Protocol: 3)
	// and are not available for RESP2 connections. No configuration option is needed.

	// PushNotificationProcessor is the processor for handling push notifications.
	// If nil, a default processor will be created for RESP3 connections.
	// With client-side caching, a custom processor runs while an idle connection
	// is borrowed from the pool and should return promptly.
	PushNotificationProcessor push.NotificationProcessor

	// FailingTimeoutSeconds is the timeout in seconds for marking a cluster node as failing.
	// When a node is marked as failing, it will be avoided for this duration.
	// Default is 15 seconds.
	FailingTimeoutSeconds int

	// MaintNotificationsConfig provides custom configuration for maintnotifications.
	// When MaintNotificationsConfig.Mode is not "disabled", the client will handle
	// cluster upgrade notifications gracefully and manage connection/pool state
	// transitions seamlessly. Requires Protocol: 3 (RESP3) for push notifications.
	// If nil, maintnotifications are in "auto" mode and will be enabled if the server supports it.
	MaintNotificationsConfig *maintnotifications.Config

	// Client-side caching is eventually consistent. A cached value may be
	// stale until its invalidation arrives, and reads are not guaranteed to
	// be monotonic: a value observed through an uncached path can be newer
	// than one a subsequent cached read returns. Applications that require
	// monotonic reads must not rely on the cache for ordering.
	// ClientSideCacheConfig enables client-side caching when non-nil. Together
	// with ClientSideCache it is the on/off switch for the feature: leave both
	// nil to disable CSC, set either one to enable it. If ClientSideCache is also set, it
	// takes precedence over this config.
	//
	// Client-side caching is disabled when CredentialsProvider,
	// CredentialsProviderContext, or StreamingCredentialsProvider is set:
	// provider-backed credentials can change the ACL identity after the cache
	// namespace is selected. Fixed Username/Password values are supported and
	// included in the cache namespace.
	//
	// Invalidation freshness: a server invalidation is applied when the client
	// next reads from the connection that carries it — at arrival on a
	// full-duplex autopipeline connection, at the next command on an active
	// connection, and at the next background drainer tick on an idle pooled
	// connection. In every case a cached entry is never served past the cache's
	// MaxStaleness, which is the hard upper bound.
	//
	// Requires the built-in push processor. Setting a custom
	// PushNotificationProcessor together with CSC is not supported: the
	// invalidation mechanism relies on the built-in processor to consume
	// kernel-only readability and tolerate boundary read timeouts, so behavior is
	// otherwise undefined (the client logs a warning at init in that case).
	//
	// Experimental: this API may change in a minor release.
	ClientSideCacheConfig *ClientSideCacheConfig

	// ClientSideCache is an explicit Cache implementation used for client-side
	// caching. When set, it overrides ClientSideCacheConfig. Intended for
	// advanced users that want to share a cache across clients or supply a
	// custom implementation.
	//
	// A shared Cache is only safe across clients on the same server and DB.
	// Clients with different fixed Username/Password values are isolated by a
	// username namespace.
	// Client-side caching is restricted to DB 0 and disabled with a warning
	// otherwise. It is also disabled with any credential provider; see
	// ClientSideCacheConfig.
	//
	// Experimental: this API may change in a minor release.
	ClientSideCache Cache

	// ClientSideCacheStrategy selects the invalidation architecture used when
	// client-side caching is enabled (via ClientSideCacheConfig or
	// ClientSideCache); it is ignored when CSC is disabled. The zero value is
	// CSCStrategySharedTracking, currently the only implemented strategy.
	//
	// Experimental: this API may change in a minor release.
	ClientSideCacheStrategy CSCStrategy

	// ClientSideCacheRefreshOnInvalidate re-fetches every cached entry of an
	// invalidated key as soon as its invalidation arrives, instead of waiting for
	// a reader to miss. It does not look at how recently the entry was read, so
	// each invalidation of a cached key costs one background read: on a
	// write-heavy keyspace that is refetch traffic across the whole resident
	// cache, not only its hot part.
	//
	// Requires the built-in cache (ClientSideCacheConfig, or ClientSideCache set
	// to a *LocalCache), like the other CSC knobs: the refresher's hot-entry
	// collection and publish path are LocalCache-specific. With a custom Cache
	// implementation the option is ignored.
	//
	// Experimental: this API may change in a minor release.
	ClientSideCacheRefreshOnInvalidate bool

	// ClientSideCacheCoalesceMisses coalesces concurrent cache misses so they
	// stream on a held tracked full-duplex connection instead of each taking a
	// pool connection: a lone miss is written immediately (no batching delay —
	// the caller is waiting), concurrent misses share writes opportunistically,
	// and new misses go out while earlier replies are still in flight (~1 RTT
	// per miss, no batch phase-lock). Cuts pool contention and the churn p99
	// tail at a small pool.
	//
	// Requires the built-in cache (ClientSideCacheConfig, or ClientSideCache set
	// to a *LocalCache): the coalescer's publish path (fetch capture, refresh
	// integration, hot-entry collection) is LocalCache-specific. With a custom
	// Cache implementation the option is ignored and every miss fetches on its
	// caller's connection as usual.
	//
	// Pool sizing: under sustained miss traffic the engine holds one pool
	// connection (released after a 1s idle gap and at the recycle age). Size
	// PoolSize for that held connection — with PoolSize 1, continuous misses
	// can make unrelated non-cacheable commands wait on the pool.
	//
	// Limiter: a coalescer session holds ONE connection and serves MANY misses on
	// it, so Options.Limiter is admitted (Allow/ReportResult) once PER SESSION —
	// per held connection — not per coalesced miss, unlike the plain per-command
	// path. This is inherent to the held-connection model (a per-miss Allow would
	// defeat the coalescing and re-admit a connection already held). A caller that
	// needs strict per-command admission or circuit-breaking should not enable
	// miss coalescing.
	//
	// Experimental: this API may change in a minor release.
	ClientSideCacheCoalesceMisses bool

	// ClientSideCacheInvalidationBatchWindow coalesces invalidation-driven cache
	// deletes into windowed background batches instead of applying them inline on
	// the connection reader. 0 (default) applies invalidations inline. Set it no
	// larger than the cache MaxStaleness: deferring a delete by up to the window
	// lets a reader see the pre-invalidation value for up to that long.
	//
	// Deferring a delete also defers ORDERING. A caller that reads the key
	// through a non-cached path -- for example when a miss is shed to the
	// pooled path -- can observe a newer value than the one a concurrent
	// in-flight fetch is about to publish, and a later cached read can then
	// return the older value. Client-side caching is eventually consistent
	// and does not guarantee monotonic reads; the window widens that envelope,
	// because a published value stays readable until its invalidation is
	// APPLIED rather than observed.
	//
	// Requires the built-in cache (ClientSideCacheConfig, or ClientSideCache set
	// to a *LocalCache), like ClientSideCacheCoalesceMisses: the batcher's
	// hot-entry refresh integration is LocalCache-specific. With a custom Cache
	// implementation the window is ignored and deletes apply inline.
	//
	// Experimental: this API may change in a minor release.
	ClientSideCacheInvalidationBatchWindow time.Duration
}

// CSCStrategy selects the client-side caching invalidation architecture. Set via
// Options.ClientSideCacheStrategy; fixed for the client's lifetime.
//
// CSCStrategySharedTracking is currently the only implemented strategy; the type
// exists as an extension point for additional architectures (e.g. a BCAST sidecar)
// without a breaking API change.
//
// Experimental: this API may change in a minor release.
type CSCStrategy int

const (
	// CSCStrategySharedTracking (default, the zero value): one shared cache; every
	// pool connection runs plain CLIENT TRACKING ON and a background drainer applies
	// buffered invalidations. Portable (no BCAST), and matches the other Redis clients.
	CSCStrategySharedTracking CSCStrategy = iota
)

// DefaultPipelinePoolSize is the pipeline pool size used when
// PipelinePoolSize is not set. Pipelining batches many commands per round
// trip, so it needs far fewer connections than regular traffic. The pool is
// pure burst capacity: it never pre-dials idle connections (MinIdleConns is
// forced to 0 on it), so an unused pipeline pool holds no connections at all
// and the size is only a cap — bursts wider than it spill to the main pool.
const DefaultPipelinePoolSize = 10

// DefaultPipelineBufferSize is the per-connection read/write buffer size for
// the dedicated pipeline pool when no explicit pipeline buffer size is set
// (the larger of this and the regular buffer size is used). Pipeline
// connections move whole batches per round trip, so they earn bigger buffers
// than regular per-command traffic: measured on the autopipeline engine,
// throughput plateaus around 64 KiB and gains nothing past ~128 KiB for
// TYPICAL (small-command) traffic, while very large buffers (>=512 KiB) can
// regress it.
//
// Set to 128 KiB anyway: the extra headroom is not about typical-traffic
// throughput but about full-duplex's large-payload backpressure guardrail
// (see MaxBatchBytes's default) — more bufio headroom before a write() to a
// slow-draining peer blocks widens the margin before that guardrail's cap is
// reached. The aggregate memory cost stays negligible because the pool this
// backs holds few connections: the dedicated pipeline pool never pre-dials
// (DefaultPipelinePoolSize, MinIdleConns forced to 0), and full-duplex holds
// exactly one connection per node regardless of pool size.
const DefaultPipelineBufferSize = 128 * 1024

// DefaultPipelinePoolTimeout is the dedicated pipeline pool's PoolTimeout
// (pipelinePoolOptions caps the pipeline clone's PoolTimeout at this value but honors
// a caller's SHORTER PoolTimeout). It does NOT gate the spill to the main pool:
// withPipelineConn acquires with a non-blocking TryGet, so a burst wider than the
// pipeline pool's cap spills to the main pool IMMEDIATELY (see the pipeline-pool note
// in Options and withPipelineConn), never waiting this timeout, and the spilled op
// then uses the MAIN pool's own PoolTimeout, not this one.
//
// It currently has NO other live effect either. Every acquisition against the
// dedicated pipeline pool — withPipelineConn's per-round-trip borrow above, and
// the full-duplex engine's own session lease (autopipeline_fullduplex.go) — uses
// TryGet, never the blocking Get. TryGet's non-wait branch returns ErrPoolTryFull
// at once for BOTH a saturated pool turn and an active maintnotifications drainer
// claim, before waitForDrainer or this deadline is ever consulted (see
// ConnPool.getConn/waitTurn in internal/pool). A previous version of this doc
// claimed a residual drainer-handoff budget; that was inaccurate (codex on #4002)
// — there is no code path that currently waits out this value. It is kept short
// anyway in case a future acquisition path, or a caller that obtains the pool via
// getPipelinePool's exported pool.Pooler interface, uses the blocking Get.
const DefaultPipelinePoolTimeout = 100 * time.Millisecond

func (opt *Options) init() {
	if opt.Addr == "" {
		opt.Addr = "localhost:6379"
	}
	// An unknown strategy would thread the CSC gates inconsistently (e.g. tracking
	// on with no drainer), serving stale data. Clamp to the only supported value.
	switch opt.ClientSideCacheStrategy {
	case CSCStrategySharedTracking:
	default:
		internal.Logger.Printf(context.Background(),
			"redis: unknown ClientSideCacheStrategy %d; falling back to CSCStrategySharedTracking",
			opt.ClientSideCacheStrategy)
		opt.ClientSideCacheStrategy = CSCStrategySharedTracking
	}
	// Deferring invalidation deletes by more than the cache's MaxStaleness lets a
	// reader see a value past the point a received invalidation should have evicted
	// it — beyond the staleness contract. Warn (not clamp): the field is
	// experimental and a caller may accept it knowingly, but a window larger than
	// MaxStaleness is almost always a misconfiguration. Checkable for the built-in
	// cache via its config AND for an injected *LocalCache via its effective bound;
	// a custom Cache implementation has no MaxStaleness to compare against.
	if opt.ClientSideCacheInvalidationBatchWindow > 0 {
		staleness := time.Duration(0)
		if opt.ClientSideCacheConfig != nil {
			staleness = opt.ClientSideCacheConfig.MaxStaleness
		} else if lc, ok := opt.ClientSideCache.(*LocalCache); ok && lc != nil {
			staleness = lc.effectiveMaxStaleness()
		}
		if staleness > 0 && opt.ClientSideCacheInvalidationBatchWindow > staleness {
			internal.Logger.Printf(context.Background(),
				"redis: ClientSideCacheInvalidationBatchWindow (%s) exceeds the cache MaxStaleness (%s); "+
					"invalidations can be deferred past the staleness bound, serving stale values",
				opt.ClientSideCacheInvalidationBatchWindow, staleness)
		}
	}
	if opt.Network == "" {
		if strings.HasPrefix(opt.Addr, "/") {
			opt.Network = "unix"
		} else {
			opt.Network = "tcp"
		}
	}
	// For standalone clients, default NodeAddress to Addr if not set.
	// This ensures maintenance notifications (SMIGRATED, etc.) can match
	// the connection's endpoint even for non-cluster clients.
	if opt.NodeAddress == "" {
		opt.NodeAddress = opt.Addr
	}
	if opt.Protocol < 2 {
		opt.Protocol = 3
	}
	if opt.DialTimeout == 0 {
		opt.DialTimeout = 5 * time.Second
	}
	// <= 0, not == 0: the pool already treats a nonpositive value as the default
	// (internal/pool ConnPool.dialConn), so normalize here too — code that
	// derives a budget from opt.DialerRetries (the miss-coalescer's acquire
	// deadline) must see the same count the pool will actually use.
	if opt.DialerRetries <= 0 {
		opt.DialerRetries = 5
	}
	if opt.DialerRetryTimeout == 0 {
		opt.DialerRetryTimeout = 100 * time.Millisecond
	}
	if opt.Dialer == nil {
		opt.Dialer = NewDialer(opt)
	}
	if opt.PoolSize == 0 {
		opt.PoolSize = 10 * runtime.GOMAXPROCS(0)
	}

	// Record explicit-vs-default BEFORE normalizing (a 0 becomes PoolSize below),
	// so pipelinePoolOptions can tell an explicit MaxConcurrentDials==PoolSize from
	// the default. Latch on the FIRST init only: a caller may reuse one *Options
	// across NewClient calls, and a second init() would see the already-normalized
	// value and wrongly mark it explicit. Clones copy the latched flags.
	if !opt.maxConcurrentDialsInit {
		opt.maxConcurrentDialsSet = opt.MaxConcurrentDials > 0
		opt.maxConcurrentDialsInit = true
	}
	if opt.MaxConcurrentDials <= 0 {
		opt.MaxConcurrentDials = opt.PoolSize
	} else if opt.MaxConcurrentDials > opt.PoolSize {
		opt.MaxConcurrentDials = opt.PoolSize
	}
	if opt.ReadBufferSize == 0 {
		opt.ReadBufferSize = proto.DefaultBufferSize
	} else if opt.Protocol == 3 && opt.ReadBufferSize < proto.MinRESP3ReadBufferSize {
		// Too small to hold a push header, the processor would consume frames before
		// knowing their name and could swallow a Pub/Sub frame. Clamp to the minimum.
		internal.Logger.Printf(context.Background(),
			"redis: ReadBufferSize=%d is below the RESP3 minimum %d; clamping.",
			opt.ReadBufferSize, proto.MinRESP3ReadBufferSize)
		opt.ReadBufferSize = proto.MinRESP3ReadBufferSize
	}
	if opt.WriteBufferSize == 0 {
		opt.WriteBufferSize = proto.DefaultBufferSize
	}
	switch opt.ReadTimeout {
	case -2:
		opt.ReadTimeout = -1
	case -1:
		opt.ReadTimeout = 0
	case 0:
		opt.ReadTimeout = 5 * time.Second
	}
	switch opt.WriteTimeout {
	case -2:
		opt.WriteTimeout = -1
	case -1:
		opt.WriteTimeout = 0
	case 0:
		opt.WriteTimeout = opt.ReadTimeout
	}
	if opt.PoolTimeout == 0 {
		if opt.ReadTimeout > 0 {
			opt.PoolTimeout = opt.ReadTimeout + time.Second
		} else {
			opt.PoolTimeout = 30 * time.Second
		}
	}
	if opt.ConnMaxIdleTime == 0 {
		opt.ConnMaxIdleTime = 30 * time.Minute
	}

	opt.ConnMaxLifetimeJitter = min(opt.ConnMaxLifetimeJitter, opt.ConnMaxLifetime)

	switch opt.MaxRetries {
	case -1:
		opt.MaxRetries = 0
	case 0:
		opt.MaxRetries = 3
	}
	switch opt.MinRetryBackoff {
	case -1:
		opt.MinRetryBackoff = 0
	case 0:
		opt.MinRetryBackoff = 10 * time.Millisecond
	}
	switch opt.MaxRetryBackoff {
	case -1:
		opt.MaxRetryBackoff = 0
	case 0:
		opt.MaxRetryBackoff = time.Second
	}

	if opt.FailingTimeoutSeconds == 0 {
		opt.FailingTimeoutSeconds = 15
	}

	if opt.Protocol == 2 && (opt.ClientSideCache != nil || opt.ClientSideCacheConfig != nil) {
		internal.Logger.Printf(context.Background(),
			"redis: client-side caching requires Protocol: 3 (RESP3); caching is disabled")
	}

	// Maintnotifications defaults (handoff workers, queue depth) must cover
	// every pool the manager hooks, not just the main one: the dedicated
	// pipeline pool adds up to PipelinePoolSize connections whose handoffs run
	// through hooks sized from this config (see enableMaintNotificationsUpgrades),
	// so derive the defaults from the combined connection ceiling.
	maintPoolSize := opt.PoolSize
	maintMaxActive := opt.MaxActiveConns
	if opt.PipelinePoolSize >= 0 {
		pps := opt.PipelinePoolSize
		if pps == 0 {
			pps = DefaultPipelinePoolSize
		}
		maintPoolSize += pps
		if maintMaxActive > 0 {
			// The pipeline pool sits outside MaxActiveConns; its ceiling is
			// MaxActiveConns + PipelinePoolSize (see the PipelinePoolSize doc).
			maintMaxActive += pps
		}
	}
	opt.MaintNotificationsConfig = opt.MaintNotificationsConfig.ApplyDefaultsWithPoolConfig(maintPoolSize, maintMaxActive)

	// skip endpoint detection when maint notifications are disabled.
	if opt.MaintNotificationsConfig.Mode != maintnotifications.ModeDisabled {
		endpointType := opt.MaintNotificationsConfig.EndpointType
		// auto-detect endpoint type if not specified
		if endpointType == "" || endpointType == maintnotifications.EndpointTypeAuto {
			endpointType = maintnotifications.DetectEndpointType(opt.Addr, opt.TLSConfig != nil)
		}
		opt.MaintNotificationsConfig.EndpointType = endpointType
	}
}

func (opt *Options) clone() *Options {
	clone := *opt

	// Deep clone MaintNotificationsConfig to avoid sharing between clients
	if opt.MaintNotificationsConfig != nil {
		configClone := *opt.MaintNotificationsConfig
		clone.MaintNotificationsConfig = &configClone
	}

	return &clone
}

// NewDialer returns a function that will be used as the default dialer
// when none is specified in Options.Dialer.
func (opt *Options) NewDialer() func(context.Context, string, string) (net.Conn, error) {
	return NewDialer(opt)
}

// defaultKeepAliveConfig is the TCP keep-alive policy of the default dialers
// here and in sentinel.go: start probing after 30s idle (below typical LB/NAT
// idle timeouts), then declare the peer dead after 3 unanswered probes 5s
// apart.
var defaultKeepAliveConfig = net.KeepAliveConfig{
	Enable:   true,
	Idle:     30 * time.Second,
	Interval: 5 * time.Second,
	Count:    3,
}

// NewDialer returns a function that will be used as the default dialer
// when none is specified in Options.Dialer.
func NewDialer(opt *Options) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		netDialer := &net.Dialer{
			Timeout:         opt.DialTimeout,
			KeepAliveConfig: defaultKeepAliveConfig,
		}
		if opt.TLSConfig == nil {
			return netDialer.DialContext(ctx, network, addr)
		}
		return tls.DialWithDialer(netDialer, network, addr, opt.TLSConfig)
	}
}

// ParseURL parses a URL into Options that can be used to connect to Redis.
// Scheme is required.
// There are two connection types: by tcp socket and by unix socket.
// Tcp connection:
//
//	redis://<user>:<password>@<host>:<port>/<db_number>
//
// Unix connection:
//
//	unix://<user>:<password>@</path/to/redis.sock>?db=<db_number>
//
// Most Option fields can be set using query parameters, with the following restrictions:
//   - field names are mapped using snake-case conversion: to set MaxRetries, use max_retries
//   - only scalar type fields are supported (bool, int, time.Duration)
//   - for time.Duration fields, values must be a valid input for time.ParseDuration();
//     additionally a plain integer as value (i.e. without unit) is interpreted as seconds
//   - to disable a duration field, use value less than or equal to 0; to use the default
//     value, leave the value blank or remove the parameter
//   - only the last value is interpreted if a parameter is given multiple times
//   - fields "network", "addr", "username" and "password" can only be set using other
//     URL attributes (scheme, host, userinfo, resp.), query parameters using these
//     names will be treated as unknown parameters
//   - unknown parameter names will result in an error
//   - use "skip_verify=true" to ignore TLS certificate validation
//
// Examples:
//
//	redis://user:password@localhost:6789/3?dial_timeout=3&db=1&read_timeout=6s&max_retries=2
//	is equivalent to:
//	&Options{
//		Network:     "tcp",
//		Addr:        "localhost:6789",
//		DB:          1,               // path "/3" was overridden by "&db=1"
//		DialTimeout: 3 * time.Second, // no time unit = seconds
//		ReadTimeout: 6 * time.Second,
//		MaxRetries:  2,
//	}
func ParseURL(redisURL string) (*Options, error) {
	u, err := url.Parse(redisURL)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "redis", "rediss":
		return setupTCPConn(u)
	case "unix":
		return setupUnixConn(u)
	default:
		return nil, fmt.Errorf("redis: invalid URL scheme: %s", u.Scheme)
	}
}

func setupTCPConn(u *url.URL) (*Options, error) {
	o := &Options{Network: "tcp"}

	o.Username, o.Password = getUserPassword(u)

	h, p := getHostPortWithDefaults(u)
	o.Addr = net.JoinHostPort(h, p)

	f := strings.FieldsFunc(u.Path, func(r rune) bool {
		return r == '/'
	})
	switch len(f) {
	case 0:
		o.DB = 0
	case 1:
		var err error
		if o.DB, err = strconv.Atoi(f[0]); err != nil {
			return nil, fmt.Errorf("redis: invalid database number: %q", f[0])
		}
	default:
		return nil, fmt.Errorf("redis: invalid URL path: %s", u.Path)
	}

	if u.Scheme == "rediss" {
		o.TLSConfig = &tls.Config{
			ServerName: h,
			MinVersion: tls.VersionTLS12,
		}
	}

	return setupConnParams(u, o)
}

// getHostPortWithDefaults is a helper function that splits the url into
// a host and a port. If the host is missing, it defaults to localhost
// and if the port is missing, it defaults to 6379.
func getHostPortWithDefaults(u *url.URL) (string, string) {
	// u.Hostname and u.Port strip the surrounding brackets from IPv6 literals
	// (e.g. "[::1]" -> "::1") and handle the missing-port case, which
	// net.SplitHostPort instead reports as an error. Relying on them avoids
	// leaving the brackets on the host, which the caller's net.JoinHostPort
	// would wrap again and turn "redis://[::1]" into "[[::1]]:6379".
	host, port := u.Hostname(), u.Port()
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "6379"
	}
	return host, port
}

func setupUnixConn(u *url.URL) (*Options, error) {
	o := &Options{
		Network: "unix",
	}

	if strings.TrimSpace(u.Path) == "" { // path is required with unix connection
		return nil, errors.New("redis: empty unix socket path")
	}
	o.Addr = u.Path
	o.Username, o.Password = getUserPassword(u)
	return setupConnParams(u, o)
}

type queryOptions struct {
	q   url.Values
	err error
}

func (o *queryOptions) has(name string) bool {
	return len(o.q[name]) > 0
}

func (o *queryOptions) string(name string) string {
	vs := o.q[name]
	if len(vs) == 0 {
		return ""
	}
	delete(o.q, name) // enable detection of unknown parameters
	return vs[len(vs)-1]
}

func (o *queryOptions) strings(name string) []string {
	vs := o.q[name]
	delete(o.q, name)
	return vs
}

func (o *queryOptions) int(name string) int {
	s := o.string(name)
	if s == "" {
		return 0
	}
	i, err := strconv.Atoi(s)
	if err == nil {
		return i
	}
	if o.err == nil {
		o.err = fmt.Errorf("redis: invalid %s number: %s", name, err)
	}
	return 0
}

func (o *queryOptions) duration(name string) time.Duration {
	s := o.string(name)
	if s == "" {
		return 0
	}
	// try plain number first
	if i, err := strconv.Atoi(s); err == nil {
		if i <= 0 {
			// disable timeouts
			return -1
		}
		return time.Duration(i) * time.Second
	}
	dur, err := time.ParseDuration(s)
	if err == nil {
		if dur <= 0 {
			// disable timeouts
			return -1
		}
		return dur
	}
	if o.err == nil {
		o.err = fmt.Errorf("redis: invalid %s duration: %w", name, err)
	}
	return 0
}

func (o *queryOptions) bool(name string) bool {
	switch s := o.string(name); s {
	case "true", "1":
		return true
	case "false", "0", "":
		return false
	default:
		if o.err == nil {
			o.err = fmt.Errorf("redis: invalid %s boolean: expected true/false/1/0 or an empty string, got %q", name, s)
		}
		return false
	}
}

func (o *queryOptions) remaining() []string {
	if len(o.q) == 0 {
		return nil
	}
	keys := slices.Collect(maps.Keys(o.q))
	slices.Sort(keys)
	return keys
}

// setupConnParams converts query parameters in u to option value in o.
func setupConnParams(u *url.URL, o *Options) (*Options, error) {
	q := queryOptions{q: u.Query()}

	// compat: a future major release may use q.int("db")
	if tmp := q.string("db"); tmp != "" {
		db, err := strconv.Atoi(tmp)
		if err != nil {
			return nil, fmt.Errorf("redis: invalid database number: %w", err)
		}
		o.DB = db
	}

	o.Protocol = q.int("protocol")
	o.ClientName = q.string("client_name")
	o.MaxRetries = q.int("max_retries")
	o.MinRetryBackoff = q.duration("min_retry_backoff")
	o.MaxRetryBackoff = q.duration("max_retry_backoff")
	o.DialTimeout = q.duration("dial_timeout")
	o.ReadTimeout = q.duration("read_timeout")
	o.WriteTimeout = q.duration("write_timeout")
	o.PoolFIFO = q.bool("pool_fifo")
	o.PoolSize = q.int("pool_size")
	o.PoolTimeout = q.duration("pool_timeout")
	o.MinIdleConns = q.int("min_idle_conns")
	o.MaxIdleConns = q.int("max_idle_conns")
	o.MaxActiveConns = q.int("max_active_conns")
	o.MaxConcurrentDials = q.int("max_concurrent_dials")
	// Pipeline pool (created by default): allow URL-configured clients to opt out
	// (pipeline_pool_size=-1) or tune it, otherwise these would be rejected as
	// unexpected options. q.int accepts a negative value.
	o.PipelinePoolSize = q.int("pipeline_pool_size")
	o.PipelineReadBufferSize = q.int("pipeline_read_buffer_size")
	o.PipelineWriteBufferSize = q.int("pipeline_write_buffer_size")
	if q.has("conn_max_idle_time") {
		o.ConnMaxIdleTime = q.duration("conn_max_idle_time")
	} else {
		o.ConnMaxIdleTime = q.duration("idle_timeout")
	}
	if q.has("conn_max_lifetime") {
		o.ConnMaxLifetime = q.duration("conn_max_lifetime")
	} else {
		o.ConnMaxLifetime = q.duration("max_conn_age")
	}
	if q.has("conn_max_lifetime_jitter") {
		o.ConnMaxLifetimeJitter = min(q.duration("conn_max_lifetime_jitter"), o.ConnMaxLifetime)
	}
	if o.TLSConfig != nil && q.has("skip_verify") {
		o.TLSConfig.InsecureSkipVerify = q.bool("skip_verify")
	}
	if q.err != nil {
		return nil, q.err
	}

	// any parameters left?
	if r := q.remaining(); len(r) > 0 {
		return nil, fmt.Errorf("redis: unexpected option: %s", strings.Join(r, ", "))
	}

	return o, nil
}

func getUserPassword(u *url.URL) (string, string) {
	var user, password string
	if u.User != nil {
		user = u.User.Username()
		if p, ok := u.User.Password(); ok {
			password = p
		}
	}
	return user, password
}

func newConnPool(
	opt *Options,
	dialer func(ctx context.Context, network, addr string) (net.Conn, error),
	poolName string,
) (*pool.ConnPool, error) {
	poolSize, err := util.SafeIntToInt32(opt.PoolSize, "PoolSize")
	if err != nil {
		return nil, err
	}

	minIdleConns, err := util.SafeIntToInt32(opt.MinIdleConns, "MinIdleConns")
	if err != nil {
		return nil, err
	}

	maxIdleConns, err := util.SafeIntToInt32(opt.MaxIdleConns, "MaxIdleConns")
	if err != nil {
		return nil, err
	}

	maxActiveConns, err := util.SafeIntToInt32(opt.MaxActiveConns, "MaxActiveConns")
	if err != nil {
		return nil, err
	}

	return pool.NewConnPool(&pool.Options{
		Dialer: func(ctx context.Context) (net.Conn, error) {
			return dialer(ctx, opt.Network, opt.Addr)
		},
		PoolFIFO:                 opt.PoolFIFO,
		PoolSize:                 poolSize,
		MaxConcurrentDials:       opt.MaxConcurrentDials,
		PoolTimeout:              opt.PoolTimeout,
		DialTimeout:              opt.DialTimeout,
		DialerRetries:            opt.DialerRetries,
		DialerRetryTimeout:       opt.DialerRetryTimeout,
		DialerRetryBackoff:       opt.DialerRetryBackoff,
		MinIdleConns:             minIdleConns,
		MaxIdleConns:             maxIdleConns,
		MaxActiveConns:           maxActiveConns,
		ConnMaxIdleTime:          opt.ConnMaxIdleTime,
		ConnMaxLifetime:          opt.ConnMaxLifetime,
		ConnMaxLifetimeJitter:    opt.ConnMaxLifetimeJitter,
		ReadBufferSize:           opt.ReadBufferSize,
		WriteBufferSize:          opt.WriteBufferSize,
		PushNotificationsEnabled: opt.Protocol == 3,
		Name:                     poolName,
	}), nil
}

func newPubSubPool(
	opt *Options,
	dialer func(ctx context.Context, network, addr string) (net.Conn, error),
	poolName string,
) (*pool.PubSubPool, error) {
	poolSize, err := util.SafeIntToInt32(opt.PoolSize, "PoolSize")
	if err != nil {
		return nil, err
	}

	minIdleConns, err := util.SafeIntToInt32(opt.MinIdleConns, "MinIdleConns")
	if err != nil {
		return nil, err
	}

	maxIdleConns, err := util.SafeIntToInt32(opt.MaxIdleConns, "MaxIdleConns")
	if err != nil {
		return nil, err
	}

	maxActiveConns, err := util.SafeIntToInt32(opt.MaxActiveConns, "MaxActiveConns")
	if err != nil {
		return nil, err
	}

	return pool.NewPubSubPool(&pool.Options{
		PoolFIFO:                 opt.PoolFIFO,
		PoolSize:                 poolSize,
		MaxConcurrentDials:       opt.MaxConcurrentDials,
		PoolTimeout:              opt.PoolTimeout,
		DialTimeout:              opt.DialTimeout,
		DialerRetries:            opt.DialerRetries,
		DialerRetryTimeout:       opt.DialerRetryTimeout,
		DialerRetryBackoff:       opt.DialerRetryBackoff,
		MinIdleConns:             minIdleConns,
		MaxIdleConns:             maxIdleConns,
		MaxActiveConns:           maxActiveConns,
		ConnMaxIdleTime:          opt.ConnMaxIdleTime,
		ConnMaxLifetime:          opt.ConnMaxLifetime,
		ConnMaxLifetimeJitter:    opt.ConnMaxLifetimeJitter,
		ReadBufferSize:           32 * 1024,
		WriteBufferSize:          32 * 1024,
		PushNotificationsEnabled: opt.Protocol == 3,
		Name:                     poolName,
	}, dialer), nil
}
