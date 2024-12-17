---
menuTitle: Hash rings
title: Consistent hash rings
description: Describes how the Loki architecture uses consistent hash rings.
weight: 800
aliases:
    - ../fundamentals/architecture/rings
---
# Consistent hash rings

[Consistent hash rings](https://en.wikipedia.org/wiki/Consistent_hashing)
are incorporated into Loki cluster architectures to

- aid in the sharding of log lines
- implement high availability
- ease the horizontal scale up and scale down of clusters.
There is less of a performance hit for operations that must rebalance data.

Hash rings connect instances of a single type of component when

- there are a set of Loki instances in monolithic deployment mode
- there are multiple read components or multiple write components in
simple scalable deployment mode
- there are multiple instances of one type of component in microservices mode

Not all Loki components are connected by hash rings.
These components need to be connected into a hash ring:

- distributors
- ingesters
- query schedulers
- compactors
- rulers

These components can optionally be connected into a hash ring:
- index gateway


In an architecture that has three distributors and three ingesters defined,
the hash rings for these components connect the instances of same-type components.

![Distributor and ingester rings](../ring-overview.png "Distributor and ingester rings")

Each node in the ring represents an instance of a component.
Each node has a key-value store that holds communication information
for each of the nodes in that ring.
Nodes update the key-value store periodically to keep the contents consistent
across all nodes.
For each node, the key-value store holds:

- an ID of the component node
- component address, used by other nodes as a communication channel
- an indication of the component node's health

## Configuring rings

Define [ring configuration](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#common) within the `common.ring_config` block.

Use the default `memberlist` key-value store type unless there is
a compelling reason to use a different key-value store type.
`memberlist` uses a [gossip protocol](https://en.wikipedia.org/wiki/Gossip_protocol)
to propagate information to all the nodes
to guarantee the eventual consistency of the key-value store contents.

There are additional configuration options for distributor rings,
ingester rings, and ruler rings.
These options are for advanced, specialized use only.
These options are defined within the `distributor.ring` block for distributors,
the `ingester.lifecycler.ring` block for ingesters,
and the `ruler.ring` block for rulers.

## About the distributor ring

Distributors use the information in their key-value store
to keep a count of the quantity of distributors in the distributor ring.
The count further informs cluster limits.

## About the ingester ring

Ingester ring information in the key-value stores is used by distributors.
The information lets the distributors shard log lines,
determining which ingester or set of ingesters a distributor sends log lines to.

## About the query scheduler ring

Query schedulers use the information in their key-value store
for service discovery of the schedulers.
This allows queriers to connect to all available schedulers,
and it allows schedulers to connect to all available query frontends,
effectively creating a single queue that aids in balancing the query load.

## About the compactor ring

Compactors use the information in the key-value store to identify
a single compactor instance that will be responsible for compaction.
The compactor is only enabled on the responsible instance,
despite the compactor target being on multiple instances.

## About the ruler ring

The ruler ring is used to determine which rulers evaluate which rule groups.

## About the index gateway ring

The index gateway ring is used to determine which gateway is responsible for which tenant's indexes when queried by rulers or queriers.
