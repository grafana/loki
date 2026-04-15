# On-Disk Serialization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **IMPORTANT: Commit protocol.** Do NOT commit at the end of each task. Instead: stage all changed files with `git add`, present a brief summary of what was done, and **PAUSE for user review**. The user will review the staged diff and approve or request changes before committing. Only commit after explicit user approval.

**Goal:** Make `postings.Builder` and `stats.Builder` implement `dataobj.SectionBuilder` so they serialize to disk via `dataobj.Builder.Append`.

**Architecture:** Replace the in-memory `ColumnarEncoder` (using `pkg/columnar/` Arrow arrays) with on-disk encoding via `dataset.ColumnBuilder` + `columnar.Encoder` + `SectionWriter` — the same path used by streams/pointers/indexpointers. Add `Open` functions for reading sections back from disk. Wire up `indexobj/builder.go` to actually flush stats and postings.

**Tech Stack:** Go, `dataset.ColumnBuilder`, `columnar.Encoder`/`Decoder`, `dataobj.SectionBuilder` interface

**Spec:** `docs/superpowers/specs/2026-04-13-on-disk-serialization-design.md`

---

## File Structure

### Modified Files (Write Side — Task 1 DONE, Task 4)
- `pkg/dataobj/sections/postings/postings.go` — `ColumnType` enum, `SectionEncoder` signature change, `Section`/`Column` types refactored
- `pkg/dataobj/sections/postings/encode_columnar.go` — Replace `ColumnarEncoder` with `ColumnarSectionEncoder`
- `pkg/dataobj/sections/postings/builder.go` — `Flush(w SectionWriter)`, remove `computeSplits`/`targetSectionSize`
- `pkg/dataobj/sections/stats/stats.go` — Same as postings
- `pkg/dataobj/sections/stats/encode_columnar.go` — Same as postings
- `pkg/dataobj/sections/stats/builder.go` — Same as postings

### Refactored Files (Read Side — Tasks 2, 5)
- `pkg/dataobj/sections/postings/postings.go` — Replace old `Section`/`ColumnReader` with new `Section` (wrapping `columnar.Section`), `Column` (wrapping `columnar.Column`), `Open`, `CheckSection`
- `pkg/dataobj/sections/postings/row_reader.go` — Rewrite to use `dataset.RowReader`, matching `streams.RowReader` lifecycle
- `pkg/dataobj/sections/stats/stats.go` — Same refactoring as postings
- `pkg/dataobj/sections/stats/row_reader.go` — Same refactoring as postings

### Deleted Files (Tasks 2, 5)
- `pkg/dataobj/sections/postings/reader.go` — Replaced by `RowReader` reading from `dataset.RowReader` directly
- `pkg/dataobj/sections/postings/open.go` — Delete the bridge-based version; `Open` moves to `postings.go`
- `pkg/dataobj/sections/stats/reader.go` — Same

### Test Files (Tasks 3, 6, 8)
- `pkg/dataobj/sections/postings/builder_test.go` — Rewrite for on-disk round-trip
- `pkg/dataobj/sections/stats/builder_test.go` — Same
- `pkg/dataobj/index/stats_calculation_test.go` — Use `Open` + `RowReader` instead of test accessors
- `pkg/dataobj/index/label_postings_calculation_test.go` — Same

### Wiring (Task 7)
- `pkg/dataobj/index/indexobj/builder.go` — Wire up stats/postings flushing, dirty state, size tracking

---

## Task 1: Postings — ColumnType, Encoder, and Builder (Atomic)

This task combines the ColumnType enum, SectionEncoder, and Builder changes into a single compilable unit.

**Files:**

- Modify: `pkg/dataobj/sections/postings/postings.go`
- Rewrite: `pkg/dataobj/sections/postings/encode_columnar.go`
- Modify: `pkg/dataobj/sections/postings/builder.go`

- [ ] **Step 1: Add ColumnType enum to postings.go**

Add the `ColumnType` enum, `columnTypeNames` map, `ParseColumnType`, and `String()` methods. Follow the pattern from `streams/streams.go:96-153`.

