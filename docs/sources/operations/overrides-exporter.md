---
title: Monitor tenant limits using the Overrides Exporter
menuTitle: Overrides Exporter
description: Describes how the Overrides Exporter exposes tenant limits as Prometheus metrics.
weight: 
---

# Monitor tenant limits using the Overrides Exporter

Loki is a multi-tenant system that supports applying limits to each tenant as a mechanism for resource management. The `overrides-exporter` module exposes these limits as Prometheus metrics in order to help operators better understand tenant behavior.

## Context

Configuration updates to tenant limits can be applied to Loki without restart via the [`runtime_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#runtime_config) feature.

## Example

The `overrides-exporter` module is disabled by default. We recommend running a single instance per cluster to avoid issues with metric cardinality. The `overrides-exporter` creates one metric for every numeric and boolean field in the limits configuration under the metric `loki_overrides_defaults` with the default value for that field after loading the Loki configuration. It also exposes another metric for _every_ differing field for _every_ tenant.

Using an example `runtime.yaml`:

```yaml
overrides:
  "tenant_1":
    ingestion_rate_mb: 10
    max_streams_per_user: 100000
```

Launch an instance of the `overrides-exporter`:

```shell
loki -target=overrides-exporter -runtime-config.file=runtime.yaml -config.file=basic_schema_config.yaml -server.http-listen-port=8080
```

To inspect the tenant limit overrides:

```shell
$ curl -s localhost:8080/metrics | grep override
# HELP loki_overrides Resource limit overrides applied to tenants
# TYPE loki_overrides gauge
loki_overrides{limit_name="ingestion_rate_mb",user="tenant_1"} 10
loki_overrides{limit_name="max_streams_per_user",user="tenant_1"} 100000
# HELP loki_overrides_defaults Default values for resource limit overrides applied to tenants
# TYPE loki_overrides_defaults gauge
loki_overrides_defaults{limit_name="block_ingestion_status_code"} 260
loki_overrides_defaults{limit_name="cardinality_limit"} 100000
loki_overrides_defaults{limit_name="ingestion_burst_size_mb"} 6
loki_overrides_defaults{limit_name="ingestion_rate_mb"} 4
loki_overrides_defaults{limit_name="max_global_streams_per_user"} 5000
loki_overrides_defaults{limit_name="max_query_parallelism"} 32
loki_overrides_defaults{limit_name="max_streams_per_user"} 0
loki_overrides_defaults{limit_name="query_timeout"} 6e+10
loki_overrides_defaults{limit_name="retention_period"} 0
loki_overrides_defaults{limit_name="volume_enabled"} 1
```

{{< admonition type="note" >}}
This output is abbreviated for readability. In practice, `loki_overrides_defaults` includes one series for every numeric and boolean field in the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config) block. The full set is much larger than what is shown here, and it grows with each release. Query your own instance's `/metrics` endpoint for the full, authoritative, current list rather than relying on a fixed example.

Values follow the field's underlying Go type: durations (such as `query_timeout`) are reported in nanoseconds, byte sizes (such as `per_stream_rate_limit`) are reported in bytes, and booleans are reported as `1` (true) or `0` (false).
{{< /admonition >}}

Alerts can be created based on these metrics to inform operators when tenants are close to hitting their limits allowing for increases to be applied before the tenant limits are exceeded.
