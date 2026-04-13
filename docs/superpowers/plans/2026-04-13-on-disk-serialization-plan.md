# On-Disk Serialization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `postings.Builder` and `stats.Builder` implement `dataobj.SectionBuilder` so they serialize to disk via `dataobj.Builder.Append`.

**Architecture:** Replace the in-memory `ColumnarEncoder` (using `pkg/columnar/` Arrow arrays) with on-disk encoding via `dataset.ColumnBuilder` + `columnar.Encoder` + `SectionWriter` — the same path used by streams/pointers/indexpointers. Add `Open` functions for reading sections back from disk. Wire up `indexobj/builder.go` to actually flush stats and postings.

**Tech Stack:** Go, `dataset.ColumnBuilder`, `columnar.Encoder`/`Decoder`, `dataobj.SectionBuilder` interface

**Spec:** `docs/superpowers/specs/2026-04-13-on-disk-serialization-design.md`

---

## File Structure

### New Files
- `pkg/dataobj/sections/postings/open.go` — `Open` function + `ColumnReader` bridge type
- `pkg/dataobj/sections/stats/open.go` — `Open` function + `ColumnReader` bridge type

### Modified Files
- `pkg/dataobj/sections/postings/postings.go` — `ColumnType` enum, `SectionEncoder` signature change
- `pkg/dataobj/sections/postings/encode_columnar.go` — Replace `ColumnarEncoder` with `ColumnarSectionEncoder`
- `pkg/dataobj/sections/postings/builder.go` — `Flush(w SectionWriter)`, remove `computeSplits`/`targetSectionSize`
- `pkg/dataobj/sections/postings/builder_test.go` — Rewrite for on-disk round-trip
- `pkg/dataobj/sections/stats/stats.go` — `ColumnType` enum, `SectionEncoder` signature change
- `pkg/dataobj/sections/stats/encode_columnar.go` — Replace `ColumnarEncoder` with `ColumnarSectionEncoder`
- `pkg/dataobj/sections/stats/builder.go` — `Flush(w SectionWriter)`, remove `computeSplits`/`targetSectionSize`
- `pkg/dataobj/sections/stats/builder_test.go` — Rewrite for on-disk round-trip
- `pkg/dataobj/index/indexobj/builder.go` — Wire up stats/postings flushing, dirty state, size tracking
- `pkg/dataobj/index/stats_calculation_test.go` — Use `Open` instead of test accessors
- `pkg/dataobj/index/label_postings_calculation_test.go` — Use `Open` instead of test accessors

---

## Task 1: Postings — ColumnType Enum and SectionEncoder Signature

**Files:**

- Modify: `pkg/dataobj/sections/postings/postings.go:14,57-74`

- [ ] **Step 1: Add ColumnType enum**

Add the `ColumnType` enum, `columnTypeNames` map, `ParseColumnType`, and `String()` methods to `postings.go`, after the existing `Posting` struct. Follow the pattern from `streams/streams.go:96-153`.

```go
// ColumnType identifies the type of a column in a postings section.
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

var columnTypeNames = map[ColumnType]string{
	ColumnTypeKind:             "kind",
	ColumnTypeObjectPath:       "object_path",
	ColumnTypeSectionIndex:     "section_index",
	ColumnTypeColumnName:       "column_name",
	ColumnTypeLabelValue:       "label_value",
	ColumnTypeBloomFilter:      "bloom_filter",
	ColumnTypeStreamIDBitmap:   "stream_id_bitmap",
	ColumnTypeUncompressedSize: "uncompressed_size",
	ColumnTypeMinTimestamp:     "min_timestamp",
	ColumnTypeMaxTimestamp:     "max_timestamp",
}

// String returns the string representation of the ColumnType.
func (ct ColumnType) String() string {
	if name, ok := columnTypeNames[ct]; ok {
		return name
	}
	return fmt.Sprintf("ColumnType(%d)", int(ct))
}

// ParseColumnType parses a string into a ColumnType. Returns
// ColumnTypeInvalid if the string is not recognized.
func ParseColumnType(s string) ColumnType {
	for ct, name := range columnTypeNames {
		if name == s {
			return ct
		}
	}
	return ColumnTypeInvalid
}
```

