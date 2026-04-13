# On-Disk Serialization for Postings and Stats Index Sections

**Context:** Follow-up items #1, #2, #3 from [PR #21442](https://github.com/grafana/loki/pull/21442)

**Goal:** Make `postings.Builder` and `stats.Builder` implement `dataobj.SectionBuilder` so they serialize to disk via `dataobj.Builder.Append`, completing the on-disk serialization path that all other section types (streams, pointers, indexpointers, logs) already use.

## Background

PR #21442 introduced the postings and stats section packages with an in-memory encoding path. The builders accumulate typed rows (`Posting` / `Stat`), and `Flush` produces in-memory `Section` objects backed by Arrow-like `columnar.Array` values from `pkg/columnar/`. These sections are never written to disk — `indexobj/builder.go` currently resets the builders without flushing.

The on-disk serialization path used by other sections follows: `dataset.ColumnBuilder` -> `columnar.Encoder` (from `pkg/dataobj/sections/internal/columnar/`) -> `SectionWriter`. This design brings postings and stats onto that same path.

## Design Decisions

### 1. SectionEncoder Abstraction (Pluggable Write Strategy)

The `SectionEncoder` function type is retained but gets a new signature:

```go
// postings package:
type SectionEncoder func(rows []Posting, enc *columnar.Encoder) error

// stats package:
type SectionEncoder func(rows []Stat, enc *columnar.Encoder) error
```

The encoder function is responsible for:
- Creating `dataset.ColumnBuilder`s for each column
- Iterating rows and appending values via `dataset.Int64Value` / `dataset.BinaryValue`
- Flushing each builder into the `columnar.Encoder` via `encodeColumn` (see below)
- Setting sort info on the encoder via `enc.SetSortInfo(*datasetmd.SortInfo)`
- Normalizing bitmap data (see Bitmap Normalization below)

This replaces the old signature `func(ctx, rows) (Section, error)` which returned in-memory sections.

#### The `encodeColumn` Helper

Both packages define an `encodeColumn` helper function following the pattern from `streams/builder.go` and `pointers/builder.go`:

```go
func encodeColumn(enc *columnar.Encoder, columnType ColumnType, builder *dataset.ColumnBuilder) error {
    column, err := builder.Flush()      // -> *dataset.MemColumn with pages
    if err != nil { return err }

    columnEnc, err := enc.OpenColumn(column.ColumnDesc())
    if err != nil { return err }
    defer func() { _ = columnEnc.Discard() }()

    if len(column.Pages) == 0 { return nil }

    for _, page := range column.Pages {
        if err := columnEnc.AppendPage(page); err != nil { return err }
    }
    return columnEnc.Commit()
}
```

This bridges `dataset.ColumnBuilder` -> `columnar.Encoder` via the `ColumnEncoder.AppendPage` -> `Commit` flow.

#### Bitmap Normalization

The current `ColumnarEncoder` normalizes all `StreamIDBitmap` values to the same byte length within a section (via `columnarNormalizeBitmaps`). The new `ColumnarSectionEncoder` must preserve this behavior — padding all bitmaps to the maximum length found in the current batch before encoding them into the `stream_id_bitmap` column. This is a correctness requirement tested by `TestBuilder_BitmapNormalization`.

#### Empty-Flush Contract

If the builder has zero rows, `Flush` writes nothing and returns `(0, nil)`. Callers should guard with `EstimatedSize() > 0` before calling `dataobj.Builder.Append`.

The builder stores the encoder at construction and delegates to it in `Flush`:

```go
func NewBuilder(encode SectionEncoder, opts ...BuilderOption) *Builder

func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
    if len(b.rows) == 0 {
        return 0, nil
    }
    sort.SliceStable(b.rows, b.sortFunc)  // sort before encoding
    var enc columnar.Encoder
    defer enc.Reset()
    if err := b.encode(b.rows, &enc); err != nil {
        return 0, err
    }
    enc.SetTenant(b.tenant)
    n, err = enc.Flush(w)
    if err == nil {
        b.Reset()
    }
    return n, err
}
```

The `SectionEncoder` receives rows in sorted order. Sorting is the builder's responsibility, performed in `Flush` before delegating to the encoder.

