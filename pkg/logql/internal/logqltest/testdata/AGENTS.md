# Authoring LogQL `.logqltest` scripts

Guide for **writing** the `testdata/*.logqltest` correctness scripts. For DSL **syntax**
(`load` / `clear` / `eval`, timestamps, sample notation) see [`README.md`](../README.md).

## Scenario

A scenario is one `clear` + `load` + the queries that share its data:

```
clear
load
  {app="a"}     "msg" @ 20s      # signal
  {app="noise"} "x"   @ 20s      # noise the query must drop
eval instant at 60s <query>
  <expected>
eval range from 40s to 60s step 20s <query>   # last step == the instant above
  <expected>
```

## Terminology

- **Range vector duration** — the duration inside `[…]`, such as the `[1m]` in `rate(…[1m])`.
  Written `[R]` below.

## Checklist — apply to every scenario

1. **Isolate the scenario.** Start with `clear`, then `load` its own streams.
   Loads are cumulative, so a scenario that forgets `clear` inherits earlier streams.

2. **Load noise the query must drop.** Every `load` carries a `"noise"` line that exercises the
   *same* filter under test (table below).
   A `{app="noise"}` stream proves nothing about a `|~` line filter, because the selector drops it
   first. Line-filter noise needs the same labels and metadata, and differs only in the line.

3. **Test instant and range, back to back.** Run each query `at T` and `from T−N·S to T step S`,
   sized so the last range step equals the instant. This gives an easy cross-check.
   Exception: `sort` and `sort_desc` are instant-only. Assert them with `expect ordered` over
   distinct, non-`NaN` values.

4. **Cover both window overlaps and a gap.** For range functions, test three shapes: instant,
   overlapping (`step ≤ [R]`), and non-overlapping (`step > [R]`). Also add an empty window that
   emits an output gap (`_`). Describe these at the language level (window overlap), not engine
   internals.

5. **Test both grouping keywords.** When a function takes `by` or `without`, test both.
   `by(l)` keeps only `l`; `without(l)` keeps the rest. Both combine samples across the streams in a
   group.

6. **Cover the edge cases, not only the happy path.** Read the implementation to list them. For
   Prometheus-ported functions such as rates and quantiles, also cross-check
   `promql/promqltest/testdata/*.test`.
   Usual cases: an empty window (emits no series, except `absent_over_time`, which gives `1`); the
   half-open boundary; a single-sample window; `NaN` and `±Inf`; and invalid queries via
   `expect fail` (forbidden grouping, an unwrap-only op without `| unwrap`, a bad `label_replace`
   regex, `vector(<non-number>)`).

7. **Cover extraction and error edge cases** where a stage builds labels or can fail (parsers,
   `| unwrap`, `label_format`).
   A clashing name — a label present in more than one of stream labels, structured metadata, and
   parsed fields — follows two rules. The base name goes to the highest-precedence source:
   `stream > structured metadata > parsed`. Each value that loses the base name spills to the single
   `<name>_extracted` key, where the precedence is the opposite: `parsed > structured metadata > stream`.
   A three-way clash therefore keeps the parsed value under `<name>_extracted` and drops the
   structured-metadata value. Test every combination. A malformed line sets `__error__`, which fails the metric query unless
   `| __error__=""` drops that line; assert both, plus a partially malformed line (valid fields kept,
   the bad one dropped, or `--strict`-failed). An empty-value label (e.g. a `json` path with no
   match → `age=""`) is significant and distinct from an absent label; JSON input needs a backtick
   raw log line.

8. **Keep one scenario per function.** Put all of a function's cases in its own scenario: the happy
   path, edge cases, grouping, and `expect fail`.
   Do not make a shared "grouping" or "parse errors" scenario. Split a function only for genuinely
   distinct modes, such as `rate`'s log-range form and its `| unwrap` form.

9. **Hand-compute discriminating expected values.** Never pin what the engine emits.
   Avoid a value that a degenerate case or a likely bug also gives. For example, `0` is the result of
   an empty window, a single sample, and broken counter-reset compensation, so a test that expects
   `0` can pass while the feature is broken. If you are unsure of the value, probe it with a throwaway
   scenario, confirm it, then delete the probe.

10. **Make the file green before you move on.** Run
   `go test ./pkg/logql/internal/logqltest/ -run 'TestLogQLScripts/<file>.logqltest' -v`.
   If you cannot reproduce an expected value from real logs, surface it as a real discrepancy. Do not
   pin the engine's output.

## Noise by filter type

| The query filters by… | Noise to add |
|-----------------------|--------------|
| Line filter (`\|~`, `\|=`, `!~`, `!=`) | the **same** stream (identical labels + metadata) with a `"noise"` line the filter drops |
| Numeric / label filter (`\| bar > 0`) | the same stream with a failing value (`bar=0`) |
| Structured metadata (`\| lvl="error"`) | the same stream with a different metadata value (`lvl="info"`) |
| Label selector only (no line filter) | a separate `{app="noise"}` stream the selector drops |
| Range vector duration (`[R]`) | entries that span ~2× the duration so the older half falls outside |

Don't mix line-filtered and non-line-filtered queries under one `load`: same-label `"noise"` is
dropped by the former but **counted** by the latter — split them into separate scenarios.

## Half-open window

`[R]` at `T` covers `(T−R, T]` — lower bound exclusive, upper inclusive. Exercise it directly: place
a sample at `T−R` (must be excluded) and one at `T` (must be included). Give
`first_over_time` / `last_over_time` samples **distinct timestamps** so their selection is
deterministic.

## Comments

Write every comment in **Simplified Technical English** (ASD-STE100): one idea per sentence
(≤ ~20 words), active voice, simple present or past tense (no perfect, progressive, or `-ing`
forms), and plain common words used the same way every time — one term per concept, no synonyms.

Keep them terse and mostly **inline** — let the DSL and expected values carry the meaning; comment
only what they can't show, and prefer a trailing `# …` on the relevant line over a separate line.

- **Section header:** start every scenario's lead comment with the function name, e.g.
  `# count_over_time()` or `# stddev_over_time() / stdvar_over_time() with grouping`. When a scenario
  shows several behaviors, write `# <func>() edge cases:` and one `-` bullet per behavior, each a
  full sentence. Do **not** prose-explain what the function does.
- **Inline derivation on the result line** when the value isn't obvious — the formula
  (`{app="a"} 2.6   # (0.4 x 2) + (0.6 x 3) = 2.6`) or the per-step window math
  (`{app="a"} 2 3   # (−30s,30s]={1,2,3}→2, (0s,60s]={1,2,3,4,5}→3`).
- **Inline tag on a range eval** for its window shape (`… count_over_time(…[1m])  # overlapping`),
  and on a `load` line for what it contributes (`{app="a"} "ccc" @ 40s   # 3 bytes`).
- **Explain a mechanism** (grouping, `_extracted` precedence, …) **once**, in the first scenario that
  needs it — not per scenario.
- Never restate the query; no line-by-line narration.

## Files

One feature per file: `range_aggregations`, `vector_aggregations`, `binary_operations`,
`functions` (`label_replace`, `vector`), `conversions`, … add more as coverage grows
(`line_filters`, `label_filters`, `parsers`, `formatters`, …).
