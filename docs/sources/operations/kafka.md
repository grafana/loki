---
title: Kafka-based ingestion (Experimental)
menuTitle: Kafka-based ingestion
description: Describes how Grafana Loki can use Kafka, or a Kafka-protocol-compatible system, as a durable buffer on the write path.
weight:
keywords:
  - kafka
  - ingestion
  - write path
---
# Kafka-based ingestion (Experimental)

{{< admonition type="warning" >}}
Kafka-based ingestion is an [experimental feature](https://grafana.com/docs/release-life-cycle/). Engineering and on-call support is not available. No SLA is provided. Configuration options and metric names may change in a future release.
{{< /admonition >}}

Grafana Loki can use Apache Kafka, or a Kafka-protocol-compatible system such as [WarpStream](https://www.warpstream.com/), as an optional durable buffer on the write path. This topic explains what Kafka-based ingestion is, when to use it, how it works, and how to configure and monitor it.

## What it is

By default, the [distributor](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/#distributor) sends log streams directly to [ingesters](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/#ingester) over gRPC. When you enable Kafka-based ingestion, the distributor instead (or also) writes log streams as records to a Kafka topic. Each ingester then reads, or *consumes*, records from one partition of that topic and processes them the same way it processes directly pushed data.

Kafka sits between the distributor and the ingester as a durable, replicated buffer. This decouples the two components so that a slow or restarting ingester doesn't block or fail writes.

Loki uses the [franz-go](https://github.com/twmb/franz-go) Kafka client library. If you need to debug Kafka client behavior in your logs, look for the component name `kafka_client`. Kafka client metrics use the `loki_kafka_client_` prefix.

## When and why to use it

Consider enabling Kafka-based ingestion if you want to:

- Decouple ingestion durability from ingester availability.
- Absorb ingester restarts and rolling updates without blocking or failing writes.
- Automatically replay buffered data from the last committed offset when an ingester restarts, instead of relying only on the ingester's write-ahead log (WAL).
- Scale write throughput independently of ingester replica count, by adding Kafka partitions.

Kafka-based ingestion also underpins the ingest-limits service and the experimental data object builders used for columnar storage. These features require Kafka even if you don't otherwise need the durability benefits above.

If you don't have an existing Kafka deployment and don't need these benefits, you can continue to use the default gRPC write path. Enabling Kafka adds an operational dependency: you must run, or pay for, a Kafka cluster (or a compatible managed service) and monitor it.

## Architecture summary

{{< mermaid >}}
flowchart LR
    A[Distributor] -->|produces records| B[(Kafka topic, partitioned)]
    B -->|partition 0| C1[Ingester A]
    B -->|partition 1| C2[Ingester B]
    B -->|partition N| C3[Ingester N]
    D[Partition ring] -.assigns partition ownership.-> C1
    D -.-> C2
    D -.-> C3
{{< /mermaid >}}

The distributor writes each stream as one or more Kafka records to a topic. The topic has multiple partitions. Each ingester owns exactly one partition and consumes records only from that partition. Ingesters use the [partition ring](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/#kafka-based-ingestion-experimental) to coordinate which ingester owns which partition.

Every ingester process always registers in the classic hash ring it uses for gRPC writes, whether or not Kafka-based ingestion is enabled. When you also enable `ingester.kafka_ingestion.enabled`, that same process additionally joins the partition ring. Loki supports two ways to deploy this:

- **A single, shared ingester pool**: the same ingester replicas serve both the classic hash ring and the partition ring at once. This is the simplest setup, and works well if you're experimenting with Kafka-based ingestion or plan to cut over quickly.
- **Two separate ingester pools**: a new, dedicated pool of Kafka-consuming ingesters runs alongside your existing ingesters, isolated from the classic hash ring by giving it a distinct `ingester.ring.kvstore.prefix`. This is the only path Grafana Labs has tested in production, and is the approach documented in [Migrate to Kafka-based ingestion](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-to-kafka/).

Because the two rings assign ownership of a tenant's streams differently (token-hash with replication, compared to shuffle-sharded partitions with a single owner), the querier has to be told explicitly which ring to use when it looks up ingesters. See [Read path](#read-path) and `querier.query_partition_ingesters` under [Configuration overview](#configuration-overview).

{{< admonition type="note" >}}
You can use any system that implements the Kafka wire protocol, such as WarpStream, instead of running Apache Kafka yourself. Loki only needs the broker address and, optionally, SASL credentials. No code changes are required.
{{< /admonition >}}

## Read path

Enabling Kafka-based ingestion primarily changes the **write path**. [LogQL](https://grafana.com/docs/loki/<LOKI_VERSION>/query/) queries still read recent data from ingester memory and older data from long-term object storage, the same way they do when Kafka-based ingestion is disabled.

However, the querier must be told which ring to use to find the ingesters that hold recent data: the classic hash ring (the default), or the partition ring that Kafka-based ingestion uses. Set `querier.query_partition_ingesters: true` to switch the querier, and any locally evaluating rulers, over to the partition ring. Until you set this, the querier keeps using the classic ring even if the distributor and ingesters are fully configured for Kafka-based ingestion. For the full migration sequence and when to flip this setting, refer to [Migrate to Kafka-based ingestion](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-to-kafka/).

## Configuration overview

This section groups the relevant configuration blocks at a high level. For the full list of fields and their defaults, refer to the [Configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-config-ref/).

- **`kafka_config`**: the Kafka client configuration shared by every Loki component that reads or writes Kafka records. It includes the broker addresses (`reader_config.address` and `writer_config.address`), the topic name, SASL credentials, producer tuning (`producer_linger`, `producer_max_inflight_requests_per_broker`, `producer_max_record_size_bytes`), and consumer tuning (`max_consumer_lag_at_startup`, `consumer_group_offset_commit_interval`, `max_consumer_workers`).
- **`distributor.kafka_writes_enabled`** and **`distributor.ingester_writes_enabled`**: control whether the distributor writes to Kafka, to ingesters over gRPC, or both. At least one must be `true`.
- **`ingester.kafka_ingestion`**: controls whether ingesters consume from Kafka, and configures the partition ring key-value store that ingesters, distributors, queriers, and rulers use to coordinate partition ownership.
- **`querier.query_partition_ingesters`**: controls whether the querier, and any ruler evaluating rules locally, looks up ingesters using the partition ring instead of the classic hash ring. Set this to `true` only after the Kafka-consuming ingesters have caught up and held data for at least `querier.query_ingesters_within` (default: 3 hours); otherwise recent queries can return incomplete results. Defaults to `false`.
- **`limits_config.ingestion_partitions_tenant_shard_size`**: the number of Kafka partitions a single tenant's data is shuffle-sharded across. The default, `0`, means a tenant's data uses all partitions.
- **`ingest_limits`**: configuration for the optional ingest-limits service, which uses its own Kafka topic, partition count, and consumer group to track stream metadata.

## Authentication limitations

The only Kafka authentication mechanism Loki supports today is **SASL PLAIN**, configured with `kafka_config.sasl_username` and `kafka_config.sasl_password`. Both fields must be set together, or both left empty.

Loki has no built-in Transport Layer Security (TLS) or mutual TLS (mTLS) configuration for the Kafka connection. If your organization requires an encrypted connection to Kafka, you must terminate TLS at the network layer, for example with a sidecar proxy or a load balancer in front of your Kafka brokers.

## Partition sizing guidance

The number of Kafka partitions in your topic limits how many ingesters can actively consume from it, because each ingester owns exactly one partition.

Keep the following in mind when you choose a partition count:

- `kafka_config.auto_create_topic_default_partitions` defaults to `1000`. This is a cluster-wide Kafka broker setting (equivalent to Kafka's `num.partitions`), not a per-topic setting, and it only takes effect if you let Loki auto-create the topic.
- The partition count sets the maximum number of ingesters that can actively consume from the topic at once. If you have more ingesters than partitions, the extra ingesters have no partition to own and stay idle for Kafka-based ingestion.
- `limits_config.ingestion_partitions_tenant_shard_size` controls how many partitions a single tenant's streams are shuffle-sharded across. The default, `0`, disables shuffle sharding, so a tenant's data spreads across all partitions.
- Start with more partitions than you expect to need. Partitions are inexpensive to create in Kafka, but reducing the partition count of an existing topic later is difficult and usually requires creating a new topic.

## Offset management and restart behavior

When Kafka-based ingestion is enabled, each ingester follows this sequence to safely resume consuming after a restart:

1. On startup, the ingester fetches the last offset it committed for its partition.
1. If no committed offset exists, for example on first startup, the ingester starts from the earliest available offset.
1. The ingester replays Kafka records from that offset until its consumption lag is within `kafka_config.max_consumer_lag_at_startup` (default: 15 seconds).
1. Only after catching up does the ingester mark itself `ACTIVE` in the partition ring and pass its readiness check.
1. During normal operation, the ingester commits its current offset to Kafka every `kafka_config.consumer_group_offset_commit_interval` (default: 1 second).

Because of this sequence, ingester restarts are safe: any data produced to Kafka while an ingester was down is still in the topic, and the ingester automatically replays it after it comes back up.

## Monitoring Kafka ingestion

This is the full list of Kafka-related metrics, organized by component. For the highest-signal subset of these metrics and example alerting queries, refer to [Key metrics for monitoring Loki](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/metrics/#kafka-based-ingestion-experimental).

**Distributor (producer side):**

- `loki_distributor_kafka_appends_total` (labels: `partition`, `status`): total number of Kafka produce attempts.
- `loki_distributor_kafka_latency_seconds`: end-to-end produce latency.
- `loki_distributor_kafka_sent_bytes_total`: bytes written to Kafka.
- `loki_distributor_kafka_records_per_write_request`: how many Kafka records each incoming push request was split into.

**Ingester (consumer side):**

- `loki_ingester_partition_records_batch_process_duration_seconds`: time to process a batch of Kafka records.
- `loki_ingester_partition_current_offset`: the offset the ingester has most recently consumed. Useful for lag alerting.
- `loki_ingester_partition_push_latency_seconds`: latency of the internal push after consuming a record from Kafka.
- `loki_ingester_partition_consume_workers_count`: number of consumer worker goroutines.

**Kafka client internals (reader and writer):**

- `loki_kafka_client_partition_reader_consumption_lag_seconds` (labels: `phase`, `partition_state`): estimated time the reader is behind the latest produced record. Alert if this grows over time.
- `loki_kafka_client_partition_reader_fetch_errors_total`: errors fetching records from Kafka brokers.
- `loki_kafka_client_partition_reader_fetches_total` and `loki_kafka_client_partition_reader_records_per_fetch`: fetch throughput.
- `loki_kafka_client_partition_reader_offset_commit_requests_total` and `loki_kafka_client_partition_reader_offset_commit_failures_total`: health of offset commits to Kafka.

## Configuration examples

### Dual-write for a safe rollout

Write to both Kafka and ingesters at the same time so you can validate Kafka-based ingestion before relying on it. Queries still use the classic hash ring at this stage (`querier.query_partition_ingesters` stays `false`).

```yaml
distributor:
  kafka_writes_enabled: true
  ingester_writes_enabled: true

ingester:
  kafka_ingestion:
    enabled: true

kafka_config:
  topic: loki-logs
  reader_config:
    address: kafka:9092
  writer_config:
    address: kafka:9092
```

### Kafka-only write path

Once you've validated dual-write and switched queriers to the partition ring, you can disable the direct gRPC writes to ingesters. Refer to [Migrate to Kafka-based ingestion](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-to-kafka/) for the full, safely ordered sequence.

```yaml
distributor:
  kafka_writes_enabled: true
  ingester_writes_enabled: false

ingester:
  kafka_ingestion:
    enabled: true

querier:
  query_partition_ingesters: true

kafka_config:
  topic: loki-logs
  reader_config:
    address: kafka:9092
  writer_config:
    address: kafka:9092
```

### Managed Kafka with SASL authentication

This example connects to a managed Kafka-protocol-compatible service, such as WarpStream, using SASL PLAIN authentication and separate reader and writer broker addresses.

```yaml
distributor:
  kafka_writes_enabled: true
  ingester_writes_enabled: true

ingester:
  kafka_ingestion:
    enabled: true

kafka_config:
  topic: loki-logs
  reader_config:
    address: reader.kafka.example.com:9092
  writer_config:
    address: writer.kafka.example.com:9092
  sasl_username: ${KAFKA_SASL_USERNAME}
  sasl_password: ${KAFKA_SASL_PASSWORD}
```

## Links

- [Migrate to Kafka-based ingestion](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-to-kafka/)
- [Configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/loki-config-ref/)
- [Troubleshoot Kafka integration errors](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/troubleshooting/troubleshoot-operations/#kafka-integration-errors)