**Page size configuration:** The `dataset.ColumnBuilder` requires `PageSizeHint` and `PageMaxRowCount` parameters. These are captured in the `ColumnarSectionEncoder` closure when it is constructed by `getStatsBuilderForTenant`/`getPostingsBuilderForTenant` in `indexobj/builder.go`, using values from `cfg.TargetPageSize` and `cfg.MaxPageRows` (or suitable defaults). This follows the pattern used by `streams.Builder` where page sizes are constructor parameters threaded through to the encoding logic.

**Error contract:** On `columnar.Encoder.Flush` error, `b.Reset()` is **not** called — rows are retained in the builder. The caller (`dataobj.Builder.Append`) propagates the error and does **not** call `sec.Reset()` on failure (see `dataobj/builder.go:46-55`). This matches the convention in `streams.Builder`.

**Rationale:** Retaining the pluggable `SectionEncoder` from #21442 is cheaper than removing it. Today's implementation (`ColumnarSectionEncoder`) uses `dataset.ColumnBuilder` + `columnar.Encoder`. If a second encoder using the dataset package directly is needed in the future, it can be provided as a different `SectionEncoder` function without changing the builder.

### 2. Caller-Driven Section Splitting

The current builders split rows into multiple sections internally via `computeSplits()` based on `targetSectionSize`. This is removed.

Instead, splitting follows the established codebase pattern: the **caller** checks `EstimatedSize()` during accumulation and flushes mid-accumulation when the size threshold is exceeded. Each `Flush` call writes all buffered rows as one section.

This matches:
- How `logsobj/builder.go` flushes logs builders mid-accumulation
- How `indexobj/builder.go` already flushes index pointer builders mid-accumulation
- The `dataobj.Builder.Append` contract (one call = one section)

**Consequence:** `targetSectionSize` is removed from the builder constructors. `EstimatedSize()` remains for callers to check.

### 3. Read-Side Architecture

The existing read-side types are preserved:
- `Section` type with `ColumnNames`, `RowCount`, and `OpenColumn` closure
- `ColumnReader` interface: `Read(ctx, count) (columnar.Array, error)` + `Close() error`
- `Reader` — reads column data from a `Section`
- `RowReader` — converts columnar data back to typed `Posting`/`Stat` rows

#### How `Open` Backs the `Section` Type

A new `Open` function is added to each package, following the `streams.Open` pattern:

```go
func Open(ctx context.Context, section *dataobj.Section) (*Section, error)
```

This creates a `columnar.Decoder` -> `columnar.Section`, then constructs a package-specific `Section` value whose `OpenColumn` closure reads from disk.

**`Section` population:** After this change, `Section` remains the shared value type — the only production code path populating it is `Open`. The in-memory path used in pre-existing tests is deleted.

`Open` populates:
- `Section.ColumnNames` — from the `columnar.Section` column metadata
- `Section.RowCount` — derived from the first column's row count in the `columnar.Section` metadata (all columns in a well-formed section have the same row count). This is required for `Reader`/`RowReader` EOF detection.
- `Section.OpenColumn` — a closure that returns a `ColumnReader` backed by on-disk data

**The `ColumnReader` bridge:** The `OpenColumn` closure returns a new concrete type that implements `ColumnReader`. This type must satisfy the following contract:
- `Read(ctx, count)` returns a `columnar.Array` of up to `count` elements, or `(nil, io.EOF)` when no more data is available
- The returned arrays must be the concrete types expected by `RowReader` — specifically `*columnar.Number[int64]` for INT64 columns and `*columnar.UTF8` for BINARY columns (these are used in type assertions in `row_reader.go`)
- `Close()` releases any internal resources (allocator, decoder state)

The bridge reads column data from the `columnar.Section`'s `Dataset` interface (via `dataset.RowReader` or `columnar.ReaderAdapter`), manages a `memory.Allocator` internally, and extracts the target column from the results. The exact internal approach is an implementation detail as long as the above contract is satisfied.

#### Validation in `Open`

