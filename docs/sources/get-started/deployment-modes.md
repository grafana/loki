---
menuTitle: Deployment modes
title: Loki deployment modes
description: Describes the three different Loki deployment models.
weight: 600
aliases:
    - ../fundamentals/architecture/deployment-modes
---
# Loki deployment modes

Loki is a distributed system consisting of many microservices. It also has a unique build model where all of those microservices exist within the same binary.

You can configure the behavior of the single binary with the `-target` command-line flag to specify which microservices will run on startup. You can further configure each of the components in the `loki.yaml` file.

Because Loki decouples the data it stores from the software which ingests and queries it, you can easily redeploy a cluster under a different mode as your needs change, with minimal or no configuration changes.

## Comparison of deployment modes

| | [Monolithic](#monilithic-mode) | [HA Monolithic](#ha-monolithic-mode) | [Microservices](#microservices-mode) |
|---|---|---|---|
| Durability | ✅ | ✅ | ✅ |
| High availability | ❌ | ✅ | ✅ |
| Separation of concerns | ❌ | ❌ | ✅ |
| Operational complexity | 🟩 low  | 🟧 medium| 🟥 high |
| Scalability | 🟥 low | 🟧 medium | 🟩 high |

## Monolithic mode

Also known as **single binary** deployment.

The simplest mode of operation is the monolithic deployment mode. You enable monolithic mode by setting the `-target=all` command line parameter. This mode runs all of Loki’s microservice components inside a single process as a single binary or Docker image.

![monolithic mode diagram](../monolithic-mode.png "Monolithic mode")

Monolithic mode is useful for getting started quickly to experiment with Loki, as well as for small read/write volumes of up to approximately 20GB per day.

Query parallelization is limited by the number of instances and the setting `max_query_parallelism` which is defined in the `loki.yaml` file.

## HA Monolithic mode

Also known as **HA single binary** deployment. This mode replaces the deprecated Simple Scalable Deployment (SSD), which will be removed in Loki 4.0.

HA monolithic mode sits between [monolithic mode](#monolithic-mode) and [microservices mode](#microservices-mode): it provides high availability and moderate horizontal scalability without the operational complexity of managing individual microservices.
If you need fine-grained, per-component scaling, use microservices mode instead.

You can horizontally scale a monolithic mode deployment to more instances by using a shared object store, and by configuring the [`ring` section](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#common) of the `loki.yaml` file to share cluster state between all instances.

### Configuration requirements

The following requirements must be met to run Loki in HA monolithic mode:

1. **Run every instance with `-target=all`.**
   All replicas use the single-binary target so that every process serves every component.

1. **Shared object storage.**
   All instances must read from and write to the same S3-compatible bucket (or equivalent) for chunk data and, if rulers are used, a separate bucket for ruler data.
   Local disk is only used for ephemeral caches and the write-ahead log (WAL).

1. **Memberlist for ring state.**
   Configure the `memberlist` block so that every instance knows the addresses of all other instances.
   Set `common.ring.kvstore.store: memberlist` so that all internal rings (ingester, distributor, and so on) use the shared memberlist cluster.

1. **Replication factor of 3.**
   Set `common.replication_factor: 3` and run **at least three** instances so that the cluster can tolerate the loss of one instance without data loss.

1. **Dedicated compactor instance.**
   Designate one instance as the main compactor by setting `compactor.horizontal_scaling_mode: main` on one instance and `worker` on all others. The address of the main compactor needs to be set in `common.compactor_grpc_address`.

1. **Load balancer in front of all instances.**
   Route all inbound push and query traffic to every replica in round-robin order.

See the [HA single binary example](https://github.com/grafana/loki/tree/main/examples/ha-singlebinary) for a more complete config.

## Microservices mode

Also known as **distributed** deployment.

The microservices deployment mode runs components of Loki as distinct processes. The microservices deployment is also referred to as a Distributed deployment. Each process is invoked specifying its `target`.
For release 3.3 the components are:

- Bloom Builder (experimental)
- Bloom Gateway (experimental)
- Bloom Planner (experimental)
- Compactor
- Distributor
- Index Gateway
- Ingester
- Overrides Exporter
- Querier
- Query Frontend
- Query Scheduler
- Ruler
- Table Manager (deprecated)

{{< admonition type="tip" >}}
You can see the complete list of targets for your version of Loki by running Loki with the flag `-list-targets`, for example:

```bash
docker run docker.io/grafana/loki:3.2.1 -config.file=/etc/loki/local-config.yaml -list-targets
```
{{< /admonition >}}

![Microservices mode diagram](../microservices-mode.png "Microservices mode")

Running components as individual microservices provides more granularity, letting you scale each component as individual microservices, to better match your specific use case.

Microservices mode deployments can be more efficient Loki installations. However, they are also the most complex to set up and maintain.

Microservices mode is only recommended for very large Loki clusters or for operators who need more precise control over scaling and cluster operations.

Microservices mode is designed for Kubernetes deployments. 
A [community-supported Helm chart](https://github.com/grafana/helm-charts/tree/main/charts/loki-distributed) is available for deploying Loki in microservices mode.
