# pkg/logline/queryfrontend/

## What this package does

This package integrates the **logline index** into **Loki's query-range pipeline**. It
injects two middlewares around Loki's existing `queryrangebase` middleware stack to skip
or narrow time intervals that the logline index proves contain no matching log lines.

## Architecture: Two-layer middleware

```
  Prefetch MW → [Loki: limits → SplitByInterval → cache → shard → ...] → Filter MW → queriers
```

1. **Prefetch middleware** (`loglinePrefetchHandler`): Sits *above* `SplitByInterval`.
   Parses the query, kicks off an async logline index lookup, and stores the result
   (hint time ranges + ingester cutoff) in context. Opt-in via `X-Logline-Index` header.

2. **Filter middleware** (`loglineFilterHandler`): Sits *below* `SplitByInterval` and
   below the results cache. For each interval sub-request, it consults the prefetched
   hints to either:
   - **Skip** the interval entirely (return empty response) if no hint ranges overlap
   - **Narrow** to only the matching time ranges within the interval
   - **Pass through** if the interval is in the ingester window, or on error/timeout

## Key integration points

- **Wiring**: `WrapMiddleware` / `WrapMiddlewareWithStore` in `integration.go` create the
  store, hint provider, optional cache, and compose the middleware stack. The caller owns
  the returned `services.Service` and prepends the wrapped middleware to
  `Loki.QueryFrontEndMiddleware`.
- **Hint provider**: `pkg/logline/hintprovider` does the actual index lookups.
- **Store**: `pkg/logline/store` manages the logline index data (object storage, polling).
- **Loki codec**: `mergeLokiResponse` in Loki's `pkg/querier/queryrange/codec.go` merges
  sub-interval responses. It inherits `Direction`, `Limit`, and `Version` from
  `responses[0]`, so any empty response returned by the filter middleware **must**
  faithfully copy these fields from the original `LokiRequest`.

## Common pitfalls

### Ingester window

The logline index only covers data in object storage. Recent data still in ingesters
is always passed through. The `ingesterCutoff` is computed from
`QueryIngestersWithin` (from Loki's querier config).

### Hint timeout

If the async hint lookup doesn't complete within `HintTimeout` (default 15s), the
filter middleware falls back to passthrough — it does not block or fail the query.

## Files

| File | Purpose |
|------|---------|
| `middleware.go` | Prefetch + filter middleware handlers, `emptyLokiResponse`, `rangesOverlapping` |
| `integration.go` | `WrapMiddleware` / `WrapMiddlewareWithStore` — builds and composes the full stack |
| `config.go` | `MiddlewareConfig` and `Config` — ngram length, parallelism, timeouts, cache settings |
| `metrics.go` | Prometheus metrics for hint provider duration, ranges returned, passthrough/skip/narrow counts |

## Running tests

```sh
cd pkg/logline/queryfrontend && go test -v ./...
```
