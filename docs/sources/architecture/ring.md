# Where does it live?
	- Before ingestors (after distributer)
	- why distributor doesn't have any ring
# Purpose
	- Set of nodes, dynamic load balancing
	- Problem: load blancing
	- Why normal mod%n won't work give example (scaling down or scaling up the numbber of ingestors)
	- That's why consistent hashing. (move only n/m series) to new ingestor
	- Helps in scale out
	- What content do we hash? - Labels
# What happens if no ring?

# Content Addressing
	- Helps in storing only one copy in the final storage
	- Chunk de-duplication
	- Replication factor: Goal is not whether it got stored in underlying storage, but whether it forward into at least one storage via ingestor.
	- Underlying storage takes of replication on storage layer like S3, GCS
	- How to check chunk de-dup ratio
	- De-dup makes query faster overall

# How it helps in dedup

# default value of 512 token per node (why vnode?)

# quorum consistency

# Service Discovery
	- How to find the ingestor in the ring in the first place? - Service Discovery
	- We use consul
	- Consul can also go down. When back, ingestor register itself for discovring.
	- Stop accepting the requests when its down for 'n' period. Its configurable.

# operations
- Ring page - Explain what are those columns and how does it help
- Number of tokens is configurable
- Vnode helps in evenly distribuing across the nodes. e.g: (hot node)
- vnode config is per ingestor

# Questions to Owen
1. where exactly ring lives? only serve for ingestors? why not ring for distributors?
2. Why do we use memberlist exactly?

---
title: Ring
weight: 1000
---

# Ring

This document builds upon the information in the [Loki Architecture](./) page

This document explains what exactly is the ring, what problem does it solve and how it actually works.

We use ring in Distributor and Ingestor components of Loki.

Loki in microservice mode usually can have multiple ingestors and distributors. This multiple instances of same component(ingestor or distributor) forms a ring (more presicisly Consisten Hash Ring).

// Diagram

## The Problem

Loki aggregates incoming log lines into something call Log stream. Log stream is just a set of logs associated with a single tenant and a unique labelset.

Here is the problem, say I have large number of log streams coming in and I have a bunch of Loki servers. Now I want to distribute these log streams across the servers in a way I can find it later when I want to read back the log stream. Here is the tricky part. I wan to do this without any global directory or lookup service.

So basically, we are talking problem of distribuing the log streams "deterministically"!

Problem is simple if all I have is single Loki server. Solution is send all logs into that single server. But having multiple servers makes the problem bit tricky

Another way to solve this is via hashing. We assign an integer to the servers (say 0, 1, 2, 3.. etc) and we hash our log stream (labelset + tenantID) to integer value 'h' and handover it to the server with the value 'h%n' where n is the max server.

Interestingly, this approach solves some of the problem we have, say same log stream goes to same server (because same set of labels + tenant gives same hash value), and while reading, we can hash it back to find the server where its value is stored.

But this solution lacks some scaling properties. Say if we introduce new server or remove existing server (intentionally or unintentially) then every single log stream will map to different server now.

There are several approach to solve this problem. One of the approach we use in Loki is Consistent Hashing. These servers

This servers may be Ingestors or Distributors.

## How it works

Distributors use consistent hashing in conjunction with a configurable replication factor to determine which instances of the ingester service should receive a given stream.

A stream is a set of logs associated to a tenant and a unique labelset. The stream is hashed using both the tenant ID and the labelset and then the hash is used to find the ingesters to send the stream to.

A hash ring stored in Consul is used to achieve consistent hashing; all ingesters register themselves into the hash ring with a set of tokens they own. Each token is a random unsigned 32-bit number. Along with a set of tokens, ingesters register their state into the hash ring. The state JOINING, and ACTIVE may all receive write requests, while ACTIVE and LEAVING ingesters may receive read requests. When doing a hash lookup, distributors only use tokens for ingesters who are in the appropriate state for the request.

To do the hash lookup, distributors find the smallest appropriate token whose value is larger than the hash of the stream. When the replication factor is larger than 1, the next subsequent tokens (clockwise in the ring) that belong to different ingesters will also be included in the result.

The effect of this hash set up is that each token that an ingester owns is responsible for a range of hashes. If there are three tokens with values 0, 25, and 50, then a hash of 3 would be given to the ingester that owns the token 25; the ingester owning token 25 is responsible for the hash range of 1-25.

### Quorum consistency
Since all distributors share access to the same hash ring, write requests can be sent to any distributor.

To ensure consistent query results, Loki uses Dynamo-style quorum consistency on reads and writes. This means that the distributor will wait for a positive response of at least one half plus one of the ingesters to send the sample to before responding to the client that initiated the send.

### Ring Status page

### Other uses of consistent hashing.

Use to find the flushed chunk on the storage via content addressing.
