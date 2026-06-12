# Loki → wiresmith migration — status, issues and incompatibilities

Status of generating Loki's protobuf Go code with
[wiresmith](https://github.com/grafana/wiresmith) instead of the current
`protoc --gogoslick_out` toolchain. **Updated 2026-06-12** against the public,
go-installable wiresmith `v0.0.0-20260611164808-4f41063d76a2` (`origin/main`
@ `4f41063`). `go.mod` pins that published pseudo-version directly — no
`replace`, no `GOPRIVATE`/`insteadOf` (the repo is public). The committed
`.pb.go` were regenerated against this post-zlce compiler: the O(n²)
pre-scan merge-unmarshal fix (#134, the Option-A grow-block swap) now applies,
changing the unmarshal pre-reservation block of every message with repeated
fields. The change is functionally gogo-equivalent (it only pre-reserves when
the target slice is empty; populated slices fall back to amortized append) and
benchmarks confirm parity (see below).

Phase 1 (previous revision of this doc) migrated 8 protos and identified
blockers W1–W4. The `databases` branch fixed W1 (`-M` go_package exemption),
W3 (unused import after customtype) and mitigated W4 (`XXX_` bitmap rename +
`no_presence`); phase 2 regenerated everything, migrated the isolated leaf
protos and the entire logproto-rooted cluster, and found N1–N4. Phase 3
migrated the queryrange/scheduler/frontend-v1+v2/ruler/bloombuild/compactor-
grpc/checkpoint cluster; the `databases` branch had since gained a
**value-getter** mode that downgrades N2 from a blocker to a non-issue (see
N2 below).

---

## TL;DR

- **The vast majority of Loki-authored protos are now wiresmith-generated**,
  including Loki's core wire types (`logproto`, `resultscache`, `sketch`,
  `metrics`, `pattern`, `bloomgateway`, `ingester-rf1`) and the entire
  queryrange/scheduler/frontend/ruler/bloombuild/compactor-grpc/checkpoint
  cluster, spanning all of Loki's gRPC service suites. Full repo builds;
  `go vet ./...` clean (3 pre-existing unrelated nits); migrated-cluster
  `go test -count=1` suites green.
- `pkg/push` deliberately **stays on gogo**: with the fixed `-M`, push.proto
  is staged import-only and pinned
  (`-M pkg/push/push.proto=github.com/grafana/loki/pkg/push`), so the
  standalone `pkg/push` Go module takes **no wiresmith dependency** — its
  only change is plain-Go `*Wiresmith` adapter methods on `Stream` /
  `LabelAdapter`. External push-API clients are unaffected.
- All migrated protos use `(wiresmith.options.no_presence_all)`: the
  exported `XXX_fieldsPresent` bitmap carries no `json:"-"` tag and leaks
  into `encoding/json` output (N1), and Loki's consumers are built around
  gogo `nullable=false` struct parity anyway.
- Remaining gogo protos: `push`/`push-rf1` (deliberate — see below) and the
  vendored Thanos/dskit protos (stay gogo regardless). `indexgateway.proto`
  is now wiresmith-generated — N3 was resolved upstream (two proto packages,
  `logproto` and `indexgatewaypb`, may share the `pkg/logproto` Go package via
  `-M`). The queryrange cluster is migrated.

---

## Migration status

Branch `wiresmith`. Phase-2 commits: `efea73e6fb` (toolchain + no_presence
regeneration), `c953e4507d` (leaf protos), `f3e71adab2` (logproto cluster).
Phase 3 is a single commit migrating the queryrange cluster. All listed
packages and their direct consumers pass `go test`.

### Generation model

`make wiresmith-protos` (wired into `make protos`) runs one wiresmith
invocation per **generation group**, each with a disjoint temp staging tree
mirroring repo paths. Groups exist because wiresmith enforces go_package
consistency across its whole `--proto_path` walk; `-M`-pinned files are
exempt (new), which the `logproto` group uses for `push.proto`. Each phase-3
group stages its already-migrated dependency protos (logproto family, stats,
resultscache, deletionproto, httpgrpcpb) **import-only** for resolution and
`-M`-pins its own outputs (and `push.proto`, plus `google.rpc.Status` where
reachable) so only the group's own files are emitted. `make wiresmith-protos`
is reproducible: a fresh run reproduces all generated files byte-identically.

| Group | Protos | Notes |
|---|---|---|
| engine | ulid, expressionpb, physicalpb, wirepb, compactionv2 | customtype ULID, stdtime/stdduration, local `Header` + `HeaderAdapter` bridging gogo `httpgrpc.Header` |
| logqlstats | logqlmodel/stats | jsontag ×84; JSON golden test pins HTTP-API shape (`testdata/result_zero.json`) |
| querierstats | querier/stats | stdduration |
| limits | limits/proto | 2 gRPC services; pointer=true keeps `[]*T` internals |
| leaves | xcap, datasetmd, filemd, metastore, deletionproto | casttype `model.Time`/`DeleteRequestStatus`; map nil-sentinel → `Chunk.IsZero()`; datasetmd/filemd pointer=true for parity across ~45 consumer files |
| compaction | dataobj/compaction/proto | pointer=true on message fields |
| logproto | logproto, metrics, pattern, bloomgateway, ingester-rf1, sketch, resultscache types + test_types | see below |
| httpgrpc | util/httpgrpcpb | wire-identical local copy of dskit `httpgrpc.HTTPRequest/Response/Header` (see queryrange cluster below) |
| bloombuild | bloombuild/protos types+service | stages logproto/stats/resultscache import-only |
| compactorgrpc | compactor/client/grpc | `Compactor`+`JobQueue` services; stages deletionproto import-only |
| checkpoint | ingester/checkpoint | stages logproto/stats/resultscache import-only; `-M` checkpoint→`pkg/ingester` |
| ruler | rulespb/rules, ruler/base/ruler | `LabelAdapter` customtype; `AnyAdapter` for `RuleGroupDesc.Options` (W2); `UnimplementedRulerServer` |
| queryrange | queryrangebase/definitions, queryrangebase/queryrange, queryrange/queryrange | phase-3 centerpiece — see below |
| scheduler | scheduler/schedulerpb | stages httpgrpcpb + the queryrange chain import-only; `-M` for `google.rpc` |
| frontend | frontend v1+v2 | uses `querier/stats` (so split from queryrange, which uses `logqlmodel/stats`); `logqlmodel/stats` `-M`-pinned for the transitive queryrange import |

### The logproto cluster (phase 2 centerpiece)

- **push.proto** import-only + `-M`-pinned (above). **push-rf1.proto is
  excluded from the staging tree**: it duplicates
  `service logproto.PusherRF1` with `ingester-rf1.proto` (a repo bug masked
  by gogo's per-file compilation); both stay generatable because only one of
  them is ever in a wiresmith walk.
- **indexgateway.proto is wiresmith-generated** (N3 resolved): proto packages
  `logproto` and `indexgatewaypb` share the `pkg/logproto` directory / Go
  package, and wiresmith's `-M` handling now allows two proto packages to claim
  one Go import path (matching protoc). It is generated in the `logproto`
  group; `indexgateway.Gateway` embeds `UnimplementedIndexGatewayServer`.
- **Customtype adapters** (each ~40 lines delegating to existing gogo-style
  implementations): `push.Stream`, `push.LabelAdapter`, `plan.QueryPlan`
  (with a nil-AST guard in `SizeWiresmith` — gogo never called `Size()` on
  a nil pointer field; wiresmith calls it on the value, and
  `QueryPlan.Size()` panics on nil AST — caught by `Test_DedupeIngester`),
  `syntax.LineFilter`, `logproto.PreallocTimeseries` (pooled unmarshal
  preserved), `resultscache.AnyAdapter` (bridges gogo `types.Any` for
  `Extent.response`).
- **casttype**: `model.Time` (×20), `model.Fingerprint`,
  `DetectedFieldType`.
- **Shape strategy**: `pointer = true` on every previously-unannotated
  message field (gogo `*T`/`[]*T` parity — zero consumer churn); forced
  value-shape changes only where wiresmith has no pointer form:
  - customtype singulars: `TailResponse.Stream` (`*push.Stream` →
    `push.Stream`), `Query/SampleQuery/TailRequest.Plan` (`*QueryPlan` →
    `QueryPlan`; `req.Plan == nil` → `req.Plan.AST == nil` at ~15 sites);
  - stdtime: `LabelRequest.Start/End` (`*time.Time` → `time.Time`,
    `IsZero()` for absence);
  - map message values: `LabelToValuesResponse.Labels`
    (`map[string]*UniqueLabelValues` → value map).
- **Getter-shape workaround** (N2): `VolumeRequest.cachingOptions` and
  `MockRequest.cachingOptions` renamed via `customname` to `CachingOpts`,
  freeing `GetCachingOptions` for hand-written **value** getters that keep
  satisfying the gogo-era `definitions.Request` / `resultscache.Request`
  interfaces.
- **Enum prefixes**: `FORWARD` → `Direction_FORWARD`, `RULE` →
  `WriteRequest_RULE` (~85 files, mechanical).
- **gRPC servers** embed generated `Unimplemented*Server`s: ingester
  (Querier, StreamData), pattern ingester (Pattern), bloom gateway
  (BloomGateway) — plus limits from phase 1.
- **gogo registry interop**: resultscache mock types are
  `proto.RegisterType`-ed into the gogo registry so gogo
  `types.MarshalAny/UnmarshalAny` (TypeUrl-based) still resolve them (N4).

---

## The queryrange cluster (phase 3)

Migrated: `queryrangebase/definitions`, `queryrangebase/queryrange`,
`queryrange/queryrange`, `scheduler/schedulerpb`, `frontend v1`/`v2`,
`ruler/rulespb` + `ruler/base/ruler`, `bloombuild/protos`,
`compactor/client/grpc`, `ingester/checkpoint`.

- **N2 solved by value getters (no customname needed).** The `databases`
  branch now emits **value** getters for value-shaped fields under
  `no_presence`, so the generated code directly satisfies the gogo
  `nullable=false` `Request`/`Response` interfaces: e.g.
  `LokiResponse.GetStatistics() stats.Result`, `LokiRequest.GetPlan()
  plan.QueryPlan`, `QueryResponse.GetStatus() RPCStatusAdapter` are all
  value-returning. This is the change that unblocked the whole cluster — the
  phase-2 `customname`+hand-written-getter workaround (VolumeRequest /
  MockRequest) was **not** needed here. A few hand-written value getters
  remain for stdtime fields where the field name differs from the getter
  (`LokiRequest.GetStart/GetEnd() time.Time` over `StartTs/EndTs`,
  `codec.go`).
- **QueryPlan customtype is value-shaped.** `Query/Instant/SampleQuery.plan`
  uses `customtype = plan.QueryPlan` with no `pointer`, so the field is a
  value `plan.QueryPlan` (was `*queryrange.Plan`); absence is `req.Plan.AST
  == nil`. Same nil-AST `Size()` guard as the logproto-cluster
  `plan.QueryPlan` adapter applies.
- **`RPCStatusAdapter`** (`rpc_status_adapter.go`): `QueryResponse.status`
  is wire-declared `google.rpc.Status` (a gogo-generated vendored type) but
  carries this customtype value bridge in Go — wiresmith cannot embed
  another runtime's messages (W2). Mirrors `resultscache.AnyAdapter`;
  delegates Size/Marshal/Unmarshal/Equal/Compare to gogo `rpc.Status`. A
  zero adapter `.Status()` yields nil for old `*rpc.Status` parity. The
  `google.rpc.Status` proto is staged with its go_package rewritten to the
  gogo import path and `-M`-pinned (see Makefile `status.proto` sed).
- **`rulespb.AnyAdapter`** (`any_adapter.go`): same pattern for
  `RuleGroupDesc.Options` (`repeated google.protobuf.Any`), bridging gogo
  `types.Any`.
- **`util/httpgrpcpb`**: wiresmith-generated, wire-identical local copies of
  dskit's `httpgrpc.HTTPRequest/HTTPResponse/Header`. The vendored dskit
  protos are gogo and cannot be embedded by wiresmith code, and oneof
  variants cannot use customtype bridges — so the queryrange/scheduler/
  frontend protos reference these copies. `convert.go` has
  `From*/To*` helpers; `pkg/util/httpgrpc/carrier.go` converts at the
  boundary (`Request.GetHttpRequest()` now returns `*httpgrpcpb.HTTPRequest`).
- **gogo registry interop (N4):** `gogo_registry.go` (in `queryrange` and
  `queryrangebase`) `proto.RegisterType`-s the migrated response/request
  message types under their original gogo FQNs, so the results cache's gogo
  `types.Any` (`Marshal/UnmarshalAny`, TypeUrl-based) still resolves them.
- **gRPC servers** embed the generated `Unimplemented*Server` (modern
  grpc-go `mustEmbed*` contract): `GRPCRequestHandler`
  (`UnimplementedCompactorServer`), `jobqueue.Queue`
  (`UnimplementedJobQueueServer`), `ruler.Ruler` (`UnimplementedRulerServer`),
  `bloombuild planner.Planner` (`UnimplementedPlannerForBuilderServer`).

---

## Remaining wiresmith blockers / gaps (ranked)

### N1 — `XXX_fieldsPresent` has no `json:"-"` tag

- **Evidence:** after the `databases`-branch rename, the stats JSON golden
  test failed with `"XXX_fieldsPresent":[0]` in every message of the
  `stats.Result` HTTP-API payload; `EqualValues` tests also see it.
- **Impact:** any wiresmith struct serialized with `encoding/json` leaks
  the bitmap. Loki serializes proto structs to JSON in its public API
  (stats, deletion API, volume).
- **Workaround:** `no_presence_all` everywhere (also restores
  `reflect.DeepEqual` parity). That forfeits `Has*`/presence round-trip,
  which Loki doesn't use.
- **Suggested fix:** emit `json:"-"` on the bitmap field.
- **Severity:** major if presence is wanted alongside JSON; for Loki,
  neutralized by no_presence.

### N2 — message getters always return pointers (RESOLVED upstream)

- **Evidence (phase 2):** `*VolumeRequest` stopped satisfying
  `definitions.Request` (`have GetCachingOptions()
  *resultscache.CachingOptions, want ... CachingOptions`). Under
  `no_presence` the getter was `&m.Field` unconditionally.
- **Resolution:** wiresmith now emits **value** getters for value-shaped
  fields under `no_presence`. The queryrange cluster relies on this:
  `GetStatistics() stats.Result`, `GetPlan() plan.QueryPlan`,
  `GetStatus() RPCStatusAdapter` are generated as value getters and satisfy
  the gogo `nullable=false` `Request`/`Response` interfaces with **no**
  `customname` renames. The earlier `customname`+hand-written-getter
  workaround (`VolumeRequest`, `MockRequest`) was removed in commit
  `b60e3d265d`.
- **Severity:** resolved; was previously the top blocker for queryrange.

### N3 — one Go import path cannot host two proto packages (RESOLVED upstream)

- **Original evidence:**
  `error: import path "github.com/grafana/loki/v3/pkg/logproto" is claimed
  by both proto packages "logproto" and "indexgatewaypb"` — the -M exemption
  used to cover go_package *value* disagreement only, not the
  path-claimed-twice check.
- **Resolution:** wiresmith now extends the `-M` exemption to the
  import-path-claim check (one directory, one Go package, N proto packages —
  matching protoc). `indexgateway.proto` migrated as part of the `logproto`
  group; `indexgateway.Gateway` embeds `UnimplementedIndexGatewayServer`.
  Confirmed against `v0.0.0-20260611164808-4f41063d76a2`: the `logproto`
  group regenerates `indexgateway.pb.go` cleanly and the repo builds.
- **Severity:** resolved.

### N4 — wiresmith types are invisible to the gogo registry

- **Evidence:** `types.MarshalAny(mockResponse)` → `any: message type ""
  isn't linked in` (gogo `proto.MessageName` finds nothing); resultscache
  tests failed until the mocks were `proto.RegisterType`-ed manually.
- **Impact:** any TypeUrl/reflection path through gogo (`types.*Any`,
  `jsonpb`, `proto.MessageType`) breaks for migrated messages. The
  queryrange phase hit this directly: results-cache extents store responses
  as gogo `types.Any`.
- **Workaround (used in phase 3):** hand-written `init()` registration in
  `gogo_registry.go` (`queryrange` and `queryrangebase`), names matching the
  old gogo FQN.
- **Suggested fix:** optional gogo-registry registration in generated code,
  or a documented helper.
- **Severity:** moderate; handled per-package by the registry shim.

### Carried over, still relevant

- **W2 (by design):** wiresmith messages cannot embed messages generated by
  other runtimes — forces leaf-first ordering and customtype envelopes
  (`HeaderAdapter`, `resultscache.AnyAdapter`, `rulespb.AnyAdapter`,
  `RPCStatusAdapter`). For dskit `httpgrpc` (oneof variants, which cannot use
  customtype) phase 3 generated a wire-identical local copy
  (`util/httpgrpcpb`) with boundary conversions instead.
- **stdtime/stdduration are value-only** — `*time.Time` fields
  (`LabelRequest`) forced into value shape; semantic change (zero time vs
  nil) absorbed at call sites.
- **map message values are value-only** (no pointer option) — nil-entry
  sentinels must become zero-value sentinels (`deletionproto.Chunk.IsZero`).
- **No `GoString`** — gogoslick callers need shims for embedded wiresmith
  values (`stats`, `resultscache`).
- **Enum constants always prefixed** (no `goproto_enum_prefix` toggle) —
  mechanical but large renames (~130 files total so far).

### Workaround review against the public `4f41063` feature set (2026-06-12)

Every documented shim/bridge was re-assessed against the now-shipped features
(`-M` transitive-import fix, customtype on message fields, casttype,
stdduration, customname, `Has<F>()`, no_presence). Decisions:

- **Removed earlier (commit `b60e3d265d`, kept removed):** the N2
  `customname`+hand-written-getter workaround (`CachingOpts` on
  `VolumeRequest`/`MockRequest`) — superseded by value getters under
  `no_presence`; and the N3 indexgateway-stays-gogo arrangement — `-M` now
  allows two proto packages per Go package, so `indexgateway.proto` is
  wiresmith-generated.
- **Kept (no shipped feature obviates them):**
  - `gogo_registry.go` (N4) — `proto.RegisterType` is a gogo *runtime
    registry* concern; no codegen feature registers wiresmith types with
    gogo. Required while the results-cache `types.Any` path stays gogo.
  - `RPCStatusAdapter`, `rulespb.AnyAdapter`, `resultscache.AnyAdapter`,
    `wirepb.HeaderAdapter` (W2) — bridges to foreign-runtime *gogo* messages
    (`google.rpc.Status`, `types.Any`, `httpgrpc.Header`). customtype-on-
    message (#117) is what *enables* these bridges; it does not let wiresmith
    embed another runtime's generated type.
  - `util/httpgrpcpb` (W2) — dskit `httpgrpc` uses oneof variants, which
    cannot carry a customtype bridge; the wire-identical local copy stays.
  - `no_presence_all` (N1) — kept for gogo `nullable=false` struct /
    `reflect.DeepEqual` parity and to avoid the `XXX_fieldsPresent` JSON leak;
    a `json:"-"` codegen fix would not change Loki's need for value-shape
    parity. Confirmed: no `XXX_fieldsPresent` appears in any generated file.
  - GoString shims (`stats`, `resultscache`) — gogoslick callers in still-gogo
    packages embed these by value; wiresmith emits no `GoString`.
  - `pkg/push` `-M` staging — push stays gogo by decision; the
    `-M pkg/push/push.proto=github.com/grafana/loki/pkg/push` pin is required
    in every group that emits files importing push (so the standalone module's
    no-`/v3` import path is honored). The #133 transitive-`-M` fix guarantees
    the pin is honored but does not remove the need to pass it per group.

---

## Is full gogoproto removal from Loki's codegen path in reach?

**Very close.** The queryrange cluster is done and N2/N3 are both resolved;
what's left:

1. **push/push-rf1**: gated on a product decision (wiresmith dep in the
   standalone module, or keep the current import-only arrangement forever,
   which works fine).
2. The `service PusherRF1` duplication and the vendored `push.proto` copy
   should be fixed in Loki regardless.
3. The vendored Thanos/dskit protos stay gogo regardless — **gogo can leave
   Loki's protoc pipeline, but not Loki's go.mod**.

Every Loki-authored proto except `push`/`push-rf1` is now
wiresmith-generated; with the `push` decision made, the `--gogoslick_out`
pipeline reduces to the vendored Thanos/dskit protos. N2 was resolved by the
value-getter mode and N3 by the multi-proto-package-per-Go-package `-M`
handling.

---

## Test evidence

- `go build ./...`: clean (re-verified 2026-06-12 post-zlce regen).
  `go vet ./...`: clean except 3 pre-existing unrelated nits
  (metastore_test context-leak, logql engine_test json-tag-repeat from
  vendored push, ruler/registry.go unkeyed-fields).
- 2026-06-12 post-zlce re-verification: migrated-cluster suites
  (`logproto`, `logqlmodel/stats`, `querier/stats`, `limits/proto`,
  `resultscache`, `engine/internal/proto`, `dataobj/...`, `deletionproto`,
  `httpgrpcpb`, `querier/queryrange/...`, `scheduler`, `ruler/...`,
  `lokifrontend/...`, `compactor/client/grpc`, `bloombuild/...`,
  `ingester/...`, `querier/...`, `util/httpgrpc`, `loghttp`, `logcli`)
  `go test -count=1`: green. `pkg/push` module `go test ./...`: green,
  `go.mod` untouched.
- Phase-2 full sweep `go test -count=1 ./pkg/...`: green.
- Phase-3 migrated-cluster suites `go test -count=1`: green (queryrange/...,
  scheduler, ruler/..., frontend v1+v2, compactor/..., bloombuild/...,
  querier/worker, querytee, ingester).
- `pkg/push`: `go test ./...` inside the module: green; `go.mod` untouched.
- stats JSON golden (`pkg/logqlmodel/stats/json_test.go`): byte-identical
  output vs gogo; it caught both the jsontag contract (phase 1) and the N1
  bitmap leak (phase 2).
- Cross-runtime direction (gogo embedding wiresmith) exercised heavily:
  `queryrange`, `indexgateway`, frontend backwards-compat fixture
  (`testdata/k173.bin`), scheduler wire codec round-trips.

## Reproduction

- Regenerate: `make wiresmith-protos BUILD_IN_CONTAINER=false` (the public
  `github.com/grafana/wiresmith@4f41063` binary on PATH —
  `go install github.com/grafana/wiresmith@v0.0.0-20260611164808-4f41063d76a2`).
  Reproducible — byte-identical output across runs (verified twice on
  2026-06-12).
- The gogo pipeline (`make protos`) still generates the remaining gogo protos
  (push/push-rf1, indexgateway, vendored Thanos/dskit) and excludes the
  migrated ones via `WIRESMITH_PROTO_DEFS`.

## Benchmarks (Apple M4 Pro, benchstat-grade — DB-9, re-run 2026-06-12 post-zlce)

gogo baseline (`d62c5906a9`, the branch merge-base) vs the wiresmith branch
regenerated against `4f41063` (post-zlce pre-scan fix). Method: two
`go test -c` binaries **alternated** 20 rounds; benchstat.

| Bench | pkg | Result |
|---|---|---|
| `Benchmark_DecodeMergeEncodeCycle` | querier/queryrange | **parity** — time p=0.096, B/op p=0.703, allocs p=0.752 (all non-significant; B/op 435 MiB & allocs 102.8k byte-identical) |
| `BenchmarkMerge{A,Some,Many}{Label,Series}Response` (×6) | logproto | **parity** — every metric non-significant (time geomean −0.36%, p≥0.28); B/op and allocs byte-identical |

The post-zlce regen holds Loki at the same **straight parity** recorded
pre-zlce — no regression, no win. The Option-A pre-scan swap only affects
reused/non-reset-message merge-unmarshal paths, which neither of these benches
exercises (`DecodeMergeEncodeCycle` decodes into fresh messages; the
`logproto` `Merge*` benches are ns-scale merge logic). Two reasons the
proto path is invisible here: `DecodeMergeEncodeCycle` decodes each response
into a *fresh* message (no reuse → no pre-scan accumulation, unlike tempo's
`EncodeDecode`) and is dominated by the trailing JSON re-encode (~435 MiB/op);
the `logproto` `Merge*` benchmarks are ns-scale merge logic that barely touches
the wire codec. Loki's migrated protos lean on compat annotations (105
`pointer`, 34 `customtype`, 32 `casttype`, 53 `stdtime`, 36 `no_presence_all`);
of these only `pointer` on *repeated* message fields (`[]*T`→`[]T`) is a real
latency/alloc lever, and its payoff is on decode-heavy paths not exercised by
these benchmarks (customtype/casttype are perf-*preserving*; stdtime/enum/
no_presence are API-only). Not pursued under DB-9.
