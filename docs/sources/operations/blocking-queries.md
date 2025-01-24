---
title: Block unwanted queries
menuTitle: Unwanted queries
description: Describes how to configure Grafana Loki to block unwanted or expensive queries using per-tenant overrides.
weight: 
---
# Block unwanted queries

In certain situations, you may not be able to control the queries being sent to your Loki installation. These queries
may be intentionally or unintentionally expensive to run, and they may affect the overall stability or cost of running
your service.

You can block queries using [per-tenant overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#runtime-configuration-file), like so:

```yaml
overrides:
  "tenant-id":
    blocked_queries:
      # block this query exactly
      - pattern: 'sum(rate({env="prod"}[1m]))'

      # block any query matching this regex pattern 
      - pattern: '.*prod.*'
        regex: true

      # block all metric queries
      - types: metric

      # block any filter or limited queries matching this regex pattern 
      - pattern: '.*prod.*'
        regex: true
        types: filter,limited

      # block any query that matches this query hash
      - hash: 2943214005          # hash of {stream="stdout",pod="loki-canary-9w49x"}
        types: filter,limited
```
{{< admonition type="note" >}}
Changes to these configurations **do not require a restart**; they are defined in the [runtime configuration file](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#runtime-configuration-file).
{{< /admonition >}}

The available query types are:

- `metric`: a query with an aggregation, e.g. `sum(rate({env="prod"}[1m]))`
- `filter`: a query with a log filter, e.g. `{env="prod"} |= "error"`
- `limited`: a query without a filter or a metric aggregation

The `hash` option uses a [32-bit FNV-1](https://en.wikipedia.org/wiki/Fowler%E2%80%93Noll%E2%80%93Vo_hash_function) hash of the query string, represented as a 32-bit unsigned integer.
This can often be easier to use than query strings that are long or require lots of string escaping. A `query_hash` field
is logged with every query request in the `query-frontend` and `querier` logs, for easy reference. Here's an example log line:

```logfmt
level=info ts=2023-03-30T09:08:15.2614555Z caller=metrics.go:152 component=frontend org_id=29 latency=fast 
query="{stream=\"stdout\",pod=\"loki-canary-9w49x\"}" query_hash=2943214005 query_type=limited range_type=range ...
```
{{< admonition type="note" >}}
The order of patterns is preserved, so the first matching pattern will be used.
{{< /admonition >}}

## Observing blocked queries

Blocked queries are logged, as well as counted in the `loki_blocked_queries` metric on a per-tenant basis.

## Scope

Queries received via the API and executed as [alerting/recording rules]({{< relref "../alert" >}}) will be blocked.
