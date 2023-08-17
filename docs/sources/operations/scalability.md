---
title: Scalability
description: Scaling with Grafana Loki
weight: 30
---
# Scalability

When scaling Loki, operators should consider running several Loki processes
partitioned by role (ingester, distributor, querier) rather than a single Loki
process. Grafana Labs' [production setup](https://github.com/grafana/loki/tree/main/production/ksonnet/loki)
contains `.libsonnet` files that demonstrates configuring separate components
and scaling for resource usage.

## Separate Query Scheduler

The Query frontend has an in-memory queue that can be moved out into a separate process similar to the
[Grafana Mimir query-scheduler](/docs/mimir/latest/operators-guide/architecture/components/query-scheduler/). This allows running multiple query frontends.

To run with the Query Scheduler, the frontend needs to be passed the scheduler's address via `-frontend.scheduler-address` and the querier processes needs to be started with `-querier.scheduler-address` set to the same address. Both options can also be defined via the [configuration file]({{< relref "../configuration/_index.md" >}}).

It is not valid to start the querier with both a configured frontend and a scheduler address.

The query scheduler process itself can be started via the `-target=query-scheduler` option of the Loki Docker image. For instance, `docker run grafana/loki:latest -config.file=/mimir/config/mimir.yaml -target=query-scheduler -server.http-listen-port=8009 -server.grpc-listen-port=9009` starts the query scheduler listening on ports `8009` and `9009`.

## Memory ballast

In compute-constrained environments, garbage collection can become a significant performance factor. Frequently-run garbage collection interferes with running the application by using CPU resources. The use of memory ballast can mitigate the issue. Memory ballast allocates extra, but unused virtual memory in order to inflate the quantity of live heap space. Garbage collection is triggered by the growth of heap space usage. The inflated quantity of heap space reduces the perceived growth, so garbage collection occurs less frequently.

Configure memory ballast using the ballast_bytes configuration option.
