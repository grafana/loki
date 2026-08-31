# LogQL declarative test scripts syntax

The `testdata/*.logqltest` scripts alongside this package are declarative correctness tests for LogQL
**metric and log-selection** queries. Each `.logqltest` file loads some log streams and evaluates
queries against absolute, hand-specified expected results. Scripts are run by `TestLogQLScripts`
through the real `logql.Engine` over a filesystem-backed chunk store (TSDB index), so the full
storage read path and parsing/extraction pipeline are exercised end to end.

The format is adapted from Prometheus' [`promqltest`](https://github.com/prometheus/prometheus/tree/main/promql/promqltest)
DSL.

For syntax highlighting of `.logqltest` files in GoLand or VS Code, see [syntax/README.md](syntax/README.md).

## Commands

A script is a sequence of `load`, `clear`, and `eval` commands. Blank lines separate blocks, and
`#` starts a comment (ignored to end of line, except inside `"…"` or `` `…` `` quotes).

### `clear`

Resets all loaded streams, so the next `load` starts from a clean slate. Use it to isolate
independent scenarios within one file (see `testdata/AGENTS.md`).

### `load`

Loads log entries into the chunk store. Each indented line places entries on one stream:

```
load
  <stream-selector>  "<line>"  @ <start>  [repeat every <step> for <count>]  [metadata key1="value1" key2="value2"]
```

- `<stream-selector>` — a LogQL stream selector, e.g. `{app="foo", env="prod"}`.
- `"<line>"` — the log line. Use double quotes, or backticks (`` `<line>` ``) for a raw line that
  itself contains `"`, e.g. a JSON object for the `json` / `unpack` parsers. Neither form unescapes.
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

Evaluates a query and checks its result. `instant`/`range` are for a metric query (results of
type vector/scalar/matrix); `select` is for a log-selection query (results of type streams — see
"Log-selection window" below), which has no notion of a step.

```
eval instant at <time> <logql>
  <expected series...>

eval range from <t0> to <t1> step <step> <logql>
  <expected series...>

eval select from <t0> to <t1> <forward|backward> <logql>
  <expected streams...>
```

- Times (`<time>`, `<t0>`, `<t1>`, `<step>`) are Go durations offset from the script epoch.
- `forward` / `backward` — **required** on `select`, and `select` only: the order log lines are
  returned in, which is the order the expected block is written in (see "Log-selection direction"
  below).
- Expected results follow on indented lines. The block ends at a blank line, a dedented line,
  or EOF.

## Expected results

- **Vector** (instant queries): one line per series, `{labels} <value>`.
- **Scalar** (e.g. `1 + 2`): a single line with just the number.
- **Matrix** (range queries): one line per series, `{labels} <p0> <p1> …`, one point per step
  from `<t0>` to `<t1>`. Use `_` for a step with no point.
- **Streams** (log-selection queries, e.g. `{app="foo"} |= "bar"`): one line per log entry,
  `{labels} "<line>" @ <ts>` (or `` `<line>` `` for a raw line). Several lines sharing the same
  `{labels}` belong to one stream and are checked as an exact, ordered sequence — a stream's line
  order is meaningful (it follows the query direction), unlike the set of series in a
  vector/matrix. Distinct label sets are compared as a set (order-independent), like series.

Point syntax (from promqltest):

- plain floats: `6`, `1.5`, `-2`
- `NaN`, `+Inf` / `Inf`, `-Inf`
- `_` — a gap (no point at that step; matrices only)
- `<base>[±<step>]x<count>` — expands to `count+1` points: `2+3x2` → `2 5 8`; `1-1x2` → `1 0 -1`;
  `4x3` → `4 4 4 4`

Label sets are compared as sets (order-independent). Note that Loki promotes **structured
metadata into the result label set**, so a stream loaded with `[metadata detected_level="info"]`
produces series labelled `{…, detected_level="info"}`.

An **empty-value label is significant**: `{app="a", age=""}` asserts that `age` is present with an
empty value (e.g. from a `json` expression whose path is missing, or `logfmt --keep-empty`), which is
distinct from omitting `age` entirely.

Every `eval` must assert exactly one kind of result — series, log streams, a scalar,
`expect empty`, or `expect fail`; otherwise the harness errors (a forgotten expected block would
otherwise pass vacuously on an empty result).

### Log-selection window

A log-selection query's window is **start-inclusive, end-exclusive**: `[t0, t1)` for
`eval select`, and `[T−30s, T)` for `eval instant at T` (a fixed 30s look-back). This is the
opposite of a metric range vector's `(start, end]` — a line exactly at `t1` (or at the instant
`T`) falls **outside** the window:

