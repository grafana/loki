---
menutitle: Architecture
title: Loki architecture
description: Grafana Loki's architecture.
weight: 300
aliases:
    - ../architecture/
    - ../fundamentals/architecture/
---
# Loki architecture

## Multi-tenancy

All data, both in memory and in long-term storage, may be partitioned by a
tenant ID, pulled from the `X-Scope-OrgID` HTTP header in the request when Grafana Loki
is running in multi-tenant mode. When Loki is **not** in multi-tenant mode, the
header is ignored and the tenant ID is set to "fake", which will appear in the
index and in stored chunks.

## Chunk Format

```
  -----------------------------------------------------------------------
  |                       |                    |                        |
  |     MagicNumber(4b)   |    version(1b)     |      encoding (1b)     |
  |                       |                    |                        |
  -----------------------------------------------------------------------
  |                     #nonIndexedLabels (uvarint)                     |
  -----------------------------------------------------------------------
  |      len(label-1) (uvarint)     |          label-1 (bytes)          |
  -----------------------------------------------------------------------
  |      len(label-2) (uvarint)     |          label-2 (bytes)          |
  -----------------------------------------------------------------------
  |      len(label-n) (uvarint)     |          label-n (bytes)          |
  -----------------------------------------------------------------------
  |                     checksum(from #nonIndexedLabels)                |
  -----------------------------------------------------------------------
  |           block-1 bytes         |           checksum (4b)           |
  -----------------------------------------------------------------------
  |           block-2 bytes         |           checksum (4b)           |
  -----------------------------------------------------------------------
  |           block-n bytes         |           checksum (4b)           |
  -----------------------------------------------------------------------
  |                          #blocks (uvarint)                          |
  -----------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint)     |
  -----------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint)     |
  -----------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint)     |
  -----------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) |  offset, len (uvarint)    |
  -----------------------------------------------------------------------
  |                        checksum(from #blocks)                       |
  -----------------------------------------------------------------------
  | #nonIndexedLabels len (uvarint) | #nonIndexedLabels offset (uvarint)|
  -----------------------------------------------------------------------
  |     #blocks len (uvarint)       |     #blocks offset (uvarint)      |
  -----------------------------------------------------------------------
```

`mint` and `maxt` describe the minimum and maximum Unix nanosecond timestamp,
respectively.

The `nonIndexedLabels` section stores non-repeated strings. It is used to store label names and label values from
[non-indexed labels]({{< relref "./labels/non-indexed-labels" >}}).
Note that the labels strings and lengths within the `nonIndexedLabels` section are stored compressed.

### Block Format

A block is comprised of a series of entries, each of which is an individual log
line.

Note that the bytes of a block are stored compressed. The following is their form when uncompressed:

```
-----------------------------------------------------------------------------------------------------------------------------------------------
|  ts (varint)  |  len (uvarint)  |  log-1 bytes  |  len(from #symbols)  |  #symbols (uvarint)  |  symbol-1 (uvarint)  | symbol-n*2 (uvarint) |
-----------------------------------------------------------------------------------------------------------------------------------------------
|  ts (varint)  |  len (uvarint)  |  log-2 bytes  |  len(from #symbols)  |  #symbols (uvarint)  |  symbol-1 (uvarint)  | symbol-n*2 (uvarint) |
-----------------------------------------------------------------------------------------------------------------------------------------------
|  ts (varint)  |  len (uvarint)  |  log-3 bytes  |  len(from #symbols)  |  #symbols (uvarint)  |  symbol-1 (uvarint)  | symbol-n*2 (uvarint) |
-----------------------------------------------------------------------------------------------------------------------------------------------
|  ts (varint)  |  len (uvarint)  |  log-n bytes  |  len(from #symbols)  |  #symbols (uvarint)  |  symbol-1 (uvarint)  | symbol-n*2 (uvarint) |
-----------------------------------------------------------------------------------------------------------------------------------------------
```

`ts` is the Unix nanosecond timestamp of the logs, while len is the length in
bytes of the log entry.

Symbols store references to the actual strings containing label names and values in the
`nonIndexedLabels` section of the chunk.

## Storage

### Single Store

Loki stores all data in a single object storage backend. This mode of operation became generally available with Loki 2.0 and is fast, cost-effective, and simple, not to mention where all current and future development lies. This mode uses an adapter called [`boltdb_shipper`]({{< relref "../operations/storage/boltdb-shipper" >}}) to store the `index` in object storage (the same way we store `chunks`).

## Read Path

To summarize, the read path works as follows:

1. The querier receives an HTTP/1 request for data.
1. The querier passes the query to all ingesters for in-memory data.
1. The ingesters receive the read request and return data matching the query, if
   any.
1. The querier lazily loads data from the backing store and runs the query
   against it if no ingesters returned data.
1. The querier iterates over all received data and deduplicates, returning a
   final set of data over the HTTP/1 connection.

## Write Path

![chunk_diagram](../chunks_diagram.png "Chunk diagram")

To summarize, the write path works as follows:

1. The distributor receives an HTTP/1 request to store data for streams.
1. Each stream is hashed using the hash ring.
1. The distributor sends each stream to the appropriate ingesters and their
   replicas (based on the configured replication factor).
1. Each ingester will create a chunk or append to an existing chunk for the
   stream's data. A chunk is unique per tenant and per labelset.
1. The distributor responds with a success code over the HTTP/1 connection.
