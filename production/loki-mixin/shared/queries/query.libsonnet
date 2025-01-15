// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the rate of batches that failed to be sent
  batches_failed_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_query_counter_batches_failed_to_be_sent{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of batches successfully sent
  batches_success_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_query_counter_batches_successfully_sent{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of lines that failed to be sent
  lines_failed_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_query_counter_lines_failed_to_be_sent{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of lines observed
  lines_observed_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_query_counter_lines_observed{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of lines successfully sent
  lines_success_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_query_counter_lines_successfully_sent{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of send duration
  send_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_query_counter_send_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of query duration
  query_duration_p99(component, by=''):: self.query_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of query duration
  query_duration_p95(component, by=''):: self.query_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of query duration
  query_duration_p90(component, by=''):: self.query_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of query duration
  query_duration_p50(component, by=''):: self.query_duration_percentile(component, 0.50, by),

  // Gets the average query duration
  query_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(logql_query_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(logql_query_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of query duration
  query_duration_histogram(component)::
    |||
      sum by (le) (
        rate(logql_query_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the batch success ratio
  batch_success_ratio(component, by='')::
    |||
      sum by (%s) (rate(loki_query_counter_batches_successfully_sent{%s}[$__rate_interval]))
      /
      (
        sum by (%s) (rate(loki_query_counter_batches_successfully_sent{%s}[$__rate_interval]))
        +
        sum by (%s) (rate(loki_query_counter_batches_failed_to_be_sent{%s}[$__rate_interval]))
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 3),

  // Gets the line success ratio
  line_success_ratio(component, by='')::
    |||
      sum by (%s) (rate(loki_query_counter_lines_successfully_sent{%s}[$__rate_interval]))
      /
      sum by (%s) (rate(loki_query_counter_lines_observed{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),


  // Gets the percentile of query duration
  query_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(logql_query_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of send duration
  send_duration_p99(component, by=''):: self.send_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of send duration
  send_duration_p95(component, by=''):: self.send_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of send duration
  send_duration_p90(component, by=''):: self.send_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of send duration
  send_duration_p50(component, by=''):: self.send_duration_percentile(component, 0.50, by),

  // Gets the average send duration
  send_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_query_counter_send_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_query_counter_send_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of send duration
  send_duration_histogram(component)::
    |||
      sum by (le) (
        rate(loki_query_counter_send_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the rate of hedged requests rate limited
  hedged_requests_rate_limited(component, by='')::
    |||
      sum by (%s) (
        rate(loki_hedged_requests_rate_limited_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of hedged requests total
  hedged_requests_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_hedged_requests_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of hedged requests won
  hedged_requests_won(component, by='')::
    |||
      sum by (%s) (
        rate(loki_hedged_requests_won_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],
}
