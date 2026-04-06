# Encoding Abstraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract an encoding abstraction so stats and postings section packages can switch between the new dataset API and the existing columnar encoder at runtime via a flag.

**Architecture:** Define `ColumnReader`, `Section`, and `SectionEncoder` types in each section package. The builder receives a `SectionEncoder` function at construction and calls it instead of the hardcoded `encodeRows`. The reader uses `Section.OpenColumn` to get `ColumnReader` instances instead of directly holding `array.Reader`s. The current dataset API encoding moves to `encode_dataset.go`. The `RowReader` is unchanged.

**Tech Stack:** Go, same packages as before. No new dependencies.

**Spec:** See design discussion in this conversation. Addendum to `docs/superpowers/specs/2026-04-03-new-index-sections-design.md`.

**Commit Strategy:** Single commit: `refactor: extract encoding abstraction for runtime-swappable section codecs`. Will be rebased into the appropriate position in the PR afterward.

---

## File Map

### Stats package (`pkg/dataobj/sections/stats/`)

| File | Action | Responsibility |
|---|---|---|
| `stats.go` | Modify | Add `ColumnReader` interface, `Section` struct (with `OpenColumn`), `SectionEncoder` type. Remove `NamedArray`. |
| `builder.go` | Modify | Accept `SectionEncoder` in constructor. Replace `FlushToArrays` → `Flush`. Remove `encodeRows` and `inMemoryStore`. |
| `encode_dataset.go` | Create | Move `encodeRows` + `inMemoryStore` here. Export as `DatasetEncoder`. Wrap results in new `Section` with `OpenColumn` closure. |
| `reader.go` | Modify | Replace `[]array.Reader` with `map[string]ColumnReader`. Open via `sec.OpenColumn`. Remove `array` and `memory` imports. |
| `row_reader.go` | No change | Only uses `columnar.Array`. |
| `builder_test.go` | Modify | Pass `DatasetEncoder` to `NewBuilder`. Rename `FlushToArrays` → `Flush`. |

### Postings package (`pkg/dataobj/sections/postings/`)

Identical pattern to stats:

| File | Action | Responsibility |
|---|---|---|
| `postings.go` | Modify | Add `ColumnReader`, `Section`, `SectionEncoder`. Remove `NamedArray`. |
| `builder.go` | Modify | Accept `SectionEncoder`. Replace `FlushToArrays` → `Flush`. Remove `encodeRows` + `inMemoryStore`. |
| `encode_dataset.go` | Create | Move `encodeRows` + `inMemoryStore` + `normalizeBitmaps` here. Export as `DatasetEncoder`. |
| `reader.go` | Modify | Use `ColumnReader` interface. Remove `array`/`memory` imports. |
| `row_reader.go` | No change | |
| `builder_test.go` | Modify | Pass `DatasetEncoder` to `NewBuilder`. Rename `FlushToArrays` → `Flush`. |

### Integration (`pkg/dataobj/index/`)

| File | Action | Responsibility |
|---|---|---|
| `indexobj/builder.go` | Modify | Pass encoder to `stats.NewBuilder` / `postings.NewBuilder`. |
| `stats_calculation_test.go` | Modify | Pass `DatasetEncoder` where `NewBuilder` is called. |
| `label_postings_calculation_test.go` | Modify | Pass `DatasetEncoder` where `NewBuilder` is called. |
| `column_values_test.go` | Modify | Pass `DatasetEncoder` where `NewBuilder` is called. |

---

## Task 1: Stats package abstraction

**Files:**

- Modify: `pkg/dataobj/sections/stats/stats.go`
- Modify: `pkg/dataobj/sections/stats/builder.go`
- Create: `pkg/dataobj/sections/stats/encode_dataset.go`
- Modify: `pkg/dataobj/sections/stats/reader.go`
- Modify: `pkg/dataobj/sections/stats/builder_test.go`

### Sub-task 1a: Define interface types in stats.go

- [ ] **Step 1: Add types to `stats.go`**

Add after the `Stat` struct:

```go
// ColumnReader reads batches of columnar values from a single column.
type ColumnReader interface {
    // Read reads up to count values. Returns columnar.Array and any error.
    // Returns io.EOF when no more data is available.
    Read(ctx context.Context, count int) (columnar.Array, error)
    Close() error
}

// Section holds encoded column data for one flushed stats section.
type Section struct {
    ColumnNames []string
    RowCount    int
    // OpenColumn returns a ColumnReader for the named column.
    // Returns an error if the column is not found.
    OpenColumn func(name string) (ColumnReader, error)
}

// SectionEncoder encodes a batch of sorted Stat rows into a Section.
type SectionEncoder func(ctx context.Context, rows []Stat) (Section, error)
```

