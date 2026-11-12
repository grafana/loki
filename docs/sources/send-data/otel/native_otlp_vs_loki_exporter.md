---
title: How is native OTLP endpoint different from Loki Exporter
menuTitle: Native OTLP endpoint vs Loki Exporter
description: Native OTLP endpoint vs Loki Exporter
weight: 251
---

# How is native OTLP endpoint different from Loki Exporter

The OpenTelemetry Collector's `lokiexporter` component has been removed from the OpenTelemetry Collector Contrib project. Loki's native OTLP endpoint is now the only supported way to send OpenTelemetry logs to Loki.

For information on how to send OpenTelemetry logs to Loki, and how OpenTelemetry data is mapped to Loki's data model, refer to [Ingesting logs to Loki using OpenTelemetry Collector](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/otel/).

## Migrating from the Loki Exporter

If you still have an OpenTelemetry Collector configuration that uses the `lokiexporter`, update it to use Loki's native OTLP endpoint instead:

- Point your OpenTelemetry Collector to the Loki native OTLP ingestion endpoint, as explained in [Loki configuration](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/otel/#loki-configuration).
- Rewrite any LogQL queries in dashboards, alerts, and saved Explore queries to match the native endpoint's log and label format, as described in [Format considerations](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/otel/#format-considerations). The `lokiexporter` encoded log attributes into a JSON or logfmt blob in the log line and used different default labels (for example `job` instead of `service_name`), so existing queries built against that format will need to change.
