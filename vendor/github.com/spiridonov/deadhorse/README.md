# DeadHorse

[![Go](https://github.com/spiridonov/deadhorse/actions/workflows/go.yml/badge.svg)](https://github.com/spiridonov/deadhorse/actions/workflows/go.yml)

> The name comes from [Dead Horse Point in Utah](https://stateparks.utah.gov/parks/dead-horse/discover/).

DeadHorse is a small, high perfomance, distributed rate limiter. A DeadHorse server holds
nothing but counters — the limit itself (how big the bucket is, how fast it drains) comes
with every request. And it talks a plain, newline-delimited text protocol simple enough to
implement a client for in any language with nothing more than a socket and a few string
operations. It has been tested to hold 20M keys at 220K rps.

```
$ nc localhost 9000
THROTTLE user:42:writes|100|10000000|1|R
RESULT user:42:writes|0|100|0
```

## Why it's built this way

Precision is traded for perfomance, simplicity, and high availability:

- **Limits travel with the request.** A DeadHorse process never has its own copy of "user 42 gets
  100 writes/sec" rule anywhere in memory — every `THROTTLE` request carries its own
  `(capacity, rate, cost)`. This means the server is never coupled to whatever system decides who
  gets what limit. It just keeps a counter.
- **Servers don't talk to each other.** There is no gossip, no membership protocol, no leader
  election, no shared storage. A DeadHorse process would behave identically whether it's the only
  one running or one of a hundred. Spreading load across many of them (sharding) is entirely a
  client-side concern — see [Sharding](#sharding).
- **The wire protocol is just text.** To have the smallest possible overhead. Plus a whole debugging
  session can be driven by hand from `nc` or `telnet`.
- **Always fails open.** If an instance of DeadHorse is down, or a cluster is scaling up, DeadHorse
  client always under-throttles. Overall availability and user experience are never impacted.
- **Idle keys are forgotten automatically**, without a per-key timer or a background scan — see
  [Garbage collection](#garbage-collection).

## Leaky Bucket via GCRA

DeadHorse rate-limits with a *leaky bucket*: each request adds `cost` units of water to a bucket
of size `capacity`. The bucket continuously drains at a fixed rate (one unit every
`emission_interval` nanoseconds). A request is admitted only if there's room for its cost once
the drain since the last request is accounted for. Get a burst of `capacity` requests through
instantly, then settle into a steady `1/emission_interval` requests per second.

Rather than storing an explicit water level that has to be recomputed and rewritten on every call,
DeadHorse implements this with GCRA 
([Generic Cell Rate Algorithm](https://en.wikipedia.org/wiki/Generic_cell_rate_algorithm)): the 
entire state of a bucket is a single timestamp, the *theoretical arrival time* (TAT) — the point 
in time at which the bucket would next have room. Checking a request is a handful of integer 
comparisons against `now`; admitting one just pushes the TAT forward by `cost * emission_interval`.
No floating point, no locks beyond the one guarding that single field, nothing to refill on read.

```
capacity=100, emission_interval=10ms  →  100 requests/sec sustained, bursts of 100

100 requests arrive at once  →  all 100 admitted, bucket now "full"
101st request, same instant  →  throttled, retry after ~10ms (one unit has to drain first)
```

A request can run in **real** mode (consume from the bucket) or **peek** mode (report what would
happen without touching any state) — useful for a caller that wants to check a limit without
committing to it.

## Sharding

One instance of DeadHorse should be enough for most applications. But if your keyspace is extremely
large or the query rate is super high you might want to spread the load between several instances of
DeadHorse.

Since a DeadHorse server holds no shared state and talks to nothing else, running a fleet of them
is just running several independent processes. A client picks which one owns a given key —
typically `hash(key) % number_of_shards` — and only ever talks to that one shard for that key.
Nothing needs to be told about the shard count except the clients.

This has a pleasant side effect: because a bucket is disposable state (losing it just means one
key gets a fresh, full bucket), resharding — changing the number of servers — costs nothing but a
little temporary under-enforcement for the keys that get remapped. There's no data to migrate and
no rebalancing to orchestrate.

`client.ShardedClient` (see [Client libraries](#client-libraries)) implements exactly this: a
static list of shard addresses, plain modulo hashing, and per-shard connections managed
independently.

A shard is also a transaction boundary (see [DHP/1](#deadhorse-protocol-reference-dhp1)):
`client.ShardedClient.Throttle(ctx, shardKey, entries)` treats one call as one transaction. 
`shardKey` -- hashed to pick which shard the whole call goes to -- is what a caller uses to
control that: every entry passed to one call is sent together as a single line, and every `R`
entry among them commits as one all-or-none group. To batch several independent checks in one call
the way you always could, just pass any one of their keys (or anything else) as `shardKey` -- it
only matters when it's shared. To force two checks that *must* be decided together onto the same
shard and the same transaction -- a per-org and a per-user check for the same request, say -- give
that one call a `shardKey` of your choosing (a tenant ID, typically) and send both
entries in it. `shardKey` is specific to `ShardedClient`: `server.InMemoryThrottler` (and therefore
a bare `TextServer`) has only one shard, itself, so its own `Throttle` takes no such argument --
every call to it is already the transaction (see [DHP/1](#deadhorse-protocol-reference-dhp1)).

## Garbage collection

A server's entire state is a striped map from key to a single `int64` (that TAT value above).
Idle keys still need to go away eventually, or memory grows without bound. Rather than a per-key
expiry timer or a periodic scan of every key, each stripe keeps **two map generations**, `hot` and
`cold`:

- Touching a key (in either mode) looks it up in `hot`; if it's only found in `cold`, it's
  promoted into `hot` first.
- On a fixed interval, a background goroutine swaps every stripe's `cold` out and starts a fresh,
  empty `hot` — an O(1) pointer swap per stripe, regardless of how many keys exist. The rest is done
  by Go GC.

A key that's touched at least once per interval never leaves `hot` and survives forever; a key
that goes untouched for one to two intervals gets dropped for free, with no scan and no per-key
bookkeeping. The interval is configurable (`-gc-interval`); pick something comfortably larger than
the longest burst window (`capacity * emission_interval`) any of your limits actually use.

## DeadHorse Protocol reference (DHP/1)

Every line is one command in, one response out, terminated by `\n`. A connection may pipeline —
write many requests before reading any responses — and gets them back in the same order it sent
them. Keys may be any non-empty run of bytes containing no whitespace and no `|`.

| Command | Request | Response |
|---|---|---|
| Throttle | `THROTTLE <entry>...` | `RESULT <result>...` |
| Handshake (optional) | `HELLO <version>` | `OK <version>` or `ERROR <reason>` |
| Liveness | `PING` | `PONG` |
| Introspection | `STATS` | `STATS <uptime_s> <keys>` |
| Disconnect | `QUIT` | *(none — connection closes)* |

An `<entry>` in a `THROTTLE` line is one pipe-separated tuple:

```
key|capacity|emission_interval_ns|cost|mode
```

`mode` is `R` (real — consume on success) or `P` (peek — never mutates state). The matching
`<result>` in the response is:

```
key|throttled|remaining|retry_after_ns
```

`throttled` is `0` or `1`; `remaining` is the bucket's headroom in cost units *as of just before
this request* — not affected by this request's own cost or outcome, so a request that itself gets
admitted (or throttled) still reports the same `remaining` a peek at that same instant would have;
`retry_after_ns` is how many nanoseconds until the request would have fit (`0` if it wasn't
throttled). One
`THROTTLE` line can carry several entries at once — handy when one logical call needs to check more
than one limit (a per-user limit and a per-org limit, say) in a single round trip. There's no count
field anywhere in the grammar: entries and results are just whatever whitespace-separated tokens
follow the command word, which is unambiguous since keys can't contain whitespace and nothing on
the wire is escaped.

If one entry in a batch is malformed, its result is the token `ERR` and every other entry in the
same batch is still answered normally — a `THROTTLE` line never fails outright. Only `QUIT` and a
line that exceeds the configured maximum length end a connection.

**A line is a transaction for its `R` entries.** Every `R` entry on one line is evaluated first,
against a private copy of each bucket it touches, without writing anything; only if every one of
them individually admits does the whole line commit, all at once — if even one would be throttled,
none of them are, and every `R` entry reports `throttled=1`, including ones whose own bucket had
plenty of room. `P` entries are entirely unaffected either way: each is still evaluated and
reported on its own, exactly as if it were the only entry on the line.

A worked example, batching a per-org and a per-user check in one round trip:

```
> THROTTLE org:acme:writes|100|10000000|1|R user:42:writes|20|50000000|1|R
< RESULT org:acme:writes|1|63|0 user:42:writes|1|0|12000000
```

The user-level check failed, so the line is denied as a whole: the org-level check reports
`throttled=1` too, even though its own bucket had 63 units of headroom to spare (`remaining` and
`retry_after_ns` still reflect that unused headroom — only `throttled` reflects the line's actual,
all-or-none outcome). Neither bucket's state changed. DeadHorse has no opinion on how a caller
combines several results from one line beyond this — that's entirely up to the caller.

`HELLO` exists for protocol/version negotiation; a client that skips it entirely is assumed to
speak version `1`. DeadHorse has no authentication or encryption of its own — treat it the way
you'd treat memcached or a bare Redis: fine on a trusted network, wrap it in TLS or put it behind
a firewall otherwise.

## Getting started

### Run the server

```sh
go install github.com/spiridonov/deadhorse/cmd/deadhorse@latest
deadhorse
```

| Flag | Default | Meaning |
|---|---|---|
| `-host` | (all interfaces) | DHP/1 text protocol listen host |
| `-port` | 9000 | DHP/1 text protocol port |
| `-prometheus-port` | 9090 | Serves `/metrics` (Go runtime/process stats plus DeadHorse's own -- see [Metrics](#metrics)) |
| `-stripes` | 256 | Concurrency stripes in the in-memory store |
| `-gc-interval` | 60s | How often idle keys are dropped |
| `-max-line-size` | 64KiB | Longest protocol line accepted before the connection is closed |

Each server is a single static binary with no external dependencies of its own — no database, no
coordination service, nothing to run besides the process itself.

### Metrics

`-prometheus-port` serves Go runtime/process stats plus these, all under a `deadhorse_` prefix:

| Metric | Type | Meaning |
|---|---|---|
| `keys{generation}` | Gauge | Keys held by the store, `generation="hot"` or `"cold"` (refreshed every 5s, independent of `-gc-interval`) |
| `connections` | Gauge | Currently open DHP/1 connections |
| `requests_total{command}` | Counter | Command lines handled, by command (`HELLO`/`THROTTLE`/`PING`/`STATS`/`QUIT`/`unknown`) |
| `request_duration_seconds{command}` | Histogram | Time to handle one command line, by command |
| `throttle_entries_total{mode,result}` | Counter | `THROTTLE` entries evaluated, by `mode` (`real`/`peek`/`unknown`) and `result` (`admitted`/`throttled`/`err`) |
| `throttle_batch_size` | Histogram | Entries per `THROTTLE` line |
| `line_length_bytes` | Histogram | Length of each protocol line read |
| `line_too_long_total` | Counter | Connections dropped for exceeding `-max-line-size` |

## Client libraries

### Go client

```go
import (
    "context"
    "time"

    "github.com/spiridonov/deadhorse"
    "github.com/spiridonov/deadhorse/client"
)

c := client.NewShardedClient([]string{"shard-0:9000", "shard-1:9000"})
defer c.Close()

results, err := c.Throttle(ctx, "user:42:writes", []deadhorse.RequestEntry{
    {Key: "user:42:writes", Limit: deadhorse.Limit{Capacity: 100, EmissionInterval: 10 * time.Millisecond}},
})
if results[0].Throttled {
    // reject the request; results[0].RetryAfter says how long until it would fit
}
```

The `shardKey` argument (here, just the one entry's own `Key`) is what `ShardedClient` hashes to
pick a shard for the whole call -- see [Sharding](#sharding) for using it to force several entries
onto one shard, and therefore one all-or-none transaction.

`Peek` defaults to `false`, so a `RequestEntry` that forgets to set it still actually enforces the
limit rather than silently becoming a no-op. `ShardedClient` fails **open** by default: if a shard
can't be reached within its timeout (10ms by default, see `WithTimeout`), the affected entries are
reported as not throttled rather than failing the caller's request — pass `WithFailClosed()` for
limits where that's the wrong default. This never applies to an entry rejected by local validation
(an empty or malformed key): that's always reported as throttled, since a caller bug isn't something
fail-open is meant to paper over.

`err` is a join of every distinct problem `Throttle` ran into, for callers who just want a cheap
"did anything go wrong" check; `results` is always fully populated at the same length as the
request, whether or not `err` is nil, and each `ResponseEntry.Err` says exactly which entry had a
problem and why (see `client.ErrInvalidKey`, `client.ErrEntryRejected`) — `Throttled` itself always
holds a sensible value either way, so code that only reads `Throttled` works the same whether or
not it checks the rest.

### Embedding directly

The same rate limiter that backs the server is a plain, importable type — useful for a
single-process application that wants leaky-bucket limiting without running anything separately:

```go
import "github.com/spiridonov/deadhorse/server"

throttler := server.NewInMemoryThrottler(0, 0) // 0 = default stripes/GC interval
defer throttler.Close()

results, err := throttler.Throttle(ctx, []deadhorse.RequestEntry{
    {Key: "user:42:writes", Limit: deadhorse.Limit{Capacity: 100, EmissionInterval: 10 * time.Millisecond}},
})
```

Unlike `client.ShardedClient.Throttle`, there's no `shardKey` here: `InMemoryThrottler` never
shards, so there's nothing to route -- every call to it is already the whole transaction (see
[DHP/1](#deadhorse-protocol-reference-dhp1)). `InMemoryThrottler` implements `server.Throttler`,
the same interface `TextServer` is built on (and never returns a non-nil error itself -- a
nonsensical limit just fails closed), so code written against it works unchanged whether the
limiter lives in-process or behind the network protocol.


## License

DeadHorse is released under the [MIT License](https://opensource.org/licenses/MIT).
