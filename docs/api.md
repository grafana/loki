# Loki's HTTP API

Loki exposes an HTTP API for pushing, querying, and tailing log data.
Note that [authenticating](operations/authentication.md) against the API is
out of scope for Loki.

The HTTP API includes the following endpoints:

- [`GET /loki/api/v1/query`](#get-lokiapiv1query)
- [`GET /loki/api/v1/query_range`](#get-lokiapiv1query_range)
- [`GET /loki/api/v1/label`](#get-lokiapiv1label)
- [`GET /loki/api/v1/label/<name>/values`](#get-lokiapiv1labelnamevalues)
- [`GET /loki/api/v1/tail`](#get-lokiapiv1tail)
- [`POST /loki/api/v1/push`](#post-lokiapiv1push)
- [`GET /api/prom/tail`](#get-apipromtail)
- [`GET /api/prom/query`](#get-apipromquery)
- [`POST /api/prom/push`](#post-apiprompush)
- [`GET /ready`](#get-ready)
- [`POST /flush`](#post-flush)
- [`GET /metrics`](#get-metrics)

## Microservices Mode

When deploying Loki in microservices mode, the set of endpoints exposed by each
component is different.

These endpoints are exposed by all components:

- [`GET /ready`](#get-ready)
- [`GET /metrics`](#get-metrics)

These endpoints are exposed by just the querier:

- [`GET /loki/api/v1/query`](#get-lokiapiv1query)
- [`GET /loki/api/v1/query_range`](#get-lokiapiv1query_range)
- [`GET /loki/api/v1/label`](#get-lokiapiv1label)
- [`GET /loki/api/v1/label/<name>/values`](#get-lokiapiv1labelnamevalues)
- [`GET /loki/api/v1/tail`](#get-lokiapiv1tail)
- [`GET /api/prom/tail`](#get-lokiapipromtail)
- [`GET /api/prom/query`](#get-apipromquery)

While these endpoints are exposed by just the distributor:

- [`POST /loki/api/v1/push`](#post-lokiapiv1push)

And these endpoints are exposed by just the ingester:

- [`POST /flush`](#post-flush)

The API endpoints starting with `/loki/` are [Prometheus API-compatible](https://prometheus.io/docs/prometheus/latest/querying/api/) and the result formats can be used interchangeably.

A [list of clients](./clients) can be found in the clients documentation.

## Matrix, Vector, And Streams

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

- `query`: The [LogQL](./logql.md) query to perform
- `limit`: The max number of entries to return
- `time`: The evaluation time for the query as a nanosecond Unix epoch. Defaults to now.
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward.`

In microservices mode, `/loki/api/v1/query` is exposed by the querier.

Response:

```
{
  "status": "success",
  "data": {
    "resultType": "vector" | "streams",
    "result": [<vector value>] | [<stream value>]
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
    <number: nanosecond unix epoch>,
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
          1559848867745737,
          "1267.1266666666666"
        ]
      },
      {
        "metric": {
          "level": "warn"
        },
        "value": [
          1559848867745737,
          "37.77166666666667"
        ]
      },
      {
        "metric": {
          "level": "info"
        },
        "value": [
          1559848867745737,
          "37.69"
        ]
      }
    ]
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
        ]
      }
    ]
  }
}
```

## `GET /loki/api/v1/query_range`

`/loki/api/v1/query_range` is used to do a query over a range of time and
accepts the following query parameters in the URL:

- `query`: The [LogQL](./logql.md) query to perform
- `limit`: The max number of entries to return
- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to one hour ago.
- `end`: The start time for the query as a nanosecond Unix epoch. Defaults to now.
- `step`: Query resolution step width in seconds. Defaults to a dynamic value based on `start` and `end`.
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward.`

Requests against this endpoint require Loki to query the index store in order to
find log streams for particular labels. Because the index store is spread out by
time, the time span covered by `start` and `end`, if large, may cause additional
load against the index server and result in a slow query.

In microservices mode, `/loki/api/v1/query_range` is exposed by the querier.

Response:

```
{
  "status": "success",
  "data": {
    "resultType": "matrix" | "streams",
    "result": [<matrix value>] | [<stream value>]
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
    <number: nanosecond unix epoch>,
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
            1559848958663735,
            "137.95"
          ],
          [
            1559849258663735,
            "467.115"
          ],
          [
            1559849558663735,
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
            1559848958663735,
            "137.27833333333334"
          ],
          [
            1559849258663735,
            "467.69"
          ],
          [
            1559849558663735,
            "660.6933333333334"
          ]
        ]
      }
    ]
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
          {
            "1569266497240578000",
            "foo"
          },
          {
            "1569266492548155000",
            "bar"
          }
        ]
      }
    ]
  }
}
```

## `GET /loki/api/v1/label`

`/loki/api/v1/label` retrieves the list of known labels within a given time span. It
accepts the following query parameters in the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The start time for the query as a nanosecond Unix epoch. Defaults to now.

In microservices mode, `/loki/api/v1/label` is exposed by the querier.

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
$ curl -G -s  "http://localhost:3100/loki/api/v1/label" | jq
{
  "values": [
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
- `end`: The start time for the query as a nanosecond Unix epoch. Defaults to now.

In microservices mode, `/loki/api/v1/label/<name>/values` is exposed by the querier.

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
$ curl -G -s  "http://localhost:3100/loki/api/v1/label/foo/values" | jq
{
  "values": [
    "cat",
    "dog",
    "axolotl"
  ]
}
```

## `GET /loki/api/v1/tail`

`/loki/api/v1/tail` is a WebSocket endpoint that will stream log messages based on
a query. It accepts the following query parameters in the URL:

- `query`: The [LogQL](./logql.md) query to perform
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
behavior is for the POST body to be a snappy-compressed protobuf messsage:

- [Protobuf definition](/pkg/logproto/logproto.proto)
- [Go client library](/pkg/promtail/client/client.go)

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

> **NOTE**: logs sent to Loki for every stream must be in timestamp-ascending
> order, meaning each log line must be more recent than the one last received.
> If logs do not follow this order, Loki will reject the log with an out of
> order error.

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

- `query`: The [LogQL](./logql.md) query to perform
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

- `query`: The [LogQL](./logql.md) query to perform
- `limit`: The max number of entries to return
- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to one hour ago.
- `end`: The start time for the query as a nanosecond Unix epoch. Defaults to now.
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward.`
- `regexp`: a regex to filter the returned results

In microservices mode, `/api/prom/query` is exposed by the querier.

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
  ]
}
```

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
  ]
}
```

### Examples

```bash
$ curl -H "Content-Type: application/json" -XPOST -s "https://localhost:3100/loki/api/v1/push" --data-raw \
  '{"streams": [{ "labels": "{foo=\"bar\"}", "entries": [{ "ts": "2018-12-18T08:28:06.801064-04:00", "line": "fizzbuzz" }] }]}'
```

## `POST /api/prom/push`

> **WARNING**: `/api/prom/push` is DEPRECATED; use `/loki/api/v1/push`
> instead.

`/api/prom/push` is the endpoint used to send log entries to Loki. The default
behavior is for the POST body to be a snappy-compressed protobuf messsage:

- [Protobuf definition](/pkg/logproto/logproto.proto)
- [Go client library](/pkg/promtail/client/client.go)

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

> **NOTE**: logs sent to Loki for every stream must be in timestamp-ascending
> order, meaning each log line must be more recent than the one last received.
> If logs do not follow this order, Loki will reject the log with an out of
> order error.

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

## `GET /metrics`

`/metrics` exposes Prometheus metrics. See
[Observing Loki](operations/observability.md)
for a list of exported metrics.

In microservices mode, the `/metrics` endpoint is exposed by all components.
