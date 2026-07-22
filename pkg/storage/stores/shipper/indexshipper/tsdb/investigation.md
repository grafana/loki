# Investigation: `MultiIndex.GetChunkRefs` merge is a naive concat-then-sort

You are picking up a targeted performance investigation. Everything you
need to start is below; you do not need to read the surrounding
directory to understand the problem, only to fix it.

## The suspect code

`pkg/storage/stores/shipper/indexshipper/tsdb/multi_file_index.go:136`
— `MultiIndex.GetChunkRefs` fans a query across all TSDB index files
whose time range overlaps `[from, through]`, then merges the results.
The merge, at line 164, is:

```go
for _, group := range xs {
    for _, ref := range g {
        if _, seen := seen[ref.ChunkRef]; seen { continue }
        seen[ref.ChunkRef] = struct{}{}
        res = append(res, ref)
    }
}
sort.Slice(res, func(i, j int) bool { return res[i].Less(res[j].ChunkRef) })
```

Two things are worth pinning down before you touch anything:

1. **Each input `xs[k]` is already sorted** by fingerprint. Per-file
   `GetChunkRefs` (`single_file_index.go:243`) walks TSDB postings in
   their on-disk order, which is fingerprint-sorted. The `sort.Slice`
   here throws that order away.
2. **Dedup is required.** During a compaction transition the same
   chunk (fingerprint + from + through + checksum) can be indexed in
   both a multi-tenant pre-compaction file and its per-tenant
   compacted successor. The merge is what removes the duplicate. Any
   replacement algorithm has to preserve this.

Current cost is `O(N log N)` on the merged list (`N = Σ Nₖ`,
`K = number of files`). With `K` typically single-digit and each `Nₖ`
already sorted, an `O(N log K)` K-way merge is the theoretical target.

The maintainers already flagged this. Line 148 has an explicit
`TODO(owen-d): loser-tree or some other heap?` sitting right above the
concat.

`MultiIndex.Series` (line 196) has the same dedup-with-map shape but
does **not** sort at all — callers of `Series` don't rely on order.
Leave that path alone unless you find a caller that would benefit.

## Real-world callers

- **`GetShards` (bounded)** — `pkg/indexgateway/gateway.go:419`. The
  merged, sorted output feeds `accumulateChunksToShards`, which
  requires fingerprint order for its binary-search step
  (`gateway.go:579-584`). This is the highest-value caller: shard
  planning runs on the query-frontend and gates every LogQL query on
  `bounded`-strategy tenants.
- **`GetChunkRefs` from the shipper store** — same result but
  ordering is less load-bearing (the caller re-filters by time range
  and dispatches to object storage).

Realistic `K`: 1 compacted per-tenant TSDB per day plus a handful of
multi-tenant "common" TSDBs from recent ingester flushes. Typical
range **1–10**, spike-y during compaction. Realistic `N` per file:
from a few hundred to millions of chunk refs depending on tenant and
matcher selectivity.

## What I want you to do

Work in three phases. Do not skip phase 1 — the point of this whole
investigation is to decide whether phases 2/3 are worth shipping.

### Phase 1 — benchmark & quantify

Write a benchmark in a new
`multi_file_index_merge_bench_test.go` in this same directory. Do
**not** wire in the `loser-tree` or heap approach yet; benchmark only
the current `sort.Slice` path.

Sweep the parameters that matter:

- **`K` (fan-in)**: `{1, 2, 4, 8, 16}`. Above 16 is unrealistic; below
  2 is a no-op case worth including as a baseline.
- **`N` per input list**: `{1_000, 10_000, 100_000, 1_000_000}`.
- **Overlap fraction** (what proportion of chunks appear in >1 input):
  `{0%, 10%, 50%}`. This exercises the dedup map cost, which any
  replacement also has to pay.

For each cell report ns/op, B/op, and allocs/op. **Also report peak
resident memory** for the largest cell (`N=1M`, `K=16`) using
`runtime.ReadMemStats` before and after — the current path
materialises the full merged slice plus the `seen` map, and one
motivation for a streaming merge is memory, not just CPU.

