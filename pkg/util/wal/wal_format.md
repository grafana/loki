# WAL Segment Format Documentation

## Overview

A WAL (Write-Ahead Log) segment is a file containing a sequence of records. Each segment is divided into 32KB pages, and records can span multiple pages but never cross segment boundaries. This document describes the binary format of WAL segment files as used in Prometheus TSDB.

## Segment Structure

```
┌─────────────────────────────────────────────────────────────────┐
│                        WAL SEGMENT FILE                         │
├─────────────────────────────────────────────────────────────────┤
│                         PAGE 0 (32KB)                          │
├─────────────────────────────────────────────────────────────────┤
│  RECORD 1  │  RECORD 2  │  RECORD 3  │     ...     │  PADDING  │
├─────────────────────────────────────────────────────────────────┤
│                         PAGE 1 (32KB)                          │
├─────────────────────────────────────────────────────────────────┤
│  RECORD N  │  RECORD N+1 │     ...     │           │  PADDING  │
├─────────────────────────────────────────────────────────────────┤
│                           ...                                   │
└─────────────────────────────────────────────────────────────────┘
```

## Record Structure

Every record in a WAL segment follows this structure:

```
┌─────────────┬─────────────────────────────────────────────────────┐
│   HEADER    │                    DATA                             │
│   (7 bytes) │                (variable length)                    │
└─────────────┴─────────────────────────────────────────────────────┘
```

### Header Format (7 bytes total)

```
 Byte 0      Bytes 1-2        Bytes 3-6
┌─────────┬─────────────────┬─────────────────────────────────────┐
│  TYPE   │      LENGTH     │               CRC32                 │
│(1 byte) │   (2 bytes)     │            (4 bytes)                │
└─────────┴─────────────────┴─────────────────────────────────────┘
```

#### Byte 0 - Record Type and Compression Flags

```
Bit:  7   6   5   4   3   2   1   0
     ┌───┬───┬───┬───┬───┬───┬───┬───┐
     │ - │ - │ - │ Z │ S │ T │ T │ T │
     └───┴───┴───┴───┴───┴───┴───┴───┘
      │   │   │   │   │   └───┴───┴───┘
      │   │   │   │   │       └─ Record Type (3 bits)
      │   │   │   │   └─ Snappy Compression Flag (1 bit)
      │   │   │   └─ Zstd Compression Flag (1 bit)  
      └───┴───┴─ Unallocated (3 bits)
```

**Record Types:**
- `0` (recPageTerm): Rest of page is empty
- `1` (recFull): Complete record fits in current page
- `2` (recFirst): First fragment of a record spanning multiple pages
- `3` (recMiddle): Middle fragment of a record spanning multiple pages
- `4` (recLast): Final fragment of a record spanning multiple pages

**Compression Flags:**
- Bit 3 (snappyMask = 0x08): Set if data is Snappy compressed
- Bit 4 (zstdMask = 0x10): Set if data is Zstd compressed

#### Bytes 1-2 - Data Length
Big-endian 16-bit unsigned integer representing the length of the data portion in bytes.

#### Bytes 3-6 - CRC32 Checksum
Big-endian 32-bit CRC32 checksum (Castagnoli polynomial) of the data portion only.

## Record Fragmentation

When a record is larger than the remaining space in a page, it gets fragmented:

```
Page N                           Page N+1
┌─────────────────────────────┐  ┌─────────────────────────────────┐
│ [HEADER] [DATA PART 1]      │  │ [HEADER] [DATA PART 2] [HEADER] │
│ Type: recFirst              │  │ Type: recLast      Type: recFull│
│ Length: 1024                │  │ Length: 512        Length: 256  │
│ CRC: 0x12345678             │  │ CRC: 0x87654321    CRC: 0xABCD  │
└─────────────────────────────┘  └─────────────────────────────────┘
```

## Page Boundaries

- Each page is exactly 32KB (32,768 bytes)
- Records never span across segment boundaries
- Unused space at the end of pages is zero-padded
- A `recPageTerm` record type indicates the rest of the page is empty

## Data Format

The data portion contains the actual record payload. The format depends on the application using the WAL
- **Series Records**: Encoded series labels and references
- **Sample Records**: Encoded time series samples  
- **Tombstone Records**: Encoded deletion markers
- **Custom Records**: Application-specific data.

## Compression

When compression is enabled:
1. The data is compressed before writing
2. The appropriate compression flag is set in the header
3. The CRC is calculated on the compressed data
4. The length field reflects the compressed data size


## References

- [Prometheus WAL Disk Format](https://github.com/prometheus/prometheus/blob/main/tsdb/docs/format/wal.md)
- [Prometheus TSDB WAL and Checkpoint](https://ganeshvernekar.com/blog/prometheus-tsdb-wal-and-checkpoint/)