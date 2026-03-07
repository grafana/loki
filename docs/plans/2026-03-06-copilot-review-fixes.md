# Copilot Review Fixes - PR #21085

> **For Claude:** REQUIRED SUB-SKILL: Use subagent-driven-development to implement this plan task-by-task.

**Goal:** Address valid feedback from Copilot's automated review on the metadata discovery tool PR, plus fix a critical unwrap syntax bug discovered during review analysis.

**Architecture:** Five tasks — the first two are related (metadata fields + query syntax), the rest are independent. Task 1b is the highest-impact fix.

**Tech Stack:** Go, LogQL bench framework, YAML query templates

---

## Summary of Copilot Feedback Triage

| # | File | Feedback | Verdict | Action |
| --- | --- | --- | --- | --- |
| 1 | `metadata.go:289-308` | Unwrappable fields don't match generators | **Valid** | Fix field lists (keep `duration` for string fields — it's valid via `unwrap duration()`) |
| 1b | `queries/**/*.yaml` | (Discovered during analysis) Bare `unwrap duration` silently fails on duration strings | **Bug** | Fix 39 queries to use `unwrap duration(duration)` |
| 2 | `validate.go:88` | `isInstant` wrongly set for metric queries | **Dismissed** | Keep `isInstant = true` for metric queries — it's the stricter validation path (uses `MinInstantRange >= MinRange`), guaranteeing queries work in both instant and range modes. Reply to PR thread. |
| 3 | `discover/cmd/main.go:316` | Probe window 24h vs 1h mismatch | **Invalid** | Dismiss — code and comment are consistent (reply to thread only) |
| 4 | `probe.go:236` | `TotalProbed` only counts successes | **Valid (naming)** | Fix docstring |
| 5 | `README.md:192-203` | Build paths are wrong | **Valid** | Fix paths |
| 6 | `.gitignore:10` | Missing space in comment | **Valid** | Fix formatting (bundle with Task 5) |

---

### Task 1a: Fix unwrappable fields to match generators

**Files:**

- Modify: `pkg/logql/bench/metadata.go:289-308`
- Test: `pkg/logql/bench/metadata_test.go`, `pkg/logql/bench/integration_metadata_test.go`

**Reuse:** Field names from `pkg/logql/bench/faker.go` generators.

**Context:** The `getUnwrappableFields` function returns fields per application that don't match what the log generators in `faker.go` actually produce. Duration-string fields (Go `time.Duration` format like "423ms") **are valid** unwrappable fields via `| unwrap duration(duration)`, so they must remain in the metadata.

**Corrected field mapping** (derived from faker.go generator audit):

| Service | Current (wrong) | Correct | Notes |
| --- | --- | --- | --- |
| database | `duration`, `duration_ms`, `status` | `duration_ms`, `rows_affected` | `duration_ms` = int, `rows_affected` = int. No `duration` string, no `status` field. |
| web-server | `status`, `duration`, `duration_ms` | `status`, `duration_ms` | `status` = int, `duration_ms` = int. No bare `duration` string. |
| cache | `size`, `duration`, `duration_ms` | `size`, `ttl`, `duration` | `size` = int, `ttl` = int, `duration` = Go duration string (70%). No `duration_ms`. |
| kafka | `size` | `partition`, `offset`, `size` | `partition` = int (always), `offset` = int (always), `size` = int (70%). |
| loki | `duration`, `size`, `streams`, `bytes` | `duration`, `streams`, `bytes` | `duration` = Go duration string (60%), `streams` = int (50%), `bytes` = int (50%). No `size` field. |
| mimir | `duration`, `size`, `streams`, `bytes` | `duration`, `streams`, `bytes` | Same as loki. `duration` = Go duration string (80%). No `size` field. |
| tempo | `duration`, `size`, `spans`, `bytes` | `duration`, `spans`, `bytes` | `duration` = Go duration string (60%), `spans` = int (50%), `bytes` = int (50%). No `size` field. |
| grafana | `duration`, `status` | `duration`, `status` | No change needed. `duration` = Go duration string (70%), `status` = int (50%). |

Key principle: **Duration strings are valid unwrappable fields.** Loki's `| unwrap duration(field)` / `| unwrap duration_seconds(field)` conversion function parses Go duration strings (e.g. "423ms") into float seconds. These fields must remain in the metadata so the resolver can find streams that have them.