Extract inputs by having each benchmark iteration synthesise
already-fingerprint-sorted `[]ChunkRefWithSizingInfo` slices with a
seeded PRNG; the sort cost is what you're measuring, so the payload
should be cheap to generate. Fingerprints can be a strided/dithered
sequence rather than random uniform — random uniform is close to
adversarial for a pattern-defeating quicksort and will overstate the
current path's cost. Use a mix.

**Deliverable at end of phase 1:** a short markdown table added to the
bottom of this file with the numbers, plus a one-paragraph verdict.
Two possible conclusions are valid:

- "This is not a bottleneck at production `K` and `N` — recommend
  closing the TODO and moving on." (Perfectly acceptable outcome. The
  point of the benchmark is to earn the right to keep working, or to
  stop.)
- "This is measurably expensive at `K ≥ X` and `N ≥ Y`; proceed to
  phase 2."

Only continue if the verdict is "proceed."

### Phase 2 — streaming binary-heap K-way merge

Implement a heap-based merge as an alternative code path (do **not**
delete the existing one yet — you want to keep it for A/B benchmarks).

- Standard `container/heap` min-heap keyed on
  `ChunkRef.Fingerprint`, then `From`, then `Through`, then `Checksum`
  — matching `ChunkRef.Less` in `pkg/logproto/types.go`.
- Each heap element is `{list_index, cursor}` into one of the K input
  slices; `Pop` advances the cursor and re-pushes if that list still
  has entries.
- Dedup happens inline: keep the last emitted `ChunkRef`; skip a pop
  if it compares equal to the last emitted. This replaces the
  `map[ChunkRef]struct{}` in the current path, which is likely to be a
  large chunk of the allocation cost.

Add benchmarks that mirror the phase-1 matrix and record the new
numbers next to the old ones.

### Phase 3 — loser-tree

Implement a loser-tree merge as a *second* alternative. Prometheus
has one you can crib from (`github.com/prometheus/prometheus/tsdb`
uses one internally for chunk iteration); check the vendored deps
first for something reusable before writing from scratch. If nothing
suitable is vendored, hand-roll it — loser-trees are ~150 lines.

Rationale for trying it: at high `K`, loser-tree does fewer
comparisons per emitted element than a binary heap (one comparison
per level vs. `log₂K` for sift-down), and the tree structure is
cache-friendlier because the comparison path is fixed by position.
At low `K` the constant-factor overhead of tree setup can lose to a
plain heap — the benchmark is what tells you which wins where.

Same benchmark matrix; same table.

### What good looks like

At the end, this file should contain three sets of numbers
(sort.Slice, heap-merge, loser-tree) across the sweep, a
recommendation on which to ship (or "keep sort.Slice"), and a diff
that swaps in the winner behind the existing function signature —
callers should not need to change.

Do not merge phases into one commit. One commit per phase makes the
review and the perf-regression bisect story much easier.

## Watch out for

- **`ChunkRefsPool` reuse**: the current code puts each input back
  into `ChunkRefsPool` after consuming it (`multi_file_index.go:161`).
  A heap/loser-tree merge that holds cursors into all inputs
  simultaneously has to defer the pool `Put` until the whole merge is
  done, or the underlying arrays get recycled mid-iteration. This is
  a subtle correctness trap.
- **Sort stability is not required** — `ChunkRef.Less` is a total
  ordering on `(Fingerprint, From, Through, Checksum)`; equal keys are
  deduplicated. But dedup by "equal on all four fields" is stricter
  than the map-keyed dedup in the current path (which uses the whole
  `ChunkRef` struct as a map key — same thing in practice, but worth
  confirming: check `logproto.ChunkRef`'s fields to make sure there
  isn't a fifth field I've missed).
- **Do not touch `MultiIndex.Series`** unless a caller shows up that
  needs it sorted. Its dedup by fingerprint (not full ChunkRef) is
  different and its output is unsorted today; changing that is a
  scope creep that risks breaking readers that don't care.
- **Empty inputs**: some `xs[k]` may be zero-length. The current path
  handles this trivially; make sure your K-way merge doesn't push an
  invalid cursor onto the heap.

## Files you'll touch

- `pkg/storage/stores/shipper/indexshipper/tsdb/multi_file_index.go`
  — the merge closure inside `GetChunkRefs`.
- `pkg/storage/stores/shipper/indexshipper/tsdb/multi_file_index_merge_bench_test.go`
  (new) — benchmarks.
