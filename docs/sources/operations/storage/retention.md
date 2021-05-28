---
title: Retention
---
# Loki Storage Retention

Retention in Loki is achieved either through the [Table Manager](#table-manager) or the [Compactor](#Compactor).

## Which one should you use ?

Retention through the [Table Manager](../table-manager/) is achieved by relying on the object store TTL feature and will work for both [boltdb-shipper](../boltdb-shipper) store and chunk/index store. However retention through the [Compactor](../boltdb-shipper#compactor) is only supported when using [boltdb-shipper](../boltdb-shipper) store.

Going forward the [Compactor](#Compactor) retention will become the default and long term supported one. While this retention is still **experimental** it supports more granular retention (per tenant and per stream) use cases.

## Compactor

As explained in the [Compactor](../boltdb-shipper#compactor) section, the compactor can deduplicate index entries but can also apply granular retention when activated.
When applying retention with the Compactor, you can totally remove the [Table Manager](../table-manager/) from your Loki workload as it become totally unnecessary.

> The compactor should always be run as a singleton. (single instance)

### How it works ?

Once activated, the Compactor will run a in loop that will apply compaction and retention at every `compaction_interval`. If it runs behind, the will executed as soon as possible.

The execution is as follow:

- First the compactor will download all available tables (previously uploaded by ingesters) for each day.
- For each day and table it will:
  - First compact the table into a single index file.
  - Then traverse the whole index to mark chunks that need to be removed based on the tenant configuration.
  - Marked chunks are directly removed from the index and their reference is saved into a file on disk.
  - Finally re-upload the new modified index files.

Retention is applying directly on the index, however chunks are not deleted directly. They actually will be deleted by the compactor asynchronously in another loop (sweeper).

Marked chunks will only  be deleted after `retention_delete_delay` configured is expired because:

1. boltdb-shipper indexes are refreshed from the shared store on components using it (querier and ruler) at a specific interval. This means deleting chunks instantly could lead to components still having reference to old chunks and so they could fails to execute queries. Having a delay allows for components to refresh their store and so remove gracefully their reference of those chunks.

2. It gives you a short period to cancel chunks deletion in case of mistakes.

Marker files (containing chunks to deletes), should be store on a persistent disk, since this is the sole reference to them anymore.

### Configuration

The following compactor config shows how to activate retention with the compactor.

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

As you can see the retention needs `schema_config` and `storage_config` to access the storage.
To activate retention you'll need to set `retention_enabled` to true otherwise the compactor will only compact tables.

The `working_directory` is the directory where marked chunks and temporary tables will be saved.
The `compaction_interval` dictates how often retention and/or compaction is applied, if it falls behind it will execute as soon as possible.
`retention_delete_delay` is the delay after which the compactor will actually delete marked chunks.

And finally `retention_delete_worker_count` setup the amount of goroutine to use to delete chunks.

> Compaction and retention is idempotent, in case of restart the compactor will restart where he left off.

#### Configuring retention period

So far we've only talked about how to setup the compactor to apply retention, in this section we will explain how to configure the actual retention period to apply.

Retention period is configure via the [`limits_config`](./../../../configuration/#limits_config) configuration section. It supports two type of retention `retention_stream` which is applied to all chunks matching the selector

The example below can be added to your Loki configuration to configure global retention.

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

Per tenant retention can be defined using the `/etc/overrides.yaml` files. For example:

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
        - selector: '{container="nginx"}'
          priority: 1
          period: 24h
```

The most specific matching rule is selected. The rule selection is as follow:

- If a per-tenant `retention_stream` matches the current stream, the highest priority is picked.
- Otherwise if a global `retention_stream` matches the current stream, the highest priority is picked.
- Otherwise if a per-tenant `retention_period` is specified it will be applied.
- The the global `retention_period` will be selected if nothing else matched.
- And finally if no global `retention_period` is set, the default `30days (744h)` retention is selected.

Stream matching is using the same syntax as Prometheus label matching:

- `=`: Select labels that are exactly equal to the provided string.
- `!=`: Select labels that are not equal to the provided string.
- `=~`: Select labels that regex-match the provided string.
- `!~`: Select labels that do not regex-match the provided string.

The two configurations will, for example, set those rules:

- All tenants except `29` and `30` will have the retention for streams that are in the `dev` namespace to `24h` hours, and other streams will have a retention of `744h`.
  - For tenant `29`:
    - All streams except, the container `loki` or the `namespace` prod, will have retention of 1 week (`168h`).
    - All streams in `prod` will have a retention of `336h` (2 weeks), even the if the container label is also `loki` since the priority of the `prod` rule is higher.
    - Steams that have the label container `loki` but are not in the namespace `prod` will have 72h retention.
  - For tenant `30`:
    - All streams, except those having the label container `nginx` will have a retention of `744h` (the global retention,since it doesn't overrides a new one).
    - Streams that have the label `nginx` will have a retention of `24h`.

> The minimum retention period is 24h

## Table Manager

In order to enable the retention support, the Table Manager needs to be
configured to enable deletions and a retention period. Please refer to the
[`table_manager_config`](../../../configuration#table_manager_config)
section of the Loki configuration reference for all available options.
Alternatively, the `table-manager.retention-period` and
`table-manager.retention-deletes-enabled` command line flags can be used. The
provided retention period needs to be a duration represented as a string that
can be parsed using Go's [time.Duration](https://golang.org/pkg/time/#ParseDuration).

> **WARNING**: The retention period must be a multiple of the index and chunks table
`period`, configured in the [`period_config`](../../../configuration#period_config)
block. See the [Table Manager](../table-manager#retention) documentation for
more information.

> **NOTE**: To avoid querying of data beyond the retention period,
`max_look_back_period` config in [`chunk_store_config`](../../../configuration#chunk_store_config) must be set to a value less than or equal to
what is set in `table_manager.retention_period`.

When using S3 or GCS, the bucket storing the chunks needs to have the expiry
policy set correctly. For more details check
[S3's documentation](https://docs.aws.amazon.com/AmazonS3/latest/dev/object-lifecycle-mgmt.html)
or
[GCS's documentation](https://cloud.google.com/storage/docs/managing-lifecycles).

Currently, the retention policy can only be set globally. A per-tenant retention
policy with an API to delete ingested logs is still under development.

Since a design goal of Loki is to make storing logs cheap, a volume-based
deletion API is deprioritized. Until this feature is released, if you suddenly
must delete ingested logs, you can delete old chunks in your object store. Note,
however, that this only deletes the log content and keeps the label index
intact; you will still be able to see related labels but will be unable to
retrieve the deleted log content.

For further details on the Table Manager internals, refer to the
[Table Manager](../table-manager/) documentation.


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

chunk_store_config:
  max_look_back_period: 672h

table_manager:
  retention_deletes_enabled: true
  retention_period: 672h
```
