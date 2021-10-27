# Miniredis

Pure Go Redis test server, used in Go unittests.


##

Sometimes you want to test code which uses Redis, without making it a full-blown
integration test.
Miniredis implements (parts of) the Redis server, to be used in unittests. It
enables a simple, cheap, in-memory, Redis replacement, with a real TCP interface. Think of it as the Redis version of `net/http/httptest`.

It saves you from using mock code, and since the redis server lives in the
test process you can query for values directly, without going through the server
stack.

There are no dependencies on external binaries, so you can easily integrate it in automated build processes.

Be sure to import v2:
```
import "github.com/alicebob/miniredis/v2"
```

## Commands

Implemented commands:

 - Connection (complete)
   - AUTH -- see RequireAuth()
   - ECHO
   - HELLO -- see RequireUserAuth()
   - PING
   - SELECT
   - SWAPDB
   - QUIT
 - Key
   - DEL
   - EXISTS
   - EXPIRE
   - EXPIREAT
   - KEYS
   - MOVE
   - PERSIST
   - PEXPIRE
   - PEXPIREAT
   - PTTL
   - RENAME
   - RENAMENX
   - RANDOMKEY -- see m.Seed(...)
   - SCAN
   - TOUCH
   - TTL
   - TYPE
   - UNLINK
 - Transactions (complete)
   - DISCARD
   - EXEC
   - MULTI
   - UNWATCH
   - WATCH
 - Server
   - DBSIZE
   - FLUSHALL
   - FLUSHDB
   - TIME -- returns time.Now() or value set by SetTime()
 - String keys (complete)
   - APPEND
   - BITCOUNT
   - BITOP
   - BITPOS
   - DECR
   - DECRBY
   - GET
   - GETBIT
   - GETRANGE
   - GETSET
   - INCR
   - INCRBY
   - INCRBYFLOAT
   - MGET
   - MSET
   - MSETNX
   - PSETEX
   - SET
   - SETBIT
   - SETEX
   - SETNX
   - SETRANGE
   - STRLEN
 - Hash keys (complete)
   - HDEL
   - HEXISTS
   - HGET
   - HGETALL
   - HINCRBY
   - HINCRBYFLOAT
   - HKEYS
   - HLEN
   - HMGET
   - HMSET
   - HSET
   - HSETNX
   - HSTRLEN
   - HVALS
   - HSCAN
 - List keys (complete)
   - BLPOP
   - BRPOP
   - BRPOPLPUSH
   - LINDEX
   - LINSERT
   - LLEN
   - LPOP
   - LPUSH
   - LPUSHX
   - LRANGE
   - LREM
   - LSET
   - LTRIM
   - RPOP
   - RPOPLPUSH
   - RPUSH
   - RPUSHX
 - Pub/Sub (complete)
   - PSUBSCRIBE
   - PUBLISH
   - PUBSUB
   - PUNSUBSCRIBE
   - SUBSCRIBE
   - UNSUBSCRIBE
 - Set keys (complete)
   - SADD
   - SCARD
   - SDIFF
   - SDIFFSTORE
   - SINTER
   - SINTERSTORE
   - SISMEMBER
   - SMEMBERS
   - SMOVE
   - SPOP -- see m.Seed(...)
   - SRANDMEMBER -- see m.Seed(...)
   - SREM
   - SUNION
   - SUNIONSTORE
   - SSCAN
 - Sorted Set keys (complete)
   - ZADD
   - ZCARD
   - ZCOUNT
   - ZINCRBY
   - ZINTERSTORE
   - ZLEXCOUNT
   - ZPOPMIN
   - ZPOPMAX
   - ZRANGE
   - ZRANGEBYLEX
   - ZRANGEBYSCORE
   - ZRANK
   - ZREM
   - ZREMRANGEBYLEX
   - ZREMRANGEBYRANK
   - ZREMRANGEBYSCORE
   - ZREVRANGE
   - ZREVRANGEBYLEX
   - ZREVRANGEBYSCORE
   - ZREVRANK
   - ZSCORE
   - ZUNIONSTORE
   - ZSCAN
 - Stream keys
   - XACK
   - XADD
   - XDEL
   - XGROUP CREATE
   - XINFO STREAM -- partly
   - XLEN
   - XRANGE
   - XREAD -- partly
   - XREADGROUP -- partly
   - XREVRANGE
 - Scripting
   - EVAL
   - EVALSHA
   - SCRIPT LOAD
   - SCRIPT EXISTS
   - SCRIPT FLUSH
 - GEO
   - GEOADD
   - GEODIST
   - ~~GEOHASH~~
   - GEOPOS
   - GEORADIUS
   - GEORADIUS_RO
   - GEORADIUSBYMEMBER
   - GEORADIUSBYMEMBER_RO
 - Server
   - COMMAND -- partly
 - Cluster
   - CLUSTER SLOTS
   - CLUSTER KEYSLOT
   - CLUSTER NODES


