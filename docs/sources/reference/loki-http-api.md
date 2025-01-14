---
title: Loki HTTP API
description: Provides a reference page for the Loki HTTP API endpoints for data ingestion, data retrieval, and cluster management.
aliases:
  - ../api/ # /docs/loki/latest/api/
  - ./api/ # /docs/loki/latest/reference/api/
weight: 500
---

# Loki HTTP API

Loki exposes an HTTP API for pushing, querying, and tailing log data, as well
as for viewing and managing cluster information.

{{< admonition type="note" >}}
Note that authorization is not part of the Loki API.
Authorization needs to be done separately, for example, using an open-source load-balancer such as NGINX.
{{< /admonition >}}

## Endpoints

### Ingest endpoints

These endpoints are exposed by the `distributor`, `write`, and `all` components:

- [`POST /loki/api/v1/push`](#ingest-logs)
- [`POST /otlp/v1/logs`](#ingest-logs-using-otlp)

A [list of clients]({{< relref "../send-data" >}}) can be found in the clients documentation.

### Query endpoints

{{< admonition type="note" >}}
Requests sent to the query endpoints must use valid LogQL syntax. For more information, see the [LogQL]({{< relref "../query" >}}) section of the documentation.
{{< /admonition >}}

These HTTP endpoints are exposed by the `querier`, `query-frontend`, `read`, and `all` components:

- [`GET /loki/api/v1/query`](#query-logs-at-a-single-point-in-time)
- [`GET /loki/api/v1/query_range`](#query-logs-within-a-range-of-time)
- [`GET /loki/api/v1/labels`](#query-labels)
- [`GET /loki/api/v1/label/<name>/values`](#query-label-values)
- [`GET /loki/api/v1/series`](#query-streams)
- [`GET /loki/api/v1/index/stats`](#query-log-statistics)
- [`GET /loki/api/v1/index/volume`](#query-log-volume)
- [`GET /loki/api/v1/index/volume_range`](#query-log-volume)
- [`GET /loki/api/v1/patterns`](#patterns-detection)
- [`GET /loki/api/v1/tail`](#stream-logs)

### Status endpoints

These HTTP endpoints are exposed by all components and return the status of the component:

- [`GET /ready`](#readiness-probe)
- [`GET /log_level`](#change-log-level)
- [`GET /metrics`](#prometheus-metrics)
- [`GET /config`](#show-current-configuration)
- [`GET /services`](#list-running-services)
- [`GET /loki/api/v1/status/buildinfo`](#show-build-information)

### Ring endpoints

These HTTP endpoints are exposed by their respective component that is part of the ring URL prefix:

- [`GET /distributor/ring`](#distributor-ring-status)
- [`GET /indexgateway/ring`](#index-gateway-ring-status)
- [`GET /ruler/ring`](#ruler-ring-status)
- [`GET /compactor/ring`](#compactor-ring-status)

### Flush/shutdown endpoints

These HTTP endpoints are exposed by the `ingester`, `write`, and `all` components for flushing chunks and/or shutting down.

- [`POST /flush`](#flush-in-memory-chunks-to-backing-store)
- [`POST /ingester/prepare_shutdown`](#prepare-ingester-shutdown)
- [`POST /ingester/shutdown`](#flush-in-memory-chunks-and-shut-down)

### Rule endpoints

These HTTP endpoints are exposed by the `ruler` component:

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

API endpoints starting with `/api/prom` are [Prometheus API-compatible](https://prometheus.io/docs/prometheus/latest/querying/api/) and the result formats can be used interchangeably.

### Log deletion endpoints

These endpoints are exposed by the `compactor`, `backend`, and `all` components:

- [`POST /loki/api/v1/delete`](#request-log-deletion)
- [`GET /loki/api/v1/delete`](#list-log-deletion-requests)
- [`DELETE /loki/api/v1/delete`](#request-cancellation-of-a-delete-request)

### Other endpoints

These HTTP endpoints are exposed by all individual components:

- [`GET /loki/api/v1/format_query`](#format-a-logql-query)

### Deprecated endpoints

{{< admonition type="note" >}}
The following endpoints are deprecated.While they still exist and work, they should not be used for new deployments.
Existing deployments should upgrade to use the supported endpoints.
{{< /admonition >}}

| Deprecated | Replacement |
| ---------- | ----------- |
| `POST /api/prom/push` | [`POST /loki/api/v1/push`](#ingest-logs) |
| `GET /api/prom/tail` | [`GET /loki/api/v1/tail`](#stream-logs) |
| `GET /api/prom/query` | [`GET /loki/api/v1/query`](#query-logs-at-a-single-point-in-time) |
| `GET /api/prom/label` | [`GET /loki/api/v1/labels`](#query-labels) |
| `GET /api/prom/label/<name>/values` | [`GET /loki/api/v1/label/<name>/values`](#query-label-values) |
| `GET /api/prom/series` | [`GET /loki/api/v1/series`](#query-streams) |

## Format

### Matrix, vector, and stream

Some Loki API endpoints return a result of a matrix, a vector, or a stream:

- **Matrix**: a table of values where each row represents a different label set
and the columns are each sample values for that row over the queried time.
Matrix types are only returned when running a query that computes some value.

- **Instant Vector**: denoted in the type as just `vector`, an Instant Vector
represents the latest value of a calculation for a given labelset. Instant
Vectors are only returned when doing a query against a single point in
time.

- **Stream**: a Stream is a set of all values (logs) for a given label set over the
queried time range. Streams are the only type that will result in log lines
being returned.

### Timestamps

The API accepts several formats for timestamps:

- All epoch values will be interpreted as a Unix timestamp in nanoseconds.
- A floating point number is a Unix timestamp with fractions of a second.
- A string in `RFC3339` and `RFC3339Nano` format, as supported by Go's [time](https://pkg.go.dev/time) package.

{{< admonition type="note" >}}
When using `/api/v1/push`, you must send the timestamp as a string and not a number, otherwise the endpoint will return a 400 error.
{{< /admonition >}}

### Statistics

Query endpoints such as `/loki/api/v1/query` and `/loki/api/v1/query_range` return a set of statistics about the query execution. Those statistics allow users to understand the amount of data processed and at which speed.

The example below show all possible statistics returned with their respective description.

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

## Ingest logs

```bash
POST /loki/api/v1/push
```

`/loki/api/v1/push` is the endpoint used to send log entries to Loki. The default
behavior is for the POST body to be a [Snappy](https://github.com/google/snappy)-compressed [Protocol Buffer](https://github.com/protocolbuffers/protobuf) message:

- [Protocol Buffer definition](https://github.com/grafana/loki/blob/main/pkg/logproto/logproto.proto)
- [Go client library](https://github.com/grafana/loki/blob/main/clients/pkg/promtail/client/client.go)

These POST requests require the `Content-Type` HTTP header to be `application/x-protobuf`.

Alternatively, if the `Content-Type` header is set to `application/json`, a JSON post body can be sent in the following format:

```json
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

You can optionally attach [structured metadata]({{< relref "../get-started/labels/structured-metadata" >}}) to each log line by adding a JSON object to the end of the log line array.
The JSON object must be a valid JSON object with string keys and string values. The JSON object should not contain any nested object.
The JSON object must be set immediately after the log line. Here is an example of a log entry with some structured metadata attached:

```json
"values": [
    [ "<unix epoch in nanoseconds>", "<log line>", {"trace_id": "0242ac120002", "user_id": "superUser123"}]
]
```

In microservices mode, `/loki/api/v1/push` is exposed by the distributor.

If [`block_ingestion_until`](/docs/loki/<LOKI_VERSION>/configuration/#limits_config) is configured and push requests are blocked, the endpoint will return the status code configured in `block_ingestion_status_code` (`260` by default)
along with an error message. If the configured status code is `200`, no error message will be returned.

### Examples

The following cURL command pushes a stream with the label "foo=bar2" and a single log line "fizzbuzz" using JSON encoding:

```bash
curl -H "Content-Type: application/json" \
  -s -X POST "http://localhost:3100/loki/api/v1/push" \
  --data-raw '{"streams": [{ "stream": { "foo": "bar2" }, "values": [ [ "1570818238000000000", "fizzbuzz" ] ] }]}'
```

## Ingest logs using OTLP

```bash
POST /otlp/v1/logs
```

`/otlp/v1/logs` lets the OpenTelemetry Collector send logs to Loki using `otlphttp` protocol.

For information on how to configure Loki, refer to the [OTel Collector topic](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/otel/).

<!-- vale Google.Will = NO -->
{{< admonition type="note" >}}
When configuring the OpenTelemetry Collector, you must use `endpoint: http://<loki-addr>:3100/otlp`, as the collector automatically completes the endpoint.  Entering the full endpoint will generate an error.
{{< /admonition >}}
<!-- vale Google.Will = YES -->

## Query logs at a single point in time

```bash
GET /loki/api/v1/query
```

`/loki/api/v1/query` allows for doing queries against a single point in time.
This type of query is often referred to as an instant query. Instant queries are only used for metric type LogQL queries
and will return a 400 (Bad Request) in case a log type query is provided.
The endpoint accepts the following query parameters in the URL:

- `query`: The [LogQL]({{< relref "../query" >}}) query to perform. Requests that do not use valid LogQL syntax will return errors.
- `limit`: The max number of entries to return. It defaults to `100`. Only applies to query types which produce a stream (log lines) response.
- `time`: The evaluation time for the query as a nanosecond Unix epoch or another [supported format](#timestamps). Defaults to now.
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward`.

In microservices mode, `/loki/api/v1/query` is exposed by the querier and the query frontend.

Response format:

```json
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

```json
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

```json
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

Parquet can be request as a response format by setting the `Accept` header to `application/vnd.apache.parquet`.

The schema is the following for streams:

| column_name |       column_type        |
|-------------|--------------------------|
| timestamp   | TIMESTAMP WITH TIME ZONE |
| labels      | MAP(VARCHAR, VARCHAR)    |
| line        |VARCHAR                   |

and for metrics:

| column_name |       column_type        |
|-------------|--------------------------|
| timestamp   | TIMESTAMP WITH TIME ZONE |
| labels      | MAP(VARCHAR, VARCHAR)    |
| value       | DOUBLE                   |

See [statistics](#statistics) for information about the statistics returned by Loki.

### Examples

This example cURL command

```bash
curl -G -s  "http://localhost:3100/loki/api/v1/query" \
  --data-urlencode 'query=sum(rate({job="varlogs"}[10m])) by (level)' | jq
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
  --data-urlencode 'query=sum(rate({job="varlogs"}[10m])) by (level)' | jq
```

To query against the three tenants `Tenant1`, `Tenant2`, and `Tenant3`,
specify the tenant names separated by the pipe (`|`) character:

```bash
curl -H 'X-Scope-OrgID:Tenant1|Tenant2|Tenant3' \
  -G -s "http://localhost:3100/loki/api/v1/query" \
  --data-urlencode 'query=sum(rate({job="varlogs"}[10m])) by (level)' | jq
```

The same example query for Grafana Enterprise Logs
uses Basic Authentication and specifies the tenant names as a `user`.
The tenant names are separated by the pipe (`|`) character.
The password in this example is an access policy token that has been
defined in the `API_TOKEN` environment variable:

```bash
curl -u "Tenant1|Tenant2|Tenant3:$API_TOKEN" \
  -G -s "http://localhost:3100/loki/api/v1/query" \
  --data-urlencode 'query=sum(rate({job="varlogs"}[10m])) by (level)' | jq
```

To query against your hosted log tenant in Grafana Cloud, use the **User** and **URL** values provided in the Loki logging service details of your Grafana Cloud stack. You can find this information in the [Cloud Portal](https://grafana.com/docs/grafana-cloud/account-management/cloud-portal/#your-grafana-cloud-stack). Use an access policy token in your queries for authentication. The password in this example is an access policy token that has been defined in the `API_TOKEN` environment variable:

```bash
curl -u "User:$API_TOKEN" \
  -G -s "<URL-PROVIDED-IN-LOKI-DATA-SOURCE-SETTINGS>/loki/api/v1/query" \
  --data-urlencode 'query=sum(rate({job="varlogs"}[10m])) by (level)' | jq
```

## Query logs within a range of time

```bash
GET /loki/api/v1/query_range
```

`/loki/api/v1/query_range` is used to do a query over a range of time.
This type of query is often referred to as a range query. Range queries are used for both log and metric type LogQL queries.
It accepts the following query parameters in the URL:

- `query`: The [LogQL]({{< relref "../query" >}}) query to perform.
- `limit`: The max number of entries to return. It defaults to `100`. Only applies to query types which produce a stream (log lines) response.
- `start`: The start time for the query as a nanosecond Unix epoch or another [supported format](#timestamps). Defaults to one hour ago. Loki returns results with timestamp greater or equal to this value.
- `end`: The end time for the query as a nanosecond Unix epoch or another [supported format](#timestamps). Defaults to now. Loki returns results with timestamp lower than this value.
- `since`: A `duration` used to calculate `start` relative to `end`. If `end` is in the future, `start` is calculated as this duration before now. Any value specified for `start` supersedes this parameter.
- `step`: Query resolution step width in `duration` format or float number of seconds. `duration` refers to Prometheus duration strings of the form `[0-9]+[smhdwy]`. For example, 5m refers to a duration of 5 minutes. Defaults to a dynamic value based on `start` and `end`. Only applies to query types which produce a matrix response.
- `interval`: Only return entries at (or greater than) the specified interval, can be a `duration` format or float number of seconds. Only applies to queries which produce a stream response. Not to be confused with `step`, see the explanation under [Step versus interval](#step-versus-interval).
- `direction`: Determines the sort order of logs. Supported values are `forward` or `backward`. Defaults to `backward.`

In microservices mode, `/loki/api/v1/query_range` is exposed by the querier and the query frontend.

### Step versus interval

Use the `step` parameter when making metric queries to Loki, or queries which return a matrix response. It is evaluated in exactly the same way Prometheus evaluates `step`. First the query will be evaluated at `start` and then evaluated again at `start + step` and again at `start + step + step` until `end` is reached. The result will be a matrix of the query result evaluated at each step.

Use the `interval` parameter when making log queries to Loki, or queries which return a stream response. It is evaluated by returning a log entry at `start`, then the next entry will be returned an entry with timestampe >= `start + interval`, and again at `start + interval + interval` and so on until `end` is reached. It does not fill missing entries.

Response format:

```json
{
  "status": "success",
  "data": {
    "resultType": "matrix" | "streams",
    "result": [<matrix value>] | [<stream value>]
    "stats" : [<statistics>]
  }
}
```

where `<matrix value>` is:

```json
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

```json
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

Parquet can be request as a response format by setting the `Accept` header to `application/vnd.apache.parquet`.

The schema is the following for streams:

| column_name |       column_type        |
|-------------|--------------------------|
| timestamp   | TIMESTAMP WITH TIME ZONE |
| labels      | MAP(VARCHAR, VARCHAR)    |
| line        |VARCHAR                   |

and for metrics:

| column_name |       column_type        |
|-------------|--------------------------|
| timestamp   | TIMESTAMP WITH TIME ZONE |
| labels      | MAP(VARCHAR, VARCHAR)    |
| value       | DOUBLE                   |

See [statistics](#statistics) for information about the statistics returned by Loki.

### Examples

This example cURL command

```bash
curl -G -s  "http://localhost:3100/loki/api/v1/query_range" \
  --data-urlencode 'query=sum(rate({job="varlogs"}[10m])) by (level)' \
  --data-urlencode 'step=300' | jq
```

gave this response:

```json
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

This example cURL command

```bash
curl -G -s "http://localhost:3100/loki/api/v1/query_range" \
  --data-urlencode 'query={job="varlogs"}' | jq
```

gave this response:

```json
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

## Query labels

```bash
GET /loki/api/v1/labels
```

`/loki/api/v1/labels` retrieves the list of known labels within a given time span.
Loki may use a larger time span than the one specified.
It accepts the following query parameters in the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.
- `since`: A `duration` used to calculate `start` relative to `end`. If `end` is in the future, `start` is calculated as this duration before now. Any value specified for `start` supersedes this parameter.
- `query`: Log stream selector that selects the streams to match and return label names. Example: `{app="myapp", environment="dev"}`

In microservices mode, `/loki/api/v1/labels` is exposed by the querier.

Response format:

```bash
{
  "status": "success",
  "data": [
    <label string>,
    ...
  ]
}
```

### Examples

This example cURL command

```bash
curl -G -s  "http://localhost:3100/loki/api/v1/labels" | jq
```

gave this response:

```json
{
  "status": "success",
  "data": [
    "foo",
    "bar",
    "baz"
  ]
}
```

## Query label values

```bash
GET /loki/api/v1/label/<name>/values
```

`/loki/api/v1/label/<name>/values` retrieves the list of known values for a given
label within a given time span. Loki may use a larger time span than the one specified.
It accepts the following query parameters in the URL:

- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to 6 hours ago.
- `end`: The end time for the query as a nanosecond Unix epoch. Defaults to now.
- `since`: A `duration` used to calculate `start` relative to `end`. If `end` is in the future, `start` is calculated as this duration before now. Any value specified for `start` supersedes this parameter.
- `query`: Log stream selector that selects the streams to match and return label values for `<name>`. Example: `{app="myapp", environment="dev"}`

In microservices mode, `/loki/api/v1/label/<name>/values` is exposed by the querier.

Response format:

```json
{
  "status": "success",
  "data": [
    <label value>,
    ...
  ]
}
```

### Examples

This example cURL command

```bash
curl -G -s  "http://localhost:3100/loki/api/v1/label/foo/values" | jq
```

gave this response:

```json
{
  "status": "success",
  "data": [
    "cat",
    "dog",
    "axolotl"
  ]
}
```

## Query streams

The Series API is available under the following:

- `GET /loki/api/v1/series`
- `POST /loki/api/v1/series`

This endpoint returns the list of streams (unique set of labels) that match a certain given selector.

URL query parameters:

- `match[]=<selector>`: Repeated log stream selector argument that selects the streams to return. At least one `match[]` argument must be provided.
- `start=<nanosecond Unix epoch>`: Start timestamp.
- `end=<nanosecond Unix epoch>`: End timestamp.
- `since`: A `duration` used to calculate `start` relative to `end`. If `end` is in the future, `start` is calculated as this duration before now. Any value specified for `start` supersedes this parameter.

You can URL-encode these parameters directly in the request body by using the POST method and `Content-Type: application/x-www-form-urlencoded` header. This is useful when specifying a large or dynamic number of stream selectors that may breach server-side URL character limits.

In microservices mode, these endpoints are exposed by the querier.

### Examples

This example cURL command

```bash
curl -s "http://localhost:3100/loki/api/v1/series" \
  --data-urlencode 'match[]={container_name=~"prometheus.*", component="server"}' \
  --data-urlencode 'match[]={app="loki"}' | jq '.'
```

gave this response:

```json
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

## Query log statistics

```bash
GET /loki/api/v1/index/stats
```

The `/loki/api/v1/index/stats` endpoint can be used to query the index for the number of `streams`, `chunks`, `entries`, and `bytes` that a query resolves to.

URL query parameters:

- `query`: The [LogQL]({{< relref "../query" >}}) matchers to check (that is, `{job="foo", env!="dev"}`)
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

- It does not include data from the ingesters.
- It is a probabilistic technique.
- Streams/chunks which span multiple period configurations may be counted twice.

These make it generally more helpful for larger queries.
It can be used for better understanding the throughput requirements and data topology for a list of matchers over a period of time.

## Query log volume

```bash
GET /loki/api/v1/index/volume
GET /loki/api/v1/index/volume_range
```

{{< admonition type="note" >}}
You must configure `volume_enabled: true` to enable this feature.
{{< /admonition >}}

The `/loki/api/v1/index/volume` and `/loki/api/v1/index/volume_range` endpoints can be used to query the index for volume information about label and label-value combinations. This is helpful in exploring the logs Loki has ingested to find high or low volume streams. The `volume` endpoint returns results for a single point in time, the time the query was processed. Each datapoint represents an aggregation of the matching label or series over the requested time period, returned in a Prometheus style vector response. The `volume_range` endoint returns a series of datapoints over a range of time, in Prometheus style matrix response, for each matching set of labels or series. The number of timestamps returned when querying `volume_range` will be determined by the provided `step` parameter and the requested time range.

The `query` should be a valid LogQL stream selector, for example `{job="foo", env=~".+"}`. By default, these endpoints will aggregate into series consisting of all matches for labels included in the query. For example, assuming you have the streams `{job="foo", env="prod", team="alpha"}`, `{job="bar", env="prod", team="beta"}`, `{job="foo", env="dev", team="alpha"}`, and `{job="bar", env="dev", team="beta"}` in your system. The query `{job="foo", env=~".+"}` would return the two metric series `{job="foo", env="dev"}` and `{job="foo", env="prod"}`, each with datapoints representing the accumulate values of chunks for the streams matching that selector, which in this case would be the streams `{job="foo", env="dev", team="alpha"}` and `{job="foo", env="prod", team="alpha"}`, respectively.

There are two parameters which can affect the aggregation strategy. First, a comma-separated list of `targetLabels` can be provided, allowing volumes to be aggregated by the speficied `targetLabels` only. This is useful for negations. For example, if you said `{team="alpha", env!="dev"}`, the default behavior would include `env` in the aggregation set. However, maybe you're looking for all non-dev jobs for team alpha, and you don't care which env those are in (other than caring that they're not dev jobs). To achieve this, you could specify `targetLabels=team,job`, resulting in a single metric series (in this case) of `{team="alpha", job="foo}`.

The other way to change aggregations is with the `aggregateBy` parameter. The default value for this is `series`, which aggregates into combinations of matching key-value pairs. Alternately this can be specified as `labels`, which will aggregate into labels only. In this case, the response will have a metric series with a label name matching each label, and a label value of `""`. This is useful for exploring logs at a high level. For example, if you wanted to know what percentage of your logs had a `team` label, you could query your logs with `aggregateBy=labels` and a query with either an exact or regex match on `team`, or by including `team` in the list of `targetLabels`.

URL query parameters:

- `query`: The [LogQL]({{< relref "../query" >}}) matchers to check (that is, `{job="foo", env=~".+"}`). This parameter is required.
- `start=<nanosecond Unix epoch>`: Start timestamp. This parameter is required.
- `end=<nanosecond Unix epoch>`: End timestamp. This parameter is required.
- `limit`: How many metric series to return. The parameter is optional, the default is `100`.
- `step`: Query resolution step width in `duration` format or float number of seconds. `duration` refers to Prometheus duration strings of the form `[0-9]+[smhdwy]`. For example, 5m refers to a duration of 5 minutes. Defaults to a dynamic value based on `start` and `end`. Only applies when querying the `volume_range` endpoint, which will always return a Prometheus style matrix response. This parameter is optional, and only applicable for `query_range`. The default step configured for range queries will be used when not provided.
- `targetLabels`: A comma separated list of labels to aggregate into. This parameter is optional. When not provided, volumes will be aggregated into the matching labels or label-value pairs.
- `aggregateBy`: Whether to aggregate into labels or label-value pairs. This parameter is optional, the default is label-value pairs.

You can URL-encode these parameters directly in the request body by using the POST method and `Content-Type: application/x-www-form-urlencoded` header. This is useful when specifying a large or dynamic number of stream selectors that may breach server-side URL character limits.

## Patterns detection

```bash
GET /loki/api/v1/patterns
```

{{< admonition type="note" >}}
You must configure

```yaml
pattern_ingester:
  enabled: true
```

to enable this feature.
{{< /admonition >}}

The `/loki/api/v1/patterns` endpoint can be used to query loki for patterns detected in the logs. This helps understand the structure of the logs Loki has ingested.

The `query` should be a valid LogQL stream selector, for example `{job="foo", env=~".+"}`. The result is aggregated by the `pattern` from all matching streams.

For each pattern detected, the response includes the pattern itself and the number of samples for each pattern at each timestamp.

For example, if you have the following logs:

```log
ts=2024-03-30T23:03:40 caller=grpc_logging.go:66 level=info method=/cortex.Ingester/Push duration=200ms msg=gRPC
ts=2024-03-30T23:03:41 caller=grpc_logging.go:66 level=info method=/cortex.Ingester/Push duration=500ms msg=gRPC
```

The pattern detected might be:

```log
ts=<_> caller=grpc_logging.go:66 level=info method=/cortex.Ingester/Push duration=<_> msg=gRPC
```

URL query parameters:

- `query`: The [LogQL]({{< relref "../query" >}}) matchers to check (that is, `{job="foo", env=~".+"}`). This parameter is required.
- `start=<nanosecond Unix epoch>`: Start timestamp. This parameter is required.
- `end=<nanosecond Unix epoch>`: End timestamp. This parameter is required.
- `step=<duration string or float number of seconds>`: Step between samples for occurrences of this pattern. This parameter is optional.

### Examples

This example cURL command

```bash
curl -s "http://localhost:3100/loki/api/v1/patterns" \
  --data-urlencode 'query={app="loki"}' | jq
```

gave this response:

```json
{
  "status": "success",
  "data": [
    {
      "pattern": "<_> caller=grpc_logging.go:66 <_> level=error method=/cortex.Ingester/Push <_> msg=gRPC err=\"connection refused to object store\"",
      "samples": [
        [
          1711839260,
          1
        ],
        [
          1711839270,
          2
        ],
        [
          1711839280,
          1
        ]
      ]
    },
    {
      "pattern": "<_> caller=grpc_logging.go:66 <_> level=info method=/cortex.Ingester/Push <_> msg=gRPC",
      "samples": [
        [
          1711839260,
          105
        ],
        [
          1711839270,
          222
        ],
        [
          1711839280,
          196
        ]
      ]
    }
  ]
}
```

The result is a list of patterns detected in the logs, with the number of samples for each pattern at each timestamp.
The pattern format is the same as the [LogQL]({{< relref "../query" >}}) pattern filter and parser and can be used in queries for filtering matching logs.
Each sample is a tuple of timestamp (second) and count.

## Stream logs

```bash
GET /loki/api/v1/tail
```

`/loki/api/v1/tail` is a WebSocket endpoint that streams log messages based on a query to the client.
It accepts the following query parameters in the URL:

- `query`: The [LogQL]({{< relref "../query" >}}) query to perform.
- `delay_for`: The number of seconds to delay retrieving logs to let slow
  loggers catch up. Defaults to 0 and cannot be larger than 5.
- `limit`: The max number of entries to return. It defaults to `100`.
- `start`: The start time for the query as a nanosecond Unix epoch. Defaults to one hour ago.

In microservices mode, `/loki/api/v1/tail` is exposed by the querier.

Response format (streamed):

```json
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

## Readiness probe

```bash
GET /ready
```

`/ready` returns HTTP 200 when the Loki instance is ready to accept traffic. If
running Loki on Kubernetes, `/ready` can be used as a readiness probe.

In microservices mode, the `/ready` endpoint is exposed by all components.

## Change log level

```bash
GET /log_level
POST /log_level
```

`/log_level` a `GET` returns the current log level and a `POST` lets you change the log level of a Loki process **at runtime**.
This can be useful for accessing debugging information during an incident. Caution should be used when running at the `debug` log level, as this produces a large volume of data.

The endpoint accepts the following query parameters in the URL:

- `log_level`: A valid log level that can be passed as a URL param (`?log_level=<level>`) or as a form value in case of `POST`. Valid levels: [debug, info, warn, error]

In microservices mode, the `/log_level` endpoint is exposed by all components.

## Prometheus metrics

```bash
GET /metrics
```

`/metrics` returns exposed Prometheus metrics. See
[Observing Loki]({{< relref "../operations/meta-monitoring" >}})
for a list of exported metrics.

In microservices mode, the `/metrics` endpoint is exposed by all components.

## Show current configuration

```bash
GET /config
```

`/config` exposes the current configuration. The optional `mode` query parameter can be used to
modify the output. If it has the value `diffs` only the differences between the default configuration
and the current are returned. A value of `defaults` returns the default configuration.

In microservices mode, the `/config` endpoint is exposed by all components.

## List running services

```bash
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

## Show build information

```bash
GET /loki/api/v1/status/buildinfo
```

`/loki/api/v1/status/buildinfo` exposes the build information in a JSON object. The fields are `version`, `revision`, `branch`, `buildDate`, `buildUser`, and `goVersion`.

## Flush in-memory chunks to backing store

```bash
POST /flush
```

`/flush` triggers a flush of all in-memory chunks held by the ingesters to the
backing store. Mainly used for local testing.

In microservices mode, the `/flush` endpoint is exposed by the ingester.

## Prepare ingester shutdown

```bash
GET, POST, DELETE /ingester/prepare_shutdown
```

This endpoint is used to tell the ingester to release all resources on receiving the next `SIGTERM` or `SIGINT` signal.

A `POST` request to the `/ingester/prepare_shutdown` endpoint configures the ingester for a full shutdown and returns immediately.
Only when the ingester process is stopped with `SIGINT` or `SIGTERM`, it will unregister from the ring, and in-memory data will be flushed to long-term storage.
This endpoint supersedes any YAML configurations and isn't necessary if the ingester is already configured to unregister from the ring or to flush on shutdown.

A `GET` request to the `/ingester/prepare_shutdown` endpoint returns the status of this configuration, either `set` or `unset`.

A `DELETE` request to the `/ingester/prepare_shutdown` endpoint reverts the configuration of the ingester to its previous state
(with respect to unregistering on shutdown and flushing of in-memory data to long-term storage).

This API endpoint is usually used by Kubernetes-specific scale down automations such as the
[rollout-operator](https://github.com/grafana/rollout-operator).

## Flush in-memory chunks and shut down

```bash
GET, POST /ingester/shutdown
```

`/ingester/shutdown` triggers a shutdown of the ingester and notably will _always_ flush any in memory chunks it holds.
This is helpful for scaling down WAL-enabled ingesters where we want to ensure old WAL directories are not orphaned,
but instead flushed to our chunk backend.

It accepts three URL query parameters `flush`, `delete_ring_tokens`, and `terminate`.

**URL query parameters:**

- `flush=<bool>`:
  Flag to control whether to flush any in-memory chunks the ingester holds. Defaults to `true`.
- `delete_ring_tokens=<bool>`:
  Flag to control whether to delete the file that contains the ingester ring tokens of the instance if the `-ingester.token-file-path` is specified. Defaults to `false`.
- `terminate=<bool>`:
  Flag to control whether to terminate the Loki process after service shutdown. Defaults to `true`.

This handler, in contrast to the deprecated `/ingester/flush_shutdown` handler, terminates the Loki process by default.
This behaviour can be changed by setting the `terminate` query parameter to `false`.

In microservices mode, the `/ingester/shutdown` endpoint is exposed by the ingester.

## Distributor ring status

```bash
GET /distributor/ring
```

Displays a web page with the distributor hash ring status, including the state, health, and last heartbeat time of each distributor.

## Index gateway ring status

```bash
GET /indexgateway/ring
```

Displays a web page with the index gateway hash ring status, including the state, health, and last heartbeat time of each index gateway.

## Ruler

The ruler API endpoints require to configure a backend object storage to store the recording rules and alerts. The ruler API uses the concept of a "namespace" when creating rule groups. This is a stand-in for the name of the rule file in Prometheus. Rule groups must be named uniquely within a namespace.

{{< admonition type="note" >}}
You must configure `enable_api: true` to enable this feature.
{{< /admonition >}}

### Ruler ring status

```bash
GET /ruler/ring
```

Displays a web page with the ruler hash ring status, including the state, health, and last heartbeat time of each ruler.

### List rule groups

```bash
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

```bash
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

```bash
GET /loki/api/v1/rules/{namespace}/{groupName}
```

Returns the rule group matching the request namespace and group name.

### Set rule group

```bash
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

```bash
DELETE /loki/api/v1/rules/{namespace}/{groupName}

```

Deletes a rule group by namespace and group name. This endpoints returns `202` on success.

### Delete namespace

```bash
DELETE /loki/api/v1/rules/{namespace}
```

Deletes all the rule groups in a namespace (including the namespace itself). This endpoint returns `202` on success.

### List rules

```bash
GET /prometheus/api/v1/rules?type={alert|record}&file={}&rule_group={}&rule_name={}
```

Prometheus-compatible rules endpoint to list alerting and recording rules that are currently loaded.

The `type` parameter is optional. If set, only the specified type of rule is returned.

The `file`, `rule_group` and `rule_name` parameters are optional, and can accept multiple values. If set, the response content is filtered accordingly.

For more information, refer to the [Prometheus rules](https://prometheus.io/docs/prometheus/latest/querying/api/#rules) documentation.

### List alerts

```bash
GET /prometheus/api/v1/alerts
```

Prometheus-compatible rules endpoint to list all active alerts.

For more information, refer to the Prometheus [alerts](https://prometheus.io/docs/prometheus/latest/querying/api/#alerts) documentation.

## Compactor

### Compactor ring status

```bash
GET /compactor/ring
```

Displays a web page with the compactor hash ring status, including the state, health, and last heartbeat time of each compactor.

### Request log deletion

```bash
POST /loki/api/v1/delete
PUT /loki/api/v1/delete
```

Create a new delete request for the authenticated tenant.
The [log entry deletion]({{< relref "../operations/storage/logs-deletion" >}}) documentation has configuration details.

Log entry deletion is supported _only_ when TSDB or BoltDB Shipper is configured for the index store.

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

```bash
GET /loki/api/v1/delete
```

List the existing delete requests for the authenticated tenant.
The [log entry deletion]({{< relref "../operations/storage/logs-deletion" >}}) documentation has configuration details.

Log entry deletion is supported _only_ when TSDB or BoltDB Shipper is configured for the index store.

List the existing delete requests using the following API:

```bash
GET /loki/api/v1/delete
```

This endpoint returns both processed and unprocessed deletion requests. It does not list canceled requests, as those requests will have been removed from storage.

#### Examples

Example cURL command:

```bash
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

```bash
DELETE /loki/api/v1/delete
```

Remove a delete request for the authenticated tenant.
The [log entry deletion]({{< relref "../operations/storage/logs-deletion" >}}) documentation has configuration details.

Loki allows cancellation of delete requests until the requests are picked up for processing. It is controlled by the `delete_request_cancel_period` YAML configuration or the equivalent command line option when invoking Loki. To cancel a delete request that has been picked up for processing or is partially complete, pass the `force=true` query parameter to the API.

Log entry deletion is supported _only_ when TSDB or BoltDB Shipper is configured for the index store.

Cancel a delete request using this compactor endpoint:

```bash
DELETE /loki/api/v1/delete
```

Query parameters:

- `request_id=<request_id>`: Identifies the delete request to cancel; IDs are found using the `delete` endpoint.
- `force=<boolean>`: When the `force` query parameter is true, partially completed delete requests will be canceled.
  {{< admonition type="note" >}}
  some data from the request may still be deleted and the deleted request will be listed as 'processed'.
  {{< /admonition >}}

A 204 response indicates success.

#### Examples

Example cURL command:

```bash
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

## Format a LogQL query

```bash
GET /loki/api/v1/format_query
POST /loki/api/v1/format_query
```

The endpoint accepts the following query parameters in the URL:

- `query`: A LogQL query string. Can be passed as URL param (`?query=<query>`) in case of both `GET` and `POST`. Or as form value in case of `POST`.

The `/loki/api/v1/format_query` endpoint lets you format LogQL queries. It returns an error if the passed LogQL is invalid. It is exposed by all Loki components and helps to improve readability and the debugging experience of LogQL queries.

The following example formats the expression LogQL `{foo=   "bar"}` into

```json
{
   "status" : "success",
   "data" : "{foo=\"bar\"}"
}
```
