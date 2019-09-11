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

- `GET /loki/api/v1/query`

  For doing instant queries at a single point in time, accepts the following parameters in the query-string:

  - `query`: a logQL query
  - `limit`: max number of entries to return (not used for metric queries)
  - `time`: the evaluation time for the query, as a nanosecond Unix epoch (nanoseconds since 1970). Default is always now.
  - `direction`: `forward` or `backward`, useful when specifying a limit. Default is backward.

  Loki needs to query the index store in order to find log streams for particular labels and the store is spread out by time,
  so you need to specify the time and labels accordingly. Querying a long time into the history will cause additional
  load to the index server and make the query slower.

  Responses looks like this:

  ```json
  {
    "status" : "success",
    "data": {
        "resultType": "vector" | "streams",
        "result": <value>
    }
  }
  ```

  Examples:

  ```bash
  $ curl -G -s  "http://localhost:3100/loki/api/v1/query" --data-urlencode 'query=sum(rate({job="varlogs"}[10m])) by (level)' | jq
  {
    "status" : "success",
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
  curl -G -s  "http://localhost:3100/loki/api/v1/query" --data-urlencode 'query={job="varlogs"}' | jq
  {
    "resultType": "streams",
    "result": [
      {
        "stream": {
          "filename": "/var/hostlog/syslog",
          "job": "varlogs"
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
  ```

- `GET /loki/api/v1/query_range`

  For doing queries over a range of time, accepts the following parameters in the query-string:

  - `query`: a logQL query
  - `limit`: max number of entries to return (not used for metric queries)
  - `start`: the start time for the query, as a nanosecond Unix epoch (nanoseconds since 1970). Default is always one hour ago.
  - `end`: the end time for the query, as a nanosecond Unix epoch (nanoseconds since 1970). Default is always now.
  - `step`: query resolution step width in seconds. Default 1 second.
  - `direction`: `forward` or `backward`, useful when specifying a limit. Default is backward.

  Loki needs to query the index store in order to find log streams for particular labels and the store is spread out by time,
  so you need to specify the time and labels accordingly. Querying a long time into the history will cause additional
  load to the index server and make the query slower.

  Responses looks like this:

  ```json
  {
    "status" : "success",
    "data": {
      "resultType": "matrix" | "streams",
      "result": <value>
    }
  }
  ```

  Examples:

  ```bash
  $ curl -G -s  "http://localhost:3100/loki/api/v1/query_range" --data-urlencode 'query=sum(rate({job="varlogs"}[10m])) by (level)' --data-urlencode 'step=300' | jq
  {
    "status" : "success",
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
  curl -G -s  "http://localhost:3100/loki/api/v1/query_range" --data-urlencode 'query={job="varlogs"}' | jq
  {
    "resultType": "streams",
    "result": [
      {
        "stream": {
          "filename": "/var/hostlog/syslog",
          "job": "varlogs"
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
  ```

- `GET /loki/api/v1/label`

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

- `GET /loki/api/v1/label/<name>/values`

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
