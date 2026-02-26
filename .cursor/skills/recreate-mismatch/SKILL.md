---
name: goldfish:recreate-mismatch
description: >
  Recreate Goldfish production mismatches as failing LogQL bench tests. Reads an analysis report,
  extracts ALL priority patterns (not just the top one), crafts multiple query variations per
  pattern, and systematically iterates through them to maximize reproduction success. Produces a
  comprehensive summary tracking every pattern and variation attempted.
allowed-tools: "Read,Write,Bash(go:*),Glob,Grep,mcp__goldfish__*"
---

# goldfish:recreate-mismatch

## Name
goldfish-recreate-mismatch

## Description
Recreate Goldfish production mismatches as failing LogQL bench tests. This skill bridges the gap
between "we know there's a mismatch in production" and "we have a failing test that captures the bug."

Unlike single-pattern approaches, this skill systematically works through ALL patterns from the
analysis report — trying multiple query variations for each — to maximize the chance of reproducing
at least one mismatch with synthetic data.

## User-Invocable
Yes

## Command
`/goldfish:recreate-mismatch`

## Input
- Path to a Goldfish analysis report (default: most recent in `tools/goldfish-mcp/reports/`)

## Output
- One or more YAML query files in `pkg/logql/bench/queries/regression/`
- Test results from `TestStorageEquality` for every variation attempted
- A comprehensive summary artifact documenting ALL patterns and variations tried

---

## Process

### Step 1: Read and Parse the Report — Extract ALL Patterns

1. **Find the report.** Ask the user for a report path, or find the most recent:
   ```bash
   ls -t tools/goldfish-mcp/reports/GOLDFISH_ANALYSIS_REPORT_*.md | head -1
   ```

2. **Read the FULL report.** Parse these sections:
   - **Priority Recommendations** (bottom of report) — ranked by total impact
   - **Category 1: Top 10 by Occurrence** — most frequent mismatch patterns
   - **Category 2: Top 10 by Data Discrepancy** — largest entry count deltas

3. **Build a ranked pattern list.** Extract ALL patterns from the report and create a single
   deduplicated list, ordered by priority:

   **Tier 1 — Priority Recommendations (highest impact first):**
   For each pattern, extract:
   - Pattern name (comma-separated feature list)
   - Total entry delta and percentage of total
   - Frequency (number of mismatching queries)
   - Severity (average entry delta per query)
   - Example queries from the report

   **Tier 2 — Category 2 high-delta patterns not already in Tier 1:**
   Extract patterns from the top data discrepancy queries that aren't covered by
   Priority Recommendations. These may reveal unique mismatch triggers.

   **Tier 3 — Category 1 high-frequency patterns not already covered:**
   Extract patterns from the most frequent mismatches. Even if individual deltas are small,
   high frequency means more users are affected.

   **Deduplication:** If a pattern appears in multiple tiers, keep only the highest-tier entry.
   Merge example queries from all tiers into the single entry.

4. **Check the mismatch direction** for each pattern from the Category 2 table:
   - Is v1 (chunk store) returning more entries than v2 (dataobj-engine)? → `v2 < v1` (data loss in v2)
   - Or vice versa? → `v1 < v2` (extra data in v2)
   - Or inconsistent? Note that too.

5. **Present the full ranked pattern list** to the user as a numbered table:

   ```markdown
   | # | Tier | Pattern | Entry Delta | Freq | Direction |
   |---|------|---------|-------------|------|-----------|
   | 1 | P1   | group_by, label_filter, ... | 183,969 (20.5%) | 10 | v1 < v2 |
   | 2 | P2   | group_by, label_filter, logfmt... | 140,153 (15.6%) | 9 | v1 < v2 |
   | ... | | | | | |
   ```

   Ask the user: "Proceed with all patterns, or select specific ones?" Default: all.

### Step 2: Decompose the LogQL Pattern Features

Break the pattern name into individual LogQL features. Use this mapping table to understand
what each feature means in terms of LogQL syntax and bench requirements:

| Report Feature | LogQL Syntax | Query Component | Bench Requirement |
|----------------|-------------|-----------------|-------------------|
| `logfmt_parser` | `\| logfmt` | Parser stage | `requires.log_format: logfmt` |
| `json_parser` | `\| json` | Parser stage | `requires.log_format: json` |
| `label_filter` | `\| field="value"` or `\| field!=""` | Label filter after parser | `requires.detected_fields` or bounded label set |
| `line_contains` | `\|= "keyword"` | Line filter | `requires.keywords` (from `filterableKeywords`) |
| `line_not_contains` | `!= "keyword"` | Negative line filter | `requires.keywords` (from `filterableKeywords`) |
| `line_regex_match` | `\|~ "pattern"` | Regex line filter | Use bounded keywords in pattern |
| `line_regex_not_match` | `!~ "pattern"` | Negative regex filter | Use bounded keywords in pattern |
| `range_rate` | `rate({...} [...])` | Range aggregation | `kind: metric`, needs `${RANGE}` |
| `range_count_over_time` | `count_over_time({...} [...])` | Range aggregation | `kind: metric`, needs `${RANGE}` |
| `range_sum_over_time` | `sum_over_time({...} [...])` | Range aggregation | `kind: metric`, needs `${RANGE}` |
| `range_avg_over_time` | `avg_over_time({...} [...])` | Range aggregation | `kind: metric`, needs `${RANGE}` |
| `range_max_over_time` | `max_over_time({...} [...])` | Range aggregation | `kind: metric`, needs `${RANGE}` |
| `range_min_over_time` | `min_over_time({...} [...])` | Range aggregation | `kind: metric`, needs `${RANGE}` |
| `range_first_over_time` | `first_over_time({...} [...])` | Range aggregation | `kind: metric`, needs `${RANGE}` |
| `range_last_over_time` | `last_over_time({...} [...])` | Range aggregation | `kind: metric`, needs `${RANGE}` |
| `range_bytes_over_time` | `bytes_over_time({...} [...])` | Range aggregation | `kind: metric`, needs `${RANGE}` |
| `range_bytes_rate` | `bytes_rate({...} [...])` | Range aggregation | `kind: metric`, needs `${RANGE}` |
| `agg_sum` | `sum(...)` | Vector aggregation | `kind: metric` |
| `agg_count` | `count(...)` | Vector aggregation | `kind: metric` |
| `agg_avg` | `avg(...)` | Vector aggregation | `kind: metric` |
| `agg_max` | `max(...)` | Vector aggregation | `kind: metric` |
| `agg_min` | `min(...)` | Vector aggregation | `kind: metric` |
| `agg_topk` | `topk(N, ...)` | Vector aggregation | `kind: metric` |
| `agg_bottomk` | `bottomk(N, ...)` | Vector aggregation | `kind: metric` |
| `group_by` | `by (label1, label2)` | Grouping clause | `requires.labels` or `requires.detected_fields` |
| `group_without` | `without (label1)` | Grouping clause | `requires.labels` |
| `unwrap` | `\| unwrap field` | Unwrap expression | `requires.unwrappable_fields` (from bounded set) |
| `drop` | `\| drop __error__, __error_details__` | Drop labels | No special requirement |
| `keep` | `\| keep field1, field2` | Keep labels | No special requirement |
| `line_format` | `\| line_format "{{.field}}"` | Line format | `requires.detected_fields` |
| `binary_arithmetic` | `+`, `-`, `*`, `/` | Binary operations | Two sub-queries needed |
| `binary_logical` | `and`, `or`, `unless` | Binary logical ops | Two sub-queries needed |
| `binary_comparison` | `==`, `!=`, `>`, `<`, `>=`, `<=` | Binary comparison | Two sub-queries needed |
| `offset` | `offset 1h` | Offset modifier | Applies to range agg |
| `decolorize` | `\| decolorize` | Decolorize stage | No special requirement |

### Step 3: Craft Synthetic Queries — Variation Strategy

This is the core creative step. For **each pattern** in the ranked list, create **at least 3
query variations** to maximize the chance of triggering the mismatch with synthetic data.