- [ ] **Step 2: Change SectionEncoder signature**

Replace the old `SectionEncoder` type (line 74) with the new signature:

```go
// SectionEncoder encodes a slice of postings rows into a columnar.Encoder.
// The encoder is responsible for creating dataset.ColumnBuilders, populating
// them from rows, and flushing each into the columnar.Encoder.
type SectionEncoder func(rows []Posting, enc *columnar.Encoder) error
```

Add the import for `columnar`:
```go
"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./pkg/dataobj/sections/postings/...`
Expected: Compilation errors in `builder.go` and `encode_columnar.go` (they still use the old signature). This is expected — we'll fix them in subsequent tasks.

- [ ] **Step 4: Commit**

```bash
git add pkg/dataobj/sections/postings/postings.go
git commit -m "feat(postings): add ColumnType enum and update SectionEncoder signature"
```

---

## Task 2: Postings — ColumnarSectionEncoder

**Files:**

- Rewrite: `pkg/dataobj/sections/postings/encode_columnar.go`

- [ ] **Step 1: Write the new ColumnarSectionEncoder**

Replace the entire contents of `encode_columnar.go` with the new on-disk encoder. The function takes `rows []Posting` and `enc *columnar.Encoder`, creates `dataset.ColumnBuilder`s per the column definition table in the spec, iterates rows, appends values (using `dataset.NullValue()` for irrelevant fields), normalizes bitmaps, flushes columns via `encodeColumn`, and sets sort info.

Key details:
- Column order: kind, object_path, section_index, column_name, label_value, bloom_filter, stream_id_bitmap, uncompressed_size, min_timestamp, max_timestamp
- `kind` uses PLAIN encoding (not DELTA — only 2 values)
- Nullable columns (column_name, label_value, bloom_filter, stream_id_bitmap) use `dataset.NullValue()` when semantically irrelevant for the posting kind
- `bloom_filter` uses no compression (pre-compressed data)
- All other BINARY columns use ZSTD compression
- INT64 columns (except kind) use DELTA encoding
- Bitmap normalization: pad all `StreamIDBitmap` values to max length before encoding
- SortInfo: columns [kind(0), column_name(3), label_value(4), min_timestamp(8), max_timestamp(9)] ascending
- The page size parameters (`PageSizeHint`, `PageMaxRowCount`) are captured in the closure when the encoder is constructed (see Task 5)

The function signature to export:

```go
// ColumnarSectionEncoder returns a SectionEncoder that encodes postings using
// dataset.ColumnBuilder and columnar.Encoder for on-disk serialization.
func ColumnarSectionEncoder(pageSizeHint int, pageMaxRowCount int) SectionEncoder {
    return func(rows []Posting, enc *columnar.Encoder) error {
        // ... implementation
    }
}
```

Include the `encodeColumn` helper (matching streams/pointers pattern) and `normalizeBitmaps` function (preserving the existing logic from `columnarNormalizeBitmaps`).

Delete: the old `ColumnarEncoder` function, `columnarSliceColumnReader` type, and the `columnarNormalizeBitmaps` function (replace with the new `normalizeBitmaps`).

- [ ] **Step 2: Verify compilation**

Run: `go build ./pkg/dataobj/sections/postings/...`
Expected: Still compilation errors in `builder.go` (Flush signature mismatch). This is expected.

- [ ] **Step 3: Commit**

```bash
git add pkg/dataobj/sections/postings/encode_columnar.go
git commit -m "feat(postings): implement ColumnarSectionEncoder using dataset.ColumnBuilder"
```

---

## Task 3: Postings — Builder Implements SectionBuilder

**Files:**

- Modify: `pkg/dataobj/sections/postings/builder.go`

- [ ] **Step 1: Update Builder struct and constructor**

Remove `targetSectionSize` from the `Builder` struct (line 35). Change `NewBuilder` to:

```go
func NewBuilder(encode SectionEncoder) *Builder {
    return &Builder{
        encode: encode,
    }
}
```

Remove: `defaultTargetSectionSize` constant (line 43).

- [ ] **Step 2: Change Flush to implement SectionBuilder**

Replace the current `Flush(ctx context.Context) ([]Section, error)` (lines 114-139) with:

