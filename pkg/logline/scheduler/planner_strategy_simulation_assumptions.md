# Planner Strategy Simulation: Assumption Inventory

Assumptions about system behavior, workload shape, or performance that diverge from production reality. Good candidates for periodic recalibration.

## Data sources

**Log lines are the primary calibration source.** Per-job logs (`"compaction complete"` joined with `"job completed"` by `job_id`) give exact (input_bytes, output_bytes, sources, duration) tuples per job — no bucketing error, no aggregation ambiguity. Always prefer log-line analysis for fitting model coefficients.

**Histograms are useful for dashboards, alerting, and quick sanity checks** but should not be used alone for model calibration. Bucket boundaries introduce quantization error (e.g., ~15s resolution in the 60-120s range), per-worker aggregation hides per-job variance, and duration formats like `1m23s` are only visible in logs.

Use both: logs to fit the model, histograms to monitor drift.

## Verify

These values are baked into `defaultSimConfig` or `estimateDurationSeconds`. Recalibrate periodically from production data.

### `estimateDurationSeconds` — `29.1 + 0.0811*MiB` + `2.3s` per source beyond 2

Recalibrated 2026-03-05 from a development environment's per-job logs after streaming merge with bounded block cache (n=75 jobs: 70 raw pairs ~980 MiB + 5 compacted pairs 3000-3700 MiB, all 2-source):

    least-squares fit: 29.1 + 0.0811*MiB
    median |residual| = 9.7s, p95 |residual| = 20.8s

