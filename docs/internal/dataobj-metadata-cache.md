# Caching the metadata of data objects

This document describes how queriers cache the metadata of data objects, and why the caches work the
way they do. Data objects and the metadata prefetcher are experimental.

## Purpose

A query opens a data object before it reads any section. To open it, the querier reads the metadata:
the header, the file metadata that lists the sections, and the metadata of the sections and columns
that the query reads. Without a cache, a cold open needs several object storage reads, one after the
other. A query opens many objects, so this latency adds up.

Metadata is small (compared to the whole object size) and an object never changes after it is written.
Every query that touches an object needs nearly the same bytes. So queriers keep the metadata in a cache that
they share. A querier that starts cold, or opens an object for the first time, finds the metadata that
another querier cached. The cache needs no invalidation: compaction writes new objects, and it never
edits old ones.

## Which cache serves what

| Object | Served by |
|---|---|
| Index objects | the metadata cache |
| Log objects | the metadata cache, or the metadata prefetcher when it is enabled |

The prefetcher is opt-in. When it is enabled, it takes over the log objects, and the metadata cache does
not serve them any more. A querier never uses both for one object. When the prefetcher cannot serve a log
object, the querier reads the metadata directly from object storage.

Both store their items in the same cache backend, for example memcached, under different keys.

## The metadata cache

The metadata cache keeps one cache item for each object, under the path of the object. A lookup reads one
key. On a miss, the querier reads the object from object storage and stores the item. Concurrent misses
for one object in one querier share one read, and the read has a time limit. A cache that fails costs
time and nothing more: the querier reads the object directly. The cache does not store an item above the
item size limit of the backend.

### Index objects

The cache holds the metadata region of an index object, or the whole object when it is tiny. An index
object has few sections with a fixed schema, its metadata is tens of kilobytes, and a query reads whole
sections. So one item for the object is the right shape. A hit replaces the first read of the open. A
tiny object is kept whole, so a warm open reads nothing from object storage.

### Log objects, when the prefetcher is off

A writer places the streams section before the logs sections. The cache holds the object from its start
to the end of the metadata of the last section that is not a logs section. So the item holds the file
metadata and the metadata of the streams section, and it is small. A tiny object is kept whole.

The item leaves out the metadata of the logs sections on purpose, because it grows with the number of
columns. This shape does not fit log objects well:

- The metadata of the logs sections is the part that grows and that a query needs, and it is not in the
  item. Every query reads it from object storage.
- One item for that metadata would not help. Every query would fetch the columns that it does not
  read.

The prefetcher exists to fix this.

## The metadata prefetcher

The prefetcher takes over the metadata cache for log objects. It applies to the log objects that a querier
opens to serve a stream-first metric query from data objects. In this section, "the cache" means the
cache items of the prefetcher.

### What the cache holds

The metadata region of an object is the part from its start to the end of the metadata of all its
sections.

```
| header + file metadata | section metadata ... | section data ... |
|<-------------- metadata region -------------->| not cached
```

The cache does not hold section data. A section can hold a lot of it, and it would push the metadata
out of the cache. Metadata is the part that every query reuses.

The cache holds the region in two shapes:

- **Groups.** Each group is a small cache item. The groups are:
  - The file metadata. It is a group of its own, because a query that needs all the metadata reads
    the file metadata and the whole region, and it does not need the core metadata.
  - The core metadata that every query needs: the stream ID and timestamp columns, and the non-log
    sections.
  - The log line column.
  - One group for each structured metadata column.
- **The whole region.** One cache item holds all of it.

The cache stores both shapes, so an object takes about twice the memory of its region. The second copy
lets a miss on one group read the region from the cache, and not from object storage.

A query tells the cache which metadata it needs, and the cache returns only the groups for it. A query
that filters on one structured metadata column then prefetches a few kilobytes, and not the whole
region. The whole region serves the queries that need all the metadata, and it is the source for the
groups.

### How a query reads metadata

```mermaid
flowchart TD
    open["Query opens an object"] --> lookup["One cache lookup for the groups the query needs"]
    lookup -->|"all groups found"| done["Use the cached metadata"]
    lookup -->|"some groups missing"| load["Load the whole region"]
    lookup -->|"lookup failed"| direct["Read the metadata directly from object storage"]
    load -->|"region too large, layout not supported, or read failed"| direct
    load --> fill["Write the missing groups to the cache"]
    fill --> done
```

- **Load the whole region on a miss.** One or two large reads cost less than the many small reads of
  a direct open. The load writes the groups that the query missed. When the cache does not know the
  object at all, it writes every group, so a later query that needs other metadata of the same object
  also hits.
- **Never fail because of the cache.** Every path that cannot use the cache ends in the direct read,
  as if there were no cache. After a failed lookup, the query goes straight to the direct read.
  Loading the whole region would cost more, and a failed cache cannot take the fill.

#### Loading the whole region

```mermaid
flowchart LR
    load["Load the region"] --> cache["Read the region item from the cache"]
    load --> storage["Read the region from object storage"]
    cache -->|"answers first with a hit"| hit["Return the cached region<br/>Cancel the storage read"]
    storage -->|"answers first, or the cache read misses or fails"| miss["Return the region<br/>Write it to the cache"]
```

The load does not know if the cache holds the region, so it reads from both at the same time. The
first answer wins. We race the request to the cache and storage because:

- Asking the cache first, and object storage after (sequentially), would add a round trip to every miss. Racing the
  two requests avoid the 2x round trip on a cache miss.
- The storage read also protects the query from a slow cache.

The price is a storage read that a cache hit makes unnecessary. The price is small. The load is the
last resort, after a group miss. It leaves the groups in the cache, so the next request for the object
stops at the groups. The load also cancels the storage read when the cache answers first.

### Size limit

The cache skips an object whose metadata region is above a limit. Moving a very large region costs
more than the direct read saves. A cache item also cannot be larger than the item size limit of the
cache backend. Loki checks at startup that the limit fits in a cache item.

A querier writes nothing to the cache for an object above its limit, so its lookup for the object
misses. The load that follows reads the head of the object only to find that the region is too large.
So each querier remembers the region size of the objects that it opened, in a cache in memory. For an
object above the limit, the prefetcher then fails before it asks the cache, and the query reads the
metadata directly.

A known size also helps an object within the limit. When the cache has no file metadata item for it,
the load reads the region with one read. Without the size, it reads the head first, and then the rest
when the region is larger than the head.

An object never changes, so an entry of the cache never goes stale. The cache has a limit in bytes
(`-dataobj.metadata-prefetcher.max-region-size-cache-bytes`). A read of an entry, and a write of a known
path, count as a use. When the cache is full, it drops the entry that was used least recently. It also
drops every entry that was not used for 24 hours. That drop runs on a read or a write, at most once a
minute, so an idle querier keeps its entries.

A querier remembers a size above the limit only when its own load read the object and found the size. A
cache hit for an object that another querier cached, with a larger limit, stays a hit.

### Cache items

A cache item has these properties:

- **Collision resistant.** The cache stores an item under a hash of its key, because backends limit
  the length of a key. The item also holds the full key, so a reader detects a hash collision.
- **Corruption resistant.** A checksum detects a truncated or corrupt item.
- **Versioned.** Old and new queriers share the cache during a rollout, and the layout of an item can
  change between them.
- **Compressed.** Metadata compresses well, which saves cache memory and network bandwidth. Small items
  and items that compress poorly stay uncompressed, so the querier spends no CPU for no gain.

A reader treats an item that fails a check as a miss, and the next write replaces it. A wrong or
damaged cache costs time, and it cannot change the result of a query.
