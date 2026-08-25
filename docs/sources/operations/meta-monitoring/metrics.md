---
title: Key metrics for monitoring Loki
menuTitle: Key Metrics
description: Describes the most important Loki metrics for detecting negative trends and abnormal behavior.
weight: 200
---

# Key metrics for monitoring Loki

Loki exposes many metrics, and each component behaves differently under load. This page focuses on the highest-signal metrics for detecting negative trends early.

{{< admonition type="note" >}}
The example queries on this page are PromQL. Run them against the Prometheus-compatible data source where your Loki metrics are stored (for example, Prometheus, Mimir, or Grafana Cloud Metrics).
{{< /admonition >}}

For setup and prebuilt dashboards and alerts, refer to:

- [Deploy Loki meta-monitoring](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/deploy/)
- [Install dashboards, alerts, and recording rules](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/mixins/)

## Request error rate

Watch request failures first. A sustained increase in 5xx responses is usually the earliest sign of user-visible impact.

Key metric:

- `loki_request_duration_seconds_count` (counter with labels including `status_code`, `job`, and `route`)

Example query:

```promql
100 * sum(rate(loki_request_duration_seconds_count{status_code=~"5.."}[2m])) by (cluster, namespace, job, route)
/
sum(rate(loki_request_duration_seconds_count[2m])) by (cluster, namespace, job, route)
```

Abnormal behavior:

- Any sustained increase in 5xx ratio.
- The Loki mixin alert `LokiRequestErrors` fires when this ratio is greater than 10% for 15 minutes.

## Request latency (p99)

Latency degradation can appear before hard failures. Track p99 for read and write routes.

Key metric:

- `loki_request_duration_seconds_bucket` (histogram buckets)

Example query:

```promql
histogram_quantile(0.99, sum(rate(loki_request_duration_seconds_bucket[1m])) by (le, cluster, namespace, job, route))
```

Abnormal behavior:

- Rising p99 over time, especially in query-frontend and distributor paths.
- The Loki mixin alert `LokiRequestLatency` fires when p99 exceeds 1 second for 15 minutes.

## Panics

Panics are high-severity faults and should stay at zero.

Key metric:

- `loki_panic_total`

Example query:

```promql
sum(increase(loki_panic_total[10m])) by (cluster, namespace, job)
```

Abnormal behavior:

- Any value above zero. The Loki mixin alert `LokiRequestPanics` treats this as critical.

## Discarded samples

Discarded samples indicate data that Loki rejected or dropped. This is one of the most important ingestion-quality signals.

Key metric:

- `loki_discarded_samples_total`

Example query:

```promql
topk(10, sum by (tenant, reason) (rate(loki_discarded_samples_total{cluster="$cluster", namespace="$namespace"}[$__rate_interval])))
```

Abnormal behavior:

- Increasing discard rate.
- New or growing `reason` values (for example, tenant limits or stream limits).

## Compaction health

Compaction issues can silently degrade read performance and retention behavior over time.

{{< admonition type="note" >}}
The compaction and retention metrics use a `loki_boltdb_shipper_` prefix for historical reasons. The compactor emits these metrics regardless of which index type you use, including TSDB.
{{< /admonition >}}

Key metrics:

- `loki_boltdb_shipper_compactor_running`
- `loki_boltdb_shipper_compact_tables_operation_last_successful_run_timestamp_seconds`
- `loki_boltdb_shipper_compact_tables_operation_total`
- `loki_boltdb_shipper_compact_tables_operation_duration_seconds`

Example queries:

```promql
sum(loki_boltdb_shipper_compactor_running) by (cluster, namespace)
```

```promql
time() - (loki_boltdb_shipper_compact_tables_operation_last_successful_run_timestamp_seconds > 0)
```

Abnormal behavior:

- More than one compactor running.
- No successful compaction for multiple hours (the mixin alert threshold is 3 hours).

## Ingester health and flush behavior

Ingester pressure often appears as memory growth, poor chunk utilization, or flush backlog.

Key metrics:

