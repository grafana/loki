---
title: "OpenTelemetry / OTLP"
description: ""
lead: ""
date: 2024-10-25T12:43:23+02:00
lastmod: 2024-10-25T12:43:23+02:00
draft: false
images: []
menu:
  docs:
    parent: "user-guides"
weight: 100
toc: true
---

## Introduction

Loki 3.0 introduced an API endpoint using the OpenTelemetry Protocol (OTLP) as a new way of ingesting log entries into Loki. This endpoint is an addition to the standard Push API that was available in Loki from the start.

Because OTLP is not specifically geared towards Loki but is a standard format, it needs additional configuration on Loki's side to map the OpenTelemetry data format to Loki's data model.

Specifically, OTLP has no concept of "stream labels" or "structured metadata". Instead, OTLP provides metadata about a log entry in _attributes_ that are grouped into three buckets (resource, scope and log), which allows setting metadata for many log entries at once or just on a single entry depending on what's needed.

## Prerequisites

Log ingestion using OTLP depends on structured metadata being available in Loki. This capability was introduced with schema version 13, which is available in Loki Operator when using `v13` in a schema configuration entry.

If you are creating a new `LokiStack`, make sure to set `version: v13` in the storage schema configuration.

If there is an existing schema configuration, a new schema version entry needs to be added, so that it becomes active in the future (see [Upgrading Schemas](loki-upgrading-schemas) in the Loki documentation).

```yaml
# [...]
spec:
  storage:
    schemas:
    - version: v13
      effectiveDate: 2024-10-25
```

Once the `effectiveDate` has passed your `LokiStack` will be using the new schema configuration and is ready to store structured metadata.

## Attribute Mapping

Loki splits the configuration for mapping OTLP attributes to stream labels and structured metadata into two places:

- `default_resource_attributes_as_index_labels` in the [`distributor` configuration](loki-docs-distributor-config)
- `otlp_config` in the `limits_config` (see [Loki documentation](loki-docs-limits-config))

By default, `default_resource_attributes_as_index_labels` provides a set of resource-attributes that are mapped to stream-labels on the Loki side.

As the field in the distributor configuration is limited to resource-level attributes and can only produce stream-labels as an output, the `otlp_config` needs to be used to map resource, scope or log level attributes to structured metadata.

The Loki Operator does not use the same approach for configuring the attributes as Loki itself does. The most visible difference is that there is no distinction between the `distributor` and `limits` configuration in the Operator.

