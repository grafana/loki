# pkg/logline/compactor/

Runs compaction jobs, as the `logline-compactor` target. Requests jobs from the scheduler via HTTP long poll, executes them, and loops. Does not poll object storage — works off source IDs provided by the scheduler.

## Configuration

The compactor has no config section of its own. `Config` is assembled from `logline.compaction` plus `logline.index.density_threshold`, so merged indexes apply the same density threshold the builder used. New settings go into those sections, never into a section named after this package.

`logline.compaction.postings_encoding` picks the postings encoding of merged indexes (`delta-varint` by default, or `elias-fano`). It applies to the output only: the merge reads each source in the encoding recorded in its header, so a deployment can switch either way without rewriting existing indexes. Enable `elias-fano` only once every component that reads its indexes supports it.

## Invariants

These must never be broken. Violating any causes stale work, wasted resources, or data corruption.

### 1. Respect the deadline

Every job carries an `ExpiresAt` timestamp. `executeJob` creates a `context.WithDeadline` from it. When the deadline fires, the compaction context is cancelled and work stops. Never continue working on an expired job — the scheduler may have already re-assigned those source indexes.

### 2. No store polling

The compactor holds a `*store.Store` for reading/writing index data during compaction, but `StartPolling` is never called. The compactor does not need a live snapshot of the index catalogue. The scheduler owns the scheduling view; the compactor just executes.

### 3. HTTP timeout exceeds long-poll timeout

The compactor's `http.Client` timeout (`schedulerapi.ClientTimeout`) must be longer than the scheduler's `schedulerapi.LongPollTimeout`. If the client times out before the scheduler responds, the compactor sees a network error instead of a clean 204. Both are constants, not config, because the scheduler and compactors roll out independently: a compactor must stay above the long poll of every scheduler build it may talk to.

### 4. No local disk for compaction output

The compactor never materialises the merged index on local disk. `compact()` pipes the merge output straight into object storage via `store.PutIndexStreaming`, which tees the bytes through an xxh3 hasher and a byte counter so `Meta.Hash` and `Meta.SizeBytes` can be populated without a second read. `Meta.IndexHeader` is populated inside the merge callback from the `format.HeaderInfo` returned by the shim's `Merger.Merge`. The compactor is stateless — no PVC, no scratch cleanup, no temp dir config.

### 5. One job at a time

The compactor is single-threaded: request a job, execute it, request the next. The scheduler enforces one-job-per-compactor via `getByWorker`. If the compactor reconnects after a crash, it receives the same job again (idempotent re-delivery).

## Key design decisions

### Compaction flow (compact())

`compact()` implements a three-phase pipeline:

1. **Prepare readers** — parallel `errgroup` fetches meta.json and index size (via `bucket.Attributes`) for each source, then opens an `io.ReaderAt` backed by range reads (`store.GetIndexReadAheadReaderAt`). No full download; source data is read on-demand during the merge.
2. **Build meta skeleton** — generate a unique `store.NewStorageID()`, extract the partition date from the first SourceID, union time bounds across all source metas, and copy shard config + document interval from the first source. Hash, SizeBytes, and IndexHeader are populated during phase 3.
3. **Streaming merge + upload** — `store.PutIndexStreaming(ctx, &meta, write)` sets up an `io.Pipe` whose reader side is fed to `bucket.Upload` on a goroutine. The `write` callback runs `Merger.Merge(ctx, readers, sizes, w)` which streams bytes straight into the pipe via an xxh3 + byte-counter tee, and returns the `format.HeaderInfo` describing the merged index. After the upload goroutine finishes, `PutIndexStreaming` populates `meta.Hash` and `meta.SizeBytes` from the tee and `meta.IndexHeader` from the returned `HeaderInfo`, then uploads `meta.json` as the commit marker. Merge duration in `merge_duration_seconds`; upload duration in `upload_duration_seconds`.

The compactor never holds a local copy of the merged output. StorageID must be generated once per job before the upload retry path, so a compactor that restarts mid-upload still addresses the same object path on the next attempt.

**Version support**: compaction uses the streaming-friendly v3 format (footer-at-EOF) via the shim's `Merger.Merge`. The scheduler groups sources by resolved version in the compaction key.

**Date extraction**: the scheduler guarantees all SourceIDs share the same partition date. The date is extracted from `strings.SplitN(sourceIDs[0], "/", 2)[0]`.

**Time bounds**: MinLogTs/MaxLogTs/MinRecordTs/MaxRecordTs are the union (min of mins, max of maxes) across all source metas.

**Hash and identity**: Both builder and compactor use xxh3 for content hashing and `store.NewStorageID()` for path identity. On the streaming path, the hash is computed as bytes flow through the tee writer inside `PutIndexStreaming`, so no second read is needed. The content hash is stored in `meta.json`; the StorageID is used in object paths. This decoupling prevents compaction self-reference — when merging a superset with its subset produces byte-identical output, the unique StorageID ensures it gets a distinct path and ID.

### Abandon on shutdown

