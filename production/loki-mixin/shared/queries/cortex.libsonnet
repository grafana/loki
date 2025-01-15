// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the number of admin API clients
  admin_api_clients(component, by='')::
    |||
      sum by (%s) (
        cortex_admin_api_clients{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets whether this client is the leader
  admin_client_is_leader(component, by='')::
    |||
      sum by (%s) (
        cortex_admin_client_is_leader{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of auth failures
  gateway_auth_failures_rate(component, by='')::
    |||
      sum by (%s) (
        rate(cortex_gateway_auth_failures_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of KV request duration
  kv_request_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(cortex_kv_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of KV request duration
  kv_request_duration_p99(component, by=''):: self.kv_request_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of KV request duration
  kv_request_duration_p95(component, by=''):: self.kv_request_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of KV request duration
  kv_request_duration_p90(component, by=''):: self.kv_request_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of KV request duration
  kv_request_duration_p50(component, by=''):: self.kv_request_duration_percentile(component, 0.50, by),

  // Gets the average KV request duration
  kv_request_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(cortex_kv_request_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(cortex_kv_request_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of KV request duration
  kv_request_duration_histogram(component)::
    |||
      sum by (le) (
        rate(cortex_kv_request_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the cgroup CPU max
  quota_cgroup_cpu_max(component, by='')::
    |||
      sum by (%s) (
        cortex_quota_cgroup_cpu_max{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the cgroup CPU period
  quota_cgroup_cpu_period(component, by='')::
    |||
      sum by (%s) (
        cortex_quota_cgroup_cpu_period{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the CPU count
  quota_cpu_count(component, by='')::
    |||
      sum by (%s) (
        cortex_quota_cpu_count{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the GOMAXPROCS value
  quota_gomaxprocs(component, by='')::
    |||
      sum by (%s) (
        cortex_quota_gomaxprocs{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],
}