**Step 1: Update `getUnwrappableFields` in `pkg/logql/bench/metadata.go:289-308`**

```go
func getUnwrappableFields(appName string) []string {
 switch appName {
 case appDatabase:
  return []string{"duration_ms", "rows_affected"}
 case "web-server":
  return []string{"status", "duration_ms"}
 case "cache":
  return []string{"size", "ttl", "duration"}
 case "kafka":
  return []string{"partition", "offset", "size"}
 case appLoki, "mimir":
  return []string{"duration", "streams", "bytes"}
 case "tempo":
  return []string{"duration", "spans", "bytes"}
 case "grafana":
  return []string{"duration", "status"}
 default:
  return []string{}
 }
}
```

**Step 2: Update the exported `UnwrappableFields` bounded set**

The bounded set in `metadata.go` must include all fields that appear in the corrected mapping. Add new fields (`rows_affected`, `ttl`, `partition`, `offset`, `spans`) and remove any that are no longer used by any service.

**Step 3: Run tests and fix assertions**

Run: `go test -v -count=1 ./pkg/logql/bench/...`

Update any test assertions in `metadata_test.go` and `integration_metadata_test.go` that reference the old field lists.

**Step 4: Commit**

```
fix(bench): align unwrappable fields with actual log generators
```

---

### Task 1b: Fix bare `unwrap duration` queries to use conversion function

**Files:**

- Modify: `pkg/logql/bench/queries/regression/metric-queries.yaml`
- Modify: `pkg/logql/bench/queries/exhaustive/unwrap-aggregations.yaml`
- Modify: `pkg/logql/bench/queries/exhaustive/aggregations.yaml`

**Reuse:** The existing `duration_seconds(duration)` queries in the same files are the correct pattern to follow.

**Context:** In the logfmt generators (loki, mimir, tempo, grafana, cache), the `duration` field is emitted as a Go `time.Duration` string (e.g. `duration=423ms`). After `| logfmt`, the label value is the string `"423ms"`.

When the query uses bare `| unwrap duration`, the LogQL lexer sees `duration` is NOT followed by `(`, so it treats it as a label name identifier, not a conversion function. The default conversion is `strconv.ParseFloat("423ms", 64)` which **fails**, producing `value=0` with `__error__=SampleExtractionErr`. These queries silently return zero values.

The correct syntax is `| unwrap duration(duration)` which invokes `time.ParseDuration()` to convert "423ms" → 0.423 seconds. (Note: `duration()` and `duration_seconds()` are equivalent — both call `time.ParseDuration`.)

Evidence this is a real bug: `drilldown-patterns.yaml:37` has a skipped query with the exact error: `SampleExtractionErr: time: missing unit in duration "301"`.

**39 queries** across 3 files use bare `| unwrap duration [` and need fixing.

**The fix for each query:**

1. Change `| unwrap duration [${RANGE}]` → `| duration != "" | unwrap duration(duration) [${RANGE}]`
2. Add the `duration != ""` guard (consistent with existing `duration_seconds(duration)` queries, filters out log lines where the conditional `duration` field is absent)
3. Update the `tags` — change `integer-field` to `duration-conversion` where present
4. Update `description` and `notes` where they say "integer unwrap" to reflect duration conversion

**Pattern — before:**

```yaml
query: sum(sum_over_time(${SELECTOR} | logfmt | unwrap duration [${RANGE}]))
tags:
  - integer-field
```

**Pattern — after:**

```yaml
query: sum(sum_over_time(${SELECTOR} | logfmt | duration != "" | unwrap duration(duration) [${RANGE}]))
tags:
  - duration-conversion
```

**Files and line counts:**

| File | Bare `unwrap duration` count |
| --- | --- |
| `regression/metric-queries.yaml` | 5 queries (lines 5, 21, 36, 149, 164) |
| `exhaustive/unwrap-aggregations.yaml` | 28 queries (lines 20, 65, 80, 95, 110, 157, 172, 218, 233, 264, 278, 293, 324, 339, 354, 369, 384, 399, 414, 430, 445, 476, 556, 572, 588, 605, 623, 642) |
| `exhaustive/aggregations.yaml` | 6 queries (lines 5, 20, 38, 55, 155, 281) |

