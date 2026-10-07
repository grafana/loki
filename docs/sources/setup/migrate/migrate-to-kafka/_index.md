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

{{< admonition type="note" >}}
Loki reuses your existing ingester replicas for Kafka-based ingestion. Enabling Kafka-based ingestion doesn't create a separate set of "Kafka ingesters": each existing ingester process joins a second ring, the partition ring, in addition to the hash ring it already uses for gRPC writes. There's no separate ingester deployment to create and no old ingester deployment to delete when you finish migrating.
{{< /admonition >}}

## Before you begin

- You need a running Kafka cluster, or a Kafka-protocol-compatible system such as [WarpStream](https://www.warpstream.com/), reachable from both your distributors and your ingesters.
- Create the Kafka topic you plan to use, or set `kafka_config.auto_create_topic_enabled: true` to let Loki create it automatically.
- Read [Kafka-based ingestion](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/kafka/) to understand the architecture, partition sizing, and authentication limitations before you begin.
- Note your current `querier.query_ingesters_within` setting (default: 3 hours). You'll need it in [step 3](#3-wait-for-the-kafka-path-to-catch-up).

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

Set `distributor.kafka_writes_enabled: true` while keeping `distributor.ingester_writes_enabled: true` (the default), and set `ingester.kafka_ingestion.enabled: true`. Set both in the same configuration change: Loki fails to start if `kafka_writes_enabled` is `true` on the distributor but `kafka_ingestion.enabled` is `false` on the ingester.

With this configuration, the distributor writes every stream to both Kafka and the ingesters over gRPC at the same time, and each ingester joins the partition ring and starts consuming its assigned partition. The write path keeps working as expected even if something is misconfigured in the Kafka path.

```yaml
distributor:
  kafka_writes_enabled: true
  ingester_writes_enabled: true

ingester:
  kafka_ingestion:
    enabled: true
```

Apply this configuration to your distributors and ingesters, and roll out the change. Because both write paths remain active, this step does not interrupt ingestion.

### 3. Wait for the Kafka path to catch up

Monitor the following metrics to confirm that Kafka writes and reads are healthy:

- `loki_distributor_kafka_appends_total` with label `status="success"` should increase at a steady rate.
- `loki_distributor_kafka_latency_seconds` should stay low and stable.
- `loki_kafka_client_partition_reader_consumption_lag_seconds` should stay near zero after an initial catch-up period.
- `loki_ingester_partition_current_offset` should keep advancing.

Beyond checking that lag is low, you also need the Kafka-consuming ingesters to have been running long enough to hold a full window of recent data. The querier only queries ingesters for data within the last `querier.query_ingesters_within` (default: 3 hours). If you move queries to the partition ring (the next step) before the partition-ring ingesters have accumulated at least this much data, recent queries can return incomplete results until that window fills in.

Only move to the next step after both of these are true:

- The metrics above have looked healthy for a representative period, for example a few hours or a full day of traffic.
- The time since you enabled Kafka ingestion is at least as long as your `querier.query_ingesters_within` setting.

### 4. Switch the query path to the partition ring

Set `querier.query_partition_ingesters: true` and roll out your queriers (and rulers, if they evaluate rules locally). This tells the querier to look up ingesters using the partition ring instead of the classic hash ring, so it finds the ingester that actually owns each tenant's data under Kafka-based ingestion.

```yaml
querier:
  query_partition_ingesters: true
```

{{< admonition type="note" >}}
The partition ring routes each read to the single ingester that owns the partition, with no replication. The classic hash ring instead fans a read out to multiple replicas and uses quorum. If a partition's owning ingester is temporarily unavailable, recent reads for that partition's tenants can fail until the ingester recovers. During dual-write, you can safely revert this setting to `false` if you need to roll back.
{{< /admonition >}}

Before continuing, confirm that queries covering recent time ranges, for example the last few minutes, still return the data you expect.

### 5. Cut over to Kafka-only writes (optional)

Once queries are reading from the partition ring, you can stop writing directly to ingesters over gRPC. Set `distributor.ingester_writes_enabled: false`. At least one of `kafka_writes_enabled` or `ingester_writes_enabled` must stay `true`; Loki fails to start otherwise.

```yaml
distributor:
  kafka_writes_enabled: true
  ingester_writes_enabled: false
```

### 6. Monitor ongoing health

Continue monitoring your Kafka-based ingestion using the metrics documented in [Kafka-based ingestion](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/kafka/#monitoring-kafka-ingestion) and [Key metrics for monitoring Loki](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/metrics/#kafka-based-ingestion-experimental).

## Rollback procedure

To revert to the direct gRPC write path:

1. If you completed [step 5](#5-cut-over-to-kafka-only-writes-optional), set `distributor.ingester_writes_enabled` back to `true`.
1. If you completed [step 4](#4-switch-the-query-path-to-the-partition-ring), set `querier.query_partition_ingesters` back to `false`. If you also completed step 5, make this change and step 1 in the **same configuration change** and roll out distributors and queriers together. Reverting the querier setting before gRPC writes are re-enabled can create a window where recent queries find no new data.
1. Set `distributor.kafka_writes_enabled` to `false`.
1. Set `ingester.kafka_ingestion.enabled` to `false`.
1. Roll out the configuration change.

Because the direct gRPC write path never stopped processing writes during dual-write, rolling back doesn't cause data loss. If you had already cut over to Kafka-only writes before deciding to roll back, any data already buffered in Kafka but not yet consumed by ingesters is read with the normal Kafka consumer catch-up process described in [Offset management and restart behavior](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/kafka/#offset-management-and-restart-behavior), so make sure ingesters have caught up on Kafka lag before you disable Kafka consumption.
