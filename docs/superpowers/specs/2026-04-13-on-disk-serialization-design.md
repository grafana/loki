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
- Calling `encodeColumn(enc, colType, builder)` to flush each builder into the `columnar.Encoder`
- Setting sort info on the encoder

This replaces the old signature `func(ctx, rows) (Section, error)` which returned in-memory sections.

The builder stores the encoder at construction and delegates to it in `Flush`:

```go
func NewBuilder(encode SectionEncoder) *Builder

func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
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

**Rationale:** Keeps the pluggable strategy from #21442. Today's implementation (`ColumnarSectionEncoder`) uses `dataset.ColumnBuilder` + `columnar.Encoder`. A future implementation can use the dataset package directly by providing a different `SectionEncoder` function.

### 2. Caller-Driven Section Splitting

The current builders split rows into multiple sections internally via `computeSplits()` based on `targetSectionSize`. This is removed.

Instead, splitting follows the established codebase pattern: the **caller** checks `EstimatedSize()` during accumulation and flushes mid-accumulation when the size threshold is exceeded. Each `Flush` call writes all buffered rows as one section.

This matches:
- How `logsobj/builder.go` flushes logs builders mid-accumulation
- How `indexobj/builder.go` already flushes index pointer builders mid-accumulation
- The `dataobj.Builder.Append` contract (one call = one section)

**Consequence:** `targetSectionSize` is removed from the builder constructors. `EstimatedSize()` remains for callers to check.

### 3. Read-Side Architecture

The existing read-side abstraction is preserved:
- `Section` type with `OpenColumn` closure returning `ColumnReader` -> `columnar.Array`
- `Reader` — reads column data from a `Section`
- `RowReader` — converts columnar data back to typed `Posting`/`Stat` rows

A new `Open` function is added to each package, following the `streams.Open` pattern:

```go
func Open(ctx context.Context, section *dataobj.Section) (*Section, error)
```

This creates a `columnar.Decoder` -> `columnar.Section`, then wraps it in a package-specific `Section` by mapping generic columns back to typed `ColumnReader` implementations backed by on-disk data via `columnar.ReaderAdapter`.

**Deleted:** The in-memory producer code (`ColumnarEncoder` function body, `columnarSliceColumnReader` / `sliceColumnReader` helper types) that constructed `Section` from in-memory arrays. These are replaced by the `Open` function reading from disk.

### 4. `indexobj/builder.go` Wiring

Four changes:

**a) `builderStateDirty` tracking** — Set `b.state = builderStateDirty` in `AppendStat`, `AppendLabelPosting`, and `AppendBloomPosting` (replacing TODOs at lines 160, 184, 207).

**b) Mid-accumulation flushing** — After each append to a tenant builder, check `EstimatedSize()` and flush if it exceeds `TargetSectionSize`:
```go
if tenantBuilder.EstimatedSize() > int(b.cfg.TargetSectionSize) {
    if err := b.builder.Append(tenantBuilder); err != nil {
        return err
    }
}
```

**c) Final Flush serialization** — Replace the no-op reset of stats/postings builders with actual serialization via `b.builder.Append`, matching how streams/pointers/indexPointers are flushed.

**d) Constructor changes** — `getStatsBuilderForTenant` and `getPostingsBuilderForTenant` pass `ColumnarSectionEncoder` instead of `targetSectionSize` + old encoder.

## Column Definitions

### Postings Columns

| Column | Physical Type | Encoding | Compression | Nullable | Notes |
|--------|--------------|----------|-------------|----------|-------|
| `kind` | INT64 | DELTA | NONE | No | `PostingKind` as int |
| `object_path` | BINARY | PLAIN | ZSTD | No | |
| `section_index` | INT64 | DELTA | NONE | No | |
| `column_name` | BINARY | PLAIN | ZSTD | Yes | Only for bloom postings |
| `label_value` | BINARY | PLAIN | ZSTD | Yes | Only for label postings |
| `bloom_filter` | BINARY | PLAIN | NONE | Yes | Pre-compressed bloom data |
| `stream_id_bitmap` | BINARY | PLAIN | ZSTD | Yes | Only for label postings |
| `uncompressed_size` | INT64 | DELTA | NONE | No | |
| `min_timestamp` | INT64 | DELTA | NONE | No | |
| `max_timestamp` | INT64 | DELTA | NONE | No | |

**Sort info:** `[kind, column_name, label_value, min_timestamp, max_timestamp]` ascending.

### Stats Columns

| Column | Physical Type | Encoding | Compression | Nullable | Notes |
|--------|--------------|----------|-------------|----------|-------|
| `object_path` | BINARY | PLAIN | ZSTD | No | |
| `section_index` | INT64 | DELTA | NONE | No | |
| `sort_schema` | BINARY | PLAIN | ZSTD | No | |
| `min_timestamp` | INT64 | DELTA | NONE | No | |
| `max_timestamp` | INT64 | DELTA | NONE | No | |
| `row_count` | INT64 | DELTA | NONE | No | |
| `uncompressed_size` | INT64 | DELTA | NONE | No | |
| Dynamic label columns | BINARY | PLAIN | ZSTD | Yes | One per unique label key from `SortSchema`; sparse |

**Sort info:** Label values in sort-schema order, then `min_timestamp`, then `max_timestamp`, all ascending.

## Files Changed

### New Files
- `pkg/dataobj/sections/postings/open.go` — `Open` function for reading postings sections from disk
- `pkg/dataobj/sections/stats/open.go` — `Open` function for reading stats sections from disk

### Modified Files
- `pkg/dataobj/sections/postings/builder.go` — New `Flush(w SectionWriter)` signature, remove `computeSplits`, remove `targetSectionSize`, implement `SectionBuilder`
- `pkg/dataobj/sections/postings/encode_columnar.go` — Replace in-memory `ColumnarEncoder` with `ColumnarSectionEncoder` using `dataset.ColumnBuilder` + `columnar.Encoder`; new `SectionEncoder` signature; add `encodeColumn` helper; delete `columnarSliceColumnReader`
- `pkg/dataobj/sections/postings/postings.go` — May need column type constants for `encodeColumn`/`Open`
- `pkg/dataobj/sections/postings/builder_test.go` — Rewrite tests to round-trip through disk
- `pkg/dataobj/sections/stats/builder.go` — Same changes as postings builder
- `pkg/dataobj/sections/stats/encode_columnar.go` — Same changes as postings encoder
- `pkg/dataobj/sections/stats/stats.go` — May need column type constants
- `pkg/dataobj/sections/stats/builder_test.go` — Rewrite tests to round-trip through disk
- `pkg/dataobj/index/indexobj/builder.go` — Set `builderStateDirty`, mid-accumulation flushing, final Flush serialization, constructor changes
- `pkg/dataobj/index/stats_calculation_test.go` — Update to flush and read back from object
- `pkg/dataobj/index/label_postings_calculation_test.go` — Update to flush and read back from object

### Deleted Code (within modified files)
- Old `SectionEncoder` function type (`func(ctx, rows) (Section, error)`) in both packages
- In-memory `ColumnarEncoder` function body in both `encode_columnar.go` files
- `columnarSliceColumnReader` / `sliceColumnReader` types in both packages
- `computeSplits` function in both builders

## Testing Strategy

**Builder tests:** Rewrite to round-trip through disk. Use a `buildObject` test helper (like `streams/builder_test.go` has) that creates a `dataobj.Builder`, appends the section builder, flushes to an `Object`, then reads back via `Open` + `RowReader`. Existing test cases (empty builder, round-trip, sort order, nullable handling, bitmap correctness, flush reset, etc.) are preserved with the new round-trip mechanism.

**Section splitting tests:** Instead of testing `computeSplits` internally, test that the caller can flush mid-accumulation and produce multiple sections in the resulting object.

**indexobj builder tests:** Verify that `Flush` serializes stats and postings sections into the object (no longer just resetting). Verify `builderStateDirty` is set correctly.

**Calculation tests:** Update `stats_calculation_test.go` and `label_postings_calculation_test.go` to flush the indexobj builder and read back from the resulting object via `Open`, instead of using test accessors to read from the in-memory path.
