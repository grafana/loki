---
title: Migrate to TSDB
menuTitle: Migrate to TSDB
description: Migration guide for moving from any of the older indexes to TSDB
weight: 200
keywords:
  - migrate
  - tsdb
---

# Migrate to TSDB

[TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/tsdb/) is the recommended index type for Loki, and it is where current development happens.

Loki 4.0 removes the older index types, which are BoltDB (`boltdb-shipper`), Google BigTable, Apache Cassandra, Amazon DynamoDB, and the gRPC store. Loki fails to start if your schema or storage configuration still refers to one of them. If you use any of these index types, migrate to TSDB before you upgrade to Loki 4.0. For the full list of removed storage backends, refer to [Upgrading](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/#breaking-change-removal-of-deprecated-storage-backends).


### Configure TSDB index for an upcoming period

To begin the migration, add a new [period_config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#period_config) entry in your [schema_config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#schema_config).
You can read more about schema config [here](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/storage/#schema-config).

{{< admonition type="note" >}}
You must roll out the new `period_config` change to all Loki components in order for it to take effect.
{{< /admonition >}}

This example adds a new `period_config` which configures Loki to start using the TSDB index for the data ingested starting from `2023-10-20`.

```
schema_config:
  configs:
    - from: 2023-01-01
      store: boltdb-shipper
      object_store: filesystem
      schema: v11
      index:
        prefix: index_
        period: 24h
    - from: 2023-10-20 ①
      store: tsdb ②
      object_store: filesystem ③
      schema: v13 ④
      index:
        prefix: index_
        period: 24h
```

1. You must set the new period `from` to a date in the future.

1. Update the new period to use TSDB as the index type by setting `store: tsdb`.

1. This sample configuration uses filesystem as the storage in both the periods. If you want to use a different storage for the TSDB index and chunks, you can specify a different `object_store` in the new period.

1.  Update the schema to v13 which is the recommended version at the time of writing. Please refer to the [configure page](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#period_config) for the current recommended version.

{{< admonition type="note" >}}
Do this migration on Loki 3.x, because Loki 4.0 does not read the older index types. The example above keeps the old `boltdb-shipper` period so that Loki can still query the data you ingested before the migration. Loki 4.0 fails to start while that period is still in your `schema_config`. Remove the old period entries after your retention period has passed, then upgrade to Loki 4.0.
{{< /admonition >}}

### Configure TSDB shipper

It's also important that you configure the `tsdb_shipper` block in [storage_config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#storage_config). Specifically the following options:
- `active_index_directory`: directory where ingesters would write index files which will then be uploaded by shipper to configured storage.
- `cache_location`: cache location for downloading index files from the storage for use in query path.

```
storage_config:
  tsdb_shipper:
    active_index_directory: /data/tsdb-index
    cache_location: /data/tsdb-cache
```

### Run compactor

We strongly recommended running the [compactor](../../../operations/storage/retention/#compactor) when using TSDB index. It is responsible for running compaction and retention on TSDB index.
Not running index compaction will result in sub-optimal query performance.

Please refer to the [compactor section](../../../operations/storage/retention/#compactor) for more information and configuration examples.
