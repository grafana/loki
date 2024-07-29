---
title: Ingesting logs to Loki using Alloy
menuTitle:  Grafana Alloy
description: Configuring Grafana Alloy to send logs to Loki.
weight:  250
---


# Ingesting logs to Loki using Alloy

Grafana Alloy is a versatile observability collector that can ingest logs in various formats and send them to Loki. We recommend Alloy as the primary method for sending logs to Loki, as it provides a more robust and feature-rich solution for building a highly scalable and reliable observability pipeline.

{{< figure src="/media/docs/alloy/flow-diagram-small-alloy.png" alt="Alloy flow diagram" >}}

## Installing Alloy

To get started with Grafana Alloy and send logs to Loki, you need to install and configure Alloy. You can follow the [Alloy documentation](https://grafana.com/docs/alloy/latest/get-started/install/) to install Alloy on your preferred platform.

## Components of Alloy for logs

Alloy pipelines are built using components that perform specific functions. For logs these can be broken down into three categories:

- **Collector:** These components collect/receive logs from various sources. This can be scraping logs from a file, receiving logs over HTTP, gRPC or ingesting logs from a message queue.
- **Transformer:** These components can be used to manipulate logs before they are sent to a writer. This can be used to add additional metadata, filter logs, or batch logs before sending them to a writer.
- **Writer:** These components send logs to the desired destination. Our documentation will focus on sending logs to Loki, but Alloy supports sending logs to various destinations.

### Log components in Alloy

Here is a non-exhaustive list of components that can be used to build a log pipeline in Alloy. For a complete list of components, refer to the [components list](https://grafana.com/docs/alloy/latest/reference/components/).

| Type       | Component                                                                                           |
|------------|-----------------------------------------------------------------------------------------------------|
| Collector  | [loki.source.api](https://grafana.com/docs/alloy/latest/reference/components/loki.source.api/)      |
| Collector  | [loki.source.awsfirehose](https://grafana.com/docs/alloy/latest/reference/components/loki.source.awsfirehose/) |
| Collector  | [loki.source.azure_event_hubs](https://grafana.com/docs/alloy/latest/reference/components/loki.source.azure_event_hubs/) |
| Collector  | [loki.source.cloudflare](https://grafana.com/docs/alloy/latest/reference/components/loki.source.cloudflare/) |
| Collector  | [loki.source.docker](https://grafana.com/docs/alloy/latest/reference/components/loki.source.docker/) |
| Collector  | [loki.source.file](https://grafana.com/docs/alloy/latest/reference/components/loki.source.file/)   |
| Collector  | [loki.source.gcplog](https://grafana.com/docs/alloy/latest/reference/components/loki.source.gcplog/) |
| Collector  | [loki.source.gelf](https://grafana.com/docs/alloy/latest/reference/components/loki.source.gelf/)   |
| Collector  | [loki.source.heroku](https://grafana.com/docs/alloy/latest/reference/components/loki.source.heroku/) |
| Collector  | [loki.source.journal](https://grafana.com/docs/alloy/latest/reference/components/loki.source.journal/) |
| Collector  | [loki.source.kafka](https://grafana.com/docs/alloy/latest/reference/components/loki.source.kafka/)  |
| Collector  | [loki.source.kubernetes](https://grafana.com/docs/alloy/latest/reference/components/loki.source.kubernetes/) |
| Collector  | [loki.source.kubernetes_events](https://grafana.com/docs/alloy/latest/reference/components/loki.source.kubernetes_events/) |
| Collector  | [loki.source.podlogs](https://grafana.com/docs/alloy/latest/reference/components/loki.source.podlogs/) |
| Collector  | [loki.source.syslog](https://grafana.com/docs/alloy/latest/reference/components/loki.source.syslog/) |
| Collector  | [loki.source.windowsevent](https://grafana.com/docs/alloy/latest/reference/components/loki.source.windowsevent/) |
| Collector  | [otelcol.receiver.loki](https://grafana.com/docs/alloy/latest/reference/components/otelcol.receiver.loki/) |
| Transformer| [loki.relabel](https://grafana.com/docs/alloy/latest/reference/components/loki.relabel/)            |
| Transformer| [loki.process](https://grafana.com/docs/alloy/latest/reference/components/loki.process/)            |
| Writer     | [loki.write](https://grafana.com/docs/alloy/latest/reference/components/loki.write/)                |
| Writer     | [otelcol.exporter.loki](https://grafana.com/docs/alloy/latest/reference/components/otelcol.exporter.loki/) |
| Writer     | [otelcol.exporter.logging](https://grafana.com/docs/alloy/latest/reference/components/otelcol.exporter.logging/) |


## Interactive Tutorials

To learn more about how to configure Alloy to send logs to Loki within different scenarios, follow these interactive tutorials:

- [Sending OpenTelemetry logs to Loki using Alloy]({{< relref "./examples/alloy-otel-logs" >}})
- [Sending logs over Kafka to Loki using Alloy]({{< relref "./examples/alloy-kafka-logs" >}})


