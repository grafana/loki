---
title: Query Acceleration with Blooms (Experimental) 
menuTitle: Query Acceleration with Blooms 
description: Describes how to enable and configure query acceleration with blooms. 
weight: 
keywords:
  - blooms
  - query acceleration
---

# Query Acceleration with Blooms (Experimental)
{{% admonition type="warning" %}}
This feature is an [experimental feature](/docs/release-life-cycle/). Engineering and on-call support is not available.  No SLA is provided.  
{{% /admonition %}}

Loki 3.0 leverages [bloom filters](https://en.wikipedia.org/wiki/Bloom_filter) to speed up queries by reducing the 
amount of data Loki needs to load from the store and iterate through. Loki is often used to run “needle in a haystack” 
queries; these are queries where a large number of log lines are searched, but only a few log lines match the [filtering 
expressions]({{< relref "../query/log_queries#line-filter-expression" >}}) of the query. 
Some common use cases are needing to find a specific text pattern in a message, or all logs tied to a specific customer ID.

An example of such queries would be looking for a trace ID on a whole cluster for the past 24 hours:

```logql
{cluster="prod"} |= "traceID=3c0e3dcd33e7"
```

Loki would download all the chunks for all the streams matching `{cluster=”prod”}` for the last 24 hours and iterate
through each log line in the chunks checking if the string `traceID=3c0e3dcd33e7` is present.

With accelerated filtering, Loki is able to skip most of the chunks and only process the ones where we have a 
statistical confidence that the string might be present. 
The underlying blooms are built by the [Bloom Builder](#bloom-planner-and-builder) component
and served by the new [Bloom Gateway](#bloom-gateway) component.

## Enable Query Acceleration with Blooms
{{< admonition type="warning" >}}
Building and querying bloom filters are by design not supported in single binary deployment.
It can be used with Single Scalable deployment (SSD), but it is recommended to
run bloom components only in fully distributed microservice mode.
The reason is that bloom filters also come with a relatively high cost for both building
and querying the bloom filters that only pays off at large scale deployments.
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

For more configuration options refer to the [Bloom Gateway][bloom-gateway-cfg], [Bloom Build][bloom-build-cfg] and 
[per tenant-limits][tenant-limits] configuration docs. 
We strongly recommend reading the whole documentation for this experimental feature before using it.

## Bloom Planner and Builder
Building bloom filters from the chunks in the object storage is done by two components: the Bloom Planner and the Bloom
Builder, where the planner creates tasks for bloom building, and sends the tasks to the builders to process and
upload the resulting blocks.
Bloom filters are grouped in bloom blocks spanning multiple streams (also known as series) and chunks from a given day. 
To learn more about how blocks and metadata files are organized, refer to the 
[Building and querying blooms](#building-and-querying-blooms) section below.

The Bloom Planner runs as a single instance and calculates the gaps in fingerprint ranges for a certain time period for
a tenant for which bloom filters need to be built. It dispatches these tasks to the available builders.
The planner also applies the [blooms retention](#retention). 

The Bloom Builder is a stateless horizontally scalable component and can be scaled independently of the planner to fulfill
the processing demand of the created tasks.

You can find all the configuration options for these components in the [Configure section for the Bloom Builder][bloom-build-cfg].
Refer to the [Enable Query Acceleration with Blooms](#enable-query-acceleration-with-blooms) section below for 
a configuration snippet enabling this feature.

### Retention
The Bloom Planner applies bloom block retention on object storage. Retention is disabled by default.
When enabled, retention is applied to all tenants. The retention for each tenant is the longest of its [configured][tenant-limits] 
general retention (`retention_period`) and the streams retention (`retention_stream`).

For example, in the following example, tenant A has a bloom retention of 30 days, and tenant B a bloom retention of 40 days.

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

### Sizing and configuration
The single planner instance runs the planning phase for bloom blocks for each tenant in the given interval
and puts the created tasks to an internal task queue.
Builders process tasks sequentially by pulling them from the queue. The amount of builder replicas required to complete
all pending tasks before the next planning iteration depends on the value of `-bloom-build.planner.bloom_split_series_keyspace_by`,
the amount of tenants, and the log volume of the streams.

The maximum block size is configured per tenant via `-bloom-build.max-block-size`.
The actual block size might exceed this limit given that we append streams blooms to the block until the 
block is larger than the configured maximum size. Blocks are created in memory and as soon as they are written to the 
object store they are freed. Chunks and TSDB files are downloaded from the object store to the file system. 
We estimate that builders are able to process 4MB worth of data per second per core.

## Bloom Gateway
Bloom Gateways handle chunks filtering requests from the [index gateway](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/#index-gateway). 
The service takes a list of chunks and a filtering expression and matches them against the blooms, 
filtering out those chunks not matching the given filter expression.

This component is horizontally scalable and every instance only owns a subset of the stream 
fingerprint range for which it performs the filtering. 
The sharding of the data is performed on the client side using DNS discovery of the server instances 
and the [jumphash](https://arxiv.org/abs/1406.2294) algorithm for consistent hashing 
and even distribution of the stream fingerprints across Bloom Gateway instances.

You can find all the configuration options for this component in the Configure section for the [Bloom Gateways][bloom-gateway-cfg].
Refer to the [Enable Query Acceleration with Blooms](#enable-query-acceleration-with-blooms) section below for a configuration snippet enabling this feature.

### Sizing and configuration
Bloom Gateways use their local file system as a Least Recently Used (LRU) cache for blooms that are 
downloaded from object storage. The size of the blooms depend on the ingest volume and the log content cardinality, 
as well as on build settings of the blooms, namely n-gram length, skip-factor, and false-positive-rate.
With default settings, bloom filters make up roughly 3% of the chunk data.

Example calculation for storage requirements of blooms for a single tenant.
```
100 MB/s ingest rate ~> 8.6 TB/day chunks ~> 260 GB/day blooms
```

Since reading blooms depends heavily on disk IOPS, Bloom Gateways should make use of multiple, 
locally attached SSD disks (NVMe) to increase i/o throughput. 
Multiple directories on different disk mounts can be specified using the `-bloom.shipper.working-directory` [setting][storage-config-cfg] 
when using a comma separated list of mount points, for example:
```
-bloom.shipper.working-directory="/mnt/data0,/mnt/data1,/mnt/data2,/mnt/data3"
```

Bloom Gateways need to deal with relatively large files: the bloom filter blocks. 
Even though the binary format of the bloom blocks allows for reading them into memory in smaller pages, 
the memory consumption depends on the amount of pages that are concurrently loaded into memory for processing. 
The product of three settings control the maximum amount of bloom data in memory at any given 
time: `-bloom-gateway.worker-concurrency`, `-bloom-gateway.block-query-concurrency`, and `-bloom.max-query-page-size`.

Example, assuming 4 CPU cores:
```
-bloom-gateway.worker-concurrency=4      // 1x NUM_CORES
-bloom-gateway.block-query-concurrency=8 // 2x NUM_CORES
-bloom.max-query-page-size=64MiB

4 x 8 x 64MiB = 2048MiB
```

Here, the memory requirement for block processing is 2GiB.
To get the minimum requirements for the Bloom Gateways, you need to double the value.

## Building and Querying Blooms

Blooms are built per stream and aggregated into block files. Streams are assigned to blocks based on their fingerprint, ensuring data locality and efficient querying within the same shard.

In addition to blocks, builders maintain metadata files referencing bloom blocks and their corresponding TSDB index files[_{{{CITATION{{{_1{Metadata - Wikipedia](https://en.wikipedia.org/wiki/Metadata). These metadata files help gateways and the planner discover existing blocks.

**Bloom Building Process**:
- Periodically, the planner compares the latest TSDB files with bloom metadata files.
- If there are new or changed TSDB files, the planner creates tasks for the referenced streams and chunks.
- The builder processes these tasks, iterating through new log chunks and building a bloom for each stream.
- For previously processed TSDB files, builders try to reuse existing blooms instead of creating new ones from scratch.

**Structured Metadata Integration**:
- Builders use structured metadata for each log line of each chunk, improving accuracy and performance[_{{{CITATION{{{_2{A Comprehensive Structured Data Guide - Elastic](https://www.elastic.co/what-is/structured-data).
- The metadata allows gateways to skip whole streams or individual chunks during queries[_{{{CITATION{{{_1{Metadata - Wikipedia](https://en.wikipedia.org/wiki/Metadata).

### Queries for which blooms are used

Loki will check blooms for any log filtering expression within a query that satisfies the following criteria:
- The filtering expression must align with the structured metadata[_{{{CITATION{{{_2{A Comprehensive Structured Data Guide - Elastic](https://www.elastic.co/what-is/structured-data).
- Regex filters simplify to sets of matchers for bloom queries (e.g., `|~ "(error|warn)"`).
- Only match (`|=`) and regex match (`|~`) filters use blooms.
- Filters before [line format expressions](https://grafana.com/docs/loki/latest/logql/log_queries/#line-format-expression) can use blooms.

For more information on structured metadata, you can refer to the [Wikipedia article on Metadata](https://en.wikipedia.org/wiki/Metadata)[_{{{CITATION{{{_1{Metadata - Wikipedia](https://en.wikipedia.org/wiki/Metadata).

## Query sharding
Query acceleration does not just happen while processing chunks, but also happens from the query planning phase where
the query frontend applies [query sharding](https://lokidex.com/posts/tsdb/#sharding). 
Loki 3.0 introduces a new [per-tenant configuration][tenant-limits] flag `tsdb_sharding_strategy` which defaults to computing 
shards as in previous versions of Loki by using the index stats to come up with the closest power of two that would 
optimistically divide the data to process in shards of roughly the same size. Unfortunately, 
the amount of data each stream has is often unbalanced with the rest, 
therefore, some shards end up processing more data than others.

Query acceleration introduces a new sharding strategy: `bounded`, which uses blooms to reduce the chunks to be 
processed right away during the planning phase in the query frontend, 
as well as evenly distributes the amount of chunks each sharded query will need to process.

[tenant-limits]: https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config
[bloom-gateway-cfg]: https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#bloom_gateway
[bloom-build-cfg]: https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#bloom_build
[storage-config-cfg]: https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#storage_config
[microservices]: https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#microservices-mode
[ssd]: https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#simple-scalable
