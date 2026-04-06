# New Index Sections Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add stats and postings sections to index objects, using the new dataset API, to enable sort-schema-based storage locality for index data.

**Architecture:** Two new section packages (`sections/stats/`, `sections/postings/`) use the `pkg/dataset/array` codec for columnar encoding. They integrate into the existing index builder pipeline via new calculation steps and `indexobj.Builder` methods. A `types.Binary` type is added to the dataset package. Section serialization (on-disk format) is deferred to a separate PR.

**Tech Stack:** Go, `pkg/dataset/array` (Arrow-aligned columnar codec), `pkg/dataobj` (section framework), protobuf (future serialization)

**Spec:** `docs/superpowers/specs/2026-04-03-new-index-sections-design.md`

---

## Task 0: Add `types.Binary` to the Dataset Package

**PR 0** — ~25 lines. Adds a `Binary` logical type so binary columns (bloom filters, bitmaps) are not misrepresented as UTF8.

**Files:**

- Modify: `pkg/dataset/types/kind.go:7-18`
- Modify: `pkg/dataset/types/types.go:18-40,178-188`
- Modify: `pkg/dataset/array/codec_binary.go:28-33,195-200`
- Create: `pkg/dataset/array/codec_binary_type_test.go`

- [ ] **Step 1: Add `KindBinary` to the Kind enum**

In `pkg/dataset/types/kind.go`, add `KindBinary` after `KindList`:

```go
KindList    // KindList represents a variable-length sequence of a single element type.
KindBinary  // KindBinary represents opaque binary data.
```

Update the `kindNames` array to include:

```go
KindBinary: "binary",
```

- [ ] **Step 2: Add `Binary` type struct**

In `pkg/dataset/types/types.go`, add to the type block (after `UTF8`):

```go
// Binary describes opaque variable-length binary data. Physically identical
// to UTF8 (offsets + data buffer) but logically distinct — no UTF-8
// validation is implied.
Binary struct{ Nullable bool }
```

Add the method set (after the `UTF8` methods):

```go
func (t *Binary) Kind() Kind   { return KindBinary }
func (t *Binary) String() string { return "binary" + nullable(t.Nullable) }
func (t *Binary) isType()       {}
```

- [ ] **Step 3: Relax binary codec type guards**

In `pkg/dataset/array/codec_binary.go`, function `newBinaryWriter` (line 31), change:

```go
} else if got, want := typ.Kind(), types.KindUTF8; got != want {
```

to:

```go
} else if got := typ.Kind(); got != types.KindUTF8 && got != types.KindBinary {
```

Update the error message to: `"expected type utf8 or binary, got %s"`.

Make the same change in `newBinaryReader` (line 198). Also relax the type assertion on line 37 (`typ.(*types.UTF8)`) and line 203 (`arr.Type.(*types.UTF8)`) — extract `Nullable` via a helper or interface to handle both `*types.UTF8` and `*types.Binary`. The simplest approach:

```go
// In newBinaryWriter, replace:
//   utf8Typ = typ.(*types.UTF8)
// With:
var isNullable bool
switch t := typ.(type) {
case *types.UTF8:
    isNullable = t.Nullable
case *types.Binary:
    isNullable = t.Nullable
}
```

Apply the same pattern in `newBinaryReader`.

- [ ] **Step 4: Write test for Binary round-trip**

Create `pkg/dataset/array/codec_binary_type_test.go`:

