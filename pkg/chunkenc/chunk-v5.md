# Organized Chunk Format Documentation (WIP)
## Overview

The organized chunk format (represented by the format version V5/ChunkFormatV5) is a new storage format that separates log lines, timestamps, and structured metadata into distinct sections within a chunk to enable more efficient querying. This format aims to improve performance by organizing data in a way that minimizes unnecessary decompression when only specific fields are needed.

## Block Structure Diagram

```
┌─────────────────────────────────────────────┐
│               Chunk Format V5               │
├─────────────────────────────────────────────┤
│ Magic Number (4 bytes)                      │
│ Format Version (1 byte)                     │
│ Encoding Type (1 byte)                      │
├─────────────────────────────────────────────┤
│                                             │
│ Structured Metadata Section                 │
│ ┌─────────────────────────────────────┐    │
│ │ Length                              │    │
│ │ Compressed Metadata Symbols         │    │
│ │ Checksum                           │    │
│ └─────────────────────────────────────┘    │
│                                             │
│ Log Lines Section                          │
│ ┌─────────────────────────────────────┐    │
│ │ Length                              │    │
│ │ Compressed Log Lines                │    │
│ │ Checksum                           │    │
│ └─────────────────────────────────────┘    │
│                                             │
│ Timestamps Section                         │
│ ┌─────────────────────────────────────┐    │
│ │ Length                              │    │
│ │ Compressed Timestamps               │    │
│ │ Checksum                           │    │
│ └─────────────────────────────────────┘    │
│                                             │
│ Block Metadata Section                      │
│ ┌─────────────────────────────────────┐    │
│ │ Number of Blocks                    │    │
│ │ Block Entry Count                   │    │
│ │ Min/Max Timestamps                  │    │
│ │ Offsets & Sizes                    │    │
│ │ Checksum                           │    │
│ └─────────────────────────────────────┘    │
│                                             │
│ Section Offsets & Lengths                  │
└─────────────────────────────────────────────┘
```

## Section Details

1. **Header**
   - Magic Number (4 bytes): Identifies the chunk format
   - Format Version (1 byte): Version 5 for organized format
   - Encoding Type (1 byte): Compression type used

2. **Structured Metadata Section**
   - Contains label key-value pairs for each log entry
   - Compressed using the specified encoding
   - Includes length and checksum
   - Uses symbol table for efficient storage of repeated strings

3. **Log Lines Section**
   - Contains the actual log message content
   - Compressed independently
   - Includes length and checksum

4. **Timestamps Section**
   - Contains entry timestamps
   - Compressed independently
   - Includes length and checksum

5. **Block Metadata Section**
   - Number of blocks in the chunk
   - Entry counts per block
   - Min/Max timestamps per block
   - Offsets and sizes for each block
   - Checksum for integrity verification

6. **Section Offsets & Lengths**
   - End of chunk contains offsets and lengths for each major section
   - Enables quick navigation to specific sections

## Query Plan

The organized format enables optimized query patterns:

1. **Label Queries**
   - Can decompress only the structured metadata section
   - Avoids decompressing log lines and timestamps
   - Efficient for label-based filtering

2. **Timestamp-Based Queries**
   - Can read only timestamps section first
   - Enables efficient time range filtering before accessing log content
   - Reduces unnecessary decompression of log lines

3. **Content Queries**
   - For full text search or parsing
   - Decompresses log lines section
   - Can correlate with timestamps and metadata as needed

### Query Optimization Flow

```
┌──────────────────┐
│  Query Request   │
└────────┬─────────┘
         │
         v
┌──────────────────┐
│  Analyze Query   │
│    Components    │
└────────┬─────────┘
         │
         v
┌──────────────────┐   Yes   ┌─────────────────┐
│ Label Filtering? ├────────>│ Read Metadata   │
└────────┬─────────┘        └────────┬────────┘
         │ No                        │
         v                           v
┌──────────────────┐         ┌─────────────────┐
│  Time Filtering? │         │  Apply Label    │
└────────┬─────────┘         │   Filters       │
         │                   └────────┬────────┘
         v                           │
┌──────────────────┐                │
│   Read Lines     │                │
└────────┬─────────┘                │
         │                          │
         v                          v
┌──────────────────────────────────────────┐
│            Combine Results               │
└──────────────────────────────────────────┘
```

## Implementation Notes

The implementation is handled through several key components:

1. **Block Organization**
   - `organisedHeadBlock` struct manages the organization of data during writes
   - Maintains separate buffers for lines, timestamps, and metadata

2. **Iterator Implementation**
   - `organizedBufferedIterator` provides efficient access to the organized format
   - Can selectively decompress only needed sections
   - Maintains separate readers for each section

3. **Compression**
   - Each section can be compressed independently
   - Enables optimal compression for different types of data
   - Supports various compression algorithms through the `compression.Codec` interface

## Benefits

1. **Reduced I/O**
   - Selective decompression of only needed sections
   - More efficient use of memory and CPU

2. **Better Compression Ratios**
   - Similar data types grouped together
   - More effective compression within sections

3. **Query Flexibility**
   - Optimized access patterns for different query types
   - Better performance for label and time-based queries

4. **Maintainability**
   - Clear separation of concerns
   - Easier to extend and modify individual sections

