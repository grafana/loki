# Release Notes

## Unreleased

# 9.24.0 (2026-11-24)

This is a minor release. It contains everything from 9.24.0-beta.1 and 9.24.0-beta.2, so these notes cover the full 9.23.0 → 9.24.0 upgrade. The release adds:

- **Redis 8.12 support**: set member expiration, `ZMOVE`, `BITRANGEINTARRAY`, `CF.COMPACT`, scalar-quantized HNSW, `BLESS` and more, tested against Redis 8.12.0.
- The experimental **MultiDB client** for Active-Active (Redis Enterprise) failover.
- **Metadata-driven cluster routing** and client-side caching (CSC) eligibility that come from the `COMMAND` metadata.
- CSC **OPT-IN / OPT-OUT** tracking modes, CSC on `ClusterClient`, and CSC with `EphemeralConn`.
- A **per-client `OTelRecorder`**, MultiDB metrics in `redisotel-native`, and much less OpenTelemetry cost on the hot path.
- New full-duplex options (`FullDuplexPipes`, `PipelineMinIdleConns`) and a cluster recovery fix for full-duplex sessions.
- An opt-in **idle connection reaper**, `ACLGetUser`, `ClusterSlotStats` and the Redis 8.10 `BACKUP` commands.
- Many fixes for connections, cluster routing, authentication, reply parsing, and the search, JSON and probabilistic commands.

## 🚀 Highlights

### Redis 8.12 support

