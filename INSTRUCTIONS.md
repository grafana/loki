# Fix: classify wrapped LogQL query errors as 400 instead of 500

## Problem

A Grafana Cloud SLO alert fired — **"Loki: Query API Error Rate - Error Budget
Burn Rate is High"** on cell `loki-prod-036` (`prod-us-east-2`). Investigation
traced the burn to a single tenant (`1295924`) issuing instant metric queries
from alerting rules (e.g. `GR1-Logs-SystemOut-log-monitoring-Critical`) whose
LogQL contained an **invalid regex** — a mis-escaped `\)` that closes a
`(?:...)` group, e.g.:

```
(?:ADML0063W: The tool cannot contact server \)
```

These queries failed in ~5–8ms with a LogQL **parse error**, which is a client
(400) error. But the failure was being surfaced as a **500**, which:

1. Polluted the **server-side** Query API error-rate SLO with what was really a
   client error.
2. Caused the query-frontend **retry middleware to retry it 3×**, logging
   `received an error but not a retryable code, this is possibly a bug`, and
   inflating the request's total latency to ~13s.

## Root cause

The parse error reaches the querier→frontend boundary already wrapped in a gRPC
status carrying a non-HTTP code (`codes.Unknown`). Three places inspected the
gRPC status **before** the typed error, so the client error was mapped to 500:

- `pkg/querier/worker/util.go` — `handleQueryRequest` returned the pre-existing
  gRPC status verbatim (`status.FromError(err)` → `s.Proto()`), bypassing
  `ClientHTTPStatusAndError`.
- `pkg/util/server/error.go` — `ClientHTTPStatusAndError` ran the
  `status.FromError` branch (which returns 500 for non-HTTP codes) before the
  typed-error checks.
- `pkg/util/server/error.go` — `WrapError` returned the existing gRPC status
  before consulting `ClientHTTPStatusAndError`.

The retry middleware (`pkg/querier/queryrange/queryrangebase/retry.go`) was
correct: it retries `code < 100 || code/100 == 5`. The bug was upstream
classification — a `codes.Unknown` (2) `< 100` got retried.

## Fix

Detect typed Loki client errors **before** inspecting the gRPC status, via a
shared helper:

- Added `isClientError(err, *QueryError, *UserError) bool` in
  `pkg/util/server/error.go`, covering: `QueryError`, `UserError`, `ErrLimit`,
  `ErrParse`, `ErrPipeline`, `ErrBlocked`, `ErrParseMatchers`,
  `ErrUnsupportedSyntaxForInstantQuery`, `ErrVariantsDisabled`, `ErrNoOrgID`.
- `ClientHTTPStatusAndError`: check `isClientError` (and the MultiError loop)
  before the `status.FromError` branch.
- `WrapError`: skip the verbatim gRPC-status passthrough when the error is a
  client error.
- `handleQueryRequest`: always route handler errors through
  `QueryResponseWrapError` (which uses `ClientHTTPStatusAndError`) instead of
  returning a pre-existing gRPC status verbatim.

Genuine gRPC statuses (e.g. `codes.Canceled` from connection-pool teardown)
keep their existing mapping.

## Tests

TDD — failing tests added first, then the fix:

- `pkg/util/server/error_test.go`: `parse error wrapped in grpc status`
  (covers `WriteError` and the `WrapError`→`UnwrapError` roundtrip).
- `pkg/querier/worker/util_test.go`: `parser error wrapped in a gRPC status`
  (covers the gRPC query path).

Both use a small `grpcStatus*Err` helper type that implements `GRPCStatus()`
(so `status.FromError` returns ok=true) while still unwrapping to the typed
error (so `errors.Is` works) — modeling the real wrapped-error case.

The existing retry test already proves code 400 → 1 call (no retry), so the
chain is verified end-to-end across suites.

## Verifying locally

The default CGO build fails in this environment with a nix-clang `-lresolv`
linker error (toolchain issue, not code). Run tests with CGO disabled:

```bash
CGO_ENABLED=0 go test ./pkg/util/server/ ./pkg/querier/worker/ ./pkg/querier/queryrange/ -count=1
```

All three suites pass; `gofmt` and `go vet` are clean.

## Out of scope (operational follow-up)

- Tenant `1295924` should fix the invalid regex in their alerting rules.
- The SLO's `runbook_url` annotation anchor is broken
  (`#loki-query-api-error-rate` vs the real
  `#loki-query-api-error-rate---slo-burn-rate-very-high`).
