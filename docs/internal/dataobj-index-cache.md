# Data object index cache

The index cache keeps the results of looking up a stream selector in the index objects. It makes the
resolution of the sections of a query read an index object once for each stream selector and each
predicate, and then serve the same lookup to every later request that needs it.

This page describes the behavior for people who run or change the metastore. The flags are in the
configuration reference under `dataobj.index_cache`.

## What it caches

A query to the data objects makes one resolution for each split and shard. Each resolution looks up
the stream selector of the query in each index object that overlaps its time range. An index object
never changes, so the result of a lookup never goes stale.

The cache holds two kinds of entry for each index object:

- Selector matches. They are the sections whose streams match the stream selector, cut for a shard
  bucket range. They do not depend on the time range of the query, so a query with another time
  range finds the same entry. The metastore applies the time range of the request when it serves the
  entry.
- Predicate admissions. They are the answers of the bloom filters and of the label names to one
  equality predicate. They do not depend on the selector, on the shard or on the time range, so
  queries with the same predicate share them.

## Keys

The key of an entry has the tenant, the path of the index object and what the entry depends on:

- Selector matches: the stream matchers in a canonical order, and the shard bucket range together
  with the shard layout bits of the index object. A request without a shard range, and a range that
  covers every bucket, share the key `all`.
- Predicate admissions: the name and the value of the predicate.

The backend gets the SHA-256 of the key. The value holds a version and the full key, and a reader
drops a value with another key or version. A new version of the layout of the values changes the
prefix `idx/v1/`.

## Misses

The metastore asks the backend for the entries of all the index objects of a request in one call. A
request has one key for the selector matches of each index object, and one more key for each equality
predicate of each index object. The memcached client splits a call into batches of
`-dataobj.index-cache.memcached.batchsize` keys, and sends up to
`-dataobj.index-cache.memcached.parallelism` batches at the same time, across all the concurrent
requests of the querier. The defaults are 4 and 5. To send a request in one batch, raise `batchsize`
above the number of keys of a typical request.

The metastore serves the index objects that the backend has at once, and reads the others from object
storage.

A request for a single shard bucket reads the rows of every bucket of the index object, and stores
the entry of each bucket. This happens for an index object with at most 256 shard buckets. A query
that splits into at least as many shards as the index object has buckets then reads the index object
for its first shards. The other shards find their bucket in the backend. Requests that miss the same
entry at the same time, in one querier, share one read.

A failure of the backend, an entry that does not decode, or an entry that is too big for the backend
counts in the metrics, and never fails the request. The request reads the index object.

A fill that reports a deadline does not fail the request either. A fill has a limit of 30 seconds,
and an object store client can report its own timeout. The request reads the index object without the
cache, as it does when the cache is off.

A read of the shard layout of an index object has its own limit of 30 seconds. When it takes longer,
the request fails with an error that names the read. The error does not wrap a deadline error, so the
query API does not report it as a query timeout.

## Metrics

| Metric | Meaning |
|---|---|
| `loki_dataobj_index_cache_requests_total{kind,result}` | Fetches of an entry. `kind` is `selector_matches` or `predicate_admission`. `result` is `hit`, `miss`, `error` (the backend failed) or `invalid` (the entry does not decode, or belongs to another key or version). The cache also logs an invalid entry. |
| `loki_dataobj_index_cache_stores_total{kind,result}` | Entries that the cache tried to store. `result` is `stored`, `error` or `oversize`. The memcached and redis backends store in the background, so `stored` counts the entries that the cache handed over, and the background queue can drop some of them. |
| `loki_dataobj_index_cache_entry_bytes{kind}` | Size of the entries that the cache stores. |

## Operating it

- Give it its own backend. Every resolution fetches its entries, so the latency of the backend adds
  to every resolution, hits included. Set a short timeout on the memcached client. The timeout
  applies to each socket operation, not to a whole fetch, so a fetch of several batches can take
  longer than the timeout.
- Entries are small for a request with a shard range: a few hundred bytes to some kilobytes. A
  request without a shard range on a compacted index object has bigger entries, from tens to
  hundreds of kilobytes. The cache skips an entry over `max_item_size` of the memcached client, and
  counts it as `oversize`. When `max_item_size` is 0, the cache uses 1 MiB.
- An entry never goes stale, so `default_validity` does not affect correctness. An entry expires that
  long after the cache stores it, whether or not queries still read it. A short value makes the cache
  read active index objects again.
- A restarted querier reads the shard layout of each index object once, through the index metadata
  cache, before it can build the keys of a request that has a shard.
- Entries are keyed by the path of the index object. Do not share a backend between installations
  that use the same bucket paths.
