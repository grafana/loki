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

#### Kafka taxonomy

Apache Kafka is a distributed event streaming platform. At its core, it is a durable, ordered, append-only log. Producers write records to the end of the log, and consumers read from it at their own pace. Records are not deleted after consumption, they are retained for a configurable period, which means multiple consumers can read the same data independently.

In Loki's context, Kafka acts as a buffer and durable write-ahead log between the distributor and the ingester. This decouples the write acknowledgment from ingester availability.

Some important Kafka concepts to better understand this enhancement are:

- Broker: A Kafka server process. A Kafka cluster consists of one or more brokers. Each broker stores a subset of the data and serves client requests. In production, you typically run 3+ brokers for fault tolerance.

- Topic: A named category or feed to which records are published. A topic is a logical grouping, the actual data is split across partitions.

- Partition: Partitions are ordered, immutable sequences of records. Partitions are the unit of parallelism: each partition can be consumed by exactly one consumer in a consumer group. More partitions = more parallelism.

- Record: A single unit of data written to a partition. It consists of a key, a value (the payload), a timestamp, and optional headers.

- Offset: A sequential ID assigned to each record within a partition. Consumers track their position using offsets. Offsets are committed to Kafka by consumers. This is how Kafka enables replay.

- Producer: A client that writes records to a partition on a topic.

- Consumer / Consumer Group: A client that reads records from partitions. A consumer group is a set of consumers that coordinate to divide partitions among themselves -- each partition is consumed by exactly one member of the group.

#### Classical Architecture VS Ingest Storage Architecture

In this section we will briefly compare the two architectures to better understand the differences both in the write path and in the read path.

##### Write Path

In the classical architecture, a distributor would write to a set of ingesters organized in a hash ring. A distributor determined the ingesters it should write to by hashing the stream labels in a log stream and then consulting the ingester ring. Once a quorum of ingesters confirms the write, the distributor moves on. Ingesters then collect log entries and eventually flush them to object storage as chunks with TSDB index entries. If an ingester dies, the ingester ring detects it via heartbeat timeout and the remaining ingesters take over its token ranges. If not enough ingesters remain to form a quorum, ingestion will stop. Distributors are independent from each other but have to sync with ingesters. Ingesters are independent of each other.
The classical architecture ensured no data was lost via replication, a distributor would write to multiple ingesters.

On the ingest storage architecture, each ingester joins both the classical ingester ring (for liveness and query routing) and a new partition ring (for Kafka partition assignment).
The new partition ring tracks Kafka partitions and which ingester owns each one. Distributors hash stream labels and use the partition ring to find the active partition for that hash.
So in the new architecture, the distributors (Kafka producers) write records (one or more serialized log streams) to Kafka partitions.
Once Kafka confirms persistence of the record, the distributor acknowledges the write back to the client.
Ingesters (Kafka consumers) read records from partitions and periodically commit their offset back to Kafka, allowing them to resume from where they left off after restarts or replacements.
Ingesters collect log entries and eventually flush them to object storage as chunks with TSDB index entries.
In the ingest storage architecture, replication is ensured through Kafka, when a write happens, Kafka replicates it to the other brokers. The compactor remains a necessary component for index compaction and retention.

Partition Ring: The partition ring wraps the ingester ring for heartbeat/liveness checks. Each ingester maintains dual ring membership. The partition ring also supports shuffle sharding for tenant isolation -- instead of spreading a tenant's data across all partitions, each tenant can be assigned a small subset of partitions.

Kafka Topic Partition Count: Loki can set partition count automatically, but Kafka 4.x no longer allows the number of partitions to be changed dynamically. This means the Kafka administrator must either pre-create the topic with the desired partition count or configure them afterwards before Loki starts. The partition count determines the maximum ingestion parallelism -- it should be at least equal to the number of ingesters, this is a prerequisite that in principle the operator will not be able to automate and should be documented.

Summary Table:

| Aspect | Classic Architecture | Ingest Storage Architecture |
| ------ | -------------------- | --------------------------- |
| **Write acknowledgment** | After quorum of ingesters confirm | After Kafka brokers confirm persistence |
| **Write replication** | Loki replicates to N ingesters (default 3) | Kafka replicates internally |
| **Ingester failure impact** | Write availability at risk | No write disruption |
| **Deduplication** | Required (compactor deduplicates replicated blocks) | Not needed (single consumer per partition) |
| **Scaling ingesters** | Complex (state transfer / hand-off) | Simple (reassign Kafka partitions) |
| **Additional infrastructure** | None beyond Loki + object storage | Kafka cluster required |
| **Read consistency** | Eventual (best-effort) | Configurable strong consistency via offset tracking |
| **Rollout risk** | High (ingester restarts can break quorum, causing write failures) | Low (resume from Kafka offset) |

##### Read Path

Unlike the write path, the read path remains largely unchanged. The biggest difference is how the querier discovers ingesters.

In the classic architecture, the querier discovers ingesters via the ingester ring. It then queries ingesters in the replication set and waits for a quorum of responses. It has to query multiple ingesters per hash range because with a replication factor higher than one, the same data exists on multiple ingesters. The querier waits for a quorum to ensure it possesses at least one copy of every stream even if some ingesters are slow or down.

In the ingest storage architecture, the querier asks the partition ring instead. Via the partition ring it gets the relevant partitions for the tenant, which returns a replication set per partition (typically one ingester per partition in a single-zone setup). Since each partition is owned by exactly one ingester, the querier knows precisely which ingester has which data. This results in fewer requests, no duplicate data to merge, and no wasted work querying ingesters that don't hold relevant data.

The Query Frontend and Index Gateway are completely unchanged.

Summary Table:

| Aspect | Classic Architecture | Ingest Storage Architecture |
| ------ | -------------------- | --------------------------- |
| **Ingester discovery** | Via ingester ring (all ingesters) | Via partition ring (only relevant ingesters) |
| **Query fan-out** | Query all ingesters, wait for quorum | Query one ingester per partition, no quorum needed |
| **Result deduplication** | Required (same data on multiple ingesters) | Not needed (single owner per partition) |
| **Query Frontend** | Unchanged | Unchanged |
| **Index Gateway** | Unchanged | Unchanged |

### API Extensions


### Implementation Details/Notes/Constraints [optional]



### Risks and Mitigations


## Design Details

### Open Questions [optional]

- If we have a running Loki cluster what is the recommendation for migrating it to the Ingest Storage Architecture? Shall we use dual-write capabilities or simply flip the switch?

## Implementation History


## Drawbacks


## Alternatives
