---
name: goldfish:analyze
description: >
  Analyze Goldfish API mismatches to identify the most impactful correctness bugs in Loki v2 vs v1 API. Generates a comprehensive report categorized by occurrence, data discrepancy, and performance differences.
allowed-tools: "Read,Write,Bash(go:*),mcp__goldfish__*"
---

# goldfish:analyze

## Name
goldfish-analyze

## Description
Analyze Goldfish API mismatches to identify the most impactful correctness bugs in Loki v2 vs v1 API. Generates a comprehensive report categorized by occurrence, data discrepancy, and performance differences.

## User-Invocable
Yes

## Prompt
You are analyzing Goldfish API mismatch data to identify patterns in Loki v2 correctness bugs.

## Task
Generate a comprehensive Goldfish mismatch analysis report with three key categories:
1. **Top 10 patterns by occurrence** - Most frequently mismatching query structures
2. **Top 10 by data discrepancy** - Largest differences in result data/entries
3. **Top 10 by performance difference** - Biggest execution time deltas

## Process

### Step 1: Gather Parameters
Ask the user for analysis parameters (provide sensible defaults):
- **Time range**: Default to last 24 hours (now-24h to now)
- **Comparison status**: Default to "mismatch" (queries that didn't match within tolerance)
- **Page size**: Default to 1000 (or max available)
- **Report filename**: Default to `GOLDFISH_ANALYSIS_REPORT_<timestamp>.md`

### Step 2: Fetch Goldfish Data
Use the Goldfish MCP to fetch mismatch data:
```
list_queries with:
- comparisonStatus: "mismatch"
- from: <start_time_rfc3339>
- to: <end_time_rfc3339>
- pageSize: 1000
```

**Important:** The tool may return a large result that gets saved to a file. Read the file path from the tool result and process the data from there.

### Step 3: Analyze Query Patterns
For each query, categorize it by LogQL features using regex patterns:

**Parsers:**
- `| json` → json_parser
- `| logfmt` → logfmt_parser
- `| regexp` → regexp_parser
- `| pattern` → pattern_parser

**Line Filters:**
- `|=` → line_contains
- `!=` → line_not_contains
- `|~` → line_regex_match
- `!~` → line_regex_not_match

**Label Operations:**
- `| drop` → drop
- `| keep` → keep
- `| line_format` → line_format
- Label filters after parsers → label_filter

**Aggregations:**
- `sum(`, `count(`, `avg(`, etc. → agg_{name}
- `rate(`, `count_over_time(`, etc. → range_{name}

**Grouping:**
- `by(` → group_by
- `without(` → group_without

**Binary Operations:**
- `+`, `-`, `*`, `/` → binary_arithmetic
- `and`, `or`, `unless` → binary_logical
- `==`, `!=`, `>`, `<` → binary_comparison

**Other:**
- `unwrap` → unwrap
- `offset` → offset
- `decolorize` → decolorize

Create a pattern signature for each query as a comma-separated sorted list of features.

### Step 4: Calculate Metrics

For each query, extract:
- **Size metrics:** cellAResponseSize, cellBResponseSize, sizeDeltaBytes
- **Performance metrics:** cellAExecTimeMs, cellBExecTimeMs (if available)
- **Entry metrics:** cellAEntriesReturned, cellBEntriesReturned (if available)
- **Stats:** cellAStats, cellBStats (if available - may be null due to MCP bug)

**Note:** If stats fields are null, fall back to response size analysis.

### Step 5: Generate Report

Create a markdown report with these sections:

1. **Executive Summary**
   - Total mismatches analyzed
   - Time range
   - Unique patterns found
   - Key findings (top 3 most impactful bugs)

2. **Category 1: Top 10 Patterns by Occurrence**
   - Table with: Rank, Pattern, Count, %, Avg Delta
   - Example queries for each pattern
   - Focus on finding patterns that appear frequently

3. **Category 2: Top 10 by Data Discrepancy**
   - If entry counts available: Use entry count differences
   - Otherwise: Use response size deltas
   - Table with: Rank, Delta, v1 vs v2 values, Features, Example query
   - Focus on queries where v2 returns significantly different data

4. **Category 3: Top 10 by Performance Difference**
   - If exec times available: Use execution time deltas
   - Otherwise: Note that performance data is unavailable
   - Table with: Rank, Time Delta, v1 vs v2 times, Features, Example query
   - Focus on queries with largest performance regressions

5. **Feature Frequency Analysis**
   - Table showing which LogQL features appear most often in mismatches
   - Include count, percentage, and average delta
   - Helps identify which features are most problematic

6. **Key Insights**
   - Most impactful patterns by total size delta
   - Recommendations for which bugs to fix first
   - Estimated impact of fixing top 5 patterns

7. **Next Steps**
   - Concrete actions based on findings
   - Links to example queries
   - Suggestions for creating targeted tests

### Step 6: Save and Present

Save the report to the specified filename and provide:
- Path to the report
- Executive summary (top 3 findings)
- Quick stats (total mismatches, patterns, etc.)
- Recommendation on which bugs to prioritize

## Output Format

Generate a well-formatted markdown report similar to this structure:

```markdown
# Goldfish Mismatch Analysis Report

**Date Range:** YYYY-MM-DD to YYYY-MM-DD  
**Total Mismatches:** N  
**Unique Patterns:** N  

## Executive Summary
[Key findings summary]

## Category 1: Top 10 Patterns by Occurrence
[Tables and examples]

## Category 2: Top 10 by Data Discrepancy  
[Tables and examples]

## Category 3: Top 10 by Performance Difference
[Tables and examples]

## Feature Frequency Analysis
[Analysis of which features appear in mismatches]

## Key Insights & Recommendations
[Actionable insights]

## Next Steps
[Concrete actions to take]
```

## Important Notes

1. **Large Data Handling**: The Goldfish MCP may return results in a file due to size. Always check the tool result for a file path and read from there.

2. **Missing Stats**: Due to a known MCP bug (see GOLDFISH_API_STRUCT_FIX.md), cellAStats and cellBStats may be null. In this case:
   - Skip Category 3 (performance) or note it's unavailable
   - Use response size deltas for Category 2 instead of entry counts
   - Still provide valuable Category 1 (occurrence) analysis

3. **Pattern Detection**: Use regex-based feature detection, not AST parsing. It's faster and sufficient for pattern categorization.

4. **Report Quality**: Focus on actionable insights:
   - Which bugs affect the most queries?
   - Which bugs have the biggest impact per query?
   - What's the total impact of each pattern?
   - Which bugs should be fixed first?

5. **Example Queries**: Include truncated example queries (120 chars max) for readability.

## Example Usage

```
User: /goldfish:analyze
Assistant: I'll analyze Goldfish mismatches. Let me gather some parameters...

What time range would you like to analyze? (default: last 24 hours)
User: last 48 hours

[Analysis proceeds...]

Report generated: GOLDFISH_ANALYSIS_REPORT_2026-02-19.md

Executive Summary:
- 137 mismatches analyzed over 48 hours
- 37 unique query patterns identified
- Top 3 bugs account for 81% of total impact

Top 3 Most Impactful Bugs:
1. Logfmt + label filtering with negation (96.8% data loss)
2. Rate queries with logfmt parsing (75KB avg discrepancy)  
3. Boolean line filter combinations (93.6% data loss)

Recommendation: Fix pattern #1 first - it affects 2 queries but has massive impact.
```

## Related Files

- Reference implementation: `analyze_goldfish_v2.py`
- Previous report: `MISMATCH_ANALYSIS_REPORT.md`
- MCP fix doc: `GOLDFISH_API_STRUCT_FIX.md`
