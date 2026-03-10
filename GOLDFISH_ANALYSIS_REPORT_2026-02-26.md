# Goldfish Mismatch Analysis Report

**Date Range:** 2026-02-24 13:00 UTC to 2026-02-26 13:00 UTC (48 hours)
**Total Queries Executed:** 9,575
**Match Rate:** 84.5%
**Total Mismatches Analyzed:** 1,000 (capped by page size)
**Unique Pattern Signatures:** 92
**Unique LogQL Features Detected:** 19
**Avg Performance Difference:** 21.2%

---

## Executive Summary

Over the past 48 hours, Goldfish detected mismatches in ~15.5% of queries (1,487+ mismatches out of 9,575 queries). This report analyzes the first 1,000 mismatches to identify the highest-impact correctness bugs in the v2 query engine.

### Top 3 Most Impactful Findings

1. **Regex line filter combinations (`!~` + `|~`) account for 27.7% of all mismatches** (277 queries). These are Logs Drilldown error-detection queries like `!~ "debug|DEBUG|info|INFO" |~ "error|ERROR|fatal|FATAL"`. While each individual delta is moderate (~27KB avg), the sheer volume makes this the #1 pattern to fix.

2. **`max_over_time` with `unwrap` produces the largest per-query data discrepancies** (up to 3.9MB). The v2 engine returns dramatically different results -- in one case cellA returned 0 entries while cellB returned 24,110 entries (3.9MB). Only 14 occurrences but avg delta is 279KB per query.

3. **Metric queries (`count_over_time`, `rate`) with the v2 engine return orders-of-magnitude more data points** -- e.g., 30 entries (v1) vs 30,000 entries (v2). This suggests a fundamental step/interval calculation difference in the v2 engine for range vector queries.

### Critical Performance Regression

The v2 engine (cellB) shows dramatic execution time regressions: up to **348 seconds** vs **2.9 seconds** for the same query. The top 15 slowest queries all show v2 being 100-4000x slower than v1. This may indicate missing query optimizations or plan inefficiencies in the new engine.

---

## Category 1: Top 10 Patterns by Occurrence

Patterns are defined as the sorted set of LogQL features used in a query. The same pattern groups structurally similar queries together.

| Rank | Pattern Signature | Count | % of Total | Avg Size Delta | Total Size Delta |
|------|-------------------|-------|-----------|----------------|-----------------|
| 1 | `line_regex_match, line_regex_not_match` | 277 | 27.7% | 27,150 B | 7,520,641 B |
| 2 | `drop, group_by, line_contains, range_count_over_time` | 77 | 7.7% | 90 B | 6,919 B |
| 3 | `line_contains` | 66 | 6.6% | 27,889 B | 1,840,655 B |
| 4 | _(no features / plain stream selector)_ | 62 | 6.2% | 4,276 B | 265,138 B |
| 5 | `drop, group_by, range_count_over_time` | 33 | 3.3% | 84 B | 2,759 B |
| 6 | `agg_sum, label_filter, range_rate` | 29 | 2.9% | 2,009 B | 58,274 B |
| 7 | `group_by, logfmt_parser, range_sum_over_time, unwrap` | 28 | 2.8% | 176 B | 4,941 B |
| 8 | `drop, group_by, line_regex_match, range_count_over_time` | 27 | 2.7% | 77 B | 2,082 B |
| 9 | `line_contains, logfmt_parser` | 26 | 2.6% | 10,383 B | 269,948 B |
| 10 | `line_contains, range_count_over_time` | 26 | 2.6% | 260 B | 6,766 B |

### Pattern Analysis

**Pattern #1: `line_regex_match, line_regex_not_match` (277 queries, 27.7%)**
These are almost all Logs Drilldown error-level queries:
```logql
{cluster="...", namespace="..."} !~ "debug|DEBUG|info|INFO" |~ "error|ERROR|fatal|FATAL"
```
Both engines return 1,000 entries but the response bodies differ by ~27KB on average, suggesting differences in how log lines are returned or ordered. **Correlation ID:** `bff75357-fb0d-4a7c-a08f-30d1f2ae3b41`

