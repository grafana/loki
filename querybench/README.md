# querybench

`querybench` benchmarks a fixed set of LogQL queries against a Loki
query-frontend over a fixed time range, and records both the client-side latency
and the backend cost (object-store traffic, CPU, memory, memcached) of each
query. Its companion `reportstat` compares two reports side by side.

The intended use is comparing two query backends — for example **chunks** vs
**dataobj** — under the same workload:

1. Point a query cell at the first backend and run `querybench` → report A.
2. Point a query cell at the second backend and run `querybench` → report B.
3. `reportstat a:A.json b:B.json` → a markdown table of A vs B, per query.

`querybench` does not switch the backend itself; it measures whatever the cell
it queries is configured to use. Record which backend a run tested in
`-report-description`.

## How it works

- The query set is fixed in the binary. Each query is either an **instant** query
  (a `[W]` range vector at one point) or a **range** query (a `query_range` over
  a stepped interval).
- Every query ends at `-query-end-time`; its data start is that end minus the
  query's window and the longest range vector in its expression (a 24h range
  query with a `[1h]` vector reads back to `end-25h`). A query whose data start
  falls before `-query-min-start-time` is skipped and logged, so that flag bounds
  how far back any query may read.
- Each query runs `-runs` times, back-to-back, over the identical time range.
  Latency is measured per execution. The results cache is always bypassed
  (`Cache-Control: no-cache`) so every run does real work.
- After a query's runs, the tool waits `-metrics-scrape-padding` and then reads
  the backend metrics for that query, over a window covering the run plus the
  padding on each side. Running one query's metric capture before the next query
  starts keeps each query's window free of the others' load.
- The report is written after every query, so an interrupted run keeps the
  queries finished so far. It is never overwritten: a second run in the same
  minute fails rather than clobbering the first.

### Backend metrics

Captured per query from `-backend-namespace`, via `gcx` (see Requirements):
object-store requests and fetched bytes (querier), querier CPU cores, querier
heap peak and allocation rate, and memcached bytes served. The exact metrics and
units are documented in [REPORT.md](REPORT.md).

CPU cores and the allocation rate are windowed totals divided by the run
duration. Keep `-metrics-scrape-padding` small relative to the total run time
(raise `-runs`) so the padding's idle time does not dilute those two figures.

## Requirements

- **Go 1.26+** to build.
- **`gcx`** on `PATH`, logged in to a Grafana context that can query the
  Prometheus datasource holding the cell's metrics (`gcx login`). The datasource
  UID defaults to dev-cortex (`2z9d6ElGk`); override with `-metrics-datasource`.
  Metric capture shells out to `gcx metrics query ... -o json`.
- **Network access to the query-frontend** at `-url`. For a dev cell, port-forward
  it first:

  ```
  kubectl -n loki-dev-002 port-forward svc/query-frontend 3199:3100
  ```

If `gcx` fails for a metric, that metric is recorded as `null` and the run
continues; latency and processed bytes are never lost to a metrics hiccup.

## Build

```
go build -o querybench ./cmd/querybench
go build -o reportstat ./cmd/reportstat
```

Build from within the Loki repository checkout.

## Usage

### querybench

```
querybench \
  -url http://localhost:3199 \
  -tenant 156331 \
  -runs 10 \
  -query-min-start-time 2026-08-19T00:00:00Z \
  -query-end-time 2026-08-20T00:00:00Z \
  -backend-namespace loki-dev-002 \
  -report-dir ./reports \
  -report-description "dataobj, sections via index-gateway"
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-url` | `http://localhost:3199` | Loki query-frontend base URL. |
| `-tenant` | — (required) | Tenant id sent as `X-Scope-OrgID`. |
| `-runs` | `10` | Times each query runs, back-to-back. |
| `-query-min-start-time` | — (required) | Earliest time any query may read; queries reaching before it are skipped (RFC3339 or unix seconds). |
| `-query-end-time` | — (required) | End time every query ends at (RFC3339 or unix seconds). |
| `-backend-namespace` | — (required) | Namespace to capture metrics from. |
| `-report-dir` | `.` | Directory the JSON report is written to. |
| `-report-description` | "" | Free-text note stored in the report. |
| `-metrics-datasource` | `2z9d6ElGk` | gcx Prometheus datasource UID. |
| `-metrics-scrape-padding` | `2m` | Padding each side of the run window, and the settle wait, to cover scrape delay. |
| `-query-timeout` | `2m` | Per-query request timeout. |
| `-query-filter` | "" | Run only queries whose name or expression matches this regex. |

The report path is printed on start and on completion. The file is named
`YYYY-MM-DD-at-HH-MM.json` after the run start.

### reportstat

```
reportstat chunks:reports/chunks.json dataobj:reports/dataobj.json -o comparison.md
```

Each argument is `<name>:<path>`; `<name>` labels that report in the output.
With no `-o`, the markdown is written to stdout.

Every cell reads `a / b (±% of b vs a)` and is **per single query**: latency
figures come from the per-run latencies; object-store and memcached totals are
divided by the run count; querier CPU, memory peak and allocation rate are
window averages or peaks, already run-count independent, and shown as captured.
A `–` marks a query missing from one report or a metric that could not be
captured, and the percentage is dropped when either side is missing or the `a`
value is zero. Queries are matched by type, expression, window and step, so two
reports taken at different times still line up.

The report format is documented in [REPORT.md](REPORT.md).
