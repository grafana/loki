---
title: Single Store TSDB (tsdb)
menuTitle: TSDB
description: Describes the Loki time series database (TSDB) single store.
weight: 100
---
# Single Store TSDB (tsdb)

TSDB is the only supported Loki index. It is heavily inspired by the Prometheus TSDB [sub-project](https://github.com/prometheus/prometheus/tree/main/tsdb). This index is efficient, fast, and scalable, and resides in object storage.

For the on-disk binary layout of the index files, see [TSDB index format](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/tsdb-index-format/).

## Example Configuration

To get started using TSDB, add the following configurations to your `config.yaml`:

{{< admonition type="note" >}}
If you are migrating from a legacy index type such as `boltdb-shipper`, do the migration while still running Loki 3.x: add a new TSDB period with a future `from` date, keep the old period entries until your retention period has passed, then remove them, and only then upgrade to Loki 4.0. Loki 4.0 fails to start if any period, including past periods, still refers to a removed index type such as `boltdb-shipper`. Refer to [Migrate to TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-to-tsdb/) for the full migration procedure.
{{< /admonition >}}

```yaml
schema_config:
  configs:
    - from: "2023-01-05"
      index:
        period: 24h
        prefix: index_
      object_store: gcs
      schema: v13
      store: tsdb

storage_config:
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

The per-tenant limit `tsdb_max_query_parallelism` in the `limits_config` controls the maximum number of parallel TSDB queries. The default parallelism is `128` which should work well for most cases, but you can extend it globally in the `limits_config` or per-tenant in the `overrides` file as needed.

### Dynamic Query Sharding

Previously we would statically shard queries based on the index row shards configured [here](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#period_config).
TSDB does Dynamic Query Sharding based on how much data a query is going to be processing.
We additionally store size(KB) and number of lines for each chunk in the TSDB index which is then used by the [Query Frontend](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/#query-frontend) for planning the query.
Based on our experience from operating many Loki clusters, we have configured TSDB to aim for processing 300-600 MBs of data per query shard.
This means with TSDB we will be running more, smaller queries.

The per-tenant `tsdb_max_bytes_per_shard` setting in `limits_config` controls this target size. It defaults to `600MB`. Each time a query would need to process more data than this target, Loki doubles the number of shards for that query. As a result, each shard typically ends up processing between half and all of the target amount. This is why, with the default target of `600MB`, query shards typically process between 300 MB and 600 MB of data each.

Loki also supports the following related per-tenant settings in `limits_config`:

- `tsdb_sharding_strategy`: The sharding strategy to use during query planning. The default is `power_of_two`, the strategy described above. An alternative, `bounded`, is also available; refer to the [configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config) before switching, since not all Loki components in a cluster may support it during a rolling upgrade.
- `tsdb_precompute_chunks`: When set to `true`, Loki computes chunk references once during query planning instead of resolving them again from the index during query execution. This can reduce the number of index calls a query makes, at the cost of higher memory usage. It defaults to `false`. This setting only takes effect when `tsdb_sharding_strategy` is set to `bounded`, because the `power_of_two` strategy does not support precomputed chunk references.

### Index Caching not required

TSDB is a compact and optimized format and the only index type Loki supports. Loki doesn't use or need an index cache for it.
