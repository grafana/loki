## [8.11.4](https://github.com/go-redis/redis/compare/v8.11.3...v8.11.4) (2021-10-04)


### Features

* add acl auth support for sentinels ([f66582f](https://github.com/go-redis/redis/commit/f66582f44f3dc3a4705a5260f982043fde4aa634))
* add Cmd.{String,Int,Float,Bool}Slice helpers and an example ([5d3d293](https://github.com/go-redis/redis/commit/5d3d293cc9c60b90871e2420602001463708ce24))
* add SetVal method for each command ([168981d](https://github.com/go-redis/redis/commit/168981da2d84ee9e07d15d3e74d738c162e264c4))



## v8.11

- Remove OpenTelemetry metrics.
- Supports more redis commands and options.

## v8.10

- Removed extra OpenTelemetry spans from go-redis core. Now go-redis instrumentation only adds a
  single span with a Redis command (instead of 4 spans). There are multiple reasons behind this
  decision:

  - Traces become smaller and less noisy.
  - It may be costly to process those 3 extra spans for each query.
  - go-redis no longer depends on OpenTelemetry.

  Eventually we hope to replace the information that we no longer collect with OpenTelemetry
  Metrics.

## v8.9

- Changed `PubSub.Channel` to only rely on `Ping` result. You can now use `WithChannelSize`,
  `WithChannelHealthCheckInterval`, and `WithChannelSendTimeout` to override default settings.

## v8.8

- To make updating easier, extra modules now have the same version as go-redis does. That means that
  you need to update your imports:

```
github.com/go-redis/redis/extra/redisotel -> github.com/go-redis/redis/extra/redisotel/v8
github.com/go-redis/redis/extra/rediscensus -> github.com/go-redis/redis/extra/rediscensus/v8
```

## v8.5

- [knadh](https://github.com/knadh) contributed long-awaited ability to scan Redis Hash into a
  struct:

```go
err := rdb.HGetAll(ctx, "hash").Scan(&data)

err := rdb.MGet(ctx, "key1", "key2").Scan(&data)
```

- Please check [redismock](https://github.com/go-redis/redismock) by
  [monkey92t](https://github.com/monkey92t) if you are looking for mocking Redis Client.

## v8

- All commands require `context.Context` as a first argument, e.g. `rdb.Ping(ctx)`. If you are not
  using `context.Context` yet, the simplest option is to define global package variable
  `var ctx = context.TODO()` and use it when `ctx` is required.

- Full support for `context.Context` canceling.

- Added `redis.NewFailoverClusterClient` that supports routing read-only commands to a slave node.

- Added `redisext.OpenTemetryHook` that adds
  [Redis OpenTelemetry instrumentation](https://redis.uptrace.dev/tracing/).

- Redis slow log support.

- Ring uses Rendezvous Hashing by default which provides better distribution. You need to move
  existing keys to a new location or keys will be inaccessible / lost. To use old hashing scheme:

```go
import "github.com/golang/groupcache/consistenthash"

ring := redis.NewRing(&redis.RingOptions{
    NewConsistentHash: func() {
        return consistenthash.New(100, crc32.ChecksumIEEE)
    },
})
```

- `ClusterOptions.MaxRedirects` default value is changed from 8 to 3.
- `Options.MaxRetries` default value is changed from 0 to 3.

- `Cluster.ForEachNode` is renamed to `ForEachShard` for consistency with `Ring`.

## v7.3

- New option `Options.Username` which causes client to use `AuthACL`. Be aware if your connection
  URL contains username.

## v7.2

- Existing `HMSet` is renamed to `HSet` and old deprecated `HMSet` is restored for Redis 3 users.

## v7.1

- Existing `Cmd.String` is renamed to `Cmd.Text`. New `Cmd.String` implements `fmt.Stringer`
  interface.

## v7

- _Important_. Tx.Pipeline now returns a non-transactional pipeline. Use Tx.TxPipeline for a
  transactional pipeline.
- WrapProcess is replaced with more convenient AddHook that has access to context.Context.
- WithContext now can not be used to create a shallow copy of the client.
- New methods ProcessContext, DoContext, and ExecContext.
- Client respects Context.Deadline when setting net.Conn deadline.
- Client listens on Context.Done while waiting for a connection from the pool and returns an error
  when context context is cancelled.
- Add PubSub.ChannelWithSubscriptions that sends `*Subscription` in addition to `*Message` to allow
  detecting reconnections.
- `time.Time` is now marshalled in RFC3339 format. `rdb.Get("foo").Time()` helper is added to parse
  the time.
- `SetLimiter` is removed and added `Options.Limiter` instead.
- `HMSet` is deprecated as of Redis v4.

## v6.15

- Cluster and Ring pipelines process commands for each node in its own goroutine.

## 6.14

- Added Options.MinIdleConns.
- Added Options.MaxConnAge.
- PoolStats.FreeConns is renamed to PoolStats.IdleConns.
- Add Client.Do to simplify creating custom commands.
- Add Cmd.String, Cmd.Int, Cmd.Int64, Cmd.Uint64, Cmd.Float64, and Cmd.Bool helpers.
- Lower memory usage.

## v6.13

- Ring got new options called `HashReplicas` and `Hash`. It is recommended to set
  `HashReplicas = 1000` for better keys distribution between shards.
- Cluster client was optimized to use much less memory when reloading cluster state.
- PubSub.ReceiveMessage is re-worked to not use ReceiveTimeout so it does not lose data when timeout
  occurres. In most cases it is recommended to use PubSub.Channel instead.
- Dialer.KeepAlive is set to 5 minutes by default.

## v6.12

- ClusterClient got new option called `ClusterSlots` which allows to build cluster of normal Redis
  Servers that don't have cluster mode enabled. See
  https://godoc.org/github.com/go-redis/redis#example-NewClusterClient--ManualSetup
