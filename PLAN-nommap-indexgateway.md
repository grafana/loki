# Loki index-gateway: remove mmap

Goal: eliminate mmap from Loki's index-gateway TSDB index reads, mirroring
Mimir's approach (originally PR grafana/mimir#3639, since evolved substantially).
Rationale: mmap page faults block goroutines invisibly to the Go runtime — a
slow disk / cold page can stall the whole gateway. Standard file I/O lets the
scheduler observe and manage the block.

> Shared scratchpad between Dan and Claude — commit updates as the work
> evolves. Dan is responsible for pushing branches and opening PRs; Claude
> creates branches, writes code, and structures commits (including updates
> to this file).

---

## Pickup guide — fresh Claude session, read this first

**What this project is**: replacing mmap in Loki's index-gateway TSDB reader
with schedulable file I/O. Mimir did the same for its store-gateway
index-header (PR grafana/mimir#3639) and iterated a lot since; we borrow
their `Decbuf` encoding layer and port Loki's `Reader` on top of it.

**Working branches**:
- `dahoppe/nommap-index-gateway` (off `main`) — the "reference" branch that
  holds the full end-state and this plan doc. Never lands directly.
- `dahoppe/introduce-nommap-config` (off `main`) — PR #23663, the first
  reviewable slice: config flag + `Reader` interface + `StreamReader` stub
  that delegates to mmap. Merging shortly (as of 2026-07-30).
- Follow-up PR branches will be cut off `dahoppe/introduce-nommap-config`
  (or off `main` once #23663 merges), one small step per PR.
Do not push — Dan pushes and opens PRs.

**Repository roles** (state on the reference branch — the merged surface
after #23663 lands is a subset; individual follow-ups fill in each piece):
| Concept | Loki path |
|---|---|
| Reader interface | `pkg/storage/stores/shipper/indexshipper/tsdb/index/reader.go` |
| Existing mmap Reader (renamed) | `pkg/storage/stores/shipper/indexshipper/tsdb/index/index.go` (`ByteSliceReader`, `NewMmapFileReader`) |
| Streaming Reader | `pkg/storage/stores/shipper/indexshipper/tsdb/index/stream_*.go` |
| Streaming decoder (from Mimir) | `pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc/` |
| Mode dispatch | `pkg/storage/stores/shipper/indexshipper/tsdb/single_file_index.go` (`openIndexFileReader`) |
| Config flag | `pkg/storage/stores/shipper/indexshipper/shipper.go` (`IndexReaderMode`) |

**Config surface (post #23663)**: `-tsdb.shipper.index-reader-mode=<mmap|stream>`
(default `mmap`) or YAML `storage_config.tsdb_shipper.index_reader_mode`.

The reference branch also carries a `buffered` mode and a `disable_index_mmap`
alias from Phase 1; both are being intentionally dropped in the incremental
PR path — buffered OOMed pods on 2026-07-29 and has no path to production,
so shipping fewer modes reduces the config surface reviewers have to reason
about. If we ever want it back it's one commit away on the reference branch.

**How to check what's landed**:
```bash
git log --oneline main..HEAD
```
Commit messages follow the plan's proposal IDs (P2.A1, P2.C0, etc.).

**How to test locally**:
```bash
go test ./pkg/storage/stores/shipper/indexshipper/tsdb/...
```
Streaming reader has method-by-method cross-check tests against the mmap
reader in `stream_reader_test.go` (V3 + V4 fixtures).

**How to check what's deployed**: preprod deployment target and image-tag
scheme are recorded in Claude's local memory file `preprod_observability.md`
(not in this repo). In short: the tag suffix encodes the loki-2 commit SHA,
so `git log <sha>` maps a running pod to a set of shipped proposals.

**Where we are** (see "Current status" section at end for detail):
- **Two PRs open on the incremental path**:
  - **PR #23663** (branch `dahoppe/introduce-nommap-config`, rebased on
    main 2026-07-31) — flag + Reader-interface extraction + StreamReader
    stub. Merges with no behaviour change.
  - **PR #23696** (branch `dahoppe/streamenc-foundation`, stacked on
    #23663) — vendored streamenc, adapted for Loki, and streaming
    header/TOC/RawFileReader. Three commits ready for review.
    Deployed to a preprod primary IG on 2026-07-31 with
    `-tsdb.shipper.index-reader-mode=stream`; zero errors, latency
    back to baseline within one 5m bucket post-restart. Stream mode
    confirmed via CPU profile (metrics-side confirmation blocked
    until item 7 in the Next ordering lands).
- **Reference-branch history** (kept for measurements):
  - Phase 1 (buffered): shipped but not viable for production —
    OOMed pods in preprod (see 2026-07-29 entry).
  - Phase 2 Bucket A (streaming, 8 commits): shipped, verified via
    cross-check tests. First preprod round showed ~2.3-2.7× latency
    regression from per-byte reads. Progressively closed by C0, C1,
    C2, C3, C4.
  - Bucket C perf tuning: **five commits landed on 2026-07-29** that
    took GetChunkRef to mmap parity and dropped GetSeries from ~8× to
    ~2.3× the mmap baseline. Detailed measurements in the 2026-07-29
    entry.
  - Buckets B/D/E remain individually gated. Only start on explicit
    go-ahead.

**Skills**:
- Interacting with the running cluster / dashboards uses `gcx` — see
  "Observability handles" section below for the exact commands.
- Reading commit history and files is via the usual git / Read tools.

---

## Observability handles (preprod)

Deployment target (cluster / namespace), image path template + known
tags, dashboard UID, datasource UIDs, and the exact `gcx` command
cookbook live in Claude's local memory file `preprod_observability.md`
rather than in this committed doc. Ask Claude to look them up when
needed.

Tips learned the hard way (safe to keep in-repo):
- The `--json list` hint on every `gcx` response is safe to ignore for
  interactive use; `-o table` and `-o raw` are the useful outputs.
- Regex-negation in LogQL uses `!~`; simple string exclude is `!=`.
  Chain multiple to drop noise in a single query.
- The config-dump lines from `config.go:27` explode to thousands per pod
  restart; searching for a specific config key across a whole namespace
  usually hits the 5000-line ceiling. Pin to a single pod when digging.
- `gcx metrics query --step 1m` matches the dashboard granularity.
- Datasource UIDs are stable across sessions — recorded once in the
  memory file so we don't re-discover.

---

## Background: how the two codebases compare

**Mimir (source of design):**
- Component: store-gateway.
- File touched: `pkg/storage/indexheader/` — a *truncated* view of the TSDB
  index (symbols + posting-offset table) written to disk per block.
- Streaming reader replaces a much smaller surface (symbol lookup + posting
  offsets only).
- Current relevant subpackages: `encoding/`, `index/` (postings, symbols,
  sparse_postings, sparse_symbols), plus `stream_binary_reader.go`,
  `reader_pool.go`, `lazy_binary_reader.go`, `snapshotter.go`.

**Loki (target):**
- Component: index-gateway.
- Mmap enters at `pkg/storage/stores/shipper/indexshipper/tsdb/index/index.go`
  in `NewFileReader` (→ `fileutil.OpenMmapFile`) and one more spot in the same
  file around `newReader`/`Symbols`. The `Reader` struct is a
  Prometheus-derived copy (not vendored) — we own it.
- The `Reader` serves the *full* TSDB index API: `Postings`, `Series`,
  `LabelValues`, `LabelNames`, `LabelValueFor`, `LabelNamesFor`, `ChunkStats`,
  `Symbols`, `SymbolTableSize`, `PostingsRanges`, `Bounds`, `Checksum`,
  `RawFileReader`, plus internal `lookupSymbol`.
- Callers of `index.NewFileReader` outside tests:
  - `pkg/storage/stores/shipper/indexshipper/tsdb/single_file_index.go:130`
    (the index-gateway path — primary target)
  - `pkg/storage/stores/shipper/indexshipper/tsdb/builder.go:137`
    (post-build verification; hot but short-lived)
  - `pkg/logql/bench/discover/pkg/tsdb/reader.go:32` (benchmarks)
- `index.NewReader(RealByteSlice(...))` is also used in
  `pkg/storage/stores/shipper/indexshipper/tsdb/builder.go:248` and
  `pkg/bloombuild/common/tsdb.go:117` — these already take a byte slice, not
  mmap. We can leave them alone.

**Key implication:** we cannot simply drop in Mimir's `indexheader` package.
Mimir's `StreamBinaryReader` reads a much smaller file and only exposes
symbol/posting-offset operations. We need to build a streaming version of the
*full* Loki `index.Reader`. Mimir's `encoding/` package (`FileReader`,
`Decbuf`, `DecbufFactory`, `bufio` pooling) is the reusable foundation.

Legacy note: BoltDB-shipper and boltdb code paths are out of scope. TSDB
(FormatV3+) only.

---

## Approach

Add a parallel, file-based `Reader` implementation gated by a feature flag.
When the flag is off, the existing mmap path is unchanged. When on, all
`Reader` methods route through a `DecbufFactory`-backed reader.

Two-track structure:

1. **Encoding foundation** — port Mimir's `encoding/` verbatim (or as close as
   licensing allows) into Loki. This is the reusable building block.
2. **Streaming Reader** — reimplement each method of the existing `Reader`
   struct against `DecbufFactory` instead of `ByteSlice`. Most methods are
   straightforward translations because the on-disk format is the same TSDB
   binary encoding.

Interface strategy: `IndexReader` (the interface consumed by `TSDBIndex` in
`single_file_index.go`) already abstracts the reader. We can introduce a
`StreamReader` type that satisfies `IndexReader` and let the callsite pick
based on a flag. No caller code changes required beyond construction.

Config surface: single boolean, e.g.
`-index-gateway.streaming-tsdb-reader.enabled` (mirroring Mimir's
`-blocks-storage.bucket-store.index-header.stream-reader-enabled` naming
philosophy). Default off. Later add tuning knobs (buffer size, pool size) as
Mimir did.

---

## Phase 1 — Hack it together (preprod-ready)

Goal: something Dan can deploy to preprod and observe. Minimum change that
eliminates mmap.

**Simplification vs Mimir**: Mimir did a full streaming rewrite in one shot
because their `Reader` surface was small (index-header only: symbols + posting
offsets). Loki's `Reader` surface is much larger (full TSDB reader). Rather
than porting the whole thing at once, split the problem:

- **Phase 1**: Read the whole index file into a `[]byte` on open using
  `os.ReadFile`, then reuse the existing `Reader` unchanged. This removes
  mmap and switches to schedulable file I/O. Costs: no page eviction (all
  bytes resident); on-open latency (full file read). Wins: mmap-induced
  goroutine stalls go away — which is the primary motivation.
- **Phase 2**: Add a true streaming path using the `streamenc` package
  (already scaffolded, uncommitted) that only pages in what's needed.

The Phase 1 change is small enough to test the fundamental hypothesis: is
avoiding mmap valuable even without streaming? If yes, invest in Phase 2. If
no, the whole project pivots.

Concrete steps:

- [x] Create branch `dh/nommap-index-gateway` off `main`.
- [x] Scaffolding for Phase 2: copy Mimir's `pkg/storage/indexheader/encoding/`
      + `pkg/util/filepool/` into Loki at
      `pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc/{,filepool/}`.
      Kept as uncommitted files; will be used in Phase 2.
- [ ] Add `NewBufferedFileReader(path)` that reads the file into memory and
      returns a `*Reader` backed by the resulting `[]byte`. Lives alongside
      the existing `NewFileReader`.
- [ ] Add a boolean config flag on the index-gateway (default false). When
      true, `NewTSDBIndexFromFile` (in `single_file_index.go`) calls
      `NewBufferedFileReader` instead of `NewFileReader`.
- [ ] Wire the flag from index-gateway config all the way down. Look at what
      already parametrizes `NewTSDBIndexFromFile`.
- [ ] Add a cross-check test that opens a real TSDB fixture both ways and
      verifies `Series` / `Postings` / `LabelValues` return identical results.
- [ ] Run `go test ./pkg/storage/stores/shipper/indexshipper/tsdb/...`.
- [ ] Report ready-for-preprod to Dan.

Explicit non-goals for Phase 1:
- Streaming reads (Phase 2).
- Metrics tuning beyond what falls out.
- Touching `builder.go` or `bloombuild` callers.

---

## Phase 2 — Production quality

Phase 2 is a set of individually-approvable proposals. Each has a Motivation,
Scope, Verification, and rough Effort estimate. Dan approves / amends /
denies each; nothing lands without a green light on that specific proposal.

Notation:
- **Effort**: S = ≤1 day, M = 1–3 days, L = 1 week, XL = multi-week.
- **Depends on**: proposals that must land first.
- Priority tags describe expected value if Phase 1 preprod shows a real win
  (H = ship-blocking, M = wants, L = optional polish).

### Bucket A: Deliver true streaming (the whole point of Mimir's design)

**P2.A1 — Streaming Reader constructor + IndexReader adapter.** [H, M,
depends: none]
Motivation: the buffered path still holds the whole file in heap. Streaming
lets the OS page-cache do its job while keeping I/O schedulable.
Scope: introduce `type StreamReader struct` in the `index` package, using
`streamenc.FilePoolDecbufFactory`. Constructor `NewStreamFileReader(path,
opts)`. Implement enough of the surface to satisfy the `tsdb.IndexReader`
interface (10 methods) plus `Version()`, `RawFileReader()`, `Close()`. Reject
`PostingsRanges()` explicitly (no production callers — confirmed).
Verification: unit test that the struct implements the interface; construction
works on a fixture written by `NewWriter`.
Note: this proposal only builds the plumbing — individual methods land in
P2.A3..P2.A7 below.

**P2.A2 — Port TOC + stream `Bounds`/`Checksum`/`Version`.** [H, S, depends:
P2.A1]
Motivation: cheapest methods; validates the encoding plumbing end-to-end.
Scope: read the TOC via `DecbufFactory.NewDecbufAtChecked` at the fixed TOC
offset; store from/through/checksum as fields on `StreamReader`.
Verification: extend the cross-check test to add `NewStreamFileReader` as a
third comparand alongside mmap and buffered.

**P2.A3 — Port `Symbols` iterator + `lookupSymbol` + `SymbolTableSize`.**
[H, M, depends: P2.A1]
Motivation: symbol lookup is on every Series read.
Scope: port Mimir's `pkg/storage/indexheader/index/symbols.go` into
`.../tsdb/index/streamidx/symbols.go`. Adjust for FormatV4 (Loki's newest).
`lookupSymbol` uses the sparse offset index built at construction to seek
into the symbol table and scan forward.
Verification: cross-check every symbol against the mmap reader on a
generated fixture; add a fingerprint-heavy fixture to exercise the sparse
scan.

**P2.A4 — Port posting-offset table + `Postings`.** [H, L, depends: P2.A3]
Motivation: `Postings` is the query hot path.
Scope: port Mimir's `pkg/storage/indexheader/index/postings.go` (v1 + v2
implementations of `PostingOffsetTable`). Wire into `StreamReader.Postings`.
Reject `FingerprintFilter` non-nil for the first cut (or fall through to a
buffered `Postings` implementation) — figure out if any callers pass one.
Verification: cross-check postings iteration for every (label, value) on
generated fixtures with 10, 1k, 100k series.

**P2.A5 — Port `Series` + `ChunkStats`.** [H, L, depends: P2.A3, P2.A4]
Motivation: `Series` is called for every matching postings ref during a
query — dominates read volume for high-cardinality queries.
Scope: reimplement the series-record decoder using `Decbuf` against the
factory. Careful: Loki's V3/V4 chunk-meta encoding differs from Prometheus's
(paging + IngestedAt in V4).
Verification: cross-check per-ref against mmap on the fixtures from P2.A4.
Include a V4 fixture with `IngestedAt`.

**P2.A6 — Port `LabelValues` + `LabelNames` + `LabelValueFor` +
`LabelNamesFor`.** [H, M, depends: P2.A3, P2.A4]
Scope: label-values comes from posting-offset table scan; label-names comes
from series decoding for a ref (LabelValueFor / LabelNamesFor) or symbol
enumeration.
Verification: cross-check test enumerates all labels + values.

**P2.A7 — FingerprintOffsets in streaming form.** [H, S, depends: P2.A2]
Motivation: Loki adds fingerprint offsets for shard-aware queries. Currently
loaded eagerly.
Scope: decide — keep the fingerprint offsets fully in memory even in the
streaming reader (they're small — 2 uint64 per 1024 series), or stream them.
Recommendation: keep in memory.
Verification: existing shard tests pass with `StreamReader`.

**P2.A8 — Wire streaming path behind the flag (mode enum).** [H, S, depends:
all of A2–A7]
Motivation: end the split between "buffered" and "streaming" behind a single
knob.
Scope: replace `DisableIndexMmap bool` with `IndexReaderMode` enum
(`mmap` / `buffered` / `streaming`) on `indexshipper.Config`. Keep
`DisableIndexMmap` as a hidden alias for one release.
Verification: three-way cross-check test.

### Bucket B: Correctness / robustness

**P2.B1 — Three-way cross-check integration test.** [H, S, depends: P2.A2]
Extend `TestBufferedFileReader_MatchesMmap` to also check `StreamReader`.
Rename accordingly.

**P2.B2 — Property test: random-labels fixture cross-check.** [M, M, depends:
P2.A8]
Motivation: catch encoding edge cases (empty label values, unicode names,
very long label values that force `Decbuf.Read` over `Peek`, etc.).
Scope: `go test -fuzz` or `rapid`-style generator producing valid label sets;
build via `NewWriter`; assert all three paths agree on all query methods.
Verification: fuzz runs to some coverage target locally; add to CI as a
short (1s) fuzz smoke test.

**P2.B3 — Concurrent-reader race test.** [M, S, depends: P2.A8]
Motivation: the streamed reader shares a file-handle pool; a shared pool
under concurrent Series/Postings must not corrupt state.
Scope: `t.Parallel` many goroutines hammering the same reader with different
queries under `-race`.

**P2.B4 — Handle `RawFileReader()` in the streaming path.** [H, S, depends:
P2.A1]
Motivation: the indexshipper uploads the raw index file via `RawFileReader`.
The current mmap path returns a `bytes.Reader` over the mmap'd bytes; the
buffered path returns one over the in-memory slice; streaming needs a fresh
`os.Open` returning an `*os.File` (satisfies `io.ReadSeeker`).
Scope: `StreamReader.RawFileReader()` opens the file, hands the caller
ownership.
Verification: unit test that reads bytes match the file on disk; the
shipper's existing upload flow exercises this.

### Bucket C: Performance polish (only after correctness lands)

**P2.C1 — bufio pool size + reader-buffer-size tuning knobs.** [M, S,
depends: P2.A1]
Scope: `readerBufferSize` currently hardcoded 4KiB (from Mimir). Expose per
`indexshipper.Config`.
Verification: pprof + benchmark shows expected effect.

**P2.C2 — Sparse symbols snapshot on disk.** [M, M, depends: P2.A3]
Motivation: Mimir's `sparse_symbols.go` writes a compact protobuf of the
symbol offset table so cold start doesn't rescan the whole symbol section.
Scope: port `indexheader/index/sparse_symbols.go` + `indexheaderpb/sparse.proto`.
Adjust for Loki's index location; sparse file lives alongside the index.
Verification: benchmark cold-start reader open time before/after.

**P2.C3 — Sparse postings snapshot on disk.** [M, M, depends: P2.A4]
Same as P2.C2, for postings offset table. Port `sparse_postings.go`.

**P2.C4 — Snapshotter for full state.** [L, L, depends: P2.C2, P2.C3]
Motivation: index-gateway restart currently rebuilds all state. Mimir's
`snapshotter.go` persists loaded readers.
Scope: adapt to Loki's index-gateway lifecycle. Deferred — likely not
critical if Loki's warmup is already fast; measure first.

**P2.C5 — Benchmark harness for reader open + query paths.** [M, M, depends:
P2.B1]
Scope: `Benchmark*` covering: open, Postings for 10/1k/100k results,
Series read for 10/1k refs. Emit numbers for all three paths.

### Bucket D: Metrics + operability

**P2.D1 — Metrics for the streaming path.** [H, S, depends: P2.A1]
Scope: file-handle pool metrics are already in `filepool.NewFilePoolMetrics`
— pass a registerer through and register them. Add histograms for
Postings/Series read latency labeled by `mode={mmap,buffered,streaming}`.
Verification: `promtool test rules` or unit test on metric registration.

**P2.D2 — Structured log line on reader construction.** [L, S, depends:
P2.A1]
Log at info level which mode was chosen per file when disabled by default,
debug otherwise. Aids incident triage.

**P2.D3 — Runbook / docs.** [H, S, depends: P2.A8]
Scope: docs section (`docs/sources/setup/`) explaining the modes, tradeoffs,
recommended mode, migration path, config flags.

### Bucket E: Loose ends from Phase 1

**P2.E1 — Replace package-level `SetDisableIndexMmap` with real plumbing.**
[M, S, depends: P2.A8]
Motivation: the atomic global is a Phase-1 hack. Once mode is an enum on
`indexshipper.Config`, thread it through `NewShippableTSDBFile` and
`NewTSDBIndexFromFile` as an argument.
Scope: add an options struct; existing call sites default to mmap.

**P2.E2 — Extend streaming to `builder.go:137` (post-build verification).**
[L, S, depends: P2.A8]
Motivation: consistency; also small cold-open latency win.
Scope: swap to `NewStreamFileReader`. Verification: existing builder tests.
Risk: hot path — verify no regression.

**P2.E3 — `pkg/bloombuild/common/tsdb.go` and `pkg/logql/bench/discover/...`
— assess or migrate.** [L, S, depends: none]
Motivation: completeness. Bench code doesn't matter. Bloom builder may.
Scope: audit call patterns; if streaming makes sense, migrate.

### Bucket F: Deferred / explicitly out of scope

- `lazy_binary_reader.go` equivalent. Mimir's lazy reader is for many cold
  blocks; Loki index-gateway keeps a smaller set of files hot. Revisit only
  if profiling shows warm-set churn.
- Multi-tenant symbol table sharing across readers.
- Reader-side chunk fetching. Out of scope entirely.

---

## Phase 2 acceptance criteria (draft)

Before we move to Phase 3 (split into PRs), we want:
1. Streaming mode passes the three-way cross-check + concurrent-race test.
2. Metrics show file-handle pool churn under normal load stays flat.
3. p50/p99 query latency in a controlled preprod comparison is within some
   band of mmap (target: p99 ≤ 1.5× mmap; specific numbers TBD by Dan).
4. Cold-start time not more than 2× mmap (with sparse snapshots enabled).
5. All existing tests pass on all three modes via a build tag or matrix run.

---

## Phase 3 — Split into reviewable PRs

The reference branch is one 17-commit monolith. Reviewers can't reason about
it in one sitting, so we're rebuilding the same end-state as a chain of small,
independently-reviewable PRs. Each compiles, tests, and adds nothing until
the next PR wires it up — the mmap path is untouched behind the default
`mmap` mode until the very last PR flips a default.

The order deliberately inverts the reference branch: **flag first, then
scaffolding, then implementation**. That way every PR is safe to merge —
even the fully-fleshed-out `stream` mode is only reached by explicit opt-in.

### In flight (open PRs)

- **PR #23663 — `chore: Introduce tsdb.shipper.index-reader-mode feature flag`**
  (branch `dahoppe/introduce-nommap-config`, opened 2026-07-30, still
  OPEN). Rebased onto `main` on 2026-07-31 to keep the stack current.
  Contains:
  - `IndexReaderMode` enum (`mmap` / `stream`), CLI flag, YAML field,
    validation.
  - `Reader` interface extracted from the existing `Reader` struct.
  - Existing mmap-backed struct renamed to `ByteSliceReader` (opened via
    `NewMmapFileReader`).
  - `StreamReader` stub in `stream_reader.go` — every method delegates to
    an inner `ByteSliceReader`. `TestReaders_CrossCheck` passes trivially.
  - `openIndexFileReader` in `single_file_index.go` picks between them by
    mode.
- **PR #23696 — `chore: Implement streaming reading of header and TOC`**
  (branch `dahoppe/streamenc-foundation`, opened 2026-07-31, still
  OPEN). Stacked on top of #23663. Three commits:
  - `Vendor Mimir's streaming index encoding + tests` — verbatim copy of
    Mimir's `pkg/storage/indexheader/encoding/` + `pkg/util/filepool/`
    (source **and** tests) with SPDX/Provenance headers preserved and
    the intra-batch `filepool` import path rewritten to Loki's. The
    tests don't compile as-is (they reference
    `github.com/grafana/mimir/pkg/util/test` and `BucketDecbufFactory`);
    the second commit adapts them.
  - `Adapt vendored streamenc for Loki` — package renamed
    `encoding` → `streamenc`; filepool metric names prefixed
    `loki_tsdb_index_`; test files adapted (dropped bucket-backed path
    and benchmarks, replaced `test.NewTB(t)`/`test.TB` with plain
    `*testing.T`, replaced two `test.EqualSlices` with `bytes.Equal`).
    Includes small fixes for CI: parameter shadowing (`cap uint` →
    `capacity uint` in `NewFilePool`, `len int` → `length int` in
    factory_test.go) and `// nolint:revive` on the vendored
    `FilePoolMetrics` / `FilePoolCloser` stutter (following the
    `pkg/queue/queue.go` precedent).
  - `Implement streaming reading of header and TOC` — `NewStreamFileReader`
    opens a `FilePoolDecbufFactory`, parses the 5-byte header and the
    76-byte TOC using **only** Mimir's existing Decbuf primitives
    (`ResetAt` → `CheckCrc32` → `ResetAt` → `Be64` × 9 → `Be32` —
    mirrors `TOCFromIndexHeader` in Mimir), and serves `Version` /
    `Bounds` / `Checksum` / `Size` / `RawFileReader` from the streamed
    state. Query-surface methods (Symbols, Postings, Series, label
    lookups, ChunkStats, PostingsRanges) still delegate to
    `ByteSliceReader`. As part of the same PR, the `Reader() (io.ReadSeeker, error)`
    interface on `shipperindex.Index`, `tsdbindex.Reader.RawFileReader`,
    and `tsdb.GetRawFileReaderFunc` becomes `io.ReadSeekCloser` — this
    fixes a real production FD leak in the shipper upload path
    (`uploads/index_set.go:152` and the equivalent
    `compactor/index_set.go:316`) that was previously invisible because
    the `bytes.Reader` return from `ByteSliceReader.RawFileReader` needed
    no Close. Test coverage: six new `TestReaders_*` subtests iterate
    over both readers and assert matching rejection behavior on bad
    magic / unknown version / truncated header / truncated TOC /
    corrupt TOC CRC, plus independence of concurrent `RawFileReader`
    handles.

Deliberately kept out of this PR (deferred to follow-ups when the
supporting profiling data lands):
  - `Decbuf.ReadInto`. Not needed for header/TOC; Mimir doesn't have it
    and its `TOCFromIndexHeader` uses the exact `ResetAt`+`CheckCrc32`+
    rewind pattern we adopted. Later series-record work will justify it
    with measured overhead (reference-branch commit `215b82d0af`).
  - `FilePoolDecbufFactory.FileSize` cache. Not measurable at
    construction time. Later Bucket A work will justify it with the
    "fstat = 7.6% of CPU" profile (reference-branch commit
    `ce00b0eeb0`).
  - Nil-metrics guards inside filepool. Kept Mimir's original
    non-nil-safe form; `NewStreamFileReader` passes
    `filepool.NewFilePoolMetrics(nil)` — an **unregistered** metrics
    struct (promauto handles nil Registerer). Downside: streamenc
    counters don't appear in Prometheus scrapes. Fix belongs to a
    "metrics for the streaming path" PR that plumbs a real Registerer
    through the whole open-index chain (`store.init` →
    `OpenShippableTSDB` → `NewShippableTSDBFile` → `NewTSDBIndexFromFile`
    → `openIndexFileReader` → `NewStreamFileReader`). Estimated at
    ~10 files / ~15 callsites / one construction-site change; mostly
    mechanical.

- **PR #23730 — `chore: Implement streaming reading of symbols section`**
  (branch `dahoppe/stream-symbols`, stacked on `dahoppe/streamenc-foundation`
  / #23696, opened 2026-08-03, OPEN). Single commit `caec49bc21`. This is the
  "stream Symbols" step from the Next list — but it **pivoted from streaming
  `Symbols()` to removing it**, after confirming during the work that the
  method is dead in Loki:
  - **Removed `Symbols()` and `SymbolTableSize()` from the `index.Reader`
    interface** (`.../tsdb/index/reader.go`). Neither has a production caller:
    they're inherited from Prometheus's reader API (Prometheus calls `Symbols()`
    during compaction to copy the symbol table forward); Loki's compaction
    instead rebuilds symbols from series labels via `AddSymbol`, so it never
    reads the table back. Verified across loki-2 **and** enterprise-logs — only
    tests call them. (Also confirmed Loki has never *written* a V1 index: the
    writer hardcoded FormatV2 from its first commit `1837c9e0b2` / PR #5376;
    V1 is read-only Prometheus-compat. So the streaming reader is V2+-only.)
  - **Cascade from that removal**: `Symbols()` was also declared on the
    `tsdb.IndexReader` interface (`querier.go`) — `index.Reader` is passed as a
    `tsdb.IndexReader` in `single_file_index.go`, so removing it from one forced
    removing it from the other (compile), which in turn made
    `headIndexReader.Symbols()` an unused method (removed, for lint).
    `MemPostings.Symbols()` was left (exported API, harmless). See the new
    "Open questions" note on de-duplicating these two interfaces — the
    duplication is what made this a multi-file edit.
  - **Kept `ByteSliceReader.Symbols()`** as a concrete mmap-only helper (no
    longer on the interface) because two pre-existing, unrelated tests depend
    on it — notably `builder_test.go`'s "sorts symbols before writing" (a real
    builder invariant, in an external package with no other way to read symbols
    back). `builder_test.go`'s `getReader` now returns the concrete
    `*index.ByteSliceReader`.
  - **Implemented `streamSymbols`** (`.../tsdb/index/stream_symbols.go`) as the
    foundation for later `Series`/label streaming: `newStreamSymbols` scans the
    symbol section once at construction (CRC-validated) capturing a sparse
    offset table (every `symbolFactor`-th symbol); `Lookup(n)` seeks via the
    sparse table then walks forward up to `symbolFactor` symbols. Built at
    construction on `StreamReader.symbols`, but not yet wired to any interface
    method (Series/labels still delegate to `ByteSliceReader`).
  - `ReverseLookup` and `Iter` were implemented then dropped at Dan's
    direction: `Iter` only existed to back the removed `Symbols()`;
    `ReverseLookup` is only needed to warm a name-symbol cache (write/open-path)
    and can be re-added when a caller appears.
  - Test: `TestStreamSymbols_LookupMatchesMmap` cross-checks
    `streamSymbols.Lookup` against the mmap `Symbols.Lookup` over every ordinal
    plus count parity and identical out-of-range rejection, on V3 + V4
    many-symbol fixtures.
  - **Method takeaway — prune before you stream.** `SymbolTableSize` (removed
    in #23730's parent work), `Symbols` (this PR), and `PostingsRanges` (plan
    already flags it callerless) are all dead on `index.Reader`. Audit the
    interface for dead methods before implementing a streaming version of each.

### Next (from #23696; each is its own PR, ~200–800 lines diff)

1. ~~**PR: stream `Symbols` + `lookupSymbol` + `SymbolTableSize`.**~~
   **LANDED as PR #23730** (see "In flight" above) — but pivoted: `Symbols`
   and `SymbolTableSize` were **removed** from the reader interface (dead in
   Loki) rather than streamed. What shipped: a `streamSymbols` type with
   `newStreamSymbols` + `Lookup` (sparse-offset seek), built at construction
   as the foundation for streaming `Series`/label methods, plus a
   Lookup-parity test vs the mmap `Symbols`. `ReverseLookup`/`Iter` were
   dropped (no caller); `lookupSymbol` wiring comes with the Series/labels
   PRs below, which are `streamSymbols.Lookup`'s first real consumers.
2. **PR: stream posting-offset table + `Postings`.** Ports Mimir's
   `postings.go` (v1 + v2). Wires into `StreamReader.Postings`. Adds
   fixtures with 10 / 1k / 100k series.
3. **PR: stream `Series` + `ChunkStats`.** Reimplements series-record
   decoder via `Decbuf` — careful with Loki's V4 chunk-meta paging +
   `IngestedAt` field. V4 fixture included.
4. **PR: stream `LabelValues` + `LabelNames` + `LabelValueFor` +
   `LabelNamesFor`.** Falls out mostly from postings-offset + series
   ports; small.
5. **PR: FingerprintOffsets in the streaming reader.** Loaded eagerly at
   open (they're small; 2 uint64 per 1024 series). Existing shard tests
   pass with `mode=stream`.
6. **PR: drop `StreamReader.mmapReader` fallback + delete
   `ByteSliceReader` delegation dead code.** By this point every method
   has a native streaming implementation. This is the "no more mmap on
   the stream path" milestone. Ships without changing any default —
   `mode=mmap` is still the default and still uses `ByteSliceReader`.
7. **PR: metrics wiring for the streaming path.** Plumbs a real
   `prometheus.Registerer` from `store.NewStore` down through
   `OpenShippableTSDB` / `NewShippableTSDBFile` / `NewTSDBIndexFromFile`
   / `openIndexFileReader` into `NewStreamFileReader` so
   `loki_tsdb_index_file_handle_{pooled,unpooled}_{open,close}_total`
   become scrapable. Currently a `NewFilePoolMetrics(nil)` sentinel is
   used, which makes it impossible to confirm from Prom whether stream
   mode is actually engaged — verified in preprod on 2026-07-31.
   ~10 files, mostly mechanical parameter threading. Do this before
   the perf tuning PRs below so their profiles/graphs make sense.
8. **PR: perf tuning — `Decbuf.ReadInto` batch reads.** Corresponds to
   commit `215b82d0af` on the reference branch. Preprod-measured 2.3–2.7×
   → 1.1–1.6× vs mmap p99. Include before/after numbers.
9. **PR: perf tuning — pool CRC32 + postings-list scratch buffers.** Commit
   `1b9dbb75b6` on reference branch.
10. **PR: `-shipper.streaming-index-max-idle-file-handles` config knob.**
    Commit `f17424eb09` on reference branch.
11. **PR: cache `FilePoolDecbufFactory` file size.** Commit `ce00b0eeb0`
    on reference branch. Small.
12. **PR: batch series reads through a single `Decbuf`.** Commit
    `2bf0b0201d` — introduces `ForPostingsSeries` on `StreamReader` and
    `batchSeriesReader` interface. This one is bigger; may split.
13. **PR: batch series+labels + pool symbols Decbuf.** Commit `557525bf5a`.

### Later (post-parity)

14. **PR: three-way / cross-mode benchmark harness in the tree.** Corresponds
    to Bucket C5 in Phase 2 above.
15. **PR: sparse snapshots on disk (symbols + postings).** Buckets C2 + C3.
    May split.
16. **PR: docs + runbook + upgrade notes.** Bucket D3.
17. **PR: flip default from `mmap` → `stream`.** Only after Dan validates
    a production cell.

### Gone from the reference branch (deliberate omissions)

- Phase 1 buffered mode + `disable_index_mmap` alias. OOM'd in preprod and
  has no path to production; landing it as a shipping mode would just add
  a config surface reviewers need to reason about and users could foot-gun
  with. If someone specifically wants to test whole-file-in-heap reads for
  research purposes they can build off the reference branch.

---

## Open questions / decisions to revisit

- Do we want a package boundary between "streaming encoding" and the TSDB
  `Reader`, or is it fine to keep everything in one package? Mimir chose
  separate packages — probably worth mirroring.
- Should the streaming reader open the file once and pread, or open per-op?
  Mimir's `FileReader` opens once and reuses via pool. Same here.
- `RawFileReader()` is used by the shipper to upload the file. That doesn't
  need the streaming reader — it just needs an `io.ReadSeeker` over the file.
  We can implement it trivially with `os.Open` in the streaming path.
- What's the interaction with the `PostingsCache` / `LabelValuesCache` layers
  above `Reader`? Assumed transparent — those caches sit above the interface.
- **De-duplicate `index.Reader` and `tsdb.IndexReader` (follow-up refactor).**
  Two interfaces currently declare the same 10-method query core: `index.Reader`
  (`.../tsdb/index/reader.go`) and `tsdb.IndexReader` (`.../tsdb/querier.go`).
  `index.Reader` is a strict superset — the shared query methods plus four
  file-only ones (`Version`, `RawFileReader`, `PostingsRanges`, `Size`). The
  duplication is real double-maintenance: removing `SymbolTableSize` and
  `Symbols` each meant editing both interfaces (and, for `Symbols`,
  `headIndexReader` too). Do NOT fully merge them — `tsdb.IndexReader` is the
  polymorphic query interface over *both* on-disk files and the in-memory head
  (`headIndexReader`, consumed by `querier.go`'s `PostingsForMatchers`), and the
  head legitimately has none of the file-only methods. Instead, extract the
  shared query core into a single interface in the `index` package (the `tsdb`
  package imports `index`, not vice-versa, so it must live in `index`), have
  `index.Reader` embed it plus the file-only methods, and make
  `tsdb.IndexReader` a type alias for it. Pure interface refactor, no behaviour
  change; best as its own small PR. Would make future query-surface changes
  single-touch. Raised 2026-08-03 while trimming dead methods (`SymbolTableSize`,
  `Symbols`) off the reader interface.

---

## Current status

- **2026-07-08**: plan drafted.
- **2026-07-08**: Phase 1 in progress on branch `dh/nommap-index-gateway`.
  Branch created. Phase 2 streamenc scaffolding present but uncommitted.
  Working on the `NewBufferedFileReader` + flag wiring.
- **2026-07-08**: Phase 1 code complete, tests passing. Ready for Dan to
  review and push to preprod.
  - `NewBufferedFileReader` added in `.../tsdb/index/index.go`.
  - `SetDisableIndexMmap` package toggle in `.../tsdb/single_file_index.go`.
  - `DisableIndexMmap` config field + `-*.shipper.disable-index-mmap` flag on
    `indexshipper.Config`.
  - Wired through `store.NewStore` (calls `SetDisableIndexMmap`).
  - Cross-check test `TestBufferedFileReader_MatchesMmap` verifies parity.
  - `go test ./pkg/storage/stores/shipper/indexshipper/...` green.
  - `go build ./...` green.
  - Phase 2 streamenc/filepool scaffolding committed at
    `.../tsdb/index/streamenc/` as `chore(tsdb): scaffold streaming decoder...`
    — unused by any caller yet, foundation for Phase 2.
- **2026-07-08**: branch renamed `dh/nommap-index-gateway` →
  `dahoppe/nommap-index-gateway`. Three commits on top of `main`:
  - `5b6b9f6940` feat(tsdb): add NewBufferedFileReader as mmap-free alternative
  - `d6caa159a9` feat(indexshipper): wire DisableIndexMmap flag to select buffered reader
  - `d383d79686` chore(tsdb): scaffold streaming decoder derived from Mimir index-header
- **2026-07-08**: Custom image build for the Phase 1 branch kicked off
  (build URL in memory file). Dan will use the resulting image to deploy
  the branch to preprod with `disable_index_mmap: true` and observe
  whether removing mmap reduces goroutine stalls in the index-gateway.
- **2026-07-08**: Phase 2 Bucket A (streaming reader) landed as 8 commits on
  the same branch. Streaming reader passes cross-check parity with mmap on
  V3 + V4 fixtures for every surface method (Symbols, Postings, Series,
  ChunkStats, LabelValues, LabelNames, LabelValueFor, LabelNamesFor,
  FingerprintOffsets). `-*.shipper.index-reader-mode=streaming` selects it;
  `disable_index_mmap` is now a deprecated alias for `buffered`. Buckets B
  (correctness), C (performance), D (metrics/docs), E (loose ends) remain.
- **2026-07-09**: linter follow-up commit (`752d9464fd`) — gofmt whitespace
  cleanup on the streaming reader files, no functional change.
- **2026-07-09**: Custom image build with the Bucket A streaming reader
  kicked off (build URL in memory file). Dan will deploy the resulting
  image to preprod and A/B the three modes (mmap / buffered / streaming)
  via `index_reader_mode`.
- **2026-07-09 preprod signal — commit `752d9464fd` deployed to preprod,
  rollout ~15:40–15:43 UTC, `index_reader_mode: streaming`:**
  - **Errors are rollout artifacts, not a streaming bug.** During the
    rolling restart window, queriers logged "removing index gateway failing
    healthcheck" with `connect: connection refused` / `i/o timeout` — the
    normal pattern for a terminating pod dropping in-flight connections
    and a starting pod not yet answering. `loki_index_gateway_requests_total{status="error"}`
    stayed at zero throughout; the client-side spikes on GetChunkRef error
    and GetShards cancel are transients that returned to baseline by ~15:44
    UTC. IG-side error logs contain only unrelated noise (memcached SRV
    lookup, license.jwt, GET /metrics broken pipes).
  - **Real regression: query latency ~2.3–2.7x baseline (steady state).**
    - GetChunkRef p50: 2.8ms → 6.5ms
    - GetChunkRef p99: 9ms → 24ms
    - GetShards p99: 10ms → 24ms
    - Root cause is well-understood: `streamReadBytes` and the postings-list
      read loop in `stream_series.go` / `stream_postings.go` consume the
      file one byte at a time (`d.Byte()` per iteration) or one uint32 at
      a time (`d.Be32()` per ref) because `streamenc.Decbuf` doesn't
      expose a batch `ReadInto`. This is the exact overhead flagged in
      the P2.A5/A4 commit messages as "acceptable but wasteful for
      Phase 1 of Bucket A."
    - **Fix path**: Bucket C tuning — expose `Decbuf.ReadInto(dst []byte)`
      via the underlying `BufReader.ReadInto`, then swap the byte loops.
      That should recover most or all of the delta. Track as **P2.C0**
      (was implicit inside C1).
  - Streaming is behaving otherwise correctly: no elevated 5xx, no OOMs,
    no crashes.
- **2026-07-09**: P2.C0 landed as commit `0ccc630a16` on branch —
  `Decbuf.ReadInto` batch-read replaces the per-byte / per-uint32 hot
  loops in `readUvarintSection` and `readPostingsList`. All cross-check
  tests still green. Ready for another preprod round to confirm the
  latency regression closes.
- **2026-07-09**: Second custom image build with the C0 batch-read fix
  kicked off (build URL in memory file). Once deployed, compare
  GetChunkRef / GetShards p50/p99 to the 15:44 UTC → 15:48 UTC
  steady-state numbers documented above.
- **2026-07-10 preprod signal — commit `0ccc630a16` deployed to preprod,
  rollout ~08:50–09:10 UTC, primary IG on `index_reader_mode: streaming`,
  shadow on a separate shadow-IG statefulset via the tee framework.
  All three primary pods and three shadow pods healthy post-rollout, no
  further restarts, no errors on either client
  (`loki_index_gateway_tee_request_duration_seconds{status="success"}`
  = 100% of ~1.83 req/s on both).**

  Tee client-side latencies (steady state 09:22–09:34 UTC, same traffic
  fanned to both):
  | Op / metric | primary (streaming C0) | secondary (shadow) |
  |---|---|---|
  | GetChunkRef p50 | ~4.5 ms | ~44 ms |
  | GetChunkRef p99 | ~10–15 ms | 90 ms – 1.15 s |
  | GetShards p50 | ~5.0 ms | ~44 ms |
  | GetShards p99 | ~12–22 ms | 100–810 ms |

  vs the 2026-07-09 mmap baseline (p50 2.8 ms / p99 9 ms GetChunkRef;
  p99 10 ms GetShards), streaming C0 sits at roughly **1.6× baseline p50
  and 1.1–1.6× baseline p99** — within the plan's draft acceptance
  criterion of "p99 ≤ 1.5× mmap". Compared to pre-C0 streaming
  (p50 6.5 ms / p99 24 ms), **P2.C0 recovered most of the regression**.

  Resource comparison: primary pods hold ~1.19 GB working-set / 250–450 MB
  Go heap (sparse offset caches + eager FingerprintOffsets); shadow pods
  hold only ~110 MB working-set. CPU is at parity (~15 mCPU/pod both
  sides).

  **Caveat that needs resolution**: the shadow container reports **zero
  major page faults** (`rate(container_memory_failures_total{failure_type="pgmajfault"})`
  = 0) and **zero mapped-file size** (`container_memory_mapped_file` = 0)
  throughout the window. Those are not what an mmap'd TSDB index under
  load should look like — expected: multi-GB mapped-file bytes and a
  nonzero pgmajfault rate on cold reads. The shadow may in fact be
  running `buffered` or `streaming` mode, not `mmap`. Dan to confirm
  shadow's `index_reader_mode` config; if it is not `mmap`, we still need
  a proper mmap-mode comparison window before signing off Bucket A on the
  acceptance criteria.

  Aggregated server-side `loki_index_gateway_request_duration_seconds`
  queries without a `container` label filter are misleading in this
  deployment: they mix primary and shadow statefulsets, and the shadow's
  tail dominates the numbers. Always filter by the appropriate container
  label to separate primary vs shadow views (exact label values recorded
  in the memory file).

- **2026-07-29**: heavy perf iteration day. Started from a buffered-mode
  OOM report in preprod, diagnosed via CPU + goroutine profiling, and
  landed **five commits** on the branch that closed most of the remaining
  streaming-vs-mmap gap. Each was built into a custom GEL image and
  measured in preprod against the primary index-gateway.

  Commits (all on `dahoppe/nommap-index-gateway`, off `main`):

  | # | Commit | Change |
  |---|---|---|
  | C1 | `1b9dbb75b6` perf(tsdb): pool CRC32 and postings-list scratch buffers | `Decbuf.CheckCrc32` was allocating a fresh 1 MiB scratch per call; `readPostingsList` was allocating a fresh postings-list `[]byte` per call. Both moved to `sync.Pool`, postings-list buffer released when the iterator drains via a `pooledBigEndianPostings` wrapper. |
  | C2 | `f17424eb09` feat(indexshipper): configurable file-handle pool for streaming reader | Added `StreamingIndexMaxIdleFileHandles` config + `-*.shipper.streaming-index-max-idle-file-handles` flag. Plumbed via `SetStreamingMaxIdleFileHandles` into `NewStreamFileReaderWithOptions`. Default 0 (no pooling); preprod set to 32. |
  | C3 | `ce00b0eeb0` perf(tsdb): cache file size in FilePoolDecbufFactory | `NewRawDecbuf` was calling `os.File.Stat` on every open — 7.6 % of total CPU. Cached size on the factory (immutable file, safe to memoise). |
  | C4a | `2bf0b0201d` perf(tsdb): batch series reads through a single Decbuf | Added `StreamReader.ForPostingsSeries` + `batchSeriesReader` interface. `TSDBIndex.forSeriesNoLabels` delegates to it — one raw Decbuf held open across the whole postings iteration instead of open/close per ref. |
  | C4b | `557525bf5a` perf(tsdb): batch series+labels reads and pool symbols Decbuf | `streamSymbols.LookupInto(d, o)` — reusable-Decbuf variant of Lookup. `StreamReader.ForPostingsSeriesWithLabels` — labels-yielding batch method that opens both series-section and symbols-section Decbufs once, then builds a batch-local Decoder whose lookupSymbol closure targets LookupInto. Wired into `forSeriesAndLabels` (chunk-filter GetChunkRef path). |

  Cumulative profile impact (top-3 slowest exemplars, cum % of profile,
  before → after each stage):

  | Function | Baseline | +C1 | +C2 | +C3 | +C4a |
  |---|---:|---:|---:|---:|---:|
  | `runtime.memclrNoHeapPointers` (flat) | 26.6 % | 1.35 % | 1.31 % | — | — |
  | `Decbuf.CheckCrc32` | 58.5 % | 2.5 % | — | — | — |
  | `readPostingsList` | 61.8 % | 15.8 % | 3.5 % | — | — |
  | `filepool.Get` | — | 29.2 % | 1.75 % | — | — |
  | `os.File.Stat` | — | 7.6 % | 7.6 % | 0.06 % | — |
  | `readUvarintSection` | 21.4 % | 21.4 % | 16.6 % | 16.6 % | **3.1 %** |
  | Total sampled CPU (top-3 exemplar) | 169 s | 34 s | — | 17 s | 17 s |

  Client-observed p99 latency vs the mmap baseline (July 5, 06:00 UTC —
  same tenant/cluster, well before any nommap work):

  | Operation | mmap baseline | pre-batch streaming | **post-C4a streaming** | vs mmap |
  |---|---:|---:|---:|---:|
  | GetChunkRef | 91 ms | 116 ms | 113 ms | **≈ parity (1.24×)** |
  | GetSeries | 39 ms | 310 ms | 90 ms | 2.3× (down from 8×) |
  | GetShards | 47 ms | 589 ms | 417 ms | 8.9× (down from 12×) |
  | GetStats | 83 ms | 162 ms | 162 ms | 1.9× |

  The remaining GetShards gap is *not* Loki-side work — server-side p99 is
  66 ms (see below); the client-vs-server delta is retries, cancels, and
  wire time. C4b (labels-yielding batch) targets the residual GetSeries
  gap; not yet deployed to preprod as of end-of-day.

  **Two production incidents examined:**

  - *preprod buffered-mode OOM* (initial trigger for the day):
    `index_reader_mode: buffered` caused the IG to `os.ReadFile` every
    locally-cached TSDB into the Go heap, per index file per tenant.
    Startup died mid-`loadLocalTables` after 91 tables. Confirmed
    interpretation of the plan doc's Phase 1 tradeoff ("no page eviction —
    all bytes on heap"). Buffered is not viable for large cached working
    sets; treat as small-scale-only.

  - *preprod `--analyze-labels` incident (~13:33–14:17 UTC)*: user
    ran `logcli series '{}' --analyze-labels --since=72h` on tenant <tenant-id>.
    That tenant produces **4.7 GB compressed TSDB blobs per compaction
    slice**. Each attempted download took ~31 s network + ~60 s gzip
    decompress = 91 s, but `download_timeout` default is 1 min → every
    attempt timed out and `syncWithRetry` restarted from byte 0. After
    ~40 min of retries one attempt got through, but by then two pods had
    OOMKilled from the streaming reader's on-heap open-time state
    (postings offset map, symbols, fingerprint offsets, nameSymbols) for
    the freshly loaded 4.7 GB tenant tables. Recommendations captured in
    the follow-up sections.

  **GetShards deep-tail investigation** (client p99 417 ms, server p99
  66 ms, but p99.9 spikes to 55–70 s intermittently):
  - CPU profile: `boundedShards` is 0.19 % of CPU — not CPU-bound.
  - Goroutine profile from spike windows: **zero goroutines in any
    query-serving stack** (boundedShards / forSeriesNoLabels /
    GetChunkRefs / indexSet / awaitReady). Only bloomshipper +
    tableManager background workers.
  - **Block, mutex, and off-CPU profiles are all disabled on the IG**
    (returning zero samples). Those are exactly the tools that would
    identify what a stuck goroutine is waiting on. Without them we can't
    root-cause the p99.9 spikes definitively.
  - Circumstantial signal: goroutine count doubles (5.4k → 11k) during
    p99.9 spikes; extra goroutines are gRPC infrastructure (framer,
    keepalive, loopyWriter). Consistent with a burst of new gRPC
    connections stressing serialization/dispatch, not with the Loki
    handler itself being stuck.
  - Action items: (1) enable block + mutex profile collection on the IG,
    (2) add explicit tracing spans inside `boundedShards` around
    `GetChunkRefsWithSizingInfo`, `accumulateChunksToShards`, and
    `server.Send`. Both would immediately disambiguate what's happening.

  **Config knob defaults deployed to preprod on 2026-07-29:**
  - `index_reader_mode: streaming`
  - `streaming_index_max_idle_file_handles: 32`

  **Follow-up work identified but not yet started (in rough priority
  order):**
  1. Enable block/mutex/offcpu profiling on IG so we can diagnose the
     GetShards p99.9 tail properly.
  2. Add tracing child-spans inside `boundedShards` (currently a single
     opaque span with no children — a 54 s trace looks like a black box).
  3. Raise `-shipper.download-timeout` from 1 min. For tenants with
     multi-GB compacted blobs the current default guarantees repeated
     failed attempts.
  4. Resumable downloads in the shipper — every retry currently restarts
     from byte 0.
  5. Bound the streaming reader's on-heap open-time state, or at minimum
     log its size at Open so operators can size pods for their largest
     tenants.
  6. Extend `readUvarintSection` content-buffer pooling (same pattern as
     postings-list buffer pool).
  7. Bump `readerBufferSize` in `streamenc/file_reader.go` from 4 KiB to
     something larger (e.g. 16 KiB) so most series records fit one refill.

  **Sanity check against mmap baseline**: the streaming reader is now
  production-viable for the dominant workload (GetChunkRef at parity).
  Remaining gap is concentrated in GetSeries/GetShards on very large
  tenants — expected to close further with C4b once it deploys.

- **2026-07-30**: PR strategy revised — reference branch is unreviewable as
  one blob. Cut the first slice as PR #23663 (`chore: Introduce
  tsdb.shipper.index-reader-mode feature flag`, branch
  `dahoppe/introduce-nommap-config`, opened today). That PR is a
  pure-refactor: extracts a `Reader` interface, renames the existing
  implementation to `ByteSliceReader` / `NewMmapFileReader`, adds a
  `StreamReader` stub that delegates every method to `ByteSliceReader`,
  and adds the `index_reader_mode` config surface (`mmap` default, `stream`
  routes to the stub). No behaviour change at any default. Phase 3
  section above rewritten to describe the ~18 follow-up PRs that rebuild
  the reference branch on top of #23663 one step at a time. **Next PR**:
  streamenc + filepool foundation plus streaming header/TOC/version/
  bounds/checksum/RawFileReader — landed on branch
  `dahoppe/streamenc-foundation` as two commits (see "PR #1" in Phase 3).
  The buffered mode + `disable_index_mmap` alias from Phase 1 are
  deliberately being dropped in the incremental path — the OOM in
  preprod killed any operator case for shipping them.

- **2026-07-31**: PR #23696 opened
  (`chore: Implement streaming reading of header and TOC`, branch
  `dahoppe/streamenc-foundation`, stacked on #23663). Restructured to
  three commits at Dan's direction so each can be reviewed on its own
  and the "here's what we borrowed from Mimir" vs "here's what we
  changed for Loki" boundaries are obvious:
  1. `Vendor Mimir's streaming index encoding + tests` — verbatim
     Mimir source + tests + Provenance headers.
  2. `Adapt vendored streamenc for Loki` — package rename, Loki metric
     prefix, test-file adaptations for the pieces we don't have
     (`test.TB`, `BucketDecbufFactory`), lint fixes (`cap`/`len`
     shadowing renames, `// nolint:revive` on FilePoolMetrics/Closer
     stutter).
  3. `Implement streaming reading of header and TOC` — the actual
     StreamReader wiring plus the `io.ReadSeeker` → `io.ReadSeekCloser`
     interface change with production leak fixes at the two known
     callers.

  Design decisions made along the way, all recorded in the PR
  description and reflected in the "deliberately kept out" list above:
  - Used Mimir's `ResetAt` + `CheckCrc32` + rewind pattern in `readTOC`
    (matching `pkg/storage/indexheader/toc.go` in Mimir) rather than
    adding `Decbuf.ReadInto`.
  - Dropped the FileSize cache (do a plain `os.Stat` at construction
    in `NewStreamFileReader`; leaves `file_factory.go` bit-for-bit
    Mimir modulo the package rename).
  - Kept Mimir's non-nil-metrics assumption; pass an unregistered
    `NewFilePoolMetrics(nil)` from `NewStreamFileReader`. Observability
    cost noted below.
  - Interface change to `io.ReadSeekCloser` promoted from a
    test-cleanup fix to a real production leak fix in
    `uploads/index_set.go:152` and `compactor/index_set.go:316`. Mock
    impls updated to return fresh `os.Open` handles.

  Late in the day both PR branches were rebased onto `main` so a fresh
  custom GEL image could be built (`bucket.NewClient` signature was
  advanced by PR #23643 on main, so building against the older
  enterprise-logs pin failed until we caught up).

- **2026-07-31 preprod signal — image tag recorded in the memory
  file, deployed to a preprod primary IG statefulset (4 pods),
  rollout ~16:11–16:14 UTC. Compactor/ingester/querier remained on
  the current weekly release image. Streaming mode confirmed active
  via CPU profiling.**
  - **No panics, no fatals, no non-transient error logs.** Zero
    `status="error"` on `loki_index_gateway_requests_total`
    throughout the rollout window.
  - **Latency returned to baseline within one 5m bucket after
    rollout completed at 16:14.** During the rolling restart
    (16:08–16:14) client p99 was elevated as pods dropped in and out
    (GetShards briefly to 400–1770 ms) — this is normal rollout
    behaviour, not a code regression. Post-rollout (16:15+):
    GetChunkRef 74–95 ms, GetSeries 40–95 ms, GetShards
    109 ms → 80 ms → 17 ms → 28 ms, GetStats 13–83 ms — all inside
    the pre-rollout envelope.
  - **Resource usage**: memory 0.16 → 0.26 GB working-set at 6 min
    post-restart, growing naturally as caches warm; CPU 0.04–0.12
    core (unchanged); `container_memory_mapped_file` 1.1–1.7 GB
    (matches pre-rollout — mmap crutch still active in stream mode);
    `pgmajfault` 1–55/s (matches pre-rollout — query methods still
    hit the mmap path via delegation).
  - **Confirming stream mode from metrics alone is currently
    impossible** because `loki_tsdb_index_file_handle_*` counters
    are created against an unregistered Registerer and never scraped.
    We had to fall back to inspecting a CPU profile to see
    `streamenc.(*FilePoolDecbufFactory).NewRawDecbuf` on the hot
    path. Adding this metrics-plumbing PR (item 7 in the Next
    ordering) is now the next priority so we can see mode-selection
    directly and get file-handle-pool visibility for the perf work
    that follows.

- **2026-07-31 review-comment fixes to commit 3 of PR #23696**:
  - Cursor Bugbot flagged the two `RawFileReader()` results in
    `TestReaders_RawFileReaderIndependence` as left open. Root cause
    ran deeper than the test: `Reader() (io.ReadSeeker, error)` on
    `shipperindex.Index` didn't require Close, and the production
    upload paths in both `uploads/index_set.go:152` and
    `compactor/index_set.go:316` genuinely leaked a `*os.File` per
    upload in streaming mode. Promoted the interface to
    `io.ReadSeekCloser` on the three touched types (`Index.Reader`,
    `Reader.RawFileReader`, `GetRawFileReaderFunc`), wrapped
    `ByteSliceReader.RawFileReader`'s `bytes.NewReader` return in a
    local `nopCloserReadSeeker` (no-op Close), and added
    log-and-continue `defer idxReader.Close()` on both production
    upload callsites plus `defer require.NoError(rf.Close())` on the
    test.
  - **Reviewer-directed cleanup**: kept commit 2 tightly focused on
    Loki adaptations only. `cap uint`/`len int` shadow warnings
    fixed via rename; `FilePoolMetrics`/`FilePoolCloser` stutter
    warnings suppressed with `// nolint:revive` following the
    `pkg/queue/queue.go` precedent (renaming the types would ripple
    to `file_factory.go`, `stream_reader.go`, and factory_test.go —
    not worth the churn for a stylistic lint). Method order in
    `stream_reader.go` preserved to match the base branch so the
    diff shows single-line replacements per delegated method rather
    than a full-file reorder.

- **2026-08-03**: PR #23730 opened
  (`chore: Implement streaming reading of symbols section`, branch
  `dahoppe/stream-symbols`, stacked on #23696, single commit
  `caec49bc21`). This is the "stream Symbols" step from the Next list,
  but it pivoted to **removing** `Symbols()` + `SymbolTableSize()` from
  the `index.Reader` interface (both dead in Loki — confirmed across
  loki-2 and enterprise-logs) and shipping a `streamSymbols` type
  (`newStreamSymbols` + `Lookup`) as the sparse-offset foundation for
  the later `Series`/label streaming PRs. Full detail in the #23730
  entry under "In flight". Highlights:
  - Established that Loki **never wrote V1 indexes** (writer hardcoded
    FormatV2 from `1837c9e0b2` / PR #5376; V1 is read-only
    Prometheus-compat) — so the streaming symbol reader is V2+-only.
  - Removing `Symbols()` cascaded through `tsdb.IndexReader`
    (`querier.go`) and `headIndexReader` because `index.Reader` is used
    as a `tsdb.IndexReader`. That coupling motivated the new
    "Open questions" note to de-duplicate the two interfaces.
  - `ByteSliceReader.Symbols()` kept as a concrete mmap-only helper to
    preserve pre-existing builder/reader tests.
  - Confirmed takeaway: **prune dead interface methods before streaming
    them**. `SymbolTableSize`, `Symbols` (done) and `PostingsRanges`
    (pending) are all callerless on `index.Reader`.
  - Note: the `dahoppe/stream-symbols` branch pointer was repeatedly
    reset to `4a10d6df11` mid-session by an external process (likely
    another running `claude` session against this checkout); commits
    survived as objects each time. Worth ruling out before the next
    stacked PR.