**Pattern #2: `drop, group_by, line_contains, range_count_over_time` (77 queries, 7.7%)**
Logs Drilldown level-detection queries:
```logql
sum by (level, detected_level) (count_over_time({...} |= "..." | drop __error__[1m]))
```
Small deltas (~90 B) but high frequency. Likely rounding or precision differences. **Correlation ID:** `e60075c1-d0cb-4266-aa70-6bf3a9d8bb11`

**Pattern #3: `line_contains` (66 queries, 6.6%)**
Simple log queries with `|=` filter:
```logql
{namespace=~"tempo-prod.*", container="live-store"} |= "cannot guarantee complete results"
```
Average delta 27KB -- both return 1,000 entries but content differs. **Correlation ID:** `6cbcd8aa-f1c5-498d-b18f-f2a382ef7362`

**Pattern #4: _(no features)_ (62 queries, 6.2%)**
Plain stream selectors with no pipeline:
```logql
{cluster="prod-au-southeast-1", job="onboarding/integrations-api-primary"}
```
Even basic queries show mismatches, suggesting differences in stream selection or result ordering. **Correlation ID:** `50a00487-8fac-4c79-939b-4c195e0474ed`

---

## Category 2: Top 10 by Data Discrepancy

Ranked by absolute response size delta. Queries where v1 and v2 return the most different amounts of data.

| Rank | Size Delta | v1 Size | v2 Size | v1 Entries | v2 Entries | Pattern | Example Query |
|------|-----------|---------|---------|------------|------------|---------|---------------|
| 1 | **3,909 KB** | 231 B | 3,909 KB | 0 | 24,110 | `group_by, json_parser, label_filter, line_not_contains, range_max_over_time, unwrap` | `sum by(cluster, pod, sessionID) (max_over_time({container="crocochrome"...} \| json...` |
| 2 | **781 KB** | 785 KB | 3.6 KB | 1,000 | 0 | `label_filter, line_regex_match` | `{...container="ruler"} \|~ "(?i)(error\|fail\|err)" \|~ "(?i)(storage..." \| __error__=""` |
| 3 | **696 KB** | 700 KB | 3.6 KB | 1,000 | 0 | `line_regex_not_match` | `{...namespace="cortex-dedicated-14", pod!~"distributor"}` |
| 4 | **638 KB** | 641 KB | 3.6 KB | 1,000 | 0 | `label_filter, line_regex_match` | `{...container="ruler"} \|~ "(?i)(storage\|timeout\|slow..." \| __error__=""` |
| 5 | **396 KB** | 4,740 KB | 4,344 KB | 1,000 | 1,000 | `line_contains` | `{job="hosted-exporters/azure-monitor-exporter-primary"} \|= "caused no m...` |
| 6 | **360 KB** | 4,724 KB | 4,364 KB | 1,000 | 1,000 | `line_contains` | `{job="hosted-exporters/azure-monitor-exporter-primary"} \|= "caused no m...` |
| 7 | **358 KB** | 4,844 KB | 4,485 KB | 1,000 | 1,000 | `line_contains` | `{job="hosted-exporters/azure-monitor-exporter-primary"} \|= "caused no m...` |
| 8 | **353 KB** | 4,839 KB | 4,485 KB | 1,000 | 1,000 | `line_contains` | `{job="hosted-exporters/azure-monitor-exporter-primary"} \|= "caused no m...` |
| 9 | **314 KB** | 318 KB | 3.6 KB | 200 | 0 | `label_filter` | `{...container="ruler"} \| detected_level="warn" \| __error__=""` |
| 10 | **259 KB** | 4,239 KB | 3,979 KB | 1,000 | 1,000 | `label_filter, line_contains, logfmt_parser` | `{...container="query-engine-scheduler"} \|= "failed to create physical...` |

### Key Observations

