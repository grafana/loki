# pkg/logline/internal/v5/

v5 is **in development**. Do not set `-logline-index.version=v5`.

This package is a fork of `internal/v3` with `ngrams.go` copied from
`internal/v4`. Later changes belong here, not in v3 or v4. The extractor is
unchanged from v4. The format rules in `internal/v3/AGENTS.md`, such as the
postings encodings and their frozen layouts, apply here too.

`CurrentVersion` stays `"v3"`. Footer auto-detection (`logline.OpenReaderAt`,
`logline.OpenFile`) reports footer 5 as `"v5"`. The query path takes the
version from `meta.json`.

## Day-relative cells

The footer version is 5 (`IndexVersion`), matching the version name. v3 and
v4 files are footer 4.

- One v5 index covers one UTC day. Its document IDs are cells of that day:
  `cell = bucket << shardBits | documentShard`, where `bucket` is the
  `document_interval` bucket since the day start and `documentShard` is
  `logline.DocumentShard` of the stream's fingerprint. Cells of one bucket in
  different document shards share the bucket's time range.
- There is no document table: `DocMetadataSize` is 0 and `Documents()` returns
  nil. `DocumentLayout()` (`format.DocumentLayout`) maps a cell to its time
  range. Code that needs document bounds must use it.
- The footer records the layout in `ReservedMid`. Bytes 0-8 hold the interval
  in nanoseconds, byte 8 the shard bits, and bytes 16-24 the day start in Unix
  nanoseconds. Bytes 12-16 are reserved for the n-gram length. The reader
  validates the layout: the interval divides 24h, every cell of the day fits a
  uint32, and the day start is a UTC midnight.
- `DocumentCount` is the number of distinct cells the postings reference. It
  is informational; nothing sizes a table from it.
- `NewWriter` refuses documents and requires `DocumentInterval` and `DayStart`.
  It refuses postings at or past `CellsPerDay()`.
- The density cutoff counts cells: `(24h / DocumentInterval) ×
  2^DocumentShardBits × DensityThreshold` (`IndexWriteConfig.sentinelCutoff`).

## Reading

`OpenIndexAtWithHeader` takes `meta.json`'s index_header, which does not record
the layout. It reads the metadata region and the 256-byte footer in one request
(the region ends where the footer starts), and the footer must match the
index_header exactly. A mismatch is an error, not a fallback.

## Merge

`Merge` requires every input to have the same interval, shard bits, and day
start, and writes them to the output. Equal document IDs are the same cell, so
the merge unions postings without remapping. A configured interval, shard
bits, or day start that disagree with the inputs is an error; the compactor
passes the sources' date as the day start.

## Footer-4 v5 files

v5 files written before day-relative cells have footer 4, with dense ranks and
a document table. This package refuses them (`unsupported index version: 4`)
from every entry point, including the merge. `logline.ValidateIndexHeader`
reports them from `meta.json`, and the compaction scheduler skips them.
