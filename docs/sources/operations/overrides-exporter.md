---
title: Overrides exporter
menuTitle:  
description: Describes how the Overrides Exporter module exposes tenant limits as Prometheus metrics.
weight: 
---

# Overrides exporter

Loki is a multi-tenant system that supports applying limits to each tenant as a mechanism for resource management. The `overrides-exporter` module exposes these limits as Prometheus metrics in order to help operators better understand tenant behavior.

## Context

Configuration updates to tenant limits can be applied to Loki without restart via the [`runtime_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#runtime_config) feature.

## Example

The `overrides-exporter` module is disabled by default. We recommend running a single instance per cluster to avoid issues with metric cardinality. The `overrides-exporter` creates one metric for every scalar field in the limits configuration under the metric `loki_overrides_defaults` with the default value for that field after loading the Loki configuration. It also exposes another metric for _every_ differing field for _every_ tenant.

Using an example `runtime.yaml`:

```yaml
overrides:
  "tenant_1":
    ingestion_rate_mb: 10
    max_streams_per_user: 100000
    max_chunks_per_query: 100000
```

Launch an instance of the `overrides-exporter`:

```shell
loki -target=overrides-exporter -runtime-config.file=runtime.yaml -config.file=basic_schema_config.yaml -server.http-listen-port=8080
```

To inspect the tenant limit overrides:

```shell
$ curl -sq localhost:8080/metrics | grep override
# HELP loki_overrides Resource limit overrides applied to tenants
# TYPE loki_overrides gauge
loki_overrides{limit_name="ingestion_rate_mb",user="tenant_1"} 10
loki_overrides{limit_name="max_chunks_per_query",user="tenant_1"} 100000
loki_overrides{limit_name="max_streams_per_user",user="tenant_1"} 100000
# HELP loki_overrides_defaults Default values for resource limit overrides applied to tenants
# TYPE loki_overrides_defaults gauge
loki_overrides_defaults{limit_name="cardinality_limit"} 100000
loki_overrides_defaults{limit_name="creation_grace_period"} 6e+11
loki_overrides_defaults{limit_name="ingestion_burst_size_mb"} 6
loki_overrides_defaults{limit_name="ingestion_rate_mb"} 4
loki_overrides_defaults{limit_name="max_cache_freshness_per_query"} 6e+10
loki_overrides_defaults{limit_name="max_chunks_per_query"} 2e+06
loki_overrides_defaults{limit_name="max_concurrent_tail_requests"} 10
loki_overrides_defaults{limit_name="max_entries_limit_per_query"} 5000
loki_overrides_defaults{limit_name="max_global_streams_per_user"} 5000
loki_overrides_defaults{limit_name="max_label_name_length"} 1024
loki_overrides_defaults{limit_name="max_label_names_per_series"} 30
loki_overrides_defaults{limit_name="max_label_value_length"} 2048
loki_overrides_defaults{limit_name="max_line_size"} 0
loki_overrides_defaults{limit_name="max_queriers_per_tenant"} 0
loki_overrides_defaults{limit_name="max_query_length"} 2.5956e+15
loki_overrides_defaults{limit_name="max_query_lookback"} 0
loki_overrides_defaults{limit_name="max_query_parallelism"} 32
loki_overrides_defaults{limit_name="max_query_series"} 500
loki_overrides_defaults{limit_name="max_streams_matchers_per_query"} 1000
loki_overrides_defaults{limit_name="max_streams_per_user"} 0
loki_overrides_defaults{limit_name="min_sharding_lookback"} 0
loki_overrides_defaults{limit_name="per_stream_rate_limit"} 3.145728e+06
loki_overrides_defaults{limit_name="per_stream_rate_limit_burst"} 1.572864e+07
loki_overrides_defaults{limit_name="per_tenant_override_period"} 1e+10
loki_overrides_defaults{limit_name="reject_old_samples_max_age"} 1.2096e+15
loki_overrides_defaults{limit_name="retention_period"} 2.6784e+15
loki_overrides_defaults{limit_name="ruler_evaluation_delay_duration"} 0
loki_overrides_defaults{limit_name="ruler_max_rule_groups_per_tenant"} 0
loki_overrides_defaults{limit_name="ruler_max_rules_per_rule_group"} 0
loki_overrides_defaults{limit_name="ruler_remote_write_queue_batch_send_deadline"} 0
loki_overrides_defaults{limit_name="ruler_remote_write_queue_capacity"} 0
loki_overrides_defaults{limit_name="ruler_remote_write_queue_max_backoff"} 0
loki_overrides_defaults{limit_name="ruler_remote_write_queue_max_samples_per_send"} 0
loki_overrides_defaults{limit_name="ruler_remote_write_queue_max_shards"} 0
loki_overrides_defaults{limit_name="ruler_remote_write_queue_min_backoff"} 0
loki_overrides_defaults{limit_name="ruler_remote_write_queue_min_shards"} 0
loki_overrides_defaults{limit_name="ruler_remote_write_timeout"} 0
loki_overrides_defaults{limit_name="split_queries_by_interval"} 0
```

Alerts can be created based on these metrics to inform operators when tenants are close to hitting their limits allowing for increases to be applied before the tenant limits are exceeded.
