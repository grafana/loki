---
title: Log retention
menuTitle:  Log retention
description: Describes how Loki implements storage retention and retention configuration options.
weight:  600
---
# Log retention

Retention in Grafana Loki is achieved through the [Compactor](#compactor).
By default the `compactor.retention-enabled` flag is not set, so the logs sent to Loki live forever.

{{% admonition type="note" %}}
If you have a lifecycle policy configured on the object store, please ensure that it is longer than the retention period.
{{% /admonition %}}

Granular retention policies to apply retention at per tenant or per stream level are also supported by the Compactor.

{{% admonition type="note" %}}
The Compactor does not support retention on [legacy index types](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/storage/#index-storage). Please use the [Table Manager](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/table-manager/) when using legacy index types.
Both the Table manager and legacy index types are deprecated and may be removed in future major versions of Loki.
{{% /admonition %}}

## Compactor

The Compactor is responsible for compaction of index files and applying log retention.

{{% admonition type="note" %}}
Run the Compactor as a singleton (a single instance).
{{% /admonition %}}

The Compactor loops to apply compaction and retention at every `compactor.compaction-interval`, or as soon as possible if running behind.
Both compaction and retention are idempotent. If the Compactor restarts, it will continue from where it left off.

The Compactor's algorithm to apply retention is as follows:
- For each day or table (one table per day with 24h index period):
  - Compact multiple index files in the table into per-tenant index files. Result of compaction is a single index file per tenant per day.
  - Traverse the per-tenant index. Use the tenant configuration to identify the chunks that need to be removed.
  - Remove the references to the matching chunks from the index and add the chunk references to a marker file on disk.
  - Upload the new modified index files.

Chunks are not deleted while applying the retention algorithm on the index. They are deleted asynchronously by a sweeper process
and this delay can be configured by setting `-compactor.retention-delete-delay`. Marker files are used to keep track of the chunks pending for deletion.

Chunks cannot be deleted immediately for the following reasons:
- Index Gateway downloads a copy of the index files to serve queries and refreshes them at a regular interval.
  Having a delay allows the index gateways to pull the modified index file which would not contain any reference to the chunks marked for deletion.
  Without the delay, index files (that are stale) on the gateways could refer to already deleted chunks leading to query failures.

- It provides a short window of time in which to cancel chunk deletion in the case of a configuration mistake.

Marker files should be stored on a persistent disk to ensure that the chunks pending for deletion are processed even if the Compactor process restarts.
{{% admonition type="note" %}}
Grafana Labs recommends running Compactor as a stateful deployment (StatefulSet when using Kubernetes) with a persistent storage for storing marker files.
{{% /admonition %}}

### Retention Configuration

This Compactor configuration example activates retention.

```yaml
compactor:
  working_directory: /data/retention
  compaction_interval: 10m
  retention_enabled: true
  retention_delete_delay: 2h
  retention_delete_worker_count: 150
  delete_request_store: gcs
schema_config:
    configs:
      - from: "2020-07-31"
        index:
            period: 24h
            prefix: index_
        object_store: gcs
        schema: v13
        store: tsdb
storage_config:
    tsdb_shipper:
        active_index_directory: /data/index
        cache_location: /data/index_cache
    gcs:
        bucket_name: loki
```

{{% admonition type="note" %}}
Retention is only available if the index period is 24h. Single store TSDB and single store BoltDB require 24h index period.
{{% /admonition %}}

`retention_enabled` should be set to true. Without this, the Compactor will only compact tables.

`delete_request_store` should be set to configure the store for delete requests. This is required when retention is enabled.

`working_directory` is the directory where marked chunks and temporary tables will be saved.

`compaction_interval` dictates how often compaction and/or retention is applied. If the Compactor falls behind, compaction and/or retention occur as soon as possible.

`retention_delete_delay` is the delay after which the Compactor will delete marked chunks.

`retention_delete_worker_count` specifies the maximum quantity of goroutine workers instantiated to delete chunks.

#### Configuring the retention period

Retention period is configured within the [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config) configuration section.

There are two ways of setting retention policies:

- `retention_period` which is applied globally for all log streams.
- `retention_stream` which is only applied to log streams matching the selector.

{{% admonition type="note" %}}
The minimum retention period is 24h.
{{% /admonition %}}

This example configures global retention that applies to all tenants (unless overridden by configuring per-tenant overrides):

```yaml
...
limits_config:
  retention_period: 744h
  retention_stream:
  - selector: '{namespace="dev"}'
    priority: 1
    period: 24h
  per_tenant_override_config: /etc/overrides.yaml
...
```

{{% admonition type="note" %}}
You can only use label matchers in the `selector` field of a `retention_stream` definition. Arbitrary LogQL expressions are not supported.
{{% /admonition %}}

Per tenant retention can be defined by configuring [runtime overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#runtime-configuration-file). For example:

```yaml
overrides:
    "29":
        retention_period: 168h
        retention_stream:
        - selector: '{namespace="prod"}'
          priority: 2
          period: 336h
        - selector: '{container="loki"}'
          priority: 1
          period: 72h
    "30":
        retention_stream:
        - selector: '{container="nginx", level="debug"}'
          priority: 1
          period: 24h
```

Retention period for a given stream is decided based on the first match in this list:
1. If mutiple per-tenant `retention_stream` selectors match the stream, retention period with the highest priority is picked.
2. If multiple global `retention_stream` selectors match the stream, retention period with the highest priority is picked. This value is not considered if per-tenant `retention_stream` is set.
3. If a per-tenant `retention_period` is specified, it will be applied.
4. The global `retention_period` will be applied if none of the above match.
5. If no global `retention_period` is specified, the default value of `744h` (30days) retention is used.

{{% admonition type="note" %}}
The larger the priority value, the higher the priority.
{{% /admonition %}}

Stream matching uses the same syntax as Prometheus label matching:

- `=`: Select labels that are exactly equal to the provided string.
- `!=`: Select labels that are not equal to the provided string.
- `=~`: Select labels that regex-match the provided string.
- `!~`: Select labels that do not regex-match the provided string.

The example configurations defined above will result in the following retention periods:
- For tenant `29`:
  - Streams that have the namespace label `prod` will have a retention period of `336h` (2 weeks), even if the container label is `loki`, since the priority of the `prod` rule is higher.
  - Streams that have the container label `loki` but are not in the namespace `prod` will have a `72h` retention period.
  - For the rest of the streams in this tenant, per-tenant override `retention_period` value of `168h` is applied.
- For tenant `30`:
  - Streams that have the label `nginx` and level `debug` will have a retention period of `24h`.
  - For the rest of the streams in this tenant the global retention period of `744h`, since there is no override specified.
- All tenants except `29` and `30`:
  - Streams that have the namespace label `dev` will have a retention period of `24h` hours.
  - Streams except those with the namespace label `dev` will have the retention period of `744h`.

## Table Manager (deprecated)

Retention through the [Table Manager](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/table-manager/) is
achieved by relying on the object store TTL feature, and will work for both
[boltdb-shipper](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/boltdb-shipper/) store and chunk/index stores.

In order to enable the retention support, the Table Manager needs to be
configured to enable deletions and a retention period. Please refer to the
[`table_manager`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#table_manager)
section of the Loki configuration reference for all available options.
Alternatively, the `table-manager.retention-period` and
`table-manager.retention-deletes-enabled` command line flags can be used. The
provided retention period needs to be a duration represented as a string that
can be parsed using the Prometheus common model [ParseDuration](https://pkg.go.dev/github.com/prometheus/common/model#ParseDuration). Examples: `7d`, `1w`, `168h`.

{{% admonition type="warning" %}}
The retention period must be a multiple of the index and chunks table
`period`, configured in the [`period_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#period_config) block.
See the [Table Manager](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/table-manager/#retention) documentation for
more information.
{{% /admonition %}}

{{% admonition type="note" %}}
To avoid querying of data beyond the retention period,`max_query_lookback` config in [`limits_config`](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config) must be set to a value less than or equal to what is set in `table_manager.retention_period`.
{{% /admonition %}}

When using S3 or GCS, the bucket storing the chunks needs to have the expiry
policy set correctly. For more details check
[S3's documentation](https://docs.aws.amazon.com/AmazonS3/latest/dev/object-lifecycle-mgmt.html)
or
[GCS's documentation](https://cloud.google.com/storage/docs/managing-lifecycles).

The retention policy for Table manager can only be set globally.
Per-tenant and per-stream retention policies along with support for deleting
ingested logs using an API are only supported by Compactor retention.

Since a design goal of Loki is to make storing logs cheap, a volume-based
deletion API is deprioritized. Until this feature is released, if you suddenly
must delete ingested logs, you can delete old chunks in your object store. Note,
however, that this only deletes the log content and keeps the label index
intact; you will still be able to see related labels but will be unable to
retrieve the deleted log content.

For further details on the Table Manager internals, refer to the
[Table Manager](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/table-manager/) documentation.


## Example Configuration

Example configuration with GCS with a 28 day retention:

```yaml
schema_config:
  configs:
  - from: 2018-04-15
    store: tsdb
    object_store: gcs
    schema: v13
    index:
      prefix: loki_index_
      period: 24h

storage_config:
  tsdb_shipper:
    active_index_directory: /loki/index
    cache_location: /loki/index_cache
  gcs:
    bucket_name: GCS_BUCKET_NAME

limits_config:
  max_query_lookback: 672h # 28 days
  retention_period: 672h   # 28 days

compactor:
  working_directory: /data/retention
  delete_request_store: gcs
  retention_enabled: true
```
