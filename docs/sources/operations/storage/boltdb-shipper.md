---
title: Single Store BoltDB (boltdb-shipper)
menuTitle: BoltDB Shipper
description: Describes the deprecated boltdb-shipper single store usage.
weight: 200
---
# Single Store BoltDB (boltdb-shipper)

{{< admonition type="note" >}}
Single store BoltDB Shipper is a legacy storage option recommended for Loki 2.0 through 2.7.x and is not recommended for new deployments. The [TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/tsdb/) is the recommended index for Loki 2.8 and newer.

`boltdb-shipper` also does not support [structured metadata](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata/) or native OpenTelemetry Protocol (OTLP) ingestion. Both features require the `tsdb` index type. Because `allow_structured_metadata` defaults to `true`, you must set `allow_structured_metadata: false` in the `limits_config` section to run `boltdb-shipper`, otherwise Loki reports a config error and does not start.
{{< /admonition >}}

BoltDB Shipper lets you run Grafana Loki without any dependency on NoSQL stores for storing index.
It locally stores the index in BoltDB files instead and keeps shipping those files to a shared object store i.e the same object store which is being used for storing chunks.
It also keeps syncing BoltDB files from shared object store to a configured local directory for getting index entries created by other services of same Loki cluster.
This helps run Loki with one less dependency and also saves costs in storage since object stores are likely to be much cheaper compared to cost of a hosted NoSQL store or running a self hosted instance of Cassandra.

{{< admonition type="note" >}}
`boltdb-shipper` requires the index period to be set to 24h for the active period and for the next upcoming period that uses it. Loki fails config validation and does not start if either of those periods uses any other value. Periods that are already in the past are not checked, so existing index files created with a different period are still queryable.
If boltdb-shipper already has created index files with a 7 day period, and you want to retain previous data, add a new schema config using boltdb-shipper with a future date and index files period set to 24h.
{{< /admonition >}}

## Example Configuration

Example configuration with GCS:

```yaml
schema_config:
  configs:
    - from: 2018-04-15
      store: boltdb-shipper
      object_store: gcs
      schema: v11
      index:
        prefix: loki_index_
        period: 24h

storage_config:
  gcs:
    bucket_name: GCS_BUCKET_NAME

  boltdb_shipper:
    active_index_directory: /loki/index
    cache_location: /loki/boltdb-cache
```

This would run Loki with BoltDB Shipper storing BoltDB files locally at `/loki/index` and chunks at configured `GCS_BUCKET_NAME`.
It would also keep shipping BoltDB files periodically to same configured bucket.
It would also keep downloading BoltDB files from shared bucket uploaded by other ingesters to `/loki/boltdb-cache` folder locally.

## Operational Details

Loki can be configured to run as just a single vertically scaled instance or as a cluster of horizontally scaled single binary(running all Loki services) instances or in micro-services mode running just one of the services in each instance.
When it comes to reads and writes, Ingesters are the ones which writes the index and chunks to stores and Queriers are the ones which reads index and chunks from the store for serving requests.

Before we get into more details, it is important to understand how Loki manages index in stores. Loki shards the index as per the period configured in `schema_config`. For table based stores like Bigtable/Cassandra/DynamoDB, this was commonly set to seven days, giving a separate table per week containing index for that week.
In the case of BoltDB Shipper, the period must be set to 24h, so a table is created per day. A table is defined by a collection of many smaller BoltDB files, each file storing just 15 mins worth of index. Tables created per day are identified by a configured `prefix_` + `<period-number-since-epoch>`.
Here `<period-number-since-epoch>` in case of boltdb-shipper would be day number since epoch.
For example, if you have a prefix set to `loki_index_` and a write request comes in on 20th April 2020, it would be stored in a table named loki_index_18372 because it has been `18371` days since the epoch, and we are in `18372`th day.
Since sharding of index creates multiple files when using BoltDB, BoltDB Shipper would create a folder per day and add files for that day in that folder and names those files after ingesters which created them.

To reduce the size of files which help with faster transfer speeds and reduced storage costs, they are stored after compressing them with gzip.

To show how BoltDB files in shared object store would look like, let us consider 2 ingesters named `ingester-0` and `ingester-1` running in a Loki cluster, and
they both having shipped files for day `18371` and `18372` with prefix `loki_index_`, here is how the files would look like:

```
└── index
    ├── loki_index_18371
    │   ├── ingester-0-1587254400.gz
    │   └── ingester-1-1587255300.gz
    |   ...
    └── loki_index_18372
        ├── ingester-0-1587254400.gz
        └── ingester-1-1587254400.gz
        ...
```

{{< admonition type="note" >}}
Loki also adds a timestamp to names of the files to randomize the names to avoid overwriting files when running Ingesters with same name and not have a persistent storage. Timestamps not shown here for simplification.
{{< /admonition >}}

Let us talk about more in depth about how both Ingesters and Queriers work when running them with BoltDB Shipper.

