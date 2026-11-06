# The data object bucket cache

This document describes how the querier caches the bytes that it reads from data objects in object
storage, and why the cache works the way it does.

## Purpose

A query that reads data objects sends many byte-range requests to object storage. Each request takes
tens to hundreds of milliseconds, and queries often read the same ranges again. The bucket cache keeps
those ranges in a cache backend, such as memcached, so a repeated read does not reach object storage.

## Overview

The cache sits between the data object readers and the object store client. It sees every byte-range
read, and it knows nothing about the data object format.

```
logs reader / streams reader / metastore
                |      per-query data object statistics count here
                v
          bucket cache  <--->  cache backend (blocks, object attributes)
                |      only the misses go down
                v
   object store metrics and tracing
                |
                v
         object storage
```

The cache splits each read into fixed-size, aligned blocks. It serves the blocks that the backend
holds and reads the others from object storage. It then stores the new blocks.

## What the cache holds

The cache holds log objects and index objects. Their names come from their content, so the content of an
object never changes and the cache never has to invalidate an entry.

The cache never holds the Table of Contents (ToC) of the metastore. The ToC is the only mutable object, and
the ToC resolver already refreshes and caches it. A rule decides which objects the cache accepts, and
that rule must accept only objects that never change, because the cache keys carry no object version.

The cache serves reads of a byte range and requests for the attributes of an object. Every other call
goes straight to the object store. A read that the cache cannot split also goes straight through. This
includes a read of the whole object from an offset to its end.

## How a read works

1. The cache checks the object name. A name that does not match goes straight to the object store.
2. The cache finds the size of the object. It needs the size to align blocks and to cut a range that
   ends past the end of the object. A range that starts at or past the end goes to the object store,
   so it fails as it does without the cache.
3. The cache lists the blocks that cover the range and asks the backend for all of them in one request.
4. A block counts as a hit only if it has the length and the full key that the cache expects. The cache
   treats every other block as a miss.
5. The cache reads the missing blocks from object storage and stores them in the backend.
6. The cache returns a reader over the blocks, trimmed to the requested range.

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
this cost small, and the backend returns all the blocks of a read in one multi-get request.

### Every value starts with its full key

A cache key holds a hash of the object name, to stay below the key length limit of the backends. A
collision of two hashes is unlikely, but it would serve the data of one object for another.

Each value therefore starts with its full key, built from the full object name. A read compares it with
the key that it expects, and treats a mismatch as a miss. The same applies to the cached attributes of an
object. The cost is one copy of the full key, about 150 bytes, in each block of 16,000 bytes (default size).

### Misses use a bounded number of requests

The readers above the cache already merge their ranges, cap the size of each request, and read in
parallel. The cache therefore does not split the range of a read. It reads only the blocks that it does
not hold.

Blocks that are next to each other share one request. A read with scattered misses could need many
requests, so the cache limits them, as Mimir does. The default limit is three requests for the missing
blocks of one read. When the misses need more, the cache merges the runs that the fewest cached blocks
separate, until the limit holds. The cache sends the requests in parallel. Each request streams its
blocks straight into their buffers, so the cache holds no second copy of the data.

A merged request reads the cached blocks between its parts again and discards them. The cache counts
these bytes as overread. The limit trades overread bytes for requests, and the overread metric shows
whether the trade pays off. A limit of 0 turns the limit off.

### Object attributes come from the metadata cache

Every read needs the object size. Without an attributes cache, each read would send a request for it.

The cache keeps the attributes of an object: its size and its modification time. It looks for them in
three places: a small in-memory table in each querier, then the metadata cache, then the object store.
The metadata cache stores them under their own kind of entry and makes concurrent readers share one
load. An object then costs one attributes request for the whole cluster. If the deployment has no
metadata cache, the attributes go to the backend of the bucket cache.

The same entries answer a direct request for the attributes of an object. The modification time keeps
millisecond precision.

### A separate backend

The bucket cache has its own backend, apart from the metadata cache. The two caches store very
different entries. The metadata cache holds a few large entries of known shape. The bucket cache holds
many small blocks. They need different sizing, and a flood of blocks must not evict metadata.

The cache is on when a backend is configured, and off otherwise. The cache keys carry an ID of the bucket
and its prefix, so two buckets cannot collide. They also carry the kind of data, and a new kind lets us
change the layout of a value later.

### Memory for hits

A hit has to copy bytes from the cache client into a buffer. The cache takes these buffers from a pool
of large slabs, and returns them when the caller closes the reader. This avoids one allocation for each
block and lowers the work of the garbage collector.

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
- A short read never ends with a plain end-of-file. The column readers take that signal as the end
  of the column and return fewer rows without an error. The cache returns an "unexpected end" error
  instead.

### Metrics and query statistics sit at different layers

The metrics and traces of the object store client sit below the cache. They show the requests that
really reach object storage. The per-query data object statistics sit above the cache and count what
the readers asked for, including the hits.

With the cache on, the downloaded-bytes statistic of a query is therefore not the traffic of object
storage. For that traffic, use the object store metrics.

The cache has its own metrics, with the prefix `loki_dataobj_bucket_cache_`:

- the bytes that readers requested, and the bytes fetched from the cache and from the bucket;
- the overread bytes;
- the number of blocks that reads asked for, and the number that hit;
- the corrupted blocks that the cache rejected;
- the errors of lookups and stores.

## Relation to the metadata cache

The two caches work at different layers and can run together. The metadata cache sits in the data
object reader. It keeps the metadata region of an object and answers before a read leaves the reader. The
bucket cache sits below the reader and caches whatever range is left, including the metadata ranges
that the metadata cache did not hold.

## Configuration

The settings are under `dataobj.bucket_cache`, with the flag prefix `-dataobj.bucket-cache.`. They are
the usual cache backend settings plus the block size, the size of the in-memory table of object
attributes, and the limit on requests for the missing blocks of a read. The querier uses the cache. The
compactor, the builder, and the explorer read the object store directly.
