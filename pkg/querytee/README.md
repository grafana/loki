# query-tee

query-tee is a proxy that sits between a client and several Loki backends.
It sends every write to every backend.
It sends every read to every backend, except that the secondary backend receives only the reads that the selection strategy picks.
It returns the response of the primary backend to the client.
It can compare the responses of two backends.

## Backends

`-backend.endpoints` is a comma-separated list of the backend endpoints. Each backend has one role:

| Role      | How to set it                            | Required | Count     |
| --------- | ---------------------------------------- | -------- | --------- |
| Primary   | `-primary-backend.hostname=<hostname>`   | Yes      | Exactly 1 |
| Secondary | `-secondary-backend.hostname=<hostname>` | No       | At most 1 |
| Other     | Listed in `-backend.endpoints` only      | No       | Any       |

To stop sending traffic to a backend, remove it from `-backend.endpoints`.

## Which backends receive the traffic

Every backend receives every write on a registered write route.
Every backend except the secondary receives every read on a registered read route.
The secondary receives the reads that the selection strategy picks.
The role of a backend otherwise only decides which response the client gets and which responses the proxy compares.

| Traffic             | Backends that receive it                           | Response the client gets                         |
| ------------------- | -------------------------------------------------- | ------------------------------------------------ |
| Read routes         | All, but the secondary only for the selected reads | The primary's response, even when it is an error |
| Write routes (push) | All                                                | The primary's response, even when it is an error |
| Other routes        | The primary only                                   | The primary's response                           |

The proxy forwards other routes only when you set `-proxy.passthrough-non-registered-routes=true`.

The proxy returns the primary's response as soon as it arrives.
It never falls back to another backend. The proxy collects the responses of the other backends in the background.

## Which reads the secondary backend receives

`-secondary-backend.selection-strategy=<strategy>` picks the reads that the secondary backend receives:

| Strategy  | Reads that the secondary backend receives                                         |
| --------- | --------------------------------------------------------------------------------- |
| `all`     | Every read. This is the default.                                                  |
| `dataobj` | The range and instant queries that the secondary backend reads from data objects. |

The `dataobj` strategy applies the eligibility check of the query-frontend with the limits of the secondary backend's Loki cluster.
Set `-config.file=<path>` to the Loki config file of that cluster, and `-runtime-config.file=<path>` for per-tenant limits.
The proxy reads only the `limits_config` block of the config file.
Set `-dataobj.enabled` and `-dataobj.storage-lag` to the values of that cluster. They have the same names and defaults as the Loki flags.
When `-dataobj.enabled` is `false`, the strategy selects no read.
Set `-querier.stream-first-execution-enabled` to the value of that cluster. It has the same name and default as the Loki flag.
The flag follows the precedence of Loki: a flag that you set takes precedence over the `stream_first_execution_enabled` value of the `limits_config` block, and the `limits_config` value takes precedence over the default of the flag.
A value in a tenant override takes precedence over both.
The strategy selects a query when the query-frontend marks it eligible for data objects, whatever share of the time range data objects hold.

The `loki_querytee_secondary_backend_decisions_total` metric counts the reads by the decision to send them to the secondary backend, or the reason not to.

## Which backends get compared

When you set `-proxy.compare-responses=true`, the proxy compares the secondary's response with the primary's response.
This flag requires you to also set `-secondary-backend.hostname=<hostname>`.
The proxy never compares other backends.
The proxy compares only the reads that the secondary backend receives.

The proxy compares only the following routes:

- `/loki/api/v1/query_range`
- `/loki/api/v1/query`
- `/api/prom/query`

The proxy compares in the background after the backends have answered, so the comparison adds no latency for the client.

The `loki_querytee_responses_compared_total` metric counts the comparisons by `result` and `cause`:

| Result     | Meaning                                                                                                                     |
| ---------- | --------------------------------------------------------------------------------------------------------------------------- |
| `match`    | The responses are equal.                                                                                                    |
| `mismatch` | The responses differ.                                                                                                       |
| `fail`     | The secondary request failed, the secondary did not answer with status 200, or the comparator could not compare the bodies. |
| `skipped`  | The primary failed or did not answer with status 200, or no samples remain after the proxy skips samples.                   |

The `cause` label says how the bodies differ for a `mismatch`.
For a `fail` or a `skipped` pair, it says why the proxy could not compare the bodies.
When a backend failed, the cause describes the secondary for a `fail`, and the primary for a `skipped` pair:

| Cause                                | Result            | Meaning                                                                                         |
| ------------------------------------ | ----------------- | ----------------------------------------------------------------------------------------------- |
| `timeout`                            | `fail`, `skipped` | The backend did not answer before its timeout.                                                  |
| `transport`                          | `fail`, `skipped` | The request to the backend failed before a response arrived, for a reason other than a timeout. |
| `http_4xx`                           | `fail`, `skipped` | The backend answered with a 4xx status.                                                         |
| `http_5xx`                           | `fail`, `skipped` | The backend answered with a 5xx status.                                                         |
| `http_other`                         | `fail`, `skipped` | The backend answered with another status that is not 200.                                       |
| `compare_error`                      | `fail`            | The comparator could not compare the bodies.                                                    |
| `status_mismatch`                    | `mismatch`        | The `status` fields of the response bodies differ, for example `success` and `error`.           |
| `result_type_mismatch`               | `mismatch`        | The `resultType` fields of the response bodies differ.                                          |
| `metric_count_mismatch`              | `mismatch`        | The responses have a different number of series.                                                |
| `metric_missing`                     | `mismatch`        | A series of the primary's response is missing from the secondary's response.                    |
| `sample_count_mismatch`              | `mismatch`        | A series has a different number of samples.                                                     |
| `sample_value_mismatch`              | `mismatch`        | A sample has a different value.                                                                 |
| `sample_timestamp_mismatch`          | `mismatch`        | A sample has a different timestamp.                                                             |
| `stream_count_mismatch`              | `mismatch`        | The responses have a different number of streams.                                               |
| `stream_missing`                     | `mismatch`        | A stream of the primary's response is missing from the secondary's response.                    |
| `stream_entry_count_mismatch`        | `mismatch`        | A stream has a different number of entries.                                                     |
| `stream_timestamp_mismatch`          | `mismatch`        | An entry has a different timestamp.                                                             |
| `stream_line_mismatch`               | `mismatch`        | An entry has a different line.                                                                  |
| `structured_metadata_count_mismatch` | `mismatch`        | An entry has a different number of structured metadata pairs.                                   |
| `structured_metadata_mismatch`       | `mismatch`        | An entry has different structured metadata.                                                     |
| `parsed_labels_count_mismatch`       | `mismatch`        | An entry has a different number of parsed label pairs.                                          |
| `parsed_labels_mismatch`             | `mismatch`        | An entry has different parsed labels.                                                           |

The `cause` is empty for a `match`, and for a `skipped` pair when no samples remain.

The proxy skips the samples from the last `-proxy.compare-skip-recent-samples` (default 60s) and the samples before `-proxy.compare-skip-samples-before`.

When several `result` rows apply, the proxy checks the cases in this order and uses the first that matches: the primary request failed, the secondary request failed, the primary status is not 200, the secondary status is not 200.

The `loki_querytee_secondary_backend_stats_ratio` metric observes the ratio of a query stat of the secondary over the same stat of the primary.
The proxy observes it for each compared pair when both backends return a decoded response.
It skips a stat when the primary value is zero.

| Label                  | Values                                                                 |
| ---------------------- | ---------------------------------------------------------------------- |
| `route`                | The route of the compared request.                                     |
| `stat`                 | `bytes_processed`, `exec_time`                                         |
| `primary_bucket`       | The bucket of the primary value of the stat, listed in the next table. |
| `queriers_load_status` | `both_idle`, `primary_busy`, `secondary_busy`, `both_busy`             |
| `result_cache_status`  | `both_miss`, `primary_hit`, `secondary_hit`, `both_hit`                |

The `primary_bucket` label has a different set of values for each stat.
Each bucket holds the values above the previous bucket, up to and including its upper bound.
So a `less_than_` bucket also holds a value equal to its bound.
The sizes are decimal: 1 GB is 10^9 bytes.

| `stat`            | `primary_bucket`  | Primary value              |
| ----------------- | ----------------- | -------------------------- |
| `bytes_processed` | `less_than_1gb`   | At most 1 GB               |
| `bytes_processed` | `1gb_to_10gb`     | Above 1 GB, up to 10 GB    |
| `bytes_processed` | `10gb_to_100gb`   | Above 10 GB, up to 100 GB  |
| `bytes_processed` | `100gb_to_1tb`    | Above 100 GB, up to 1 TB   |
| `bytes_processed` | `more_than_1tb`   | Above 1 TB                 |
| `exec_time`       | `less_than_100ms` | At most 100 ms             |
| `exec_time`       | `100ms_to_500ms`  | Above 100 ms, up to 500 ms |
| `exec_time`       | `500ms_to_1s`     | Above 500 ms, up to 1 s    |
| `exec_time`       | `1s_to_5s`        | Above 1 s, up to 5 s       |
| `exec_time`       | `5s_to_10s`       | Above 5 s, up to 10 s      |
| `exec_time`       | `10s_to_30s`      | Above 10 s, up to 30 s     |
| `exec_time`       | `30s_to_60s`      | Above 30 s, up to 60 s     |
| `exec_time`       | `more_than_60s`   | Above 60 s                 |

A backend is busy when its query waited more than 100ms in the queue.
A backend hits the cache when the range query, instant metric or log results cache of its query-frontend found at least one entry.

To also report the number of missing series in the `loki_querytee_missing_metrics_series` metric, set `-proxy.compare-report-missing-series=true`.
This flag requires `-proxy.compare-responses=true`.
