---
menuTitle: Loki overview
title: Loki overview
description: Loki product overview and features.
weight: 200
aliases:
    - ../overview/
    - ../fundamentals/overview/
---

# Loki overview

Loki is a horizontally-scalable, highly-available, multi-tenant log aggregation system inspired by [Prometheus](https://prometheus.io/). Loki differs from Prometheus by focusing on logs instead of metrics, and collecting logs via push, instead of pull.

Loki is designed to be very cost effective and highly scalable. Unlike other logging systems, Loki does not index the contents of the logs, but only indexes metadata about your logs as a set of labels for each log stream.

A log stream is a set of logs which share the same labels. Labels help Loki to find a log stream within your data store, so having a quality set of labels is key to efficient query execution.

Log data is then compressed and stored in chunks in an object store such as Amazon Simple Storage Service (S3) or Google Cloud Storage (GCS), or even, for development or proof of concept, on the filesystem. A small index and highly compressed chunks simplify the operation and significantly lower the cost of Loki.

{{< figure  src="../loki-overview-2.png" caption="**Loki logging stack**" >}}

A typical Loki-based logging stack consists of 3 components:

- **Agent** - An agent or client, for example [Grafana Alloy](https://grafana.com/docs/alloy/latest/). The agent scrapes logs, turns the logs into streams by adding labels, and pushes the streams to Loki through an HTTP API.

- **Loki** - The main server, responsible for ingesting and storing logs and processing queries. It can be deployed in three different configurations, for more information see [deployment modes](../deployment-modes/).
  
- **[Grafana](https://github.com/grafana/grafana)** for querying and displaying log data. You can also query logs from the command line, using [LogCLI](../../query/logcli/) or using the Loki API directly.

## Loki features

- **Scalability** - Loki is designed for scalability, and can scale from as small as running on a Raspberry Pi to ingesting petabytes a day.
Loki can run as a single binary for simple setups, in [HA monolithic mode](../deployment-modes/#ha-monolithic-mode) for moderate horizontal scalability without added operational complexity, or as fine-grained microservices designed to run natively within Kubernetes for the largest, highest-scale installations.
<!-- vale Google.Will = NO -->
{{< admonition type="note" >}}
Simple Scalable Deployment (SSD) mode, which decoupled requests into separate read and write paths, is deprecated and will be removed in Loki 4.0. The new HA monolithic mode will be the recommended replacement for most SSD use cases. See [deployment modes](../deployment-modes/) for details.
{{< /admonition >}}
<!-- vale Google.Will = YES -->

- **Multi-tenancy** - Loki allows multiple tenants to share a single Loki instance. With multi-tenancy, the data and requests of each tenant is completely isolated from the others.
Multi-tenancy is [configured](../../operations/multi-tenancy/) by assigning a tenant ID in the agent.

- **Third-party integrations** - Several third-party agents (clients) have support for Loki, via plugins. This lets you keep your existing observability setup while also shipping logs to Loki.

- **Efficient storage** - Loki stores log data in highly compressed chunks.
Similarly, the Loki index, because it indexes only the set of labels, is significantly smaller than other log aggregation tools.
By leveraging object storage as the only data storage mechanism, Loki inherits the reliability and stability of the underlying object store. It also capitalizes on both the cost efficiency and operational simplicity of object storage over other storage mechanisms like locally attached solid state drives (SSD) and hard disk drives (HDD).  
The compressed chunks, smaller index, and use of low-cost object storage, make Loki less expensive to operate.

- **LogQL, the Loki query language** - [LogQL](../../query/) is the query language for Loki.  Users who are already familiar with the Prometheus query language, [PromQL](https://prometheus.io/docs/prometheus/latest/querying/basics/), will find LogQL familiar and flexible for generating queries against the logs.
The language also facilitates the generation of metrics from log data,
a powerful feature that goes well beyond log aggregation.

- **Alerting** - Loki includes a component called the [ruler](../../alert/), which can continually evaluate queries against your logs, and perform an action based on the result. This allows you to monitor your logs for anomalies or events. Loki integrates with [Prometheus Alertmanager](https://prometheus.io/docs/alerting/latest/alertmanager/), or the [alert manager](/docs/grafana/latest/alerting) within Grafana.

- **Grafana integration** - Loki integrates with Grafana, Mimir, and Tempo, providing a complete observability stack, and seamless correlation between logs, metrics and traces.
