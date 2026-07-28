# Loki index-gateway: remove mmap

Goal: eliminate mmap from Loki's index-gateway TSDB index reads, mirroring
Mimir's approach (originally PR grafana/mimir#3639, since evolved substantially).
Rationale: mmap page faults block goroutines invisibly to the Go runtime — a
slow disk / cold page can stall the whole gateway. Standard file I/O lets the
scheduler observe and manage the block.

> **Do not commit this file.** It is a shared scratchpad between Dan and Claude.
> Dan is responsible for pushing branches and opening PRs. Claude only works
> locally: creates branches, writes code, and structures commits.

---

## Pickup guide — fresh Claude session, read this first

**What this project is**: replacing mmap in Loki's index-gateway TSDB reader
with schedulable file I/O. Mimir did the same for its store-gateway
index-header (PR grafana/mimir#3639) and iterated a lot since; we borrow
their `Decbuf` encoding layer and port Loki's `Reader` on top of it.

**Working branch**: `dahoppe/nommap-index-gateway` (off `main`). All work
lives here. Do not push — Dan pushes and opens PRs. Do not commit
`PLAN-nommap-indexgateway.md`.

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

**How to check what's deployed**: pull the pod image tag — the suffix after
`custom--<build>-` matches the loki-2 commit SHA. `git log <sha>` locally
tells you exactly which proposals shipped. Preprod cluster/namespace is
`dev-us-central-0/loki-dev-006`.

**Where we are** (see "Current status" section at end for detail):
- Phase 1 (buffered): shipped, in preprod.
- Phase 2 Bucket A (streaming, 8 commits): shipped, in preprod. Correctness
  verified via cross-check tests. First preprod round showed ~2.3-2.7x
  latency regression from per-byte reads.
- P2.C0 (`Decbuf.ReadInto` batch, from Bucket C): shipped, awaiting preprod
  re-measurement.
- Buckets B/C/D/E: not yet started. Individual proposals are gated on Dan's
  approval — do not start Bucket B/C/D/E without explicit go-ahead.

**Skills**:
- Interacting with the running cluster / dashboards uses `gcx` — see
  "Observability handles" section below for the exact commands.
- Reading commit history and files is via the usual git / Read tools.

---

## Observability handles (preprod)

Test cluster / namespace where Dan runs the custom image:
- **Cluster**: `dev-us-central-0`
- **Namespace**: `loki-dev-006`
- **Image path**: `us-docker.pkg.dev/grafanalabs-global/docker-enterprise-logs-prod/enterprise-logs:custom--<build>-<commit>`
  — the `<commit>` suffix matches the loki-2 commit built into that image, so
  cross-referencing `git log` against a running pod's image tag is a reliable
  way to check "which build is deployed?"

**Dashboard — Loki Index Gateways**
- UID: `dad4skf` — Grafana ops instance (`https://ops.grafana-ops.net`).
- Panels of interest: RPS by status, IG request latency (p99), RPS per pod,
  failed-requests-per-client (client side), Tee/Shadow overview.
- Prom datasource UID: `2z9d6ElGk` (grafanacloud-ops-prom on the ops
  instance).

**Log datasource**
- `OP27Xzxnk` (Grafana Logging Dev) — carries dev-cluster container logs.
  `c-R8UWvVk` (Loki-Ops) covers ops-cluster logs but not dev-us clusters.

**gcx cookbook — the exact commands that worked**

```bash
# Current context (should be "ops" for this project)
gcx config current-context

# Resolve a Grafana share short-URL to its dashboard path
gcx api /api/short-urls/<code>

# Get a dashboard by UID (JSON dump — pipe through grep '"title\|expr"' to
# discover panels + queries)
gcx api /api/dashboards/uid/dad4skf

# PromQL against the ops Prometheus. --since is relative to now.
gcx metrics query -d 2z9d6ElGk '<promql>' --since 30m --step 1m -o table

# LogQL against the dev Loki. Filter with |= / !~ / |~. Regex is RE2.
gcx logs query -d OP27Xzxnk '{cluster="dev-us-central-0", namespace="loki-dev-006", container="index-gateway"} |~ "(?i)error"' --since 20m --limit 40 -o raw

# List label values (no --since — labels are global)
gcx logs labels -d OP27Xzxnk cluster
```

Tips learned the hard way:
- The `--json list` hint on every response is safe to ignore for
  interactive use; `-o table` and `-o raw` are the useful outputs.
- Regex-negation in LogQL uses `!~`; simple string exclude is `!=`.
  Chain multiple to drop noise in a single query.
- The config-dump lines from `config.go:27` explode to thousands per pod
  restart; searching for a specific config key across a whole namespace
  usually hits the 5000-line ceiling. Pin to a single pod when digging.
- `gcx metrics query --step 1m` matches the dashboard granularity.
- Datasource UIDs are stable across sessions — hard-code them in this doc
  so we don't re-discover.

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
- **2026-07-08**: Custom enterprise-logs image build for the Phase 1 branch
  in progress:
  https://github.com/grafana/enterprise-logs/actions/runs/28943766074
  Dan will use the resulting image to deploy the branch to preprod with
  `disable_index_mmap: true` and observe whether removing mmap reduces
  goroutine stalls in the index-gateway.
- **2026-07-08**: Phase 2 Bucket A (streaming reader) landed as 8 commits on
  the same branch. Streaming reader passes cross-check parity with mmap on
  V3 + V4 fixtures for every surface method (Symbols, Postings, Series,
  ChunkStats, LabelValues, LabelNames, LabelValueFor, LabelNamesFor,
  FingerprintOffsets). `-*.shipper.index-reader-mode=streaming` selects it;
  `disable_index_mmap` is now a deprecated alias for `buffered`. Buckets B
  (correctness), C (performance), D (metrics/docs), E (loose ends) remain.
- **2026-07-09**: linter follow-up commit (`752d9464fd`) — gofmt whitespace
  cleanup on the streaming reader files, no functional change.
- **2026-07-09**: Custom enterprise-logs image build with the Bucket A
  streaming reader in progress:
  https://github.com/grafana/enterprise-logs/actions/runs/29027207589
  Dan will deploy the resulting image to preprod and A/B the three modes
  (mmap / buffered / streaming) via `index_reader_mode`.
- **2026-07-09 preprod signal — dev-us-central-0/loki-dev-006, image tag
  `custom--03209849f32f-752d9464fd99` (=commit `752d9464fd`), rollout
  ~15:40–15:43 UTC, `index_reader_mode: streaming`:**
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
- **2026-07-09**: Second custom enterprise-logs image build with the C0
  batch-read fix in progress:
  https://github.com/grafana/enterprise-logs/actions/runs/29031701500
  Once deployed, compare GetChunkRef / GetShards p50/p99 to the
  15:44 UTC → 15:48 UTC steady-state numbers documented above. The image
  tag suffix will match commit `0ccc630a16`.
- **2026-07-10 preprod signal — dev-us-central-0/loki-dev-006, image tag
  `custom--1e8b1b105257-0ccc630a16a1` (=commit `0ccc630a16`), rollout
  ~08:50–09:10 UTC, primary IG on `index_reader_mode: streaming`, shadow
  on a separate `shadow-index-gateway` statefulset via the tee framework.
  All three primary pods (`index-gateway-0/1/2`) and three shadow pods
  (`shadow-index-gateway-0/1/2`) healthy post-rollout, no further restarts,
  no errors on either client (`loki_index_gateway_tee_request_duration_seconds{status="success"}`
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
  queries (i.e. without a `container` filter) are misleading in this
  deployment: they mix both statefulsets, and the shadow's tail dominates
  the numbers. When querying the primary IG's own view, filter
  `container="index-gateway"`; when querying the shadow's, filter
  `container="shadow-index-gateway"`.