- `pkg/storage/stores/shipper/indexshipper/tsdb/investigate-multi-index-merge.md`
  (this file) — record results here as you go.

Callers should not need to change; keep the public function signature
identical.

## Success criteria

Ship phase 2 or phase 3 only if it beats `sort.Slice` on both CPU and
allocations at `K ≥ 4, N ≥ 100_000`, and does no worse than 10% slower
at the small end (`K = 2, N = 1_000`). Otherwise the complexity is not
worth it — record the numbers, close the TODO in `multi_file_index.go`,
and move on.

## Results

_(fill in during the investigation)_

### Phase 1 — current `sort.Slice` baseline

Machine: Apple M4 Pro (12 logical cores), Go bench with `-benchtime=3x`
(so each cell is the mean of three iterations; the largest cells swing
±10% run-to-run). K=16/N=1M was skipped in the sweep (~1 GB of live
inputs per iteration triggers heavy GC pressure that swamps the
measurement); it appears once in the "peak memory" row below.

| K  | N/file    | overlap | ns/op         | B/op            | allocs/op |
| -- | --------- | ------- | ------------- | --------------- | --------- |
| 1  | 1,000     | 0%      | 118,792       | 319,512         | 24        |
| 1  | 1,000     | 10%     | 94,139        | 319,512         | 24        |
| 1  | 1,000     | 50%     | 90,736        | 319,512         | 24        |
| 1  | 10,000    | 0%      | 1,125,986     | 4,687,605       | 92        |
| 1  | 10,000    | 10%     | 1,034,959     | 4,687,573       | 92        |
| 1  | 10,000    | 50%     | 995,028       | 4,687,568       | 92        |
| 1  | 100,000   | 0%      | 8,687,722     | 48,985,072      | 556       |
| 1  | 100,000   | 10%     | 8,481,333     | 48,983,208      | 554       |
| 1  | 100,000   | 50%     | 8,796,333     | 48,983,218      | 554       |
| 1  | 1,000,000 | 0%      | 154,333,931   | 581,192,173     | 8,231     |
| 1  | 1,000,000 | 10%     | 153,531,833   | 581,323,304     | 8,234     |
| 1  | 1,000,000 | 50%     | 152,856,903   | 581,061,037     | 8,227     |
| 2  | 1,000     | 0%      | 162,833       | 803,000         | 35        |
| 2  | 1,000     | 10%     | 153,806       | 803,000         | 35        |
| 2  | 1,000     | 50%     | 110,778       | 409,624         | 25        |
| 2  | 10,000    | 0%      | 1,922,653     | 10,063,405      | 161       |
| 2  | 10,000    | 10%     | 1,835,250     | 8,703,528       | 160       |
| 2  | 10,000    | 50%     | 1,791,347     | 7,499,240       | 155       |
| 2  | 100,000   | 0%      | 18,742,778    | 97,858,984      | 1,070     |
| 2  | 100,000   | 10%     | 18,954,680    | 97,858,984      | 1,070     |
| 2  | 100,000   | 50%     | 15,135,792    | 74,175,912      | 1,068     |
| 2  | 1,000,000 | 0%      | 350,563,903   | 1,149,610,152   | 16,406    |
| 2  | 1,000,000 | 10%     | 331,013,569   | 1,108,914,280   | 15,165    |
| 2  | 1,000,000 | 50%     | 260,052,597   | 759,076,264     | 8,250     |
| 4  | 1,000     | 0%      | 392,334       | 1,745,400       | 54        |
| 4  | 1,000     | 10%     | 285,153       | 1,657,976       | 52        |
| 4  | 1,000     | 50%     | 190,556       | 983,224         | 36        |
| 4  | 10,000    | 0%      | 3,921,694     | 20,838,440      | 293       |
| 4  | 10,000    | 10%     | 3,564,361     | 18,126,888      | 292       |
| 4  | 10,000    | 50%     | 2,372,903     | 11,775,528      | 162       |
| 4  | 100,000   | 0%      | 45,699,972    | 194,300,072     | 2,098     |
| 4  | 100,000   | 10%     | 43,399,319    | 194,300,109     | 2,098     |
| 4  | 100,000   | 50%     | 32,855,306    | 147,646,504     | 2,088     |
| 4  | 1,000,000 | 0%      | 776,609,819   | 2,272,773,800   | 32,762    |
| 4  | 1,000,000 | 10%     | 703,593,111   | 2,056,311,976   | 26,160    |
| 4  | 1,000,000 | 50%     | 503,678,278   | 1,304,794,536   | 16,445    |
| 8  | 1,000     | 0%      | 940,195       | 4,024,016       | 91        |
| 8  | 1,000     | 10%     | 818,917       | 3,849,168       | 86        |
| 8  | 1,000     | 50%     | 536,542       | 2,057,296       | 56        |
| 8  | 10,000    | 0%      | 7,624,680     | 42,273,960      | 553       |
| 8  | 10,000    | 10%     | 7,362,680     | 36,916,392      | 552       |
| 8  | 10,000    | 50%     | 4,666,931     | 20,838,440      | 293       |
| 8  | 100,000   | 0%      | 104,854,931   | 384,429,480     | 4,150     |
| 8  | 100,000   | 10%     | 101,382,833   | 384,429,480     | 4,150     |
| 8  | 100,000   | 50%     | 63,715,264    | 212,275,176     | 2,647     |
| 16 | 1,000     | 0%      | 1,825,722     | 8,703,528       | 160       |
| 16 | 1,000     | 10%     | 1,433,639     | 6,668,712       | 129       |
| 16 | 1,000     | 50%     | 946,139       | 4,024,016       | 91        |
| 16 | 10,000    | 0%      | 16,083,500    | 84,694,477      | 1,069     |
| 16 | 10,000    | 10%     | 14,583,458    | 74,175,912      | 1,068     |
| 16 | 10,000    | 50%     | 8,809,861     | 42,273,997      | 553       |
| 16 | 100,000   | 0%      | 244,188,347   | 759,076,264     | 8,250     |
| 16 | 100,000   | 10%     | 222,771,847   | 759,076,264     | 8,250     |
| 16 | 100,000   | 50%     | 136,442,972   | 385,759,272     | 4,191     |

