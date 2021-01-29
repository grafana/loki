## Metadata

Author: Owen Diehl - [owen-d](https://github.com/owen-d) ([Grafana Labs](https://grafana.com/))

Date: 28/01/2021

## Problem

Loki imposes an ordering constraint on ingested data; that is to say incoming data must have monotonically increasing timestamps, partitioned by stream. This has historical inertia from our parent project, Cortex, but presents unintended consequences specific to log ingestion. In contrast to metric scraping, Loki has reasonable use cases where the ordering constraint poses a problem, including:

- Ingesting logs from a cloud function without feeling pressured to add high cardinality labels like invocation_id to avoid out of order errors.
- Ingesting logs from other agents/mechanisms that don’t take into account Loki’s ordering constraint. For instance, fluent{d,bit} variants may batch and retry writes independently of other batches, causing unpredictable log loss via out of order errors.
- Importing old data and new data concurrently.

Many of these illustrate the adversity between _ordering_ and _cardinality_. In addition to enabling some previously difficult/impossible use cases, removing the ordering constraint lets us avoid potential conflict between these two concepts and helps incentivize good practice in the form of fewer useful labels.

### Requirements

- Enable out of order writes
- Maintain query interface parity

#### Bonuses

- Optimize for in order writes


## Alternatives

- Implement order-agnostic blocks + increase memory usage by compression_ratio: Deemed unacceptable due to TCO (total cost of ownership).
- Implement order-agnostic blocks + scale horizontally (reduce per-ingester streams): Deemed unacceptable due to TCO and increasing ring pressure.
- Implement order-agnostic blocks + flush chunks more frequently: Deemed unacceptable due to negatively increasing the index and the number of chunks requiring merge during reads.

## Design

### Background

I suggest allowing a stream's head block to accept unordered writes and re-ordering cut blocks similar to merge-sort before flushing to storage. Currently, writes are accepted in monotonically increasing timestamp order to a _headBlock_, which is occasionally "cut" into a compressed, immutable _block_. In turn, these _blocks_ are combined into a _chunk_ and persisted to storage.

```
Figure 1

    Blocks                    Head

--------------           ----------------
|    blocks  |--         |  head block  |
|(compressed)| |         |(uncompressed)|
|            | | ------> |              |
|            | |         |              |
-------------- |         ----------------
  |            |
  --------------
```

Historically because of Loki's ordering constraint, these blocks maintain a monotonically increasing timestamp (abbreviated `ts`) order where

```
Figure 2

start       end
ts0         ts1          ts2        ts3
--------------           --------------
|            |           |            |
|            | --------> |            |
|            |           |            |
|            |           |            |
--------------           --------------
```

This allows us two optimzations:

1) We can store much more data in memory because each block is compressed after being cut from a head chunk.
2) We can query the block's metadata, such as `ts0` and `ts1` and skip querying it in the case of i.e. the timestamps are outside a request's bounds.

### Unordered Head Blocks

The head block's internal structure will be replaced with a tree structure, enabling logarithmic inserts/lookups and `n log(n)` scans. _Cutting_ a block from the headchunk will iterate through this tree, creating a sorted block identical to the ones currently in use. However, because we'll be accepting arbitrarily-ordered writes, there will no longer be any guaranteed inter-block order. In contrast to figure 2, blocks may have overlapping data:

```
Figure 3

start       end
ts1         ts3          ts0        ts2
--------------           --------------
|            |           |            |
|            | --------> |            |
|            |           |            |
|            |           |            |
--------------           --------------
```

Thus _all_ blocks must have their metadata checked against a query. In this example, a query for the bounds `[ts1,ts2]`  would need to decompress and scan the `[ts1, ts2]` range across them, but a query against `[ts3, ts4]` would only decompress & scan _one_ block.

```
Figure 4

     chunk1
-------------------
                chunk2
         ---------------------
ts0     ts1      ts2       ts3        ts4 (not in any block)
------------------------------
|        |        |          |
|        |        |          |
|        |        |          |
|        |        |          |
------------------------------

```

The performance losses against the current approach includes:

1) Appending a log line is now performed in logarithmic time instead of amortized (due to array resizing) constant time.
2) Blocks may contain overlapping data (although intra-block ordering is still guaranteed).
3) Head block scans are now `O(n log(n))` instead of `O(n)`

### Flushing & Chunk Creation

Loki regularly combines multiple blocks into a chunk and "flushes** it to storage. In order to ensure that reads over flushed chunks remain as performant as possible, we will re-order a possibly-overlapping set of blocks into a set of blocks that maintain monotonically increasing order between them.

**Note: In the case that data for a stream is ingested in order, this is effectively a no-op, making it well optimized for in-order writes (which is both the requirement and default in Loki currently). Thus, this should have little to no performance impact on ordered data while enabling Loki to ingest unordered data.**

#### Chunk Durations

When `--validation.reject-old-samples` is enabled, Loki accepts incoming timestamps within the range [now() - `--validation.reject-old-samples.max-age`, now() + `--validation.create-grace-period`]. For most of our clusters, this would mean the range of acceptable data is one week long. In contrast, our max chunk age is `2h`. Allowing unordered writes would mean that ingesters would willingly receive data for 168h, or up to 84 distinct chunk lengths. This presents a problem: a malicious user could be writing to many (84 in this case) distinct chunks simultaneously, flooding Loki with underutilized chunks which bloat the index.

In order to mitigate this, there are a few options (not mutually exclusive):
1) Lower the valid acceptance range
2) Create an _active_ validity window, such as `[most_recent_sample-max_chunk_age, now() + creation_grace_period]`.

The first option is simple, already available, and likely somewhat reasonable.
The second is simple to implement and an effective way to ensure Loki can ingest unordered logs but maintain a sliding validity window. I expect this to cover nearly all reasonable use cases and effectively mitigate bad actors.

#### Chunk Synchronization

Currently (with the ordering constraint), we cut chunks according to `sync_period` as well. The first timestamp past this bound will trigger a cut. This process aids in increasing chunk determinism and therefore our deduplication ratio in object storage because chunks are [content addressed](https://en.wikipedia.org/wiki/Content-addressable_storage). With the removal of our ordering constraint, it's possible that in some cases the synchronization method will not be as effective, such as during concurrent writes to the same stream across this bound.

**Note: It's important to mention that this is possible today with the current ordering constraint as well, but we'll be incentivizing it more by removing the odering constraint**

```
Figure 5

       Concurrent Writes over threshold
                   ^ ^
                   | |
                   | |
-----------------|-----------------
                 |
                 v
             Sync Marker
```


To mitigate this problem and preserve the benefits of chunk deduplication, we'll need to make chunk synchronization cutting less susceptible to non-determinism during concurrent writes. To do this, we can move the synchronization trigger from the `Append` code path to the asynchronous `FlushLoop`. Note, the semantics for _when_ a chunk is cut will not change: that is, on the first timestamp crossing the synchronization bound.



### Future Opportunities

#### Variance Budget

The intended approach of validating a "sliding validity" window for each stream is simple and effective at preventing misuse & bad actors from writing across the entire acceptable range for incoming timestamps. However, we may in the future wish to take a more sophisticated approach, introducing per tenant "variance" budgets, likely derived from the stream limit. This ingester limit could, for example use an incremental (online) standard deviation/variance algorithm such as [Welford's](https://en.wikipedia.org/wiki/Algorithms_for_calculating_variance), which would allow writing to larger ranges than option (2) in the _Chunk Durations_ section.

#### LSM Tree

memory -> disk
possibly more effective chunks?
larger acceptable time range?
