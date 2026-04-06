# New Index Sections (Stats & Postings) Design Spec

**Date:** 2026-04-03 **Issue:** grafana/loki-private#2394 **Author:** Trevor Whitney

## Problem

Thor's scheduler saturates under load because metastore queries open too many index objects. The existing index object layout uses `streams` and `pointers` sections with compound keys (one row per stream with all labels together). This layout doesn't support the sort schema needed for storage locality: `[__index_type__, __label__, __timestamp__]`.

Storage locality for indexes requires:

- **Data locality:** minimizing the number of sections where postings for queried predicates are stored.
- **Time locality:** minimizing the number of sections where timestamps for queried predicates are stored.

The compound-key layout in the existing `streams` section makes it impossible to sort by individual label names, which is the key requirement for the new sort schema.

## Solution

Add two new sections to index objects: **stats** and **postings**. These co-exist alongside the existing `streams` and `pointers` sections (which remain unchanged). The new sections use a flattened, per-label-key layout that supports the sort schema from Robert Fratto's proposal.

Both new sections are written using the new dataset API from `rfratto/loki:new-dataset` (`pkg/dataset/array`). This branch is based on that branch, so the necessary code should be available.

### Scope

- Implement write and read for both new sections.
- Wire into the index builder pipeline (dual-write alongside existing sections).
- Do NOT wire into the query path yet.
- Hardcoded sort schema: `[service_name]` (single key for initial implementation).
- No feature flag plumbing yet (always write new sections when code is present).
- Run-ID: FNV-64a hash of the data object path, stored as int64.
- Sections support size-based splitting: rows are sorted globally, then streamed into sections up to the target section size. A new section starts when the size threshold is exceeded, regardless of sort key boundaries.

### Non-Goals

- Query path integration (metastore reads).
- TOC updates for min/max column name pruning.
- Compaction support.
- Feature flag / config for enabling/disabling new sections.
- Dictionary encoding for repeated values.
- Migration from existing sections to new sections.

## Data Model

### Stats Section

**Section type:** `{Namespace: "github.com/grafana/loki", Kind: "stats"}`

Purpose: High-level information about individual sort schema keys in data object sections. Primarily intended for the compactor (not query planning).

One row per unique (object_path, section_index, label_value) combination within the sort schema.

**Columns:**

| Column              | Type  | Nullable | Encoding | Description                                                                   |
| ------------------- | ----- | -------- | -------- | ----------------------------------------------------------------------------- |
| `object_path`       | UTF8  | No       | Binary   | Path to the logs data object                                                  |
| `section_index`     | Int64 | No       | Plain    | Section index within the data object                                          |
| `run_id`            | Int64 | No       | Plain    | FNV-64a hash of `object_path`                                                 |
| `sort_schema`       | UTF8  | No       | Binary   | Comma-delimited sort key column names (e.g., "service_name")                  |
| `<label_name>`      | UTF8  | No       | Binary   | Dynamic column, one per sort schema key. Label value for this row's sort key. |
| `min_timestamp`     | Int64 | No       | Plain    | Min timestamp (UnixNano) across records with this key                         |
| `max_timestamp`     | Int64 | No       | Plain    | Max timestamp (UnixNano) across records with this key                         |
| `row_count`         | Int64 | No       | Plain    | Number of log records with this key                                           |
| `uncompressed_size` | Int64 | No       | Plain    | Total uncompressed bytes (`len(log.Line)`) for records with this key          |

With the hardcoded sort schema `[service_name]`, there is exactly one dynamic label column: `service_name`. In this initial implementation, the sort schema is the same per tenant and every row has a value for every column, so no nulls are needed. However, the structure must accommodate a future where the sort schema is configurable per tenant -- a tenant with sort schema `[service_name, namespace]` would have two dynamic columns, and the reader must discover them via the `sort_schema` column (see below). If different tenants in the same index object have different sort schemas, their sections will have different column sets, which is acceptable since sections are per-tenant.

**Missing labels:** If a stream does not have the `service_name` label (or any other sort schema label), the label value is normalized to empty string `""`. The stream's records are still indexed and contribute to stats/postings rows with `service_name=""`.

**Dynamic column parsing:** A reader determines which columns are sort-key label columns by parsing the `sort_schema` column. The `sort_schema` value is a comma-delimited list of column names (e.g., `"service_name"` or `"service_name,namespace"`). Each name in the list corresponds to a dynamic label column in the section.

