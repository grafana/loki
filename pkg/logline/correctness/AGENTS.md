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
   - For the selected needle, sampled Loki result entries are passed to `verifyHints` (defined in `verify.go`).
   - `verifyHints` calls `QueryHintProvider.ProvideHints` and performs coverage analysis locally.
   - A test is only `correct` when all sampled results are covered by hint ranges and at least one sampled result exists.
   - False positives are hint ranges with no sampled result timestamp; they are tracked via metrics but do not affect correctness.
   - Coverage boundaries in `coverTimestamp` are inclusive on both start and end.
   - `verificationReport.Correct` requires zero false negatives and at least one result (`FalseNegatives == 0 && TotalResults > 0`).
   - Cycles pick among four hint-query shapes with equal weight: structured
     metadata label filter (`sm_label_filter`), stream label filter
     (`label_filter`), post-parser JSON (`json_label_filter`:
     `{sel} | json | field="value"` from a top-level JSON string on the
     sampled line), and line filter (`|= "needle"`). The chosen path is
     tried first; on failure the cycle falls back to a line-filter needle.
     JSON fields must be LogQL identifiers, long enough for n-grams,
     `IsVerbatimLineLiteral`, and a substring of the raw line. Keys that
     already exist as stream or SM labels are skipped (`| json` extracts
     those as `name_extracted`, so `| name="..."` would filter the stream/SM
     label and can return zero hits).
     Loki query requests must send
     `X-Loki-Response-Encoding-Flags: categorize-labels` so SM appears on
     entries (otherwise Loki folds it into stream labels). Loki and ProvideHints
     must see the same query string: Logline looks up value n-grams for all
     four shapes, but Loki's result set differs.

4. **Operator visibility**
   - Every non-skipped cycle emits a `msg="correctness cycle step"` log at each major step: `picked_range`, `picked_label_value`, `fetched_sample_logs`, `picked_needle`, `fetched_verification_results`.
   - Final outcome is logged as `msg="correctness cycle completed"` with the cycle report (selector, range, needle, candidate counts, true/false positives, correctness).
   - On incorrect cycles, `correctness failure detail` includes `excluded_covering_fn_count`, and each matching index is logged as `correctness failure FN covered by ingester-window index` — candidates for the Loki event-time vs logline `min_rec_ts` asymmetry (metadata only; no term probe).
   - Aggregate counters are exported via Prometheus metrics.

5. **Queryable token requirement**
   - Needles must produce at least one n-gram using the same normalization path as indexing (`logline.ExtractFeatures`).

6. **Debug handler consistency**
   - The `/correctness/debug` handler must use the same core logic as `runVerificationCycle`: `buildLineFilterHintQuery` / `buildLabelFilterHintQuery` / `buildJSONLabelFilterHintQuery` + `newHintQuery` for query construction (via `query_type`), `rangeCoversTimestamp` for boundary checks, and `verifyHints` for coverage analysis.
   - Never duplicate or inline these primitives — drift between the debug endpoint and the cycle makes the debug endpoint unreliable for diagnosing correctness failures.
