# Report format

`querybench` writes one JSON report per run to `-report-dir`. The filename is the
run's start time: `YYYY-MM-DD-at-HH-MM.json` (local time). The tool never
overwrites an existing report; it errors if the target file already exists.

The report is rewritten after every query completes, so if the tool is
interrupted the file still holds the queries finished so far. `finished_at` is
`null` until the whole run completes.

Every field that carries a unit names it in the field: byte counts end in
`_bytes`, durations in `_seconds`, a rate in `_per_second`.

## Top level

| Field | Type | Meaning |
| --- | --- | --- |
| `description` | string | The `-report-description` text, e.g. which backend was tested. |
| `loki_url` | string | Loki query-frontend base URL the queries ran against. |
| `tenant` | string | Tenant id sent as `X-Scope-OrgID`. |
| `backend_namespace` | string | Namespace the system metrics were captured from. |
| `requested_start` | RFC3339 | The `-query-min-start-time` bound: no query reads before this. |
| `requested_end` | RFC3339 | The `-query-end-time` anchor: every query ends here. |
| `started_at` | RFC3339 | Wall-clock time the run began. |
| `finished_at` | RFC3339 or null | Wall-clock time the run ended; `null` while running. |
| `queries` | array | One entry per query, in execution order. |

## Query entry

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | Stable query label, e.g. `range/count_24h_15m`. |
| `type` | string | `instant` or `range`. |
| `expr` | string | The exact LogQL expression sent. |
| `start` | RFC3339 | Real data start: `end` minus the query's window and the longest range vector in `expr` (so a 24h range query with a `[1h]` vector starts at `end-25h`). |
| `end` | RFC3339 | Data end, always `requested_end`; the evaluation time for an instant query. |
| `step_seconds` | number | Range query step, in seconds. `0` for instant queries. |
| `runs` | number | How many times the query was executed. |
| `execution_started_at` | RFC3339 | Wall-clock start of this query's runs. |
| `execution_finished_at` | RFC3339 | Wall-clock end of this query's runs. |
| `latencies_seconds` | number[] | One latency per successful run, in seconds. |
| `failed_runs` | number | Runs that errored or returned a non-200 status. They add no latency and no bytes. |
| `query_stats` | object | Response statistics summed over the runs (see below). |
| `system_metrics` | object | Backend metrics for the run window (see below). |

### `query_stats`

Totals extracted from the query responses, summed over all successful runs.

| Field | Type | Meaning |
| --- | --- | --- |
| `processed_bytes` | number | Sum of `stats.summary.totalBytesProcessed` across the runs. |

### `system_metrics`

Backend metrics captured from `backend_namespace` after the query's runs, over a
window that spans the run plus `-metrics-scrape-padding` on each side (see the README
for how the window and evaluation time are chosen). Each field is `null` when the
metric could not be captured, so a gap is never confused with a real zero.

Counts and byte totals are whole numbers (rounded); the CPU fields keep their
fractional values.

| Field | Type | Meaning |
| --- | --- | --- |
| `objstore_requests` | number or null | Object-store operations the querier issued over the window. |
| `objstore_fetched_bytes` | number or null | Bytes the querier fetched from object storage over the window. |
| `cpu_seconds` | number or null | Total querier CPU-seconds consumed over the window. |
| `cpu_peak_cores` | number or null | Peak querier CPU cores over the window (max of the summed per-pod CPU rate). |
| `heap_inuse_peak_bytes` | number or null | Peak querier heap in use over the window. |
| `alloc_bytes_per_second` | number or null | Querier bytes allocated over the window ÷ run duration: average allocation rate during the run. |
| `memcached_written_bytes` | number or null | Bytes memcached served over the window. |

`objstore_requests`, `objstore_fetched_bytes`, `memcached_written_bytes` and
`cpu_seconds` are additive totals over the window. `alloc_bytes_per_second` is a
total divided by the run duration (a rate). `heap_inuse_peak_bytes` and
`cpu_peak_cores` are peaks over the window.

`alloc_bytes_per_second` divides by the un-padded run duration
(`execution_finished_at − execution_started_at`). The window totals (`cpu_seconds`,
`*_bytes`, `objstore_requests`) span the full padded window, so keep
`-metrics-scrape-padding` small relative to the run (raise `-runs`) to limit the
idle baseline the padding folds in. `cpu_peak_cores` is immune to the padding,
since idle time never exceeds the active peak.
