---
headless: true
description: Shared file for Loki OTEL content.
labels:
  products:
    - enterprise
    - oss
    - cloud
---

[//]: # 'This file documents configuring the OTEL collector to import logs to Loki'
[//]: #
[//]: # 'This shared file is included in these locations:'
[//]: # '/website/docs/grafana-cloud/send-data/logs/collect-logs-with-otel.md'
[//]: # '/loki/docs/loki/latest/send-data/otel/_index.md'
[//]: #
[//]: # 'If you make changes to this file, verify that the meaning and content are not changed in any place where the file is included.'
[//]: # 'Any links should be fully qualified and not relative: /docs/grafana/ instead of ../grafana/.'

Loki natively supports ingesting OpenTelemetry logs over HTTP.
For ingesting logs to Loki using the OpenTelemetry Collector, you must use the [`otlphttp` exporter](https://github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/otlphttpexporter).

{{< youtube id="snXhe1fDDa8" >}}

For more information about using OpenTelemetry with Grafana products, refer to the [Grafana OpenTelemetry documentation](https://grafana.com/docs/opentelemetry/).

## Loki configuration

When logs are ingested by Loki using an OpenTelemetry protocol (OTLP) ingestion endpoint, some of the data is stored as [Structured Metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/).

You must set `allow_structured_metadata` to `true` within your Loki config file. Otherwise, Loki will reject the log payload as malformed.  Note that Structured Metadata is enabled by default in Loki 3.0 and later.

```yaml
limits_config:
  allow_structured_metadata: true
```

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

- Index labels: Resource attributes map well to index labels in Loki, since both usually identify the source of the logs. The default list of Resource Attributes to store as Index labels can be configured using `default_resource_attributes_as_index_labels` under [distributor's otlp_config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#distributor). The distributor's `otlp_config` also supports `defer_attribute_expansion`, which controls whether resource and scope attributes are expanded into Structured Metadata during parsing or only when needed downstream; most users do not need to change this setting. By default, the following resource attributes will be stored as index labels, while the remaining attributes are stored as [Structured Metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/) with each log entry:
  - cloud.availability_zone
  - cloud.region
  - container.name
  - deployment.environment
  - deployment.environment.name
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

    {{< admonition type="note" >}}
    Because Loki has a default limit of 15 index labels, we recommend storing only select resource attributes as index labels. Although the default config selects more than 15 Resource Attributes, it should be fine since a few are mutually exclusive.
    {{< /admonition >}}

    {{< admonition type="tip" >}}
    For Grafana Cloud Logs, see the [current OpenTelemetry guidance](https://grafana.com/docs/grafana-cloud/send-data/otlp/otlp-format-considerations/#logs).
    {{< /admonition >}}

- Service name discovery: Loki requires every log stream to carry a `service_name` label. If the incoming OTLP data includes a non-empty `service.name` resource attribute, Loki stores it as the `service_name` index label. If `service.name` is absent or empty, Loki looks through the other index labels for the first match from a configurable discovery list (by default: `service`, `app`, `application`, `name`, and others). The matched label's value is copied to `service_name`. If no label from the list is present, `service_name` is set to `unknown_service`. You can change the discovery list with `discover_service_name` in the [limits_config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config). For the full default list and more detail, refer to [Default labels for all users](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/#default-labels-for-all-users).

- Timestamp: One of `LogRecord.TimeUnixNano` or `LogRecord.ObservedTimestamp`, based on which one is set. If both are not set, the ingestion timestamp will be used.

- LogLine: `LogRecord.Body` holds the body of the log. However, since Loki only supports Log body in string format, we will stringify non-string values using the [AsString method from the OTel collector lib](https://github.com/open-telemetry/opentelemetry-collector/blob/ab3d6c5b64701e690aaa340b0a63f443ff22c1f0/pdata/pcommon/value.go#L353).

- [Structured Metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/): Anything not stored as an Index label or LogLine is stored as Structured Metadata. This includes:
  - Resource Attributes not stored as Index labels. These are replicated and stored with each log entry.
  - Scope (InstrumentationScope) fields, replicated and stored with each log entry:
    - `scope_name`: the instrumentation scope name (when non-empty).
    - `scope_version`: the instrumentation scope version (when non-empty).
    - `scope_dropped_attributes_count`: the number of scope attributes dropped (when non-zero).
    - Any Scope Attributes not configured to be dropped.
  - LogRecord fields (other than `Body` and the primary timestamp):
    - `observed_timestamp`: stored only when both `LogRecord.TimeUnixNano` and `LogRecord.ObservedTimestamp` are set, since one of the two is already used as the entry timestamp.
    - `severity_number`: the numeric severity (when non-zero).
    - `severity_text`: the severity string such as `INFO` or `ERROR` (when non-empty). Always stored as Structured Metadata. Can also be promoted to an index label with `severity_text_as_label`.
    - `trace_id`: the hex-encoded trace ID (when present). Use this to correlate logs with traces.
    - `span_id`: the hex-encoded span ID (when present).
    - `event_name`: the OTLP event name field (when non-empty).
    - `flags`: the log record flags value (when non-zero).
    - `dropped_attributes_count`: the number of log attributes dropped by the SDK (when non-zero).
    - Any Log Attributes not configured to be dropped or promoted to index labels.

The default list of resource attributes to store as labels can be configured using `default_resource_attributes_as_index_labels` under the [distributor's otlp_config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#distributor). You can set global limits using [limits_config.otlp_config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config). If you are using Grafana Cloud, contact support to configure this setting.

{{< admonition type="caution" >}}
Because of the potential for high [cardinality](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/cardinality/), `k8s.pod.name` and `service.instance.id` are no longer recommended as default labels. But because removing these resource attributes from the default labels would be a breaking change for existing users, they have not yet been deprecated as default labels. If you are a new user of Grafana Loki, we recommend that you modify your Alloy or OpenTelemetry Collector configuration to convert `k8s.pod.name` and `service.instance.id` from index labels to structured metadata.
For sample configurations, refer to [Modify default labels](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/modify-default-labels).
{{< /admonition >}}

Things to note before ingesting OpenTelemetry logs to Loki:

- Dots (.) are converted to underscores (_).

  Loki does not support `.` or any other special characters other than `_` in label names. The unsupported characters are replaced with an `_` while converting Attributes to Index Labels or Structured Metadata.
  Also, please note that while writing the queries, you must use the normalized format, i.e. use `_` instead of special characters while querying data using OTel Attributes.

  For example, `service.name` in OTLP would become `service_name` in Loki.

- Flattening of nested Attributes

  While converting Attributes in OTLP to Index labels or Structured Metadata, any nested attribute values are flattened out using `_` as a separator.
  It is done in a similar way as to how it is done in the [LogQL json parser](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#json).

- Stringification of non-string Attribute values

  While converting Attribute values in OTLP to Index label values or Structured Metadata, any non-string values are converted to string using [AsString method from the OTel collector lib](https://github.com/open-telemetry/opentelemetry-collector/blob/ab3d6c5b64701e690aaa340b0a63f443ff22c1f0/pdata/pcommon/value.go#L353).

  ### Changing the default mapping of OTLP to Loki Format

Loki supports [per tenant](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config) OTLP config which lets you change the default mapping of OTLP to Loki format for each tenant.
It supports changing how Attributes and select LogRecord fields are stored. Here is what the config looks like:

```yaml
# OTLP log ingestion configurations
limits_config:
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
  
    # Configuration for Log Attributes to store them as index labels or
    # Structured Metadata, or to drop them altogether
    [log_attributes: <list of attributes_configs>]

    # When true, the severity_text field from the log record is also stored as
    # an index label (it is always stored as Structured Metadata regardless of
    # this setting). Not recommended unless absolutely necessary, since it can
    # increase the number of streams.
    [severity_text_as_label: <boolean> | default = false]
  
  attributes_config:
    # Configures action to take on matching Attributes. It allows one of
    # [structured_metadata, drop] for all Attribute types. It additionally allows
    # index_label action for Resource Attributes and Log Attributes
    [action: <string> | default = ""]
  
    # List of attributes to configure how to store them or drop them altogether
    [attributes: <list of strings>]
  
    # Regex to choose attributes to configure how to store them or drop them
    # altogether
    [regex: <Regexp>]
```

Promoting a Log Attribute to an index label with `action: index_label` splits log entries into a different stream whenever the value of that attribute changes. Only do this for attributes with a small, stable set of values, since high-cardinality Log Attributes promoted to labels can create a large number of streams. For example, the following configuration promotes a log attribute named `http.route` to an index label:

```yaml
limits_config:
  otlp_config:
    log_attributes:
      - action: index_label
        attributes:
          - http.route
```

{{< admonition type="note" >}}
If you are a Grafana Cloud customer, you can use the [config self-serve UI](https://grafana.com/docs/grafana-cloud/platform/security-and-account-management/account-management/cloud-stacks/grafana-dbcfg-app/logs/#otlp-label-mappings) to configure your OTLP config.
{{< /admonition >}}
  
