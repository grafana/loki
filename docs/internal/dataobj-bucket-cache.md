# The data object bucket cache

This document describes how the querier caches the bytes that it reads from data objects in object
storage, and why the cache works the way it does.

## Purpose

A query that reads data objects sends many byte-range requests to object storage. Each request takes
tens to hundreds of milliseconds, and queries often read the same ranges again. The bucket cache keeps
those ranges in a cache backend, such as memcached, so a repeated read does not reach object storage.

## Overview

The cache sits between the data object readers and the object store client. It sees every byte-range
read, and it knows nothing about the data object format. A router picks one of two caches from the
object name: one for log objects, and one for index objects.

```
logs reader / streams reader / metastore
                |
                v
              router
         |             |
         v             v
    logs cache     index cache  <--->  cache backends (blocks, object attributes)
         |             |      only the misses go down
         v             v
         coalescing bucket       merges the missing ranges
                |
                v
        instrumented bucket      per-query data object statistics count here
                |
                v
   object store metrics and tracing
                |
                v
         object storage
```

Each cache splits the reads it gets into fixed-size, aligned blocks. The code, the flags, and the
metrics call them subranges. It serves the blocks that its backend holds and reads the others from
object storage. It then stores the new blocks.

The readers ask for all the pages of a batch in one call, a multi-range read. The cache sees the exact
ranges that the query needs. The coalescing bucket below it merges the missing ranges before they go
to object storage.

## What the cache holds

The cache holds log objects and index objects. Their names come from their content, so the content of an
object never changes and the cache never has to invalidate an entry.

The cache never holds the Table of Contents (ToC) of the metastore. The ToC is the only mutable object, and
the ToC resolver already refreshes and caches it. The router decides which objects a cache gets, from
the prefix of the object name. Log objects live under `objects/`, and index objects under
`index/v0/indexes/`. The router must send only objects that never change, because the cache keys carry
no object version. It sends every other name, the ToC included, to the object store.

The cache serves reads of a byte range, multi-range reads, and requests for the attributes of an
object. Every other call goes straight to the object store. A read that the cache cannot split also
goes straight through. This includes a read of the whole object from an offset to its end.

## How a read works

1. The router checks the object name and picks the logs cache or the index cache. A name that no cache
   serves goes straight to the object store. So does a name of a kind that has no cache.
2. The cache finds the size of the object. It needs the size to align blocks. A read of one range that
   ends past the end of the object is cut at the end. A read of one range that starts at or past the
   end goes to the object store, so it fails as it does without the cache. A multi-range read goes
   whole to the object store when any of its ranges ends past the end of the object, because the
   cache cannot cut a buffer of fixed length.
3. The cache lists the blocks that cover the ranges and asks the backend for all of them in one call.
   A block that two ranges share counts once. A memcached backend splits the call into batches of its
   configured batch size.
4. A block counts as a hit only if it has the length and the full key that the cache expects. The cache
   treats every other block as a miss.
5. The cache reads the missing blocks from the bucket below with one multi-range call, and stores them
   in the backend.
6. A multi-range read copies the bytes into the buffers of the caller. A read of one range returns a
   reader over the blocks, trimmed to the requested range.

A read in which every block hits sends no request to object storage.

## Design decisions

### Cache byte ranges, not column pages

The cache works on the byte ranges that the readers request. It does not cache decoded pages or
columns. This has the following advantages:

- Fixed cached items size reduces the cache backend (e.g. memcached) memory fragmentation.
- Avoids caching very small items (e.g. some highly compressed column pages could be a few KB).

### Fixed-size aligned blocks

The cache does not store the exact range of each read. It stores blocks of a fixed size at aligned offsets.
The default size is 16,000 bytes. Reads of different sizes and offsets then share entries, and a read that
overlaps an earlier one hits on the overlap.

The cost is at the edges. A read fetches up to two partial blocks more than it needs. Small blocks keep
this cost small, and the cache asks the backend for all the blocks of a read in one call.

### Every value starts with its full key

A cache key holds a hash of the object name, to stay below the key length limit of the backends. A
collision of two hashes is unlikely, but it would serve the data of one object for another.

Each value therefore starts with its full key, built from the full object name. A read compares it with
the key that it expects, and treats a mismatch as a miss. The same applies to the cached attributes of an
object. The cost is one copy of the full key, about 150 bytes, in each block of 16,000 bytes (default size).

### The cache sees exact ranges, and the coalescing bucket merges the misses

A reader needs many pages of one object, and the pages of one batch are not next to each other. If the
readers merged the pages into big ranges before the cache, the ranges would include the gaps between
the pages. The cache would fetch and store the bytes of the gaps, which no query asked for, and every
warm query would read them back from the cache backend.

The readers therefore ask for the pages with one multi-range read: one object name and many ranges.
The cache looks up the blocks of all the ranges in one call, and passes the missing blocks to the bucket
below, as ranges, in one call.

