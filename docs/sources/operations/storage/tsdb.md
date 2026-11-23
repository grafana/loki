---
title: Single Store TSDB (tsdb)
menuTitle: TSDB
description: Describes the Loki time series database (TSDB) single store.
weight: 100
---
# Single Store TSDB (tsdb)

Starting with Loki v2.8, TSDB is the recommended Loki index. It is heavily inspired by the Prometheus TSDB [sub-project](https://github.com/prometheus/prometheus/tree/main/tsdb). This index is more efficient, faster, and more scalable than the BoltDB Shipper index that preceded it.

Loki 4.0 removes the BoltDB Shipper index; TSDB is the only supported index type for Loki. If your schema configuration still has a `boltdb-shipper` period, migrate to TSDB before upgrading to Loki 4.0. For migration steps, refer to [Migrate to TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-to-tsdb/).

For the on-disk binary layout of the index files, see [TSDB index format](https://grafana.com/docs/loki/<LOKI_VERSION>/reference/tsdb-index-format/).

## Example Configuration

To get started using TSDB, add the following configurations to your `config.yaml`:

```yaml
schema_config:
  configs:
    - from: "2023-01-05" # <---- A date in the past
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
  # The TSDB index dispatches many more, but each individually smaller, requests.
  # Increase the pending request queue size to compensate.
  max_outstanding_requests_per_tenant: 32768

querier:
  # Each `querier` component process runs a number of parallel workers to process queries simultaneously.
  # Adjust this up or down depending on available resources
  # (more available CPU and memory can tolerate higher values, and vice versa).
  # A value of `16` works well for many deployments running TSDB.
  max_concurrent: 16

```

{{< admonition type="note" >}}
If you are migrating an existing installation from `boltdb-shipper` to `tsdb`, add a new `schema_config` period with a future `from` date rather than replacing your existing configuration outright, so that Loki can continue to query previously ingested data. See [Migrate to TSDB](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-to-tsdb/) for details.
{{< /admonition >}}

## Operations

### Limits

The per-tenant `tsdb_max_query_parallelism` setting in `limits_config` controls how many sub-queries the query frontend schedules in parallel for TSDB queries. It is distinct from `max_query_parallelism`, which applies to the legacy index types that Loki 4.0 removes. The default is `128`, which works well for most cases. You can increase it globally in `limits_config`, or per-tenant in the `overrides` file, as needed.

### Dynamic Query Sharding

TSDB uses Dynamic Query Sharding, sharding queries based on how much data a query is going to process, rather than on a fixed number of shards.
Loki stores the size (in KB) and number of lines for each chunk in the TSDB index, and the [Query Frontend](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/components/#query-frontend) uses this information when planning the query.
By default, TSDB aims to process 300-600 MB of data per query shard.
This results in more, smaller queries than a fixed-shard approach would produce.

The per-tenant `tsdb_max_bytes_per_shard` setting in `limits_config` controls this target size. It defaults to `600MB`. Each time a query would need to process more data than this target, Loki doubles the number of shards for that query. As a result, each shard typically ends up processing between half and all of the target amount. This is why, with the default target of `600MB`, query shards typically process between 300 MB and 600 MB of data each.

Loki also supports the following related per-tenant settings in `limits_config`:

- `tsdb_sharding_strategy`: The sharding strategy to use during query planning. The default is `power_of_two`, the strategy described above. An alternative, `bounded`, is also available; refer to the [configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config) before switching. When upgrading from an earlier Loki version, confirm that all Loki components in the cluster recognize `bounded` before enabling it, to avoid issues during a rolling upgrade.
- `tsdb_precompute_chunks`: When set to `true`, Loki computes chunk references once during query planning instead of resolving them again from the index during query execution. This can reduce the number of index calls a query makes, at the cost of higher memory usage. It defaults to `false`. This setting only takes effect when `tsdb_sharding_strategy` is set to `bounded`, because the `power_of_two` strategy does not support precomputed chunk references.

### Index caching not required

TSDB is a compact and optimized format, and it is the only index type Loki supports. TSDB does not require an index cache for normal operation. An experimental `postings_cache` option is available under `tsdb_shipper` in `storage_config` for advanced use cases that cache expanded postings for downloaded per-tenant TSDB index files; refer to the [configuration reference](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#storage_config) for details.
