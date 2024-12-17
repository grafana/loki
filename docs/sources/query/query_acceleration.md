---
title: Query acceleration
menuTitle: Query acceleration
description: Provides instructions on how to write LogQL queries to benefit from query acceleration.
weight: 900
keywords:
  - blooms
  - query acceleration
---

# Query acceleration

{{< admonition type="warning" >}}
In Loki and Grafana Enterprise Logs (GEL), Query acceleration using blooms is an [experimental feature](/docs/release-life-cycle/). Engineering and on-call support is not available. No SLA is provided.

In Grafana Cloud, Query acceleration using Bloom filters is enabled as a [public preview](/docs/release-life-cycle/) for select large-scale customers that are ingesting more that 75TB of logs a month. Limited support and no SLA are provided.
{{< /admonition >}}

If [bloom filters][] are enabled, you can write LogQL queries using [structured metadata][] to benefit from query acceleration.

## Prerequisites

* [Bloom filters][bloom filters] must be enabled. OpenSource and Enterprise customers must [enable bloom filters](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/bloom-filters/#enable-bloom-filters). Grafana Support enables Bloom Filters for Cloud Customers who are part of the public preview.
* Logs must be sending [structured metadata][]. OpenSource and Enterprise customers must [enable structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/#enable-or-disable-structured-metadata). Structured metadata is enabled by default in Grafana Cloud.

### Query blooms

Queries will be accelerated for any [label filter expression][] that satisfies _all_ of the following criteria:

* The label filter expression using **string equality**, such as `| key="value"`.
  * `or` and `and` operators can be used to match multiple values, such as `| detected_level="error" or detected_level="warn"`.
  * _Basic_ regular expressions are automatically simplified into a supported expression:
    * `| key=~"value"` is converted to `| key="value"`.
    * `| key=~"value1|value2"` is converted to `| key="value1" or key="value2"`.
    * `| key=~".+"` checks for existence of `key`. `.*` is not supported.
* The label filter expression is querying for structured metadata and not a stream label.
* The label filter expression is placed before any [parser expression][], [labels format expression][], [drop labels expression][], or [keep labels expression][].

To take full advantage of query acceleration with blooms, ensure that filtering structured metadata is done before any parser expression:

In the following example, the query is not accelerated because the structured metadata filter, `detected_level="error"`, is after a parser stage, `json`.

```logql
{cluster="prod"} | logfmt | json | detected_level="error" 
```

In the following example, the query is accelerated because the structured metadata filter is before any parser stage.

```logql
{cluster="prod"} | detected_level="error" | logfmt | json 
```

[bloom filters]: https://grafana.com/docs/loki/<LOKI_VERSION>/operations/bloom-filters/
[structured metadata]: https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata
[label filter expression]: https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#label-filter-expression
[parser expression]: https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#parser-expression
[labels format expression]: https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#labels-format-expression
[drop labels expression]: https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#drop-labels-expression
[keep labels expression]: https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#keep-labels-expression