**IMPORTANT:** Before crafting queries, ALWAYS read `pkg/logql/bench/metadata.go` to get the
current bounded variable sets. They may have been updated since this skill was written.

#### Variation Strategy

For each pattern, generate these variations:

**Variation A — Direct Adaptation:**
Take the closest example query from the report and replace production-specific parts with
bounded set values:
1. Replace stream selectors with `${SELECTOR}`
2. Replace hardcoded ranges with `[${RANGE}]`
3. Replace production keywords in line filters with keywords from `filterableKeywords`
4. Replace production labels in `by()` grouping with labels from `labelKeys`
5. Replace production label filter values with values from the bounded sets

This variation most closely mirrors the production query structure.

**Variation B — Simplified (Minimum Viable Pattern):**
Strip the query to the minimum features that define the pattern. Remove optional pipeline
stages, extra filters, and complex grouping. Keep only the features listed in the pattern name.

Examples:
- Pattern `group_by, label_filter, logfmt_parser, range_rate` →
  `sum by (level) (rate(${SELECTOR} | logfmt | level!="" [${RANGE}]))`
- Pattern `agg_sum, line_contains, range_count_over_time` →
  `sum(count_over_time(${SELECTOR} |= "error" [${RANGE}]))`

This variation tests whether the core feature combination alone triggers the mismatch.

**Variation C — Expanded (Extra Pipeline Stages):**
Start from Variation A and add more pipeline stages from the production example that weren't
in the core pattern. Try different combinations:
- Different grouping labels (e.g., `by (service_name)` instead of `by (level)`)
- Additional line filters not in the pattern
- Different aggregation functions (e.g., `avg` instead of `sum`)
- Regex selectors (`=~`) instead of equality selectors (`=`)
- Different `requires` combinations to select different stream types

This variation tests whether the mismatch depends on surrounding query context.

**Pattern-Specific Extra Variations:**
Some patterns warrant additional variations beyond A/B/C:
- **Patterns with `unwrap`:** Try different unwrappable fields (`bytes`, `duration`, `status`, etc.)
- **Patterns with `drop`:** Try with and without `| drop __error__, __error_details__`
- **Patterns with dual parsers:** Try each parser alone, then combined
- **Patterns with regex selectors:** Try `=~` variants with exact values (exercises regex code path)

#### Requirements Mapping

For each variation, set the corresponding requirements:

| Query Uses | Requirement to Set |
|-----------|-------------------|
| `\| logfmt` | `requires.log_format: logfmt` |
| `\| json` | `requires.log_format: json` |
| `\| unwrap field` | `requires.unwrappable_fields: [field]` — pick from bounded set |
| `\|= "keyword"` | `requires.keywords: [keyword]` — pick from `filterableKeywords` |
| `by(label)` | `requires.labels: [label]` (stream label) OR `requires.detected_fields: [field]` (parsed field) |
| `\| field!=""` | `requires.detected_fields: [field]` |
| structured metadata key | `requires.structured_metadata: [key]` |

#### Query Kind

- If the query has a range aggregation (`rate(`, `count_over_time(`, etc.) → `kind: metric`
- If it's a pure log query → `kind: log`

#### Time Range and Direction

- **Metric queries:** `time_range: { length: "24h", step: "1m" }`
- **Log queries:** `time_range: { length: "24h" }` and optionally set `directions: "both"`

#### Tags and Notes

- **Description:** Reference the Goldfish pattern and variation (e.g., "Goldfish P1-A: rate with logfmt parser — direct adaptation")
- **Tags:** Include feature names from the pattern plus `goldfish`, `regression`
- **Notes:** Reference the report, variation type, and expected failure mode

#### Use ONLY Bounded Variable Sets

The query MUST use only variables and values from the bounded sets. If a feature requires something
not available, adapt the query to use what IS available while preserving the structural pattern.

### Step 4: Write the Query YAML Files

For each pattern, create a YAML file containing ALL variations for that pattern:

1. **Create the file** at: `pkg/logql/bench/queries/regression/goldfish-{pattern-slug}.yaml`
   - The slug should be descriptive (e.g., `goldfish-rate-logfmt-label-filter.yaml`)
   - One file per pattern, with multiple query entries (one per variation)

