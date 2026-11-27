# pkg/chunkbuilder/

The chunk-builder is an experimental target. Each instance consumes one partition of a Kafka topic, and will turn it into chunks and a TSDB index in object storage. Today it only consumes and decodes records.

## Public API

- `Config` (`config.go`) has these flags:
  - `-chunk-builder.enabled` (default false);
  - `-chunk-builder.topic`, which is required;
  - `-chunk-builder.partition-id` (default -1, which means the hostname's ordinal);
  - `-chunk-builder.consumer-group` (default `chunk-builder`, `DefaultConsumerGroup`).

  `Validate` checks every field. Loki calls it only when the chunk-builder is enabled, because the topic has no default.
- `New(kafkaCfg, cfg, reg, logger)` (`service.go`) validates both configs, resolves the partition, and builds the Kafka client, the consumer and the processor. It returns a `*Service`, which is a dskit service.
- Loki wiring is in `pkg/loki`. The `chunk-builder` target is a `UserInvisibleTargetableModule` that depends only on `Server`. `initChunkBuilder` fails when the target is selected without `-chunk-builder.enabled`. No other module depends on it, so selecting it without the flag must be a mistake.

## Architecture

```
partition N ──► kafkav2.SinglePartitionConsumer ──records (unbuffered)──► Service.running ──► processor.process
               own kgo client, no topic auto-creation                   one goroutine        decode + metrics
```

- **One partition per instance.** `resolvePartitionID` uses `-chunk-builder.partition-id` if it is 0 or more. Otherwise it takes the `-N` ordinal suffix of the hostname, so `chunk-builder-3` consumes partition 3. A hostname without a suffix fails the start. It deliberately does not use `partitionring.ExtractPartitionID`, which maps any hostname that contains `local` or ends in `.lan` to partition 0.
- **Kafka client.** `client.NewReaderClient` gets the shared `kafka_config` with two changes:
  - the topic is replaced by `chunk_builder.topic`;
  - topic auto-creation is off. Otherwise a mistyped topic would be created and consumed silently, and every start would change the default partition count of the whole Kafka cluster.

  Loki validates `kafka_config` only when the ingesters consume Kafka, so `New` validates it with the chunk-builder's topic.
- **Resume offset.** `initResumeOffset` runs before the consumer starts, with three retries and backoff:
  1. `EndOffsets` checks that the topic has the partition. Without this check, a missing topic or partition would make the consumer wait forever without an error.
  2. `ResumeOffset` returns the consumer group's committed offset plus one, or the start of the partition when the group has none.
- **Decoding.** `kafka.Decoder.Decode` reads both record formats, the flat `logproto.Stream` and the nested `logproto.InternalStreamAdapter`. It parses the labels and caches them in an LRU.
  - A record fails to decode if it has no key (the key holds the tenant), holds data in neither format, has labels that do not parse, or has no labels. An empty record decodes to a stream with no labels.
  - A failed record is counted in `loki_chunk_builder_decode_failures_total`, logged at a limited rate (1 per second, burst 5) and skipped, because reading it again cannot fix it.
- **Metrics, and nothing else.**
  - The processor exports `loki_chunk_builder_records_total`, `received_bytes_total`, `decode_failures_total`, `last_offset` and `consumption_lag_seconds`.
  - The consumer exports `loki_chunk_builder_single_partition_consumer_fetch_errors_total` and `polls_total`, both with `topic` and `partition` labels.
  - `consumption_lag_seconds` is the lag of the last consumed record. It does not change while no records arrive, so it cannot detect a stall; alert on fetch errors for that.
- **No offset commits.** See invariant 1.

## Lifecycle

- `starting` sets the resume offset, then starts the consumer. If either step fails, it closes the watcher and the Kafka client.
- `running` is one loop that waits on the context, the `FailureWatcher` and the records channel, and processes each record. The consumer closes the channel only when it stops, and only `stopping` stops it. So if the channel closes outside a shutdown, the service fails:
  - with the watcher's error, which names the consumer, if the consumer failed;
  - otherwise with "the consumer stopped unexpectedly".
- `stopping` stops the consumer unless it already failed, then closes the watcher and the Kafka client. Closing the watcher drains any failure that was never read. The watcher's channel is unbuffered, and `FailureWatcher.Close` waits for the listener, so an unread failure would block the shutdown forever.

The consumer's context comes from the service's context, so a shutdown also stops the consumer and closes the channel. `consumerStopped` checks the context first, so that it does not report a shutdown as a failure.

## Invariants

These must never be broken. Breaking one loses data or builds it twice.

### 1. No offset commit until the records' chunks and index are durable

The offset committed to the consumer group is where the next start resumes. If an offset is committed before the chunks and index for its records are in object storage, those records count as consumed while no chunk holds them, and a restart skips them for good.

The commit point will be a checkpoint, written with compare-and-set. It holds the offset, a snapshot of the open chunks and the paths of the staged index files. The Kafka commit that follows it only feeds lag metrics.

This package does not build chunks yet, so it commits nothing: a test asserts that the group has no committed offset. Until checkpoints exist, every start consumes its partition from the beginning.

### 2. Exactly one chunk-builder per partition

Configuration picks each partition's owner. A consumer-group rebalance does not, because moving a partition between instances would split each stream's chunks between them.

- Two owners of a partition would build the same chunks twice and race on its checkpoint.
- A partition with no owner is never consumed.

This is why hostnames have no fallback to partition 0 and why the partition must exist in the topic. It also means the StatefulSet must have as many replicas as the topic has partitions. Nothing else fences a second owner yet. The checkpoint's compare-and-set will reject a second writer.

### 3. No limits, no WAL, no reads

- **No limits.** The write path already enforced them when it accepted the data, so a limit here would drop accepted data.
- **No WAL.** Kafka is the log, so a restart replays from the checkpoint instead of a local write-ahead log.
- **No reads.** Queriers read what the chunk-builder uploads.

Adding any of these would tie the chunk-builder to the ingester's operational model, which it exists to avoid. Serving reads in particular needs a ring and makes every instance latency-critical.

## Design rules for upcoming code

- **Cut and finalize decisions depend only on the data, never on the wall clock.** A chunk is finalized when it is full or idle. Idle is measured in consumed bytes or offsets, never in time. A restart from the same checkpoint then rebuilds identical chunks under identical keys, so replaying a range overwrites chunks instead of duplicating them. A flush can still run on a timer, because it does not change what a chunk holds.
- **Chunks stay open across flushes.** A flush snapshots the open chunks into the checkpoint instead of cutting them, so the flush interval does not cap chunk size. Chunks are kept per stream and per aligned data-time window.
- **Each flush rotates the TSDB head into one multi-tenant file per daily table.** The files use the ingester's naming, `<unix-seconds>-<node>.tsdb.gz`, and go to a staging prefix. A separate compactor later splits them per tenant and publishes them.

### Why not offset-range jobs

Loki once had a block-builder that built chunks from jobs, each covering a range of offsets. It was added in #14621 and removed in #19234. Each job ended by cutting a partial chunk for every stream it touched. A stream therefore got at least one chunk per job, and chunks got smaller as jobs got smaller. The chunk-builder instead owns its partition continuously and keeps chunks open across flushes. The block-builder's code for writing chunks and indexes (`pkg/blockbuilder` before #19234) may still be worth reusing.

## Known gaps

- The resume offset gets three retries, well under a second in total. On a new broker whose group coordinator is not ready yet, the start fails with `COORDINATOR_NOT_AVAILABLE` and relies on the orchestrator to restart it.
- There is no offset-lag metric yet, and `consumption_lag_seconds` cannot show a stall.
- Without commits, every restart consumes the whole partition again.

## Stack status

The chunk-builders land as a stack of PRs. Every PR in the stack updates this file in the same change, so it always describes the code. Each new package in the stack, such as the chunk-builders-compactor, gets its own `AGENTS.md` and `CLAUDE.md` symlink the same way.

| PR | Status | Adds |
|----|--------|------|
| 01 scaffolding | this code | The `chunk-builder` target. It consumes and decodes one partition and only updates metrics. It commits no offsets. |
| 02 chunks | planned | Groups decoded streams per tenant and stream, reusing the ingester's stream and chunk code. Keeps chunks open per stream and aligned time window, finalizes them when full or idle (in consumed bytes) and uploads them. |
| 03 TSDB head | planned | Adds finalized chunk refs to an in-memory TSDB head. Each flush rotates the head into one multi-tenant file per daily table and uploads it to a staging prefix. |
| 04 checkpoints | planned | Snapshots the open chunks and writes the checkpoint (offset, snapshot, staged paths) with compare-and-set as the commit point, then commits the Kafka offset. |
| 05 compactor | planned | Adds a chunk-builders-compactor target. It merges staged files per tenant and table, publishes per-tenant files into the live tables, applies processed deletes and bumps cache generations. |
| 06 replay writer | planned | The distributor writes replayed data to the replay topic with `kafka.EncodeWithTopic`. |