```go
package array_test

import (
    "context"
    "io"
    "testing"

    "github.com/stretchr/testify/require"

    "github.com/grafana/loki/v3/pkg/columnar"
    "github.com/grafana/loki/v3/pkg/dataset/array"
    "github.com/grafana/loki/v3/pkg/dataset/types"
    "github.com/grafana/loki/v3/pkg/memory"
)

func TestBinaryTypeRoundTrip(t *testing.T) {
    alloc := memory.NewAllocator(nil)
    defer alloc.Free()

    store := &inMemoryStore{}

    spec := &array.SpecBinary{Offsets: &array.SpecPlain{}}
    writer, err := array.NewWriter(alloc, spec, &types.Binary{})
    require.NoError(t, err)

    // Write some opaque binary data (not valid UTF-8).
    data := [][]byte{{0xFF, 0xFE, 0x00}, {0x01, 0x02}}
    builder := columnar.NewUTF8Builder(alloc)
    for _, d := range data {
        builder.AppendBytes(d)
    }
    err = writer.Append(builder.Build())
    require.NoError(t, err)

    arr, err := writer.Flush(context.Background(), store)
    require.NoError(t, err)
    require.Equal(t, types.KindBinary, arr.Type.Kind())

    reader, err := array.NewReader(alloc, arr, store)
    require.NoError(t, err)
    defer reader.Close()

    result, err := reader.Read(context.Background(), alloc, 10)
    require.NoError(t, err)

    utf8Arr := result.(*columnar.UTF8)
    require.Equal(t, 2, utf8Arr.Len())
    require.Equal(t, data[0], utf8Arr.ValueBytes(0))
    require.Equal(t, data[1], utf8Arr.ValueBytes(1))

    _, err = reader.Read(context.Background(), alloc, 10)
    require.ErrorIs(t, err, io.EOF)
}

func TestBinaryNullableRoundTrip(t *testing.T) {
    alloc := memory.NewAllocator(nil)
    defer alloc.Free()

    store := &inMemoryStore{}

    spec := &array.SpecBinary{
        Offsets:  &array.SpecPlain{},
        Validity: &array.SpecBool{},
    }
    writer, err := array.NewWriter(alloc, spec, &types.Binary{Nullable: true})
    require.NoError(t, err)

    builder := columnar.NewUTF8Builder(alloc)
    builder.AppendBytes([]byte{0xDE, 0xAD})
    builder.AppendNull()
    builder.AppendBytes([]byte{0xBE, 0xEF})
    err = writer.Append(builder.Build())
    require.NoError(t, err)

    arr, err := writer.Flush(context.Background(), store)
    require.NoError(t, err)

    reader, err := array.NewReader(alloc, arr, store)
    require.NoError(t, err)
    defer reader.Close()

    result, err := reader.Read(context.Background(), alloc, 10)
    require.NoError(t, err)

    utf8Arr := result.(*columnar.UTF8)
    require.Equal(t, 3, utf8Arr.Len())
    require.True(t, utf8Arr.IsValid(0))
    require.False(t, utf8Arr.IsValid(1))
    require.True(t, utf8Arr.IsValid(2))
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./pkg/dataset/... -v -count=1`

Expected: All existing tests pass, plus new `TestBinaryTypeRoundTrip` and `TestBinaryNullableRoundTrip` pass.

- [ ] **Step 6: Commit**

```
git add pkg/dataset/
git commit -m "feat(dataset): add types.Binary logical type for opaque binary data"
```

---

## Task 1: Stats Section Package

**PR 1** — New `sections/stats/` package with builder, reader, row reader, and in-memory unit tests.

**Files:**

- Create: `pkg/dataobj/sections/stats/stats.go`
- Create: `pkg/dataobj/sections/stats/builder.go`
- Create: `pkg/dataobj/sections/stats/reader.go`
- Create: `pkg/dataobj/sections/stats/row_reader.go`
- Create: `pkg/dataobj/sections/stats/builder_test.go`
- Reference: `pkg/dataobj/sections/streams/streams.go` (SectionType pattern)
- Reference: `pkg/dataobj/sections/pointers/builder.go` (EstimatedSize, SetTenant patterns)
- Reference: `pkg/dataset/array/codec_util_test.go` (inMemoryStore)

### Sub-task 1a: Section type and column definitions

- [ ] **Step 1: Create `stats.go` with type registration and column enum**

Create `pkg/dataobj/sections/stats/stats.go`:

```go
package stats

import "github.com/grafana/loki/v3/pkg/dataobj"

// SectionType identifies stats sections in a data object.
var SectionType = dataobj.SectionType{
    Namespace: "github.com/grafana/loki",
    Kind:      "stats",
    Version:   1,
}

// CheckSection returns true if the section is a stats section.
func CheckSection(section *dataobj.Section) bool {
    return SectionType.Equals(section.Type)
}

// Stat represents a single row in the stats section.
type Stat struct {
    ObjectPath       string
    SectionIndex     int64
    RunID            int64
    SortSchema       string
    ServiceName      string // Dynamic label column (hardcoded to service_name for now)
    MinTimestamp      int64  // UnixNano
    MaxTimestamp      int64  // UnixNano
    RowCount         int64
    UncompressedSize int64
}
```