### Ingesters

Ingesters write the index to BoltDB files in `active_index_directory`,
and the BoltDB Shipper looks for new and updated files in that directory at 1 minute intervals, to upload them to the shared object store.
When running Loki in microservices mode, there could be multiple ingesters serving write requests.
Each ingester generates BoltDB files locally.

{{< admonition type="note" >}}
To avoid any loss of index when an ingester crashes, we recommend running ingesters as a StatefulSet (when using Kubernetes) with a persistent storage for storing index files.
{{< /admonition >}}

When chunks are flushed, they are available for reads in the object store instantly. The index is not available instantly. Each ingester writes to a new BoltDB file every 15 minutes, and the BoltDB Shipper only uploads a file once it is no longer the active file for that 15 minute window, so a newly written index entry can take up to about 15 minutes to reach the shared object store.
Ingesters expose a new RPC for letting queriers query the ingester's local index for chunks which were recently flushed, but its index might not be available yet with queriers.
For all the queries which require chunks to be read from the store, queriers also query ingesters over RPC for IDs of chunks which were recently flushed.
This avoids missing any logs from queries.

### Queriers

To avoid running Queriers as a StatefulSet with persistent storage, we recommend running an Index Gateway. An Index Gateway will download and synchronize the index, and it will serve it over gRPC to Queriers and Rulers.

Queriers lazily loads BoltDB files from shared object store to configured `cache_location`.
When a querier receives a read request, the query range from the request is resolved to period numbers and all the files for those period numbers are downloaded to `cache_location`, if not already.
Once we have downloaded files for a period we keep looking for updates in shared object store and download them every 5 Minutes by default.
Frequency for checking updates can be configured with `resync_interval` config.

To avoid keeping downloaded index files forever there is a ttl for them which defaults to 24 hours, which means if index files for a period are not used for 24 hours they would be removed from cache location.
ttl can be configured using `cache_ttl` config.

Within Kubernetes, if you are not using an Index Gateway, we recommend running Queriers as a StatefulSet with persistent storage for downloading and querying index files. This will obtain better read performance, and it will avoid using node disk.

### Index Gateway

An Index Gateway downloads and synchronizes the BoltDB index from the Object Storage in order to serve index queries to the Queriers and Rulers over gRPC.
This avoids running Queriers and Rulers with a disk for persistence. Disks can become costly in a big cluster.

To run an Index Gateway, configure [StorageConfig](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#storage_config) and set the `-target` CLI flag to `index-gateway`.
To connect Queriers and Rulers to the Index Gateway, set the address (with gRPC port) of the Index Gateway with the `-boltdb.shipper.index-gateway-client.server-address` CLI flag or its equivalent YAML value under [StorageConfig](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#storage_config).

When using the Index Gateway within Kubernetes, we recommend using a StatefulSet with persistent storage for downloading and querying index files. This can obtain better read performance, avoids [noisy neighbor problems](https://en.wikipedia.org/wiki/Cloud_computing_issues#Performance_interference_and_noisy_neighbors) by not using the node disk, and avoids the time consuming index downloading step on startup after rescheduling to a new node.

### Write Deduplication disabled

Loki does write deduplication of chunks and index using Chunks and WriteDedupe cache respectively, configured with [ChunkStoreConfig](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#chunk_store_config).
The problem with write deduplication when using `boltdb-shipper` though is ingesters only keep uploading boltdb files periodically to make them available to all the other services which means there would be a brief period where some of the services would not have received updated index yet.
The problem due to that is if an ingester which first wrote the chunks and index goes down and all the other ingesters which were part of replication scheme skipped writing those chunks and index due to deduplication, we would end up missing those logs from query responses since only the ingester which had the index went down.
This problem would be faced even during rollouts which is quite common.

To avoid this, Loki disables deduplication of index whenever the replication factor is greater than 1.
While using `boltdb-shipper` avoid configuring WriteDedupe cache since it is used purely for the index deduplication, so it would not be used anyways.

### Compactor

The [Compactor](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/retention/#compactor) is a shared service, used by both `boltdb-shipper` and `tsdb`, that reduces the index size by deduping the index and merging all the files to a single file per table.
We recommend running a Compactor since a single Ingester creates 96 BoltDB files per day which include a lot of duplicate index entries, and querying multiple files per table adds up the overall query latency.

{{< admonition type="note" >}}
There should be only one compactor instance running at a time that otherwise could create problems and may lead to data loss.
{{< /admonition >}}

Example compactor configuration with GCS:

```yaml
compactor:
  working_directory: /loki/compactor

storage_config:
  gcs:
    bucket_name: GCS_BUCKET_NAME
```

#### Delete Permissions

The compactor is an optional but suggested component that combines and deduplicates the boltdb-shipper index files. When compacting index files, the compactor writes a new file and deletes unoptimized files. Ensure that the compactor has appropriate permissions for deleting files, for example, s3:DeleteObject permission for AWS S3.
