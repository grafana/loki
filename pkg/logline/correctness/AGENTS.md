# pkg/logline/correctness/

Continuously verifies that queryable terms in Loki are reflected by overlapping logline indexes.

## Invariants

1. **No pre-start / retention-edge querying**
   - Random test ranges must never start before `max(startedAt, now - max_lookback)`.
   - `started_at` is the farthest lookback; `max_lookback` (default 30d) clamps away from retention deletion races.
   - Combined with the ingester window, this avoids asserting correctness for data that predates this verifier process or sits at the retention boundary.

2. **Ingester window respected**
   - Test ranges must end at or before `now - query_ingesters_within`.
   - Ingesters cover the most recent `query_ingesters_within` duration; correctness tests only verify object-storage-backed data.
   - Indexes whose `MinRecordTs` falls within the ingester window are filtered out — they contain data still covered by ingesters.

3. **Cross-system verification**
   - The cycle queries Loki, then calls `ProvideHints`, then scores locally. `verifyHints` and `verifyMetricSamples` do not fetch hints.
   - A log check passes result entries to `verifyHints`. It is `correct` only when every entry is covered and at least one entry came back (`FalseNegatives == 0 && TotalResults > 0`). False positives are hint ranges with no sampled timestamp. They are tracked and do not affect correctness. Timestamp coverage is inclusive on both ends.
   - A metric check passes samples to `verifyMetricSamples`, which calls `verification.ScoreMetricSamples`. Only a sample with value greater than zero is scored. It is covered when any hint overlaps its lookback, and a miss records that whole lookback. `Correct` is `FalseNegatives == 0`, so an empty sample set is correct. Whether a metric check should also require a scored sample is still open. Metric checks do not count false positives.
   - `getRangeBounds` builds the hint-lookup window. A metric range query aligns onto a one-minute step, then widens by the `[5m]` lookback. An instant query collapses to the cycle end and only widens. A log selector does not widen. Index overlap, `ProvideHints`, and the ingester-window check use that window. The Loki verification query stays on the cycle start and end. A metric range query sends the one-minute step. An instant query is evaluated at the cycle end.
   - Cycles pick among six shapes with equal weight: structured metadata label filter (`sm_label_filter`), stream label filter (`label_filter`), post-parser JSON (`json_label_filter`: `{sel} | json | field="value"` from a top-level JSON string on the sampled line), line filter (`|= "needle"`), metric range (`metric_range`), and metric instant (`metric_instant`). The chosen path is tried first. On failure the cycle falls back to a line-filter needle. A metric check wraps that line-filter needle as `sum(count_over_time(<needle> [5m]))`.
   - JSON fields must be LogQL identifiers, long enough for n-grams, `IsVerbatimLineLiteral`, and a substring of the raw line. Keys that already exist as stream or SM labels are skipped (`| json` extracts those as `name_extracted`, so `| name="..."` would filter the stream/SM label and can return zero hits).
   - Loki query requests must send `X-Loki-Response-Encoding-Flags: categorize-labels` so SM appears on entries (otherwise Loki folds it into stream labels). Loki and `ProvideHints` must see the same query string. Logline looks up value n-grams for the log shapes and for the inner needle of a metric check. Loki's result set differs.

4. **Operator visibility**
   - Every non-skipped cycle emits a `msg="correctness cycle step"` log at each major step: `picked_range`, `picked_label_value`, `fetched_sample_logs`, `picked_needle`, `fetched_needle_results`, `fetched_hints`.
   - Final outcome is logged as `msg="correctness cycle completed"` with the cycle report (selector, range, needle, candidate counts, true/false positives, correctness).
   - A log miss logs `false_negative_timestamps` and `excluded_covering_fn_count` on `correctness failure detail`, and each matching index as `correctness failure FN covered by ingester-window index`. Those lines are candidates for the Loki event-time vs logline `min_rec_ts` asymmetry (metadata only; no term probe).
   - A metric miss logs `false_negative_ranges` on `correctness failure detail`. Each range is that sample's whole lookback. There is no timestamp, so the ingester-window index lines are not emitted.
   - Aggregate counters are exported via Prometheus metrics.

5. **Queryable token requirement**
   - Needles must produce at least one n-gram using the same normalization path as indexing (`logline.ExtractFeatures`).

6. **Debug handler consistency**
   - The `/correctness/debug` handler must use the same core logic as `runVerificationCycle`: `buildLineFilterHintQuery` / `buildLabelFilterHintQuery` / `buildJSONLabelFilterHintQuery` / `buildMetricQuery` + `newHintQuery` for query construction (via `query_type`), `getRangeBounds` for the hint lookup, `queryLogs` or `queryMetrics` for the Loki query, and `verifyHints` or `verifyMetricSamples` for coverage.
   - The Loki query uses the requested start and end. A metric range query sends the one-minute step. An instant query is evaluated at the requested end. The hint lookup uses the widened window from `getRangeBounds`.
   - Log coverage uses `RangeCoversTimestamp`. Metric coverage uses `RangesOverlap` on each positive sample's lookback.
   - Never duplicate or inline these primitives. Drift between the debug endpoint and the cycle makes the debug endpoint unreliable for diagnosing correctness failures.