- [ ] **Step 2: Commit**

```
git commit -m "feat(stats): add section type registration and Stat row struct"
```

### Sub-task 1b: Stats builder

- [ ] **Step 3: Write failing test for builder round-trip**

Create `pkg/dataobj/sections/stats/builder_test.go` with a test that:
1. Creates a `stats.Builder`
2. Appends several `Stat` rows with different service_name values
3. Calls `Flush()` to get back column data via in-memory Sink/Source
4. Reads back and verifies rows match, sorted by `[service_name, min_timestamp]`

The test should cover:
- Basic round-trip (2-3 rows with different service names)
- Sort order verification (rows returned sorted by service_name then min_timestamp)
- Empty builder (flush returns zero sections)
- Missing service_name (empty string)

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./pkg/dataobj/sections/stats/... -v -run TestBuilder -count=1`

Expected: FAIL — `Builder` type not defined.

- [ ] **Step 5: Implement `builder.go`**

Create `pkg/dataobj/sections/stats/builder.go`:

The builder should:
- Hold a `[]Stat` slice for accumulation
- Implement `Append(stat Stat)` to add rows
- Implement `EstimatedSize() int` (sum of per-row estimates: 8 bytes per Int64 column + len(string) for string columns)
- Implement `SetTenant(string)` / `Tenant() string`
- Implement `Type() dataobj.SectionType`
- Implement `Flush()` that:
  1. Sorts rows by `[service_name, min_timestamp]`
  2. Creates `array.Writer` per column with appropriate specs
  3. Builds `columnar.Array` values from the sorted rows
  4. Appends to writers, flushes to sink
  5. Returns `[]array.Array` (one per column) and column names
  6. Supports splitting: when accumulated row size exceeds `targetSectionSize`, flush current batch and start new section
- Implement `Reset()` to clear state

Key patterns:
- Int64 columns: `array.NewWriter(alloc, &array.SpecPlain{}, &types.Int64{})`
- UTF8 columns: `array.NewWriter(alloc, &array.SpecBinary{Offsets: &array.SpecPlain{}}, &types.UTF8{})`
- Use `inMemoryStore` pattern from `codec_util_test.go` for the Sink

- [ ] **Step 6: Run tests and verify they pass**

Run: `go test ./pkg/dataobj/sections/stats/... -v -count=1`

Expected: PASS

- [ ] **Step 7: Commit**

```
git commit -m "feat(stats): implement builder with sort, split, and in-memory round-trip"
```

### Sub-task 1c: Stats reader and row reader

- [ ] **Step 8: Write failing test for row reader**

Add test to `builder_test.go` (or a new `row_reader_test.go`) that:
1. Builds a stats section via the builder
2. Creates a `RowReader` from the flushed arrays
3. Iterates and verifies each `Stat` struct matches expected values

- [ ] **Step 9: Run test to verify it fails**

Expected: FAIL — `RowReader` not defined.

- [ ] **Step 10: Implement `reader.go` and `row_reader.go`**

`reader.go` — Provides columnar read access to stats section arrays via `array.Reader`.

`row_reader.go` — Wraps the reader to return typed `Stat` structs. Pattern matches `sections/streams/row_reader.go`.

- [ ] **Step 11: Run tests and verify they pass**

Run: `go test ./pkg/dataobj/sections/stats/... -v -count=1`

Expected: PASS

- [ ] **Step 12: Add edge case and splitting tests**

Add tests for:
- Section splitting: set small `targetSectionSize`, verify multiple section outputs
- Empty builder: flush produces no sections
- All rows same service_name: sort is stable
- Large values: long object paths and label values

- [ ] **Step 13: Run all tests**

Run: `go test ./pkg/dataobj/sections/stats/... -v -count=1`

Expected: PASS

- [ ] **Step 14: Commit**

```
git commit -m "feat(stats): add reader, row reader, and comprehensive tests"
```

---

## Task 2: Postings Section Package

**PR 2** — New `sections/postings/` package. Same structure as Task 1 but with nullable columns and bitmap handling. Depends on Task 0 (types.Binary).

**Files:**

- Create: `pkg/dataobj/sections/postings/postings.go`
- Create: `pkg/dataobj/sections/postings/builder.go`
- Create: `pkg/dataobj/sections/postings/reader.go`
- Create: `pkg/dataobj/sections/postings/row_reader.go`
- Create: `pkg/dataobj/sections/postings/bitmap.go`
- Create: `pkg/dataobj/sections/postings/builder_test.go`
- Create: `pkg/dataobj/sections/postings/bitmap_test.go`

### Sub-task 2a: Bitmap utilities

- [ ] **Step 1: Write failing tests for bitmap helpers**

Create `pkg/dataobj/sections/postings/bitmap_test.go`:

```go
func TestBitmapSetGet(t *testing.T) {
    bm := NewBitmap(0)
    bm.Set(1)
    bm.Set(5)
    bm.Set(8)
    require.True(t, bm.IsSet(1))
    require.True(t, bm.IsSet(5))
    require.True(t, bm.IsSet(8))
    require.False(t, bm.IsSet(0))
    require.False(t, bm.IsSet(2))
}

