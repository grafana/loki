# Redis client for Go

[![build workflow](https://github.com/redis/go-redis/actions/workflows/build.yml/badge.svg)](https://github.com/redis/go-redis/actions)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/redis/go-redis/v9)](https://pkg.go.dev/github.com/redis/go-redis/v9?tab=doc)
[![Documentation](https://img.shields.io/badge/redis-documentation-informational)](https://redis.io/docs/latest/develop/clients/go/)
[![Go Report Card](https://goreportcard.com/badge/github.com/redis/go-redis/v9)](https://goreportcard.com/report/github.com/redis/go-redis/v9)
[![codecov](https://codecov.io/github/redis/go-redis/graph/badge.svg?token=tsrCZKuSSw)](https://codecov.io/github/redis/go-redis)

[![Discord](https://img.shields.io/discord/697882427875393627.svg?style=social&logo=discord)](https://discord.gg/W4txy5AeKM)
[![Twitch](https://img.shields.io/twitch/status/redisinc?style=social)](https://www.twitch.tv/redisinc)
[![YouTube](https://img.shields.io/youtube/channel/views/UCD78lHSwYqMlyetR0_P4Vig?style=social)](https://www.youtube.com/redisinc)
[![Twitter](https://img.shields.io/twitter/follow/redisinc?style=social)](https://twitter.com/redisinc)
[![Stack Exchange questions](https://img.shields.io/stackexchange/stackoverflow/t/go-redis?style=social&logo=stackoverflow&label=Stackoverflow)](https://stackoverflow.com/questions/tagged/go-redis)

> go-redis is the official Redis client library for the Go programming language. It offers a straightforward interface for interacting with Redis servers. 

## Supported versions

In `go-redis` we are aiming to support the last three releases of Redis. Currently, this means we do support:
- [Redis 8.0](https://raw.githubusercontent.com/redis/redis/8.0/00-RELEASENOTES) - using Redis CE 8.0
- [Redis 8.2](https://raw.githubusercontent.com/redis/redis/8.2/00-RELEASENOTES) - using Redis CE 8.2
- [Redis 8.4](https://raw.githubusercontent.com/redis/redis/8.4/00-RELEASENOTES) - using Redis CE 8.4
- [Redis 8.6](https://raw.githubusercontent.com/redis/redis/8.6/00-RELEASENOTES) - using Redis CE 8.6
- [Redis 8.8](https://raw.githubusercontent.com/redis/redis/8.8/00-RELEASENOTES) - using Redis CE 8.8
- [Redis 8.10](https://raw.githubusercontent.com/redis/redis/8.10/00-RELEASENOTES) - using Redis CE 8.10
- [Redis 8.12](https://raw.githubusercontent.com/redis/redis/8.12/00-RELEASENOTES) - using Redis CE 8.12

Although the `go.mod` states it requires at minimum `go 1.26`, our CI is configured to run the tests against all supported
versions of Redis and multiple versions of Go ([1.26](https://go.dev/doc/devel/release#go1.26.0), oldstable, and stable). We observe that some modules related test may not pass with
Redis Stack 7.2 and some commands are changed with Redis CE 8.0.
Although it is not officially supported, `go-redis/v9`  should be able to work with any Redis 7.0+.
Please do refer to the documentation and the tests if you experience any issues.

### Array data type (Redis 8.8+)

Starting with Redis 8.8, go-redis exposes the new array data type via the `AR*` command family
(`ARSET`, `ARGET`, `ARGETRANGE`, `ARMSET`, `ARMGET`, `ARINSERT`, `ARDEL`, `ARDELRANGE`,
`ARLEN`, `ARCOUNT`, `ARNEXT`, `ARSEEK`, `ARSCAN`, `ARGREP`, `ARRING`, `ARLASTITEMS`,
`ARINFO`/`ARINFOFULL`, and the `AROP*` reducers). See `array_commands.go` for the full
surface. The API is experimental and may change in a future release.

## How do I Redis?

[Learn for free at Redis University](https://university.redis.com/)

[Build faster with the Redis Launchpad](https://launchpad.redis.com/)

[Try the Redis Cloud](https://redis.com/try-free/)

[Dive in developer tutorials](https://developer.redis.com/)

[Join the Redis community](https://redis.com/community/)

[Work at Redis](https://redis.com/company/careers/jobs/)


## Resources

- [Discussions](https://github.com/redis/go-redis/discussions)
- [Chat](https://discord.gg/W4txy5AeKM)
- [Reference](https://pkg.go.dev/github.com/redis/go-redis/v9)
- [Examples](https://pkg.go.dev/github.com/redis/go-redis/v9#pkg-examples)
- [Release notes](./RELEASE-NOTES.md) ([GitHub Releases](https://github.com/redis/go-redis/releases))

## old documentation

- [English](https://redis.uptrace.dev)
- [简体中文](https://redis.uptrace.dev/zh/)

## Ecosystem

- [Entra ID (Azure AD)](https://github.com/redis/go-redis-entraid)
- [Distributed Locks](https://github.com/bsm/redislock)
- [Redis Cache](https://github.com/go-redis/cache)
- [Rate limiting](https://github.com/go-redis/redis_rate)

## Features

- Redis commands except QUIT and SYNC.
- Automatic connection pooling.
- [StreamingCredentialsProvider (e.g. entra id, oauth)](#1-streaming-credentials-provider-highest-priority)
- [Pub/Sub](https://redis.uptrace.dev/guide/go-redis-pubsub.html).
- [Pipelines and transactions](https://redis.uptrace.dev/guide/go-redis-pipelines.html).
- [Automatic pipelining](#automatic-pipelining) (experimental) — batches concurrent
  commands into pipelines for you; meant for high-throughput / high-load / scale
  use cases.
- [Scripting](https://redis.uptrace.dev/guide/lua-scripting.html).
- [Redis Sentinel](https://redis.uptrace.dev/guide/go-redis-sentinel.html).
- [Redis Cluster](https://redis.uptrace.dev/guide/go-redis-cluster.html).
- [Client-side caching](#client-side-caching).
- [MultiDB (Active-Active failover)](#multidb-active-active-failover) (experimental).
- [Redis Performance Monitoring](https://redis.uptrace.dev/guide/redis-performance-monitoring.html).
- [Redis Probabilistic [RedisStack]](https://redis.io/docs/data-types/probabilistic/)
- [Customizable read and write buffers size.](#custom-buffer-sizes)

## Installation

go-redis supports 2 last Go versions and requires a Go version with
[modules](https://github.com/golang/go/wiki/Modules) support. So make sure to initialize a Go
module:

```shell
go mod init github.com/my/repo
```

Then install go-redis/**v9**:

```shell
go get github.com/redis/go-redis/v9
```

## Quickstart

```go
import (
    "context"
    "fmt"

    "github.com/redis/go-redis/v9"
)

var ctx = context.Background()

func ExampleClient() {
    rdb := redis.NewClient(&redis.Options{
        Addr:     "localhost:6379",
        Password: "", // no password set
        DB:       0,  // use default DB
    })
    defer rdb.Close()

    err := rdb.Set(ctx, "key", "value", 0).Err()
    if err != nil {
        panic(err)
    }

    val, err := rdb.Get(ctx, "key").Result()
    if err != nil {
        panic(err)
    }
    fmt.Println("key", val)

    val2, err := rdb.Get(ctx, "key2").Result()
    if err == redis.Nil {
        fmt.Println("key2 does not exist")
    } else if err != nil {
        panic(err)
    } else {
        fmt.Println("key2", val2)
    }
    // Output: key value
    // key2 does not exist
}
```

### Dial retries and backoff

Connection establishment can be retried by the connection pool when dialing fails.

- **`DialerRetries`**: maximum number of dial attempts (default: 5).
- **`DialerRetryTimeout`**: default delay between attempts when no custom backoff is provided (default: 100ms).
- **`DialerRetryBackoff`**: optional function hook to control the delay between attempts.

Example:

```go
rdb := redis.NewClient(&redis.Options{
	Addr: "localhost:6379",

	DialerRetries:      5,
	DialerRetryTimeout: 100 * time.Millisecond, // used when DialerRetryBackoff is nil

	// Optional: exponential backoff with jitter and a cap.
	DialerRetryBackoff: redis.DialRetryBackoffExponential(100*time.Millisecond, 2*time.Second),
})
defer rdb.Close()
```

### Authentication

The Redis client supports multiple ways to provide authentication credentials, with a clear priority order. Here are the available options:

#### 1. Streaming Credentials Provider (Highest Priority)

The streaming credentials provider allows for dynamic credential updates during the connection lifetime. This is particularly useful for managed identity services and token-based authentication.

```go
type StreamingCredentialsProvider interface {
    Subscribe(listener CredentialsListener) (Credentials, UnsubscribeFunc, error)
}

type CredentialsListener interface {
    OnNext(credentials Credentials)  // Called when credentials are updated
    OnError(err error)              // Called when an error occurs
}

type Credentials interface {
    BasicAuth() (username string, password string)
    RawCredentials() string
}
```

Providers can optionally implement `auth.ContextStreamingCredentialsProvider` by
adding `SubscribeWithContext(ctx, listener)` alongside `Subscribe`. go-redis uses
this method during connection initialization so the caller's context can bound
the wait for credentials. This applies whether or not `ContextTimeoutEnabled` is
set. Canceling that context must not end an established subscription or cancel
token acquisition shared with other connections. Providers
and wrappers without this method retain synchronous `Subscribe` behavior; there
is no goroutine fallback. go-redis-entraid v1.1.0 and later support this method.

Example usage:
```go
rdb := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
    StreamingCredentialsProvider: &MyCredentialsProvider{},
})
```

**Note:** The streaming credentials provider can be used with [go-redis-entraid](https://github.com/redis/go-redis-entraid) to enable Entra ID (formerly Azure AD) authentication. This allows for seamless integration with Azure's managed identity services and token-based authentication.

Example with Entra ID:
```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/redis/go-redis-entraid"
)

// Create an Entra ID credentials provider
provider := entraid.NewDefaultAzureIdentityProvider()

// Configure Redis client with Entra ID authentication
rdb := redis.NewClient(&redis.Options{
    Addr: "your-redis-server.redis.cache.windows.net:6380",
    StreamingCredentialsProvider: provider,
    TLSConfig: &tls.Config{
        MinVersion: tls.VersionTLS12,
    },
})
```

#### 2. Context-based Credentials Provider

The context-based provider allows credentials to be determined at the time of each operation, using the context.

```go
rdb := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
    CredentialsProviderContext: func(ctx context.Context) (string, string, error) {
        // Return username, password, and any error
        return "user", "pass", nil
    },
})
```

#### 3. Regular Credentials Provider

A simple function-based provider that returns static credentials.

```go
rdb := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
    CredentialsProvider: func() (string, string) {
        // Return username and password
        return "user", "pass"
    },
})
```

#### 4. Username/Password Fields (Lowest Priority)

The most basic way to provide credentials is through the `Username` and `Password` fields in the options.

```go
rdb := redis.NewClient(&redis.Options{
    Addr:     "localhost:6379",
    Username: "user",
    Password: "pass",
})
```

#### Priority Order

The client will use credentials in the following priority order:
1. Streaming Credentials Provider (if set)
2. Context-based Credentials Provider (if set)
3. Regular Credentials Provider (if set)
4. Username/Password fields (if set)

If none of these are set, the client will attempt to connect without authentication.

### Protocol Version

The client supports both RESP2 and RESP3 protocols. You can specify the protocol version in the options:

```go
rdb := redis.NewClient(&redis.Options{
    Addr:     "localhost:6379",
    Password: "", // no password set
    DB:       0,  // use default DB
    Protocol: 3,  // specify 2 for RESP 2 or 3 for RESP 3
})
```

### Redis Cluster fan-out safety

Cluster routing uses shared command metadata for key positions and read-only
status. The default resolver preserves existing routing behavior, including the
cluster-wide `DBSize`, `ScriptLoad`, `ScriptFlush`, and `ScriptExists` helpers.
`CommandMetadata` configuration alone does not enable new fan-out policies.

Opt into metadata-based fan-out and aggregation explicitly, before using the client:

```go
c := redis.NewClusterClient(&redis.ClusterOptions{
    Addrs: []string{"localhost:7000", "localhost:7001", "localhost:7002"},
})
c.SetCommandInfoResolver(redis.NewCommandMetadataResolver())
```

The resolver uses the client's metadata view, including configured overrides and
live metadata. To request a live metadata attempt during resolution, install
`c.NewDynamicResolver()` instead; this is also an explicit fan-out opt-in.
Supported policies can send `PING` to all masters or split commands such as
`MGET`, `DEL`, and `MSET` across slots. Without opt-in, their existing routing
and server-side `CROSSSLOT` behavior remain unchanged.

The default `CommandMetadataStatic` mode performs no metadata network requests
unless resolution reaches an explicitly installed `NewDynamicResolver`.
After that first live attempt, reconnects and topology changes trigger identity
verification, and a configured `RefreshInterval` also checks for module or
version changes on existing connections. With no interval, an in-place module
change needs a reconnect or topology change to be detected.
Only the initial verification can delay resolution. After failure or later
invalidation, commands immediately use the conservative metadata view while
background attempts retry with backoff. Live metadata is published only after
all required server compatibility checks succeed.
For commands missing from the bundled snapshot, configure
`CommandMetadataPreferLive` or `CommandMetadataConfig.Overrides` to supply their
key positions and read-only flags. Resolved key specifications take precedence
over `SetFirstKeyPos`; that hint applies when command metadata is unavailable.

For opted-in numeric fan-out, sum/min/max preserve integer replies as `int64`,
including raw `Do(...).Result()` values; integer overflow returns an error.
Aggregates containing floating-point replies return `float64`.

Explicit `Pipeline` and `TxPipeline` calls keep each command local to its selected
node; automatic metadata fan-out is suppressed inside these batches. A transaction
still requires all keys to share a slot. Ordered `AsyncAutoPipeline` rejects
metadata fan-out before dispatch; same-slot metadata commands remain batchable.
Use the blocking face or unordered half-duplex mode for automatic fan-out.
Existing direct helpers and special routing retain their separate execution
path: await prior async results before a dependent direct call.
`SCAN`, `INFO`, and known diagnostic commands remain node-local; use
`ForEachMaster` for whole-cluster inspection or scanning.

`FT.CURSOR READ` is not retried after a lost reply because the server may
already have advanced the cursor. Native cursor IDs and the existing cursor
routing remain unchanged; cluster-wide cursor ownership and reply parsing are
being addressed separately.

Cluster-wide operations and distributed DDL can affect multiple shards and are
not atomic across them, whether fan-out is performed by go-redis or by a Redis
server/coordinator. If one shard fails, earlier shards may already have applied
the command. There is no automatic rollback of those successful operations;
inspect the error and be prepared for manual intervention or cleanup.

### Client-side caching

go-redis supports server-assisted client-side caching for standalone and Cluster clients.
Eligible read replies are stored in the application's memory, so repeated reads
can avoid a Redis round trip. Redis tracks which keys each connection has read
and sends RESP3 invalidation notifications when those keys change. go-redis
uses those notifications to evict affected entries automatically.

> **Experimental:** The client-side caching API may change in a minor release.

Enable the built-in bounded cache with `ClientSideCacheConfig`:

```go
rdb := redis.NewClient(&redis.Options{
    Addr:     "localhost:6379",
    Protocol: 3,
    DB:       0,
    ClientSideCacheConfig: &redis.ClientSideCacheConfig{
        MaxEntries: 10_000,
    },
})
defer rdb.Close()
```

Client-side caching currently requires RESP3 and database
0. Fixed `Username` and `Password` values are supported. It is disabled when a
dynamic credential provider is configured, because cached data must never be
reused after the client's ACL identity changes. Only deterministic read
commands supported by the cache are stored; writes and streaming responses
bypass it.

Tracking modes and application intent are configured on the client options:

```go
ClientSideCacheConfig: &redis.ClientSideCacheConfig{MaxEntries: 10_000},
ClientSideCacheTrackingMode: redis.TrackingModeOptIn,
ClientSideCacheable: func(command string, keys []string) bool {
    return command == "get" && len(keys) == 1 && strings.HasPrefix(keys[0], "user:")
},
```

`plain` (including the zero value) caches eligible reads by default; `optin`
bypasses by default; `optout` caches by default. The predicate has the same
meaning in every mode, including plain. It runs concurrently, outside cache
locks, on eligible unmarked calls, and receives actual Redis keys. Its owned
key slice may be retained but must not be modified. Mode/policy are fixed at
client construction. These options also apply with an explicit custom Cache;
setting them alone does not enable caching. An invalid tracking mode logs a
warning once at construction and disables caching without failing commands.

In plain mode, the predicate and context marks control only local cache use;
Redis still tracks reads on tracked connections. `OPTIN` and `OPTOUT` also limit
server tracking according to the caching decision. `NewLocalCache` configures
storage only and does not select a tracking mode or predicate.

Use a context mark to override intent for a call, with any client type:

```go
value, err := rdb.Get(redis.WithCache(ctx), "user:42").Result()
value, err = rdb.Get(redis.WithoutCache(ctx), "user:42").Result()
```

The returned context inherits cancellation, deadlines and other values from
`ctx`. It can be reused; every command submitted with it receives the mark.
The last mark wins when contexts are nested, and the original context is
unchanged. Intent is captured at submission and survives internal context
replacement, asynchronous dispatch and retries.

A bypass reads the server without using, refreshing or removing
an existing entry. `WithCache` requests caching when the command and execution
path support it; it never overrides eligibility or guarantees that every reply
will be stored. Otherwise the command executes normally without caching, with
no error or warning caused by the mark. Operational errors and diagnostics are
unchanged. A fully read Redis error from CLIENT CACHING returns successful data
uncached; protocol/transport failures retire the connection. Hits send no commands.

All three fetch paths (foreground, miss coalescer, background refresh) support
the modes. Context marks preserve AutoPipeline's normal call semantics:
blocking calls wait; `AsyncAutoPipeline` returns immediately and result accessors
wait for completion. Blocking and explicitly unordered faces use the normal
cache path. On ordered `AsyncAutoPipeline`, marked typed reads remain in the ordered
stream and execute uncached. Bypass marks also retain ordering. This does not
change the existing ordering exceptions for raw `Do` calls, blocking commands
and direct administrative helpers.
Public batches, pipelines, transactions and scripts remain uncached, even with
`WithCache`. Marked commands retain their normal execution and do not prevent
other queued commands from running. Apply marks to command submission, not
`Exec`. Typed helpers such as Cluster `DBSize` and script/fieldset management
keep their normal shard coverage and execute uncached.

Cluster CSC supports single-target execution and eligible metadata-based
fan-out. Enable metadata routing explicitly as described above. The predicate
runs once on the original call and receives all its Redis keys. Each fragment
is cached independently; failed, absent or redirected fragments do not roll
back successful siblings. MOVED permits a fresh tracked destination fetch;
ASK executes without cache lookup or publication.

Marked `AutoPipeline` calls pass through normal routing admission. Ordered async
mode rejects cross-slot fan-out before dispatch; blocking and unordered
half-duplex modes support it. Custom node factories must preserve the supplied
Options, including cache policy and command metadata configuration.

For a Cluster client, use the same configuration on `ClusterOptions`:

```go
rdb := redis.NewClusterClient(&redis.ClusterOptions{
    Addrs: []string{"localhost:7000", "localhost:7001"},
    Protocol: 3,
    ClientSideCacheConfig: &redis.ClientSideCacheConfig{MaxEntries: 10_000},
})
defer rdb.Close()
```

Each internal node client owns an independent cache shared by its connections.
Limits apply **per node**: discovering more nodes can increase total memory.
Cluster cache hits still perform slot routing, but do not acquire a connection.
Primary and replica reads use their selected node's cache. Replica routing and
its replication-lag trade-off are unchanged.
Ordinary pipelines, transactions and full-duplex batches remain uncached;
AutoPipeline's existing ordered solo path can use CSC. With CSC enabled, native
full-duplex sessions use pipeline-pool connections with tracking disabled and send no
`CLIENT CACHING` commands. If that pool is full, acquisition waits up to
`DefaultPipelinePoolTimeout` (or a smaller positive `PoolTimeout`) before applying
the existing retry policy; these sessions do not spill to the main pool. Ordinary
pipeline fallback execution and commands diverted to `Process` retain their
normal behavior.
Direct node handles (`MasterForKey`, `SlaveForKey`, `ForEachShard`) and their clones
behave as standalone clients, including caching when enabled. `ClusterClient.CSCStats()` and
`CSCRefreshStats()` aggregate currently registered node statistics; counters of
removed nodes are not retained.

`NewFailoverClusterClient` also accepts CSC configuration, including when selected
by `NewUniversalClient`. Sentinel discovery connections remain uncached, and
`NewFailoverClient` does not support CSC. The existing DB-0-only CSC
restriction applies; nonzero databases execute normally without caching.

Cache lifecycle belongs to each standalone node client. There is no Cluster CSC
manager or transfer of cached values between nodes. Nodes entering routing have
their caches cleared before publication; nodes leaving routing are cleared afterward,
instead of waiting for registry GC. Clearing on entry also removes values refilled
after an earlier removal. Nodes present in both maps, including replicas, keep
their caches. Concurrent reloads retain last-publication-wins behavior.
When a cache miss, coalesced fetch or background refresh receives MOVED or ASK,
the node client evicts results depending on that fetch's known Redis keys across
its local cache, including pending reservations. Unknown keys and uncached
commands rely on server invalidations; they never trigger a whole-cache flush.
Unrelated entries and other nodes' caches are untouched. With plain tracking and
no predicate or per-call mark, redirected attempts bypass caching. With opt-in,
opt-out, a predicate or a per-call mark, MOVED forces a destination fetch and
allows caching when requested and tracking is available. ASK always bypasses
lookup and storage.
Background refresh stays node-local: it applies the same key eviction but does
not follow redirects or cache errors. Ordinary Cluster routing and topology reload
triggers remain unchanged. Losing a tracking connection evicts only entries
dependent on that connection, not the entire node or Cluster cache. Reconnected
connections establish tracking again for future reads.

For nodes that remain routed, cache correctness relies on server tracking
invalidations and connection-loss eviction, not on topology polling. A fresh slot
map cannot rule out migration or failback between observations. Cluster caches only replies establishing that all
Redis keys they depend on exist. During migration, a different client can create a missing key
on an importing node without invalidating the source node's tracked absence.
Therefore `GET` null replies and `MGET`/`JSON.MGET` replies containing any null element
are returned normally but not cached. `EXISTS` is cached only when its count equals
the number of key arguments (including duplicates). Single-key commands use
command-specific presence checks: an empty `GET` string is cached, but an empty
`GETRANGE` reply or zero `STRLEN` is not, since it can also represent a missing key.
`HMGET` can be cached when at least one field is present: its fields belong to one
Redis key, unlike `MGET`'s separate keys. Ambiguous commands (`SDIFF`, `SUNION`, `ZDIFF`,
`ZUNION`, `LCS`, `BITPOS`, `BITFIELD_RO`, and `JSON` commands other than `GET`/`MGET`) bypass
Cluster caching. This trades cache hits on ambiguous/absent results for safety
without extra existence probes. The same filter applies to refresh and
coalesced fills, including custom per-node caches.

Standalone negative caching is unchanged. Cluster adds no cache-age policy or
topology trust window; by default, entries do not expire based on age.
Presence filtering and asynchronous invalidations do not
guarantee immediate consistency or an ownership fence. Redis Enterprise
proxy deployments may require reconnecting caching clients after shard additions
or same-node slot moves; this policy does not remove that operational requirement.

With atomic slot migration (`CLUSTER MIGRATION`, Redis 8.4+), the importing node
can accept writes before the source sends invalidations for the moved keys.
Cached reads can return the previous value during that window; tens of
milliseconds have been observed. This implementation relies on the server's
invalidations and does not eliminate that handoff window.
Legacy `MIGRATE`-based slot migration does not use this atomic-migration handoff;
ordinary asynchronous invalidation delays still apply.

Custom per-node caches can be created with `ClusterOptions.NewClient`: preserve
the supplied options and create a fresh cache for each node-client lifetime.
Custom caches remain responsible for any optional entry-age policy; the Cache
interface does not expose one for `ClusterClient` to enforce. For a built-in
`LocalCache` created in a factory, use the supplied cache configuration.
`UniversalOptions` forwards CSC configuration to Cluster, but its explicit
`ClientSideCache` instance remains standalone-only. Refresh, miss coalescing and
invalidation batching retain their standalone built-in-cache limitations.

While client-side caching is enabled, go-redis rejects `SELECT`, `AUTH`,
`HELLO` with arguments, `RESET`, `CLIENT TRACKING`, `CLIENT CACHING`, and raw `SUBSCRIBE`,
`PSUBSCRIBE`, or `SSUBSCRIBE` commands because they would change connection
state that the cache relies on. A guarded command also fails its whole
pipeline. The typed `Subscribe`, `PSubscribe`, and `SSubscribe` APIs remain
supported because they use dedicated connections.

For session-scoped work, use `client.EphemeralConn()` and close it afterwards.
With caching active it dials a fresh connection with tracking disabled, so session
commands are allowed without disrupting the parent cache. It does not consume a
`PoolSize` slot or support maintenance handoff. It obeys `MaxActiveConns` and
`MaxConcurrentDials`. Its reads always hit Redis.

Invalidations are processed asynchronously. `DrainInterval` controls how often
idle connections are checked for them, while `MaxStaleness` can provide an
optional upper bound on an entry's lifetime.

Until a key's invalidation arrives, a cache hit can return a value older than
the one on the server. Reads are also not monotonic for one caller. A read that
waits twice for another caller's fetch of the same key reads the server
directly instead. The other fetch ran on the server earlier, can store its
older value afterwards, and a later hit then returns that older value until the
invalidation arrives. See the
[client-side caching example](./example/client-side-caching) for a working
demonstration.

### Connecting via a redis url

go-redis also supports connecting via the
[redis uri specification](https://github.com/redis/redis-specifications/tree/master/uri/redis.txt).
The example below demonstrates how the connection can easily be configured using a string, adhering
to this specification.

```go
import (
    "github.com/redis/go-redis/v9"
)

func ExampleClient() *redis.Client {
    url := "redis://user:password@localhost:6379/0?protocol=3"
    opts, err := redis.ParseURL(url)
    if err != nil {
        panic(err)
    }

    return redis.NewClient(opts)
}

```

### Instrument with OpenTelemetry

```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/redis/go-redis/extra/redisotel/v9"
    "errors"
)

func main() {
    ...
    rdb := redis.NewClient(&redis.Options{...})

    if err := errors.Join(redisotel.InstrumentTracing(rdb), redisotel.InstrumentMetrics(rdb)); err != nil {
        log.Fatal(err)
    }
```


### Buffer Size Configuration

go-redis uses 32KiB read and write buffers by default for optimal performance. For high-throughput applications or large pipelines, you can customize buffer sizes:

```go
rdb := redis.NewClient(&redis.Options{
    Addr:            "localhost:6379",
    ReadBufferSize:  1024 * 1024, // 1MiB read buffer
    WriteBufferSize: 1024 * 1024, // 1MiB write buffer
})
```

### Automatic pipelining

**Experimental** — the API may still change. Reach for autopipelining in
high-throughput / high-load / scale scenarios; at low concurrency a plain
client is simpler and just as fast. A runnable usage tour and throughput
comparison live in [`example/autopipeline`](example/autopipeline).

> **EXPERIMENTAL:** the autopipelining API is subject to change in a future
> release as we gather feedback — pin your go-redis version if you adopt it.

When many goroutines issue commands concurrently, autopipelining batches them
into Redis pipelines automatically — without you writing any pipeline code. It
comes in two faces:

- **`AutoPipeline()` — blocking, drop-in.** Each command call blocks until it
  executes and returns its own value/error, exactly like a normal client, so
  existing code keeps working unchanged. Under concurrency the engine coalesces
  commands from all goroutines into deep, back-to-back pipelines (a single
  ordered batch stream by default), reaching several times a plain client's
  executed commands per second in the same environment — roughly an order of
  magnitude with a parallel-batch config (`MaxConcurrentBatches` > 1 with
  `Unordered`). Per-goroutine ordering is preserved.
- **`AsyncAutoPipeline()` — deferred, highest throughput.** Command calls return
  immediately; you submit a window of commands and read their results afterward,
  which keeps each pipeline deep — tens of times a plain client's throughput.
  Ordered by default. Absolute numbers depend heavily on the machine, network
  path and server; see `autopipeline_bench_README.md` for the benchmark
  methodology and multipliers.

```go
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
defer rdb.Close()
ctx := context.Background()

// Blocking face: drop-in for a normal client, batched under the hood.
ap, err := rdb.AutoPipeline()
if err != nil { // invalid AutoPipelineOptions, or the client is closed
    log.Fatal(err)
}
defer ap.Close()

var wg sync.WaitGroup
for i := 0; i < 1000; i++ {
    wg.Add(1)
    go func(i int) {
        defer wg.Done()
        key := fmt.Sprintf("key:%d", i)
        if err := ap.Set(ctx, key, i, 0).Err(); err != nil { // blocks until executed
            log.Printf("set %s: %v", key, err)
        }
    }(i)
}
wg.Wait()
```

For maximum throughput, submit a window on the async face and read later:

```go
ctx := context.Background()
ap, err := rdb.AsyncAutoPipeline() // ordered by default
if err != nil {
    log.Fatal(err)
}
defer ap.Close()

cmds := make([]*redis.StatusCmd, 0, 200)
for i := 0; i < 200; i++ {
    cmds = append(cmds, ap.Set(ctx, fmt.Sprintf("key:%d", i), i, 0)) // returns immediately
}
for _, cmd := range cmds {
    if err := cmd.Err(); err != nil { // blocks until executed
        log.Printf("set: %v", err)
    }
}
```

Each face has a no-argument form that uses `Options.AutoPipelineOptions` (or the
built-in default) and a `WithOptions` form that takes an explicit
`*AutoPipelineOptions`; both return `(*AutoPipeliner, error)` — the error is
non-nil for an invalid config or a closed client (e.g.
`ap, err := rdb.AsyncAutoPipelineWithOptions(&redis.AutoPipelineOptions{MaxConcurrentBatches: 8, Unordered: true})`);
a handful of parallel batches saturates the link — more permits only add
overlapping batches without deepening them.
They work on `ClusterClient` too: commands are routed to the correct shard per
key, so a single batch may span many slots; ordering across nodes is per key
(same-key commands stay in order, different nodes' sub-pipelines run
concurrently). Ordered `AsyncAutoPipeline` rejects metadata fan-out and custom
multi-shard policies that would bypass its batch stream. Same-slot commands
with metadata policies remain batchable. Use the blocking face, or
`Unordered: true` in half-duplex mode, for automatic fan-out. Alternatively,
await pending results and execute the dependent command through `ClusterClient`.
Existing direct helpers (`DBSize`, script-management methods, and `Do`) and
special routing still bypass the batch stream; await dependent results when
mixing these with async batches. Because batches share
a few pipeline connections, autopipelining
also needs far fewer connections than a plain client at the same concurrency
(see `PipelinePoolSize`). Autopipelining is only a win under concurrency (or
windowed submission) — a single goroutine issuing one blocking command at a
time sees little benefit, and a hand-written `Pipeline()` is still fastest when
you can batch by hand.

Caveats: a command's context is not honored once it is queued (batches execute
on the autopipeliner's own context) — use a plain client for per-command
deadlines. Blocking commands (`BLPOP`, `WAIT`, ...) are never batched and run
directly on your context — as are `SHUTDOWN` and `MONITOR`, which would
poison a shared pipeline connection — and `Do` also bypasses batching with plain
`Client.Do` semantics — prefer the typed methods (`ap.Set`, `ap.Get`, ...). On
a dropped connection a batch is retried whole (up to `MaxRetries`), so
non-idempotent commands may execute twice. Both faces return a cached,
client-shared instance: the first call's config wins and `Close` stops it for
all callers. Hooks may read command results (the engine hands a hook running
on the dispatch goroutine the same view a plain pipeline hook gets), but a
hook must never issue a command on the same autopipeliner and wait for it —
the nested command needs the very dispatch slot the hook is holding, and the
engine only recovers by failing that flush after its 30s permit backstops.
`Options.Limiter` is consulted once per batch dispatch (as with a manual
pipeline), not once per command, plus once for the handshake of a fresh
connection, as with a pooled command that dials. An autopipeliner created on a
`WithTimeout`/`WithReadTimeout` clone is not stopped by the parent's `Close` —
close it explicitly.

### MultiDB (Active-Active failover)

> **EXPERIMENTAL:** the MultiDB API is subject to change in a future release.

`MultiDBClient` routes all commands to one active database out of several
member databases (standalone or cluster), for example the regions of a Redis
Enterprise Active-Active database. It fails over to another member based on
circuit breakers, background health checks and a failover strategy (highest
weight by default), and falls back to a higher-weight member once it recovers.
It implements `UniversalClient`; the control API (`SetActiveDatabase`,
`AddDatabase`, `RemoveDatabase`, ...) is described by `MultiDBCtrl`. Extra
health checks and policies (lag-aware, probe counts) live in the
`github.com/redis/go-redis/v9/multidb` package.

```go
// The deadline bounds startup only: NewMultiDBClient can use most of it.
startCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

client, err := redis.NewMultiDBClient(startCtx, &redis.MultiDBOptions{
	Clients: []redis.MultiDBClientConfig{
		{Options: &redis.Options{Addr: "east.example.com:6379"}, Weight: 1.0},
		{Options: &redis.Options{Addr: "west.example.com:6379"}, Weight: 0.5},
	},
})
if err != nil {
	panic(err)
}
defer client.Close()

ctx := context.Background()
if err := client.Set(ctx, "key", "value", 0).Err(); err != nil {
	panic(err)
}
fmt.Println("active database:", client.ActiveDatabaseID())
```

Members get ids `0..len(Clients)-1` in slice order. A weight is in `(0, 1]`
(0 means the default 1.0); -1 disables a member, so failover never picks it.
By default half of the members, rounded up, must be healthy for
`NewMultiDBClient` to succeed (one of two, two of three); set `InitialDBState`
to change this. A failed start matches `ErrInsufficientHealthyDatabases`; when
no member is healthy it also matches `ErrPermanentlyNotAvailable`.

MultiDB owns retries: a failed command is retried `CommandRetries` times (0
means 2, -1 none), each time on the database active at that moment, and every
attempt counts toward the circuit breaker and the failure detector. Standalone
members get no retries of their own unless `Options.MaxRetries` is set. The
default failure detector trips at 1000 failures and a 10% failure rate within
2s. A `FailureDetectorConfig` replaces these values as given (a zero field is
not filled in), so start from `redis.DefaultMultiDBFailureDetectorConfig()`
and change the fields you need. Health checks are probed 3 times, 500ms apart,
and every probe must pass, so a healthy start or `AddDatabase` takes at least
1s; under the default policy `SetActiveDatabase` uses one probe. Set `multidb.WithProbes` /
`multidb.WithDelay` on a check to change that. Probes that cannot fit
`HealthCheckTimeout` (default 3s, at most half of `HealthCheckInterval`) are
rejected at construction. MultiDB does not require
RESP3. Cluster members cannot use `RouteByLatency`, `RouteRandomly` or
`ReadOnly` (health checks probe masters only). Maintenance notifications are
off on members unless `MaintNotificationsConfig` is set. A runnable example
lives in [`example/multidb`](example/multidb).

### Advanced Configuration

go-redis supports extending the client identification phase to allow projects to send their own custom client identification.

#### Default Client Identification

By default, go-redis automatically sends the client library name and version during the connection process. This feature is available in redis-server as of version 7.2. As a result, the command is "fire and forget", meaning it should fail silently, in the case that the redis server does not support this feature.

#### Disabling Client Identification

The `DisableIdentity` option disables sending the client library name and version with `CLIENT SETINFO` during connection initialization. It does not disable authentication or TLS certificate verification.
Initially there was a typo and the option was named `DisableIndentity` instead of `DisableIdentity`. The misspelled option is marked as Deprecated and will be removed in V10 of this library.
Although both options will work at the moment, the correct option is `DisableIdentity`. The deprecated option will be removed in V10 of this library, so please use the correct option name to avoid any issues.

To disable client identification, set the `DisableIdentity` option to `true` in the Redis client options:

```go
rdb := redis.NewClient(&redis.Options{
    Addr:            "localhost:6379",
    Password:        "",
    DB:              0,
    DisableIdentity: true, // Disable set-info on connect
})
```

#### RESP3 for RediSearch Commands (`UnstableResp3` is deprecated)
As of v9.20, `FT.SEARCH`, `FT.AGGREGATE`, `FT.INFO`, `FT.SPELLCHECK`, and `FT.SYNDUMP`
parse RESP3 (map) responses into the same typed result objects as RESP2. **No flag
is required — `Val()` / `Result()` work uniformly on both protocols.**

The legacy `UnstableResp3` option is now a **no-op** and is retained on every
options struct only for backwards compatibility. It will be removed in a future
release; new code should not set it.

`RawResult()` / `RawVal()` continue to work for callers that prefer the raw RESP
payload directly:

```go
res1, err := client.FTSearchWithArgs(ctx, "txt", "foo bar", &redis.FTSearchOptions{}).RawResult()
val1 := client.FTSearchWithArgs(ctx, "txt", "foo bar", &redis.FTSearchOptions{}).RawVal()
```

#### Redis-Search Default Dialect

In the Redis-Search module, **the default dialect is 2**. If needed, you can explicitly specify a different dialect using the appropriate configuration in your queries.

**Important**: Be aware that the query dialect may impact the results returned. If needed, you can revert to a different dialect version by passing the desired dialect in the arguments of the command you want to execute.
For example:
```
	res2, err := rdb.FTSearchWithArgs(ctx,
		"idx:bicycle",
		"@pickup_zone:[CONTAINS $bike]",
		&redis.FTSearchOptions{
			Params: map[string]interface{}{
				"bike": "POINT(-0.1278 51.5074)",
			},
			DialectVersion: 3,
		},
	).Result()
```
You can find further details in the [query dialect documentation](https://redis.io/docs/latest/develop/interact/search-and-query/advanced-concepts/dialects/).

#### Custom buffer sizes
Prior to v9.12, the buffer size was the default go value of 4096 bytes. Starting from v9.12, 
go-redis uses 32KiB read and write buffers by default for optimal performance.
For high-throughput applications or large pipelines, you can customize buffer sizes:

```go
rdb := redis.NewClient(&redis.Options{
    Addr:            "localhost:6379",
    ReadBufferSize:  1024 * 1024, // 1MiB read buffer
    WriteBufferSize: 1024 * 1024, // 1MiB write buffer
})
```

**Important**: If you experience any issues with the default buffer sizes, please try setting them to the go default of 4096 bytes.

## Contributing
We welcome contributions to the go-redis library! If you have a bug fix, feature request, or improvement, please open an issue or pull request on GitHub.
We appreciate your help in making go-redis better for everyone.
If you are interested in contributing to the go-redis library, please check out our [contributing guidelines](CONTRIBUTING.md) for more information on how to get started.

## Look and feel

Some corner cases:

```go
// SET key value EX 10 NX
set, err := rdb.SetNX(ctx, "key", "value", 10*time.Second).Result()

// SET key value KEEPTTL NX
set, err := rdb.SetNX(ctx, "key", "value", redis.KeepTTL).Result()

// SORT list LIMIT 0 2 ASC
vals, err := rdb.Sort(ctx, "list", &redis.Sort{Offset: 0, Count: 2, Order: "ASC"}).Result()

// ZRANGEBYSCORE zset -inf +inf WITHSCORES LIMIT 0 2
vals, err := rdb.ZRangeByScoreWithScores(ctx, "zset", &redis.ZRangeBy{
    Min: "-inf",
    Max: "+inf",
    Offset: 0,
    Count: 2,
}).Result()

// ZINTERSTORE out 2 zset1 zset2 WEIGHTS 2 3 AGGREGATE SUM
vals, err := rdb.ZInterStore(ctx, "out", &redis.ZStore{
    Keys: []string{"zset1", "zset2"},
    Weights: []float64{2, 3},
}).Result()

// EVAL "return {KEYS[1],ARGV[1]}" 1 "key" "hello"
vals, err := rdb.Eval(ctx, "return {KEYS[1],ARGV[1]}", []string{"key"}, "hello").Result()

// custom command
res, err := rdb.Do(ctx, "set", "key", "value").Result()
```

### Raw commands and connection state

`Do` sends the command verbatim on whichever pooled connection happens to be
free. For keyspace commands that is all you need. It is the wrong tool for
any command that alters **connection session state** — `SELECT`,
`CLIENT SETNAME`, `CLIENT TRACKING`, `RESET`, `HIMPORT PREPARE`/`DISCARD`,
and similar: the state lands on (or is wiped from) a single arbitrary
connection, later commands are served by other connections that don't share
it, and the affected connection eventually returns to the pool and serves
unrelated callers. The result is nondeterministic behavior that typed APIs
manage for you — for example, the typed `HImport*` methods keep a
client-side registry and replay fieldsets onto every connection that needs
them, while a raw `Do(ctx, "himport", "prepare", ...)` bypasses that
entirely, with no replay, recovery, or discard propagation.

For session-scoped work without a typed API, hold a dedicated connection
(`client.EphemeralConn()`) for its whole lifetime and close it afterwards.
`client.Conn()` is also a dedicated connection, but `Close` returns it to the
pool with whatever session state is left on it, so it only suits commands that
leave the connection as they found it.

## Typed Errors

go-redis provides typed error checking functions for common Redis errors:

```go
// Cluster and replication errors
redis.IsLoadingError(err)        // Redis is loading the dataset
redis.IsReadOnlyError(err)       // Write to read-only replica
redis.IsClusterDownError(err)    // Cluster is down
redis.IsTryAgainError(err)       // Command should be retried
redis.IsMasterDownError(err)     // Master is down
redis.IsMovedError(err)          // Returns (address, true) if key moved
redis.IsAskError(err)            // Returns (address, true) if key being migrated

// Connection and resource errors
redis.IsMaxClientsError(err)     // Maximum clients reached
redis.IsAuthError(err)           // Authentication failed (NOAUTH, WRONGPASS, unauthenticated)
redis.IsPermissionError(err)     // Permission denied (NOPERM)
redis.IsOOMError(err)            // Out of memory (OOM)

// Transaction errors
redis.IsExecAbortError(err)      // Transaction aborted (EXECABORT)
```

### Error Wrapping in Hooks

When wrapping errors in hooks, use custom error types with `Unwrap()` method (preferred) or `fmt.Errorf` with `%w`. Always call `cmd.SetErr()` to preserve error type information:

```go
// Custom error type (preferred)
type AppError struct {
    Code      string
    RequestID string
    Err       error
}

func (e *AppError) Error() string {
    return fmt.Sprintf("[%s] request_id=%s: %v", e.Code, e.RequestID, e.Err)
}

func (e *AppError) Unwrap() error {
    return e.Err
}

// Hook implementation
func (h MyHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
    return func(ctx context.Context, cmd redis.Cmder) error {
        err := next(ctx, cmd)
        if err != nil {
            // Wrap with custom error type
            wrappedErr := &AppError{
                Code:      "REDIS_ERROR",
                RequestID: getRequestID(ctx),
                Err:       err,
            }
            cmd.SetErr(wrappedErr)
            return wrappedErr  // Return wrapped error to preserve it
        }
        return nil
    }
}

// Typed error detection works through wrappers
if redis.IsLoadingError(err) {
    // Retry logic
}

// Extract custom error if needed
var appErr *AppError
if errors.As(err, &appErr) {
    log.Printf("Request: %s", appErr.RequestID)
}
```

Alternatively, use `fmt.Errorf` with `%w`:
```go
wrappedErr := fmt.Errorf("context: %w", err)
cmd.SetErr(wrappedErr)
```

### Pipeline Hook Example

For pipeline operations, use `ProcessPipelineHook`:

```go
type PipelineLoggingHook struct{}

func (h PipelineLoggingHook) DialHook(next redis.DialHook) redis.DialHook {
    return next
}

func (h PipelineLoggingHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
    return next
}

func (h PipelineLoggingHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
    return func(ctx context.Context, cmds []redis.Cmder) error {
        start := time.Now()

        // Execute the pipeline
        err := next(ctx, cmds)

        duration := time.Since(start)
        log.Printf("Pipeline executed %d commands in %v", len(cmds), duration)

        // Process individual command errors
        // Note: Individual command errors are already set on each cmd by the pipeline execution
        for _, cmd := range cmds {
            if cmdErr := cmd.Err(); cmdErr != nil {
                // Check for specific error types using typed error functions
                if redis.IsAuthError(cmdErr) {
                    log.Printf("Auth error in pipeline command %s: %v", cmd.Name(), cmdErr)
                } else if redis.IsPermissionError(cmdErr) {
                    log.Printf("Permission error in pipeline command %s: %v", cmd.Name(), cmdErr)
                }

                // Optionally wrap individual command errors to add context
                // The wrapped error preserves type information through errors.As()
                wrappedErr := fmt.Errorf("pipeline cmd %s failed: %w", cmd.Name(), cmdErr)
                cmd.SetErr(wrappedErr)
            }
        }

        // Return the pipeline-level error (connection errors, etc.)
        // You can wrap it if needed, or return it as-is
        return err
    }
}

// Register the hook
rdb.AddHook(PipelineLoggingHook{})

// Use pipeline - errors are still properly typed
pipe := rdb.Pipeline()
pipe.Set(ctx, "key1", "value1", 0)
pipe.Get(ctx, "key2")
_, err := pipe.Exec(ctx)
```

## Run the test

Recommended to use Docker, just need to run:
```shell
make test
```

## See also

- [Golang ORM](https://bun.uptrace.dev) for PostgreSQL, MySQL, MSSQL, and SQLite
- [Golang PostgreSQL](https://bun.uptrace.dev/postgres/)
- [Golang HTTP router](https://bunrouter.uptrace.dev/)
- [Golang ClickHouse ORM](https://github.com/uptrace/go-clickhouse)

## Contributors

> The go-redis project was originally initiated by :star: [**uptrace/uptrace**](https://github.com/uptrace/uptrace).
> Uptrace is an open-source APM tool that supports distributed tracing, metrics, and logs. You can
> use it to monitor applications and set up automatic alerts to receive notifications via email,
> Slack, Telegram, and others.
>
> See [OpenTelemetry](https://github.com/redis/go-redis/tree/master/example/otel) example which
> demonstrates how you can use Uptrace to monitor go-redis.

Thanks to all the people who already contributed!

<a href="https://github.com/redis/go-redis/graphs/contributors">
  <img src="https://contributors-img.web.app/image?repo=redis/go-redis" />
</a>
