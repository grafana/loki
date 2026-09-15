# pkg/logline/

Version-dispatch shim for logline index formats plus shared helpers.

## Import rule

**Never import `pkg/logline/internal/v3` (or future `vN`) outside this package.**
All external callers must go through the shim (`OpenFile`, `OpenReaderAt`, `NewWriter`,
`NewMerger`, `ValidateVersion`, `ExtractorForVersion`). If the shim doesn't expose what
you need, extend it.

The versioned packages live under `internal/` so the compiler enforces this rather than
review.

## Extraction is coupled to index version

The n-gram extraction algorithm is part of the index format contract. Each
`pkg/logline/internal/vN` package owns a frozen `ExtractFeatures` implementation paired
with its on-disk format: a v3 index is queried with v3's extractor. There is no separate
"extraction version" axis on the meta.

The shim's `ExtractorForVersion(version)` returns the matching package-level function.
Callers (the builder at construction, the hint provider per-block) must look up the
function once and reuse it.

### Frozen-by-design

`internal/v3/ngrams.go` contains a complete copy of the algorithm and its lookup tables,
with a golden test alongside it. The duplication across future versions is intentional:

- A v3 index already on disk was built with v3's tables. Modifying the live extractor
  changes query semantics for data we cannot rewrite without a new version.
- Adding a future v4 means adding `pkg/logline/internal/v4/ngrams.go` (typically forked
  from v3), a v4 golden test, and a case in `ValidateVersion` and `ExtractorForVersion`.

If a golden test in `internal/vN/ngrams_test.go` ever fails, you have accidentally changed
a frozen algorithm: revert. Do not update the expected values.

## Other helpers

- `version.go` — Reader/Writer/Merger interfaces, `CurrentVersion`, `AllVersions`,
  `ValidateVersion`, factory functions
- `ngrams.go` — `ExtractorForVersion` shim that returns the matching `vN` extractor

## Compaction open question

When the worker implements real merge logic, blocks with different `Version` values will
need handling. Recommendation, consistent with `ShardAlgorithm`: require all source blocks
to share the same index version before compacting. Mixed-version merges would require
re-indexing under one canonical version. No code change needed now.