**Invariant:** All stats sections within a single index object for a given tenant have the same `sort_schema` value. This is guaranteed by the hardcoded sort schema in the initial implementation.

**Sort order:** `[<label columns in schema order>, min_timestamp]`

**Example:** Given a data object `obj-a` with section 0 containing streams with `service_name` values of "frontend" and "backend":

```
object_path | section_index | run_id | sort_schema  | service_name | min_ts | max_ts | rows | size
obj-a       | 0             | 0x1A2B | service_name | backend      | 1100   | 2100   | 30   | 2048
obj-a       | 0             | 0x1A2B | service_name | frontend     | 1000   | 2000   | 50   | 4096
```

### Postings Section

**Section type:** `{Namespace: "github.com/grafana/loki", Kind: "postings"}`

Purpose: Equivalent to the existing `pointers` section but "flattened" to permit following the index object sort schema. Each label value gets its own row, enabling the section to be sorted and split by column name.

One row per (kind, object_path, section_index, column_name, label_value) tuple.

**Columns:**

| Column              | Type   | Nullable | Encoding | Description                                                               |
| ------------------- | ------ | -------- | -------- | ------------------------------------------------------------------------- |
| `kind`              | Int64  | No       | Plain    | 0=Bloom, 1=Label                                                          |
| `object_path`       | UTF8   | No       | Binary   | Path to the logs data object                                              |
| `section_index`     | Int64  | No       | Plain    | Section index within the data object                                      |
| `column_name`       | UTF8   | No       | Binary   | Indexed column/label name                                                 |
| `label_value`       | UTF8   | Yes      | Binary   | Label value (only set for Kind=Label)                                     |
| `bloom_filter`      | Binary | Yes      | Binary   | Serialized bloom filter bytes (only set for Kind=Bloom)                   |
| `stream_id_bitmap`  | Binary | No       | Binary   | Raw bit-packed bitmap of matching stream IDs (see Stream ID Bitmap)       |
| `uncompressed_size` | Int64  | No       | Plain    | Sum of `len(log.Line)` for all records belonging to streams in the bitmap |
| `min_timestamp`     | Int64  | No       | Plain    | Min timestamp (UnixNano) across records in matching streams               |
| `max_timestamp`     | Int64  | No       | Plain    | Max timestamp (UnixNano) across records in matching streams               |

**Sort order:** `[kind, column_name, label_value, min_timestamp]`

Note: `bloom_filter` and `label_value` are the only nullable columns. They are mutually exclusive based on `kind`. `stream_id_bitmap` is always present for both Bloom and Label postings.

**Label postings scope:** Label postings are generated only for sort-schema labels (currently just `service_name`), not for all stream labels. Bloom postings are generated for structured metadata columns (same as existing `columnValuesCalculation`).

**`uncompressed_size` semantics:** For both Label and Bloom postings, `uncompressed_size` is the sum of `len(log.Line)` across all log records whose stream ID appears in the `stream_id_bitmap`. Note: when a stream has multiple sort-schema labels and appears in multiple label postings, its log record sizes are counted toward each posting that includes the stream. With the initial `[service_name]` schema (single key), no double-counting occurs since each stream has exactly one `service_name` value.

**Example:**

```
kind  | object_path | section_index | column_name    | label_value | bloom   | bitmap | size | min_ts | max_ts
Bloom | obj-a       | 0             | detected_level |             | <bloom> | 0b111  | 8192 | 1000   | 2000
Label | obj-a       | 0             | service_name   | backend     |         | 0b001  | 2048 | 1100   | 2100
Label | obj-a       | 0             | service_name   | frontend    |         | 0b110  | 4096 | 1000   | 1800
```

### Stream ID Bitmap

The `stream_id_bitmap` column stores a raw bit-packed bitmap (binary, not
UTF8) where bit N indicates that stream ID N is associated with this
posting.

**Stream ID scope:** Stream IDs are scoped to the **data object** (per
tenant), not to individual logs sections within it. A data object has one
`streams.Builder` per tenant (see
`consumer/logsobj/builder.go:183,228,284`), which assigns IDs
sequentially starting from 1 via an atomic counter. All logs sections
within the same data object share this ID space -- `logs.Record.StreamID`
values come from the single streams builder. A given logs section may
contain records for only a **subset** of the data object's streams.

