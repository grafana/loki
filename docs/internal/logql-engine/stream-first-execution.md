# Stream-first metric query execution (v1 engine)

Internal design documentation for the classic (v1) LogQL engine's two execution models for metric
(range-vector) queries:

- The **timestamp-first** model (default)
- The **stream-first** model (opt-in)

It describes the current design — what each model does, how they differ, and the invariants they
rely on.

## Summary

- Metric queries can be executed in two ways that produce **identical results** for the queries they
  both support, but differ in the order samples flow through the pipeline and how they are
  aggregated — which in turn changes memory, CPU, and latency behaviour.
- **Timestamp-first** (default) delivers all sources' samples in one global timestamp order and
  slides a window across them. **Stream-first** (opt-in) delivers samples grouped per stream and
  folds each into order-independent per-step accumulators.
- Stream-first is **opt-in**, applies only to **decomposable** range aggregations, and is the model
  that processes samples in the order closer to how data is physically structured on storage.
- The two models are equivalent for eligible queries; the ordering is an **enabler**, not a
  correctness requirement of the stream-first aggregator.

## The metric query pipeline (shared context)

A metric query reduces log samples to a per-step numeric matrix. Regardless of model, it flows
through the same stages:

1. **Fan-out to sources.** The query is sent to every source that may hold matching data: ingesters
   (including replicas), the chunk store, and — in the future — columnar data objects. Each source
   returns a stream of samples.
2. **Cross-source merge + deduplication.** One merge layer combines the sources into a single
   stream and removes duplicate samples (the same log line served by more than one source).
3. **Range-vector evaluation.** The merged stream is turned into per-step values for each output
   series (e.g. a count or rate per step).
4. **Vector aggregation.** Optional top-level grouping (e.g. `sum by (...)`) consumes the per-step
   values.

Two properties of this pipeline are load-bearing and define the two models:

