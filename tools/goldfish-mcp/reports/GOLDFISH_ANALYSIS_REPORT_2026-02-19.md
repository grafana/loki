# Goldfish Mismatch Analysis Report

**Date Range:** 2026-02-12 to 2026-02-19
**Total Mismatches:** 1,000
**Unique Patterns:** 90
**Generated:** 2026-02-19 13:58:49

## Executive Summary

This analysis examined 1,000 query mismatches between Loki v1 and v2 APIs over a 7-day period.

### Key Findings

1. **group_by, label_filter, line_contains, line_not_contains, logfmt_parser, range_rate**
   - Occurs in 10 queries (1.0%)
   - Total entry delta: 183,969 (20.5% of all deltas)
   - Average entry delta: 18,396
   - Max entry delta: 63,234
   - Average time delta: 17,948ms

2. **group_by, label_filter, logfmt_parser, range_sum_over_time, unwrap**
   - Occurs in 9 queries (0.9%)
   - Total entry delta: 140,153 (15.6% of all deltas)
   - Average entry delta: 15,572
   - Max entry delta: 30,248
   - Average time delta: 55,658ms

3. **agg_sum, group_by, label_filter, line_contains, logfmt_parser, range_rate, unwrap**
   - Occurs in 8 queries (0.8%)
   - Total entry delta: 108,054 (12.0% of all deltas)
   - Average entry delta: 13,506
   - Max entry delta: 37,684
   - Average time delta: 10,701ms


---

## Category 1: Top 10 Patterns by Occurrence

Patterns that appear most frequently in mismatches:

| Rank | Pattern | Count | % | Avg Entry Δ | Max Entry Δ | Avg Time Δ |
|------|---------|-------|---|-------------|-------------|-------------|
| 1 | drop, json_parser, label_filter, +2 more | 192 | 19.2% | 10 | 11 | 5,183ms |
| 2 | agg_sum, line_contains, range_count_over_time | 92 | 9.2% | 178 | 1,161 | 23,972ms |
| 3 | drop, group_by, line_contains, +1 more | 74 | 7.4% | 327 | 2,648 | 28,183ms |
| 4 |  | 64 | 6.4% | 1 | 31 | 24,074ms |
| 5 | drop, group_by, range_count_over_time | 49 | 4.9% | 735 | 7,584 | 14,572ms |
| 6 | agg_sum, line_regex_match, range_rate | 48 | 4.8% | 68 | 600 | 33,505ms |
| 7 | line_contains | 41 | 4.1% | 2 | 65 | 38,089ms |
| 8 | agg_sum, group_by, label_filter, +3 more | 35 | 3.5% | 3 | 12 | 10,101ms |
| 9 | group_by, label_filter, line_contains, +2 more | 26 | 2.6% | 1,387 | 14,039 | 8,568ms |
| 10 | line_regex_match | 26 | 2.6% | 2 | 15 | 27,929ms |

### Example Queries

**Pattern 1: drop, json_parser, label_filter, line_regex_not_match, logfmt_parser**

```logql
{
  cluster=~"ops-eu-south-0" , namespace="slo-bigquery-sync-loki" , container="bigquery-sync-loki",
  job!~"integration...
```

```logql
{
  cluster=~"ops-eu-south-0" , namespace="slo-bigquery-sync-loki" , container="bigquery-sync-loki",
  job!~"integration...
```

**Pattern 2: agg_sum, line_contains, range_count_over_time**

```logql
sum(count_over_time({app="grafana", org="tritondigital"} |="victoriametrics"|="Plugin Request"|="level=error"[1m]))
```

```logql
sum(count_over_time({kind="event", app_id="83"} |= "ErrorToastDisplayed" [1h]))
```

**Pattern 3: drop, group_by, line_contains, range_count_over_time**

```logql
sum by (level, detected_level) (count_over_time({app="grafana", org="tritondigital"} |="victoriametrics"|="Plugin Reques...
```

```logql
sum by (level, detected_level) (count_over_time({container="community-datasource-grafana-app-main",cluster="prod-us-cent...
```

**Pattern 4: **

```logql
{cluster="prod-us-east-0", namespace="k6-api-prod", pod=~"apisix-ingress-controller-.*"}
```

```logql
{cluster="prod-us-east-0", namespace="k6-api-prod", pod=~"apisix-ingress-controller-.*"}
```

**Pattern 5: drop, group_by, range_count_over_time**

```logql
sum by (level, detected_level) (count_over_time({app="grafana", org="gohighway"} | drop __error__[1m]))
```

```logql
sum by (level, detected_level) (count_over_time({container="archiver-operator", cluster="prod-us-central-5", namespace="...
```


---

