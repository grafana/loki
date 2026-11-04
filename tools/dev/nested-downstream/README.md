# Nested downstream ingestion POC

Baseline: `0d4599aecc` (main when this branch was created).
Reference reviewed: `origin/defer-otlp-sm-expansion-poc` at `bbbc6aa0d0`.

This POC carries the existing internal nested streams through Kafka consumers and
internal ingestion RPCs. It does not change Kafka record encoding, stored chunk or
index formats, WAL encoding, or query responses.

## Scope

| Path | Where attributes are resolved |
| --- | --- |
| Partition ingester | Entry references retain resource/scope attributes through validation. Chunk append resolves overrides while interning shared symbols once per group and symbol table. |
| Direct distributor → ingester | The existing deferred-expansion setting selects the new `InternalPusher.PushInternal` RPC. `Unimplemented` alone falls back to the legacy flat RPC. |
| Logline index builder | Groups reach the extraction worker directly. A reusable scratch slice resolves metadata immediately before the existing versioned extractor. No extraction algorithm changes. |
| Pattern ingester | Reads only the effective `detected_level` across entry, scope and resource attributes. Both Kafka and tee RPC paths retain groups. |
| Pattern tee | Buffers detached group/entry slices and uses the existing pattern `PushInternal` RPC. Buffer bytes count the nested serialized stream size. |
| Data-object builder | Preserves groups through tenant/window routing, resolving metadata when creating column records. Entries with only shared attributes reuse an immutable label set. |
| WAL and live tail | Expands accepted entries at these boundaries. WAL replay continues using its existing codec and flat records. |

The prototype deliberately differs from the earlier POC:

- Uses the merged internal nested protocol, rather than adding metadata-set IDs to public entries.
- Keeps the existing on-disk formats, including the WAL. No storage migration is required.
- Uses request-scoped symbol caches keyed by the actual symbol table, rather than an unbounded hash-keyed cache on each chunk. A chunk reorder cannot leave cached symbol IDs pointing at the old table.
- Preserves phase-1 precedence: entry > scope > resource, including empty values.
- Preserves downstream expanded rate-limit/discard accounting and chunk-cut sizing. `detected_level` remains excluded from accounted bytes but included in chunk sizing. Distributor accounting is unchanged.

## Local validation

Use Go 1.27.3 or the version required by the branch's `go.mod`. Set `TZ=Etc/UTC`; existing pattern persistence tests assume an integral-hour
local offset, and Arrow tests distinguish the named local zone from `time.UTC`.

```sh
TZ=Etc/UTC go test -p 4 ./pkg/...
TZ=Etc/UTC go test -race -p 2 ./pkg/kafka ./pkg/logproto ./pkg/chunkenc ./pkg/ingester ./pkg/logline/builder ./pkg/pattern ./pkg/dataobj/builder ./pkg/dataobj/logsobj
GOMAXPROCS=1 TZ=Etc/UTC go test ./pkg/kafka ./pkg/chunkenc -run '^$' -bench 'Benchmark(DecodeNestedConsumer|AppendSharedMetadata)$' -benchmem -benchtime=1s -count=5
```

Added coverage includes mixed flat/nested Kafka decoding and ownership across
subsequent decodes; chunk bytes, deduplication and symbol-table replacement;
partial and whole-stream rate limits, old timestamps and discards; actual WAL
encoding/replay and tail delivery; v3/v4/v5 index equivalence with serial/parallel
extraction; pattern level precedence; tee ownership/caps; data-object readback and
window splitting; and direct-RPC rollout fallback.

The focused benchmarks isolate decoding and 64-entry chunk appends. They do not
predict cell-wide savings. Chunk append deliberately isolates shared-only
metadata; include entry-level metadata and native traffic in the cell test.

## Dev-cell comparison

1. Build baseline and POC images from the same baseline source. Deploy the POC to
   distributors, (partition) ingesters, logline index builders, pattern ingesters
   and data-object builders present in the chosen cell. Deploy consumers before
   distributors. Keep the versions of other services fixed.
2. Keep `distributor.otlp.defer-attribute-expansion=true` in **both** arms. The
   comparison measures downstream handling, not phase-1 wire-size savings. Also
   smoke-test `false` to verify legacy Kafka input and direct RPC compatibility.
3. Keep rate/time sharding, consumer/extraction concurrency, limits, partition
   counts and replicas identical. Do not turn off normal sharding or artificially
   suppress consumer load. Record any autoscaling, rollouts or unrelated traffic.