## TTLs, key expiration, and time

Since miniredis is intended to be used in unittests TTLs don't decrease
automatically. You can use `TTL()` to get the TTL (as a time.Duration) of a
key. It will return 0 when no TTL is set.

`m.FastForward(d)` can be used to decrement all TTLs. All TTLs which become <=
0 will be removed.

EXPIREAT and PEXPIREAT values will be
converted to a duration. For that you can either set m.SetTime(t) to use that
time as the base for the (P)EXPIREAT conversion, or don't call SetTime(), in
which case time.Now() will be used.

SetTime() also sets the value returned by TIME, which defaults to time.Now().
It is not updated by FastForward, only by SetTime.

## Randomness and Seed()

Miniredis will use `math/rand`'s global RNG for randomness unless a seed is
provided by calling `m.Seed(...)`. If a seed is provided, then miniredis will
use its own RNG based on that seed.

Commands which use randomness are: RANDOMKEY, SPOP, and SRANDMEMBER.

## Example

``` Go

import (
    ...
    "github.com/alicebob/miniredis/v2"
    ...
)

func TestSomething(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	defer s.Close()

	// Optionally set some keys your code expects:
	s.Set("foo", "bar")
	s.HSet("some", "other", "key")

	// Run your code and see if it behaves.
	// An example using the redigo library from "github.com/gomodule/redigo/redis":
	c, err := redis.Dial("tcp", s.Addr())
	_, err = c.Do("SET", "foo", "bar")

	// Optionally check values in redis...
	if got, err := s.Get("foo"); err != nil || got != "bar" {
		t.Error("'foo' has the wrong value")
	}
	// ... or use a helper for that:
	s.CheckGet(t, "foo", "bar")

	// TTL and expiration:
	s.Set("foo", "bar")
	s.SetTTL("foo", 10*time.Second)
	s.FastForward(11 * time.Second)
	if s.Exists("foo") {
		t.Fatal("'foo' should not have existed anymore")
	}
}
```

## Not supported

Commands which will probably not be implemented:

 - CLUSTER (all)
    - ~~CLUSTER *~~
    - ~~READONLY~~
    - ~~READWRITE~~
 - HyperLogLog (all) -- unless someone needs these
    - ~~PFADD~~
    - ~~PFCOUNT~~
    - ~~PFMERGE~~
 - Key
    - ~~DUMP~~
    - ~~MIGRATE~~
    - ~~OBJECT~~
    - ~~RESTORE~~
    - ~~WAIT~~
 - Scripting
    - ~~SCRIPT DEBUG~~
    - ~~SCRIPT KILL~~
 - Server
    - ~~BGSAVE~~
    - ~~BGWRITEAOF~~
    - ~~CLIENT *~~
    - ~~CONFIG *~~
    - ~~DEBUG *~~
    - ~~INFO~~
    - ~~LASTSAVE~~
    - ~~MONITOR~~
    - ~~ROLE~~
    - ~~SAVE~~
    - ~~SHUTDOWN~~
    - ~~SLAVEOF~~
    - ~~SLOWLOG~~
    - ~~SYNC~~


## &c.

Integration tests are run against Redis 6.0.10. The [./integration](./integration/) subdir
compares miniredis against a real redis instance.

The Redis 6 RESP3 protocol is supported. If there are problems, please open
an issue.

If you want to test Redis Sentinel have a look at [minisentinel](https://github.com/Bose/minisentinel).

A changelog is kept at [CHANGELOG.md](https://github.com/alicebob/miniredis/blob/master/CHANGELOG.md).

[![Build Status](https://travis-ci.org/alicebob/miniredis.svg?branch=master)](https://travis-ci.org/alicebob/miniredis)
[![Go Reference](https://pkg.go.dev/badge/github.com/alicebob/miniredis/v2.svg)](https://pkg.go.dev/github.com/alicebob/miniredis/v2)