**Complete data loss (v2 returns 0 entries):**
- Ranks #2, #3, #4, #9 all show v2 returning **zero entries** while v1 returns hundreds or thousands. These represent the most severe correctness bugs where v2 silently drops all results.
- Rank #2 and #4: `__error__=""` label filter after regex line filters -- v2 may be evaluating `__error__` incorrectly.
- Rank #3: Simple `pod!~"distributor"` negation regex in stream selector -- v2 may have a bug in stream-level regex negation.
- Rank #9: `detected_level="warn" | __error__=""` -- again `__error__` filter related.

**Massive over-return (v2 returns far more data):**
- Rank #1 is the most striking: v1 returns 0 entries/231 bytes, v2 returns 24,110 entries/3.9MB. This is a `max_over_time` query with `json | unwrap` -- v2 may be failing to apply filters or unwrap correctly.

**Content differences (same entry count, different content):**
- Ranks #5-8 and #10 show both engines returning 1,000 entries but with different response bodies (up to 396KB difference). This suggests differences in log line content, metadata, or ordering.

---

## Category 3: Top 10 by Performance Difference

Ranked by absolute execution time delta between v1 (cellA) and v2 (cellB).

| Rank | Time Delta | v1 Time | v2 Time | Slowdown | Pattern | Example Query |
|------|-----------|---------|---------|----------|---------|---------------|
| 1 | **348s** | 2.9s | 351s | 120x | `drop, group_by, range_count_over_time` | `sum by (level, detected_level) (count_over_time({...} \| drop __error__[...]))` |
| 2 | **347s** | 1.9s | 349s | 185x | `drop, group_by, line_not_contains, line_regex_match, range_count_over_time` | `sum by (level, detected_level) (count_over_time({...grafana-com...} ...` |
| 3 | **340s** | 1.7s | 342s | 197x | `drop, group_by, line_not_contains, line_regex_match, range_count_over_time` | _(same pattern as #2)_ |
| 4 | **330s** | 1.6s | 332s | 203x | `drop, group_by, line_not_contains, line_regex_match, range_count_over_time` | _(same pattern as #2)_ |
| 5 | **304s** | 0.08s | 304s | **3,948x** | `json_parser, line_contains` | `{...} \|= "mismatch" \| json` |
| 6 | **283s** | 0.6s | 284s | 451x | `logfmt_parser` | `{...container="rollout-operator"} \| logfmt` |
| 7 | **279s** | 1.4s | 280s | 201x | `agg_sum, line_contains, line_regex_match, line_regex_not_match, range_rate` | `sum(rate({...} !~ "debug..." \|~ "error..." \|= "fail..."[...]))` |
| 8 | **260s** | 2.9s | 263s | 92x | `line_regex_match` | `{namespace=~"crossplane.*"} \|~ "(?i)(BindCompositeResource\|...)"` |
| 9 | **260s** | 0.3s | 261s | 875x | `agg_sum, json_parser, label_filter, range_count_over_time` | `sum(count_over_time({...} \| json \| status_code >= 500 [...]))` |
| 10 | **258s** | 0.6s | 258s | 427x | `line_regex_match, line_regex_not_match` | `{...} !~ "debug\|DEBUG\|info\|INFO" \|~ "error\|ERROR\|fatal\|FATAL"` |

### Key Observations

- **Every single top-10 performance regression is v2 being slower than v1**, often by 100-4000x.
- The worst case is **3,948x slowdown** for a simple `|= "mismatch" | json` query (77ms vs 304 seconds).
- Logs Drilldown queries (`sum by (level, detected_level) (count_over_time(...))`) are particularly affected, with multiple entries at 330-348 seconds.
- Parser-based queries (`| logfmt`, `| json`) seem especially prone to performance regressions.
- The v2 engine appears to have significant optimization gaps for queries involving:
  - Range aggregations with parsers
  - Regex line filters on large streams
  - JSON/logfmt parsing pipelines

---

## Feature Frequency Analysis

How often each LogQL feature appears across all 1,000 mismatches, and its average impact.

| Feature | Count | % of Mismatches | Avg Size Delta | Impact Assessment |
|---------|-------|----------------|----------------|-------------------|
| `line_regex_match` (`\|~`) | 373 | 37.3% | 25,592 B | **HIGH** - Most common filter in mismatches |
| `group_by` | 345 | 34.5% | 11,606 B | MEDIUM - Common in metric queries |
| `line_contains` (`\|=`) | 340 | 34.0% | 9,460 B | MEDIUM - Very common filter |
| `range_count_over_time` | 331 | 33.1% | 352 B | LOW delta - Frequent but small impact |
| `line_regex_not_match` (`!~`) | 323 | 32.3% | 26,626 B | **HIGH** - Paired with `\|~` in top pattern |
| `drop` | 215 | 21.5% | 415 B | LOW delta - Likely incidental |
| `label_filter` | 191 | 19.1% | 38,310 B | **HIGH** - Highest avg delta of common features |
| `logfmt_parser` | 163 | 16.3% | 8,309 B | MEDIUM - Parser-related bugs |
| `line_not_contains` (`!=`) | 116 | 11.6% | 36,162 B | **HIGH** - High avg delta |
| `agg_sum` | 103 | 10.3% | 922 B | LOW delta |
| `json_parser` | 75 | 7.5% | 62,056 B | **CRITICAL** - Very high avg delta |
| `range_rate` | 60 | 6.0% | 1,319 B | LOW delta |
| `unwrap` | 52 | 5.2% | 75,410 B | **CRITICAL** - Highest avg delta |
| `range_sum_over_time` | 29 | 2.9% | 174 B | LOW |
| `range_max_over_time` | 14 | 1.4% | **279,437 B** | **CRITICAL** - Extreme per-query impact |
| `binary_arithmetic` | 12 | 1.2% | 107 B | LOW |
| `range_avg_over_time` | 9 | 0.9% | 463 B | LOW |
| `agg_count` | 3 | 0.3% | 136 B | LOW |
| `agg_avg` | 1 | 0.1% | 95 B | LOW |

### Impact Matrix

| | Low Frequency (<5%) | Medium Frequency (5-20%) | High Frequency (>20%) |
|---|---|---|---|
| **High Avg Delta (>25KB)** | `range_max_over_time` (279KB) | `unwrap` (75KB), `json_parser` (62KB), `label_filter` (38KB), `line_not_contains` (36KB) | `line_regex_not_match` (27KB), `line_regex_match` (26KB) |
| **Medium Avg Delta (5-25KB)** | | `logfmt_parser` (8KB) | `group_by` (12KB), `line_contains` (9KB) |
| **Low Avg Delta (<5KB)** | `binary_arithmetic`, `range_avg_over_time`, `agg_count`, `agg_avg` | `agg_sum` (1KB), `range_rate` (1KB) | `range_count_over_time` (352B), `drop` (415B) |

---

## Key Insights & Recommendations

### Insight 1: Three Classes of Bugs

The mismatches fall into three distinct categories:

1. **Content/ordering differences** (most common, ~60%): Both engines return similar entry counts but different response bodies. Patterns: `line_regex_match,line_regex_not_match`, `line_contains`, plain selectors. Likely caused by log line ordering, deduplication, or metadata differences.

2. **Complete data loss in v2** (~10%): v2 returns 0 entries where v1 returns hundreds+. Patterns involving `__error__=""` label filter, `pod!~` stream-level negation. These are the most severe correctness bugs.

3. **Data explosion in v2** (~5%): v2 returns far more data than v1 (e.g., 30 vs 30,000 entries). Primarily affects metric queries with range aggregations. Suggests step/interval calculation bugs.

### Insight 2: Priority Ranking by Impact

Combining frequency and severity:

| Priority | Bug Class | Frequency | Per-Query Impact | Total Impact | Effort |
|----------|-----------|-----------|-----------------|--------------|--------|
| **P0** | `__error__=""` label filter drops all results | ~25 queries | 314-800 KB (100% data loss) | HIGH | Medium |
| **P0** | `max_over_time` + `unwrap` correctness | 14 queries | 279 KB avg (up to 3.9MB) | HIGH | Medium |
| **P1** | Metric step/interval calculation (count_over_time/rate) | ~50 queries | Up to 30K entry difference | HIGH | High |
| **P1** | Regex line filter result differences (`!~` + `\|~`) | 277 queries | 27 KB avg | VERY HIGH (7.5MB total) | Medium |
| **P2** | Simple log query ordering/content differences | 128 queries | 4-28 KB avg | MEDIUM | Low |
| **P2** | Performance regressions (100-4000x slower) | ~50 queries | N/A (correctness OK) | HIGH (user experience) | High |

### Insight 3: Estimated Impact of Fixes

Fixing the top 5 patterns would address:
- **Pattern 1** (`line_regex_match,line_regex_not_match`): 277 queries = **27.7% of all mismatches**
- **Pattern 2** (`drop, group_by, line_contains, range_count_over_time`): 77 queries = **7.7%**
- **Pattern 3** (`line_contains`): 66 queries = **6.6%**
- **Pattern 4** (plain selectors): 62 queries = **6.2%**
- **Pattern 5** (`drop, group_by, range_count_over_time`): 33 queries = **3.3%**

**Combined: 515 queries = 51.5% of all mismatches** could be resolved by fixing just 5 pattern classes.

---

## Next Steps

### Immediate Actions (P0)

1. **Investigate `__error__=""` label filter behavior in v2**
   - Queries with `| __error__=""` return 0 results in v2 but correct results in v1
   - Example: `{...} |~ "(?i)(error|fail)" | __error__=""`
   - Correlation IDs: `6885893c`, `d10aae25`, `5697154c`
   - This affects label filtering correctness after line filters

2. **Fix `max_over_time` + `unwrap` + `json` pipeline**
   - v2 returns 24,110 entries / 3.9MB where v1 returns 0 entries / 231 bytes
   - Or vice versa -- one engine is very wrong
   - Correlation ID: `22dea2b0-904a-4adb-88af-cdb934e164e3`

### Short-Term Actions (P1)

3. **Investigate metric step/interval calculation differences**
   - `count_over_time` and `rate` queries return 1000x different entry counts
   - Likely a step alignment or interval width bug in v2
   - Correlation IDs: `c61aa541`, `bb61f35b`, `6d98024f`

4. **Address regex line filter combination differences**
   - The `!~ "debug|..." |~ "error|..."` pattern accounts for 27.7% of mismatches
   - Both engines return 1,000 entries but different content
   - May be an ordering, deduplication, or filter evaluation order issue
   - Correlation ID: `bff75357-fb0d-4a7c-a08f-30d1f2ae3b41`

### Medium-Term Actions (P2)

5. **Profile v2 engine performance for parser queries**
   - `| json` and `| logfmt` queries show 100-4000x slowdowns
   - May need query plan optimization or parallel execution improvements
   - Correlation ID: `e63e9cf8` (3,948x slowdown for json parse)

6. **Create targeted correctness tests**
   - Use the `/goldfish:recreate-mismatch` skill to reproduce top patterns as bench tests
   - Focus on: `__error__` filter, `max_over_time`+`unwrap`, metric step calculation
   - Each pattern has correlation IDs above for fetching full query details

### Data Files

- **Full analysis data:** `mismatch_analysis.json` (1,000 queries with extracted features)
- **Analysis script:** `analyze_mismatches.py` (reusable for future analyses)
- **Raw data:** Goldfish MCP query with `comparisonStatus=mismatch`, `from=2026-02-24T13:00:00Z`, `to=2026-02-26T13:00:00Z`

---

_Report generated: 2026-02-26 by Goldfish Analysis Skill_
_Data source: Goldfish API (9,575 queries, 1,000 mismatches analyzed)_
