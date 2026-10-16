# pkg/logline/internal/v3/

Implementation of the v3 index format (`.lidx` files). Registered with the `pkg/logline`
shim as version `"v3"`.

Internal by design: everything outside `pkg/logline/` must go through the shim. See
`pkg/logline/AGENTS.md`.

## Format

Binary `.lidx` files: no header prefix, data starts at offset 0. 256-byte footer at EOF
(magic `LOGL`, version 4).

Layout:
```
[Postings data: variable]      ← offset 0 (no header prefix)
[Term dictionary: variable]
[Document metadata: variable]
[Term block directory: variable]
[Postings block directory: variable]
[Footer: 256B]                 ← offset: totalSize - 256
```

On-disk `IndexVersion = 4`, while the shim registers the package as `"v3"`. The two
numbers are independent and both are frozen.

## What v3 indexes

N-grams from both the log line and structured metadata values. The on-disk format is
unchanged from the previous version; the only difference is what data is fed to the
n-gram extraction pipeline at build time.

## Key types

`IndexReader`, `StreamingIndexWriter`, `IndexWriter`, `IndexHeader`, `IndexWriteConfig`,
`CachedReaderState`.

## Postings encodings

Each file records its postings encoding (low nibble of the header flags) and its block
compression (`PostingsCompression`) independently. Readers choose the decoder per file,
so compaction can mix sources of different encodings.

Every compression mode is checksummed, so a corrupted block fails to load instead of
decoding to wrong postings:

- `1`: zstd frames. zstd's own checksum covers them.
- `2`: uncompressed. Each block is followed by the little-endian xxh3-64 of its bytes.
- `0` and anything else is rejected.

| Encoding | Value | Blocks |
|---|---|---|
| `PostingsEncodingFastDeltaVarIntBlocked` | 4 | zstd (default) |
| `PostingsEncodingFastEliasFanoBlocked` | 5 | uncompressed + xxh3, payload layout in `elias_fano.go` |

- **Readers first.** Every reader must be deployed with support for an encoding before
  any production writer emits it, and must keep that support while such indexes are
  within retention. A writer only emits a non-default encoding when its caller sets
  `format.WriterConfig.Encoding`.
- **Frozen layouts.** `TestEliasFanoGolden` pins the Elias-Fano bytes and
  `TestUncompressedBlockFraming` pins the checksummed block framing. If either fails, revert.
- **bitpack contract.** Elias-Fano packs unmasked docIDs into reused buffers and relies
  on `parquet-go/bitpack` masking, overwriting and staying in bounds. `TestBitpackContract`
  pins this; if it fails after a bitpack upgrade, do not take the upgrade.

## Performance rules

Zero-alloc on the hot paths, buffer reuse, radix sort, roaring pool. Benchmark any
encoding, density filter, or merge change.

### Merge must not round-trip through roaring

`mergeIndexes` remaps decoded docID slices, sorts and uniques them (remap is not
order-preserving), then two-pointer merge-unions into `WriteTermDocIDs`. Do not rebuild
roaring bitmaps on the merge path: use `GetDocIDs` + `DocIDs()`, not `GetBitmap` /
`WriteTermBitmap`. Query and tooling paths may still materialize roaring via `Bitmap()`
lazily.

## Frozen algorithm

`ngrams.go` and its lookup tables are frozen. `TestExtractFeatures_GoldenOutputs` pins the
byte-level output. If it fails you have changed a frozen algorithm: revert rather than
updating the expected values.