```go
func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
    if len(b.rows) == 0 {
        return 0, nil
    }
    sort.SliceStable(b.rows, func(i, j int) bool {
        return b.comparePostings(b.rows[i], b.rows[j])
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

Add the `columnar` import:
```go
"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
```

Remove the `context` import (no longer needed).

- [ ] **Step 3: Delete computeSplits**

Delete the `computeSplits` method (lines 142-166).

- [ ] **Step 4: Verify compilation**

Run: `go build ./pkg/dataobj/sections/postings/...`
Expected: PASS (builder now compiles with new Flush signature matching SectionBuilder interface)

- [ ] **Step 5: Commit**

```bash
git add pkg/dataobj/sections/postings/builder.go
git commit -m "feat(postings): implement SectionBuilder interface on Builder"
```

---

## Task 4: Postings — Open Function

**Files:**

- Create: `pkg/dataobj/sections/postings/open.go`

- [ ] **Step 1: Write the Open function and ColumnReader bridge**

Create `open.go` with:
1. `Open(ctx context.Context, section *dataobj.Section) (*Section, error)` — validates section type/version, decodes via `columnar.NewDecoder` + `columnar.Open`, populates a `Section` with `ColumnNames`, `RowCount` (from first column's metadata), and an `OpenColumn` closure backed by on-disk data.
2. A concrete `columnReader` type implementing `ColumnReader` — wraps the on-disk column reading, produces `*columnar.Number[int64]` or `*columnar.UTF8` arrays as required by `RowReader` type assertions.

Follow the `streams.Open` pattern (`streams/streams.go:31-53`) for structure, but adapt for the postings `Section` type which uses closures rather than wrapping `columnar.Section` directly.

Key validation:
- Check `section.Type` matches `sectionType` (namespace + kind)
- Check `section.Type.Version == 1`
- Skip unrecognized columns (forward compat)

- [ ] **Step 2: Verify compilation**

Run: `go build ./pkg/dataobj/sections/postings/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add pkg/dataobj/sections/postings/open.go
git commit -m "feat(postings): add Open function for reading postings sections from disk"
```

---

## Task 5: Postings — Rewrite Builder Tests

**Files:**

- Rewrite: `pkg/dataobj/sections/postings/builder_test.go`

- [ ] **Step 1: Add buildObject test helper**

Add a `buildObject` helper at the bottom of the test file, following the `streams/builder_test.go` pattern:

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

Add a `readAllPostings` helper that uses `Open` + `RowReader`:

```go
func readAllPostingsFromObject(t *testing.T, obj *dataobj.Object, sectionIdx int) []postings.Posting {
    t.Helper()
    sec, err := postings.Open(context.Background(), &obj.Sections[sectionIdx])
    require.NoError(t, err)
    // use RowReader to read all postings
    rr := postings.NewRowReader(sec, 100)
    defer rr.Close()
    var result []postings.Posting
    for {
        row, err := rr.ReadRow()
        if err == io.EOF { break }
        require.NoError(t, err)
        result = append(result, row)
    }
    return result
}
```

- [ ] **Step 2: Rewrite all test functions**

Rewrite each test to:
1. Create a builder via `postings.NewBuilder(postings.ColumnarSectionEncoder(pageSizeHint, maxPageRows))` (with reasonable defaults like 1024*1024, 10000)
2. Append postings
3. Call `buildObject(t, b)` to flush to disk
4. Call `readAllPostingsFromObject(t, obj, 0)` to read back
5. Assert the expected values

Preserve all existing test cases:
- `TestBuilder_Empty` — build with no rows, verify `builder.Append` doesn't error (since Flush returns 0,nil for empty)
- `TestBuilder_LabelPostingRoundTrip` — append label posting, round-trip, verify fields
- `TestBuilder_BloomPostingRoundTrip` — append bloom posting, round-trip, verify fields including nil BloomFilter/StreamIDBitmap null semantics
- `TestBuilder_MixedPostings` — both kinds, verify round-trip
- `TestBuilder_SortOrder` — append in unsorted order, verify sorted output
- `TestBuilder_NullableHandling` — verify null round-trip for kind-specific fields
- `TestBuilder_BitmapCorrectness` — verify bitmap data round-trips correctly
- `TestBuilder_BitmapNormalization` — verify bitmaps are padded to same length
- `TestBuilder_SectionSplitting` — **rewrite**: append rows, call `dataobj.Builder.Append` mid-accumulation, append more, flush again, verify 2 sections in object
- `TestBuilder_AllBloom` / `TestBuilder_AllLabel` — single-kind round-trip
- `TestBuilder_FlushResetsBuilder` — flush, verify builder is empty
- `TestBuilder_Type` — verify `Type()` returns correct section type
- `TestRowReader_SmallBuffer` — verify small read buffer works

- [ ] **Step 3: Run tests**

Run: `go test -v ./pkg/dataobj/sections/postings/...`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/dataobj/sections/postings/builder_test.go
git commit -m "test(postings): rewrite builder tests for on-disk round-trip"
```

