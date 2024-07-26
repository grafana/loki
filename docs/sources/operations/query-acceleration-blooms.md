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
The underlying blooms are built by the new [Bloom Compactor](#bloom-compactor) component 
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
- Deploy the [Bloom Compactor](#bloom-compactor) component and enable the component in the [Bloom Compactor config][compactor-cfg].
- Deploy the [Bloom Gateway](#bloom-gateway) component (as a [microservice][microservices] or via the [SSD][ssd] Backend target) and enable the component in the [Bloom Gateway config][gateway-cfg].
- Enable blooms filtering and compaction for each tenant individually, or for all of them by default.

```yaml
bloom_compactor:
  enabled: true

bloom_gateway:
  enabled: true
  client:
    addresses: dnssrvnoa+_bloom-gateway-grpc._tcp.bloom-gateway-headless.<namespace>.svc.cluster.local

# Enable blooms filtering and compaction for all tenants by default
limits_config:
  bloom_gateway_enable_filtering: true
  bloom_compactor_enable_compaction: true
```

For more configuration options refer to the [Bloom Gateways][gateway-cfg], [Bloom Compactor][compactor-cfg] and 
[per tenant-limits][tenant-limits] configuration docs. 
We strongly recommend reading the whole documentation for this experimental feature before using it.

## Bloom Compactor
The Bloom Compactor component builds blooms from the chunks in the object store. 
The resulting blooms are grouped in bloom blocks spanning multiple streams (also known as series) and chunks from a given day. 
To learn more about how blocks and metadata files are organized, refer to the 
[Building and querying blooms](#building-and-querying-blooms) section below.

Bloom Compactors are horizontally scalable and use a [ring] for sharding tenants and stream fingerprints, 
as well as determining which compactor should apply [blooms retention](#retention). 
Each compactor owns a configurable number of contiguous streams fingerprint ranges for a tenant. 
The compactor builds blooms for all the chunks from the tenant streams whose fingerprint 
falls within its owned key-space ranges.

You can find all the configuration options for this component in the [Configure section for the Bloom Compactor][compactor-cfg].
Refer to the [Enable Query Acceleration with Blooms](#enable-query-acceleration-with-blooms) section below for 
a configuration snippet enabling this feature.

### Retention
One Bloom Compactor from all those running will apply retention. Retention is disabled by default.
The instance owning the smallest token in the ring owns retention. 
Retention is applied to all tenants. The retention for each tenant is the longest of its [configured][tenant-limits] 
general retention (`retention_period`) and the streams retention (`retention_stream`).

For example, in the following example, tenant A has a bloom retention of 30 days, 
and tenant B a bloom retention of 40 days.

```yaml
overrides:
    "A": 
        retention: 30d
    "B":
        retention: 30d
        retention_stream:
            - selector: '{namespace="prod"}'
              priority: 1
              period: 40d
```

### Sizing
Compactors build blocks concurrently. Concurrency is [configured][compactor-cfg] via `-bloom-compactor.worker-parallelism`.
Each worker will build bloom blocks from streams and chunks. 
The maximum block size is configured per tenant via `-bloom-compactor.max-block-size`. 
Note that the actual block size might exceed this limit given that we append streams blooms to the block until the 
block is larger than the configured maximum size. Blocks are created in memory and as soon as they are written to the 
object store they are freed. Chunks and TSDB files are downloaded from the object store to the file system. 
We estimate that compactors are able to process 4 MB worth of data per second per core.

## Bloom Gateway
Bloom Gateways handle chunks filtering requests from the [index gateway](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/#index-gateway). 
The service takes a list of chunks and a filtering expression and matches them against the blooms, 
filtering out those chunks not matching the given filter expression.

This component is horizontally scalable and every instance only owns a subset of the stream 
fingerprint range for which it performs the filtering. 
The sharding of the data is performed on the client side using DNS discovery of the server instances 
and the [jumphash](https://arxiv.org/abs/1406.2294) algorithm for consistent hashing 
and even distribution of the stream fingerprints across Bloom Gateway instances.

You can find all the configuration options for this component in the Configure section for the [Bloom Gateways][gateway-cfg].
Refer to the [Enable Query Acceleration with Blooms](#enable-query-acceleration-with-blooms) section below for a configuration snippet enabling this feature.

### Sizing
Bloom Gateways use their local filesystem as a Least Recently Used (LRU) cache for blooms that are 
downloaded from object storage. The size of the blooms depend on the ingest volume and the log content cardinality, 
as well as on compaction settings of the blooms, namely n-gram length, skip-factor, and false-positive-rate.
With default settings, bloom filters make up roughly 3% of the chunk data.

Example calculation for storage requirements of blooms for a single tenant.
```
100 MB/s ingest rate ~> 8.6 TB/day chunks ~> 260 GB/day blooms
```

Since reading blooms depends heavily on disk IOPS, Bloom Gateways should make use of multiple, 
locally attached SSD disks (NVMe) to increase i/o throughput. 
Multiple directories on different disk mounts can be specified using the `-bloom.shipper.working-directory` [setting][gateway-cfg] 
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

## Building and querying blooms
Bloom filters are built per stream and aggregated together into block files. 
Streams are assigned to blocks by their fingerprint, following the same ordering scheme as Loki’s TSDB and sharding calculation.
This gives a data locality benefit when querying as streams in the same shard are likely to be in the same block.

In addition to blocks, compactors maintain a list of metadata files containing references to bloom blocks and the 
TSDB index files they were built from. They also contain tombstones for old blocks which are outdated and 
can be deleted in future iterations. Gateways and compactors use these metadata files to discover existing blocks.

Every `-bloom-compactor.compaction-interval`, compactors will load the latest TSDB files for all tenants for
which bloom compaction is enabled, and compare the TSDB files with the latest bloom metadata files. 
If there are new TSDB files or any of them have changed, the compactor will process all the streams and chunks pointed 
by the TSDB file. In case of changes for a previously processed TSDB file, 
compactors will try to reuse blooms from existing blocks instead of building new ones from scratch.

For a given stream, the compactor owning that stream will iterate through all the log lines inside its new 
chunks and build a bloom for the stream. For each log line, we compute its [n-grams](https://en.wikipedia.org/wiki/N-gram#:~:text=An%20n%2Dgram%20is%20a,pairs%20extracted%20from%20a%20genome.) 
and append to the bloom both the hash for each n-gram and the hash for each n-gram plus the chunk identifier. 
The former allows gateways to skip whole streams while the latter is for skipping individual chunks.

For example, given a log line `abcdef` in the chunk `c6dj8g`, we compute its n-grams: `abc`, `bcd`, `cde`, `def`. 
And append to the stream bloom the following hashes: `hash("abc")`, `hash("abc" + "c6dj8g")` ... `hash("def")`, `hash("def" + "c6dj8g")`.

By adding n-grams to blooms instead of whole log lines, we can perform partial matches. 
For the example above, a filter expression `|= "bcd"` would match against the bloom.
The filter `|= "bcde` would also match the bloom since we decompose the filter into n-grams: 
`bcd`, `cde` which both are present in the bloom.

N-grams sizes are configurable. The longer the n-gram is, the fewer tokens we need to append to the blooms, 
but the longer filtering expressions need to be able to check them against blooms. 
For the example above, where the n-gram length is 3, we need filtering expressions that have at least 3 characters.

### Queries for which blooms are used
Loki will check blooms for any log filtering expression within a query that satisfies the following criteria:
- The filtering expression contains at least as many characters as the n-gram length used to build the blooms.
  - For example, if the n-grams length is 5, the filter `|= "foo"` will not take advantage of blooms but `|= "foobar"` would.
- If the filter is a regex, we use blooms only if we can simplify the regex to a set of simple matchers.
  - For example, `|~ "(error|warn)"` would be simplified into `|= "error" or "warn"` thus would make use of blooms, 
    whereas `|~ "f.*oo"` would not be simplifiable.
- The filtering expression is a match (`|=`) or regex match (`|~`) filter. We don’t use blooms for not equal (`!=`) or not regex (`!~`) expressions.
  - For example, `|= "level=error"` would use blooms but `!= "level=error"` would not.
- The filtering expression is placed before a [line format expression](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#line-format-expression).
  - For example, with `|= "level=error" | logfmt | line_format "ERROR {{.err}}" |= "traceID=3ksn8d4jj3"`, 
    the first filter (`|= "level=error"`) will benefit from blooms but the second one (`|= "traceID=3ksn8d4jj3"`) will not.

## Query sharding
Query acceleration does not just happen while processing chunks, 
but also happens from the query planning phase where the query frontend applies [query sharding](https://lokidex.com/posts/tsdb/#sharding). 
Loki 3.0 introduces a new {per-tenant configuration][tenant-limits] flag `tsdb_sharding_strategy` which defaults to computing 
shards as in previous versions of Loki by using the index stats to come up with the closest power of two that would 
optimistically divide the data to process in shards of roughly the same size. Unfortunately, 
the amount of data each stream has is often unbalanced with the rest, 
therefore, some shards end up processing more data than others.

Query acceleration introduces a new sharding strategy: `bounded`, which uses blooms to reduce the chunks to be 
processed right away during the planning phase in the query frontend, 
as well as evenly distributes the amount of chunks each sharded query will need to process.

[ring]: https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/hash-rings/
[tenant-limits]: https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config
[gateway-cfg]: https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#bloom_gateway
[compactor-cfg]: https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#bloom_compactor
[microservices]: https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#microservices-mode
[ssd]: https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#simple-scalable
