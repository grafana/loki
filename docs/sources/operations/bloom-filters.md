---
title: Manage bloom filter building and querying (Experimental)
menuTitle: Bloom filters
description: Describes how to enable and configure query acceleration with bloom filters.
weight:
keywords:
  - blooms
  - query acceleration
aliases:
  - ./query-acceleration-blooms
---
# Manage bloom filter building and querying (Experimental)

{{< admonition type="warning" >}}
In Loki and Grafana Enterprise Logs (GEL), Query acceleration using blooms is an [experimental feature](/docs/release-life-cycle/). Engineering and on-call support is not available. No SLA is provided. Note that this feature is intended for users who are ingesting more than 75TB of logs a month, as it is designed to accelerate queries against large volumes of logs.

In Grafana Cloud, Query acceleration using bloom filters is enabled as a [public preview](/docs/release-life-cycle/) for select large-scale customers that are ingesting more that 75TB of logs a month. Limited support and no SLA are provided.
{{< /admonition >}}

Loki leverages [bloom filters](https://en.wikipedia.org/wiki/Bloom_filter) to speed up queries by reducing the amount of data Loki needs to load from the store and iterate through.
Loki is often used to run "needle in a haystack" queries; these are queries where a large number of log lines are searched, but only a few log lines match the query.
Some common use cases are searching all logs tied to a specific trace ID or customer ID.

An example of such queries would be looking for a trace ID on a whole cluster for the past 24 hours:

```logql
{cluster="prod"} | traceID="3c0e3dcd33e7"
```

Without accelerated filtering, Loki downloads all the chunks for all the streams matching `{cluster="prod"}` for the last 24 hours and iterates through each log line in the chunks, checking if the [structured metadata][] key `traceID` with value `3c0e3dcd33e7` is present.

With accelerated filtering, Loki is able to skip most of the chunks and only process the ones where we have a statistical confidence that the structured metadata pair might be present.

To learn how to write queries to use bloom filters, refer to [Query acceleration][].

## Enable bloom filters

{{< admonition type="warning" >}}
Building and querying bloom filters are by design not supported in single binary deployment.
It can be used with Simple Scalable deployment (SSD), but it is recommended to run bloom components only in fully distributed microservice mode.
The reason is that bloom filters also come with a relatively high cost for both building and querying the bloom filters that only pays off at large scale deployments.
{{< /admonition >}}

To start building and using blooms you need to:

- Deploy the [Bloom Planner and Builder](#bloom-planner-and-builder) components (as [microservices][microservices] or via the [SSD][ssd] `backend` target) and enable the components in the [Bloom Build config][bloom-build-cfg].
- Deploy the [Bloom Gateway](#bloom-gateway) component (as a [microservice][microservices] or via the [SSD][ssd] `backend` target) and enable the component in the [Bloom Gateway config][bloom-gateway-cfg].
- Enable blooms building and filtering for each tenant individually, or for all of them by default.

```yaml
# Configuration block for the bloom creation.
bloom_build:
  enabled: true
  planner:
    planning_interval: 6h
  builder:
    planner_address: bloom-planner.<namespace>.svc.cluster.local.:9095

# Configuration block for bloom filtering.
bloom_gateway:
  enabled: true
  client:
    addresses: dnssrvnoa+_bloom-gateway-grpc._tcp.bloom-gateway-headless.<namespace>.svc.cluster.local

# Enable blooms creation and filtering for all tenants by default
# or do it on a per-tenant basis.
limits_config:
  bloom_creation_enabled: true
  bloom_split_series_keyspace_by: 1024
  bloom_gateway_enable_filtering: true
```

For more configuration options refer to the [Bloom Gateway][bloom-gateway-cfg], [Bloom Build][bloom-build-cfg] and [per tenant-limits][tenant-limits] configuration docs.
We strongly recommend reading the whole documentation for this experimental feature before using it.

## Bloom Planner and Builder

Building bloom filters from the chunks in the object storage is done by two components: the Bloom Planner and the Bloom
Builder, where the planner creates tasks for bloom building, and sends the tasks to the builders to process and upload the resulting blocks.
Bloom filters are grouped in bloom blocks spanning multiple streams (also known as series) and chunks from a given day.
To learn more about how blocks and metadata files are organized, refer to the [Building blooms](#building-blooms) section below.

The Bloom Planner runs as a single instance and calculates the gaps in fingerprint ranges for a certain time period for a tenant for which bloom filters need to be built.
It dispatches these tasks to the available builders. The planner also applies the [blooms retention](#retention).

{{< admonition type="warning" >}}
Do not run more than one instance of the Bloom Planner.
{{< /admonition >}}

The Bloom Builder is a stateless horizontally scalable component and can be scaled independently of the planner to fulfill the processing demand of the created tasks.

You can find all the configuration options for these components in the [Configure section for the Bloom Builder][bloom-build-cfg].
Refer to the [Enable bloom filters](#enable-bloom-filters) section above for a configuration snippet enabling this feature.

### Retention

The Bloom Planner applies bloom block retention on object storage. Retention is disabled by default.
When enabled, retention is applied to all tenants. The retention for each tenant is the longest of its [configured][tenant-limits] general retention (`retention_period`) and the streams retention (`retention_stream`).

For example, in the following example, tenant A has a bloom retention of 30 days, and tenant B a bloom retention of 40 days for the `{namespace="prod"}` stream.

```yaml
overrides:
    "A":
        retention_period: 30d
    "B":
        retention_period: 30d
        retention_stream:
            - selector: '{namespace="prod"}'
              priority: 1
              period: 40d
```

### Planner and Builder sizing and configuration

The single planner instance runs the planning phase for bloom blocks for each tenant in the given interval and puts the created tasks to an internal task queue.
Builders process tasks sequentially by pulling them from the queue. The amount of builder replicas required to complete all pending tasks before the next planning iteration depends on the value of `-bloom-build.planner.bloom_split_series_keyspace_by`, the number of tenants, and the log volume of the streams.

The maximum block size is configured per tenant via `-bloom-build.max-block-size`.
The actual block size might exceed this limit given that we append streams blooms to the block until the block is larger than the configured maximum size.
Blocks are created in memory and as soon as they are written to the object store they are freed. Chunks and TSDB files are downloaded from the object store to the file system.
We estimate that builders are able to process 4MB worth of data per second per core.

## Bloom Gateway

Bloom Gateways handle chunks filtering requests from the [index gateway](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/#index-gateway).
The service takes a list of chunks and a filtering expression and matches them against the blooms, filtering out those chunks not matching the given filter expression.

This component is horizontally scalable and every instance only owns a subset of the stream fingerprint range for which it performs the filtering.
The sharding of the data is performed on the client side using DNS discovery of the server instances and the [jumphash](https://arxiv.org/abs/1406.2294) algorithm for consistent hashing and even distribution of the stream fingerprints across Bloom Gateway instances.

You can find all the configuration options for this component in the Configure section for the [Bloom Gateways][bloom-gateway-cfg].
Refer to the [Enable bloom filters](#enable-bloom-filters) section above for a configuration snippet enabling this feature.

### Gateway sizing and configuration

Bloom Gateways use their local file system as a Least Recently Used (LRU) cache for blooms that are downloaded from object storage.
The size of the blooms depend on the ingest volume and number of unique structured metadata key-value pairs, as well as on build settings of the blooms, namely false-positive-rate.
With default settings, bloom filters make up <1% of the raw structured metadata size.

Since reading blooms depends heavily on disk IOPS, Bloom Gateways should make use of multiple, locally attached SSD disks (NVMe) to increase I/O throughput.
Multiple directories on different disk mounts can be specified using the `-bloom.shipper.working-directory` [setting][storage-config-cfg] when using a comma separated list of mount points, for example:

```yaml
-bloom.shipper.working-directory="/mnt/data0,/mnt/data1,/mnt/data2,/mnt/data3"
```

Bloom Gateways need to deal with relatively large files: the bloom filter blocks.
Even though the binary format of the bloom blocks allows for reading them into memory in smaller pages, the memory consumption depends on the number of pages that are concurrently loaded into memory for processing.
The product of three settings control the maximum amount of bloom data in memory at any given time: `-bloom-gateway.worker-concurrency`, `-bloom-gateway.block-query-concurrency`, and `-bloom.max-query-page-size`.

Example, assuming 4 CPU cores:

```yaml
-bloom-gateway.worker-concurrency=4      // 1x NUM_CORES
-bloom-gateway.block-query-concurrency=8 // 2x NUM_CORES
-bloom.max-query-page-size=64MiB

4 x 8 x 64MiB = 2048MiB
```

Here, the memory requirement for block processing is 2GiB.
To get the minimum requirements for the Bloom Gateways, you need to double the value.

## Building blooms

Bloom filters are built per stream and aggregated together into block files.
Streams are assigned to blocks by their fingerprint, following the same ordering scheme as Loki’s TSDB and sharding calculation.
This gives a data locality benefit when querying as streams in the same shard are likely to be in the same block.

In addition to blocks, builders maintain a list of metadata files containing references to bloom blocks and the
TSDB index files they were built from. Gateways and the planner use these metadata files to discover existing blocks.

Every `-bloom-build.planner.interval`, the planner will load the latest TSDB files for all tenants for which bloom building is enabled, and compares the TSDB files with the latest bloom metadata files.
If there are new TSDB files or any of them have changed, the planner will create a task for the streams and chunks referenced by the TSDB file.

The builder pulls a task from the planner's queue and processes the containing streams and chunks.
For a given stream, the builder will iterate through all the log lines inside its new chunks and build a bloom for the stream.
In case of changes for a previously processed TSDB file, builders will try to reuse blooms from existing blocks instead of building new ones from scratch.
The builder converts structured metadata from each log line of each chunk of a stream and appends the hash of each key, and key-value pair to the bloom, followed by the hashes combined with the chunk identifier.
The first set of hashes allows gateways to skip whole streams, while the latter is for skipping individual chunks.

For example, given structured metadata `foo=bar` in the chunk `c6dj8g`, we append to the stream bloom the following hashes: `hash("foo")`, `hash("foo=bar")`, `hash("c6dj8g" + "foo")` and `hash("c6dj8g" + "foo=bar")`.

## Query sharding

Query acceleration does not just happen while processing chunks, but also happens from the query planning phase where the query frontend applies [query sharding](https://lokidex.com/posts/tsdb/#sharding).
Loki 3.0 introduces a new [per-tenant configuration][tenant-limits] flag `tsdb_sharding_strategy` which defaults to computing shards as in previous versions of Loki by using the index stats to come up with the closest power of two that would optimistically divide the data to process in shards of roughly the same size.
Unfortunately, the amount of data each stream has is often unbalanced with the rest, therefore, some shards end up processing more data than others.

Query acceleration introduces a new sharding strategy: `bounded`, which uses blooms to reduce the chunks to be processed right away during the planning phase in the query frontend, as well as evenly distributes the amount of chunks each sharded query will need to process.

[Query acceleration]: https://grafana.com/docs/loki/<LOKI_VERSION>/query/query_acceleration
[structured metadata]: https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/labels/structured-metadata
[tenant-limits]: https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config
[bloom-gateway-cfg]: https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#bloom_gateway
[bloom-build-cfg]: https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#bloom_build
[storage-config-cfg]: https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#storage_config
[microservices]: https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#microservices-mode
[ssd]: https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#simple-scalable
