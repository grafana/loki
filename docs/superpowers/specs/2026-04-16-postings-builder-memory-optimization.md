# Postings Builder Memory Optimization

**Status:** Future work — identified during on-disk serialization implementation

**Date:** 2026-04-16

## Problem

The postings builder uses a struct-append/accumulator pattern: the calculation pipeline (`label_postings_calculation.go`, `bloom_postings_calculation.go`) fully constructs each `Posting` struct — including heap-allocated `[]byte` fields for bloom filters and stream ID bitmaps — and passes it to the builder via `b.Append(Posting{...})`. The builder accumulates all rows in a `[]Posting` slice until flush.


This means both the calculator and the builder hold postings data in memory simultaneously:

1. **Calculator** — holds state keyed by posting key (label value, column name, etc.) to aggregate stream IDs into bitmaps and build bloom filters
2. **Builder** — holds the fully-formed `[]Posting` slice waiting to be flushed

The bloom filters and bitmaps are the dominant memory cost. Mid-accumulation flushing (when `EstimatedSize() > TargetSectionSize`) bounds the builder's slice, but the calculator's state persists across flushes since it's still aggregating.

Question: What does is mean that the calculator holds aggregates across flushes?
I don't think it shoudl do that, and the seems like a really bad potential for a
memory leak.

By contrast, the streams builder uses an observation/aggregation pattern: callers pass raw events (`Record(labels, timestamp, size)`) and the builder aggregates in-place (deduplicating streams, merging timestamp ranges, summing sizes). Memory is proportional to unique streams, and there's no separate "calculator holding state + builder holding rows" duplication.

## Proposed Approach: Streaming Postings to the Builder

Instead of the calculator building full `Posting` structs and appending them, the postings builder could accept raw observations and aggregate internally — matching the streams/pointers pattern:

```go
// Instead of:
builder.Append(postings.Posting{
    Kind: postings.KindLabel,
    ObjectPath: objectPath,
    SectionIndex: sectionIdx,
    ColumnName: columnName,
    LabelValue: labelValue,
    BloomFilter: nil,
    StreamIDBitmap: bitmap,
    ...
})

// Something like:
builder.ObserveLabelPosting(objectPath, sectionIdx, columnName, labelValue, streamID, uncompressedSize, minTs, maxTs)
```

The builder would:
- Maintain a map keyed by `(kind, objectPath, sectionIndex, columnName, labelValue)` (the posting key)
- Accumulate stream IDs into a bitmap incrementally
- Track min/max timestamps and uncompressed size as running aggregates
- Build bloom filters during flush (or incrementally)
- Encode directly into column builders during flush, without materializing `[]Posting`

### Benefits

- **Eliminates double-storage** — calculator no longer needs to hold postings state by posting key; the builder owns that aggregation
- **Lower peak memory** — no intermediate `[]Posting` slice; the builder can encode columns incrementally
- **Matches established pattern** — consistent with how streams/pointers builders work
- **Incremental encoding opportunity** — the builder could append to `dataset.ColumnBuilder`s during `Observe*` calls rather than batching everything in `Flush`, further reducing memory

### Trade-offs

- **Builder becomes more complex** — it takes on aggregation responsibility (bitmap building, bloom filter construction) that currently lives in the calculation pipeline
- **Tighter coupling** — the builder would need to understand bloom filter construction, bitmap operations, etc.
- **SectionEncoder abstraction changes** — the pluggable `SectionEncoder func(rows []Stat, enc *columnar.Encoder) error` wouldn't work as-is if rows aren't materialized. The encoding logic would need to be integrated into the builder or the abstraction would need to change.
- **Mid-accumulation flush semantics change** — currently, flushing writes all buffered rows as one section. With incremental encoding, the builder would need to track what's been encoded vs. what's pending.

### Open Questions

- Should bloom filter construction move into the builder, or should the calculator still build blooms and pass them via the observation API?
- Can we incrementally encode into column builders during `Observe*` calls, or does the sort requirement (rows must be sorted before encoding) force a batch step?
- How does this interact with the future dataset/vortex encoding format swap?