```go
type ColumnType int

const (
	ColumnTypeInvalid         ColumnType = iota
	ColumnTypeKind                       // "kind"
	ColumnTypeObjectPath                 // "object_path"
	ColumnTypeSectionIndex               // "section_index"
	ColumnTypeColumnName                 // "column_name"
	ColumnTypeLabelValue                 // "label_value"
	ColumnTypeBloomFilter                // "bloom_filter"
	ColumnTypeStreamIDBitmap             // "stream_id_bitmap"
	ColumnTypeUncompressedSize           // "uncompressed_size"
	ColumnTypeMinTimestamp               // "min_timestamp"
	ColumnTypeMaxTimestamp               // "max_timestamp"
)
```

`ParseColumnType` must return `(ColumnType, error)` matching the streams pattern — callers (like `Open`) decide whether to skip unknown columns.

- [ ] **Step 2: Change SectionEncoder signature in postings.go**

Replace the old `SectionEncoder` type (line 74) with:

```go
type SectionEncoder func(rows []Posting, enc *columnar.Encoder) error
```

Add the `columnar` import.

- [ ] **Step 3: Write ColumnarSectionEncoder in encode_columnar.go**

Replace the entire contents of `encode_columnar.go`. The new encoder:
- Exports `ColumnarSectionEncoder(pageSizeHint int, pageMaxRowCount int) SectionEncoder` — returns a closure
- Creates `dataset.ColumnBuilder`s per the column encoding table in the spec
- Uses `dataset.NullValue()` for semantically irrelevant fields (preserving null round-trip for `BloomFilter`, `LabelValue`, etc.)
- Normalizes bitmaps before encoding (preserving `columnarNormalizeBitmaps` behavior)
- Sets sort info: `[kind(0), column_name(3), label_value(4), min_timestamp(8), max_timestamp(9)]` ascending
- Includes `encodeColumn` helper matching streams/pointers pattern
- Deletes: old `ColumnarEncoder`, `columnarSliceColumnReader`

- [ ] **Step 4: Update Builder in builder.go**

Remove `targetSectionSize` from `Builder` struct and `NewBuilder`:
```go
func NewBuilder(encode SectionEncoder) *Builder
```

Replace `Flush(ctx context.Context) ([]Section, error)` with:
```go
func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
    if len(b.rows) == 0 {
        return 0, nil
    }
    sort.SliceStable(b.rows, func(i, j int) bool {
        return comparePostings(b.rows[i], b.rows[j])  // package-level function, not method
    })
    var enc columnar.Encoder
    defer enc.Reset()
    if err := b.encode(b.rows, &enc); err != nil {
        return 0, fmt.Errorf("encoding postings: %w", err)
    }
    enc.SetTenant(b.tenant)
    n, err = enc.Flush(w)
    if err == nil {
        b.Reset()
    }
    return n, err
}
```

Delete `computeSplits` method. Remove `context` import; add `columnar` import.

- [ ] **Step 5: Verify compilation**

Run: `go build ./pkg/dataobj/sections/postings/...`
Expected: PASS (all three files compile together)

- [ ] **Step 6: Stage and pause for review**

```bash
git add pkg/dataobj/sections/postings/postings.go pkg/dataobj/sections/postings/encode_columnar.go pkg/dataobj/sections/postings/builder.go
```

Present a brief summary of changes. **PAUSE** — wait for user to review staged diff and approve before committing.

Suggested commit message: `feat(postings): implement SectionBuilder with on-disk ColumnarSectionEncoder`

---

## Task 2: Postings — Read-Side Refactoring (Section, Open, RowReader)

Refactor the read side to match the established streams/pointers/logs pattern. This replaces the custom `Section`/`ColumnReader`/`Reader` architecture from #21442.

**Files:**

