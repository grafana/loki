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

| K | N/file | overlap | ns/op | B/op | allocs/op |
| - | ------ | ------- | ----- | ---- | --------- |
|   |        |         |       |      |           |

**Verdict:**

### Phase 2 — binary-heap merge

| K | N/file | overlap | ns/op | B/op | allocs/op | vs. baseline |
| - | ------ | ------- | ----- | ---- | --------- | ------------ |
|   |        |         |       |      |           |              |

### Phase 3 — loser-tree merge

| K | N/file | overlap | ns/op | B/op | allocs/op | vs. baseline |
| - | ------ | ------- | ----- | ---- | --------- | ------------ |
|   |        |         |       |      |           |              |

### Recommendation
