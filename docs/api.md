# Loki API

## Authentication

*nb* Authentication is out of scope for this project.
You are expected to run an authenticating reverse proxy in front of our services, such as an Nginx with basic auth or a OAuth2 proxy.

Loki is a multitenant system; requests and data for tenant A are isolated from tenant B.
Requests to the Loki API should include an HTTP header (`X-Scope-OrgID`) identifying the tentant for the request.
Tenant IDs can be any alphanumeric string; limiting them to 20 bytes is reasonable.

Loki can be run in "single-tenant" mode where the `X-Scope-OrgID` header is not required.
In this situation, the tenant ID is defaulted to be `fake`.

## REST API

There are 4 API endpoints:

- `POST /api/prom/push`

  For sending log entries, expects a snappy compressed proto in the HTTP Body:

  - [ProtoBuffer definition](/pkg/logproto/logproto.proto)
  - [Golang client library](/pkg/promtail/client.go)
  - [Third party client library](https://github.com/afiskon/promtail-client)

  Also accepts JSON formatted requests when the header `Content-Type: application/json` is sent.  Example of the JSON format:

  ```json
  {
      "streams": [
          {
              "labels": "{foo=\"bar\"}",
              "entries": [
                  {"ts": "2018-12-18T08:28:06.801064-04:00", "line": "baz"}
              ]
          }
      ]
  }
  ```

- `GET /api/prom/query`

  For doing queries, accepts the following parameters in the query-string:
  - `query`: a logQL query
  - `limit`: max number of entries to return
  - `start`: the start time for the query, as a nanosecond Unix epoch (nanoseconds since 1970)
  - `end`: the end time for the query, as a nanosecond Unix epoch (nanoseconds since 1970)
  - `direction`: `forward` or `backward`, useful when specifying a limit
  - `regexp`: a regex to filter the returned results, will eventually be rolled into the query language

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
