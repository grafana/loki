# Organized Chunk Format Documentation (WIP)
## Overview

The organized chunk format (represented by the format version V5/ChunkFormatV5) is a new storage format that separates log lines, timestamps, and structured metadata into distinct sections within a chunk to enable more efficient querying. This format aims to improve performance by organizing data in a way that minimizes unnecessary decompression when only specific fields are needed.

## Block Structure Diagram

```
┌─────────────────────────────────────────┐
│    Compresssed Block (for Chunk V5)     │
├─────────────────────────────────────────┤
│           Log Lines Section             │
│ ┌─────────────────────────────────────┐ │
│ │ Length                              │ │
│ │ Compressed Log Lines                │ │
│ │ Checksum                            │ │
│ └─────────────────────────────────────┘ │
│       Structured Metadata Section       │
│ ┌─────────────────────────────────────┐ │
│ │ Length                              │ │
│ │ Compressed Metadata Symbols         │ │
│ │ Checksum                            │ │
│ └─────────────────────────────────────┘ │
│        Timestamps Section               │
│ ┌─────────────────────────────────────┐ │
│ │ Length                              │ │
│ │ Compressed Timestamps               │ │
│ │ Checksum                            │ │
│ └─────────────────────────────────────┘ │
│                                         │
│ Block Metadata Section                  │
│ ┌─────────────────────────────────────┐ │
│ │ Number of Blocks                    │ │
│ │ Block Entry Count                   │ │
│ │ Min/Max Timestamps                  │ │
│ │ Offsets & Sizes                     │ │
│ │ Checksum                            │ │
│ └─────────────────────────────────────┘ │
│                                         │
│ Section Offsets & Lengths               │
└─────────────────────────────────────────┘
```

## Section Details

1. **Log Lines Section**
   - Contains the actual log message content
   - Each entry prefixed with its length (varint encoded)
   - Compressed using the configured compression algorithm
   - Format: `len(line1) | line1 | len(line2) | line2 | ...`

2. **Structured Metadata Section**
   - Stores label key-value pairs using a symbol table
   - Each entry contains the count of symbol pairs followed by the pairs
   - Symbol pairs are stored as integer references to the symbol table
   - Format: `section_len | num_symbols | (symbol_ref_name, symbol_ref_value)*`

3. **Timestamps Section**
   - Contains entry timestamps in chronological order
   - Timestamps are varint encoded
   - Compressed independently of other sections
   - Format: `timestamp1 | timestamp2 | ...`

## Implementation Components

### Key Structures

```go
type organisedHeadBlock struct {
    unorderedHeadBlock
}
```

Extends the unordered head block with organized storage capabilities.

### Main Methods

1. **Serialization Methods**
```go
// Serializes log lines section
func (b *organisedHeadBlock) Serialise(pool compression.WriterPool) ([]byte, error)

// Serializes structured metadata section
func (b *organisedHeadBlock) serialiseStructuredMetadata(pool compression.WriterPool) ([]byte, error)

// Serializes timestamps section
func (b *organisedHeadBlock) serialiseTimestamps(pool compression.WriterPool) ([]byte, error)
```

2. **Iterator Implementation**
```go
type organizedBufferedIterator struct {
    // Separate readers for each section
    reader     io.Reader    // for log lines
    smReader   io.Reader    // for structured metadata
    tsReader   io.Reader    // for timestamps
    // ... other fields
}
```

## Query Plan Considerations

The organized format enables several potential query optimizations (to be implemented):

1. **Label-Only Queries**
   - Can read only the structured metadata section
   - Avoids decompressing log lines and timestamps

2. **Time-Range Queries**
   - Can read only the timestamps section first
   - Enables efficient time filtering before accessing log content

3. **Content Queries**
   - Requires reading log lines section
   - Can correlate with timestamps and metadata as needed

## Current Limitations

1. Query optimizations are not yet implemented - this is just the format definition
2. Performance characteristics need to be benchmarked
3. The impact on memory usage during writes needs to be evaluated

## Future Enhancements

1. Implementation of selective section reading based on query type
2. Addition of query optimization logic
3. Performance benchmarking and tuning
4. Potential addition of indexes within sections

## Implementation Notes

- The format maintains backwards compatibility with existing unordered head blocks
- Each section is independently compressed, allowing for section-specific optimization
- The symbol table approach in structured metadata reduces memory usage for repeated labels
