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

**Working branch**: `dahoppe/nommap-index-gateway` (off `main`). All work
lives here. Do not push — Dan pushes and opens PRs.

**Repository roles**:
| Concept | Loki path |
|---|---|
| Existing mmap Reader | `pkg/storage/stores/shipper/indexshipper/tsdb/index/index.go` |
| Buffered Reader (Phase 1) | same file, `NewBufferedFileReader` |
| Streaming Reader (Phase 2) | `pkg/storage/stores/shipper/indexshipper/tsdb/index/stream_*.go` |
| Streaming decoder (from Mimir) | `pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc/` |
| Mode dispatch | `pkg/storage/stores/shipper/indexshipper/tsdb/single_file_index.go` (`SetIndexReaderMode`) |
| Config flag | `pkg/storage/stores/shipper/indexshipper/shipper.go` (`IndexReaderMode`) |

**Config surface**: `-tsdb.shipper.index-reader-mode=<mmap|buffered|streaming>`
(default `mmap`) or YAML `storage_config.tsdb_shipper.index_reader_mode`.
`disable_index_mmap` is a deprecated alias for `buffered`.

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
- Phase 1 (buffered): shipped. Not the production path — buffered holds every
  cached file's bytes on the Go heap and OOMed pods in <preprod-ns> (see
  2026-07-29 entry).
- Phase 2 Bucket A (streaming, 8 commits): shipped, verified via cross-check
  tests. First preprod round showed ~2.3-2.7× latency regression from
  per-byte reads. Progressively closed by C0, C1, C2, C3, C4 (see below).
- Bucket C perf tuning: **five commits landed on 2026-07-29** that took
  GetChunkRef to mmap parity and dropped GetSeries from ~8× to ~2.3× the
  mmap baseline. Detailed measurements in the 2026-07-29 entry.
- Buckets B/D/E remain individually gated. Only start on explicit go-ahead.

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

Rough ordering, each should compile and pass tests standalone:

1. **PR: streaming encoding foundation.** Just the `encoding/` package copied
   from Mimir. Vendored / adapted, with tests. Not wired into anything.
2. **PR: streaming postings + symbols.** The subset of Mimir's `index/`
   package needed for our reader. Standalone with tests.
3. **PR: streaming `Reader` implementation.** New type living alongside
   existing `Reader`. Feature flag defined but no caller wired.
4. **PR: wire feature flag through index-gateway construction.** Default off.
   This is the "safe to merge" PR — mmap path unchanged.
5. **PR: cross-check test / correctness benchmarks.** Compare streaming vs
   mmap on real fixtures.
6. **PR: metrics + tuning knobs.**
7. **PR: sparse snapshots** (may split further).
8. **PR: enable-by-default** (only after Dan validates in production).

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
  OOM report (<preprod-ns>), diagnosed via CPU + goroutine profiling, and
  landed **five commits** on the branch that closed most of the remaining
  streaming-vs-mmap gap. Each was built into a custom GEL image and
  measured in preprod against `<preprod-ns>/index-gateway`.

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

  - *<preprod-ns> buffered-mode OOM* (initial trigger for the day):
    `index_reader_mode: buffered` caused the IG to `os.ReadFile` every
    locally-cached TSDB into the Go heap, per index file per tenant.
    Startup died mid-`loadLocalTables` after 91 tables. Confirmed
    interpretation of the plan doc's Phase 1 tradeoff ("no page eviction —
    all bytes on heap"). Buffered is not viable for large cached working
    sets; treat as small-scale-only.

  - *<preprod-ns> `--analyze-labels` incident (~13:33–14:17 UTC)*: user
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
