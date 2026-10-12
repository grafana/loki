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
2. The query is a metric query supported by stream-first ordering in the query engine.
3. Every tenant of the request has stream-first execution turned on and a data-object query start time.
4. The data that the query reads overlaps the time range that data objects hold. That range starts
   at the latest start time among the tenants. It ends at the current time minus the storage lag,
   because the newest data is not in data objects yet.

The data that a query reads is wider than the time range of the request. It also covers the range
vector of the query, and it moves back by the `offset` of the query.

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
- the querier evaluates the query in stream-first order, which needs stream-first execution for
  the tenant;
- the tenant has a data-object start time, and the request overlaps the time range that data
  objects hold.

The querier reads the part of the request outside that time range from chunks, as it did before.
A request that did not pass through a query-frontend is never marked, so it reads chunks.

## Which queries prefer data objects

Eligibility decides whether a querier may read data objects. A second rule decides whether the
query-frontend changes how it splits and shards the query. A query prefers data objects when it is
eligible and data objects hold more than half of the time range that it reads. The query-frontend
applies this rule to the piece it is working on:

- to the whole query when it splits it in time;
- to each time split when it shards it, so a recent split that no data object covers keeps the
  chunk logic.

## How time splitting changes

A query that prefers data objects gets a longer split interval. The interval is never shorter than
the window of time that one data objects ToC file of data objects covers, which is 12 hours. A
longer interval is rounded up to a multiple of that window. The cut points stay aligned to UTC, and
the step alignment and the ingester window behave as before. The same rule applies to the split of
the range vector of an instant query.

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
  setting `-querier.dataobj-max-bytes-per-shard`. The number of shards is capped at 256, which is
  8 times the number of shard buckets of a data object. A data object has 32 buckets, so above 32
  shards several shards read the same bucket. The query then reads up to 8 times more data objects
  than it needs. We accept that to get more shards and finish the query sooner;
- with the bounded strategy, the index returns its shards as before. If it returns more than 256,
  the query-frontend merges neighbouring shards by size into at most 256 shards. The merged shards
  still cover the same range without gaps. The same over-read applies. The shards carry no chunk
  refs, because the query reads most of its range from data objects, and the querier resolves the
  chunks it needs itself.

## Limitations

- The shard count still comes from the chunk index.
- The bounded strategy still asks the index for shards of the size of the chunk setting, then merges
  them. It does not use the data-object setting.
- The prediction for time splitting is approximate.