## Category 2: Top 10 by Data Discrepancy

Queries with the largest differences in entry counts:

| Rank | Entry Delta | v1 Entries | v2 Entries | % Diff | Query |
|------|-------------|------------|------------|--------|-------|
| 1 | 63,234 | 487 | 63,721 | 12984.4% | `sum by (slug, pod)(rate({namespace="grafana-datasources", cluster=~"prod-us-cent...` |
| 2 | 37,684 | 1,093 | 38,777 | 3447.8% | `sum(rate({cluster="prod-us-east-0", name="ingest-api"} |= "Ingested data sent to...` |
| 3 | 37,684 | 1,093 | 38,777 | 3447.8% | `
avg_over_time({cluster="prod-us-east-0", name="ingest-api"} |= "Ingested data s...` |
| 4 | 37,684 | 1,093 | 38,777 | 3447.8% | `
sum(rate({cluster="prod-us-east-0", name="ingest-api"} |= "Ingested data sent t...` |
| 5 | 37,684 | 1,093 | 38,777 | 3447.8% | `
avg_over_time({cluster="prod-us-east-0", name="ingest-api"} |= "Ingested data s...` |
| 6 | 31,131 | 1,094 | 32,225 | 2845.6% | `
sum(rate({cluster="prod-us-east-0", name="ingest-api"} |= "Ingested data sent t...` |
| 7 | 31,131 | 1,094 | 32,225 | 2845.6% | `
avg_over_time({cluster="prod-us-east-0", name="ingest-api"} |= "Ingested data s...` |
| 8 | 31,012 | 29 | 31,041 | 106937.9% | `
  sum by(cluster) (  
    rate({namespace=~"grafana-frontend-service", service_...` |
