# Goldfish Early Correlation ID Calculation

## Problem

The Goldfish correlation ID is currently generated inside `processQueryPair` (`pkg/querytee/goldfish/manager.go:173`), which runs asynchronously in a background goroutine after the HTTP response has already been sent to the client. This means the correlation ID cannot be returned to the caller in a response header.

Additionally, the reads path has a double-sampling bug: `SplittingHandler` and `FanOutHandler` each independently call `ShouldSample`, making the effective sampling rate `rate * rate` instead of `rate`.

## Goals

1. Generate the correlation ID at the point of the sampling decision, before the HTTP response is written.
2. Return the correlation ID in an `X-Loki-Goldfish-ID` response header on sampled requests.
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

Defined once in a shared location (e.g., `pkg/querytee/goldfish/context.go` alongside the context helpers).

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

`SendToGoldfish` accepts the pre-generated correlation ID and threads it to `processQueryPair`, which uses it instead of generating its own.

### Context helpers for reads path

A new file `pkg/querytee/goldfish/context.go` provides context-based threading of the correlation ID from `SplittingHandler` to `FanOutHandler`:

```go
type contextKey int

const correlationIDKey contextKey = iota

func ContextWithCorrelationID(ctx context.Context, id string) context.Context {
    return context.WithValue(ctx, correlationIDKey, id)
}

func CorrelationIDFromContext(ctx context.Context) (string, bool) {
    id, ok := ctx.Value(correlationIDKey).(string)
    return id, ok && id != ""
}
```

### Writes path (proxy_endpoint.go)

The writes path has a single sampling decision point with no downstream handler complications.

1. `serveWrites` calls `ShouldSample(tenantID)`, receiving `(sampled bool, correlationID string)`.
2. If sampled, sets `w.Header().Set(goldfish.GoldfishCorrelationIDHeader, correlationID)` before calling `w.WriteHeader`.
3. Passes `correlationID` through to `processWithGoldfish` and then to `SendToGoldfish`.

### Reads path (splitting_handler.go -> fanout_handler.go)

This path involves two handlers. The design makes `SplittingHandler`'s sampling decision authoritative and fixes the double-sampling bug.

**SplittingHandler.ServeHTTP:**

1. Calls `ShouldSample(tenantID)`, receiving `(sampled, correlationID)`.
2. If sampled, stores the ID in request context: `ctx = goldfish.ContextWithCorrelationID(ctx, correlationID)`.
3. Stores the `correlationID` on the `SplittingHandler` instance or passes it to `writeResponse`.
4. In `writeResponse`, if correlation ID is non-empty, sets `w.Header().Set(goldfish.GoldfishCorrelationIDHeader, correlationID)` before calling `w.WriteHeader`.

**FanOutHandler.Do:**

1. Checks context first: `correlationID, hasSamplingDecision := goldfish.CorrelationIDFromContext(ctx)`.
2. If `hasSamplingDecision` is true: uses `shouldSample=true` and the context's `correlationID`. Skips its own `ShouldSample` call.
3. If `hasSamplingDecision` is false (standalone use, tests): falls back to calling `ShouldSample` itself, same as today but now receiving the tuple.
4. Passes `correlationID` through to `sendToGoldfish` and then to `SendToGoldfish`.

This fixes the double-sampling bug: the effective rate becomes `rate` instead of `rate * rate`.

### processQueryPair changes

`processQueryPair` in `manager.go` currently generates the ID at line 173:

```go
correlationID := uuid.New().String()
```

This line is replaced by using the `correlationID` parameter passed in from `SendToGoldfish`. The rest of the method is unchanged.

### No-op manager

The no-op manager (`storage_noop.go` or similar) updates its `ShouldSample` to return `(false, "")` and `SendToGoldfish` to accept the extra parameter (ignored).

### What doesn't change

- `Sampler` internals: still probabilistic, same rate logic.
- Object storage paths, MySQL schema, database queries: all use UUID strings already.
- Comparison logic, stats extraction: untouched.
- UI handler: retrieves records by correlation ID as before.

## Files modified

| File | Change |
|------|--------|
| `pkg/querytee/goldfish/manager.go` | `Manager` interface: update `ShouldSample` and `SendToGoldfish` signatures. Implementation: generate UUID in `ShouldSample`, accept ID in `SendToGoldfish`/`processQueryPair`. |
| `pkg/querytee/goldfish/context.go` | New file. Context helpers and header constant. |
| `pkg/querytee/proxy_endpoint.go` | `serveWrites`: update `ShouldSample` call site, set response header, pass ID to `processWithGoldfish`/`SendToGoldfish`. |
| `pkg/querytee/splitting_handler.go` | `ServeHTTP`: update `ShouldSample` call site, store ID in context. `writeResponse`: set response header. Update `shouldSample` helper. |
| `pkg/querytee/fanout_handler.go` | `Do`: check context for existing sampling decision before calling `ShouldSample`. Thread `correlationID` through to `sendToGoldfish`. |
| `pkg/querytee/goldfish/noop_manager.go` or equivalent | Update to match new interface signatures. |
| Tests for all above files | Update to match new signatures and test header presence/absence. |

## Edge cases

- **Goldfish processing fails after header is set:** The client sees the header but the record may not exist in the database. This is acceptable; the header means "sampled", not "record exists".
- **FanOutHandler used without SplittingHandler:** Falls back to its own `ShouldSample` call. Works correctly in standalone mode and in tests.
- **Multi-tenant queries:** `SplittingHandler` routes these directly to v1 (line 203) without fanout. No goldfish sampling occurs, no header is set.
- **Non-sampled requests:** No header is set. Absence of the header signals "not sampled".
