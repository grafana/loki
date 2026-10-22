# query-tee

query-tee is a proxy that sits between a client and several Loki backends.
It sends every request on a registered route to every backend.
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

Every backend receives every request on a registered read or write route.
The role of a backend only decides which response the client gets and which responses the proxy compares.

| Traffic             | Backends that receive it | Response the client gets                         |
| ------------------- | ------------------------ | ------------------------------------------------ |
| Read routes         | All                      | The primary's response, even when it is an error |
| Write routes (push) | All                      | The primary's response, even when it is an error |
| Other routes        | The primary only         | The primary's response                           |

The proxy forwards other routes only when you set `-proxy.passthrough-non-registered-routes=true`.

The proxy returns the primary's response as soon as it arrives.
It never falls back to another backend. The proxy collects the responses of the other backends in the background.

## Which backends get compared

When you set `-proxy.compare-responses=true`, the proxy compares the secondary's response with the primary's response.
This flag requires you to also set `-secondary-backend.hostname=<hostname>`.
The proxy never compares other backends.

The proxy compares only the following routes:

- `/loki/api/v1/query_range`
- `/loki/api/v1/query`
- `/api/prom/query`

The proxy compares in the background after the backends have answered, so the comparison adds no latency for the client.

The `loki_querytee_responses_compared_total` metric counts the comparisons by `result`:

| Result     | Meaning                                                                                                                     |
| ---------- | --------------------------------------------------------------------------------------------------------------------------- |
| `match`    | The responses are equal.                                                                                                    |
| `mismatch` | The responses differ.                                                                                                       |
| `fail`     | The secondary request failed, the secondary did not answer with status 200, or the comparator could not compare the bodies. |
| `skipped`  | The primary failed or did not answer with status 200, or no samples remain after the proxy skips samples.                   |

The proxy skips the samples from the last `-proxy.compare-skip-recent-samples` (default 60s) and the samples before `-proxy.compare-skip-samples-before`.

When several rows apply, the proxy checks the cases in this order and uses the first that matches: the primary request failed, the secondary request failed, the primary status is not 200, the secondary status is not 200.

To also report the number of missing series in the `loki_querytee_missing_metrics_series` metric, set `-proxy.compare-report-missing-series=true`.
This flag requires `-proxy.compare-responses=true`.
