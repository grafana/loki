---
title: Grafana Loki
description: Grafana Loki is a set of open source components that can be composed into a fully featured logging stack.
aliases:
  - /docs/loki/
weight: 100
cascade:
  GRAFANA_VERSION: latest
hero:
  title: Grafana Loki
  level: 1
  image: /media/docs/loki/logo-grafana-loki.png
  width: 110
  height: 110
  description: Grafana Loki is a set of open source components that can be composed into a fully featured logging stack. A small index and highly compressed chunks simplifies the operation and significantly lowers the cost of Loki.
cards:
  title_class: pt-0 lh-1
  items:
    - title: Learn about Loki
      href: /docs/loki/latest/get-started/
      description: Learn about the Loki architecture and components, the various deployment modes, and best practices for labels.
    - title: Set up Loki
      href: /docs/loki/latest/setup/
      description: View instructions for how to configure and install Loki, migrate from previous deployments, and upgrade your Loki environment.
    - title: Configure Loki
      href: /docs/loki/latest/configure/
      description: View the Loki configuration reference and configuration examples.
    - title: Send logs to Loki
      href: /docs/loki/latest/send-data/
      description: Select one or more clients to use to send your logs to Loki.
    - title: Manage Loki
      href: /docs/loki/latest/operations/
      description: Learn how to manage tenants, log ingestion, storage, queries, and more.
    - title: Query with LogQL
      href: /docs/loki/latest/query/
      description: Inspired by PromQL, LogQL is Grafana Loki’s query language. LogQL uses labels and operators for filtering.
---

{{< docs/hero-simple key="hero" >}}

---

## Overview

Unlike other logging systems, Loki is built around the idea of only indexing metadata about your logs' labels (just like Prometheus labels).
Log data itself is then compressed and stored in chunks in object stores such as Amazon Simple Storage Service (S3) or Google Cloud Storage (GCS), or even locally on the filesystem.

## Explore

{{< card-grid key="cards" type="simple" >}}

## Grafana Loki, frequently asked questions

Here are answers to some frequently asked questions about how to configure and run Loki.

### What are the best practices for Loki labels vs. structured metadata?

Loki's indexing model differs significantly from other observability products. Choosing the wrong fields as labels is the most common cause of poor query performance and high resource usage.

**Labels** define a log stream. Every unique combination of label values creates a new stream, which is stored and indexed separately.

- ✅ Use labels for **low-cardinality** dimensions you will always filter on: `env`, `cluster`, `namespace`, `app`, `job`.
- ❌ Do not use labels for **high-cardinality** values: request IDs, user IDs, trace IDs, HTTP status codes, IP addresses. Each unique value creates a new stream, causing "cardinality explosion" that degrades ingestion and query performance.

**Structured metadata** (introduced in Loki 3.0) allows attaching key-value pairs to log entries without creating new streams. Use it for values you want to filter or display but that are too high-cardinality for labels:

```alloy
// Example: attaching trace_id as structured metadata via Alloy
loki.process "default" {
  stage.json {
    expressions = { "trace_id" = "" }
  }

  stage.structured_metadata {
    values = { "trace_id" = "" }
  }

  forward_to = [loki.write.local.receiver]
}

loki.write "local" {
  endpoint {
    url = "http://loki:3100/loki/api/v1/push"
  }
}
```

**Parsed fields** (extracted at query time with `| json`, `| logfmt`, `| pattern`, `| regexp`) require no schema changes and add no indexing overhead. Prefer them for ad-hoc filtering on log content.

**Rule of thumb:** If you would filter by it in _every_ query, it belongs in a label. If you only need it occasionally or it has many unique values, use structured metadata or parse it from the log line at query time.

### Why does `service_name` show as `unknown_service` in Explore Logs?

Loki's Explore Logs feature uses a `service_name` label to group and navigate logs. If it shows `unknown_service`, Loki could not automatically determine a service name from the incoming stream.

If a `service_name` label is already present on the stream, Loki uses it directly. Otherwise, Loki's `discover_service_name` feature looks for the first non-empty value from these label keys, in order:

- service
- app
- application
- app_name
- name
- app_kubernetes_io_name
- container
- container_name
- k8s_container_name
- component
- workload
- job
- k8s_job_name

**Common causes:**

- The log shipper (Alloy, OTel Collector, kube-logging-operator) is not forwarding Kubernetes metadata as stream labels.
- The label name uses dot notation (`service.name`). Loki normalizes label names server-side, replacing dots with underscores (for example, `service.name` becomes `service_name`).
- `discover_service_name` is disabled in your Loki configuration.

**Verify by checking your stream labels** in Explore. If none of the above keys are present as labels, configure your log shipper to include them. For Alloy, ensure `loki.source.kubernetes` or `discovery.kubernetes` is passing through pod metadata.

### How do I fix "ingestion rate limit exceeded" (HTTP 429) errors?

A 429 error from Loki means an ingestion rate limit has been hit. Loki enforces limits both globally and per-stream.

**Global ingestion limits** (applied per tenant):