- Modify: `pkg/dataobj/sections/postings/postings.go` — replace `Section`/`ColumnReader` types, add `Column`, `Open`
- Delete: `pkg/dataobj/sections/postings/reader.go` — replaced by `RowReader` using `dataset.RowReader`
- Delete: `pkg/dataobj/sections/postings/open.go` — the bridge-based version created earlier; `Open` moves to `postings.go`
- Rewrite: `pkg/dataobj/sections/postings/row_reader.go` — use `dataset.RowReader`, match `streams.RowReader` lifecycle

- [ ] **Step 1: Refactor Section and Column types in postings.go**

Replace the old types:
```go
// OLD (delete):
// type ColumnReader interface { Read(ctx, count) (columnar.Array, error); Close() error }
// type Section struct { ColumnNames []string; RowCount int; OpenColumn func(name string) (ColumnReader, error) }
// type SectionEncoder func(ctx context.Context, rows []Posting) (Section, error)

// NEW (add):
type Section struct {
    inner   *columnar.Section
    columns []*Column
}

func (s *Section) Columns() []*Column { return s.columns }

type Column struct {
    Section *Section
    Name    string
    Type    ColumnType
    inner   *columnar.Column
}
```

Add `Open` function following `streams.Open` pattern:
```go
func Open(ctx context.Context, section *dataobj.Section) (*Section, error) {
    if !CheckSection(section) {
        return nil, fmt.Errorf("section type mismatch: got=%s want=%s", section.Type, sectionType)
    }
    if section.Type.Version != 1 {
        return nil, fmt.Errorf("unsupported section schema version: got=%d want=1", section.Type.Version)
    }
    dec, err := columnar.NewDecoder(section.Reader, columnar.FormatVersion)
    // ... columnar.Open, init columns (skip unrecognized), return Section
}
```

The `SectionEncoder` type (already changed in Task 1) stays as-is.

- [ ] **Step 2: Delete reader.go and open.go**

Delete `reader.go` entirely — the intermediate column-by-column Reader is no longer needed.
Delete `open.go` (the bridge-based version) — `Open` now lives in `postings.go`.

- [ ] **Step 3: Rewrite row_reader.go**

Rewrite to match `streams.RowReader` pattern. Key differences from the old version:

```go
type RowReader struct {
    sec     *Section
    ready   bool
    buf     []dataset.Row
    reader  *dataset.RowReader
    columns []dataset.Column
}

func NewRowReader(sec *Section) *RowReader
func (r *RowReader) Open(ctx context.Context) error   // creates dataset, opens reader
func (r *RowReader) Read(ctx context.Context, p []Posting) (int, error)  // reads rows, decodes
func (r *RowReader) Reset(sec *Section)
func (r *RowReader) Close() error
```

`Read` reads `dataset.Row`s and decodes them into `Posting` structs by matching column positions from `Section.Columns()` to fields:
- INT64 values → `dataset.Row.Values[i].Int64()` for kind, section_index, uncompressed_size, min_timestamp, max_timestamp
- BINARY values → `dataset.Row.Values[i].Binary()` for object_path, column_name, label_value, bloom_filter, stream_id_bitmap
- Null check → `dataset.Row.Values[i].IsNull()` for nullable fields (bloom_filter, label_value, stream_id_bitmap)

Follow `streams.RowReader` for the `initReader`/`decodeRow` pattern.

- [ ] **Step 4: Verify compilation**

Run: `CGO_ENABLED=0 go build ./pkg/dataobj/sections/postings/...`
Expected: PASS

- [ ] **Step 5: Stage and pause for review**

```bash
git add pkg/dataobj/sections/postings/postings.go pkg/dataobj/sections/postings/row_reader.go
git rm pkg/dataobj/sections/postings/reader.go pkg/dataobj/sections/postings/open.go
```

Present a brief summary of changes. **PAUSE** — wait for user to review staged diff and approve before committing.

Suggested commit message: `refactor(postings): align read-side with streams/pointers pattern (Section, Column, Open, RowReader)`

---

## Task 3: Postings — Rewrite Builder Tests

**Files:**

- Rewrite: `pkg/dataobj/sections/postings/builder_test.go`