When the compactor stops (e.g. rollout), `stopping()` calls `abandonCurrentJob()` which POSTs to the scheduler's abandon endpoint. This releases the in-flight job immediately so its sources become eligible for re-planning, rather than waiting for the job to expire. Uses generous backoff (500ms–5s, up to 10 retries, 30s total timeout) because the scheduler is likely rolling at the same time. 4xx responses are treated as terminal (e.g. 409 if the job was reassigned) to avoid stalling shutdown. `currentJobID` is tracked under `currentJobMu` and set by `executeJob` on entry. It is only cleared when the job finishes normally (success or failure) — during shutdown (ctx cancelled), it remains set so `stopping()` can read it. Abandon does NOT mark sources as completed — the sources return to the available pool. The scheduler rebuilds the planner and notifies waiting compactors immediately.

### Backoff on error, immediate retry on no-work

When `requestJob` returns an error (network failure, bad response), the compactor waits 5s before retrying. The scheduler answers 503 until it is running, so every scheduler restart produces a run of these: a 503 is logged at warn with the scheduler's state, any other error at error. When `executeJob` fails, the compactor also waits 5s before requesting the next job to avoid a tight retry loop on transient errors (the scheduler would re-deliver the same job). When the scheduler returns 204 (no work available), the compactor retries immediately — the scheduler's long poll already absorbed the wait.

### Compactor-reported completion (last_completed_job_id)

After a successful compaction, `runLoop` marks the job completed pending ack. Every job request then carries its ID as `last_completed_job_id`, so the scheduler can remove the job immediately (fast-path completion). The ID is cleared only once the scheduler answers with 200 or 204. A failed request keeps it for the retry. That includes the 503 the scheduler answers while it starts, so without this every scheduler restart would drop the reports of jobs that finish during it.

Dropping the ID on a failed request is not safe. Until a store poll sees the merged index, the scheduler still holds the job for this compactor, and `getByWorker` hands it back to be compacted again, which breaks the scheduler's "compacted at most once" invariant. Resending is safe because the scheduler only removes a job that still exists and belongs to this compactor. A failed job is never marked completed, so its ID is never sent.

### Compactor ID defaults to hostname

If `logline.compaction.compactor_id` is not configured, `Validate()` falls back to `os.Hostname()`. In a StatefulSet, this gives each pod a stable, unique identity without extra config.

## Key files

| File | Purpose |
|------|---------|
| `service.go` | dskit service lifecycle, `runLoop`, `requestJob` (HTTP long poll), `executeJob` (deadline enforcement) |
| `compactor.go` | `compact()` — three-phase prepare-readers / meta-skeleton / streaming-merge-and-upload pipeline |
| `config.go` | Config assembled from `logline.compaction` and `logline.index.density_threshold` |
| `metrics.go` | 9 prometheus metrics via `promauto.With(reg)` |

## Dependencies

The compactor imports `pkg/logline/scheduler/api` (package `schedulerapi`) for the shared HTTP API contract — route paths, long poll timing, query param names, `JobResponse` wire type, and request/response helpers. The request helpers still use the deprecated `/api/v1/...` paths, see the scheduler's HTTP API section. This keeps the scheduler and compactor in sync on API details without the compactor depending on the scheduler's internals.

## Key metrics

```
logline_index_compactor_jobs_requested_total           # long-poll requests sent to scheduler
logline_index_compactor_jobs_received_total            # jobs received from scheduler
logline_index_compactor_jobs_completed_total           # jobs finished successfully
logline_index_compactor_jobs_failed_total              # jobs that failed (non-expiry errors)
logline_index_compactor_jobs_expired_total             # jobs abandoned because deadline passed
logline_index_compactor_compaction_duration_seconds    # wall-clock time of each compaction
logline_index_compactor_merge_duration_seconds         # CPU time running Merger.Merge inside the streaming upload
logline_index_compactor_upload_duration_seconds        # bucket.Upload time after the merge finishes producing bytes
logline_index_compactor_bytes_uploaded_total           # bytes written to object storage
```

## Run loop

```
loop:
  requestJob(ctx) via GET <job path>?worker_id=X[&last_completed_job_id=Y]
    ├─ error → log, keep Y, backoff 5s, continue
    ├─ nil (204) → clear Y, continue immediately (scheduler already waited)
    └─ job → clear Y, executeJob(ctx, job)
                ├─ deadline exceeded → log, metric, backoff 5s, continue
                ├─ error → log, metric, backoff 5s, continue
                └─ success → log, metric, set Y to the job ID, continue
```

## Testing

- `newTestService` creates a compactor with nil store (no compaction executed in lifecycle tests)
- `newTestCompactor` creates a compactor with a real `*store.Store` backed by `objstore.NewInMemBucket()` for compaction tests
- `writeRealIndex` writes a real LOGL index file with a bitmap and uploads it via `store.PutIndex`
- Lifecycle tests verify StartAsync/AwaitRunning/StopAsync/AwaitTerminated
- Config validation tests verify required `scheduler_address`, hostname fallback, and default download concurrency
- Compaction tests cover: happy path (2 sources), too-few source IDs (0 and 1), cancelled context, missing source, and time-bound union
- Benchmarks in `compactor_bench_test.go`: `BenchmarkCompact_EndToEnd` (full pipeline) and `BenchmarkCompact_MergeOnly` (CPU-bound merge phase writing to `io.Discard`)
- `objstore.InMemBucket` is wrapped in `streamingInMemBucket` in tests so streaming uploads don't deadlock on the bucket's write-lock-during-ReadAll behaviour (real S3/GCS backends do not hold locks across the upload body)
- All tests use `require` (fail-fast), never `assert`