This release adds the new commands and options of Redis 8.12 and runs its tests against Redis 8.12.0 ([#4404](https://github.com/redis/go-redis/pull/4404)).

- **Set member expiration:** `SExpire`, `SPExpire`, `SExpireAt`, `SPExpireAt` (each with `WithArgs`), `STTL`, `SPTTL`, `SExpireTime`, `SPExpireTime`, `SPersist`, `SAddEX` and `SAddEXWithArgs` ([#4165](https://github.com/redis/go-redis/pull/4165)) by [@ndyakov](https://github.com/ndyakov).
- **Sorted sets and bitmaps:** `ZMOVE`, `BZMOVE` and `ZMOVEMEMBER` ([#4289](https://github.com/redis/go-redis/pull/4289)), and `BITRANGEINTARRAY` ([#4242](https://github.com/redis/go-redis/pull/4242)) by [@ndyakov](https://github.com/ndyakov).
- **Probabilistic and search:** `CF.COMPACT` as `CFCompact` ([#4243](https://github.com/redis/go-redis/pull/4243)) by [@ndyakov](https://github.com/ndyakov). `CELL_SIZE` for `CMS.INITBYDIM` and `CMS.INITBYPROB` (`CMSInitByDimCellSize`, `CMSInitByProbCellSize`) ([#4023](https://github.com/redis/go-redis/pull/4023)), and scalar-quantized HNSW indexes in `FT.CREATE` with the two new `FT.INFO` fields ([#4077](https://github.com/redis/go-redis/pull/4077)) by [@vlady-kotsev](https://github.com/vlady-kotsev). `RERANK` for `FT.HYBRID`, the `BITRANGEINTARRAY` options and the new command metadata ([#4277](https://github.com/redis/go-redis/pull/4277)) by [@ndyakov](https://github.com/ndyakov).
- **`BLESS`:** `BLESS GET`, `BLESS SET`, `BLESS CLEAR` and `BLESS SCAN` for the `NO-EVICT` flag ([#4041](https://github.com/redis/go-redis/pull/4041)) by [@vlady-kotsev](https://github.com/vlady-kotsev). `BlessScan` is node-local: scan each master of a cluster or ring.

### MultiDB client (experimental)

`NewMultiDBClient` routes commands to one active database out of several standalone or cluster members (e.g. Redis Enterprise Active-Active) and fails over based on circuit breakers, health checks and a weight-based failover strategy, with auto-fallback to higher-weight members. The `multidb` package adds ping and lag-aware health checks and probe policies. See `example/multidb` ([#3954](https://github.com/redis/go-redis/pull/3954)) by [@ndyakov](https://github.com/ndyakov). Defaults:

- MultiDB owns retries: `CommandRetries` (0 = 2, -1 = none) retries on the database active at each attempt, and every attempt is counted by the breaker and failure detector. Standalone members get no retries of their own (`MaxRetries` 0 becomes -1); an explicit `MaxRetries` is kept.
- Health checks: HEALTHY_ALL, 3 probes 500ms apart per check, honoring `multidb.WithProbes` / `WithDelay` / `WithTimeout`; `HealthCheckTimeout` (3s) caps the pass. Probes that cannot fit it are rejected at construction and by `AddDatabase`. Under the default policy, `SetActiveDatabase` and `ProbeTargetBeforeFailover` use one probe per check.
- Startup: `InitialDBStateMajorityAvailable` needs half the members, rounded up (1 of 2). A failed start matches `ErrInsufficientHealthyDatabases`; with no healthy member it also matches `ErrPermanentlyNotAvailable`.
- Weights are in (0, 1] (0 = 1.0); -1 disables a member. The last enabled member cannot be disabled.
- The default failure detector (1000 failures and 10% within 2s) is set with `FailureDetectorConfig`, whose fields are used as given; start from `DefaultMultiDBFailureDetectorConfig()`.
- `redisotel-native` reports MultiDB events (failover, active database change, circuit breaker state change, health check) in a new `multidb` metric group, `MetricGroupFlagMultiDB`, which `MetricGroupAll` includes ([#4007](https://github.com/redis/go-redis/pull/4007)) by [@ndyakov](https://github.com/ndyakov).
- A member `ProcessHook` that returns `nil` without calling `next` is no longer counted as a healthy database response ([#4271](https://github.com/redis/go-redis/pull/4271)), and a local result of a member hook in a cluster pipeline is kept when another node fails ([#4279](https://github.com/redis/go-redis/pull/4279)) by [@harshitdeora](https://github.com/harshitdeora). Full-duplex outcomes are recorded in the failure window after the active member is selected again ([#4283](https://github.com/redis/go-redis/pull/4283)) by [@xixi-box](https://github.com/xixi-box).

**Experimental:** the MultiDB API can change in a minor release. User hooks and health checks must not call MultiDB control methods or `Close`.

### Metadata-driven cluster routing

Cluster routing and CSC use the same resolved `COMMAND` record for each command, so flags, key positions and policies cannot drift between two metadata sources ([#3960](https://github.com/redis/go-redis/pull/3960), [#4093](https://github.com/redis/go-redis/pull/4093)) by [@ofekshenawa](https://github.com/ofekshenawa).

- Existing default routing and the cluster-wide `DBSize`, `ScriptLoad`, `ScriptFlush` and `ScriptExists` helpers are preserved. Install `NewCommandMetadataResolver()` or `client.NewDynamicResolver()` with `SetCommandInfoResolver` to enable metadata-based fan-out and aggregation. `CommandMetadata` configuration alone does not enable them. Cross-slot fan-out is not atomic.
- With metadata routing enabled, cross-slot `MGET` follows redirects for each affected slot during resharding instead of returning a missing value for a key that moved. Successful slot groups are not replayed, and fan-out replies that contain `MOVED` or `ASK` refresh the topology for later commands.
- Explicit pipelines and transactions keep node-local execution; ordered `AsyncAutoPipeline` rejects fan-out before dispatch. Automatic fan-out is available through the blocking face or unordered half-duplex mode. `SCAN`, `INFO` and known diagnostics stay node-local.
- Numeric sum/min/max fan-out keeps integer replies as exact `int64` results, including raw `Do` results; integer overflow returns an error. Aggregates with floating-point replies return `float64`.
- Resolved command key specifications take precedence over `SetFirstKeyPos`. Commands missing from the snapshot keep the legacy key-position fallback; set `CommandMetadataPreferLive` or `CommandMetadataConfig.Overrides` for full metadata. The default `CommandMetadataStatic` mode does no network lookup unless resolution reaches an installed `NewDynamicResolver`. Identity probes have bounded deadlines, and after a failure or invalidation commands use conservative metadata while verification retries in the background.
- Topology loading prefers `CLUSTER SHARDS` and falls back to `CLUSTER SLOTS`, and concurrent reloads share one in-flight load. Keyless routing prefers healthy nodes without excluding every candidate on a temporary local marker. `ErrClusterTopologyUnhealthy` supports `errors.Is`, and unknown response policies return an error.
- `FT.CURSOR READ` is no longer retried after a lost reply, on standalone and cluster clients, because the server may already have advanced the cursor.
- A `SCAN` iterator on a `ClusterClient` now visits every master. Before, it sent each page to a random node with the cursor of another node and could end early without an error ([#4200](https://github.com/redis/go-redis/pull/4200)) by [@ndyakov](https://github.com/ndyakov).
- With the dynamic resolver, `DEL`, `EXISTS`, `TOUCH` and `UNLINK` on several keys of one slot returned the per-slot count multiplied by the number of keys in that slot. They now count each slot once ([#4141](https://github.com/redis/go-redis/pull/4141)) by [@lazerg](https://github.com/lazerg).

### Client-side caching

**Experimental:** the CSC options can change in a minor release.

- **OPT-IN and OPT-OUT tracking modes:** `Options.ClientSideCacheTrackingMode` selects `TrackingModePlain` (default), `TrackingModeOptIn` or `TrackingModeOptOut`. `Options.ClientSideCacheable` is a predicate that selects the eligible unmarked calls, and `WithCache(ctx)` / `WithoutCache(ctx)` mark one call. The mode works with miss coalescing, refresh on invalidate, pipelines, autopipeline and cluster fan-out. With CSC on, the native full-duplex engine leases only the untracked pipeline pool and does not use the main pool ([#4193](https://github.com/redis/go-redis/pull/4193)) by [@ofekshenawa](https://github.com/ofekshenawa).
- **CSC on `ClusterClient`:** each node client has its own cache, shared by its connection pool, with per-node capacity limits. `ClusterOptions`, `UniversalOptions` and `NewFailoverClusterClient` accept the CSC options, and `CSCStats()` and `CSCRefreshStats()` add up the registered nodes. The client does not store a reply that cannot prove that every key it depends on exists. A MOVED or ASK redirect bypasses the cache, and a topology change flushes only the caches of nodes that enter or leave routing. Replica reads, ordinary pipelines, transactions and full-duplex batches stay uncached ([#4080](https://github.com/redis/go-redis/pull/4080)) by [@ofekshenawa](https://github.com/ofekshenawa).
- **Ephemeral connections with client-side caching:** with CSC active, `EphemeralConn` lazily dials a fresh, untracked connection. Session commands are allowed without disrupting the parent cache, and reads always go to Redis. The connection takes no `PoolSize` turn, does not support maintenance handoff, and must be closed after use. Without CSC, idle-connection reuse is unchanged ([#4174](https://github.com/redis/go-redis/pull/4174)) by [@aditya-rai-5](https://github.com/aditya-rai-5).

### Full-duplex autopipelining

- **`AutoPipelineOptions.FullDuplexPipes`** sets the number of full-duplex pipes (held connections) on a standalone `Client`. 0 means 1. `NumShards > 1` under `FullDuplex` still selects that number when `FullDuplexPipes` is 0, but `FullDuplexPipes` is the preferred name. `Config().FullDuplexPipes` reports the effective count. On a cluster, a full-duplex autopipeliner keeps one pipe for each master ([#4107](https://github.com/redis/go-redis/pull/4107)) by [@ndyakov](https://github.com/ndyakov).
- **Warm spare connection:** `Options.PipelineMinIdleConns` (default 0, `pipeline_min_idle_conns` in URLs) sets the minimum idle connections of the dedicated pipeline pool, and is available on `ClusterOptions`, `RingOptions`, `FailoverOptions` and `UniversalOptions`. A full-duplex engine keeps one idle spare, so a replacement for a failed held connection does not wait for a TCP or TLS dial. The spare is dialed but not initialized: `HELLO` and `AUTH` still run on the first lease ([#4101](https://github.com/redis/go-redis/pull/4101)) by [@ndyakov](https://github.com/ndyakov).
- **Cluster recovery:** when the session of a node engine drops, the unacknowledged commands now go through the cluster router, so they reach the new owner of the slot. Before, they were replayed only on a new connection to the same node and failed if that node was closed or replaced ([#4111](https://github.com/redis/go-redis/pull/4111)) by [@ndyakov](https://github.com/ndyakov).

### Lower OpenTelemetry cost

Two groups of changes remove most of the cost of the `redisotel-native` metrics on the hot path.

- **Full duplex:** it lost about 90% of its throughput. In one test with 256 callers and 64 commands in flight on a local Redis, `GET` throughput went from about 190k/s on v9.23.0 to about 2.1M/s (about 2.0M/s with OTel off). The gain depends on RTT, the number of commands in flight and the CPU, so expect a smaller effect on a remote Redis. The full-duplex reader reads its goroutine id once for each session, and `RecordOperationDuration` uses a bounded attribute cache and a queue for the success path ([#4269](https://github.com/redis/go-redis/pull/4269)) by [@ndyakov](https://github.com/ndyakov).
- **Pool metrics:** `db.client.connection.count` and `db.client.connection.pending_requests` are now observed at collection time from the pool statistics, and no longer updated on every `Get` and `Put`. The measurement options of the pool and command metrics are cached, and the global recorder is read without a lock. In one in-memory test (500 connections, 128 workers, all metric groups on) a command ran at 165k ops/s before, 678k with the observed connection count, 895k with the cache, and about 1.05M with the observed pending requests (1.21M with OTel off). `Init` and `Shutdown` with 10k connections went from about 7.4 ms to 0.13 ms and 0.011 ms. Over a real Redis there is no measurable difference between OTel on and off ([#4406](https://github.com/redis/go-redis/pull/4406), [#4407](https://github.com/redis/go-redis/pull/4407), [#4408](https://github.com/redis/go-redis/pull/4408)) by [@ndyakov](https://github.com/ndyakov).

### Per-client `OTelRecorder`

Each client can have its own metrics recorder. Set `OTelRecorder` in `Options`, `ClusterOptions`, `RingOptions`, `FailoverOptions`, `UniversalOptions` or `MultiDBOptions`. The client sends its commands, pipelines, errors, cluster redirects, Pub/Sub messages, stream lag, and pool and connection events to it. Cluster nodes, ring shards, sentinel clients and MultiDB members use the recorder of their parent. Clients without it use the recorder from `redis.SetOTelRecorder`, as before. In `redisotel-native`, `NewRecorder` makes a recorder with its own `WithCustomAttributes`, which take precedence over the attributes of the global `Init` config ([#4179](https://github.com/redis/go-redis/pull/4179)) by [@vladvildanov](https://github.com/vladvildanov).

```go
inst := redisotel.GetObservabilityInstance()
_ = inst.Init(redisotel.NewConfig().WithEnabled(true).WithMeterProvider(provider))
defer inst.Shutdown()

acmeRec, _ := inst.NewRecorder(redisotel.NewConfig().
	WithCustomAttributes(attribute.String("tenant", "acme")))
acme := redis.NewClient(&redis.Options{Addr: "acme:6379", OTelRecorder: acmeRec})
```

### Idle connection reaper

`ConnMaxLifetimeReapInterval` (on `Options`, `ClusterOptions`, `FailoverOptions`, `RingOptions` and `UniversalOptions`, and `conn_max_lifetime_reap_interval` in URLs) starts a background goroutine that closes idle connections past `ConnMaxLifetime` or `ConnMaxIdleTime` and refills the pool toward `MinIdleConns`, so expired connections are closed off the request path. It is off by default. When it is on, it also closes connections that were idle longer than `ConnMaxIdleTime`, 30 minutes by default. Connections in use, and connections that a re-auth or handoff worker owns, are skipped, and a refill toward `MinIdleConns` adds dials at low concurrency ([#4334](https://github.com/redis/go-redis/pull/4334), [#4340](https://github.com/redis/go-redis/pull/4340)) by [@ndyakov](https://github.com/ndyakov).

### More new commands

- `BACKUP START`, `SEAL`, `ABORT`, `CLEANUP` and `STATUS` for Redis 8.10 ([#4286](https://github.com/redis/go-redis/pull/4286)) by [@Purv-Kabaria](https://github.com/Purv-Kabaria).
- `ACLGetUser` returns an `ACLUser` with the flags, passwords, command, key and channel rules and the selectors of a user, for Redis 7.0 and later. A missing user returns `redis.Nil` ([#4357](https://github.com/redis/go-redis/pull/4357)) by [@ruziba3vich](https://github.com/ruziba3vich).
- `ClusterSlotStats` and `ClusterSlotStatsOrderBy` run `CLUSTER SLOT-STATS` with a slot range or `ORDERBY`, for Redis 8.2 and later. On a `ClusterClient`, `MultiDBClient` and `AutoPipeliner` the call asks every master and merges the replies, so the result covers the whole cluster and the top slots are global, as with `DBSize`. A pipeline still sends the command to one node. The slot metrics beyond the key count need `cluster-slot-stats-enabled yes` on the servers ([#4358](https://github.com/redis/go-redis/pull/4358)) by [@ruziba3vich](https://github.com/ruziba3vich).

## ⚠️ Behavior changes

Check these when you upgrade from 9.23.0:

- **Command retries no longer replay a failed dial sequence:** for clients without a custom `Dialer` (single commands, pipelines, `Watch`, `Conn` and `Tx`, the Sentinel client, Ring shards and cluster nodes), `MaxRetries` no longer starts a new `DialerRetries` sequence on an endpoint where one has already failed. With default options a refused connection is now reported after about 0.4s, where it took about 1.7s before. Attempts on other cluster nodes or Ring shards are kept, and a custom `Dialer` keeps its previous retry behavior. `DialTimeout` still applies to each attempt, so a dial that hangs is still bounded by `DialTimeout` times `DialerRetries`. A command that reaches an exhausted node or shard skips that address and keeps its bounded retries for the others, and a cluster client asks for a topology reload, so a replacement can be reached in the same call. An unpinned `SCAN` that exhausts a node also falls back to the replicas that `ReadOnly` routing allows ([#4304](https://github.com/redis/go-redis/pull/4304), [#4331](https://github.com/redis/go-redis/pull/4331)) by [@xixi-box](https://github.com/xixi-box), issue [#4329](https://github.com/redis/go-redis/issues/4329).
- **`ContextDeadlineError` for socket timeouts that come from a context deadline** ([#4263](https://github.com/redis/go-redis/pull/4263)) by [@aditya-rai-5](https://github.com/aditya-rai-5). With `ContextTimeoutEnabled: true`, a socket timeout caused by the deadline of the caller's context now matches `context.DeadlineExceeded` with `errors.Is`, and still matches `net.Error` with `Timeout() == true`. The complete original transport error stays in the chain. Such an error is no longer retried, because the context has already expired, and its message starts with `context deadline exceeded:`. A read timeout that arrives together with data no longer reaches the next command, and a socket timeout that fires slightly before the context deadline is still reported as `context.DeadlineExceeded` ([#4306](https://github.com/redis/go-redis/pull/4306)) by [@PerryLink](https://github.com/PerryLink), issue [#4305](https://github.com/redis/go-redis/issues/4305).
- **`WithTimeout` keeps hooks:** a client returned by `WithTimeout` now runs the hooks of its parent. Before, it dropped them, so hooks added with `AddHook` did not run for commands, pipelines and transactions on that client ([#4095](https://github.com/redis/go-redis/pull/4095)) by [@CaliLuke](https://github.com/CaliLuke).
- **CSC eligibility comes from the `COMMAND` metadata and fails closed** ([#3960](https://github.com/redis/go-redis/pull/3960)) by [@ofekshenawa](https://github.com/ofekshenawa). An unknown, malformed, blocking, nondeterministic or script-based command runs as usual but is not cached. `JSON.MGET` and `TS.INFO` are no longer cached by default. With CSC enabled, more eligible commands can be cached in the default `Static` metadata mode (the `BF.*`, `CF.*`, `TOPK.*`, `TDIGEST.*`, `V*` and `AR*` families, `CMS.QUERY`, `EXPIRETIME`, `PEXPIRETIME`, `HEXPIRETIME`, `HPEXPIRETIME`, `PFCOUNT`, `SDIFFCARD`, `SUNIONCARD`, `ZINTERCARD` and others). `Command()` results now also contain subcommand entries named `parent|child`, for example `client|id`. When built-in CSC is on, a raw `CLIENT CACHING` command is rejected, like the other commands that change connection state ([#4193](https://github.com/redis/go-redis/pull/4193)) by [@ofekshenawa](https://github.com/ofekshenawa).
- **Connection-pool limits and dials:** `MaxActiveConns` now counts all connections registered with the pool (pooled, excess, and dedicated `NewConn` connections) plus in-flight dial reservations. Before, admission checked only the pooled count, so clients with this limit can now receive `ErrPoolExhausted` from `NewConn`/`EphemeralConn` or during bursts that previously exceeded it. `NewConn` now waits for the shared `MaxConcurrentDials` limit and honors context cancellation while waiting, and idle prewarming shares this limit without holding a pool turn. These changes apply to all clients using the pool, not only CSC ephemeral sessions ([#4174](https://github.com/redis/go-redis/pull/4174)) by [@aditya-rai-5](https://github.com/aditya-rai-5).
- **Struct scan on a bad field:** `Scan` on `HGETALL`/`HMGET` results used to stop at the first field that failed to decode, and with `HGETALL` the set of filled fields was random. It now sets every field it can decode and still returns an error. When several fields fail, their errors are joined with `errors.Join`, in key order for `HGETALL` and argument order for `HMGET`, so `errors.Is` and `errors.As` match each of them. For the built-in types, a field that fails to decode keeps its previous value. A custom decoder (`ScanRedis`, `UnmarshalText` or `UnmarshalBinary`) runs on the existing field and can leave a failed field changed. Code that reuses the struct after an error will see more fields set ([#4136](https://github.com/redis/go-redis/pull/4136) by [@lazerg](https://github.com/lazerg), [#2255](https://github.com/redis/go-redis/pull/2255) by [@cpoole](https://github.com/cpoole), [#4294](https://github.com/redis/go-redis/pull/4294) by [@Avv123](https://github.com/Avv123)).
- **URL and option validation:** `ParseURL`, `ParseClusterURL` and `ParseFailoverURL` now reject negative `dialer_retry_timeout`, `read_buffer_size` and `write_buffer_size` values, and a negative `pool_size`, which used to panic. Replace negative retry timeouts with `0` for no delay, or omit the parameter for the default 100 ms delay; an explicit `dialer_retry_timeout=0` now disables the delay ([#4144](https://github.com/redis/go-redis/pull/4144)) by [@Samuel-Sorial](https://github.com/Samuel-Sorial). A negative `PoolSize` in the options structs selects the default ([#4325](https://github.com/redis/go-redis/pull/4325)) by [@ump45nose](https://github.com/ump45nose). `ParseURL` and `ParseFailoverURL` also reject a negative database number ([#4275](https://github.com/redis/go-redis/pull/4275)) and a plain-second duration that overflows `int64` nanoseconds ([#4276](https://github.com/redis/go-redis/pull/4276)) by [@ump45nose](https://github.com/ump45nose). A disabled `dial_timeout` (`0` or a negative value in the URL) now means no dial deadline. Before, every dial failed at once ([#4168](https://github.com/redis/go-redis/pull/4168)) by [@PerryLink](https://github.com/PerryLink).
- **Empty password with a username (opt-in):** a `Username` with an empty `Password` sends no `AUTH`, the same as 9.23.0. To authenticate an ACL user created with `nopass`, set `AuthEmptyPassword: true` (or `auth_empty_password=true` in the URL); for sentinel connections use `SentinelAuthEmptyPassword` (`sentinel_auth_empty_password=true`). The rule also applies to credentials from `CredentialsProvider`, `CredentialsProviderContext` and `StreamingCredentialsProvider` used when a connection is set up; re-authentication on a credentials refresh is unchanged. Redis Enterprise and Redis Cloud databases with unauthenticated access can reject an empty password, which is why this is opt-in. The client-side cache keeps a separate namespace for a username that sends no `AUTH` ([#4161](https://github.com/redis/go-redis/pull/4161) by [@saddamr3e](https://github.com/saddamr3e), [#4194](https://github.com/redis/go-redis/pull/4194) by [@ndyakov](https://github.com/ndyakov)).
- **Ring telemetry:** `redisotel.InstrumentTracing` and `InstrumentMetrics` now instrument existing shards. Remove any per-shard instrumentation workaround when instrumenting the ring to avoid duplicate spans and metrics ([#4227](https://github.com/redis/go-redis/pull/4227)) by [@nk-batman21](https://github.com/nk-batman21).
- **OpenTelemetry metrics:** `db.client.connection.count` and `db.client.connection.pending_requests` are now asynchronous instruments, so views or selectors keyed on the instrument kind match them differently. A series that exists is reported at every collection, and the series of a closed pool stay at 0 until `Shutdown`. `OTelConnectionCounter.RecordConnectionCount` is no longer called and is deprecated; a custom recorder that built its own count from it should implement `OTelPoolRegistrar` and read `PoolStats()` ([#4406](https://github.com/redis/go-redis/pull/4406), [#4408](https://github.com/redis/go-redis/pull/4408)) by [@ndyakov](https://github.com/ndyakov). In `redisotel-native`, only a server error reply gets `redis:<PREFIX>` as `error.type`, the `server` category and `db.response.status_code`. A closed connection (`io.EOF`) is now `eof` in the `network` category with no status code, and a server error wrapped by a hook keeps its prefix ([#4391](https://github.com/redis/go-redis/pull/4391)) by [@ruziba3vich](https://github.com/ruziba3vich).
- **Full-duplex clients keep one idle spare connection:** a full-duplex engine raises `MinIdleConns` of its pipeline pool to 1, so the pool holds one extra idle connection for each client (for each node on a cluster). Set `PipelineMinIdleConns` to a negative value to opt out ([#4101](https://github.com/redis/go-redis/pull/4101)) by [@ndyakov](https://github.com/ndyakov).
- **Replies nested deeper than 1000 levels fail:** the RESP reader returns an error for a reply with more than 1000 levels of nested aggregates or attributes, instead of exhausting the stack ([#4296](https://github.com/redis/go-redis/pull/4296)) by [@xixi-box](https://github.com/xixi-box).
- **`Script.Load` keeps the client-computed SHA-1:** a `Script` made with `NewScript` no longer takes the digest from the `SCRIPT LOAD` reply, so a server that replies with another digest cannot redirect later `Run`/`RunRO` calls. `NewScriptServerSHA` still uses the server digest ([#3943](https://github.com/redis/go-redis/pull/3943)) by [@saddamr3e](https://github.com/saddamr3e).
- **`PubSub.Channel` misuse does not panic:** calling `Channel` and `ChannelWithSubscriptions` on one `PubSub` now returns a closed channel and logs a warning. `Ring.Subscribe`, `PSubscribe` and `SSubscribe` with no channels, or a failed shard lookup, return a `PubSub` that carries the error ([#3968](https://github.com/redis/go-redis/pull/3968)) by [@VedantMadane](https://github.com/VedantMadane).
- **Search, `Cmd` and return values:** a search that sets `LimitOffset` and leaves `Limit` at 0 now returns up to 10 documents from that offset. On Redis 7 it returned an empty page before, and on Redis 8 it was an error ([#4285](https://github.com/redis/go-redis/pull/4285)) by [@kevin9327](https://github.com/kevin9327). `FTHybridWithArgs` sends `WINDOW` for `COMBINE LINEAR` as well as for `RRF`, where a `Window` with `LINEAR` was ignored ([#4379](https://github.com/redis/go-redis/pull/4379)) by [@ruziba3vich](https://github.com/ruziba3vich). `Cmd.Uint64` of a negative integer returns an error, where it returned the wrapped value ([#4369](https://github.com/redis/go-redis/pull/4369)) by [@ruziba3vich](https://github.com/ruziba3vich). `XInfoStreamFull` reports a `-1` consumer-group lag when Redis replies with a nil lag ([#4217](https://github.com/redis/go-redis/pull/4217)), and `ClusterClient.ScriptFlush` returns `OK` ([#4252](https://github.com/redis/go-redis/pull/4252)) by [@LindseyZ1205](https://github.com/LindseyZ1205).
- **New interface methods:** `Cmdable`, `ClusterCmdable` and their sub-interfaces gain methods for the new commands (`ACLGetUser`, `ClusterSlotStats`, `ClusterSlotStatsOrderBy`, the 8.10 and 8.12 commands and `BlessCmdable`). This is the usual way go-redis adds commands. A custom type that implements these interfaces, for example a mock, must add the methods.

## ✨ New Features

- **`FT.AGGREGATE` steps:** `FILTER` and `LIMIT` are now steps in `FTAggregateOptions.Steps`, so you can filter before `GROUPBY`, filter more than once, and place `LIMIT` between steps. `AggregateBuilder` gets `Limit()`. The old tail fields `Filter` and `LimitOffset`/`Limit` still work, are sent after all steps, and are deprecated in favor of steps ([#4076](https://github.com/redis/go-redis/pull/4076)) by [@ndyakov](https://github.com/ndyakov)
- **Per-stream IDs for `XRead`:** `XReadArgs.IDs` gives one ID for each stream, in the order of `Streams`, and takes precedence over `ID` ([#4203](https://github.com/redis/go-redis/pull/4203)) by [@aozmekik](https://github.com/aozmekik)
- **Native iterators for `ScanIterator`:** `ScanIterator.Vals(ctx)` returns an `iter.Seq[string]` for use with `range` ([#3348](https://github.com/redis/go-redis/pull/3348)) by [@khasanovbi](https://github.com/khasanovbi)
- **More URL options:** `ParseURL`, `ParseClusterURL` and `ParseFailoverURL` accept `dialer_retries`, `dialer_retry_timeout`, `context_timeout_enabled`, `read_buffer_size`, `write_buffer_size`, `disable_identity` and `identity_suffix` where the options struct has the field ([#4063](https://github.com/redis/go-redis/pull/4063)) by [@SulimanAbdulrazzaq](https://github.com/SulimanAbdulrazzaq). `ParseClusterURL` also accepts `?skip_verify=true` on `rediss://` URLs ([#4106](https://github.com/redis/go-redis/pull/4106)) and `tls_verify_node_host=true`, which clears `TLSConfig.ServerName` so each node is verified against the host it is dialed at and not against the host of the first URL. The default is unchanged, and `rediss://` cluster URLs now set `MinVersion: tls.VersionTLS12`, as `ParseURL` and `ParseFailoverURL` do ([#4145](https://github.com/redis/go-redis/pull/4145)) by [@PragalvaXFREZ](https://github.com/PragalvaXFREZ)
- **Context-aware credential subscriptions:** the optional `auth.ContextStreamingCredentialsProvider` interface lets the client call `SubscribeWithContext` when it sets up a connection, so the context bounds the wait for the first credentials. Providers without the method keep `Subscribe` ([#4249](https://github.com/redis/go-redis/pull/4249)) by [@PragalvaXFREZ](https://github.com/PragalvaXFREZ)
- **`TSCreateRuleOptions.AlignTimestamp`:** the field is now exported, so callers outside the package can set the bucket alignment for `TSCreateRuleWithArgs` ([#4280](https://github.com/redis/go-redis/pull/4280)) by [@pcbeingused333](https://github.com/pcbeingused333)

## 🐛 Bug Fixes

**Connections, pool and transactions**

- **Connection handshake and `Options.Limiter`:** the handshake of a new connection (`HELLO`, `AUTH`, `SELECT`, `CLIENT SETINFO`, `CLIENT MAINT_NOTIFICATIONS`) no longer calls `Limiter.Allow` per command. The command that dials pays one admission and reports the handshake result once, and a PubSub connection, a reconnect after a handoff and a fresh full-duplex session each pay one. Before, a half-open circuit breaker spent its probe budget on the handshake and never closed ([#4232](https://github.com/redis/go-redis/pull/4232)) by [@othoathydrahost](https://github.com/othoathydrahost). A panic in an `OnConnect` or credentials hook now settles the admission ([#4247](https://github.com/redis/go-redis/pull/4247)) by [@ump45nose](https://github.com/ump45nose)
- **Silent reply mix-up after a maintenance handoff:** with maintenance notifications on, a `MOVING` handoff under concurrent traffic could give one connection two owners. Both wrote on the same socket and read each other's replies, so a `GET` could return the `"OK"` of a `SET`, with no error ([#4110](https://github.com/redis/go-redis/pull/4110), [#4239](https://github.com/redis/go-redis/pull/4239)) by [@ndyakov](https://github.com/ndyakov). A completed handoff is now removed from the tracked `MOVING` operations, so `GetState()` no longer stays `StateMoving` ([#4392](https://github.com/redis/go-redis/pull/4392)), and the handoff queue of a nil config is capped by `MaxActiveConns` like any other ([#4389](https://github.com/redis/go-redis/pull/4389)) by [@ruziba3vich](https://github.com/ruziba3vich)
- **Pool turns and idle connections:** `Get` frees its pool turn when an `OnGet` hook rejects a new connection, so the pool no longer loses a slot on each such failure ([#4396](https://github.com/redis/go-redis/pull/4396)) by [@ruziba3vich](https://github.com/ruziba3vich). An idle scan that skipped an unusable connection and acquired a usable one on its last attempt no longer loses it from the idle list ([#4322](https://github.com/redis/go-redis/pull/4322)) by [@xixi-box](https://github.com/xixi-box). Idle TLS connections that the server closed are retired and the command runs again without spending a retry ([#4197](https://github.com/redis/go-redis/pull/4197)) by [@dunglas](https://github.com/dunglas)
- **Credential rotation with `MaxActiveConns`:** at the cap, `Get` now waits within `PoolTimeout` for an idle connection that is re-authenticating, and does not fail with `ErrPoolExhausted` ([#4313](https://github.com/redis/go-redis/pull/4313)) by [@xixi-box](https://github.com/xixi-box), issue [#4311](https://github.com/redis/go-redis/issues/4311). A race at the cap, where the only connection finished re-authentication just before the capacity check, is closed by the same release ([#4333](https://github.com/redis/go-redis/pull/4333)) by [@Tidehyc](https://github.com/Tidehyc), issue [#4332](https://github.com/redis/go-redis/issues/4332)
- **Transactions:** `TxPipelined` returns an error and drops the connection when `EXEC` replies with a different number of results than the number of queued commands, and a rejected `MULTI` drops the connection ([#4186](https://github.com/redis/go-redis/pull/4186), [#4192](https://github.com/redis/go-redis/pull/4192)) by [@saddamr3e](https://github.com/saddamr3e). `Tx.Close` requires an `OK` reply to `UNWATCH` and removes the connection when it cannot confirm a clean state ([#4316](https://github.com/redis/go-redis/pull/4316)) by [@xixi-box](https://github.com/xixi-box)
- **Pipeline setup errors:** when connection setup fails with a Redis error reply (for example `WRONGPASS`), each queued command now gets the error, where `Pipeline.Exec` returned it but every command had `Err() == nil` ([#4312](https://github.com/redis/go-redis/pull/4312)) by [@xh123637](https://github.com/xh123637)
- **TLS dial and PubSub:** a TLS dial honors the caller's context deadline and cancellation during the handshake ([#4143](https://github.com/redis/go-redis/pull/4143)) by [@Samuel-Sorial](https://github.com/Samuel-Sorial). A `PubSub` connection that finished its setup after `Client.Close` drained the pool is closed and returns `ErrClosed` ([#4157](https://github.com/redis/go-redis/pull/4157)) by [@aditya-rai-5](https://github.com/aditya-rai-5). A subscribed `PubSub` connection that gets a `MOVING` with a null endpoint stays under the control of the `PubSub`, so `Channel()` keeps delivering messages ([#4246](https://github.com/redis/go-redis/pull/4246)) by [@ump45nose](https://github.com/ump45nose)
- **Retry limits:** any `MaxRetries` or `MaxRedirects` below -1 now means one attempt with no retries, the same as -1. Before, a value such as -2 skipped the retry loop, so some commands were never sent and the call returned a nil error ([#4349](https://github.com/redis/go-redis/pull/4349)) by [@ruziba3vich](https://github.com/ruziba3vich). The cluster wording `max number of clients + cluster connections reached` is also matched by `shouldRetry` and `IsMaxClientsError` ([#4367](https://github.com/redis/go-redis/pull/4367)) by [@ruziba3vich](https://github.com/ruziba3vich)

**Authentication and credentials**

- **Pub/Sub re-authentication with `StreamingCredentialsProvider` on RESP3:** credential rotations now re-authenticate established Pub/Sub connections in place ([#4195](https://github.com/redis/go-redis/pull/4195)) by [@AdamMagued](https://github.com/AdamMagued). Re-authentication is serialized for each connection ([#4317](https://github.com/redis/go-redis/pull/4317)) by [@Nacl-HCl](https://github.com/Nacl-HCl), skips a connection that was replaced after the rotation ([#4309](https://github.com/redis/go-redis/pull/4309)) by [@ump45nose](https://github.com/ump45nose), and a close during subscription setup no longer panics ([#4262](https://github.com/redis/go-redis/pull/4262)) by [@PragalvaXFREZ](https://github.com/PragalvaXFREZ)
- **Rejected re-authentication:** the connection is closed when the server rejects a re-auth with `WRONGPASS` or `NOPERM`, where it kept serving commands with the replaced credentials ([#4058](https://github.com/redis/go-redis/pull/4058)) by [@saddamr3e](https://github.com/saddamr3e)
- **`StreamingCredentialsProvider` with derived and failover clients:** `Conn`, `EphemeralConn` and `Watch` carry the credentials manager, so a cold connection no longer panics ([#4155](https://github.com/redis/go-redis/pull/4155)) by [@TastyHeadphones](https://github.com/TastyHeadphones), and `NewFailoverClient` creates it ([#4182](https://github.com/redis/go-redis/pull/4182)) by [@lazerg](https://github.com/lazerg). A closed Pub/Sub connection no longer stays in memory with its buffers ([#4119](https://github.com/redis/go-redis/pull/4119)) by [@meruiden](https://github.com/meruiden)
- **CSC cache and TLS client certificates:** clients that authenticate with different TLS client certificates (`tls-auth-clients-user`) no longer share cache entries, so a restricted user cannot read entries that a privileged user fetched ([#4261](https://github.com/redis/go-redis/pull/4261)) by [@saddamr3e](https://github.com/saddamr3e)
- **Passwords in traces and logs:** `rediscmd.CmdString` redacts the `HELLO` password when `AUTH` comes after `SETNAME` ([#4348](https://github.com/redis/go-redis/pull/4348)), and prints the value of a pointer argument to a predeclared type or `time.Time` where it printed an address. The redaction of secrets such as `CONFIG SET requirepass` also covers a keyword or password passed behind a pointer or as a named string ([#4363](https://github.com/redis/go-redis/pull/4363)) by [@ruziba3vich](https://github.com/ruziba3vich)

**Cluster, Sentinel and Ring**

- **Redirects without a host:** with `cluster-preferred-endpoint-type unknown-endpoint`, a null host in `CLUSTER SLOTS` takes the host of the node that was asked ([#4131](https://github.com/redis/go-redis/pull/4131)) by [@spicysharkco](https://github.com/spicysharkco), `MOVED`/`ASK` replies without a host reuse the host of the node that sent them ([#4205](https://github.com/redis/go-redis/pull/4205)) by [@soujasK](https://github.com/soujasK), and after a maintenance handoff the client uses the host of the serving connection ([#4233](https://github.com/redis/go-redis/pull/4233)) by [@ump45nose](https://github.com/ump45nose). A `MOVED`/`ASK` to an IPv6 address that ends in a dotted quad, such as `::ffff:10.0.0.1`, is bracketed and dialable ([#4350](https://github.com/redis/go-redis/pull/4350)) by [@ruziba3vich](https://github.com/ruziba3vich)
- **Redirect handling:** the client drops the connection when `MOVED` points to the node that sent it ([#4229](https://github.com/redis/go-redis/pull/4229)) by [@SergeiSadov](https://github.com/SergeiSadov). When the first reply of a node batch is retryable and a later command gets `MOVED` or `ASK`, the redirected command is no longer sent twice, where a non-idempotent `INCR` could run two times ([#4258](https://github.com/redis/go-redis/pull/4258)) by [@lazerg](https://github.com/lazerg)
- **Cluster options and aggregation:** a `-1` retry backoff set on `ClusterOptions` now reaches the node clients ([#4345](https://github.com/redis/go-redis/pull/4345)), and reusing one `*ClusterOptions` for a second client no longer turns disabled options back into their defaults ([#4360](https://github.com/redis/go-redis/pull/4360)) by [@ruziba3vich](https://github.com/ruziba3vich). The response aggregators no longer panic when two shards reply with different Go types ([#4397](https://github.com/redis/go-redis/pull/4397)) by [@ruziba3vich](https://github.com/ruziba3vich)
- **`NewFailoverClusterClient`:** `ConnMaxLifetimeJitter` and the failing timeout are kept ([#4196](https://github.com/redis/go-redis/pull/4196)) by [@nk-batman21](https://github.com/nk-batman21), and the `onClose` hook is registered ([#4000](https://github.com/redis/go-redis/pull/4000)) by [@roganartu](https://github.com/roganartu)
- **Sentinel:** the failover dialer uses `DialTimeout`, so `Subscribe` to an unreachable master does not hang until the kernel gives up ([#4067](https://github.com/redis/go-redis/pull/4067)) by [@lazerg](https://github.com/lazerg). Unused replica clients from empty or filtered discovery replies are closed ([#4220](https://github.com/redis/go-redis/pull/4220)) by [@Grivn](https://github.com/Grivn)
- **Ring and cluster commands:** `SetAddrs` no longer leaks a client on each call when two shard names point to the same address ([#4356](https://github.com/redis/go-redis/pull/4356)) by [@ruziba3vich](https://github.com/ruziba3vich). `ClusterAddSlotsRange` and `ClusterDelSlotsRange` return an error for an invalid range ([#4225](https://github.com/redis/go-redis/pull/4225)) by [@ump45nose](https://github.com/ump45nose), and `ClusterShards` rejects an odd-length `slots` array ([#4282](https://github.com/redis/go-redis/pull/4282)) by [@saddamr3e](https://github.com/saddamr3e)

**Protocol and reply parsing**

- **Error elements inside arrays:** `IntSliceCmd` (`TS.MADD`, `CMS.INCRBY`), `UintSliceCmd`, `FloatSliceCmd`, `StringSliceCmd` and `BoolSliceCmd` now read the whole array when an element is a Redis error, where they stopped at that element and the next pipeline command read the rest as its own reply ([#4071](https://github.com/redis/go-redis/pull/4071) by [@saddamr3e](https://github.com/saddamr3e), [#4137](https://github.com/redis/go-redis/pull/4137) by [@lazerg](https://github.com/lazerg)). The reader also keeps typed errors (`MOVED`, `ASK`, `LOADING`) as array elements ([#4274](https://github.com/redis/go-redis/pull/4274)) by [@ndyakov](https://github.com/ndyakov)
- **Reply desync hardening:** `ReadSlice` checks the type of the header ([#4219](https://github.com/redis/go-redis/pull/4219)) and `VoidProcessor` consumes push frames whose first element is not a string ([#4046](https://github.com/redis/go-redis/pull/4046)) by [@saddamr3e](https://github.com/saddamr3e). `Discard` counts a RESP3 attribute and the element it decorates as one element ([#4318](https://github.com/redis/go-redis/pull/4318)) by [@waw666waw666](https://github.com/waw666waw666). A buffered push drain stops at a frame boundary, so a slow handler no longer makes a healthy connection fail with `i/o timeout` and drops the CSC entries fetched on it ([#4224](https://github.com/redis/go-redis/pull/4224)) by [@ndyakov](https://github.com/ndyakov)
- **`Cmd` accessors and scanning:** `Cmd.Float64`, `Float32` and their slice forms accept RESP3 doubles ([#4088](https://github.com/redis/go-redis/pull/4088)) by [@LindseyZ1205](https://github.com/LindseyZ1205), and `Text`, `Int64`, `StringSlice` and related accessors accept RESP3 doubles and big numbers ([#4138](https://github.com/redis/go-redis/pull/4138)) by [@lazerg](https://github.com/lazerg). Scanning a value that does not fit in `uint` returns an error on 32-bit platforms ([#4081](https://github.com/redis/go-redis/pull/4081)) by [@MokiMeow](https://github.com/MokiMeow), and `Cmd.Int()` rejects an out-of-range integer reply ([#4256](https://github.com/redis/go-redis/pull/4256)) by [@b84330808](https://github.com/b84330808)
- **Parsing details:** the `CLIENT INFO` parser accepts `=` in values such as the client name and `lib-name` ([#4096](https://github.com/redis/go-redis/pull/4096)) by [@LindseyZ1205](https://github.com/LindseyZ1205). `HGetAll(...).Scan` and `HMGet(...).Scan` read a `net.IP` that `HSet` stored as raw bytes ([#4362](https://github.com/redis/go-redis/pull/4362)) by [@ruziba3vich](https://github.com/ruziba3vich), and the value passed to a `TextUnmarshaler` or `BinaryUnmarshaler` is a copy ([#4078](https://github.com/redis/go-redis/pull/4078)) by [@saddamr3e](https://github.com/saddamr3e)
- **`Clone` of finished commands:** `XInfoStreamFullCmd.Clone` keeps all fields and no longer shares the pending storage of its groups and consumers ([#4372](https://github.com/redis/go-redis/pull/4372)). The vector set slice commands copy their result slice ([#4373](https://github.com/redis/go-redis/pull/4373)). `FunctionStatsCmd.Clone` copies the command of the running script ([#4378](https://github.com/redis/go-redis/pull/4378)). Clones of `XMessageSliceCmd`, `XStreamSliceCmd`, `XInfoStreamCmd`, `ClientInfoCmd`, `ACLLogCmd` and `FTSearchCmd` keep every field of the value, such as the `XREADGROUP` claim fields and `Document.Error` ([#4346](https://github.com/redis/go-redis/pull/4346)). By [@ruziba3vich](https://github.com/ruziba3vich)

**Commands, search, JSON and probabilistic types**

- **RediSearch:** `FT.SEARCH` on RESP2 reads the per-document score, payload and sort key correctly with `NoContent` ([#4099](https://github.com/redis/go-redis/pull/4099)) by [@jayhemnani9910](https://github.com/jayhemnani9910), and a stored JSON null field no longer fails the whole search ([#4370](https://github.com/redis/go-redis/pull/4370)) by [@ruziba3vich](https://github.com/ruziba3vich). `EXPLAINSCORE` and an empty `RETURN` parse correctly ([#4140](https://github.com/redis/go-redis/pull/4140)) by [@lazerg](https://github.com/lazerg). `FTInfo` reads fractional doubles on RESP2 ([#4207](https://github.com/redis/go-redis/pull/4207)) by [@ump45nose](https://github.com/ump45nose), the `CASESENSITIVE` flag of a TAG field ([#4291](https://github.com/redis/go-redis/pull/4291)) by [@Dv2Corn](https://github.com/Dv2Corn), `index_definition` by key ([#4347](https://github.com/redis/go-redis/pull/4347)) and a TAG separator of `m` or `M` ([#4371](https://github.com/redis/go-redis/pull/4371)) by [@ruziba3vich](https://github.com/ruziba3vich). `FTCreate` sends `SORTABLE`, `UNF` and `NOINDEX` after the field options ([#4199](https://github.com/redis/go-redis/pull/4199)) by [@RaphaelFakhri](https://github.com/RaphaelFakhri) and `MAXTEXTFIELDS` as a bare flag ([#4344](https://github.com/redis/go-redis/pull/4344)) by [@ruziba3vich](https://github.com/ruziba3vich). `FTSearchQuery` applies `CountOnly` and keeps the offset ([#4268](https://github.com/redis/go-redis/pull/4268)) by [@RaphaelFakhri](https://github.com/RaphaelFakhri). `NewDropIndexBuilder(...).DeleteDocs()` does not panic ([#4105](https://github.com/redis/go-redis/pull/4105)) by [@PragalvaXFREZ](https://github.com/PragalvaXFREZ)
- **`FT.HYBRID` and search arguments:** `FTHybridWithArgs` always sends the `COMBINE` argument count ([#4298](https://github.com/redis/go-redis/pull/4298)) by [@lazerg](https://github.com/lazerg), counts `SHARD_K_RATIO` in the `KNN` block ([#4353](https://github.com/redis/go-redis/pull/4353)), sends both weights when only `Alpha` or `Beta` is set ([#4354](https://github.com/redis/go-redis/pull/4354)), and the search commands no longer send `PARAMS 0` for an empty map ([#4355](https://github.com/redis/go-redis/pull/4355)) by [@ruziba3vich](https://github.com/ruziba3vich).
- **JSON:** `JSONObjLen`, `JSONStrLen`, `JSONStrAppend` and `JSONToggle` no longer fail when the path does not start with `$` ([#4337](https://github.com/redis/go-redis/pull/4337)), the array commands parse a legacy path and one such reply no longer shifts the replies after it in a pipeline ([#4359](https://github.com/redis/go-redis/pull/4359)), `JSONArrTrim` sends both `start` and `stop` ([#4361](https://github.com/redis/go-redis/pull/4361)), and `JSONNumIncrBy` with RESP3 returns the same string as RESP2 for a path with non-numeric matches ([#4338](https://github.com/redis/go-redis/pull/4338)) by [@ruziba3vich](https://github.com/ruziba3vich). `JSONGetWithArgs` with `nil` options sends the paths ([#4185](https://github.com/redis/go-redis/pull/4185)) by [@LindseyZ1205](https://github.com/LindseyZ1205)
- **Bloom, Cuckoo and Count-Min:** `BFInfo` of a non-scaling filter and a single-option `BF.INFO` no longer fail ([#4336](https://github.com/redis/go-redis/pull/4336), [#4368](https://github.com/redis/go-redis/pull/4368)), `CFScanDump` does not fail on its last call ([#4366](https://github.com/redis/go-redis/pull/4366)), and `BFInsert`, `BFMAdd`, `BFMExists`, `CFInsert`, `CFInsertNX`, `CFMExists` and `CMSQuery` expand a single `[]string` or `[]interface{}` ([#4377](https://github.com/redis/go-redis/pull/4377)) by [@ruziba3vich](https://github.com/ruziba3vich)
- **`nil` options no longer panic:** `FTExplainWithArgs`, `FTSynUpdateWithArgs` and `CFReserveWithArgs` ([#4248](https://github.com/redis/go-redis/pull/4248)) by [@hktitof](https://github.com/hktitof), `HGetEXWithArgs` and `HSetEXWithArgs` ([#4214](https://github.com/redis/go-redis/pull/4214)) by [@LindseyZ1205](https://github.com/LindseyZ1205)
- **Argument order and count:** `IGNORE` is sent before `LABELS` in the five time-series builders that support it ([#4160](https://github.com/redis/go-redis/pull/4160)) by [@yuefdev](https://github.com/yuefdev). `ModuleLoadex` sends `ARGS` once ([#4191](https://github.com/redis/go-redis/pull/4191)) by [@LindseyZ1205](https://github.com/LindseyZ1205). `HSet` and `HMSet` flatten typed `map[string]T` arguments such as `map[string]int` ([#4235](https://github.com/redis/go-redis/pull/4235)) by [@ump45nose](https://github.com/ump45nose). `FCall` and `FCallRO` expand a `[]string` or `[]interface{}` argument ([#4365](https://github.com/redis/go-redis/pull/4365)) by [@ruziba3vich](https://github.com/ruziba3vich)
- **Timeouts, geo and scripts:** `XRead`, `XReadGroup`, `XClaim`, `Wait` and `WaitAOF` no longer send a positive timeout shorter than 1 ms as `0`, which means wait forever ([#4292](https://github.com/redis/go-redis/pull/4292)) by [@Dv2Corn](https://github.com/Dv2Corn). `GeoRadius` and `GeoRadiusByMember` no longer change the caller's `*GeoRadiusQuery` ([#4173](https://github.com/redis/go-redis/pull/4173)) by [@LindseyZ1205](https://github.com/LindseyZ1205), and `GeoSearchLocation` with none of the `With*` flags no longer fails with a parse error ([#4335](https://github.com/redis/go-redis/pull/4335)) by [@ruziba3vich](https://github.com/ruziba3vich). A queued `SCRIPT LOAD` no longer gives an empty digest to `NewScriptServerSHA` ([#4226](https://github.com/redis/go-redis/pull/4226)), and the `HOTKEYS` CPU time fields use the names that the server sends ([#4206](https://github.com/redis/go-redis/pull/4206)) by [@ump45nose](https://github.com/ump45nose)

**Observability**

- **`redisotel-native`:** `server.port` no longer contains the pool id ([#4086](https://github.com/redis/go-redis/pull/4086)) by [@zhechen-cs](https://github.com/zhechen-cs). The connection count includes connections that existed before `Init` ([#4089](https://github.com/redis/go-redis/pull/4089)), and pending requests and state no longer drift across `Init` and `Shutdown` ([#4142](https://github.com/redis/go-redis/pull/4142)) by [@ninadsutrave](https://github.com/ninadsutrave). The pool usage metric does not underflow when pool statistics disagree ([#4031](https://github.com/redis/go-redis/pull/4031)) by [@andystaples](https://github.com/andystaples). The relaxed-timeout metric is recorded for `SMIGRATING` ([#4390](https://github.com/redis/go-redis/pull/4390)) by [@ruziba3vich](https://github.com/ruziba3vich)

## ⚡ Performance

- **Short bulk strings in one write:** `Writer` writes the header of a bulk string with one call, and writes the value and CRLF together when the value is 32 bytes or less. Benchmarks of `WriteArgs` into a `bufio.Writer` showed 15% to 31% less time for each command on three CPUs, for example -24% to -26% for `GET key` and -27% to -31% for an 8-pair `MSET`. The bytes on the wire do not change ([#4281](https://github.com/redis/go-redis/pull/4281)) by [@g4lb](https://github.com/g4lb)
- **OpenTelemetry metrics:** see the highlight above ([#4269](https://github.com/redis/go-redis/pull/4269), [#4406](https://github.com/redis/go-redis/pull/4406), [#4407](https://github.com/redis/go-redis/pull/4407), [#4408](https://github.com/redis/go-redis/pull/4408))

## 🧪 Testing & Infrastructure

- **CI images:** the test matrix uses Redis 8.10.3 and 8.12.0 ([#4231](https://github.com/redis/go-redis/pull/4231), [#4241](https://github.com/redis/go-redis/pull/4241), [#4264](https://github.com/redis/go-redis/pull/4264), [#4288](https://github.com/redis/go-redis/pull/4288), [#4404](https://github.com/redis/go-redis/pull/4404)) by [@ndyakov](https://github.com/ndyakov)
- **End-to-end failure scenarios:** the e2e suites cover connection failures (dropped, refused, timed out) for regular, pub/sub, blocking and mixed workloads, `MultiDBClient` failover on member outage and on slow responses, and standalone topology changes. The fault-injector client gains `proxy_failure`, BDB-scoped `network_latency` and the `/topology-change-standalone` routes ([#4352](https://github.com/redis/go-redis/pull/4352))
- **32-bit build:** a `386` CI job runs `go vet` and the tests, and the root tests compile for 32-bit ([#4139](https://github.com/redis/go-redis/pull/4139)) by [@lazerg](https://github.com/lazerg)
- **Dependencies:** the OpenTelemetry SDK and `golang.org/x/net` are upgraded in the OTel adapters and examples, to fix `govulncheck` findings ([#4087](https://github.com/redis/go-redis/pull/4087)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **Documentation:** doc link definitions in the set and search command comments render as links on pkg.go.dev ([#4328](https://github.com/redis/go-redis/pull/4328)) by [@farisbasha](https://github.com/farisbasha), and the dial and retry timing is documented ([#4398](https://github.com/redis/go-redis/pull/4398)) by [@wozhenbang2004](https://github.com/wozhenbang2004)
- **Regression and flaky tests:** a cold-start full-duplex cluster test bounds the number of connections ([#4223](https://github.com/redis/go-redis/pull/4223)) by [@ndyakov](https://github.com/ndyakov), a test locks the `MSETEX` multi-shard split ([#4314](https://github.com/redis/go-redis/pull/4314)) by [@Zoomish](https://github.com/Zoomish), the autopipeline tests are consolidated from 28 files into 5 ([#4091](https://github.com/redis/go-redis/pull/4091)), and some tests move to the standard `testing` package ([#4085](https://github.com/redis/go-redis/pull/4085), [#4187](https://github.com/redis/go-redis/pull/4187)). Fixes for flaky or Redis Enterprise-incompatible specs: [#4172](https://github.com/redis/go-redis/pull/4172), [#4180](https://github.com/redis/go-redis/pull/4180), [#4210](https://github.com/redis/go-redis/pull/4210), [#4211](https://github.com/redis/go-redis/pull/4211), [#4215](https://github.com/redis/go-redis/pull/4215), [#4218](https://github.com/redis/go-redis/pull/4218), [#4255](https://github.com/redis/go-redis/pull/4255), [#4260](https://github.com/redis/go-redis/pull/4260), [#4270](https://github.com/redis/go-redis/pull/4270), [#4307](https://github.com/redis/go-redis/pull/4307)

## 👥 Contributors

We'd like to thank all the contributors who worked on this release!

[@AdamMagued](https://github.com/AdamMagued), [@aditya-rai-5](https://github.com/aditya-rai-5), [@andystaples](https://github.com/andystaples), [@aozmekik](https://github.com/aozmekik), [@Avv123](https://github.com/Avv123), [@b84330808](https://github.com/b84330808), [@CaliLuke](https://github.com/CaliLuke), [@cpoole](https://github.com/cpoole), [@dunglas](https://github.com/dunglas), [@Dv2Corn](https://github.com/Dv2Corn), [@farisbasha](https://github.com/farisbasha), [@g4lb](https://github.com/g4lb), [@Grivn](https://github.com/Grivn), [@harshitdeora](https://github.com/harshitdeora), [@hktitof](https://github.com/hktitof), [@jayhemnani9910](https://github.com/jayhemnani9910), [@jjj-n](https://github.com/jjj-n), [@kevin9327](https://github.com/kevin9327), [@khasanovbi](https://github.com/khasanovbi), [@lazerg](https://github.com/lazerg), [@LindseyZ1205](https://github.com/LindseyZ1205), [@meruiden](https://github.com/meruiden), [@MokiMeow](https://github.com/MokiMeow), [@mtnaskari](https://github.com/mtnaskari), [@Nacl-HCl](https://github.com/Nacl-HCl), [@ndyakov](https://github.com/ndyakov), [@ninadsutrave](https://github.com/ninadsutrave), [@nk-batman21](https://github.com/nk-batman21), [@ofekshenawa](https://github.com/ofekshenawa), [@othoathydrahost](https://github.com/othoathydrahost), [@pcbeingused333](https://github.com/pcbeingused333), [@PerryLink](https://github.com/PerryLink), [@PragalvaXFREZ](https://github.com/PragalvaXFREZ), [@Purv-Kabaria](https://github.com/Purv-Kabaria), [@RaphaelFakhri](https://github.com/RaphaelFakhri), [@rleungx](https://github.com/rleungx), [@roganartu](https://github.com/roganartu), [@ruziba3vich](https://github.com/ruziba3vich), [@saddamr3e](https://github.com/saddamr3e), [@Samuel-Sorial](https://github.com/Samuel-Sorial), [@SergeiSadov](https://github.com/SergeiSadov), [@soujasK](https://github.com/soujasK), [@spicysharkco](https://github.com/spicysharkco), [@SulimanAbdulrazzaq](https://github.com/SulimanAbdulrazzaq), [@TastyHeadphones](https://github.com/TastyHeadphones), [@Tidehyc](https://github.com/Tidehyc), [@ump45nose](https://github.com/ump45nose), [@VedantMadane](https://github.com/VedantMadane), [@vladvildanov](https://github.com/vladvildanov), [@vlady-kotsev](https://github.com/vlady-kotsev), [@waw666waw666](https://github.com/waw666waw666), [@wozhenbang2004](https://github.com/wozhenbang2004), [@xh123637](https://github.com/xh123637), [@xixi-box](https://github.com/xixi-box), [@yamaankhan20](https://github.com/yamaankhan20), [@yuefdev](https://github.com/yuefdev), [@zhechen-cs](https://github.com/zhechen-cs), [@Zoomish](https://github.com/Zoomish)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.23.0...v9.24.0

# 9.23.0 (2026-10-05)

This is a minor release. It contains everything from 9.23.0-beta.1, so these notes cover the full 9.22.0 → 9.23.0 upgrade. The release adds:

- An experimental **full-duplex mode** for the automatic pipeliner, now with `Pipeline()` on the full-duplex connection and several engines on one client.
- Refresh-on-invalidate and miss coalescing for client-side caching, and a faster cache read path.
- Better distribution of cluster reads under latency-based routing.
- `Client.EphemeralConn`, an opt-in limit on queued autopipeline commands, and many stability fixes.

⚠️ Changes to check when you upgrade from 9.22.0:

- **Go 1.26 is now the minimum Go version** ([#4060](https://github.com/redis/go-redis/pull/4060)). The `go` directive of the root module and of the submodules moved from `1.24` to `1.26`, as part of the dependency bumps for `govulncheck` findings. Projects that build with Go 1.24 or 1.25 must upgrade the toolchain.
- **Each `Client` has a dedicated pipeline connection pool** ([#4002](https://github.com/redis/go-redis/pull/4002)). This includes failover clients and cluster node clients. `Pipeline`, `TxPipeline`, and autopipeline operations use this pool, so they do not compete with regular commands for main-pool connections. The pool is for burst capacity only: it dials only when necessary, an unused pool holds zero connections, and if the pool is full an operation uses the main pool. Set `PipelinePoolSize: -1` to get the previous single-pool behavior.
- **`AutoPipelineOptions.MaxBatchBytes` has a default of 128 KiB** (before, there was no limit). The default prevents a write/reply deadlock; it is not a throughput control. The buffers of the pipeline pool also have a default of 128 KiB.

⚠️ Changes to experimental APIs since 9.23.0-beta.1 (no effect if you upgrade from 9.22.0):

- **`AutoPipelineOptions.FullDuplexFastSubmit` is removed** ([#4018](https://github.com/redis/go-redis/pull/4018)). The submit channel is replaced by a queue that takes a full wave of commands in one lock, so the separate fast path is not necessary. Remove the field from your options.
- **`Options.ClientSideCacheRefreshRecencyWindow` is removed** ([#4034](https://github.com/redis/go-redis/pull/4034)). Refresh-on-invalidate now reads again every cached entry of an invalidated key and does not look at how recently the entry was read. On a write-heavy keyspace this means refresh traffic across the full resident cache. Remove the field from your options.
- **`LocalCache.LRUClock()` is removed** ([#4034](https://github.com/redis/go-redis/pull/4034)). It returned the recency token that the removed recency window used, and has no replacement. Remove calls to it.
- **`ClientSideCacheInvalidationBatchWindow` served stale values in beta.1** ([#4033](https://github.com/redis/go-redis/pull/4033)). Under load the batcher applied only ~15% of invalidations, and the cache served invalidated values with a ~100% hit rate. 9.23.0 applies all of them. If you used this option on beta.1, upgrade.

## 🚀 Highlights

### Full-Duplex Auto-Pipelining (Experimental)

Set `AutoPipelineOptions.FullDuplex` to enable the full-duplex mode of the automatic pipeliner. The default mode sends one batch per round trip. In full-duplex mode the engine holds one pipeline-pool connection, and a writer goroutine and a reader goroutine move the ordered command stream in both directions at the same time. On a high-latency link each command completes in approximately one round-trip time (RTT) on a single connection. On a 50 ms WAN profile: ~389k ops/s at 52 ms p50, against ~207k ops/s at 116 ms on the half-duplex ordered path.

The mode works on both faces of a standalone `Client` (`AutoPipeline()` and `AsyncAutoPipeline()`), and natively on a `ClusterClient` when routing goes to masters only. On a cluster, one child engine per master sends each command to the node that owns its slot, and follows `MOVED` / `ASK` redirects and retryable replies (`LOADING`, `READONLY`, ...) through the usual cluster redirect path. With replica routing (`ReadOnly`, `RouteByLatency`, or `RouteRandomly`), the autopipeliner uses the half-duplex shard flushers, which obey the shard picker. `Config().FullDuplex` reports the mode in effect.

Options:

- `FullDuplexWindow` — the maximum number of commands in flight (backpressure).
- `FullDuplexIdleTimeout` and `FullDuplexMaxHold` — when the engine returns the held connection to the pool. The pool hooks then do re-authentication and maintenance-notification handoffs.
- `MaxFlushDelay` — now also used in full-duplex mode ([#4014](https://github.com/redis/go-redis/pull/4014)). The writer waits only when enough commands are in flight, so low-concurrency callers keep the 1×RTT behavior. With `MaxFlushDelay=250µs` at 1024 callers: +22% ops/s and −26% p99. The default is 0, so this is opt-in.
- `NumShards` — on a standalone `Client`, the number of full-duplex engines ([#4022](https://github.com/redis/go-redis/pull/4022)). See below.

**`Pipeline()` on the full-duplex connection** ([#4021](https://github.com/redis/go-redis/pull/4021)). Standalone `Client` only. Before, `ap.Pipeline()` used a pooled connection for each `Exec`. It now submits the batch to the engine as one contiguous run (`FDPipelined`), so replies come back in submit order, as on a dedicated connection. At 256–1024 callers with 10 commands per `Exec`: +49% to +52% throughput on one socket instead of ~150. Errors, retries, hooks, and metrics behave like an ordinary pipeline. A batch that cannot use the stream (a blocking command, a per-command read timeout, a diverted command, or a batch larger than the queue) falls back to the ordinary pipeline. `TxPipeline` stays on a pooled connection, because `MULTI`/`EXEC` needs connection affinity. On a `ClusterClient`, `ap.Pipeline()` is still the ordinary cluster pipeline. A batch that the queue cannot admit now reserves its slots and waits in a first-come line, so single commands cannot starve a long pipeline ([#4057](https://github.com/redis/go-redis/pull/4057), merged as part of [#4021](https://github.com/redis/go-redis/pull/4021)) by [@vlady-kotsev](https://github.com/vlady-kotsev).

**Several engines on one client** ([#4022](https://github.com/redis/go-redis/pull/4022)). With `FullDuplex`, `NumShards > 1` now means N engines on one client: one pool, one hook chain, N held connections. Commands route by a hash of their first key, so commands on the same key stay on one connection and keep their order without `Unordered`. Keyless commands are distributed round-robin. An `FDPipelined` batch stays on one engine. The pipeline pool must hold at least `NumShards` connections for each full-duplex autopipeliner. More engines help only under high concurrency: with 10-command pipelines, 8 engines were 30% slower than 1 at 8 callers, equal at about 128 callers, and 2.2× faster at 1024 callers.

The full-duplex path supports blocking commands, `Options.Limiter` (one admission per written batch), per-command and pipeline hooks, OTel metrics, retry budgets (a `NoRetry` command is never sent again after it is on the wire), and seamless maintenance handoffs. Limits of the stream model:

- **A hook can observe a command, but cannot stop it.** A `ProcessHook` that returns without calling `next` does not cancel the command on the full-duplex path; the command is already queued on the held connection. Run policy or kill-switch hooks on a plain client or on the half-duplex autopipeliner.
- **Diverted commands are not ordered with the stream.** The engine diverts blocking commands, connection-hostile commands, and managed `HIMPORT` commands. On the async face, wait for the result of a diverted command before you submit a command that depends on it.
- **Single commands on the full-duplex stream do not use the client-side cache.** If CSC is enabled, a cacheable command on the stream does not read or write the cache.
- **Duration metrics are recorded per reply.** A command that fails before it reaches the reader (lease failure, limiter denial, retry exhaustion, close) calls the error callback but records no operation-duration sample.

```go
rdb := redis.NewClient(&redis.Options{
	Addr: "localhost:6379",
	AutoPipelineOptions: &redis.AutoPipelineOptions{
		FullDuplex: true, // stream commands on one held connection, ~1 RTT each
	},
})
defer rdb.Close()

// Deferred face: calls return immediately; result accessors block until executed.
ap, err := rdb.AsyncAutoPipeline()
if err != nil {
	panic(err)
}
cmds := make([]*redis.StatusCmd, 0, 1000)
for i := 0; i < 1000; i++ {
	cmds = append(cmds, ap.Set(ctx, fmt.Sprintf("key:%d", i), i, 0))
}
for _, cmd := range cmds {
	if err := cmd.Err(); err != nil {
		// handle error
	}
}

// Or the blocking face — a drop-in Cmdable where each call blocks like a
// plain client while concurrent callers share the full-duplex pipe:
//	ap, err := rdb.AutoPipeline()
//	val, err := ap.Get(ctx, "key").Result()
```

Runnable examples: [`example/autopipeline`](example/autopipeline) (all faces), [`example/autopipeline-fullduplex`](example/autopipeline-fullduplex) (blocking face, `Pipeline()` and `FDPipelined`), and [`example/autopipeline-fullduplex-async`](example/autopipeline-fullduplex-async) (async face, `Submit`, `NumShards`) ([#4061](https://github.com/redis/go-redis/pull/4061)).

**Experimental:** the auto-pipelining APIs can change in a minor release.

([#4002](https://github.com/redis/go-redis/pull/4002), [#4014](https://github.com/redis/go-redis/pull/4014), [#4018](https://github.com/redis/go-redis/pull/4018), [#4021](https://github.com/redis/go-redis/pull/4021), [#4022](https://github.com/redis/go-redis/pull/4022)) by [@ndyakov](https://github.com/ndyakov)

### Client-Side Caching: Refresh-on-Invalidate, Miss Coalescing, Faster Reads

Two new functions for the experimental shared-tracking client-side cache. They have no effect unless CSC is enabled ([#3989](https://github.com/redis/go-redis/pull/3989)) by [@ndyakov](https://github.com/ndyakov):

- **Refresh-on-invalidate** (`Options.ClientSideCacheRefreshOnInvalidate`): when an invalidation push arrives, the client reads every cached entry of that key again in the background, so the next reader does not pay the miss. Each invalidation of a cached key costs one background read. `ClientSideCacheInvalidationBatchWindow` collects invalidation-driven deletes into background batches; without it, the connection reader applies each delete inline.
- **Miss coalescing** (`Options.ClientSideCacheCoalesceMisses`): concurrent cache misses are pipelined on one tracked connection. Each miss keeps its own per-key command (no rewrite to `MGET`), so it is safe on a cluster. Each reply is written to the cache with the connection's tracking generation, so the server can invalidate it.

Both options need the built-in cache (`ClientSideCacheConfig`, or `ClientSideCache` set to a `*LocalCache`). With a custom `Cache` implementation they are ignored.

The cache read path is also faster ([#4034](https://github.com/redis/go-redis/pull/4034), [#4035](https://github.com/redis/go-redis/pull/4035), [#4036](https://github.com/redis/go-redis/pull/4036), [#4037](https://github.com/redis/go-redis/pull/4037)) by [@ndyakov](https://github.com/ndyakov). A hit no longer writes a timestamp to the entry; a second-chance bit marks reads, and is set only under eviction pressure. The cache key is built in pooled scratch memory, the key list is built only on a miss, and nil/status/bulk-string hits are decoded without allocation. Against 9.23.0-beta.1 on a warm cache: +18% to +57% reads/s from 4 to 1024 callers, p50 from 2.6–5.4 µs to 1.0–1.5 µs, with identical hit rates and 100% of invalidations applied.

### Better Distribution for Latency-Based Cluster Read Routing

`RouteByLatency` selects the node with the strictly minimum latency. The measurement has noise (the mean of ten pings, refreshed at most every 10 s), so all clients can select the same node from a set of nodes with almost equal latency. In one production system, GET rates across five replicas in one availability zone had a 590× spread.

The new `ClusterOptions.RouteByLatencyTolerance` widens the selection to every node within the tolerance of the fastest node, and distributes reads across them round-robin with the `ShardPicker`. A node in a different availability zone stays outside a sensible tolerance, so zone locality is kept. The default is zero (strict minimum). The option is also on `FailoverOptions`, for clients from `NewFailoverClusterClient`; the plain `NewFailoverClient` does not support latency routing. ([#3973](https://github.com/redis/go-redis/pull/3973)) by [@jozenstar](https://github.com/jozenstar)

The same work fixed a routing defect: the client recorded the nearest healthy node only when that node was also the fastest overall. A node that fails fast (for example, a refused connection) hid every healthy node, and reads went to the failing node. The client now records the healthy minimum separately. ([#3994](https://github.com/redis/go-redis/pull/3994)) by [@jozenstar](https://github.com/jozenstar)

### `EphemeralConn` for Session-Scoped Work

`Client.Conn()` returns its connection to the parent pool on `Close`. If you ran `AUTH` or `SELECT` on it, later commands on the pooled client run as that ACL user and in that DB. The new `Client.EphemeralConn()` returns the same sticky connection, but `Close` removes it from the pool instead. The cost is one dial per `EphemeralConn`. `Client.Conn()` is unchanged. ([#4039](https://github.com/redis/go-redis/pull/4039)) by [@saddamr3e](https://github.com/saddamr3e)

## ✨ New Features

- **Full-duplex auto-pipelining**: `AutoPipelineOptions.FullDuplex`, with `FullDuplexWindow` / `FullDuplexIdleTimeout` / `FullDuplexMaxHold`, on standalone and cluster clients ([#4002](https://github.com/redis/go-redis/pull/4002)) by [@ndyakov](https://github.com/ndyakov)
- **`Pipeline()` on the full-duplex connection**: `ap.Pipeline()` and `FDPipelined` submit a batch to the engine as one contiguous run, on a standalone `Client` ([#4021](https://github.com/redis/go-redis/pull/4021)) by [@ndyakov](https://github.com/ndyakov)
- **Several full-duplex engines on one client**: `NumShards > 1` with `FullDuplex` on a standalone `Client`, with key-hash routing ([#4022](https://github.com/redis/go-redis/pull/4022)) by [@ndyakov](https://github.com/ndyakov)
- **`AutoPipelineOptions.MaxQueuedCommands`**: an opt-in hard limit on accepted, not-yet-completed autopipeline commands. At the limit a command fails immediately with `ErrAutoPipelineQueueFull`, so memory does not grow without limit when the server is slow. With `FullDuplex`, the ordered stream is bounded by `FullDuplexWindow` instead (a full window blocks the submitter, it does not reject), and `MaxQueuedCommands` limits only the commands that run outside the stream (blocking, connection-hostile, `Do`). The default `0` means no limit ([#4070](https://github.com/redis/go-redis/pull/4070)) by [@ndyakov](https://github.com/ndyakov)
- **Dedicated pipeline pool by default**: `PipelinePoolSize` has a default of `DefaultPipelinePoolSize` (10) on each client. If the pool is full, an operation uses the main pool. Set `-1` to disable the pool ([#4002](https://github.com/redis/go-redis/pull/4002), [#3959](https://github.com/redis/go-redis/pull/3959)) by [@ndyakov](https://github.com/ndyakov)
- **CSC refresh-on-invalidate and miss coalescing**: `Options.ClientSideCacheRefreshOnInvalidate` (with `ClientSideCacheInvalidationBatchWindow`) and `Options.ClientSideCacheCoalesceMisses` ([#3989](https://github.com/redis/go-redis/pull/3989)) by [@ndyakov](https://github.com/ndyakov)
- **`Client.EphemeralConn`**: a sticky connection that is removed from the pool on `Close`, for session-scoped `AUTH` / `SELECT` work ([#4039](https://github.com/redis/go-redis/pull/4039)) by [@saddamr3e](https://github.com/saddamr3e)
- **`RouteByLatencyTolerance`**: distributes reads across nodes with almost equal latency, on cluster clients and on `NewFailoverClusterClient` ([#3973](https://github.com/redis/go-redis/pull/3973)) by [@jozenstar](https://github.com/jozenstar)
- **`AutoPipeliner.WaitClosed`**: blocks until the drain of accepted commands completes, and returns the drain result. Use it in a wrapper that must not close shared pools during a flush ([#3998](https://github.com/redis/go-redis/pull/3998)) by [@ndyakov](https://github.com/ndyakov)
- **`redisotel-native` `WithRecordNilErrors`**: opt in to count `redis.Nil` replies as client errors (see the fix below) ([#4025](https://github.com/redis/go-redis/pull/4025)) by [@lazerg](https://github.com/lazerg)
- **`CMSInfo.CellSize`**: the cell-size field of `CMS.INFO` in Redis 8.12 ([#4010](https://github.com/redis/go-redis/pull/4010)) by [@elena-kolevska](https://github.com/elena-kolevska)

## 🐛 Bug Fixes

- **CSC invalidation batching**: `ClientSideCacheInvalidationBatchWindow` applied only ~15% of invalidations under load, so the cache served stale values with a ~100% hit rate. Each batch now takes each shard lock once, and 100% of invalidations are applied ([#4033](https://github.com/redis/go-redis/pull/4033)) by [@ndyakov](https://github.com/ndyakov)
- **Pool waiter wake-ups**: `Put` did not wake a worker that waited for a connection to become `IDLE`, so a streaming-credentials re-auth could stall until `PoolTimeout` and then close the connection ([#4028](https://github.com/redis/go-redis/pull/4028)) by [@avitenzer](https://github.com/avitenzer). Two related waiter-queue races are fixed: a failed transition left a dead waiter queued, which later moved a connection to `UNUSABLE` for nobody ([#4072](https://github.com/redis/go-redis/pull/4072)) by [@ndyakov](https://github.com/ndyakov)
- **Command key positions**: 16 typed commands (`BitOp*`, `MIGRATE`, `OBJECT ENCODING/FREQ/IDLETIME/REFCOUNT`, `XINFO STREAM/GROUPS/CONSUMERS`, `LMPOP`, `BLMPOP`, `SINTERCARD`, `ZINTERCARD`, `ZMPOP`, `BZMPOP`) routed by the wrong argument. On a `Ring` they went to the wrong shard; on a cluster they cost an extra `MOVED` round trip. A raw `FCALL` / `FCALL_RO` hashed the function name. All now route by their key ([#4048](https://github.com/redis/go-redis/pull/4048)) by [@ndyakov](https://github.com/ndyakov). Raw `XGROUP`, `ZUNION` / `ZINTER` / `ZDIFF`, `SDIFFCARD` / `SUNIONCARD`, `TS.NRANGE` / `TS.NREVRANGE`, and `HIMPORT SET` forms are also fixed ([#4022](https://github.com/redis/go-redis/pull/4022))
- **Millisecond expirations**: `PExpireAt`, `HPExpireAt`, and `HPExpireAtWithArgs` overflowed for dates outside the nanosecond range (for example, year 2270), so Redis deleted the key or rejected the command ([#4026](https://github.com/redis/go-redis/pull/4026)) by [@jakezwang](https://github.com/jakezwang). `SetArgs.ExpireAt` now sends `PXAT` when the deadline has millisecond precision; before, `EXAT` dropped the milliseconds ([#4053](https://github.com/redis/go-redis/pull/4053)) by [@LindseyZ1205](https://github.com/LindseyZ1205)
- **Cluster `-1` timeouts**: a `-1` read/write timeout on `ClusterOptions` gave node clients the 5 s default instead of no timeout (fixes [#4049](https://github.com/redis/go-redis/issues/4049)) ([#4050](https://github.com/redis/go-redis/pull/4050)) by [@lazerg](https://github.com/lazerg)
- **OTel `redis.Nil` as error**: a cache miss (`redis.Nil`) incremented `redis.client.errors` and was tagged as a server error on `db.client.operation.duration`. Nil replies are now classified `NIL` and not counted, unless `WithRecordNilErrors(true)` is set (fixes [#4024](https://github.com/redis/go-redis/issues/4024)) ([#4025](https://github.com/redis/go-redis/pull/4025)) by [@lazerg](https://github.com/lazerg)
- **Credential redaction**: `SENTINEL SET ... auth-pass` is now redacted in command rendering (used by `redisotel` / `rediscensus` span attributes), and a `MIGRATE` password with the literal value `auth` / `auth2` is redacted at the correct position ([#4011](https://github.com/redis/go-redis/pull/4011)) by [@saddamr3e](https://github.com/saddamr3e)
- **Min/max aggregator panic**: a non-numeric shard reply made `AggMinAggregator` / `AggMaxAggregator` panic instead of returning an error from `Result()` ([#4056](https://github.com/redis/go-redis/pull/4056)) by [@Suselz](https://github.com/Suselz)
- **`ParseURL` `skip_verify`**: an invalid value (for example, `skip_verify=yes`) was silently treated as `false`. `ParseURL` and `ParseFailoverURL` now return an error, like for the other boolean options ([#4064](https://github.com/redis/go-redis/pull/4064)) by [@orinnz](https://github.com/orinnz)
- **`uintptr` arguments**: `uintptr` and `*uintptr` are now encoded like the other unsigned integers; a nil `*uintptr` is encoded as `0` (fixes [#3100](https://github.com/redis/go-redis/issues/3100)) ([#4054](https://github.com/redis/go-redis/pull/4054)) by [@yamaankhan20](https://github.com/yamaankhan20)
- **Cluster read routing**: the client records the nearest healthy node separately from the overall minimum, so a node that fails fast does not hide the healthy nodes ([#3994](https://github.com/redis/go-redis/pull/3994)) by [@jozenstar](https://github.com/jozenstar)
- **Probabilistic `*.INFO` forward compatibility**: the `BF.INFO` / `CF.INFO` / `CMS.INFO` / `TOPK.INFO` / `TDIGEST.INFO` parsers skip unknown fields instead of returning an error. Redis 8.12 adds `cell size` to `CMS.INFO` ([#4010](https://github.com/redis/go-redis/pull/4010)) by [@elena-kolevska](https://github.com/elena-kolevska)
- **`NewClient` panic leak**: a panic during construction (for example, a maintnotifications failure in `ModeEnabled`) does not leak the connection pools that already exist, and a typed-nil pool cannot hide the initial panic ([#4003](https://github.com/redis/go-redis/pull/4003)) by [@ndyakov](https://github.com/ndyakov). The same guards apply to `NewFailoverClient` ([#4002](https://github.com/redis/go-redis/pull/4002))
- **File-descriptor leak on rejected connections**: `Conn.Close` does the socket teardown and the unsubscribe/CSC callbacks when the connection is already `CLOSED`. Before, init and auth failures leaked open descriptors (fixes [#3982](https://github.com/redis/go-redis/issues/3982)) ([#3985](https://github.com/redis/go-redis/pull/3985)) by [@ndyakov](https://github.com/ndyakov)
- **Global logger races**: atomics protect the global `Logger` and `LogLevel`, and call-site attribution is correct again ([#3988](https://github.com/redis/go-redis/pull/3988)) by [@saddamr3e](https://github.com/saddamr3e)
- **`Conn.onClose` data race**: the close hooks that init installs (`onClose` and `onCscClose`) are atomic against a concurrent `Close` ([#3966](https://github.com/redis/go-redis/pull/3966)) by [@saddamr3e](https://github.com/saddamr3e)
- **Reply-parser hardening**: the reply parsers accept zero-length entry arrays ([#3995](https://github.com/redis/go-redis/pull/3995)), and `FTHybridCmd` reads the full RESP3 map reply without desynchronizing the connection ([#3956](https://github.com/redis/go-redis/pull/3956)) by [@saddamr3e](https://github.com/saddamr3e)
- **`CLIENT INFO` forward compatibility**: the parser skips unknown client-flag characters instead of failing the reply ([#3977](https://github.com/redis/go-redis/pull/3977)) by [@ndyakov](https://github.com/ndyakov)
- **`GEOSEARCH` duplicate args**: the command no longer sends duplicate arguments ([#3955](https://github.com/redis/go-redis/pull/3955)) by [@mehmettokgoz](https://github.com/mehmettokgoz)
- **`MSetEX` cluster routing**: the constructor sets the first-key position, so typed calls go to the correct slot ([#3984](https://github.com/redis/go-redis/pull/3984)) by [@shivamrustagi](https://github.com/shivamrustagi)
- **Maintenance notifications**: no endpoint DNS detection when the mode is disabled ([#3969](https://github.com/redis/go-redis/pull/3969)) by [@Phalanyx](https://github.com/Phalanyx)
- **Autopipeliner `Close`**: a concurrent `Close` does not block (no re-entrant deadlock), and `WaitClosed` returns the drain result ([#3998](https://github.com/redis/go-redis/pull/3998)) by [@ndyakov](https://github.com/ndyakov)
- **Buffered-push log noise**: the buffered-push-data notice in `isHealthyConn` is logged only at debug level, so CSC invalidations do not fill the log ([#3948](https://github.com/redis/go-redis/pull/3948)) by [@ndyakov](https://github.com/ndyakov)
- **Sentinel teardown order**: close hooks run in LIFO order, so an autopipeliner drain completes before Sentinel discovery stops, and a closed failover client cannot recreate its Sentinel resources from a late dial ([#4002](https://github.com/redis/go-redis/pull/4002)) by [@ndyakov](https://github.com/ndyakov)
- **Pipeline desync containment**: if a pre-write push-notification drain fails or a command encoder panics, the `Pipeline` / `TxPipeline` path removes the connection instead of returning a desynchronized connection to the pool ([#4002](https://github.com/redis/go-redis/pull/4002)) by [@ndyakov](https://github.com/ndyakov)

## ⚡ Performance

- **CSC read path**: second-chance read bit, pooled cache-key scratch, key list built only on a miss, allocation-free decode of simple hits. +18% to +57% reads/s against 9.23.0-beta.1 on a warm cache ([#4034](https://github.com/redis/go-redis/pull/4034), [#4035](https://github.com/redis/go-redis/pull/4035), [#4036](https://github.com/redis/go-redis/pull/4036), [#4037](https://github.com/redis/go-redis/pull/4037)) by [@ndyakov](https://github.com/ndyakov)
- **Full-duplex reads and submit queue**: replies already in the buffer are read as one group (one read deadline per group, not per reply), and a mutex+slice queue replaces the submit channel, so the writer takes a whole wave in one lock ([#4018](https://github.com/redis/go-redis/pull/4018)) by [@ndyakov](https://github.com/ndyakov)
- **Full-duplex `MaxFlushDelay`**: opt-in write coalescing under load. With 250 µs at 1024–2048 callers: +22% to +25% ops/s and −26% to −29% p99; 30% less CPU at 256 callers ([#4014](https://github.com/redis/go-redis/pull/4014)) by [@ndyakov](https://github.com/ndyakov)
- **Full-duplex allocations**: a ring buffer holds the in-flight queue, and the blocking face uses a pool of batches. 770 B/op → 353 B/op at 2048 concurrent callers ([#3970](https://github.com/redis/go-redis/pull/3970), part of [#4002](https://github.com/redis/go-redis/pull/4002)) by [@ndyakov](https://github.com/ndyakov)
- **Zero-copy scan**: `Scan` gets zero-copy semantics, and the RESP reader skips unnecessary data conversions ([#3972](https://github.com/redis/go-redis/pull/3972)) by [@vlady-kotsev](https://github.com/vlady-kotsev)
- **Autopipeline straggler hold**: the engine limits the hold on queued commands when the pipeline pool has a free connection. Uncached p95 on a 50 ms link: 111 ms → 65 ms; real-WAN uncached p99: 314 ms → 177 ms ([#3962](https://github.com/redis/go-redis/pull/3962)) by [@ndyakov](https://github.com/ndyakov)

## 🧪 Testing & Infrastructure

- **Go 1.26 and `govulncheck` dependency bumps**: the minimum Go version is 1.26, CI defaults to 1.26, and a new `go-versions` job builds the test matrix from `go.mod` plus `oldstable` / `stable` ([#4060](https://github.com/redis/go-redis/pull/4060)) by [@vlady-kotsev](https://github.com/vlady-kotsev)
- **Fast skip gates**: tests do a TCP probe of each address before the `Ping` gate, which removes ~1.6 min of dial-retry waits when the full stack is not running ([#4001](https://github.com/redis/go-redis/pull/4001)) by [@ndyakov](https://github.com/ndyakov)
- **Redis Enterprise coverage**: the autopipeline suites connect to the RE database and use the suite DB ([#3976](https://github.com/redis/go-redis/pull/3976), [#3975](https://github.com/redis/go-redis/pull/3975)), timing assertions scale to the measured RTT ([#3978](https://github.com/redis/go-redis/pull/3978)), and the `CLIENT INFO` tracking-flag assertion is skipped behind the RE proxy ([#3981](https://github.com/redis/go-redis/pull/3981)) by [@ndyakov](https://github.com/ndyakov)
- **Docs**: `UniversalOptions` documents `ReadTimeout` / `WriteTimeout` / `ContextTimeoutEnabled` ([#4047](https://github.com/redis/go-redis/pull/4047)) by [@lazerg](https://github.com/lazerg); the README describes `DisableIdentity` as client identification (`CLIENT SETINFO`), not identity verification ([#4059](https://github.com/redis/go-redis/pull/4059)) by [@FanWu-ai](https://github.com/FanWu-ai)
- **Security policy**: send vulnerability reports to the Redis VDP ([#3949](https://github.com/redis/go-redis/pull/3949)) by [@ndyakov](https://github.com/ndyakov)

## 👥 Contributors

We thank all the contributors who worked on this release!

[@avitenzer](https://github.com/avitenzer), [@elena-kolevska](https://github.com/elena-kolevska), [@FanWu-ai](https://github.com/FanWu-ai), [@jakezwang](https://github.com/jakezwang), [@jozenstar](https://github.com/jozenstar), [@lazerg](https://github.com/lazerg), [@LindseyZ1205](https://github.com/LindseyZ1205), [@mehmettokgoz](https://github.com/mehmettokgoz), [@ndyakov](https://github.com/ndyakov), [@orinnz](https://github.com/orinnz), [@Phalanyx](https://github.com/Phalanyx), [@saddamr3e](https://github.com/saddamr3e), [@shivamrustagi](https://github.com/shivamrustagi), [@Suselz](https://github.com/Suselz), [@vlady-kotsev](https://github.com/vlady-kotsev), [@yamaankhan20](https://github.com/yamaankhan20)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.22.0...v9.23.0

# 9.23.0-beta.1 (2026-09-11)

This is a **beta** release. You can upgrade without changes to your code. The release adds three primary features:

- An experimental **full-duplex mode** for the automatic pipeliner.
- Refresh and miss-coalescing functions for client-side caching.
- Better distribution of cluster reads under latency-based routing.

The release also contains many stability fixes and robustness fixes.

⚠️ This release changes one default behavior. Each `Client` (this includes failover clients and cluster node clients) now creates a small, dedicated **pipeline connection pool** ([#4002](https://github.com/redis/go-redis/pull/4002)). `Pipeline`, `TxPipeline`, and autopipeline operations use this pool. These operations do not compete with regular commands for main-pool connections. The pool supplies burst capacity only:

- The pool does not dial a connection before the connection is necessary.
- An unused pool holds zero connections. The idle cost is zero.
- If the pool is full, an operation immediately uses the main pool.

Set `PipelinePoolSize: -1` to get the previous single-pool behavior.

## 🚀 Highlights

### Full-Duplex Auto-Pipelining (Experimental)

Set `AutoPipelineOptions.FullDuplex` to enable the full-duplex mode of the automatic pipeliner. The default mode sends one batch for each round trip. The full-duplex mode is different: the engine holds one pipeline-pool connection, and a writer goroutine and a reader goroutine move the ordered command stream in the two directions at the same time. On a link with high latency, each command completes in approximately one round-trip time (RTT) on a single connection. A test on a 50 ms WAN profile measured ~389k operations/s at 52 ms p50. The half-duplex ordered path measured ~207k operations/s at 116 ms.

The mode operates on the two faces of a standalone `Client`: `AutoPipeline()` and `AsyncAutoPipeline()`. The mode also operates natively on a `ClusterClient` if the routing goes to masters only. A child engine for each master sends each command to the node that owns the command's slot. The engine obeys `MOVED` and `ASK` redirects and retryable replies (`LOADING`, `READONLY`, ...) through the usual cluster redirect procedure. If replica routing is set (`ReadOnly`, `RouteByLatency`, or `RouteRandomly`), the autopipeliner uses the half-duplex shard flushers, which obey the shard picker. `Config().FullDuplex` reports the mode that is in effect.

These options tune the mode:

- `FullDuplexWindow` — the maximum number of commands in flight (backpressure).
- `FullDuplexIdleTimeout` and `FullDuplexMaxHold` — control when the engine returns the held connection to the pool. The pool hooks then do the re-authentication and the maintenance-notification handoffs.
- `FullDuplexFastSubmit` — an optional fast submit path for many producers on low-RTT links.

The full-duplex path supports blocking commands, `Options.Limiter` (one admission for each written batch), per-command hooks, OTel metrics, retry budgets (the client never sends a `NoRetry` command again after the command is on the wire), and seamless maintenance handoffs. The stream model causes these limits:

- **A hook can monitor a command. A hook cannot stop a command.** A `ProcessHook` that returns without a call to `next` does not cancel the command on the full-duplex path. The command is already in the queue of the held connection, and the client sends it. Run policy hooks and kill-switch hooks on a plain client or on the half-duplex autopipeliner.
- **The order guarantee does not include diverted commands.** The commands of one caller keep their order on the shared stream. The engine diverts some commands from the stream: blocking commands, connection-hostile commands, and managed `HIMPORT` commands. A diverted command has no order relation to the stream. On the async face, wait for the result of a diverted command before you submit a command that depends on it.
- **The fast path does not use the client-side cache.** If CSC is enabled, a cacheable command on the full-duplex pipe does not read the cache and does not write to the cache. A subsequent change will correct this.
- **The engine records a duration metric for each reply.** Some commands fail before they get to the reader: a lease failure, a limiter denial, retry exhaustion, or a close. These commands cause the error callback, but they do not cause an operation-duration sample.

```go
rdb := redis.NewClient(&redis.Options{
	Addr: "localhost:6379",
	AutoPipelineOptions: &redis.AutoPipelineOptions{
		FullDuplex: true, // stream commands on one held connection, ~1 RTT each
	},
})
defer rdb.Close()

// Deferred face: calls return immediately; result accessors block until executed.
ap, err := rdb.AsyncAutoPipeline()
if err != nil {
	panic(err)
}
cmds := make([]*redis.StatusCmd, 0, 1000)
for i := 0; i < 1000; i++ {
	cmds = append(cmds, ap.Set(ctx, fmt.Sprintf("key:%d", i), i, 0))
}
for _, cmd := range cmds {
	if err := cmd.Err(); err != nil {
		// handle error
	}
}

// Or the blocking face — a drop-in Cmdable where each call blocks like a
// plain client while concurrent callers share the full-duplex pipe:
//	ap, err := rdb.AutoPipeline()
//	val, err := ap.Get(ctx, "key").Result()
```

The [`example/autopipeline`](example/autopipeline) directory contains a runnable tour of all the autopipeliner faces.

Two related defaults changed. `AutoPipelineOptions.MaxBatchBytes` now has a default of 128 KiB (before, the size had no limit). This default prevents a write/reply deadlock. It is not a throughput control. The buffers of the pipeline pool also have a default of 128 KiB.

**Experimental:** the auto-pipelining APIs can change in a minor release.

([#4002](https://github.com/redis/go-redis/pull/4002)) by [@ndyakov](https://github.com/ndyakov)

### Client-Side Caching: Refresh-on-Invalidate and Miss Coalescing

The release adds two functions to the experimental shared-tracking client-side cache. The functions have no effect unless CSC is enabled ([#3989](https://github.com/redis/go-redis/pull/3989)) by [@ndyakov](https://github.com/ndyakov):

- **Refresh-on-invalidate** (`Options.ClientSideCacheRefreshOnInvalidate`): the client reads recently-read keys again in the background immediately after their invalidation push arrives. The next reader does not pay the cache miss. The client collects invalidated hot keys in a short window and reads them again on a pipelined connection. `ClientSideCacheRefreshRecencyWindow` sets which entries count as recently read. `ClientSideCacheInvalidationBatchWindow` collects invalidation-driven cache deletes into background batches; without it, the connection reader applies each delete inline.
- **Miss coalescing** (`Options.ClientSideCacheCoalesceMisses`): the client pipelines concurrent cache-miss reads onto one tracked connection. Each miss keeps the caller's own per-key command. The client does not rewrite the commands to `MGET`, so the function is safe on a cluster. The client writes each reply to the cache with the tracking generation of the connection. The server can thus invalidate each entry.

These options have the same requirement as the other CSC options: the built-in cache (`ClientSideCacheConfig`, or `ClientSideCache` set to a `*LocalCache`). The client ignores the options if a custom `Cache` implementation is set.

### Better Distribution for Latency-Based Cluster Read Routing

`RouteByLatency` selects the node with the strictly minimum latency. The latency measurement has noise: the value is the mean of ten pings, and the client refreshes it at most each 10 s. Thus all clients can select the same node from a set of nodes that have almost equal latency. A production system showed this problem: the GET rates across a five-replica set in one availability zone had a 590x spread.

The new `ClusterOptions.RouteByLatencyTolerance` widens the selection to each node with a latency in the tolerance above the fastest node. The client distributes reads across these nodes with the round-robin procedure of the `ShardPicker`. A node in a different availability zone stays outside a sensible tolerance, so zone locality is kept. The default is zero, which keeps the strict-minimum behavior. The option is also on `FailoverOptions`, where it applies to clients from `NewFailoverClusterClient`. The plain `NewFailoverClient` does not support latency routing. ([#3973](https://github.com/redis/go-redis/pull/3973)) by [@jozenstar](https://github.com/jozenstar)

The same work corrected a routing defect. The client recorded the nearest healthy node only when that node was also the fastest node overall. A node that fails fast (a refused connection fails fast, so this is frequent) thus hid each healthy node, and the client sent reads to the node that failed. The client now records the healthy minimum separately. ([#3994](https://github.com/redis/go-redis/pull/3994)) by [@jozenstar](https://github.com/jozenstar)

## ✨ New Features

- **Full-duplex auto-pipelining**: `AutoPipelineOptions.FullDuplex`, with `FullDuplexWindow` / `FullDuplexIdleTimeout` / `FullDuplexMaxHold` / `FullDuplexFastSubmit`. Available on standalone clients and cluster clients ([#4002](https://github.com/redis/go-redis/pull/4002)) by [@ndyakov](https://github.com/ndyakov)
- **Dedicated pipeline pool by default**: `PipelinePoolSize` has a default of `DefaultPipelinePoolSize` (10) on each client. If the pool is full, an operation immediately uses the main pool. Set `-1` to disable the pool ([#4002](https://github.com/redis/go-redis/pull/4002), [#3959](https://github.com/redis/go-redis/pull/3959)) by [@ndyakov](https://github.com/ndyakov)
- **CSC refresh-on-invalidate and miss coalescing**: `Options.ClientSideCacheRefreshOnInvalidate` (with `ClientSideCacheRefreshRecencyWindow` / `ClientSideCacheInvalidationBatchWindow`) and `Options.ClientSideCacheCoalesceMisses` ([#3989](https://github.com/redis/go-redis/pull/3989)) by [@ndyakov](https://github.com/ndyakov)
- **`RouteByLatencyTolerance`**: distributes reads across nodes that have almost equal latency, on cluster clients and on `NewFailoverClusterClient` ([#3973](https://github.com/redis/go-redis/pull/3973)) by [@jozenstar](https://github.com/jozenstar)
- **`AutoPipeliner.WaitClosed`**: blocks until the drain of the accepted commands completes, and returns the drain result. Use it in a wrapper that must not close shared pools while a flush is in progress ([#3998](https://github.com/redis/go-redis/pull/3998)) by [@ndyakov](https://github.com/ndyakov)
- **`CMSInfo.CellSize`**: contains the cell-size field of `CMS.INFO` in Redis 8.12 ([#4010](https://github.com/redis/go-redis/pull/4010)) by [@elena-kolevska](https://github.com/elena-kolevska)

## 🐛 Bug Fixes

- **Cluster read routing**: the client records the nearest healthy node separately from the overall minimum. A node that fails fast does not hide the healthy nodes ([#3994](https://github.com/redis/go-redis/pull/3994)) by [@jozenstar](https://github.com/jozenstar)
- **Probabilistic `*.INFO` forward compatibility**: the parsers for `BF.INFO` / `CF.INFO` / `CMS.INFO` / `TOPK.INFO` / `TDIGEST.INFO` skip unknown fields and do not return an error. Redis 8.12 adds `cell size` to `CMS.INFO` ([#4010](https://github.com/redis/go-redis/pull/4010)) by [@elena-kolevska](https://github.com/elena-kolevska)
- **`NewClient` panic leak**: a panic during construction (for example, a maintnotifications failure in `ModeEnabled`) does not leak the connection pools that already exist. A typed-nil pool cannot hide the initial panic ([#4003](https://github.com/redis/go-redis/pull/4003)) by [@ndyakov](https://github.com/ndyakov). The same guards are applied to `NewFailoverClient` ([#4002](https://github.com/redis/go-redis/pull/4002))
- **File-descriptor leak on rejected connections**: `Conn.Close` does the socket teardown and the unsubscribe/CSC callbacks when the connection is already `CLOSED`. Before, init and auth failures collected open descriptors. The transport now closes exactly one time for each socket generation (fixes [#3982](https://github.com/redis/go-redis/issues/3982)) ([#3985](https://github.com/redis/go-redis/pull/3985)) by [@ndyakov](https://github.com/ndyakov)
- **Global logger races**: atomics protect the global `Logger` and `LogLevel`. The call-site attribution is correct again ([#3988](https://github.com/redis/go-redis/pull/3988)) by [@saddamr3e](https://github.com/saddamr3e)
- **`Conn.onClose` data race**: the close hooks that init installs (`onClose` and `onCscClose`) are now atomic against a concurrent `Close` ([#3966](https://github.com/redis/go-redis/pull/3966)) by [@saddamr3e](https://github.com/saddamr3e)
- **Reply-parser hardening**: the reply parsers accept zero-length entry arrays ([#3995](https://github.com/redis/go-redis/pull/3995)) by [@saddamr3e](https://github.com/saddamr3e). `FTHybridCmd` reads the full RESP3 map reply and does not desynchronize the connection ([#3956](https://github.com/redis/go-redis/pull/3956)) by [@saddamr3e](https://github.com/saddamr3e)
- **`CLIENT INFO` forward compatibility**: the parser skips unknown client-flag characters. The full reply does not fail ([#3977](https://github.com/redis/go-redis/pull/3977)) by [@ndyakov](https://github.com/ndyakov)
- **`GEOSEARCH` duplicate args**: the command does not send duplicate arguments ([#3955](https://github.com/redis/go-redis/pull/3955)) by [@mehmettokgoz](https://github.com/mehmettokgoz)
- **`MSetEX` cluster routing**: the constructor sets the first-key position. Typed calls thus go to the correct slot ([#3984](https://github.com/redis/go-redis/pull/3984)) by [@shivamrustagi](https://github.com/shivamrustagi)
- **Maintenance notifications**: the client does not do the endpoint DNS detection when the mode is disabled ([#3969](https://github.com/redis/go-redis/pull/3969)) by [@Phalanyx](https://github.com/Phalanyx)
- **Autopipeliner `Close`**: a concurrent `Close` does not block (no re-entrant deadlock). `WaitClosed` supplies the drain result ([#3998](https://github.com/redis/go-redis/pull/3998)) by [@ndyakov](https://github.com/ndyakov)
- **Buffered-push log noise**: the buffered-push-data notice in `isHealthyConn` shows only at the debug level. CSC invalidations do not fill the log ([#3948](https://github.com/redis/go-redis/pull/3948)) by [@ndyakov](https://github.com/ndyakov)
- **Sentinel teardown order**: close hooks run in LIFO order. An autopipeliner drain thus completes before the Sentinel discovery stops. A closed failover client cannot create its Sentinel resources again from a late dial ([#4002](https://github.com/redis/go-redis/pull/4002)) by [@ndyakov](https://github.com/ndyakov)
- **Pipeline desync containment**: if a pre-write push-notification drain fails, or if a command encoder panics, the client removes the connection. The client does not return a desynchronized connection to the pool. This applies to the shared `Pipeline`/`TxPipeline` path ([#4002](https://github.com/redis/go-redis/pull/4002)) by [@ndyakov](https://github.com/ndyakov)

## ⚡ Performance

- **Zero-copy scan**: `Scan` gets zero-copy semantics, and the RESP reader does not do unnecessary data conversions ([#3972](https://github.com/redis/go-redis/pull/3972)) by [@vlady-kotsev](https://github.com/vlady-kotsev)
- **Autopipeline straggler hold**: the engine limits the hold on queued commands when the pipeline pool has a free connection. Uncached p95 decreased from 111 ms to 65 ms on a 50 ms link. Real-WAN uncached p99 decreased from 314 ms to 177 ms ([#3962](https://github.com/redis/go-redis/pull/3962)) by [@ndyakov](https://github.com/ndyakov)
- **Full-duplex allocations**: a ring buffer holds the in-flight queue, and the blocking face uses a pool of batches. Allocations decreased from 770 B/op to 353 B/op at 2048 concurrent callers ([#3970](https://github.com/redis/go-redis/pull/3970), part of [#4002](https://github.com/redis/go-redis/pull/4002)) by [@ndyakov](https://github.com/ndyakov)

## 🧪 Testing & Infrastructure

- **Fast skip gates**: the tests do a TCP probe of each address before the `Ping` gate. This removes ~1.6 min of dial-retry waits in environments without the full stack ([#4001](https://github.com/redis/go-redis/pull/4001)) by [@ndyakov](https://github.com/ndyakov)
- **Redis Enterprise coverage**: the autopipeline suites connect to the RE database and use the suite DB ([#3976](https://github.com/redis/go-redis/pull/3976), [#3975](https://github.com/redis/go-redis/pull/3975)). The timing assertions scale to the measured RTT ([#3978](https://github.com/redis/go-redis/pull/3978)). The `CLIENT INFO` tracking-flag assertion does not run behind the RE proxy ([#3981](https://github.com/redis/go-redis/pull/3981)) by [@ndyakov](https://github.com/ndyakov)
- **Security policy**: send vulnerability reports to the Redis VDP ([#3949](https://github.com/redis/go-redis/pull/3949)) by [@ndyakov](https://github.com/ndyakov)

## 👥 Contributors

We thank all the contributors who worked on this release!

[@elena-kolevska](https://github.com/elena-kolevska), [@jozenstar](https://github.com/jozenstar), [@mehmettokgoz](https://github.com/mehmettokgoz), [@ndyakov](https://github.com/ndyakov), [@Phalanyx](https://github.com/Phalanyx), [@saddamr3e](https://github.com/saddamr3e), [@shivamrustagi](https://github.com/shivamrustagi), [@vlady-kotsev](https://github.com/vlady-kotsev)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.22.0...v9.23.0-beta.1

# 9.22.0 (2026-08-03)

This is a minor release introducing two flagship (experimental) features — **client-side caching** and **automatic pipelining** — alongside support for Redis 8.10, new commands, and a large batch of stability and parser-robustness fixes. It consolidates everything shipped in 9.22.0-beta.1, so the notes below cover the full 9.21.0 → 9.22.0 upgrade.

⚠️ Two changes to be aware of when upgrading from 9.21.0:

- **Default configuration values changed** ([#3918](https://github.com/redis/go-redis/pull/3918)): read/write timeouts, retry backoff, cluster state reload interval, and TCP keep-alive defaults are now aligned with the cross-SDK configuration proposal (see the highlight below). Explicitly configured values are unaffected.
- **`WaitAOF` return type corrected** ([#3888](https://github.com/redis/go-redis/pull/3888)): `WaitAOF` now returns `*IntSliceCmd`, matching the two-integer reply of `WAITAOF` (previously `*IntCmd`, which failed to parse the reply at runtime). Code referencing the old return type needs a one-line update.

## 🚀 Highlights

### Client-Side Caching (Experimental)

The standalone `Client` gains server-assisted client-side caching built on RESP3 `CLIENT TRACKING`. Enable it by setting `ClientSideCacheConfig` in `Options` (or supply your own cache via `ClientSideCache` — e.g. to share one cache across clients). Cacheable read results are served from a local in-process cache and invalidated automatically when the server reports a change, cutting round trips for read-heavy workloads.

The invalidation architecture is selected by `ClientSideCacheStrategy`; the default (and currently only) strategy is `CSCStrategySharedTracking`: one shared cache, every pool connection runs plain `CLIENT TRACKING ON`, and a background drainer applies buffered invalidations — portable (no BCAST) and consistent with the other Redis client libraries. Requirements and guardrails: RESP3 (`Protocol: 3`), standalone client, DB 0 only; commands that would change the connection identity (`SELECT`, `AUTH`, ...) are rejected while caching is enabled, and CSC is disabled when a credentials provider is set (fixed `Username`/`Password` work and are namespaced). See the README's [client-side caching section](README.md#client-side-caching) and the runnable [example](example/client-side-caching).

**Experimental:** the API may change in a minor release.

([#3941](https://github.com/redis/go-redis/pull/3941)) by [@ofekshenawa](https://github.com/ofekshenawa)

### Automatic Pipelining (Experimental)

`AutoPipeliner` is a background batcher that coalesces commands from many concurrent goroutines into Redis pipelines, multiplying throughput without any manual pipeline management. It comes in two faces, available on `Client` and `ClusterClient` (and configurable via `Options.AutoPipelineOptions` / `UniversalOptions.AutoPipelineOptions`):

- **`AutoPipeline()`** — the blocking face: a drop-in `Cmdable` where each call blocks until executed, exactly like a plain client, while concurrent callers' commands batch together under the hood (measured locally over loopback: ~1M+ SET/sec vs ~100k unpipelined; indicative, not a guarantee). Per-goroutine command order is preserved.
- **`AsyncAutoPipeline()`** — the deferred face: command calls return immediately and every typed result accessor (`Val`/`Result`/`Err`/...) blocks until the command has executed. Submit a window of commands, then read the results, to keep pipelines deep (~2–3M SET/sec locally; indicative).

`AutoPipelineOptions` controls batching: `MaxBatchSize` (soft target, default 200; the blocking face's preset uses 300), `MaxBatchBytes` (approximate payload cap so huge values flush as several bounded writes), `MaxFlushDelay` with optional `AdaptiveDelay` (delay scales down as the queue fills), and `MaxConcurrentBatches` (default 1 = a single ordered batch stream; raising it requires `Unordered: true`, so ordering is never lost by accident — `Validate()` rejects the combination otherwise). A usage tour and throughput comparison live in [`example/autopipeline`](example/autopipeline).

**Experimental:** the API may change in a future release — pin your go-redis version if you adopt it.

([#3942](https://github.com/redis/go-redis/pull/3942)) by [@ndyakov](https://github.com/ndyakov), with help from [@cxljs](https://github.com/cxljs)

### Redis 8.10 Support

This release adds support for **Redis 8.10**. The README's supported-versions list now includes Redis 8.10, and CI runs the full suite against the `redislabs/client-libs-test:8.10.0` image by default ([#3920](https://github.com/redis/go-redis/pull/3920), [#3940](https://github.com/redis/go-redis/pull/3940)).

Coverage for the new commands and options that ship with Redis 8.10:

- **`HIMPORT`** ([#3919](https://github.com/redis/go-redis/pull/3919)) — bulk hash import via server-side fieldsets, exposed as `HImportPrepare`, `HImportSet`, `HImportDiscard`, and `HImportDiscardAll`. Fieldsets are session state scoped to a single physical connection, which does not mix well with connection pooling — so the client keeps a versioned fieldset registry and lazily replays the `PREPARE` on whichever pooled connection executes a `SET` that needs it, at most once per connection, with no extra round trip (the `PREPARE` is injected into the same write as the `SET`).
- **`LMOVEM` / `BLMOVEM`** ([#3913](https://github.com/redis/go-redis/pull/3913)) — move multiple elements between lists in one call.
- **`SUNIONCARD` / `SDIFFCARD`** ([#3897](https://github.com/redis/go-redis/pull/3897)) — cardinality of set union/difference without materializing the result.
- **`XREAD` / `XREADGROUP` `MAXCOUNT` and `MAXSIZE`** ([#3898](https://github.com/redis/go-redis/pull/3898)) — bound how much data a stream read returns.
- **`TS.READ`** ([#3896](https://github.com/redis/go-redis/pull/3896)), **`TS.QUERYLABELS`** ([#3926](https://github.com/redis/go-redis/pull/3926)), **`TS.NRANGE` / `TS.NREVRANGE`** ([#3870](https://github.com/redis/go-redis/pull/3870)) with multiple aggregators per key ([#3937](https://github.com/redis/go-redis/pull/3937)), and **`EXCLUDEEMPTY`** on `TS.MRANGE` / `TS.MREVRANGE` ([#3912](https://github.com/redis/go-redis/pull/3912)) — new time-series query surface.
- **`FT.ALIASLIST`** ([#3925](https://github.com/redis/go-redis/pull/3925)), **`COLLECT` reducer for `FT.AGGREGATE`** ([#3886](https://github.com/redis/go-redis/pull/3886)), **`RERANK` on HNSW vector fields in `FT.CREATE`** ([#3927](https://github.com/redis/go-redis/pull/3927)), and **`FT.HYBRID` timeout warnings** ([#3911](https://github.com/redis/go-redis/pull/3911)) — search coverage.

### Cross-SDK Aligned Defaults

Default configuration values now follow the cross-SDK configuration proposal shared by all Redis client libraries ([#3918](https://github.com/redis/go-redis/pull/3918)):

| Setting | Old default | New default |
|---|---|---|
| `ReadTimeout` / `WriteTimeout` | 3s | 5s |
| Retry backoff (min/max) | 8ms / 512ms | 10ms / 1s |
| Cluster state reload interval | 10s | 60s |
| TCP keep-alive | 5min period | 30s idle / 5s interval / 3 probes (`net.KeepAliveConfig`) |

Applications that set these values explicitly are unaffected; applications relying on the old defaults inherit the new ones.

### Data-Race and Parser Hardening Sweep

A systematic audit fixed data races across the client — hooks (`AddHook`, [#3868](https://github.com/redis/go-redis/pull/3868)), `Ring.SetAddrs` ([#3862](https://github.com/redis/go-redis/pull/3862)), cluster node slices ([#3861](https://github.com/redis/go-redis/pull/3861)), pub/sub reconnect ([#3906](https://github.com/redis/go-redis/pull/3906)), maintenance notifications ([#3894](https://github.com/redis/go-redis/pull/3894), [#3872](https://github.com/redis/go-redis/pull/3872)), pool handoff ([#3876](https://github.com/redis/go-redis/pull/3876)), and `redisotel` ([#3881](https://github.com/redis/go-redis/pull/3881)) — and hardened the RESP parsers against malformed or unexpected replies: over-reads on nil replies ([#3874](https://github.com/redis/go-redis/pull/3874)), integer overflow when skipping map/attribute bodies ([#3877](https://github.com/redis/go-redis/pull/3877)), unhashable RESP3 map keys ([#3873](https://github.com/redis/go-redis/pull/3873)), odd-length flat replies ([#3900](https://github.com/redis/go-redis/pull/3900)), mismatched declared array lengths ([#3907](https://github.com/redis/go-redis/pull/3907)), unexpected extra reply frames ([#3884](https://github.com/redis/go-redis/pull/3884)), and nil elements in numeric/bool slice replies ([#3922](https://github.com/redis/go-redis/pull/3922)).

### PubSub `Receive` Hang Fix

`PeekPushNotificationName` blocked until 36 bytes were buffered, so a short subscribe confirmation (channel name of six or fewer characters) on an otherwise idle connection hung `PubSub.Receive` forever — a regression introduced in 9.20.1 by [#3842](https://github.com/redis/go-redis/pull/3842). The peek now parses whatever is already buffered and only waits for one more byte when the frame prefix is valid but incomplete. Fixes [#3935](https://github.com/redis/go-redis/issues/3935).

([#3936](https://github.com/redis/go-redis/pull/3936)) by [@ndyakov](https://github.com/ndyakov)

### Correct Cluster Transaction Retries

The cluster transaction pipeline treated a `MULTI`...`EXEC` block as independently retryable commands, which could scatter a transaction across nodes or send malformed transactions on retry. Redirects (`MOVED`/`ASK`/`TRYAGAIN`) and aborts are now handled at the whole-transaction level, matching Redis transaction semantics: the transaction is re-routed and retried as a unit, never partially ([#3909](https://github.com/redis/go-redis/pull/3909)) by [@cxljs](https://github.com/cxljs).

### Credential Redaction in Command Tracing

`rediscmd.AppendCmd` — used by `redisotel` and `rediscensus` to render commands into span attributes — now redacts credential arguments as `<redacted>`: `AUTH`, `HELLO ... AUTH`, `CONFIG SET` of `requirepass` / `masterauth` / TLS key passphrases, `ACL SETUSER` password rules, and `MIGRATE ... AUTH`/`AUTH2`. The client sends `HELLO ... AUTH` on every handshake and `AUTH` on every streaming-credentials rotation through the regular hook chain, so tracing hooks previously captured credentials even when the application never issued an auth command itself ([#3939](https://github.com/redis/go-redis/pull/3939)) by [@saddamr3e](https://github.com/saddamr3e).

## ✨ New Features

- **Client-side caching**: server-assisted caching for the standalone client via `ClientSideCacheConfig` / `ClientSideCache`, with the `CSCStrategySharedTracking` invalidation strategy ([#3941](https://github.com/redis/go-redis/pull/3941)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **Automatic pipelining**: `AutoPipeline()` (blocking) and `AsyncAutoPipeline()` (deferred results) on `Client` and `ClusterClient`, configured via `AutoPipelineOptions` ([#3942](https://github.com/redis/go-redis/pull/3942)) by [@ndyakov](https://github.com/ndyakov), with help from [@cxljs](https://github.com/cxljs)
- **`HIMPORT` command family**: `HImportPrepare` / `HImportSet` / `HImportDiscard` / `HImportDiscardAll` with lazy per-connection fieldset prepare replay ([#3919](https://github.com/redis/go-redis/pull/3919)) by [@ndyakov](https://github.com/ndyakov)
- **`LMOVEM` / `BLMOVEM`**: move multiple list elements in one call, with `COUNT` (up to N) or `EXACTLY` (all-or-nothing) semantics via `LMoveMArgs` ([#3913](https://github.com/redis/go-redis/pull/3913)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`SUnionCard` / `SDiffCard`**: cardinality of set union/difference ([#3897](https://github.com/redis/go-redis/pull/3897)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`XRead` / `XReadGroup` `MAXCOUNT` / `MAXSIZE`**: bound stream read responses by entry count or payload size ([#3898](https://github.com/redis/go-redis/pull/3898)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`TS.READ`**: read samples from a series starting at a given timestamp, with `TSReadEarliest` (`-`), `TSReadLatest` (`+`), and `TSReadNew` (`$`) sentinels ([#3896](https://github.com/redis/go-redis/pull/3896)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`TS.QUERYLABELS`**: query label names/values across time series ([#3926](https://github.com/redis/go-redis/pull/3926)) by [@ndyakov](https://github.com/ndyakov)
- **`TS.NRANGE` / `TS.NREVRANGE`**: range queries across multiple series ([#3870](https://github.com/redis/go-redis/pull/3870)) by [@ofekshenawa](https://github.com/ofekshenawa), with multiple aggregators per key ([#3937](https://github.com/redis/go-redis/pull/3937)) by [@ndyakov](https://github.com/ndyakov)
- **`TS.MRANGE` / `TS.MREVRANGE` `EXCLUDEEMPTY`**: skip series with no samples in the result ([#3912](https://github.com/redis/go-redis/pull/3912)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`FT.ALIASLIST`**: list all index aliases ([#3925](https://github.com/redis/go-redis/pull/3925)) by [@ndyakov](https://github.com/ndyakov)
- **`FT.AGGREGATE` `COLLECT` reducer**: collect grouped values into an array ([#3886](https://github.com/redis/go-redis/pull/3886)) by [@ndyakov](https://github.com/ndyakov)
- **`FT.CREATE` `RERANK`**: `RERANK` parameter on HNSW vector field definitions ([#3927](https://github.com/redis/go-redis/pull/3927)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`FT.HYBRID` timeout warnings**: timeout warnings are now populated in hybrid search results ([#3911](https://github.com/redis/go-redis/pull/3911)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`FT.HYBRID` KNN `SHARD_K_RATIO`** (Redis 8.8+): per-shard K ratio for KNN clauses ([#3841](https://github.com/redis/go-redis/pull/3841)) by [@ndyakov](https://github.com/ndyakov)

## 🐛 Bug Fixes

- **PubSub `Receive` hang**: peek push-notification names without demanding 36 buffered bytes, fixing a hang on short subscribe confirmations (fixes [#3935](https://github.com/redis/go-redis/issues/3935), regression from 9.20.1) ([#3936](https://github.com/redis/go-redis/pull/3936)) by [@ndyakov](https://github.com/ndyakov)
- **Cluster transactions**: re-route the whole tx pipeline on redirect/abort instead of per-command ([#3909](https://github.com/redis/go-redis/pull/3909)) by [@cxljs](https://github.com/cxljs)
- **Credential leak in traces**: `rediscmd.AppendCmd` redacts credential arguments (`AUTH`, `HELLO ... AUTH`, `CONFIG SET` secret params, `ACL SETUSER` password rules, `MIGRATE AUTH`/`AUTH2`), so `redisotel` / `rediscensus` span attributes no longer contain passwords ([#3939](https://github.com/redis/go-redis/pull/3939)) by [@saddamr3e](https://github.com/saddamr3e)
- **`WaitAOF` return type**: returns `*IntSliceCmd` matching the two-integer `WAITAOF` reply ([#3888](https://github.com/redis/go-redis/pull/3888)) by [@CipherN9](https://github.com/CipherN9)
- **`Ring.Publish` routing**: publish to the shard that owns the topic instead of a round-robined one ([#3893](https://github.com/redis/go-redis/pull/3893)) by [@dkindel](https://github.com/dkindel)
- **Pool `OnRemove` hooks**: fire `OnRemove` on `putConn` eviction paths so removal hooks see every evicted connection ([#3932](https://github.com/redis/go-redis/pull/3932)) by [@cxljs](https://github.com/cxljs)
- **`UniversalClient` `InfoMap`**: added `InfoMap` to the `Cmdable` interface ([#3904](https://github.com/redis/go-redis/pull/3904)) by [@nazarli-shabnam](https://github.com/nazarli-shabnam)
- **`SlowLogGet` context**: pass the caller's context instead of a background one ([#3915](https://github.com/redis/go-redis/pull/3915)) by [@sonnemusk](https://github.com/sonnemusk)
- **`ModuleLoadex` nil config**: return an error instead of panicking on nil config ([#3916](https://github.com/redis/go-redis/pull/3916)) by [@sonnemusk](https://github.com/sonnemusk)
- **`ParseURL` IPv6 hosts**: keep single brackets for IPv6 hosts without a port ([#3882](https://github.com/redis/go-redis/pull/3882)) by [@sueun-dev](https://github.com/sueun-dev)
- **`ParseURL` durations**: treat unit durations `<= 0` as disabled ([#3866](https://github.com/redis/go-redis/pull/3866)) by [@sueun-dev](https://github.com/sueun-dev)
- **Nil `*uint8` encoding**: encode nil `*uint8` as `"0"` like other numeric pointers ([#3869](https://github.com/redis/go-redis/pull/3869)) by [@sueun-dev](https://github.com/sueun-dev)
- **`JSONSliceCmd` read errors**: return the read error from `readReply` instead of swallowing it ([#3903](https://github.com/redis/go-redis/pull/3903)) by [@saddamr3e](https://github.com/saddamr3e)
- **RESP parser hardening**: reconcile declared entry-array lengths ([#3907](https://github.com/redis/go-redis/pull/3907)), handle nil elements in int/uint/bool slice parsers ([#3922](https://github.com/redis/go-redis/pull/3922)), drain unexpected reply frames ([#3884](https://github.com/redis/go-redis/pull/3884)), reject odd-length flat replies in Z/KeyValue parsers ([#3900](https://github.com/redis/go-redis/pull/3900)), avoid int overflow when skipping map/attr bodies ([#3877](https://github.com/redis/go-redis/pull/3877)), don't over-read nil replies in `Reader.Discard` ([#3874](https://github.com/redis/go-redis/pull/3874)) by [@saddamr3e](https://github.com/saddamr3e); reject unhashable keys in RESP3 map parsing ([#3873](https://github.com/redis/go-redis/pull/3873)) by [@iabdullah215](https://github.com/iabdullah215)
- **Data races**: hook state during `AddHook` ([#3868](https://github.com/redis/go-redis/pull/3868)), `onNewNode` during `Ring.SetAddrs` ([#3862](https://github.com/redis/go-redis/pull/3862)), shared masters/slaves slices in cluster ([#3861](https://github.com/redis/go-redis/pull/3861)), shared `opt.Addr` during pub/sub reconnect ([#3906](https://github.com/redis/go-redis/pull/3906)), `clusterStateReloadCallback` in maintnotifications ([#3894](https://github.com/redis/go-redis/pull/3894)), conn reader in `isHealthyConn` during handoff ([#3876](https://github.com/redis/go-redis/pull/3876)) by [@saddamr3e](https://github.com/saddamr3e); handoff race window in maintnotifications ([#3872](https://github.com/redis/go-redis/pull/3872)) by [@ndyakov](https://github.com/ndyakov)
- **`redisotel`**: use `ObservableCounter` for cumulative pool stats ([#3914](https://github.com/redis/go-redis/pull/3914)) by [@Solaris-star](https://github.com/Solaris-star); avoid a data race on shared attributes during `MinIdleConns` warmup ([#3881](https://github.com/redis/go-redis/pull/3881)) by [@ndyakov](https://github.com/ndyakov)

## 🧰 Maintenance

- **Cross-SDK default alignment**: new defaults for timeouts, retry backoff, cluster state reload, and TCP keep-alive ([#3918](https://github.com/redis/go-redis/pull/3918)) by [@ndyakov](https://github.com/ndyakov)
- **CI on Redis 8.10**: 8.10 made the default test version ([#3920](https://github.com/redis/go-redis/pull/3920)) with version gating by major.minor ([#3908](https://github.com/redis/go-redis/pull/3908)) by [@ofekshenawa](https://github.com/ofekshenawa); the test stack now runs the GA `redislabs/client-libs-test:8.10.0` image and 8.8 was dropped from the CI matrix ([#3940](https://github.com/redis/go-redis/pull/3940))
- **Type-safe atomics**: use typed `sync/atomic` value types ([#3860](https://github.com/redis/go-redis/pull/3860)) and remove the dead `assertUnstableCommand` RESP3 path ([#3928](https://github.com/redis/go-redis/pull/3928)) by [@cxljs](https://github.com/cxljs)
- **Docs**: clarify that `ExpireTime` / `PExpireTime` return Unix timestamps ([#3917](https://github.com/redis/go-redis/pull/3917)) by [@sonnemusk](https://github.com/sonnemusk); remove a duplicate example step ([#3875](https://github.com/redis/go-redis/pull/3875)) by [@andy-stark-redis](https://github.com/andy-stark-redis)

## 👥 Contributors

We'd like to thank all the contributors who worked on this release!

[@andy-stark-redis](https://github.com/andy-stark-redis), [@CipherN9](https://github.com/CipherN9), [@cxljs](https://github.com/cxljs), [@dkindel](https://github.com/dkindel), [@iabdullah215](https://github.com/iabdullah215), [@nazarli-shabnam](https://github.com/nazarli-shabnam), [@ndyakov](https://github.com/ndyakov), [@ofekshenawa](https://github.com/ofekshenawa), [@saddamr3e](https://github.com/saddamr3e), [@Solaris-star](https://github.com/Solaris-star), [@sonnemusk](https://github.com/sonnemusk), [@sueun-dev](https://github.com/sueun-dev)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.21.0...v9.22.0

# 9.22.0-beta.1 (2026-07-29)

This is a **beta** release adding support for Redis 8.10, new commands, and a large batch of stability and parser-robustness fixes. The 9.22.0 GA release will follow once client-side caching and auto-pipelining are merged.

⚠️ Two changes to be aware of when upgrading from 9.21.0:

- **Default configuration values changed** ([#3918](https://github.com/redis/go-redis/pull/3918)): read/write timeouts, retry backoff, cluster state reload interval, and TCP keep-alive defaults are now aligned with the cross-SDK configuration proposal (see the highlight below). Explicitly configured values are unaffected.
- **`WaitAOF` return type corrected** ([#3888](https://github.com/redis/go-redis/pull/3888)): `WaitAOF` now returns `*IntSliceCmd`, matching the two-integer reply of `WAITAOF` (previously `*IntCmd`, which failed to parse the reply at runtime). Code referencing the old return type needs a one-line update.

## 🚀 Highlights

### Redis 8.10 Support

This release adds support for **Redis 8.10**. The README's supported-versions list now includes Redis 8.10, and CI runs the full suite against the `redislabs/client-libs-test:8.10.0` image by default ([#3920](https://github.com/redis/go-redis/pull/3920), [#3940](https://github.com/redis/go-redis/pull/3940)).

Coverage for the new commands and options that ship with Redis 8.10:

- **`HIMPORT`** ([#3919](https://github.com/redis/go-redis/pull/3919)) — bulk hash import via server-side fieldsets, exposed as `HImportPrepare`, `HImportSet`, `HImportDiscard`, and `HImportDiscardAll`. Fieldsets are session state scoped to a single physical connection, which does not mix well with connection pooling — so the client keeps a versioned fieldset registry and lazily replays the `PREPARE` on whichever pooled connection executes a `SET` that needs it, at most once per connection, with no extra round trip (the `PREPARE` is injected into the same write as the `SET`).
- **`LMOVEM` / `BLMOVEM`** ([#3913](https://github.com/redis/go-redis/pull/3913)) — move multiple elements between lists in one call.
- **`SUNIONCARD` / `SDIFFCARD`** ([#3897](https://github.com/redis/go-redis/pull/3897)) — cardinality of set union/difference without materializing the result.
- **`XREAD` / `XREADGROUP` `MAXCOUNT` and `MAXSIZE`** ([#3898](https://github.com/redis/go-redis/pull/3898)) — bound how much data a stream read returns.
- **`TS.READ`** ([#3896](https://github.com/redis/go-redis/pull/3896)), **`TS.QUERYLABELS`** ([#3926](https://github.com/redis/go-redis/pull/3926)), **`TS.NRANGE` / `TS.NREVRANGE`** ([#3870](https://github.com/redis/go-redis/pull/3870)) with multiple aggregators per key ([#3937](https://github.com/redis/go-redis/pull/3937)), and **`EXCLUDEEMPTY`** on `TS.MRANGE` / `TS.MREVRANGE` ([#3912](https://github.com/redis/go-redis/pull/3912)) — new time-series query surface.
- **`FT.ALIASLIST`** ([#3925](https://github.com/redis/go-redis/pull/3925)), **`COLLECT` reducer for `FT.AGGREGATE`** ([#3886](https://github.com/redis/go-redis/pull/3886)), **`RERANK` on HNSW vector fields in `FT.CREATE`** ([#3927](https://github.com/redis/go-redis/pull/3927)), and **`FT.HYBRID` timeout warnings** ([#3911](https://github.com/redis/go-redis/pull/3911)) — search coverage.

### Cross-SDK Aligned Defaults

Default configuration values now follow the cross-SDK configuration proposal shared by all Redis client libraries ([#3918](https://github.com/redis/go-redis/pull/3918)):

| Setting | Old default | New default |
|---|---|---|
| `ReadTimeout` / `WriteTimeout` | 3s | 5s |
| Retry backoff (min/max) | 8ms / 512ms | 10ms / 1s |
| Cluster state reload interval | 10s | 60s |
| TCP keep-alive | 5min period | 30s idle / 5s interval / 3 probes (`net.KeepAliveConfig`) |

Applications that set these values explicitly are unaffected; applications relying on the old defaults inherit the new ones.

### Data-Race and Parser Hardening Sweep

A systematic audit fixed data races across the client — hooks (`AddHook`, [#3868](https://github.com/redis/go-redis/pull/3868)), `Ring.SetAddrs` ([#3862](https://github.com/redis/go-redis/pull/3862)), cluster node slices ([#3861](https://github.com/redis/go-redis/pull/3861)), pub/sub reconnect ([#3906](https://github.com/redis/go-redis/pull/3906)), maintenance notifications ([#3894](https://github.com/redis/go-redis/pull/3894), [#3872](https://github.com/redis/go-redis/pull/3872)), pool handoff ([#3876](https://github.com/redis/go-redis/pull/3876)), and `redisotel` ([#3881](https://github.com/redis/go-redis/pull/3881)) — and hardened the RESP parsers against malformed or unexpected replies: over-reads on nil replies ([#3874](https://github.com/redis/go-redis/pull/3874)), integer overflow when skipping map/attribute bodies ([#3877](https://github.com/redis/go-redis/pull/3877)), unhashable RESP3 map keys ([#3873](https://github.com/redis/go-redis/pull/3873)), odd-length flat replies ([#3900](https://github.com/redis/go-redis/pull/3900)), mismatched declared array lengths ([#3907](https://github.com/redis/go-redis/pull/3907)), unexpected extra reply frames ([#3884](https://github.com/redis/go-redis/pull/3884)), and nil elements in numeric/bool slice replies ([#3922](https://github.com/redis/go-redis/pull/3922)).

### PubSub `Receive` Hang Fix

`PeekPushNotificationName` blocked until 36 bytes were buffered, so a short subscribe confirmation (channel name of six or fewer characters) on an otherwise idle connection hung `PubSub.Receive` forever — a regression introduced in 9.20.1 by [#3842](https://github.com/redis/go-redis/pull/3842). The peek now parses whatever is already buffered and only waits for one more byte when the frame prefix is valid but incomplete. Fixes [#3935](https://github.com/redis/go-redis/issues/3935).

([#3936](https://github.com/redis/go-redis/pull/3936)) by [@ndyakov](https://github.com/ndyakov)

### Correct Cluster Transaction Retries

The cluster transaction pipeline treated a `MULTI`...`EXEC` block as independently retryable commands, which could scatter a transaction across nodes or send malformed transactions on retry. Redirects (`MOVED`/`ASK`/`TRYAGAIN`) and aborts are now handled at the whole-transaction level, matching Redis transaction semantics: the transaction is re-routed and retried as a unit, never partially ([#3909](https://github.com/redis/go-redis/pull/3909)) by [@cxljs](https://github.com/cxljs).

### Credential Redaction in Command Tracing

`rediscmd.AppendCmd` — used by `redisotel` and `rediscensus` to render commands into span attributes — now redacts credential arguments as `<redacted>`: `AUTH`, `HELLO ... AUTH`, `CONFIG SET` of `requirepass` / `masterauth` / TLS key passphrases, `ACL SETUSER` password rules, and `MIGRATE ... AUTH`/`AUTH2`. The client sends `HELLO ... AUTH` on every handshake and `AUTH` on every streaming-credentials rotation through the regular hook chain, so tracing hooks previously captured credentials even when the application never issued an auth command itself ([#3939](https://github.com/redis/go-redis/pull/3939)) by [@saddamr3e](https://github.com/saddamr3e).

## ✨ New Features

- **`HIMPORT` command family**: `HImportPrepare` / `HImportSet` / `HImportDiscard` / `HImportDiscardAll` with lazy per-connection fieldset prepare replay ([#3919](https://github.com/redis/go-redis/pull/3919)) by [@ndyakov](https://github.com/ndyakov)
- **`LMOVEM` / `BLMOVEM`**: move multiple list elements in one call, with `COUNT` (up to N) or `EXACTLY` (all-or-nothing) semantics via `LMoveMArgs` ([#3913](https://github.com/redis/go-redis/pull/3913)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`SUnionCard` / `SDiffCard`**: cardinality of set union/difference ([#3897](https://github.com/redis/go-redis/pull/3897)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`XRead` / `XReadGroup` `MAXCOUNT` / `MAXSIZE`**: bound stream read responses by entry count or payload size ([#3898](https://github.com/redis/go-redis/pull/3898)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`TS.READ`**: read samples from a series starting at a given timestamp, with `TSReadEarliest` (`-`), `TSReadLatest` (`+`), and `TSReadNew` (`$`) sentinels ([#3896](https://github.com/redis/go-redis/pull/3896)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`TS.QUERYLABELS`**: query label names/values across time series ([#3926](https://github.com/redis/go-redis/pull/3926)) by [@ndyakov](https://github.com/ndyakov)
- **`TS.NRANGE` / `TS.NREVRANGE`**: range queries across multiple series ([#3870](https://github.com/redis/go-redis/pull/3870)) by [@ofekshenawa](https://github.com/ofekshenawa), with multiple aggregators per key ([#3937](https://github.com/redis/go-redis/pull/3937)) by [@ndyakov](https://github.com/ndyakov)
- **`TS.MRANGE` / `TS.MREVRANGE` `EXCLUDEEMPTY`**: skip series with no samples in the result ([#3912](https://github.com/redis/go-redis/pull/3912)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`FT.ALIASLIST`**: list all index aliases ([#3925](https://github.com/redis/go-redis/pull/3925)) by [@ndyakov](https://github.com/ndyakov)
- **`FT.AGGREGATE` `COLLECT` reducer**: collect grouped values into an array ([#3886](https://github.com/redis/go-redis/pull/3886)) by [@ndyakov](https://github.com/ndyakov)
- **`FT.CREATE` `RERANK`**: `RERANK` parameter on HNSW vector field definitions ([#3927](https://github.com/redis/go-redis/pull/3927)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`FT.HYBRID` timeout warnings**: timeout warnings are now populated in hybrid search results ([#3911](https://github.com/redis/go-redis/pull/3911)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`FT.HYBRID` KNN `SHARD_K_RATIO`** (Redis 8.8+): per-shard K ratio for KNN clauses ([#3841](https://github.com/redis/go-redis/pull/3841)) by [@ndyakov](https://github.com/ndyakov)

## 🐛 Bug Fixes

- **PubSub `Receive` hang**: peek push-notification names without demanding 36 buffered bytes, fixing a hang on short subscribe confirmations (fixes [#3935](https://github.com/redis/go-redis/issues/3935), regression from 9.20.1) ([#3936](https://github.com/redis/go-redis/pull/3936)) by [@ndyakov](https://github.com/ndyakov)
- **Cluster transactions**: re-route the whole tx pipeline on redirect/abort instead of per-command ([#3909](https://github.com/redis/go-redis/pull/3909)) by [@cxljs](https://github.com/cxljs)
- **Credential leak in traces**: `rediscmd.AppendCmd` redacts credential arguments (`AUTH`, `HELLO ... AUTH`, `CONFIG SET` secret params, `ACL SETUSER` password rules, `MIGRATE AUTH`/`AUTH2`), so `redisotel` / `rediscensus` span attributes no longer contain passwords ([#3939](https://github.com/redis/go-redis/pull/3939)) by [@saddamr3e](https://github.com/saddamr3e)
- **`WaitAOF` return type**: returns `*IntSliceCmd` matching the two-integer `WAITAOF` reply ([#3888](https://github.com/redis/go-redis/pull/3888)) by [@CipherN9](https://github.com/CipherN9)
- **`Ring.Publish` routing**: publish to the shard that owns the topic instead of a round-robined one ([#3893](https://github.com/redis/go-redis/pull/3893)) by [@dkindel](https://github.com/dkindel)
- **Pool `OnRemove` hooks**: fire `OnRemove` on `putConn` eviction paths so removal hooks see every evicted connection ([#3932](https://github.com/redis/go-redis/pull/3932)) by [@cxljs](https://github.com/cxljs)
- **`UniversalClient` `InfoMap`**: added `InfoMap` to the `Cmdable` interface ([#3904](https://github.com/redis/go-redis/pull/3904)) by [@nazarli-shabnam](https://github.com/nazarli-shabnam)
- **`SlowLogGet` context**: pass the caller's context instead of a background one ([#3915](https://github.com/redis/go-redis/pull/3915)) by [@sonnemusk](https://github.com/sonnemusk)
- **`ModuleLoadex` nil config**: return an error instead of panicking on nil config ([#3916](https://github.com/redis/go-redis/pull/3916)) by [@sonnemusk](https://github.com/sonnemusk)
- **`ParseURL` IPv6 hosts**: keep single brackets for IPv6 hosts without a port ([#3882](https://github.com/redis/go-redis/pull/3882)) by [@sueun-dev](https://github.com/sueun-dev)
- **`ParseURL` durations**: treat unit durations `<= 0` as disabled ([#3866](https://github.com/redis/go-redis/pull/3866)) by [@sueun-dev](https://github.com/sueun-dev)
- **Nil `*uint8` encoding**: encode nil `*uint8` as `"0"` like other numeric pointers ([#3869](https://github.com/redis/go-redis/pull/3869)) by [@sueun-dev](https://github.com/sueun-dev)
- **`JSONSliceCmd` read errors**: return the read error from `readReply` instead of swallowing it ([#3903](https://github.com/redis/go-redis/pull/3903)) by [@saddamr3e](https://github.com/saddamr3e)
- **RESP parser hardening**: reconcile declared entry-array lengths ([#3907](https://github.com/redis/go-redis/pull/3907)), handle nil elements in int/uint/bool slice parsers ([#3922](https://github.com/redis/go-redis/pull/3922)), drain unexpected reply frames ([#3884](https://github.com/redis/go-redis/pull/3884)), reject odd-length flat replies in Z/KeyValue parsers ([#3900](https://github.com/redis/go-redis/pull/3900)), avoid int overflow when skipping map/attr bodies ([#3877](https://github.com/redis/go-redis/pull/3877)), don't over-read nil replies in `Reader.Discard` ([#3874](https://github.com/redis/go-redis/pull/3874)) by [@saddamr3e](https://github.com/saddamr3e); reject unhashable keys in RESP3 map parsing ([#3873](https://github.com/redis/go-redis/pull/3873)) by [@iabdullah215](https://github.com/iabdullah215)
- **Data races**: hook state during `AddHook` ([#3868](https://github.com/redis/go-redis/pull/3868)), `onNewNode` during `Ring.SetAddrs` ([#3862](https://github.com/redis/go-redis/pull/3862)), shared masters/slaves slices in cluster ([#3861](https://github.com/redis/go-redis/pull/3861)), shared `opt.Addr` during pub/sub reconnect ([#3906](https://github.com/redis/go-redis/pull/3906)), `clusterStateReloadCallback` in maintnotifications ([#3894](https://github.com/redis/go-redis/pull/3894)), conn reader in `isHealthyConn` during handoff ([#3876](https://github.com/redis/go-redis/pull/3876)) by [@saddamr3e](https://github.com/saddamr3e); handoff race window in maintnotifications ([#3872](https://github.com/redis/go-redis/pull/3872)) by [@ndyakov](https://github.com/ndyakov)
- **`redisotel`**: use `ObservableCounter` for cumulative pool stats ([#3914](https://github.com/redis/go-redis/pull/3914)) by [@Solaris-star](https://github.com/Solaris-star); avoid a data race on shared attributes during `MinIdleConns` warmup ([#3881](https://github.com/redis/go-redis/pull/3881)) by [@ndyakov](https://github.com/ndyakov)

## 🧰 Maintenance

- **Cross-SDK default alignment**: new defaults for timeouts, retry backoff, cluster state reload, and TCP keep-alive ([#3918](https://github.com/redis/go-redis/pull/3918)) by [@ndyakov](https://github.com/ndyakov)
- **CI on Redis 8.10**: 8.10 made the default test version ([#3920](https://github.com/redis/go-redis/pull/3920)) with version gating by major.minor ([#3908](https://github.com/redis/go-redis/pull/3908)) by [@ofekshenawa](https://github.com/ofekshenawa); the test stack now runs the GA `redislabs/client-libs-test:8.10.0` image and 8.8 was dropped from the CI matrix ([#3940](https://github.com/redis/go-redis/pull/3940))
- **Type-safe atomics**: use typed `sync/atomic` value types ([#3860](https://github.com/redis/go-redis/pull/3860)) and remove the dead `assertUnstableCommand` RESP3 path ([#3928](https://github.com/redis/go-redis/pull/3928)) by [@cxljs](https://github.com/cxljs)
- **Docs**: clarify that `ExpireTime` / `PExpireTime` return Unix timestamps ([#3917](https://github.com/redis/go-redis/pull/3917)) by [@sonnemusk](https://github.com/sonnemusk); remove a duplicate example step ([#3875](https://github.com/redis/go-redis/pull/3875)) by [@andy-stark-redis](https://github.com/andy-stark-redis)

## 👥 Contributors

We'd like to thank all the contributors who worked on this release!

[@andy-stark-redis](https://github.com/andy-stark-redis), [@CipherN9](https://github.com/CipherN9), [@cxljs](https://github.com/cxljs), [@dkindel](https://github.com/dkindel), [@iabdullah215](https://github.com/iabdullah215), [@nazarli-shabnam](https://github.com/nazarli-shabnam), [@ndyakov](https://github.com/ndyakov), [@ofekshenawa](https://github.com/ofekshenawa), [@saddamr3e](https://github.com/saddamr3e), [@Solaris-star](https://github.com/Solaris-star), [@sonnemusk](https://github.com/sonnemusk), [@sueun-dev](https://github.com/sueun-dev)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.21.0...v9.22.0-beta.1

# 9.21.0 (2026-06-18)

This is a minor release adding new features and bug fixes. There are no breaking changes; upgrading from 9.20.x is a drop-in replacement.

## 🚀 Highlights

### Zero-copy `GetToBuffer` / `SetFromBuffer`

Two new `StringCmdable` methods let callers read and write Redis string values directly into and from pre-allocated byte buffers, eliminating the per-call payload allocation that `Get`/`Set` incur:

```go
GetToBuffer(ctx, key, buf) *ZeroCopyStringCmd   // reads into buf; ZeroCopyStringCmd { Val() int; Bytes() []byte; Result() (int, error) }
SetFromBuffer(ctx, key, buf) *StatusCmd
```

`GetToBuffer` decodes the bulk reply straight into the caller-owned `buf` (no intermediate allocation); a buffer that is too small returns an error after draining the payload, so the connection stays aligned for the next reply. `SetFromBuffer` is provided for API symmetry — it dispatches to the same `[]byte` writer path as `Set(ctx, key, buf, 0)` and produces byte-identical output on the wire. Available on `*Client`, `*ClusterClient`, `*Ring`, `*Conn` and `Pipeliner`.

([#3834](https://github.com/redis/go-redis/pull/3834)) by [@ndyakov](https://github.com/ndyakov)

### Explicit `LIMIT 0` for stream trimming

Redis treats `XTRIM`/`XADD` approximate-trim (`~`) `LIMIT 0` as "disable the trimming effort cap entirely", which differs from omitting `LIMIT` (the implicit `100 * stream-node-max-entries` default). The command builders previously only emitted `LIMIT` when `limit > 0`, so callers could never send an explicit `LIMIT 0`. Following the `KeepTTL = -1` precedent, the new `XTrimLimitDisabled = -1` sentinel now emits an explicit `LIMIT 0`; `limit == 0` keeps the historical no-`LIMIT` behavior, so existing callers produce byte-identical commands.

([#3848](https://github.com/redis/go-redis/pull/3848)) by [@TheRealMal](https://github.com/TheRealMal)

## ✨ New Features

- **Zero-copy buffer string commands**: new `GetToBuffer` / `SetFromBuffer` on `StringCmdable` and the `ZeroCopyStringCmd` result type, reading/writing string values into caller-owned buffers without per-call payload allocation ([#3834](https://github.com/redis/go-redis/pull/3834)) by [@ndyakov](https://github.com/ndyakov)
- **`XTrimLimitDisabled` sentinel**: `XTRIM`/`XADD` approximate trimming can now send an explicit `LIMIT 0` to disable the trim effort cap, via the new `XTrimLimitDisabled = -1` sentinel ([#3848](https://github.com/redis/go-redis/pull/3848)) by [@TheRealMal](https://github.com/TheRealMal)
- **PubSub health-check timeouts**: `channel.initHealthCheck` now bounds the `Ping` it issues with a fresh per-check timeout context (the exported `pingTimeout` / `reconnectTimeout`) instead of `context.TODO()`, so a stuck health-check Ping can no longer block indefinitely ([#3819](https://github.com/redis/go-redis/pull/3819)) by [@abdellani](https://github.com/abdellani)
- **Skip redundant `UNWATCH` in `Tx.Close`**: a transaction now tracks whether a `WATCH` is still active (`watchArmed`) and only issues `UNWATCH` on `Close` when it is, removing an extra round trip on the common `WATCH`/.../`EXEC` and no-key `Watch` paths while never returning a connection to the pool with an active watch ([#3854](https://github.com/redis/go-redis/pull/3854)) by [@fcostaoliveira](https://github.com/fcostaoliveira)

## 🐛 Bug Fixes

- **`maintnotifications` `ModeAuto` fail-open**: `ModeAuto` now stays fail-open when the server does not support maintenance notifications — connections are retired and tracking is guarded during downgrade so the client keeps working instead of erroring ([#3853](https://github.com/redis/go-redis/pull/3853)) by [@terrorobe](https://github.com/terrorobe)

## 👥 Contributors

We'd like to thank all the contributors who worked on this release!

[@abdellani](https://github.com/abdellani), [@fcostaoliveira](https://github.com/fcostaoliveira), [@ndyakov](https://github.com/ndyakov), [@terrorobe](https://github.com/terrorobe), [@TheRealMal](https://github.com/TheRealMal)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.20.1...v9.21.0

# 9.20.1 (2026-06-11)

This is a patch release containing bug fixes only. There are no new features or breaking changes; upgrading from 9.20.0 is a drop-in replacement.

## 🚀 Highlights

### RESP3 pub/sub message loss fixed

`PeekPushNotificationName` previously inspected only the bytes already buffered by `bufio`, so when a push frame header straddled a buffer fill boundary it could return a **truncated** notification name (e.g. `"messa"` instead of `"message"`). The push processor then mis-routed the frame and `ReadReply` silently dropped it, causing intermittent RESP3 pub/sub message loss. The peek now grows its window (36 bytes → up to 4 KiB) and reads more from the connection until the header is complete, cleanly separating incomplete prefixes from corrupt frames (including overflow-safe bulk-length handling). Fixes [#3839](https://github.com/redis/go-redis/issues/3839).

([#3842](https://github.com/redis/go-redis/pull/3842)) by [@ndyakov](https://github.com/ndyakov)

## 🐛 Bug Fixes

- **RESP3 push peeking**: `PeekPushNotificationName` no longer returns a truncated notification name when a push frame header spans a buffer boundary, preventing silent RESP3 pub/sub message loss (fixes [#3839](https://github.com/redis/go-redis/issues/3839)) ([#3842](https://github.com/redis/go-redis/pull/3842)) by [@ndyakov](https://github.com/ndyakov)
- **`FT.HYBRID` vector params**: Vector data is now always sent via `PARAMS` with auto-generated param names (`__vector_param_N`, with collision avoidance) when `VectorParamName` is omitted, since Redis no longer accepts inline vector blobs; the `FTHybridOptions.Params` map is no longer mutated, so the same options struct can be reused across calls ([#3844](https://github.com/redis/go-redis/pull/3844)) by [@ndyakov](https://github.com/ndyakov)
- **`CLUSTER SHARDS` forward compatibility**: Unknown shard- and node-level attributes in the `CLUSTER SHARDS` reply are now skipped via `DiscardNext()` instead of erroring, so clients keep working when the server introduces new fields ([#3843](https://github.com/redis/go-redis/pull/3843)) by [@madolson](https://github.com/madolson)
- **PubSub double reconnect**: `PubSub.releaseConn` no longer reconnects twice when a connection is both unusable (or pending handoff) and reports a bad-connection error, avoiding a wasted connection establish-then-close cycle ([#3833](https://github.com/redis/go-redis/pull/3833)) by [@cxljs](https://github.com/cxljs)

## 👥 Contributors

We'd like to thank all the contributors who worked on this release!

[@cxljs](https://github.com/cxljs), [@madolson](https://github.com/madolson), [@ndyakov](https://github.com/ndyakov)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.20.0...v9.20.1

# 9.20.0 (2026-05-28)

## 🚀 Highlights

### Redis 8.8 Support

This release adds support for **Redis 8.8**. The README's supported-versions list now includes Redis 8.8 alongside 8.0/8.2/8.4, and CI exercises the `8.8-rc1` client-libs-test image across the full suite (Makefile, build workflow, doctests, run-tests action, and docker-compose).

Coverage for the new commands that ship in the 8.x line, rounded out in this release:

- **`AR*` array data type** ([#3813](https://github.com/redis/go-redis/pull/3813)) — new array data structure, exposed via the `ArrayCmdable` interface (see the experimental-features highlight below).
- **`INCREX`** ([#3816](https://github.com/redis/go-redis/pull/3816)) — atomic increment with expiration in a single round-trip.
- **`XNACK`** ([#3790](https://github.com/redis/go-redis/pull/3790)) — explicit negative-acknowledge of pending stream entries.
- **`XAUTOCLAIM` PEL deletes** ([#3798](https://github.com/redis/go-redis/pull/3798)) — `XAUTOCLAIM`/`XAUTOCLAIMJUSTID` now return the list of deleted message IDs from the pending entries list.
- **`TS.RANGE` multiple aggregators** ([#3791](https://github.com/redis/go-redis/pull/3791)) — `TS.RANGE`/`TS.REVRANGE`/`TS.MRANGE`/`TS.MREVRANGE` accept multiple aggregators in a single call.
- **`Z(UNION|INTER|DIFF)` `COUNT` aggregator** ([#3802](https://github.com/redis/go-redis/pull/3802)) — `COUNT` reducer for sorted-set set operations.
- **`JSON.SET FPHA`** ([#3797](https://github.com/redis/go-redis/pull/3797)) — new `FPHA` argument that specifies the floating-point type for homogeneous FP arrays.

CI image bump ([#3814](https://github.com/redis/go-redis/pull/3814)) by [@ofekshenawa](https://github.com/ofekshenawa). Command coverage contributions by [@cxljs](https://github.com/cxljs), [@elena-kolevska](https://github.com/elena-kolevska), [@Khukharr](https://github.com/Khukharr), [@ndyakov](https://github.com/ndyakov), and [@ofekshenawa](https://github.com/ofekshenawa).

### Stable RESP3 for RediSearch (`UnstableResp3` deprecated)

`FT.SEARCH`, `FT.AGGREGATE`, `FT.INFO`, `FT.SPELLCHECK`, and `FT.SYNDUMP` now parse RESP3 (map) responses into the same typed result objects as RESP2 — `Val()` and `Result()` work uniformly on both protocols, no flag required. Previously, RESP3 search responses required `UnstableResp3: true` and were returned as opaque maps accessible only via `RawResult()` / `RawVal()`.

As a result, the `UnstableResp3` option is now a **no-op** across every options struct (`Options`, `ClusterOptions`, `UniversalOptions`, `FailoverOptions`, `RingOptions`) and has been marked `// Deprecated:`. The field is retained for backwards compatibility — existing code that sets `UnstableResp3: true` will continue to compile and behave identically — but it will be removed in a future release and new code should not set it. `RawResult()` / `RawVal()` continue to work for callers that prefer the raw RESP payload.

([#3741](https://github.com/redis/go-redis/pull/3741)) by [@ndyakov](https://github.com/ndyakov)

### Experimental Array Data Structure Commands

Adds an experimental `ArrayCmdable` interface with the `AR*` command family (`ARSet`, `ARGet`, `ARGetRange`, `ARMSet`, `ARMGet`, `ARDel`, `ARDelRange`, `ARScan`, `ARSeek`, `ARNext`, `ARLastItems`, `ARGrep`, `ARGrepWithValues`, `ARInfo`/`ARInfoFull`, and typed reducers `AROpSum`/`AROpMin`/`AROpMax`/`AROpAnd`/`AROpOr`/`AROpXor`/`AROpMatch`/`AROpUsed`) for working with Redis 8.8's new array data type. **API is experimental and may change in a future release.**

([#3813](https://github.com/redis/go-redis/pull/3813)) by [@cxljs](https://github.com/cxljs)

## ✨ New Features

- **RESP3 search parser**: First-class RESP3 parsing for `FT.SEARCH`/`FT.AGGREGATE`/`FT.INFO`/`FT.SPELLCHECK`/`FT.SYNDUMP` responses with backwards compatibility for RESP2 ([#3741](https://github.com/redis/go-redis/pull/3741)) by [@ndyakov](https://github.com/ndyakov)
- **INCREX**: New `INCREX` command support — atomic increment with expiration ([#3816](https://github.com/redis/go-redis/pull/3816)) by [@ndyakov](https://github.com/ndyakov)
- **XNACK**: Client support for the `XNACK` stream command for explicitly negative-acknowledging pending entries ([#3790](https://github.com/redis/go-redis/pull/3790)) by [@elena-kolevska](https://github.com/elena-kolevska)
- **TS range multiple aggregators**: `TS.RANGE`/`TS.REVRANGE`/`TS.MRANGE`/`TS.MREVRANGE` now accept multiple aggregators in a single call ([#3791](https://github.com/redis/go-redis/pull/3791)) by [@elena-kolevska](https://github.com/elena-kolevska)
- **`XAutoClaim` deleted IDs**: `XAUTOCLAIM`/`XAUTOCLAIMJUSTID` now return the list of deleted message IDs from the PEL ([#3798](https://github.com/redis/go-redis/pull/3798)) by [@Khukharr](https://github.com/Khukharr)
- **`JSON.SET FPHA`**: `JSON.SET` accepts a new `FPHA` argument that specifies the floating-point type for homogeneous floating-point arrays ([#3797](https://github.com/redis/go-redis/pull/3797)) by [@ndyakov](https://github.com/ndyakov)
- **Sorted-set union/intersection COUNT**: `ZUNION`/`ZINTER`/`ZDIFF` aggregator now supports `COUNT` ([#3802](https://github.com/redis/go-redis/pull/3802)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **`FT.HYBRID` vector validation**: Validates hybrid-search vector input types and adds proper typed vector parameters ([#3756](https://github.com/redis/go-redis/pull/3756)) by [@DengY11](https://github.com/DengY11)
- **Cluster pool wait stats**: `ClusterClient.PoolStats()` now accumulates `WaitCount` and `WaitDurationNs` across all node pools (previously always zero) ([#3809](https://github.com/redis/go-redis/pull/3809)) by [@LINKIWI](https://github.com/LINKIWI)

## 🐛 Bug Fixes

- **TLS-only Cluster PubSub**: `CLUSTER SLOTS` port-0 entries now fall back to the origin endpoint's port, fixing `dial tcp <ip>:0: connection refused` on TLS-only clusters started with `--port 0 --tls-port <port>` (fixes [#3726](https://github.com/redis/go-redis/issues/3726)) ([#3828](https://github.com/redis/go-redis/pull/3828)) by [@ndyakov](https://github.com/ndyakov)
- **Sharded PubSub reconnect routing**: `PubSub.conn()` now passes both regular (`c.channels`) and sharded (`c.schannels`) channels into the per-PubSub `newConn` closure. Previously, `ClusterClient.SSubscribe`-only PubSubs reconnected to a random node (because the routing closure saw an empty channel list), the `SSUBSCRIBE` was sent to the wrong shard, and the resulting `MOVED` reply was silently dropped ([#3829](https://github.com/redis/go-redis/pull/3829)) by [@ndyakov](https://github.com/ndyakov)
- **ClusterClient `Watch` retry**: User errors returned from a `Watch` callback are no longer subjected to cluster-retry classification; transient cluster errors still retry, but a callback returning e.g. `net.ErrClosed` short-circuits immediately ([#3821](https://github.com/redis/go-redis/pull/3821)) by [@obiyang](https://github.com/obiyang)
- **Sentinel concurrent-probe leak**: `MasterAddr`'s concurrent sentinel probe now closes the non-winning sentinel clients instead of leaking them ([#3827](https://github.com/redis/go-redis/pull/3827)) by [@cxljs](https://github.com/cxljs)
- **Sentinel rediscovery loop on master-only setups**: `replicaAddrs` no longer tears down the cached sentinel client when the replica list is empty, eliminating a continuous rediscovery loop on master-only Sentinel deployments that flooded logs and added per-operation latency ([#3795](https://github.com/redis/go-redis/pull/3795)) by [@shahyash2609](https://github.com/shahyash2609)
- **Pool `CloseConn` hooks**: `Pool.CloseConn` now triggers registered hooks, fixing a memory leak when connections are closed explicitly rather than via the normal removal path ([#3818](https://github.com/redis/go-redis/pull/3818)) by [@ndyakov](https://github.com/ndyakov)
- **Dial TCP error redirection**: Wrapped `dial tcp` errors are now correctly classified as redirectable so cluster routing can recover from a single unreachable node ([#3810](https://github.com/redis/go-redis/pull/3810)) by [@vladisa88](https://github.com/vladisa88)
- **Pool `Close` health checks**: `ConnPool.Close` now only runs health checks against idle connections, avoiding spurious activity on connections still in use ([#3805](https://github.com/redis/go-redis/pull/3805)) by [@ndyakov](https://github.com/ndyakov)
- **VLinks return type**: Fixed the return type of `VLINKS`/`VLINKSWITHSCORES` vector-set replies ([#3820](https://github.com/redis/go-redis/pull/3820)) by [@romanpovol](https://github.com/romanpovol)

## 🧪 Testing & Infrastructure

- **Flaky tests**: Stabilized several flaky tests in the sentinel and pool suites ([#3815](https://github.com/redis/go-redis/pull/3815)) by [@ndyakov](https://github.com/ndyakov)
- **Sentinel failover metric race**: Fixed a data race in the sentinel failover metric test ([#3824](https://github.com/redis/go-redis/pull/3824)) by [@cxljs](https://github.com/cxljs)
- **`waitForSentinelClusterStable` post-conditions**: The sentinel test harness now waits for replicas to be fully connected (not just present in the count) and is robust to randomized spec ordering after failover specs, eliminating an intermittent `Expected master to equal slave` flake ([#3830](https://github.com/redis/go-redis/pull/3830)) by [@ndyakov](https://github.com/ndyakov)
- **`govulncheck` workflow**: New scheduled GitHub Actions workflow runs `govulncheck` on every push, PR, and weekly, surfacing newly disclosed Go vulnerabilities even when no code changes ([#3779](https://github.com/redis/go-redis/pull/3779)) by [@solardome](https://github.com/solardome)
- **CI Redis 8.8-rc1**: CI now exercises the 8.8-rc1 Redis image ([#3814](https://github.com/redis/go-redis/pull/3814)) by [@ofekshenawa](https://github.com/ofekshenawa)

## 🧰 Maintenance

- **`Cmd.Slot()` lookup refactor**: Caches the per-command `CommandInfo` and short-circuits keyless commands before the switch dispatch, removing redundant `Peek` calls ([#3804](https://github.com/redis/go-redis/pull/3804)) by [@retr0-kernel](https://github.com/retr0-kernel)
- **stdlib `math/rand`**: Replaced `internal/rand` with `math/rand` from the standard library now that the minimum Go version is 1.24 ([#3823](https://github.com/redis/go-redis/pull/3823)) by [@cxljs](https://github.com/cxljs)
- **ConnPool queue channel**: Removed the unused queue channel from `ConnPool`, trimming the pool's footprint ([#3826](https://github.com/redis/go-redis/pull/3826)) by [@cxljs](https://github.com/cxljs)
- **Extra packages LICENSE**: Added a LICENSE file to each `extra/*` package ([#3817](https://github.com/redis/go-redis/pull/3817)) by [@ndyakov](https://github.com/ndyakov)
- **README & CI image**: Documentation refresh and bumped the default CI image tag ([#3822](https://github.com/redis/go-redis/pull/3822)) by [@ndyakov](https://github.com/ndyakov)

## 👥 Contributors

We'd like to thank all the contributors who worked on this release!

[@cxljs](https://github.com/cxljs), [@DengY11](https://github.com/DengY11), [@elena-kolevska](https://github.com/elena-kolevska), [@Khukharr](https://github.com/Khukharr), [@LINKIWI](https://github.com/LINKIWI), [@ndyakov](https://github.com/ndyakov), [@obiyang](https://github.com/obiyang), [@ofekshenawa](https://github.com/ofekshenawa), [@retr0-kernel](https://github.com/retr0-kernel), [@romanpovol](https://github.com/romanpovol), [@shahyash2609](https://github.com/shahyash2609), [@solardome](https://github.com/solardome), [@vladisa88](https://github.com/vladisa88)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.19.0...v9.20.0

# 9.19.0 (2026-04-27)

## 🚀 Highlights

### FIPS-Compatible Script Helper

`Script` now supports a FIPS-safe execution mode that avoids client-side SHA-1 computation, which is blocked in strict FIPS environments. A new `NewScriptServerSHA` constructor uses `SCRIPT LOAD` to obtain and cache the digest from the server, then runs commands via `EVALSHA`/`EVALSHA_RO`. Falls back to `EVAL`/`EVALRO` if loading fails, and transparently retries once on `NOSCRIPT`. The default behavior is unchanged for existing users.

([#3700](https://github.com/redis/go-redis/pull/3700)) by [@chaitanyabodlapati](https://github.com/chaitanyabodlapati)

### FT.AGGREGATE Step-Based Pipeline Builder

Added a new step-based `FT.AGGREGATE` pipeline API via `FTAggregateOptions.Steps`, allowing `LOAD`, `APPLY`, `GROUPBY`, and `SORTBY` (with per-step `MAX`) to be repeated and interleaved in arbitrary order — matching Redis's native multi-stage aggregation semantics. The legacy `Load`/`Apply`/`GroupBy`/`SortBy`/`SortByMax` fields are now deprecated.

([#3782](https://github.com/redis/go-redis/pull/3782)) by [@ndyakov](https://github.com/ndyakov)

### Raw RESP Protocol Access

Added `DoRaw` and `DoRawWriteTo` methods for executing arbitrary commands and reading the raw RESP response. Useful for proxying, custom protocol inspection, and working with commands not yet wrapped by go-redis.

([#3713](https://github.com/redis/go-redis/pull/3713)) by [@ofekshenawa](https://github.com/ofekshenawa)

### Configurable Dial Retry Backoff

Added `DialerRetryBackoff` option (plumbed through `Options`, `ClusterOptions`, `RingOptions`, `FailoverOptions`) to let callers customize the delay between failed dial attempts. Helpers `DialRetryBackoffConstant` and `DialRetryBackoffExponential` (with jitter and cap) are provided out of the box. Dial timeout is now also applied **per attempt** rather than across all retries.

([#3706](https://github.com/redis/go-redis/pull/3706), [#3705](https://github.com/redis/go-redis/pull/3705)) by [@mwhooker](https://github.com/mwhooker)

## ✨ New Features

- **FT.AGGREGATE Steps**: Step-based pipeline builder for `FT.AGGREGATE` with support for repeated/interleaved `LOAD`, `APPLY`, `GROUPBY`, and `SORTBY` stages ([#3782](https://github.com/redis/go-redis/pull/3782)) by [@ndyakov](https://github.com/ndyakov)
- **VectorSet commands**: Added `VISMEMBER` and `WITHATTRIBS` support ([#3753](https://github.com/redis/go-redis/pull/3753)) by [@romanpovol](https://github.com/romanpovol)
- **FIPS-safe Script**: `NewScriptServerSHA` uses `SCRIPT LOAD` to obtain the digest from the server, avoiding client-side SHA-1 ([#3700](https://github.com/redis/go-redis/pull/3700)) by [@chaitanyabodlapati](https://github.com/chaitanyabodlapati)
- **Raw RESP access**: `DoRaw` and `DoRawWriteTo` for raw RESP protocol access ([#3713](https://github.com/redis/go-redis/pull/3713)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **Dial retry backoff**: `DialerRetryBackoff` function option with constant and exponential helpers ([#3706](https://github.com/redis/go-redis/pull/3706)) by [@mwhooker](https://github.com/mwhooker)
- **Typed NOSCRIPT error**: Redis `NOSCRIPT` replies are now surfaced as a typed error for easier handling ([#3738](https://github.com/redis/go-redis/pull/3738)) by [@LINKIWI](https://github.com/LINKIWI)
- **PubSub ClientSetName**: Added `ClientSetName` method to `PubSub` ([#3727](https://github.com/redis/go-redis/pull/3727)) by [@Flack74](https://github.com/Flack74)
- **ReplicaOf**: New `ReplicaOf` method replaces the deprecated `SlaveOf` ([#3720](https://github.com/redis/go-redis/pull/3720)) by [@Copilot](https://github.com/apps/copilot-swe-agent)
- **HSCAN BinaryUnmarshaler**: `HScan` now supports types implementing `encoding.BinaryUnmarshaler` ([#3768](https://github.com/redis/go-redis/pull/3768)) by [@Aaditya-dubey1](https://github.com/Aaditya-dubey1)

## 🐛 Bug Fixes

- **Auto hostname type detection**: Improved endpoint type detection for maintenance notifications using DNS-based classification; handles empty hosts and expanded private-IP ranges ([#3789](https://github.com/redis/go-redis/pull/3789)) by [@ndyakov](https://github.com/ndyakov)
- **HELLO fallback**: Don't send `CLIENT MAINT_NOTIFICATIONS` handshake when `HELLO` fails and connection falls back to RESP2; fail fast when explicitly enabled with RESP3 ([#3788](https://github.com/redis/go-redis/pull/3788)) by [@ndyakov](https://github.com/ndyakov)
- **Dial TCP retry**: `ShouldRetry` now treats `net.OpError` with `Op == "dial"` timeout errors as safe to retry since no command was sent ([#3787](https://github.com/redis/go-redis/pull/3787)) by [@vladisa88](https://github.com/vladisa88)
- **wrappedOnClose leak**: Fixed resource leak caused by repeatedly wrapping `baseClient` close logic; replaced with a bounded, concurrency-safe named-hook registry ([#3785](https://github.com/redis/go-redis/pull/3785)) by [@ndyakov](https://github.com/ndyakov)
- **Pool Close() on stale connections**: Suppress close errors (e.g., TLS `closeNotify` timeouts) for connections already dropped by the server due to idle timeout ([#3778](https://github.com/redis/go-redis/pull/3778)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **FIFO waiter ordering**: Fixed race in `ConnStateMachine.notifyWaiters` that could wake multiple waiters under a single mutex hold and violate FIFO ordering ([#3777](https://github.com/redis/go-redis/pull/3777)) by [@0x48core](https://github.com/0x48core)
- **Lua READONLY detection**: Detect `READONLY` errors embedded in Lua script error messages on read-only replicas so commands are correctly retried ([#3769](https://github.com/redis/go-redis/pull/3769)) by [@zhengjilei](https://github.com/zhengjilei)
- **VectorScoreSliceCmd RESP2**: Fixed `VSimWithScores`, `VSimWithArgsWithScores`, and `VLinksWithScores` which were broken on RESP2 connections returning flat arrays instead of maps ([#3767](https://github.com/redis/go-redis/pull/3767)) by [@Copilot](https://github.com/apps/copilot-swe-agent)
- **Closed connection handling**: Two fixes for closed connection handling in the pool ([#3764](https://github.com/redis/go-redis/pull/3764)) by [@cxljs](https://github.com/cxljs)
- **ZRangeArgs Rev**: Fixed `ZRangeArgs` with `Rev` + `ByScore`/`ByLex` incorrectly swapping `Start`/`Stop`, breaking `ZRANGESTORE` ([#3751](https://github.com/redis/go-redis/pull/3751)) by [@Copilot](https://github.com/apps/copilot-swe-agent)
- **OTel metric instrument types**: Fixed metric instrument types in `redisotel-native` ([#3743](https://github.com/redis/go-redis/pull/3743)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **Options.clone() data race**: Fixed data race when cloning `Options` ([#3739](https://github.com/redis/go-redis/pull/3739)) by [@rubensayshi](https://github.com/rubensayshi)
- **Connection closure metrics**: Fixed connection closure metrics and enabled all metric groups by default in `redisotel-native` ([#3735](https://github.com/redis/go-redis/pull/3735)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **OTel semconv v1.38.0**: Use metric definition from `otel/semconv/v1.38.0` in `redisotel-native` ([#3731](https://github.com/redis/go-redis/pull/3731)) by [@wzy9607](https://github.com/wzy9607)
- **SETNX semantics**: Use `SET ... NX` instead of the deprecated `SETNX` command ([#3723](https://github.com/redis/go-redis/pull/3723)) by [@ndyakov](https://github.com/ndyakov)
- **TIME keyless routing**: Mark `TIME` as a keyless command for correct cluster routing ([#3722](https://github.com/redis/go-redis/pull/3722)) by [@fatal10110](https://github.com/fatal10110)
- **Dial timeout per retry**: Dial timeout now applies per attempt instead of across all retry attempts combined ([#3705](https://github.com/redis/go-redis/pull/3705)) by [@mwhooker](https://github.com/mwhooker)
- **Cluster metrics attributes**: Fixed `pool.name` being appended per node, which corrupted and dropped user-provided custom attributes ([#3699](https://github.com/redis/go-redis/pull/3699)) by [@Jesse-Bonfire](https://github.com/Jesse-Bonfire)
- **initConn nil dereference**: Fixed nil pointer dereference and potential deadlock in `*baseClient.initConn()`; added explicit nil option guards to client constructors ([#3676](https://github.com/redis/go-redis/pull/3676)) by [@olde-ducke](https://github.com/olde-ducke)

## ⚡ Performance

- **RESP reader**: Optimized RESP reader by eliminating intermediate string allocations ([#3774](https://github.com/redis/go-redis/pull/3774)) by [@Aaditya-dubey1](https://github.com/Aaditya-dubey1)
- **Inline rendezvous hashing**: Replaced `github.com/dgryski/go-rendezvous` dependency with an in-repo implementation in `internal/hashtag`, reducing the dependency graph while preserving algorithm parity ([#3762](https://github.com/redis/go-redis/pull/3762)) by [@bigsk05](https://github.com/bigsk05)

## 🧪 Testing & Infrastructure

- **Release automation**: Added `repository`, `ref`, and `client-libs-test-image-tag` inputs to the `run-tests` composite action; `redis-version` is now optional so unstable builds use `REDIS_VERSION` from the Makefile ([#3749](https://github.com/redis/go-redis/pull/3749)) by [@dariaguy](https://github.com/dariaguy)
- **Go 1.24**: Updated minimum Go version to 1.24 and use `-compat=1.24` in release scripts ([#3714](https://github.com/redis/go-redis/pull/3714), [#3754](https://github.com/redis/go-redis/pull/3754)) by [@ndyakov](https://github.com/ndyakov), [@cxljs](https://github.com/cxljs)

## 🧰 Maintenance

- **Pool state machine**: Removed redundant `Conn.closed` atomic field in favor of the state machine's `StateClosed` ([#3783](https://github.com/redis/go-redis/pull/3783)) by [@cxljs](https://github.com/cxljs)
- **OTel SDK**: Updated OpenTelemetry SDK dependencies in `redisotel`/`redisotel-native` ([#3770](https://github.com/redis/go-redis/pull/3770)) by [@ndyakov](https://github.com/ndyakov)
- **Go 1.21+ built-ins**: Use `maps.Keys`, `slices.Collect`, `slices.Contains`, `clear()`, and `slices.SortFunc` instead of custom helpers ([#3758](https://github.com/redis/go-redis/pull/3758), [#3746](https://github.com/redis/go-redis/pull/3746)) by [@cxljs](https://github.com/cxljs)
- **HGetAll docs**: Added Go doc comment to `HGetAll` describing behavior and complexity ([#3776](https://github.com/redis/go-redis/pull/3776)) by [@0x48core](https://github.com/0x48core)
- **Docs links**: Fixed irrelevant docs links ([#3724](https://github.com/redis/go-redis/pull/3724)) by [@olzhas-sabiyev](https://github.com/olzhas-sabiyev)
- **Examples cleanup**: Removed throughput binary from examples ([#3733](https://github.com/redis/go-redis/pull/3733)) by [@ndyakov](https://github.com/ndyakov)

## 👥 Contributors

We'd like to thank all the contributors who worked on this release!

[@0x48core](https://github.com/0x48core), [@Aaditya-dubey1](https://github.com/Aaditya-dubey1), [@Copilot](https://github.com/apps/copilot-swe-agent), [@Flack74](https://github.com/Flack74), [@Jesse-Bonfire](https://github.com/Jesse-Bonfire), [@LINKIWI](https://github.com/LINKIWI), [@bigsk05](https://github.com/bigsk05), [@chaitanyabodlapati](https://github.com/chaitanyabodlapati), [@cxljs](https://github.com/cxljs), [@dariaguy](https://github.com/dariaguy), [@fatal10110](https://github.com/fatal10110), [@mwhooker](https://github.com/mwhooker), [@ndyakov](https://github.com/ndyakov), [@ofekshenawa](https://github.com/ofekshenawa), [@olde-ducke](https://github.com/olde-ducke), [@olzhas-sabiyev](https://github.com/olzhas-sabiyev), [@romanpovol](https://github.com/romanpovol), [@rubensayshi](https://github.com/rubensayshi), [@vladisa88](https://github.com/vladisa88), [@wzy9607](https://github.com/wzy9607), [@zhengjilei](https://github.com/zhengjilei)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.18.0...v9.19.0

# 9.18.0 (2026-02-16)

## 🚀 Highlights

### Redis 8.6 Support

Added support for Redis 8.6, including new commands and features for streams idempotent production and HOTKEYS.

### Smart Client Handoff (Maintenance Notifications) for Cluster

This release introduces comprehensive support for Redis Cluster maintenance notifications via SMIGRATING/SMIGRATED push notifications. The client now automatically handles slot migrations by:
- **Relaxing timeouts during migration** (SMIGRATING) to prevent false failures
- **Triggering lazy cluster state reloads** upon completion (SMIGRATED)
- Enabling seamless operations during Redis Enterprise maintenance windows

([#3643](https://github.com/redis/go-redis/pull/3643)) by [@ndyakov](https://github.com/ndyakov)

### OpenTelemetry Native Metrics Support

Added comprehensive OpenTelemetry metrics support following the [OpenTelemetry Database Client Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/database/database-metrics/). The implementation uses a Bridge Pattern to keep the core library dependency-free while providing optional metrics instrumentation through the new `extra/redisotel-native` package.

**Metric groups include:**
- Command metrics: Operation duration with retry tracking
- Connection basic: Connection count and creation time
- Resiliency: Errors, handoffs, timeout relaxation
- Connection advanced: Wait time and use time
- Pubsub metrics: Published and received messages
- Stream metrics: Processing duration and maintenance notifications

([#3637](https://github.com/redis/go-redis/pull/3637)) by [@ofekshenawa](https://github.com/ofekshenawa)

## ✨ New Features

- **HOTKEYS Commands**: Added support for Redis HOTKEYS feature for identifying hot keys based on CPU consumption and network utilization ([#3695](https://github.com/redis/go-redis/pull/3695)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **Streams Idempotent Production**: Added support for Redis 8.6+ Streams Idempotent Production with `ProducerID`, `IdempotentID`, `IdempotentAuto` in `XAddArgs` and new `XCFGSET` command ([#3693](https://github.com/redis/go-redis/pull/3693)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **NaN Values for TimeSeries**: Added support for NaN (Not a Number) values in Redis time series commands ([#3687](https://github.com/redis/go-redis/pull/3687)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **DialerRetries Options**: Added `DialerRetries` and `DialerRetryTimeout` to `ClusterOptions`, `RingOptions`, and `FailoverOptions` ([#3686](https://github.com/redis/go-redis/pull/3686)) by [@naveenchander30](https://github.com/naveenchander30)
- **ConnMaxLifetimeJitter**: Added jitter configuration to distribute connection expiration times and prevent thundering herd ([#3666](https://github.com/redis/go-redis/pull/3666)) by [@cyningsun](https://github.com/cyningsun)
- **Digest Helper Functions**: Added `DigestString` and `DigestBytes` helper functions for client-side xxh3 hashing compatible with Redis DIGEST command ([#3679](https://github.com/redis/go-redis/pull/3679)) by [@ofekshenawa](https://github.com/ofekshenawa)
- **SMIGRATED New Format**: Updated SMIGRATED parser to support new format and remember original host:port ([#3697](https://github.com/redis/go-redis/pull/3697)) by [@ndyakov](https://github.com/ndyakov)
- **Cluster State Reload Interval**: Added cluster state reload interval option for maintenance notifications ([#3663](https://github.com/redis/go-redis/pull/3663)) by [@ndyakov](https://github.com/ndyakov)

## 🐛 Bug Fixes

- **PubSub nil pointer dereference**: Fixed nil pointer dereference in PubSub after `WithTimeout()` - `pubSubPool` is now properly cloned ([#3710](https://github.com/redis/go-redis/pull/3710)) by [@Copilot](https://github.com/apps/copilot-swe-agent)
- **MaintNotificationsConfig nil check**: Guard against nil `MaintNotificationsConfig` in `initConn` ([#3707](https://github.com/redis/go-redis/pull/3707)) by [@veeceey](https://github.com/veeceey)
- **wantConnQueue zombie elements**: Fixed zombie `wantConn` elements accumulation in `wantConnQueue` ([#3680](https://github.com/redis/go-redis/pull/3680)) by [@cyningsun](https://github.com/cyningsun)
- **XADD/XTRIM approx flag**: Fixed XADD and XTRIM to use `=` when approx is false ([#3684](https://github.com/redis/go-redis/pull/3684)) by [@ndyakov](https://github.com/ndyakov)
- **Sentinel timeout retry**: When connection to a sentinel times out, attempt to connect to other sentinels ([#3654](https://github.com/redis/go-redis/pull/3654)) by [@cxljs](https://github.com/cxljs)

## ⚡ Performance

- **Fuzz test optimization**: Eliminated repeated string conversions, used functional approach for cleaner operation selection ([#3692](https://github.com/redis/go-redis/pull/3692)) by [@feiguoL](https://github.com/feiguoL)
- **Pre-allocate capacity**: Pre-allocate slice capacity to prevent multiple capacity expansions ([#3689](https://github.com/redis/go-redis/pull/3689)) by [@feelshu](https://github.com/feelshu)

## 🧪 Testing

- **Comprehensive TLS tests**: Added comprehensive TLS tests and example for standalone, cluster, and certificate authentication ([#3681](https://github.com/redis/go-redis/pull/3681)) by [@ndyakov](https://github.com/ndyakov)
- **Redis 8.6**: Updated CI to use Redis 8.6-pre ([#3685](https://github.com/redis/go-redis/pull/3685)) by [@ndyakov](https://github.com/ndyakov)

## 🧰 Maintenance

- **Deprecation warnings**: Added deprecation warnings for commands based on Redis documentation ([#3673](https://github.com/redis/go-redis/pull/3673)) by [@ndyakov](https://github.com/ndyakov)
- **Use errors.Join()**: Replaced custom error join function with standard library `errors.Join()` ([#3653](https://github.com/redis/go-redis/pull/3653)) by [@cxljs](https://github.com/cxljs)
- **Use Go 1.21 min/max**: Use Go 1.21's built-in min/max functions ([#3656](https://github.com/redis/go-redis/pull/3656)) by [@cxljs](https://github.com/cxljs)
- **Proper formatting**: Code formatting improvements ([#3670](https://github.com/redis/go-redis/pull/3670)) by [@12ya](https://github.com/12ya)
- **Set commands documentation**: Added comprehensive documentation to all set command methods ([#3642](https://github.com/redis/go-redis/pull/3642)) by [@iamamirsalehi](https://github.com/iamamirsalehi)
- **MaxActiveConns docs**: Added default value documentation for `MaxActiveConns` ([#3674](https://github.com/redis/go-redis/pull/3674)) by [@codykaup](https://github.com/codykaup)
- **README example update**: Updated README example ([#3657](https://github.com/redis/go-redis/pull/3657)) by [@cxljs](https://github.com/cxljs)
- **Cluster maintnotif example**: Added example application for cluster maintenance notifications ([#3651](https://github.com/redis/go-redis/pull/3651)) by [@ndyakov](https://github.com/ndyakov)

## 👥 Contributors

We'd like to thank all the contributors who worked on this release!

[@12ya](https://github.com/12ya), [@Copilot](https://github.com/apps/copilot-swe-agent), [@codykaup](https://github.com/codykaup), [@cxljs](https://github.com/cxljs), [@cyningsun](https://github.com/cyningsun), [@feelshu](https://github.com/feelshu), [@feiguoL](https://github.com/feiguoL), [@iamamirsalehi](https://github.com/iamamirsalehi), [@naveenchander30](https://github.com/naveenchander30), [@ndyakov](https://github.com/ndyakov), [@ofekshenawa](https://github.com/ofekshenawa), [@veeceey](https://github.com/veeceey)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.17.0...v9.18.0

# 9.18.0-beta.2 (2025-12-09)

## 🚀 Highlights

### Go Version Update

This release updates the minimum required Go version to 1.21. This is part of a gradual migration strategy where the minimum supported Go version will be three versions behind the latest release. With each new Go version release, we will bump the minimum version by one, ensuring compatibility while staying current with the Go ecosystem.

### Stability Improvements

This release includes several important stability fixes:
- Fixed a critical panic in the handoff worker manager that could occur when handling nil errors
- Improved test reliability for Smart Client Handoff functionality
- Fixed logging format issues that could cause runtime errors

## ✨ New Features

- OpenTelemetry metrics improvements for nil response handling ([#3638](https://github.com/redis/go-redis/pull/3638)) by [@fengve](https://github.com/fengve)

## 🐛 Bug Fixes

- Fixed panic on nil error in handoffWorkerManager closeConnFromRequest ([#3633](https://github.com/redis/go-redis/pull/3633)) by [@ccoVeille](https://github.com/ccoVeille)
- Fixed bad sprintf syntax in logging ([#3632](https://github.com/redis/go-redis/pull/3632)) by [@ccoVeille](https://github.com/ccoVeille)

## 🧰 Maintenance

- Updated minimum Go version to 1.21 ([#3640](https://github.com/redis/go-redis/pull/3640)) by [@ndyakov](https://github.com/ndyakov)
- Use Go 1.20 idiomatic string<->byte conversion ([#3435](https://github.com/redis/go-redis/pull/3435)) by [@justinhwang](https://github.com/justinhwang)
- Reduce flakiness of Smart Client Handoff test ([#3641](https://github.com/redis/go-redis/pull/3641)) by [@kiryazovi-redis](https://github.com/kiryazovi-redis)
- Revert PR #3634 (Observability metrics phase1) ([#3635](https://github.com/redis/go-redis/pull/3635)) by [@ofekshenawa](https://github.com/ofekshenawa)

## 👥 Contributors

We'd like to thank all the contributors who worked on this release!

[@justinhwang](https://github.com/justinhwang), [@ndyakov](https://github.com/ndyakov), [@kiryazovi-redis](https://github.com/kiryazovi-redis), [@fengve](https://github.com/fengve), [@ccoVeille](https://github.com/ccoVeille), [@ofekshenawa](https://github.com/ofekshenawa)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.18.0-beta.1...v9.18.0-beta.2

# 9.18.0-beta.1 (2025-12-01)

## 🚀 Highlights

### Request and Response Policy Based Routing in Cluster Mode

This beta release introduces comprehensive support for Redis COMMAND-based request and response policy routing for cluster clients. This feature enables intelligent command routing and response aggregation based on Redis command metadata.

**Key Features:**
- **Command Policy Loader**: Automatically parses and caches COMMAND metadata with routing/aggregation hints
- **Enhanced Routing Engine**: Supports all request policies including:
  - `default(keyless)` - Commands without keys
  - `default(hashslot)` - Commands with hash slot routing
  - `all_shards` - Commands that need to run on all shards
  - `all_nodes` - Commands that need to run on all nodes
  - `multi_shard` - Commands that span multiple shards
  - `special` - Commands with custom routing logic
- **Response Aggregator**: Intelligently combines multi-shard replies based on response policies:
  - `all_succeeded` - All shards must succeed
  - `one_succeeded` - At least one shard must succeed
  - `agg_sum` - Aggregate numeric responses
  - `special` - Custom aggregation logic (e.g., FT.CURSOR)
- **Raw Command Support**: Policies are enforced on `Client.Do(ctx, args...)`

This feature is particularly useful for Redis Stack commands like RediSearch that need to operate across multiple shards in a cluster.

### Connection Pool Improvements

Fixed a critical defect in the connection pool's turn management mechanism that could lead to connection leaks under certain conditions. The fix ensures proper 1:1 correspondence between turns and connections.

## ✨ New Features

- Request and Response Policy Based Routing in Cluster Mode ([#3422](https://github.com/redis/go-redis/pull/3422)) by [@ofekshenawa](https://github.com/ofekshenawa)

## 🐛 Bug Fixes

- Fixed connection pool turn management to prevent connection leaks ([#3626](https://github.com/redis/go-redis/pull/3626)) by [@cyningsun](https://github.com/cyningsun)

## 🧰 Maintenance

- chore(deps): bump rojopolis/spellcheck-github-actions from 0.54.0 to 0.55.0 ([#3627](https://github.com/redis/go-redis/pull/3627))

## 👥 Contributors

We'd like to thank all the contributors who worked on this release!

[@cyningsun](https://github.com/cyningsun), [@ofekshenawa](https://github.com/ofekshenawa), [@ndyakov](https://github.com/ndyakov)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.17.1...v9.18.0-beta.1

# 9.17.1 (2025-11-25)

## 🐛 Bug Fixes

- add wait to keyless commands list ([#3615](https://github.com/redis/go-redis/pull/3615)) by [@marcoferrer](https://github.com/marcoferrer)
- fix(time): remove cached time optimization ([#3611](https://github.com/redis/go-redis/pull/3611)) by [@ndyakov](https://github.com/ndyakov)

## 🧰 Maintenance

- chore(deps): bump golangci/golangci-lint-action from 9.0.0 to 9.1.0 ([#3609](https://github.com/redis/go-redis/pull/3609))
- chore(deps): bump actions/checkout from 5 to 6 ([#3610](https://github.com/redis/go-redis/pull/3610))
- chore(script): fix help call in tag.sh ([#3606](https://github.com/redis/go-redis/pull/3606)) by [@ndyakov](https://github.com/ndyakov)

## Contributors
We'd like to thank all the contributors who worked on this release!

[@marcoferrer](https://github.com/marcoferrer) and [@ndyakov](https://github.com/ndyakov)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.17.0...v9.17.1

# 9.17.0 (2025-11-19)

## 🚀 Highlights

### Redis 8.4 Support
Added support for Redis 8.4, including new commands and features ([#3572](https://github.com/redis/go-redis/pull/3572))

### Typed Errors
Introduced typed errors for better error handling using `errors.As` instead of string checks. Errors can now be wrapped and set to commands in hooks without breaking library functionality ([#3602](https://github.com/redis/go-redis/pull/3602))

### New Commands
- **CAS/CAD Commands**: Added support for Compare-And-Set/Compare-And-Delete operations with conditional matching (`IFEQ`, `IFNE`, `IFDEQ`, `IFDNE`) ([#3583](https://github.com/redis/go-redis/pull/3583), [#3595](https://github.com/redis/go-redis/pull/3595))
- **MSETEX**: Atomically set multiple key-value pairs with expiration options and conditional modes ([#3580](https://github.com/redis/go-redis/pull/3580))
- **XReadGroup CLAIM**: Consume both incoming and idle pending entries from streams in a single call ([#3578](https://github.com/redis/go-redis/pull/3578))
- **ACL Commands**: Added `ACLGenPass`, `ACLUsers`, and `ACLWhoAmI` ([#3576](https://github.com/redis/go-redis/pull/3576))
- **SLOWLOG Commands**: Added `SLOWLOG LEN` and `SLOWLOG RESET` ([#3585](https://github.com/redis/go-redis/pull/3585))
- **LATENCY Commands**: Added `LATENCY LATEST` and `LATENCY RESET` ([#3584](https://github.com/redis/go-redis/pull/3584))

### Search & Vector Improvements
- **Hybrid Search**: Added  **EXPERIMENTAL** support for the new `FT.HYBRID` command ([#3573](https://github.com/redis/go-redis/pull/3573))
- **Vector Range**: Added `VRANGE` command for vector sets ([#3543](https://github.com/redis/go-redis/pull/3543))
- **FT.INFO Enhancements**: Added vector-specific attributes in FT.INFO response ([#3596](https://github.com/redis/go-redis/pull/3596))

### Connection Pool Improvements
- **Improved Connection Success Rate**: Implemented FIFO queue-based fairness and context pattern for connection creation to prevent premature cancellation under high concurrency ([#3518](https://github.com/redis/go-redis/pull/3518))
- **Connection State Machine**: Resolved race conditions and improved pool performance with proper state tracking ([#3559](https://github.com/redis/go-redis/pull/3559))
- **Pool Performance**: Significant performance improvements with faster semaphores, lockless hook manager, and reduced allocations (47-67% faster Get/Put operations) ([#3565](https://github.com/redis/go-redis/pull/3565))

### Metrics & Observability
- **Canceled Metric Attribute**: Added 'canceled' metrics attribute to distinguish context cancellation errors from other errors ([#3566](https://github.com/redis/go-redis/pull/3566))

## ✨ New Features

- Typed errors with wrapping support ([#3602](https://github.com/redis/go-redis/pull/3602)) by [@ndyakov](https://github.com/ndyakov)
- CAS/CAD commands (marked as experimental) ([#3583](https://github.com/redis/go-redis/pull/3583), [#3595](https://github.com/redis/go-redis/pull/3595)) by [@ndyakov](https://github.com/ndyakov), [@htemelski-redis](https://github.com/htemelski-redis)
- MSETEX command support ([#3580](https://github.com/redis/go-redis/pull/3580)) by [@ofekshenawa](https://github.com/ofekshenawa)
- XReadGroup CLAIM argument ([#3578](https://github.com/redis/go-redis/pull/3578)) by [@ofekshenawa](https://github.com/ofekshenawa)
- ACL commands: GenPass, Users, WhoAmI ([#3576](https://github.com/redis/go-redis/pull/3576)) by [@destinyoooo](https://github.com/destinyoooo)
- SLOWLOG commands: LEN, RESET ([#3585](https://github.com/redis/go-redis/pull/3585)) by [@destinyoooo](https://github.com/destinyoooo)
- LATENCY commands: LATEST, RESET ([#3584](https://github.com/redis/go-redis/pull/3584)) by [@destinyoooo](https://github.com/destinyoooo)
- Hybrid search command (FT.HYBRID) ([#3573](https://github.com/redis/go-redis/pull/3573)) by [@htemelski-redis](https://github.com/htemelski-redis)
- Vector range command (VRANGE) ([#3543](https://github.com/redis/go-redis/pull/3543)) by [@cxljs](https://github.com/cxljs)
- Vector-specific attributes in FT.INFO ([#3596](https://github.com/redis/go-redis/pull/3596)) by [@ndyakov](https://github.com/ndyakov)
- Improved connection pool success rate with FIFO queue ([#3518](https://github.com/redis/go-redis/pull/3518)) by [@cyningsun](https://github.com/cyningsun)
- Canceled metrics attribute for context errors ([#3566](https://github.com/redis/go-redis/pull/3566)) by [@pvragov](https://github.com/pvragov)

## 🐛 Bug Fixes

- Fixed Failover Client MaintNotificationsConfig ([#3600](https://github.com/redis/go-redis/pull/3600)) by [@ajax16384](https://github.com/ajax16384)
- Fixed ACLGenPass function to use the bit parameter ([#3597](https://github.com/redis/go-redis/pull/3597)) by [@destinyoooo](https://github.com/destinyoooo)
- Return error instead of panic from commands ([#3568](https://github.com/redis/go-redis/pull/3568)) by [@dragneelfps](https://github.com/dragneelfps)
- Safety harness in `joinErrors` to prevent panic ([#3577](https://github.com/redis/go-redis/pull/3577)) by [@manisharma](https://github.com/manisharma)

## ⚡ Performance

- Connection state machine with race condition fixes ([#3559](https://github.com/redis/go-redis/pull/3559)) by [@ndyakov](https://github.com/ndyakov)
- Pool performance improvements: 47-67% faster Get/Put, 33% less memory, 50% fewer allocations ([#3565](https://github.com/redis/go-redis/pull/3565)) by [@ndyakov](https://github.com/ndyakov)

## 🧪 Testing & Infrastructure

- Updated to Redis 8.4.0 image ([#3603](https://github.com/redis/go-redis/pull/3603)) by [@ndyakov](https://github.com/ndyakov)
- Added Redis 8.4-RC1-pre to CI ([#3572](https://github.com/redis/go-redis/pull/3572)) by [@ndyakov](https://github.com/ndyakov)
- Refactored tests for idiomatic Go ([#3561](https://github.com/redis/go-redis/pull/3561), [#3562](https://github.com/redis/go-redis/pull/3562), [#3563](https://github.com/redis/go-redis/pull/3563)) by [@12ya](https://github.com/12ya)

## 👥 Contributors

We'd like to thank all the contributors who worked on this release!

[@12ya](https://github.com/12ya), [@ajax16384](https://github.com/ajax16384), [@cxljs](https://github.com/cxljs), [@cyningsun](https://github.com/cyningsun), [@destinyoooo](https://github.com/destinyoooo), [@dragneelfps](https://github.com/dragneelfps), [@htemelski-redis](https://github.com/htemelski-redis), [@manisharma](https://github.com/manisharma), [@ndyakov](https://github.com/ndyakov), [@ofekshenawa](https://github.com/ofekshenawa), [@pvragov](https://github.com/pvragov)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.16.0...v9.17.0

# 9.16.0 (2025-10-23)

## 🚀 Highlights

### Maintenance Notifications Support

This release introduces comprehensive support for Redis maintenance notifications, enabling applications to handle server maintenance events gracefully. The new `maintnotifications` package provides:

- **RESP3 Push Notifications**: Full support for Redis RESP3 protocol push notifications
- **Connection Handoff**: Automatic connection migration during server maintenance with configurable retry policies and circuit breakers
- **Graceful Degradation**: Configurable timeout relaxation during maintenance windows to prevent false failures
- **Event-Driven Architecture**: Background workers with on-demand scaling for efficient handoff processing
- **Production-Ready**: Comprehensive E2E testing framework and monitoring capabilities

For detailed usage examples and configuration options, see the [maintenance notifications documentation](maintnotifications/README.md).

## ✨ New Features

- **Trace Filtering**: Add support for filtering traces for specific commands, including pipeline operations and dial operations ([#3519](https://github.com/redis/go-redis/pull/3519), [#3550](https://github.com/redis/go-redis/pull/3550))
  - New `TraceCmdFilter` option to selectively trace commands
  - Reduces overhead by excluding high-frequency or low-value commands from traces

## 🐛 Bug Fixes

- **Pipeline Error Handling**: Fix issue where pipeline repeatedly sets the same error ([#3525](https://github.com/redis/go-redis/pull/3525))
- **Connection Pool**: Ensure re-authentication does not interfere with connection handoff operations ([#3547](https://github.com/redis/go-redis/pull/3547))

## 🔧 Improvements

- **Hash Commands**: Update hash command implementations ([#3523](https://github.com/redis/go-redis/pull/3523))
- **OpenTelemetry**: Use `metric.WithAttributeSet` to avoid unnecessary attribute copying in redisotel ([#3552](https://github.com/redis/go-redis/pull/3552))

## 📚 Documentation

- **Cluster Client**: Add explanation for why `MaxRetries` is disabled for `ClusterClient` ([#3551](https://github.com/redis/go-redis/pull/3551))

## 🧪 Testing & Infrastructure

- **E2E Testing**: Upgrade E2E testing framework with improved reliability and coverage ([#3541](https://github.com/redis/go-redis/pull/3541))
- **Release Process**: Improved resiliency of the release process ([#3530](https://github.com/redis/go-redis/pull/3530))

## 📦 Dependencies

- Bump `rojopolis/spellcheck-github-actions` from 0.51.0 to 0.52.0 ([#3520](https://github.com/redis/go-redis/pull/3520))
- Bump `github/codeql-action` from 3 to 4 ([#3544](https://github.com/redis/go-redis/pull/3544))

## 👥 Contributors

We'd like to thank all the contributors who worked on this release!

[@ndyakov](https://github.com/ndyakov), [@htemelski-redis](https://github.com/htemelski-redis), [@Sovietaced](https://github.com/Sovietaced), [@Udhayarajan](https://github.com/Udhayarajan), [@boekkooi-impossiblecloud](https://github.com/boekkooi-impossiblecloud), [@Pika-Gopher](https://github.com/Pika-Gopher), [@cxljs](https://github.com/cxljs), [@huiyifyj](https://github.com/huiyifyj), [@omid-h70](https://github.com/omid-h70)

---

**Full Changelog**: https://github.com/redis/go-redis/compare/v9.14.0...v9.16.0


# 9.15.0 was accidentally released. Please use version 9.16.0 instead.

# 9.15.0-beta.3 (2025-09-26)

## Highlights
This beta release includes a pre-production version of processing push notifications and hitless upgrades.

# Changes

- chore: Update hash_commands.go ([#3523](https://github.com/redis/go-redis/pull/3523))

## 🚀 New Features

- feat: RESP3 notifications support & Hitless notifications handling ([#3418](https://github.com/redis/go-redis/pull/3418))

## 🐛 Bug Fixes

- fix: pipeline repeatedly sets the error ([#3525](https://github.com/redis/go-redis/pull/3525))

## 🧰 Maintenance

- chore(deps): bump rojopolis/spellcheck-github-actions from 0.51.0 to 0.52.0 ([#3520](https://github.com/redis/go-redis/pull/3520))
- feat(e2e-testing): maintnotifications e2e and refactor ([#3526](https://github.com/redis/go-redis/pull/3526))
- feat(tag.sh): Improved resiliency of the release process ([#3530](https://github.com/redis/go-redis/pull/3530))

## Contributors
We'd like to thank all the contributors who worked on this release!

[@cxljs](https://github.com/cxljs), [@ndyakov](https://github.com/ndyakov), [@htemelski-redis](https://github.com/htemelski-redis), and [@omid-h70](https://github.com/omid-h70)


# 9.15.0-beta.1 (2025-09-10)

## Highlights
This beta release includes a pre-production version of processing push notifications and hitless upgrades.

### Hitless Upgrades
Hitless upgrades is a major new feature that allows for zero-downtime upgrades in Redis clusters.
You can find more information in the [Hitless Upgrades documentation](https://github.com/redis/go-redis/tree/master/hitless).

# Changes

## 🚀 New Features
- [CAE-1088] & [CAE-1072] feat: RESP3 notifications support & Hitless notifications handling ([#3418](https://github.com/redis/go-redis/pull/3418))

## Contributors
We'd like to thank all the contributors who worked on this release!

[@ndyakov](https://github.com/ndyakov), [@htemelski-redis](https://github.com/htemelski-redis), [@ofekshenawa](https://github.com/ofekshenawa)


# 9.14.0 (2025-09-10)

## Highlights
- Added batch process method to the pipeline ([#3510](https://github.com/redis/go-redis/pull/3510))

# Changes

## 🚀 New Features

- Added batch process method to the pipeline ([#3510](https://github.com/redis/go-redis/pull/3510))

## 🐛 Bug Fixes

- fix: SetErr on Cmd if the command cannot be queued correctly in multi/exec ([#3509](https://github.com/redis/go-redis/pull/3509))

## 🧰 Maintenance

- Updates release drafter config to exclude dependabot ([#3511](https://github.com/redis/go-redis/pull/3511))
- chore(deps): bump actions/setup-go from 5 to 6 ([#3504](https://github.com/redis/go-redis/pull/3504))

## Contributors
We'd like to thank all the contributors who worked on this release!

[@elena-kolevska](https://github.com/elena-kolevksa), [@htemelski-redis](https://github.com/htemelski-redis) and [@ndyakov](https://github.com/ndyakov)


# 9.13.0 (2025-09-03)

## Highlights
- Pipeliner expose queued commands ([#3496](https://github.com/redis/go-redis/pull/3496))
- Ensure that JSON.GET returns Nil response ([#3470](https://github.com/redis/go-redis/pull/3470))
- Fixes on Read and Write buffer sizes and UniversalOptions

## Changes
- Pipeliner expose queued commands ([#3496](https://github.com/redis/go-redis/pull/3496))
- fix(test): fix a timing issue in pubsub test ([#3498](https://github.com/redis/go-redis/pull/3498))
- Allow users to enable read-write splitting in failover mode. ([#3482](https://github.com/redis/go-redis/pull/3482))
- Set the read/write buffer size of the sentinel client to 4KiB ([#3476](https://github.com/redis/go-redis/pull/3476))

## 🚀 New Features

- fix(otel): register wait metrics ([#3499](https://github.com/redis/go-redis/pull/3499))
- Support subscriptions against cluster slave nodes ([#3480](https://github.com/redis/go-redis/pull/3480))
- Add wait metrics to otel ([#3493](https://github.com/redis/go-redis/pull/3493))
- Clean failing timeout implementation ([#3472](https://github.com/redis/go-redis/pull/3472))

## 🐛 Bug Fixes

- Do not assume that all non-IP hosts are loopbacks ([#3085](https://github.com/redis/go-redis/pull/3085))
- Ensure that JSON.GET returns Nil response ([#3470](https://github.com/redis/go-redis/pull/3470))

## 🧰 Maintenance

- fix(otel): register wait metrics ([#3499](https://github.com/redis/go-redis/pull/3499))
- fix(make test): Add default env in makefile ([#3491](https://github.com/redis/go-redis/pull/3491))
- Update the introduction to running tests in README.md ([#3495](https://github.com/redis/go-redis/pull/3495))
- test: Add comprehensive edge case tests for IncrByFloat command ([#3477](https://github.com/redis/go-redis/pull/3477))
- Set the default read/write buffer size of Redis connection to 32KiB ([#3483](https://github.com/redis/go-redis/pull/3483))
- Bumps test image to 8.2.1-pre ([#3478](https://github.com/redis/go-redis/pull/3478))
- fix UniversalOptions miss ReadBufferSize and WriteBufferSize options ([#3485](https://github.com/redis/go-redis/pull/3485))
- chore(deps): bump actions/checkout from 4 to 5 ([#3484](https://github.com/redis/go-redis/pull/3484))
- Removes dry run for stale issues policy ([#3471](https://github.com/redis/go-redis/pull/3471))
- Update otel metrics URL ([#3474](https://github.com/redis/go-redis/pull/3474))

## Contributors
We'd like to thank all the contributors who worked on this release!

[@LINKIWI](https://github.com/LINKIWI), [@cxljs](https://github.com/cxljs), [@cybersmeashish](https://github.com/cybersmeashish), [@elena-kolevska](https://github.com/elena-kolevska), [@htemelski-redis](https://github.com/htemelski-redis), [@mwhooker](https://github.com/mwhooker), [@ndyakov](https://github.com/ndyakov), [@ofekshenawa](https://github.com/ofekshenawa), [@suever](https://github.com/suever)


# 9.12.1 (2025-08-11)
## 🚀 Highlights
In the last version (9.12.0) the client introduced bigger write and read buffer sized. The default value we set was 512KiB.
However, users reported that this is too big for most use cases and can lead to high memory usage.
In this version the default value is changed to 256KiB. The `README.md` was updated to reflect the
correct default value and include a note that the default value can be changed.

## 🐛 Bug Fixes

- fix(options): Add buffer sizes to failover. Update README ([#3468](https://github.com/redis/go-redis/pull/3468))

## 🧰 Maintenance

- fix(options): Add buffer sizes to failover. Update README ([#3468](https://github.com/redis/go-redis/pull/3468))
- chore: update & fix otel example ([#3466](https://github.com/redis/go-redis/pull/3466))

## Contributors
We'd like to thank all the contributors who worked on this release!

[@ndyakov](https://github.com/ndyakov) and [@vmihailenco](https://github.com/vmihailenco)

# 9.12.0 (2025-08-05)

## 🚀 Highlights

- This release includes support for [Redis 8.2](https://redis.io/docs/latest/operate/oss_and_stack/stack-with-enterprise/release-notes/redisce/redisos-8.2-release-notes/).
- Introduces an experimental Query Builders for `FTSearch`, `FTAggregate` and other search commands.
- Adds support for `EPSILON` option in `FT.VSIM`.
- Includes bug fixes and improvements contributed by the community related to ring and [redisotel](https://github.com/redis/go-redis/tree/master/extra/redisotel).

## Changes
- Improve stale issue workflow ([#3458](https://github.com/redis/go-redis/pull/3458))
- chore(ci): Add 8.2 rc2 pre build for CI ([#3459](https://github.com/redis/go-redis/pull/3459))
- Added new stream commands ([#3450](https://github.com/redis/go-redis/pull/3450))
- feat: Add "skip_verify" to Sentinel ([#3428](https://github.com/redis/go-redis/pull/3428))
- fix: `errors.Join` requires Go 1.20 or later ([#3442](https://github.com/redis/go-redis/pull/3442))
- DOC-4344 document quickstart examples ([#3426](https://github.com/redis/go-redis/pull/3426))
- feat(bitop): add support for the new bitop operations ([#3409](https://github.com/redis/go-redis/pull/3409))

## 🚀 New Features

- feat: recover addIdleConn may occur panic ([#2445](https://github.com/redis/go-redis/pull/2445))
- feat(ring): specify custom health check func via HeartbeatFn option ([#2940](https://github.com/redis/go-redis/pull/2940))
- Add Query Builder for RediSearch commands ([#3436](https://github.com/redis/go-redis/pull/3436))
- add configurable buffer sizes for Redis connections ([#3453](https://github.com/redis/go-redis/pull/3453))
- Add VAMANA vector type to RediSearch ([#3449](https://github.com/redis/go-redis/pull/3449))
- VSIM add `EPSILON` option ([#3454](https://github.com/redis/go-redis/pull/3454))
- Add closing support to otel metrics instrumentation ([#3444](https://github.com/redis/go-redis/pull/3444))

## 🐛 Bug Fixes

- fix(redisotel): fix buggy append in reportPoolStats ([#3122](https://github.com/redis/go-redis/pull/3122))
- fix(search): return results even if doc is empty ([#3457](https://github.com/redis/go-redis/pull/3457))
- [ISSUE-3402]: Ring.Pipelined return dial timeout error ([#3403](https://github.com/redis/go-redis/pull/3403))

## 🧰 Maintenance

- Merges stale issues jobs into one job with two steps ([#3463](https://github.com/redis/go-redis/pull/3463))
- improve code readability ([#3446](https://github.com/redis/go-redis/pull/3446))
- chore(release): 9.12.0-beta.1 ([#3460](https://github.com/redis/go-redis/pull/3460))
- DOC-5472 time series doc examples ([#3443](https://github.com/redis/go-redis/pull/3443))
- Add VAMANA compression algorithm tests ([#3461](https://github.com/redis/go-redis/pull/3461))
- bumped redis 8.2 version used in the CI/CD ([#3451](https://github.com/redis/go-redis/pull/3451))

## Contributors
We'd like to thank all the contributors who worked on this release!

[@andy-stark-redis](https://github.com/andy-stark-redis), [@cxljs](https://github.com/cxljs), [@elena-kolevska](https://github.com/elena-kolevska), [@htemelski-redis](https://github.com/htemelski-redis), [@jouir](https://github.com/jouir), [@monkey92t](https://github.com/monkey92t), [@ndyakov](https://github.com/ndyakov), [@ofekshenawa](https://github.com/ofekshenawa), [@rokn](https://github.com/rokn), [@smnvdev](https://github.com/smnvdev), [@strobil](https://github.com/strobil) and [@wzy9607](https://github.com/wzy9607)

## New Contributors
* [@htemelski-redis](https://github.com/htemelski-redis) made their first contribution in [#3409](https://github.com/redis/go-redis/pull/3409)
* [@smnvdev](https://github.com/smnvdev) made their first contribution in [#3403](https://github.com/redis/go-redis/pull/3403)
* [@rokn](https://github.com/rokn) made their first contribution in [#3444](https://github.com/redis/go-redis/pull/3444)

# 9.11.0 (2025-06-24)

## 🚀 Highlights

Fixes TxPipeline to work correctly in cluster scenarios, allowing execution of commands
only in the same slot.

# Changes

## 🚀 New Features

- Set cluster slot for `scan` commands, rather than random ([#2623](https://github.com/redis/go-redis/pull/2623))
- Add CredentialsProvider field to UniversalOptions ([#2927](https://github.com/redis/go-redis/pull/2927))
- feat(redisotel): add WithCallerEnabled option ([#3415](https://github.com/redis/go-redis/pull/3415))

## 🐛 Bug Fixes

- fix(txpipeline): keyless commands should take the slot of the keyed ([#3411](https://github.com/redis/go-redis/pull/3411))
- fix(loading): cache the loaded flag for slave nodes ([#3410](https://github.com/redis/go-redis/pull/3410))
- fix(txpipeline): should return error on multi/exec on multiple slots ([#3408](https://github.com/redis/go-redis/pull/3408))
- fix: check if the shard exists to avoid returning nil ([#3396](https://github.com/redis/go-redis/pull/3396))

## 🧰 Maintenance

- feat: optimize connection pool waitTurn ([#3412](https://github.com/redis/go-redis/pull/3412))
- chore(ci): update CI redis builds ([#3407](https://github.com/redis/go-redis/pull/3407))
- chore: remove a redundant method from `Ring`, `Client` and `ClusterClient` ([#3401](https://github.com/redis/go-redis/pull/3401))
- test: refactor TestBasicCredentials using table-driven tests ([#3406](https://github.com/redis/go-redis/pull/3406))
- perf: reduce unnecessary memory allocation operations ([#3399](https://github.com/redis/go-redis/pull/3399))
- fix: insert entry during iterating over a map ([#3398](https://github.com/redis/go-redis/pull/3398))
- DOC-5229 probabilistic data type examples ([#3413](https://github.com/redis/go-redis/pull/3413))
- chore(deps): bump rojopolis/spellcheck-github-actions from 0.49.0 to 0.51.0 ([#3414](https://github.com/redis/go-redis/pull/3414))

## Contributors
We'd like to thank all the contributors who worked on this release!

[@andy-stark-redis](https://github.com/andy-stark-redis), [@boekkooi-impossiblecloud](https://github.com/boekkooi-impossiblecloud), [@cxljs](https://github.com/cxljs), [@dcherubini](https://github.com/dcherubini), [@dependabot[bot]](https://github.com/apps/dependabot), [@iamamirsalehi](https://github.com/iamamirsalehi), [@ndyakov](https://github.com/ndyakov), [@pete-woods](https://github.com/pete-woods), [@twz915](https://github.com/twz915) and [dependabot[bot]](https://github.com/apps/dependabot)

# 9.10.0 (2025-06-06)

## 🚀 Highlights

`go-redis` now supports [vector sets](https://redis.io/docs/latest/develop/data-types/vector-sets/). This data type is marked
as "in preview" in Redis and its support in `go-redis` is marked as experimental. You can find examples in the documentation and
in the `doctests` folder.

# Changes

## 🚀 New Features

- feat: support vectorset ([#3375](https://github.com/redis/go-redis/pull/3375))

## 🧰 Maintenance

- Add the missing NewFloatSliceResult for testing ([#3393](https://github.com/redis/go-redis/pull/3393))
- DOC-5078 vector set examples ([#3394](https://github.com/redis/go-redis/pull/3394))

## Contributors
We'd like to thank all the contributors who worked on this release!

[@AndBobsYourUncle](https://github.com/AndBobsYourUncle), [@andy-stark-redis](https://github.com/andy-stark-redis), [@fukua95](https://github.com/fukua95) and [@ndyakov](https://github.com/ndyakov)



# 9.9.0 (2025-05-27)

## 🚀 Highlights
- **Token-based Authentication**: Added `StreamingCredentialsProvider` for dynamic credential updates (experimental)
  - Can be used with [go-redis-entraid](https://github.com/redis/go-redis-entraid) for Azure AD authentication
- **Connection Statistics**: Added connection waiting statistics for better monitoring
- **Failover Improvements**: Added `ParseFailoverURL` for easier failover configuration
- **Ring Client Enhancements**: Added shard access methods for better Pub/Sub management

## ✨ New Features
- Added `StreamingCredentialsProvider` for token-based authentication ([#3320](https://github.com/redis/go-redis/pull/3320))
  - Supports dynamic credential updates
  - Includes connection close hooks
  - Note: Currently marked as experimental
- Added `ParseFailoverURL` for parsing failover URLs ([#3362](https://github.com/redis/go-redis/pull/3362))
- Added connection waiting statistics ([#2804](https://github.com/redis/go-redis/pull/2804))
- Added new utility functions:
  - `ParseFloat` and `MustParseFloat` in public utils package ([#3371](https://github.com/redis/go-redis/pull/3371))
  - Unit tests for `Atoi`, `ParseInt`, `ParseUint`, and `ParseFloat` ([#3377](https://github.com/redis/go-redis/pull/3377))
- Added Ring client shard access methods:
  - `GetShardClients()` to retrieve all active shard clients
  - `GetShardClientForKey(key string)` to get the shard client for a specific key ([#3388](https://github.com/redis/go-redis/pull/3388))

## 🐛 Bug Fixes
- Fixed routing reads to loading slave nodes ([#3370](https://github.com/redis/go-redis/pull/3370))
- Added support for nil lag in XINFO GROUPS ([#3369](https://github.com/redis/go-redis/pull/3369))
- Fixed pool acquisition timeout issues ([#3381](https://github.com/redis/go-redis/pull/3381))
- Optimized unnecessary copy operations ([#3376](https://github.com/redis/go-redis/pull/3376))

## 📚 Documentation
- Updated documentation for XINFO GROUPS with nil lag support ([#3369](https://github.com/redis/go-redis/pull/3369))
- Added package-level comments for new features

## ⚡ Performance and Reliability
- Optimized `ReplaceSpaces` function ([#3383](https://github.com/redis/go-redis/pull/3383))
- Set default value for `Options.Protocol` in `init()` ([#3387](https://github.com/redis/go-redis/pull/3387))
- Exported pool errors for public consumption ([#3380](https://github.com/redis/go-redis/pull/3380))

## 🔧 Dependencies and Infrastructure
- Updated Redis CI to version 8.0.1 ([#3372](https://github.com/redis/go-redis/pull/3372))
- Updated spellcheck GitHub Actions ([#3389](https://github.com/redis/go-redis/pull/3389))
- Removed unused parameters ([#3382](https://github.com/redis/go-redis/pull/3382), [#3384](https://github.com/redis/go-redis/pull/3384))

## 🧪 Testing
- Added unit tests for pool acquisition timeout ([#3381](https://github.com/redis/go-redis/pull/3381))
- Added unit tests for utility functions ([#3377](https://github.com/redis/go-redis/pull/3377))

## 👥 Contributors

We would like to thank all the contributors who made this release possible:

[@ndyakov](https://github.com/ndyakov), [@ofekshenawa](https://github.com/ofekshenawa), [@LINKIWI](https://github.com/LINKIWI), [@iamamirsalehi](https://github.com/iamamirsalehi), [@fukua95](https://github.com/fukua95), [@lzakharov](https://github.com/lzakharov), [@DengY11](https://github.com/DengY11)

## 📝 Changelog

For a complete list of changes, see the [full changelog](https://github.com/redis/go-redis/compare/v9.8.0...v9.9.0).

# 9.8.0 (2025-04-30)

## 🚀 Highlights
- **Redis 8 Support**: Full compatibility with Redis 8.0, including testing and CI integration
- **Enhanced Hash Operations**: Added support for new hash commands (`HGETDEL`, `HGETEX`, `HSETEX`) and `HSTRLEN` command
- **Search Improvements**: Enabled Search DIALECT 2 by default and added `CountOnly` argument for `FT.Search`

## ✨ New Features
- Added support for new hash commands: `HGETDEL`, `HGETEX`, `HSETEX` ([#3305](https://github.com/redis/go-redis/pull/3305))
- Added `HSTRLEN` command for hash operations ([#2843](https://github.com/redis/go-redis/pull/2843))
- Added `Do` method for raw query by single connection from `pool.Conn()` ([#3182](https://github.com/redis/go-redis/pull/3182))
- Prevent false-positive marshaling by treating zero time.Time as empty in isEmptyValue ([#3273](https://github.com/redis/go-redis/pull/3273))
- Added FailoverClusterClient support for Universal client ([#2794](https://github.com/redis/go-redis/pull/2794))
- Added support for cluster mode with `IsClusterMode` config parameter ([#3255](https://github.com/redis/go-redis/pull/3255))
- Added client name support in `HELLO` RESP handshake ([#3294](https://github.com/redis/go-redis/pull/3294))
- **Enabled Search DIALECT 2 by default** ([#3213](https://github.com/redis/go-redis/pull/3213))
- Added read-only option for failover configurations ([#3281](https://github.com/redis/go-redis/pull/3281))
- Added `CountOnly` argument for `FT.Search` to use `LIMIT 0 0` ([#3338](https://github.com/redis/go-redis/pull/3338))
- Added `DB` option support in `NewFailoverClusterClient` ([#3342](https://github.com/redis/go-redis/pull/3342))
- Added `nil` check for the options when creating a client ([#3363](https://github.com/redis/go-redis/pull/3363))

## 🐛 Bug Fixes
- Fixed `PubSub` concurrency safety issues ([#3360](https://github.com/redis/go-redis/pull/3360))
- Fixed panic caused when argument is `nil` ([#3353](https://github.com/redis/go-redis/pull/3353))
- Improved error handling when fetching master node from sentinels ([#3349](https://github.com/redis/go-redis/pull/3349))
- Fixed connection pool timeout issues and increased retries ([#3298](https://github.com/redis/go-redis/pull/3298))
- Fixed context cancellation error leading to connection spikes on Primary instances ([#3190](https://github.com/redis/go-redis/pull/3190))
- Fixed RedisCluster client to consider `MASTERDOWN` a retriable error ([#3164](https://github.com/redis/go-redis/pull/3164))
- Fixed tracing to show complete commands instead of truncated versions ([#3290](https://github.com/redis/go-redis/pull/3290))
- Fixed OpenTelemetry instrumentation to prevent multiple span reporting ([#3168](https://github.com/redis/go-redis/pull/3168))
- Fixed `FT.Search` Limit argument and added `CountOnly` argument for limit 0 0 ([#3338](https://github.com/redis/go-redis/pull/3338))
- Fixed missing command in interface ([#3344](https://github.com/redis/go-redis/pull/3344))
- Fixed slot calculation for `COUNTKEYSINSLOT` command ([#3327](https://github.com/redis/go-redis/pull/3327))
- Updated PubSub implementation with correct context ([#3329](https://github.com/redis/go-redis/pull/3329))

## 📚 Documentation
- Added hash search examples ([#3357](https://github.com/redis/go-redis/pull/3357))
- Fixed documentation comments ([#3351](https://github.com/redis/go-redis/pull/3351))
- Added `CountOnly` search example ([#3345](https://github.com/redis/go-redis/pull/3345))
- Added examples for list commands: `LLEN`, `LPOP`, `LPUSH`, `LRANGE`, `RPOP`, `RPUSH` ([#3234](https://github.com/redis/go-redis/pull/3234))
- Added `SADD` and `SMEMBERS` command examples ([#3242](https://github.com/redis/go-redis/pull/3242))
- Updated `README.md` to use Redis Discord guild ([#3331](https://github.com/redis/go-redis/pull/3331))
- Updated `HExpire` command documentation ([#3355](https://github.com/redis/go-redis/pull/3355))
- Featured OpenTelemetry instrumentation more prominently ([#3316](https://github.com/redis/go-redis/pull/3316))
- Updated `README.md` with additional information ([#310ce55](https://github.com/redis/go-redis/commit/310ce55))

## ⚡ Performance and Reliability
- Bound connection pool background dials to configured dial timeout ([#3089](https://github.com/redis/go-redis/pull/3089))
- Ensured context isn't exhausted via concurrent query ([#3334](https://github.com/redis/go-redis/pull/3334))

## 🔧 Dependencies and Infrastructure
- Updated testing image to Redis 8.0-RC2 ([#3361](https://github.com/redis/go-redis/pull/3361))
- Enabled CI for Redis CE 8.0 ([#3274](https://github.com/redis/go-redis/pull/3274))
- Updated various dependencies:
  - Bumped golangci/golangci-lint-action from 6.5.0 to 7.0.0 ([#3354](https://github.com/redis/go-redis/pull/3354))
  - Bumped rojopolis/spellcheck-github-actions ([#3336](https://github.com/redis/go-redis/pull/3336))
  - Bumped golang.org/x/net in example/otel ([#3308](https://github.com/redis/go-redis/pull/3308))
- Migrated golangci-lint configuration to v2 format ([#3354](https://github.com/redis/go-redis/pull/3354))

## ⚠️ Breaking Changes
- **Enabled Search DIALECT 2 by default** ([#3213](https://github.com/redis/go-redis/pull/3213))
- Dropped RedisGears (Triggers and Functions) support ([#3321](https://github.com/redis/go-redis/pull/3321))
- Dropped FT.PROFILE command that was never enabled ([#3323](https://github.com/redis/go-redis/pull/3323))

## 🔒 Security
- Fixed network error handling on SETINFO (CVE-2025-29923) ([#3295](https://github.com/redis/go-redis/pull/3295))

## 🧪 Testing
- Added integration tests for Redis 8 behavior changes in Redis Search ([#3337](https://github.com/redis/go-redis/pull/3337))
- Added vector types INT8 and UINT8 tests ([#3299](https://github.com/redis/go-redis/pull/3299))
- Added test codes for search_commands.go ([#3285](https://github.com/redis/go-redis/pull/3285))
- Fixed example test sorting ([#3292](https://github.com/redis/go-redis/pull/3292))

## 👥 Contributors

We would like to thank all the contributors who made this release possible:

[@alexander-menshchikov](https://github.com/alexander-menshchikov), [@EXPEbdodla](https://github.com/EXPEbdodla), [@afti](https://github.com/afti), [@dmaier-redislabs](https://github.com/dmaier-redislabs), [@four_leaf_clover](https://github.com/four_leaf_clover), [@alohaglenn](https://github.com/alohaglenn), [@gh73962](https://github.com/gh73962), [@justinmir](https://github.com/justinmir), [@LINKIWI](https://github.com/LINKIWI), [@liushuangbill](https://github.com/liushuangbill), [@golang88](https://github.com/golang88), [@gnpaone](https://github.com/gnpaone), [@ndyakov](https://github.com/ndyakov), [@nikolaydubina](https://github.com/nikolaydubina), [@oleglacto](https://github.com/oleglacto), [@andy-stark-redis](https://github.com/andy-stark-redis), [@rodneyosodo](https://github.com/rodneyosodo), [@dependabot](https://github.com/dependabot), [@rfyiamcool](https://github.com/rfyiamcool), [@frankxjkuang](https://github.com/frankxjkuang), [@fukua95](https://github.com/fukua95), [@soleymani-milad](https://github.com/soleymani-milad), [@ofekshenawa](https://github.com/ofekshenawa), [@khasanovbi](https://github.com/khasanovbi)


# Old Changelog
## Unreleased

### Changed

* `go-redis` won't skip span creation if the parent spans is not recording. ([#2980](https://github.com/redis/go-redis/issues/2980))
  Users can use the OpenTelemetry sampler to control the sampling behavior.
  For instance, you can use the `ParentBased(NeverSample())` sampler from `go.opentelemetry.io/otel/sdk/trace` to keep
  a similar behavior (drop orphan spans) of `go-redis` as before.

## [9.0.5](https://github.com/redis/go-redis/compare/v9.0.4...v9.0.5) (2023-05-29)


### Features

* Add ACL LOG ([#2536](https://github.com/redis/go-redis/issues/2536)) ([31ba855](https://github.com/redis/go-redis/commit/31ba855ddebc38fbcc69a75d9d4fb769417cf602))
* add field protocol to setupClusterQueryParams ([#2600](https://github.com/redis/go-redis/issues/2600)) ([840c25c](https://github.com/redis/go-redis/commit/840c25cb6f320501886a82a5e75f47b491e46fbe))
* add protocol option ([#2598](https://github.com/redis/go-redis/issues/2598)) ([3917988](https://github.com/redis/go-redis/commit/391798880cfb915c4660f6c3ba63e0c1a459e2af))



## [9.0.4](https://github.com/redis/go-redis/compare/v9.0.3...v9.0.4) (2023-05-01)


### Bug Fixes

* reader float parser ([#2513](https://github.com/redis/go-redis/issues/2513)) ([46f2450](https://github.com/redis/go-redis/commit/46f245075e6e3a8bd8471f9ca67ea95fd675e241))


### Features

* add client info command ([#2483](https://github.com/redis/go-redis/issues/2483)) ([b8c7317](https://github.com/redis/go-redis/commit/b8c7317cc6af444603731f7017c602347c0ba61e))
* no longer verify HELLO error messages ([#2515](https://github.com/redis/go-redis/issues/2515)) ([7b4f217](https://github.com/redis/go-redis/commit/7b4f2179cb5dba3d3c6b0c6f10db52b837c912c8))
* read the structure to increase the judgment of the omitempty op… ([#2529](https://github.com/redis/go-redis/issues/2529)) ([37c057b](https://github.com/redis/go-redis/commit/37c057b8e597c5e8a0e372337f6a8ad27f6030af))



## [9.0.3](https://github.com/redis/go-redis/compare/v9.0.2...v9.0.3) (2023-04-02)

### New Features

- feat(scan): scan time.Time sets the default decoding (#2413)
- Add support for CLUSTER LINKS command (#2504)
- Add support for acl dryrun command (#2502)
- Add support for COMMAND GETKEYS & COMMAND GETKEYSANDFLAGS (#2500)
- Add support for LCS Command (#2480)
- Add support for BZMPOP (#2456)
- Adding support for ZMPOP command (#2408)
- Add support for LMPOP (#2440)
- feat: remove pool unused fields (#2438)
- Expiretime and PExpireTime (#2426)
- Implement `FUNCTION` group of commands (#2475)
- feat(zadd): add ZAddLT and ZAddGT (#2429)
- Add: Support for COMMAND LIST command (#2491)
- Add support for BLMPOP (#2442)
- feat: check pipeline.Do to prevent confusion with Exec (#2517)
- Function stats, function kill, fcall and fcall_ro (#2486)
- feat: Add support for CLUSTER SHARDS command (#2507)
- feat(cmd): support for adding byte,bit parameters to the bitpos command (#2498)

### Fixed

- fix: eval api cmd.SetFirstKeyPos (#2501)
- fix: limit the number of connections created (#2441)
- fixed #2462  v9 continue support dragonfly,  it's Hello command return "NOAUTH Authentication required" error (#2479)
- Fix for internal/hscan/structmap.go:89:23: undefined: reflect.Pointer (#2458)
- fix: group lag can be null (#2448)

### Maintenance

- Updating to the latest version of redis (#2508)
- Allowing for running tests on a port other than the fixed 6380 (#2466)
- redis 7.0.8 in tests (#2450)
- docs: Update redisotel example for v9 (#2425)
- chore: update go mod, Upgrade golang.org/x/net version to 0.7.0 (#2476)
- chore: add Chinese translation (#2436)
- chore(deps): bump github.com/bsm/gomega from 1.20.0 to 1.26.0 (#2421)
- chore(deps): bump github.com/bsm/ginkgo/v2 from 2.5.0 to 2.7.0 (#2420)
- chore(deps): bump actions/setup-go from 3 to 4 (#2495)
- docs: add instructions for the HSet api (#2503)
- docs: add reading lag field comment (#2451)
- test: update go mod before testing(go mod tidy) (#2423)
- docs: fix comment typo (#2505)
- test: remove testify (#2463)
- refactor: change ListElementCmd to KeyValuesCmd. (#2443)
- fix(appendArg): appendArg case special type (#2489)

## [9.0.2](https://github.com/redis/go-redis/compare/v9.0.1...v9.0.2) (2023-02-01)

### Features

* upgrade OpenTelemetry, use the new metrics API. ([#2410](https://github.com/redis/go-redis/issues/2410)) ([e29e42c](https://github.com/redis/go-redis/commit/e29e42cde2755ab910d04185025dc43ce6f59c65))

## v9 2023-01-30

### Breaking

- Changed Pipelines to not be thread-safe any more.

### Added

- Added support for [RESP3](https://github.com/antirez/RESP3/blob/master/spec.md) protocol. It was
  contributed by @monkey92t who has done the majority of work in this release.
- Added `ContextTimeoutEnabled` option that controls whether the client respects context timeouts
  and deadlines. See
  [Redis Timeouts](https://redis.uptrace.dev/guide/go-redis-debugging.html#timeouts) for details.
- Added `ParseClusterURL` to parse URLs into `ClusterOptions`, for example,
  `redis://user:password@localhost:6789?dial_timeout=3&read_timeout=6s&addr=localhost:6790&addr=localhost:6791`.
- Added metrics instrumentation using `redisotel.IstrumentMetrics`. See
  [documentation](https://redis.uptrace.dev/guide/go-redis-monitoring.html)
- Added `redis.HasErrorPrefix` to help working with errors.

### Changed

- Removed asynchronous cancellation based on the context timeout. It was racy in v8 and is
  completely gone in v9.
- Reworked hook interface and added `DialHook`.
- Replaced `redisotel.NewTracingHook` with `redisotel.InstrumentTracing`. See
  [example](example/otel) and
  [documentation](https://redis.uptrace.dev/guide/go-redis-monitoring.html).
- Replaced `*redis.Z` with `redis.Z` since it is small enough to be passed as value without making
  an allocation.
- Renamed the option `MaxConnAge` to `ConnMaxLifetime`.
- Renamed the option `IdleTimeout` to `ConnMaxIdleTime`.
- Removed connection reaper in favor of `MaxIdleConns`.
- Removed `WithContext` since `context.Context` can be passed directly as an arg.
- Removed `Pipeline.Close` since there is no real need to explicitly manage pipeline resources and
  it can be safely reused via `sync.Pool` etc. `Pipeline.Discard` is still available if you want to
  reset commands for some reason.

### Fixed

- Improved and fixed pipeline retries.
- As usually, added support for more commands and fixed some bugs.
