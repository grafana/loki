# DataObj Package Documentation

## Overview

The `dataobj` package provides a container format for storing and retrieving structured data from object storage. It's designed specifically for Loki's log storage needs, enabling efficient columnar storage and retrieval of log data with support for multiple tenants, metadata indexing, and flexible querying.

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Key Concepts](#key-concepts)
- [File Format Structure](#file-format-structure)
- [Encoding Process](#encoding-process)
- [Decoding Process](#decoding-process)
- [Section Types](#section-types)
- [Operational Components](#operational-components)
- [Creating Custom Sections](#creating-custom-sections)
- [Usage Examples](#usage-examples)

---

## Architecture Overview

The dataobj package provides a hierarchical container format:

```
┌─────────────────────────────────────┐
│         Data Object File            │
├─────────────────────────────────────┤
│  Header (Magic: "THOR")            │
├─────────────────────────────────────┤
│  Section 1 Data                     │
│  Section 2 Data                     │
│  ...                                │
│  Section N Data                     │
├─────────────────────────────────────┤
│  Section 1 Metadata                 │
│  Section 2 Metadata                 │
│  ...                                │
│  Section N Metadata                 │
├─────────────────────────────────────┤
│  File Metadata (Protobuf)           │
│  - Dictionary                       │
│  - Section Types                    │
│  - Section Layout Info              │
├─────────────────────────────────────┤
│  Footer                             │
│  - File Format Version              │
│  - File Metadata Size (4 bytes)     │
│  - Magic: "THOR"                    │
└─────────────────────────────────────┘
```

### Core Components

1. **Builder** - Constructs data objects by accumulating sections
2. **Encoder** - Low-level encoding of sections and metadata
3. **Decoder** - Low-level decoding of sections from object storage
4. **SectionReader** - Interface for reading data/metadata from sections
5. **Section Implementations** - Specific section types (logs, streams, pointers, etc.)

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
- Pages can be compressed (typically with zstd)
- Supports predicates for filtering during reads

---

## File Format Structure

### File Layout

```
Offset  | Content
--------|----------------------------------------------------------
0       | Magic bytes: "THOR" (4 bytes)
4       | [Section Data Region - All sections concatenated]
...     | [Section Metadata Region - All sections concatenated]
...     | Format Version (varint)
...     | File Metadata (protobuf-encoded)
-8      | Metadata Size (uint32, little-endian)
-4      | Magic bytes: "THOR" (4 bytes)
```

File Metadata Structure (Protobuf) can be found in the `pkg/dataobj/internal/metadata` package.

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
4. Compresses each page independently (zstd)

#### 3. Writing to Data Object

The data object builder:
1. Collects data and metadata from each section
2. Builds file metadata with section layout information

Column metadata includes:
- Page descriptors (offset, size, row count, compression)
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
       │ NewReader
       ▼
┌──────────────────┐
│ logs.Reader      │
│ - Read pages     │
│ - Apply filters  │
│ - Decompress     │
└──────┬───────────┘
       │
       │ Read batches
       ▼
┌──────────────────┐
│ Arrow Records    │
└──────────────────┘
```

### Detailed Decoding Steps

#### 1. Opening a Data Object

Opening process:
1. Reads last 16KB of file in one request (optimistic read for metadata)
2. Parses footer to find metadata offset and size
3. Reads and decodes file metadata
4. Constructs Section objects with SectionReaders

#### 2. Decoding File Metadata

The decoder:
1. Validates magic bytes ("THOR")
2. Reads metadata size from last 8 bytes
3. Seeks to metadata offset
4. Decodes format version and protobuf metadata

#### 3. Opening a Section

Section opening:
1. Validates section type and version
2. Reads extension data for quick access to section metadata
3. Decodes section metadata (columnar structure)

#### 4. Reading Data

Reading process:
1. Validates reader options
2. Maps predicates to internal dataset predicates
3. Reads pages in batches (with prefetching)
4. Applies predicates at page and row level
5. Converts to Arrow record batches

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

Stores stream metadata and statistics.

**Columns:**
- `stream_id` (int64): Unique stream identifier
- `min_timestamp` (int64): Earliest timestamp in stream
- `max_timestamp` (int64): Latest timestamp in stream
- `labels` (binary): Label key-value pairs for the stream, one column per key
- `rows` (uint64): Number of log records
- `uncompressed_size` (uint64): Uncompressed data size

### 3. Pointers Section

**Namespace**: `github.com/grafana/loki`
**Kind**: `pointers`
**Location**: `pkg/dataobj/sections/pointers/`

Used in index objects to point to other objects that contain logs, via stream or predicate lookup.

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

### Consumer

**Location**: `pkg/dataobj/consumer/`

The consumer reads log data from Kafka and builds data objects.

**Key Features:**
- Reads from Kafka partitions
- Accumulates logs into data objects
- Flushes based on size or idle timeout
- Commits offsets after successful upload
- Emits metadata events containing a reference to each successfully uploaded object.

### Metastore

**Location**: `pkg/dataobj/metastore/`

Manages an index of data objects and their contents for efficient querying.

The metastore serves queries by the following:
1. Fetch and scan relevant Table of Contents (toc) files from the query time range to resolve index objects.
2. Fetches resolved index objects and utilises the contained indexes (stream sections, blooms, etc.) to resolve log objects & metadata such as size and number of log lines.

### Index Builder

**Location**: `pkg/dataobj/index/`

Creates index objects that contain indexes over data objects containing the logs.

### Explorer Service

**Location**: `pkg/dataobj/explorer/`

HTTP service for inspecting data objects.

---

## Usage Examples

### Example 1: Building and Uploading a Data Object

```go
package main

import (
    "context"
    "time"

    "github.com/grafana/loki/v3/pkg/dataobj"
    "github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
    "github.com/grafana/loki/v3/pkg/dataobj/uploader"
    "github.com/prometheus/prometheus/model/labels"
)

func main() {
    ctx := context.Background()

    // Create logs builder
    logsBuilder := logs.NewBuilder(nil, logs.BuilderOptions{
        PageSizeHint: 256 * 1024,
        BufferSize: 10 * 1024 * 1024,
        AppendStrategy: logs.AppendUnordered,
        SortOrder: logs.SortStreamASC,
    })

    // Append log records
    logsBuilder.Append(logs.Record{
        StreamID:  12345,
        Timestamp: time.Now(),
        Metadata:  labels.Labels{{Name: "level", Value: "error"}},
        Line:      []byte("error occurred"),
    })

    // Create data object
    objBuilder := dataobj.NewBuilder(nil)
    objBuilder.Append(logsBuilder)

    obj, closer, err := objBuilder.Flush()
    if err != nil {
        panic(err)
    }
    defer closer.Close()

    // Upload to object storage
    uploader := uploader.New(uploaderCfg, bucket, logger)
    objectPath, err := uploader.Upload(ctx, obj)
    if err != nil {
        panic(err)
    }

    println("Uploaded to:", objectPath)
}
```

### Example 2: Reading and Querying Logs

```go
package main

import (
    "context"
    "io"

    "github.com/apache/arrow-go/v18/arrow/scalar"
    "github.com/grafana/loki/v3/pkg/dataobj"
    "github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

func main() {
    ctx := context.Background()

    // Open data object
    obj, err := dataobj.FromBucket(ctx, bucket, "objects/ab/cd123...")
    if err != nil {
        panic(err)
    }

    // Find logs sections
    var logsSection *dataobj.Section
    for _, sec := range obj.Sections() {
        if logs.CheckSection(sec) {
            logsSection = sec
            break
        }
    }

    // Open logs section
    section, err := logs.Open(ctx, logsSection)
    if err != nil {
        panic(err)
    }

    // Find columns
    var timestampCol, messageCol *logs.Column
    for _, col := range section.Columns() {
        switch col.Type {
        case logs.ColumnTypeTimestamp:
            timestampCol = col
        case logs.ColumnTypeMessage:
            messageCol = col
        }
    }

    // Create reader with predicate
    reader := logs.NewReader(logs.ReaderOptions{
        Columns: []*logs.Column{timestampCol, messageCol},
        Predicates: []logs.Predicate{
            logs.GreaterThanPredicate{
                Column: timestampCol,
                Value:  scalar.NewInt64Scalar(startTime.UnixNano()),
            },
        },
    })
    defer reader.Close()
    
    if err := reader.Open(ctx); err != nil {
        panic(err)
    }

    // Read batches
    for {
        batch, err := reader.Read(ctx, 1000)
        if err == io.EOF {
            break
        }
        if err != nil {
            panic(err)
        }

        // Process batch (Arrow record)
        for i := 0; i < int(batch.NumRows()); i++ {
            timestamp := batch.Column(0).(*array.Timestamp).Value(i)
            message := batch.Column(1).(*array.String).Value(i)
            println(timestamp, message)
        }

        batch.Release()
    }
}
```

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
