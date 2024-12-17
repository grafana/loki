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

## Monolithic mode

The simplest mode of operation is the monolithic deployment mode. You enable monolithic mode by setting the `-target=all` command line parameter. This mode runs all of Loki’s microservice components inside a single process as a single binary or Docker image.

![monolithic mode diagram](../monolithic-mode.png "Monolithic mode")

Monolithic mode is useful for getting started quickly to experiment with Loki, as well as for small read/write volumes of up to approximately 20GB per day.

You can horizontally scale a monolithic mode deployment to more instances by using a shared object store, and by configuring the [`ring` section](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#common) of the `loki.yaml` file to share state between all instances, but the recommendation is to use simple scalable mode if you need to scale your deployment.

You can configure high availability by running two Loki instances using `memberlist_config` configuration and a shared object store and setting the `replication_factor` to `3`. You route traffic to all the Loki instances in a round robin fashion.

Query parallelization is limited by the number of instances and the setting `max_query_parallelism` which is defined in the `loki.yaml` file.

## Simple Scalable

The simple scalable deployment is the default configuration installed by the [Loki Helm Chart]({{< relref "../setup/install/helm" >}}). This deployment mode is the easiest way to deploy Loki at scale. It strikes a balance between deploying in [monolithic mode](#monolithic-mode) or deploying each component as a [separate microservice](#microservices-mode).

{{< admonition type="note" >}}
This deployment mode is sometimes referred to by the acronym SSD for simple scalable deployment, not to be confused with solid state drives. Loki uses an object store.
{{< /admonition >}}

Loki’s simple scalable deployment mode separates execution paths into read, write, and backend targets. These targets can be scaled independently, letting you customize your Loki deployment to meet your business needs for log ingestion and log query so that your infrastructure costs better match how you use Loki.

The simple scalable deployment mode can scale up to a few TBs of logs per day, however if you go much beyond this, the microservices mode will be a better choice for most users.

![Simple scalable mode diagram](../scalable-monolithic-mode.png "Simple scalable mode")

The three execution paths in simple scalable mode are each activated by appending the following arguments to Loki on startup:

- `-target=write` - The write target is stateful and is controlled by a Kubernetes StatefulSet. It contains the following components:
  * Distributor
  * Ingester
- `-target=read` - The read target is stateless and can be run as a Kubernetes Deployment that can be scaled automatically (Note that in the official helm chart it is currently deployed as a stateful set). It contains the following components:
  * Query Frontend
  * Querier
- `-target=backend` - The backend target is stateful, and is controlled by a Kubernetes StatefulSet. Contains the following components:
  - Compactor
  - Index Gateway
  - Query Scheduler
  - Ruler
  - Bloom Planner (experimental)
  - Bloom Builder (experimental)
  - Bloom Gateway (experimental)

The simple scalable deployment mode requires a reverse proxy to be deployed in front of Loki, to direct client API requests to either the read or write nodes. The Loki Helm chart includes a default reverse proxy configuration, using Nginx.

## Microservices mode
The microservices deployment mode runs components of Loki as distinct processes. Each process is invoked specifying its `target`.
For release 3.2 the components are:

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
