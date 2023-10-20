---
title: Migrate to TSDB
menuTitle: Migrate to TSDB
description: Migration guide for moving from any of the older indexes to TSDB
weight: 100
keywords:
  - migrate
  - tsdb
---

# Migrate to TSDB

[TSDB]({{< relref "../../../operations/storage/tsdb" >}}) is the recommeneded index type for Loki and is where the current development lies.
If you are running Loki with [boltb-shipper]({{< relref "../../../operations/storage/boltdb-shipper" >}}) or any of the [legacy index types]({{<relref "../../../storage#index-storage">}}) that have been deprecated,
we strongly recommend migrating to TSDB.


### Configure TSDB index for an upcoming period

To begin the migration, add a new [period_config]({{< relref "../../../configure#period_config" >}}) entry in your [schema_config]({{< relref "../../../configure#schema_config" >}}).
You can read more about schema config [here]({{< relref "../../../storage#schema-config" >}}).

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
      schema: v12 ④
      index:
        prefix: index_
        period: 24h
```

This example adds a new `period_config` which configures Loki to start using tsdb index for the data ingested from `2023-10-20`.

①  Make sure you are setting the new period `from` to a date in the future.

②  Update the new period to use **tsdb** as the index type.

③  This setup uses filesystem as the storage in both the periods. But if you desire to store the `tsdb` index along with it's chunks in a differnt object store, you can update the `object_store` in the new period.

④  Optionally update the schema to v12 which is the recommended verison at the time of writing. Please refer to [configure page]({{< relref "../../../configure#period_config" >}}) for the current recommend version.

{{% admonition type="note" %}}
It's important that you rollout the new `period_config` change to all loki components for it to take effect.
{{% /admonition %}}

### Configure TSDB shipper

It's also important that you configure the `tsdb_shipper` block in [storage_config]({{< relref "../../../configure#storage_config" >}}). Mainly the following options:
- `active_index_directory`: directory where ingesters would write index files which would then be uploaded by shipper to configured storage.
- `cache_location`: cache location for downloading index files from the storage for use in query path.

```
tsdb_shipper:
  active_index_directory: /data/tsdb-index
  cache_location: /data/tsdb-cache
```

### Run compactor

It is strongly recommended that you run compactor when using TSDB index. It is responsible for running compaction and retention on TSDB index.
Not running index compaction would result in sub-optimal query performance.

Please refer to the [compactor section]({{< relref "../../../operations/storage/retention#compactor" >}}) for more information and configuration examples.
