---
title: Migrate to Kafka-based ingestion
menuTitle: Migrate to Kafka-based ingestion
description: Migration guide for moving from the direct gRPC write path to Kafka-based ingestion.
weight: 600
keywords:
  - migrate
  - kafka
---

# Migrate to Kafka-based ingestion

{{< admonition type="warning" >}}
Kafka-based ingestion is an [experimental feature](https://grafana.com/docs/release-life-cycle/). Engineering and on-call support is not available. No SLA is provided.
{{< /admonition >}}

This guide explains how to migrate a running Loki cluster from the default gRPC write path to [Kafka-based ingestion](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/kafka/). Kafka-based ingestion adds Kafka, or a Kafka-protocol-compatible system, as a durable buffer between the distributor and the ingester.

## Before you begin

- You need a running Kafka cluster, or a Kafka-protocol-compatible system such as [WarpStream](https://www.warpstream.com/), reachable from both your distributors and your ingesters.
- Create the Kafka topic you plan to use, or set `kafka_config.auto_create_topic_enabled: true` to let Loki create it automatically.
- Read [Kafka-based ingestion](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/kafka/) to understand the architecture, partition sizing, and authentication limitations before you begin.

## Migration steps

### 1. Configure `kafka_config`

Set the Kafka broker addresses and topic name. If your Kafka deployment requires authentication, also set `sasl_username` and `sasl_password`.

```yaml
kafka_config:
  topic: loki-logs
  reader_config:
    address: kafka:9092
  writer_config:
    address: kafka:9092
```

### 2. Enable dual-write

Set `distributor.kafka_writes_enabled: true` while keeping `distributor.ingester_writes_enabled: true` (the default). Also enable `ingester.kafka_ingestion.enabled: true`. With this configuration, the distributor writes every stream to both Kafka and the ingesters over gRPC at the same time, so the write path keeps working as expected even if something is misconfigured in the Kafka path.

```yaml
distributor:
  kafka_writes_enabled: true
  ingester_writes_enabled: true

ingester:
  kafka_ingestion:
    enabled: true
```

### 3. Roll out the configuration change

Apply this configuration to your distributors and ingesters, and roll out the change. Because both write paths remain active, this step does not interrupt ingestion.

### 4. Validate

Monitor the following metrics to confirm that Kafka writes and reads are healthy:

- `loki_distributor_kafka_appends_total` with label `status="success"` should increase at a steady rate.
- `loki_distributor_kafka_latency_seconds` should stay low and stable.
- `loki_kafka_client_partition_reader_consumption_lag_seconds` should stay near zero after an initial catch-up period.
- `loki_ingester_partition_current_offset` should keep advancing.

Only move to the next step after you've confirmed these metrics look healthy for a representative period, for example a few hours or a full day of traffic.

### 5. Cut over to Kafka-only (optional)

If you want to stop writing directly to ingesters over gRPC, set `distributor.ingester_writes_enabled: false`. At least one of `kafka_writes_enabled` or `ingester_writes_enabled` must stay `true`; Loki fails to start otherwise.

```yaml
distributor:
  kafka_writes_enabled: true
  ingester_writes_enabled: false
```

### 6. Monitor ongoing health

Continue monitoring your Kafka-based ingestion using the metrics documented in [Kafka-based ingestion](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/kafka/#monitoring-kafka-ingestion) and [Key metrics for monitoring Loki](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/metrics/#kafka-based-ingestion-experimental).

## Rollback procedure

To revert to the direct gRPC write path:

1. Set `distributor.kafka_writes_enabled: false` and `distributor.ingester_writes_enabled: true`.
1. Disable `ingester.kafka_ingestion.enabled: false`.
1. Roll out the configuration change.

Because the direct gRPC write path never stopped processing writes during dual-write, rolling back doesn't cause data loss. If you had already cut over to Kafka-only writes before deciding to roll back, any data already buffered in Kafka but not yet consumed by ingesters is read with the normal Kafka consumer catch-up process described in [Offset management and restart behavior](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/kafka/#offset-management-and-restart-behavior), so make sure ingesters have caught up on Kafka lag before you disable Kafka consumption.
