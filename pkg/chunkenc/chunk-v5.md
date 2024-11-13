# Organized Chunk Format Documentation (WIP)
## Overview

The organized head format (represented by the format version V5/ChunkFormatV5) is a new storage format that separates log lines, timestamps, and structured metadata into distinct sections within a compressed block to enable more efficient querying. This format aims to improve performance by organizing data in a way that minimizes unnecessary decompression when only specific fields are needed.

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

The organized format enables several potential query optimizations on queries with vector aggregation on structured metadata:
It can read only the structured metadata section  and avoids decompressing log lines and timestamps.

## Implementation Notes

- The format maintains backwards compatibility with existing unordered head blocks
- Each section is independently compressed, allowing for section-specific optimization
- The symbol table approach in structured metadata reduces memory usage for repeated labels

-----------------

# Iteration 1: Initial Benchmark Results and Analysis

## Key Findings thus far
1. **Positive Results**
   - Significant in total decompressed bytes (as lines are not decompressed at all)
   - Successful selective decompression (no lines decompressed for sample queries)

2. **Areas for Improvement**
   - V5 is 12% slower execution time in Chunk V5. This needs to be definitely optimized further.
   - Higher memory usage (~1.6GB increase)
   - Increased allocation count (possibly due to use of multiple buffers for TS, line and metadata)

## Next Steps

- [ ] **Performance Optimization**
   - Profile + compare memory usage to identify causes of increased allocation
   - Optimize metadata section compression/decompression
   - Investigate potential buffer reuse strategies
   - Consider adding memory pools for common operations (?)

- [ ] **Query Path Enhancements**
   - Do not read lines only when the vector aggregation is on structured metadata. Still evaluating how do do this in code.


- [ ] **Testing**
   - Verify if st.stats are reported properly 
   - Add more comprehensive benchmark scenarios
   - Test with various query patterns and load on dev environment
   - Measure impact of different compression settings

The initial results show promise in terms of data organization and selective access, but a lot of further optimization is needed to address performance overhead.
