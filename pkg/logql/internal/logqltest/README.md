# LogQL declarative test scripts syntax

The `testdata/*.test` scripts alongside this package are declarative correctness tests for LogQL
**metric** queries. Each `.test` file loads some log streams and evaluates queries against
absolute, hand-specified expected results. Scripts are run by `TestLogQLScripts` through the real
`logql.Engine` over an in-memory pipeline-running querier, so the full parsing/extraction pipeline
is exercised end to end.

The format is adapted from Prometheus' [`promqltest`](https://github.com/prometheus/prometheus/tree/main/promql/promqltest)
DSL.

## Commands

A script is a sequence of `load` and `eval` commands. Blank lines separate blocks, and `#`
starts a comment (ignored to end of line, except inside `"…"` or `` `…` `` quotes).

### `load`

Loads log entries into the in-memory store. Each indented line places entries on one stream:

```
load
  <stream-selector>  "<line>"  @ <start>  [repeat every <step> for <count>]  [metadata key1="value1" key2="value2"]
```

- `<stream-selector>` — a LogQL stream selector, e.g. `{app="foo", env="prod"}`.
- `"<line>"` — the log line (double-quoted).
- `@ <start>` — **required** timestamp of the entry, as a Go duration offset from the script
  epoch (`t=0`), e.g. `@ 0s`, `@ 90s`, `@ 1m30s`. Repeat the selector on multiple lines to
  place entries at arbitrary different times.
- `[repeat every <step> for <count>]` — optional; generate `<count>` entries, the first at
  `@ <start>` and each `<step>` apart. The **square brackets are literal syntax**. Example:
  `@ 0s [repeat every 10s for 6]` → entries at 0,10,20,30,40,50s.
- `[metadata key1="value1" key2="value2"]` — optional structured metadata attached to the
  generated entries, as space-separated `key="value"` pairs (a key may itself be double-quoted
  if it contains whitespace). The **square brackets are literal syntax**.
- `{{.i}}` anywhere in the line is replaced with the 0-based index of the generated entry, e.g.
  `"value={{.i}}"` produces `value=0`, `value=1`, … — handy for `unwrap` scenarios.

`load` blocks are additive and may be interleaved with `eval` commands.

### `eval`

Evaluates a metric query and checks its result.

```
eval instant at <time> <logql>
  <expected series...>

eval range from <t0> to <t1> step <step> <logql>
  <expected series...>
```

- Times (`<time>`, `<t0>`, `<t1>`, `<step>`) are Go durations offset from the script epoch.
- Expected results follow on indented lines. The block ends at a blank line, a dedented line,
  or EOF.

## Expected results

- **Vector** (instant queries): one line per series, `{labels} <value>`.
- **Scalar** (e.g. `1 + 2`): a single line with just the number.
- **Matrix** (range queries): one line per series, `{labels} <p0> <p1> …`, one point per step
  from `<t0>` to `<t1>`. Use `_` for a step with no point.

Point syntax (from promqltest):

- plain floats: `6`, `1.5`, `-2`
- `NaN`, `+Inf` / `Inf`, `-Inf`
- `_` — a gap (no point at that step; matrices only)
- `<base>[+<step>]x<count>` — expands to `count+1` points: `2+3x2` → `2 5 8`; `4x3` → `4 4 4 4`

Label sets are compared as sets (order-independent). Note that Loki promotes **structured
metadata into the result label set**, so a stream loaded with `[metadata detected_level="info"]`
produces series labelled `{…, detected_level="info"}`.

### Failure assertions

To assert that a query fails (at parse or evaluation time), give the query a single expected
line:

```
eval instant at 0s count_over_time({app="foo"})
  expect fail
```

Optionally match the error: `expect fail msg: <substring>` or `expect fail regex: <re>`.

## Example

```
load
  {app="foo"} "level=info status=200"  @ 10s [repeat every 10s for 6]
  {app="bar"} "level=error status=500" @ 10s [repeat every 10s for 6] [metadata detected_level="error"]

eval instant at 60s sum by (app) (count_over_time({app=~"foo|bar"}[1m]))
  {app="foo"} 6
  {app="bar"} 6

eval range from 0 to 60s step 30s count_over_time({app="foo"}[30s])
  {app="foo"} _ 3 3
```

## Scope

Metric queries only (results of type vector / scalar / matrix). Log queries (streams) are not
yet supported.
