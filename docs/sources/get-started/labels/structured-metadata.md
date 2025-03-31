---
menuTitle: Structured metadata
title: What is structured metadata
description: Describes how to enable structure metadata for logs and how to query using structured metadata to filter log lines.
---
# What is structured metadata

{{< admonition type="warning" >}}
Structured metadata was added to chunk format V4 which is used if the schema version is greater or equal to `13`. See [Schema Config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/storage/#schema-config) for more details about schema versions.
{{< /admonition >}}

Selecting proper, low cardinality labels is critical to operating and querying Loki effectively. Some metadata, especially infrastructure related metadata, can be difficult to embed in log lines, and is too high cardinality to effectively store as indexed labels (and therefore reducing performance of the index).

Structured metadata is a way to attach metadata to logs without indexing them or including them in the log line content itself. Examples of useful metadata are
kubernetes pod names, process ID's, or any other label that is often used in queries but has high cardinality and is expensive
to extract at query time.

Structured metadata can also be used to query commonly needed metadata from log lines without needing to apply a parser at query time. Large json blobs or a poorly written query using complex regex patterns, for example, come with a high performance cost. Examples of useful metadata include container_IDs or user IDs.

## When to use structured metadata

You should only use structured metadata in the following situations:

- If you are ingesting data in OpenTelemetry format, using Grafana Alloy or an OpenTelemetry Collector. Structured metadata was designed to support native ingestion of OpenTelemetry data.
- If you have high cardinality metadata that should not be used as a label and does not exist in the log line.  Some examples might include `process_id` or `thread_id` or Kubernetes pod names.
- If you are using [Logs Drilldown](https://grafana.com/docs/grafana-cloud/visualizations/simplified-exploration/logs/) to visualize and explore your Loki logs.  You must set `discover_log_levels` and `allow_structured_metadata` to `true` in your Loki configuration.
- If you are a large-scale customer, who is ingesting more than 75TB of logs a month and are using [Bloom filters](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/bloom-filters/) (Experimental), starting in [Loki 3.3](https://grafana.com/docs/loki/<LOKI_VERSION>/release-notes/v3-3/) Bloom filters now utilize structured metadata.

## Enable or disable structured metadata

You enable structured metadata in the Loki config.yaml file.

```yaml
limits_config:
    allow_structured_metadata: true
    volume_enabled: true
    retention_period: 672h # 28 days retention
```

You can disable Structured Metadata by setting `allow_structured_metadata: false` in the `limits_config` section or set the command line argument `-validation.allow-structured-metadata=false`. Note structured metadata is required to support ingesting OTLP data.

## Attaching structured metadata to log lines

You have the option to attach structured metadata to log lines in the push payload along with each log line and the timestamp.
For more information on how to push logs to Loki via the HTTP endpoint, refer to the [HTTP API documentation](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/api/#ingest-logs).

Alternatively, you can use Grafana Alloy or Promtail to extract and attach structured metadata to your log lines.
See the [Promtail: Structured metadata stage](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/promtail/stages/structured_metadata/) for more information.

With Loki version 1.2.0, support for structured metadata has been added to the Logstash output plugin. For more information, see [logstash](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/logstash/).

{{< admonition type="warning" >}}
Structured metadata size is taken into account while asserting ingestion rate limiting.
Along with that, there are separate limits on how much structured metadata can be attached per log line.

```yaml
# Maximum size accepted for structured metadata per log line.
# CLI flag: -limits.max-structured-metadata-size
[max_structured_metadata_size: <int> | default = 64KB]

# Maximum number of structured metadata entries per log line.
# CLI flag: -limits.max-structured-metadata-entries-count
[max_structured_metadata_entries_count: <int> | default = 128]
```

{{< /admonition >}}

## Querying structured metadata

Structured metadata is extracted automatically for each returned log line and added to the labels returned for the query.
You can use labels of structured metadata to filter log line using a [label filter expression](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#label-filter-expression).

For example, if you have a label `pod` attached to some of your log lines as structured metadata, you can filter log lines using:

```logql
{job="example"} | pod="myservice-abc1234-56789"
```

Of course, you can filter by multiple labels of structured metadata at the same time:

```logql
{job="example"} | pod="myservice-abc1234-56789" | trace_id="0242ac120002"
```

Note that since structured metadata is extracted automatically to the results labels, some metric queries might return an error like `maximum of series (50000) reached for a single query`. You can use the [Keep](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#keep-labels-expression) and [Drop](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#drop-labels-expression) stages to filter out labels that you don't need.
For example:

```logql
count_over_time({job="example"} | trace_id="0242ac120002" | keep job  [5m])
```