---

## Task 6: Stats — ColumnType Enum and SectionEncoder Signature

**Files:**

- Modify: `pkg/dataobj/sections/stats/stats.go:14,38-55`

- [ ] **Step 1: Add ColumnType enum**

Same pattern as postings. Add after the `Stat` struct:

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
	ColumnTypeLabel                       // "label" — dynamic label columns; tag carries label name
)
```

With `columnTypeNames`, `ParseColumnType`, `String()`.

- [ ] **Step 2: Change SectionEncoder signature**

```go
type SectionEncoder func(rows []Stat, enc *columnar.Encoder) error
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./pkg/dataobj/sections/stats/...`
Expected: Compilation errors in `builder.go` and `encode_columnar.go` (expected).

- [ ] **Step 4: Commit**

```bash
git add pkg/dataobj/sections/stats/stats.go
git commit -m "feat(stats): add ColumnType enum and update SectionEncoder signature"
```

---

## Task 7: Stats — ColumnarSectionEncoder

**Files:**

- Rewrite: `pkg/dataobj/sections/stats/encode_columnar.go`

- [ ] **Step 1: Write the new ColumnarSectionEncoder**

Same pattern as postings Task 2, but with stats-specific columns:
- Fixed columns: object_path, section_index, sort_schema, min_timestamp, max_timestamp, row_count, uncompressed_size (indices 0-6)
- Dynamic label columns from SortSchema (indices 7+)
- Column layout change: labels now come AFTER fixed columns (not interleaved)
- SortSchema invariant validation: check all rows have same SortSchema, error if not
- Dynamic label null handling: absent label key -> `dataset.NullValue()`
- SortInfo: dynamic label columns (in order), then min_timestamp (index 3), then max_timestamp (index 4), all ascending

Export as:
```go
func ColumnarSectionEncoder(pageSizeHint int, pageMaxRowCount int) SectionEncoder {
    return func(rows []Stat, enc *columnar.Encoder) error { ... }
}
```

Delete: old `ColumnarEncoder`, `sliceColumnReader`.

- [ ] **Step 2: Verify compilation**

Run: `go build ./pkg/dataobj/sections/stats/...`
Expected: Compilation errors in `builder.go` (expected).

- [ ] **Step 3: Commit**

```bash
git add pkg/dataobj/sections/stats/encode_columnar.go
git commit -m "feat(stats): implement ColumnarSectionEncoder using dataset.ColumnBuilder"
```

---

## Task 8: Stats — Builder Implements SectionBuilder

**Files:**

- Modify: `pkg/dataobj/sections/stats/builder.go`

- [ ] **Step 1: Update Builder struct and constructor**

Remove `targetSectionSize`. Change `NewBuilder`:

```go
func NewBuilder(encode SectionEncoder) *Builder {
    return &Builder{
        encode: encode,
    }
}
```

- [ ] **Step 2: Change Flush to implement SectionBuilder**

Replace `Flush(ctx) ([]Section, error)` with `Flush(w dataobj.SectionWriter) (n int64, err error)`. Include the sort step (sort by label values in sort schema order, then MinTimestamp, then MaxTimestamp — preserving existing sort logic).

- [ ] **Step 3: Delete computeSplits**

Delete the `computeSplits` method.

- [ ] **Step 4: Verify compilation**

Run: `go build ./pkg/dataobj/sections/stats/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/dataobj/sections/stats/builder.go
git commit -m "feat(stats): implement SectionBuilder interface on Builder"
```

---

## Task 9: Stats — Open Function

**Files:**

- Create: `pkg/dataobj/sections/stats/open.go`

- [ ] **Step 1: Write the Open function and ColumnReader bridge**

Same pattern as postings Task 4, adapted for stats:
- Validate section type/version (`Version == 1`)
- Handle dynamic label columns (identified by `ColumnTypeLabel` with tag = label name)
- Populate `Section.RowCount` from first column's metadata
- ColumnReader bridge must produce `*columnar.Number[int64]` for INT64 and `*columnar.UTF8` for BINARY

- [ ] **Step 2: Verify compilation**

Run: `go build ./pkg/dataobj/sections/stats/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add pkg/dataobj/sections/stats/open.go
git commit -m "feat(stats): add Open function for reading stats sections from disk"
```

---

## Task 10: Stats — Rewrite Builder Tests

**Files:**

- Rewrite: `pkg/dataobj/sections/stats/builder_test.go`

- [ ] **Step 1: Add buildObject and readAllStats helpers**

Same pattern as postings Task 5.

- [ ] **Step 2: Rewrite all test functions**

Preserve all existing test cases, adapted for on-disk round-trip:
- `TestBuilder_Empty`
- `TestBuilder_RoundTrip`
- `TestBuilder_SortOrder`
- `TestBuilder_AllSameServiceName`
- `TestBuilder_MissingServiceName`
- `TestBuilder_SectionSplitting` — **rewrite**: mid-accumulation flush pattern
- `TestBuilder_LargeValues`
- `TestBuilder_ResetAndReuse`
- `TestBuilder_EstimatedSize`
- `TestBuilder_FlushResetsBuilder`
- `TestBuilder_Type`
- `TestRowReader_SmallBuffer`

Add negative test:
- `TestBuilder_InconsistentSortSchema` — append rows with different SortSchema, verify encoder returns error

- [ ] **Step 3: Run tests**

Run: `go test -v ./pkg/dataobj/sections/stats/...`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/dataobj/sections/stats/builder_test.go
git commit -m "test(stats): rewrite builder tests for on-disk round-trip"
```