- `loki_ingester_memory_streams`
- `loki_ingester_memory_chunks`
- `loki_ingester_flush_queue_length`
- `loki_ingester_chunk_utilization`
- `loki_ingester_chunks_flushed_total`

Example queries:

```promql
sum(loki_ingester_memory_streams{cluster="$cluster", namespace="$namespace"})
```

```promql
sum(loki_ingester_flush_queue_length{cluster="$cluster", namespace="$namespace"})
```

Abnormal behavior:

- Persistent growth in in-memory streams or chunks.
- Increasing flush queue length.
- Low chunk utilization for long periods.

## Distributor throughput

Throughput changes help identify upstream sender issues, sudden traffic shifts, or ingestion bottlenecks.

Key metrics:

- `loki_distributor_bytes_received_total`
- `loki_distributor_lines_received_total`

Example queries:

```promql
sum(rate(loki_distributor_bytes_received_total{cluster="$cluster", namespace="$namespace"}[$__rate_interval]))
```

```promql
sum(rate(loki_distributor_lines_received_total{cluster="$cluster", namespace="$namespace"}[$__rate_interval]))
```

Abnormal behavior:

- Sharp drops (possible data path interruption).
- Unexpected spikes (possible overload or noisy tenants).

## Object store operations

Object store latency and failures directly impact query and retention workflows.

Key metrics:

- `loki_objstore_bucket_operations_total`
- `loki_objstore_bucket_operation_failures_total`
- `loki_objstore_bucket_operation_duration_seconds`

Example queries:

```promql
sum by (operation) (rate(loki_objstore_bucket_operation_failures_total{cluster="$cluster", namespace="$namespace"}[$__rate_interval]))
```

```promql
histogram_quantile(0.99, sum(rate(loki_objstore_bucket_operation_duration_seconds_bucket{cluster="$cluster", namespace="$namespace"}[$__rate_interval])) by (le, operation))
```

Abnormal behavior:

- Growing failure rates by operation.
- Increasing p99 latency for `get`, `get_range`, or `upload`.

## Resource and runtime health

Resource pressure can explain or predict service degradation before alert thresholds are crossed.

Common signals to track:

- Container CPU usage
- Container memory working set
- Go heap in use
- Disk read and write rates
- Container restarts

Abnormal behavior:

- Repeated restart spikes.
- Sustained CPU saturation.
- Memory growth without recovery.

## Loki Canary (end-to-end data verification)

If you run Loki Canary, use it as an end-to-end correctness signal, not only a performance signal.

Key metrics:

- `loki_canary_missing_entries_total`
- `loki_canary_spot_check_missing_entries_total`
- `loki_canary_response_latency_seconds_bucket`

Example query:

```promql
sum(increase(loki_canary_missing_entries_total{cluster=~"$cluster", namespace=~"$namespace"}[$__range]))
/
sum(increase(loki_canary_entries_total{cluster=~"$cluster", namespace=~"$namespace"}[$__range]))
* 100
```

Abnormal behavior:

- Any non-zero missing rate sustained over time.

## Internal error log rate

Internal logs provide fast context when metrics indicate degradation.

Key metric:

- `loki_internal_log_messages_total`

Use this metric with component logs to correlate where failures begin.

## Retention and sweeper progress

Retention and sweeper lag can cause storage growth and delayed data lifecycle actions.

Key metrics:

- `loki_compactor_apply_retention_last_successful_run_timestamp_seconds`
- `loki_boltdb_shipper_retention_sweeper_marker_file_processing_current_time`
- `loki_boltdb_shipper_retention_sweeper_chunk_deleted_duration_seconds_count`

Example query:

```promql
time() - (loki_boltdb_shipper_retention_sweeper_marker_file_processing_current_time{cluster="$cluster", namespace="$namespace"} > 0)
```

Abnormal behavior:

- Increasing sweeper lag.
- Falling delete throughput or sustained delete failures.

## Next steps

- Install and keep the latest Loki mixin dashboards and alerts.
- Build component-specific alerts on top of these baseline signals.
- Pair metrics with Loki component logs, including `metrics.go`, during incident response.
