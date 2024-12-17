---
title: Query examples
menuTitle: Query examples  
description: Provides LogQL query examples with explanations on what those queries accomplish.
aliases: 
- ../logql/query_examples/
weight: 800 
---

# Query examples

These LogQL query examples have explanations of what the queries accomplish.

## Log query examples

### Examples that filter on IP address 

- Return log lines that are not within a range of IPv4 addresses:

    ```logql
    {job_name="myapp"} != ip("192.168.4.5-192.168.4.20")
    ```


- This example matches log lines with all IPv4 subnet values `192.168.4.5/16` except IP address `192.168.4.2`:

    ```logql
    {job_name="myapp"}
		| logfmt
		| addr = ip("192.168.4.5/16")
		| addr != ip("192.168.4.2")
    ```

### Examples that aid in security evaluation

- Extract the user and IP address of failed logins from Linux `/var/log/secure`

    ```logql
    {job="security"} 
        |~ "Invalid user.*"
        | regexp "(^(?P<user>\\S+ {1,2}){8})"
        | regexp "(^(?P<ip>\\S+ {1,2}){10})"
        | line_format "IP = {{.ip}}\tUSER = {{.user}}"
    ```
   
- Get successful logins from Linux `/var/log/secure`

    ```logql
    {job="security"}
        != "grafana_com"
        |= "session opened"
        != "sudo: "
        | regexp "(^(?P<user>\\S+ {1,2}){11})"
        | line_format "USER = {{.user}}"
    ```

## Metrics query examples

- Return the per-second rate of all non-timeout errors
within the last minutes per host for the MySQL job,
and only include errors whose duration is above ten seconds.

    ```
    sum by (host) (rate({job="mysql"}
        |= "error" != "timeout"
        | json
        | duration > 10s [1m]))
    ```

## Multiple filtering stages examples

Query results are gathered by successive evaluation of parts of the query from left to right.
To make querying efficient,
order the filtering stages left to right:

1. stream selector
2. line filters
3. label filters

Consider the query:

```logql
{cluster="ops-tools1", namespace="loki-dev", job="loki-dev/query-frontend"} |= "metrics.go" != "out of order" | logfmt | duration > 30s or status_code != "200"
```
Within this query, the stream selector is

```
{cluster="ops-tools1", namespace="loki-dev", job="loki-dev/query-frontend"}
```

There are two line filters: 
`|= "metrics.go"`
and `!="out of order"`.
Of the log lines identified with the stream selector,
the query results
include only those log lines that contain the string "metrics.go"
and do not contain the string "out of order".

The `logfmt` parser produces the `duration` and `status_code` labels,
such that they can be used by a label filter.

The label filter 
`| duration > 30s or status_code!="200"`
further filters out log lines.
It includes those log lines that contain a `status_code` label
with any value other than the value 200,
as well as log lines that contain a `duration` label
with a value greater than 30 sections,

While every query will have a stream selector,
not all queries will have line and label filters.

## Examples that use multiple parsers

Consider this logfmt log line.
To extract the method and the path of this logfmt log line,

```log
level=debug ts=2020-10-02T10:10:42.092268913Z caller=logging.go:66 traceID=a9d4d8a928d8db1 msg="POST /api/prom/api/v1/query_range (200) 1.5s"
```

To extract the method and the path,
use multiple parsers (logfmt and regexp):

```logql
{job="loki-ops/query-frontend"} | logfmt | line_format "{{.msg}}" | regexp "(?P<method>\\w+) (?P<path>[\\w|/]+) \\((?P<status>\\d+?)\\) (?P<duration>.*)"
```

This is possible because the `| line_format` reformats the log line to become `POST /api/prom/api/v1/query_range (200) 1.5s` which can then be parsed with the `| regexp ...` parser.

## Log line formatting examples

The following query shows how you can reformat a log line to make it easier to read on screen.

```logql
{cluster="ops-tools1", name="querier", namespace="loki-dev"}
  |= "metrics.go" != "loki-canary"
  | logfmt
  | query != ""
  | label_format query="{{ Replace .query \"\\n\" \"\" -1 }}"
  | line_format "{{ .ts}}\t{{.duration}}\ttraceID = {{.traceID}}\t{{ printf \"%-100.100s\" .query }} "
```

Label formatting is used to sanitize the query while the line format reduce the amount of information and creates a tabular output.

For these given log lines:

```log
level=info ts=2020-10-23T20:32:18.094668233Z caller=metrics.go:81 org_id=29 traceID=1980d41501b57b68 latency=fast query="{cluster=\"ops-tools1\", job=\"loki-ops/query-frontend\"} |= \"query_range\"" query_type=filter range_type=range length=15m0s step=7s duration=650.22401ms status=200 throughput_mb=1.529717 total_bytes_mb=0.994659
level=info ts=2020-10-23T20:32:18.068866235Z caller=metrics.go:81 org_id=29 traceID=1980d41501b57b68 latency=fast query="{cluster=\"ops-tools1\", job=\"loki-ops/query-frontend\"} |= \"query_range\"" query_type=filter range_type=range length=15m0s step=7s duration=624.008132ms status=200 throughput_mb=0.693449 total_bytes_mb=0.432718
```

The result would be:

```log
2020-10-23T20:32:18.094668233Z	650.22401ms	    traceID = 1980d41501b57b68	{cluster="ops-tools1", job="loki-ops/query-frontend"} |= "query_range"
2020-10-23T20:32:18.068866235Z	624.008132ms	traceID = 1980d41501b57b68	{cluster="ops-tools1", job="loki-ops/query-frontend"} |= "query_range"
```

It's possible to strip ANSI sequences from the log line, making it easier
to parse it further:

```
{job="example"} | decolorize
```

This way this log line:

```
[2022-11-04 22:17:57.811] \033[0;32http\033[0m: GET /_health (0 ms) 204
```

turns into:
```
[2022-11-04 22:17:57.811] http: GET /_health (0 ms) 204
```


## Unwrap examples

- Calculate the p99 of the nginx-ingress latency by path:

    ```logql
    quantile_over_time(0.99,
      {cluster="ops-tools1",container="ingress-nginx"}
        | json
        | __error__ = ""
        | unwrap request_time [1m]) by (path)
    ```

- Calculate the quantity of bytes processed per organization ID:

    ```logql
    sum by (org_id) (
      sum_over_time(
      {cluster="ops-tools1",container="loki-dev"}
          |= "metrics.go"
          | logfmt
          | unwrap bytes_processed [1m])
      )
    ```

## Vector aggregation examples

Get the top 10 applications by the highest log throughput:

```logql
topk(10,sum(rate({region="us-east1"}[5m])) by (name))
```

Get the count of log lines for the last five minutes for a specified job, grouping
by level:

```logql
sum(count_over_time({job="mysql"}[5m])) by (level)
```

Get the rate of HTTP GET requests to the `/home` endpoint for NGINX logs by region:

```logql
avg(rate(({job="nginx"} |= "GET" | json | path="/home")[10s])) by (region)
```

