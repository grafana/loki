# Release Notes

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
