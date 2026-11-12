---
title: "Draft: FSST encoding for log message pages"
description: "Explore FSST with a shared table per page and offsets for individual strings."
draft: true
---

# Draft: FSST encoding for log message pages

**Author:** @dimitarvdimitrov

**Date:** 10/2026

**Sponsor(s):** TBD

**Type:** Feature

**Status:** Draft

**Related issues/PRs:** TBD

---

## Background

Log message columns currently use plain, length-prefixed strings with Zstd compression.
To decode individual strings the reader decompresses the complete values block.

FSST uses a trained table of symbols to compress strings.
Each symbol represents up to eight bytes.
Multiple strings can share one table and remain independently decodable.

The optimized benchmarks use [the FSST fork at revision `ee30e95`](https://github.com/dimitarvdimitrov/fsst/commit/ee30e95db62e).
The fork starts from [axiomhq/fsst revision `392aa0d`](https://github.com/axiomhq/fsst/tree/392aa0d5eac7621f855b80df1fd6dc71f204a94f).
It adapts `DecodeBatch` to the existing checked eight-symbol kernel.

## Problem Statement

The current layout couples string access to whole-page decompression.
We want to explore a layout that removes per-string varints and preserves independent access to compressed strings.

## Goals

- Find a cheaper replacement for Zstd for log message columns.
- Remove the varint length prefix from each string.
- Permit selective string decompression in a later reader optimization.
- TODO: format migration & compatibility guarantees

## Non-Goals

Other columns retain their current encodings and compression.

## Proposals

### Proposal 0: Retain plain strings with Zstd

This approach preserves the current implementation.
It retains per-string varints and requires whole-page decompression.

### Proposal 1: Shared table with symbol offsets

FSST acts as a value encoding.
Message columns use `ENCODING_TYPE_FSST` with `COMPRESSION_TYPE_NONE`.

Each page contains one trained table.
The encoder compresses each non-NULL string independently with that table.
The page stores the resulting byte ranges in an offsets array next to the FSST table and the symbol sequences.
Symbol sequences contain codes that reference table entries, plus literal escapes for bytes without a symbol match.
The space savings come from codes that replace longer byte sequences in the table.

Conceptually, the page contains:

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
There are no per-string varints.

#### String boundaries and decoded lengths

The FSST table does not contain string boundaries.
It contains symbols and their byte lengths.
Offsets identify the symbol sequence for each string.

This is the equivalent of the string lengths when strings are encoded with `ENCODING_TYPE_PLAIN` and `COMPRESSION_TYPE_ZSTD`.
The decoder derives the decoded length from symbols and literal escapes within each range.

// TODO dimitarvdimitrov figure out how we can integrate the allocator into this design - iirc right now we may not need to worry about this, but to keep random access

Doing random reads without decoding the whole page would allow the `pageReader` to do proper skipping of values in a column, as opposed to skipping via reading and discarding (`skipUnwantedRows`).

#### Accepted tradeoffs

##### Offset overhead

Fixed-width symbol offsets cost four bytes per boundary (`<symbol offsets: uint32[N+1]>`).
Short strings can therefore require more boundary metadata than varint lengths.
Fixed-width offsets permit direct string access without a prefix scan.

The `fsst-varint` fixture replaces `uint32[N+1]` offsets with one unsigned varint compressed length per non-NULL string.
Both layouts use identical symbol tables and payloads.
All page sizes are in bytes:

| Page values | uint32 boundaries | Varint boundaries | uint32 page | Varint page |
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

| Decode mode | uint32 offsets | Varint lengths |
| --- | ---: | ---: |
| Random single value | 4,447 | 183 |
| Whole page | 4,961 | 4,863 |

The timer includes boundary parsing and FSST decode, but excludes table construction, bitmap processing, CRC32C, and I/O.
Full results reside in `fsst-go/results/loki-boundary-decode-summary.json`.

##### Symbol table and small-page overhead

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
Full results and reproduction commands reside in `fsst-go/RESEARCH.md`, under “Small-page storage experiment (2026-11-12)”.

##### Training cost per page

Independent pages avoid shared dictionary dependencies but require repeated table training.

##### Compression ratio

FSST can produce larger output than Zstd (~2x larger compressed blobs according to [Preliminary Go benchmark results
](#preliminary-go-benchmark-results))

### Proposal 2: Apply FSST to the existing length-prefixed stream

This approach fits the existing compression hooks.
It retains per-string varints and requires whole-page decompression.
This is less desired because it closes the door to random access decompression.

### Proposal 3: Compress concatenated strings with decoded offsets

This approach removes per-string varints and preserves decoded boundaries.
Individual strings still require whole-page decompression.
We prefer symbol offsets because they preserve random access decompression.

## Other Notes

### Preliminary Go benchmark results

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

### Validation and open questions

Validation covers:

- Exact round trips for empty strings, Unicode, and arbitrary bytes.
- NULL values and pages with no non-NULL values.
- Multiple pages and reused builders.
- Ordered appends and incremental merges.
- Invalid offsets, truncated tables, and corrupt compressed streams.
- Compression ratio, encode throughput, decode throughput, and allocations.
- Table overhead and repeated table training costs.
- Accuracy of existing object size estimates.

Further work must resolve:

- How the decoder receives the number of strings.
- How decoded buffers integrate with the existing memory allocator.
- Whether fixed-width offsets need a more compact representation.
- Where the page stores its total decoded size for exact page allocation.

### String boundaries and decoded lengths illustration

This is an illustration of how strings are stored
from [String boundaries and decoded lenghts](#string-boundaries-and-decoded-lengths)

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
