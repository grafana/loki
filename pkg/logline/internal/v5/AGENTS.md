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
2^`document_shard_bits` cells (`logline.DocumentShard`). The file layout is still
v3's: postings hold dense ranks and each document has a 20-byte
`(id, min, max)` record. Cells of one interval in different shards share
`min`/`max`, so readers still see time-only ranges; the shard is not
recoverable from the file yet.

- The footer records the document layout in `ReservedMid`: bytes 0-8 hold the
  interval in nanoseconds and byte 8 the shard bits. v3 readers ignore those
  bytes. An interval of 0 means none was recorded. 0 shard bits is one shard.
- The density cutoff counts cells: `(24h / DocumentInterval) ×
  2^DocumentShardBits × DensityThreshold` (`IndexWriteConfig.sentinelCutoff`).
- `Merge` requires every input to have the same layout and writes it to the
  output. A configured interval or shard bits that disagree with the inputs
  are an error. The merge dedupes documents by time bounds, which would
  collapse the cells of one interval, so it rejects indexes with any document
  shard bits.
- Footer-4 v5 files are time-only to readers. When docIDs start carrying the
  shard, readers must refuse footer-4 v5 files or read them as time-only.
  They must not decode docIDs with the shard bits in `ReservedMid`.
