// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the hit rate for get commands
  get_hit_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_commands_total{command="get", status="hit", %s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(memcached_commands_total{command="get", %s}[$__rate_interval])
      )
    ||| % std.repeat([by, ''], 2),

  // Gets the connection usage ratio (current/max)
  connection_usage(by='')::
    |||
      sum by (%s) (
        memcached_current_connections{%s}
      )
      /
      sum by (%s) (
        memcached_max_connections{%s}
      )
    ||| % std.repeat([by, ''], 2),

  // Gets the total CPU usage (user + system)
  cpu_usage_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_process_user_cpu_seconds_total{%s}[$__rate_interval]) +
        rate(memcached_process_system_cpu_seconds_total{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, ''], 3),

  // Gets the percentile of memcache request duration
  request_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_memcache_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of memcache request duration
  request_duration_p99(component, by=''):: self.request_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of memcache request duration
  request_duration_p95(component, by=''):: self.request_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of memcache request duration
  request_duration_p90(component, by=''):: self.request_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of memcache request duration
  request_duration_p50(component, by=''):: self.request_duration_percentile(component, 0.50, by),

  // Gets the average memcache request duration
  request_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memcache_request_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_memcache_request_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the number of memcache client servers
  client_servers(component, by='')::
    |||
      sum by (%s) (
        loki_memcache_client_servers{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of memcache client set skips
  client_set_skip_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_memcache_client_set_skip_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets whether memcached is accepting connections
  accepting_connections(by='')::
    |||
      sum by (%s) (
        memcached_accepting_connections{%s}
      )
    ||| % [by, ''],

  // Gets the rate of memcached commands by command and status
  commands_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_commands_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the rate of memcached connections being disabled by the listener
  connections_listener_disabled_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_connections_listener_disabled_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the rate of rejected memcached connections
  connections_rejected_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_connections_rejected_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the rate of total memcached connections
  connections_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_connections_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the rate of yielded memcached connections
  connections_yielded_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_connections_yielded_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the current bytes used by memcached
  current_bytes(by='')::
    |||
      sum by (%s) (
        memcached_current_bytes{%s}
      )
    ||| % [by, ''],

  // Gets the current number of memcached connections
  current_connections(by='')::
    |||
      sum by (%s) (
        memcached_current_connections{%s}
      )
    ||| % [by, ''],

  // Gets the current number of items in memcached
  current_items(by='')::
    |||
      sum by (%s) (
        memcached_current_items{%s}
      )
    ||| % [by, ''],

  // Gets the rate of direct reclaims
  direct_reclaims_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_direct_reclaims_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the rate of no memory errors for items
  item_no_memory_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_item_no_memory_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the rate of too large items
  item_too_large_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_item_too_large_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the rate of evicted items
  items_evicted_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_items_evicted_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the rate of reclaimed items
  items_reclaimed_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_items_reclaimed_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the total number of items
  items_total(by='')::
    |||
      sum by (%s) (
        memcached_items_total{%s}
      )
    ||| % [by, ''],

  // Gets the memory limit in bytes
  limit_bytes(by='')::
    |||
      sum by (%s) (
        memcached_limit_bytes{%s}
      )
    ||| % [by, ''],

  // Gets whether the LRU crawler is enabled
  lru_crawler_enabled(by='')::
    |||
      sum by (%s) (
        memcached_lru_crawler_enabled{%s}
      )
    ||| % [by, ''],

  // Gets the rate of items checked by LRU crawler
  lru_crawler_items_checked_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_lru_crawler_items_checked_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets whether the LRU crawler maintainer thread is running
  lru_crawler_maintainer_thread(by='')::
    |||
      sum by (%s) (
        memcached_lru_crawler_maintainer_thread{%s}
      )
    ||| % [by, ''],

  // Gets the rate of LRU crawler moves to cold
  lru_crawler_moves_to_cold_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_lru_crawler_moves_to_cold_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the rate of LRU crawler moves to warm
  lru_crawler_moves_to_warm_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_lru_crawler_moves_to_warm_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the rate of LRU crawler moves within LRU
  lru_crawler_moves_within_lru_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_lru_crawler_moves_within_lru_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the rate of items reclaimed by LRU crawler
  lru_crawler_reclaimed_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_lru_crawler_reclaimed_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the LRU crawler sleep time
  lru_crawler_sleep(by='')::
    |||
      sum by (%s) (
        memcached_lru_crawler_sleep{%s}
      )
    ||| % [by, ''],

  // Gets the rate of LRU crawler starts
  lru_crawler_starts_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_lru_crawler_starts_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the number of items to crawl
  lru_crawler_to_crawl(by='')::
    |||
      sum by (%s) (
        memcached_lru_crawler_to_crawl{%s}
      )
    ||| % [by, ''],

  // Gets the bytes allocated by malloc
  malloced_bytes(by='')::
    |||
      sum by (%s) (
        memcached_malloced_bytes{%s}
      )
    ||| % [by, ''],

  // Gets the maximum number of connections
  max_connections(by='')::
    |||
      sum by (%s) (
        memcached_max_connections{%s}
      )
    ||| % [by, ''],

  // Gets the rate of bytes read
  read_bytes_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_read_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets the rate of bytes written
  written_bytes_rate(by='')::
    |||
      sum by (%s) (
        rate(memcached_written_bytes_total{%s}[$__rate_interval])
      )
    ||| % [by, ''],

  // Gets whether memcached is up
  up(by='')::
    |||
      sum by (%s) (
        memcached_up{%s}
      )
    ||| % [by, ''],

  // Gets the uptime in seconds
  uptime_seconds(by='')::
    |||
      sum by (%s) (
        memcached_uptime_seconds{%s}
      )
    ||| % [by, ''],

  // Gets the version info
  version(by='')::
    |||
      sum by (%s) (
        memcached_version{%s}
      )
    ||| % [by, ''],
}
