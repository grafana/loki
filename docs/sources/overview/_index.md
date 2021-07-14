---
title: Overview
weight: 150
---
# Overview

Grafana Loki is a log aggregation tool,
and it is the core of a fully-featured logging stack.

Loki is a datastore optimized for efficiently holding log data.
The efficient indexing of log data
distinguishes Loki from other logging systems.
Unlike other logging systems, a Loki index is built from labels,
leaving the original log message unindexed.

![Loki overview](loki-overview-1.png)

An agent (also called a client) acquires logs,
turns the logs into streams,
and pushes the streams to Loki through an HTTP API.
The Promtail agent is designed for Loki installations,
but many other [Agents](../clients/) seamlessly integrate with Loki.

![Loki agent interaction](loki-overview-2.png)

Loki indexes streams.
Each stream identifies a set of logs associated with a unique set of labels.
A quality identification and specification of the set of labels
is key to the creation of an index that is both compact
and allows for efficient query execution.

[LogQL](../logql) is the query language for Loki.

## Loki features

-  **Efficient memory usage for indexing the logs**

    By indexing on a set of labels, the index can be significantly smaller
    than other log aggregation products.
    Less memory makes it less expensive to operate.

-  **Multi-tenancy**

    Loki allows multiple tenants to utilize a single Loki instance.
    The data of distinct tenants is completely isolated from other tentants.
    Multi-tenancy is configured by assigning a tenant ID in the agent.

-  **LogQL, Loki's query language**

    Users of the Prometheus query language, PromQL, will find LogQL familiar
    and flexible for generating queries against the logs.
    The language also facilitates the generation of metrics from log data,
    a powerful feature that goes well beyond log aggregation.

-  **Scalability**

    Loki works well at small scale. 
    In single process mode, all required microservices run in one process.
    Single process mode is great for testing Loki,
    running it locally, or running it at a small scale.

    Loki is also designed to scale out for large scale installations.
    Each of the Loki's microservice components can be broken out into
    separate processes, and configuration permits individual scaling 
    of the components.

-  **Flexibility**

    Many agents (clients) have plugin support.
    This allows a current observability structure
    to add Loki as their log aggregation tool without needing
    to switch existing portions of the observability stack.

-  **Grafana integration**

    Loki seamlessly integrates with Grafana,
    providing a complete observability stack.


