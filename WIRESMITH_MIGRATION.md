# Loki → wiresmith migration — status, issues and incompatibilities

Status of generating Loki's protobuf Go code with
[wiresmith](https://github.com/grafana/wiresmith) instead of the current
`protoc --gogoslick_out` toolchain. **Updated 2026-06-10 (phase 2)** against
the wiresmith `databases` branch (commit 9407011, local worktree at
`../wiresmith-databases`; `go.mod` `replace` points there).

Phase 1 (previous revision of this doc) migrated 8 protos and identified
blockers W1–W4. The `databases` branch fixed W1 (`-M` go_package exemption),
W3 (unused import after customtype) and mitigated W4 (`XXX_` bitmap rename +
`no_presence`); this phase regenerated everything, migrated the isolated
leaf protos and the entire logproto-rooted cluster, and found three new
issues (N1–N3 below).

---

## TL;DR

- **22 of 38 protos are now wiresmith-generated**, including Loki's core
  wire types (`logproto`, `resultscache`, `sketch`, `metrics`, `pattern`,
  `bloomgateway`, `ingester-rf1`) and four gRPC service suites. Full repo
  builds; `go vet ./...` clean; full `go test -count=1 ./pkg/...` green.
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
- Remaining gogo protos (16): `push`/`push-rf1` (deliberate),
  `indexgateway` (N3 blocker), and the queryrange/scheduler/frontend/ruler/
  bloombuild/checkpoint/compactor-grpc cluster (next phase; no known hard
  blocker except per-case items listed under "Path to full removal").

---

## Migration status

Branch `wiresmith`. Phase-2 commits: `efea73e6fb` (toolchain + no_presence
regeneration), `c953e4507d` (leaf protos), `f3e71adab2` (logproto cluster).
All listed packages and their direct consumers pass `go test`.

### Generation model

`make wiresmith-protos` (wired into `make protos`) runs one wiresmith
invocation per **generation group**, each with a disjoint temp staging tree
mirroring repo paths. Groups exist because wiresmith enforces go_package
consistency across its whole `--proto_path` walk; `-M`-pinned files are
exempt (new), which the `logproto` group uses for `push.proto`.

| Group | Protos | Notes |
|---|---|---|
| engine | ulid, expressionpb, physicalpb, wirepb, compactionv2 | customtype ULID, stdtime/stdduration, local `Header` + `HeaderAdapter` bridging gogo `httpgrpc.Header` |
| logqlstats | logqlmodel/stats | jsontag ×84; JSON golden test pins HTTP-API shape (`testdata/result_zero.json`) |
| querierstats | querier/stats | stdduration |
| limits | limits/proto | 2 gRPC services; pointer=true keeps `[]*T` internals |
| leaves | xcap, datasetmd, filemd, metastore, deletionproto | casttype `model.Time`/`DeleteRequestStatus`; map nil-sentinel → `Chunk.IsZero()`; datasetmd/filemd pointer=true for parity across ~45 consumer files |
| compaction | dataobj/compaction/proto | pointer=true on message fields |
| logproto | logproto, metrics, pattern, bloomgateway, ingester-rf1, sketch, resultscache types + test_types | see below |

### The logproto cluster (phase 2 centerpiece)

- **push.proto** import-only + `-M`-pinned (above). **push-rf1.proto is
  excluded from the staging tree**: it duplicates
  `service logproto.PusherRF1` with `ingester-rf1.proto` (a repo bug masked
  by gogo's per-file compilation); both stay generatable because only one of
  them is ever in a wiresmith walk.
- **indexgateway.proto stays gogo** (N3). Mixed generation inside one Go
  package works: the gogo file embeds wiresmith types and calls
  `Size/Marshal*/Unmarshal/Equal/GoString` on them.
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

### N2 — message getters always return pointers; gogo `nullable=false` getters returned values

- **Evidence:** `*VolumeRequest` stopped satisfying `definitions.Request`
  (`have GetCachingOptions() *resultscache.CachingOptions, want ...
  CachingOptions`). Under `no_presence` the getter is `&m.Field`
  unconditionally; there is no value-getter mode.
- **Impact:** every getter-shaped interface spanning gogo and wiresmith
  implementations breaks. Hit twice (VolumeRequest, MockRequest); will
  recur for the queryrange `Request`/`Response` interfaces in the next
  phase (e.g. `GetStatistics() stats.Result`, implemented today by gogo
  value getters on ~10 response types).
- **Workaround:** `customname` the field, hand-write the value getter.
- **Suggested fix:** a `value_getters`/gogo-parity option (at least under
  `no_presence`, where nil-signaling via the getter is meaningless anyway).
- **Severity:** **top remaining blocker** for the queryrange phase — the
  workaround is per-field and pollutes the public Go API with renamed
  fields.

### N3 — one Go import path cannot host two proto packages

- **Evidence:**
  `error: import path "github.com/grafana/loki/v3/pkg/logproto" is claimed
  by both proto packages "logproto" and "indexgatewaypb"` — even with
  `-M pkg/logproto/indexgateway.proto=...` (the -M exemption covers
  go_package *value* disagreement, not the path-claimed-twice check).
  protoc allows this layout and Loki ships it today.
- **Impact:** `indexgateway.proto` cannot migrate. Renaming its proto
  package would change gRPC method paths (wire-breaking); moving its Go
  output would break every consumer import.
- **Workaround:** leave it gogo (works fine in the mixed package).
- **Suggested fix:** extend the `-M` exemption to the import-path-claim
  check (one directory, one Go package, N proto packages — matches protoc).
- **Severity:** blocker for 1 proto today; the pattern may recur in other
  consumers.

### N4 — wiresmith types are invisible to the gogo registry

- **Evidence:** `types.MarshalAny(mockResponse)` → `any: message type ""
  isn't linked in` (gogo `proto.MessageName` finds nothing); resultscache
  tests failed until the mocks were `proto.RegisterType`-ed manually.
- **Impact:** any TypeUrl/reflection path through gogo (`types.*Any`,
  `jsonpb`, `proto.MessageType`) breaks for migrated messages. The
  queryrange phase hits this hard: cache extents store responses as `Any`.
- **Workaround:** hand-written `init()` registration (name must match the
  old gogo FQN).
- **Suggested fix:** optional gogo-registry registration in generated code,
  or a documented helper.
- **Severity:** moderate now, major for the queryrange phase.

### Carried over, still relevant

- **W2 (by design):** wiresmith messages cannot embed messages generated by
  other runtimes — forces leaf-first ordering and customtype envelopes
  (`HeaderAdapter`, `AnyAdapter`). Will bite again for
  `ruler/base/ruler.proto`, which embeds vendored Thanos/dskit gogo types.
- **stdtime/stdduration are value-only** — `*time.Time` fields
  (`LabelRequest`) forced into value shape; semantic change (zero time vs
  nil) absorbed at call sites.
- **map message values are value-only** (no pointer option) — nil-entry
  sentinels must become zero-value sentinels (`deletionproto.Chunk.IsZero`).
- **No `GoString`** — gogoslick callers need shims for embedded wiresmith
  values (`stats`, `resultscache`).
- **Enum constants always prefixed** (no `goproto_enum_prefix` toggle) —
  mechanical but large renames (~130 files total so far).

---

## Is full gogoproto removal from Loki's codegen path in reach?

**Close, but not yet.** What's left and what gates it:

1. **queryrange cluster** (`definitions`, `queryrangebase/queryrange`,
   `queryrange`, `scheduler`, `frontend v1/v2`, `bloombuild`, `checkpoint`,
   `compactor/grpc`): no hard generation blocker, but
   - N2 (pointer getters) hits the central `Request`/`Response` interfaces —
     migrate-and-rename works but is ugly at this scale; a wiresmith
     value-getter option would collapse the cost;
   - N4 (gogo Any registry) needs registration shims or switching the
     extent plumbing off gogo `types.Any`;
   - `queryrange.proto` embeds `LokiResponse` etc. with heavy custom JSON
     marshalers — a large but mechanical consumer surface.
2. **ruler.proto / rules.proto**: embed vendored gogo types (Thanos
   store-gateway, cortexpb) → W2; needs customtype envelopes per field.
   The vendored protos themselves (`thanos`, `dskit`) stay gogo regardless —
   **gogo can leave Loki's protoc pipeline, but not Loki's go.mod**.
3. **indexgateway.proto**: gated solely on N3.
4. **push/push-rf1**: gated on a product decision (wiresmith dep in the
   standalone module, or keep the current import-only arrangement forever,
   which works fine).
5. The `service PusherRF1` duplication and the vendored `push.proto` copy
   should be fixed in Loki regardless.

With N2 + N3 fixed in wiresmith and adapters for ruler's vendored types,
every Loki-authored proto could be wiresmith-generated and the
`--gogoslick_out` pipeline reduced to the vendored Thanos/dskit protos.

---

## Test evidence

- `go build ./...`, `go vet ./pkg/...` (also `cmd`, `tools`): clean.
- `go test -count=1 ./pkg/...` full sweep: green (after phase 2).
- `pkg/push`: `go test ./...` inside the module: green; `go.mod` untouched.
- stats JSON golden (`pkg/logqlmodel/stats/json_test.go`): byte-identical
  output vs gogo; it caught both the jsontag contract (phase 1) and the N1
  bitmap leak (phase 2).
- Cross-runtime direction (gogo embedding wiresmith) exercised heavily:
  `queryrange`, `indexgateway`, frontend backwards-compat fixture
  (`testdata/k173.bin`), scheduler wire codec round-trips.

## Reproduction

- Regenerate: `make wiresmith-protos BUILD_IN_CONTAINER=false` (wiresmith
  binary built from the `databases` branch on PATH).
- The gogo pipeline (`make protos`) still generates the remaining 16 protos
  and excludes the migrated ones via `WIRESMITH_PROTO_DEFS`.
