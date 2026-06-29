---
title: Support Kafka-based Ingest Storage Architecture
authors:
  - @JoaoBraveCoding
reviewers: 
  - @xperimental
  - @btaani
creation-date: 2026-04-06
last-updated: 2026-04-06
tracking-link: 
  - https://redhat.atlassian.net/browse/LOG-7377
see-also:
  - Initial working LokiStack + Kafka PoC:  https://github.com/grafana/loki/compare/main...JoaoBraveCoding:loki:LOG-7377-poc
  - API Implementation + Strimzi resources: https://github.com/grafana/loki/compare/main...JoaoBraveCoding:loki:LOG-7377-api
replaces:
  - 
superseded-by:
  - 
---

## Summary

Since the beginning of 2025, the Loki team has been working to align Loki with the new Mimir architecture around [Kafka/Warpstream](https://grafana.com/docs/mimir/latest/get-started/about-grafana-mimir-architecture/about-ingest-storage-architecture/).
The new architecture is an improvement over the current model as it:

- Increases reliability and resilience in the event of sudden spikes in query traffic or ingest volume
- Decouples read and write paths to prevent performance interference

The goal of this enhancement is to help the team define a path for the operator to migrate from the classical architecture to the new ingest storage architecture.

## Motivation

We should migrate to the new ingest storage architecture as this will not only bring to our users the improvements mentioned before, but it will also ensure the operator continues on the supported path. The Grafana Loki team has not officially commented on the deprecation of the classical architecture but we suspect this will come once Loki moves to release 4.0. There is a chance the classical architecture might continue to exist but it will mainly be maintained by members of the Loki community.

### Goals

Introduce API extensions that allow users to:

- Remain on the classical architecture
- Adopt the new ingest storage architecture

### Non-Goals

- Bundle a Kafka deployment into the operator.
- Adopt the new `DataObj` format. The `DataObj` format requires the ingest storage architecture but is not a prerequisite for adopting it. Because of this, `DataObj` adoption should happen in a separate enhancement proposal to reduce the complexity of this one.

## Proposal

### Context

#### Kafka Taxonomy

Apache Kafka is a distributed event streaming platform. At its core, it is a durable, ordered, append-only log. Producers write records to the end of the log, and consumers read from it at their own pace. Records are not deleted after consumption, they are retained for a configurable period, which means multiple consumers can read the same data independently.

In Loki's context, Kafka acts as a buffer and durable write-ahead log between the distributor and the ingester. This decouples the write acknowledgment from ingester availability.

Some important Kafka concepts to better understand this enhancement are:

- Broker: A Kafka server process. A Kafka cluster consists of one or more brokers. Each broker stores a subset of the data and serves client requests. In production, you typically run 3+ brokers for fault tolerance.

- Topic: A named category or feed to which records are published. A topic is a logical grouping, the actual data is split across partitions.

- Partition: Partitions are ordered, immutable sequences of records. Partitions are the unit of parallelism: each partition can be consumed by exactly one consumer in a consumer group. More partitions = more parallelism.

- Record: A single unit of data written to a partition. It consists of a key, a value (the payload), a timestamp, and optional headers.

- Offset: A sequential ID assigned to each record within a partition. Consumers track their position using offsets. Offsets are committed to Kafka by consumers. This is how Kafka enables replay.

- Producer: A client that writes records to a partition on a topic. In Loki, each distributor is a producer.

- Consumer / Consumer Group: A client that reads records from partitions. A consumer group is a set of consumers that coordinate to divide partitions among themselves -- each partition is consumed by exactly one member of the group. In Loki, each ingester is a consumer that uses its own consumer group (by default, the ingester's instance ID, e.g. `ingester-zone-a-0`). This means each ingester tracks its own offset independently — in a multi-zone deployment, all zone ingesters consume the same partition in parallel, each at their own pace, because they belong to different consumer groups.

#### Classical Architecture VS Ingest Storage Architecture

In this section we will briefly compare the two architectures to better understand the differences both in the write path and in the read path. However, first we will introduce the partition ring concept which is essential to understand the new architecture.

##### Partition Ring

In the new ingest storage architecture there is a new membership ring called partition ring. The partition ring wraps the ingester ring for heartbeat/liveness checks and each ingester maintains dual ring membership. The new partition ring tracks Kafka partitions and which ingester owns each one.

- On a single zone deployment: each ingester maps to a single partition using their hostname. E.g `ingester-0 --> partition 0`
- On multi zone deployments: ingesters in different zones map to the same partition, using their hostname. E.g `ingester-zone-a-0, ingester-zone-b-0, ingester-zone-c-0 --> partition 0`. All zone ingesters for the same partition consume from it independently, each using their own Kafka consumer group, and each committing offsets at their own pace.

When distributors (producers) want to write they hash the stream-labels and use the partition ring to find the active partition for that hash. Once they know which partition they should write to, distributors set the `Partition` field in the record and send the record to Kafka. Note that by setting the `Partition` field Loki is actively controlling how information is distributed instead of relying on Kafka.

The partition ring also supports shuffle sharding for tenant isolation -- instead of spreading a tenant's data across all partitions, each tenant can be assigned a small subset of partitions.

Kafka Topic Partition Count: Loki can set partition count automatically, but Kafka 4.x no longer allows the number of partitions to be changed dynamically. This means the Kafka administrator must either pre-create the topic with the desired partition count or configure them afterwards before Loki starts. The partition count determines the maximum number of ingesters, this is a prerequisite that in principle the operator will not be able to automate and should be documented. Note that Loki has no mechanism to handle Kafka reducing the number of partitions — if a partition that Loki is actively using is removed, writes and consumption for that partition will fail.

##### Write Path

In the classical architecture, a distributor would write to a set of ingesters organized in a hash ring. A distributor determined the ingesters it should write to by hashing the stream-labels in a log stream and then consulting the ingester ring. Once a quorum of ingesters confirms the write, the distributor moves on. Ingesters then collect log entries and eventually flush them to object storage as chunks with TSDB index entries. If an ingester dies, the ingester ring detects it via heartbeat timeout and the remaining ingesters take over its token ranges. If not enough ingesters remain to form a quorum, ingestion will stop. Distributors are independent from each other but have to sync with ingesters. Ingesters are independent of each other.
Ingesters are able to enforce per-tenant limits since they receive streams directly from distributors.
The classical architecture ensured no data was lost via replication, a distributor would write to multiple ingesters.

On the ingest storage architecture, each ingester joins both the classical ingester ring (for liveness and query routing) and the new partition ring (for Kafka partition assignment).
So in the new architecture, the distributors (producers) write records (one or more serialized log streams) to Kafka partitions.
Once Kafka confirms persistence of the record, the distributor acknowledges the write back to the client.
Ingesters (consumers) read records from their partition and periodically commit their offset back to Kafka under their own consumer group. On startup, an ingester fetches the last committed offset for its consumer group and resumes consumption from that point, allowing seamless recovery after restarts or replacements.
Ingesters collect log entries and eventually flush them to object storage as chunks with TSDB index entries. The TSDB index format remains the same as in the classical architecture.
In the new architecture, ingesters are no longer in the write path. To enforce per-tenant ingestion limits, Loki introduces an optional `ingest-limits` component that the distributor queries before accepting writes. Without it, limits like maximum active streams per tenant are not enforced at ingestion time.
In the ingest storage architecture, replication is ensured through Kafka. When a write happens, Kafka replicates it to the other brokers. Ingesters will always start reading from the last committed offset. The compactor remains a necessary component for index compaction and retention.

Summary Table:

| Aspect | Classic Architecture | Ingest Storage Architecture |
| ------ | -------------------- | --------------------------- |
| **Write acknowledgment** | After quorum of ingesters confirm | After Kafka brokers confirm persistence |
| **Per-tenant limits enforcement** | Ingesters enforce limits | Requires optional `ingest-limits` component |
| **Write replication** | Loki replicates to replication-factor ingesters | Kafka replicates internally |
| **Ingester failure impact** | Write availability at risk | No write disruption |
| **Scaling ingesters** | Complex (state transfer / hand-off) | Simple (assign some traffic to the new Kafka partitions) |
| **Additional infrastructure** | None beyond Loki + object storage | Kafka cluster required |
| **Rollout risk** | High (ingester restarts can break quorum, causing write failures) | Low (resume from Kafka offset) |

###### Record Structure

In the ingest storage architecture, each Kafka record represents one or more log streams for a single tenant. The record structure is:

| Field | Value |
| ----- | ----- |
| **Key** | Tenant ID (used for identification, not for partitioning) |
| **Value** | Protobuf-serialized `logproto.Stream` containing: stream-labels (string), log entries (timestamp + line + structured metadata), and a stream hash |
| **Partition** | Explicitly set by Loki via the partition ring |

The distributor determines the target partition by hashing the stream-labels. It then sets the partition number directly on the Kafka record, so Kafka honors the explicit partition and ignores the key. The tenant ID is set as the key purely as metadata.

If a single stream exceeds the maximum record size (`producer_max_record_size_bytes`, ~15MB by default), it is automatically split into multiple records, each containing a subset of entries but sharing the same labels and hash.

Records are sent to Kafka uncompressed. Loki does not configure producer-side compression, although the client it uses supports it.
Kafka-side compression can be used independently to mitigate this.

##### Read Path

Unlike the write path, the read path remains largely unchanged. The biggest difference is how the querier discovers ingesters.

In the classic architecture, the querier discovers ingesters via the ingester ring. It then queries ingesters in the replication set and waits for a quorum of responses. It has to query multiple ingesters per hash range because with a replication factor higher than one, the same data exists on multiple ingesters. The querier waits for a quorum to ensure it possesses at least one copy of every stream even if some ingesters are slow or down.

In the ingest storage architecture, the querier asks the partition ring instead. Via the partition ring it gets the relevant partitions for the tenant, which returns a replication set per partition. In a single-zone setup this is one ingester per partition; in a multi-zone setup it includes one ingester per zone per partition, but the querier only needs one successful response (the data is identical across zones). Since partition ownership is deterministic, the querier knows precisely which ingester(s) hold which data. This results in fewer requests, no duplicate data to merge, and no wasted work querying ingesters that don't hold relevant data.

The Query Frontend and Index Gateway are completely unchanged.

Summary Table:

| Aspect | Classic Architecture | Ingest Storage Architecture |
| ------ | -------------------- | --------------------------- |
| **Ingester discovery** | Via ingester ring (all ingesters) | Via partition ring (only partition owners, one per zone) |
| **Query fan-out** | Query all ingesters, wait for quorum | Query partition owners, need only one successful response per partition |
| **Result deduplication** | Required (multiple ingesters write the same data with different chunk boundaries, querier must merge and deduplicate) | Not needed (consumers of the same partition produce byte-identical, content-addressed chunks; querier needs only one successful response per partition) |
| **Read consistency** | Eventual (best-effort) | Eventual (best-effort) |

##### Scaling Ingesters

###### Scaling Up

Partition assignment is static and hostname-based: `ingester-N` owns Kafka partition N. Each ingester owns exactly one partition. The Kafka topic may have many more partitions than ingesters, but only N partitions are ACTIVE at any time (one per ingester).

Two flags control when a PENDING partition transitions to ACTIVE:

- `max_consumer_lag_at_startup` (default: 15s): When an ingester starts, it replays records from Kafka starting at its last committed offset. The ingester repeatedly reads until the gap between the latest produced offset and the last processed offset represents less than this threshold of time. Only then does the ingester pass its readiness check. This ensures the ingester has caught up with the Kafka partition before serving queries.

- `min_partition_owners_count` (default: 1): The minimum number of owners that must be registered in the partition ring before the partition transitions from PENDING to ACTIVE. Each owner must have been registered for at least `min_partition_owners_duration` (default: 10s). In a multi-zone deployment, this should be increased to match (or approximate) the number of zones to ensure read redundancy is established before the partition starts receiving writes.

Consider a cluster with 3 ingesters:

```
Before (3 active partitions out of many in Kafka):
  ingester-0 → partition 0 (ACTIVE)
  ingester-1 → partition 1 (ACTIVE)
  ingester-2 → partition 2 (ACTIVE)
```

When `ingester-3` joins:

1. `ingester-3` registers in both the ingester ring (liveness) and the partition ring, claiming partition 3
2. Partition 3 starts in PENDING state
3. `ingester-3` begins consuming from Kafka partition 3, replaying from the last committed offset to catch up
4. Once the consumer lag is within the configured threshold (`max_consumer_lag_at_startup`, default 15s) and the minimum ownership duration has elapsed (`min_partition_owners_duration`, default 10s), partition 3 transitions to ACTIVE

```
After (4 active partitions):
  ingester-0 → partition 0 (ACTIVE)
  ingester-1 → partition 1 (ACTIVE)
  ingester-2 → partition 2 (ACTIVE)
  ingester-3 → partition 3 (ACTIVE)
```

**Write path impact**: While partition 3 is PENDING, `ActivePartitionForKey()` skips it and distributors continue routing to partitions 0, 1, 2. Once partition 3 becomes ACTIVE, distributors see the updated ring and some streams begin routing to partition 3. No writes are lost during the transition because Kafka decouples writes from consumption.

**Read path impact**: When partition 3's tokens enter the ring, some streams that previously hashed to partitions 0, 1, or 2 now hash to partition 3. However, `ingester-3` has no historical data for those streams — it only sees new writes going forward. The historical data is still in the memory of the ingesters that owned the old partitions. Since all old partitions (0, 1, 2) remain ACTIVE, queriers continue querying them alongside partition 3 via the lookback window (`ShuffleShardWithLookback`, controlled by `query_ingesters_within`, default: 3h), which ensures that partitions whose state recently changed remain included in the query set. This prevents gaps during the transition. Once the old ingesters have flushed their in-memory data to object storage, queriers serve historical data from there.

###### Scaling Down

Consider a cluster with 4 ingesters:

```
Before (4 active partitions):
  ingester-0 → partition 0 (ACTIVE)
  ingester-1 → partition 1 (ACTIVE)
  ingester-2 → partition 2 (ACTIVE)
  ingester-3 → partition 3 (ACTIVE)
```

**Graceful shutdown** of `ingester-3`:

1. `ingester-3` receives a shutdown signal and calls its `prepare-downscale` endpoint
2. Partition 3 transitions to INACTIVE in the partition ring
3. `ActivePartitionForKey()` skips INACTIVE partitions, so distributors reroute streams that would have gone to partition 3 to one of the remaining ACTIVE partitions (0, 1, or 2)
4. `ingester-3` stops consuming from Kafka, then flushes its in-memory chunks to object storage as part of the lifecycler shutdown, and exits
5. The INACTIVE partition entry remains in the ring for a grace period (default 13h) and is eventually removed

```
After (3 active partitions):
  ingester-0 → partition 0 (ACTIVE)
  ingester-1 → partition 1 (ACTIVE)
  ingester-2 → partition 2 (ACTIVE)
  partition 3: INACTIVE, no consumer
```

The Kafka partition 3 still exists but no ingester consumes from it. Any unconsumed records in partition 3 remain in Kafka until Kafka's retention period expires. Streams that previously hashed to partition 3 are rerouted to whichever ACTIVE partition is next in the ring for their hash key.

**Write path impact**: Writes are immediately rerouted away from partition 3. Since no ingester will consume from it, any writes that were not processed will be lost when Kafka's retention expires unless the partition is claimed again (e.g., by scaling back up).

**Read path impact**: Before shutting down, `ingester-3` flushes its in-memory data to object storage. After the flush, queriers serve all historical data from object storage.

**Crash scenario**: If `ingester-3` crashes instead of shutting down gracefully, partition 3 remains ACTIVE in the partition ring — no component changes its state since the lifecycler runs on the ingester itself. Distributors continue writing to Kafka partition 3 as normal, which is safe because writes go to Kafka, not to the ingester directly. Records simply accumulate in the Kafka partition until `ingester-3` comes back. When `ingester-3` restarts, it resumes consuming from the last committed Kafka offset, replaying all records that accumulated during the downtime. Data consumed before the crash but not yet flushed to object storage is recovered from the WAL if persistent volumes are used.

### API Extensions

The new API extensions are:

- The addition of the IngestLimits component to `LokiTemplateSpec`.
- The new `IngestStorage` struct, to hold all the configuration concerning the new architecture.

#### New Component: `IngestLimits`

```go
type LokiTemplateSpec struct {
	IngestLimits *LokiComponentSpec `json:"ingestLimits,omitempty"`
}
```

#### New Field on `LokiStackSpec`

```go
type LokiStackSpec struct {
	// IngestStorage defines the ingest storage architecture configuration.
	// When not set, the classical architecture is used.
	// When set, distributors write to Kafka and ingesters consume from it.
	IngestStorage *IngestStorageSpec `json:"ingestStorage,omitempty"`
}
```

#### `IngestStorageSpec`

Currently only Kafka configuration, but this level of nesting allows future extensions (e.g., DataObj configuration) to be added as siblings without restructuring the API.

```go
type IngestStorageSpec struct {
	// Kafka defines the connection configuration for a Kafka-compatible ingest storage backend
	Kafka KafkaSpec `json:"kafka"`
}
```

#### `KafkaSpec`

```go
type KafkaSpec struct {
	// Topic is the Kafka topic name used for log ingestion.
	// Defaults to "loki".
	Topic string `json:"topic,omitempty"`

	// Secret for Kafka connection and authentication.
	// Name of a secret in the same namespace as the LokiStack custom resource.
	Secret KafkaSecretSpec `json:"secret"`

	// TLS configuration for the Kafka connection.
	TLS *TLSSpec `json:"tls,omitempty"`
}
```

The `TLS` field reuses the existing `TLSSpec` type which provides:

- `CA *ValueReference` — CA certificate to verify the broker
- `Certificate *ValueReference` — client certificate for mTLS authentication
- `PrivateKey *SecretReference` — client private key for mTLS authentication

#### `KafkaSecretSpec`

```go
type KafkaSecretSpec struct {
	// Name of a secret in the same namespace as the LokiStack custom resource.
	Name string `json:"name"`
}
```

The referenced secret can contain the following keys:

| Key | Required | Description |
|-----|----------|-------------|
| `readerAddress` | Always | Broker addresses for consumers (host:port, comma-separated) |
| `writerAddress` | Always | Broker addresses for producers (host:port, comma-separated) |
| `saslMechanism` | For SASL | One of `PLAIN`, `SCRAM-SHA-256`, `SCRAM-SHA-512` |
| `username` | For SASL | SASL username |
| `password` | For SASL | SASL password |

Authentication mode is inferred from the secret contents and TLS spec:

- **No auth**: Secret contains only `readerAddress` and `writerAddress`
- **SASL**: Secret additionally contains `saslMechanism`, `username`, and `password`
- **mTLS**: Secret contains only addresses; `tls.certificate` and `tls.privateKey` are set in the spec

#### Example CR

```yaml
apiVersion: loki.grafana.com/v1
kind: LokiStack
metadata:
  name: lokistack-dev
spec:
  size: 1x.demo
  storage:
    schemas:
    - version: v13
      effectiveDate: 2023-10-15
    secret:
      name: storage-secret
      type: s3
  ingestStorage:
    kafka:
      topic: loki-logs
      secret:
        name: kafka-credentials
      tls:
        ca:
          key: ca.crt
          secretName: my-cluster-cluster-ca-cert
---
# Kafka credentials secret
apiVersion: v1
kind: Secret
metadata:
  name: kafka-credentials
type: Opaque
stringData:
  readerAddress: my-cluster-kafka-bootstrap.kafka.svc.cluster.local:9093
  writerAddress: my-cluster-kafka-bootstrap.kafka.svc.cluster.local:9093
  saslMechanism: SCRAM-SHA-512
  username: loki-user
  password: secret-password
```

### Implementation Details/Notes/Constraints [optional]

#### Upstream Loki Kafka Client Limitations

We identified two significant limitations in upstream Loki's Kafka client:

1. **No TLS support**: The Kafka client does not call `kgo.DialTLSConfig()`, meaning it cannot connect to TLS-enabled Kafka listeners. All connections are plaintext. This prevents the operator from using Strimzi/AMQ Streams TLS listeners or mTLS authentication.

2. **SASL/PLAIN only**: The client hardcodes `plain.Plain(...)` for SASL authentication. It does not support SCRAM-SHA-256, SCRAM-SHA-512, or OAUTHBEARER. Since Strimzi/AMQ Streams does not offer SASL/PLAIN as a listener authentication type we can only connect to Strimzi using no authentication. Upstream issue: [grafana/loki#21712](https://github.com/grafana/loki/issues/21712).

#### Observability and Dashboards

The operator deploys Prometheus recording rules and alerting rules for Loki components. The ingest storage architecture changes the write path and the read path, which may affect existing dashboard panels and alerts. Specifically:

Investigation needs to be done to fully understand how to observe the new system and how the operator can customize the dashboards deployed depending on the architecture mode selected.

### Risks and Mitigations

- Increase in resource requirements with the addition of Kafka
  - A Strimzi cluster with 3 replicas with 1x.Demo was consuming 0.1 CPU and 1.8Gi. Further benchmarking needs to happen to better determine the impact
- No backpressure from ingester consumption lag
  - In the ingest storage architecture, the distributor writes to Kafka and has no awareness of whether ingesters are keeping up with consumption. If ingesters fall behind, records accumulate in Kafka - there is no mechanism to reject or throttle writes based on consumer lag.
- Misconfiguration of the Kafka cluster
  - We should write extensive documentation to support proper configuration
  - If Kafka is temporarily unavailable, each distributor can buffer up to `producer_max_buffered_bytes` (default: 1GB) of unacknowledged records before it starts rejecting new writes.

## Design Details

### Open Questions [optional]

- If we have a running Loki cluster what is the recommendation for migrating it to the Ingest Storage Architecture? Shall we use dual-write capabilities or simply flip the switch?

## Implementation History

- Initial working LokiStack + Kafka PoC:  https://github.com/grafana/loki/compare/main...JoaoBraveCoding:loki:LOG-7377-poc
- API Implementation + Strimzi resources: https://github.com/grafana/loki/compare/main...JoaoBraveCoding:loki:LOG-7377-api

## Drawbacks

## Alternatives
