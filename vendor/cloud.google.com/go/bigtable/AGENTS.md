# Agent instructions for `bigtable/`

These instructions apply to any AI coding agent (Claude Code, GitHub
Copilot, Gemini Code Assist, Cursor, Aider, etc.) editing files under
`bigtable/`.

## When editing the Session subsystem

Before editing ANY file under:

- `bigtable/internal/transport/**` (all transport-layer code — session,
  channel pool, direct-access checker, primer, AFE picker, diverter,
  the connection factory, everything that speaks to the wire)
- `bigtable/internal/session/**`
- `bigtable/table_shim.go`
- `bigtable/debugview/**`
- `bigtable/session_*.go`

Read the specs under `bigtable/docs/specs/` that match the layer being
touched:

- **`SESSION_SPEC.md`** — one Session's lifecycle (10 invariants): state
  machine, one-in-flight vRPC, PeerInfo timing, hook ordering, close /
  GOAWAY behavior, heartbeat, retry oracle, concurrency.
- **`SESSION_CLIENT_SPEC.md`** — SessionClient topology (4 invariants):
  `Client`↔`SessionClient` 1:1, shared channel pool, lazy pool creation,
  `GetClientConfiguration` as authoritative config source,
  `OpenSessionRequest` envelope.
- **`SESSION_POOL_SPEC.md`** — pool + picking (5 invariants):
  read/write pool per resource, AFE picker (K-choice / PeakEwma /
  three impls), Diverter + TableShim routing, debug views MUST NOT
  block hot-path, server-driven scaling.
- **`CLIENT_SIDE_METRICS_SPEC.md`** — per-attempt metrics: how
  `cluster_id` / `zone_id` / transport peer are sourced differently on
  classic vs session data paths.
- **`SESSION_COMPONENT_SPEC.md`** — component topology and boundary
  rules (Part B: 12 boundary MUSTs; Part C: ownership matrix).

If a change would violate a rule, either the change is wrong, or the
rule is stale. In the latter case, update the spec in the same PR.

## Testing before commit

Run the module's smoke gate:

```
go test ./bigtable/internal/transport/ ./bigtable/internal/session/ \
        ./bigtable/debugview/... -count=1 -short -timeout=90s
```

The top-level `bigtable` package integration tests hang under `-short`
today (pre-existing; unrelated to session work), so keep the gate
scoped to the packages above unless working on classic-path code.

## Reviewers

Human reviewer on channel-pool / session / DirectPath PRs: **mutianf**.

Bot reviewer `gemini-code-assist[bot]` sometimes false-alarms on
package names for files under `bigtable/internal/transport/` (claims
`package internal` should be `package transport`); this is a known
false positive — ignore.