Remove the old `NamedArray` type (it will move to `encode_dataset.go` as unexported).

Add import for `"github.com/grafana/loki/v3/pkg/columnar"`.

### Sub-task 1b: Extract encoding to encode_dataset.go

- [ ] **Step 2: Create `encode_dataset.go`**

Move the following from `builder.go` to this new file:
- The `encodeRows` function (renamed and exported as `DatasetEncoder`)
- The `inMemoryStore` struct and its methods
- All imports these need (`array`, `types`, `memory`, `columnar`)

The `DatasetEncoder` function signature must match `SectionEncoder`:
```go
func DatasetEncoder(ctx context.Context, rows []Stat) (Section, error)
```

Internally it does exactly what `encodeRows` does today, but wraps the result in the new `Section` type:
- `Section.ColumnNames` comes from the column name list
- `Section.RowCount` comes from `len(rows)`
- `Section.OpenColumn` is a closure that creates an `array.Reader` for the named column on demand

The closure needs access to the `[]NamedArray` (kept as a local unexported type in this file) and the `inMemoryStore`. It looks like:

```go
// namedArray pairs an array.Array descriptor with a column name (internal to this file).
type namedArray struct {
    Name  string
    Array array.Array
}

func DatasetEncoder(ctx context.Context, rows []Stat) (Section, error) {
    // ... existing encodeRows logic producing []namedArray and store ...

    columnNames := make([]string, len(namedArrays))
    for i, na := range namedArrays {
        columnNames[i] = na.Name
    }

    return Section{
        ColumnNames: columnNames,
        RowCount:    len(rows),
        OpenColumn: func(name string) (ColumnReader, error) {
            for _, na := range namedArrays {
                if na.Name == name {
                    var alloc memory.Allocator
                    r, err := array.NewReader(&alloc, na.Array, store)
                    if err != nil {
                        return nil, fmt.Errorf("creating reader for column %q: %w", name, err)
                    }
                    return &datasetColumnReader{reader: r, alloc: &alloc}, nil
                }
            }
            return nil, fmt.Errorf("column %q not found", name)
        },
    }, nil
}

// datasetColumnReader wraps an array.Reader as a ColumnReader.
type datasetColumnReader struct {
    reader array.Reader
    alloc  *memory.Allocator
}

func (r *datasetColumnReader) Read(ctx context.Context, count int) (columnar.Array, error) {
    return r.reader.Read(ctx, r.alloc, count)
}

func (r *datasetColumnReader) Close() error {
    return r.reader.Close()
}
```

### Sub-task 1c: Update builder.go

- [ ] **Step 3: Modify builder to accept encoder**

In `builder.go`:

1. Add `encode SectionEncoder` field to `Builder` struct.
2. Update `NewBuilder` to accept `encode SectionEncoder`:
   ```go
   func NewBuilder(targetSectionSize int, encode SectionEncoder) *Builder
   ```
3. Rename `FlushToArrays` to `Flush`. Replace `encodeRows(ctx, chunk)` with `b.encode(ctx, chunk)`.
4. Remove the `encodeRows` function (moved to `encode_dataset.go`).
5. Remove the `inMemoryStore` type (moved to `encode_dataset.go`).
6. Remove imports for `array`, `types`, `memory`, `columnar` (no longer needed in this file).

### Sub-task 1d: Update reader.go

- [ ] **Step 4: Refactor reader to use ColumnReader interface**

Replace the current `Reader` implementation:

```go
type Reader struct {
    sec     *Section
    readers map[string]ColumnReader
    rowCount int
}

func NewReader(sec *Section) (*Reader, error) {
    if sec == nil {
        return nil, fmt.Errorf("section must not be nil")
    }
    return &Reader{
        sec:      sec,
        readers:  make(map[string]ColumnReader),
        rowCount: sec.RowCount,
    }, nil
}
```

The `readInt64Column` and `readStringColumn` methods lazily open column readers:

