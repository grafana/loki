---
title: HTTP API
weight: 900
---

# Loki HTTP API

Loki exposes an HTTP API for pushing, querying, and tailing log data.
Note that [authenticating](../operations/authentication/) against the API is
out of scope for Loki.

## Microservices mode

When deploying Loki in microservices mode, the set of endpoints exposed by each
component is different.

These endpoints are exposed by all components:

- [`GET /ready`](#get-ready)
- [`GET /metrics`](#get-metrics)
- [`GET /config`](#get-config)
- [`GET /loki/api/v1/status/buildinfo`](#get-lokiapiv1statusbuildinfo)

These endpoints are exposed by the querier and the frontend:

- [Loki's HTTP API](#lokis-http-api)
  - [Microservices Mode](#microservices-mode)
  - [Matrix, Vector, And Streams](#matrix-vector-and-streams)
  - [`GET /loki/api/v1/query`](#get-lokiapiv1query)
    - [Examples](#examples)
  - [`GET /loki/api/v1/query_range`](#get-lokiapiv1query_range)
        - [Step vs Interval](#step-vs-interval)
    - [Examples](#examples-1)
  - [`GET /loki/api/v1/labels`](#get-lokiapiv1labels)
    - [Examples](#examples-2)
  - [`GET /loki/api/v1/label/<name>/values`](#get-lokiapiv1labelnamevalues)
    - [Examples](#examples-3)
  - [`GET /loki/api/v1/tail`](#get-lokiapiv1tail)
  - [`POST /loki/api/v1/push`](#post-lokiapiv1push)
    - [Examples](#examples-4)
  - [`GET /api/prom/tail`](#get-apipromtail)
  - [`GET /api/prom/query`](#get-apipromquery)
    - [Examples](#examples-5)
  - [`GET /api/prom/label`](#get-apipromlabel)
    - [Examples](#examples-6)
  - [`GET /api/prom/label/<name>/values`](#get-apipromlabelnamevalues)
    - [Examples](#examples-7)
  - [`POST /api/prom/push`](#post-apiprompush)
    - [Examples](#examples-8)
  - [`GET /ready`](#get-ready)
  - [`GET /metrics`](#get-metrics)
  - [Series](#series)
    - [Examples](#examples-9)
  - [Statistics](#statistics)

While these endpoints are exposed by just the distributor:

- [`POST /loki/api/v1/push`](#post-lokiapiv1push)

And these endpoints are exposed by just the ingester:

- [`POST /flush`](#post-flush)
- [`POST /ingester/flush_shutdown`](#post-ingesterflush_shutdown)

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

A [list of clients](../clients) can be found in the clients documentation.

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

## `GET /loki/api/v1/query`

`/loki/api/v1/query` allows for doing queries against a single point in time. The URL
query parameters support the following values:

- `query`: The [LogQL](../logql/) query to perform
- `limit`: The max number of entries to return
- `time`: The evaluation time for the query as a nanosecond Unix epoch. Defaults to now.
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward.`

In microservices mode, `/loki/api/v1/query` is exposed by the querier and the frontend.

Response:

```
{
  "status": "success",
  "data": {
    "resultType": "vector" | "streams",
    "result": [<vector value>] | [<stream value>].
    "stats" : [<statistics>]
  }
}
```

Where `<vector value>` is:

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

See [statistics](#Statistics) for information about the statistics returned by Loki.

### Examples

```bash
$ curl -G -s  "http://localhost:3100/loki/api/v1/query" --data-urlencode 'query=sum(rate({job="varlogs"}[10m])) by (level)' | jq
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

```bash
$ curl -G -s  "http://localhost:3100/loki/api/v1/query" --data-urlencode 'query={job="varlogs"}' | jq
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
            "1568234281726420425",
            "foo"
          ],
          [
            "1568234269716526880",
            "bar"
          ]
        ],
      }
    ],
    "stats": {
      ...
    }
  }
}
```

## `GET /loki/api/v1/query_range`

`/loki/api/v1/query_range` is used to do a query over a range of time and
accepts the following query parameters in the URL:

- `query`: The [LogQL](../logql/) query to perform
- `limit`: The max number of entries to return
- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to one hour ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.
- `step`: Query resolution step width in `duration` format or float number of seconds. `duration` refers to Prometheus duration strings of the form `[0-9]+[smhdwy]`. For example, 5m refers to a duration of 5 minutes. Defaults to a dynamic value based on `start` and `end`.  Only applies to query types which produce a matrix response.
- `interval`: <span style="background-color:#f3f973;">This parameter is experimental; see the explanation under Step versus Interval.</span> Only return entries at (or greater than) the specified interval, can be a `duration` format or float number of seconds. Only applies to queries which produce a stream response.
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward.`

In microservices mode, `/loki/api/v1/query_range` is exposed by the querier and the frontend.

##### Step versus Interval

Use the `step` parameter when making metric queries to Loki, or queries which return a matrix response.  It is evaluated in exactly the same way Prometheus evaluates `step`.  First the query will be evaluated at `start` and then evaluated again at `start + step` and again at `start + step + step` until `end` is reached.  The result will be a matrix of the query result evaluated at each step.

Use the `interval` parameter when making log queries to Loki, or queries which return a stream response. It is evaluated by returning a log entry at `start`, then the next entry will be returned an entry with timestampe >= `start + interval`, and again at `start + interval + interval` and so on until `end` is reached.  It does not fill missing entries.

<span style="background-color:#f3f973;">Note about the experimental nature of the interval parameter:</span> This flag may be removed in the future, if so it will likely be in favor of a LogQL expression to perform similar behavior, however that is uncertain at this time.  [Issue 1779](https://github.com/grafana/loki/issues/1779) was created to track the discussion, if you are using `interval` please go add your use case and thoughts to that issue.



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
    <number: second unix epoch>,
    <string: value>
  ]
}
```

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

See [statistics](#Statistics) for information about the statistics returned by Loki.

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

## `GET /loki/api/v1/labels`

`/loki/api/v1/labels` retrieves the list of known labels within a given time span. It
accepts the following query parameters in the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.

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

## `GET /loki/api/v1/label/<name>/values`

`/loki/api/v1/label/<name>/values` retrieves the list of known values for a given
label within a given time span. It accepts the following query parameters in
the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.

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

## `GET /loki/api/v1/tail`

`/loki/api/v1/tail` is a WebSocket endpoint that will stream log messages based on
a query. It accepts the following query parameters in the URL:

- `query`: The [LogQL](../logql/) query to perform
- `delay_for`: The number of seconds to delay retrieving logs to let slow
    loggers catch up. Defaults to 0 and cannot be larger than 5.
- `limit`: The max number of entries to return
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

## `POST /loki/api/v1/push`

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

You can set `Content-Encoding: gzip` request header and post gzipped JSON.

Loki can be configured to [accept out-of-order writes](../configuration/#accept-out-of-order-writes).

In microservices mode, `/loki/api/v1/push` is exposed by the distributor.

### Examples

```bash
$ curl -v -H "Content-Type: application/json" -XPOST -s "http://localhost:3100/loki/api/v1/push" --data-raw \
  '{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}'
```

## `GET /api/prom/tail`

> **DEPRECATED**: `/api/prom/tail` is deprecated. Use `/loki/api/v1/tail`
> instead.

`/api/prom/tail` is a WebSocket endpoint that will stream log messages based on
a query. It accepts the following query parameters in the URL:

- `query`: The [LogQL](../logql/) query to perform
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

## `GET /api/prom/query`

> **WARNING**: `/api/prom/query` is DEPRECATED; use `/loki/api/v1/query_range`
> instead.

`/api/prom/query` supports doing general queries. The URL query parameters
support the following values:

- `query`: The [LogQL](../logql/) query to perform
- `limit`: The max number of entries to return
- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to one hour ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.
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

See [statistics](#Statistics) for information about the statistics returned by Loki.

### Examples

```bash
$ curl -G -s  "http://localhost:3100/api/prom/query" --data-urlencode '{foo="bar"}' | jq
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

## `GET /api/prom/label`

> **WARNING**: `/api/prom/label` is DEPRECATED; use `/loki/api/v1/label`

`/api/prom/label` retrieves the list of known labels within a given time span. It
accepts the following query parameters in the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.

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

### Examples

```bash
$ curl -G -s  "http://localhost:3100/api/prom/label" | jq
{
  "values": [
    "foo",
    "bar",
    "baz"
  ]
}
```

## `GET /api/prom/label/<name>/values`

> **WARNING**: `/api/prom/label/<name>/values` is DEPRECATED; use `/loki/api/v1/label/<name>/values`

`/api/prom/label/<name>/values` retrieves the list of known values for a given
label within a given time span. It accepts the following query parameters in
the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.

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

### Examples

```bash
$ curl -G -s  "http://localhost:3100/api/prom/label/foo/values" | jq
{
  "values": [
    "cat",
    "dog",
    "axolotl"
  ]
}
```

## `POST /api/prom/push`

> **WARNING**: `/api/prom/push` is DEPRECATED; use `/loki/api/v1/push`
> instead.

`/api/prom/push` is the endpoint used to send log entries to Loki. The default
behavior is for the POST body to be a snappy-compressed protobuf message:

- [Protobuf definition](https://github.com/grafana/loki/tree/master/pkg/logproto/logproto.proto)
- [Go client library](https://github.com/grafana/loki/tree/master/pkg/promtail/client/client.go)

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

Loki can be configured to [accept out-of-order writes](../configuration/#accept-out-of-order-writes).

In microservices mode, `/api/prom/push` is exposed by the distributor.

### Examples

```bash
$ curl -H "Content-Type: application/json" -XPOST -s "https://localhost:3100/api/prom/push" --data-raw \
  '{"streams": [{ "labels": "{foo=\"bar\"}", "entries": [{ "ts": "2018-12-18T08:28:06.801064-04:00", "line": "fizzbuzz" }] }]}'
```

## `GET /ready`

`/ready` returns HTTP 200 when the Loki ingester is ready to accept traffic. If
running Loki on Kubernetes, `/ready` can be used as a readiness probe.

In microservices mode, the `/ready` endpoint is exposed by all components.

## `POST /flush`

`/flush` triggers a flush of all in-memory chunks held by the ingesters to the
backing store. Mainly used for local testing.

In microservices mode, the `/flush` endpoint is exposed by the ingester.

## `POST /ingester/flush_shutdown`

`/ingester/flush_shutdown` triggers a shutdown of the ingester and notably will _always_ flush any in memory chunks it holds.
This is helpful for scaling down WAL-enabled ingesters where we want to ensure old WAL directories are not orphaned,
but instead flushed to our chunk backend.

In microservices mode, the `/ingester/flush_shutdown` endpoint is exposed by the ingester.

## `GET /metrics`

`/metrics` exposes Prometheus metrics. See
[Observing Loki](../operations/observability/)
for a list of exported metrics.

In microservices mode, the `/metrics` endpoint is exposed by all components.

## `GET /config`

`/config` exposes the current configuration. The optional `mode` query parameter can be used to
modify the output. If it has the value `diff` only the differences between the default configuration
and the current are returned. A value of `defaults` returns the default configuration.

In microservices mode, the `/config` endpoint is exposed by all components.

## `GET /loki/api/v1/status/buildinfo`

`/loki/api/v1/status/buildinfo` exposes the build information in a JSON object. The fields are `version`, `revision`, `branch`, `buildDate`, `buildUser`, and `goVersion`.

## Series

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

You can URL-encode these parameters directly in the request body by using the POST method and `Content-Type: application/x-www-form-urlencoded` header. This is useful when specifying a large or dynamic number of stream selectors that may breach server-side URL character limits.

In microservices mode, these endpoints are exposed by the querier.

### Examples

``` bash
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
     "ingester" : {
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
        "decompressedBytes": 0,  // Total bytes decompressed and processed by the store
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
        "totalBytesProcessed":0, // Total amount of bytes processed overall for this request
        "totalLinesProcessed":0 // Total amount of lines processed overall for this request
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

<span style="background-color:#f3f973;">This experimental endpoint is disabled by default and can be enabled via the -experimental.ruler.enable-api CLI flag or the YAML config option.</span>

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

<span style="background-color:#f3f973;">This experimental endpoint is disabled by default and can be enabled via the -experimental.ruler.enable-api CLI flag or the YAML config option.</span>

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

<span style="background-color:#f3f973;">This experimental endpoint is disabled by default and can be enabled via the -experimental.ruler.enable-api CLI flag or the YAML config option.</span>

```
GET /loki/api/v1/rules/{namespace}/{groupName}
```

Returns the rule group matching the request namespace and group name.

### Set rule group

<span style="background-color:#f3f973;">This experimental endpoint is disabled by default and can be enabled via the -experimental.ruler.enable-api CLI flag or the YAML config option.</span>

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

<span style="background-color:#f3f973;">This experimental endpoint is disabled by default and can be enabled via the -experimental.ruler.enable-api CLI flag or the YAML config option.</span>

```
DELETE /loki/api/v1/rules/{namespace}
```

Deletes all the rule groups in a namespace (including the namespace itself). This endpoint returns `202` on success.

### List rules

<span style="background-color:#f3f973;">This experimental endpoint is disabled by default and can be enabled via the -experimental.ruler.enable-api CLI flag or the YAML config option.</span>

```
GET /prometheus/api/v1/rules
```

Prometheus-compatible rules endpoint to list alerting and recording rules that are currently loaded.

For more information, refer to the [Prometheus rules](https://prometheus.io/docs/prometheus/latest/querying/api/#rules) documentation.

### List alerts

<span style="background-color:#f3f973;">This experimental endpoint is disabled by default and can be enabled via the -experimental.ruler.enable-api CLI flag or the YAML config option.</span>

```
GET /prometheus/api/v1/alerts
```

Prometheus-compatible rules endpoint to list all active alerts.

For more information, please check out the Prometheus [alerts](https://prometheus.io/docs/prometheus/latest/querying/api/#alerts) documentation.
