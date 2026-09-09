---
title: Ingesting logs to Loki using OpenTelemetry Collector
menuTitle:  OpenTelemetry
description: Configuring the OpenTelemetry Collector to send logs to Loki.
aliases: 
- ../clients/k6/
weight: 200
---

[//]: # 'Shared content for configuring the OTEL collector to import logs to Loki'
[//]: # 'This content is located in /loki/docs/sources/shared/otel.md'

# Ingesting logs to Loki using OpenTelemetry Collector

{{< docs/shared source="loki" lookup="otel.md" version="<LOKI_VERSION>">}}

Here are some example configs to change the default mapping of OTLP to Loki format:

#### Example 1

```yaml
limits_config:
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

#### Example 2

```yaml
limits_config:
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

#### Example 3

```yaml
limits_config:
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

## Handle large attributes such as stack traces

By default, Loki stores OpenTelemetry log attributes as [structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/). Two per-line limits apply to them:

- `max_structured_metadata_size`: the total size of structured metadata per log line, 64KB by default.
- `max_structured_metadata_entries_count`: the number of structured metadata entries per log line, 128 by default.

A large log attribute — for example `exception.stacktrace`, which some OpenTelemetry SDK log integrations attach when a log records an exception — can push a log line over these limits. The distributor rejects such entries with an HTTP 400 (non-retryable) response and counts them in the `loki_discarded_samples_total` metric with reason `structured_metadata_too_large` or `structured_metadata_too_many`; other valid entries in the same push request may still be ingested. For the exact error messages, refer to [Troubleshoot ingestion errors](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/troubleshooting/troubleshoot-ingest/#error-structured-metadata-too-large).

If your stack traces are being rejected, use one of the following mitigations.

### Move the stack trace into the log body

Use the OpenTelemetry Collector [transform processor](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/transformprocessor) to append the stack trace to the log body and delete the attribute:

```yaml
processors:
  transform/stacktrace:
    log_statements:
      - context: log
        statements:
          - set(body, Concat([body, attributes["exception.stacktrace"]], "\n")) where attributes["exception.stacktrace"] != nil
          - delete_key(attributes, "exception.stacktrace") where attributes["exception.stacktrace"] != nil
service:
  pipelines:
    logs:
      processors: [transform/stacktrace] # append to your existing processors list
```

The `set` statement must come before `delete_key`. The stack trace then counts against the log line size limit instead: `max_line_size`, 256KB by default, with `max_line_size_truncate` available to truncate rather than reject oversized lines. If you collect logs with Grafana Alloy, the [otelcol.processor.transform](https://grafana.com/docs/alloy/latest/reference/components/otelcol/otelcol.processor.transform/) component applies the same statements.

### Drop the attribute or adjust the limits

- If you don't need stack traces in Loki, drop the attribute at ingest with a `log_attributes` rule using `action: drop` under `limits_config.otlp_config` (Example 3 above shows the pattern). Rules are applied first-match-wins, so place the specific attribute before any catch-all regex.
- On self-managed Loki, you can raise `max_structured_metadata_size` or `max_structured_metadata_entries_count` for a tenant through runtime overrides.
- On Grafana Cloud, contact support to discuss limit adjustments.