**Peak memory (K=16, N=1M, overlap=10%, `BenchmarkMergeChunkRefsSortPeakMem`):**

| metric                        | value        |
| ----------------------------- | ------------ |
| wall-clock (single merge)     | 3.88 s       |
| heap alloc delta (live)       | 2,378 MB     |
| total alloc delta (churn)     | 7,994 MB     |
| Sys delta                     | 4,039 MB     |
| allocs                        | 88,592       |
| merged output length          | 14,500,000   |

**Verdict — proceed to phase 2.** This is measurably expensive well
inside the realistic operating range. Concrete pain points:

- **CPU.** At the low end of what shard planning sees on a busy tenant
  (K=4, N=100k, 10% overlap) each merge is already **43 ms** and
  climbs super-linearly with K. At K=16 / N=100k it is **223 ms per
  merge** — a substantial chunk of a single LogQL query's shard-planning
  latency for a `bounded`-strategy tenant.
- **Allocations.** The `map[ChunkRef]struct{}` dedup dominates the
  allocation count and total churn — 88k allocations and ~8 GB
  allocated to produce one 14.5M-entry merge is exactly the profile
  the investigation flagged as a motivation for streaming the merge.
- **Overlap sensitivity.** More overlap → smaller output → less
  sorting cost. But the map cost still tracks total input size, which
  is why 50%-overlap cells only shave ~30–40% off ns/op despite
  producing half as many output rows. Any replacement needs to keep
  dedup cheap at the same input scale.

The dedup map and the `O(N log N)` sort are both first-order costs,
and the streaming/heap/loser-tree approaches all attack both at once
(inline dedup on the last emitted value, no full-materialised sort).
Continuing to phase 2.

### Phase 2 — binary-heap merge

| K | N/file | overlap | ns/op | B/op | allocs/op | vs. baseline |
| - | ------ | ------- | ----- | ---- | --------- | ------------ |
|   |        |         |       |      |           |              |

### Phase 3 — loser-tree merge

| K | N/file | overlap | ns/op | B/op | allocs/op | vs. baseline |
| - | ------ | ------- | ----- | ---- | --------- | ------------ |
|   |        |         |       |      |           |              |

### Recommendation