- [ ] **Step 1: Add test helpers**

Add a `buildObject` helper:

```go
func buildObject(t *testing.T, b *postings.Builder) (*dataobj.Object, io.Closer) {
    t.Helper()
    builder := dataobj.NewBuilder(nil)
    err := builder.Append(b)
    require.NoError(t, err)
    obj, closer, err := builder.Flush()
    require.NoError(t, err)
    t.Cleanup(func() { _ = closer.Close() })
    return obj, closer
}
```

Add a `readAllPostingsFromObject` helper that iterates sections, filters by `postings.CheckSection`, and reads via `Open` + `RowReader`:

```go
func readAllPostingsFromObject(t *testing.T, obj *dataobj.Object) []postings.Posting {
    t.Helper()
    var result []postings.Posting
    for _, sec := range obj.Sections() {
        if !postings.CheckSection(sec) { continue }
        opened, err := postings.Open(context.Background(), sec)
        require.NoError(t, err)
        rr := postings.NewRowReader(opened)
        require.NoError(t, rr.Open(context.Background()))
        defer rr.Close()
        buf := make([]postings.Posting, 100)
        for {
            n, err := rr.Read(context.Background(), buf)
            result = append(result, buf[:n]...)
            if err == io.EOF { break }
            require.NoError(t, err)
        }
    }
    return result
}
```

Note: `NewRowReader` takes `*Section` and returns `*RowReader` (no error). Caller must call `rr.Open(ctx)` before `rr.Read`. `Read` takes `(ctx, []Posting)` and returns `(int, error)`. This matches the `streams.RowReader` lifecycle.

- [ ] **Step 2: Rewrite all test functions**

Create builders via `postings.NewBuilder(postings.ColumnarSectionEncoder(pageSizeHint, maxPageRows))`. Use `buildObject` + `readAllPostingsFromObject` for round-trip. Call `b.SetTenant("test-tenant")` in tests that need tenant context.

Preserve all existing test cases:
- `TestBuilder_Empty` — build with no rows; `dataobj.Builder.Append` with empty builder returns `(0, nil)` from `Flush`, which is fine
- `TestBuilder_LabelPostingRoundTrip` — append, round-trip, verify fields
- `TestBuilder_BloomPostingRoundTrip` — verify nil BloomFilter/StreamIDBitmap null round-trip
- `TestBuilder_MixedPostings` — both kinds
- `TestBuilder_SortOrder` — unsorted input, verify sorted output
- `TestBuilder_NullableHandling` — null round-trip for kind-specific fields
- `TestBuilder_BitmapCorrectness` — bitmap data round-trips
- `TestBuilder_BitmapNormalization` — bitmaps padded to same length
- `TestBuilder_SectionSplitting` — **rewrite**: create a `postings.Builder`, append first batch, call `dataobjBuilder.Append(postingsBuilder)` (flushes section 1 + resets builder), append second batch, call `dataobjBuilder.Flush()` to get object, verify object has 2 postings sections
- `TestBuilder_AllBloom` / `TestBuilder_AllLabel` — single-kind round-trip
- `TestBuilder_FlushResetsBuilder` — flush, verify builder is empty
- `TestBuilder_Type` — verify `Type()` returns correct section type
- `TestRowReader_SmallBuffer` — verify small read buffer works
- Add: `TestOpen_WrongSectionType` — verify `Open` rejects sections with wrong type
- Add: `TestOpen_WrongVersion` — verify `Open` rejects sections with wrong version

- [ ] **Step 3: Run tests**

Run: `go test -v ./pkg/dataobj/sections/postings/...`
Expected: ALL PASS

- [ ] **Step 4: Stage and pause for review**

```bash
git add pkg/dataobj/sections/postings/builder_test.go
```

Present a brief summary of changes. **PAUSE** — wait for user to review staged diff and approve before committing.

Suggested commit message: `test(postings): rewrite builder tests for on-disk round-trip`

---

## Task 4: Stats — ColumnType, Encoder, and Builder (Atomic)

