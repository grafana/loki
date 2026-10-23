# pkg/logline/scheduler/

Singleton compaction scheduler, run by the `logline-scheduler` target. Polls object storage for active indexes, groups them into compaction jobs, serves jobs to compactors via long-poll HTTP, and deletes indexes past retention or merged away.

## Configuration

The scheduler has no config section of its own. `Config` is assembled from `logline.compaction` and `logline.retention`, which are named after what they control so they survive implementation moves. New settings go into those sections, never into a section named after this package.

- `logline.compaction.job_state_dir` is required and must be durable (see invariant 3 and 5).
- The retention period is not config: it comes from `limits.Limits`, the longest `limits_config.retention_period` across tenants.
- The long poll timeout is `schedulerapi.LongPollTimeout`, a constant shared with the compactor. `Service.longPollTimeout` exists only so tests can shorten it.

## Invariants

These must never be broken. Violating any causes duplicate work, lost jobs, or silent data corruption.

### 1. Persist before returning

Every job assignment is written to disk (via `jobStore.add`) before the HTTP response is sent. If persistence fails, the job is not returned to the compactor. This ensures that a scheduler crash after responding never loses track of in-flight work.

### 2. Atomic persistence

`jobStore.save` writes to a `.tmp` file and renames over the original. A crash mid-write never leaves a partial or corrupt `jobs.json`. On-disk state is always a valid JSON array.

### 3. Corrupt state is fatal

If `jobs.json` exists but is malformed, the scheduler refuses to start. Never silently discard persisted state — compactors may already be executing those jobs. Recovery requires stopping all compactors, wiping the file, and restarting.

### 4. Rollback on save failure

Every `jobStore` mutation (add, remove, removeExpired) rolls back its in-memory change if `save()` fails. The in-memory map and the on-disk file are always consistent.

### 5. A source index is compacted at most once

Once a source has been assigned to a compaction job, no other job may include it — whether the original job is in-flight, completed, or pending confirmation in the store snapshot. The system has no mechanism to undo or deduplicate redundant compaction, so prevention is the only defense. Enforced by the planner (filters in-flight and recently-completed sources) and by `jobStore.add` (rejects overlapping sources as a hard guard).

### 6. One job per compactor

`handleJobRequest` checks `getByWorker` before assigning new work. If a compactor already has an in-flight job (e.g., reconnecting after a timeout), the same job is re-delivered. A compactor never receives a second job while one is in-flight.

### 7. Completion is detected, not reported (with fast path)

The primary completion mechanism is `resolveCompletedWork`, which asks the store snapshot whether all of a job's source IDs have been resolved — either compacted (covered by a merged index) or absent from the snapshot entirely. It also prunes the `completedSources` cache for entries the snapshot has confirmed. This makes the system resilient to compactor crashes — the only source of truth is object storage.

As a fast path, compactors send `last_completed_job_id` with their next job request. `handleJobRequest` removes the job immediately (after verifying the compactor ID matches), avoiding redundant re-delivery between ticks. Compactors resend the ID until a request succeeds, so this removal must stay a no-op for a job that is gone or belongs to another compactor. If the compactor crashes before reporting, the tick-based detection handles it.

### 8. One snapshot per tick

`tick()` receives the snapshot from the store poll channel and passes it to `resolveCompletedWorkLocked` and `rebuildPlannerLocked`. Never call `s.store.Snapshot()` within a tick — the snapshot is immutable and the consistency within a single tick matters.

### 9. Single service mutex

All mutable state (`jobs`, `planner`, `completedSources`, `workCh`) is guarded by `s.mu`. The `jobStore` has no internal locking — callers must hold `s.mu`. `tick()` holds the lock for its entire body. Methods like `rebuildPlanner`, `resolveCompletedWork`, `notifyWorkers`, `assignJob`, and `markSourcesCompleted` take the lock themselves for use by the handler and tests.

## Key design decisions

### CompactionPlanner interface

```go
type CompactionPlanner interface {
    Plan() []string
    Groups() [][]string
}
```

Stateful iterator: initialized with available indexes, each `Plan()` call returns the next group of source IDs (or nil when exhausted). The planner is rebuilt from scratch every tick — it has no cross-tick state. This decouples grouping strategy from the scheduler loop.

Production uses `overlapPlanner`, a two-pass strategy:

1. **Overlap pass**: sweep-line clusters indexes by time-range overlap, then greedily packs consecutive time-sorted indexes within each cluster into multi-source groups (bounded by `maxSourcesPerJob` and `maxJobSizeBytes`). Consecutive indexes have the highest mutual overlap, maximizing dedup benefit (~0.82 output ratio).
2. **Fallback pass**: any ungrouped indexes (disjoint singletons, trailing singletons from oversized clusters) are packed by time proximity. No dedup benefit (~0.95 ratio), but reduces total index count on settled dates where no overlap exists.

Zero-timestamp indexes are excluded from both passes.

`dateGroupPlanner` groups by date and packs biggest-first — faster count reduction per round but causes cascading re-compaction of merged indexes (storage amplification ~5-6× from lingering copies during grace period).

### Long poll with close-and-replace