Following the `streams.Open` pattern:
- Check `section.Type` matches the package's section type (namespace + kind)
- Check `section.Type.Version` matches the package's declared version
- Skip unrecognized columns (forward compatibility)
- No fail-fast on missing columns at open time — let the reader surface errors when a required column is accessed

#### Section Type Version

Both `postings.sectionType` and `stats.sectionType` retain `Version: 1`. Section versions are section-specific — they version the section's logical schema and semantics, not the internal encoding format. The columnar encoding format has its own version (`columnar.FormatVersion`), but that is an internal implementation detail of how data is serialized, not a section-level concern. Tying section version to `columnar.FormatVersion` would couple them unnecessarily — a change in the columnar encoding library would force a section version bump even if the section's schema hasn't changed.

The `Open` function checks `section.Type.Version == 1` for schema validation. When calling `columnar.NewDecoder`, it passes `columnar.FormatVersion` directly (not `section.Type.Version`), because `NewDecoder` validates the encoding format version, which is a separate concern from the section schema version. This decouples the two version concepts at the call site.

Note: other section packages (`streams`, `pointers`, `indexpointers`, `logs`) pass `section.Type.Version` to `NewDecoder` because they set their section version to `columnar.FormatVersion`. That coupling is a pre-existing design choice in those packages, not a requirement we need to follow.

#### Deleted In-Memory Producer Code

The in-memory producer code is deleted:
- `ColumnarEncoder` function body (the one using `pkg/columnar/` Arrow arrays)
- `columnarSliceColumnReader` / `sliceColumnReader` helper types

These are replaced by the `Open` function reading from disk.

### 4. `indexobj/builder.go` Wiring

Six changes:

**a) `builderStateDirty` tracking** — Set `b.state = builderStateDirty` in `AppendStat`, `AppendLabelPosting`, and `AppendBloomPosting` (replacing TODOs at lines 160, 184, 207).

**b) Size estimate tracking** — Each `Append*` method must track the pre/post `EstimatedSize()` delta on the tenant builder and update `b.unflushedSizeEstimate`, matching the pattern from `AppendIndexPointer` (`builder.go:222-228`). After updating `unflushedSizeEstimate`, recompute `b.currentSizeEstimate` via `b.estimatedSize()` and check `builderFull` against `TargetObjectSize`, same as other append paths.

**c) Mid-accumulation flushing** — After each append to a tenant builder, check `EstimatedSize()` and flush if it exceeds `TargetSectionSize`:
```go
if tenantBuilder.EstimatedSize() > int(b.cfg.TargetSectionSize) {
    flushedSize := tenantBuilder.EstimatedSize()
    if err := b.builder.Append(tenantBuilder); err != nil {
        return err
    }
    b.unflushedSizeEstimate -= flushedSize
}
```
The `flushedSize` is captured **before** `Append` (which calls `Flush` + `Reset`, zeroing the tenant builder's `EstimatedSize()`). The decrement only happens on success — if `Append` returns an error, `unflushedSizeEstimate` is left unchanged since the function returns the error immediately.

**d) Final Flush serialization** — Replace the no-op reset of stats/postings builders with actual serialization via `b.builder.Append`, guarded by `EstimatedSize() > 0`, matching how streams/pointers/indexPointers are flushed:
```go
for _, tenantStats := range b.stats {
    if tenantStats.EstimatedSize() > 0 {
        flushErrors = append(flushErrors, b.builder.Append(tenantStats))
    }
}
for _, tenantPostings := range b.postings {
    if tenantPostings.EstimatedSize() > 0 {
        flushErrors = append(flushErrors, b.builder.Append(tenantPostings))
    }
}
```

**e) Constructor changes** — `getStatsBuilderForTenant` and `getPostingsBuilderForTenant` pass `ColumnarSectionEncoder` instead of `targetSectionSize` + old encoder.

**f) Metrics observation (out of scope)** — The `observeObject` method currently handles indexpointers, pointers, and streams sections. Adding postings/stats observation is out of scope for this PR — it can be added in a follow-up alongside item #7 (tenant count metrics) from the follow-up list.

## Column Definitions

### Postings Columns

