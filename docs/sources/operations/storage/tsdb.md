---
title: TSDB
---
# TSDB Index Store

:warning: TSDB is still an experimental feature. It is not recommended to be used in production environments.

With TSDB index store, you can run Grafana Loki without any dependency on NoSQL stores for storing index.
It works the same way as [boltdb-shipper](./boltdb-shipper) index store but with reduced TCO and better query performance.
Loki uses [Prometheus TSDB format](https://github.com/prometheus/prometheus/tree/main/tsdb#tsdb) for storing the index with 
some tweaks on top of it for query planning.

## Example Configuration

Example configuration with GCS:

```yaml
schema_config:
  configs:
    - from: 2018-04-15
      store: tsdb
      object_store: gcs
      schema: v12
      index:
        prefix: loki_index_
        period: 24h

storage_config:
  gcs:
    bucket_name: GCS_BUCKET_NAME

  tsdb_shipper:
    active_index_directory: /loki/index
    shared_store: gcs
    cache_location: /loki/tsdb-cache
```

This would run Loki with TSDB index storing TSDB files locally at `/loki/index` and chunks at configured `GCS_BUCKET_NAME`.
It would also keep shipping TSDB files periodically to same configured bucket.
It would also keep downloading TSDB files from shared bucket uploaded by other ingesters to `/loki/tsdb-cache` folder locally.

## Operational Details

Operationally, TSDB works the same way as BoltDB Shipper except the index format is different.
All the [Operational details for BoltDB Shipper](./boltdb-shipper#operational-details) apply to TSDB as well.
However, there as some additional details to TSDB index store which are mentioned below.

### Dynamic Query Sharding

*Note:* Query sharding needs to be enabled using `parallelise_shardable_queries` under [query_range](../../configuration/_index#queryrange)

Previously we would statically shard queries based on the index row shards configured [here](../../configuration/_index#periodconfig).
TSDB does Dynamic Query Sharding based on how much data a query is going to be processing.
We additionally store size(KB) and number of lines for each chunk in the TSDB index which is then used for planning the query.
Based on our learnings from operating a lot of Loki clusters, we have configured TSDB to aim for processing 300-600 MBs of data per query shard.
This means with TSDB we will be running more, smaller queries, so we recommend tuning some configs below.

```yaml
frontend_worker:
    match_max_concurrent: true # force alignment with querier max concurrency (this should always be true and isn't specific to TSDB)

frontend:
    scheduler_worker_concurrency: 60 # TSDB uses more, smaller queries

query_scheduler:
    max_outstanding_requests_per_tenant: 32768 # TSDB uses more, smaller queries
```

The values here are what we set for our deployments. You can use the same or tune them based on your usage.

### Index Caching not required

TSDB is a compact and optimized impact format.
It is so quick and efficient in processing index queries that we have not seen a need to use index caching for TSDB.
If you are already using Loki with other index types, it is recommended to keep the index caching until
all of your existing data falls out of [retention](./retention) or your configured `max_query_lookback` under [limits_config](../../configuration/_index#limitsconfig)

