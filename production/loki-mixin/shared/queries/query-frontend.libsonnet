// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _component:: 'query-frontend',

  _resourceSelector::
    selector()
      .resource(self._component)
      .build(),

  // Histogram queries for retries
  // Gets the percentile of retries per query
  retries_percentile(percentile)::
    |||
      histogram_quantile(
        %g,
        sum by (le) (
          rate(loki_query_frontend_retries_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of retries per query
  retries_p99:: self.retries_percentile(0.99),
  // Gets the 95th percentile of retries per query
  retries_p95:: self.retries_percentile(0.95),
  // Gets the 90th percentile of retries per query
  retries_p90:: self.retries_percentile(0.90),
  // Gets the 50th percentile of retries per query
  retries_p50:: self.retries_percentile(0.50),

  // Gets the average retries per query
  retries_average::
    |||
      sum(
        rate(loki_query_frontend_retries_sum{%s}[$__rate_interval])
      )
      /
      sum(
        rate(loki_query_frontend_retries_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([self._resourceSelector], 2),

  // Histogram queries for partitions
  // Gets the percentile of partitions per query
  partitions_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le) (
          rate(loki_query_frontend_partitions_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of partitions per query
  partitions_p99:: self.partitions_percentile(0.99),
  // Gets the 95th percentile of partitions per query
  partitions_p95:: self.partitions_percentile(0.95),
  // Gets the 90th percentile of partitions per query
  partitions_p90:: self.partitions_percentile(0.90),
  // Gets the 50th percentile of partitions per query
  partitions_p50:: self.partitions_percentile(0.50),

  // Gets the average partitions per query
  partitions_average::
    |||
      sum(
        rate(loki_query_frontend_partitions_sum{%s}[$__rate_interval])
      )
      /
      sum(
        rate(loki_query_frontend_partitions_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([self._resourceSelector], 2),

  // Histogram queries for shard factor
  // Gets the percentile of shard factor per query
  shard_factor_percentile(percentile)::
    |||
      histogram_quantile(
        %g,
        sum by (le) (
          rate(loki_query_frontend_shard_factor_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of shard factor per query
  shard_factor_p99:: self.shard_factor_percentile(0.99),
  // Gets the 95th percentile of shard factor per query
  shard_factor_p95:: self.shard_factor_percentile(0.95),
  // Gets the 90th percentile of shard factor per query
  shard_factor_p90:: self.shard_factor_percentile(0.90),
  // Gets the 50th percentile of shard factor per query
  shard_factor_p50:: self.shard_factor_percentile(0.50),

  // Gets the average shard factor per query
  shard_factor_average::
    |||
      sum(
        rate(loki_query_frontend_shard_factor_sum{%s}[$__rate_interval])
      )
      /
      sum(
        rate(loki_query_frontend_shard_factor_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([self._resourceSelector], 2),

  // Counter rates
  // Gets the rate of cache hits
  log_result_cache_hit_rate::
    |||
      sum(
        rate(loki_query_frontend_log_result_cache_hit_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of cache misses
  log_result_cache_miss_rate::
    |||
      sum(
        rate(loki_query_frontend_log_result_cache_miss_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gauge metrics
  // Gets the number of connected schedulers
  connected_schedulers::
    |||
      loki_query_frontend_connected_schedulers{%s}
    ||| % [self._resourceSelector],

  // Gets the number of queries in progress
  queries_in_progress::
    |||
      loki_query_frontend_queries_in_progress{%s}
    ||| % [self._resourceSelector],

  // Calculated metrics
  // Gets the cache hit ratio
  log_result_cache_hit_ratio::
    |||
      sum(rate(loki_query_frontend_log_result_cache_hit_total{%s}[$__rate_interval]))
      /
      (
        sum(rate(loki_query_frontend_log_result_cache_hit_total{%s}[$__rate_interval]))
        +
        sum(rate(loki_query_frontend_log_result_cache_miss_total{%s}[$__rate_interval]))
      )
    ||| % std.repeat([self._resourceSelector], 3),

  // gets the number of queries that bypassed sharding
  sharding_bypassed_queries_percent::
    |||
      sum(
        rate(loki_frontend_query_range_duration_seconds_count{%s, method="sharding-bypass"}[$__rate_interval])
      )
      /
      sum(
        rate(loki_frontend_query_range_duration_seconds_count{%s, method=~"(sharding-bypass|shardingware)"}[$__rate_interval])
      )
    ||| % std.repeat([self._resourceSelector], 2),

  // gets the percentage of sharded queries
  sharded_queries_percent::
    |||
      (
        sum(
          rate(loki_frontend_query_range_duration_seconds_count{%s, method="shardingware"}[$__rate_interval])
        )
        -
        (
          sum(
            rate(loki_query_frontend_sharding_parsed_queries_total{%s, type!="success"}[$__rate_interval])
          )
          or vector(0)
        )
      )
      /
      sum(
        rate(loki_frontend_query_range_duration_seconds_count{%s, method=~"(sharding-bypass|shardingware)"}[$__rate_interval])
      )
    ||| % std.repeat([self._resourceSelector], 3),
}
