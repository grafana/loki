# pkg/logline/internal/v4/

v4 n-gram extraction. Registered with the `pkg/logline` shim as version `"v4"`.

## Current state: an exact clone of v3

v4 is byte-for-byte identical to v3 today, asserted by
`TestExtractFeatures_IdenticalToV3`. The fork exists so extraction can diverge
without invalidating v3 indexes already on disk.

## Why extraction changes need a new version

Extraction is part of the index-version contract. A v3 index was built with the v3
extractor, so it must always be queried with the v3 extractor: if build-time and query-time
n-grams disagree, a term lookup misses and the block is wrongly skipped, which is a silent
false negative. Editing v3 in place would therefore corrupt every v3 index already written.

The reader picks the extractor from `meta.json`'s version
(`pkg/hintprovider`, `ExtractorForVersion`), so v3 and v4 indexes can coexist in a cell and
each is read with the algorithm that wrote it.

## On-disk format: shared with v3

v4 defines no file format. The `.lidx` layout, footer, term dictionary and postings are
v3's, so the shim dispatches all format operations (`OpenReader`, `OpenReaderCached`,
`NewWriter`, `NewMerger`) for `"v4"` to the v3 implementation, and only
`ExtractorForVersion` returns something different.

Consequence: footer auto-detection (`OpenFile`, `OpenReaderAt`) cannot tell a v4 file from a
v3 file and reports `"v3"` for both. That is safe because only format-only tooling
auto-detects (dump, convert, identity); the query path takes the version from `meta.json`.

## Invariants

1. **Frozen once v4 indexes exist.** Do not change the emitted n-gram set. Add a v5 instead.
2. **Bytes 6-7 of each key stay zero.** `radixSortByNgram` orders only bytes 0-5 and assumes
   the rest are zero; violating this mis-sorts runs and wedges the writer's ascending-term
   check.
3. `CurrentVersion` stays `v3`. v4 is opt-in per deployment via `index_version: v4`.