The coalescing bucket below the cache merges the missing ranges that are close, splits the ranges that
are big, and reads them in parallel. It takes the sizes from the `rangeio` config in the context of the
request. A merged read can include a gap. The bucket reads the gap and
discards it, and the cache never stores it. A counter shows these bytes as overread, next to a counter of the bytes that the callers needed.

The cache does not bound the requests of a read. The coalescing bucket does. A read of one range, such
as the read of a footer, takes the same path: its misses go down in one multi-range call. A bucket that
is not the bucket of the querier has no multi-range read, and its readers merge the ranges with
`GetRange`.

### Object attributes come from the metadata cache

Every read needs the object size. Without an attributes cache, each read would send a request for it.

The cache keeps the attributes of an object: its size and its modification time. It looks for them in
three places: a small in-memory table in each querier, then the metadata cache, then the object store.
The metadata cache stores them under their own kind of entry and makes concurrent readers share one
load. An object then costs one attributes request for the whole cluster. If the deployment has no
metadata cache, the attributes go to the backend of their own cache. Each of the two caches has its own
in-memory table, so log objects cannot evict the entries of index objects.

The same entries answer a direct request for the attributes of an object. The modification time keeps
millisecond precision.

### A separate backend

Each bucket cache has its own backend, apart from the metadata cache. The metadata cache and the bucket
caches store very different entries. The metadata cache holds a few large entries of known shape. The
bucket caches hold many small blocks. They need different sizing, and a flood of blocks must not evict
metadata.

A cache is on when its backend is configured, and off otherwise. The cache keys carry an ID of the bucket
and its prefix, so two buckets cannot collide. They also carry the type of data, and a new type lets us
change the layout of a value later.

### One cache for each kind of object

Log reads move most of the bytes that a querier reads from object storage, but queries reuse index
objects much more. With one backend, a burst of log blocks evicts index blocks. The logs cache and the
index cache therefore have their own backends and their own settings: the block size, the size of the
in-memory table of attributes. Either cache can be on while the other is off.

The two caches isolate eviction only when their backends are separate servers. Memcached evicts the least
recently used item inside a size class, and both kinds store blocks of about the same size. Two caches
that point to one memcached server share their evictions. The keys of the two kinds cannot collide, so
sharing a server is safe.

### Memory for hits

A hit has to copy bytes from the cache client into a buffer. The cache takes these buffers from a pool
of large slabs. A multi-range read returns them when it has copied the hits into the buffers of the
caller. A read of one range returns them when the caller closes the reader. This avoids one allocation
for each block and lowers the work of the garbage collector.

The pool serves only the hits. The cache backend can keep a buffer after a store call returns, so the
blocks that it stores use ordinary memory. A tiered cache copies hits from its lower tier into its upper
tier, so it ignores the pool and allocates for itself.

### The cache is best effort

The cache must not fail a query, and must not return wrong data:

- A failed lookup counts as a miss for the blocks it did not return. A failed store is only counted.
- A block with an unexpected length counts as a miss, and the cache overwrites it.
- A canceled query does not count as a cache error. Once a read succeeds, the store of its blocks
  ignores the cancellation of the query, because other queries can use the blocks.
- A failure to read the attributes of an object fails the read. The read would fail without the cache
  for the same reason.
- A short read of the missing blocks never ends with a plain end-of-file. The column readers take that
  signal as the end of the column and return fewer rows without an error. The cache returns an
  "unexpected end" error instead. A multi-range read that goes beyond the end of the object goes to
  the bucket, and its error can wrap an end-of-file. The section reader turns that error into an
  "unexpected end" error too.

### Requests are counted below the cache

The instrumented bucket sits below the coalescing bucket. It counts each request that reaches object
storage and the bytes of its body. The per-query data object statistics and the metric
`loki_querier_dataobj_object_store_requests_total` come from it. The metrics and traces of the object
store client sit directly below it and see the same requests.

A hit sends no request, so it counts nothing in these statistics. A merged read counts once, with the
bytes of its gaps. The readers above the cache do not record requests when the bucket records its own.
Every other caller of the readers keeps the instrumented reader, because it reads a bucket that
records nothing.

The cache has its own metrics, with the prefix `loki_dataobj_bucket_cache_`:

- the bytes that readers requested, and the bytes fetched from the cache and from the bucket;
- the number of blocks that reads asked for, and the number that hit;
- the corrupted blocks that the cache rejected;
- the errors of lookups and stores.

The coalescing bucket has two metrics. `loki_dataobj_coalescing_bucket_needed_bytes_total` counts the bytes
in the ranges that callers asked it to read. `loki_dataobj_coalescing_bucket_overread_bytes_total` counts
the bytes that it read from object storage and no range needed. The bucket reads the sum of the two.

## Relation to the metadata cache

The metadata cache and the bucket caches work at different layers and can run together. The metadata
cache sits in the data object reader. It keeps the metadata region of an object and answers before a
read leaves the reader. The bucket caches sit below the reader and cache whatever range is left,
including the metadata ranges that the metadata cache did not hold.
