# DataObj Package Documentation

## Overview

The `dataobj` package provides a container format for storing and retrieving structured data from object storage. It's designed specifically for Loki's log storage needs, enabling efficient columnar storage and retrieval of log data with support for multiple tenants, metadata indexing, and flexible querying.

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Key Concepts](#key-concepts)
- [File Format Structure](#file-format-structure)
- [Columnar Section Format](#columnar-section-format)
- [Encoding Process](#encoding-process)
- [Decoding Process](#decoding-process)
- [Section Types](#section-types)
- [Operational Components](#operational-components)
- [Creating Custom Sections](#creating-custom-sections)

---

## Architecture Overview

The dataobj package provides a hierarchical container format:

```
┌─────────────────────────────────────┐
│         Data Object File            │
├─────────────────────────────────────┤
│  Header                             │
│  - Magic: "DOBJ"                    │
│  - File Metadata Size (4 bytes)     │
│  - File Format Version              │
│  - File Metadata (Protobuf)         │
│    - Dictionary                     │
│    - Section Types                  │
│    - Section Layout Info            │
├─────────────────────────────────────┤
│  Section 1 Metadata                 │
│  Section 2 Metadata                 │
│  ...                                │
│  Section N Metadata                 │
├─────────────────────────────────────┤
│  Section 1 Data                     │
│  Section 2 Data                     │
│  ...                                │
│  Section N Data                     │
├─────────────────────────────────────┤
│  Tailer                             │
│  - Magic: "DOBJ"                    │
└─────────────────────────────────────┘
```

The file metadata sits in the header, at the start of the file. This lets a
reader resolve every section's layout with one read from offset 0.

### Core Components

1. **Builder** - Constructs data objects by accumulating sections
2. **Encoder** - Low-level encoding of sections and metadata
3. **Decoder** - Low-level decoding of sections from object storage
4. **SectionReader** - Interface for reading data/metadata from sections
5. **Section Implementations** - Specific section types (logs, streams, postings, etc.)

---

## Key Concepts

### Data Objects

A **Data Object** is a self-contained file stored in object storage containing:
- One or more **Sections**, each holding a specific type of data
- **File Metadata** describing all sections and their layout
- **Dictionary** for efficient string storage (namespaces, kinds, tenant IDs)

### Sections

**Sections** are the primary organizational unit within a data object:
- Each section has a **type** (namespace, kind, version)
- Sections contain both **data** and **metadata** regions
- Multiple sections of the same type can exist in one object
- Sections can be tenant-specific

### Section Regions

Each section is split into two regions:

1. **Data Region** - The actual encoded data (e.g., columnar log data)
2. **Metadata Region** - Lightweight metadata to aid in reading the data region

This separation allows reading section metadata without loading the entire data payload.

### Columnar Storage

Most section implementations use columnar storage via the `internal/dataset` package:
- Data is organized into **columns** (e.g., timestamp, stream_id, message)
- Columns are split into **pages** for efficient random access
- Each page has a presence bitmap and the values. The values can be compressed (typically with zstd)
- Each page has a CRC32C checksum that readers verify
- Supports predicates for filtering during reads

---

## File Format Structure

### File Layout

```
Offset  | Content
--------|----------------------------------------------------------
0       | Magic bytes: "DOBJ" (4 bytes)
4       | File Metadata Size (uint32, little-endian)
8       | File Format Version (varint)
...     | File Metadata (protobuf-encoded)
...     | [Section Metadata Region - All sections concatenated]
...     | [Section Data Region - All sections concatenated]
-4      | Magic bytes: "DOBJ" (4 bytes)
```

File Metadata Structure (Protobuf) can be found in the `pkg/dataobj/internal/metadata` package.

---

## Columnar Section Format

The logs, streams, stats, postings, index pointers and watermarks sections share one encoding. It is implemented in `pkg/dataobj/sections/internal/columnar`. This part describes that encoding.

### Section Versions

The section version is the `version` of the section type in the file metadata. It decides the format of the pages, which is the format version. Section version 3 has format version 2.

| Section version | Page checksum | Written by |
|---|---|---|
| 2 | In the page descriptor | Older writers. All columnar sections. |
| 3 | In the page descriptor | Older writers. Logs sections in schema sort order only. |
| 4 | In the page | Current writers. All columnar sections. |

Readers accept section versions 2 and 4 for every columnar section, and version 3 for logs sections. Version 3 only marked the schema sort order of a logs section. It has format version 2. Writers no longer write it.

A reader that does not know a section version fails when it opens the section. A data object can hold sections of different versions.

### Section Layout

A columnar section has a data region and a metadata region.

```
Data region                      Metadata region
┌──────────────────────────┐     ┌──────────────────────────────────┐
│ Column 0                 │     │ ColumnMetadata of column 0       │
│   Page 0, Page 1, ...    │     │ ColumnMetadata of column 1       │
│ Column 1                 │     │ ...                              │
│   Page 0, Page 1, ...    │     │ SectionMetadata                  │
│ ...                      │     └──────────────────────────────────┘
└──────────────────────────┘
```

- The data region holds the columns one after the other. A column holds its pages one after the other, in row order.
- In format version 2, the metadata region starts with the format version, 2, as a uvarint. This includes logs sections of version 3. Readers do not use it: they take the version from the section type. It exists for readers of the first data object format, which kept the version in the metadata. Format version 4 does not have it.
- `ColumnMetadata` and `SectionMetadata` are protobuf messages. Each one starts with its size as a uvarint.
- `SectionMetadata` lists the columns. Each column descriptor has the offset and length of its `ColumnMetadata` in the metadata region.
- `ColumnMetadata` has one page descriptor for each page of the column.
- The extension data of the section, in the file metadata, holds the offset and length of `SectionMetadata` in the metadata region.
- Offsets in the metadata region are relative to the start of that region. The offset of a page is relative to the start of the data region.

### Page Descriptor

| Field | Meaning |
|---|---|
| `uncompressed_size` | Size of the page before compression: the presence bitmap size, the presence bitmap and the values. |
| `rows_count` | Number of rows in the page, NULLs included. |
| `values_count` | Number of values in the page, without NULLs. |
| `encoding` | Encoding of the values: plain, delta or bitmap. |
| `data_offset` | Offset of the page in the data region. |
| `data_size` | Size of the page in the data region: the page after compression and, in format version 4, its four-byte checksum. Readers use it to read the page. |
| `statistics` | Optional minimum and maximum value of the page. |
| `deprecated_crc32` | CRC32C of the page. Section versions 2 and 3 only. Readers use it for those pages. |

The compression is a property of the column, not of the page. Older writers also wrote the size of the page after compression, as `compressed_size` (field 2, now reserved). It has the same value as `data_size` in section versions 2 and 3. Readers do not use it.

### Page Layout

The layout of a page depends on the format version.

Format version 2 (section versions 2 and 3). The page has no checksum. The page descriptor holds the CRC32C and `data_size` is the size of the page.

```
┌──────────────────────────────┬─────────────────┬────────┐
│ uvarint presence bitmap size │ presence bitmap │ values │
└──────────────────────────────┴─────────────────┴────────┘
```

Format version 4 (section version 4). A four-byte CRC32C footer ends the page. The page descriptor has no checksum and `data_size` includes the footer.

```
┌──────────────────────────────┬─────────────────┬────────┬────────┐
│ uvarint presence bitmap size │ presence bitmap │ values │ CRC32C │
└──────────────────────────────┴─────────────────┴────────┴────────┘
└───────────────── covered by the CRC32C ─────────────────┘
```

- The presence bitmap has one entry for each row: 1 if the row has a value, 0 if it is NULL. It is never compressed.
- The values are the non-NULL values in row order. The encoding of the page, for example plain for binary values and delta for integers, turns them into bytes. The compression of the column then compresses those bytes.
- The CRC32C is the footer of a page of format version 4. It is little-endian.

### Page Checksum

A reader verifies the checksum of a page when it opens the page. The checksum is the CRC32C (Castagnoli) of the page bytes. It does not cover the page descriptor.

- Section versions 2 and 3: the descriptor holds the checksum. The reader compares it with the CRC32C of the `data_size` bytes at `data_offset`.
- Section version 4: the checksum is the last four bytes of the page, little-endian, and covers the bytes before it. The descriptor has no checksum, and `data_size` includes the four bytes.

In the descriptor, the checksum takes about six bytes for each page in the metadata region. A reader downloads the descriptors of every column it reads. Section version 4 moves the checksum into the page to make the descriptors smaller.

In section versions 2 and 3, the checksum in the descriptor also detects a descriptor that points at another valid page. Section version 4 does not detect this, because the checksum is part of the page.

---

## Encoding Process

### High-Level Encoding Flow

```
┌──────────────┐
│ Log Records  │
└──────┬───────┘
       │
       ▼
┌──────────────────┐
│ Section Builder  │  (e.g., logs.Builder)
│ - Buffer records │
│ - Create stripes │
│ - Merge stripes  │
└──────┬───────────┘
       │
       │ Flush
       ▼
┌──────────────────┐
│ Columnar Encoder │
│ - Encode columns │
│ - Compress pages │
└──────┬───────────┘
       │
       │ WriteSection
       ▼
┌──────────────────┐
│ dataobj.Builder  │
│ - Append section │
│ - Build metadata │
└──────┬───────────┘
       │
       │ Flush
       ▼
┌──────────────────┐
│ Snapshot         │
│ - In-memory/disk │
│ - Ready to upload│
└──────┬───────────┘
       │
       ▼
┌──────────────────┐
│ Object Storage   │
└──────────────────┘
```

### Encoding

#### 1. Section builders

A section builder:
1. Buffers records in memory up to `BufferSize`
2. When buffer is full, sorts records and creates a **stripe**
3. Stripes are intermediate compressed tables
4. Multiple stripes are merged when flushing the section

#### 2. Encoding to Columnar Format

When flushing, the builder:
1. Converts buffered records into columnar format
2. Creates separate columns for each field (stream_id, timestamp, metadata, message)
3. Splits columns into pages
4. Compresses the values of each page independently (zstd)
5. Computes the CRC32C checksum of each page and writes it as the last four bytes of the page

#### 3. Writing to Data Object

The data object builder:
1. Collects data and metadata from each section
2. Builds file metadata with section layout information

Column metadata includes:
- Page descriptors (sizes, row and value counts, encoding, offset, statistics and, in section versions 2 and 3, the checksum)
- Column statistics (min/max values)
- Encoding information

#### 4. Compression Strategy

The encoding uses a multi-level compression strategy:

- **Stripes** (intermediate): zstd with `SpeedFastest` for quick buffering
- **Sections** (final): zstd with `SpeedDefault` for better compression
- **Ordered Appends**: When logs arrive pre-sorted, skips stripe creation for better performance

---

## Decoding Process

### High-Level Decoding Flow

```
┌──────────────────┐
│ Object Storage   │
└──────┬───────────┘
       │
       │ Open
       ▼
┌──────────────────┐
│ dataobj.Object   │
│ - Read metadata  │
│ - Parse sections │
└──────┬───────────┘
       │
       │ Open Section
       ▼
┌──────────────────┐
│ Section (e.g.    │
│  logs.Section)   │
│ - Decode metadata│
│ - List columns   │
└──────┬───────────┘
       │
       │ NewRowReader
       ▼
┌──────────────────┐
│ logs.RowReader   │
│ - Read pages     │
│ - Apply filters  │
│ - Decompress     │
└──────┬───────────┘
       │
       │ Read batches
       ▼
┌──────────────────┐
│ Records          │
└──────────────────┘
```

### Detailed Decoding Steps

#### 1. Opening a Data Object

Opening process:
1. Reads first 16KB of file in one request (optimistic read for metadata)
2. Parses the header, at the start of the file, to find the metadata size
3. Reads and decodes file metadata
4. Constructs Section objects with SectionReaders

#### 2. Decoding File Metadata

The decoder:
1. Validates magic bytes ("DOBJ")
2. Reads metadata size from the header, right after the magic bytes
3. Reads the file format version, then the protobuf metadata that follows it
4. Uses the header size to compute where the section metadata region starts

#### 3. Opening a Section

Section opening:
1. Validates the section type and version (columnar sections accept section versions 2 and 4, logs sections also 3)
2. Reads extension data for quick access to section metadata
3. Decodes section metadata (columnar structure)

#### 4. Reading Data

Reading process:
1. Resolves the projected columns of the section
2. Maps predicates to internal dataset predicates
3. Reads pages in batches (with prefetching), and verifies the checksum of each page when it opens it
4. Applies predicates at page and row level
5. Decodes the rows into records

Reader optimizations:
- **Range coalescing**: Adjacent pages are read in a single request
- **Parallel reads**: Multiple pages can be fetched concurrently
- **Prefetching**: Pages are fetched ahead of time while processing current batch
- **Predicate pushdown**: Filters applied at page level using statistics

#### 5. Value Encoding Types

Values in pages can use different encodings:

- **Plain**: Raw values stored directly
- **Delta**: Store deltas between consecutive values (efficient for sorted data)
- **Bitmap**: For repetition levels (null/non-null indicators)

---

## Section Types

The package provides several built-in section types:

### 1. Logs Section

**Namespace**: `github.com/grafana/loki`
**Kind**: `logs`
**Location**: `pkg/dataobj/sections/logs/`

Stores log records in columnar format.

**Columns:**
- `stream_id` (int64): Stream identifier
- `timestamp` (int64): Nanosecond timestamp
- `metadata` (binary): Structured metadata key-value pairs, one column per key
- `message` (binary): Log line content

### 2. Streams Section

**Namespace**: `github.com/grafana/loki`
**Kind**: `streams`
**Location**: `pkg/dataobj/sections/streams/`

Stores stream metadata and statistics in logs objects.

**Columns:**
- `stream_id` (int64): Unique stream identifier
- `min_timestamp` (int64): Earliest timestamp in stream
- `max_timestamp` (int64): Latest timestamp in stream
- `labels` (binary): Label key-value pairs for the stream, one column per key
- `rows` (uint64): Number of log records
- `uncompressed_size` (uint64): Uncompressed data size

### 3. Pointers Section (legacy)

**Namespace**: `github.com/grafana/loki`
**Kind**: `pointers`

Older index objects hold this section. It points to objects that contain logs, via a stream or predicate lookup.
The index builder does not write it, and Loki has no code to read it.

**Columns:**
- `path` (binary): Path to data object in object storage
- `section` (int64): The section number within the referenced data object
- `pointer_kind` (int64): The type of pointer entry. Determines which fields are set in the section. Either stream or column.

// Fields present for a stream pointer type
- `stream_id` (int64): Stream identifier in this index object
- `stream_id_ref` (int64): Stream identifier in the target data object's stream section
- `min_timestamp` (int64): Min timestamp in the object for this stream
- `max_timestamp` (int64): Max timestamp in the object for this stream
- `row_count` (int64): Total rows for this stream in the target object
- `uncompressed_size` (int64): Total bytes for this stream in the target object

// Fields for a column pointer type
- `column_name` (binary): The name of the column in the referenced object
- `column_index` (int64): The index number of the column in the referenced object
- `values_bloom_filter` (binary): A bloom filter for unique values seen in the referenced column

### 4. Index Pointers Section

**Namespace**: `github.com/grafana/loki`
**Kind**: `indexpointers`
**Location**: `pkg/dataobj/sections/indexpointers/`

Used in Table of Contents (toc) objects to point to index objects, via a time range lookup.

**Columns:**
- `path` (binary): Path to the index file
- `min_time` (int64): Minimum time covered by the referenced index object
- `max_time` (int64): Maximum time covered by the referenced index object

---

## Operational Components

### Builder

**Location**: `pkg/dataobj/builder/`

The builder (the `dataobj-builder` target) reads log data from Kafka and builds data objects.

**Key Features:**
- Reads from Kafka partitions
- Accumulates logs into data objects, one per tenant and 12-hour window
- Flushes based on size or idle timeout
- Commits offsets after successful upload
- Builds an index object for each uploaded data object and records it in the Table of Contents.

### Metastore

**Location**: `pkg/dataobj/metastore/`

Manages an index of data objects and their contents for efficient querying.

The metastore serves queries by the following:
1. Fetch and scan the querying tenant's Table of Contents (toc) files for every 12-hour window in the query time range to resolve index objects. Each toc file holds a single tenant and is stored at `tocs/<window>/<tenant>/toc.toc`.
2. Fetches resolved index objects and reads their postings sections to resolve the log object sections that match the query.

### Index

**Location**: `pkg/dataobj/index/`

Creates index objects that contain indexes over data objects containing the logs. Used by the builder and by compaction.

### Explorer Service

**Location**: `pkg/dataobj/explorer/`

HTTP service for inspecting data objects.

---

## Testing and Tools

### Inspect Tool

```bash
# Inspect data object structure
go run ./pkg/dataobj/tools/inspect.go -path objects/ab/cd123...

# Show statistics
go run ./pkg/dataobj/tools/stats.go -path objects/ab/cd123...
```

### Explorer Service

The explorer provides a web UI for browsing data objects:

```bash
# Start explorer
./loki -target=dataobj-explorer

# Access UI
curl http://localhost:3100/dataobj/api/v1/list
```
