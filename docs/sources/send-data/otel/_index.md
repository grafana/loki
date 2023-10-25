---
title: Ingesting logs to Loki using OpenTelemetry Collector
menuTitle:  Ingesting OpenTelemetry logs to Loki
description: Ingesting logs to Loki using OpenTelemetry Collector
aliases: 
- ../clients/k6/
weight:  250
---

# Ingesting logs to Loki using OpenTelemetry Collector

Loki now natively supports ingesting OpenTelemetry logs over HTTP.
For ingesting logs to Loki using OpenTelemetry Collector, you need to use the [`otlphttp`](https://github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/otlphttpexporter) exporter.

# Configure the OpenTelemetry Collector to write logs into Loki

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

If you want to authenticate using basic auth, we recommend the [`basicauth`](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/basicauthextension) extension:

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

Since OTLP format differs from the Loki storage model, here is how data in the OTLP format will be mapped to the Loki data model during ingestion:

- Index labels: Resource attributes map well to index labels in Loki, since both usually identify the source of the logs. Because Loki has a limit of 30 index labels, we have selected following resource attributes to be stored as index labels, while the remaining attributes are stored as [Structured Metadata](../../get-started/labels/structured-metadata) with each log entry:
  - service.name
  - service.namespace
  - service.instance.id
  - deployment.environment
  - cloud.region
  - cloud.availability_zone
  - k8s.cluster.name
  - k8s.namespace.name
  - k8s.pod.name
  - k8s.container.name
  - container.name
  - k8s.replicaset.name
  - k8s.deployment.name
  - k8s.statefulset.name
  - k8s.daemonset.name
  - k8s.cronjob.name
  - k8s.job.name

- Timestamp: One of LogRecord.TimeUnixNano or LogRecord.ObservedTimestamp, based on which one is set. If both are not set, the ingestion timestamp would be used.

- LogLine: LogRecord.Body holds the body of the log. However, since Loki only supports Log body in string format, we will stringify non-string values using [AsString method from OTEL collector lib](https://github.com/open-telemetry/opentelemetry-collector/blob/ab3d6c5b64701e690aaa340b0a63f443ff22c1f0/pdata/pcommon/value.go#L353).

- [Structured Metadata](../../get-started/labels/structured-metadata): Anything which can’t be stored in Index labels and LogLine would be stored as Structured Metadata. Here is a non-exhaustive list of what will be stored in Structured Metadata to give a sense of what it will hold:
  - Resource Attributes not stored as Index labels is replicated and stored with each log entry.
  - Everything under InstrumentationScope is replicated and stored with each log entry.
  - Everything under LogRecord except LogRecord.Body, LogRecord.TimeUnixNano and sometimes LogRecord.ObservedTimestamp.

Things to note before ingesting OpenTelemetry logs to Loki:

- Dots (.) are converted to \_

  Loki does not support `.` or any other special characters other than `_` in label names. The unsupported characters are replace with an `_`.

  For example:

  `service.name` in OTLP would become `service_name` in Loki

- Flattening of nested Attributes

  While converting Attributes in OTLP to Index labels or Structured Metadata, any nested attribute values are flattened out using `_` as separator.
  It is done in a similar way as how it is done in [LogQL json parser](https://grafana.com/docs/loki/latest/query/log_queries/#json).

- Stringification of non-string Attribute values

  While converting Attribute values in OTLP to Index label values or Structured Metadata, any non-string values are converted to string using [AsString method from OTEL collector lib](https://github.com/open-telemetry/opentelemetry-collector/blob/ab3d6c5b64701e690aaa340b0a63f443ff22c1f0/pdata/pcommon/value.go#L353).