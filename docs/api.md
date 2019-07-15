# Loki API

The Loki server has the following API endpoints (_Note:_ Authentication is out of scope for this project):

- `POST /api/prom/push`

  For sending log entries, expects a snappy compressed proto in the HTTP Body:

  - [ProtoBuffer definition](/pkg/logproto/logproto.proto)
  - [Golang client library](/pkg/promtail/client/client.go)

  Also accepts JSON formatted requests when the header `Content-Type: application/json` is sent. Example of the JSON format:

  ```json
  {
    "streams": [
      {
        "labels": "{foo=\"bar\"}",
        "entries": [{ "ts": "2018-12-18T08:28:06.801064-04:00", "line": "baz" }]
      }
    ]
  }
  ```

- `GET /api/prom/query`

  For doing queries, accepts the following parameters in the query-string:

  - `query`: a [logQL query](./usage.md) (eg: `{name=~"mysql.+"}` or `{name=~"mysql.+"} |= "error"`)
  - `limit`: max number of entries to return
  - `start`: the start time for the query, as a nanosecond Unix epoch (nanoseconds since 1970) or as RFC3339Nano (eg: "2006-01-02T15:04:05.999999999-07:00"). Default is always one hour ago.
  - `end`: the end time for the query, as a nanosecond Unix epoch (nanoseconds since 1970) or as RFC3339Nano (eg: "2006-01-02T15:04:05.999999999-07:00"). Default is current time.
  - `direction`: `forward` or `backward`, useful when specifying a limit. Default is backward.
  - `regexp`: a regex to filter the returned results

  Loki needs to query the index store in order to find log streams for particular labels and the store is spread out by time,
  so you need to specify the start and end labels accordingly. Querying a long time into the history will cause additional
  load to the index server and make the query slower.

  Responses looks like this:

  ```json
  {
    "streams": [
      {
        "labels": "{instance=\"...\", job=\"...\", namespace=\"...\"}",
        "entries": [
          {
            "ts": "2018-06-27T05:20:28.699492635Z",
            "line": "..."
          },
          ...
        ]
      },
      ...
    ]
  }
  ```

- `GET /api/prom/label`

  For doing label name queries, accepts the following parameters in the query-string:

  - `start`: the start time for the query, as a nanosecond Unix epoch (nanoseconds since 1970). Default is always 6 hour ago.
  - `end`: the end time for the query, as a nanosecond Unix epoch (nanoseconds since 1970). Default is current time.

  Responses looks like this:

  ```json
  {
    "values": [
      "instance",
      "job",
      ...
    ]
  }
  ```

- `GET /api/prom/label/<name>/values`

  For doing label values queries, accepts the following parameters in the query-string:

  - `start`: the start time for the query, as a nanosecond Unix epoch (nanoseconds since 1970). Default is always 6 hour ago.
  - `end`: the end time for the query, as a nanosecond Unix epoch (nanoseconds since 1970). Default is current time.

  Responses looks like this:

  ```json
  {
    "values": [
      "default",
      "cortex-ops",
      ...
    ]
  }
  ```

- `GET /ready`

  This endpoint returns 200 when Loki ingester is ready to accept traffic. If you're running Loki on Kubernetes, this endpoint can be used as readiness probe.

- `GET /flush`

  This endpoint triggers a flush of all in memory chunks in the ingester. Mainly used for local testing.

- `GET /metrics`

  This endpoint returns Loki metrics for Prometheus. See "[Operations > Observability > Metrics](./operations.md)" to have a list of exported metrics.


## Examples of using the API in a third-party client library

1) Take a look at this [client](https://github.com/afiskon/promtail-client), but be aware that the API is not stable yet (Golang).
2) Example on [Python3](https://github.com/sleleko/devops-kb/blob/master/python/push-to-loki.py)
