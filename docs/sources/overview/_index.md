---
title: Overview
weight: 150
---
# Overview of Loki

Grafana Loki is a set of components that can be composed into a fully featured
logging stack.

Unlike other logging systems, Loki is built around the idea of only indexing
labels for logs and leaving the original log message unindexed. This means
that Loki is cheaper to operate and can be orders of magnitude more efficient.

For a more detailed version of this same document, please read
[Architecture](../architecture/).

## Multi Tenancy

Loki supports multi-tenancy so that data between tenants is completely
separated. Multi-tenancy is achieved through a tenant ID (which is represented
as an alphanumeric string). When multi-tenancy mode is disabled, all requests
are internally given a tenant ID of "fake".

## Modes of Operation

Loki is optimized for both running locally (or at small scale) and for scaling
horizontally: Loki comes with a _single process mode_ that runs all of the required
microservices in one process. The single process mode is great for testing Loki
or for running it at a small scale. For horizontal scalability, the
microservices of Loki can be broken out into separate processes, allowing them
to scale independently of each other.