| 9 | 30,248 | 5 | 30,253 | 604960.0% | `sum by (detected_level) (sum_over_time({__aggregated_metric__=`grafana/alertmana...` |
| 10 | 26,415 | 5 | 26,420 | 528300.0% | `sum by (detected_level) (sum_over_time({__aggregated_metric__=`grafana/prometheu...` |

---

## Category 3: Top 10 by Performance Difference

Queries with the largest execution time differences:

| Rank | Time Delta | v1 Time (ms) | v2 Time (ms) | % Diff | Query |
|------|------------|--------------|--------------|--------|-------|
| 1 | 299,943ms | 4,492ms | 304,435ms | 6677.3% | `sum by(namespace) (count_over_time({cluster="prod-us-central-0", namespace=~"cor...` |
| 2 | 298,725ms | 675ms | 299,400ms | 44255.6% | `sum by (level, detected_level) (count_over_time({cluster="prod-us-east-0", names...` |
| 3 | 284,501ms | 4,278ms | 288,779ms | 6650.3% | `{namespace="grafana-cloud-migration-service", cluster=~".+", name="grafana-cloud...` |
| 4 | 273,488ms | 3,840ms | 277,328ms | 7122.1% | `sum(rate({cluster="prod-us-east-3",namespace="mimir-prod-66"} !~ "debug|DEBUG|in...` |
| 5 | 269,478ms | 7ms | 269,485ms | 3849685.7% | `sum(rate({service_name="assistant/assistant"} |~ "context canceled" [5m]))` |
| 6 | 268,438ms | 3,465ms | 271,903ms | 7747.1% | `{namespace=~"", cluster=~".+"} |= "error"` |
| 7 | 266,503ms | 0ms | 266,503ms | 26650300.0% | `sum(rate({service_name="assistant/assistant"} | detected_level="error" [5m]))` |
| 8 | 264,286ms | 2,120ms | 266,406ms | 12466.3% | `{service_name="assistant/assistant"} | detected_level="error"` |
| 9 | 260,001ms | 0ms | 260,001ms | 26000100.0% | `sum(rate({service_name="assistant/assistant"} | detected_level="error" [5m]))` |
| 10 | 257,779ms | 0ms | 257,779ms | 25777900.0% | `sum(rate({service_name="assistant/assistant"} |~ "context canceled" [5m]))` |

---

## Category 4: Patterns by Total Time Impact

Patterns with the largest total execution time differences:

| Rank | Pattern | Count | Total Time Δ | Avg Time Δ | Max Time Δ |
|------|---------|-------|--------------|------------|-------------|
| 1 | agg_sum, line_contains, range_count_over_time | 92 | 2,205,430ms | 23,972ms | 59,736ms |
| 2 | drop, group_by, line_contains, +1 more | 74 | 2,085,596ms | 28,183ms | 298,725ms |
| 3 | agg_sum, line_regex_match, range_rate | 48 | 1,608,243ms | 33,505ms | 269,478ms |
| 4 | line_contains | 41 | 1,561,662ms | 38,089ms | 268,438ms |
| 5 |  | 64 | 1,540,772ms | 24,074ms | 264,286ms |
| 6 | line_regex_match, line_regex_not_match | 7 | 1,200,124ms | 171,446ms | 203,647ms |
| 7 | drop, json_parser, label_filter, +2 more | 192 | 995,229ms | 5,183ms | 212,953ms |
| 8 | group_by, label_filter, logfmt_parser, +2 more | 14 | 894,245ms | 63,874ms | 69,668ms |
| 9 | agg_sum, range_rate | 13 | 838,466ms | 64,497ms | 266,503ms |
| 10 | line_regex_match | 26 | 726,175ms | 27,929ms | 227,148ms |

---

## Feature Frequency Analysis

LogQL features that appear most often in mismatches:

| Feature | Count | % of Queries | Avg Entry Δ |
|---------|-------|--------------|-------------|
| `label_filter` | 510 | 51.0% | 1,476 |
| `logfmt_parser` | 463 | 46.3% | 1,453 |
| `range_count_over_time` | 413 | 41.3% | 549 |
| `group_by` | 407 | 40.7% | 2,117 |
| `drop` | 395 | 39.5% | 220 |
| `line_contains` | 387 | 38.7% | 1,611 |
| `json_parser` | 242 | 24.2% | 340 |
| `line_regex_not_match` | 237 | 23.7% | 39 |
| `agg_sum` | 235 | 23.5% | 793 |
| `line_regex_match` | 157 | 15.7% | 42 |
| `range_rate` | 124 | 12.4% | 2,692 |
| `line_not_contains` | 107 | 10.7% | 2,831 |
| `unwrap` | 54 | 5.4% | 8,109 |
| `binary_logical` | 51 | 5.1% | 14 |
| `range_avg_over_time` | 25 | 2.5% | 4,321 |

---

## Key Insights & Recommendations

### Impact Analysis

- The top 5 patterns (by entry delta) account for **614,901 entries** (68.5%) of total impact
- The top 5 patterns (by time) account for **9,001,703ms** of total time difference
- Most frequent pattern appears in 192 queries (19.2%)
- 25 patterns appear only once
- Average entry delta per query: 897

### Priority Recommendations

Fix these patterns first for maximum impact:

1. **group_by, label_filter, line_contains, line_not_contains, logfmt_parser, range_rate**
   - Entry impact: 183,969 (20.5% of total)
   - Time impact: 179,488ms total, 17,948ms avg
   - Frequency: 10 queries
   - Severity: 18,396 entry delta average

2. **group_by, label_filter, logfmt_parser, range_sum_over_time, unwrap**
   - Entry impact: 140,153 (15.6% of total)
   - Time impact: 500,925ms total, 55,658ms avg
   - Frequency: 9 queries
   - Severity: 15,572 entry delta average

3. **agg_sum, group_by, label_filter, line_contains, logfmt_parser, range_rate, unwrap**
   - Entry impact: 108,054 (12.0% of total)
   - Time impact: 85,609ms total, 10,701ms avg
   - Frequency: 8 queries
   - Severity: 13,506 entry delta average

4. **group_by, label_filter, line_contains, logfmt_parser, range_avg_over_time, unwrap**
   - Entry impact: 106,499 (11.9% of total)
   - Time impact: 50,461ms total, 16,820ms avg
   - Frequency: 3 queries
   - Severity: 35,499 entry delta average

5. **group_by, json_parser, label_filter, line_contains, line_not_contains, range_max_over_time, unwrap**
   - Entry impact: 76,226 (8.5% of total)
   - Time impact: 313,200ms total, 44,742ms avg
   - Frequency: 7 queries
   - Severity: 10,889 entry delta average


---

## Next Steps

### Immediate Actions

1. **Investigate Top Pattern**: Focus on the most impactful pattern first
   - Create unit tests using `/test-correctness-hypothesis`
   - Use example queries from this report
   - Target features: group_by, label_filter, line_contains, line_not_contains, logfmt_parser, range_rate

2. **Verify Fixes**: After implementing fixes, re-run this analysis
   - Expected reduction: ~183,969 entries
   - Queries affected: 10

3. **Systematic Approach**: Work through top 5 patterns
   - Combined entry impact: 614,901 (68.5%)
   - Combined time impact: 9,001,703ms

### Analysis Commands

```bash
# Re-run this analysis after fixes
/goldfish-analyze

# Investigate specific correlation IDs
# Example: 623648f1-c4e5-4fc3-b7d4-bc957cd02a2e
```

---

*Report generated by Goldfish MCP Analysis Tool*
