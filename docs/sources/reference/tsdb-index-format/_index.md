---
title: TSDB index format
menuTitle: TSDB index format
description: Describes the on-disk binary layout of the Loki TSDB index and links to the specification of each format version.
weight: 700
---
# TSDB index format

Loki stores its [TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/tsdb/) index as a single immutable file per index period. The file layout derives from the Prometheus TSDB index, but Loki extends it with log-specific data such as per-chunk size and entry counts, a series fingerprint, and a fingerprint offsets table used for sharding.

The first five bytes of every index file identify the format:

```
+----------------------------------+
| magic(0xBAAAD700) <4 bytes>      |
+----------------------------------+
| version <1 byte>                 |
+----------------------------------+
```

Loki reads and writes three versions. Which version is written depends on the schema version of the period configuration:

| Index format                                                                        | Schema version | Added                                                                     | Notes        |
| ----------------------------------------------------------------------------------- | -------------- | ------------------------------------------------------------------------- | ------------ |
| [v2](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/tsdb-index-format/v2/)  | `v9` - `v12`   | Loki's initial TSDB format.                                               | Deprecated   |
| [v3](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/tsdb-index-format/v3/)  | `v13`          | Chunk page markers, which allow paging through the chunks of a series.    | Active       |
| [v4](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/tsdb-index-format/v4/)  | `v14`          | Per-chunk ingestion timestamp, which allows retention based on ingestion. | Experimental |

The index decoder rejects any other version. All three versions are readable by the same Loki binary, so periods with different schema versions coexist and no data migration is required when you change the schema. To rewrite existing index files into another version, use the `tools/tsdb/migrate-versions` tool.

## Encoding conventions

The following notations are used on the version pages:

| Notation      | Meaning                                                                                |
| ------------- | -------------------------------------------------------------------------------------- |
| `<N bytes>`   | Fixed-width big-endian integer.                                                        |
| `uvarint`     | Variable-length unsigned integer, as written by Go's `binary.PutUvarint`.              |
| `varint`      | Variable-length zig-zag encoded signed integer, as written by Go's `binary.PutVarint`. |
| `uvarint_str` | String prefixed with its byte length as a `uvarint`.                                   |
| `CRC32`       | 4-byte CRC32 checksum using the Castagnoli polynomial.                                 |

Additional rules that hold for every version:

- A `len` field always counts the bytes that follow it up to, but excluding, the trailing `CRC32` of the same section.
- Series entries are padded to 16-byte alignment. A series reference is the byte offset of the entry divided by 16, which extends the addressable range of 4-byte references to 64 GB.
- Label index entries, postings lists, and the start of the postings section are padded to 4-byte alignment for more efficient scans.
- All sections are located through the table of contents (TOC) in the last 76 bytes of the file, so sections are found by offset rather than by sequential parsing.

## Section order

All versions share the same set of sections and the same order:

```
+----------------------------------+
| header                           |
+----------------------------------+
| symbol table                     |
+----------------------------------+
| series                           |
+----------------------------------+
| label indices                    |
+----------------------------------+
| postings                         |
+----------------------------------+
| label offset table               |
+----------------------------------+
| postings offset table            |
+----------------------------------+
| fingerprint offsets table        |
+----------------------------------+
| TOC                              |
+----------------------------------+
```

The versions differ only in the chunks part of a series entry. Everything else is identical, which is why an index file can be upgraded or downgraded without touching the symbol table or the postings.

## Version specifications

- [Index format v2](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/tsdb-index-format/v2/)
- [Index format v3](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/tsdb-index-format/v3/)
- [Index format v4](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/tsdb-index-format/v4/)

## Source code

The format is implemented in [`pkg/storage/stores/shipper/indexshipper/tsdb/index`](https://github.com/grafana/loki/tree/main/pkg/storage/stores/shipper/indexshipper/tsdb/index):

- [`index.go`](https://github.com/grafana/loki/blob/main/pkg/storage/stores/shipper/indexshipper/tsdb/index/index.go) defines the version constants, the `Creator` that writes each section, and the `Decoder` that reads them.
- [`chunk.go`](https://github.com/grafana/loki/blob/main/pkg/storage/stores/shipper/indexshipper/tsdb/index/chunk.go) defines the chunk meta and the page markers, including the page size constants.
- [`schema_config.go`](https://github.com/grafana/loki/blob/main/pkg/storage/config/schema_config.go) maps a schema version to an index format in `PeriodConfig.TSDBFormat`.
