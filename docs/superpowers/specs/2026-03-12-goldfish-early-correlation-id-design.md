# Goldfish Early Correlation ID Calculation

## Problem

The Goldfish correlation ID is currently generated inside `processQueryPair` (`pkg/querytee/goldfish/manager.go:173`), which runs asynchronously in a background goroutine after the HTTP response has already been sent to the client. This means the correlation ID cannot be returned to the caller in a response header.

Additionally, the reads path has a double-sampling bug: `SplittingHandler` and `FanOutHandler` each independently call `ShouldSample`, making the effective sampling rate `rate * rate` instead of `rate`.

## Goals

1. Generate the correlation ID at the point of the sampling decision, before the HTTP response is written.
2. Return the correlation ID in an `X-Loki-Goldfish-ID` response header on sampled requests at HTTP entrypoints that own `http.ResponseWriter` (i.e., `ProxyEndpoint` for writes, `SplittingHandler` for reads). When `FanOutHandler` is used standalone (tests), the ID is used for goldfish storage but cannot be set on the HTTP response.
3. Fix the double-sampling bug on the reads path so the effective rate matches the configured rate.

## Non-goals

- Changing the sampling algorithm itself.
- Setting the header on non-sampled requests.
- Waiting for goldfish processing to complete before sending the response (the header represents "this request was sampled", not "the goldfish record exists").

## Design

### Header constant

```go
const GoldfishCorrelationIDHeader = "X-Loki-Goldfish-ID"
```

Defined once in `pkg/querytee/goldfish/context.go` alongside the context helpers.

### Manager interface changes

The `Manager` interface in `pkg/querytee/goldfish/manager.go` changes two method signatures:

```go
// Before
type Manager interface {
    ShouldSample(tenantID string) bool
    SendToGoldfish(httpReq *http.Request, cellAResp, cellBResp *BackendResponse)
    Close() error
}

// After
type Manager interface {
    ShouldSample(tenantID string) (sampled bool, correlationID string)
    SendToGoldfish(httpReq *http.Request, cellAResp, cellBResp *BackendResponse, correlationID string)
    Close() error
}
```

When `ShouldSample` returns `sampled=true`, it also returns a freshly generated UUID. When `sampled=false`, `correlationID` is `""`.

`SendToGoldfish` accepts the pre-generated correlation ID and threads it to `processQueryPair`, which uses it instead of generating its own. `SendToGoldfish` must validate that `correlationID` is non-empty before proceeding; if empty, it logs a warning and drops the request rather than risking a storage failure (correlation ID is the primary key in the `sampled_queries` table).

### Context helpers for reads path

A new file `pkg/querytee/goldfish/context.go` provides context-based threading of the sampling decision from `SplittingHandler` to `FanOutHandler`.

The context carries an explicit sampling decision (not just a correlation ID) so that both "sampled" and "not sampled" outcomes are propagated. This prevents `FanOutHandler` from resampling a request that `SplittingHandler` already decided not to sample.

```go
type SamplingDecision struct {
    Sampled       bool
    CorrelationID string // non-empty only when Sampled is true
}

type contextKey int

const samplingDecisionKey contextKey = iota

func ContextWithSamplingDecision(ctx context.Context, decision SamplingDecision) context.Context {
    return context.WithValue(ctx, samplingDecisionKey, decision)
}

func SamplingDecisionFromContext(ctx context.Context) (SamplingDecision, bool) {
    decision, ok := ctx.Value(samplingDecisionKey).(SamplingDecision)
    return decision, ok
}
```

### Writes path (proxy_endpoint.go)

The writes path has a single sampling decision point with no downstream handler complications.

1. `serveWrites` calls `ShouldSample(tenantID)`, receiving `(sampled bool, correlationID string)`.
2. The header is set early, **before** the error/success branch diverges, so it appears on both successful and error responses. Specifically: after the sampling decision and before calling `w.WriteHeader` or `server.WriteError`. If auth or tenant extraction fails before the sampling decision, no header is set.
3. If sampled, the header is set **after** copying backend response headers (to avoid being overwritten by a backend header of the same name) and **before** `w.WriteHeader`.
4. `executeBackendRequests` signature changes to accept `correlationID string` (replacing or alongside `goldfishSample bool`). It passes the ID to `processWithGoldfish`.
5. `processWithGoldfish` signature changes to accept `correlationID string` and passes it to `SendToGoldfish`.

