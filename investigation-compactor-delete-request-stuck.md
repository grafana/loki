# Investigation: Compactor delete-request stuck after k316→k317 rollout

## Summary

**No compactor, deletion, or retention code changes exist between k316 and k317 in the
OSS Loki repo.** All key files are byte-identical. The build SHAs referenced in the
alert (`9b0d6d0` for k316, `c20ef26` for k317) do not exist in this repository—they
are from the GEL (Grafana Enterprise Logs) repo. The root cause is therefore either in
GEL-specific overlay code, a configuration change coinciding with the rollout, or an
unrelated timing coincidence.

## Branch & commit details

| Branch | Tip commit | Date |
|--------|-----------|------|
| `origin/k316` | `e2ad82cf91` | 2026-07-21 17:26:22 UTC |
| `origin/k317` | `5eed1a74ed` | 2026-07-30 17:00:06 +0530 |
| Merge base | `d0822bcc3c` | — |
| Commits k316..k317 | 22 total | — |

## Files verified as byte-identical between k316 and k317

```
pkg/compactor/tables_manager.go       889da35fa0b640bb0fe7d41a24fed39d
pkg/compactor/compactor.go            90c2aba6d34a0ea59875a686a2baee4b
pkg/compactor/deletion/delete_requests_manager.go  230d8e603f73bf08d9e3c904ff34539c
pkg/compactor/deletion/delete_request_batch.go     e5eaf9e6fb472f5b069c4c1f910974d5
pkg/compactor/retention/expiration.go  a89cd9bf7cc5cad5d1886bc020bf2bd3
pkg/storage/config/schema_config.go    (no diff)
```

## Changes between k316 and k317 (that touch Go code)

None of the 22 commits between k316 and k317 modify any file under `pkg/compactor/`,
`pkg/compactor/deletion/`, `pkg/compactor/retention/`, or `pkg/storage/config/`.

The only storage-adjacent changes are:

1. **`6907cbc2b8`** — `fix(index-gateway): Correctness fixes for broken indexSet
   handling + configurable download timeout (#23356)` — Modifies
   `pkg/storage/stores/shipper/indexshipper/downloads/`. This is the **query-path**
   index download code, not the compactor's own table management. The compactor does not
   import `indexshipper/downloads`.

2. **`4a3278cce7`** — `fix: Update objstore to include fix for GCS Exists (#23380)` —
   Changes `vendor/github.com/thanos-io/objstore/providers/gcs/gcs.go` so that
   `Bucket.Exists` uses `IsObjNotFoundErr()` instead of comparing against
   `storage.ErrObjectNotExist` directly. This could surface previously-hidden GCS
   errors, but the compactor's main loop does not use `Exists` for table listing or
   compaction.

3. **`562a762ab1`** — Test de-flaking only (changes
   `uploads/table_manager_test.go`).

## Current behavior: schema-not-found in the compactor

### `SchemaPeriodForTable` (compactor.go:641)

```go
func SchemaPeriodForTable(cfg config.SchemaConfig, tableName string) (config.PeriodConfig, bool) {
    tableInterval := retention.ExtractIntervalFromTableName(tableName)
    schemaCfg, err := cfg.SchemaForTime(tableInterval.Start)
    if err != nil || schemaCfg.IndexTables.TableFor(tableInterval.Start) != tableName {
        return config.PeriodConfig{}, false
    }
    return schemaCfg, true
}
```

The **strictness check** on line 644 (`TableFor(…) != tableName`) means orphaned tables
from a prefix-boundary gap will always return `false`.

### `CompactTable` (tables_manager.go:324–328)

```go
schemaCfg, ok := SchemaPeriodForTable(c.schemaConfig, tableName)
if !ok {
    level.Error(util_log.Logger).Log("msg", "skipping compaction since we can't find schema for table", "table", tableName)
    return nil   // ← returns nil, NOT an error
}
```

Orphaned tables are **silently skipped** with a `nil` return. The overall
`runCompaction` succeeds.

### `runCompaction(ctx, true)` — retention path (tables_manager.go:221–321)

1. Calls `c.expirationChecker.MarkPhaseStarted()` → `DeleteRequestsManager` loads
   pending delete requests into `currentBatch`.
2. Iterates all tables via parallel workers; each calls `CompactTable(…, true)`.
3. For orphaned tables, `CompactTable` returns `nil` → worker continues.
4. If **all** tables succeed: the deferred cleanup calls
   `c.expirationChecker.MarkPhaseFinished()` → **all** loaded delete requests are
   marked as processed, regardless of whether specific tables were skipped.
5. If **any** table returns an error: `MarkPhaseFailed()` → delete requests remain
   unprocessed.

**Conclusion:** In the current OSS code, orphaned tables do NOT prevent delete requests
from being marked as processed. The "skipping compaction" error is cosmetic.

### `ApplyStorageUpdates` — HS mode only (tables_manager.go:449–450)

```go
table, err = c.initTable(ctx, tableName)
if err != nil {
    return err   // ← NO errSchemaForTableNotFound check
}
```

**Pre-existing bug:** If the compactor runs in horizontally-scalable (HS) mode and a
deletion manifest references an orphaned table, `ApplyStorageUpdates` would fail with
`errSchemaForTableNotFound` and abort manifest processing. This could leave delete
requests unprocessed. However, in practice the manifest is built via `IterateTables`,
which skips orphaned tables—so the manifest should not contain orphaned table names
unless the data was produced externally.

### Timeout path (retention.go:347–350)

```go
if errors.Is(err, context.DeadlineExceeded) && errors.Is(iterCtx.Err(), context.DeadlineExceeded) {
    level.Warn(logger).Log("msg", "Timed out while running delete")
    expiration.MarkPhaseTimedOut()
}
```

If `retention_table_timeout` is configured (default 0 = no timeout) and processing
exceeds it, `MarkPhaseTimedOut()` is called, which resets the batch without marking
requests as processed.

## What could make delete requests permanently stuck

In the OSS code as written, delete requests can remain permanently unprocessed if:

1. **`runCompaction(ctx, true)` fails on every cycle** — any error from ANY table's
   compaction/retention causes `MarkPhaseFailed()`. The orphaned-table skip returns nil,
   so this would require a **different** table to be failing.

2. **Retention timeout** — if `retention_table_timeout` is set and processing
   consistently times out, `MarkPhaseTimedOut()` prevents completion.

3. **HS-mode `ApplyStorageUpdates` bug** — if the HS mode was enabled and a manifest
   somehow references an orphaned table.

4. **Batch not including the request** — if `batchSize` is too small and the stuck
   request is always beyond the batch limit, or if `shouldProcessRequest` filters it
   out (user's deletion mode changed).

## Recommendation

Since the OSS Loki code is identical between k316 and k317, investigate:

1. **GEL-specific overlay code** in `grafana/enterprise-logs` repo between the builds
   tagged `weekly-k316-9b0d6d0` and `weekly-k317-c20ef26`.

2. **Configuration changes** deployed alongside the rollout: HS mode enablement,
   `retention_table_timeout`, `delete-batch-size`, or per-tenant `deletion_mode`
   overrides.

3. **Whether `runCompaction` is returning errors** on every retention cycle — check the
   `loki_compactor_apply_retention_operation_total{status="failure"}` metric.

4. **Whether retention is timing out** — check for `"Timed out while running delete"`
   log messages.
