---
title: Ingesting logs to Loki using OpenTelemetry Collector
menuTitle:  OTEL Collector
description: Configuring the OpenTelemetry Collector to send logs to Loki.
aliases: 
- ../clients/k6/
weight:  250
---

# Ingesting logs to Loki using OpenTelemetry Collector

Loki natively supports ingesting OpenTelemetry logs over HTTP.
For ingesting logs to Loki using the OpenTelemetry Collector, you must use the [`otlphttp` exporter](https://github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/otlphttpexporter).

## Loki configuration

When logs are ingested by Loki using an OpenTelemetry protocol (OTLP) ingestion endpoint, some of the data is stored as [Structured Metadata]({{< relref "../../get-started/labels/structured-metadata" >}}).

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
    logs:
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
    logs:
      receivers: [...]
      processors: [...]
      exporters: [..., otlphttp]
```

## Format considerations

Since the OpenTelemetry protocol differs from the Loki storage model, here is how data in the OpenTelemetry format will be mapped by default to the Loki data model during ingestion, which can be changed as explained later:

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
  It is done in a similar way as to how it is done in the [LogQL json parser](/docs/loki/<LOKI_VERSION>/query/log_queries/#json).

- Stringification of non-string Attribute values

  While converting Attribute values in OTLP to Index label values or Structured Metadata, any non-string values are converted to string using [AsString method from the OTEL collector lib](https://github.com/open-telemetry/opentelemetry-collector/blob/ab3d6c5b64701e690aaa340b0a63f443ff22c1f0/pdata/pcommon/value.go#L353).

### Changing the default mapping of OTLP to Loki Format

Loki supports [per tenant]({{< relref "../../configure#limits_config" >}}) OTLP config which lets you change the default mapping of OTLP to Loki format for each tenant.
It currently only supports changing the storage of Attributes. Here is how the config looks like:

```yaml
# OTLP log ingestion configurations
otlp_config:
  # Configuration for Resource Attributes to store them as index labels or
  # Structured Metadata or drop them altogether
  resource_attributes:
    # Configure whether to ignore the default list of resource attributes set in
    # 'distributor.otlp.default_resource_attributes_as_index_labels' to be
    # stored as index labels and only use the given resource attributes config
    [ignore_defaults: <boolean>]

    [attributes_config: <list of attributes_configs>]

  # Configuration for Scope Attributes to store them as Structured Metadata or
  # drop them altogether
  [scope_attributes: <list of attributes_configs>]

  # Configuration for Log Attributes to store them as Structured Metadata or
  # drop them altogether
  [log_attributes: <list of attributes_configs>]

attributes_config:
  # Configures action to take on matching Attributes. It allows one of
  # [structured_metadata, drop] for all Attribute types. It additionally allows
  # index_label action for Resource Attributes
  [action: <string> | default = ""]

  # List of attributes to configure how to store them or drop them altogether
  [attributes: <list of strings>]

  # Regex to choose attributes to configure how to store them or drop them
  # altogether
  [regex: <Regexp>]
```

Here are some example configs to change the default mapping of OTLP to Loki format:

#### Example 1:

```yaml
otlp_config:
  resource_attributes:
    attributes_config:
      - action: index_label
        attributes:
          - service.group
```

With the example config, here is how various kinds of Attributes would be stored:
* Store all 17 Resource Attributes mentioned earlier and `service.group` Resource Attribute as index labels.
* Store remaining Resource Attributes as Structured Metadata.
* Store all the Scope and Log Attributes as Structured Metadata.

#### Example 2:

```yaml
otlp_config:
  resource_attributes:
    ignore_defaults: true
    attributes_config:
      - action: index_label
        regex: service.group
```

With the example config, here is how various kinds of Attributes would be stored:
* **Only** store `service.group` Resource Attribute as index labels.
* Store remaining Resource Attributes as Structured Metadata.
* Store all the Scope and Log Attributes as Structured Metadata.

#### Example 2:

```yaml
otlp_config:
  resource_attributes:
    attributes_config:
      - action: index_label
        regex: service.group
  scope_attributes:
    - action: drop
      attributes:
        - method.name
  log_attributes:
    - action: structured_metadata
      attributes:
        - user.id
    - action: drop
      regex: .*
```

With the example config, here is how various kinds of Attributes would be stored:
* Store all 17 Resource Attributes mentioned earlier and `service.group` Resource Attribute as index labels.
* Store remaining Resource Attributes as Structured Metadata.
* Drop Scope Attribute named `method.name` and store all other Scope Attributes as Structured Metadata.
* Store Log Attribute named `user.id` as Structured Metadata and drop all other Log Attributes.