- **The order samples are delivered in** (by timestamp, or by stream).
- **Where aggregation happens** — it must run *above* deduplication, never inside a source (see
  [Deduplication](#deduplication)).

## The two execution models

### Timestamp-first

- Every source returns its samples in **global timestamp order**.
- The merge layer produces a single, globally time-ordered stream, deduplicating within each
  (stream, timestamp) group as it advances.
- The range-vector evaluator slides a time window across the ordered stream. For overlapping windows
  it buffers roughly one window of raw samples per output series at a time.
- **Memory characteristic:** to emit samples in global time order, the merge must keep at least **one
  decoded chunk per actively-read stream resident at once** (it needs the current chunk of every
  stream to know which sample is globally next). Peak memory therefore scales with the number of
  streams read concurrently.

### Stream-first

- Every source returns its samples **grouped by stream**: ordered by stream identity first, then by
  timestamp within each stream.
- The merge layer aligns the same stream across sources (they all use the same stream ordering) and
  deduplicates within each (stream, timestamp) group, needing only bounded state.
- The range-vector evaluator **drains the merged stream once**, folding each sample into
  per-(output series, step) accumulators, then replays the result step by step. Because folding is
  order-independent, the per-stream delivery order is fine.
- **Memory characteristic:** a source decodes **one stream at a time and releases it** before moving
  on, so peak memory is bounded by a small fetch-ahead buffer plus the result matrix.

## The ordering contract

The desired sample order is chosen per query and carried explicitly to every source and to the
evaluator, so the whole pipeline makes a consistent decision.

- The request carries a **sample-order selector** whose default/zero value means *timestamp order*. This
  makes the choice backward compatible: any source that predates the selector, or any query that
  does not set it, behaves exactly as the default model.
- **Stream-first order** is defined as: samples ordered by **stream identity ascending**, then by
  **timestamp ascending within each stream**. Every source must present streams in the *same* stream
  order so the merge can line up the same stream across sources with bounded memory.

## Eligibility

Stream-first is used for a query only when **both** hold:

1. The feature is **enabled** (a setting that is off by default).
2. The range aggregation is **decomposable** — its per-window value can be accumulated incrementally,
   one sample at a time, without retaining the raw samples. Both instant and range queries qualify.

Everything else — the feature disabled, or a non-decomposable operation (e.g. quantile, standard
deviation/variance, ...) — uses the default timestamp-first path unchanged.

## Stream identity

The merge and deduplication both key on a **stream identity** that must be identical for the same
stream across every source.

- The identity is a **stable hash of the stream's label set** — a pure function of the labels, so an
  ingester, the chunk store, and a future data-object reader all compute the same value for the same
  stream and therefore align. (The store derives it from the TSDB index fingerprint, which equals this
  hash except under fingerprint-mapper remapping — see Current limitations.)
- Under label-reducing push-down (e.g. `sum by (...)`, where sources project labels down to the
  grouping set before returning samples), the identity used for ordering and merging remains the
  **stream's own label hash**, *not* a hash of the reduced/grouped labels. Using the reduced hash
  would collapse many streams onto one value, so a source's output would no longer be monotonic in
  (stream identity, timestamp) and the per-stream merge alignment would break. Label reduction still
  happens (it drives the evaluator's output grouping), but it does not drive the ordering identity.

## Deduplication

Deduplication is necessary because sources overlap: the same log line is commonly served by more
than one source (replica ingesters, and the ingester↔store overlap where recently-flushed chunks
still live in the ingester while also present in the store).

### The core rule: aggregate above deduplication

Time-range aggregation must run **above** the merge/dedup layer, over raw samples — never inside a
source. If two overlapping sources each aggregated their own copy of a shared window, the shared
samples would be counted on both sides and summing them would double-count; partial aggregates have
lost sample identity, so no later step could recover it. Keeping aggregation above a dedup over raw
samples is what makes overlap safe.

### The deduplication key

Two samples are duplicates iff they share all three of:

- **stream identity** (the stable stream-label hash, above);
- **timestamp**;
- **sample hash** — a hash of the extractor's *output* labels together with the log line.

The merge delivers all samples sharing a (stream identity, timestamp) group adjacently; within that
group, a sample whose sample hash has already been seen is dropped. Two physical copies of one log
line (from replicas, or from ingester↔store overlap) share all three components and collapse to one.

### Correctness matrix

| Scenario | Reconciled by | Outcome |
|---|---|---|
| Replica ingesters, same physical sample | identical (stream, timestamp, sample hash) | ✅ collapse |
| Ingester ↔ store overlap, same sample | identical key | ✅ collapse |
| Several samples from one entry (multiple extractors / variants) | a distinguishing output label → different sample hash | ✅ kept |
| Distinct streams, or distinct lines | different stream identity or sample hash | ✅ kept |
| Two entries in one stream: same timestamp, same output labels, same line, differing only in an un-projected structured-metadata label | identical sample hash | ⚠️ under-count |
| Two distinct streams colliding on the 64-bit stream hash, with the same output labels, line, and nanosecond | same merge group + same sample hash | ⚠️ mis-collapse |

### Quirks (current, and mostly pre-existing)

- **Sample hash uses the extractor's output labels.** This is deliberate: it keeps multiple samples
  produced from a single entry (multi-extractor / variants) distinct, because each carries a
  distinguishing label. The trade-off is the rare under-count in the matrix above — two entries that
  are identical after label projection and differ only in un-projected content collapse. It requires
  the same line *and* the same nanosecond, so it is rare, and it is a pre-existing property of Loki's
  dedup, not specific to stream-first.
- **A zero sample hash disables dedup for that sample.** Every source must populate the sample hash;
  a source that leaves it zero would not be de-duplicated.
- **64-bit stream-hash collisions.** Two distinct streams can share the same 64-bit stream hash. The
  merge orders by (stream identity, then labels, then timestamp) — using labels as a tie-breaker, not
  the hash alone — so colliding streams get a deterministic order and are not interleaved
  nondeterministically, and the group boundary also considers labels so distinct colliding streams
  are not merged. The only residual mis-collapse needs the same hash *and* the same output labels
  *and* the same line *and* the same nanosecond — rare, and currently accepted by design.
- **Per-line label cache is not collision-safe.** For queries that transform labels per line — a
  parser (`| json`, `| logfmt`), `label_format`, `by`/`without` grouping, or structured-metadata
  merge — the per-line result cache is keyed by the output labels' 64-bit hash with no label-equality
  check. If two *distinct* output-label sets produced within one query collide on that hash, a
  line/sample is attributed to the wrong labels — e.g. `sum by (path) (count_over_time({app="x"} |
  logfmt [5m]))` could add a `path="/a"` sample to the `path="/b"` series. Same astronomically-rare
  64-bit trigger as above, confined to that one query. The per-stream identity caches (`ForStream` /
  `ForLabels`) *are* collision-safe; extending the guard to this per-line cache is deferred to avoid a
  hot-path allocation on every line.

## Stream-first reading and prefetch

For stream-first order to help rather than hurt, a source must produce per-stream output natively
and hide storage latency without inflating memory. The chunk store does this as follows:

- **Native stream-first production.** Chunks are grouped by stream, streams are ordered by stream
  identity, and each stream's chunks are read in timestamp order — so the output is stream-first
  without an in-memory re-sort.
- **Fetch is separated from decode.** Fetching compressed chunk bytes is I/O-bound and latency-heavy;
  decoding to samples is CPU- and memory-heavy. A prefetcher fetches compressed bytes **ahead** of
  the consumer, while the consumer decodes **one stream at a time** and releases each stream's
  compressed data as it advances. This keeps the working set bounded while still hiding latency.
- **Bounded, ordered fetch-ahead.** A small fixed pool of loaders fetches several chunk batches
  concurrently and delivers them to the consumer **in order** (out-of-order completions are
  re-sequenced). The pool size is derived from two existing store settings — the object client's
  max-parallel-GET width and the chunk batch size — so the aggregate fetch width matches the default
  path's and adds no extra load on the object store. Because only compressed bytes are held ahead
  (and only for a few in-flight batches), the prefetch buffer is small relative to a single decoded
  stream.

## Range-vector evaluation via per-step accumulators

The stream-first evaluator turns the merged, deduplicated sample stream into per-step values using
order-independent accumulation:

- Each output series owns an array of small **per-step accumulators**, one per query step. An
  accumulator holds a running value and a sample count.
- A per-operation **reducer** defines how to fold a sample into an accumulator and how to finalize an
  accumulator into the window's value (count → the count; sum/rate → the running sum, scaled by the
  range for rates; average → a streaming mean; min/max → the running extremum).
- The evaluator drains the whole input once, folding each sample into every step-window it belongs
  to, then replays the accumulators step by step. Folding is commutative, so the result is
  independent of the order samples arrive in — which is why any per-stream delivery order works.
- A window with no samples (count zero) is skipped.
- **Memory characteristic:** peak memory is bounded by the number of accumulators — output series ×
  steps — not by the number of input samples.

## Trade-offs, limitations, and invariants

### Current limitations

- **Decomposable operations only, opt-in.** Non-decomposable operations and the disabled state always
  use the default path.
- **Store identity relies on the index fingerprint.** The store must know each stream's identity
  before loading a chunk (to order and prefetch streams), but a chunk's labels are only populated once
  it is fetched — so the store keys on the TSDB index fingerprint, which is available from the ref.
  That fingerprint equals the ingester's `StableHash` of the raw labels in the normal case (both are
  the xxhash of the same labels), so cross-source deduplication aligns. It diverges only when the
  ingester's fingerprint mapper remaps a fingerprint on a 64-bit collision: the store then reports the
  remapped fingerprint while the ingester reports `StableHash`, so the two copies of that stream would
  not deduplicate and its samples could double-count. This is astronomically rare; fully closing it
  would require plumbing the raw labels from the index into the reader before the fetch.
- **No static guard on result-matrix width.** The accumulator matrix is bounded by output cardinality;
  for high-cardinality queries that are not reduced by push-down this can be large. A runtime
  series/memory guard is future work.
- **Additive operations fold per window.** Every operation currently folds each sample into every
  window it covers (cost proportional to window overlap). An additive fast path (difference-array +
  prefix-sum, constant work per sample) is a possible future optimisation.
- **Label-reduction push-down is kept.** Sources still project labels down to the grouping set. This
  is deliberate: disabling it would turn a grouped query over a large selector into one output series
  per raw stream, a large cardinality regression that would dwarf any memory saving.

### Invariants

- The default timestamp-first path is unchanged and is used whenever stream-first is disabled or the
  query is ineligible.
- The sample-order selector defaults to timestamp order; an unset selector or an older source yields
  the default behaviour.
- Aggregation always runs above deduplication.
- Stream identity is a stable hash of the stream's labels, identical across every source (except the
  store's index fingerprint under fingerprint-mapper remapping — see Current limitations).
- The stream-first aggregator is order-independent; eligible queries produce results identical to the
  default path (enforced by differential tests).

## Forward compatibility: columnar data objects

The strategic motivation for stream-first is columnar data objects, whose native on-disk layout is
stream-first (samples laid out per stream). A data-object reader joins the pipeline as **just another
stream-first source**: it presents each stream's samples in the required per-stream order and
populates the same stream identity and sample hash as the other sources, so it merges and deduplicates
against ingester and chunk-store data through the exact same seam. Timestamp-first, by contrast, would
force a data object into a global time order its layout does not provide.

## Reproducing the benchmark comparison (agent prompt)

The prompt below drives an AI agent to run the benchmarks and emit a timestamp-first vs stream-first
comparison markdown file. Substitute `<source>` with `store_without_duplicates` (a single read) or
`store_with_duplicates` (every sample duplicated, to also exercise cross-source deduplication).

```text
Run the v1-engine metric benchmarks for source <source> and write a timestamp-first vs stream-first
comparison markdown file.

1. Latency + allocations — run BenchmarkLogQLQueries over the chosen source, both modes, all
   injected latencies, single-shot:

     go test ./pkg/logql/ -run '^$' \
       -bench 'BenchmarkLogQLQueries/mode=.*/source=<source>' \
       -benchmem -benchtime=1x -count=1 -timeout=60m

2. Peak RSS — for every query, measure the per-timestamp and per-stream leaf at latency=0s with the
   process-isolated harness (median of 3):

     go run ./tools/memory-peak-bench -pkg ./pkg/logql/ -count 3 -bench '<comma-separated leaves>'

   where each leaf is:
     BenchmarkLogQLQueries/mode=<per-timestamp|per-stream>/source=<source>/query=<name>/latency=0s
   Build the leaf list carefully: in zsh `for q in $list` does NOT word-split — use ${=list} or bash.

3. Write a markdown file with two comparison tables:
     - Wall-clock latency, with columns 0s / 50ms / 250ms.
     - Memory utilization at 0s artificial latency, with columns: allocated bytes (B/op),
       allocations (allocs/op), and peak RSS.
   One row per query. The first column describes the query's cardinality (input streams -> output
   series); the second column is the actual LogQL query. Show each value as
   "timestamp-first -> stream-first (delta)" where delta = (stream-first - timestamp-first) /
   timestamp-first, so a negative delta means stream-first is better (e.g. 2x faster -> -50%).
```