2. **Follow the schema** at `pkg/logql/bench/queries/schema.json`

3. **Include the schema header:**
   ```yaml
   # yaml-language-server: $schema=../schema.json
   ---
   ```

4. **Write ALL variations as separate query entries within one file:**
   ```yaml
   # yaml-language-server: $schema=../schema.json
   ---
   queries:
     - description: "Goldfish P1-A: rate+logfmt+label_filter — direct adaptation"
       query: >-
         sum by (level) (rate(${SELECTOR}
         |= "duration" != "error"
         | logfmt
         | level!=""
         [${RANGE}]))
       kind: metric
       time_range:
         length: 24h
         step: 1m
       tags: [goldfish, regression, rate, logfmt, label_filter, variation-a]
       notes: >-
         Pattern 1 Variation A: Direct adaptation of production example.
       requires:
         log_format: logfmt
         keywords: [duration, error]
         detected_fields: [level]

     - description: "Goldfish P1-B: rate+logfmt+label_filter — simplified"
       query: >-
         sum by (level) (rate(${SELECTOR}
         | logfmt
         | level!=""
         [${RANGE}]))
       kind: metric
       time_range:
         length: 24h
         step: 1m
       tags: [goldfish, regression, rate, logfmt, label_filter, variation-b]
       notes: >-
         Pattern 1 Variation B: Minimum viable pattern — just parser + filter + rate.
       requires:
         log_format: logfmt
         detected_fields: [level]

     - description: "Goldfish P1-C: rate+logfmt+label_filter — expanded with regex selector"
       query: >-
         sum by (service_name) (rate(${SELECTOR}
         |= "error" != "debug" |~ "duration|status"
         | logfmt
         | level!="" | status!=""
         [${RANGE}]))
       kind: metric
       time_range:
         length: 24h
         step: 1m
       tags: [goldfish, regression, rate, logfmt, label_filter, variation-c]
       notes: >-
         Pattern 1 Variation C: Expanded with extra filters and different grouping.
       requires:
         log_format: logfmt
         keywords: [error, debug, duration, status]
         detected_fields: [level, status]
   ```

### Step 5: Run the Tests

Run tests for EACH variation, tracking results as you go:

```bash
# Check if generated data exists
ls pkg/logql/bench/data/ 2>/dev/null

# If no data directory exists, generate it (takes ~5 minutes):
# go generate ./pkg/logql/bench/...

# Run ALL variations for a pattern at once (they're in one file):
go test -v -count=1 -timeout 10m -slow-tests \
  -run "TestStorageEquality/regression/goldfish-{pattern-slug}" \
  ./pkg/logql/bench/
```

#### Interpreting Results

| Result | Meaning | Action |
|--------|---------|--------|
| **Test FAILS with assertion error** | SUCCESS — mismatch reproduced | Record as FAIL (reproduced), continue to next pattern |
| **Test FAILS with `ErrNotSupported`** | Feature not supported in dataobj-engine | Record as SKIP, note limitation |
| **Test PASSES** | Mismatch NOT reproduced | Record as PASS, try next variation |
| **Test FAILS with "empty results"** | Query templating issue | Check `requires` and bounded sets |
| **Test times out** | Query too broad | Narrow time range, add more filters |

### Step 6: Systematic Pattern Iteration Loop

This is the core execution loop. Iterate through ALL patterns, trying ALL variations for each.

```
FOR each pattern in ranked_pattern_list:
  Log: "=== Pattern {N}/{total}: {pattern_name} (Tier: {tier}) ==="

  FOR each variation (A, B, C, plus any pattern-specific extras):
    1. Write/update YAML query file with this variation
       (or include all variations in one file from Step 4)
    2. Run TestStorageEquality targeting this variation
    3. Record result in tracking table:
       - Pattern name
       - Variation (A/B/C/extra)
       - Query structure (abbreviated)
       - Result: PASS | FAIL (reproduced!) | SKIP (ErrNotSupported) | ERROR (other)
       - Details: entry counts, delta, error message
    4. IF FAIL (mismatch reproduced):
       → Document the successful reproduction
       → Note which variation triggered it
       → Continue to next pattern (don't stop — try to reproduce more)
    5. IF PASS (no mismatch):
       → Try next variation for this pattern
    6. IF SKIP/ERROR:
       → Note the limitation
       → Try next variation (the limitation may not apply to all variations)
  END FOR (variations)

  Write pattern-level summary:
  - All variations tried and their results
  - Best result (closest to reproducing)
  - Whether pattern was reproduced
  - Hypotheses for why it didn't reproduce (if applicable)

END FOR (patterns)
```

