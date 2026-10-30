# Routing queries to data objects

This document describes how Loki decides to read a metric query from data objects, and how that
decision changes the way the query-frontend splits and shards the query. Data objects are
experimental.

## Purpose

A query can read its samples from chunks or from data objects. Both hold the same data, and they
need different amounts of parallelism. The query-frontend splits a query in time and shards it
before a querier runs it. It must therefore know early whether a querier is going to read data
objects.

The query-frontend makes the first decision: it tells the querier that a request may read data
objects. The querier makes the last decision. It reads data objects only when the query-frontend
allowed it and its own checks pass.

## When a query is eligible

The query-frontend calls a request eligible for data objects when all of these hold:

1. The query-frontend has data objects turned on.
2. The query is a metric query, and the query engine can run at least one part of it in stream-first
   order. The part can sit at any depth, for example under `label_replace` or inside a binary
   expression.
3. Every tenant of the request has stream-first execution turned on and a data-object query start time.
4. The data that at least one stream-first part reads overlaps the time range that data objects
   hold. That range starts at the latest start time among the tenants. It ends at the current time
   minus the storage lag, because the newest data is not in data objects yet.

The data that a part reads is wider than the time range of the request. It also covers the range
vector of the part, and it moves back by the `offset` of the part.

## Where the decision is made

The query-frontend rewrites queries. Query sharding replaces a query with sub-queries, one for each
shard. Splitting the range vector of an instant query does the same. A querier judges the query it
receives, and that is a sub-query.

A user query that is not eligible can produce eligible sub-queries. The query-frontend therefore does
not decide once, when the query arrives. It decides for every request that it sends to a querier, as
the last step before it sends the request. The request carries the answer to the querier.

## What the querier does with the decision

The querier reads data objects for a request only when all of these hold:

- the query-frontend marked the request eligible;
- the querier has data objects turned on;
- the querier evaluates a part of the query in stream-first order, which needs stream-first
  execution for the tenant;
- the tenant has a data-object start time, and the request overlaps the time range that data
  objects hold.

The querier reads the time range of the request outside that range from chunks. It also reads from
chunks any part of the query that does not run in stream-first order. A request that did not pass
through a query-frontend is never marked, so it reads chunks.

## Which queries prefer data objects

Eligibility decides whether a querier may read data objects. A second rule decides whether the
query-frontend changes how it splits and shards the query. A query prefers data objects when it is
eligible and data objects hold more than half of the read windows of all its range vectors, taken
together. A longer window weighs more. A range vector that reads from chunks, such as one that does
not run in stream-first order, counts as zero. The query-frontend applies this rule to the piece it
is working on:

- to the whole query when it splits it in time;
- to each time split when it shards it, so a recent split that no data object covers keeps the
  chunk logic.

## How time splitting changes

When splitting is on, a query that prefers data objects gets a split interval of at least one
metastore window, which is 12 hours. One Table of Contents file of the metastore covers that
window. A longer interval is rounded up to a multiple of the window. The step alignment and the
ingester window work as for any other query. An instant query uses the same interval for the split
of its range vector.

A range query cuts its time range at UTC multiples of the interval. An instant query cuts its range
vector back from its evaluation time. Its cut points align to UTC only when
`-querier.instant-metric-query-split-align` is on, and only if the query has no `offset`, or an
`offset` that is a multiple of the interval. Otherwise, a sub-query can span two metastore windows.

Time splitting runs before sharding, so the query-frontend cannot yet see the sub-queries that the
queriers will receive. It predicts from the whole query. It treats the query as data-object shaped
when any part of it, such as one side of a division, has the eligible shape. It also requires that
data objects hold more than half of the time range that the query reads, measured with the longest
range vector and the longest offset of the query. A wrong prediction only changes how the query is
cut. Every request is still judged on its own when it leaves the query-frontend.

## How sharding changes

A piece of a query that prefers data objects is sharded with its own rules. The tenant keeps its
configured sharding strategy:

- with the power-of-two strategy, the number of shards comes from the bytes of the query and the
  setting `-querier.dataobj-max-bytes-per-shard`. The number of shards is capped at 8 times the
  number of shard buckets that the tenant sets for new data objects, and never above 256. The
  default is 32 buckets, so the default cap is 256. Above the number of buckets, several shards
  read the same bucket. The query then reads up to 8 times as much data as it needs. We accept that
  to get more shards and finish the query sooner;
- with the bounded strategy, the index returns its shards. If it returns more than the cap, the
  query-frontend merges neighbouring shards by size into at most that many shards. The merged
  shards still cover the same range without gaps. The same over-read applies. The query-frontend
  drops the chunk refs of the shards, merged or not, because the query reads most of its range from
  data objects. The querier resolves the chunks it needs itself.

### Shard buckets

A tenant sets the number of shard buckets of its new data objects with the `dataobj_shard_factor`
limit. The query-frontend takes the number from the current setting of the tenant. A query can have
several tenants. The query-frontend then takes the largest number, because a smaller one would
limit the shards of the tenant with the most buckets. A tenant with fewer buckets over-reads by
more than 8 times in that query.

The query-frontend only estimates the cost. The metastore and the querier decide which streams a
shard reads. The data objects of one query can have different numbers of buckets, because a tenant
can change its setting. The metastore reads the number of buckets from each index object, and the
querier reads it from each data object. Both take 32 for an object that records none, because
legacy data objects used 32 hardcoded shard buckets. Each of them maps the shard to the buckets of
that object. A shard reads the same streams whatever the number of buckets of the objects.

## Limitations

- The shard count still comes from the chunk index.
- The bounded strategy still asks the index for shards of the size of the chunk setting, then merges
  them. It does not use the data-object setting.
- The cap on the number of shards can make a shard larger than `max_querier_bytes_read` allows. The
  query-frontend then rejects a query that the chunk logic would shard into smaller pieces and
  accept. This affects only the largest queries.
- The prediction for time splitting is approximate.