Instead, the Loki Operator only uses the `limits` configuration for all its attributes. The structure of the `limits` configuration also differs from the structure in the Loki configuration file. See [Custom Attribute Mapping](#custom-attribute-mapping) below for an explanation of the configuration options available in the Operator.

### Picking Stream Labels and Structured Metadata

Whether you choose to map an attribute to a stream label or to structured metadata depends on what data is present in the attribute. Stream labels are used to identify a set of log entries "belonging together" in a stream of events. These labels are used for indexing and identifying the streams and so should not contain information that changes between different log entries of the same application. They should also not contain values that have a high number of different values ("cardinality").

Structured metadata on the other hand is just saved together with the log entries and only read when querying for logs, so it's more suitable to store "any data". Both stream labels and structured metadata can be used to filter log entries during a query.

**Note:** Attributes that are not mapped to either a stream label or structured metadata will not be stored into Loki.

### Loki Operator Defaults

When using the Loki Operator the default attribute mappings depend on the [tenancy mode]({{< ref "api.md#loki-grafana-com-v1-ModeType" >}}) used for the `LokiStack`:

- `static` and `dynamic` use the Grafana defaults
- `openshift-logging` uses OpenShift defaults

### Custom Attribute Mapping

All tenancy modes support customization of the attribute mapping configuration. This can be done globally (for all tenants) or on a per-tenant basis. When a custom attribute mapping configuration is defined, then the Grafana defaults are not used. If the default labels are desired as well, they need to be added to the custom configuration. See also the section about [Customizing OpenShift Defaults](#customizing-openshift-defaults) below.

**Note:** A major difference between the Operator and Loki is how it handles inheritance. Loki by default only copies the attributes defined in the `default_resource_attributes_as_index_labels` setting to the tenants whereas the Operator will copy all global configuration into every tenant.

The attribute mapping configuration in `LokiStack` is done through the limits configuration:

```yaml
# [...]
spec:
  limits:
    global:
      otlp: {} # Global OTLP Attribute Configuration
    tenants:
      example-tenant:
        otlp: {} # OTLP Attribute Configuration for tenant "example-tenant"
```

Both global and per-tenant OTLP configurations can map attributes to stream-labels or structured-metadata. At least _one stream-label_ is needed for successfully saving a log entry to Loki storage, so the configuration should account for that.

Stream labels can only be generated from resource-level attributes, which is mirrored in the data structure of the `LokiStack` resource:

```yaml
# [...]
spec:
  limits:
    global:
      otlp:
        streamLabels:
          resourceAttributes:
          - name: "k8s.namespace.name"
          - name: "k8s.pod.name"
          - name: "k8s.container.name"
```

Structured metadata on the other hand can be generated from all types of attributes (resource, scope and log):

```yaml
# [...]
spec:
  limits:
    global:
      otlp:
        streamLabels:
          # [...]
        structuredMetadata:
          resourceAttributes:
          - name: "process.command_line"
          - name: "k8s\\.pod\\.labels\\..+"
            regex: true
          scopeAttributes:
          - name: "service.name"
          logAttributes:
          - name: "http.route"
```

The previous example also shows that the attribute names can be expressed as _regular expressions_ by setting `regex: true`.

Using a regular expression makes sense when there are many attributes with similar names that should be mapped into Loki. It is not recommended to be used for stream labels, as it can potentially create a lot of data.

### Customizing OpenShift Defaults

The `openshift-logging` tenancy mode contains its own set of default attributes. Some of these attributes (called "required attributes") can not be removed by applying a custom configuration, because they are needed for other OpenShift components to function properly. Other attributes (called "recommended attributes") are provided but can be disabled in case they influence performance negatively. The complete set of attributes is documented in the [data model](rhobs-data-model) repository.

Because the OpenShift attribute configuration is applied based on the tenancy mode, the simplest configuration is to just set the tenancy mode and not apply any custom attributes. This will provide instant compatibility with the other OpenShift tools.

In case additional attributes are needed, either as stream labels or structured metadata, the normal custom attribute configuration mentioned above can be used. Attributes defined in the custom configuration will be **merged** with the default configuration.

#### Removing Recommended Attributes

In case of issues with the default set of attributes, there is a way to slim down the default set of attributes applied to a LokiStack operating in `openshift-logging` tenancy mode:

```yaml
# [...]
spec:
  tenants:
    mode: openshift-logging
    openshift:
      otlp:
        disableRecommendedAttributes: true # Set this to remove recommended attributes
```

Setting `disableRecommendedAttributes: true` reduces the set of default attributes to only the "required attributes".

This option is meant for situations when some of the default attributes cause performance issues during ingestion of logs or if the default set causes excessive use of storage.

Because the set of required attributes only contains a subset of the default stream labels, only setting this option will negatively affect query performance. It needs to be combined with a custom attribute configuration that reintroduces attributes that are needed for queries so that the data contained in those attributes is available again.

## References

- [Loki Labels](loki-labels)
- [Structured Metadata](loki-structured-metadata)
- [OpenTelemetry Attribute](otel-attributes)
- [OpenShift Default Attributes](rhobs-data-model)

[loki-docs-distributor-config]: https://grafana.com/docs/loki/latest/configure/#distributor
[loki-docs-limits-config]: https://grafana.com/docs/loki/latest/configure/#limits_config
[loki-labels]: https://grafana.com/docs/loki/latest/get-started/labels/
[loki-structured-metadata]: https://grafana.com/docs/loki/latest/get-started/labels/structured-metadata/
[loki-upgrading-schemas]: https://grafana.com/docs/loki/latest/configure/storage/#upgrading-schemas
[otel-attributes]: https://opentelemetry.io/docs/specs/otel/common/#attribute
[rhobs-data-model]: https://github.com/rhobs/observability-data-model/blob/main/cluster-logging.md#attributes
