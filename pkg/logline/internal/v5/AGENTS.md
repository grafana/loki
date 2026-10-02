# pkg/logline/internal/v5/

v5 is **in development**. Do not set `-logline-index.version=v5`.

This package is a fork of `internal/v3` (on-disk format, footer version 4) with
`ngrams.go` copied from `internal/v4`. Later changes belong here, not in v3 or
v4. The extractor is unchanged from v4.

`CurrentVersion` stays `"v3"`. Footer auto-detection cannot tell a v5 file
from a v3 or v4 file and reports `"v3"`. The query path takes the version
from `meta.json`.

## Document shards

v5 builders split each `document_interval` by stream fingerprint into
`document_shards` cells (`logline.DocumentShard`). The file layout is still
v3's: postings hold dense ranks and each document has a 20-byte
`(id, min, max)` record. Cells of one interval in different shards share
`min`/`max`, so readers still see time-only ranges; the shard is not
recoverable from the file yet.

- The footer records the document layout in `ReservedMid`: bytes 0-8 hold the
  interval in nanoseconds and bytes 8-12 the shard count. v3 readers ignore
  those bytes. An interval of 0 means none was recorded. The shard count is
  always at least 1, so an unset count and one shard are the same layout.
- The density cutoff counts cells: `(24h / DocumentInterval) ×
  max(DocumentShards, 1) × DensityThreshold` (`IndexWriteConfig.sentinelCutoff`).
- `Merge` requires every input to have the same layout and writes it to the
  output. A configured interval or shard count that disagrees with the inputs
  is an error. The merge dedupes documents by time bounds, which would
  collapse the cells of one interval, so it rejects indexes with more than one
  document shard.
- Footer-4 v5 files are time-only to readers. When docIDs start carrying the
  shard, readers must refuse footer-4 v5 files or read them as time-only.
  They must not decode docIDs with the shard count in `ReservedMid`.
