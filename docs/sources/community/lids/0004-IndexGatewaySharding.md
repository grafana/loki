---
title: "0004: Index Gateway Sharding"
description: Loki Improvement Document for index gateway sharding.
---

# 0004: Index Gateway Sharding

**Author:** Christian Haudum (christian.haudum@grafana.com)

**Date:** 02/2023

**Sponsor(s):** @chaudum @owen-d

**Type:** Feature

**Status:** Rejected / Not Implemented

**Related issues/PRs:**

**Thread from [mailing list](https://groups.google.com/forum/#!forum/lokiproject):**

---

## Background

This document tries to come up with a proposal on how to do a better sharding of data on the index gateways so we are able to scale the service horizontally to fulfill the increased need for metadata queries of big tenants.

The index gateway service can be run in "simple mode", where an index gateway instance is responsible for handling, storing and returning requests for all indices for all tenants, or in "ring mode", where an instance is responsible for a subset of tenants instead of all tenants.

On top of that, in order to achieve redundancy as well as spreading load, the index gateway ring uses by default a replication factor of 3.

This means, before an index gateway client makes a request to the index gateway server, it first hashes the tenant ID and then requests a replication set for that hash from the index gateway ring. Due to the fixed replication factor (RF), the replication set contains three server addresses. On every request, a random server from that list is picked to then execute the request on.

## Problem Statement

The current strategy of sharding by tenant ID and having a replication factor fails in the long run, because even when running lots of index gateways, only a maximum of `n` instances could be utilized by a single tenant, where `n` is the value of the configured RF.

Another problem is that the RF is fixed and the same for all tenants, independent of their actual size in terms of log volume or query rate.

## Goals

The goal of this document is to find a better sharding mechanism for the index gateway, so that there are no boundaries for scaling the service horizontally.

* The sharding needs to account for the "size" of a tenant.
* A single tenant needs to be able to utilize more than three index gateways.

## Proposals

### Proposal 0: Do nothing

If we do not improve the sharding mechanism for the index gateways and leave it as it is, it will become more and more difficult to serve metadata queries for large tenant in a reasonable amount of time, proportionally to the demand for these queries.

### Proposal 1: Dynamic replication factor

Instead of using a fixed replication factor of 3, the RF can be derived from the amount of active members in the index gateway ring. That means that the RF would be a percentage of the available gateway instances. For example, a ring with 12 instances and 30% replication utilization would result in a RF of 3 (`floor(12*0.3)`). Scaling up to 18 instances would result in a RF of 5.

This approach would solve the problem of horizontal scaling. However, it does not solve the problem of different tenant sizes. It also fails to ensure replication for availability when running a small number of instances, unless there is a fixed lower value for the RF. It also tends to over-shard data in large index gateway deployments.

### Proposal 2: Fixed per-tenant replication factor

Adding a random shard ID (e.g. `shard-0`, `shard-1`, ... `shard-n`) to the tenant ID allows to utilize a certain amount of `n` instances. The amount of shards can be implemented as a per-tenant override setting. This would allow to use different amount of instances for each tenant. However, this approach results in non-deterministic hash keys.

### Proposal 3: Shard by index files

In order to answer requests, the index gateway needs to download index files from the object storage, and since Loki builds a daily index file per tenant, these index files can be sharded evenly across all available index gateway instances. Each instance is then assigned a unique set of index files which it can answer metadata queries for.

This means that the sharding key is the name of the file in object storage. While this name encodes both the tenant and the date, this is not strictly necessary. Such a sharding mechanism could shard any files from object storage across a set of instances of a ring.

If the time range for the requested metadata is within a single day then a single index gateway instance can answer the metadata request.
However, if a metadata request spans multiple days, also multiple index gateway instances are involved. There are two ways to solve this:

#### A) Split and merge on client side

The client resolves the necessary index files and their respective gateway instances. It splits the request into multiple sub-requests, executes them and merges them into a single result.

**Pros:**
* Only the minimum necessary amount of requests are performed.

**Cons:**
* The client requires information about how to split and merge requests.

#### B) Split and merge on index gateway handler side

The client can execute a request on any index gateway. This handler instance then identifies the index files that are involved, splits the query, and resolves the appropriate instances. Once it received the sub-queries it resembles the full response result and sends it back to the client.

**Pros:**
* Sharding is handled transparently to the client.
* Clients can communicate with any instance of the index gateway ring.
* Domain information about splitting and merging is kept within index gateway server implementation.

Due to it's architectural advantages, option B is proposed.

## Other Notes

### Architectural diagram of proposal 3

![index gateway sharding](../index-gw-sharding-diagram.svg)
