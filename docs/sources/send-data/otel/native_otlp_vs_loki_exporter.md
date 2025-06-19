---
title: How is native OTLP endpoint different from Loki Exporter
menuTitle:  Native OTLP endpoint vs Loki Exporter
description: Native OTLP endpoint vs Loki Exporter
weight:  251
---

# How is native OTLP endpoint different from Loki Exporter

## Introduction

OpenTelemetry (OTel) is quickly becoming an industry standard with increasing adoption. Prior to the Loki 3.0 release, there was no native support for ingesting OTel logs to Loki, which led to creation of the [LokiExporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/exporter/lokiexporter/README.md). While the LokiExporter got the job done of ingesting OTel logs to Loki, it did not provide a native user experience, and the querying experience was not optimal. As part of our effort to improve user experience with OTel, we added native OTel log ingestion support to Loki with 3.0 release.

## What has changed?

### Formatting of logs

[LokiExporter's README](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/exporter/lokiexporter/README.md) explains in detail the formatting of OTel logs ingested via LokiExporter, while Loki’s native OTel log ingestion endpoint is described in detail in its [documentation](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/otel/). Here is a summary of how the logs are formatted between the two:

**LokiExporter**

- Index Labels:
  - It supports label control via hints set by the OTel client/collector.
  - Default labels:
    - job=service.namespace/service.name
    - instance=service.instance.id
    - exporter=OTLP
    - level=severity
- Log Body: Encodes log records and attributes in json(default) or logfmt format.

**Loki’s native OTel log ingestion endpoint**

- Index Labels:
  - It supports label control via per-tenant OTLP configuration in Loki.
  - By default, it picks some pre-configured resource attributes as index labels as explained [here](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/otel/#format-considerations).
- LogLine: Stringified LogRecord.Body.
- Structured Metadata: Anything not stored in Index labels and LogLine gets stored as [Structured Metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/).

### Sample Log

Let us take the following sample OTel log and see how it would look after ingestion in both formats with the default configuration:
- Resource Attributes:
  - service.name: "auth"
  - service.namespace: "dev"
  - service.kind: "app"
- Log Record:
  - Timestamp: 1715247552000000000
  - Body: "user logged in"
  - Severity Text: "INFO"
  - Attributes:
    - email: "foo@bar.com"

**Ingested with LokiExporter:**

- Index Labels:
  - job="dev/auth"
  - exporter="OTLP"
- Log Body: `{"body":"user logged in","severity":"INFO","attributes":{"email":"foo@bar.com"}, "resources":{"service.kind":"app","service.name":"auth","service.namespace":"dev"}}`

**Ingested with Loki’s native OTel endpoint:**

- Index Labels:
  - service_name="auth"
  - service_namespace="dev"
- Log Body: "user logged in"
- Structured Metadata:
  - service_kind: "app"
  - severity_text: "INFO"
  - email: "foo@bar.com"

### Querying experience

As seen earlier, LokiExporter encodes data to json or logfmt blob, which requires decoding it at query time to interact with OTel attributes.
However, the native OTel endpoint doesn't do any encoding of data before storing it. It leverages [Structured Metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/), which makes it easier to interact with OTel attributes without having to use any parsers in the queries.
Taking the above-ingested log line, let us look at how the querying experience would look between the two considering various scenarios:

**Query all logs without any filters**

- Ingested with LokiExporter: `{job="dev/auth"}`
- Ingested with Loki’s native OTel endpoint: `{service_name="auth", service_namespace="dev"}`

**Query logs with severity INFO**

- Ingested with LokiExporter: `{job="dev/auth"} | json | severity="INFO"`
- Ingested with Loki’s native OTel endpoint: `{service_name="auth", service_namespace="dev"} | severity_text="INFO"`

**Display log message as log line in logs query results**

- Ingested with LokiExporter: `{job="dev/auth"} | json | line_format "{{.body}}"`
- Ingested with Loki’s native OTel endpoint: `{service_name="auth", service_namespace="dev"}`

**Count login events in the last hour by email**

- Ingested with LokiExporter: `sum(count_over_time({job="dev/auth"} |= "user logged in" | json[1h])) by (email)`
- Ingested with Loki’s native OTel endpoint: `sum(count_over_time({service_name="auth", service_namespace="dev"} |= "user logged in"[1h])) by (email)`

## Benefits of switching from LokiExporter to native OTel endpoint:

- **Future improvements:** There is an ongoing discussion on deprecating LokiExporter, which would stop receiving future enhancements. Loki’s native OTel endpoint represents the future of our product, and all future development and enhancements will be focused there. Upgrading to the native endpoint will ensure you benefit from the latest enhancements and the best possible user experience.
- **Simplified client config:** LokiExporter requires setting hints for managing stream labels, which defeats the purpose of choosing OTel in the first place since you can’t switch from one OTel-compatible storage to another without having to reconfigure your clients. With Loki’s native OTel endpoint, there is no added complexity in setting hints for your client to manage the labels.
- **Simplified querying:** Loki’s native OTel endpoint leverages Structured Metadata, which makes it easier to reference the attributes and other fields from the OTel LogRecord without having to use json or logfmt parsers for decoding the data at query time.
- **More modern high context data model:** Better aligned with modern observability practices than the older json based model.

## What do you need to do to switch from LokiExporter to native OTel ingestion format?

- Point your OpenTelemetry Collector to the Loki native OTel ingestion endpoint as explained [here](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/otel/#loki-configuration).
- Rewrite your LogQL queries in various places, including dashboards, alerts, starred queries in Grafana Explore, etc. to query OTel logs as per the new format.