**Why the model changed:** The streaming merge (PR #57) replaced the batch `MergeIndexFiles` with `StreamingMergeIndexFiles` and a bounded 2-block LRU cache in the fast postings reader. This is ~30% faster per MiB than batch (slope 0.081 vs 0.135) because it avoids materializing all bitmaps in memory. The higher intercept (29.1 vs 5.5) captures fixed costs (GCS download, upload, iterator init, term dictionary decode) that are proportionally larger now that the per-MiB merge cost is lower. The wider input range (980-3700 MiB vs previously 50-490 MiB) also improved the fit — the old model was extrapolating well beyond its calibration range for large inputs.

**Per-job log observations (2026-03-05, streaming merge, overlap planner, 50 workers):**
- Two distinct populations: raw pairs (~980 MiB) and compacted pairs (~3000-3700 MiB)
- Raw pairs: p50 duration=105s, input p50=981 MiB
- Compacted pairs: p50 duration=281s, input p50=3224 MiB
- All jobs were 2-source (overlap planner groups pairs)
- Overlap does NOT independently affect duration — input MiB is the dominant predictor
- Per-source penalty (2.3s) retained from prior calibration (2026-03-04, dateGroup planner with multi-source jobs)

**Prior calibration (2026-03-05, batch merge, n=646 jobs):** `5.5 + 0.135*MiB`, median |residual|=2.0s. That model was calibrated on smaller inputs (p50=205 MiB, p95=490 MiB) and overestimates by ~30% at 1000+ MiB. The low residual reflected the narrow input range, not model accuracy at scale.

**Use log lines** for recalibration. The `"compaction complete"` log emits `input_bytes`, `output_bytes`, and `sources` (count). Join with `"job completed"` (same `job_id`) for exact `duration`. This gives per-job (input_bytes, source_count, duration) triples to fit the linear model against. Note: Go durations may be formatted as `1m23.456s` — parse the full string, not just the leading number.

**Histograms** for monitoring:
```promql
histogram_quantile(0.50, rate(logline_index_compactor_compaction_duration_seconds_bucket[1h]))
histogram_quantile(0.95, rate(logline_index_compactor_compaction_duration_seconds_bucket[1h]))
```

Sub-phase histograms for spotting which phase dominates:
```promql
histogram_quantile(0.95, rate(logline_index_compactor_download_duration_seconds_bucket[1h]))
histogram_quantile(0.95, rate(logline_index_compactor_merge_duration_seconds_bucket[1h]))
histogram_quantile(0.95, rate(logline_index_compactor_upload_duration_seconds_bucket[1h]))
```

### `outputSizeRatio` — `BaseReductionRatio * (1 - overlap * 0.5)`

Recalibrated 2026-03-05 from a development environment's per-job logs, now including both raw and compacted input pairs (n=76 jobs, streaming merge, overlap planner, all 2-source):

**Raw+raw pairs** (n=70, ~980 MiB inputs — two raw builder indexes with real overlap):
- p05=0.775, p25=0.798, p50=0.824, p75=0.860, p95=0.907
- Implies ~31% effective overlap via the model: `0.97 * (1 - 0.31*0.5) = 0.82`

**Compacted+compacted pairs** (n=6, ~3000-3700 MiB inputs — previously-unmergeable indexes, already internally deduplicated):
- p50=0.952, min=0.926, max=1.018
- Near-zero cross-pair overlap → ratio ≈ BaseReductionRatio
- The 1.018 outlier (output > input) reflects metadata overhead exceeding dedup in a zero-overlap merge

BaseReductionRatio=0.97 confirmed. The compacted+compacted pairs (0.95) provide a direct measurement of the base ratio independent of overlap, matching the 0.97 value within the expected ~2% metadata overhead. The overlap coefficient (0.5) remains reasonable: raw pairs at ~31% overlap produce the observed 0.82 ratio.

**Prior calibration (2026-03-05, batch merge, n=646 jobs):** overall median=0.897 on smaller inputs (p50=205 MiB). The wider input range now confirms the model holds across both raw (high-overlap, ratio~0.82) and compacted (low-overlap, ratio~0.95) populations.

**Use log lines** for recalibration. The `"compaction complete"` log emits `input_bytes` and `output_bytes` per job. Compute `output_bytes / input_bytes` for the per-job ratio.

**Histograms** for monitoring:
```promql
rate(logline_index_compactor_bytes_uploaded_total[1h]) / rate(logline_index_compactor_bytes_downloaded_total[1h])
```

### `IndexesPerBuilderPerHour` (default: 44.0)

Recalibrated 2026-03-06 from a development environment's aggregated metrics (3h window, 7 active builders):

    fleet total: ~310 files/hr (sum of per-builder rates)
    active builders: 7 (builders 0-6, with 10-14 scaling up)
    per-builder: 310/7 ≈ 44/hr

**Prior calibration (2026-03-05):** 14.3/hr (fleet 215/hr ÷ 15 active builders). Fleet shrank from 15 to 7 active builders, but each builder now handles more partitions, producing ~3x more indexes/hr. Fleet total increased from ~215/hr to ~310/hr.

**Metrics are sufficient.** `index_files_written_total` is an exact counter.
```promql
# fleet total rate
sum(rate(logline_index_builder_index_files_written_total{namespace="<namespace>"}[1h]))
# active builder count
count(rate(logline_index_builder_index_files_written_total{namespace="<namespace>"}[1h]) > 0)
```

Note: a single flush can produce multiple index files (one per date bucket). `index_files_written_total` counts files, not flushes. The `"flush complete"` log line includes `files` count if per-flush detail is needed.

### Raw index size distribution — weighted-bucket model (calibrated 2026-03-06)

Replaced the uniform [45, 135] MiB model with a weighted-bucket sampler derived
from a measured index size histogram (n=5,367 indexes, 2026-03-06):

| Bucket (MiB)  | Count | Weight |
|---------------|-------|--------|
| 0 – 1         | 124   | 2.3%   |
| 1 – 5         | 1     | 0.0%   |
| 5 – 10        | 4     | 0.1%   |
| 10 – 20       | 408   | 7.6%   |
| 20 – 40       | 1,641 | 30.6%  |
| 40 – 60       | 1,387 | 25.8%  |
| 60 – 80       | 723   | 13.5%  |
| 80 – 100      | 390   | 7.3%   |
| 100 – 150     | 644   | 12.0%  |
| 150 – 256     | 44    | 0.8%   |

Simulated mean under this model ≈ 66 MiB vs production p50 = 72 MiB — much
closer than the old uniform mean of 90 MiB (25% above median). The remaining
gap reflects within-bucket non-uniformity (intra-bucket sampling is still
uniform).

`RawIndexSizeMin` and `RawIndexSizeMax` config fields are removed. The
distribution is not configurable via YAML; recalibrate by editing
`simSizeBuckets` directly in the test file.

**Histograms** for recalibration (run against a representative environment after fleet changes):
```promql
sum(increase(logline_index_builder_index_file_size_bytes_bucket{namespace="<namespace>"}[3h])) by (le)
```

## Assume

- No I/O contention — job duration is independent of concurrent worker count. Real compaction shares object store throughput.
- Raw index sizes follow the measured weighted-bucket histogram (2026-03-06). Within each bucket, sizes are sampled uniformly; intra-bucket skew is not modeled.
- Each raw index covers `1h / IndexesPerBuilderPerHour` of logs (~1.4min at default 44/h). Flushes are uniformly spread through each tick. In production, multiple builders share stream partitions, so indexes from different builders covering the same time range will have real overlap — the sim doesn't model this cross-builder overlap.
- Single synthetic date (`day-000`) — `dateGroupPlanner` only ever sees one date group, unlike production which has multiple dates.
