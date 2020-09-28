## Impetus

Loki already takes numerous steps to ensure the persistence of log data, most notably the use of a configurable replication factor (redundancy) in the ingesters. However, this still leaves much to be desired in persistence guarantees, especially for single binary deployments. This proposal outlines a write ahead log (WAL) in order to compliment existing measures by allowing storage/replay of incoming writes via local disk on the ingesters.

## Strategy

We suggest a two pass WAL implementation which includes an intial recording of accepted writes (`segments`) and a subsequent checkpointing (`checkpoints`) which coalesces the first pass into more efficient representations to speed up replaying.

### Segments

Segments are the first pass and most basic WAL. They store individual records of incoming writes that have been accepted and can be used to reconstruct the in memory state of an ingester without any external input. They are sequentially named on disk and are automatically created when a target size is hit as follows:

```
data
└── wal
   ├── 000000
   ├── 000001
   └── 000002
```

### Truncation

In order to prevent unbounded growth and remove flushed operations from the WAL, it is regularly truncated and all but the last segment (which is currently active) are deleted at a configurable interval (`ingester.checkpoint-duration`) or after a combined max segment size is reached (`ingester.checkpoint-threshold`), whichever comes first. This is where checkpoints come into the picture.

### Checkpoints

Before truncating the WAL, we advance the WAL segments by one in order to ensure we don't delete the currently writing segment. The directory will look like:

```
data
└── wal
   ├── 000000
   ├── 000001
   ├── 000002 <- likely not full, no matter
   └── 000003 <- newly written, empty
```

In order to be adaptive in the face of dynamic throughputs, the ingester will calculate the expected time until the next checkpointing operation is performed. We suggest a simplistic approach first: the duration before the current checkpointing operation was requested, which will be a maximum of `ingester.checkpoint-duration` or less if the `ingester.checkpoint-threshold` was triggered. We'll call this our `checkpoint_duration`. Then, to give some wiggle room in case we've overestimated how much time we have until the next checkpoint, we'll reduce this (say to 90%). This `checkpoint_duration` will be used to amortize writes to disk to avoid IOPs burst.

Then, each in memory stream is iterated every across an interval, calculated by `checkpoint_duration / in_memory_streams` and written to the checkpoint. After the checkpoint completes, it is moved from it's temp directory to the `ingester.wal-dir`, taking the name of the last segment before it started (`checkpoint.000003`) and then all applicable segments (`00000`, `00001`, `00002`) and any previous checkpoint are deleted.

#### Queueing Checkpoint operations

Since checkpoints are created at dynamic intervals, it's possible that one operation will start at the same time another is running. In this case, the existing checkpoint operation should disregard it's internal ticker and flush it's series as fast as is possible. Afterwords, the next checkpoint operation can begin. This will likely create a localized spike in IOPS before the amortization of the following checkpoint operation takes over and is another important reason to run the WAL on an isolated disk in order to mitigate noisy neighbor problems.

### WAL Record Types

#### Streams

A `Stream` record type is written when an ingester receives a push for a series it doesn't yet have in memory.

#### Logs

A `Logs` record type is written when an ingester receives a push, containing the fingerprint of the series it refers to and a list of `(timestamp, log_line)` tuples, _after_ a `Stream` record type is written, if applicable.

### Restoration

Replaying a WAL is done by loading any available checkpoints into memory and then replaying any operations from segments on top. It's likely some of these operations will fail because they're already included in the checkpoint (due to delay introduced in our amortizations), but this is ok -- we won't _lose_ any data, only try to write some data twice, which we'll ignore.

### Deployment

Introduction of the WAL requires that ingesters have persistent disks which are reconnected across restarts (this is a good fit for StatefulSets in Kubernetes). Additionally, it's recommended that the WAL uses an independent disk such that it's isolated from being affected by or causing noisy neighbor problems, especially during any IOPS spike(s).

### Questions
- Can we use underlying prometheus wal pkg when possible for consistency & to mitigate undifferentiated heavy lifting. Interfaces handle page alignment & use []byte.
- Make ingester series data impl proto.Message in order to generate byte conversions for disk loading?
- how to handle `ingester.retain-period` which is helpful b/c queries cache index lookups for 5min
  - could we instead only cache index lookups for periods older than `querier.query-ingesters-within`?
  - otherwise can be done
- Will the prometheus wal library handle arbitrarily long records?

### Alternatives

#### Use the Cortex WAL

Attractive in a specious way, the Cortex WAL seems unsuited for use in Loki due to revolving around _time_. Creating segments & checkpoints based on elapsed time suits Prometheus data where throughput is uniform and calculable via the `scrape_interval`, but unpredictable stream throughtput in Loki could result in problematic segment/checkpoint sizing. Thus a more dynamic approach is needed.

Additionally, it would be a less efficient approach which wouldn't take advantage of the block compressions that already occurs in the ingesters.

#### Don't build checkpoints from memory, instead write new WAL elements

Instead of building checkpoints from memory, this would build the same efficiencies into two distinct WAL Record types: `Blocks` and `FlushedChunks`. The former is a record type which willl contain an entire compressed block after it's cut and the latter will contain an entire chunk + the sequence of blocks it holds when it's flushed. This may offer good enough amortization of writes because block cuts are assumed to be evenly distributed & chunk flushes have the same property and use jitter for synchronization.

This would allow building checkpoints without relying on an ingester's internal state, but would likely require multiple WALs, partitioned by record type in order to be able to iterate all `FlushedChunks` -> `Blocks` -> `Series` -> `Samples` such that we could no-op the later (lesser priority) types that are superseded by the former types. The benefits do not seem worth the cost here, especially considering the simpler suggested alternative and the extensibility costs if we need to add new record types if/when the ingester changes internally.
