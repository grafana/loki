# pkg/logline/store/

Index lifecycle management and object storage bucket creation.

## Files

| File | Purpose |
|------|---------|
| `bucket.go` | `NewBucket()` — creates `objstore.Bucket` from a schema + object-store config with a configurable path prefix. `New()` wraps it and returns a `Store`. |
| `meta.go` | `Meta` struct — index identity, metadata, path generation, and `SetFileInfo()` (hash + size from a finished file) |
| `index_identity.go` | `NewStorageID()` — random path ID |
| `config.go` | `Config` with flag registration and validation |
| `index_store.go` | `Store` — write, poll, query, delete indexes in object storage |
| `metrics.go` | Prometheus metrics via `promauto.With(reg)` |
| `bucket_reader.go` | `NewBucketReaderAt()` — `io.ReaderAt` over a bucket object |
| `readahead_reader.go` | `NewReadAheadReaderAt()` — chunked read-ahead `io.ReaderAt`, used by compaction |

## Invariants

### 1. Write order: index before meta

Meta is the commit marker. Poll discovers indexes by finding `meta.json`. If the index upload fails, meta is never written and the index is invisible. Never reverse this order.

Both `PutIndex` (pre-populated Meta, `io.Reader` body) and `PutIndexStreaming` (producer callback, Hash/SizeBytes/IndexHeader filled from the stream) preserve this ordering. `PutIndexStreaming` pipes bytes from the callback through an xxh3 + byte-counter tee into `bucket.Upload`, then populates `meta.Hash`/`meta.SizeBytes` from the tee and `meta.IndexHeader` from the `format.HeaderInfo` returned by the callback (typically the writer's or merger's `Info()`), then uploads `meta.json`.

For the non-streaming path the caller fills those fields itself: `Meta.SetFileInfo(f)`
records the content hash and size, and the caller assigns `IndexHeader`, because decoding
a header requires resolving the file to a format version and that is the job of the layer
above the store.

### 2. Delete order: meta before index

Removing meta first makes the index invisible to Poll immediately. An orphaned index data file causes no read errors because nothing references it. Never reverse this order.

### 3. Delete safety: sources before merged

`Delete()` refuses to remove a merged index (non-empty `CompactedFrom`) while any of its source indexes still exist in the snapshot. Deleting a merged index would un-compact its sources — they'd reappear as active on the next Poll. Always delete compacted sources first.

Both `Delete()` and `EligibleForDeletion()` enforce this via `canDelete()`.

### 4. Snapshot is the read model

All read operations (`Indexes`, `EligibleForDeletion`, `canDelete`) use an immutable snapshot built by `Poll`. No I/O on read paths. The snapshot is replaced atomically via `atomic.Pointer`.

### 5. Poll rebuilds the snapshot; delta fetch skips known metas

Each Poll lists date prefixes, then skips canonical `YYYY-MM-DD` partitions older than
`Config.MinDate` before listing index prefixes or fetching metas under them. Malformed top-level
prefixes are still passed through the existing validation path rather than silently hidden. The
snapshot is rebuilt from scratch on each poll from the remaining prefixes.
However, metas for already-known indexes are reused from an in-memory cache (`knownMetas`)
rather than re-fetched — meta.json is immutable once written. Only newly discovered prefixes
trigger a Get call. The cache is rebuilt each poll from only the listed prefixes, so deleted
indexes disappear naturally (they're absent from the listing and thus absent from the next cache).
On a poll error, the cache is not updated.

### 6. Path is source of truth for identity

Object storage paths use `<date>/<storage_id>/index` and `<date>/<storage_id>/meta.json`. During Poll, `Meta.Date` and `Meta.StorageID` are parsed from the storage path, not read from the JSON body. `Hash` (xxh3 content hash) is read from `meta.json` and kept for integrity/debugging. Legacy indexes that predate `StorageID` used `Hash` as the path component; `objectID()` falls back to `Hash` when `StorageID` is empty to preserve backward compatibility.

### 7. GaugeVec metrics are fully reset on every Poll

All `GaugeVec` metrics (`indexes`, `indexBytes`) are reset and repopulated on every successful
Poll, including the early-return path when the store is empty. Both reset sites must stay in sync
with each other. Stale label combinations disappear automatically when indexes are deleted.

Both `indexes` and `indexBytes` carry `state` and `date` labels.

State values:
- `"active"` — queryable, not compacted, MinRecordTs outside the ingester window
- `"ingester_window"` — active but MinRecordTs falls within `QueryIngestersWithin` of now (not yet queryable)
- `"compacted"` — source index covered by a merged index

Date label: individual date strings for the past 7 days, `"past"` for everything older.

Compacted source indexes contribute their bytes under `state="compacted"` — they remain in
object storage until explicitly deleted.

## Key concepts

- **`Meta`** is the single handle for an index. It carries identity (`Date`/`StorageID`/`Hash`) and metadata. There is no separate ID type.
- **`StorageID`** is an opaque identifier used in object paths and index IDs: a ULID (26-char Crockford base32; 48-bit millisecond timestamp + 80 bits of randomness) from `NewStorageID()`. Serialized as `"id"` in `meta.json` (`omitempty` for legacy compat). On read, the path is source of truth and validated against the JSON value. Each upload generates a unique StorageID via `NewStorageID()`, ensuring no two indexes share a path even if their content is byte-identical.
- **`Hash`** is the xxh3 content hash of the index file, serialized in `meta.json`. Used for integrity verification and debugging, not for identity or path construction.
- **`objectID()`** returns `StorageID` if set, else falls back to `Hash` for legacy indexes.
- An index is **compacted** if its ID appears in any other index's `CompactedFrom`. **Active** = not compacted.
- **`CompactedFrom`** is `[]string` of IDs (`"date/id"`). Nil means leaf index (written directly by the builder).
- **`IndexHeader`** is `*format.HeaderInfo` (`pkg/logline/format.HeaderInfo`), a JSON-friendly subset of the binary index header. Current writers populate it before upload; nil is legacy-only.
- JSON tags are **snake_case**. `CompactedFrom` uses `omitempty`; `IndexHeader` is always serialized as `index_header` (null only for legacy/nil values).

## Deletion eligibility

Two paths to eligibility:

| Condition | Rule |
|-----------|------|
| Compacted source | Merged index's `CreatedAt` is older than `CompactionGracePeriod` |
| Active index | `MaxLogTs` is older than `RetentionDuration` |

Both paths also require `canDelete()` — merged indexes with live sources are never eligible.

`Snapshot.EligibleForDeletion(retentionCutoff, graceCutoff)` implements the logic with caller-provided cutoffs. `Store.EligibleForDeletion(now)` delegates using the store's own config. The scheduler calls the Snapshot method directly with its own retention/grace config.

## Poll concurrency

Poll runs in four phases:
1. **List** — sequential outer `Iter` for date prefixes, skip canonical dates before `Config.MinDate`, then concurrent inner `Iter` per retained date (bounded by `PollConcurrency`) to collect index prefixes
2. **Classify** — sequential comparison of listed prefixes against `knownMetas`; known prefixes reuse cached Meta; unknown prefixes go into `toFetch`
3. **Fetch** — worker pool (size = `PollConcurrency`) fetches and parses metas for `toFetch` only
4. **Build** — sequential snapshot construction

I/O errors in fetch abort the poll. Malformed or missing metas are warned and skipped.

## Testing

- Bucket: `objstore.NewInMemBucket()`
- Registry: `prometheus.NewPedanticRegistry()`
- Assertions: `require` only (fail-fast), never `assert`
- Call `Poll()` directly in tests. `StartPolling` is for production.
