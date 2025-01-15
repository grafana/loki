// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _component:: 'gateway',

  _resourceSelector::
    selector()
      .resource('gateway')
      .build(),

  // Gets the rate of skipped backends in bounded load balancer
  logs_proxy_grpc_bounded_load_balancer_backend_skipped_rate(by='')::
    |||
      sum by (%s) (
        rate(gateway_logs_proxy_grpc_bounded_load_balancer_backend_skipped_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of connections in bounded load balancer
  logs_proxy_grpc_bounded_load_balancer_connections_rate(by='')::
    |||
      sum by (%s) (
        rate(gateway_logs_proxy_grpc_bounded_load_balancer_connections_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of RPCs in bounded load balancer
  logs_proxy_grpc_bounded_load_balancer_rpc_rate(by='')::
    |||
      sum by (%s) (
        rate(gateway_logs_proxy_grpc_bounded_load_balancer_rpc_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the rate of request errors
  logs_proxy_requests_errors_rate(by='')::
    |||
      sum by (%s) (
        rate(gateway_logs_proxy_requests_errors_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gets the percentile of downstream request duration
  logs_proxy_request_downstream_duration_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(gateway_logs_proxy_request_downstream_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of downstream request duration
  logs_proxy_request_downstream_duration_p99(by='')::
    self.logs_proxy_request_downstream_duration_percentile(0.99, by),

  // Gets the 95th percentile of downstream request duration
  logs_proxy_request_downstream_duration_p95(by='')::
    self.logs_proxy_request_downstream_duration_percentile(0.95, by),

  // Gets the 90th percentile of downstream request duration
  logs_proxy_request_downstream_duration_p90(by='')::
    self.logs_proxy_request_downstream_duration_percentile(0.90, by),

  // Gets the 50th percentile of downstream request duration
  logs_proxy_request_downstream_duration_p50(by='')::
    self.logs_proxy_request_downstream_duration_percentile(0.50, by),

  // Gets the average downstream request duration
  logs_proxy_request_downstream_duration_average(by='')::
    |||
      sum by (%s) (
        rate(gateway_logs_proxy_request_downstream_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(gateway_logs_proxy_request_downstream_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the histogram of downstream request duration
  logs_proxy_request_downstream_duration_histogram(by='')::
    |||
      sum by (le) (
        rate(gateway_logs_proxy_request_downstream_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],
}
