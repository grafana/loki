---
title: Configure caches to speed up queries
menuTitle: Caching 
description: Describes how to enable and configure caching, including Memcached, to improve query performance. 
weight: 
keywords:
  - memcached
  - caching
---
# Configure caches to speed up queries

Loki supports caching for query results and chunks to speed up query performance and reduce calls to the storage layer. Memcached is included in the Loki Helm chart and enabled by default for the `chunksCache` and `resultsCache`.
This section describes the recommended Memcached configuration to enable caching for chunks and query results.

## Cache backends

Every cache in Loki (`chunk_cache_config`, `results_cache`, and so on) can use one of three interchangeable backends: Memcached, Redis, or an embedded (in-process) cache. Memcached is the backend used throughout this page and is the recommended choice for production, multi-replica deployments, because it is shared across all instances of a component. Redis is supported as an alternative external cache. The embedded cache stores data in the memory of the Loki process itself, so it isn't shared between replicas; it is best suited to single-binary or small deployments. Configure only one backend per cache. If you configure both Memcached and Redis for the same cache, Loki fails to start.

If you don't configure Memcached or Redis for a cache, Loki automatically enables the embedded cache for it, so that caching still works out of the box. This applies to the chunks cache and to all six results caches. Because the embedded cache is per-process, this automatic fallback is not a substitute for Memcached or Redis in a deployment with more than one replica.

