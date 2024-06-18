# Loki New Object Storage WAL

## Principles

- The WAL can be streamed to a file or remote object storage.
- When building WAL segments in the ingester, prioritize colocation first by tenant and then by series. This allows efficient reading during compaction and querying.
- At compaction, chunks from the WAL should be reusable and writable to the new block format without decompression.

We aim for at least 8MB WAL segments, preferably larger. In a cluster with a 32MB/s write rate, using 4 ingesters will suffice, halving the current ingester requirement.

## Overview

Multitenancy is achieved by storing the tenant as a label `__0_tenant_id__` in the index to ensure sorting by tenant first. This label is not exposed to users and is removed during compaction.

```
┌──────────────────────────────┐
│    Magic Header ("LOKW")     │
│           (4 bytes)          │
├──────────────────────────────┤
│ ┌──────────────────────────┐ │
│ │         Chunk 1          │ │
│ ├──────────────────────────┤ │
│ │          ...             │ │
│ ├──────────────────────────┤ │
│ │         Chunk N          │ │
│ └──────────────────────────┘ │
├──────────────────────────────┤
│           Index              │
├──────────────────────────────┤
│       Index Len (4b)         │
├──────────────────────────────┤
│    Version (1 byte)          │
├──────────────────────────────┤
│    Magic Footer ("LOKW")     │
│           (4 bytes)          │
└──────────────────────────────┘
```

## Index