**Simplification:** The `goldfishSample bool` parameter/variable can be replaced by checking `correlationID != ""`, which collapses two parameters into one.

### Reads path (splitting_handler.go -> fanout_handler.go)

This path involves two handlers. The design makes `SplittingHandler`'s sampling decision authoritative and fixes the double-sampling bug.

**SplittingHandler.ServeHTTP:**

1. Checks for multi-tenant queries first. If multi-tenant, routes directly to v1 without sampling (move the multi-tenant check before the `shouldSample` call to avoid generating a wasted UUID).
2. For single-tenant queries, calls `shouldSample(tenants, r)`. The `shouldSample` helper signature changes from `func (f *SplittingHandler) shouldSample(tenants []string, r *http.Request) bool` to `func (f *SplittingHandler) shouldSample(tenants []string, r *http.Request) (bool, string)`. It iterates tenants and returns the correlation ID from the first tenant that `ShouldSample` returns `sampled=true` for. If no tenant is sampled, it returns `(false, "")`.
3. Stores the decision in request context regardless of outcome: `ctx = goldfish.ContextWithSamplingDecision(ctx, goldfish.SamplingDecision{Sampled: sampled, CorrelationID: correlationID})`. This ensures `FanOutHandler` sees both positive and negative decisions.
4. In `writeResponse`, extracts the sampling decision from context using `goldfish.SamplingDecisionFromContext(ctx)`. If the decision is sampled and the correlation ID is non-empty, sets `w.Header().Set(goldfish.GoldfishCorrelationIDHeader, correlationID)`. This extraction happens at the **top** of `writeResponse`, before any error/success branching, so the header is present regardless of the response path. If no sampling decision is found in context (e.g., multi-tenant queries), `writeResponse` does not set the header.

The context containing the sampling decision flows through the engine router middleware chain to `FanOutHandler`, as all middleware in the chain preserves the incoming context (standard Go `context.Context` contract).

**FanOutHandler.Do:**

1. Checks context first: `decision, hasDecision := goldfish.SamplingDecisionFromContext(ctx)`.
2. If `hasDecision` is true: uses `decision.Sampled` and `decision.CorrelationID`. Skips its own `ShouldSample` call entirely, whether the upstream decision was positive or negative.
3. If `hasDecision` is false (standalone use, tests): falls back to calling `ShouldSample` itself, receiving the tuple. The `shouldSample` helper signature changes similarly to `SplittingHandler`'s. In this case, the correlation ID is used for goldfish storage but cannot be set on the HTTP response (since `FanOutHandler` returns a `queryrangebase.Response`, not an `http.ResponseWriter`).
4. The correlation ID is threaded through the full internal call chain. Currently, `shouldSample bool` is passed through `doWithPreferred`/`doWithRacing` -> `finishRace` -> `collectRemainingAndCompare` -> `processGoldfishComparison` -> `sendToGoldfish` -> `SendToGoldfish`. The `shouldSample bool` parameter is replaced by `correlationID string` in all these functions (non-empty = sampled). This collapses two concepts into one parameter.

This fixes the double-sampling bug: the effective rate becomes `rate` instead of `rate * rate`.

### Goldfish comparison pairing

`processGoldfishComparison` (`fanout_handler.go:444`) currently loops over results and calls `sendToGoldfish` for each non-preferred backend. In practice, there are always exactly 2 backends (one preferred, one comparison), so only one comparison pair is produced per sampled request. A single correlation ID per request is correct for this deployment model.

The spec does not change this behavior. If >2 backends were ever supported, the pairing model would need revisiting (per-pair IDs or a different storage key), but that is out of scope.

### processQueryPair changes

`processQueryPair` in `manager.go` currently generates the ID at line 173:

```go
correlationID := uuid.New().String()
```

This line is replaced by using the `correlationID` parameter passed in from `SendToGoldfish`. The rest of the method is unchanged.

### No-op / nil manager behavior

The codebase currently uses `nil` checks rather than a no-op manager (e.g., `if f.goldfishManager == nil` at `splitting_handler.go:278`, `if h.goldfishManager == nil` at `fanout_handler.go:506`). No changes are needed for the nil case. The mock implementations in test files must be updated to match the new `Manager` interface signatures.

### What doesn't change

- `Sampler` internals: still probabilistic, same rate logic.
- Object storage paths, MySQL schema, database queries: all use UUID strings already.
- Comparison logic, stats extraction: untouched.
- UI handler: retrieves records by correlation ID as before.