```yaml
limits_config:
  ingestion_rate_mb: 16        # default: 4 MB/s
  ingestion_burst_size_mb: 32  # default: 6 MB
```

**Per-stream rate limit:**

```yaml
limits_config:
  per_stream_rate_limit: 5MB        # default: 3MB
  per_stream_rate_limit_burst: 15MB # default: 15MB (5x the rate limit)
```

**To identify which streams are responsible**, check the error message in your log shipper. It includes the stream labels of the offending stream. You can also query Loki's metrics endpoint for `loki_ingester_streams_created_total` broken down by tenant.

If you are self-hosting and consistently hitting limits with legitimate log volume, increase the values above in `limits_config`. If you are on Grafana Cloud, contact support to adjust your plan limits.

{{< admonition type="note" >}}
Increasing limits without investigating root cause can mask runaway log producers. Always check whether a specific workload or namespace is generating an unexpected spike before raising limits.
{{< /admonition >}}

### Why can't Grafana connect to Loki when both are running in Docker?

When Grafana and Loki run as separate Docker containers, `localhost` inside the Grafana container refers to the Grafana container itself, not the host machine or the Loki container. This is the most common misconfiguration for new users.

**Correct datasource URL in Docker Compose:**

```yaml
# docker-compose.yml
services:
  loki:
    image: grafana/loki:latest
    ports:
      - "3100:3100"

  grafana:
    image: grafana/grafana:latest
    environment:
      - GF_DATASOURCES_DEFAULT_URL=http://loki:3100
```

In Grafana's datasource settings, use `http://loki:3100` (the Docker Compose service name) instead of `http://localhost:3100`.

**For Kubernetes deployments**, use the in-cluster service DNS name:

```
http://loki.monitoring.svc.cluster.local:3100
```

**Verify connectivity** by exec-ing into the Grafana container and running:

```bash
curl http://loki:3100/ready
```

If this returns `ready`, the networking is correct and the issue lies in the Grafana datasource configuration itself.

### What causes "Too many outstanding requests" errors on my dashboard?

This error occurs when the number of concurrent queries to Loki exceeds the configured limit for a tenant. It is most common when a dashboard has four or more panels querying over a long time range against a single-node (monolithic) deployment.

**Key configuration knobs:**

```yaml
query_scheduler:
  max_outstanding_requests_per_tenant: 32000  # default: 32000

frontend:
  max_outstanding_per_tenant: 2048            # default: 2048

limits_config:
  split_queries_by_interval: 24h              # default: 1h
```

Splitting queries by interval reduces the load per request by breaking large time-range queries into smaller parallel chunks. For single-node deployments under sustained dashboard load, also consider increasing chunk sizes to reduce the total number of chunk reads:

```yaml
ingester:
  chunk_target_size: 1572864
  max_chunk_age: 2h
  chunk_idle_period: 30m
```

If the issue persists at scale, consider moving from a monolithic deployment to [microservices](https://grafana.com/docs/loki/latest/get-started/deployment-modes/) mode.

### How do I make a LogQL query return zero instead of "no data"?

By default, LogQL metric queries return no data points for intervals where no log lines matched, rather than returning `0`. This breaks percentage calculations and alert rules that expect a numeric baseline.

**Workaround using `or on() vector(0)`** (mirrors the PromQL pattern):

```logql
(
  sum(count_over_time({app="my-app"} |= "error" [5m]))
  or on() vector(0)
)
```

**For ratio/percentage queries**, wrap both the numerator and denominator:

```logql
(
  sum(count_over_time({app="my-app"} | json | status=~"5.." [5m]))
  or on() vector(0)
)
/
(
  sum(count_over_time({app="my-app"} [5m]))
  or on() vector(0)
)
```

**For alerting**, this pattern is essential. Without it, an alert rule using `count_over_time` produces no evaluation result (not `0`) during quiet periods, which can cause alerts to flap or never resolve correctly.

### Why isn't my log retention policy deleting old data?

Retention in Loki is handled by the **Compactor**, not by the ingester or storage backend directly. A few common reasons why old data persists even after configuring a retention period:

- `retention_enabled: true` must be set under the `compactor` block, not just in `limits_config`.
- The Compactor must be running. In monolithic deployments it is sometimes inadvertently disabled.
- Deletion is not immediate. The Compactor runs on a schedule and only marks chunks for deletion after a configurable grace period (`retention_delete_delay`).
- When using filesystem storage, the chunks directory size may not visibly decrease right away. The Compactor marks files for deletion first, then removes them on subsequent runs.

**Minimum required configuration:**

```yaml
compactor:
  working_directory: /data/loki/compactor
  retention_enabled: true
  delete_request_store: <your-object-store>

limits_config:
  retention_period: 360h
```

To verify the Compactor is running and retention is active, check the Loki logs for lines referencing `compactor` and look for the metrics `loki_boltdb_shipper_compact_tables_operation_total` (compaction runs) and `loki_compactor_apply_retention_operation_total` (retention runs).