Same structure as Task 1 but for stats.

**Files:**

- Modify: `pkg/dataobj/sections/stats/stats.go`
- Rewrite: `pkg/dataobj/sections/stats/encode_columnar.go`
- Modify: `pkg/dataobj/sections/stats/builder.go`

- [ ] **Step 1: Add ColumnType enum to stats.go**

```go
type ColumnType int

const (
	ColumnTypeInvalid          ColumnType = iota
	ColumnTypeObjectPath                  // "object_path"
	ColumnTypeSectionIndex                // "section_index"
	ColumnTypeSortSchema                  // "sort_schema"
	ColumnTypeMinTimestamp                // "min_timestamp"
	ColumnTypeMaxTimestamp                // "max_timestamp"
	ColumnTypeRowCount                    // "row_count"
	ColumnTypeUncompressedSize            // "uncompressed_size"
	ColumnTypeLabel                       // "label" — dynamic; tag carries label name
)
```

`ParseColumnType` returns `(ColumnType, error)`.

- [ ] **Step 2: Change SectionEncoder signature**

```go
type SectionEncoder func(rows []Stat, enc *columnar.Encoder) error
```

- [ ] **Step 3: Write ColumnarSectionEncoder in encode_columnar.go**

Same pattern as postings, with stats-specific columns:
- Fixed columns first (indices 0-6): object_path, section_index, sort_schema, min_timestamp, max_timestamp, row_count, uncompressed_size
- Dynamic label columns after (indices 7+), in sort-schema order
- **Column layout change from current encoder:** labels move from between sort_schema and timestamps to after all fixed columns (intentional, documented in spec)
- SortSchema invariant validation: read `SortSchema` from first row, error if any subsequent row differs
- Dynamic label null handling: absent label key -> `dataset.NullValue()`
- SortInfo: dynamic label columns (in order), then min_timestamp (index 3), then max_timestamp (index 4)

Delete: old `ColumnarEncoder`, `sliceColumnReader`.

- [ ] **Step 4: Update Builder in builder.go**

Same changes as postings Task 1 Step 4. Remove `targetSectionSize`, change `Flush` to `SectionBuilder` interface, delete `computeSplits`. The sort logic sorts by label values in sort schema order, then MinTimestamp, then MaxTimestamp (preserving existing sort).

- [ ] **Step 5: Verify compilation**

Run: `go build ./pkg/dataobj/sections/stats/...`
Expected: PASS

- [ ] **Step 6: Stage and pause for review**

```bash
git add pkg/dataobj/sections/stats/stats.go pkg/dataobj/sections/stats/encode_columnar.go pkg/dataobj/sections/stats/builder.go
```

Present a brief summary of changes. **PAUSE** — wait for user to review staged diff and approve before committing.

Suggested commit message: `feat(stats): implement SectionBuilder with on-disk ColumnarSectionEncoder`

---

## Task 5: Stats — Read-Side Refactoring (Section, Open, RowReader)

Same pattern as postings Task 2, adapted for stats.

**Files:**

- Modify: `pkg/dataobj/sections/stats/stats.go` — replace `Section`/`ColumnReader` types, add `Column`, `Open`
- Delete: `pkg/dataobj/sections/stats/reader.go` — replaced by `RowReader` using `dataset.RowReader`
- Rewrite: `pkg/dataobj/sections/stats/row_reader.go` — use `dataset.RowReader`, match `streams.RowReader` lifecycle

- [ ] **Step 1: Refactor Section and Column types in stats.go**

Same pattern as postings Task 2. `Section` wraps `columnar.Section`, `Column` wraps `columnar.Column`. `Open` validates version == 1, passes `columnar.FormatVersion` to decoder.

Handle dynamic label columns: `ColumnTypeLabel` with tag = label name (matching streams convention).

- [ ] **Step 2: Delete reader.go**

- [ ] **Step 3: Rewrite row_reader.go**

Same pattern as postings, adapted for stats fields. Dynamic label columns are decoded by iterating `Section.Columns()` and collecting `ColumnTypeLabel` columns, populating `Stat.Labels` map from their tag and value. Null label values map to empty string.

