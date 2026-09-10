# target/builder/

Streaming Kafka consumer that builds partial n-gram indexes. This is the most complex and performance-sensitive component.

## Architecture: flat buffer, deferred bucketing

Ingest appends `(ngram [8]byte, docID uint32)` pairs into ONE flat SoA buffer (`postingsBuffer`). `docID` is an **epoch tick**: `absDocBucket - epochBucket`, document buckets counted from the FIXED `docIDEpoch` — 2026-01-01T00:00Z, the earliest Grafana Adaptive Logs Archive date, which Archive/Replay can legitimately replay, so the epoch must never move past it (a per-cycle derived epoch of now − 365d was tried and reverted for exactly that reason; `docid_window.go`). All docID/date/doc-metadata math is in absolute buckets (baseBucket + tick), so `.lidx` output is epoch-invariant. The epoch's midnight-UTC alignment keeps the epoch bucket an exact multiple of `ticksPerDay`, which the merge's day/date math relies on. Date and shard are both recoverable from the pair itself, so all (date, shard) bucketing is deferred to flush. On buffer fill (`postings_buffer_pairs` pairs, default 20M) the fresh tail is radix-sorted, deduped, and 2-way merged into a sorted head ("incremental fill"); when the deduped head crosses the spill watermark (`postings_spill_watermark`, default 0.70), it is spilled to scratch disk as a sorted **run** (shard-contiguous records, one independently seekable s2 stream per shard — see `run_file.go`). At flush (`prepareIndexes`), runs are k-way merged shard-by-shard directly into per-(date,shard) `.lidx` files (`merge.go`). Referenced-tick bitsets per (shard, day), recorded at spill, seed each writer's document metadata and the epoch-tick-docID → dense-rank remap.

Ingest memory does not depend on shard/date count (no per-(date,shard) accumulators), but the resident working set is the full CAPACITY of the four sort buffers — keys/docs plus the keyBuf/docBuf radix scratch, ~24 B per `postings_buffer_pairs` pair (12 B/pair live + 12 B/pair scratch, ~460 MiB at the default 20M), allocated up front — plus the `refTicks` bitsets. During a builder swap the retiring and fresh builders briefly coexist, so budget ~2× that. `estimatedMemoryBytes` reports this capacity-based figure (buffer caps + the lazily-allocated shard-reorder scratch at 1 B/pair + a running refTicks byte counter) via the `logline_index_builder_estimated_memory_bytes` gauge, feeding the GOMEMLIMIT-fraction flush trigger; the buffer term drops to zero after `releaseSortBuffers` (merge phase). The merge phase itself transiently holds per-(shard, day) rank maps plus doc slices for all of a shard's open day-writers — hundreds of MB on dense days — outside the memory gauge; this is the same accounting scope the old builder had, listed here for honesty. `refTicks` couples to shardCount × active days: each (shard, day) touched allocates a dense tick bitset of `ticksPerDay/8` bytes (108 KB at 100 ms) on first touch — a few MB at ≤32 shards, ~190 MB at 256 shards × 7 days. Note client-supplied FUTURE timestamps inside the docID window (accepted up to epoch + 2^32 ticks — 2039-08-12 at the default 100 ms interval) each allocate these bitsets on first touch too, so adversarial timestamp scatter is a memory-amplification vector, bounded upstream by distributor timestamp validation and by the window end itself. A sparse refTicks representation is a known future optimization, deliberately deferred to keep the validated structure.

`.lidx` meta time ranges (`MinLogTs`/`MaxLogTs`, `MinRecordTs`/`MaxRecordTs`) are the per-DATE superset shared by all of that date's shards, not per-(date,shard) spans: shard membership is a per-ngram property resolved only at merge, so ingest tracks ranges per date (`dateRanges`). Conservative for query narrowing — a superset can never cause a miss, only extra file scans.

## Invariants

These must never be broken. Violating any causes data loss or corruption.

### 1. At-least-once: upload before commit

```
prepare .lidx files → upload to object storage → commit Kafka offsets → clear state
```

Never reorder. If upload fails, retry until success. Never commit offsets for data that hasn't been uploaded. Decode errors fail the service (`processRecordBatch` returns the error from `running()` and `decode_errors_total` is incremented before the failure) — the orchestrator restarts the pod and the same offset is re-consumed, so the failure stays loud until upstream is fixed.

