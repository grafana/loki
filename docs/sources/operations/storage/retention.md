---
title: Log retention
menuTitle:  Log retention
description: Describes Grafana Loki Storage Retention
weight:  600
---
# Log retention

Retention in Grafana Loki is achieved through the [Compactor](#compactor).
It is the recommended way of applying retention for [boltdb-shipper]({{< relref "./boltdb-shipper" >}}) and [tsdb]({{< relref "./tsdb" >}}).

Though [Table Manager]({{< relref "./table-manager" >}}) can perform retention on [boltdb-shipper]({{< relref "./boltdb-shipper" >}}) and other [legacy index types]({{< relref "../../storage#index-storage" >}}), it is now deprecated.

By default `compactor.retention_enabled` flag is not set, then logs sent to Loki live forever.

Granular retention policies to apply retention at per tenant or per stream level are also supported by the Compactor.

## Compactor

The [Compactor]({{< relref "./boltdb-shipper#compactor" >}}) can deduplicate index entries. It can also apply granular retention. When applying retention with the Compactor, the [Table Manager]({{< relref "./table-manager" >}}) is unnecessary.

> Run the Compactor as a singleton (a single instance).

Compaction and retention are idempotent. If the Compactor restarts, it will continue from where it left off.

The Compactor loops to apply compaction and retention at every `compactor.compaction-interval`, or as soon as possible if running behind.

The Compactor's algorithm to update the index:

- For each table within each day:
  - Compact the table into per-tenant index files. A single index file for a tenant.
  - Traverse the per-tenant index. Use the tenant configuration to identify and mark chunks that need to be removed.
  - Remove marked chunks from the index and save their reference in a file on disk.
  - Upload the new modified index files.

Chunks are not deleted while applying the retention algorithm on the index. They are deleted asynchronously by a sweeper process
and this delay can be configured by setting `-compactor.retention-delete-delay`.

Chunks cannot be deleting immediately for the following reasons:
- Index files are refreshed from the shared store on components using it (index gateway, querier and ruler) at a specific interval. This means deleting chunks instantly could lead to some of the components still having reference to old chunks and so they could fail to execute queries. Having a delay allows for components to pull the updated index file from the store allowing the compactor to remove gracefully their reference of those chunks.
- It provides a short window of time in which to cancel chunk deletion in the case of a configuration mistake.

Compactor uses marker files to keep track of the chunks that need to be deleted.
Marker files should be stored on a persistent disk to ensure that the chunks pending for deletion are processed even if the compactor process restarts.

Note: We recommend running compactor as a statefulset (when using Kubernetes) with a persistent storage for storing marker files.

### Retention Configuration

This Compactor configuration example activates retention.

```yaml
compactor:
  working_directory: /data/retention
  shared_store: gcs
  compaction_interval: 10m
  retention_enabled: true
  retention_delete_delay: 2h
  retention_delete_worker_count: 150
schema_config:
    configs:
      - from: "2020-07-31"
        index:
            period: 24h
            prefix: loki_index_
        object_store: gcs
        schema: v11
        store: boltdb-shipper
storage_config:
    boltdb_shipper:
        active_index_directory: /data/index
        cache_location: /data/boltdb-cache
        shared_store: gcs
    gcs:
        bucket_name: loki
```

> Note that retention is only available if the index period is 24h.

Set `retention_enabled` to true. Without this, the Compactor will only compact tables.

Define `schema_config` and `storage_config` to access the storage.

The index period must be 24h.

`working_directory` is the directory where marked chunks and temporary tables will be saved.

`compaction_interval` dictates how often compaction and/or retention is applied. If the Compactor falls behind, compaction and/or retention occur as soon as possible.

`retention_delete_delay` is the delay after which the Compactor will delete marked chunks.

`retention_delete_worker_count` specifies the maximum quantity of goroutine workers instantiated to delete chunks.

#### Configuring the retention period

Retention period is configured within the [`limits_config`]({{< relref "../../configure#limits_config" >}}) configuration section.

There are two ways of setting retention policies:

- `retention_period` which is applied globally.
- `retention_stream` which is only applied to chunks matching the selector

> The minimum retention period is 24h.

This example configures global retention:

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

**NOTE:** You can only use label matchers in the `selector` field of a `retention_stream` definition. Arbitrary LogQL expressions are not supported.

Per tenant retention can be defined using the [runtime config overrides]({{< relref "../../configure#runtime-configuration-file" >}}). For example:

```yaml
overrides:
    "29":
        retention_period: 168h
        retention_stream:
        - selector: '{namespace="prod", container=~"(nginx|loki)"}'
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

A rule to apply is selected by choosing the first in this list that matches:

1. If a per-tenant `retention_stream` matches the current stream, the highest priority is picked.
2. If a global `retention_stream` matches the current stream, the highest priority is picked.
3. If a per-tenant `retention_period` is specified, it will be applied.
4. The global `retention_period` will be selected if nothing else matched.
5. If no global `retention_period` is specified, the default value of `744h` (30days) retention is used.

Stream matching uses the same syntax as Prometheus label matching:

- `=`: Select labels that are exactly equal to the provided string.
- `!=`: Select labels that are not equal to the provided string.
- `=~`: Select labels that regex-match the provided string.
- `!~`: Select labels that do not regex-match the provided string.

The example configurations will set these rules:

- All tenants except `29` and `30` in the `dev` namespace will have a retention period of `24h` hours.
- All tenants except `29` and `30` that are not in the `dev` namespace will have the retention period of `744h`.
- For tenant `29`:
  - All streams except those in the container `loki` or in the namespace `prod` will have retention period of `168h` (1 week).
  - All streams in the `prod` namespace will have a retention period of `336h` (2 weeks), even if the container label is `loki`, since the priority of the `prod` rule is higher.
  - Streams that have the container label `loki` but are not in the namespace `prod` will have a `72h` retention period.
- For tenant `30`:
  - All streams except those having the container label `nginx` will have the global retention period of `744h`, since there is no override specified.
  - Streams that have the label `nginx` will have a retention period of `24h`.

## Table Manager (deprecated)

Retention through the [Table Manager]({{< relref "./table-manager" >}}) is
achieved by relying on the object store TTL feature, and will work for both
[boltdb-shipper]({{< relref "./boltdb-shipper" >}}) store and chunk/index store.

In order to enable the retention support, the Table Manager needs to be
configured to enable deletions and a retention period. Please refer to the
[`table_manager`]({{< relref "../../configure#table_manager" >}})
section of the Loki configuration reference for all available options.
Alternatively, the `table-manager.retention-period` and
`table-manager.retention-deletes-enabled` command line flags can be used. The
provided retention period needs to be a duration represented as a string that
can be parsed using the Prometheus common model [ParseDuration](https://pkg.go.dev/github.com/prometheus/common/model#ParseDuration). Examples: `7d`, `1w`, `168h`.

> **WARNING**: The retention period must be a multiple of the index and chunks table
`period`, configured in the [`period_config`]({{< relref "../../configure#period_config" >}})
block. See the [Table Manager]({{< relref "./table-manager#retention" >}}) documentation for
more information.

> **NOTE**: To avoid querying of data beyond the retention period,
`max_query_lookback` config in [`limits_config`]({{< relref "../../configure#limits_config" >}}) must be set to a value less than or equal to
what is set in `table_manager.retention_period`.

When using S3 or GCS, the bucket storing the chunks needs to have the expiry
policy set correctly. For more details check
[S3's documentation](https://docs.aws.amazon.com/AmazonS3/latest/dev/object-lifecycle-mgmt.html)
or
[GCS's documentation](https://cloud.google.com/storage/docs/managing-lifecycles).

Currently, the retention policy for table manager can only be set globally.
per-tenant and per-stream retention policies along with support for deleting
ingested logs using an API are only supported by Compactor retention.

Since a design goal of Loki is to make storing logs cheap, a volume-based
deletion API is deprioritized. Until this feature is released, if you suddenly
must delete ingested logs, you can delete old chunks in your object store. Note,
however, that this only deletes the log content and keeps the label index
intact; you will still be able to see related labels but will be unable to
retrieve the deleted log content.

For further details on the Table Manager internals, refer to the
[Table Manager]({{< relref "./table-manager" >}}) documentation.


## Example Configuration

Example configuration with GCS with a 28 day retention:

```yaml
schema_config:
  configs:
  - from: 2018-04-15
    store: bigtable
    object_store: gcs
    schema: v11
    index:
      prefix: loki_index_
      period: 168h

storage_config:
  bigtable:
    instance: BIGTABLE_INSTANCE
    project: BIGTABLE_PROJECT
  gcs:
    bucket_name: GCS_BUCKET_NAME

limits_config:
  max_query_lookback: 672h

table_manager:
  retention_deletes_enabled: true
  retention_period: 672h
```
