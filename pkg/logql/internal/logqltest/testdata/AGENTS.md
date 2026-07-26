# Authoring LogQL `.logqltest` scripts

This is the guide for **writing** the `testdata/*.logqltest` correctness scripts (for humans and
agents). For the DSL **syntax** (`load` / `clear` / `eval`, timestamps, sample notation, etc.)
see [`README.md`](../README.md). Follow the conventions below so the corpus stays consistent and
each test genuinely earns its place.

## 1. Organize by feature, one file per feature

Each `.logqltest` file targets one core LogQL feature. Current / expected files:

- `range_aggregations.logqltest` — `count_over_time`, `rate`, `rate_counter`, `bytes_over_time`,
  `bytes_rate`, `absent_over_time`, and the unwrapped `sum/avg/min/max/first/last/quantile/
  stddev/stdvar_over_time`.
- `vector_aggregations.logqltest` — `sum/avg/min/max/count/stddev/stdvar/topk/bottomk/sort/sort_desc`
  and `by`/`without` grouping.
- `binary_operations.logqltest` — arithmetic, comparison, logical/set (`and`/`or`/`unless`), the
  `bool` modifier, and vector matching (`on`/`ignoring`/`group_left`/`group_right`).
- `functions.logqltest` — `label_replace`, `vector`.
- (add more as coverage grows: `line_filters.logqltest`, `label_filters.logqltest`,
  `parsers.logqltest`, `formatters.logqltest`, …)

## 2. Scenario structure

The unit is a **scenario**, not a single query:

```
clear
load
  <streams: signal + noise>

eval instant at <T> <query>
  <expected>
eval range from <T-N·S> to <T> step <S> <query>
  <expected>
```

- **Start every scenario with `clear`**, then `load` its own data. This isolates scenarios so
  one can't leak streams into another (loads are otherwise cumulative).
- **Run one or more queries per fixture.** If several queries exercise the same data (e.g.
  `count_over_time(...)` and `sum by (...) (count_over_time(...))`), keep them under one
  `clear`/`load` — only start a new scenario when the data must change.
- **Test every query both instant and range, back to back.** Pick a query time `T`; the instant
  runs `at T`, and the range runs `from T-N·S to T step S` so its **last step equals the instant**
  (same value, easy to cross-check) while earlier steps exercise the sliding window.
- **`sort` / `sort_desc` are the exception — instant only.** They assert order, so use
  `expect ordered` (positional comparison) with distinct, non-`NaN` values.

## 3. Always load noise that exercises the tested filter

Every scenario loads **noise** the query must exclude, so the test proves its filtering actually
*excludes* — not merely that it counts what matches. **Every `load` block includes noise**, and the
noise must exercise the *same* filter the test asserts on:

| The query filters by… | Noise to add |
|-----------------------|--------------|
| Line filter (`\|~`, `\|=`, `!~`, `!=`) | the **same** stream (identical labels + structured metadata) with a `"noise"` line the filter drops |
| Numeric / label filter (`\| bar > 0`) | the same stream with a failing value (`bar=0`) |
| Structured metadata (`\| lvl="error"`) | the same stream with a different metadata value (`lvl="info"`) |
| Label selector only (no line filter) | a separate `{app="noise"}` stream the selector drops |
| Time window (`[1m]`) | entries spanning ~2× the window so the older half falls outside |

The point is to make the *actual code path under test* do its job. A `{app="noise"}` stream proves
nothing about a `|~` line filter — the selector drops it first. So for a line-filter test the noise
must reach the filter: same stream labels and structured metadata as the valid logs, only the log
line differs (`"noise"`).

Consequence: don't mix line-filtered and non-line-filtered queries under one `load`. Same-label
`"noise"` lines are dropped by the former but **counted** by the latter, so split them into separate
scenarios, each carrying the noise its queries exercise.

Make noise self-identifying: use a `"noise"` log line (and, for selector-only noise, an
`{app="noise"}` label) so a reader sees at a glance what must be dropped — no explanatory comment
needed.

## 4. Comments

Sparse and purposeful. Never restate the query, and never re-explain these conventions (that
loads carry noise is stated here, once — not per load):

- Comment a `clear`/`load` **only** when the fixture itself isn't self-evident (e.g. a
  non-obvious value distribution).
- Annotate an `eval` **only** when the expected value's derivation is non-obvious (e.g. why a
  window yields N) — one line, at the eval, not per result line.
- No line-by-line narration.

## 5. Expected results are absolute truth

Expected values are hand-computed, not whatever the engine happens to emit. When porting a case
from a Go test, preserve its original expected value and reconstruct log input that reproduces
it through the full pipeline. Remember Loki's range window is **half-open**: `[R]` at time `T`
covers `(T-R, T]` (lower bound exclusive, upper inclusive).

## 6. Run it

```
go test ./pkg/logql/internal/logqltest/ -run 'TestLogQLScripts/<file>.logqltest' -v
```

Every scenario must be green before moving on. If an expected value can't be reproduced from
real logs, that's a genuine discrepancy to surface — don't paper over it by pinning whatever the
engine returned.
