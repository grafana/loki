# pkg/logline/

Version-dispatch shim for logline index formats plus shared helpers.

## Import rule

**Never import `pkg/logline/internal/v3` (or future `vN`) outside this package.**
All external callers must go through the shim (`OpenFile`, `OpenReaderAt`, `NewWriter`,
`NewMerger`, `ValidateVersion`, `ExtractorForVersion`). If the shim doesn't expose what
you need, extend it.

The versioned packages live under `internal/` so the compiler enforces this rather than
review.

## Versions

| Version | Status | On-disk format | Extractor |
| --- | --- | --- | --- |
| v3 | Current (`CurrentVersion`) | footer 4 | v3 |
| v4 | Supported | v3 bytes | packed numeric terms |
| v5 | In development. Do not set `-logline-index.version=v5` | footer 5; postings hold cells of the index's day and there is no document table | v4's extractor, forked into `internal/v5` |

v5 will change. It is registered so the fork can be tested, and `CurrentVersion` stays `"v3"` so a binary upgrade does not write it.

A v5 document ID is a cell of the index's day: `bucket << document_shard_bits | document shard`. The footer records the document interval, the shard bits, and the day start, and `DocumentLayout(reader)` maps a cell to its bucket's time range. `Reader.Documents()` is nil for v5, so code that maps document IDs to time must check `VersionHasDocumentTable` or `DocumentLayout` first. v5 indexes written before day-relative cells have footer 4: the v5 reader refuses them, and `ValidateIndexHeader` reports them from `meta.json` without opening the file.

## Document shards

`IndexConfig.DocumentShardBits` (`-logline-index.document-shard-bits`) splits
each `document_interval` into 2^bits shards by stream fingerprint, like
dataobj's `streams.ShardBits`. The flag defaults to `DefaultDocumentShardBits`
(5, so 32 shards). For v5, `Validate` accepts 0 to `MaxDocumentShardBits` (7);
0 is one shard. v3 and v4 ignore it: `Validate` resets it to 0. Configuring
bits rather than a shard count means every value is a valid power of two.

`document_shard.go` is the one definition of a stream's document shard:
`StreamFingerprint` computes the stream's ingester fingerprint, and
`DocumentShard(fp, shardBits)` takes its top `shardBits` bits, the same prefix
`index.ShardAnnotation.Match` uses with `1 << shardBits` shards. Anything that computes or matches a
document shard must call both. `pkg/ingester` has a test that pins
`instance.getHashForLabels` to `StreamFingerprint`.

## Extraction is coupled to index version

The n-gram extraction algorithm is part of the index format contract. Each
`pkg/logline/internal/vN` package owns a frozen `ExtractFeatures` implementation paired
with its on-disk format: a v3 index is queried with v3's extractor. There is no separate
"extraction version" axis on the meta.

The shim's `ExtractorForVersion(version)` returns the matching package-level function.
Callers (the builder at construction, the hint provider per-block) must look up the
function once and reuse it.

`FormatTerm` is the other half of that contract and is owned by the same `vN` package:
it turns an extracted `[8]byte` key into the term string to look up. A key is 8 bytes
wide but how many of them carry the term is version-specific. `ngramsByVersion` registers both functions in one entry so a new version
cannot ship an extractor without a formatter.

### Frozen-by-design

`internal/v3/ngrams.go` contains a complete copy of the algorithm and its lookup tables,
with a golden test alongside it. The duplication across future versions is intentional:

- A v3 index already on disk was built with v3's tables. Modifying the live extractor
  changes query semantics for data we cannot rewrite without a new version.
- Adding a future version means adding `pkg/logline/internal/vN/ngrams.go` (typically
  forked from the previous version), a golden test, a case in `ValidateVersion`, and an
  `ngramsByVersion` entry with its `ExtractFeatures` and `FormatTerm`.

If a golden test in `internal/vN/ngrams_test.go` ever fails, you have accidentally changed
a frozen algorithm: revert. Do not update the expected values.

## Other helpers

- `version.go` — Reader/Writer/Merger interfaces, `CurrentVersion`, `AllVersions`,
  `ValidateVersion`, `ValidateIndexHeader`, `DocumentLayout`, factory functions
- `ngrams.go` — `ExtractorForVersion` and `FormatterForVersion` shims that return the
  matching `vN` extractor and term formatter
- `index_config.go` — `IndexConfig`, the shared `logline.index` section (see below)
- `document_shard.go` — `StreamFingerprint`, `DocumentShard`, `VersionHasDocumentShards`,
  and `VersionHasDocumentTable`
- `compaction_config.go` — `CompactionConfig`, the `logline.compaction` section
- `retention_config.go` — `RetentionConfig`, the `logline.retention` section

## Compaction requires one version per job

The scheduler groups sources by resolved `Version` (and shard settings, document
interval, and document shard bits), and the compactor verifies it again before merging.
Mixed-version merges would require re-indexing under one canonical version. The scheduler
skips indexes whose `meta.json` index_header fails `ValidateIndexHeader` (v5 indexes with
footer 4), because their jobs would fail on every pass. The compactor passes the sources'
date as `WriterConfig.DayStart`, and the v5 merge refuses inputs whose footer covers
another day.
