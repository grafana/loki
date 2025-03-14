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

Loki supports caching of index writes and lookups, chunks and query results to
speed up query performance. This sections describes the recommended Memcached
configuration to enable caching for chunks and query results. The index lookup
cache is configured to be in-memory by default.

## Before you begin

- It is recommended to deploy three, or in the case of the Helm chart two, dedicated Memcached clusters.
- As of 2025-02-06, the `memcached:1.6.35-alpine` version of the library is recommended.
- Consult the Loki ksonnet [memcached](https://github.com/grafana/loki/blob/main/production/ksonnet/loki/memcached.libsonnet) deployment and the ksonnet [memcached library](https://github.com/grafana/jsonnet-libs/tree/master/memcached).
- Index caching is not required for the [TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/tsdb/#index-caching-not-required) index format.

## Steps

To enable and configure Memcached:

1. Deploy each Memcached service with at least three replicas and configure
   each as follows:
    1. Chunk cache 
       ```
       --memory-limit=4096 --max-item-size=2m --conn-limit=1024
       ```
    1. Query result and index queries cache
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
    1. If the Loki configuration is used, modify the following three sections in
       the Loki configuration file.
        1. Configure the chunk and index write cache
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
                   timeout: 500ms
                   update_interval: 1m
           ```
        1. Configure the index queries cache
           ```yaml
           storage_config:
             index_queries_cache_config:
               memcached:
                 batch_size: 100
                 parallelism: 100
               memcached_client:
                 host: <memcached host>
                 service: <port name of memcached service>
                 consistent_hash: true
           ```