**Key principles:**
- **Don't stop at first reproduction.** Continue through all patterns to maximize coverage.
- **Don't skip patterns after failures.** Each pattern may reveal a different bug.
- **Track everything.** The tracking table is the primary output artifact.

### Step 7: Create Comprehensive Summary Artifact

After completing the iteration loop for ALL patterns, create a master summary file.

**Location:** `pkg/logql/bench/queries/regression/goldfish-{report-date}-reproduction.summary.md`

**Template:**

```markdown
# Goldfish Mismatch Reproduction: Comprehensive Report

## Source
- Report: {path to report}
- Report date range: {date range}
- Total patterns attempted: {N}
- Total variations attempted: {N}
- Successful reproductions: {N}

## Master Reproduction Tracking

| # | Pattern | Tier | Variations Tried | Best Result | Reproduced? | YAML File |
|---|---------|------|-----------------|-------------|-------------|-----------|
| 1 | group_by, label_filter, ... | P1 | 3/3 | PASS (all equal) | No | goldfish-rate-logfmt-label-filter.yaml |
| 2 | group_by, label_filter, logfmt... | P1 | 3/3 | PASS (all equal) | No | goldfish-sum-logfmt-unwrap.yaml |
| 3 | agg_sum, group_by, ... | P1 | 2/3 | FAIL (delta=1234) | YES | goldfish-sum-rate-unwrap.yaml |
| 4 | drop, json_parser, ... | C1 | 1/3 | SKIP (ErrNotSupported) | No | goldfish-dual-parser.yaml |
| ... | | | | | | |

## Reproduction Details

### Pattern 1: {pattern_name} — {Reproduced / Not Reproduced}

**Source:** Priority Recommendation #{N}, Entry delta: {N} ({X}%), Frequency: {N} queries

| Variation | Query Structure | Result | v1 Entries | v2 Entries | Delta |
|-----------|----------------|--------|------------|------------|-------|
| A (direct) | `sum by (level) (rate(... \|= ... \| logfmt ...))` | PASS | 4,320 | 4,320 | 0 |
| B (simplified) | `sum by (level) (rate(... \| logfmt \| level!="" ...))` | PASS | 4,320 | 4,320 | 0 |
| C (expanded) | `sum by (service_name) (rate(... \|= ... \|~ ... \| logfmt ...))` | PASS | 1,440 | 1,440 | 0 |

**Analysis:** {Why this pattern didn't reproduce / details of reproduction}

### Pattern 2: {pattern_name} — ...
[Repeat for each pattern]

## Successfully Reproduced Patterns

[For each pattern that was reproduced:]

### {Pattern Name}
- **Winning variation:** {A/B/C}
- **Query file:** {path}
- **Query:** `{the actual LogQL query}`
- **v1 returned:** {describe}
- **v2 returned:** {describe}
- **Delta:** {N} entries
- **Matches report direction:** {yes/no}
- **Hypothesis:** {What this suggests about root cause}

## Patterns NOT Reproduced

[For each pattern NOT reproduced:]

### {Pattern Name}
- **Variations tried:** {N}
- **Best result:** {closest to mismatch}
- **Likely reason:** {Why synthetic data doesn't trigger this}
- **Suggestions:** {Alternative approaches to try}

## Cross-Pattern Analysis

- **Common factors in reproduced mismatches:** {What reproduced patterns share}
- **Common factors in non-reproduced mismatches:** {What non-reproduced patterns share}
- **Synthetic data limitations:** {What aspects of production data are missing}
- **Recommendations for improving reproduction rate:** {Concrete suggestions}

## Next Steps
- [ ] Investigate root causes for reproduced mismatches
- [ ] Consider alternative reproduction strategies for non-reproduced patterns
- [ ] Re-run /goldfish:analyze after fixes to track impact reduction
- [ ] Explore Goldfish MCP direct investigation for non-reproducible patterns
```