- [ ] **Step 4: Verify compilation**

Run: `CGO_ENABLED=0 go build ./pkg/dataobj/sections/stats/...`
Expected: PASS

- [ ] **Step 5: Stage and pause for review**

```bash
git add pkg/dataobj/sections/stats/stats.go pkg/dataobj/sections/stats/row_reader.go
git rm pkg/dataobj/sections/stats/reader.go
```

Present a brief summary of changes. **PAUSE** — wait for user to review staged diff and approve before committing.

Suggested commit message: `refactor(stats): align read-side with streams/pointers pattern (Section, Column, Open, RowReader)`

---

## Task 6: Stats — Rewrite Builder Tests

**Files:**

- Rewrite: `pkg/dataobj/sections/stats/builder_test.go`

- [ ] **Step 1: Add test helpers**

Same `buildObject` and `readAllStatsFromObject` pattern as postings Task 3. Use `stats.CheckSection` to filter, `stats.NewRowReader(opened)` (returns `*RowReader`), `rr.Open(ctx)` then `rr.Read(ctx, buf)` for batch reading. Matches `streams.RowReader` lifecycle.

- [ ] **Step 2: Rewrite all test functions**

Preserve existing tests adapted for on-disk round-trip. Also add:
- `TestBuilder_InconsistentSortSchema` — append rows with different SortSchema, verify encoder returns error at flush time
- `TestOpen_WrongSectionType` / `TestOpen_WrongVersion` — verify Open rejects bad sections

- [ ] **Step 3: Run tests**

Run: `go test -v ./pkg/dataobj/sections/stats/...`
Expected: ALL PASS

- [ ] **Step 4: Stage and pause for review**

```bash
git add pkg/dataobj/sections/stats/builder_test.go
```

Present a brief summary of changes. **PAUSE** — wait for user to review staged diff and approve before committing.

Suggested commit message: `test(stats): rewrite builder tests for on-disk round-trip`

---

## Task 7: indexobj/builder.go — Wire Up Stats and Postings Flushing

**Files:**

- Modify: `pkg/dataobj/index/indexobj/builder.go`

- [ ] **Step 1: Update constructor helper functions**

Change `getStatsBuilderForTenant` (line 125) and `getPostingsBuilderForTenant` (line 134):

```go
// getStatsBuilderForTenant — change:
stats.NewBuilder(stats.ColumnarSectionEncoder(int(b.cfg.TargetPageSize), int(b.cfg.MaxPageRows)))

// getPostingsBuilderForTenant — change:
postings.NewBuilder(postings.ColumnarSectionEncoder(int(b.cfg.TargetPageSize), int(b.cfg.MaxPageRows)))
```

`b.cfg.TargetPageSize` and `b.cfg.MaxPageRows` exist in `logsobj.BuilderBaseConfig` and are already used by the streams builder.

- [ ] **Step 2: Add builderStateDirty + size tracking to AppendStat, AppendLabelPosting, AppendBloomPosting**

Follow the exact pattern from `AppendIndexPointer` (lines 222-239 of `builder.go`):
1. Set `b.state = builderStateDirty` (replacing TODOs at lines 160, 184, 207)
2. Capture `preAppendSizeEstimate := tenantBuilder.EstimatedSize()` before append
3. Append to tenant builder
4. Capture `postAppendSizeEstimate := tenantBuilder.EstimatedSize()` after append
5. `b.unflushedSizeEstimate += postAppendSizeEstimate - preAppendSizeEstimate`
6. If `postAppendSizeEstimate > int(b.cfg.TargetSectionSize)`, call `b.builder.Append(tenantBuilder)` for mid-accumulation flush
7. `b.currentSizeEstimate = b.estimatedSize()`
8. If `b.currentSizeEstimate > int(b.cfg.TargetObjectSize)`, set `b.builderFull = true`

