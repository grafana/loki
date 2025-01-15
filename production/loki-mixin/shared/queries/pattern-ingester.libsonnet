// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _component:: 'pattern-ingester',

  _resourceSelector::
    selector()
      .resource(self._component)
      .build(),

  // Histogram queries for client request duration
  // Gets the percentile of client request duration
  client_request_duration_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_pattern_ingester_client_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of client request duration
  client_request_duration_p99(by=''):: self.client_request_duration_percentile(0.99, by),
  // Gets the 95th percentile of client request duration
  client_request_duration_p95(by=''):: self.client_request_duration_percentile(0.95, by),
  // Gets the 90th percentile of client request duration
  client_request_duration_p90(by=''):: self.client_request_duration_percentile(0.90, by),
  // Gets the 50th percentile of client request duration
  client_request_duration_p50(by=''):: self.client_request_duration_percentile(0.50, by),

  // Gets the average client request duration
  client_request_duration_average(by='')::
    |||
      sum by (%s) (
        rate(loki_pattern_ingester_client_request_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_pattern_ingester_client_request_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Histogram queries for state per line
  // Gets the percentile of state size per line
  state_per_line_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_pattern_ingester_state_per_line_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of state size per line
  state_per_line_p99(by=''):: self.state_per_line_percentile(0.99, by),
  // Gets the 95th percentile of state size per line
  state_per_line_p95(by=''):: self.state_per_line_percentile(0.95, by),
  // Gets the 90th percentile of state size per line
  state_per_line_p90(by=''):: self.state_per_line_percentile(0.90, by),
  // Gets the 50th percentile of state size per line
  state_per_line_p50(by=''):: self.state_per_line_percentile(0.50, by),

  // Gets the average state size per line
  state_per_line_average(by='')::
    |||
      sum by (%s) (
        rate(loki_pattern_ingester_state_per_line_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_pattern_ingester_state_per_line_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Histogram queries for tokens per line
  // Gets the percentile of tokens per line
  tokens_per_line_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_pattern_ingester_tokens_per_line_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of tokens per line
  tokens_per_line_p99(by=''):: self.tokens_per_line_percentile(0.99, by),
  // Gets the 95th percentile of tokens per line
  tokens_per_line_p95(by=''):: self.tokens_per_line_percentile(0.95, by),
  // Gets the 90th percentile of tokens per line
  tokens_per_line_p90(by=''):: self.tokens_per_line_percentile(0.90, by),
  // Gets the 50th percentile of tokens per line
  tokens_per_line_p50(by=''):: self.tokens_per_line_percentile(0.50, by),

  // Gets the average tokens per line
  tokens_per_line_average(by='')::
    |||
      sum by (%s) (
        rate(loki_pattern_ingester_tokens_per_line_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_pattern_ingester_tokens_per_line_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Counter rates
  // Gets the rate of appends
  appends_rate::
    |||
      sum(
        rate(loki_pattern_ingester_appends_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of metric appends
  metric_appends_rate::
    |||
      sum(
        rate(loki_pattern_ingester_metric_appends_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of patterns detected
  patterns_detected_rate::
    |||
      sum(
        rate(loki_pattern_ingester_patterns_detected_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of patterns evicted
  patterns_evicted_rate::
    |||
      sum(
        rate(loki_pattern_ingester_patterns_evicted_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of queries pruned
  query_pruned_rate::
    |||
      sum(
        rate(loki_pattern_ingester_query_pruned_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of queries retained
  query_retained_rate::
    |||
      sum(
        rate(loki_pattern_ingester_query_retained_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of teed requests
  teed_requests_rate::
    |||
      sum(
        rate(loki_pattern_ingester_teed_requests_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of teed streams
  teed_streams_rate::
    |||
      sum(
        rate(loki_pattern_ingester_teed_streams_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gauge metrics
  // Gets the number of connected clients
  clients::
    |||
      loki_pattern_ingester_clients{%s}
    ||| % [self._resourceSelector],

  // Gets the length of the flush queue
  flush_queue_length::
    |||
      loki_pattern_ingester_flush_queue_length{%s}
    ||| % [self._resourceSelector],
}
