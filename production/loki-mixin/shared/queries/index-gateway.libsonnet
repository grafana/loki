// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector:: selector().resource('index-gateway').build(),

  // Histogram queries
  // Gets the percentile of request duration
  request_duration_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_index_gateway_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of request duration
  request_duration_p99(component, by=config.labels.resource_selector)::
    self.request_duration_percentile(component, 0.99, by),

  // Gets the 95th percentile of request duration
  request_duration_p95(component, by=config.labels.resource_selector)::
    self.request_duration_percentile(component, 0.95, by),

  // Gets the 90th percentile of request duration
  request_duration_p90(component, by=config.labels.resource_selector)::
    self.request_duration_percentile(component, 0.90, by),

  // Gets the 50th percentile of request duration
  request_duration_p50(component, by=config.labels.resource_selector)::
    self.request_duration_percentile(component, 0.50, by),

  // Gets the average request duration
  request_duration_average(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_index_gateway_request_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_index_gateway_request_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the percentile of postfilter chunks
  postfilter_chunks_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_index_gateway_postfilter_chunks_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the percentile of prefilter chunks
  prefilter_chunks_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_index_gateway_prefilter_chunks_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Counter rates
  // Gets the rate of total requests
  requests_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_index_gateway_requests_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector],

  // Gauge metrics
  // Gets the number of connected clients
  clients(component, by=config.labels.resource_selector)::
    |||
      loki_index_gateway_clients{%s}
    ||| % [self._resourceSelector],

  // Calculated metrics
  // Gets the filtering efficiency ratio (postfilter/prefilter)
  filtering_efficiency_ratio(component, by=config.labels.resource_selector)::
    |||
      (
        sum by (%s) (rate(loki_index_gateway_prefilter_chunks_sum{%s}[$__rate_interval]))
        -
        sum by (%s) (rate(loki_index_gateway_postfilter_chunks_sum{%s}[$__rate_interval]))
      )
      /
      sum by (%s) (rate(loki_index_gateway_prefilter_chunks_sum{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector], 3),

  // Gets the request error ratio
  request_error_ratio(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (rate(loki_index_gateway_requests_total{status_code=~"5..|4..", %s}[$__rate_interval]))
      /
      sum by (%s) (rate(loki_index_gateway_requests_total{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the request success ratio
  request_success_ratio(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (rate(loki_index_gateway_requests_total{status_code=~"2..", %s}[$__rate_interval]))
      /
      sum by (%s) (rate(loki_index_gateway_requests_total{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector], 2),
}
