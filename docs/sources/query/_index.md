---
title: "LogQL: Log query language"
menuTItle: Query
description: LogQL, Loki's query language for logs.
aliases: 
- ./logql
weight: 600
---

# LogQL: Log query language

LogQL is Grafana Loki's PromQL-inspired query language.
Queries act as if they are a distributed `grep` to aggregate log sources.
LogQL uses labels and operators for filtering.

There are two types of LogQL queries:

- [Log queries]({{< relref "./log_queries" >}}) return the contents of log lines.
- [Metric queries]({{< relref "./metric_queries" >}}) extend log queries to calculate values
based on query results.

## Comments

LogQL queries can be commented using the `#` character:

```logql
{app="foo"} # anything that comes after will not be interpreted in your query
```

With multi-line LogQL queries, the query parser can exclude whole or partial lines using `#`:

```logql
{app="foo"}
    | json
    # this line will be ignored
    | bar="baz" # this checks if bar = "baz"
```

## Pipeline Errors

There are multiple reasons which cause pipeline processing errors, such as:

- A numeric label filter may fail to turn a label value into a number
- A metric conversion for a label may fail.
- A log line is not a valid json document.
- etc...

When those failures happen, Loki won't filter out those log lines. Instead they are passed into the next stage of the pipeline with a new system label named `__error__`. The only way to filter out errors is by using a label filter expressions. The `__error__` label can't be renamed via the language.

For example to remove json errors:

```logql
  {cluster="ops-tools1",container="ingress-nginx"}
    | json
    | __error__ != "JSONParserErr"
```

Alternatively you can remove all error using a catch all matcher such as `__error__ = ""` or even show only errors using `__error__ != ""`.

The filter should be placed after the stage that generated this error. This means if you need to remove errors from an unwrap expression it needs to be placed after the unwrap.

```logql
quantile_over_time(
	0.99,
	{container="ingress-nginx",service="hosted-grafana"}
	| json
	| unwrap response_latency_seconds
	| __error__=""[1m]
	) by (cluster)
```

>Metric queries cannot contain errors, in case errors are found during execution, Loki will return an error and appropriate status code.
