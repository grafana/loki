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
This guide documents running **two distinct sets of ingesters** side by side during the migration: your existing ingesters, unchanged, and a new set of Kafka-consuming ingesters. This is the only migration path Grafana Labs has tested. Because both sets run at the same time, you need enough extra capacity to run a second, full-sized ingester group for at least `querier.query_ingesters_within` (default: 3 hours), and often longer.
{{< /admonition >}}

## Before you begin

- You need a running Kafka cluster, or a Kafka-protocol-compatible system such as [WarpStream](https://www.warpstream.com/), reachable from both your distributors and your new Kafka-consuming ingesters.
- Create the Kafka topic you plan to use. `kafka_config.auto_create_topic_enabled` defaults to `true`, which allows the broker to create a missing topic when Loki first uses it, if the broker permits topic auto-creation. Loki also sets the broker-wide `num.partitions` default to `kafka_config.auto_create_topic_default_partitions` (default: 1000), which affects every auto-created topic on the cluster. Set `auto_create_topic_enabled: false` if you create the topic yourself or share the cluster with other applications.
- Read [Kafka-based ingestion](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/kafka/) to understand the architecture, partition sizing, and authentication limitations before you begin.
- Note your current `querier.query_ingesters_within` setting (default: 3 hours). You'll need it in [step 4](#4-wait-for-the-kafka-path-to-catch-up).
- Plan for enough compute and storage capacity to run a second, full-sized ingester group alongside your existing ingesters for the duration of the migration.
- Make sure your Kafka topic has at least as many partitions as the number of Kafka-consuming ingester replicas you plan to run. Each ingester owns exactly one partition. Loki doesn't check the topic's partition count. An ingester whose partition doesn't exist keeps retrying the Kafka offset lookup for it, so it never becomes ready and never joins the partition ring. Refer to [Partition sizing guidance](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/kafka/#partition-sizing-guidance).
- Plan to give each new Kafka-consuming ingester an instance ID that ends in a hyphen followed by a sequential numeric index, for example `kafka-ingester-0`, `kafka-ingester-1`. Loki derives each ingester's partition ID from this suffix at startup, and fails to start if it can't parse one. Kubernetes StatefulSet ordinals satisfy this automatically. Don't use instance IDs that contain `local` or end in `.lan`: Loki always maps these to partition 0, regardless of any numeric suffix.

## Migration steps

### 1. Configure `kafka_config`

Set the Kafka broker addresses and topic name. If your Kafka deployment requires authentication, also set `sasl_username` and `sasl_password`.

This block is required on both the distributor (which produces records) and the new Kafka-consuming ingesters (which consume records). If your queriers share a base configuration file with your ingesters, no further action is needed for the querier; otherwise, see [step 5](#5-switch-the-query-path-to-the-partition-ring) for the partition ring settings the querier also needs.

```yaml
kafka_config:
  topic: loki-logs
  reader_config:
    address: kafka:9092
  writer_config:
    address: kafka:9092
```

### 2. Deploy Kafka-consuming ingesters

Deploy a new, independently-scaled group of ingesters dedicated to consuming from Kafka. Size this group for your full ingestion volume, because it eventually takes over completely from your existing ingesters.

Apply the following configuration to this new group only:

```yaml
# Kafka-consuming ingester config (new ingester group)
ingester:
  kafka_ingestion:
    enabled: true
  lifecycler:
    ring:
      kvstore:
        prefix: kafka-ingesters/ # any value different from the default, "collectors/"
```

- `ingester.kafka_ingestion.enabled: true` makes this group join the partition ring and start consuming from its assigned Kafka partitions.
- `ingester.lifecycler.ring.kvstore.prefix` isolates this group's registration in the classic hash ring from your existing ingesters. Every ingester, including Kafka-consuming ones, always registers in the classic hash ring. Giving the new group a different key-value store prefix keeps the distributor's gRPC write path from discovering or routing to them, so they don't receive duplicate gRPC writes. It also keeps the querier's classic-ring lookups from discovering them until [step 5](#5-switch-the-query-path-to-the-partition-ring), where you point the querier at the same prefix.

Leave your existing ingesters' configuration unchanged at this point: `kafka_ingestion.enabled` stays `false` (the default), and they keep the default `ingester.lifecycler.ring.kvstore.prefix`.

{{< admonition type="note" >}}
Every component that participates in the partition ring, the distributor, the Kafka-consuming ingesters, and the querier, must use the same `ingester.kafka_ingestion.partition_ring.kvstore` configuration. Only the classic ring prefix above is meant to differ. Don't change the other `partition_ring` settings from their defaults.

The partition ring key-value store defaults to `memberlist`. If you don't already run memberlist, configure `memberlist.join_members` on these components or set the same `partition_ring.kvstore` store on all of them. If you set `common.ring`, the partition ring uses the key-value store from there.

The existing ingesters don't join the partition ring, so they don't need this configuration.
{{< /admonition >}}

Roll out this new ingester group before you enable any Kafka writes in the next step, so you can confirm the new ingesters start cleanly and join the partition ring while receiving no traffic yet.

### 3. Enable dual-write

On your distributors, set `distributor.kafka_writes_enabled: true` while keeping `distributor.ingester_writes_enabled: true` (the default).
Additionally, set `ingester.kafka_ingestion.enabled: true` while keeping it `false` on the existing ingester group. This means that the old ingester group and the distributors cannot share the same config file. (Not setting this would cause the error `kafka is enabled in distributor but not in ingester` on startup.)

```yaml
# Distributor config
distributor:
  kafka_writes_enabled: true
  ingester_writes_enabled: true
ingester:
  kafka_ingestion:
    enabled: true
```

With this configuration, the distributor writes every stream to both Kafka, which your new Kafka-consuming ingesters read from, and to your existing ingesters over gRPC, at the same time. A push request succeeds only if both writes succeed. If the Kafka write fails, for example because Kafka is unreachable or the partition ring has no `ACTIVE` partition, the distributor returns an HTTP 500 error to the client, even though your existing ingesters still received the data.

Before you roll out this change, confirm on the `/partition-ring` page of a distributor or ingester that all new partitions are `ACTIVE`. Roll the change out to your distributors gradually, and watch `loki_distributor_kafka_appends_total` with label `status="fail"` and your push error rate. To undo this step, set `kafka_writes_enabled: false` again.

### 4. Wait for the Kafka path to catch up

Monitor the following metrics to confirm that Kafka writes and reads are healthy:

- `loki_distributor_kafka_appends_total` with label `status="success"` should increase at a steady rate.
- `loki_distributor_kafka_latency_seconds` should stay low and stable.
- `loki_kafka_client_partition_reader_consumption_lag_seconds` should stay near zero after an initial catch-up period.
- `loki_ingester_partition_current_offset` on your new Kafka-consuming ingesters should keep advancing.

Beyond checking that lag is low, you also need your dual-write to have been running long enough to hold a full window of recent data. The querier only queries ingesters for data within the last `querier.query_ingesters_within` (default: 3 hours). If you move queries to the partition ring (the next step) before your Kafka-consuming ingesters have accumulated at least this much data, recent queries can return incomplete results until that window fills in.

{{< admonition type="note" >}}
Keep both ingester groups running for this entire step. Budget at least `querier.query_ingesters_within` (default: 3 hours) for this wait, and often longer, since you want a representative period of healthy metrics in addition to the minimum data-retention window.
{{< /admonition >}}

Only move to the next step after both of these are true:

- The metrics above have looked healthy for a representative period, for example a few hours or a full day of traffic.
- The time since dual-write was fully rolled out to all distributors is at least as long as your `querier.query_ingesters_within` setting.

### 5. Switch the query path to the partition ring

Set `querier.query_partition_ingesters: true` and roll out your queriers (and rulers, if they evaluate rules locally). This tells the querier to look up ingesters using the partition ring instead of the classic hash ring, so it finds your new Kafka-consuming ingesters instead of your existing ones.

```yaml
# Querier and ruler config
querier:
  query_partition_ingesters: true
ingester:
  lifecycler:
    ring:
      kvstore:
        prefix: kafka-ingesters/ # Same as in new ingester config
```

The querier also needs the same `ingester.kafka_ingestion.partition_ring` key-value store configuration as your Kafka-consuming ingesters, so it can watch the partition ring and discover them. If your querier already shares a base configuration file with your ingesters, this is already in place; otherwise, add it explicitly.

{{< admonition type="note" >}}
The partition ring routes each read to the single ingester that owns the partition, with no replication. The classic hash ring instead fans a read out to multiple replicas and uses quorum. If a partition's owning ingester is temporarily unavailable, recent reads can fail until the ingester recovers. With the default `limits_config.ingestion_partitions_tenant_shard_size: 0`, every query covers all partitions, so this affects all tenants. With a non-zero shard size, it affects only the tenants whose shard includes that partition. During dual-write, you can safely revert this setting and the ring prefix to their original values if you need to roll back.
{{< /admonition >}}

Before continuing, confirm that queries covering recent time ranges, for example the last few minutes, still return the data you expect.

### 6. Cut over to Kafka-only writes

Once queries are reading from your Kafka-consuming ingesters, you can stop writing directly to your existing ingesters over gRPC. On your distributors, set `ingester_writes_enabled: false`. At least one of `kafka_writes_enabled` or `ingester_writes_enabled` must stay `true`; Loki fails to start otherwise.

```yaml
# Distributor config
distributor:
  kafka_writes_enabled: true
  ingester_writes_enabled: false
```

Your existing ingesters stop receiving new writes after this change, but keep them running. Don't delete them yet; you still need them for a safe rollback until you complete the next step.

### 7. Decommission the original ingesters

After you've confirmed that reads and writes are fully and reliably served by your Kafka-consuming ingesters, for example after a day of stable operation with no errors, scale down and remove your original ingester deployment.

Keep your original ingesters' configuration and deployment definitions available for a buffer period after you remove them, in case you need to roll back.

### 8. Monitor ongoing health

Continue monitoring your Kafka-based ingestion using the metrics documented in [Kafka-based ingestion](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/kafka/#monitoring-kafka-ingestion) and [Key metrics for monitoring Loki](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/metrics/#kafka-based-ingestion-experimental).

## Rollback procedure

To revert to the direct gRPC write path:

1. If you completed [step 6](#6-cut-over-to-kafka-only-writes), set `distributor.ingester_writes_enabled` back to `true` and roll out your distributors. Leave `distributor.kafka_writes_enabled` and the querier settings unchanged for now. Then wait at least `querier.query_ingesters_within` (default: 3 hours). During this wait, your existing ingesters build up a full window of recent data while queries still read from the Kafka-consuming ingesters. If you switch queries back earlier, recent queries miss the data that was written only to Kafka. This wait is the slow part of a rollback. If you didn't complete step 6, skip this item, because your existing ingesters received every write.
1. If you completed [step 5](#5-switch-the-query-path-to-the-partition-ring), set `querier.query_partition_ingesters` back to `false` and change `ingester.lifecycler.ring.kvstore.prefix` back to its original value on your queriers (and rulers). Roll out these changes.
1. Set `distributor.kafka_writes_enabled` to `false` and roll out your distributors. Do this after the queriers no longer read from the partition ring, because the Kafka-consuming ingesters receive no new data after this change.
1. Once you've confirmed your existing ingesters are serving all reads and writes correctly again, scale down your Kafka-consuming ingesters. Because they're isolated from the classic hash ring, you can leave some of them running for a while in case you want to retry the migration, without affecting your existing ingesters. Before you stop each Kafka-consuming ingester that you remove:
   1. Send `POST /ingester/prepare_partition_downscale` to it. This switches its partition to `INACTIVE`, so distributors stop writing to it. If you stop an ingester without this call, its partition stays `ACTIVE` with no consumer, and queries on the partition ring fail if you retry the migration later.
   1. Send `POST /ingester/prepare_shutdown` to it, then stop it. This removes the ingester as partition owner. The remaining Kafka-consuming ingesters delete an `INACTIVE` partition that has no owners from the partition ring after `ingester.kafka_ingestion.partition_ring.delete_inactive_partition_after` (default: 13 hours). The ingester writes a shutdown marker file (see `ingester.shutdown_marker_path`). Before you reuse the ingester, remove the marker with `DELETE /ingester/prepare_shutdown`. Refer to [Prepare ingester shutdown](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-http-api/#prepare-ingester-shutdown).

Because your existing ingesters never stopped processing gRPC writes during dual-write, rolling back doesn't cause data loss as long as you haven't completed [step 6](#6-cut-over-to-kafka-only-writes). If you had already cut over to Kafka-only writes before deciding to roll back, any data already buffered in Kafka but not yet consumed is read with the normal Kafka consumer catch-up process described in [Offset management and restart behavior](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/kafka/#offset-management-and-restart-behavior), so make sure your Kafka-consuming ingesters have caught up on Kafka lag before you disable Kafka consumption on them.

{{< admonition type="warning" >}}
Rollback is straightforward only while your original ingesters are still running. Once you've decommissioned them in step 7, rolling back requires redeploying them from scratch. Freshly redeployed ingesters start with no recent data in memory, so queries for recent time ranges return incomplete results until they rebuild state, either by replaying their write-ahead log (WAL) or by accumulating new data over another `querier.query_ingesters_within` window. Don't decommission your original ingesters until you're confident you won't need to roll back.
{{< /admonition >}}
