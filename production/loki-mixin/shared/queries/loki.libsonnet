// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the rate of deprecated flags in use
  deprecated_flags_inuse_rate(component, by='')::
    |||
      sum by (%s) (
        rate(deprecated_flags_inuse_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the build info
  build_info(component, by='')::
    |||
      loki_build_info{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the percentile of bytes per line
  bytes_per_line_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bytes_per_line_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of bytes per line
  bytes_per_line_p99(component, by='')::
    self.bytes_per_line_percentile(component, 0.99, by),

  // Gets the 95th percentile of bytes per line
  bytes_per_line_p95(component, by='')::
    self.bytes_per_line_percentile(component, 0.95, by),

  // Gets the 90th percentile of bytes per line
  bytes_per_line_p90(component, by='')::
    self.bytes_per_line_percentile(component, 0.90, by),

  // Gets the 50th percentile of bytes per line
  bytes_per_line_p50(component, by='')::
    self.bytes_per_line_percentile(component, 0.50, by),

  // Gets the average bytes per line
  bytes_per_line_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_bytes_per_line_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bytes_per_line_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the rate of discarded bytes
  discarded_bytes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_discarded_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of discarded samples
  discarded_samples_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_discarded_samples_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of DNS failures
  dns_failures_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_dns_failures_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of DNS lookups
  dns_lookups_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_dns_lookups_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the DNS provider results
  dns_provider_results(component, by='')::
    |||
      loki_dns_provider_results{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the rate of experimental features in use
  experimental_features_in_use_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_experimental_features_in_use_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the number of inflight requests
  inflight_requests(component, by='')::
    |||
      loki_inflight_requests{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the percentile of KV request duration
  kv_request_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_kv_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of KV request duration
  kv_request_duration_p99(component, by='')::
    self.kv_request_duration_percentile(component, 0.99, by),

  // Gets the 95th percentile of KV request duration
  kv_request_duration_p95(component, by='')::
    self.kv_request_duration_percentile(component, 0.95, by),

  // Gets the 90th percentile of KV request duration
  kv_request_duration_p90(component, by='')::
    self.kv_request_duration_percentile(component, 0.90, by),

  // Gets the 50th percentile of KV request duration
  kv_request_duration_p50(component, by='')::
    self.kv_request_duration_percentile(component, 0.50, by),

  // Gets the average KV request duration
  kv_request_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_kv_request_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_kv_request_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets whether the lifecycler is read only
  lifecycler_read_only(component, by='')::
    |||
      loki_lifecycler_read_only{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the percentile of log flushes
  log_flushes_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_log_flushes_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of log flushes
  log_flushes_p99(component, by='')::
    self.log_flushes_percentile(component, 0.99, by),

  // Gets the 95th percentile of log flushes
  log_flushes_p95(component, by='')::
    self.log_flushes_percentile(component, 0.95, by),

  // Gets the 90th percentile of log flushes
  log_flushes_p90(component, by='')::
    self.log_flushes_percentile(component, 0.90, by),

  // Gets the 50th percentile of log flushes
  log_flushes_p50(component, by='')::
    self.log_flushes_percentile(component, 0.50, by),

  // Gets the average log flushes
  log_flushes_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_log_flushes_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_log_flushes_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the rate of log messages
  log_messages_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_log_messages_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of consul heartbeats
  member_consul_heartbeats_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_member_consul_heartbeats_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the available buffers per slab
  mempool_available_buffers_per_slab(component, by='')::
    |||
      loki_mempool_available_buffers_per_slab{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the percentile of mempool wait duration
  mempool_wait_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_mempool_wait_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of mempool wait duration
  mempool_wait_duration_p99(component, by='')::
    self.mempool_wait_duration_percentile(component, 0.99, by),

  // Gets the 95th percentile of mempool wait duration
  mempool_wait_duration_p95(component, by='')::
    self.mempool_wait_duration_percentile(component, 0.95, by),

  // Gets the 90th percentile of mempool wait duration
  mempool_wait_duration_p90(component, by='')::
    self.mempool_wait_duration_percentile(component, 0.90, by),

  // Gets the 50th percentile of mempool wait duration
  mempool_wait_duration_p50(component, by='')::
    self.mempool_wait_duration_percentile(component, 0.50, by),

  // Gets the rate of mutated bytes
  mutated_bytes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_mutated_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of mutated samples
  mutated_samples_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_mutated_samples_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of panics
  panic_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_panic_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the runtime config hash
  runtime_config_hash(component, by='')::
    |||
      loki_runtime_config_hash{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets whether the last runtime config reload was successful
  runtime_config_last_reload_successful(component, by='')::
    |||
      loki_runtime_config_last_reload_successful{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the stream sharding count
  stream_sharding_count(component, by='')::
    |||
      loki_stream_sharding_count{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the number of TCP connections
  tcp_connections(component, by='')::
    |||
      loki_tcp_connections{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the TCP connections limit
  tcp_connections_limit(component, by='')::
    |||
      loki_tcp_connections_limit{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the rate of discarded write failures
  write_failures_discarded_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_write_failures_discarded_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of logged write failures
  write_failures_logged_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_write_failures_logged_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],
}
