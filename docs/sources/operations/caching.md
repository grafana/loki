---
title: Configure caches to speed up queries
menuTitle: Caching 
description: Describes how to enable and configure memcached to improve query performance. 
weight: 
keywords:
  - memcached
  - caching
---
# Configure caches to speed up queries

Loki supports two types of caching for query results and chunks to speed up query performance and reduce calls to the storage layer. Memcached is included in the Loki Helm chart and enabled by default for the `chunksCache` and `resultsCache`.
This sections describes the recommended Memcached configuration to enable caching for chunks and query results.

#### Results cache
The results cache stores the results for index-stat, instant-metric, label and volume queries and it supports negative caching for log queries. It is sometimes called frontend cache in some configurations. For details of each supported request type, refer to the [Components section](https://grafana.com/docs/loki/<LOKI_VERSION>//get-started/components).
The results cache is consulted by query-frontends to be used in subsequent queries. If the cached results are incomplete, the query frontend calculates the required sub-queries and sends them further along to be executed in queriers, then also caches those results.
To orchestrate all of the above, the results cache uses a query hash as the key that is computed and stored in the headers.

The index lookup cache only supports the legacy BoltDB index storage and is configured to be in-memory by default.
Since moving to the TSDB indexes the attached disks/persistent volumes are utilised as cache and in-memory index lookup cache is obsolete.

#### Chunks cache
The chunks are cached using the `chunkRef` as the cache key, which is the unique reference to a chunk when it's cut in the Loki ingesters.
The chunk cache is consulted by queriers each time a set of `chunkRef`s are calculated to serve the query, before going to the storage layer.

Query results are significantly smaller compared to chunks. As the Loki cluster gets bigger in ingested volume, the results cache can continue to perform, whereas the chunks cache will need to grow in proportion to demand more memory.
To be able to support the growing needs of a cluster, in 2023 we introduced support for memcached-extstore. Extstore is an additional feature on Memcached which supports attaching SSD disks to memcached pods to maximize their capacity.

Please see this [blog post](https://grafana.com/blog/2023/08/23/how-we-scaled-grafana-cloud-logs-memcached-cluster-to-50tb-and-improved-reliability/) on Loki's experience with memcached-extstore for our SaaS offfering, Grafana Cloud.
For more information on how to tune memcached-extstore please consult the open source [memcached documentation](https://docs.memcached.org/advisories/grafanaloki/).

## Before you begin

- It is recommended to deploy separate Memcached type as separate components (`memcached_frontend` and `memcached_chunks`).
- As of 2025-02-06, the `memcached:1.6.32-alpine` version of the library is recommended.
- Consult the Loki ksonnet [memcached](https://github.com/grafana/loki/blob/main/production/ksonnet/loki/memcached.libsonnet) deployment and the ksonnet [memcached library](https://github.com/grafana/jsonnet-libs/tree/master/memcached).
- Index caching is not required for the [TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/tsdb/#index-caching-not-required) index format.
- For recommendations on scaling the cache, refer to the [Size the cluster](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/size/) page.

## Steps

To enable and configure Memcached:

1. Deploy each Memcached service with at least three replicas and configure
   each as follows:
    1. Chunk cache 
       ```
       --memory-limit=4096 --max-item-size=2m --conn-limit=1024
       ```
    1. Query result cache
       ```
       --memory-limit=1024 --max-item-size=5m --conn-limit=1024
       ```

1. Configure Loki to use the cache.
    1. If the Helm chart is used

       Set `memcached.chunk_cache.host` to the Memcached address for the chunk cache, `memcached.results_cache.host` to the Memcached address for the query result cache, `memcached.chunk_cache.enabled=true` and `memcached.results_cache.enabled=true`. 
       
       Ensure that the connection limit of Memcached is at least `number_of_clients * max_idle_conns`.
       
       The options `host` and `service` depend on the type of installation. For example, using the `bitnami/memcached` Helm Charts with the following commands, the `service` values are always `memcached`.
       ```
       helm upgrade --install chunk-cache -n loki bitnami/memcached -f memcached-overrides-chunk.yaml
       helm upgrade --install results-cache -n loki bitnami/memcached -f memcached-overrides-results.yaml
       ```
       The current Helm Chart only supports the chunk and results cache.

       In this case, the Loki configuration would be
       ```yaml
       loki:
         memcached:
           chunk_cache:
             enabled: true
             host: chunk-cache-memcached.loki.svc
             service: memcached-client
             batch_size: 256
             parallelism: 10
           results_cache:
             enabled: true
             host: results-cache-memcached.loki.svc
             service: memcached-client
             default_validity: 12h
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