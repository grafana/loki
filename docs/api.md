# Loki's HTTP API

Loki exposes an HTTP API for pushing, querying, and tailing log data.
Note that [authenticating](operations/authentication.md) against the API is
out of scope for Loki.

The HTTP API includes the following endpoints:

- [`GET /api/v1/query`](#get-/api/v1/query)
- [`GET /api/v1/query_range`](#get-/api/v1/query_range)
- [`GET /api/v1/label`](#get-/api/v1/label)
- [`GET /api/v1/label/<name>/values`](#get-/api/v1/label/<name>/values)
- [`GET /api/prom/query`](#get-/api/prom/query)
- [`POST /api/prom/push`](#post-/api/prom/push)
- [`GET /api/prom/tail`](#get-/api/prom/tail)
- [`GET /ready`](#get-/ready)
- [`GET /flush`](#get-/flush)
- [`GET /metrics`](#get-/metrics)

[Example clients](#example-clients) can be found at the bottom of this document.

## `GET /api/v1/query`

`/api/v1/query` allows for doing queries against a single point in time. The URL
query parameters support the following values:

- `query`: The [LogQL](./logql.md) query to perform
- `limit`: The max number of entries to return
- `time`: The evaluation time for the query as a nanosecond Unix epoch. Defaults to now.
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward.`

Response:

```json
{
  "resultType": "vector" | "streams",
  "result": [<vector value>] | [<stream value>]
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
  "labels": "<LogQL label key-value pairs>",
  "entries": [
    {
      "ts": "<RFC3339Nano string>",
      "line": "<log line>",
    },
    ...
  ]
}
```

### Examples

```bash
$ curl -G -s  "http://localhost:3100/api/v1/query" --data-urlencode 'query=sum(rate({job="varlogs"}[10m])) by (level)' | jq
{
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
```

```bash
$ curl -G -s  "http://localhost:3100/api/v1/query" --data-urlencode 'query={job="varlogs"}' | jq
{
  "resultType": "streams",
  "result": [
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

## `GET /api/v1/query_range`

`/api/v1/query_range` is used to do a query over a range of time and
accepts the following query parameters in the URL:

- `query`: The [LogQL](./logql.md) query to perform
- `limit`: The max number of entries to return
- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to one hour ago.
- `end`: The start time for the query as a nanosecond Unix epoch. Defaults to now.
- `step`: Query resolution step width in seconds. Defaults to 1.
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward.`

Requests against this endpoint require Loki to query the index store in order to
find log streams for particular labels. Because the index store is spread out by
time, the time span covered by `start` and `end`, if large, may cause additional
load against the index server and result in a slow query.

Response:

```json
{
  "resultType": "matrix" | "streams",
  "result": [<matrix value>] | [<stream value>]
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
  "labels": "<LogQL label key-value pairs>",
  "entries": [
    {
      "ts": "<RFC3339Nano string>",
      "line": "<log line>",
    },
    ...
  ]
}
```

### Examples

```bash
$ curl -G -s  "http://localhost:3100/api/v1/query_range" --data-urlencode 'query=sum(rate({job="varlogs"}[10m])) by (level)' --data-urlencode 'step=300' | jq
{
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
```

```bash
$ curl -G -s  "http://localhost:3100/api/v1/query_range" --data-urlencode 'query={job="varlogs"}' | jq
{
  "resultType": "streams",
  "result": [
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

## `GET /api/v1/label`

`/api/v1/label` retrieves the list of known labels within a given time span. It
accepts the following query parameters in the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The start time for the query as a nanosecond Unix epoch. Defaults to now.

Response:

```json
{
  "values": [
    <label string>,
    ...
  ]
}
```

### Examples

```bash
$ curl -G -s  "http://localhost:3100/api/v1/label" | jq
{
  "values": [
    "foo",
    "bar",
    "baz"
  ]
}
```

## `GET /api/v1/label/<name>/values`

`/api/v1/label/<name>/values` retrieves the list of known values for a given
label within a given time span. It accepts thw following query parameters in
the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The start time for the query as a nanosecond Unix epoch. Defaults to now.

Response:

```json
{
  "values": [
    <label value>,
    ...
  ]
}
```

### Examples

```bash
$ curl -G -s  "http://localhost:3100/api/v1/label/foo/values" | jq
{
  "values": [
    "cat",
    "dog",
    "axolotl"
  ]
}
```

## `GET /api/prom/query`

`/api/prom/query` supports doing general queries. The URL query parameters
support the following values:

- `query`: The [LogQL](./logql.md) query to perform
- `limit`: The max number of entries to return
- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to one hour ago.
- `end`: The start time for the query as a nanosecond Unix epoch. Defaults to now.
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward.`
- `regexp`: a regex to filter the returned results

Note that the larger the time span between `start` and `end` will cause
additional load on Loki and the index store, resulting in slower queries.

`/api/prom/query` is DEPRECATED and `/api/v1/query_range` should be used
instead.

Response:

```json
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

## `POST /api/prom/push`

`/api/prom/push` is how log entries are sent to Loki. The deafult behavior is
for the POST body to be a snappy-compress protobuf messsage:

- [Protobuf definition](/pkg/logproto/logproto.proto)
- [Golang client library](/pkg/promtail/client/client.go)

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

### Examples


```bash
$ curl -H "Content-Type: application/json" -XPOST -s "https://localhost:3100/api/prom/push" --data-raw \
  '{"streams": [{ "labels": "{foo=\"bar\"}", "entries": [{ "ts": "2018-12-18T08:28:06.801064-04:00", "line": "fizzbuzz" }] }]}'
```

## `GET /api/prom/tail`

`/api/prom/tail` is a websocket endpoint that will stream log messsages based on
a query. It accepts the following query parameters in the URL:

- `query`: The [LogQL](./logql.md) query to perform
- `delay_for`: The number of seconds to delay retrieving logs to let slow
    loggers catch up. Defaults to 0 and cannot be larger than 5.
- `limit`: The max number of entries to return
- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to one hour ago.

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
will be sent over the websocket multiple times.

## `GET /ready`

`/ready` returns HTTP 200 when the Loki ingester is ready to accept traffic. If
running Loki on Kubernetes, `/ready` can be used as a readiness probe.

## `GET /flush`

`/flush` triggers a flush of all in-memory chunks held by the ingesters to the
backing store. Mainly used for local testing.

## `GET /metrics`

`/metrics` exposes Prometheus metrics. See
[Observing Loki](operations/observability.md)
for a list of exported metrics.

## Example Clients

Please note that the Loki API is not stable yet and breaking changes may occur
when using or writing a third-party client.

- [promtail-client](https://github.com/afiskon/promtail-client) (Go)
- [push-to-loki.py](https://github.com/sleleko/devops-kb/blob/master/python/push-to-loki.py) (Python 3)