Compactors block on `waitForWork()` which returns a `<-chan struct{}`. When `notifyWorkers()` fires, it closes the current channel (waking all waiters) and installs a fresh one. Compactors that miss the close block on the new channel until the next notification. No `sync.Cond`, no broadcast — just channel semantics.

### Job identity

`jobID()` = SHA-256(sorted source IDs), truncated to 16 hex chars. Deterministic: the same set of sources always produces the same job ID. This prevents duplicate jobs if the same group is planned twice.

## Key files

| File | Purpose |
|------|---------|
| `service.go` | dskit service lifecycle, tick loop, `assignJob`, `resolveCompletedWork`, `rebuildPlanner`, `notifyWorkers` |
| `handlers.go` | `RegisterHandlers`, `handleJobRequest` (long poll), `writeJob` |
| `planner.go` | `CompactionPlanner` interface, `dateGroupPlanner` (group by date, biggest-first) |
| `planner_overlap.go` | `overlapPlanner` (production default): sweep-line clustering + greedy consecutive packing |
| `job.go` | `Job` struct, `Job.Response()` (converts to API wire type), `jobID` (deterministic hashing) |
| `job_store.go` | Disk-persisted job state: add/remove/expire with atomic write and rollback |
| `config.go` | Config assembled from `logline.compaction` and `logline.retention`, Validate applies defaults for zero values |
| `metrics.go` | 9 prometheus metrics via `promauto.With(reg)` |
| `api/api.go` | Shared HTTP API contract: route constants, long poll timing, `JobResponse` wire type, request/response helpers (package `schedulerapi`) |

## Key metrics

```
logline_index_scheduler_jobs_created_total       # jobs assigned to compactors
logline_index_scheduler_jobs_completed_total       # jobs completed, by reason={worker_report,state_change,expiration,abandon}
logline_index_scheduler_jobs_in_flight            # current in-flight count
logline_index_scheduler_available_indexes         # indexes eligible for compaction
logline_index_scheduler_long_poll_waiting         # compactors blocked in long poll
logline_index_scheduler_job_requests_total        # HTTP requests from compactors
logline_index_scheduler_tick_duration_seconds     # wall-clock time of each tick cycle
logline_index_scheduler_jobs_skipped_oversized    # jobs skipped because source count exceeds limit
logline_index_scheduler_indexes_deleted_total     # indexes deleted (retention or compacted source cleanup)
```

## Tick cycle

Driven by store poll events (via `store.PollNotify()`). Each successful store poll sends the new snapshot to the scheduler, which runs `tick(ctx, snap)`:

1. **Expire** — remove jobs past their `ExpiresAt`
2. **Resolve completed work** — remove jobs whose sources are all resolved (compacted or absent), prune `completedSources` cache
3. **Delete eligible indexes** — delete compacted sources older than `retention.compacted_source_delete_delay` and active indexes past retention. The retention cutoff comes from `limits.RetentionPeriod()`. Zero means no active index is deleted for retention. Deletes run on a separate goroutine fed by a coalescing channel, so only the freshest snapshot is used.
4. **Rebuild planner** — active indexes minus in-flight sources, grouped into compaction batches
5. **Notify compactors** — close `workCh` to wake any long-polling compactors
6. **Update gauges** — `jobs_in_flight`, `available_indexes`

Tick frequency equals the store's `PollInterval` (default 15m). Job expiration granularity is therefore tied to `PollInterval` — acceptable for background compaction.

## HTTP API

Every route is served under `/logline/api/v1/...` and, deprecated, under the old `/api/v1/...` paths. Compactors still call the old paths so a compactor and scheduler of different versions can talk to each other during a rollout. To retire the old paths, switch the compactor to the `/logline` paths and roll it out first, then stop serving the old ones.

Every route answers `503 Service Unavailable` until the service is running. The HTTP server is a dependency of the scheduler, so it serves requests before `starting()` has loaded the job store. Compactors retry on 5xx.

```
GET /logline/api/v1/compaction/job?worker_id=<id>[&last_completed_job_id=<id>]

200 OK            — job assigned (JSON: {id, source_ids, expires_at})
204 No Content    — long poll timed out, no work; retry
400 Bad Request   — missing worker_id

POST /logline/api/v1/compaction/abandon?worker_id=<id>&job_id=<id>

200 OK            — job removed (or already gone)
400 Bad Request   — missing worker_id or job_id
409 Conflict      — job belongs to a different compactor

GET /logline/api/v1/compaction/status

200 OK            — scheduler state (markdown default, JSON with Accept: application/json)

GET /logline/api/v1/store[?state=all|active|compacted]

200 OK            — store snapshot (markdown default, JSON with Accept: application/json)
400 Bad Request   — invalid state parameter
```

## Testing

- `newTestService` creates a Service with in-memory bucket, temp dir, and short poll timeout
- `populateBucket` / `populateMergedBucket` write stub indexes directly to the in-mem bucket
- `pollStore` calls `store.Poll()` for synchronous snapshot updates
- All tests use `require` (fail-fast), never `assert`
- Job store tests verify atomic persistence, rollback, and corrupt-file handling