## Files modified

| File | Change |
|------|--------|
| `pkg/querytee/goldfish/manager.go` | `Manager` interface: update `ShouldSample` and `SendToGoldfish` signatures. Implementation: generate UUID in `ShouldSample`, accept and validate ID in `SendToGoldfish`/`processQueryPair`. |
| `pkg/querytee/goldfish/context.go` | New file. `SamplingDecision` struct, context helpers, and header constant. |
| `pkg/querytee/proxy_endpoint.go` | `serveWrites`: update `ShouldSample` call site, set response header before error/success branching. `executeBackendRequests`: replace `goldfishSample bool` with `correlationID string`. `processWithGoldfish`: add `correlationID string` parameter, pass to `SendToGoldfish`. |
| `pkg/querytee/splitting_handler.go` | `ServeHTTP`: reorder multi-tenant check before sampling, update `shouldSample` call site to return `(bool, string)`, store decision in context. `writeResponse`: extract from context at top of function and set response header before error/success branching. |
| `pkg/querytee/fanout_handler.go` | `Do`: check context for existing sampling decision before calling `shouldSample`. Replace `shouldSample bool` with `correlationID string` through full internal chain: `doWithPreferred`, `doWithRacing`, `finishRace`, `collectRemainingAndCompare`, `processGoldfishComparison`, `sendToGoldfish`. Update `shouldSample` helper to return `(bool, string)`. |
| `pkg/querytee/splitting_handler_test.go` | Update mock `Manager` to match new interface. Test header presence on sampled requests, absence on non-sampled. |
| `pkg/querytee/fanout_handler_test.go` | Update mock `Manager`. Test that `ShouldSample` is NOT called when context carries a decision (positive or negative). |
| `pkg/querytee/proxy_endpoint_test.go` | Update mock `Manager`. Test header on sampled writes. |
| `pkg/querytee/goldfish/manager_test.go` | Update call sites for new signatures. |
| `pkg/querytee/goldfish/end_to_end_test.go` | Update `ShouldSample` and `SendToGoldfish` call sites for new signatures. |
| `pkg/querytee/goldfish/context_test.go` | New file. Test `SamplingDecision` context round-trip for both positive and negative decisions. |

## Testing requirements

Beyond updating existing tests for the new signatures:

1. **Header presence/absence:** Verify `X-Loki-Goldfish-ID` is present on sampled responses and absent on non-sampled responses, for both reads and writes paths.
2. **Double-sampling elimination:** Verify that `FanOutHandler` does **not** call `ShouldSample` when context carries a decision. This should be tested for both positive (`Sampled=true`) and negative (`Sampled=false`) upstream decisions.
3. **Context propagation:** Verify the sampling decision survives the middleware chain from `SplittingHandler` through to `FanOutHandler`.
4. **Error responses:** Verify the header is present on sampled requests that return error responses.
5. **Correlation ID consistency:** Verify the same ID that appears in the response header is the one stored in the goldfish database.

## Edge cases

- **Goldfish processing fails after header is set:** The client sees the header but the record may not exist in the database. This is acceptable; the header means "sampled", not "record exists".
- **Empty or invalid correlation ID at storage boundary:** `SendToGoldfish` validates the correlation ID is non-empty. If empty (programming error), it logs a warning and drops the request to avoid a storage primary key violation.
- **FanOutHandler used without SplittingHandler:** Falls back to its own `ShouldSample` call. The correlation ID is used for goldfish storage, but cannot be set on the HTTP response header (since `FanOutHandler` returns a `queryrangebase.Response`, not an `http.ResponseWriter`). This is an intentional limitation; in production, `FanOutHandler` is always wrapped by `SplittingHandler`.
- **Multi-tenant queries:** The multi-tenant check in `SplittingHandler` is evaluated before the sampling decision. No UUID is generated, no header is set, and the query routes directly to v1.
- **Non-sampled requests:** No header is set. Absence of the header signals "not sampled". The negative decision is stored in context so downstream handlers do not resample.
- **Error responses:** The header is set on sampled requests regardless of backend response status, as long as the sampling decision was made before the error occurred.
- **>2 backends:** The current deployment uses exactly 2 backends. `processGoldfishComparison` loops over non-preferred backends but only one pair is produced. If >2 backends were supported, the single-correlation-ID model would need revisiting. This is out of scope.