All columns are present for every row. Columns that are semantically irrelevant for a given `PostingKind` use **null values** (via `dataset.NullValue()` / `builder.Append` with null, producing validity bitmap entries). This preserves the existing behavior where the current `ColumnarEncoder` uses `AppendNull()` for irrelevant fields, and `RowReader` checks `IsNull(i)` to return `nil` for `BloomFilter` and other fields. Existing tests (e.g., `require.Nil(t, label.BloomFilter)`) depend on null round-trip semantics.

| Column | Physical Type | Encoding | Compression | Nullable | Notes |
|--------|--------------|----------|-------------|----------|-------|
| `kind` | INT64 | PLAIN | NONE | No | `PostingKind` as int (only 2 values: 0=bloom, 1=label) |
| `object_path` | BINARY | PLAIN | ZSTD | No | |
| `section_index` | INT64 | DELTA | NONE | No | |
| `column_name` | BINARY | PLAIN | ZSTD | Yes | Null for label postings |
| `label_value` | BINARY | PLAIN | ZSTD | Yes | Null for bloom postings |
| `bloom_filter` | BINARY | PLAIN | NONE | Yes | Null for label postings; bloom data is pre-compressed by the caller, encoder applies no additional compression |
| `stream_id_bitmap` | BINARY | PLAIN | ZSTD | Yes | Null for bloom postings |
| `uncompressed_size` | INT64 | DELTA | NONE | No | |
| `min_timestamp` | INT64 | DELTA | NONE | No | |
| `max_timestamp` | INT64 | DELTA | NONE | No | |

**Sort info:** `[kind, column_name, label_value, min_timestamp, max_timestamp]` ascending. Since these are fixed columns at known positions, the `SortInfo` is constructed with hardcoded column indices matching the order columns are opened in the encoder.

### Stats Columns

| Column | Physical Type | Encoding | Compression | Notes |
|--------|--------------|----------|-------------|-------|
| `object_path` | BINARY | PLAIN | ZSTD | |
| `section_index` | INT64 | DELTA | NONE | |
| `sort_schema` | BINARY | PLAIN | ZSTD | |
| `min_timestamp` | INT64 | DELTA | NONE | |
| `max_timestamp` | INT64 | DELTA | NONE | |
| `row_count` | INT64 | DELTA | NONE | |
| `uncompressed_size` | INT64 | DELTA | NONE | |
| Dynamic label columns | BINARY | PLAIN | ZSTD | One per unique label key; sparse/nullable |

#### Stats Column Layout Change

Note: the column layout changes from the current in-memory encoder. The existing `encode_columnar.go` interleaves dynamic label columns between `sort_schema` and `min_timestamp`. The new on-disk layout places all fixed columns first (indices 0-6), then dynamic label columns (indices 7+). This is intentional — it simplifies `SortInfo` construction and aligns with the `streams.Builder` pattern where dynamic columns come after fixed columns. Since stats sections have never been persisted to disk, there is no migration concern.

#### Stats SortSchema Invariant

All rows within a single section (single `Flush` call) must share the same `SortSchema`. This is guaranteed by the builder being per-tenant and the calculation pipeline producing rows with a consistent schema per tenant. The encoder validates this invariant at encode time (inside the `SectionEncoder` function): it reads `SortSchema` from the first row, then checks all subsequent rows for consistency. If any row has a differing `SortSchema`, the encoder returns an error.

#### Stats Dynamic Label Columns and SortInfo

The encoder determines dynamic label columns by parsing `SortSchema` from the first row (validated to be consistent across all rows). For rows where a label key from the schema is absent in the `Labels` map, the encoder appends a null value (`dataset.NullValue()`). At the `RowReader` boundary, null label values map to empty string in the `Stat.Labels` map (matching current behavior).

Label columns are opened in sort-schema order, after all fixed columns. The `SortInfo` is constructed after all columns are opened:

1. Open fixed columns (object_path through uncompressed_size) — indices 0-6
2. Open dynamic label columns in sort-schema order — indices 7, 8, ...
3. Build `SortInfo` with column sorts: dynamic label columns (in order), then `min_timestamp` (index 3), then `max_timestamp` (index 4), all ascending

**Sort info:** Label values in sort-schema order, then `min_timestamp`, then `max_timestamp`, all ascending.