This means: for a postings row scoped to `(object_path, section_index)`,
the bitmap represents which of the data object's streams have records in
that specific logs section AND match the posted label/bloom. The bitmap
is sized to the max stream ID across the entire data object, not just the
streams that appear in the referenced logs section. Bits for streams that
don't appear in this logs section will be unset.

**Encoding details:**

- The bitmap uses **source stream IDs** from the data object (i.e.,
  `log.StreamID` from `logs.Record`), NOT the remapped index stream IDs
  from `streamIDLookup`. This ensures the bitmap can be used to correlate
  postings with data in the source data object directly.
- Stream IDs are **0-indexed** in the bitmap. Bit 0 corresponds to
  stream ID 0.
- Bits are packed using **LSB (least-significant bit) numbering**, the
  same convention used by [Arrow validity
  bitmaps](https://arrow.apache.org/docs/format/Columnar.html#validity-bitmaps).
  Within each byte, we read right-to-left: bit N is set if
  `bitmap[N/8] & (1 << (N%8)) != 0`.
- The bitmap is sized to `ceil(maxStreamID+1 / 8)` bytes, where
  `maxStreamID` is the highest stream ID in the data object (determined
  by the streams section, which is the sole ID authority).
- Stream IDs are dense (contiguous) integers starting from 1, assigned
  by `streams.Builder.Record()` via an atomic counter
  (`builder.go:221: b.lastID.Add(1)`). Bit 0 is therefore always
  unused/zero. The maximum stream ID equals the number of unique
  streams for that tenant in the data object.

For Kind=Label postings, the bitmap indicates which streams in the
referenced logs section have this specific label value. For Kind=Bloom
postings, the bitmap indicates which streams in the referenced logs
section have the named metadata column at all.

## Architecture

### Package Structure

```
pkg/dataobj/sections/stats/
    stats.go          -- SectionType, ColumnType enum, Open/CheckSection helpers
    builder.go        -- Builder: accumulates stats rows, sorts, flushes via dataset API
    reader.go         -- Reader: columnar reads via dataset API
    row_reader.go     -- RowReader: returns typed Stat structs

pkg/dataobj/sections/postings/
    postings.go       -- SectionType, ColumnType enum, Open/CheckSection helpers
    builder.go        -- Builder: accumulates posting rows, sorts, flushes via dataset API
    reader.go         -- Reader: columnar reads via dataset API
    row_reader.go     -- RowReader: returns typed Posting structs
```

Both packages follow the same patterns as existing section packages (`sections/streams/`, `sections/pointers/`), but use the new dataset API (`pkg/dataset/array`) for encoding/decoding instead of `sections/internal/columnar`.

Readers are required even without query path integration -- they are the validation mechanism for integration tests (round-trip verification).

### Dataset API

The new sections use the `pkg/dataset/array` package (already available in
this branch, from Robert's new-dataset work). This package provides:

- `array.Writer` / `array.Reader` -- columnar array encoding/decoding
- `array.Sink` / `array.Source` -- storage abstraction for buffer data
- `array.Spec` / `array.Encoding` -- write-time and read-time encoding specs
- `types.Type` -- logical type system (Int64, UTF8, Bool, etc.)

The codecs produce buffer data that closely aligns with Arrow's in-memory
layout (e.g., plain int64 values are stored as contiguous 8-byte values,
UTF8 strings use separate offsets and data buffers). This alignment is the
core motivation for using the new API -- reads should require minimal
transformation from on-disk to in-memory representation.

### Section Storage Format (Sub-Spec)

The `pkg/dataset/array` package currently has **no serialization format**
for `Array` metadata. The codecs can encode/decode column data between
in-memory `columnar.Array` values and raw `BufferData` byte slices, but
there is no way to persist the `Array` descriptor tree (encoding, type,
stats, children, buffer byte ranges) to bytes and reconstruct it.

This is a critical gap: without a serialization format, we cannot write
sections to disk and read them back (round-trip), which blocks all
testing.

**A separate sub-spec will define the on-disk storage format for sections
that use the new dataset API.** This sub-spec covers:

1. **Array metadata serialization** -- A protobuf (or FlatBuffer) schema
   for persisting the `Array` descriptor tree. Each node in the tree
   contains: encoding kind, encoding-specific metadata, logical type,
   statistics (row count, null count), buffer references (byte offsets
   and lengths into the section's data region), and child array nodes.
   This follows the same pattern as [Vortex's `ArrayNode` FlatBuffer
   schema](https://github.com/vortex-data/vortex), which uses a
   recursive node tree with flat buffer descriptors.

2. **Section-level metadata** -- How the per-column `Array` descriptors
   and column names are organized within the section's metadata region.
   Must be compatible with the existing dataobj file container (the
   section gets a metadata region and a data region, with an
   `ExtensionData` blob at the file level for bootstrapping).

3. **Buffer data layout** -- How the raw encoded buffers from
   `array.Writer.Flush()` are laid out in the section's data region.
   The buffers are written as-is (preserving their Arrow-aligned format)
   and referenced by byte offset + length from the metadata.

4. **Reading path** -- How a reader reconstructs `Array` descriptors
   from the persisted metadata and creates `array.Source` + `array.Reader`
   instances, with minimal data transformation (ideally zero-copy or
   near-zero-copy for the buffer data).

The sub-spec will draw heavily from Vortex's prior art:
- `vortex-flatbuffers/flatbuffers/vortex-array/array.fbs` -- Array/ArrayNode tree serialization
- `vortex-flatbuffers/flatbuffers/vortex-file/footer.fbs` -- Segment map (buffer descriptors)
- `vortex-flatbuffers/flatbuffers/vortex-layout/layout.fbs` -- Layout tree

**Implementation ordering:** The serialization format sub-spec and its
implementation are saved for last in the implementation plan, as Robert
may have work in flight for this part of the dataset package. If Robert
provides a serialization solution, we adopt it. If not, we implement our
own based on the sub-spec.

**Interim testing:** Until the serialization format is implemented,
unit tests for the section builders can use in-memory round-trips (the
`array.Writer` -> `Array` descriptor -> `array.Reader` path works in
memory via a test `Sink`/`Source`). Integration tests that persist to
the full dataobj file format will require the serialization format.

### Integration with Index Builder Pipeline

#### Stream Label Access

The new calculation steps (`statsCalculation`, `labelPostingsCalculation`) need access to stream labels, which are not currently available in the `logsCalculationContext`. The `streamIDLookup` field is `map[int64]int64` (source stream ID -> index stream ID) and carries no labels.

**Solution:** Add a `streamLabels map[int64]labels.Labels` field to the `logsCalculationContext` struct. This map is keyed by source stream ID (within the data object) and populated during `processStreamsSection` (Phase 1), where stream labels are already being read from the data object's streams section. The labels are cached in memory for the duration of the Calculator's processing of that data object.

```go
type logsCalculationContext struct {
    tenantID       string
    objectPath     string
    sectionIdx     int64
    streamIDLookup map[int64]int64     // source stream ID -> index stream ID
    streamLabels   map[int64]labels.Labels  // NEW: source stream ID -> labels
    builder        *indexobj.Builder
}
```

**Propagation mechanism:** Following the existing pattern for `streamIDLookup`, a new `streamLabelsByTenant sync.Map` field is added to the `Calculator` struct. During `processStreamsSection` (Phase 1), stream labels are collected into a `map[int64]labels.Labels` alongside the existing `streamIDLookup`, then stored in `streamLabelsByTenant` keyed by tenant ID. In `processLogsSection` (Phase 2), the map is loaded from `streamLabelsByTenant` and passed to `logsCalculationContext` the same way `streamIDLookup` is loaded today. The `processLogsSection` function signature gains an additional `streamLabels map[int64]labels.Labels` parameter.

```go
// In processStreamsSection:
streamLabels := make(map[int64]labels.Labels, len(streams))
for _, stream := range streams {
    streamLabels[stream.ID] = stream.Labels
}
c.streamLabelsByTenant.Store(tenantID, streamLabels)
```

**Error handling:** If `context.streamLabels[log.StreamID]` returns no entry (indicating a corrupt or incomplete data object), the calculation should return an error. This is a hard failure -- partial index data from corrupt sources would be worse than no index data.

**Memory lifecycle:** The `streamLabels` maps are cleared when `Calculator.Reset()` is called after flushing the index object, same as the existing `streamIDLookupByTenant` maps.

#### New Fields on `indexobj.Builder`

```go
type Builder struct {
    // ... existing fields ...
    stats    map[string]*stats.Builder     // key=TenantID
    postings map[string]*postings.Builder  // key=TenantID
}
```

Builders are lazily created when the first `Append*` call is made for a tenant, following the same pattern as existing builders. They receive `TargetSectionSize` from the shared `BuilderBaseConfig`, the same source used by the existing `pointers.Builder`.

#### New Methods on `indexobj.Builder`

All new `Append*` methods follow the existing concurrency contract: they are **not goroutine-safe**. The `Calculator` serializes all builder access under `c.builderMtx` (existing pattern in `calculate.go`).

```go
// AppendStat records a per-sort-key aggregate for a data object section.
// Called once per unique label value in the sort schema, per section.
func (b *Builder) AppendStat(tenantID, objectPath string, sectionIdx int64,
    label labels.Label, minTs, maxTs time.Time, rows int, uncompressedSize int64) error

// AppendLabelPosting records a label value posting.
// Called once per unique (column_name, label_value) per section.
func (b *Builder) AppendLabelPosting(tenantID, objectPath string, sectionIdx int64,
    columnName, labelValue string, streamIDBitmap []byte,
    uncompressedSize int64, minTs, maxTs time.Time) error

// AppendBloomPosting records a bloom filter posting.
// Called once per metadata column per section.
func (b *Builder) AppendBloomPosting(tenantID, objectPath string, sectionIdx int64,
    columnName string, bloomFilter, streamIDBitmap []byte,
    uncompressedSize int64, minTs, maxTs time.Time) error
```

#### Flush Order

The `indexobj.Builder.Flush()` writes sections in this order:

1. Streams sections (existing, per tenant)
2. Pointers sections (existing, per tenant)
3. Stats sections (new, per tenant, may produce multiple sections per tenant)
4. Postings sections (new, per tenant, may produce multiple sections per tenant)
5. IndexPointers sections (existing, per tenant)

Note: The existing `Flush()` iterates Go maps (unordered by tenant within each section type). The new code maintains the section-type ordering (all streams before all pointers before all stats, etc.) but tenant ordering within a type is unspecified, matching existing behavior.

The existing `observeObject()` method (which iterates flushed sections for metrics) will need to be updated to recognize the new section types.

#### New Calculation Steps

Two new implementations of the `logsIndexCalculation` interface, registered in `getLogsCalculationSteps()`:

**`statsCalculation`:**

State:

```go
type statsCalculation struct {
    // Per (label_value) aggregates, keyed by label value string
    aggregates map[string]*statsAggregate
}

type statsAggregate struct {
    labelValue       string
    minTimestamp      time.Time
    maxTimestamp      time.Time
    rowCount         int
    uncompressedSize int64
}
```

- Prepare: Initialize aggregates map. No data fetching needed -- stream labels are accessed via `context.streamLabels` during ProcessBatch.
- ProcessBatch: For each log record:
  1. Look up the stream's labels via `context.streamLabels[log.StreamID]`.
  2. Extract the `service_name` label value (empty string if missing).
  3. Find or create the aggregate for this label value.
  4. Update min/max timestamp, increment row count, add `len(log.Line)` to uncompressed size.
- Flush: Compute run-ID as `hash/fnv.New64a()` over `context.objectPath`. Sort aggregates by label value. Call `builder.AppendStat()` for each.

**`labelPostingsCalculation`:**

State:

```go
type labelPostingsCalculation struct {
    // Per (column_name, label_value) posting data
    postings map[postingKey]*labelPosting
}

type postingKey struct {
    columnName string
    labelValue string
}

type labelPosting struct {
    streamIDBitmap   []byte  // bit-packed bitmap
    minTimestamp      time.Time
    maxTimestamp      time.Time
    uncompressedSize int64
}
```

- Prepare: Initialize postings map.
- ProcessBatch: For each log record:
  1. Look up the stream's labels via `context.streamLabels[log.StreamID]`.
  2. For each sort-schema key (currently just `service_name`): read the label value from the stream's labels. If the label is absent, use empty string `""`. Find or create the posting for `(columnName, labelValue)`.
  3. Set bit `log.StreamID` in the bitmap (0-indexed, growing as needed).
  4. Update min/max timestamp, add `len(log.Line)` to uncompressed size.
- Flush: Sort postings by `[column_name, label_value]`. Call `builder.AppendLabelPosting()` for each.

**Extension to `columnValuesCalculation`:**

The existing `columnValuesCalculation` is extended with additional per-column tracking for bloom postings:

```go
type columnValuesCalculation struct {
    columnBloomBuilders map[string]*bloom.BloomFilter  // existing
    columnIndexes       map[string]int64                // existing

    // NEW: per-column tracking for AppendBloomPosting
    columnStreamBitmaps  map[string][]byte      // column name -> stream ID bitmap
    columnMinTimestamps  map[string]time.Time   // column name -> min timestamp
    columnMaxTimestamps  map[string]time.Time   // column name -> max timestamp
    columnSizes          map[string]int64        // column name -> sum of len(log.Line)
}
```

- Prepare: Initialize new maps alongside existing ones.
- ProcessBatch: For each log record, for each metadata label on that record:
  - Add bloom value (existing): `columnBloomBuilders[label.Name].Add(label.Value)`
  - Set bitmap bit (new): `columnStreamBitmaps[label.Name]` bit `log.StreamID`
  - Update timestamps (new): expand `columnMinTimestamps[label.Name]` / `columnMaxTimestamps[label.Name]` with `log.Timestamp`
  - Accumulate size (new): `columnSizes[label.Name] += len(log.Line)` Note: a single log record with multiple metadata labels contributes its `len(log.Line)` to each column independently. This means `uncompressed_size` for bloom postings is not deduplicated across columns -- a stream appearing in multiple column bitmaps will have its log record sizes counted in each. Bloom postings are generated exclusively for structured metadata columns (`ColumnTypeMetadata`), not for stream labels.
- Flush (extended): After the existing `AppendColumnIndex` call, also call `builder.AppendBloomPosting()` with the bloom filter bytes, stream ID bitmap, accumulated timestamps, and size for each column.

### Bridging New Dataset API to SectionWriter

The new section builders use the dataset API (`array.Writer`, `Sink`) for encoding. To integrate with `dataobj.Builder` (which expects `SectionBuilder.Flush(SectionWriter)`), each builder:

1. Accumulates rows in memory as typed slices (one slice per column).
2. On flush, sorts the rows by the section's sort order.
3. Creates `array.Writer` instances for each column with appropriate specs:
   - Int64 columns: `&array.SpecPlain{}` with `types.Int64{}`
   - UTF8 columns: `&array.SpecBinary{Offsets: &array.SpecPlain{}}` with `types.UTF8{}`
   - Nullable UTF8: same but with `types.UTF8{Nullable: true}`
   - Binary columns (bloom_filter, stream_id_bitmap):
     `&array.SpecBinary{Offsets: &array.SpecPlain{}}` with
     `types.Binary{}`. A new `types.Binary` type will be added to the
     dataset package as part of this work (~25 lines: new `KindBinary`
     in the Kind enum, new `Binary` struct mirroring `UTF8`, relaxed
     type checks in `codec_binary.go` to accept both `KindUTF8` and
     `KindBinary`). The physical encoding is identical to UTF8 (offsets
     + data buffer); the distinction is purely logical, allowing readers
     to distinguish UTF-8 strings from opaque byte sequences.
4. Appends column data to the writers using in-memory `columnar.Array` values.
5. Flushes writers to an in-memory `Sink`, producing `Array` descriptors and raw buffer data.
6. Writes the buffer data as section content to the `SectionWriter`.
7. Stores the `Array` metadata as section metadata (see "Dataset API Dependency" for serialization details).

### Section Splitting & Memory

Both stats and postings builders support size-based splitting. They use an
**accumulate-sort-then-split** pattern, which differs from the existing
`pointers.Builder`'s incremental-flush pattern. The difference is necessary
because global sort order must be maintained across sections, and sorting
requires all rows to be accumulated first.

**Memory impact:** The accumulate-sort-then-split pattern holds all rows in
memory until flush. The additional memory relative to the existing pipeline
is modest:

- Stats rows: ~200B per unique (object_path, section, service_name). With
  typical workloads (~10 data objects, ~3 sections each, ~10 service
  names): ~600 rows = ~120KB.
- Label postings: ~200B + bitmap per (label_name, label_value, object_path,
  section). Bitmaps are `ceil(maxStreamID/8)` bytes each. With ~1000
  streams and ~100 unique label values across ~30 sections: ~3000 rows =
  ~1MB.
- Bloom postings: the bloom filter bytes are the same data already held by
  the existing pointers builder (duplicate reference, not new data). The
  new cost is only the per-posting bitmap.

Total new memory is on the order of a few MB, which is small relative to
the existing pipeline's memory (downloaded data objects, streams builders,
bloom filters in the pointers builder). Peak memory remains bounded by the
Calculator's batch size -- the number of data objects processed before the
index is flushed.

1. All `Append*` calls accumulate rows in memory. No flushing occurs during append.
2. On `Flush()`, all accumulated rows are sorted globally by the section's sort order.
3. Rows are streamed into sections sequentially. The split metric is the sum of per-row estimated uncompressed sizes. The per-row size is calculated as the sum of all column data sizes for that row:
   - Int64 columns: 8 bytes each
   - UTF8/Binary columns: `len(value)` bytes each (includes `object_path`, `sort_schema`, label values, `bloom_filter`, `stream_id_bitmap`, etc.)
   - Nullable columns with null values: 0 bytes Encoding overhead (offsets, validity bitmaps, compression) is NOT included in the size estimate.
4. When adding the next row would make the running total **strictly greater than** `TargetSectionSize` (from `BuilderBaseConfig`), the current section is flushed to the `SectionWriter` and a new section begins with the remaining rows.
5. The split point is wherever the size limit is exceeded -- it is NOT aligned to sort key boundaries. A single label value's postings may span two sections.
6. The last row of section N is sorted before the first row of section N+1 (global sort order is preserved across sections).
7. If a single row exceeds `TargetSectionSize`, the section is written anyway (oversized). This matches the existing behavior for edge cases.

## Data Flow

```
Data Object (downloaded from storage)
  |
  +--- Phase 1: Streams sections ---
  |    Read streams.Stream { ID, Labels, MinTimestamp, MaxTimestamp, Rows, UncompressedSize }
  |    |
  |    +-> indexobj.Builder.AppendStream() [existing]
  |    +-> Build streamIDLookup[sourceID] -> indexID
  |    +-> Build streamLabels[sourceID] -> labels.Labels [NEW]
  |
  +--- Phase 2: Logs sections (per section, in parallel) ---
       |
       +-- streamStatisticsCalculation [existing, unchanged]
       |     -> builder.ObserveLogLine()
       |
       +-- columnValuesCalculation [existing, extended with bloom posting state]
       |     -> builder.AppendColumnIndex() [existing]
       |     -> builder.AppendBloomPosting() [new]
       |
       +-- statsCalculation [new]
       |     For each log record, accumulate per-service_name aggregates
       |     using context.streamLabels to resolve stream ID -> label value
       |     -> builder.AppendStat()
       |
       +-- labelPostingsCalculation [new]
             For each log record, build per-(service_name, value) bitmaps
             using context.streamLabels to resolve stream ID -> label value
             -> builder.AppendLabelPosting()
```

Note: Phase 2 calculations run in parallel per logs section, but all `builder.Append*` calls are serialized under `Calculator.builderMtx` (existing pattern).

## Run ID

The run-ID is computed as `FNV-64a(objectPath)` where `objectPath` is the storage path of the data object being indexed. This is deterministic: the same data object always produces the same run-ID.

**Conversion:** The FNV-64a hash produces a `uint64`. It is stored as `Int64` using Go's standard two's-complement reinterpretation: `int64(fnvHash)`. Values above `math.MaxInt64` will appear as negative integers. This is intentional -- the run-ID is an opaque identifier, not an ordered value.

The run-ID is stored as an Int64 column in the stats section only. It is not included in the postings section because the compactor's run-tracking needs are met by the stats section alone.

## Testing

### In-Memory Unit Tests (PRs 1-3, no serialization needed)

For each new section package (stats, postings), using in-memory
`Sink`/`Source` for round-trips:

1. **In-memory round-trip test:** Build columns with synthetic data via
   `array.Writer`, flush to in-memory `Sink`, read back via
   `array.Reader` from in-memory `Source`, verify all rows match.
2. **Sort order test:** Verify rows are returned in the expected sort
   order.
3. **Section splitting test:** Configure a small TargetSectionSize, write
   enough data to trigger multiple sections, verify rows are correctly
   distributed in sort order across sections.
4. **Edge cases:** Empty builder (no rows), single-row section, many rows
   with the same label value, very large values, streams with missing
   `service_name` label (should produce rows with `service_name=""`).
5. **Nullable column handling (postings only):** Verify bloom_filter is
   null for Label postings, label_value is null for Bloom postings.
   Verify that invalid combinations (kind=Label with bloom set) produce
   errors.
6. **Bitmap correctness:** Verify stream ID bitmaps are correctly built
   -- correct bits set for matching streams, 0-indexed, LSB numbering.

### On-Disk Integration Tests (PR 5, requires serialization format)

These tests require the Section Storage Format (PR 4) to be implemented:

1. **Full pipeline round-trip:** Create a data object with known streams
   and log records. Run the Calculator with the existing + new
   calculations. Write the index object to bytes. Read it back. Verify:
   - Existing streams and pointers sections (unchanged behavior).
   - New stats and postings sections with correct data.
2. **Cross-validation with concrete invariants:**
   - For each `(object_path, section_index, service_name)` in the stats
     section, the `row_count` must equal the count of log records in
     that data object section whose stream has that `service_name` value.
   - For each `(object_path, section_index, service_name)` in the stats
     section, the `uncompressed_size` must equal the sum of
     `len(log.Line)` for those records.
   - The set of `service_name` values in the stats section must equal the
     set of `service_name` values across all streams in the corresponding
     streams section (treating missing labels as `""`).
   - For each Label posting, the stream IDs in the bitmap must match
     exactly the stream IDs from the streams section that have the posted
     label value.
   - For each Bloom posting, the bloom filter bytes must match the
     corresponding `ValuesBloomFilter` from the pointers section for the
     same `(object_path, section_index, column_name)`.

## Testing

### Unit Test Phasing

Unit tests for the section builders can use **in-memory round-trips**
before the on-disk serialization format exists. The `array.Writer` ->
`Array` descriptor -> `array.Reader` path works via an in-memory
`Sink`/`Source` (the test infrastructure already in the dataset package).
This validates the data model, sort ordering, section splitting logic,
and column correctness without needing on-disk persistence.

### Integration Tests

Integration tests that verify the full pipeline (Calculator -> index
object -> read back) require the on-disk serialization format. These
tests are blocked on the Section Storage Format sub-spec implementation.

## PR Decomposition

This work is expected to span multiple PRs:

0. **PR 0: Add `types.Binary` to the dataset package** -- ~25 lines:
   new `KindBinary` in `types/kind.go`, new `Binary` struct in
   `types/types.go`, relaxed type checks in `array/codec_binary.go` to
   accept both `KindUTF8` and `KindBinary`. Unit tests for Binary
   round-trip via the binary codec.
1. **PR 1: Stats section package** -- `sections/stats/` with builder,
   reader, row_reader, and in-memory unit tests (using test Sink/Source).
2. **PR 2: Postings section package** -- `sections/postings/` with
   builder, reader, row_reader, and in-memory unit tests. Depends on
   PR 0 for `types.Binary`.
3. **PR 3: Pipeline integration** -- New calculation steps,
   `streamLabels` field on `logsCalculationContext`, `indexobj.Builder`
   changes (new fields, methods, flush loop, `observeObject` update).
   Unit tests for the calculation steps using mocked builders.
4. **PR 4: Section Storage Format** -- Sub-spec + implementation of the
   on-disk serialization format for new dataset API sections. Defines
   the protobuf/FlatBuffer schema for `Array` metadata, implements
   `Sink`/`Source` backed by the section's data region, and bridges to
   the existing dataobj file container. Draws from Vortex prior art.
   **Saved for last** -- Robert may have work in flight for this.
5. **PR 5: On-disk integration tests** -- End-to-end tests: build data
   object -> run Calculator -> write index object to bytes -> read back
   -> verify stats and postings sections. Cross-validation with existing
   streams/pointers sections.

PRs 1 and 2 can be developed in parallel. PR 3 depends on both.
PR 4 is independent of PRs 1-3 (it's a serialization layer). PR 5
depends on PRs 3 and 4.

## Open Questions

- **Sort schema configuration:** Currently hardcoded to `[service_name]`.
  Future work will make this configurable per tenant. The design supports
  arbitrary sort schemas via the dynamic label columns and `sort_schema`
  column.
- **Feature flag:** Not implemented in this work. Future issue will add a
  config toggle to enable/disable writing new sections.
- **Compaction:** The stats section is designed for the compactor but
  compaction logic is not part of this work.
