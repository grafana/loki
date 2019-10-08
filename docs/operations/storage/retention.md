# Loki Storage Retention

Retention in Loki is achieved through the [Table Manager](./table-manager.md).
In order to enable the retention support, the Table Manager needs to be
configured to enable deletions and a retention period. Please refer to the
[`table_manager_config`](../../configuration/README.md#table_manager_config)
section of the Loki configuration reference for all available options.
Alternatively, the `table-manager.retention-period` and
`table-manager.retention-deletes-enabled` command line flags can be used. The
provided retention period needs to be a duration represented as a string that
can be parsed using Go's [time.Duration](https://golang.org/pkg/time/#ParseDuration).

> **WARNING**: The retention period must be a multiple of the index and chunks table
`period`, configured in the [`period_config`](../../configuration/README.md#period_config)
block. See the [Table Manager](./table-manager.md#retention) documentation for
more information.

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
[Table Manager](./table-manager.md) documentation.


## Example Configuration

Example configuration with GCS with a 30 day retention:

```yaml
schema_config:
  configs:
  - from: 2018-04-15
    store: bigtable
    object_store: gcs
    schema: v9
    index:
      prefix: loki_index_
      period: 168h

storage_config:
  bigtable:
    instance: BIGTABLE_INSTANCE
    project: BIGTABLE_PROJECT
  gcs:
    bucket_name: GCS_BUCKET_NAME

table_manager:
  retention_deletes_enabled: true
  retention_period: 720h
```
