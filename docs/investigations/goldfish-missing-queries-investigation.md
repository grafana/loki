# Investigation: 30/107 Queries Missing from Goldfish

## Summary

After the correlation ID refactor, the k6 test consistently matches ~77/107 queries via Goldfish, with ~30 queries never producing a Goldfish comparison result within the 2-minute polling window. No errors appear in query-tee logs. The header `X-Loki-Goldfish-Id` is returned for all 107 queries (100% sampling confirmed), but only ~77 produce a stored comparison result.

## Findings

### Routing decisions reveal the pattern

With `-routing.add-routing-decisions-to-warnings` enabled on the query-tee, the response body `warnings` field shows routing decisions. The key difference between successful and missing queries:

**Successful queries (produce Goldfish results):**
```
query-frontend.loki-dev-005.svc.cluster.local. backend won the race | metrics query was split between v1 and v2 backends
```
Two warnings: the FanOutHandler race winner AND the splitting handler split decision.

**Missing queries (no Goldfish results):**
```
logs query was split between v1 and v2 backends
```
Only one warning: the splitting handler split decision. No FanOutHandler warning.

### Which queries are missing

The missing ~30 queries are consistently from:
- `fast/basic-selectors.yaml` (5 queries) — simple log stream selectors
- `fast/structured-metadata.yaml` (4 queries) — structured metadata filters
- `regression/drilldown-patterns.yaml` (3-4 queries) — drilldown log patterns
- `regression/structured-metadata.yaml` (8-10 queries) — structured metadata log queries
- `exhaustive/unwrap-aggregations.yaml` lines 367-513 (10 queries) — the last ~10 unwrap aggregations

The first four categories are **log queries**. The last is metric queries (unwrap aggregations). All successful metric queries from `regression/metric-queries.yaml`, `exhaustive/aggregations.yaml`, and `fast/simple-metrics.yaml` consistently produce Goldfish results.

### What this means

The splitting handler confirms all queries are "split between v1 and v2 backends." The v2 portion is sent to FanOutHandler. But for the missing queries, FanOutHandler does not emit a race winner or preferred-backend warning, and no Goldfish comparison record is created.

The Goldfish comparison happens inside `FanOutHandler.collectRemainingAndCompare()`, which runs as a goroutine after the preferred backend returns. If it never runs (or never reaches `processGoldfishComparison`), no Goldfish result is stored.

### Absence of errors

There are **zero** error-level logs from the query-tee during the test window. This rules out:
- Unique key constraint violations on `correlationId` (would log `"failed to store query sample"`)
- Goldfish manager failures (would log at error level)
- Backend request failures (would log from FanOutHandler)

## Hypotheses to investigate

### 1. v2 backend returns 404 for log queries, causing FanOutHandler to skip comparison

In `FanOutHandler.doWithPreferred()` (fanout_handler.go:209-211):
```go
if !preferV1 && result.backendResp.status == 404 {
    continue  // skip this result, don't treat as preferred
}
```

If the v2 backend returns 404 for a log query (because it doesn't implement log query support), the preferred result is skipped. If all backends return 404, the handler falls through to `returnFallback()` which does NOT call `collectRemainingAndCompare` — so no Goldfish comparison happens.

**How to test:** Add logging to `doWithPreferred` to capture when 404 fallback occurs, or check the v2 backend's support for log query types.

### 2. v2 backend doesn't support certain query types, returning errors that skip the comparison path

The v2 (new engine) may not support all LogQL query types. If a sub-query returns an error from v2, the FanOutHandler may not have a valid preferred result, causing `processGoldfishComparison` to bail out at:
```go
if h.goldfishManager == nil || len(results) < 2 || preferredResult == nil {
    return
}
```

**How to test:** Add debug logging to `processGoldfishComparison`'s guard clause to see if it's being hit with `preferredResult == nil`.

### 3. The EngineRouterMiddleware routes log queries entirely to v1

The `EngineRouterMiddleware` may determine that certain log query types are not supported by v2 (`engine.IsQuerySupported` validation). If a query fails validation, the middleware routes it to `v1Next` instead of `v2Next` (FanOutHandler), bypassing Goldfish comparison entirely.

**How to test:** Add debug logging to the EngineRouterMiddleware's routing decision, or check what `engine.IsQuerySupported` returns for the missing log query expressions. The relevant code is in `pkg/querier/queryrange/engine_router.go`.

### 4. Unwrap aggregation queries exceed the 30-second processQueryPair timeout

The `processQueryPair` function uses a 30-second context timeout (manager.go:185). Complex unwrap aggregation queries might take longer than 30s for the goldfish comparison to complete. If the context expires, the goroutine silently abandons the comparison.

**How to test:** Check if the unwrap aggregation queries that fail are consistently the ones with the largest response payloads or longest cell execution times.

## Recommended next steps

1. **Check EngineRouterMiddleware routing for log queries** — This is the most likely explanation. Look at `engine.IsQuerySupported` to see if basic log selectors and structured metadata queries are classified as unsupported for v2. If so, these queries go to v1 only and never reach FanOutHandler.

   ```bash
   # In ~/workspace/loki/loki, check:
   grep -n 'IsQuerySupported\|SupportedExpr' pkg/engine/*.go
   ```

2. **Add a debug log to processGoldfishComparison's guard clause** — To confirm whether the missing queries reach FanOutHandler but fail the `len(results) < 2 || preferredResult == nil` check:

   ```go
   // In fanout_handler.go, processGoldfishComparison:
   if h.goldfishManager == nil || len(results) < 2 || preferredResult == nil {
       level.Debug(h.logger).Log("msg", "skipping goldfish comparison",
           "goldfishManager_nil", h.goldfishManager == nil,
           "results_count", len(results),
           "preferredResult_nil", preferredResult == nil,
           "correlationID", correlationID)
       return
   }
   ```

3. **For this test suite** — Relax the `goldfish_missing_count` threshold to account for queries the v2 engine doesn't support. A reasonable threshold might be `count < 35` until the v2 engine covers all query types, or filter the test cases to only include query types known to be supported by v2.

## Environment

- Query-tee image: `grafana/loki-query-tee:main-e90e4a6` (PR #21155)
- Cluster: loki-dev-005
- Tenant: 29
- Sampling config: `-goldfish.sampling.tenant-rules=29:1.0`
- Split lag: 3h
- Test query range: `-28h` to `-4h` (all data >3h old, eligible for v2)