---

## Task 11: indexobj/builder.go — Wire Up Stats and Postings Flushing

**Files:**

- Modify: `pkg/dataobj/index/indexobj/builder.go:125-134,144-210,424-460,545-554`

- [ ] **Step 1: Update constructor helper functions**

Change `getStatsBuilderForTenant` (line 125) and `getPostingsBuilderForTenant` (line 134):

```go
// getStatsBuilderForTenant
// Before: stats.NewBuilder(int(b.cfg.TargetSectionSize), stats.ColumnarEncoder)
// After:
stats.NewBuilder(stats.ColumnarSectionEncoder(int(b.cfg.TargetPageSize), int(b.cfg.MaxPageRows)))

// getPostingsBuilderForTenant
// Before: postings.NewBuilder(int(b.cfg.TargetSectionSize), postings.ColumnarEncoder)
// After:
postings.NewBuilder(postings.ColumnarSectionEncoder(int(b.cfg.TargetPageSize), int(b.cfg.MaxPageRows)))
```

Note: verify `b.cfg.TargetPageSize` and `b.cfg.MaxPageRows` exist in the config. If not, use suitable defaults or the same values used by the streams builder constructor.

- [ ] **Step 2: Add builderStateDirty + size tracking to AppendStat**

At `AppendStat` (line 144), after appending to the tenant builder:

```go
b.state = builderStateDirty

preAppendSizeEstimate := tenantBuilder.EstimatedSize()
tenantBuilder.Append(stat)
postAppendSizeEstimate := tenantBuilder.EstimatedSize()

b.unflushedSizeEstimate += postAppendSizeEstimate - preAppendSizeEstimate

if postAppendSizeEstimate > int(b.cfg.TargetSectionSize) {
    flushedSize := tenantBuilder.EstimatedSize()
    if err := b.builder.Append(tenantBuilder); err != nil {
        return err
    }
    b.unflushedSizeEstimate -= flushedSize
}

b.currentSizeEstimate = b.estimatedSize()
if b.currentSizeEstimate > int(b.cfg.TargetObjectSize) {
    b.builderFull = true
}
```