**Step 1: Fix all 39 queries**

For each query, apply the pattern transformation above. The `requires.unwrappable_fields: [duration]` stays the same — the field name hasn't changed, only the unwrap syntax.

**Step 2: Run tests**

Run: `go test -v -count=1 ./pkg/logql/bench/... -run TestQueryRegistry`

This validates that all query templates parse correctly and their requirements resolve against the metadata.

**Step 3: Commit**

```
fix(bench): use duration() conversion for Go duration string unwrap

Bare `| unwrap duration` treats `duration` as a label name and attempts
float conversion, which fails on Go duration strings like "423ms".
Use `| unwrap duration(duration)` to invoke time.ParseDuration() and
add `duration != ""` guard for log lines where the field is absent.
```

---

---

### Task 3: Fix TotalProbed docstring

**Files:**

- Modify: `pkg/logql/bench/discover/pkg/keyword_types.go:38`

**Reuse:** None.

**Context:** The `KeywordResult.TotalProbed` docstring says "total number of stream-keyword pairs probed" but the field counts successful keyword probes (one per keyword, not per stream-keyword pair). The simplest fix is to correct the docstring rather than rename the field — the code and print output already use `TotalProbed` consistently, and `TotalProbed + TotalSkipped == len(FilterableKeywords)` makes the semantics clear.

**Important:** `TotalProbed` also exists on `tsdb.RangeResult` with different (correct) semantics. Do NOT rename that field.

**Step 1: Fix the docstring**

In `pkg/logql/bench/discover/pkg/keyword_types.go:38`:

```go
// Before:
// TotalProbed is the total number of stream-keyword pairs probed.
TotalProbed int

// After:
// TotalProbed is the number of keyword probes that completed successfully.
TotalProbed int
```

**Step 2: Commit**

```
fix(bench): correct TotalProbed docstring in KeywordResult
```

---

### Task 4: Fix README paths and .gitignore formatting

**Files:**

- Modify: `pkg/logql/bench/README.md:192-203`
- Modify: `pkg/logql/bench/.gitignore:10`

**Reuse:** `pkg/logql/bench/Makefile:99-100` for correct build paths.

**Context:** The README references `./pkg/logql/bench/cmd/discover/` but the actual entrypoint is at `./pkg/logql/bench/discover/cmd/main.go`. The Makefile (line 99) correctly uses `discover/cmd/discover` as the build target.

**Step 1: Fix the build command in README**

In `pkg/logql/bench/README.md:192-194`, replace the manual `go build` with the Makefile target:

```bash
# Before:
go build -o /tmp/discover ./pkg/logql/bench/cmd/discover/

# After:
make -C pkg/logql/bench discover/cmd/discover
```

This uses the Makefile target (line 99) which builds the binary to `pkg/logql/bench/discover/cmd/discover`.

**Step 2: Fix the run example in README**

In `pkg/logql/bench/README.md:203`, update to use the Makefile-built binary path:

```bash
# Before:
discover/cmd/discover \

# After:
pkg/logql/bench/discover/cmd/discover \
```

**Step 3: Fix .gitignore comment formatting**

In `pkg/logql/bench/.gitignore:10`:

```
# Before:
#discovery

# After:
# discovery
```

**Step 4: Verify by building**

Run: `make -C pkg/logql/bench discover/cmd/discover`

Confirm it compiles.

**Step 5: Commit**

```
fix(bench): correct discover tool paths in README and fix gitignore formatting
```

---

## Verification

After all code tasks are complete, run the full test suite: Note: Task 1a changes the synthetic metadata output. Existing generated datasets may need `make generate` re-run (before running the bench tests)

```bash
go test -v -count=1 ./pkg/logql/bench/...
go test -v -count=1 ./pkg/logql/bench/discover/...
make -C ./pkg/logql/bench discover
```

---

## Execution Order

Task 1b (query fix) should come after Task 1a (field list fix) since query registry tests validate requirements against bounded sets. The rest are independent.

Suggested order: 4 (trivial) → 3 (trivial) → 1a (field lists) → 1b (39 query fixes) → 5 (reply).
