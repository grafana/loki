# pkg/logline/queryfrontend/

## What this package does

This package integrates the **logline index** into **Loki's query-range pipeline**. It
injects two middlewares around Loki's existing `queryrangebase` middleware stack to skip
or attach hint ranges for time intervals that the logline index proves contain no matching log lines.

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
   - **Forward** one request with the clipped raw hint ranges attached. Request bounds
     stay the split interval. Queriers use the ranges to skip storage work.
   - **Pass through** if the interval is in the ingester window, is covered only by a
     pre-min-date sentinel, or on error/timeout. Ingester-window and pre-min-date
     intervals pass through before waiting for the lookup, so a limited query can
     return from them while the lookup is still running.

## Key integration points

- **Wiring**: `WrapMiddleware` / `WrapMiddlewareWithStore` in `integration.go` create the
  store, hint provider, optional cache, and compose the middleware stack.
- **Per-tenant settings are limits.** The mode and the minimum query bytes are
  `limits_config` fields (`logline_query_mode`, `logline_query_min_query_bytes_for_index`),
  so a tenant overrides them in the runtime config like any other limit.
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

### Detected fields

The prefetch middleware also handles `DetectedFieldsRequest`, for both
`detected_fields` and `detected_field/{name}/values`. The detected fields handler turns
it into one `BACKWARD` `LokiRequest` limited to the line limit, and the filter treats
that request like any log query.

Detected fields only look up hints in live mode. Dry-run verifies hints against the log
lines or samples in the response, and a detected fields response has neither. The hints
are the same ones log queries get, so dry-run on log and metric queries covers their
correctness.

### Pass-through intervals don't wait for the lookup

Ingester-window and pre-min-date intervals pass through whatever the lookup returns,
so the filter dispatches them without waiting. This changes what some signals count:

- `logline_query_frontend_passthrough_sub_requests_total` counts these intervals even
  when the lookup fails or times out. `logline_query_frontend_hint_passthrough_total`
  and `QueryStats.PrefetchCalls` only count sub-requests that waited for the lookup.
- A lookup still running when the query returns is canceled. It counts in
  `logline_query_frontend_hint_lookups_canceled_total` and not in
  `logline_query_frontend_hint_provider_duration_seconds`, which only observes
  lookups that finished.
- When no sub-request can use the lookup, the prefetch middleware doesn't start it.
  If the part of the query outside the ingester window predates the index, the
  middleware records the pre-min-date range the lookup would have returned, so the
  hint status, ranges and outcome match a query that ran the lookup.

### Hint timeout

If the async hint lookup doesn't complete within `HintTimeout` (default 15s), the
filter middleware falls back to passthrough — it does not block or fail the query.

The timeout applies to each sub-request, not to the lookup. A live lookup keeps
running, so sub-requests that reach the filter in a later wave wait up to
`HintTimeout` again.

### Hint wait

The hint wait is the wall time during which at least one sub-request was held at the
filter for the lookup. It is the latency Logline adds by holding sub-requests, which
isn't all of the latency Logline adds: it leaves out the index-stats call that checks
the byte threshold. The filter runs
once per split and shard sub-request, and those wait in parallel and in waves, so
`waitTracker` records the union of their wait intervals rather than the sum.

The prefetch middleware reports the wait once per live query that starts a hint
lookup: on the response as `Logline.HintWaitTime` (`logline_hint_wait_time` on the
`metrics.go` line) and in the `logline_query_frontend_hint_wait_seconds` histogram.
The histogram also counts failed queries, and records a zero when the hints were ready
before any sub-request needed them or when no sub-request could use them, so the
lookup didn't run. Dry-run queries and queries skipped before the lookup aren't
observed.

The histogram's `status` label is the hint status when the query returns, not whether
a sub-request timed out. `incomplete` includes queries that the results cache answered
before the lookup finished, with a zero wait. `ok` includes queries whose sub-requests
timed out, if the lookup finished before the query did. A timed-out wait shows up as a
wait of at least `HintTimeout`.

### Query stats

Logline stats travel on the response `Statistics.Logline`, not on counters on the
prefetch result. They reach the frontend `metrics.go` line as `logline_*` fields and the
HTTP response as `data.stats.logline`. `Statistics.Logline` is a pointer that is nil for
queries that didn't go through this middleware, so the JSON key is left out for them.
Use `Statistics.LoglineStats()` to set fields.

The filter adds the skipped and narrowed counts to the response it returns, so the split
and shard merges sum them and discarded responses (retries) drop them. The prefetch
middleware adds the rest to the final response: the lookup
status, time and ranges, the hint wait time, the lookup's object storage I/O, copies of
`Index.ShardPlannedChunks` and `Index.ShardPlannedChunksScanned`, the chunk filter ratio,
and the outcome. `Logline.Merge` sums the two counts and keeps the first value of every
other field.

The chunk filter ratio compares `Index.ShardPlannedChunks`, the chunks shard planning
estimated, with `Index.ShardPlannedChunksScanned`, the chunks scanned for the same
requests (`Store.TotalChunksScanned` plus ingester matches). The shard middleware records
both per split, so splits that bypass shard planning count toward neither.

`loglineOutcome` sets `Logline.Outcome`, which tells whether the index skipped data or
why it didn't:

1. The three early skips in the prefetch handler record `unsupported_query`,
   `size_estimate_failed` and `query_too_small`, in live mode only.
2. A lookup that didn't end `ok` maps to `unsupported_query`, `lookup_failed`,
   `lookup_timeout`, `answered_before_lookup`, `canceled` or `recent_data_only`.
   A lookup still running when the query returns is `lookup_timeout` if a
   sub-request gave up waiting for it, and `answered_before_lookup` otherwise.
3. Without shard planning, the outcome is `skip_ratio_unavailable`.
4. If no sub-request was skipped or narrowed, it is `time_range_not_indexed`.
5. A ratio below `tinyDataSkippedRatio` (0.25) is `tiny_data_skipped`, and anything else
   is `data_skipped`.

## Files

| File | Purpose |
|------|---------|
| `middleware.go` | Prefetch + filter middleware handlers, `emptyLokiResponse`, `rangesOverlapping` |
| `integration.go` | `WrapMiddleware` / `WrapMiddlewareWithStore` — builds and composes the full stack |
| `config.go` | `Config`, the `logline.query` section: enable switch, parallelism, timeouts, cache settings |
| `limits/definitions.go` | The per-tenant `Limits` interface |
| `metrics.go` | Prometheus metrics for hint provider duration, canceled lookups, hint wait, ranges returned, passthrough/skip/narrow counts |

## Running tests

```sh
cd pkg/logline/queryfrontend && go test -v ./...
```