### Step 8: Deep Production Investigation (Fallback for Non-Reproducible Patterns)

When local reproduction fails for a pattern (all variations PASS), this step activates to
investigate the production mismatch directly using the Goldfish MCP tools. This does NOT
replace the bench test approach — it supplements it by examining actual production queries
to understand WHY synthetic data can't trigger the mismatch and what characteristics to try next.

**Activation criteria:** Execute this step for each pattern where ALL variations passed
(no mismatch reproduced). Skip patterns that were successfully reproduced in Step 6.

#### 8.A: Sample Production Queries for the Pattern

Use `goldfish_list_queries` to fetch actual production mismatch queries from the report's
time period. Extract the report's date range from the report header.

```
goldfish_list_queries:
  comparisonStatus: "mismatch"
  from: <report_start_time_rfc3339>
  to: <report_end_time_rfc3339>
  pageSize: 100
```

Then filter the returned queries to find those matching the current pattern's feature
signature. Match by checking that the query's LogQL contains the same key features
(parsers, aggregations, filters) identified in the pattern decomposition from Step 2.

If the result set is large, focus on the top 20 queries with the highest entry deltas.

#### 8.B: Analyze Query Characteristics

For each matching production query, extract and analyze these dimensions:

1. **Selector type distribution:**
   - Does the query use `=~` (regex) or `=` (equality) selectors?
   - Count how many queries use regex vs equality for each label name.
   - This is critical — production queries heavily use regex selectors while bench only
     generates equality selectors.

2. **Label patterns:**
   - Which stream labels appear in selectors? (cluster, namespace, pod, service_name, etc.)
   - Are certain labels (e.g., pod, namespace) consistently using regex matchers?
   - What label values are used — broad wildcards or specific values?

3. **Data volume indicators:**
   - What are the entry counts for v1 vs v2?
   - Are high-delta queries consistently larger or smaller datasets?
   - Is there a threshold below/above which mismatches appear?

4. **Pipeline structure:**
   - Beyond the core pattern features, what other pipeline stages appear?
   - Are there `| drop __error__, __error_details__` stages?
   - Multiple parsers chained? (e.g., `| json | logfmt`)
   - Line format stages?
   - Does the presence of additional stages correlate with higher deltas?

#### 8.C: Build Correlation Tables

Create structured tables summarizing the investigation findings:

```markdown
## Production Query Analysis for Pattern: {pattern_name}

### Selector Type Distribution
| Label | Exact (=) | Regex (=~) | % Regex |
|-------|-----------|------------|---------|
| cluster | 2 | 8 | 80% |
| namespace | 1 | 9 | 90% |
| pod | 0 | 3 | 100% |
| service_name | 5 | 5 | 50% |

### Data Volume Distribution
| Volume Bucket | Count | Avg Delta | Avg % Diff |
|--------------|-------|-----------|------------|
| < 100 entries | 3 | 12 | 15% |
| 100–1000 entries | 4 | 245 | 42% |
| > 1000 entries | 3 | 8,432 | 89% |

### Correlation with Mismatch Severity
| Characteristic | Low Delta (<100) | High Delta (>1000) |
|---------------|-------------------|---------------------|
| Uses regex selectors | 1 | 9 |
| Uses exact selectors | 5 | 0 |
| Has drop stage | 3 | 7 |
| Has multiple parsers | 0 | 4 |
| Has line_format | 1 | 2 |
```

#### 8.D: Fetch Detailed Query Pairs (Top 2-3 Highest Delta)

For the top 2-3 highest-delta queries matching this pattern, use `goldfish_get_query_details`
to fetch the complete result payloads from both cells:

```
goldfish_get_query_details:
  correlationId: <correlation_id_from_list>
```