4. Start with correctness: native and OTLP batches; no/shared/per-entry metadata;
   multiple resources/scopes/streams; conflicting keys and empty overrides; old,
   out-of-order and duplicate timestamps; batches crossing shard/chunk/window
   boundaries; partial rejection; and WAL restart/query/tail if enabled. Compare
   returned entries and metadata against the input after the documented precedence
   and filtering rules, not just entry counts. Exercise the pattern and data-object
   paths if the cell enables them.
5. Run matched sustained native-heavy, OTLP shared-heavy, and mixed traffic. A
   moderate starting rate is 2 MiB/s of line bytes. Keep batch sizes, metadata,
   concurrency and compression fixed. Use the same steady duration in both arms
   after warm-up; capture the drain/flush period separately.
6. Compare CPU seconds and allocated bytes per **consumed entry** and per expanded
   logical byte. Also compare working set/live heap, GC, throughput, consumer lag,
   push latency, errors/discards, chunk sizes/counts, index/object sizes and flush
   time. Ingestion rate alone is an invalid denominator if consumers are catching
   up. Separate native and OTLP profiles where possible.

## Interpretation and limits

- Kafka decode results now own their slices. This avoids asynchronous entry copies
  for nested records. The serial index builder no longer reuses its flat entry
  array for **legacy flat records**; include this transitional allocation cost in
  the flag-off smoke/benchmark results. New native records are already nested when
  the deferred-expansion flag is on.
- No need to wait 24 hours solely for this code rollout: the Kafka format is
  unchanged from phase 1. If the distributor flag was only recently enabled,
  distinguish legacy flat backlog using rollout timing and consumer lag; retention
  is an upper bound, not the normal transition time.
- WAL-enabled ingestion still pays expansion for accepted entries. Measure that
  separately from Kafka cells with the WAL disabled. WAL deduplication/checkpoint
  semantics are unchanged.
- The tee cap remains a serialized-byte estimate, not a heap limit. Its numerical
  interpretation changes to nested bytes; native wrappers add a little wire size,
  while shared metadata is counted once. Every queued stream detaches wrappers and
  entries so sent/dropped sibling streams cannot stay alive through shared arrays.
- The new direct RPC extends `ingester.Interface`. Any out-of-repository wrapper
  implementing that interface must also expose it and preserve any custom push
  hooks. Build the final deployment image against this branch before deployment.
- Index extraction still processes effective metadata per entry; reusable scratch
  avoids per-entry expanded allocations but does not cache shared n-grams. This
  keeps the POC independent of index-version semantics.
- No Kubernetes or deployment configuration was changed by this branch.

## Verification recorded on 2026-11-04

- Full `./pkg/...` suite passed (233 packages with tests), using `TZ=Etc/UTC`.
- All eight packages in the race command above passed.
- `go build -o /tmp/loki-nested-downstream-poc ./cmd/loki` passed.
- Focused benchmarks ran with Go 1.27.3, Apple M4 Pro, `GOMAXPROCS=1`,
  five repetitions of 300 ms. Figures below are medians, not dev-cell estimates.

Decoder measurements include label parsing/cache lookup and 64 entries with
1 KiB lines. The shared case has two resource attributes. The comparison is
against the existing `Decoder.Decode` implementation in this checkout.

| Kafka record | Existing decode (µs/record) | Nested consumer (µs/record) | Existing allocation (B/record) | Nested consumer allocation (B/record) |
| --- | ---: | ---: | ---: | ---: |
| Nested, native | 12.672 | 11.681 | 77,792 | 77,792 |
| Nested, shared attributes | 16.173 | 12.501 | 82,200 | 78,040 |
| Legacy flat, native | 9.665 | 12.358 | 65,633 | 77,873 |
| Legacy flat, expanded attributes | 20.297 | 22.776 | 81,507 | 93,746 |

The legacy-flat cost is real in this microbenchmark: native decoding is ~28%
slower and allocates ~19% more because the old serial decoder reuses its entry
array. This is not the steady-state Kafka input when phase 1's flag is enabled.
It is a POC trade-off to reassess before extending this implementation to cells
that continue writing flat records.

Chunk measurements append 64 short lines with only shared metadata, without
WAL, rate validation, flushing or compression. The baseline expands metadata
before each ordinary append; the POC interns the group.

| Shared attributes | Ordinary append (µs/batch) | Shared append (µs/batch) | Ordinary allocation (B/batch) | Shared allocation (B/batch) |
| --- | ---: | ---: | ---: | ---: |
| 0 | 5.879 | 5.808 | 11,792 | 11,792 |
| 8 | 43.407 | 7.407 | 66,611 | 13,432 |
| 32 | 187.853 | 12.828 | 246,395 | 19,344 |

These shared-only append results show the optimization's best case. Entries
with their own metadata still need merged symbol slices; the dev comparison
must include that case and should not use this table as an expected cell-wide gain.
