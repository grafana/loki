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

  - `query`: a logQL query
  - `limit`: max number of entries to return
  - `start`: the start time for the query, as a nanosecond Unix epoch (nanoseconds since 1970). Default is always one hour ago.
  - `end`: the end time for the query, as a nanosecond Unix epoch (nanoseconds since 1970). Default is current time.
  - `direction`: `forward` or `backward`, useful when specifying a limit. Default is backward.
  - `regexp`: a regex to filter the returned results, will eventually be rolled into the query language

  Loki needs to query the index store in order to find log streams for particular labels and the store is spread out by time,
  so you need to specify the start and end labels accordingly. Querying a long time into the history will cause additional
  load to the index server and make the query slower.

  Responses looks like this:

  ```
  {
    "streams": [
      {
        "labels": "{instance=\"...\", job=\"...\", namespace=\"...\"}",
        "entries": [
          {
            "timestamp": "2018-06-27T05:20:28.699492635Z",
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

  For retrieving the names of the labels one can query on.

  Responses looks like this:

  ```
  {
    "values": [
      "instance",
      "job",
      ...
    ]
  }
  ```

- `GET /api/prom/label/<name>/values`
  For retrieving the label values one can query on.

  Responses looks like this:

  ```
  {
    "values": [
      "default",
      "cortex-ops",
      ...
    ]
  }
  ```

## Example of using the API in a third-party client library

Take a look at this [client](https://github.com/afiskon/promtail-client), but be aware that the API is not stable yet.