```go
func (r *Reader) getOrOpenColumn(name string) (ColumnReader, error) {
    if cr, ok := r.readers[name]; ok {
        return cr, nil
    }
    cr, err := r.sec.OpenColumn(name)
    if err != nil {
        return nil, err
    }
    r.readers[name] = cr
    return cr, nil
}

func (r *Reader) readInt64Column(ctx context.Context, name string, count int) ([]int64, error) {
    cr, err := r.getOrOpenColumn(name)
    if err != nil {
        return nil, err
    }
    arr, err := cr.Read(ctx, count)
    if err != nil && err != io.EOF {
        return nil, fmt.Errorf("reading column %q: %w", name, err)
    }
    if arr == nil {
        return nil, io.EOF
    }
    return extractInt64Values(arr)
}
```

`Close()` iterates `r.readers` and closes each.

Remove imports for `array` and `memory`.

### Sub-task 1e: Update tests

- [ ] **Step 5: Update builder_test.go**

1. Every `stats.NewBuilder(size)` becomes `stats.NewBuilder(size, stats.DatasetEncoder)`.
2. Every `b.FlushToArrays(ctx)` becomes `b.Flush(ctx)`.

- [ ] **Step 6: Run stats tests**

Run: `go test ./pkg/dataobj/sections/stats/... -v -count=1`

Expected: PASS

---

## Task 2: Postings package abstraction

Identical pattern to Task 1. Same types (`ColumnReader`, `Section`, `SectionEncoder`), same refactor.

**Files:**

- Modify: `pkg/dataobj/sections/postings/postings.go`
- Modify: `pkg/dataobj/sections/postings/builder.go`
- Create: `pkg/dataobj/sections/postings/encode_dataset.go`
- Modify: `pkg/dataobj/sections/postings/reader.go`
- Modify: `pkg/dataobj/sections/postings/builder_test.go`

- [ ] **Step 1: Add `ColumnReader`, `Section`, `SectionEncoder` to `postings.go`**

Same as stats, but `SectionEncoder` takes `[]Posting`:
```go
type SectionEncoder func(ctx context.Context, rows []Posting) (Section, error)
```

- [ ] **Step 2: Create `encode_dataset.go`**

Move `encodeRows`, `normalizeBitmaps`, `inMemoryStore` from `builder.go`. Export as `DatasetEncoder`. Wraps result in `Section` with `OpenColumn` closure, same pattern as stats.

Note: the postings reader has 4 read methods (`readInt64Column`, `readStringColumn`, `readNullableStringColumn`, `readBytesColumn`). The `ColumnReader` interface is the same — it returns `columnar.Array`. The nullable/bytes extraction happens in the row_reader via `extractNullableStringValues` and `extractBytesValues`, which operate on `columnar.Array` (specifically `*columnar.UTF8`). No interface change needed.

- [ ] **Step 3: Update `builder.go`** — accept encoder, rename `FlushToArrays` → `Flush`

- [ ] **Step 4: Update `reader.go`** — use `ColumnReader` interface, lazy open

- [ ] **Step 5: Update `builder_test.go`** — pass `DatasetEncoder`, rename method

- [ ] **Step 6: Run postings tests**

Run: `go test ./pkg/dataobj/sections/postings/... -v -count=1`

Expected: PASS

---

## Task 3: Integration updates

**Files:**

- Modify: `pkg/dataobj/index/indexobj/builder.go:127,136`
- Modify: `pkg/dataobj/index/stats_calculation_test.go`
- Modify: `pkg/dataobj/index/label_postings_calculation_test.go`
- Modify: `pkg/dataobj/index/column_values_test.go`

- [ ] **Step 1: Update `indexobj/builder.go`**

Change the two `NewBuilder` calls to pass the dataset encoder:

```go
// In getStatsBuilderForTenant:
sb := stats.NewBuilder(int(b.cfg.TargetSectionSize), stats.DatasetEncoder)

// In getPostingsBuilderForTenant:
pb := postings.NewBuilder(int(b.cfg.TargetSectionSize), postings.DatasetEncoder)
```

- [ ] **Step 2: Update test files**

In `stats_calculation_test.go`, `label_postings_calculation_test.go`, and `column_values_test.go`, find the `newTestIndexBuilder` helper or wherever `stats.NewBuilder` / `postings.NewBuilder` is called, and pass the encoder.

Also update any `FlushToArrays` calls to `Flush`.

- [ ] **Step 3: Run all tests**

Run: `go test ./pkg/dataobj/sections/stats/... ./pkg/dataobj/sections/postings/... ./pkg/dataobj/index/... -count=1`

Expected: all PASS

- [ ] **Step 4: Commit**

```
git -c commit.gpgsign=false add -A
git -c commit.gpgsign=false commit -m "refactor: extract encoding abstraction for runtime-swappable section codecs"
```