The index format is designed to enable efficient seeking to specific chunks required for recent queries. Inspired by the [Prometheus](https://github.com/prometheus/prometheus/blob/main/tsdb/docs/format/index.md) tsdb index, it has some key differences, particularly in the chunk reference within the Series tables. This reference contains sufficient information to seek directly to the chunk in the WAL (Write-Ahead Log).

```
┌────────────────────────────────────────────────────────────────────────────┐
│ len <uvarint>                                                              │
├────────────────────────────────────────────────────────────────────────────┤
│ ┌────────────────────────────────────────────────────────────────────────┐ │
│ │                     labels count <uvarint64>                           │ │
│ ├────────────────────────────────────────────────────────────────────────┤ │
│ │              ┌────────────────────────────────────────────────┐        │ │
│ │              │ ref(l_i.name) <uvarint32>                      │        │ │
│ │              ├────────────────────────────────────────────────┤        │ │
│ │              │ ref(l_i.value) <uvarint32>                     │        │ │
│ │              └────────────────────────────────────────────────┘        │ │
│ │                             ...                                        │ │
│ ├────────────────────────────────────────────────────────────────────────┤ │
│ │                     chunks count <uvarint64>                           │ │
│ ├────────────────────────────────────────────────────────────────────────┤ │
│ │              ┌────────────────────────────────────────────────┐        │ │
│ │              │ c_0.mint <varint64>                            │        │ │
│ │              ├────────────────────────────────────────────────┤        │ │
│ │              │ c_0.maxt - c_0.mint <uvarint64>                │        │ │
│ │              ├────────────────────────────────────────────────┤        │ │
│ │              │ ref(c_0.data) <uvarint64>                      │        │ │
│ │              ├────────────────────────────────────────────────┤        │ │
│ │              │ c_0.entries <uvarint32>                        │        │ │
│ │              └────────────────────────────────────────────────┘        │ │
│ │              ┌────────────────────────────────────────────────┐        │ │
│ │              │ c_i.mint - c_i-1.maxt <uvarint64>              │        │ │
│ │              ├────────────────────────────────────────────────┤        │ │
│ │              │ c_i.maxt - c_i.mint <uvarint64>                │        │ │
│ │              ├────────────────────────────────────────────────┤        │ │
│ │              │ ref(c_i.data) - ref(c_i-1.data) <varint64>     │        │ │
│ │              ├────────────────────────────────────────────────┤        │ │
│ │              │ c_i.entries <uvarint32>                        │        │ │
│ │              └────────────────────────────────────────────────┘        │ │
│ │                             ...                                        │ │
│ ├────────────────────────────────────────────────────────────────────────┤ │
│ │              ┌────────────────────────────────────────────────┐        │ │
│ │              │ last_chunk.mint - prev_chunk.maxt <uvarint64>  │        │ │
│ │              ├────────────────────────────────────────────────┤        │ │
│ │              │ last_chunk.maxt - last_chunk.mint <uvarint64>  │        │ │
│ │              ├────────────────────────────────────────────────┤        │ │
│ │              │ ref(last_chunk.data) - ref(prev_chunk.data)    │        │ │
│ │              │ <varint64>                                     │        │ │
│ │              ├────────────────────────────────────────────────┤        │ │
│ │              │ last_chunk.entries <uvarint32>                 │        │ │
│ │              └────────────────────────────────────────────────┘        │ │
│ └────────────────────────────────────────────────────────────────────────┘ │
├────────────────────────────────────────────────────────────────────────────┤
│ CRC32 <4b>                                                                 │
└────────────────────────────────────────────────────────────────────────────┘
```

> Note: data_len for all entries except the last one is inferred from the offset of the next entry.

### Explanation

- **len <uvarint>**: The length of the series entry.
- **labels count <uvarint64>**: The number of labels in the series.
- **ref(l_i.name) <uvarint32>**: Reference to the label name in the symbol table.
- **ref(l_i.value) <uvarint32>**: Reference to the label value in the symbol table.
- **chunks count <uvarint64>**: The number of chunks in the series.
- **c_0.mint <varint64>**: Minimum timestamp of the first chunk.
- **c_0.maxt - c_0.mint <uvarint64>**: Time delta between the minimum and maximum timestamp of the first chunk.
- **ref(c_0.data) <uvarint64>**: Reference to the chunk data.
- **c_0.entries <uvarint32>**: Number of entries in the chunk.
- **c_i.mint - c_i-1.maxt <uvarint64>**: Time delta between the minimum timestamp of the current chunk and the maximum timestamp of the previous chunk.
- **c_i.maxt - c_i.mint <uvarint64>**: Time delta between the minimum and maximum timestamp of the current chunk.
- **ref(c_i.data) - ref(c_i-1.data) <varint64>**: Delta between the current chunk reference and the previous chunk reference.
- **c_i.entries <uvarint32>**: Number of entries in the chunk.
- **CRC32 <4b>**: CRC32 checksum of the series entry.

## Chunks

### Chunk Format Overview

The chunk format is structured to efficiently store and retrieve log data. It starts with a byte that indicates the encoding used for the raw logs, followed by a sequence of double-delta encoded timestamps and the lengths of each log line. Finally, it includes the raw compressed logs and a CRC32 checksum for the metadata.

#### Key Components of the Chunk Format

1. **Initial Byte**:
   - Indicates the encoding (compression format, we'll start with 1 for snappy ) used for the raw logs.

2. **Timestamps and Lengths**:
   - A sequence of double-delta encoded timestamps.
   - Lengths of each log line.

3. **Raw Compressed Logs**:
   - The actual log data, compressed for efficiency.

4. **CRC32 Checksum**:
   - Ensures the integrity of the metadata.

Unlike the current Loki chunk format, this format does not use smaller blocks because WAL (Write-Ahead Log) segments are typically created within seconds.

### Structure of a Chunk

```
┌──────────────────────────────────────────────────────────────────────────┐
│ encoding (1 byte)                                                        │
├──────────────────────────────────────────────────────────────────────────┤
│ #entries <uvarint>                                                       │
├──────────────────────────────────────────────────────────────────────────┤
│ ts_0 <uvarint>                                                           │
├──────────────────────────────────────────────────────────────────────────┤
│ len_line_0 <uvarint>                                                     │
├──────────────────────────────────────────────────────────────────────────┤
│ ts_1_delta <uvarint>                                                     │
├──────────────────────────────────────────────────────────────────────────┤
│ len_line_1 <uvarint>                                                     │
├──────────────────────────────────────────────────────────────────────────┤
│ ts_2_dod <varint>                                                        │
├──────────────────────────────────────────────────────────────────────────┤
│ len_line_2 <uvarint>                                                     │
├──────────────────────────────────────────────────────────────────────────┤
│ ...                                                                      │
├──────────────────────────────────────────────────────────────────────────┤
│ compressed logs <bytes>                                                  │
├──────────────────────────────────────────────────────────────────────────┤
| compressed logs offset <4b>                                              |
├──────────────────────────────────────────────────────────────────────────┤
│ crc32 <4 bytes>                                                          │
└──────────────────────────────────────────────────────────────────────────┘
```

#### Explanation

- **encoding (1 byte)**: Indicates the encoding used for the raw logs (e.g., 0 for no compression, 1 for gzip, etc.).
- **#entries <uvarint>**: The number of log entries in the chunk.
- **ts_0 <uvarint>**: The initial timestamp, with nanosecond precision.
- **len_line_0 <uvarint>**: The length of the first log line.
- **ts_1_delta <uvarint>**: The delta from the initial timestamp to the second timestamp.
- **len_line_1 <uvarint>**: The length of the second log line.
- **ts_2_dod <varint>**: The delta of deltas, representing the difference from the previous delta (i.e., double-delta encoding). Can be negative if the spacing between points is decreasing.
- **len_line_2 <uvarint>**: The length of the third log line.
- **compressed logs <bytes>**: The actual log data, compressed according to the specified encoding.
- **compressed logs offset <4 bytes>**: The offset of the compressed log data.
- **crc32 (4 bytes)**: CRC32 checksum for the metadata (excluding the compressed data), ensuring the integrity of the timestamp and length information.

The offset to the compressed logs is known from the index, allowing efficient access and decompression. The CRC32 checksum at the end verifies the integrity of the metadata, as the compressed data typically includes its own CRC for verification.

This structure ensures efficient storage and retrieval of log entries, utilizing double-delta encoding for timestamps and compressing the log data to save space. The timestamps are precise to nanoseconds, allowing for high-resolution time tracking.