func TestBitmapLSBNumbering(t *testing.T) {
    bm := NewBitmap(0)
    bm.Set(0) // bit 0 in byte 0
    bm.Set(7) // bit 7 in byte 0
    bytes := bm.Bytes()
    // LSB numbering: bit 0 = 0x01, bit 7 = 0x80
    require.Equal(t, byte(0x81), bytes[0])
}

func TestBitmapPadTo(t *testing.T) {
    bm := NewBitmap(0)
    bm.Set(3)
    bm.PadTo(24) // 3 bytes
    require.Equal(t, 3, len(bm.Bytes()))
}
```

- [ ] **Step 2: Implement `bitmap.go`**

Create `pkg/dataobj/sections/postings/bitmap.go`:

```go
package postings

// Bitmap is a growable bit-packed bitmap using LSB numbering
// (Arrow validity bitmap convention).
type Bitmap struct {
    data []byte
}

func NewBitmap(initialBits int) *Bitmap { ... }
func (b *Bitmap) Set(bit int)           { /* grow + set bit at data[bit/8] |= 1 << (bit % 8) */ }
func (b *Bitmap) IsSet(bit int) bool    { ... }
func (b *Bitmap) Bytes() []byte         { return b.data }
func (b *Bitmap) PadTo(bits int)        { /* grow to ceil(bits/8) bytes, zero-filled */ }
```

- [ ] **Step 3: Run bitmap tests**

Run: `go test ./pkg/dataobj/sections/postings/... -v -run TestBitmap -count=1`

Expected: PASS

- [ ] **Step 4: Commit**

```
git commit -m "feat(postings): add LSB bitmap utility with Arrow-compatible encoding"
```

### Sub-task 2b: Section type and row struct

- [ ] **Step 5: Create `postings.go`**

Pattern matches `stats.go` but with the `Posting` struct:

```go
type PostingKind int64

const (
    KindBloom PostingKind = 0
    KindLabel PostingKind = 1
)