```
load
  {app="foo"} "in range"    @ 10s
  {app="foo"} "at boundary" @ 20s

eval select from 0 to 20s forward {app="foo"}
  {app="foo"} "in range" @ 10s
```

### Log-selection direction

Every `eval select` states its direction between the window and the query: `forward` reads the
window oldest line first, `backward` newest line first.

```
load
  {app="foo"} "1st" @ 10s
  {app="foo"} "2nd" @ 20s

eval select from 0 to 30s backward {app="foo"}
  {app="foo"} "2nd" @ 20s
  {app="foo"} "1st" @ 10s
```

The direction only changes the order lines come back in, never which lines match: the window
bounds and the pipeline are unaffected. Expected lines within one stream are compared in the order
they are written, so a `backward` block lists them newest first. Streams themselves are still
compared as a set, and each is ordered independently.


### Empty results

To assert that a query returns no series or streams — e.g. `absent_over_time` over present data,
a comparison whose sides never match, or a selector matching no stream — use `expect empty`:

```
eval instant at 60s count_over_time({app="missing"}[1m])
  expect empty
```

### Failure assertions

To assert that a query fails (at parse or evaluation time), give the query a single expected
line:

```
eval instant at 0s count_over_time({app="foo"})
  expect fail
```

Optionally match the error: `expect fail msg: <substring>` or `expect fail regex: <re>`.

### Ordered results

Series are compared as a set (order-independent) by default. Prefix an instant query's expected
block with `expect ordered` to compare series **positionally** — needed for `sort` / `sort_desc`:

```
eval instant at 60s sort(count_over_time({app=~"a|b"}[1m]))
  expect ordered
  {app="a"} 1
  {app="b"} 2
```

`expect ordered` is instant-only; use distinct, non-`NaN` values so the order is unambiguous.

### Relaxing or skipping value comparison on one stack

Every query runs on multiple [execution stacks](#execution-stacks). Loosen the comparison, or skip
it, when one stack legitimately returns different values. Keep every other stack exact.

`expect values-toleration <epsilon> on "<stack>"` compares values on the named stack, but with
`<epsilon>` in place of the default `1e-9` tolerance. Use this when a stack's result is
approximate, but the query still exercises a real assertion — e.g. a sharded query whose value goes
through a probabilistic sketch:

```
eval instant at 60s quantile_over_time(0.5, {app="a"} | logfmt | unwrap v [1m]) by (pod)
  expect values-toleration 0.02 on "query-frontend + query-scheduler (sharding)"
  {pod="1"} 3
  {pod="2"} 15
```

- `<epsilon>` is a plain positive, finite number, the same dual absolute/relative bound the
  default `1e-9` uses: a match if the absolute difference is within `<epsilon>`, or the
  difference relative to the larger magnitude is.
- May repeat for multiple stacks, one directive per stack.

`skip values-comparison on "<stack>"` drops the value check entirely for the named stack:

```
eval instant at 60s some_query_with_a_nondeterministic_value({app="a"}[1m])
  skip values-comparison on "query-frontend + query-scheduler (sharding)"
  {app="a"} 3
```

- `<stack>` is the exact stack name, in double quotes.
- The named stack still runs the query, must not error, and is still checked for series/stream
  count, sample/line count, and timestamps. Only the float value comparison (or, for a
  log-selection query, the log line text) is skipped.

A stack can have `skip values-comparison` or `expect values-toleration`, not both — they're
contradictory ("don't compare" vs. "compare, loosely").

## Execution stacks

Each `eval` runs on multiple execution stacks:

- `direct` — the query runs straight through `logql.Engine` over the chunk store.
- `query-frontend + query-scheduler (no sharding)` — a real frontend, scheduler, and querier loop.
- `query-frontend + query-scheduler (sharding)` — the same loop with query sharding on.

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

eval select from 0 to 60s forward {app="foo"} |= "status=200"
  {app="foo"} "level=info status=200" @ 10s
  {app="foo"} "level=info status=200" @ 20s
  {app="foo"} "level=info status=200" @ 30s
  {app="foo"} "level=info status=200" @ 40s
  {app="foo"} "level=info status=200" @ 50s
```

## Scope

Metric queries (results of type vector / scalar / matrix) and log-selection queries (results of
type streams — a stream selector with optional line/label filters, parsers, and formatters).
Tailing and `expect ordered` for streams are not yet supported.