Note: the existing `AppendIndexPointer` does NOT decrement `unflushedSizeEstimate` after mid-accumulation flush. Follow the same convention — don't add a decrement that the existing code doesn't have.

- [ ] **Step 3: Wire up final Flush**

In `Flush` (around line 451), replace the no-op reset loops:

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

- [ ] **Step 4: Delete test accessors**

Delete `StatsBuilderForTenant` (line 545) and `PostingsBuilderForTenant` (line 552).

- [ ] **Step 5: Verify compilation**

Run: `go build ./pkg/dataobj/index/...`
Expected: Compilation errors in test files that use deleted accessors. This is expected — we fix those in Task 8.

- [ ] **Step 6: Stage and pause for review**

```bash
git add pkg/dataobj/index/indexobj/builder.go
```

Present a brief summary of changes. **PAUSE** — wait for user to review staged diff and approve before committing.

Suggested commit message: `feat(indexobj): wire up stats/postings flushing, add dirty state + size tracking`

---

## Task 8: Update Calculation Tests

**Files:**

- Modify: `pkg/dataobj/index/stats_calculation_test.go`
- Modify: `pkg/dataobj/index/label_postings_calculation_test.go`

- [ ] **Step 1: Rewrite flushStatsForTenant helper**

In `stats_calculation_test.go`, replace `flushStatsForTenant` (line 46) which currently calls `builder.StatsBuilderForTenant(tenantID).Flush(ctx)`. The new version flushes the entire indexobj builder and reads stats from the resulting object via `stats.Open`:

```go
func flushStatsForTenant(t *testing.T, builder *indexobj.Builder, tenantID string) []stats.Stat {
    t.Helper()
    obj, closer, err := builder.Flush()
    require.NoError(t, err)
    t.Cleanup(func() { _ = closer.Close() })

    var result []stats.Stat
    for _, sec := range obj.Sections() {
        if !stats.CheckSection(sec) { continue }
        opened, err := stats.Open(context.Background(), sec)
        if err != nil { continue }
        rr := stats.NewRowReader(opened)
        if err := rr.Open(context.Background()); err != nil { continue }
        buf := make([]stats.Stat, 100)
        for {
            n, readErr := rr.Read(context.Background(), buf)
            result = append(result, buf[:n]...)
            if readErr != nil { break }
        }
        rr.Close()
    }
    return result
}
```

Note: `builder.Flush()` takes no arguments (not `Flush(ctx)` — the indexobj builder's Flush has no context parameter).

- [ ] **Step 2: Rewrite readAllPostingsForTenant helper**

In `label_postings_calculation_test.go`, replace `readAllPostingsForTenant` (line 19). Same pattern — flush indexobj builder, read postings via `postings.Open`.

- [ ] **Step 3: Fix empty-batch tests**

`TestStatsCalculation_EmptyBatch` (line 202) and `TestLabelPostingsCalculation_EmptyBatch` (line 192) currently check that `builder.StatsBuilderForTenant("tenant-1")` / `builder.PostingsBuilderForTenant("tenant-1")` returns nil.

After accessor deletion, rewrite these tests to: call `builder.Flush()` and check for `indexobj.ErrBuilderEmpty` (or equivalent empty signal), OR verify that the flushed object contains no stats/postings sections. The exact approach depends on whether `Flush` errors or succeeds with an empty object when no data was appended.

- [ ] **Step 4: Run all tests**

Run: `go test -v ./pkg/dataobj/index/...`
Expected: ALL PASS

- [ ] **Step 5: Stage and pause for review**

```bash
git add pkg/dataobj/index/stats_calculation_test.go pkg/dataobj/index/label_postings_calculation_test.go
```

Present a brief summary of changes. **PAUSE** — wait for user to review staged diff and approve before committing.

Suggested commit message: `test(index): update calculation tests to use Open for on-disk round-trip`

---

## Task 9: Final Verification

- [ ] **Step 1: Run all dataobj tests and vet**

Run: `go test ./pkg/dataobj/... && go vet ./pkg/dataobj/...`
Expected: ALL PASS, no vet errors
