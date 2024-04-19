---
title: Single Store TSDB (tsdb)
menuTitle: TSDB
description: Describes the Loki time series database (TSDB) single store.
weight: 100
---
# Single Store TSDB (tsdb)

Starting with Loki v2.8, TSDB is the recommended Loki index. It is heavily inspired by the Prometheus's TSDB [sub-project](https://github.com/prometheus/prometheus/tree/main/tsdb). For a deeper explanation you can read Loki maintainer Owen's [blog post](https://lokidex.com/posts/tsdb/). The short version is that this new index is more efficient, faster, and more scalable. It also resides in object storage like the [boltdb-shipper]({{< relref "./boltdb-shipper" >}}) index which preceded it.

## Example Configuration

To get started using TSDB, add the following configurations to your `config.yaml`:

```yaml
schema_config:
  configs:
    # Old boltdb-shipper schema. Included in example for reference but does not need changing.
    - from: "2023-01-03" # <---- A date in the past
      index:
        period: 24h
        prefix: index_
      object_store: gcs
      schema: v12
      store: boltdb-shipper
    # New TSDB schema below
    - from: "2023-01-05" # <---- A date in the future
      index:
        period: 24h
        prefix: index_
      object_store: gcs
      schema: v13
      store: tsdb

storage_config:
  # Old boltdb-shipper configuration. Included in example for reference but does not need changing.
  boltdb_shipper:
    active_index_directory: /data/index
    build_per_tenant_index: true
    cache_location: /data/boltdb-cache
    index_gateway_client:
      # only applicable if using microservices where index-gateways are independently deployed.
      # This example is using kubernetes-style naming.
      server_address: dns:///index-gateway.<namespace>.svc.cluster.local:9095
  # New tsdb-shipper configuration
  tsdb_shipper:
    active_index_directory: /data/tsdb-index
    cache_location: /data/tsdb-cache
    index_gateway_client:
      # only applicable if using microservices where index-gateways are independently deployed.
      # This example is using kubernetes-style naming.
      server_address: dns:///index-gateway.<namespace>.svc.cluster.local:9095

query_scheduler:
  # the TSDB index dispatches many more, but each individually smaller, requests. 
  # We increase the pending request queue sizes to compensate.
  max_outstanding_requests_per_tenant: 32768

querier:
  # Each `querier` component process runs a number of parallel workers to process queries simultaneously.
  # You may want to adjust this up or down depending on your resource usage
  # (more available cpu and memory can tolerate higher values and vice versa),
  # but we find the most success running at around `16` with tsdb
  max_concurrent: 16

```

## Operations

### Limits

We've added a user per-tenant limit called `tsdb_max_query_parallelism` in the `limits_config`. This functions the same as the prior `max_query_parallelism` configuration but applies to tsdb queries instead. Since the TSDB index will create many more smaller queries compared to the other index types before it, we've added a separate configuration so they can coexist. This is helpful when transitioning between index types. The default parallelism is `512` which should work well for most cases, but you can extend it globally in the `limits_config` or per-tenant in the `overrides` file as needed.

### Dynamic Query Sharding

Previously we would statically shard queries based on the index row shards configured [here](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#period_config).
TSDB does Dynamic Query Sharding based on how much data a query is going to be processing.
We additionally store size(KB) and number of lines for each chunk in the TSDB index which is then used by the [Query Frontend]({{< relref "../../get-started/components#query-frontend" >}}) for planning the query.
Based on our experience from operating many Loki clusters, we have configured TSDB to aim for processing 300-600 MBs of data per query shard.
This means with TSDB we will be running more, smaller queries.

### Index Caching not required

TSDB is a compact and optimized format. Loki does not currently use an index cache for TSDB. If you are already using Loki with other index types, it is recommended to keep the index caching until all of your existing data falls out of [retention](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/storage/retention/)) or your configured `max_query_lookback` under [limits_config](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config). After that, we suggest running without an index cache (it isn't used in TSDB).
