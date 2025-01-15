{
  chunks_downloaded: (import './chunks-downloaded.libsonnet').new,
  frontend_retries: import './frontend-retries.libsonnet',
  frontend_sharded_queries: import './sharded-queries.libsonnet',
  frontend_partitions: import './frontend-partitions.libsonnet',
  latency: (import './latency.libsonnet').new,
  shard_factor: import './shard-factor.libsonnet',
  sharding_parsed_queries: import './sharding-parsed-queries.libsonnet',
  qps: (import './qps.libsonnet').new,
  throughput: (import './throughput.libsonnet').new,
  throughput_percentiles: (import './throughput-percentiles.libsonnet').new,
}