Compare the detailed results:
- **Entry counts:** v1 entry count vs v2 entry count — magnitude and direction
- **Label sets:** If available, compare which label sets appear in v1 vs v2 results
- **Response structure:** Are there structural differences (e.g., missing series, extra streams)?
- **Error indicators:** Does v2 return `__error__` labels that v1 doesn't?

Document specific examples:
```markdown
### Detailed Query Comparison: {correlation_id}

**Query:** `sum by (namespace) (rate({namespace=~"loki.*"} | logfmt | level!="" [5m]))`

| Metric | v1 (chunk store) | v2 (dataobj-engine) | Delta |
|--------|-----------------|---------------------|-------|
| Entries | 4,320 | 148,320 | +144,000 |
| Series | 12 | 12 | 0 |
| Bytes | 125,400 | 4,312,800 | +4,187,400 |
```

#### 8.E: Generate Hypotheses and New Query Variations

Based on the analysis, generate specific, testable hypotheses about why the mismatch
occurs in production but not with synthetic data:

**Hypothesis format:**
```markdown
### Hypothesis {N}: {Title}

**Evidence:** {What the data shows — cite specific numbers from correlation tables}
**Mechanism:** {Proposed explanation for how this causes the mismatch}
**Test:** {How to validate this hypothesis — new query variation or investigation}
**Confidence:** {High / Medium / Low}
```

**Example hypotheses:**
- "90% of high-delta mismatches use regex selectors → the regex matcher code path in
  dataobj-engine may handle stream selection differently than the equality path"
- "All mismatches involve pod=~ wildcard patterns → wildcard expansion over many pods
  triggers a stream merging bug in v2"
- "Drop stages appear in 70% of high-delta cases → __error__ label handling differs
  between engines when errors are dropped"

**Generate regex selector variations:** If the investigation shows regex selectors correlate
with mismatches (which is likely given prior findings), create a NEW set of query variations
using the regex selector support from Plan 01-02:

```yaml
# Variation D: Regex selector variant (from production investigation)
- description: "Goldfish P{N}-D: {pattern} — regex selector variant"
  query: >-
    {same query structure as Variation A but with regex selectors}
  kind: metric
  time_range:
    length: 24h
    step: 1m
  tags: [goldfish, regression, {features}, variation-d, regex-selector]
  notes: >-
    Pattern {N} Variation D: Regex selector variant based on production investigation.
    {X}% of production mismatches for this pattern use regex selectors.
  requires:
    log_format: logfmt
    keywords: [duration, error]
    detected_fields: [level]
    regex_selectors: true  # Exercise =~ code path (from Plan 01-02)
```

**Feed variations back to the iteration loop:** After generating Variation D (and any other
investigation-derived variations), return to Step 6 to execute them through `TestStorageEquality`.
This creates a feedback loop:

```
Step 6 (all variations PASS) → Step 8 (investigate production) → generate Variation D
→ Step 6 again (test Variation D) → if still PASS → record in summary
```

Only iterate once — if Variation D also passes, record the comprehensive findings in the
summary and move to the next pattern.

#### 8.F: Update Summary Artifact

Add a "Production Investigation" section to the summary artifact (from Step 7) for each
investigated pattern:

```markdown
## Production Investigation: {Pattern Name}

### Investigation Summary
- **Production queries analyzed:** {N}
- **Regex selector correlation:** {X}% of high-delta queries use regex selectors
- **Key finding:** {One-line summary of the most important discovery}

### Correlation Tables
{Tables from Step 8.C}

### Detailed Comparisons
{Results from Step 8.D}

### Hypotheses
{Hypotheses from Step 8.E}

### Regex Selector Variation Results
| Variation | Query | Result | Delta |
|-----------|-------|--------|-------|
| D (regex) | `sum by (level) (rate(${SELECTOR} ...))` | PASS / FAIL | {N} |

### Conclusion
{Why synthetic data doesn't trigger this mismatch, or what was discovered}
{Recommendations for future investigation}
```

### Step 9: Present to Human

Show the user:

1. **The master tracking table** (quick overview of all patterns)
2. **Reproduction count** (e.g., "Reproduced 2 of 8 patterns")
3. **The summary artifact** (the `.summary.md` file)
4. **For each reproduction:** the test output and failing assertion
5. **Production investigation findings** (if Step 8 was executed):
   - Which patterns were investigated via Goldfish MCP
   - Key hypotheses generated
   - Whether regex selector variations changed any results
6. **Ask for confirmation:**
   - Do the reproduced queries correctly represent the production patterns?
   - Do the failure modes match what the report describes?
   - Are the investigation hypotheses reasonable?
   - Should we dig deeper into specific patterns or hypotheses?
7. If the human approves, the skill is complete.
8. If the human wants changes, iterate on specific patterns (return to Step 3).

---

## Reference: Existing Query Examples

Use these files as templates when crafting new queries:

| Category | File | Use For |
|----------|------|---------|
| Simple log queries | `queries/fast/basic-selectors.yaml` | Stream selector patterns |
| Parser + filter | `queries/regression/drilldown-patterns.yaml` | Parser pipelines with filters |
| Metric/unwrap | `queries/regression/metric-queries.yaml` | Range aggregations, unwrap |
| Structured metadata | `queries/fast/structured-metadata.yaml` | Structured metadata queries |
| Complex aggregations | `queries/exhaustive/aggregations.yaml` | Multi-level aggregations |

## Reference: Bounded Variable Sets

These are the valid values for synthetic queries. **ALWAYS read `pkg/logql/bench/metadata.go`
at the start of execution** to get the current sets — they may have been updated.

### `unwrappableFields` — Numeric fields for `| unwrap`
```
bytes, duration, rows_affected, size, spans, status, streams, ttl
```

### `filterableKeywords` — Strings for line filters (`|=`, `!=`, `|~`, `!~`)
```
DEBUG, ERROR, INFO, WARN, debug, duration, error, failed, info, level, query, refused, status, success, warn
```

### `structuredMetadataKeys` — Keys for structured metadata queries
```
detected_level, pod, span_id, trace_id
```

### `labelKeys` — Stream labels for selectors and grouping
```
cluster, component, detected_level, env, level, namespace, pod, query_type, region, service_name, status
```

### Template Variables (resolved by `metadata_resolver.go`)
| Variable | Purpose | Example Resolution |
|----------|---------|-------------------|
| `${SELECTOR}` | Label selector matching requirements | `{service_name="loki", env="prod"}` |
| `${LABEL_NAME}` | A label key from the resolved selector | `env` |
| `${LABEL_VALUE}` | A label value from the resolved selector | `prod` |
| `${RANGE}` | Duration for range aggregations | `5m` |

## Reference: Test Execution

### Test Name Format
```
TestStorageEquality/{suite}/{file}:{line}/kind={kind}/store=dataobj-engine
```
Example: `TestStorageEquality/regression/goldfish-rate-logfmt.yaml:4/kind=metric/store=dataobj-engine`

### Run Command
```bash
go test -v -count=1 -timeout 10m -slow-tests \
  -run "TestStorageEquality/regression/goldfish-{pattern-slug}" \
  ./pkg/logql/bench/
```

### Flags Explained
| Flag | Purpose |
|------|---------|
| `-v` | Verbose output — shows individual test results |
| `-count=1` | Disable test caching — always re-run |
| `-timeout 10m` | Allow up to 10 minutes (data loading can be slow) |
| `-slow-tests` | Build tag enabling bench tests (they're disabled by default) |
| `-run "..."` | Regex filter to run only matching tests |

## Related Files
- Analysis skill: `.claude/skills/goldfish-analyze/SKILL.md`
- Query schema: `pkg/logql/bench/queries/schema.json`
- Test runner: `pkg/logql/bench/bench_test.go`
- Query registry: `pkg/logql/bench/query_registry.go`
- Metadata/bounded sets: `pkg/logql/bench/metadata.go`
- Variable resolver: `pkg/logql/bench/metadata_resolver.go`
- LogQL syntax: `pkg/logql/syntax/syntax.y`
- Reports directory: `tools/goldfish-mcp/reports/`

## Example Usage

```
User: /goldfish:recreate-mismatch
```
