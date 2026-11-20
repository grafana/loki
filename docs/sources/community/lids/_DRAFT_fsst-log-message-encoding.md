---
title: "FSST encoding for log message pages"
description: "Use FSST with a shared table per page and offsets for individual strings."
draft: true
---

# FSST encoding for log message pages

**Author:** @dimitarvdimitrov

**Date:** 11/2026

**Sponsor(s):** Marco Pracucci (@pracucci), Ben Clive (@benclive)

**Type:** Feature

**Status:** In discussion

**Related issues/PRs:** TBD

---

## Background

Log message columns currently use plain, length-prefixed strings with Zstd compression.
To decode individual strings the reader decompresses the complete values block.

[Fast Static Symbol Table (FSST)](https://www.vldb.org/pvldb/vol13/p2649-boncz.pdf) uses a trained table of symbols to compress strings.
Each symbol represents up to eight bytes.
Multiple strings can share one table and remain independently decodable.

The optimized benchmarks use [the FSST fork at revision `ee30e95`](https://github.com/dimitarvdimitrov/fsst/commit/ee30e95db62e).
The fork starts from [axiomhq/fsst revision `392aa0d`](https://github.com/axiomhq/fsst/tree/392aa0d5eac7621f855b80df1fd6dc71f204a94f).
It adapts `DecodeBatch` to the existing checked eight-symbol kernel.

## Problem Statement

The current layout couples string access to whole-page decompression.
Zstd decompression takes a noticeable part of the CPU time in the Loki querier.
During one query surge, it accounted for 15% of CPU time (see the [querier CPU profile in the appendix](#querier-cpu-profile)).

We want to explore a layout that provides independent access to compressed strings and reduces the decompression overhead.

## Goals

- Find a cheaper replacement for Zstd for log message columns.
- Remove the varint length prefix from each string.
- Permit selective string decompression in a later reader optimization.
- TODO: format migration & compatibility guarantees

## Non-Goals

- Change compression of other column types.

## Proposal overview

This proposal has a slightly different structure to other LIDs. 
Since we have to make decisions on multiple challenges, those challenges are the main sections of the
LID with sub-proposals for each challenge. 
What follows is an overview of the FSST encoding where we've selected the preferred solution (proposal) for each challenge.

FSST acts as a value encoding.
Message columns use `ENCODING_TYPE_FSST` with `COMPRESSION_TYPE_NONE`.

Each page uses its own FSST table.
The page stores the resulting byte ranges in an offsets array next to the FSST table and the symbol sequences.
Symbol sequences contain codes that reference table entries, plus literal escapes for bytes without a symbol match.

In practice, the page contains:

```text
<presence bitmap size>
<presence bitmap>
<serialized FSST table>
<symbol offsets: uint32[N+1]>
<symbol sequences>

N = number of non-NULL strings
```

Offsets use little-endian integers and refer to byte positions within the symbol sequences.
The first offset is zero.
The final offset equals the total size of the symbol sequences in bytes.

String `i` occupies:

```text
symbolSequences[offsets[i]:offsets[i+1]]
```

The presence bitmap retains its existing page header.

Offsets identify the symbol sequence for each string.
The FSST table contains symbols and their byte lengths, but no string boundaries.

The reader can decode each string independently from its symbol sequence.
This permits future changes to `pageReader` that skip unwanted values without the current read-and-discard path in `skipUnwantedRows`.

## Challenge 1: Choose the FSST table scope

### Proposal A: One table per page (preferred)

Each page carries its own FSST table.
Pages have no shared dictionary dependencies but require repeated table training.

A table per page gives compaction the most flexibility.
Compaction can retain an encoded page without a dependency on the table of its source object or column.
If compaction changes a page, only that page needs a new table and re-encoding.

### Symbol table and small-page overhead

Each page also carries a symbol table, with a fixed 16-byte header plus symbol bytes.
Small-page experiments with a fresh table per page show these costs, in bytes:

| Page values | FSST table | FSST payload | FSST offsets | FSST total | Zstd total |
| --- | ---: | ---: | ---: | ---: | ---: |
| `["1"]` | 17 | 1 | 8 | 26 | 15 |
| Ten strings, `"0"`–`"9"` | 26 | 10 | 44 | 80 | 33 |

FSST totals amplify the original string bytes by 26× and 8×, respectively.
FSST includes `uint32[N+1]` offsets. Zstd includes varint string lengths and its frame checksum.
All three Zstd presets produce the same sizes for these cases.
Both layouts exclude Loki page headers and presence bitmaps. The optional FSST total decoded size adds four bytes.

One-byte strings receive no FSST payload reduction.
At 10,000 cyclic digit rows, offsets keep FSST amplification near 5×, despite the small table cost per row.

### Proposal B: One table per column

All pages in a message column share one FSST table.
Pages depend on that column's table for decompression.

### Proposal C: One table per data object

All message pages in the data object share one FSST table.
If new data changes the table, all existing pages in the object require re-encoding.

## Challenge 2: Choose how to use FSST

### Proposal A: Use a separate encoding with symbol offsets (preferred)

FSST acts as a value encoding with a dedicated page format, as described in the [proposal overview](#proposal-overview).
Symbol offsets preserve direct access to each compressed string.

The dedicated format permits random access decompression and future reader optimizations that skip unwanted values.
The other two proposals require whole-page decompression for individual strings.

FSST can produce larger output than Zstd.
The [preliminary Go benchmark results](#preliminary-go-benchmark-results) show approximately twice as many stored bytes for FSST.

### Proposal B: Compress concatenated strings and their lengths

This approach applies FSST to the existing stream of length-prefixed strings.
It fits the existing compression hooks, retains per-string varints, and requires whole-page decompression.

### Proposal C: Compress concatenated strings with decoded offsets

This approach removes per-string varints and preserves decoded boundaries.
Individual strings still require whole-page decompression.

## Challenge 3: Right-size decompression buffers

The decoder needs the decoded length to allocate a buffer of the correct size before decompression.
The proposals differ in how the decoder obtains that length.
The preferred proposal remains undecided until the comparison of decoded length layouts.

### Proposal A: Do not store decoded lengths

The decoder derives the decoded length from symbols and literal escapes within each range.

TODO: Describe the tradeoffs and add measurements.

### Proposal B: Store decoded lengths beside symbol offsets

Each symbol offset shares an entry with its decoded length for cache locality.

TODO: Describe the layout and add measurements.

### Proposal C: Store decoded lengths in a separate sequence

Decoded lengths occupy a separate sequence alongside the symbol offsets.

TODO: Describe the layout and add measurements.

## Challenge 4: Store symbol offsets

### Proposal A: Store uint32 offsets (preferred)

Fixed-width symbol offsets cost four bytes per boundary (`<symbol offsets: uint32[N+1]>`).
Short strings can therefore require more boundary metadata than varint lengths.
Fixed-width offsets permit direct string access without a prefix scan or a reconstructed offset array.
The log-page measurements show a small space cost and substantially higher throughput for random access.

### Proposal B: Store varint lengths

The `fsst-varint` fixture replaces `uint32[N+1]` offsets with one unsigned varint compressed length per non-NULL string.
Both layouts use identical symbol tables and payloads.
Varint lengths require a prefix scan or a reconstructed offset array before direct access.

### Comparison

All page sizes are in bytes:

| Page values | uint32 boundaries | varint boundaries | uint32 page | varint page |
| --- | ---: | ---: | ---: | ---: |
| `["1"]` | 8 | 1 | 29 | 22 |
| Ten strings, `"0"`–`"9"` | 44 | 10 | 83 | 49 |
| 10,000 cyclic digits | 40,004 | 10,000 | 50,035 | 20,031 |
| 2,048 synthetic log strings | 8,196 | 2,730 | 249,633 | 244,167 |

Page sizes include the header and presence bitmap, but exclude descriptor metadata and the optional decoded size.
Varints save 5,466 bytes (2.19%) on the log page, versus 41% on the ten-digit page.
They require a prefix scan or a reconstructed offset array before direct access.
The existing `DecodeBatch` accepts that reconstructed array.
Full results reside in `fsst-go/results/loki-pages-varint.json`.

Warm decode throughput for the 2,048-row log page, in decoded MB/s:

| Decode mode | uint32 offsets | varint lengths |
| --- | ---: | ---: |
| Random single value | 4,447 | 183 |
| Whole page | 4,961 | 4,863 |

The timer includes boundary parsing and FSST decode, but excludes table construction, bitmap processing, CRC32C, and I/O.
Full results reside in `fsst-go/results/loki-boundary-decode-summary.json`.

## Other Notes

### Benchmark results

The page comparison from 2026-11-05 uses the optimized FSST fork at revision `ee30e95` and `github.com/klauspost/compress v1.20.1`.
All six configurations run in the same executable and process, with an independent repeat.
The host uses an Apple M1 Pro, Go 1.27.1, and one benchmark worker.

The results below use 2,048 synthetic service-log strings, each 40–1,023 bytes long.
FSST trains its table on 512 separate sample strings.
Each case contains seven samples per operation, with at least 200 ms per sample.
Rates are medians in decimal MB/s, calculated from uncompressed bytes.

The independent repeat differs by at most 12.61% in median throughput across all page comparisons.
That change concerns best-compression URL encode.
All decode medians differ by less than 1.75%.

| Codec and layout | Encode (MB/s) | Decode (MB/s) | Stored ratio |
| --- | ---: | ---: | ---: |
| FSST fork, independent strings, optimized batch decode | 467.4 | 5,112.5 | 3.33× |
| Zstd `SpeedFastest`, blocks up to 1 MiB | 368.6 | 1,363.9 | 5.87× |
| Zstd `SpeedDefault`, blocks up to 1 MiB | 327.0 | 1,489.7 | 5.76× |
| Zstd `SpeedBestCompression`, blocks up to 1 MiB | 72.8 | 1,844.5 | 6.59× |

A larger stored ratio means less stored data.
FSST uses approximately 1.76× as many stored bytes as the Zstd `SpeedFastest` case on this corpus.
Its measured decode throughput is approximately 3.75× higher.
The grouped Zstd cases approximate the current whole-page compression model.
Loki page framing and reader overhead remain unmeasured.

Stored ratios include serialized tables and boundary metadata.
The measured FSST layout also includes a four-byte total decoded size for exact buffer allocation.

The measurements exclude table training and construction.
Zstd checksums remain enabled, while the FSST benchmark adds no checksum.
Optimized throughput measurements cover ARM64 only.

The Zstd target is 1 MiB before compression, which matches Loki's default target for log-message pages.
Each corpus fits in one block without padding.
The log block contains 819,418 original bytes plus 4,096 bytes of row framing.

### Integration

`tableBuffer.Message` selects the new encoding.
A dedicated FSST encoder and decoder retain the existing plain encoding for other columns.

The encoder buffers strings until the page flush.
It trains one table, encodes the strings, and writes the table, offsets, and symbol sequences.
The buffer must own the string bytes until the flush.

`compressWriter.Reset` uses its existing pass-through path.
`MemPage.open` retains the FSST payload in encoded form.
The value decoder owns FSST decompression.

The reader initially decodes requested sequential batches.
Offsets also permit direct access to individual compressed strings.
Predicate-aware access remains future work.
The current reader still decodes skipped rows, so offsets alone do not provide that optimization.

The page descriptor contains `ValuesCount`, which identifies the number of offsets.
The value decoder constructor currently accepts only bytes.
The integration needs count propagation or a count within the payload.
This detail remains open.

Page size estimates must include buffered strings before the encoder flush.
Uncompressed size statistics must describe decoded data.
CRC32 and stored size statistics must include the table and offsets.

### Protobuf changes

The prototype adds `ENCODING_TYPE_FSST` to `EncodingType`.
The dedicated Makefile target regenerates `datasetmd.pb.go`:

```bash
make pkg/dataobj/internal/metadata/datasetmd/datasetmd.pb.go
```

The broad `make protos` target deletes all generated protobuf files before regeneration.
The dedicated target limits regeneration to the affected file.

No FSST compression enum is necessary for this approach.
The table and offsets reside within the page payload.
They need no additional protobuf fields.

### String boundaries and decoded lengths illustration

This example illustrates the layout from the [proposal overview](#proposal-overview).

```text
FSST table:
    A → "hello"
    B → "world"

Input rows:          ["hello", "", NULL, "world"]
Presence bitmap:     [1, 1, 0, 1]
Symbol offsets:      [0, 1, 1, 2]
Symbol sequences:    [A, B]
```

Equal adjacent offsets represent an empty string.
NULL rows have no entry in the values stream.

After decompression, the non-NULL values have this representation:

```text
Decoded offsets:  [0, 5, 5, 10]
Decoded bytes:    "helloworld"
```

This representation matches the offsets and bytes that `columnar.UTF8` uses.

The library provides `EncodeBatch` and `DecodeBatch`.
These APIs produce symbol offsets and reconstruct decoded offsets.
The integration must serialize the symbol offsets itself.

### Querier CPU profile

The profile shows CPU use in the Loki querier during the query surge.

![Loki querier CPU profile during a query surge](fsst-encoding-profile.png)
