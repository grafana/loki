---
title: Ingesting logs to Loki using OpenTelemetry Collector
menuTitle:  Ingesting OpenTelemetry logs to Loki
description: Configuring the OpenTelemetry Collector to send logs to Loki.
aliases: 
- ../clients/k6/
weight:  250
---

# Ingesting logs to Loki using OpenTelemetry Collector

{{% admonition type="warning" %}}
OpenTelemetry logs ingestion is an experimental feature and is subject to change in future releases of Grafana Loki.
{{% /admonition %}}

Loki natively supports ingesting OpenTelemetry logs over HTTP.
For ingesting logs to Loki using the OpenTelemetry Collector, you must use the [`otlphttp` exporter](https://github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/otlphttpexporter).

## Loki configuration

When logs are ingested by Loki using an OpenTelemetry protocol (OTLP) ingestion endpoint, some of the data is stored as [Structured Metadata]({{< relref "../../get-started/labels/structured-metadata" >}}).
Since Structured Metadata is still an experimental feature, Loki by default rejects any writes using that feature.
To start ingesting logs in OpenTelemetry format, you need to enable `allow_structured_metadata` per tenant configuration (in the `limits_config`).

## Configure the OpenTelemetry Collector to write logs into Loki

You need to make the following changes to the [OpenTelemetry Collector config](https://opentelemetry.io/docs/collector/configuration/) to write logs to Loki on its OTLP ingestion endpoint.

```yaml
exporters:
  otlphttp:
    endpoint: http://<loki-addr>/otlp
```

And enable it in `service.pipelines`:

```yaml
service:
  pipelines:
    metrics:
      receivers: [...]
      processors: [...]
      exporters: [..., otlphttp]
```

If you want to authenticate using basic auth, we recommend the [`basicauth` extension](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/basicauthextension).

```yaml
extensions:
  basicauth/otlp:
    client_auth:
      username: username
      password: password

exporters:
  otlphttp:
    auth:
      authenticator: basicauth/otlp
    endpoint: http://<loki-addr>/otlp

service:
  extensions: [basicauth/otlp]
  pipelines:
    metrics:
      receivers: [...]
      processors: [...]
      exporters: [..., otlphttp]
```

## Format considerations

Since the OpenTelemetry protocol  differs from the Loki storage model, here is how data in the OpenTelemetry format will be mapped to the Loki data model during ingestion:

- Index labels: Resource attributes map well to index labels in Loki, since both usually identify the source of the logs. Because Loki has a limit of 30 index labels, we have selected the following resource attributes to be stored as index labels, while the remaining attributes are stored as [Structured Metadata]({{< relref "../../get-started/labels/structured-metadata" >}}) with each log entry:
  - cloud.availability_zone
  - cloud.region
  - container.name
  - deployment.environment
  - k8s.cluster.name
  - k8s.container.name
  - k8s.cronjob.name
  - k8s.daemonset.name
  - k8s.deployment.name
  - k8s.job.name
  - k8s.namespace.name
  - k8s.pod.name
  - k8s.replicaset.name
  - k8s.statefulset.name
  - service.instance.id
  - service.name
  - service.namespace

- Timestamp: One of `LogRecord.TimeUnixNano` or `LogRecord.ObservedTimestamp`, based on which one is set. If both are not set, the ingestion timestamp will be used.

- LogLine: `LogRecord.Body` holds the body of the log. However, since Loki only supports Log body in string format, we will stringify non-string values using the [AsString method from the OTEL collector lib](https://github.com/open-telemetry/opentelemetry-collector/blob/ab3d6c5b64701e690aaa340b0a63f443ff22c1f0/pdata/pcommon/value.go#L353).

- [Structured Metadata]({{< relref "../../get-started/labels/structured-metadata" >}}): Anything which can’t be stored in Index labels and LogLine would be stored as Structured Metadata. Here is a non-exhaustive list of what will be stored in Structured Metadata to give a sense of what it will hold:
  - Resource Attributes not stored as Index labels is replicated and stored with each log entry.
  - Everything under InstrumentationScope is replicated and stored with each log entry.
  - Everything under LogRecord except `LogRecord.Body`, `LogRecord.TimeUnixNano` and sometimes `LogRecord.ObservedTimestamp`.

Things to note before ingesting OpenTelemetry logs to Loki:

- Dots (.) are converted to underscores (_).

  Loki does not support `.` or any other special characters other than `_` in label names. The unsupported characters are replaced with an `_` while converting Attributes to Index Labels or Structured Metadata.
  Also, please note that while writing the queries, you must use the normalized format, i.e. use `_` instead of special characters while querying data using OTEL Attributes.

  For example, `service.name` in OTLP would become `service_name` in Loki.

- Flattening of nested Attributes

  While converting Attributes in OTLP to Index labels or Structured Metadata, any nested attribute values are flattened out using `_` as a separator.
  It is done in a similar way as to how it is done in the [LogQL json parser](/docs/loki/latest/query/log_queries/#json).

- Stringification of non-string Attribute values

  While converting Attribute values in OTLP to Index label values or Structured Metadata, any non-string values are converted to string using [AsString method from the OTEL collector lib](https://github.com/open-telemetry/opentelemetry-collector/blob/ab3d6c5b64701e690aaa340b0a63f443ff22c1f0/pdata/pcommon/value.go#L353).