## Files Changed

### New Files
- `pkg/dataobj/sections/postings/open.go` — `Open` function for reading postings sections from disk, `ColumnReader` bridging type
- `pkg/dataobj/sections/stats/open.go` — `Open` function for reading stats sections from disk, `ColumnReader` bridging type

### Modified Files
- `pkg/dataobj/sections/postings/builder.go` — New `Flush(w SectionWriter)` signature, remove `computeSplits`, remove `targetSectionSize`, implement `SectionBuilder`
- `pkg/dataobj/sections/postings/encode_columnar.go` — Replace in-memory `ColumnarEncoder` with `ColumnarSectionEncoder` using `dataset.ColumnBuilder` + `columnar.Encoder`; new `SectionEncoder` signature; add `encodeColumn` helper; delete `columnarSliceColumnReader`
- `pkg/dataobj/sections/postings/postings.go` — Add `ColumnType` enum with `String()` method producing logical type names for on-disk metadata (e.g., `ColumnTypeKind` → `"kind"`, `ColumnTypeLabelValue` → `"label_value"`, etc., matching the column names in the Column Definitions table). These strings are stored in the section's protobuf metadata dictionary and used by `Open` to reconstruct typed columns via `ParseColumnType`. `sectionType.Version` stays at `1`
- `pkg/dataobj/sections/postings/builder_test.go` — Rewrite tests to round-trip through disk
- `pkg/dataobj/sections/stats/builder.go` — Same changes as postings builder
- `pkg/dataobj/sections/stats/encode_columnar.go` — Same changes as postings encoder
- `pkg/dataobj/sections/stats/stats.go` — Add `ColumnType` enum with `String()` method (same pattern as postings); dynamic label columns use a shared `ColumnTypeLabel` with the tag field carrying the label name (matching the `streams` convention). `sectionType.Version` stays at `1`
- `pkg/dataobj/sections/stats/builder_test.go` — Rewrite tests to round-trip through disk
- `pkg/dataobj/index/indexobj/builder.go` — Set `builderStateDirty`, size estimate tracking, mid-accumulation flushing, final Flush serialization, constructor changes
- `pkg/dataobj/index/stats_calculation_test.go` — Update to flush and read back from object
- `pkg/dataobj/index/label_postings_calculation_test.go` — Update to flush and read back from object

### Deleted Code (within modified files)
- Old `SectionEncoder` function type (`func(ctx, rows) (Section, error)`) in both packages
- In-memory `ColumnarEncoder` function body in both `encode_columnar.go` files
- `columnarSliceColumnReader` / `sliceColumnReader` types in both packages
- `computeSplits` function in both builders
- `StatsBuilderForTenant` / `PostingsBuilderForTenant` test accessors in `indexobj/builder.go` (no longer needed since tests go through the full flush + `Open` path)

## Testing Strategy

**Builder tests:** Rewrite to round-trip through disk. Use a `buildObject` test helper (like `streams/builder_test.go` has) that creates a `dataobj.Builder`, appends the section builder, flushes to an `Object`, then reads back via `Open` + `RowReader`. Existing test cases (empty builder, round-trip, sort order, nullable handling, bitmap correctness/normalization, flush reset, etc.) are preserved with the new round-trip mechanism.

**Section splitting tests:** Instead of testing `computeSplits` internally, test that the caller can flush mid-accumulation and produce multiple sections in the resulting object. The test creates a builder, appends some rows, calls `dataobj.Builder.Append` (triggering `Flush`), appends more rows, calls `Append` again, then verifies the object contains two sections with the expected rows in each.

**indexobj builder tests:** Verify that `Flush` serializes stats and postings sections into the object (no longer just resetting). Verify `builderStateDirty` is set correctly. Verify size estimate tracking and `builderFull` signaling work for stats/postings appends.

**Calculation tests:** Update `stats_calculation_test.go` and `label_postings_calculation_test.go` to flush the indexobj builder and read back from the resulting object via `Open`, instead of using test accessors to read from the in-memory path.

**Negative/error tests:**
- Stats encoder returns an error when rows have inconsistent `SortSchema` values
- `Open` rejects sections with wrong section type or version mismatch
