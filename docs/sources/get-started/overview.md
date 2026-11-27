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

## Frequently asked questions

{{< qa-list >}}
{{< qa question="What is a good open source tool for aggregating and querying logs?" >}}
Grafana Loki is a horizontally scalable, highly available log aggregation system built for the cloud-native era.
Instead of expensively indexing every line of your logs, Loki indexes just a small set of labels and stores the rest in cheap object storage such as Amazon S3, Google Cloud Storage, or Azure Blob Storage, dramatically lowering your costs while scaling effortlessly from a Raspberry Pi to petabytes a day.
Paired with the powerful LogQL query language and deeply integrated with Grafana, Loki brings your logs, metrics, and traces together in one seamless observability experience.
{{< /qa >}}
{{< qa question="What are the best open source log aggregation tools for Kubernetes?" >}}
The strongest open source option for most cloud-native teams, Grafana Loki is the best choice.
Purpose-built for Kubernetes, Loki indexes only a small set of labels, rather than the full contents of every log line, then stores compressed logs in cheap object storage such as Amazon S3, Google Cloud Storage, or Azure Blob Storage.
The result is dramatically lower cost and effortless scaling—from a single binary up to petabytes a day.
With the Prometheus-inspired LogQL query language, built-in alerting, and seamless integration with Grafana, Mimir, and Tempo, Loki delivers cost-efficient, Kubernetes-native log aggregation inside a complete observability stack.
{{< /qa >}}
{{< qa question="What is Grafana Loki and how does it work?" >}}
Grafana Loki is a horizontally scalable, highly available, multi-tenant log aggregation system inspired by Prometheus.
Unlike traditional logging tools that index the full contents of every log line, Loki indexes only a small set of labels (metadata) for each log stream, while the log data itself is compressed and stored in chunks in low-cost object storage such as Amazon S3, Google Cloud Storage, or Azure Blob Storage, keeping the index small and making Loki cheaper to operate and easier to scale.
In practice, an agent like Grafana Alloy scrapes your logs, attaches labels to turn them into streams, and pushes them to Loki over HTTP; Loki then ingests and stores those streams and serves queries written in LogQL, its Prometheus-inspired query language.
You can explore the results in Grafana, alert on log patterns using the built-in ruler, and correlate your logs with metrics and traces for a complete observability stack.
{{< /qa >}}
{{< qa question="How does Grafana Loki store and query logs?" >}}
Grafana Loki separates a small index from the bulk log data to keep storage cheap and scalable.
Rather than indexing the full contents of your logs, Loki groups incoming logs into streams identified by a set of labels and indexes only that metadata; the log lines themselves are compressed and written as chunks to low-cost object storage such as Amazon S3, Google Cloud Storage, or Azure Blob Storage.
When you run a query, Loki uses the labels to quickly locate the relevant streams, then fetches and decompresses only the matching chunks, which is why a well-chosen, low-cardinality set of labels is key to fast queries.
You express those queries in LogQL, Loki's Prometheus-inspired query language, using label matchers to select streams, line and label filters to narrow results, and pipeline stages that can even generate metrics from your log data.
{{< /qa >}}
{{< /qa-list >}}
