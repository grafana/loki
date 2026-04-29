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

In this section we will briefly compare the two architectures to better understand the differences both in the write path and in the read path. However first we will introduce the partition ring concept which is essential to understand the new architecture.

##### Partition Ring

In the new ingest storage architecture there is a new membership ring called partition ring. The partition ring wraps the ingester ring for heartbeat/liveness checks and each ingester maintains dual ring membership. The new partition ring tracks Kafka partitions and which ingester owns each one.

- On a single zone deployment: each ingester maps to a single partition using their hostname. E.g `ingester-0 --> partition 0`
- On multi zone deployments: ingesters in different zones map to the same partition, using their hostname. E.g `ingester-zone-a-0, ingester-zone-b-0, ingester-zone-c-0 --> partition 0`. All zone ingesters for the same partition consume from it independently, each using their own Kafka consumer group, and each committing offsets at their own pace.

When distributors (producers) want to write they hash the stream-labels and use the partition ring to find the active partition for that hash. Once they know which partition they should write to distributors set the `Partition` field in the record and send the record to Kafka. Note that by setting the `Partition` field Loki is actively controlling how information is distributed instead of relying on Kafka.

The partition ring also supports shuffle sharding for tenant isolation -- instead of spreading a tenant's data across all partitions, each tenant can be assigned a small subset of partitions.

Kafka Topic Partition Count: Loki can set partition count automatically, but Kafka 4.x no longer allows the number of partitions to be changed dynamically. This means the Kafka administrator must either pre-create the topic with the desired partition count or configure them afterwards before Loki starts. The partition count determines the maximum number of ingesters, this is a prerequisite that in principle the operator will not be able to automate and should be documented.

##### Write Path

In the classical architecture, a distributor would write to a set of ingesters organized in a hash ring. A distributor determined the ingesters it should write to by hashing the stream-labels in a log stream and then consulting the ingester ring. Once a quorum of ingesters confirms the write, the distributor moves on. Ingesters then collect log entries and eventually flush them to object storage as chunks with TSDB index entries. If an ingester dies, the ingester ring detects it via heartbeat timeout and the remaining ingesters take over its token ranges. If not enough ingesters remain to form a quorum, ingestion will stop. Distributors are independent from each other but have to sync with ingesters. Ingesters are independent of each other.
The classical architecture ensured no data was lost via replication, a distributor would write to multiple ingesters.

On the ingest storage architecture, each ingester joins both the classical ingester ring (for liveness and query routing) and the new partition ring (for Kafka partition assignment).
So in the new architecture, the distributors (producers) write records (one or more serialized log streams) to Kafka partitions.
Once Kafka confirms persistence of the record, the distributor acknowledges the write back to the client.
Ingesters (consumers) read records from their partition and periodically commit their offset back to Kafka under their own consumer group. On startup, an ingester fetches the last committed offset for its consumer group and resumes consumption from that point, allowing seamless recovery after restarts or replacements.
Ingesters collect log entries and eventually flush them to object storage as chunks with TSDB index entries. The TSDB index format remains the same as in the classical architecture.
In the ingest storage architecture, replication is ensured through Kafka. When a write happens, Kafka replicates it to the other brokers. Ingesters will always start reading from the last committed offset. The compactor remains a necessary component for index compaction and retention.

Summary Table:

| Aspect | Classic Architecture | Ingest Storage Architecture |
| ------ | -------------------- | --------------------------- |
| **Write acknowledgment** | After quorum of ingesters confirm | After Kafka brokers confirm persistence |
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


### Implementation Details/Notes/Constraints [optional]



### Risks and Mitigations


## Design Details

### Open Questions [optional]

- If we have a running Loki cluster what is the recommendation for migrating it to the Ingest Storage Architecture? Shall we use dual-write capabilities or simply flip the switch?

## Implementation History


## Drawbacks


## Alternatives
