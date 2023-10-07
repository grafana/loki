---
title: Caching 
menuTitle: Caching 
description: Enable and configure memcached for caching. 
weight: 
keywords:
  - memcached
  - caching
---

# Caching

Loki supports caching of index writes and lookups, chunks and query results to speed up query performance. This sections describes the recommended Memcached configuration to enable caching for chunks and query results. The index lookup cache is configured to be in-memory by default. The index lookup cache is an important component of any database system, including Loki, as it's used to cache the index lookups to speed up the read queries. Using Memcached for caching the index lookups can significantly improve the performance. It's important to note that, if Memcached is deployed for this purpose, it should be deployed as a separate instance from the one used for caching chunks and query results.

You can configure the index lookup cache in the Loki configuration under the `index_queries_cache_config` section. If you are using the Helm chart for deployment, you may need to create a separate values file like `memcached-overrides-index.yaml` to specify the configuration parameters for the Memcached instance used for the index lookup cache. Here is an example of how to configure the index lookup cache:
  
```yaml
storage_config:
  index_queries_cache_config:
    memcached:
      batch_size: 100
      parallelism: 10
    memcached_client:
      host: <memcached host>,
      service: <port name of memcached service>
      consistent_hash: true
```

In the Helm values file, you'd set the relevant parameters under loki.config similar to the other caches:

```yaml
loki:
  config:
    storage_config:
      index_queries_cache_config:
        memcached:
          batch_size: 100
          parallelism: 10
        memcached_client:
          host: <memcached host>,
          service: <port name of memcached service>
          consistent_hash: true
```

Again, please ensure that you adjust these settings according to your specific needs and environment.

## Before you begin

- It is recommended to deploy three, or in the case of the Helm chart two, dedicated Memcached clusters.
- As of 2023-02-01, the `memcached:1.6.17-alpine` version of the library is recommended.
- Consult the Loki ksonnet [memcached](https://github.com/grafana/loki/blob/main/production/ksonnet/loki/memcached.libsonnet) deployment and the ksonnet [memcached library](https://github.com/grafana/jsonnet-libs/tree/master/memcached) to understand how it is implemented by default in Loki.
- And consult the [Loki Helm Chart](https://artifacthub.io/packages/helm/grafana/loki) and the [Bitnami Memcached Helm Chart](https://artifacthub.io/packages/helm/bitnami/memcached) for additional configuration options.

## Steps

To enable and configure Memcached:

1. Deploy each Memcached service with at least three replicas. Use the [bitnami/memcached Helm Chart](https://artifacthub.io/packages/helm/bitnami/memcached) and create values files (`memcached-overrides-chunk.yaml`, `memcached-overrides-write.yaml`, `memcached-overrides-results.yaml`, `memcached-overrides-index.yaml`) with the following content for chunk, write, results and index caches respectively:

   1. For chunk cache:
   ```yaml
    # memcached-overrides-chunk.yaml
    replicaCount: 3
    # Arguments for the Memcached binary
    extraArgs:
      - "--memory-limit=4096"
      - "--max-item-size=2m"
      - "--conn-limit=1024"
    ```

    1. Query result cache:

   ```yaml
    # memcached-overrides-index.yaml
    replicaCount: 3
    # Arguments for the Memcached binary
    extraArgs:
      - "--memory-limit=1024"
      - "--max-item-size=5m"
      - "--conn-limit=1024"
    ```

  

1. Configure Loki to use the cache.
    1. If the Helm chart is used

       Set `memcached.chunk_cache.host` to the Memcached address for the chunk cache, `memcached.results_cache.host` to the Memcached address for the query result cache, `memcached.chunk_cache.enabled=true` and `memcached.results_cache.enabled=true`. 
       
       Ensure that the connection limit of Memcached is at least `number_of_clients * max_idle_conns`.
       
       The options `host` and `service` depend on the type of installation. For example, using the `bitnami/memcached` Helm Charts with the following commands, the `service` values are always `memcached`.
       ```
       helm upgrade --install chunk-cache -n loki bitnami/memcached -f memcached-overrides-chunk.yaml
       helm upgrade --install write-cache -n loki bitnami/memcached -f memcached-overides-write.yaml
       helm upgrade --install results-cache -n loki bitnami/memcached -f memcached-overides-results.yaml
       helm upgrade --install index-cache -n loki bitnami/memcached -f memcached-overides-index.yaml
       
       ```
       The current Helm Chart only supports the chunk and results cache.

       In this case, the Loki configuration would be
       ```yaml
       loki:
         memcached:
           chunk_cache:
             enabled: true
             host: chunk-cache-memcached.loki.svc
             service: memcache-client
             batch_size: 256
             parallelism: 10
           results_cache:
             enabled: true
             host: results-cache-memcached.loki.svc
             service: memcache-client
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
