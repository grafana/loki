# dataobj/consumer

## Flush pipeline: `processor` ↔ `flushCommitter` ↔ `flusher`

The consumer pipeline turns Kafka records into data objects, emits metastore
events for them, and commits Kafka offsets. Three layers cooperate:

1. **`processor`** (`processor.go`) — owns the per-partition lifecycle. Reads
   records from Kafka, appends them to a `builderGroup` (one `logsobj.Builder`
   per 12h TOC window), and decides when to flush (max-age, size, idle,
   shutdown).
2. **`flushCommitterImpl`** (`flush_committer.go`) — given a slice of builders
   and an offset, flushes each builder via the `flusher`, emits a metastore
   event per produced object, and finally commits the Kafka offset.
3. **`flusherImpl`** (`flush.go`) — turns a single `logsobj.Builder` into an
   uploaded object: calls `builder.Flush()`, sorts, uploads to object storage,
   returns the object path.

## Failure model: crash-and-replay, not in-process retry

`logsobj.Builder.Flush` is **not re-entrant**. On a successful internal flush
it calls `b.Reset()` before returning, so the buffered records are consumed
regardless of what the caller does with the produced object. If anything
after `builder.Flush()` (upload, sort, event emit, commit) fails, the data
cannot be reconstructed in-process.

The pipeline therefore uses an at-least-once model backed by Kafka replay:

- `flushCommitterImpl.Flush` is a plain "do the work, return wrapped errors"
  function. It does **not** decide how to react to failures.
- `emitEvent` and `commit` retry indefinitely with exponential backoff
  (`MaxRetries: 0`). The only way they return an error is when `ctx` is
  canceled, i.e. the app is shutting down. `flusher.Flush` can surface other
  errors too (upload/sort/object-storage problems).
- `processor.flush` is where the reaction lives:
  - `err == nil` → reset state, return nil, continue consuming.
  - `errors.Is(err, context.Canceled)` → return the error so the caller exits
    cleanly. The offset is **not** committed, so Kafka will replay on next
    startup.
  - Anything else → **panic**. The process exits, Kafka replays from the last
    committed offset, and the data is rebuilt from scratch. This is the only
    safe option given the non-re-entrant builder: an orphan object may be
    left in storage, but no committed-but-missing state is ever visible to
    queries.

## Processor state reset

`processor.flush` uses a `defer` to reset `firstAppend`, `lastAppend`, the
builder group, and the size estimate. The reset runs on every outcome:

- **Success** — obvious; prepare for the next object.
- **`context.Canceled`** — we're shutting down, the state is no longer used.
- **Panic** — the process is about to crash, so the reset is a no-op in
  practice; it just runs during panic unwinding.

There is intentionally no "retry with preserved state" path.

## Caveats

- **Metastore event idempotency.** On a crash-replay, a given batch of
  records may produce a fresh object *and* a duplicate metastore event
  pointing at the newly-uploaded path. Downstream readers must tolerate (or
  dedupe) multiple TOC entries for the same logical data. The previous
  attempt's object, if it uploaded, is orphaned in storage.
- **`flushCommitterImpl.Flush` partial progress.** Within a single call the
  loop runs `flusher.Flush` + `emitEvent` for each window builder in order;
  if it fails mid-loop the already-flushed builders have already been
  uploaded and had events emitted. Because we panic on non-cancel errors,
  the replay re-produces them as duplicates rather than skipping them — that
  is the intended trade-off.