For more information about which caching backend to use for each cache, refer to [Configure caches](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#cache_config) and [Components](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components). For help diagnosing cache configuration problems, refer to [Troubleshoot operations](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/troubleshooting/troubleshoot-operations/).

### Results cache

The results cache stores results so that Loki can reuse them on subsequent queries, and it supports negative caching for log queries. It is sometimes called the frontend cache in some configurations. The results cache is consulted by query frontends. If the cached results are incomplete, the query frontend calculates the required sub-queries and sends them further along to be executed in queriers, then also caches those results.
To orchestrate all of the above, the results cache uses a query hash as the key that is computed and stored in the headers.

The results cache is really a set of six independently configurable caches under `query_range`, one per query type:

| Query type | Enable with | Cache config |
| --- | --- | --- |
| Metric and log queries | `cache_results` | `results_cache` |
| Index-stats queries | `cache_index_stats_results` | `index_stats_results_cache` |
| Volume queries | `cache_volume_results` | `volume_results_cache` |
| Instant metric queries | `cache_instant_metric_results` | `instant_metric_results_cache` |
| Series queries | `cache_series_results` | `series_results_cache` |
| Label queries | `cache_label_results` | `label_results_cache` |

If a per-query-type cache is enabled but its cache config is left empty, Loki falls back to the `results_cache` configuration for that query type. This means you only need to configure `results_cache` to get caching for all query types, and you only need to give a query type its own cache config if you want it to use a different Memcached, Redis, or embedded cache instance. For details of each supported request type, refer to the [Components section](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components).

The index lookup cache and the write dedupe cache were removed together with support for the legacy BoltDB index storage. For details, refer to the [upgrade guide](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/). TSDB, the only index type Loki now supports, doesn't use an index cache; for more information, refer to [Index Caching not required](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/tsdb/#index-caching-not-required).

### Chunks cache

The chunks are cached using the `chunkRef` as the cache key, which is the unique reference to a chunk when it's cut in the Loki ingesters.
The chunk cache is consulted by queriers each time a set of `chunkRef`s are calculated to serve the query, before going to the storage layer.

Query results are significantly smaller compared to chunks. As the Loki cluster gets bigger in ingested volume, the results cache can continue to perform, whereas the chunks cache will need to grow in proportion to demand more memory.
To support the growing needs of a cluster, Loki added support for memcached-extstore. Extstore is a Memcached feature that supports attaching SSD disks to Memcached pods to maximize their capacity.

For more information about Loki's experience with memcached-extstore for Grafana Cloud, refer to this [blog post](https://grafana.com/blog/2023/08/23/how-we-scaled-grafana-cloud-logs-memcached-cluster-to-50tb-and-improved-reliability/).
For more information about how to tune memcached-extstore, refer to the open source [memcached documentation](https://docs.memcached.org/advisories/grafanaloki/).

### Skip cache for old chunks

The `skip_query_writeback_cache_older_than` setting in `chunk_store_config` (CLI flag: `-store.skip-query-writeback-older-than`) controls how far back chunks are eligible for caching. When set to a positive duration, chunks whose start time is older than that duration are not looked up in any cache tier and are not written back to the cache after being fetched from object storage. This prevents rarely queried historical data from evicting more frequently accessed recent chunks.

A value of `0` (the default) disables this threshold, so that all chunks are cached regardless of age.

```yaml
chunk_store_config:
  skip_query_writeback_cache_older_than: 72h
```

This setting applies to both the primary (L1) and L2 chunk caches. It is independent of `l2_chunk_cache_handoff`: chunks that fall within the handoff window are routed to the appropriate cache tier as usual, but chunks older than `skip_query_writeback_cache_older_than` bypass all cache tiers entirely.

### L2 chunks cache

As an alternative, or a complement, to memcached-extstore, Loki supports a second, "L2" chunk cache tier configured with `chunk_cache_config_l2` and `l2_chunk_cache_handoff`. The `l2_chunk_cache_handoff` setting is an age threshold. Chunks younger than this age are written to and read from the primary (L1) cache that you configure in `chunk_cache_config`. Chunks older than this age are written to and read from the L2 cache instead. A value of `0` disables the L2 cache.

Loki does not copy or move entries between the two tiers. Each chunk is routed to one tier or the other based on its age at the time of the request. On the read path, Loki widens the threshold by 10% so that chunks near the boundary are still looked up in the L1 cache.

This lets you keep a small, fast cache of recent chunks while routing older, less frequently accessed chunks to a larger and cheaper second tier, such as a disk-backed Memcached instance.

```yaml
chunk_store_config:
  l2_chunk_cache_handoff: 24h
  chunk_cache_config_l2:
    memcached:
      batch_size: 256
      parallelism: 10
    memcached_client:
      host: <l2 chunk cache memcached host>
      service: <port name of memcached service>
```

If the Helm chart is used, the L2 cache is configured under `chunksCache.l2`, which is disabled by default (`chunksCache.l2.enabled: false`) and defaults to a four day handoff (`chunksCache.l2.l2ChunkCacheHandoff: 345600s`). The Helm chart's `chunksCache.l2.persistence` settings can attach a persistent volume to the L2 cache pods, which is a chart-native alternative to memcached-extstore for adding disk-backed capacity.

## Before you begin

- Deploy separate Memcached instances for the chunks cache and the results cache (`memcached_chunks` and `memcached_frontend` in Tanka deployments).
- Use the Memcached image version shipped with the current release of the [Loki Helm chart](https://github.com/grafana-community/helm-charts/blob/main/charts/loki/values.yaml) (`memcached.image.tag`) rather than pinning to a specific version here, since the recommended version changes over time.
- Consult the Loki Tanka [memcached](https://github.com/grafana/loki/blob/main/production/ksonnet/loki/memcached.libsonnet) deployment and the [memcached library](https://github.com/grafana/jsonnet-libs/tree/master/memcached).
- For recommendations on scaling the cache, refer to the [Size the cluster](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/size/) page.

## Steps

To enable and configure Memcached:

1. Deploy each Memcached service with at least three replicas and configure
   each as follows:
    1. Chunk cache

       ```text
       --memory-limit=8192 --max-item-size=5m --conn-limit=16384
       ```

    1. Query result cache

       ```text
       --memory-limit=1024 --max-item-size=5m --conn-limit=16384
       ```

1. Configure Loki to use the cache.
    1. If the Helm chart is used

       Set `chunksCache.addresses` to the Memcached address for the chunk cache and `resultsCache.addresses` to the Memcached address for the query result cache, then set `chunksCache.enabled=true` and `resultsCache.enabled=true`.

       Ensure that the connection limit of Memcached is at least `number_of_clients * max_idle_conns`.

       By default, the chart also deploys and manages the Memcached servers themselves. To use your own Memcached instances instead, set `memcached.enabled` to `false` to stop the chart from deploying its built-in Memcached server, keep `chunksCache.enabled` and `resultsCache.enabled` set to `true` so that Loki still uses a Memcached-based cache, and point the `addresses` values at your own service. Use the same comma-separated DNS Service Discovery format that the chart uses by default:

       ```yaml
       memcached:
         enabled: false
       chunksCache:
         enabled: true
         addresses: "dnssrvnoa+_memcached-client._tcp.chunk-cache-memcached.loki.svc"
       resultsCache:
         enabled: true
         addresses: "dnssrvnoa+_memcached-client._tcp.results-cache-memcached.loki.svc"
       ```

       The Helm chart's `chunksCache` and `resultsCache` values only manage the chunk cache and the main query-range results cache. The other five results caches (`index_stats_results_cache`, `volume_results_cache`, `instant_metric_results_cache`, `series_results_cache`, and `label_results_cache`) don't have dedicated Helm values. You can still enable and configure them by adding the settings to the chart's `loki.query_range` value, which the chart copies into the Loki configuration file as it is written. For example:

       ```yaml
       loki:
         query_range:
           cache_index_stats_results: true
           cache_volume_results: true
       ```

    1. If the Loki configuration is used, modify the following two sections in
       the Loki configuration file.
        1. Configure the chunk cache

           ```yaml
           chunk_store_config:
             chunk_cache_config:
               memcached:
                 batch_size: 256
                 parallelism: 10
               memcached_client:
                 host: <chunk cache memcached host>
                 service: <port name of memcached service>
           ```

        1. Configure the query result cache

           ```yaml
           query_range:
             cache_results: true
             results_cache:
               cache:
                 memcached_client:
                   consistent_hash: true
                   host: <memcached host>
                   service: <port name of memcached service>
                   max_idle_conns: 16
                   timeout: 200ms
                   update_interval: 1m
           ```