**Exception: zero-file flush path.** When `prepareIndexes` returns no files (all consumed data was dropped by `minDate`, or extracted zero n-grams), there is nothing to upload. Kafka offsets are still committed before `clear()` — the invariant is vacuously satisfied because there is no data to make durable, and the commit stops dropped records from being re-consumed on restart.

**Sub-invariant: zero files ⇒ zero indexed pairs.** The exception is only safe because `prepareIndexes` first calls `finish()`, which spills every buffered pair as a run, and returns zero files exactly when `runPaths` is empty. So "zero files" reliably means "no pair was ever appended this cycle" — every consumed line was deliberately dropped (and counted by `droppedLinesPreMinDate`) or contributed no n-grams. If you ever add a path that drains or discards buffered pairs or runs outside `clear()`, committing offsets on the zero-file path will silently lose data. This is also why out-of-window timestamps PANIC instead of being dropped (invariant #7): a drop here plus the zero-file offset commit would be a permanent, silent data skip.

### 2. Flush is atomic across the whole cycle

The full sequence in `flushAndCommit` / `executeFlush`:

1. `builder.prepareIndexes()` — finish the buffer, merge runs into per-(date,shard) .lidx files
2. `uploadPartialIndexes()` — upload all files via `store.Store` (parallel, with retry)
3. `commitOffsets()` — commit Kafka offsets
4. `clear()` — reset all builder state, remove the scratch subdir

If any step fails, retry that step (unbounded, with backoff). Never skip ahead. Never clear without committing. Known gap: `prepareIndexes` re-reads scratch runs, so a truncated/corrupt run fails identically on every attempt and the flush goroutine retries forever (and, via backpressure, eventually stalls the poll loop) until the pod is restarted — no data loss, but no forward progress. Fail-fast handling (bounded prepare retries → self-healing restart) is a backlog item.

### 3. Consumer-group ownership — at-least-once across rebalances

Partitions are assigned by the Kafka consumer-group coordinator (`kgo.ConsumerGroup` + `kgo.ConsumeTopics`). Scaling the StatefulSet up or down is safe — the coordinator rebalances ownership; no config edit is required. Two settings shape rebalance disruption:

- `kgo.InstanceID(cfg.InstanceID)` — Kafka static membership. A pod that restarts within `cfg.KafkaSessionTimeout` (default 2 min, configurable via `logline-index-builder.kafka-session-timeout`) reclaims its partitions without triggering a group-wide rebalance, so rolling deploys and crash loops do not churn partitions. The broker's `group.max.session.timeout.ms` must be at least this value. Tests use `cfg.disableStaticMembership = true` because `kfake` rejects `InstanceID`.
- `kgo.Balancers(kgo.CooperativeStickyBalancer())` — only the minimum set of partitions moves on each membership change; the rest stay put.

**At-least-once is preserved by the same upload-before-commit ordering as invariant #1.** Steady-state ownership is exclusive. During a rebalance a partition may briefly be consumed by both the old and new owner — the old owner's pending records already in memory are still flushed and uploaded, but offset commits for revoked partitions are abandoned (see fencing below). The new owner re-processes the overlap from the last successful commit, producing duplicate `.lidx` files with fresh `storageID`s. Compaction downstream collapses these.

**Zombie-commit fencing.** The classic Kafka protocol fences `OffsetCommit` by member ID + generation only — not partition ownership. A background flush that still holds a revoked partition in its offset snapshot can therefore rewind the new owner's committed offset. We fence this client-side:

1. `onPartitionsLostOrRevoked` strips revoked partitions from `lastConsumedOffsets` and from `pendingFlushOffsets` (the in-flight `executeFlush` snapshot) under `builderMtx`, which is shared with the commit path. The revoke callback blocks the rebalance and franz-go serialises `CommitOffsetsSync` against join/sync, so a commit issued before revoke returns still legitimately owns the partition, and a commit issued after can no longer contain it.
2. `commitOffsets` filters to `snapshotOwnedPartitions()` immediately before `CommitOffsetsSync` (defense in depth). The filter is load-bearing, not just belt-and-suspenders: kgo invalidates buffered fetches before `onRevoked`, but a batch already returned by `PollRecords` can still be processed afterward and re-add a stripped partition to `lastConsumedOffsets`.
3. `onPartitionsAssigned` deletes `lastConsumedOffsets[p]` for each newly-assigned partition **before** publishing ownership via `storeOwnedPartitions`. kgo restarts consumption from the committed offset on assignment, so any pre-existing entry is stale. Delete-before-publish closes both the revoke→stale-re-add→reassign-back rewind window and a tiny gap where `swapBuilder` could otherwise move the stale entry into `pendingFlushOffsets` after the owned-filter would already pass it. Cooperative assign delivers only the delta, so continuously-owned partitions are untouched.
4. Membership errors (`REBALANCE_IN_PROGRESS` / `ILLEGAL_GENERATION` / `UNKNOWN_MEMBER_ID` / `FENCED_INSTANCE_ID` / `UNKNOWN_TOPIC_OR_PARTITION`) are **not** soft-skipped as flush success — they propagate with `offsets NOT committed for partitions X` so the retry loop and logs stay honest. With fencing they are rare; on retry, revoke strip / owned filter typically leaves nothing to commit.

**`builderMtx` vs flush wait.** `commitOffsets` takes `builderMtx`, so any wait for an in-flight flush (`awaitPendingFlush`, `waitForFlushBackpressure`) must release the lock before blocking on `pendingFlush`. Holding it across the wait deadlocks shutdown (flush stuck in commit) and stalls revoke callbacks.

**Owned-partition tracking.** `ownedPartitions` (`atomic.Pointer[[]PartitionID]`) records the set the coordinator has granted us, mutated only by `onPartitionsAssigned` / `onPartitionsRevoked` and read lock-free via `snapshotOwnedPartitions`. The set is **not** used to drive consumption — kgo already knows which partitions it is fetching. Its purposes are narrower:

1. The poll loop zeroes `consumptionLagSeconds` for currently-owned partitions each cycle so idle partitions don't freeze on stale lag.
2. `onPartitionsRevoked` deletes `consumptionLagSeconds` and `bytesReceivedTotal` label values for revoked partitions so dashboards don't carry stale series.
3. `commitOffsets` filters the commit set to currently-owned partitions (zombie-commit fence).

**No forced flush on revocation.** We accept duplicates rather than blocking the rebalance callback on a synchronous flush. The revoked-partition data still in the active builder will be uploaded by the next flush cycle (the upload itself doesn't care about ownership); only the offset commit is abandoned.

### 4. .lidx files have exactly one entry per n-gram

The dedup happens in stages: the incremental fill dedupes exact `(ngram, docID)` pairs within and across tails (`sortAndDedupeGroups`, `mergeSortedPairs`), so each run is sorted and deduped; the k-way merge dedupes across runs and emits each term once per (date, shard) writer (`emitTerm` splits a term's docID run at day boundaries — a term appears once per file, never twice). Breaking any stage corrupts the index.

### 5. Object keys use unique StorageIDs

Path: `<YYYY-MM-DD>/<storage_id>/index` + `<YYYY-MM-DD>/<storage_id>/meta.json`, where `storage_id` is a ULID (26-char Crockford base32; 48-bit millisecond timestamp + 80 bits of randomness) generated by `store.NewStorageID()`. Each upload gets a globally unique path regardless of content. The xxh3 content hash is stored in `meta.json` for integrity/debugging but is not used in paths. StorageID must be generated once per file before the retry loop — retries reuse the same StorageID to prevent duplicate uploads. See `pkg/store/CLAUDE.md` for write ordering invariants and the StorageID/Hash distinction.

### 6. Runs survive `prepareIndexes` (retry safety)

`prepareIndexes` writes `.lidx` files but does NOT delete run files. If an upload fails and the flush is retried, `finish()` is idempotent (buffer already drained ⇒ no-op) and the merge re-reads the surviving runs, reproducing the same file set. On merge failure, every `.lidx` written that attempt is removed and the runs are preserved: the merge itself cleans up (`mergeShard` removes its own files, `mergeRuns` those of shards completed before the failure — files not yet returned to the builder, so invisible to `discardIndexes`), and `discardIndexes` covers files already registered. Runs are deleted only by `clear()`, after every upload in the cycle succeeds.

### 7. docID window: out-of-window timestamps PANIC (deliberate never-panic override)

`processStream` panics (`panicOutOfWindow`) on any timestamp outside the docID window: before `docIDEpoch` (2026-01-01T00:00Z) or at/past epoch + 2^32 ticks (2039-08-12T00:38:49.6Z at the default 100ms interval — pinned by `TestDocIDWindowEndAtDefaultInterval`; keep every documented window-end date in sync with it). This deliberately overrides the repo-wide "never panic" rule, documented here and at the panic site. Why: Grafana Adaptive Logs Archive/Replay can legitimately replay logs as old as 2026-01-01, and dropping an out-of-window line while the zero-file flush path commits Kafka offsets (invariant #1) would be a permanent, silent data skip; wrapping the tick would silently index the line under a wrong — possibly recent — date. A loud crash forces the anomaly (pre-epoch archive data, absurd future timestamp, misconfigured interval) to be dealt with. The panic message carries the offending record's partition/offset/tenant (`recordRef`, threaded through `processStream` and read only on the panic path) so remediation can target the poison record.

**The PRE-epoch side of the panic is unreachable in practice.** `newIndexBuilder` rejects any non-empty `min_date` earlier than `docIDEpoch`, and the minDate drop in `processStream` runs BEFORE `tick()` — so with a valid config every pre-epoch entry is dropped (and counted by `droppedLinesPreMinDate`) before it can reach the window check. The pre-epoch panic branch defends only against future minDate-bypass bugs (an empty minDate, or reordering the drop after the tick).

**The FUTURE side of the window is guarded solely by Loki distributor timestamp validation** (`creation_grace_period`, per-tenant overridable) — the builder itself accepts any timestamp up to the window end (>2039-08-12 at 100ms). A poison record with an absurd future timestamp that slips past the distributor therefore crash-loops the pod — and with it the pod's WHOLE partition set — by design; remediation is advancing the committed offset past the record or changing `document_interval`. That trade-off is accepted; revisiting it (e.g. quarantining instead of crashing) belongs to the deferred docID-scheme follow-up.

`Validate` keeps the panic reserved for genuinely anomalous timestamps: it requires `document_interval` to evenly divide 24h and the window end (epoch + 2^32 × interval) to be at least `minDocIDFutureRunway` (1 year) beyond now — a deliberately time-DEPENDENT rule, because a fixed-epoch window consumes its headroom as calendar time passes; at 100ms it starts rejecting deploys ~2038-08, well before live traffic could reach the window end.

### 8. Private per-cycle scratch subdir

Each `indexBuilder` owns a private subdirectory `<scratch_dir>/flat_<storageID>` holding its runs and produced `.lidx` files, created lazily on first spill. This is what makes builder swap safe: a swapped-out builder being flushed and the fresh active builder never collide on filenames. `clear()` removes the subdir wholesale; `Service.starting()` wipes the whole scratch dir (orphans from previous runs).

### 9. One global index config per cycle

The epoch-tick docID admits exactly one document interval, shard function, and index version per accumulation cycle. `Validate()` enforces this: `document_interval` must evenly divide 24h (the merge derives a pair's date as `day = absBucket / (24h / interval)`, which matches the calendar date only when the division is exact).

## Catchup mode: `extract_threads` (parallel extract pipeline)

`extract_threads` (default 1, max 4) is an **incident/catchup knob, never the default**. At 1 the builder runs the exact serial inline ingest path — no queue, no goroutines, none of the pipeline machinery is even constructed. Values 2-4 turn the per-record path into a pipeline for burning down Kafka lag faster than one core can extract.

**Shape (N ≥ 2):** decode stays on the poll goroutine; decoded streams are shallow-copied (the kafka.Decoder reuses its Entries backing array) and enqueued into ONE bounded queue consumed by N **competing** workers — deliberately not per-worker queues with modulo assignment, because a worker mid-spill would head-of-line block streams an idle worker could take. Each worker owns a full `streamIngester`: its own postings buffer running the untouched incremental-fill/spill path, its own date cache, dateRanges, and scratch, spilling `run_w<i>_<seq>.frun` into the shared per-builder runDir. A full queue blocks enqueue — natural backpressure into PollFetches, same as a slow serial builder (queue sizing rationale in `extract_pipeline.go`).

**Routing is correctness-free.** docIDs are epoch ticks, so a stream produces the same `(ngram, docID)` pairs whichever worker handles it; the flush k-way merge over the UNION of all workers' runs dedupes identical pairs across runs exactly as it already does across one buffer's runs (invariant #4 machinery, unchanged). No ordering or affinity assumption exists anywhere downstream.

**Drain-barrier invariant.** `prepareIndexes` on a (retired) pipeline builder first closes the queue, waits for every worker to finish in-flight items and exit, and surfaces the first worker error — only then does it `finish()` every buffer, so the zero-files ⇒ zero-pairs sub-invariant of invariant #1 still holds. After the barrier it OR-unions refTicks into the merge host, min/max-unions dateRanges, and merges the run union; all three are idempotent so retried prepares are safe. A worker spill error latches: subsequent enqueues fail fast through `processStream` (fails `running()`, pod restarts — identical failure surface to a serial spill error), and the barrier refuses the flush so pairs lost by the failed worker can never have their offsets committed. `clear()` re-runs the barrier as a backstop, so no worker goroutine outlives its builder even on abandoned flushes.

**Sizing.** The resident sort-buffer floor is ×2N (~480 MiB per worker at the default `postings_buffer_pairs` × 2 for new and old builder due to `swapBuilder`); the GOMEMLIMIT-fraction flush trigger's `estimatedMemoryBytes` sums `residentBytes` across every worker buffer, so the memory trigger already accounts for the fan-out. `firstAppend`/`lastAppend` are still stamped on the main enqueue path, so age/idle triggers keep wall-clock semantics. CPU demand measured on captured single-partition data from ops-002 (ingest phase):

| extract_threads | ~cores (decode + workers) | expected ingest speedup |
|-----------------|---------------------------|-------------------------|
| 2 | ~2.4 | ~1.6-2× |
| 3 | ~3.6 | ~2-2.5× |
| 4 | ~4.9 | diminishing — decode/main becomes the limit |

Measured on the capture (M4 MacBook, indicative only — dev hardware is the real gate): ingest phase 2.0× at 2 threads, 2.9× at 3; whole cycle 1.5× / 1.75× because the flush merge is unchanged and the drain barrier attributes in-flight extraction to prepare.

**Run incidents at 3.** The flush merge is unchanged and single-threaded per shard here (~5.5× is the point where merge time caps whole-cycle throughput); the shard-parallel merge is the future relief valve if catchup cycles become merge-bound.

## Input data distribution

Data from the Kafka topic is distributed backward in time from now, scattered over the last 7 days. The majority of records have near-present timestamps; there is a long tail of older records.

Implications:
- Up to ~7 dates active simultaneously; ingest cost does not depend on that count (one buffer).
- Each flush produces one .lidx per (date, shard) with data. Today's is large; old days produce many tiny files over time. The merge pipeline must handle this.
- `document_interval` impact varies by date: today is dense in ticks, old days sparse. Document metadata is synthesized per (date, shard) from the referenced-tick bitsets, so dense days don't inflate sparse days' files.
- Future timestamps are indexed under their own date (no clamping). The tenant ID comes from the Kafka record key (`r.Key`).

## Flush triggers

Triggers are checked by both the poll loop (after each successful batch) and a periodic ticker (`flush_check_interval`, default 5m, independent of poll success/failure):

| Trigger | Config key | What it measures |
|---------|-----------|-----------------|
| Scratch disk | `flush_on_max_bytes` | Sum of run-file bytes spilled by the active builder (`logline_index_builder_run_disk_bytes`, flush reason `run_disk`) — bounds **scratch disk footprint**; the dominant trigger under load |
| Builder memory | *(none — automatic)* | Capacity-based resident bytes (sort buffers + refTicks bitsets; `logline_index_builder_estimated_memory_bytes`, flush reason `memory`) — full flush at **70% of GOMEMLIMIT** when set. The buffer term is a fixed floor (`postings_buffer_pairs` × 24 B, ~460 MiB at the default); refTicks growth (shardCount × active days) is what can push it over. If GOMEMLIMIT is small enough that 70% of it sits at or below that fixed floor, the trigger is permanently armed and every poll batch swaps+flushes a near-empty builder — a known gap; see backlog for a storm guard |
| Max age | `flush_on_max_age` | **Wall-clock time since `firstAppend`** in this accumulation cycle — bounds **latency** |
| Idle timeout | `flush_on_idle` | Wall-clock time since `lastAppend` (last record processed) |

**Scratch headroom: size the volume at ~2-3× `flush_on_max_bytes`.** The trigger counts only the ACTIVE builder's run bytes, but peak scratch usage stacks the swapped-out builder's runs (≤ threshold) + its merged `.lidx` output (uncounted) + the fresh builder's runs accumulating during a slow upload. A full volume is a zero-progress crash loop: spill fails → `running()` fails → restart → wipe → re-consume → fill again. There is no automated startup check for this yet (backlog item); size the scratch PVC accordingly at deploy time.

**`flush_on_max_age` is NOT log data age.** It measures how long the current accumulation window has been open. With the 7-day data distribution, data age and accumulation window duration are completely different things.

Buffer sizing is configured by two builder-level knobs: `postings_buffer_pairs` (buffer capacity in (ngram, docID) pairs, default 20M, floor 2^16; resident memory ≈ pairs × 24 B, allocated up front — ~460 MiB at the default, ~2× during a builder swap) and `postings_spill_watermark` (deduped-head fraction that triggers a run spill, default 0.70, max 0.95).

**Metric names describe the flat-buffer architecture — the old accumulator-era names were retired, not aliased.** Keeping the old names would have been a dashboard trap: their semantics already changed with the architecture. New-architecture metrics: counters `runs_spilled_total` / `run_spill_bytes_total` (observed by `indexBuilder` as before/after deltas of the buffer's `runSeq`/`runBytes` around `onFull`/`finish`, so `postingsBuffer` carries no metrics dependency and nothing runs per pair) and histograms `merge_duration_seconds` (isolates the runs→.lidx merge; `flush_duration_seconds` covers the whole cycle including upload) and `runs_per_merge` (k-way fan-in).

## Scratch directory layout

```
<scratch_dir>/
  flat_<storageID>/            # one private subdir per builder cycle; clear() removes it wholesale
    run_<seq>.frun             # sorted (ngram, docID) runs — per-shard seekable s2 streams (run_file.go)
    <YYYY-MM-DD>_s<shard>.lidx # merged indexes produced by prepareIndexes, pending upload
```

## Performance: critical path

The critical path is: **poll → decode → extract n-grams → append pair → (on fill) radix-sort/merge**

This runs per-log-line at wire speed. Rules:

1. **Zero allocation in steady state.** Reuse buffers: `buf = buf[:0]`. The buffer and its radix scratch are allocated once per cycle.
2. **The pair append stays a bare two-slice append.** `appendPair` + `bufferFull` must remain inlinable into `processStream` (verify with `go build -gcflags='-m'`). Don't add branches, interfaces, or bounds work per pair.
3. **Radix sort, not comparison sort, for bulk ordering.** `radixSortByNgram` is LSD over the 3 data-bearing 16-bit digits — key bytes 0-5 only, which is why `Validate` caps `ngram_length` at 6 (a 7th/8th byte would land in the ignored digit, mis-sort runs, and wedge flush at the writer's ascending-term check). `slices.Sort` is only acceptable on tiny per-ngram doc groups (`sortAndDedupeGroups`) where it's adaptive.
4. **Amortize: sort on fill, not per entry.** Every appended pair is radix-sorted exactly once (incremental fill), then only linearly merged.
5. **Minimal lock scope.** `builderMtx` held only during record processing. Uploads happen outside the lock.

### NOT on the critical path (optimize for clarity)

- Flush path (finish + merge + upload + commit) — runs every few minutes
- Config validation — runs once at startup
- Metrics updates — cheap prometheus ops
- Test code

## Kafka fetch budget is shared across partitions

`FetchMaxBytes` is a **per-client** budget, not per-partition. A pod assigned N partitions must scale this proportionally or it will starve hot partitions (e.g. 4 partitions → ~400 MB). The other fetch knobs tune latency/throughput tradeoffs and can stay at their defaults.
