# Goldfish Early Correlation ID Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move goldfish correlation ID generation to the sampling decision point so it can be returned in an `X-Loki-Goldfish-ID` response header, and fix the double-sampling bug on the reads path.

**Architecture:** The `Manager.ShouldSample` method returns a `(bool, string)` tuple containing the sampling decision and a pre-generated UUID. A `SamplingDecision` struct is threaded via request context from `SplittingHandler` to `FanOutHandler` to eliminate double-sampling. The correlation ID replaces the `shouldSample bool` parameter throughout the fanout call chain.

**Tech Stack:** Go, `github.com/google/uuid`, `context.Context`

**Spec:** `docs/superpowers/specs/2026-03-12-goldfish-early-correlation-id-design.md`

**Commits:** Aim for 2-3 logical commits, each leaving the tree in a compilable/green state:
1. Context helpers (new additive code, no callers yet)
2. Interface change + all callsite updates across manager, proxy_endpoint, splitting_handler, fanout_handler, and all tests
3. New behavior tests (header presence, double-sampling elimination)

---

### Task 1: Context helpers and header constant

Purely additive — no existing code changes, no compilation risk.

**Files:**
- Create: `pkg/querytee/goldfish/context.go`
- Create: `pkg/querytee/goldfish/context_test.go`

- [ ] **Step 1: Write tests for context helpers**

Create `pkg/querytee/goldfish/context_test.go` with three tests:
- `TestSamplingDecisionFromContext_NotSet`: background context returns `(zero, false)`.
- `TestSamplingDecisionFromContext_Sampled`: stores `{Sampled: true, CorrelationID: "test-uuid"}`, asserts round-trip.
- `TestSamplingDecisionFromContext_NotSampled`: stores `{Sampled: false, CorrelationID: ""}`, asserts `ok=true` and `Sampled=false` (both positive and negative decisions are propagated).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/querytee/goldfish/ -run TestSamplingDecision -v`
Expected: compilation failure — types not defined yet.

- [ ] **Step 3: Implement context helpers**

Create `pkg/querytee/goldfish/context.go` with:
- `GoldfishCorrelationIDHeader = "X-Loki-Goldfish-ID"` constant.
- `SamplingDecision` struct with `Sampled bool` and `CorrelationID string` fields.
- `ContextWithSamplingDecision(ctx, decision)` and `SamplingDecisionFromContext(ctx)` functions.

See spec "Context helpers for reads path" section for the exact implementation.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/querytee/goldfish/ -run TestSamplingDecision -v`
Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```
feat: add goldfish SamplingDecision context helpers and header constant
```

---

### Task 2: Update Manager interface and all callsites

This is one atomic task that changes the interface and updates every caller so the package compiles cleanly at the end. Do NOT run package-level tests until all steps are complete — intermediate states will not compile.

**Files:**
- Modify: `pkg/querytee/goldfish/manager.go` (interface + implementation)
- Modify: `pkg/querytee/goldfish/end_to_end_test.go` (ShouldSample and SendToGoldfish calls)
- Modify: `pkg/querytee/goldfish/manager_test.go` (all direct ShouldSample/SendToGoldfish calls — there are many: lines ~126, 165, 262, 344, 478, 572, 701)
- Modify: `pkg/querytee/proxy_endpoint.go` (serveWrites, executeBackendRequests, processWithGoldfish)
- Modify: `pkg/querytee/splitting_handler.go` (shouldSample, ServeHTTP, writeResponse)
- Modify: `pkg/querytee/splitting_handler_test.go` (mock Manager)
- Modify: `pkg/querytee/fanout_handler.go` (shouldSample, Do, doWithPreferred, doWithRacing, finishRace, collectRemainingAndCompare, processGoldfishComparison, sendToGoldfish)
- Modify: `pkg/querytee/fanout_handler_test.go` (if mock Manager exists)
- Modify: `pkg/querytee/proxy_endpoint_test.go` (if mock Manager exists)

#### Manager interface and implementation

- [ ] **Step 1: Update Manager interface in manager.go**

Change `ShouldSample(tenantID string) bool` → `ShouldSample(tenantID string) (sampled bool, correlationID string)`.
Change `SendToGoldfish(httpReq, cellAResp, cellBResp)` → `SendToGoldfish(httpReq, cellAResp, cellBResp, correlationID string)`.

See spec "Manager interface changes" section for exact signatures and doc comments.

- [ ] **Step 2: Update ShouldSample implementation in manager.go**

When not enabled or not sampled, return `(false, "")`. When sampled, generate `uuid.New().String()` and return `(true, correlationID)`. The `uuid` package is already imported.

