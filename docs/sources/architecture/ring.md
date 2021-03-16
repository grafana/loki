---
title: Ring
weight: 1000
---

# Ring

This document builds upon the information in the [Loki Architecture](./) page

It explains what exactly is the ring, what problem does it solve and how it actually works.

We use ring in Distributor and Ingester components of Loki.

Loki in microservice mode usually can have multiple ingesters and distributors. This multiple instances of same component(ingester or distributor) forms a ring (more precisely Consistent Hash Ring).

Both distributors and ingesters have their own ring. Write path looks like Client -> Distributors (ring) -> Ingesters (ring). Read path looks like Client -> Querier -> Ingesters (ring).

## The Problem

Loki aggregates incoming log lines into something called Log stream. Log stream is just a set of logs associated with a single tenant and a unique labelset(key value pairs).

Here is the problem, say I have large number of log streams coming in and I have a bunch of Loki servers(can be ingesters or distributors). Now I want to distribute these log streams across the servers in a way I can find it later when I want to read back the log stream. Here is the tricky part. I want to do this without any global directory or lookup service.

So basically, we are talking problem of distribuing the log streams "deterministically"!

Problem is simple if all I have is single Loki server. Solution is send all logs into that single server. But having multiple servers makes the problem bit tricky

Another way to solve this problem is via hashing. We assign an integer to the servers (say 0, 1, 2, 3.. etc) and we hash our log stream (labelset + tenantID) to integer value 'h' and handover it to the server with the value 'h%n' where n is the max server.

Interestingly, this approach solves some of the problem we have, say same log stream goes to same server (because same set of labels + tenant gives same hash value), and while reading, we can hash it back to find the server where its value is stored.

But this solution lacks some scaling properties. Say if we introduce new server or remove existing server (intentionally or unintentially) then every single log stream will map to different server now.

There are several better approach to solve this problem. One of the approach we use in Loki is Consistent Hashing.

## How it works

Distributors use consistent hashing in conjunction with a configurable replication factor to determine which instances of the ingester service should receive a given stream.

Each ingester belongs to a single hash ring. This hash ring stored in Consul is used to achieve consistent hashing;

Every ingester (also distributor) that is part of the ring has two things associated with it.
- State
- Set of tokens they own

State can be one of the following
- PENDING
- JOINING
- ACTIVE
- LEAVING
- UNHEALTHY

The state ACITVE may receive both read and write requests. While state JOINING can receive only write requests, the state LEAVING may receive read requests.

Each token is a random unsigned 32-bit number.

When doing a hash lookup, distributors only use tokens for ingesters who are in the appropriate state for the request.

To do the hash lookup, distributors find the smallest appropriate token whose value is larger than the hash of the stream. When the replication factor is larger than 1, the next subsequent tokens (clockwise in the ring) that belong to different ingesters will also be included in the result.

The effect of this hash set up is that each token that an ingester owns is responsible for a range of hashes (hence load is distributed more evenly across the instances). If there are three tokens with values 0, 25, and 50, then a hash of 3 would be given to the ingester that owns the token 25; the ingester owning token 25 is responsible for the hash range of 1-25.

Token is same as virtual node (or vnode) in consistent hashing.

Number of tokens per ring is configurable via `--ingester.num-tokens`. Default is 128

### Quorum consistency
Since all ingesters share access to the same hash ring, write requests can be sent to any ingester.

To ensure consistent query results, Loki uses Dynamo-style quorum consistency on reads and writes. This means for example that the distributor will wait for a positive response of at least one half plus one of the ingesters to send the sample to before responding to the client that initiated the send.

### Ring Status page

If Loki is run in microservice mode, both ingester and distributor exposes their ring status via the endpoint `api/v1/ruler/ring`

### Other uses of consistent hashing.

Consistent Hashing also helps in underlying storage layer. We use consistent hashing to find the flushed chunk on the storage via content addressing.
