---
menutitle: Components
title: Loki components
description: The components that make up Grafana Loki.
weight: 500
aliases:
    - ../fundamentals/architecture/components
---
# Loki components

![components_diagram](../loki_architecture_components.svg "Components diagram")

## Distributor

The **distributor** service is responsible for handling incoming streams by
clients. It's the first stop in the write path for log data. Once the
distributor receives a set of streams, each stream is validated for correctness
and to ensure that it is within the configured tenant (or global) limits. Valid
chunks are then split into batches and sent to multiple [ingesters](#ingester)
in parallel.

It is important that a load balancer sits in front of the distributor in order to properly balance traffic to them.

The distributor is a stateless component. This makes it easy to scale and offload as much work as possible from the ingesters, which are the most critical component on the write path. The ability to independently scale these validation operations mean that Loki can also protect itself against denial of service attacks (either malicious or not) that could otherwise overload the ingesters. They act like the bouncer at the front door, ensuring everyone is appropriately dressed and has an invitation. It also allows us to fan-out writes according to our replication factor.

### Validation

The first step the distributor takes is to ensure that all incoming data is according to specification. This includes things like checking that the labels are valid Prometheus labels as well as ensuring the timestamps aren't too old or too new or the log lines aren't too long.

### Preprocessing

Currently the only way the distributor mutates incoming data is by normalizing labels. What this means is making `{foo="bar", bazz="buzz"}` equivalent to `{bazz="buzz", foo="bar"}`, or in other words, sorting the labels. This allows Loki to cache and hash them deterministically.

### Rate limiting

The distributor can also rate limit incoming logs based on the maximum per-tenant bitrate. It does this by checking a per tenant limit and dividing it by the current number of distributors. This allows the rate limit to be specified per tenant at the cluster level and enables us to scale the distributors up or down and have the per-distributor limit adjust accordingly. For instance, say we have 10 distributors and tenant A has a 10MB rate limit. Each distributor will allow up to 1MB/second before limiting. Now, say another large tenant joins the cluster and we need to spin up 10 more distributors. The now 20 distributors will adjust their rate limits for tenant A to `(10MB / 20 distributors) = 500KB/s`! This is how global limits allow much simpler and safer operation of the Loki cluster.

**Note: The distributor uses the `ring` component under the hood to register itself amongst its peers and get the total number of active distributors. This is a different "key" than the ingesters use in the ring and comes from the distributor's own [ring configuration]({{< relref "../configure#distributor" >}}).**

### Forwarding

Once the distributor has performed all of its validation duties, it forwards data to the ingester component which is ultimately responsible for acknowledging the write.

#### Replication factor

In order to mitigate the chance of _losing_ data on any single ingester, the distributor will forward writes to a _replication_factor_ of them. Generally, this is `3`. Replication allows for ingester restarts and rollouts without failing writes and adds additional protection from data loss for some scenarios. Loosely, for each label set (called a _stream_) that is pushed to a distributor, it will hash the labels and use the resulting value to look up `replication_factor` ingesters in the `ring` (which is a subcomponent that exposes a [distributed hash table](https://en.wikipedia.org/wiki/Distributed_hash_table)). It will then try to write the same data to all of them. This will error if less than a _quorum_ of writes succeed. A quorum is defined as `floor(replication_factor / 2) + 1`. So, for our `replication_factor` of `3`, we require that two writes succeed. If less than two writes succeed, the distributor returns an error and the write can be retried.

**Caveat: If a write is acknowledged by 2 out of 3 ingesters, we can tolerate the loss of one ingester but not two, as this would result in data loss.**

Replication factor isn't the only thing that prevents data loss, though, and arguably these days its main purpose is to allow writes to continue uninterrupted during rollouts & restarts. The `ingester` component now includes a [write ahead log](https://en.wikipedia.org/wiki/Write-ahead_logging) which persists incoming writes to disk to ensure they're not lost as long as the disk isn't corrupted. The complementary nature of replication factor and WAL ensures data isn't lost unless there are significant failures in both mechanisms (i.e. multiple ingesters die and lose/corrupt their disks).

### Hashing

Distributors use consistent hashing in conjunction with a configurable
replication factor to determine which instances of the ingester service should
receive a given stream.

A stream is a set of logs associated to a tenant and a unique labelset. The
stream is hashed using both the tenant ID and the labelset and then the hash is
used to find the ingesters to send the stream to.

A hash ring stored in [Consul](https://www.consul.io) is used to achieve
consistent hashing; all [ingesters](#ingester) register themselves into the hash
ring with a set of tokens they own. Each token is a random unsigned 32-bit
number. Along with a set of tokens, ingesters register their state into the
hash ring. The state JOINING, and ACTIVE may all receive write requests, while
ACTIVE and LEAVING ingesters may receive read requests. When doing a hash
lookup, distributors only use tokens for ingesters who are in the appropriate
state for the request.

To do the hash lookup, distributors find the smallest appropriate token whose
value is larger than the hash of the stream. When the replication factor is
larger than 1, the next subsequent tokens (clockwise in the ring) that belong to
different ingesters will also be included in the result.

The effect of this hash set up is that each token that an ingester owns is
responsible for a range of hashes. If there are three tokens with values 0, 25,
and 50, then a hash of 3 would be given to the ingester that owns the token 25;
the ingester owning token 25 is responsible for the hash range of 1-25.

### Quorum consistency

Since all distributors share access to the same hash ring, write requests can be
sent to any distributor.

To ensure consistent query results, Loki uses
[Dynamo-style](https://www.cs.princeton.edu/courses/archive/fall15/cos518/studpres/dynamo.pdf)
quorum consistency on reads and writes. This means that the distributor will wait
for a positive response of at least one half plus one of the ingesters to send
the sample to before responding to the client that initiated the send.

## Ingester

The **ingester** service is responsible for writing log data to long-term
storage backends (DynamoDB, S3, Cassandra, etc.) on the write path and returning
log data for in-memory queries on the read path.

Ingesters contain a _lifecycler_ which manages the lifecycle of an ingester in
the hash ring. Each ingester has a state of either `PENDING`, `JOINING`,
`ACTIVE`, `LEAVING`, or `UNHEALTHY`:

**Deprecated: the WAL (write ahead log) supersedes this feature**
1. `PENDING` is an Ingester's state when it is waiting for a handoff from
   another ingester that is `LEAVING`.

1. `JOINING` is an Ingester's state when it is currently inserting its tokens
   into the ring and initializing itself. It may receive write requests for
   tokens it owns.

1. `ACTIVE` is an Ingester's state when it is fully initialized. It may receive
   both write and read requests for tokens it owns.

1. `LEAVING` is an Ingester's state when it is shutting down. It may receive
   read requests for data it still has in memory.

1. `UNHEALTHY` is an Ingester's state when it has failed to heartbeat to
   Consul. `UNHEALTHY` is set by the distributor when it periodically checks the ring.

Each log stream that an ingester receives is built up into a set of many
"chunks" in memory and flushed to the backing storage backend at a configurable
interval.

Chunks are compressed and marked as read-only when:

1. The current chunk has reached capacity (a configurable value).
1. Too much time has passed without the current chunk being updated
1. A flush occurs.

Whenever a chunk is compressed and marked as read-only, a writable chunk takes
its place.

If an ingester process crashes or exits abruptly, all the data that has not yet
been flushed will be lost. Loki is usually configured to replicate multiple
replicas (usually 3) of each log to mitigate this risk.

When a flush occurs to a persistent storage provider, the chunk is hashed based
on its tenant, labels, and contents. This means that multiple ingesters with the
same copy of data will not write the same data to the backing store twice, but
if any write failed to one of the replicas, multiple differing chunk objects
will be created in the backing store. See [Querier](#querier) for how data is
deduplicated.

### Timestamp Ordering

Loki is configured to [accept out-of-order writes]({{< relref "../configure#accept-out-of-order-writes" >}}) by default.

When not configured to accept out-of-order writes, the ingester validates that ingested log lines are in order. When an
ingester receives a log line that doesn't follow the expected order, the line
is rejected and an error is returned to the user. 

The ingester validates that log lines are received in
timestamp-ascending order. Each log has a timestamp that occurs at a later
time than the log before it. When the ingester receives a log that does not
follow this order, the log line is rejected and an error is returned.

Logs from each unique set of labels are built up into "chunks" in memory and
then flushed to the backing storage backend.

If an ingester process crashes or exits abruptly, all the data that has not yet
been flushed could be lost. Loki is usually configured with a [Write Ahead Log]({{< relref "../operations/storage/wal" >}}) which can be _replayed_ on restart as well as with a `replication_factor` (usually 3) of each log to mitigate this risk.

When not configured to accept out-of-order writes,
all lines pushed to Loki for a given stream (unique combination of
labels) must have a newer timestamp than the line received before it. There are,
however, two cases for handling logs for the same stream with identical
nanosecond timestamps:

1. If the incoming line exactly matches the previously received line (matching
   both the previous timestamp and log text), the incoming line will be treated
   as an exact duplicate and ignored.

2. If the incoming line has the same timestamp as the previous line but
   different content, the log line is accepted. This means it is possible to
   have two different log lines for the same timestamp.

### Handoff - Deprecated in favor of the [WAL]({{< relref "../operations/storage/wal" >}})

By default, when an ingester is shutting down and tries to leave the hash ring,
it will wait to see if a new ingester tries to enter before flushing and will
try to initiate a handoff. The handoff will transfer all of the tokens and
in-memory chunks owned by the leaving ingester to the new ingester.

Before joining the hash ring, ingesters will wait in `PENDING` state for a
handoff to occur. After a configurable timeout, ingesters in the `PENDING` state
that have not received a transfer will join the ring normally, inserting a new
set of tokens.

This process is used to avoid flushing all chunks when shutting down, which is a
slow process.

### Filesystem Support

While ingesters do support writing to the filesystem through BoltDB, this only
works in single-process mode as [queriers](#querier) need access to the same
back-end store and BoltDB only allows one process to have a lock on the DB at a
given time.

## Query frontend

The **query frontend** is an **optional service** providing the querier's API endpoints and can be used to accelerate the read path. When the query frontend is in place, incoming query requests should be directed to the query frontend instead of the queriers. The querier service will be still required within the cluster, in order to execute the actual queries.

The query frontend internally performs some query adjustments and holds queries in an internal queue. In this setup, queriers act as workers which pull jobs from the queue, execute them, and return them to the query-frontend for aggregation. Queriers need to be configured with the query frontend address (via the `-querier.frontend-address` CLI flag) in order to allow them to connect to the query frontends.

Query frontends are **stateless**. However, due to how the internal queue works, it's recommended to run a few query frontend replicas to reap the benefit of fair scheduling. Two replicas should suffice in most cases.

### Queueing

The query frontend queuing mechanism is used to:

- Ensure that large queries, that could cause an out-of-memory (OOM) error in the querier, will be retried on failure. This allows administrators to under-provision memory for queries, or optimistically run more small queries in parallel, which helps to reduce the TCO.
- Prevent multiple large requests from being convoyed on a single querier by distributing them across all queriers using a first-in/first-out queue (FIFO).
- Prevent a single tenant from denial-of-service-ing (DOSing) other tenants by fairly scheduling queries between tenants.

### Splitting

The query frontend splits larger queries into multiple smaller queries, executing these queries in parallel on downstream queriers and stitching the results back together again. This prevents large (multi-day, etc) queries from causing out of memory issues in a single querier and helps to execute them faster.

### Caching

#### Metric Queries

The query frontend supports caching metric query results and reuses them on subsequent queries. If the cached results are incomplete, the query frontend calculates the required subqueries and executes them in parallel on downstream queriers. The query frontend can optionally align queries with their step parameter to improve the cacheability of the query results. The result cache is compatible with any loki caching backend (currently memcached, redis, and an in-memory cache).

#### Log Queries - Coming soon!

Caching log (filter, regexp) queries are under active development.

## Querier

The **querier** service handles queries using the [LogQL]({{< relref "../query" >}}) query
language, fetching logs both from the ingesters and from long-term storage.

Queriers query all ingesters for in-memory data before falling back to
running the same query against the backend store. Because of the replication
factor, it is possible that the querier may receive duplicate data. To resolve
this, the querier internally **deduplicates** data that has the same nanosecond
timestamp, label set, and log message.

At read path, [replication factor]({{< relref "#replication-factor" >}}) also plays a role here. For example with `replication-factor` of `3`, we require that two queries to be running. 