type Posting struct {
    Kind             PostingKind
    ObjectPath       string
    SectionIndex     int64
    ColumnName       string
    LabelValue       *string  // nil for Bloom postings
    BloomFilter      []byte   // nil for Label postings
    StreamIDBitmap   []byte   // always present
    UncompressedSize int64
    MinTimestamp      int64
    MaxTimestamp      int64
}
```

- [ ] **Step 6: Commit**

```
git commit -m "feat(postings): add section type registration and Posting row struct"
```

### Sub-task 2c: Postings builder

- [ ] **Step 7: Write failing test for builder round-trip**

Test should cover:
- Label postings with non-nil label_value, nil bloom_filter
- Bloom postings with non-nil bloom_filter, nil label_value
- Sort order: `[kind, column_name, label_value, min_timestamp]`
- Nullable column handling (label_value null for Bloom, bloom_filter null for Label)
- Bitmap correctness (build bitmap with known stream IDs, verify bytes match LSB encoding)
- Binary type columns (`bloom_filter`, `stream_id_bitmap` use `types.Binary{}`)

- [ ] **Step 8: Implement `builder.go`**

Same accumulate-sort-split pattern as stats builder, but with:
- Nullable UTF8 column for `label_value`: `types.UTF8{Nullable: true}` with `SpecBinary{Offsets: &SpecPlain{}, Validity: &SpecBool{}}`
- Nullable Binary column for `bloom_filter`: `types.Binary{Nullable: true}` with same spec pattern
- Non-nullable Binary column for `stream_id_bitmap`: `types.Binary{}`
- Bitmap normalization on flush: determine max stream ID across all postings, pad all bitmaps to same length

- [ ] **Step 9: Run tests and iterate**

Run: `go test ./pkg/dataobj/sections/postings/... -v -count=1`

- [ ] **Step 10: Commit**

```
git commit -m "feat(postings): implement builder with nullable columns, bitmap normalization, and sort"
```

### Sub-task 2d: Postings reader and row reader

- [ ] **Step 11: Write failing test for row reader**

- [ ] **Step 12: Implement `reader.go` and `row_reader.go`**

- [ ] **Step 13: Add splitting and edge case tests**

Cover: section splitting, all-bloom postings, all-label postings, mixed, empty builder.

- [ ] **Step 14: Run all tests**

Run: `go test ./pkg/dataobj/sections/postings/... -v -count=1`

Expected: PASS

- [ ] **Step 15: Commit**

```
git commit -m "feat(postings): add reader, row reader, and comprehensive tests"
```

---

## Task 3: Pipeline Integration

**PR 3** — Wire new sections into the index builder. Depends on Tasks 0, 1, 2.

**Files:**

- Modify: `pkg/dataobj/index/calculate.go:75-130` (add `streamLabelsByTenant`, pass to processLogsSection)
- Modify: `pkg/dataobj/index/calculate.go:22-47` (add `streamLabels` to context, register new steps)
- Create: `pkg/dataobj/index/stats_calculation.go`
- Create: `pkg/dataobj/index/label_postings_calculation.go`
- Modify: `pkg/dataobj/index/column_values.go` (extend with bloom posting state)
- Modify: `pkg/dataobj/index/indexobj/builder.go` (add new fields, methods, flush loop)
- Create: `pkg/dataobj/index/stats_calculation_test.go`
- Create: `pkg/dataobj/index/label_postings_calculation_test.go`

### Sub-task 3a: Add `streamLabels` to calculation context

- [ ] **Step 1: Add `streamLabels` field to `logsCalculationContext`**

In `pkg/dataobj/index/calculate.go`, add to the struct (around line 33):

```go
streamLabels   map[int64]labels.Labels  // source stream ID -> labels
```

- [ ] **Step 2: Add `streamLabelsByTenant` local variable in `Calculate()`**

In `Calculate()` (around line 81), add alongside `streamIDLookupByTenant`:

```go
var streamLabelsByTenant sync.Map
```

- [ ] **Step 3: Populate `streamLabels` in `processStreamsSection`**

After the existing stream processing loop, collect labels into a map and store it:

```go
streamLabels := make(map[int64]labels.Labels, len(streams))
for _, stream := range streams {
    streamLabels[stream.ID] = stream.Labels
}
streamLabelsByTenant.Store(section.Tenant, streamLabels)
```

Note: `processStreamsSection` needs the `streamLabelsByTenant` reference passed in. Update its signature.

- [ ] **Step 4: Thread `streamLabels` into `processLogsSection`**

Load from `streamLabelsByTenant` alongside `streamIDLookup` and pass to the context:

```go
streamLabelsVal, ok := streamLabelsByTenant.Load(section.Tenant)
if !ok {
    return fmt.Errorf("stream labels not found for tenant %s", section.Tenant)
}
// Set on the context:
ctx.streamLabels = streamLabelsVal.(map[int64]labels.Labels)
```

- [ ] **Step 5: Run existing tests to verify no regression**

Run: `go test ./pkg/dataobj/index/... -v -count=1`

Expected: PASS (no behavior change yet)

- [ ] **Step 6: Commit**

```
git commit -m "feat(index): add streamLabels to calculation context for new section support"
```

### Sub-task 3b: New `indexobj.Builder` fields and methods

- [ ] **Step 7: Add stats and postings builder maps to `indexobj.Builder`**

In `pkg/dataobj/index/indexobj/builder.go`, add:

```go
stats    map[string]*stats.Builder     // key=TenantID
postings map[string]*postings.Builder  // key=TenantID
```

Initialize in `NewBuilder`. Add lazy-creation helpers following `getIndexPointerBuilderForTenant` pattern.

- [ ] **Step 8: Implement `AppendStat`, `AppendLabelPosting`, `AppendBloomPosting`**

Each method:
1. Gets-or-creates the per-tenant builder
2. Records `preSize := builder.EstimatedSize()`
3. Appends the row
4. Records `postSize := builder.EstimatedSize()`
5. Updates `b.unflushedSizeEstimate += postSize - preSize`

- [ ] **Step 9: Update `Flush()` to write new sections**

After pointers flush, before indexpointers flush:

```go
// Flush stats sections
for _, tenantStats := range b.stats {
    if err := b.builder.Append(tenantStats); err != nil { ... }
}
// Flush postings sections
for _, tenantPostings := range b.postings {
    if err := b.builder.Append(tenantPostings); err != nil { ... }
}
```

- [ ] **Step 10: Update `Reset()` to clear new maps**

- [ ] **Step 11: Run existing tests**

Run: `go test ./pkg/dataobj/index/... -v -count=1`

Expected: PASS

- [ ] **Step 12: Commit**

```
git commit -m "feat(indexobj): add stats and postings builders with Append methods and flush integration"
```

### Sub-task 3c: Stats calculation step

- [ ] **Step 13: Write failing test for `statsCalculation`**

Create `pkg/dataobj/index/stats_calculation_test.go`:
- Build a mock `logsCalculationContext` with known `streamLabels`
- Feed batches of `logs.Record` through `ProcessBatch`
- Call `Flush` and verify `AppendStat` was called with correct aggregates

- [ ] **Step 14: Implement `stats_calculation.go`**

Create `pkg/dataobj/index/stats_calculation.go` implementing `logsIndexCalculation`:
- `Prepare`: initialize `aggregates` map
- `ProcessBatch`: for each record, look up stream labels, extract `service_name`, accumulate
- `Flush`: compute run-ID (`hash/fnv.New64a()` over `context.objectPath`), sort, call `builder.AppendStat()`

- [ ] **Step 15: Run test**

Run: `go test ./pkg/dataobj/index/... -v -run TestStats -count=1`

Expected: PASS

- [ ] **Step 16: Commit**

```
git commit -m "feat(index): implement statsCalculation step"
```

### Sub-task 3d: Label postings calculation step

- [ ] **Step 17: Write failing test for `labelPostingsCalculation`**

Create `pkg/dataobj/index/label_postings_calculation_test.go`:
- Build context with 3 streams, 2 service_name values
- Feed records and verify bitmaps + aggregates on flush
- Verify bitmap normalization (all bitmaps same size)

- [ ] **Step 18: Implement `label_postings_calculation.go`**

- `Prepare`: initialize postings map
- `ProcessBatch`: for each record, look up labels, build per-(column_name, label_value) bitmaps
- `Flush`: normalize bitmaps (pad to max stream ID), sort, call `builder.AppendLabelPosting()`

- [ ] **Step 19: Run test**

Expected: PASS

- [ ] **Step 20: Commit**

```
git commit -m "feat(index): implement labelPostingsCalculation step"
```

### Sub-task 3e: Extend `columnValuesCalculation` with bloom posting support

- [ ] **Step 21: Write failing test for bloom posting extension**

Add test to verify that after `Flush`, `AppendBloomPosting` is called with correct bloom bytes, bitmap, timestamps, and sizes per column.

- [ ] **Step 22: Extend `column_values.go`**

Add the 4 new maps (`columnStreamBitmaps`, `columnMinTimestamps`, `columnMaxTimestamps`, `columnSizes`). Update `Prepare` to initialize them, `ProcessBatch` to populate them, `Flush` to call `builder.AppendBloomPosting()`.

- [ ] **Step 23: Run tests**

Run: `go test ./pkg/dataobj/index/... -v -count=1`

Expected: PASS

- [ ] **Step 24: Commit**

```
git commit -m "feat(index): extend columnValuesCalculation with bloom posting support"
```

### Sub-task 3f: Register new calculation steps

- [ ] **Step 25: Add new steps to `getLogsCalculationSteps()`**

In `calculate.go`, update:

```go
func getLogsCalculationSteps() []logsIndexCalculation {
    return []logsIndexCalculation{
        &streamStatisticsCalculation{},
        &columnValuesCalculation{},
        &statsCalculation{},
        &labelPostingsCalculation{},
    }
}
```

- [ ] **Step 26: Run full test suite**

Run: `go test ./pkg/dataobj/index/... -v -count=1`

Expected: PASS (all existing + new tests)

- [ ] **Step 27: Commit**

```
git commit -m "feat(index): register stats and label postings calculation steps"
```

---

## Task 4: Section Storage Format (Sub-Spec + Implementation)

**PR 4** — Deferred. Define and implement on-disk serialization for new dataset API sections. Draws from Vortex prior art. **Check with Robert first** — he may have work in flight.

**Files:**

- Create: `docs/superpowers/specs/2026-XX-XX-section-storage-format.md` (sub-spec)
- Create: `pkg/dataobj/sections/internal/datasetenc/` (new package for dataset section encoding)
- Create: protobuf or FlatBuffer schema for Array metadata

This task is intentionally underspecified in the plan because:
1. Robert may provide a serialization solution as part of `pkg/dataset`
2. The sub-spec must be written and reviewed before implementation
3. The Vortex prior art needs detailed study to determine the right adaptation

**Scope of the sub-spec:**
- Array metadata protobuf/FlatBuffer schema (recursive ArrayNode tree)
- Buffer descriptor mapping (byte offsets in section data region)
- Section metadata layout (how to find column arrays)
- Integration with existing `SectionWriter`/`SectionReader` interfaces
- `Sink`/`Source` implementations backed by section data region

---

## Task 5: On-Disk Integration Tests

**PR 5** — End-to-end tests through the full pipeline. Depends on Tasks 3 and 4.

**Files:**

- Create: `pkg/dataobj/index/integration_test.go` (or extend existing)
- Modify: `pkg/dataobj/index/indexobj/builder.go` (add `observeObject` cases for new sections)

- [ ] **Step 1: Write full pipeline round-trip test**

Build a data object with known streams and log records. Run Calculator with all calculation steps. Write index object. Read it back. Verify:
- Existing streams/pointers sections unchanged
- New stats sections present with correct data
- New postings sections present with correct data

- [ ] **Step 2: Write cross-validation test**

Verify invariants from spec:
- Stats `row_count` equals actual log record counts per service_name
- Stats `uncompressed_size` equals sum of `len(log.Line)`
- Label posting bitmaps match streams with matching labels
- Bloom posting bytes match pointers section bloom filters

- [ ] **Step 3: Add `observeObject` cases for new section types**

Add stats and postings cases to the switch in `builder.go:378-410` for metrics.

- [ ] **Step 4: Run full test suite**

Run: `go test ./pkg/dataobj/... -v -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```
git commit -m "test(index): add end-to-end integration tests for stats and postings sections"
```

---

## Dependency Graph

```
Task 0 (types.Binary)
  |
  +---> Task 1 (Stats section)
  |       |
  +---> Task 2 (Postings section)
  |       |
  +-------+---> Task 3 (Pipeline integration)
                  |
Task 4 (Storage format) --- deferred, may come from Robert
                  |
                  +---> Task 5 (Integration tests)
```

Tasks 1 and 2 can be developed in parallel after Task 0.
Task 3 requires Tasks 0, 1, and 2.
Task 4 is independent of 1-3 but blocks Task 5.
Task 5 requires both Task 3 and Task 4.
