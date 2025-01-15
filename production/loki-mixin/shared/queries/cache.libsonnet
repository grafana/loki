// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the percentile of cache request duration
  request_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_cache_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of cache request duration
  request_duration_p99(component, by=''):: self.request_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of cache request duration
  request_duration_p95(component, by=''):: self.request_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of cache request duration
  request_duration_p90(component, by=''):: self.request_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of cache request duration
  request_duration_p50(component, by=''):: self.request_duration_percentile(component, 0.50, by),

  // Gets the average cache request duration
  request_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_cache_request_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_cache_request_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the percentile of cache value size in bytes
  value_size_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_cache_value_size_bytes_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of cache value size
  value_size_p99(component, by=''):: self.value_size_percentile(component, 0.99, by),
  // Gets the 95th percentile of cache value size
  value_size_p95(component, by=''):: self.value_size_percentile(component, 0.95, by),
  // Gets the 90th percentile of cache value size
  value_size_p90(component, by=''):: self.value_size_percentile(component, 0.90, by),
  // Gets the 50th percentile of cache value size
  value_size_p50(component, by=''):: self.value_size_percentile(component, 0.50, by),

  // Gets the average cache value size
  value_size_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_cache_value_size_bytes_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_cache_value_size_bytes_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the rate of background bytes dequeued
  background_dequeued_bytes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_cache_background_dequeued_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of background bytes enqueued
  background_enqueued_bytes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_cache_background_enqueued_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the current background queue size in bytes
  background_queue_bytes(component, by='')::
    |||
      sum by (%s) (
        loki_cache_background_queue_bytes{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the current background queue length
  background_queue_length(component, by='')::
    |||
      sum by (%s) (
        loki_cache_background_queue_length{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of corrupt chunks
  corrupt_chunks_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_cache_corrupt_chunks_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of dropped background writes in bytes
  dropped_background_writes_bytes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_cache_dropped_background_writes_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of dropped background writes
  dropped_background_writes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_cache_dropped_background_writes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the number of fetched keys
  fetched_keys(component, by='')::
    |||
      sum by (%s) (
        loki_cache_fetched_keys{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the number of cache hits
  hits(component, by='')::
    |||
      sum by (%s) (
        loki_cache_hits{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the cache hit ratio
  hit_ratio(component, by='')::
    |||
      sum by (%s) (
        loki_cache_hits{%s}
      )
      /
      sum by (%s) (
        loki_cache_fetched_keys{%s}
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the cache miss ratio
  miss_ratio(component, by='')::
    |||
      (
        sum by (%s) (
          loki_cache_fetched_keys{%s}
        )
        -
        sum by (%s) (
          loki_cache_hits{%s})
        )
      /
      sum by (%s) (
        loki_cache_fetched_keys{%s}
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 3),

  // Gets the percentile of admin cache response duration
  admin_cache_response_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(admin_cache_response_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of admin cache response duration
  admin_cache_response_duration_p99(component, by=''):: self.admin_cache_response_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of admin cache response duration
  admin_cache_response_duration_p95(component, by=''):: self.admin_cache_response_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of admin cache response duration
  admin_cache_response_duration_p90(component, by=''):: self.admin_cache_response_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of admin cache response duration
  admin_cache_response_duration_p50(component, by=''):: self.admin_cache_response_duration_percentile(component, 0.50, by),

  // Gets the average admin cache response duration
  admin_cache_response_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(admin_cache_response_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(admin_cache_response_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the rate of admin cache evictions
  admin_cache_evictions_rate(component, by='')::
    |||
      sum by (%s) (rate(admin_cache_evictions_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of admin cache hits
  admin_cache_hits_rate(component, by='')::
    |||
      sum by (%s) (rate(admin_cache_hits_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of admin cache inserts
  admin_cache_inserts_rate(component, by='')::
    |||
      sum by (%s) (rate(admin_cache_inserts_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of admin cache misses
  admin_cache_misses_rate(component, by='')::
    |||
      sum by (%s) (rate(admin_cache_misses_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of authentication cache response duration
  auth_cache_response_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(authentication_cache_response_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of authentication cache response duration
  auth_cache_response_duration_p99(component, by=''):: self.auth_cache_response_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of authentication cache response duration
  auth_cache_response_duration_p95(component, by=''):: self.auth_cache_response_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of authentication cache response duration
  auth_cache_response_duration_p90(component, by=''):: self.auth_cache_response_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of authentication cache response duration
  auth_cache_response_duration_p50(component, by=''):: self.auth_cache_response_duration_percentile(component, 0.50, by),

  // Gets the average authentication cache response duration
  auth_cache_response_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(authentication_cache_response_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(authentication_cache_response_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the rate of authentication cache evictions
  auth_cache_evictions_rate(component, by='')::
    |||
      sum by (%s) (rate(authentication_cache_evictions_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of authentication cache hits
  auth_cache_hits_rate(component, by='')::
    |||
      sum by (%s) (rate(authentication_cache_hits_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of authentication cache inserts
  auth_cache_inserts_rate(component, by='')::
    |||
      sum by (%s) (rate(authentication_cache_inserts_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of authentication cache misses
  auth_cache_misses_rate(component, by='')::
    |||
      sum by (%s) (rate(authentication_cache_misses_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of instance validator cache response duration
  validator_cache_response_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(instance_validator_cache_response_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of instance validator cache response duration
  validator_cache_response_duration_p99(component, by=''):: self.validator_cache_response_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of instance validator cache response duration
  validator_cache_response_duration_p95(component, by=''):: self.validator_cache_response_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of instance validator cache response duration
  validator_cache_response_duration_p90(component, by=''):: self.validator_cache_response_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of instance validator cache response duration
  validator_cache_response_duration_p50(component, by=''):: self.validator_cache_response_duration_percentile(component, 0.50, by),

  // Gets the average instance validator cache response duration
  validator_cache_response_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(instance_validator_cache_response_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(instance_validator_cache_response_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the rate of instance validator cache evictions
  validator_cache_evictions_rate(component, by='')::
    |||
      sum by (%s) (rate(instance_validator_cache_evictions_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of instance validator cache hits
  validator_cache_hits_rate(component, by='')::
    |||
      sum by (%s) (rate(instance_validator_cache_hits_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of instance validator cache inserts
  validator_cache_inserts_rate(component, by='')::
    |||
      sum by (%s) (rate(instance_validator_cache_inserts_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of instance validator cache misses
  validator_cache_misses_rate(component, by='')::
    |||
      sum by (%s) (rate(instance_validator_cache_misses_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of embedded cache additions
  embedded_cache_added_rate(component, by='')::
    |||
      sum by (%s) (rate(loki_embeddedcache_added_new_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the number of embedded cache entries
  embedded_cache_entries(component, by='')::
    |||
      sum by (%s) (loki_embeddedcache_entries{%s})
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of embedded cache evictions
  embedded_cache_evicted_rate(component, by='')::
    |||
      sum by (%s) (rate(loki_embeddedcache_evicted_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the embedded cache memory usage in bytes
  embedded_cache_memory_bytes(component, by='')::
    |||
      sum by (%s) (loki_embeddedcache_memory_bytes{%s})
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of index cache corruptions
  index_cache_corruptions_rate(component, by='')::
    |||
      sum by (%s) (rate(loki_querier_index_cache_corruptions_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of index cache encode errors
  index_cache_encode_errors_rate(component, by='')::
    |||
      sum by (%s) (rate(loki_querier_index_cache_encode_errors_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of index cache gets
  index_cache_gets_rate(component, by='')::
    |||
      sum by (%s) (rate(loki_querier_index_cache_gets_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of index cache hits
  index_cache_hits_rate(component, by='')::
    |||
      sum by (%s) (rate(loki_querier_index_cache_hits_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of index cache puts
  index_cache_puts_rate(component, by='')::
    |||
      sum by (%s) (rate(loki_querier_index_cache_puts_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of results cache version comparisons
  results_cache_version_comparisons_rate(component, by='')::
    |||
      sum by (%s) (rate(loki_results_cache_version_comparisons_total{%s}[$__rate_interval]))
    ||| % [by, self._resourceSelector(component).build()],
}