- [ ] **Step 3: Add builderStateDirty + size tracking to AppendLabelPosting**

Same pattern at `AppendLabelPosting` (line 165).

- [ ] **Step 4: Add builderStateDirty + size tracking to AppendBloomPosting**

Same pattern at `AppendBloomPosting` (line 189).

- [ ] **Step 5: Wire up final Flush**

In `Flush` (around line 451), replace the no-op reset loops with actual serialization:

```go
// Replace:
// for _, tenantStats := range b.stats { tenantStats.Reset() }
// for _, tenantPostings := range b.postings { tenantPostings.Reset() }

// With:
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

- [ ] **Step 6: Delete test accessors**

Delete `StatsBuilderForTenant` (line 545) and `PostingsBuilderForTenant` (line 552).

- [ ] **Step 7: Verify compilation**

Run: `go build ./pkg/dataobj/index/...`
Expected: Compilation errors in test files that use deleted accessors. This is expected — we fix those next.

- [ ] **Step 8: Commit**

```bash
git add pkg/dataobj/index/indexobj/builder.go
git commit -m "feat(indexobj): wire up stats/postings flushing, add dirty state + size tracking"
```

---

## Task 12: Update Calculation Tests

**Files:**

- Modify: `pkg/dataobj/index/stats_calculation_test.go`
- Modify: `pkg/dataobj/index/label_postings_calculation_test.go`

- [ ] **Step 1: Rewrite flushStatsForTenant helper**

In `stats_calculation_test.go`, replace `flushStatsForTenant` (line 46) which currently calls `builder.StatsBuilderForTenant(tenantID).Flush(ctx)` with a version that flushes the entire indexobj builder and reads stats from the resulting object via `stats.Open`:

```go
func flushStatsForTenant(t *testing.T, builder *indexobj.Builder, tenantID string) []stats.Stat {
    t.Helper()
    obj, closer, err := builder.Flush(context.Background())
    require.NoError(t, err)
    t.Cleanup(func() { _ = closer.Close() })

    var result []stats.Stat
    for i := range obj.Sections {
        if obj.Sections[i].Type.Kind != "stats" {
            continue
        }
        sec, err := stats.Open(context.Background(), &obj.Sections[i])
        if err != nil { continue }
        rr := stats.NewRowReader(sec, 100)
        for {
            row, err := rr.ReadRow()
            if err != nil { break }
            result = append(result, row)
        }
        rr.Close()
    }
    return result
}
```

- [ ] **Step 2: Rewrite readAllPostingsForTenant helper**

In `label_postings_calculation_test.go`, replace `readAllPostingsForTenant` (line 19) with a version that flushes the indexobj builder and reads postings via `postings.Open`. Same pattern as Step 1.

Note: Some tests may need adjustment since `builder.Flush` consumes the builder state. If tests call `flushStatsForTenant`/`readAllPostingsForTenant` multiple times, they'll need to rebuild the data between calls. Check each test and adjust accordingly.

- [ ] **Step 3: Run all tests**

Run: `go test -v ./pkg/dataobj/index/...`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/dataobj/index/stats_calculation_test.go pkg/dataobj/index/label_postings_calculation_test.go
git commit -m "test(index): update calculation tests to use Open for on-disk round-trip"
```

---

## Task 13: Final Verification

- [ ] **Step 1: Run all postings tests**

Run: `go test -v ./pkg/dataobj/sections/postings/...`
Expected: ALL PASS

- [ ] **Step 2: Run all stats tests**

Run: `go test -v ./pkg/dataobj/sections/stats/...`
Expected: ALL PASS

- [ ] **Step 3: Run all index tests**

Run: `go test -v ./pkg/dataobj/index/...`
Expected: ALL PASS

- [ ] **Step 4: Run all dataobj tests**

Run: `go test ./pkg/dataobj/...`
Expected: ALL PASS

- [ ] **Step 5: Run linter**

Run: `make lint` or `golangci-lint run ./pkg/dataobj/...`
Expected: No new lint errors

- [ ] **Step 6: Verify no unused imports or dead code**

Run: `go vet ./pkg/dataobj/...`
Expected: PASS
