---
title: Distributor
weight: 1000
---
# Distributor Component

This document builds upon the information in the [Loki Architecture](./) page.

## Where does it live?

The distributor is the first component on Loki's write path downstream from any gateways providing auth or load balancing. It's responsible for validating, preprocessing, and applying a subset of rate limiting to incoming data before sending it to the ingester component. It is important that a load balancer sits in front of the distributor in order to properly balance traffic to them.

## What does it do?

### Validation

The first step the distributor takes is to ensure that all incoming data is according to specification. This includes things like checking that the labels are valid Prometheus labels as well as ensuring the timestamps aren't too old or too new or the log lines aren't too long.

### Preprocessing

Currently the only way the distributor mutates incoming data is by normalizing labels. What this means is making `{foo="bar", bazz="buzz"}` equivalent to `{bazz="buzz", foo="bar"}`, or in other words, sorting the labels. This allows Loki to cache and hash them deterministically.

### Rate limiting

The distributor can also rate limit incoming logs based on the maximum per-tenant bitrate. It does this by checking a per tenant limit and dividing it by the current number of distributors. This allows the rate limit to be specified per tenant at the cluster level and enables us to scale the distributors up or down and have the per-distributor limit adjust accordingly. For instance, say we have 10 distributors and tenant A has a 10MB rate limit. Each distributor will allow up to 1MB/second before limiting. Now, say another large tenant joins the cluster and we need to spin up 10 more distributors. The now 20 distributors will adjust their rate limits for tenant A to `(10MB / 20 distributors) = 500KB/s`! This is how global limits allow much simpler and safer operation of the Loki cluster.

**Note: The distributor uses the `ring` component under the hood to register itself amongst it's peers and get the total number of active distributors. This is a different "key" than the ingesters use in the ring and comes from the distributor's own [ring config](../../configuration#distributor_config).**

### Forwarding

Once the distributor has performed all of it's validation duties, it forwards data to the ingester component which is ultimately responsible for acknowledging the write.

#### Replication factor

In order to mitigate the chance of _losing_ data on any single ingester, the distributor will forward writes to a _replication_factor_ of them. Generally, this is `3`. Replication allows for ingester restarts and rollouts without failing writes and adds additional protection from data loss for some scenarios. Loosely, for each label set (called a _stream_) that is pushed to a distributor, it will hash the labels and use the resulting value to look up `replication_factor` ingesters in the `ring` (which is a subcomponent that exposes a [distributed hash table](https://en.wikipedia.org/wiki/Distributed_hash_table)). It will then try to write the same data to all of them. This will error if less than a _quorum_ of writes succeed. A quorum is defined as `floor(replication_factor / 2) + 1`. So, for our `replication_factor` of `3`, we require that two writes succeed. If less than two writes succeed, the distributor returns an error and the write can be retried.

**Caveat: There's also an edge case where we acknowledge a write if 2 of the three ingesters do which means that in the case where 2 writes succeed, we can only lose one ingester before suffering data loss.**

Replication factor isn't the only thing that prevents data loss, though, and arguably these days it's main purpose it to allow writes to continue uninterrupted during rollouts & restarts. The `ingester` component now includes a [write ahead log](https://en.wikipedia.org/wiki/Write-ahead_logging) which persists incoming writes to disk to ensure they're not lost as long as the disk isn't corrupted. The complementary nature of replication factor and WAL ensures data isn't lost unless there are significant failures in both mechanisms (i.e. multiple ingesters die and lose/corrupt their disks).

## Why does it deserve it's own component?

Notably, the distributor is a stateless component. This makes it easy to scale and offload as much work as possible from the ingesters, which are the most critical component on the write path. The ability to independently scale these validation operations mean that Loki can also protect itself against denial of service attacks (either malicious or not) that could otherwise overload the ingesters. They act like the bouncer at the front door, ensuring everyone is appropriately dressed and has an invitation. It also allows us to fan-out writes according to our replication factor as described earlier.
