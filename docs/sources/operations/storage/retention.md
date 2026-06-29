---
title: Log retention
menuTitle:  Log retention
description: Describes how Loki implements storage retention and retention configuration options.
weight:  600
---
# Log retention

Retention in Grafana Loki is achieved through the [Compactor](#compactor).
By default the `compactor.retention-enabled` flag is not set, so the logs sent to Loki live forever.

{{< admonition type="note" >}}
If you have a lifecycle policy configured on the object store, please ensure that it is longer than the retention period.
{{< /admonition >}}

Granular retention policies to apply retention at per tenant or per stream level are also supported by the Compactor.

## Compactor

The Compactor is responsible for compaction of index files and applying log retention.

{{< admonition type="note" >}}
Run the Compactor as a singleton (a single instance).
{{< /admonition >}}

The Compactor loops to apply compaction and retention at every `compactor.compaction-interval`, or as soon as possible if running behind.
Both compaction and retention are idempotent, which means once the action has been performed, if the action is performed multiple times, it has no further effect on logs after the first time it is performed. If the Compactor restarts, it continues from where it left off.

{{< admonition type="note" >}}
Changes to your retention period are not retroactive, that is, they are not applied to logs that have already been ingested.
{{< /admonition >}}

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
{{< admonition type="note" >}}
Grafana Labs recommends running Compactor as a stateful deployment (StatefulSet when using Kubernetes) with a persistent storage for storing marker files.
{{< /admonition >}}

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

{{< admonition type="note" >}}
Retention is only available if the index period is 24h. Single store TSDB requires a 24h index period.
{{< /admonition >}}

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

{{< admonition type="note" >}}
The minimum retention period is 24h.
{{< /admonition >}}

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

{{< admonition type="note" >}}
You can only use label matchers in the `selector` field of a `retention_stream` definition. Arbitrary LogQL expressions are not supported.
{{< /admonition >}}

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

1. If multiple per-tenant `retention_stream` selectors match the stream, retention period with the highest priority is picked.
2. If multiple global `retention_stream` selectors match the stream, retention period with the highest priority is picked. This value is not considered if per-tenant `retention_stream` is set.
3. If a per-tenant `retention_period` is specified, it will be applied.
4. The global `retention_period` will be applied if none of the above match.
5. If no global `retention_period` is specified, the default value of `0s` is used, which means logs are kept indefinitely.

{{< admonition type="note" >}}
The larger the priority value, the higher the priority.
{{< /admonition >}}

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

{{< admonition type="note" >}}
If you are a Grafana Cloud customer, you can use the [config self-serve API](https://grafana.com/docs/grafana-cloud/send-data/logs/config-self-serve/#configure-retention) to configure your tenant retention.
{{< /admonition >}}

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

## Object store lifecycle policies

Loki stores chunks and indexes as objects in the configured object store (S3, GCS, Azure Blob Storage, and so on). Chunks are deleted by the [Compactor](#compactor) according to your configured retention, but the Compactor only removes chunk objects: it never deletes the objects that hold cluster state, such as the index or the cluster seed.

If you also configure an object store lifecycle policy on the bucket, scope it carefully. A blanket "delete everything older than N days" rule will eventually remove objects that Loki must keep, which corrupts the store and causes query failures. The note at the top of this page applies here as well: any lifecycle expiration must be longer than your retention period.

{{< admonition type="warning" >}}
Never apply a lifecycle rule with an empty prefix (one that targets the whole bucket). Always scope lifecycle rules to the per-tenant chunk prefixes described below.
{{< /admonition >}}

### Bucket object layout

The following objects are written to the configured bucket. The exact prefixes depend on your `schema_config` and on whether multi-tenancy is enabled, so confirm them against your own configuration before writing a lifecycle rule.

| Object or prefix | Content | Safe to expire with a lifecycle policy? |
|---|---|---|
| `<tenant_id>/` (for example, `fake/` in single-tenant mode, or your org ID per tenant) | Chunk objects (the log data). | Yes. These are the only objects a lifecycle rule should target. |
| Index objects (under the index path prefix, `index/` by default, set by `index.path_prefix` in `schema_config`) | TSDB or BoltDB index files, written by ingesters and rewritten by the Compactor. The `index.prefix` value (for example `index_` or `loki_index_`) is the table-name prefix used inside this path, not the path itself, so match a lifecycle rule against the path prefix. | No. Loki manages the index lifecycle internally. Deleting index objects removes references to live chunks and breaks queries. |
| `loki_cluster_seed.json` | A small file at the bucket root holding the cluster seed used by usage reporting. It is written once and never rotated. | No. A blanket age-based rule will delete it once it ages past the time to live (TTL). |
| Compactor delete-request store (the `delete_request_store` you configure, such as `markers/` and the delete-request objects) | The Compactor's record of chunks pending deletion and of in-flight delete requests. | No. Removing these makes the Compactor lose track of pending deletions. |
| Ruler store objects (if the Ruler is backed by the same bucket) | Recording and alerting rule files. | No. These are configuration, not time-series data. |

### Compactor retention versus object store lifecycle policies

Compactor retention and object store lifecycle policies are two independent mechanisms, and they should not be configured in ways that conflict:

- The Compactor is index-aware. It only deletes a chunk after it has removed every reference to that chunk from the index, and it waits `compactor.retention-delete-delay` (default `2h`) before the sweeper deletes the marked chunk, so that index gateways pick up the rewritten index first.
- An object store lifecycle policy is index-unaware. It deletes objects purely by age and prefix, with no knowledge of whether a chunk is still referenced by the index.

For most deployments, enabling Compactor retention is sufficient and an object store lifecycle policy is not required. If you do add a lifecycle policy as a cost safety net, set its expiration **longer** than your retention period plus `compactor.retention-delete-delay`, so the Compactor always deletes chunks first and the lifecycle rule only ever catches objects the Compactor has already orphaned.

### Example: Amazon S3 lifecycle policy

This policy expires only chunk objects under a single tenant prefix. Replace `fake/` with your tenant prefix (use one rule per tenant, or your org ID prefix, when multi-tenancy is enabled). The `Days` value must be larger than your retention period plus the delete delay.

```json
{
  "Rules": [
    {
      "ID": "loki-chunk-expiration",
      "Status": "Enabled",
      "Filter": {
        "Prefix": "fake/"
      },
      "Expiration": {
        "Days": 395
      }
    }
  ]
}
```

Apply it with the AWS CLI:

```bash
aws s3api put-bucket-lifecycle-configuration \
  --bucket YOUR_LOKI_BUCKET \
  --lifecycle-configuration file://lifecycle.json
```

### Example: Google Cloud Storage lifecycle rule

The equivalent GCS rule expires objects by age and prefix. As with S3, scope `matchesPrefix` to the tenant chunk prefix and keep `age` larger than your retention period plus the delete delay.

```json
{
  "rule": [
    {
      "action": {"type": "Delete"},
      "condition": {
        "age": 395,
        "matchesPrefix": ["fake/"]
      }
    }
  ]
}
```

Apply it with the Google Cloud CLI:

```bash
gcloud storage buckets update gs://YOUR_LOKI_BUCKET --lifecycle-file=lifecycle.json
```

For Azure Blob Storage, use a management policy with a `prefixMatch` set to the tenant chunk prefix and the same "expiration must exceed retention plus delete delay" rule.
