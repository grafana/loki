---
title: Grafana Loki HTTP API
menuTitle: "HTTP API"
description: Loki exposes REST endpoints for operating on a Loki cluster. This section details the REST endpoints.
aliases:
- ../api/
weight: 100
---

# Grafana Loki HTTP API

Grafana Loki exposes an HTTP API for pushing, querying, and tailing log data.
Note that authenticating against the API is
out of scope for Loki.

## Microservices mode

When deploying Loki in microservices mode, the set of endpoints exposed by each
component is different.

These endpoints are exposed by all components:

- [`GET /ready`](#identify-ready-loki-instance)
- [`GET /log_level`](#change-log-level-at-runtime)
- [`GET /metrics`](#return-exposed-prometheus-metrics)
- [`GET /config`](#list-current-configuration)
- [`GET /services`](#list-running-services)
- [`GET /loki/api/v1/status/buildinfo`](#list-build-information)
- [`GET /loki/api/v1/format_query`](#format-query)

These endpoints are exposed by the querier and the query frontend:

- [`GET /loki/api/v1/query`](#query-loki)
- [`GET /loki/api/v1/query_range`](#query-loki-over-a-range-of-time)
- [`GET /loki/api/v1/labels`](#list-labels-within-a-range-of-time)
- [`GET /loki/api/v1/label/<name>/values`](#list-label-values-within-a-range-of-time)
- [`GET /loki/api/v1/series`](#list-series)
- [`GET /loki/api/v1/index/stats`](#index-stats)
- [`GET /loki/api/v1/index/volume`](#volume)
- [`GET /loki/api/v1/index/volume_range`](#volume)
- [`GET /loki/api/v1/tail`](#stream-log-messages)
- **Deprecated** [`GET /api/prom/tail`](#get-apipromtail)
- **Deprecated** [`GET /api/prom/query`](#get-apipromquery)
- **Deprecated** [`GET /api/prom/label`](#get-apipromlabel)
- **Deprecated** [`GET /api/prom/label/<name>/values`](#get-apipromlabelnamevalues)
- **Deprecated** [`GET /api/prom/series`](#list-series)

These endpoints are exposed by the distributor:

- [`POST /loki/api/v1/push`](#push-log-entries-to-loki)
- [`GET /distributor/ring`](#display-distributor-consistent-hash-ring-status)
- **Deprecated** [`POST /api/prom/push`](#post-apiprompush)

These endpoints are exposed by the ingester:

- [`POST /flush`](#flush-in-memory-chunks-to-backing-store)
- [`POST /ingester/shutdown`](#flush-in-memory-chunks-and-shut-down)
- **Deprecated** [`POST /ingester/flush_shutdown`](#post-ingesterflush_shutdown)

The API endpoints starting with `/loki/` are [Prometheus API-compatible](https://prometheus.io/docs/prometheus/latest/querying/api/) and the result formats can be used interchangeably.

These endpoints are exposed by the ruler:

- [`GET /ruler/ring`](#ruler-ring-status)
- [`GET /loki/api/v1/rules`](#list-rule-groups)
- [`GET /loki/api/v1/rules/{namespace}`](#get-rule-groups-by-namespace)
- [`GET /loki/api/v1/rules/{namespace}/{groupName}`](#get-rule-group)
- [`POST /loki/api/v1/rules/{namespace}`](#set-rule-group)
- [`DELETE /loki/api/v1/rules/{namespace}/{groupName}`](#delete-rule-group)
- [`DELETE /loki/api/v1/rules/{namespace}`](#delete-namespace)
- [`GET /api/prom/rules`](#list-rule-groups)
- [`GET /api/prom/rules/{namespace}`](#get-rule-groups-by-namespace)
- [`GET /api/prom/rules/{namespace}/{groupName}`](#get-rule-group)
- [`POST /api/prom/rules/{namespace}`](#set-rule-group)
- [`DELETE /api/prom/rules/{namespace}/{groupName}`](#delete-rule-group)
- [`DELETE /api/prom/rules/{namespace}`](#delete-namespace)
- [`GET /prometheus/api/v1/rules`](#list-rules)
- [`GET /prometheus/api/v1/alerts`](#list-alerts)

These endpoints are exposed by the compactor:

- [`GET /compactor/ring`](#compactor-ring-status)
- [`POST /loki/api/v1/delete`](#request-log-deletion)
- [`GET /loki/api/v1/delete`](#list-log-deletion-requests)
- [`DELETE /loki/api/v1/delete`](#request-cancellation-of-a-delete-request)

A [list of clients]({{< relref "../send-data" >}}) can be found in the clients documentation.

## Matrix, vector, and streams

Some Loki API endpoints return a result of a matrix, a vector, or a stream:

- Matrix: a table of values where each row represents a different label set
  and the columns are each sample value for that row over the queried time.
  Matrix types are only returned when running a query that computes some value.

- Instant Vector: denoted in the type as just `vector`, an Instant Vector
  represents the latest value of a calculation for a given labelset. Instant
  Vectors are only returned when doing a query against a single point in
  time.

- Stream: a Stream is a set of all values (logs) for a given label set over the
  queried time range. Streams are the only type that will result in log lines
  being returned.

## Timestamp formats

The API accepts several formats for timestamps. An integer with ten or fewer digits is interpreted as a Unix timestamp in seconds. More than ten digits are interpreted as a Unix timestamp in nanoseconds. A floating point number is a Unix timestamp with fractions of a second.

The timestamps can also be written in `RFC3339` and `RFC3339Nano` format, as supported by Go's [time](https://pkg.go.dev/time) package.

## Query Loki

```
GET /loki/api/v1/query
```

`/loki/api/v1/query` allows for doing queries against a single point in time. The URL
query parameters support the following values:

- `query`: The [LogQL]({{< relref "../query" >}}) query to perform
- `limit`: The max number of entries to return. It defaults to `100`. Only applies to query types which produce a stream(log lines) response.
- `time`: The evaluation time for the query as a nanosecond Unix epoch or another [supported format](#timestamp-formats). Defaults to now.
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward`.

In microservices mode, `/loki/api/v1/query` is exposed by the querier and the frontend.

Response format:

```
{
  "status": "success",
  "data": {
    "resultType": "vector" | "streams",
    "result": [<vector value>] | [<stream value>],
    "stats" : [<statistics>]
  }
}
```

where `<vector value>` is:

```
{
  "metric": {
    <label key-value pairs>
  },
  "value": [
    <number: second unix epoch>,
    <string: value>
  ]
}
```

and `<stream value>` is:

```
{
  "stream": {
    <label key-value pairs>
  },
  "values": [
    [
      <string: nanosecond unix epoch>,
      <string: log line>
    ],
    ...
  ]
}
```

The items in the `values` array are sorted by timestamp.
The most recent item is first when using `direction=backward`.
The oldest item is first when using `direction=forward`.

See [statistics](#statistics) for information about the statistics returned by Loki.

### Examples

This example query

```bash
curl -G -s  "http://localhost:3100/loki/api/v1/query" \
  --data-urlencode \
  'query=sum(rate({job="varlogs"}[10m])) by (level)' | jq
```

gave this response:

```json
{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {
        "metric": {},
        "value": [
          1588889221,
          "1267.1266666666666"
        ]
      },
      {
        "metric": {
          "level": "warn"
        },
        "value": [
          1588889221,
          "37.77166666666667"
        ]
      },
      {
        "metric": {
          "level": "info"
        },
        "value": [
          1588889221,
          "37.69"
        ]
      }
    ],
    "stats": {
      ...
    }
  }
}
```

If your cluster has
[Grafana Loki Multi-Tenancy]({{< relref "../operations/multi-tenancy" >}}) enabled,
set the `X-Scope-OrgID` header to identify the tenant you want to query.
Here is the same example query for the single tenant called `Tenant1`:

```bash
curl -H 'X-Scope-OrgID:Tenant1' \
  -G -s "http://localhost:3100/loki/api/v1/query" \
  --data-urlencode \
  'query=sum(rate({job="varlogs"}[10m])) by (level)' | jq
```

To query against the three tenants `Tenant1`, `Tenant2`, and `Tenant3`,
specify the tenant names separated by the pipe (`|`) character:

```bash
curl -H 'X-Scope-OrgID:Tenant1|Tenant2|Tenant3' \
  -G -s "http://localhost:3100/loki/api/v1/query" \
  --data-urlencode \
  'query=sum(rate({job="varlogs"}[10m])) by (level)' | jq
```

The same example query for Grafana Enterprise Logs
uses Basic Authentication and specifies the tenant names as a `user`.
The tenant names are separated by the pipe (`|`) character.
The password in this example is an access policy token that has been
defined in the `API_TOKEN` environment variable:

```bash
curl -u "Tenant1|Tenant2|Tenant3:$API_TOKEN" \
  -G -s "http://localhost:3100/loki/api/v1/query" \
  --data-urlencode \
  'query=sum(rate({job="varlogs"}[10m])) by (level)' | jq
```

## Query Loki over a range of time

```
GET /loki/api/v1/query_range
```

`/loki/api/v1/query_range` is used to do a query over a range of time and
accepts the following query parameters in the URL:

- `query`: The [LogQL]({{< relref "../query" >}}) query to perform
- `limit`: The max number of entries to return. It defaults to `100`. Only applies to query types which produce a stream(log lines) response.
- `start`: The start time for the query as a nanosecond Unix epoch or another [supported format](#timestamp-formats). Defaults to one hour ago. Loki returns results with timestamp greater or equal to this value.
- `end`: The end time for the query as a nanosecond Unix epoch or another [supported format](#timestamp-formats). Defaults to now. Loki returns results with timestamp lower than this value.
- `since`: A `duration` used to calculate `start` relative to `end`. If `end` is in the future, `start` is calculated as this duration before now. Any value specified for `start` supersedes this parameter.
- `step`: Query resolution step width in `duration` format or float number of seconds. `duration` refers to Prometheus duration strings of the form `[0-9]+[smhdwy]`. For example, 5m refers to a duration of 5 minutes. Defaults to a dynamic value based on `start` and `end`. Only applies to query types which produce a matrix response.
- `interval`: <span style="background-color:#f3f973;">This parameter is experimental; see the explanation under Step versus interval.</span> Only return entries at (or greater than) the specified interval, can be a `duration` format or float number of seconds. Only applies to queries which produce a stream response.
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward.`

In microservices mode, `/loki/api/v1/query_range` is exposed by the querier and the frontend.

### Step versus interval

Use the `step` parameter when making metric queries to Loki, or queries which return a matrix response. It is evaluated in exactly the same way Prometheus evaluates `step`. First the query will be evaluated at `start` and then evaluated again at `start + step` and again at `start + step + step` until `end` is reached. The result will be a matrix of the query result evaluated at each step.

Use the `interval` parameter when making log queries to Loki, or queries which return a stream response. It is evaluated by returning a log entry at `start`, then the next entry will be returned an entry with timestampe >= `start + interval`, and again at `start + interval + interval` and so on until `end` is reached. It does not fill missing entries.

<span style="background-color:#f3f973;">Note about the experimental nature of the interval parameter:</span> This flag may be removed in the future, if so it will likely be in favor of a LogQL expression to perform similar behavior, however that is uncertain at this time. [Issue 1779](https://github.com/grafana/loki/issues/1779) was created to track the discussion, if you are using `interval`, go add your use case and thoughts to that issue.

Response:

```
{
  "status": "success",
  "data": {
    "resultType": "matrix" | "streams",
    "result": [<matrix value>] | [<stream value>]
    "stats" : [<statistics>]
  }
}
```

Where `<matrix value>` is:

```
{
  "metric": {
    <label key-value pairs>
  },
  "values": [
    [
      <number: second unix epoch>,
      <string: value>
    ],
    ...
  ]
}
```

The items in the `values` array are sorted by timestamp, and the oldest item is first.

And `<stream value>` is:

```
{
  "stream": {
    <label key-value pairs>
  },
  "values": [
    [
      <string: nanosecond unix epoch>,
      <string: log line>
    ],
    ...
  ]
}
```

The items in the `values` array are sorted by timestamp.
The most recent item is first when using `direction=backward`.
The oldest item is first when using `direction=forward`.

See [statistics](#statistics) for information about the statistics returned by Loki.

### Examples

```bash
$ curl -G -s  "http://localhost:3100/loki/api/v1/query_range" --data-urlencode 'query=sum(rate({job="varlogs"}[10m])) by (level)' --data-urlencode 'step=300' | jq
{
  "status": "success",
  "data": {
    "resultType": "matrix",
    "result": [
      {
       "metric": {
          "level": "info"
        },
        "values": [
          [
            1588889221,
            "137.95"
          ],
          [
            1588889221,
            "467.115"
          ],
          [
            1588889221,
            "658.8516666666667"
          ]
        ]
      },
      {
        "metric": {
          "level": "warn"
        },
        "values": [
          [
            1588889221,
            "137.27833333333334"
          ],
          [
            1588889221,
            "467.69"
          ],
          [
            1588889221,
            "660.6933333333334"
          ]
        ]
      }
    ],
    "stats": {
      ...
    }
  }
}
```

```bash
$ curl -G -s  "http://localhost:3100/loki/api/v1/query_range" --data-urlencode 'query={job="varlogs"}' | jq
{
  "status": "success",
  "data": {
    "resultType": "streams",
    "result": [
      {
        "stream": {
          "filename": "/var/log/myproject.log",
          "job": "varlogs",
          "level": "info"
        },
        "values": [
          [
            "1569266497240578000",
            "foo"
          ],
          [
            "1569266492548155000",
            "bar"
          ]
        ]
      }
    ],
    "stats": {
      ...
    }
  }
}
```

## List labels within a range of time

```
GET /loki/api/v1/labels
```

`/loki/api/v1/labels` retrieves the list of known labels within a given time span.
Loki may use a larger time span than the one specified.
It accepts the following query parameters in the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.
- `since`: A `duration` used to calculate `start` relative to `end`. If `end` is in the future, `start` is calculated as this duration before now. Any value specified for `start` supersedes this parameter.

In microservices mode, `/loki/api/v1/labels` is exposed by the querier.

Response:

```
{
  "status": "success",
  "data": [
    <label string>,
    ...
  ]
}
```

### Examples

```bash
$ curl -G -s  "http://localhost:3100/loki/api/v1/labels" | jq
{
  "status": "success",
  "data": [
    "foo",
    "bar",
    "baz"
  ]
}
```

## List label values within a range of time

```
GET /loki/api/v1/label/<name>/values
```

`/loki/api/v1/label/<name>/values` retrieves the list of known values for a given
label within a given time span. Loki may use a larger time span than the one specified.
It accepts the following query parameters in the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.
- `since`: A `duration` used to calculate `start` relative to `end`. If `end` is in the future, `start` is calculated as this duration before now. Any value specified for `start` supersedes this parameter.
- `query`: A set of log stream selector that selects the streams to match and return label values for `<name>`. Example: `{"app": "myapp", "environment": "dev"}`

In microservices mode, `/loki/api/v1/label/<name>/values` is exposed by the querier.

Response:

```
{
  "status": "success",
  "data": [
    <label value>,
    ...
  ]
}
```

### Examples

```bash
$ curl -G -s  "http://localhost:3100/loki/api/v1/label/foo/values" | jq
{
  "status": "success",
  "data": [
    "cat",
    "dog",
    "axolotl"
  ]
}
```

## Stream log messages

```
GET /loki/api/v1/tail
```

`/loki/api/v1/tail` is a WebSocket endpoint that will stream log messages based on
a query. It accepts the following query parameters in the URL:

- `query`: The [LogQL]({{< relref "../query" >}}) query to perform
- `delay_for`: The number of seconds to delay retrieving logs to let slow
  loggers catch up. Defaults to 0 and cannot be larger than 5.
- `limit`: The max number of entries to return. It defaults to `100`.
- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to one hour ago.

In microservices mode, `/loki/api/v1/tail` is exposed by the querier.

Response (streamed):

```
{
  "streams": [
    {
      "stream": {
        <label key-value pairs>
      },
      "values": [
        [
          <string: nanosecond unix epoch>,
          <string: log line>
        ]
      ]
    }
  ],
  "dropped_entries": [
    {
      "labels": {
        <label key-value pairs>
      },
      "timestamp": "<nanosecond unix epoch>"
    }
  ]
}
```

## Push log entries to Loki

```
POST /loki/api/v1/push
```

`/loki/api/v1/push` is the endpoint used to send log entries to Loki. The default
behavior is for the POST body to be a snappy-compressed protobuf message:

- [Protobuf definition](https://github.com/grafana/loki/blob/main/pkg/logproto/logproto.proto)
- [Go client library](https://github.com/grafana/loki/blob/main/clients/pkg/promtail/client/client.go)

Alternatively, if the `Content-Type` header is set to `application/json`, a
JSON post body can be sent in the following format:

```
{
  "streams": [
    {
      "stream": {
        "label": "value"
      },
      "values": [
          [ "<unix epoch in nanoseconds>", "<log line>" ],
          [ "<unix epoch in nanoseconds>", "<log line>" ]
      ]
    }
  ]
}
```

You can optionally attach [structured metadata]({{< relref "../get-started/labels/structured-metadata" >}}) to each log line by adding a JSON object to the end of the log line array.
The JSON object must be a valid JSON object with string keys and string values. The JSON object should not contain any nested object.
The JSON object must be set immediately after the log line. Here is an example of a log entry with some structured metadata attached:

```
"values": [
    [ "<unix epoch in nanoseconds>", "<log line>", {"trace_id": "0242ac120002", "user_id": "superUser123"}]
]
```

You can set `Content-Encoding: gzip` request header and post gzipped JSON.

In microservices mode, `/loki/api/v1/push` is exposed by the distributor.

### Examples

```console
$ curl -v -H "Content-Type: application/json" -XPOST -s "http://localhost:3100/loki/api/v1/push" --data-raw \
  '{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}'
```

## Identify ready Loki instance

```
GET /ready
```

`/ready` returns HTTP 200 when the Loki instance is ready to accept traffic. If
running Loki on Kubernetes, `/ready` can be used as a readiness probe.

In microservices mode, the `/ready` endpoint is exposed by all components.

## Change log level at runtime

```
GET /log_level
POST /log_level
```

`/log_level` a `GET` returns the current log level and a `POST` lets you change the log level of a Loki process at runtime. This can be useful for accessing debugging information during an incident. Caution should be used when running at the `debug` log level, as this produces a large volume of data.

Params:

- `log_level`: A valid log level that can be passed as a URL param (`?log_level=<level>`) or as a form value in case of `POST`. Valid levels: [debug, info, warn, error]

In microservices mode, the `/log_level` endpoint is exposed by all components.

## Flush in-memory chunks to backing store

```
POST /flush
```

`/flush` triggers a flush of all in-memory chunks held by the ingesters to the
backing store. Mainly used for local testing.

In microservices mode, the `/flush` endpoint is exposed by the ingester.

### Tell ingester to release all resources on next SIGTERM

```
GET, POST, DELETE /ingester/prepare_shutdown
```

After a `POST` to the `prepare_shutdown` endpoint returns, when the ingester process is stopped with `SIGINT` / `SIGTERM`,
the ingester will be unregistered from the ring and in-memory time series data will be flushed to long-term storage.
This endpoint supersedes any YAML configurations and isn't necessary if the ingester is already
configured to unregister from the ring or to flush on shutdown.

A `GET` to the `prepare_shutdown` endpoint returns the status of this configuration, either `set` or `unset`.

A `DELETE` to the `prepare_shutdown` endpoint reverts the configuration of the ingester to its previous state
(with respect to unregistering on shutdown and flushing of in-memory time series data to long-term storage).

This API endpoint is usually used by Kubernetes-specific scale down automations such as the
[rollout-operator](https://github.com/grafana/rollout-operator).

## Flush in-memory chunks and shut down

```
POST /ingester/shutdown
```

`/ingester/shutdown` triggers a shutdown of the ingester and notably will _always_ flush any in memory chunks it holds.
This is helpful for scaling down WAL-enabled ingesters where we want to ensure old WAL directories are not orphaned,
but instead flushed to our chunk backend.

It accepts three URL query parameters `flush`, `delete_ring_tokens`, and `terminate`.

**URL query parameters:**

- `flush=<bool>`:
  Flag to control whether to flush any in-memory chunks the ingester holds. Defaults to `true`.
- `delete_ring_tokens=<bool>`:
  Flag to control whether to delete the file that contains the ingester ring tokens of the instance if the `-ingester.token-file-path` is specified. Defaults to `false.
- `terminate=<bool>`:
  Flag to control whether to terminate the Loki process after service shutdown. Defaults to `true`.

This handler, in contrast to the deprecated `/ingester/flush_shutdown` handler, terminates the Loki process by default.
This behaviour can be changed by setting the `terminate` query parameter to `false`.

In microservices mode, the `/ingester/shutdown` endpoint is exposed by the ingester.

## Display distributor consistent hash ring status

```
GET /distributor/ring
```

Displays a web page with the distributor hash ring status, including the state, healthy and last heartbeat time of each distributor.

## Return exposed Prometheus metrics

```
GET /metrics
```

`/metrics` returns exposed Prometheus metrics. See
[Observing Loki]({{< relref "../operations/observability" >}})
for a list of exported metrics.

In microservices mode, the `/metrics` endpoint is exposed by all components.

## List current configuration

```
GET /config
```

`/config` exposes the current configuration. The optional `mode` query parameter can be used to
modify the output. If it has the value `diff` only the differences between the default configuration
and the current are returned. A value of `defaults` returns the default configuration.

In microservices mode, the `/config` endpoint is exposed by all components.

## List running services

```
GET /services
```

`/services` returns a list of all running services and their current states.

Services can have the following states:

- **New**: Service is new, not running yet (initial state)
- **Starting**: Service is starting; if starting succeeds, service enters **Running** state
- **Running**: Service is fully running now; when service stops running, it enters **Stopping** state
- **Stopping**: Service is shutting down
- **Terminated**: Service has stopped successfully (terminal state)
- **Failed**: Service has failed in **Starting**, **Running** or **Stopping** state (terminal state)

## List build information

```
GET /loki/api/v1/status/buildinfo
```

`/loki/api/v1/status/buildinfo` exposes the build information in a JSON object. The fields are `version`, `revision`, `branch`, `buildDate`, `buildUser`, and `goVersion`.

## Format query

```
GET /loki/api/v1/format_query
POST /loki/api/v1/format_query
```

Params:

- `query`: A LogQL query string. Can be passed as URL param (`?query=<query>`) in case of both `GET` and `POST`. Or as form value in case of `POST`.

The `/loki/api/v1/format_query` endpoint allows to format LogQL queries. It returns an error if the passed LogQL is invalid. It is exposed by all Loki components and helps to improve readability and the debugging experience of LogQL queries.

The following example formats the expression LogQL `{foo=   "bar"}` into

```json
{
   "status" : "success",
   "data" : "{foo=\"bar\"}"
}
```

## List series

The Series API is available under the following:

- `GET /loki/api/v1/series`
- `POST /loki/api/v1/series`
- `GET /api/prom/series`
- `POST /api/prom/series`

This endpoint returns the list of time series that match a certain label set.

URL query parameters:

- `match[]=<series_selector>`: Repeated log stream selector argument that selects the streams to return. At least one `match[]` argument must be provided.
- `start=<nanosecond Unix epoch>`: Start timestamp.
- `end=<nanosecond Unix epoch>`: End timestamp.
- `since`: A `duration` used to calculate `start` relative to `end`. If `end` is in the future, `start` is calculated as this duration before now. Any value specified for `start` supersedes this parameter.

You can URL-encode these parameters directly in the request body by using the POST method and `Content-Type: application/x-www-form-urlencoded` header. This is useful when specifying a large or dynamic number of stream selectors that may breach server-side URL character limits.

In microservices mode, these endpoints are exposed by the querier.

### Examples

```console
$ curl -s "http://localhost:3100/loki/api/v1/series" --data-urlencode 'match[]={container_name=~"prometheus.*", component="server"}' --data-urlencode 'match[]={app="loki"}' | jq '.'
{
  "status": "success",
  "data": [
    {
      "container_name": "loki",
      "app": "loki",
      "stream": "stderr",
      "filename": "/var/log/pods/default_loki-stack-0_50835643-1df0-11ea-ba79-025000000001/loki/0.log",
      "name": "loki",
      "job": "default/loki",
      "controller_revision_hash": "loki-stack-757479754d",
      "statefulset_kubernetes_io_pod_name": "loki-stack-0",
      "release": "loki-stack",
      "namespace": "default",
      "instance": "loki-stack-0"
    },
    {
      "chart": "prometheus-9.3.3",
      "container_name": "prometheus-server-configmap-reload",
      "filename": "/var/log/pods/default_loki-stack-prometheus-server-696cc9ddff-87lmq_507b1db4-1df0-11ea-ba79-025000000001/prometheus-server-configmap-reload/0.log",
      "instance": "loki-stack-prometheus-server-696cc9ddff-87lmq",
      "pod_template_hash": "696cc9ddff",
      "app": "prometheus",
      "component": "server",
      "heritage": "Tiller",
      "job": "default/prometheus",
      "namespace": "default",
      "release": "loki-stack",
      "stream": "stderr"
    },
    {
      "app": "prometheus",
      "component": "server",
      "filename": "/var/log/pods/default_loki-stack-prometheus-server-696cc9ddff-87lmq_507b1db4-1df0-11ea-ba79-025000000001/prometheus-server/0.log",
      "release": "loki-stack",
      "namespace": "default",
      "pod_template_hash": "696cc9ddff",
      "stream": "stderr",
      "chart": "prometheus-9.3.3",
      "container_name": "prometheus-server",
      "heritage": "Tiller",
      "instance": "loki-stack-prometheus-server-696cc9ddff-87lmq",
      "job": "default/prometheus"
    }
  ]
}
```

## Index Stats

The `/loki/api/v1/index/stats` endpoint can be used to query the index for the number of `streams`, `chunks`, `entries`, and `bytes` that a query resolves to.

URL query parameters:

- `query`: The [LogQL]({{< relref "../query" >}}) matchers to check (i.e. `{job="foo", env!="dev"}`)
- `start=<nanosecond Unix epoch>`: Start timestamp.
- `end=<nanosecond Unix epoch>`: End timestamp.

You can URL-encode these parameters directly in the request body by using the POST method and `Content-Type: application/x-www-form-urlencoded` header. This is useful when specifying a large or dynamic number of stream selectors that may breach server-side URL character limits.

Response:

```json
{
  "streams": 100,
  "chunks": 1000,
  "entries": 5000,
  "bytes": 100000
}
```

It is an approximation with the following caveats:

- It does not include data from the ingesters
- It is a probabilistic technique
- streams/chunks which span multiple period configurations may be counted twice.

These make it generally more helpful for larger queries.
It can be used for better understanding the throughput requirements and data topology for a list of matchers over a period of time.

## Volume

The `/loki/api/v1/index/volume` and `/loki/api/v1/index/volume_range` endpoints can be used to query the index for volume information about label and label-value combinations. This is helpful in exploring the logs Loki has ingested to find high or low volume streams. The `volume` endpoint returns results for a single point in time, the time the query was processed. Each datapoint represents an aggregation of the matching label or series over the requested time period, returned in a Prometheus style vector response. The `volume_range` endoint returns a series of datapoints over a range of time, in Prometheus style matrix response, for each matching set of labels or series. The number of timestamps returned when querying `volume_range` will be determined by the provided `step` parameter and the requested time range.

The `query` should be a valid LogQL stream selector, for example `{job="foo", env=~".+"}`. By default, these endpoints will aggregate into series consisting of all matches for labels included in the query. For example, assuming you have the streams `{job="foo", env="prod", team="alpha"}`, `{job="bar", env="prod", team="beta"}`, `{job="foo", env="dev", team="alpha"}`, and `{job="bar", env="dev", team="beta"}` in your system. The query `{job="foo", env=~".+"}` would return the two metric series `{job="foo", env="dev"}` and `{job="foo", env="prod"}`, each with datapoints representing the accumulate values of chunks for the streams matching that selector, which in this case would be the streams `{job="foo", env="dev", team="alpha"}` and `{job="foo", env="prod", team="alpha"}`, respectively.

There are two parameters which can affect the aggregation strategy. First, a comma-seperated list of `targetLabels` can be provided, allowing volumes to be aggregated by the speficied `targetLabels` only. This is useful for negations. For example, if you said `{team="alpha", env!="dev"}`, the default behavior would include `env` in the aggregation set. However, maybe you're looking for all non-dev jobs for team alpha, and you don't care which env those are in (other than caring that they're not dev jobs). To achieve this, you could specify `targetLabels=team,job`, resulting in a single metric series (in this case) of `{team="alpha", job="foo}`.

The other way to change aggregations is with the `aggregateBy` parameter. The default value for this is `series`, which aggregates into combinations of matching key-value pairs. Alternately this can be specified as `labels`, which will aggregate into labels only. In this case, the response will have a metric series with a label name matching each label, and a label value of `""`. This is useful for exploring logs at a high level. For example, if you wanted to know what percentage of your logs had a `team` label, you could query your logs with `aggregateBy=labels` and a query with either an exact or regex match on `team`, or by including `team` in the list of `targetLabels`.

URL query parameters:

- `query`: The [LogQL]({{< relref "../query" >}}) matchers to check (i.e. `{job="foo", env=~".+"}`). This parameter is required.
- `start=<nanosecond Unix epoch>`: Start timestamp. This parameter is required.
- `end=<nanosecond Unix epoch>`: End timestamp. This parameter is required.
- `limit`: How many metric series to return. The parameter is optional, the default is 100.
- `step`: Query resolution step width in `duration` format or float number of seconds. `duration` refers to Prometheus duration strings of the form `[0-9]+[smhdwy]`. For example, 5m refers to a duration of 5 minutes. Defaults to a dynamic value based on `start` and `end`. Only applies when querying the `volume_range` endpoint, which will always return a Prometheus style matrix response. This parameter is optional, and only applicable for `query_range`. The default step configured for range queries will be used when not provided.
- `targetLabels`: A comma separated list of labels to aggregate into. This parameter is optional. When not provided, volumes will be aggregated into the matching labels or label-value pairs.
- `aggregateBy`: Whether to aggregate into labels or label-value pairs. This parameter is optional, the default is label-value pairs.

You can URL-encode these parameters directly in the request body by using the POST method and `Content-Type: application/x-www-form-urlencoded` header. This is useful when specifying a large or dynamic number of stream selectors that may breach server-side URL character limits.

## Statistics

Query endpoints such as `/api/prom/query`, `/loki/api/v1/query` and `/loki/api/v1/query_range` return a set of statistics about the query execution. Those statistics allow users to understand the amount of data processed and at which speed.

The example belows show all possible statistics returned with their respective description.

```json
{
  "status": "success",
  "data": {
    "resultType": "streams",
    "result": [],
    "stats": {
      "ingester": {
        "compressedBytes": 0, // Total bytes of compressed chunks (blocks) processed by ingesters
        "decompressedBytes": 0, // Total bytes decompressed and processed by ingesters
        "decompressedLines": 0, // Total lines decompressed and processed by ingesters
        "headChunkBytes": 0, // Total bytes read from ingesters head chunks
        "headChunkLines": 0, // Total lines read from ingesters head chunks
        "totalBatches": 0, // Total batches sent by ingesters
        "totalChunksMatched": 0, // Total chunks matched by ingesters
        "totalDuplicates": 0, // Total of duplicates found by ingesters
        "totalLinesSent": 0, // Total lines sent by ingesters
        "totalReached": 0 // Amount of ingesters reached.
      },
      "store": {
        "compressedBytes": 0, // Total bytes of compressed chunks (blocks) processed by the store
        "decompressedBytes": 0, // Total bytes decompressed and processed by the store
        "decompressedLines": 0, // Total lines decompressed and processed by the store
        "chunksDownloadTime": 0, // Total time spent downloading chunks in seconds (float)
        "totalChunksRef": 0, // Total chunks found in the index for the current query
        "totalChunksDownloaded": 0, // Total of chunks downloaded
        "totalDuplicates": 0 // Total of duplicates removed from replication
      },
      "summary": {
        "bytesProcessedPerSecond": 0, // Total of bytes processed per second
        "execTime": 0, // Total execution time in seconds (float)
        "linesProcessedPerSecond": 0, // Total lines processed per second
        "queueTime": 0, // Total queue time in seconds (float)
        "totalBytesProcessed": 0, // Total amount of bytes processed overall for this request
        "totalLinesProcessed": 0 // Total amount of lines processed overall for this request
      }
    }
  }
}
```

## Ruler

The ruler API endpoints require to configure a backend object storage to store the recording rules and alerts. The ruler API uses the concept of a "namespace" when creating rule groups. This is a stand-in for the name of the rule file in Prometheus. Rule groups must be named uniquely within a namespace.

### Ruler ring status

```
GET /ruler/ring
```

Displays a web page with the ruler hash ring status, including the state, healthy and last heartbeat time of each ruler.

### List rule groups

```
GET /loki/api/v1/rules
```

List all rules configured for the authenticated tenant. This endpoint returns a YAML dictionary with all the rule groups for each namespace and `200` status code on success.

#### Example response

```yaml
---
<namespace1>:
- name: <string>
  interval: <duration;optional>
  rules:
  - alert: <string>
      expr: <string>
      for: <duration>
      annotations:
      <annotation_name>: <string>
      labels:
      <label_name>: <string>
- name: <string>
  interval: <duration;optional>
  rules:
  - alert: <string>
      expr: <string>
      for: <duration>
      annotations:
      <annotation_name>: <string>
      labels:
      <label_name>: <string>
<namespace2>:
- name: <string>
  interval: <duration;optional>
  rules:
  - alert: <string>
      expr: <string>
      for: <duration>
      annotations:
      <annotation_name>: <string>
      labels:
      <label_name>: <string>
```

### Get rule groups by namespace

```
GET /loki/api/v1/rules/{namespace}
```

Returns the rule groups defined for a given namespace.

#### Example response

```yaml
name: <string>
interval: <duration;optional>
rules:
  - alert: <string>
    expr: <string>
    for: <duration>
    annotations:
      <annotation_name>: <string>
    labels:
      <label_name>: <string>
```

### Get rule group

```
GET /loki/api/v1/rules/{namespace}/{groupName}
```

Returns the rule group matching the request namespace and group name.

### Set rule group

```
POST /loki/api/v1/rules/{namespace}
```

Creates or updates a rule group. This endpoint expects a request with `Content-Type: application/yaml` header and the rules **YAML** definition in the request body, and returns `202` on success.

#### Example request

Request headers:

- `Content-Type: application/yaml`

Request body:

```yaml
name: <string>
interval: <duration;optional>
rules:
  - alert: <string>
    expr: <string>
    for: <duration>
    annotations:
      <annotation_name>: <string>
    labels:
      <label_name>: <string>
```

### Delete rule group

```
DELETE /loki/api/v1/rules/{namespace}/{groupName}

```

Deletes a rule group by namespace and group name. This endpoints returns `202` on success.

### Delete namespace

```
DELETE /loki/api/v1/rules/{namespace}
```

Deletes all the rule groups in a namespace (including the namespace itself). This endpoint returns `202` on success.

### List rules

```
GET /prometheus/api/v1/rules
```

Prometheus-compatible rules endpoint to list alerting and recording rules that are currently loaded.

For more information, refer to the [Prometheus rules](https://prometheus.io/docs/prometheus/latest/querying/api/#rules) documentation.

### List alerts

```
GET /prometheus/api/v1/alerts
```

Prometheus-compatible rules endpoint to list all active alerts.

For more information, refer to the Prometheus [alerts](https://prometheus.io/docs/prometheus/latest/querying/api/#alerts) documentation.

## Compactor

### Compactor ring status

```
GET /compactor/ring
```

Displays a web page with the compactor hash ring status, including the state, health, and last heartbeat time of each compactor.

### Request log deletion

```
POST /loki/api/v1/delete
PUT /loki/api/v1/delete
```

Create a new delete request for the authenticated tenant.
The [log entry deletion]({{< relref "../operations/storage/logs-deletion" >}}) documentation has configuration details.

Log entry deletion is supported _only_ when the BoltDB Shipper is configured for the index store.

Query parameters:

- `query=<series_selector>`: query argument that identifies the streams from which to delete with optional line filters.
- `start=<rfc3339 | unix_seconds_timestamp>`: A timestamp that identifies the start of the time window within which entries will be deleted. This parameter is required.
- `end=<rfc3339 | unix_seconds_timestamp>`: A timestamp that identifies the end of the time window within which entries will be deleted. If not specified, defaults to the current time.
- `max_interval=<duration>`: The maximum time period the delete request can span. If the request is larger than this value, it is split into several requests of <= `max_interval`. Valid time units are `s`, `m`, and `h`.

A 204 response indicates success.

The query parameter can also include filter operations. For example `query={foo="bar"} |= "other"` will filter out lines that contain the string "other" for the streams matching the stream selector `{foo="bar"}`.

#### Examples

URL encode the `query` parameter. This sample form of a cURL command URL encodes `query={foo="bar"}`:

```bash
curl -g -X POST \
  'http://127.0.0.1:3100/loki/api/v1/delete?query={foo="bar"}&start=1591616227&end=1591619692' \
  -H 'X-Scope-OrgID: 1'
```

The same example deletion request for Grafana Enterprise Logs uses Basic Authentication and specifies the tenant name as a user; `Tenant1` is the tenant name in this example. The password in this example is an access policy token that has been defined in the API_TOKEN environment variable. The token must be for an access policy with `logs:delete` scope for the tenant specified in the user field:

```bash
curl -u "Tenant1:$API_TOKEN" \
  -g -X POST \
  'http://127.0.0.1:3100/loki/api/v1/delete?query={foo="bar"}&start=1591616227&end=1591619692'
```

### List log deletion requests

```
GET /loki/api/v1/delete
```

List the existing delete requests for the authenticated tenant.
The [log entry deletion]({{< relref "../operations/storage/logs-deletion" >}}) documentation has configuration details.

Log entry deletion is supported _only_ when the BoltDB Shipper is configured for the index store.

List the existing delete requests using the following API:

```
GET /loki/api/v1/delete
```

This endpoint returns both processed and unprocessed deletion requests. It does not list canceled requests, as those requests will have been removed from storage.

#### Examples

Example cURL command:

```
curl -X GET \
  <compactor_addr>/loki/api/v1/delete \
  -H 'X-Scope-OrgID: <orgid>'
```

The same example deletion request for Grafana Enterprise Logs uses Basic Authentication and specifies the tenant name as a user; `Tenant1` is the tenant name in this example. The password in this example is an access policy token that has been defined in the API_TOKEN environment variable. The token must be for an access policy with `logs:delete` scope for the tenant specified in the user field.

```bash
curl -u "Tenant1:$API_TOKEN" \
  -X GET \
  <compactor_addr>/loki/api/v1/delete
```

### Request cancellation of a delete request

```
DELETE /loki/api/v1/delete
```

Remove a delete request for the authenticated tenant.
The [log entry deletion]({{< relref "../operations/storage/logs-deletion" >}}) documentation has configuration details.

Loki allows cancellation of delete requests until the requests are picked up for processing. It is controlled by the `delete_request_cancel_period` YAML configuration or the equivalent command line option when invoking Loki. To cancel a delete request that has been picked up for processing or is partially complete, pass the `force=true` query parameter to the API.

Log entry deletion is supported _only_ when the BoltDB Shipper is configured for the index store.

Cancel a delete request using this compactor endpoint:

```
DELETE /loki/api/v1/delete
```

Query parameters:

- `request_id=<request_id>`: Identifies the delete request to cancel; IDs are found using the `delete` endpoint.
- `force=<boolean>`: When the `force` query parameter is true, partially completed delete requests will be canceled. NOTE: some data from the request may still be deleted and the deleted request will be listed as 'processed'

A 204 response indicates success.

#### Examples

Example cURL command:

```
curl -X DELETE \
  '<compactor_addr>/loki/api/v1/delete?request_id=<request_id>' \
  -H 'X-Scope-OrgID: <tenant-id>'
```

The same example deletion cancellation request for Grafana Enterprise Logs uses Basic Authentication and specifies the tenant name as a user; `Tenant1` is the tenant name in this example. The password in this example is an access policy token that has been defined in the API_TOKEN environment variable. The token must be for an access policy with `logs:delete` scope for the tenant specified in the user field.

```bash
curl -u "Tenant1:$API_TOKEN" \
  -X DELETE \
  '<compactor_addr>/loki/api/v1/delete?request_id=<request_id>'
```

## Deprecated endpoints

### `GET /api/prom/tail`

> **DEPRECATED**: `/api/prom/tail` is deprecated. Use `/loki/api/v1/tail`
> instead.

`/api/prom/tail` is a WebSocket endpoint that will stream log messages based on
a query. It accepts the following query parameters in the URL:

- `query`: The [LogQL]({{< relref "../query" >}}) query to perform
- `delay_for`: The number of seconds to delay retrieving logs to let slow
  loggers catch up. Defaults to 0 and cannot be larger than 5.
- `limit`: The max number of entries to return
- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to one hour ago.

In microservices mode, `/api/prom/tail` is exposed by the querier.

Response (streamed):

```json
{
  "streams": [
    {
      "labels": "<LogQL label key-value pairs>",
      "entries": [
        {
          "ts": "<RFC3339Nano timestamp>",
          "line": "<log line>"
        }
      ]
    }
  ],
  "dropped_entries": [
    {
      "Timestamp": "<RFC3339Nano timestamp>",
      "Labels": "<LogQL label key-value pairs>"
    }
  ]
}
```

`dropped_entries` will be populated when the tailer could not keep up with the
amount of traffic in Loki. When present, it indicates that the entries received
in the streams is not the full amount of logs that are present in Loki. Note
that the keys in `dropped_entries` will be sent as uppercase `Timestamp`
and `Labels` instead of `labels` and `ts` like in the entries for the stream.

As the response is streamed, the object defined by the response format above
will be sent over the WebSocket multiple times.

### `GET /api/prom/query`

> **WARNING**: `/api/prom/query` is DEPRECATED; use `/loki/api/v1/query_range`
> instead.

`/api/prom/query` supports doing general queries. The URL query parameters
support the following values:

- `query`: The [LogQL]({{< relref "../query" >}}) query to perform
- `limit`: The max number of entries to return
- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to one hour ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.
- `since`: A `duration` used to calculate `start` relative to `end`. If `end` is in the future, `start` is calculated as this duration before now. Any value specified for `start` supersedes this parameter.
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward.`
- `regexp`: a regex to filter the returned results

In microservices mode, `/api/prom/query` is exposed by the querier and the frontend.

Note that the larger the time span between `start` and `end` will cause
additional load on Loki and the index store, resulting in slower queries.

Response:

```
{
  "streams": [
    {
      "labels": "<LogQL label key-value pairs>",
      "entries": [
        {
          "ts": "<RFC3339Nano string>",
          "line": "<log line>"
        },
        ...
      ],
    },
    ...
  ],
  "stats": [<statistics>]
}
```

See [statistics](#statistics) for information about the statistics returned by Loki.

#### Examples

```console
$ curl -G -s "http://localhost:3100/api/prom/query" --data-urlencode 'query={foo="bar"}' | jq
{
  "streams": [
    {
      "labels": "{filename=\"/var/log/myproject.log\", job=\"varlogs\", level=\"info\"}",
      "entries": [
        {
          "ts": "2019-06-06T19:25:41.972739Z",
          "line": "foo"
        },
        {
          "ts": "2019-06-06T19:25:41.972722Z",
          "line": "bar"
        }
      ]
    }
  ],
  "stats": {
    ...
  }
}
```

### `GET /api/prom/label/<name>/values`

> **WARNING**: `/api/prom/label/<name>/values` is DEPRECATED; use `/loki/api/v1/label/<name>/values`

`/api/prom/label/<name>/values` retrieves the list of known values for a given
label within a given time span. It accepts the following query parameters in
the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.
- `since`: A `duration` used to calculate `start` relative to `end`. If `end` is in the future, `start` is calculated as this duration before now. Any value specified for `start` supersedes this parameter.

In microservices mode, `/api/prom/label/<name>/values` is exposed by the querier.

Response:

```
{
  "values": [
    <label value>,
    ...
  ]
}
```

#### Examples

```console
$ curl -G -s  "http://localhost:3100/api/prom/label/foo/values" | jq
{
  "values": [
    "cat",
    "dog",
    "axolotl"
  ]
}
```

### `GET /api/prom/label`

> **WARNING**: `/api/prom/label` is DEPRECATED; use `/loki/api/v1/labels`

`/api/prom/label` retrieves the list of known labels within a given time span. It
accepts the following query parameters in the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.
- `since`: A `duration` used to calculate `start` relative to `end`. If `end` is in the future, `start` is calculated as this duration before now. Any value specified for `start` supersedes this parameter.

In microservices mode, `/api/prom/label` is exposed by the querier.

Response:

```
{
  "values": [
    <label string>,
    ...
  ]
}
```

#### Examples

```console
$ curl -G -s  "http://localhost:3100/api/prom/label" | jq
{
  "values": [
    "foo",
    "bar",
    "baz"
  ]
}
```

### `POST /api/prom/push`

> **WARNING**: `/api/prom/push` is DEPRECATED; use `/loki/api/v1/push`
> instead.

`/api/prom/push` is the endpoint used to send log entries to Loki. The default
behavior is for the POST body to be a snappy-compressed protobuf message:

- [Protobuf definition](https://github.com/grafana/loki/blob/main/pkg/logproto/logproto.proto)
- [Go client library](https://github.com/grafana/loki/blob/main/clients/pkg/promtail/client/client.go)

Alternatively, if the `Content-Type` header is set to `application/json`, a
JSON post body can be sent in the following format:

```
{
  "streams": [
    {
      "labels": "<LogQL label key-value pairs>",
      "entries": [
        {
          "ts": "<RFC3339Nano string>",
          "line": "<log line>"
        }
      ]
    }
  ]
}
```

In microservices mode, `/api/prom/push` is exposed by the distributor.

#### Examples

```console
$ curl -H "Content-Type: application/json" -XPOST -s "https://localhost:3100/api/prom/push" --data-raw \
  '{"streams": [{ "labels": "{foo=\"bar\"}", "entries": [{ "ts": "2018-12-18T08:28:06.801064-04:00", "line": "fizzbuzz" }] }]}'
```

### `POST /ingester/flush_shutdown`

> **WARNING**: `/ingester/flush_shutdown` is DEPRECATED; use `/ingester/shutdown?flush=true`
> instead.

`/ingester/flush_shutdown` triggers a shutdown of the ingester and notably will _always_ flush any in memory chunks it holds.
This is helpful for scaling down WAL-enabled ingesters where we want to ensure old WAL directories are not orphaned,
but instead flushed to our chunk backend.

In microservices mode, the `/ingester/flush_shutdown` endpoint is exposed by the ingester.
