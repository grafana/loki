// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _component:: 'query-scheduler',

  _resourceSelector::
    selector()
      .resource(self._component)
      .build(),

  // Histogram queries for queue duration
  // Gets the percentile of queue duration
  queue_duration_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_query_scheduler_queue_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of queue duration
  queue_duration_p99(by=''):: self.queue_duration_percentile(0.99, by),
  // Gets the 95th percentile of queue duration
  queue_duration_p95(by=''):: self.queue_duration_percentile(0.95, by),
  // Gets the 90th percentile of queue duration
  queue_duration_p90(by=''):: self.queue_duration_percentile(0.90, by),
  // Gets the 50th percentile of queue duration
  queue_duration_p50(by=''):: self.queue_duration_percentile(0.50, by),

  // Gets the average queue duration
  queue_duration_average(by='')::
    |||
      sum by (%s) (
        rate(loki_query_scheduler_queue_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_query_scheduler_queue_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Histogram queries for inflight requests
  // Gets the percentile of inflight requests
  inflight_requests_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_query_scheduler_inflight_requests_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the average inflight requests
  inflight_requests_average(by='')::
    |||
      sum by (%s) (
        rate(loki_query_scheduler_inflight_requests_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_query_scheduler_inflight_requests_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gauge metrics
  // Gets the number of connected frontend clients
  connected_frontend_clients::
    |||
      loki_query_scheduler_connected_frontend_clients{%s}
    ||| % [self._resourceSelector],

  // Gets the number of connected querier clients
  connected_querier_clients::
    |||
      loki_query_scheduler_connected_querier_clients{%s}
    ||| % [self._resourceSelector],

  // Gets the current number of inflight requests
  inflight_requests::
    |||
      loki_query_scheduler_inflight_requests{%s}
    ||| % [self._resourceSelector],

  // Gets whether the scheduler is running
  running::
    |||
      loki_query_scheduler_running{%s}
    ||| % [self._resourceSelector],
}
