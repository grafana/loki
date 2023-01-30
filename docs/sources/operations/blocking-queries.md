---
title: Blocking Queries
description: Blocking Queries
weight: 60
---
# Blocking Queries

In certain situations, you may not be able to control the queries being sent to your Loki installation. These queries
may be intentionally or unintentionally expensive to run, and they may affect the overall stability or cost of running
your service.

You can block queries using [per-tenant overrides]({{<relref "../configuration/#runtime-configuration-file">}}), like so:

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
```

The available query types are:

- `metric`: a query with an aggregation, e.g. `sum(rate({env="prod"}[1m]))`
- `filter`: a query with a log filter, e.g. `{env="prod"} |= "error"`
- `limited`: a query without a filter or a metric aggregation

**Note:** the order of patterns is preserved, so the first matching pattern will be used

## Observing blocked queries

Blocked queries are logged, as well as counted in the `loki_blocked_queries` metric on a per-tenant basis.

## Scope

Queries received via the API and executed as [alerting/recording rules]({{<relref "../rules">}}) will be blocked.