Add a code comment about the >2 backends assumption per the spec:
```
// NOTE: Currently one correlation ID is generated per sampled request, which maps 1:1
// to a comparison pair. If >2 backends are ever supported, this model would need to
// separate "request ID" (shared across all comparisons from one request) from
// "comparison ID" (unique per pair).
```

- [ ] **Step 3: Update SendToGoldfish implementation in manager.go**

Accept `correlationID string` parameter. Add validation: if `correlationID` is empty, log a warning and return early (it's the PK in the database). Thread to `processQueryPair`.

- [ ] **Step 4: Update processQueryPair in manager.go**

Change signature to accept `correlationID string`. Remove the `uuid.New().String()` generation at line 173 — use the parameter instead.

- [ ] **Step 5: Update manager_test.go**

Update all `ShouldSample` calls to unpack `(sampled, correlationID)` tuple. Update all `SendToGoldfish` calls to pass a test correlation ID string.

- [ ] **Step 6: Update end_to_end_test.go**

Same updates as Step 5: unpack `ShouldSample` tuples, pass correlation IDs to `SendToGoldfish`.

#### Writes path (proxy_endpoint.go)

- [ ] **Step 7: Update serveWrites in proxy_endpoint.go**

Replace `shouldSample bool` with `sampled, correlationID` from `ShouldSample`. Set the header **once before** the error/success branch diverges:
```go
// Set goldfish header before any response writing (error or success).
if correlationID != "" {
    w.Header().Set(goldfish.GoldfishCorrelationIDHeader, correlationID)
}
```
Then in the success branch, copy backend headers and use `w.Header().Set` again **after** the copy loop to re-assert the goldfish header (in case a backend sent a header with the same name).

- [ ] **Step 8: Update executeBackendRequests in proxy_endpoint.go**

Change parameter from `goldfishSample bool` to `correlationID string`. Update the goldfish gate from `if goldfishSample && ...` to `if correlationID != "" && ...`. Pass `correlationID` to `processWithGoldfish`.

- [ ] **Step 9: Update processWithGoldfish in proxy_endpoint.go**

Add `correlationID string` parameter. Pass it to `SendToGoldfish`.

- [ ] **Step 10: Update proxy_endpoint_test.go**

The proxy endpoint tests use a real Manager (not a mock), so they'll compile after the interface change. Check for any test helpers or direct calls that need updating. The `mockGoldfishStorage` (lines ~718-758) implements `goldfish.Storage`, not `goldfish.Manager`, so it doesn't need interface changes.

#### Reads path — SplittingHandler (splitting_handler.go)

- [ ] **Step 11: Update mock Manager in splitting_handler_test.go**

Change `mockGoldfishManager` to match new interface:
- `ShouldSample` returns `(bool, string)` using `shouldSampleResult` and `correlationIDResult` fields.
- `SendToGoldfish` accepts `correlationID string`.
- Add tracking fields: `sendCalled bool`, `sendCorrelationID string`.

Update all `mockGoldfishManager` instantiations: set `correlationIDResult: "test-correlation-id"` when `shouldSampleResult: true`.

- [ ] **Step 12: Update shouldSample helper in splitting_handler.go**

Change return from `bool` to `(bool, string)`. Iterate tenants, call `ShouldSample` (now returns tuple), return correlation ID from first sampled tenant. Return `(false, "")` if none sampled.

- [ ] **Step 13: Update ServeHTTP in splitting_handler.go**

Three changes:
1. Move the `isMultiTenant(tenants)` check **before** the `shouldSample` call. Multi-tenant path returns early without sampling — no wasted UUID. Add a brief comment: "Multi-tenant path returns before sampling, so writeResponse correctly won't set the header."
2. Call updated `shouldSample` to get `(sampled, correlationID)`.
3. Store the decision in context for both outcomes: `ctx = goldfish.ContextWithSamplingDecision(ctx, goldfish.SamplingDecision{Sampled: sampled, CorrelationID: correlationID})`.
4. Update the routing decision to use `sampled` instead of `shouldSample`.

- [ ] **Step 14: Update writeResponse in splitting_handler.go**

Add header extraction at the **top** of the method, before any error/success branching:
```go
if decision, ok := goldfish.SamplingDecisionFromContext(ctx); ok && decision.Sampled && decision.CorrelationID != "" {
    w.Header().Set(goldfish.GoldfishCorrelationIDHeader, decision.CorrelationID)
}
```

This ensures the header is present on both success and error responses. If no decision in context (e.g., multi-tenant), no header is set.

#### Reads path — FanOutHandler (fanout_handler.go)

- [ ] **Step 15: Update shouldSample helper in fanout_handler.go**

Same change as splitting_handler: return `(bool, string)`, iterate tenants, return on first sampled.

Both handlers have their own `shouldSample` — this mirrors the existing pattern and extraction isn't worth the abstraction.

- [ ] **Step 16: Update Do method in fanout_handler.go**

Check context first: `decision, hasDecision := goldfish.SamplingDecisionFromContext(ctx)`. If `hasDecision`, use `decision.CorrelationID` directly (empty = not sampled). If not, fall back to calling `shouldSample` (standalone/test use).

Replace `shouldSample bool` with `correlationID string` in the calls to `doWithPreferred`/`doWithRacing`.

Move `tenant.TenantIDs(ctx)` into the fallback branch (only needed when no context decision).

- [ ] **Step 17: Update internal method chain — replace shouldSample bool with correlationID string**

Update all signatures in the chain:
- `doWithRacing(results, collected, httpReq, correlationID string)`
- `doWithPreferred(results, collected, httpReq, correlationID string, preferV1 bool)`
- `finishRace(winner, remaining, httpReq, results, collected, correlationID string)`
- `collectRemainingAndCompare(remaining, httpReq, results, collected, correlationID string, preferV1 bool)`

In `collectRemainingAndCompare`, change the goldfish gate from `if shouldSample {` to `if correlationID != "" {`.

- [ ] **Step 18: Update processGoldfishComparison and sendToGoldfish**

Add `correlationID string` parameter to both. Add the >2 backends comment in `processGoldfishComparison` at the loop over results, per the spec:
```
// NOTE: Currently one correlation ID is generated per sampled request, assuming
// exactly 2 backends (one preferred, one comparison). If >2 backends are ever
// supported, this model would need to separate "request ID" (shared across all
// comparisons from one request) from "comparison ID" (unique per pair).
```

Pass `correlationID` through to `sendToGoldfish` → `SendToGoldfish`.

- [ ] **Step 19: Update fanout_handler_test.go**

Update any mock Manager implementations or direct calls to match new signatures.

#### Verify compilation and tests

- [ ] **Step 20: Verify compilation**

Run: `go build ./pkg/querytee/... && go build ./pkg/querytee/goldfish/...`
Expected: clean compilation.

- [ ] **Step 21: Run all tests**

Run: `go test ./pkg/querytee/... -v -count=1`
Expected: all tests PASS. Note: `./pkg/querytee/...` includes `./pkg/querytee/goldfish/...`.

- [ ] **Step 22: Run go vet**

Run: `go vet ./pkg/querytee/...`
Expected: clean.

- [ ] **Step 23: Fix any issues and re-run**

If any tests fail or vet warnings appear, fix them and re-run Steps 20-22.

- [ ] **Step 24: Commit**

```
feat: move correlation ID generation to sampling decision, set X-Loki-Goldfish-ID header, fix double-sampling
```

---

### Task 3: Add targeted tests for new behavior

Three focused tests covering the new behaviors not exercised by existing tests.

**Files:**
- Modify: `pkg/querytee/proxy_endpoint_test.go` or `pkg/querytee/splitting_handler_test.go`
- Modify: `pkg/querytee/fanout_handler_test.go`

- [ ] **Step 1: Test header presence/absence on writes path**

Add tests to `proxy_endpoint_test.go`:
- Sampled write: mock manager returns `(true, "test-uuid")`. Send a write request. Assert `X-Loki-Goldfish-ID: test-uuid` in response headers.
- Non-sampled write: mock manager returns `(false, "")`. Assert header absent.

Follow existing test patterns in the file for creating test endpoints and sending requests.

- [ ] **Step 2: Test header presence/absence on reads path**

Add tests to `splitting_handler_test.go`:
- Sampled read: mock manager returns `(true, "test-uuid")`. Send a read request. Assert `X-Loki-Goldfish-ID: test-uuid` in response headers.
- Non-sampled read: mock returns `(false, "")`. Assert header absent.

- [ ] **Step 3: Test FanOutHandler respects upstream context decision**

Add tests to `fanout_handler_test.go` with a mock Manager that tracks whether `ShouldSample` was called:
- **Positive upstream decision**: Set `SamplingDecision{Sampled: true, CorrelationID: "test-uuid"}` in context. Call `Do`. Assert `ShouldSample` was NOT called.
- **Negative upstream decision**: Set `SamplingDecision{Sampled: false}` in context. Call `Do`. Assert `ShouldSample` was NOT called.
- **No upstream decision**: No context. Call `Do`. Assert `ShouldSample` WAS called.

- [ ] **Step 4: Run all tests**

Run: `go test ./pkg/querytee/... -v -count=1`
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```
test: add tests for correlation ID header and double-sampling elimination
```
